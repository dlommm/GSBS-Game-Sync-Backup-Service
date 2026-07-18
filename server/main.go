package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/metrics"
	"github.com/gsbs/gsbs/server/netutil"
	"github.com/gsbs/gsbs/server/ratelimit"
)

// responseRecorder wraps http.ResponseWriter to capture status code for logging.
// It implements http.Flusher so SSE and other streaming handlers work through this middleware.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.NewResponseController reach the underlying connection
// (for per-request write deadlines) through this wrapper.
func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// draining is set to 1 when the server is shutting down; new requests get 503.
var draining int32

func requestID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); id != "" {
		return id
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// recoverMiddleware catches panics in downstream handlers, logs them with the
// stack trace and request-id, and returns a structured 500 response so the
// server process keeps running.  API paths get JSON; all others get plain text.
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				stack := debug.Stack()
				rid := requestID(r)
				logx.Logger().Error().
					Str("request_id", rid).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("panic", fmt.Sprintf("%v", v)).
					Str("stack", string(stack)).
					Msg("panic recovered")
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal error"}`))
				} else {
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets HTTP security headers on every response.
// HSTS is only emitted when the connection is HTTPS (direct TLS or X-Forwarded-Proto: https).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// No 'unsafe-inline' and no external hosts: all scripts are external
		// (/static/*.js), all styling is in app.css or applied via the CSSOM
		// by app.js (inline scripts, on*= handlers, and style="" attributes
		// are guarded against by template_csp_test.go), and fonts are
		// vendored woff2 served from /static/fonts/.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self'; "+
				"font-src 'self'; "+
				"img-src 'self' data:; "+
				"connect-src 'self'")
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// logRequests wraps a handler to log every request, return 503 when draining, and optionally record metrics.
func logRequests(next http.Handler, mc *metrics.Collector) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&draining) != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable","error":"server shutting down"}`))
			return
		}
		rid := requestID(r)
		w.Header().Set("X-Request-ID", rid)
		start := time.Now()
		// Per-request write deadline bounds slow-reader clients on every
		// route except the SSE streams, which roll their own deadline
		// forward on each event/heartbeat write. (http.Server.WriteTimeout
		// must stay 0 for those long-lived streams.)
		if r.URL.Path != "/api/events" && r.URL.Path != "/dashboard/events" {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute))
		}
		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		if mc != nil {
			mpath := metrics.NormalizePath(r.URL.Path)
			mc.Record(mpath, rec.status)
			mc.RecordDuration(mpath, time.Since(start))
		}
		logx.Logger().Info().
			Str("component", "http").
			Str("event", "http.request").
			Str("request_id", rid).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rec.status).
			Str("ip", netutil.ClientIP(r)).
			Dur("duration", time.Since(start).Round(time.Millisecond)).
			Msg(fmt.Sprintf("%s %s %d", r.Method, r.URL.Path, rec.status))
	})
}

func rateLimiterFromEnv(envKey string, defaultLimit int, defaultWindow time.Duration) *ratelimit.Limiter {
	if v := os.Getenv(envKey); v != "" {
		if limit, window := parseRateLimit(v); limit > 0 && window > 0 {
			logx.Logger().Info().Str("key", envKey).Int("limit", limit).Dur("window", window).Msg("rate limit: custom")
			return ratelimit.New(limit, window)
		}
	}
	logx.Logger().Info().Str("key", envKey).Int("limit", defaultLimit).Dur("window", defaultWindow).Msg("rate limit: default")
	return ratelimit.New(defaultLimit, defaultWindow)
}

func metricsAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(hdr, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		provided := strings.TrimSpace(hdr[7:])
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		if err := runRestore(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "restore:", err)
			os.Exit(1)
		}
		return
	}
	opts, err := parseCLIOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if opts.showVersion {
		printVersion()
		return
	}
	if err := runServerWithOptions(opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseRateLimit parses "N,duration" e.g. "60,1m" -> (60, 1*time.Minute). Returns 0,0 on parse error.
func parseRateLimit(s string) (limit int, window time.Duration) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	limit, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || limit <= 0 {
		return 0, 0
	}
	window, err = time.ParseDuration(strings.TrimSpace(parts[1]))
	if err != nil || window <= 0 {
		return 0, 0
	}
	return limit, window
}

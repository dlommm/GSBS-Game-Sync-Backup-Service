package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gsbs/gsbs/server/api"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/metrics"
	"github.com/gsbs/gsbs/server/netutil"
	"github.com/gsbs/gsbs/server/ratelimit"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
	"github.com/gsbs/gsbs/server/webui"
	"github.com/robfig/cron/v3"
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
		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		if mc != nil {
			mc.Record(r.URL.Path, rec.status)
			mc.RecordDuration(r.URL.Path, time.Since(start))
		}
		logx.Logger().Info().
			Str("request_id", rid).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rec.status).
			Str("ip", netutil.ClientIP(r)).
			Dur("duration", time.Since(start).Round(time.Millisecond)).
			Msg("request")
	})
}

func rateLimiterFromEnv(envKey string, defaultLimit int, defaultWindow time.Duration) *ratelimit.Limiter {
	if v := os.Getenv(envKey); v != "" {
		if limit, window := parseRateLimit(v); limit > 0 && window > 0 {
			log.Printf("rate limit: %s %d per %s", envKey, limit, window)
			return ratelimit.New(limit, window)
		}
	}
	log.Printf("rate limit: %s default %d per %s", envKey, defaultLimit, defaultWindow)
	return ratelimit.New(defaultLimit, defaultWindow)
}

func metricsAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimSpace(auth[7:]) != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version" || os.Args[1] == "-v") {
		printVersion()
		return
	}
	logx.Init()
	dbPath := os.Getenv("GSBS_DB")
	if dbPath == "" {
		dbPath = "gsbs.db"
	}
	st, err := store.NewSQLite(dbPath)
	if err != nil {
		log.Fatal("store:", err)
	}
	defer st.Close()
	log.Printf("database: opened %s", dbPath)
	if adminUser := os.Getenv("GSBS_ADMIN_USERNAME"); adminUser != "" {
		if err := st.EnsureAdminByUsername(context.Background(), adminUser); err != nil {
			log.Printf("ensure admin username %q: %v", adminUser, err)
		}
	}

	authSvc := auth.NewService(st)

	allowRegister := true
	if v := os.Getenv("GSBS_ALLOW_REGISTER"); strings.EqualFold(v, "false") || v == "0" {
		allowRegister = false
		log.Println("Public registration is DISABLED (set GSBS_ALLOW_REGISTER=true to enable)")
	}

	sessionSecret := os.Getenv("GSBS_SESSION_SECRET")
	if sessionSecret == "" || sessionSecret == "gsbs-default-secret-change-me" {
		log.Println("WARNING: GSBS_SESSION_SECRET is not set or is default; set a strong secret in production")
	}

	hub := sse.NewHub()
	var runner *job.Runner

	authLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_AUTH", 20, time.Minute)
	pushLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_PUSH", 120, time.Minute)
	pullLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_PULL", 60, time.Minute)
	generalLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_GENERAL", 300, time.Minute)
	manifestLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_MANIFEST", 60, time.Minute)

	maxStorageBytes := int64(0)
	if v := os.Getenv("GSBS_MAX_STORAGE_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			maxStorageBytes = n
			if n > 0 {
				log.Printf("global storage limit: %d bytes", n)
			}
		}
	}
	readOnly := strings.EqualFold(os.Getenv("GSBS_READ_ONLY"), "true") || os.Getenv("GSBS_READ_ONLY") == "1"
	if readOnly {
		log.Println("server is in READ-ONLY mode (push/delete disabled)")
	}
	apiHandler := api.NewHandler(st, authSvc, allowRegister, hub, authLimiter, pushLimiter, pullLimiter, generalLimiter, manifestLimiter, maxStorageBytes, readOnly, sessionSecret, Version)
	runner = job.NewRunner(st, hub, apiHandler)
	webHandler := webui.NewWebHandler(st, authSvc, sessionSecret, os.Getenv("GSBS_ADMIN_USERNAME"), allowRegister, hub, apiHandler, runner, maxStorageBytes, readOnly, authLimiter)
	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(webui.StaticFiles())))
	mux.Handle("/", webHandler)
	var metricsCollector *metrics.Collector
	if os.Getenv("GSBS_METRICS") == "1" {
		metricsCollector = metrics.NewCollector(st, hub)
		metricsHandler := metricsAuth(os.Getenv("GSBS_METRICS_TOKEN"), metricsCollector)
		mux.Handle("/metrics", metricsHandler)
		if os.Getenv("GSBS_METRICS_TOKEN") != "" {
			log.Println("metrics: enabled at GET /metrics (Bearer token required)")
		} else {
			log.Println("metrics: enabled at GET /metrics")
		}
	}
	handler := logRequests(mux, metricsCollector)

	pcgwCron := os.Getenv("GSBS_PCGW_CRON")
	if pcgwCron == "" {
		pcgwCron = "0 3 * * 0"
	}
	c := cron.New()
	id, err := c.AddFunc(pcgwCron, func() {
		runner.RunPCGWSync(context.Background())
	})
	if err != nil {
		log.Printf("cron: failed to schedule PCGW sync %q: %v", pcgwCron, err)
	} else {
		_ = id
		log.Printf("cron: PCGW sync scheduled %s", pcgwCron)
	}
	if id2, err := c.AddFunc("0 0 * * *", func() {
		if err := st.AppendStatsSnapshot(context.Background()); err != nil {
			log.Printf("cron: stats snapshot: %v", err)
		}
	}); err != nil {
		log.Printf("cron: failed to schedule stats snapshot: %v", err)
	} else {
		_ = id2
		log.Println("cron: stats snapshot scheduled daily 00:00")
	}
	c.Start()
	defer c.Stop()

	addr := os.Getenv("GSBS_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		log.Printf("listen: %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("shutting down server...")
	atomic.StoreInt32(&draining, 1)
	hub.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
	log.Println("server stopped")
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

package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
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
	"github.com/gsbs/gsbs/server/schedule"
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
		logx.Logger().Fatal().Err(err).Msg("store: open failed")
	}
	defer st.Close()
	logx.Logger().Info().Str("path", dbPath).Msg("database: opened")
	ctx := context.Background()
	if err := st.ReconcileStaleJobRuns(ctx); err != nil {
		logx.Logger().Error().Err(err).Msg("reconcile stale job_runs")
	}
	if err := st.ReconcileStalePCGWSyncRuns(ctx); err != nil {
		logx.Logger().Error().Err(err).Msg("reconcile stale pcgw_sync_runs")
	}
	if n, err := st.DeleteExpiredSessions(ctx, time.Now().Add(-store.WebSessionMaxAge)); err != nil {
		logx.Logger().Error().Err(err).Msg("session GC on startup")
	} else if n > 0 {
		logx.Logger().Info().Int64("count", n).Msg("session GC on startup: removed expired sessions")
	}
	if adminUser := os.Getenv("GSBS_ADMIN_USERNAME"); adminUser != "" {
		if err := st.EnsureAdminByUsername(context.Background(), adminUser); err != nil {
			logx.Logger().Error().Str("username", adminUser).Err(err).Msg("ensure admin username")
		}
	}

	authSvc := auth.NewService(st)

	allowRegister := true
	if v := os.Getenv("GSBS_ALLOW_REGISTER"); strings.EqualFold(v, "false") || v == "0" {
		allowRegister = false
		logx.Logger().Info().Msg("public registration is DISABLED (set GSBS_ALLOW_REGISTER=true to enable)")
	}

	sessionSecret := os.Getenv("GSBS_SESSION_SECRET")
	if sessionSecret == "" {
		logx.Logger().Fatal().Msg("GSBS_SESSION_SECRET must be set; generate with: openssl rand -base64 32")
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
				logx.Logger().Info().Int64("bytes", n).Msg("global storage limit set")
			}
		}
	}
	readOnly := strings.EqualFold(os.Getenv("GSBS_READ_ONLY"), "true") || os.Getenv("GSBS_READ_ONLY") == "1"
	if readOnly {
		logx.Logger().Warn().Msg("server is in READ-ONLY mode (push/delete disabled)")
	}
	apiHandler := api.NewHandler(st, authSvc, allowRegister, hub, authLimiter, pushLimiter, pullLimiter, generalLimiter, manifestLimiter, maxStorageBytes, readOnly, sessionSecret, Version)
	runner = job.NewRunner(st, hub, apiHandler)
	c := cron.New()
	pcgwCronSched := schedule.NewPCGWCron(c, st, runner)
	webHandler := webui.NewWebHandler(st, authSvc, sessionSecret, os.Getenv("GSBS_ADMIN_USERNAME"), allowRegister, hub, apiHandler, runner, pcgwCronSched, Version, maxStorageBytes, readOnly, authLimiter)
	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(webui.StaticFiles())))
	mux.Handle("/", webHandler)
	var metricsCollector *metrics.Collector
	if os.Getenv("GSBS_METRICS") == "1" {
		metricsToken := os.Getenv("GSBS_METRICS_TOKEN")
		if metricsToken == "" {
			// Auto-generate a token so /metrics is never exposed without auth.
			// Set GSBS_METRICS_TOKEN to make it permanent across restarts.
			b := make([]byte, 16)
			if _, randErr := rand.Read(b); randErr == nil {
				metricsToken = hex.EncodeToString(b)
			}
			logx.Logger().Warn().Str("token", metricsToken).Msg("metrics: GSBS_METRICS_TOKEN not set; using auto-generated token (set env var to persist)")
		}
		metricsCollector = metrics.NewCollector(st, hub)
		metricsHandler := metricsAuth(metricsToken, metricsCollector)
		mux.Handle("/metrics", metricsHandler)
		logx.Logger().Info().Msg("metrics: enabled at GET /metrics (Bearer token required)")
	}
	handler := logRequests(mux, metricsCollector)

	if err := pcgwCronSched.Start(ctx); err != nil {
		logx.Logger().Error().Err(err).Msg("cron: failed to schedule PCGW sync")
	}
	if shouldAutoRunPCGWOnFirstStart(ctx, st) {
		go func() {
			started, err := runner.TryRunPCGWSync(context.Background())
			if err != nil {
				logx.Logger().Error().Err(err).Msg("first-start pcgw sync")
				return
			}
			if started {
				if err := st.SetAdminSetting(context.Background(), store.AdminSettingPCGWFirstRunDone, "true"); err != nil {
					logx.Logger().Error().Err(err).Msg("first-start pcgw sync: set marker")
				}
				logx.Logger().Info().Msg("first-start pcgw sync started")
			}
		}()
	}
	if id2, err := c.AddFunc("0 0 * * *", func() {
		if err := st.AppendStatsSnapshot(context.Background()); err != nil {
			logx.Logger().Error().Err(err).Msg("cron: stats snapshot")
		}
	}); err != nil {
		logx.Logger().Error().Err(err).Msg("cron: failed to schedule stats snapshot")
	} else {
		_ = id2
		logx.Logger().Info().Msg("cron: stats snapshot scheduled daily 00:00")
	}
	if id3, err := c.AddFunc("0 4 * * *", func() {
		n, err := st.DeleteExpiredSessions(context.Background(), time.Now().Add(-store.WebSessionMaxAge))
		if err != nil {
			logx.Logger().Error().Err(err).Msg("cron: session GC")
		} else if n > 0 {
			logx.Logger().Info().Int64("count", n).Msg("cron: session GC removed expired sessions")
		}
	}); err != nil {
		logx.Logger().Error().Err(err).Msg("cron: failed to schedule session GC")
	} else {
		_ = id3
		logx.Logger().Info().Msg("cron: session GC scheduled daily 04:00")
	}
	c.Start()
	defer c.Stop()

	addr := os.Getenv("GSBS_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		logx.Logger().Info().Str("addr", addr).Msg("listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logx.Logger().Fatal().Err(err).Msg("listen failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	logx.Logger().Info().Msg("shutting down server...")
	atomic.StoreInt32(&draining, 1)
	hub.Shutdown()

	// Cancel in-flight jobs and wait up to 10s before closing the HTTP server and DB.
	if runner != nil {
		runner.CancelAll()
		jobDrainCtx, jobDrainCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer jobDrainCancel()
		if err := runner.WaitJobs(jobDrainCtx); err != nil {
			logx.Logger().Warn().Err(err).Msg("shutdown: job drain timed out; some writes may be incomplete")
		}
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		logx.Logger().Error().Err(err).Msg("server shutdown")
	}
	logx.Logger().Info().Msg("server stopped")
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

func shouldAutoRunPCGWOnFirstStart(ctx context.Context, st store.Store) bool {
	done, _ := st.GetAdminSetting(ctx, store.AdminSettingPCGWFirstRunDone)
	if done == "true" {
		return false
	}
	auto, _ := st.GetAdminSetting(ctx, store.AdminSettingPCGWAutoRunFirstStart)
	return auto == "true" || auto == "1"
}

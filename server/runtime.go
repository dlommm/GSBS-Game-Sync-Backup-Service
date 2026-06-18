package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gsbs/gsbs/server/api"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/metrics"
	"github.com/gsbs/gsbs/server/schedule"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
	"github.com/gsbs/gsbs/server/webui"
	"github.com/robfig/cron/v3"
)

type serverApp struct {
	st           store.Store
	hub          *sse.Hub
	runner       *job.Runner
	cron         *cron.Cron
	pcgwCron     *schedule.PCGWCron
	srv          *http.Server
	listenErrCh  chan error
	startOnce    sync.Once
	shutdownOnce sync.Once
	shutdownErr  error
}

func newServerApp() (*serverApp, error) {
	dbPath := os.Getenv("GSBS_DB")
	if dbPath == "" {
		dbPath = "gsbs.db"
	}
	st, err := store.NewSQLite(dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: open failed: %w", err)
	}
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
		_ = st.Close()
		return nil, fmt.Errorf("GSBS_SESSION_SECRET must be set; generate with: openssl rand -base64 32")
	}

	hub := sse.NewHub()
	authLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_AUTH", 20, time.Minute)
	pushLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_PUSH", 120, time.Minute)
	pullLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_PULL", 60, time.Minute)
	generalLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_GENERAL", 300, time.Minute)
	manifestLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_MANIFEST", 60, time.Minute)

	maxStorageBytes := int64(0)
	if v := os.Getenv("GSBS_MAX_STORAGE_BYTES"); v != "" {
		if n, parseErr := strconv.ParseInt(v, 10, 64); parseErr == nil && n >= 0 {
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
	runner := job.NewRunner(st, hub, apiHandler)
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
			b := make([]byte, 16)
			if _, randErr := rand.Read(b); randErr == nil {
				metricsToken = hex.EncodeToString(b)
			}
			logx.Logger().Warn().Str("token", metricsToken).Msg("metrics: GSBS_METRICS_TOKEN not set; using auto-generated token (set env var to persist)")
		}
		metricsCollector = metrics.NewCollector(st, hub)
		mux.Handle("/metrics", metricsAuth(metricsToken, metricsCollector))
		logx.Logger().Info().Msg("metrics: enabled at GET /metrics (Bearer token required)")
	}

	handler := logRequests(securityHeaders(recoverMiddleware(mux)), metricsCollector)

	if id2, scheduleErr := c.AddFunc("0 0 * * *", func() {
		if err := st.AppendStatsSnapshot(context.Background()); err != nil {
			logx.Logger().Error().Err(err).Msg("cron: stats snapshot")
		}
	}); scheduleErr != nil {
		logx.Logger().Error().Err(scheduleErr).Msg("cron: failed to schedule stats snapshot")
	} else {
		_ = id2
		logx.Logger().Info().Msg("cron: stats snapshot scheduled daily 00:00")
	}

	if id3, scheduleErr := c.AddFunc("0 4 * * *", func() {
		n, err := st.DeleteExpiredSessions(context.Background(), time.Now().Add(-store.WebSessionMaxAge))
		if err != nil {
			logx.Logger().Error().Err(err).Msg("cron: session GC")
		} else if n > 0 {
			logx.Logger().Info().Int64("count", n).Msg("cron: session GC removed expired sessions")
		}
	}); scheduleErr != nil {
		logx.Logger().Error().Err(scheduleErr).Msg("cron: failed to schedule session GC")
	} else {
		_ = id3
		logx.Logger().Info().Msg("cron: session GC scheduled daily 04:00")
	}

	addr := os.Getenv("GSBS_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	return &serverApp{
		st:          st,
		hub:         hub,
		runner:      runner,
		cron:        c,
		pcgwCron:    pcgwCronSched,
		srv:         &http.Server{Addr: addr, Handler: handler},
		listenErrCh: make(chan error, 1),
	}, nil
}

func (a *serverApp) Start(ctx context.Context) {
	a.startOnce.Do(func() {
		if err := a.pcgwCron.Start(ctx); err != nil {
			logx.Logger().Error().Err(err).Msg("cron: failed to schedule PCGW sync")
		}
		if shouldAutoRunPCGWOnFirstStart(context.Background(), a.st) {
			go func() {
				settings, _ := a.st.ListAdminSettings(context.Background())
				source := store.PCGWSyncSourceFromSettings(settings)
				var started bool
				var err error
				if source == store.PCGWSyncSourceS3 {
					started, err = a.runner.TryRunPCGWBundleFetch(context.Background(), true)
				} else {
					started, err = a.runner.TryRunPCGWSync(context.Background())
				}
				if err != nil {
					logx.Logger().Error().Err(err).Str("source", source).Msg("first-start pcgw sync")
					return
				}
				if started {
					if err := a.st.SetAdminSetting(context.Background(), store.AdminSettingPCGWFirstRunDone, "true"); err != nil {
						logx.Logger().Error().Err(err).Msg("first-start pcgw sync: set marker")
					}
					logx.Logger().Info().Str("source", source).Msg("first-start pcgw sync started")
				}
			}()
		}

		a.cron.Start()
		logx.Logger().Info().Str("addr", a.srv.Addr).Msg("listening")
		go func() {
			err := a.srv.ListenAndServe()
			if err != nil && err != http.ErrServerClosed {
				a.listenErrCh <- err
				return
			}
			a.listenErrCh <- nil
		}()
	})
}

func (a *serverApp) Errors() <-chan error {
	return a.listenErrCh
}

func (a *serverApp) Shutdown(ctx context.Context) error {
	a.shutdownOnce.Do(func() {
		atomic.StoreInt32(&draining, 1)
		a.hub.Shutdown()
		a.cron.Stop()

		if a.runner != nil {
			a.runner.CancelAll()
			jobDrainCtx, jobDrainCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer jobDrainCancel()
			if err := a.runner.WaitJobs(jobDrainCtx); err != nil {
				logx.Logger().Warn().Err(err).Msg("shutdown: job drain timed out; some writes may be incomplete")
			}
		}

		shutCtx := ctx
		if shutCtx == nil {
			shutCtx = context.Background()
		}
		shutCtx, shutCancel := context.WithTimeout(shutCtx, 15*time.Second)
		defer shutCancel()
		if err := a.srv.Shutdown(shutCtx); err != nil && err != context.Canceled {
			logx.Logger().Error().Err(err).Msg("server shutdown")
			a.shutdownErr = err
		}
		if err := a.st.Close(); err != nil {
			logx.Logger().Error().Err(err).Msg("database close failed")
			if a.shutdownErr == nil {
				a.shutdownErr = err
			}
		}
	})
	return a.shutdownErr
}

func runConsoleMode() error {
	initConsoleLogging()
	defer func() {
		if err := logx.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close logger: %v\n", err)
		}
	}()

	ctx, stop := withShutdownSignals(context.Background())
	defer stop()

	app, err := newServerApp()
	if err != nil {
		return err
	}
	app.Start(context.Background())

	select {
	case err := <-app.Errors():
		if err != nil {
			_ = app.Shutdown(context.Background())
			return fmt.Errorf("listen failed: %w", err)
		}
		logx.Logger().Info().Msg("server stopped")
		return nil
	case <-ctx.Done():
		logx.Logger().Info().Msg("shutting down server...")
		if err := app.Shutdown(context.Background()); err != nil {
			return err
		}
		logx.Logger().Info().Msg("server stopped")
		return nil
	}
}

func initConsoleLogging() {
	if path, sourceEnv := logx.ConfiguredLogFilePath(); path != "" {
		if err := logx.InitFile(path); err != nil {
			logx.Init()
			logx.Logger().Warn().
				Str("env", sourceEnv).
				Str("path", path).
				Err(err).
				Msg("file logging init failed; using stdout")
			return
		}
		logx.Logger().Info().
			Str("env", sourceEnv).
			Str("path", path).
			Msg("file logging enabled")
		return
	}
	logx.Init()
}

func runServerWithOptions(opts cliOptions) error {
	if envPath := strings.TrimSpace(opts.envFile); envPath != "" {
		if err := loadEnvFile(envPath); err != nil {
			return fmt.Errorf("load env file %s: %w", envPath, err)
		}
	} else if defaultPath := strings.TrimSpace(defaultEnvFilePath()); defaultPath != "" {
		if _, err := os.Stat(defaultPath); err == nil {
			if err := loadEnvFile(defaultPath); err != nil {
				return fmt.Errorf("load env file %s: %w", defaultPath, err)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check env file %s: %w", defaultPath, err)
		}
	}

	if opts.installService || opts.uninstallService || opts.startService || opts.stopService {
		return manageWindowsService(opts)
	}
	if opts.serviceMode {
		return runWindowsServiceHost()
	}
	return runConsoleMode()
}

func shouldAutoRunPCGWOnFirstStart(ctx context.Context, st store.Store) bool {
	done, _ := st.GetAdminSetting(ctx, store.AdminSettingPCGWFirstRunDone)
	if done == "true" {
		return false
	}
	auto, _ := st.GetAdminSetting(ctx, store.AdminSettingPCGWAutoRunFirstStart)
	return auto == "true" || auto == "1"
}

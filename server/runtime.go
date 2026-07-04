package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/gsbs/gsbs/server/notify"
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

// checkSessionSecretStrength rejects secrets that are too short to resist
// brute force or that match known placeholder values from example configs.
// GSBS_INSECURE_DEV_SECRET=1 bypasses the check for local development only.
func checkSessionSecretStrength(secret string) error {
	if os.Getenv("GSBS_INSECURE_DEV_SECRET") == "1" {
		logx.Logger().Warn().Msg("GSBS_INSECURE_DEV_SECRET=1: skipping session secret strength check — never use this in production")
		return nil
	}
	if len(secret) < 32 || secret == "dev-change-me-in-production" || strings.Contains(secret, "change-me") { //nolint:gosec // G101: this is the REJECTED placeholder value, not a credential
		return fmt.Errorf("GSBS_SESSION_SECRET is too weak (need >= 32 characters, not a placeholder); generate with: openssl rand -base64 32 (or set GSBS_INSECURE_DEV_SECRET=1 for local development)")
	}
	return nil
}

// envRetentionDays reads a non-negative day count from an env var (0 = keep
// forever). Invalid or negative values fall back to the default.
func envRetentionDays(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		logx.Logger().Warn().Str("var", name).Str("value", v).Int("default", def).
			Msg("invalid retention-days value; using default")
		return def
	}
	return n
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
	// Registration policy precedence: env GSBS_ALLOW_REGISTER wins; otherwise
	// the admin_settings value (set by the setup wizard or Server Settings);
	// otherwise the default (open). This lets a zero-env deployment be
	// configured entirely through the web UI.
	allowRegister := true
	if v, ok := os.LookupEnv("GSBS_ALLOW_REGISTER"); ok {
		allowRegister = !(strings.EqualFold(v, "false") || v == "0")
	} else if s, err := st.GetAdminSetting(ctx, store.AdminSettingAllowRegister); err == nil && s != "" {
		allowRegister = !(s == "false" || s == "0")
	}
	if !allowRegister {
		logx.Logger().Info().Msg("public registration is DISABLED")
	}

	sessionSecret := os.Getenv("GSBS_SESSION_SECRET")
	if sessionSecret == "" {
		// Zero-config path: persist a strong random secret in gsbs-keys/ (the
		// same protected directory as the TOTP key) and reuse it thereafter.
		// An explicitly-set env value always wins and is strength-checked.
		s, secErr := loadOrCreateSessionSecret(dbPath)
		if secErr != nil {
			_ = st.Close()
			return nil, fmt.Errorf("session secret: %w", secErr)
		}
		sessionSecret = s
		logx.Logger().Info().Msg("session secret: using auto-generated key (gsbs-keys/session.secret); set GSBS_SESSION_SECRET to override")
	} else {
		// This one secret signs sessions, CSRF tokens, and both TOTP step
		// tokens; a guessable value makes all of them forgeable.
		if err := checkSessionSecretStrength(sessionSecret); err != nil {
			_ = st.Close()
			return nil, err
		}
	}

	hub := sse.NewHub()
	authLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_AUTH", 20, time.Minute)
	pushLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_PUSH", 120, time.Minute)
	pullLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_PULL", 60, time.Minute)
	generalLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_GENERAL", 300, time.Minute)
	manifestLimiter := rateLimiterFromEnv("GSBS_RATE_LIMIT_MANIFEST", 60, time.Minute)

	maxStorageBytes := int64(0)
	if v := os.Getenv("GSBS_MAX_STORAGE_BYTES"); v == "" {
		// DB-backed default when the env var is unset (setup wizard / Server Settings).
		if s, err := st.GetAdminSetting(ctx, store.AdminSettingMaxStorageBytes); err == nil && s != "" {
			if n, parseErr := strconv.ParseInt(s, 10, 64); parseErr == nil && n >= 0 {
				maxStorageBytes = n
			}
		}
	} else {
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

	// Notifications: events flow to admin sinks (admin_settings) and, for
	// user-scoped events, additionally to that user's own sinks. Sinks are
	// resolved per event so settings changes apply without a restart.
	notifier := notify.New(context.Background(),
		func(nctx context.Context) notify.Sinks {
			settings, err := st.ListAdminSettings(nctx)
			if err != nil {
				return notify.Sinks{}
			}
			return notify.Sinks{
				WebhookURL: strings.TrimSpace(settings[store.AdminSettingNotifyWebhookURL]),
				DiscordURL: strings.TrimSpace(settings[store.AdminSettingNotifyDiscordURL]),
				NtfyURL:    strings.TrimSpace(settings[store.AdminSettingNotifyNtfyURL]),
				Events:     notify.ParseEventFilter(settings[store.AdminSettingNotifyEvents]),
			}
		},
		func(nctx context.Context, userID string) notify.Sinks {
			ns, err := st.GetUserNotifySettings(nctx, userID)
			if err != nil {
				return notify.Sinks{}
			}
			return notify.Sinks{
				WebhookURL: strings.TrimSpace(ns.WebhookURL),
				DiscordURL: strings.TrimSpace(ns.DiscordURL),
				NtfyURL:    strings.TrimSpace(ns.NtfyURL),
				Events:     notify.ParseEventFilter(ns.EventsJSON),
			}
		},
	)
	apiHandler.SetNotifier(notifier.Notify)
	webHandler.SetNotifier(notifier.Notify)
	job.OnBackupFinished = func(success bool, detail string) {
		title := "GSBS backup completed"
		if !success {
			title = "GSBS backup FAILED"
		}
		notifier.Notify(notify.Event{Type: notify.EventBackup, Title: title, Body: detail})
	}

	// Daily stale-device check (07:15): alerts once per stale period.
	if _, scheduleErr := c.AddFunc("15 7 * * *", func() {
		nctx := context.Background()
		settings, err := st.ListAdminSettings(nctx)
		if err != nil {
			return
		}
		staleDays := 14
		if v := strings.TrimSpace(settings[store.AdminSettingNotifyStaleDays]); v != "" {
			if n, convErr := strconv.Atoi(v); convErr == nil && n >= 0 {
				staleDays = n
			}
		}
		if staleDays == 0 {
			return
		}
		stale, err := st.ListStaleClientsNeedingAlert(nctx, staleDays)
		if err != nil {
			logx.Logger().Error().Err(err).Msg("cron: stale-device check")
			return
		}
		for _, sc := range stale {
			notifier.Notify(notify.Event{
				Type:   notify.EventStaleDevice,
				Title:  "Device has stopped syncing",
				Body:   fmt.Sprintf("%s (%s) last synced %s — its saves are no longer being backed up.", sc.Name, sc.Username, sc.LastSeen),
				UserID: sc.UserID,
			})
			_ = st.MarkClientStaleNotified(nctx, sc.ClientID)
		}
	}); scheduleErr != nil {
		logx.Logger().Error().Err(scheduleErr).Msg("cron: failed to schedule stale-device check")
	}

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
			// Log only a fingerprint: /metrics is often scraped over plain HTTP
			// on a LAN, but the token itself must never land in log files.
			sum := sha256.Sum256([]byte(metricsToken))
			logx.Logger().Warn().Str("token_sha256_prefix", hex.EncodeToString(sum[:4])).Msg("metrics: GSBS_METRICS_TOKEN not set; using an auto-generated token this run — set GSBS_METRICS_TOKEN explicitly to scrape /metrics")
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

	// History pruning keeps the append-only tables (audit log, manifest-fetch
	// log, stats snapshots) and optionally old save versions from growing
	// forever. Each window is configurable in days; 0 disables that table.
	auditDays := envRetentionDays("GSBS_AUDIT_RETENTION_DAYS", 180)
	manifestDays := envRetentionDays("GSBS_MANIFEST_FETCH_RETENTION_DAYS", 30)
	statsDays := envRetentionDays("GSBS_STATS_RETENTION_DAYS", 730)
	versionMaxAgeDays := envRetentionDays("GSBS_SAVE_VERSION_MAX_AGE_DAYS", 0)
	if auditDays > 0 || manifestDays > 0 || statsDays > 0 || versionMaxAgeDays > 0 {
		if _, scheduleErr := c.AddFunc("30 3 * * *", func() {
			pc, err := st.PruneHistory(context.Background(), auditDays, manifestDays, statsDays, versionMaxAgeDays)
			if err != nil {
				logx.Logger().Error().Err(err).Msg("cron: history pruning")
			} else if pc.Total() > 0 {
				logx.Logger().Info().
					Int64("audit", pc.Audit).Int64("manifest_fetches", pc.ManifestFetches).
					Int64("stats", pc.Stats).Int64("save_versions", pc.SaveVersions).
					Msg("cron: history pruning removed old rows")
			}
		}); scheduleErr != nil {
			logx.Logger().Error().Err(scheduleErr).Msg("cron: failed to schedule history pruning")
		} else {
			logx.Logger().Info().
				Int("audit_days", auditDays).Int("manifest_days", manifestDays).
				Int("stats_days", statsDays).Int("version_max_age_days", versionMaxAgeDays).
				Msg("cron: history pruning scheduled daily 03:30")
		}
	}

	// Weekly blob-integrity verification (Monday 06:00): re-hashes stored
	// unencrypted saves against their recorded hashes; findings surface on
	// the admin overview. Admins can also trigger it manually there.
	if _, scheduleErr := c.AddFunc("0 6 * * 1", func() {
		if _, err := runner.TryRunIntegrityCheck(context.Background()); err != nil && !errors.Is(err, job.ErrJobAlreadyRunning) {
			logx.Logger().Error().Err(err).Msg("cron: integrity check failed to start")
		}
	}); scheduleErr != nil {
		logx.Logger().Error().Err(scheduleErr).Msg("cron: failed to schedule integrity check")
	} else {
		logx.Logger().Info().Msg("cron: integrity check scheduled weekly Monday 06:00")
	}

	// Scheduled offsite backups: opt-in via admin settings or GSBS_BACKUP_DIR.
	// The schedule is read at startup (changing it in Settings needs a
	// restart, noted in the UI); "Backup now" works regardless.
	if backupSettings, sErr := st.ListAdminSettings(ctx); sErr == nil && job.BackupEnabled(backupSettings) {
		expr := job.BackupCronExpr(backupSettings)
		if _, scheduleErr := c.AddFunc(expr, func() {
			if _, err := runner.TryRunBackup(context.Background()); err != nil && !errors.Is(err, job.ErrJobAlreadyRunning) {
				logx.Logger().Error().Err(err).Msg("cron: backup failed to start")
			}
		}); scheduleErr != nil {
			logx.Logger().Error().Err(scheduleErr).Str("expr", expr).Msg("cron: failed to schedule backup")
		} else {
			logx.Logger().Info().Str("expr", expr).Msg("cron: backup scheduled")
		}
	}

	addr := os.Getenv("GSBS_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	return &serverApp{
		st:       st,
		hub:      hub,
		runner:   runner,
		cron:     c,
		pcgwCron: pcgwCronSched,
		srv: &http.Server{
			Addr:    addr,
			Handler: handler,
			// Slowloris/slow-body protection. ReadTimeout is generous so a
			// 50 MiB push still fits on a slow uplink. WriteTimeout stays 0
			// because the SSE streams are long-lived — per-request write
			// deadlines are applied in logRequests and the SSE handlers.
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       15 * time.Minute,
			IdleTimeout:       2 * time.Minute,
			MaxHeaderBytes:    64 << 10,
		},
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

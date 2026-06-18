package schedule

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
	"github.com/robfig/cron/v3"
)

// PCGWCron manages scheduled PCGW sync and manifest bundle fetch cron entries.
type PCGWCron struct {
	cron   *cron.Cron
	store  store.Store
	runner *job.Runner

	mu            sync.Mutex
	entryID       cron.EntryID // API incremental sync
	bundleEntryID cron.EntryID // GitHub bundle fetch
	fullEntryID   cron.EntryID // full sync (GSBS_PCGW_FULL_CRON, env-only)
}

// CronView describes effective cron scheduling for the admin UI.
type CronView struct {
	Expr        string
	Source      string // env, db, default, disabled
	Disabled    bool
	EnvOverride bool
	NextRun     time.Time
	SyncSource  string
	BundleExpr  string
	BundleNext  time.Time
}

func NewPCGWCron(c *cron.Cron, st store.Store, runner *job.Runner) *PCGWCron {
	return &PCGWCron{cron: c, store: st, runner: runner}
}

func (p *PCGWCron) Start(ctx context.Context) error {
	return p.Reschedule(ctx)
}

func (p *PCGWCron) View(ctx context.Context) CronView {
	settings, _ := p.store.ListAdminSettings(ctx)
	syncSource := store.PCGWSyncSourceFromSettings(settings)
	expr, disabled, source, envOverride := p.resolveAPISync(ctx, settings)
	view := CronView{
		Expr: expr, Source: source, Disabled: disabled, EnvOverride: envOverride, SyncSource: syncSource,
	}
	if !disabled && expr != "" && syncSource == store.PCGWSyncSourceAPI {
		if sched, err := cron.ParseStandard(expr); err == nil {
			view.NextRun = sched.Next(time.Now())
		}
	}
	bundleExpr, bundleDisabled := p.resolveBundleCron(settings)
	view.BundleExpr = bundleExpr
	if !bundleDisabled && bundleExpr != "" && syncSource == store.PCGWSyncSourceS3 {
		if sched, err := cron.ParseStandard(bundleExpr); err == nil {
			view.BundleNext = sched.Next(time.Now())
		}
	}
	return view
}

func (p *PCGWCron) Reschedule(ctx context.Context) error {
	settings, err := p.store.ListAdminSettings(ctx)
	if err != nil {
		settings = map[string]string{}
	}
	syncSource := store.PCGWSyncSourceFromSettings(settings)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.entryID != 0 {
		p.cron.Remove(p.entryID)
		p.entryID = 0
	}
	if p.bundleEntryID != 0 {
		p.cron.Remove(p.bundleEntryID)
		p.bundleEntryID = 0
	}
	if p.fullEntryID != 0 {
		p.cron.Remove(p.fullEntryID)
		p.fullEntryID = 0
	}

	if syncSource == store.PCGWSyncSourceS3 {
		bundleExpr, bundleDisabled := p.resolveBundleCron(settings)
		if bundleDisabled {
			logx.Logger().Info().Str("component", "cron").Msg("cron: PCGW bundle fetch disabled")
		} else {
			id, err := p.cron.AddFunc(bundleExpr, func() {
				if _, err := p.runner.TryRunPCGWBundleFetch(context.Background(), false); err != nil {
					logx.Logger().Error().Str("component", "cron").Err(err).Msg("cron: pcgw bundle fetch")
				}
			})
			if err != nil {
				return err
			}
			p.bundleEntryID = id
			logx.Logger().Info().Str("component", "cron").Str("expr", bundleExpr).
				Msg("cron: PCGW bundle fetch scheduled")
		}
	} else {
		expr, disabled, source, _ := p.resolveAPISync(ctx, settings)
		if disabled {
			logx.Logger().Info().Str("component", "cron").Msg("cron: PCGW sync disabled (manual mode)")
		} else {
			id, err := p.cron.AddFunc(expr, func() {
				if _, err := p.runner.RunPCGWSync(context.Background()); err != nil {
					logx.Logger().Error().Str("component", "cron").Err(err).Msg("cron: pcgw sync")
				}
			})
			if err != nil {
				return err
			}
			p.entryID = id
			logx.Logger().Info().Str("component", "cron").Str("expr", expr).Str("source", source).
				Msg("cron: PCGW sync scheduled")
		}
	}

	if fullExpr := strings.TrimSpace(os.Getenv("GSBS_PCGW_FULL_CRON")); fullExpr != "" {
		fid, err := p.cron.AddFunc(fullExpr, func() {
			if _, err := p.runner.RunPCGWSyncFull(context.Background()); err != nil {
				logx.Logger().Error().Str("component", "cron").Err(err).Msg("cron: pcgw full sync")
			}
		})
		if err != nil {
			logx.Logger().Warn().Str("component", "cron").Str("expr", fullExpr).Err(err).
				Msg("cron: GSBS_PCGW_FULL_CRON invalid — full sync not scheduled")
		} else {
			p.fullEntryID = fid
			logx.Logger().Info().Str("component", "cron").Str("expr", fullExpr).
				Msg("cron: PCGW full sync scheduled")
		}
	}

	return nil
}

func (p *PCGWCron) resolveAPISync(ctx context.Context, settings map[string]string) (expr string, disabled bool, source string, envOverride bool) {
	if settings == nil {
		var err error
		settings, err = p.store.ListAdminSettings(ctx)
		if err != nil {
			return store.DefaultPCGWCron, false, "default", false
		}
	}
	if v, ok := os.LookupEnv("GSBS_PCGW_CRON"); ok {
		if v == "" {
			return "", true, "env", true
		}
		return v, false, "env", true
	}
	if v, ok := settings[store.AdminSettingPCGWCron]; ok {
		if v == "" {
			return "", true, "db", false
		}
		return v, false, "db", false
	}
	return store.DefaultPCGWCron, false, "default", false
}

func (p *PCGWCron) resolveBundleCron(settings map[string]string) (expr string, disabled bool) {
	expr = store.PCGWBundleCronFromSettings(settings)
	if expr == "" {
		return "", true
	}
	return expr, false
}

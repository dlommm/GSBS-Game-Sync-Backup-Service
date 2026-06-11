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

// PCGWCron manages the scheduled PCGW sync cron entries (incremental and full).
type PCGWCron struct {
	cron   *cron.Cron
	store  store.Store
	runner *job.Runner

	mu          sync.Mutex
	entryID     cron.EntryID // incremental sync
	fullEntryID cron.EntryID // full sync (GSBS_PCGW_FULL_CRON, env-only)
}

// CronView describes effective cron scheduling for the admin UI.
type CronView struct {
	Expr        string
	Source      string // env, db, default, disabled
	Disabled    bool
	EnvOverride bool
	NextRun     time.Time
}

func NewPCGWCron(c *cron.Cron, st store.Store, runner *job.Runner) *PCGWCron {
	return &PCGWCron{cron: c, store: st, runner: runner}
}

func (p *PCGWCron) Start(ctx context.Context) error {
	return p.Reschedule(ctx)
}

func (p *PCGWCron) View(ctx context.Context) CronView {
	expr, disabled, source, envOverride := p.resolve(ctx)
	view := CronView{
		Expr: expr, Source: source, Disabled: disabled, EnvOverride: envOverride,
	}
	if !disabled && expr != "" {
		if sched, err := cron.ParseStandard(expr); err == nil {
			view.NextRun = sched.Next(time.Now())
		}
	}
	return view
}

func (p *PCGWCron) Reschedule(ctx context.Context) error {
	expr, disabled, source, _ := p.resolve(ctx)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.entryID != 0 {
		p.cron.Remove(p.entryID)
		p.entryID = 0
	}
	if p.fullEntryID != 0 {
		p.cron.Remove(p.fullEntryID)
		p.fullEntryID = 0
	}

	if disabled {
		logx.Logger().Info().Str("component", "cron").Msg("cron: PCGW sync disabled")
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

	// GSBS_PCGW_FULL_CRON: optional env-only cron for periodic full resync.
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

func (p *PCGWCron) resolve(ctx context.Context) (expr string, disabled bool, source string, envOverride bool) {
	if v, ok := os.LookupEnv("GSBS_PCGW_CRON"); ok {
		if v == "" {
			return "", true, "env", true
		}
		return v, false, "env", true
	}

	settings, err := p.store.ListAdminSettings(ctx)
	if err != nil {
		return store.DefaultPCGWCron, false, "default", false
	}
	if v, ok := settings[store.AdminSettingPCGWCron]; ok {
		if v == "" {
			return "", true, "db", false
		}
		return v, false, "db", false
	}
	return store.DefaultPCGWCron, false, "default", false
}

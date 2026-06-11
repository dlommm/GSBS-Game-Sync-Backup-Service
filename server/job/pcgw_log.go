package job

import (
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/rs/zerolog"
)

const (
	phase1ReasonFullSync          = "full_sync_requested"
	phase1ReasonForceFull         = "force_full"
	phase1ReasonCatalogStatsError = "catalog_stats_error"
	phase1ReasonEmptyCatalog      = "empty_local_catalog"
	phase1ReasonNoPriorPhase1     = "no_prior_phase1_stats"
	phase1ReasonProbeFailed       = "probe_failed"
	phase1ReasonCatalogIncomplete = "catalog_incomplete_rescan"
)

const catalogProgressInterval = 5000

func logPCGWSyncStart(runID, mode string, opts PCGWSyncOptions, budget int) {
	logx.Logger().Info().
		Str("component", "pcgw").
		Str("event", "pcgw.sync.start").
		Str("run_id", runID).
		Str("mode", mode).
		Bool("full", opts.Full).
		Bool("force_full", opts.ForceFull).
		Bool("skip_catalog", opts.SkipCatalogPhase).
		Bool("skip_ingest", opts.SkipIngestPhase).
		Bool("resume_catalog", opts.ResumeCatalogScan).
		Int("resume_cursor", opts.ResumeQueueCursor).
		Bool("missing_only", opts.MissingOnly).
		Bool("retry_failed_only", opts.RetryFailedOnly).
		Bool("auto_catch_up", opts.AutoCatchUp).
		Int("phase2_budget", budget).
		Msg("pcgw sync: starting")
}

func logPhase1Decision(reason string, extra ...func(*zerolog.Event)) {
	ev := logx.Logger().Info().
		Str("component", "pcgw").
		Str("event", "pcgw.phase1.decision").
		Str("phase1_reason", reason)
	for _, fn := range extra {
		fn(ev)
	}
	ev.Msg("pcgw sync phase1: " + phase1ReasonMessage(reason))
}

func phase1ReasonMessage(reason string) string {
	switch reason {
	case phase1ReasonFullSync:
		return "full catalog scan — incremental full mode"
	case phase1ReasonForceFull:
		return "full catalog scan — force full / bypass resume"
	case phase1ReasonCatalogStatsError:
		return "full catalog scan — could not read local catalog stats"
	case phase1ReasonEmptyCatalog:
		return "full catalog scan — local catalog empty"
	case phase1ReasonNoPriorPhase1:
		return "full catalog scan — no prior successful Phase 1 run"
	case phase1ReasonProbeFailed:
		return "full catalog scan — growth probe failed"
	case phase1ReasonCatalogIncomplete:
		return "full catalog scan — pcgw_catalog row count below last remote total"
	case "fast_probe":
		return "fast probe — no new remote pages, reusing cached Phase 1 stats"
	case "tail":
		return "tail scan — new remote pages detected beyond local count"
	case "skipped":
		return "Phase 1 skipped — targeted mode or admin action"
	case "resumed":
		return "Phase 1 skipped — resuming Phase 2 from checkpoint"
	default:
		return reason
	}
}

func logPhase1Complete(catalogScanMode string, phase1 types.Phase1Stats, tailGrew bool) {
	logx.Logger().Info().
		Str("component", "pcgw").
		Str("event", "pcgw.phase1.complete").
		Str("catalog_scan_mode", catalogScanMode).
		Bool("tail_grew", tailGrew).
		Int("remote_total", phase1.RemoteTotalIDs).
		Int("missing_local", phase1.MissingLocalIDs).
		Int("extra_local", phase1.ExtraLocalIDs).
		Str("catalog_hash", truncateHash(phase1.CatalogHash)).
		Msg("pcgw sync phase1: complete")
}

func logCatalogScanStart(scanKind, reason string, startOffset, estimatedTotal int) {
	logx.Logger().Info().
		Str("component", "pcgw").
		Str("event", "pcgw.catalog.scan_start").
		Str("scan_kind", scanKind).
		Str("phase1_reason", reason).
		Int("start_offset", startOffset).
		Int("estimated_total", estimatedTotal).
		Msg("pcgw catalog scan: started")
}

func logCatalogScanProgress(scanKind string, remoteTotal, offset int) {
	if remoteTotal > 0 && remoteTotal%catalogProgressInterval != 0 {
		return
	}
	logx.Logger().Info().
		Str("component", "pcgw").
		Str("event", "pcgw.catalog.scan_progress").
		Str("scan_kind", scanKind).
		Int("remote_total", remoteTotal).
		Int("offset", offset).
		Msg("pcgw catalog scan: progress")
}

func logRevCheckDecision(run bool, reason, lastRevCheckAt string, age time.Duration) {
	ev := logx.Logger().Info().
		Str("component", "pcgw").
		Str("event", "pcgw.rev_check.decision").
		Bool("run_rev_check", run).
		Str("rev_check_reason", reason)
	if lastRevCheckAt != "" {
		ev = ev.Str("last_rev_check_at", lastRevCheckAt)
	}
	if age > 0 {
		ev = ev.Float64("days_since_rev_check", age.Hours()/24)
	}
	if run {
		ev.Msg("pcgw sync: running wiki revision check across stored OK games (can take a long time — one API call per game)")
	} else {
		ev.Msg("pcgw sync: skipping revision check — unchanged catalog and rev-check interval not elapsed")
	}
}

func logRevCheckProgress(checked, changed int) {
	if checked == 0 || checked%1000 != 0 {
		return
	}
	logx.Logger().Info().
		Str("component", "pcgw").
		Str("event", "pcgw.rev_check.progress").
		Int("games_checked", checked).
		Int("changed_found", changed).
		Msg("pcgw sync: revision check progress")
}

func logPhase2Skip(reason string, extra ...func(*zerolog.Event)) {
	ev := logx.Logger().Info().
		Str("component", "pcgw").
		Str("event", "pcgw.phase2.skip").
		Str("phase2_skip_reason", reason)
	for _, fn := range extra {
		fn(ev)
	}
	ev.Msg("pcgw sync phase2: skipped — " + reason)
}

func logPhase2IngestProgress(processed, queueSize, cursor int) {
	if processed == 0 || processed%250 != 0 {
		return
	}
	logx.Logger().Info().
		Str("component", "pcgw").
		Str("event", "pcgw.phase2.progress").
		Int("processed", processed).
		Int("queue_size", queueSize).
		Int("cursor", cursor).
		Msg("pcgw sync phase2: ingest progress")
}

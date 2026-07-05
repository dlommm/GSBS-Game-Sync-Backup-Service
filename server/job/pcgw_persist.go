package job

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
	"github.com/rs/zerolog"
)

// PCGWSyncOptions configures a sync run.
type PCGWSyncOptions struct {
	Full             bool
	ForceFull        bool  // bypass resume checkpoint
	SinglePage       int64 // if >0, sync only this page
	Offset           int   // resume offset for full/incremental list
	SyncRunID        string
	ResumedFromRunID string
	Notes            string
	SkipStartRun     bool // use SyncRunID instead of StartPCGWSyncRun
	// MaxPagesPerRun caps Phase 2 ingest budget per run (0 = use env/default).
	MaxPagesPerRun int
	// ResumeCatalogScan skips Phase 1 and goes straight to Phase 2 (resume from checkpoint).
	ResumeCatalogScan bool
	// ResumeQueueCursor is the Phase 2 queue position to resume from.
	ResumeQueueCursor int
	// SkipCatalogScan is true when running catalog-only (no Phase 2).
	SkipIngestPhase bool
	// SkipCatalogPhase skips Phase 1 and reuses the last successful Phase 1 stats.
	SkipCatalogPhase bool
	// MissingOnly limits Phase 2 queue to catalog IDs not yet stored in pcgw_games.
	MissingOnly bool
	// RetryFailedOnly limits Phase 2 queue to failed/partial IDs only.
	RetryFailedOnly bool
	// RebuildManifestOnly skips all ingest and just bumps the manifest.
	RebuildManifestOnly bool
	// AutoCatchUp repeatedly runs budgeted sync cycles until backlog is empty.
	AutoCatchUp bool
}

// PCGWSyncProgress reports sync progress for SSE.
type PCGWSyncProgress struct {
	PagesProcessed int
	TotalEstimate  int
	Phase          string
	GamesSkipped   int
	QueueSize      int
	QueueCursor    int
	ETASeconds     int // set by runner when broadcasting
}

// ReportProgress is an optional callback (pages processed).
type ReportProgress func(pagesProcessed int)

// ReportProgressEx reports richer progress when non-nil.
type ReportProgressEx func(PCGWSyncProgress)

// PCGWSync runs PCGW ingest: list pages, incremental skip, persist full mirror, project manifest v1.
// ErrPCGWMirrorNotSeeded is returned when an API sync is attempted against an
// empty PCGW mirror. Seed from the S3 manifest bundle first (Admin -> PCGW ->
// Fetch bundle now, or import a bundle file on air-gapped hosts).
var ErrPCGWMirrorNotSeeded = errors.New("pcgw mirror is empty: fetch or import the manifest bundle before running an API sync (direct full crawls of PCGamingWiki are disabled)")

func PCGWSync(ctx context.Context, st store.Store, client *pcgw.Client, reportProgress ReportProgress, opts PCGWSyncOptions) (int, error) {
	return PCGWSyncEx(ctx, st, client, reportProgress, nil, opts)
}

// PCGWSyncEx is PCGWSync with optional extended progress reporting.
// It implements the two-phase pipeline: Phase 1 = catalog scan, Phase 2 = targeted ingest.
func PCGWSyncEx(ctx context.Context, st store.Store, client *pcgw.Client, reportProgress ReportProgress, reportEx ReportProgressEx, opts PCGWSyncOptions) (int, error) {
	// Absolute seeded gate: never crawl the PCGW API against an empty mirror.
	// Fresh installs must seed from the prebuilt S3 bundle (or import a bundle
	// file manually on air-gapped hosts). There is deliberately no override —
	// a fleet of empty servers falling back to API crawls would flood
	// PCGamingWiki with hundreds of thousands of requests.
	if !opts.RebuildManifestOnly {
		if seeded, err := st.IsPCGWBundleSeeded(ctx); err == nil && !seeded {
			return 0, ErrPCGWMirrorNotSeeded
		}
	}

	mode := "incremental"
	if opts.Full {
		mode = "full"
	}
	if opts.SinglePage > 0 {
		mode = "single"
	}

	runID := opts.SyncRunID
	if runID == "" && !opts.SkipStartRun {
		var err error
		if opts.ResumedFromRunID != "" || opts.Notes != "" {
			runID, err = st.StartPCGWSyncRunWithResume(ctx, mode, opts.ResumedFromRunID, opts.Notes)
		} else {
			runID, err = st.StartPCGWSyncRun(ctx, mode)
		}
		if err != nil {
			return 0, err
		}
	}

	stats := store.PCGWSyncRunStats{}
	totalUpserted := 0
	parseMsTotal := 0
	parseCount := 0
	filters := LoadPCGWFilters(ctx, st)

	finishRun := func(status, errMsg string) {
		_ = st.FinishPCGWSyncRun(context.Background(), runID, status, errMsg, stats)
	}

	// ─── Single-page mode (unchanged) ────────────────────────────────────────
	if opts.SinglePage > 0 {
		n, err := syncOnePage(ctx, st, client, runID, opts.SinglePage, pcgw.PageInfo{PageID: opts.SinglePage}, &stats, filters)
		if err != nil {
			finishRun(JobFailed, err.Error())
			return n, err
		}
		stats.GamesOK++
		finishRun(JobSuccess, "")
		return bumpManifestAndReturn(ctx, st, n)
	}

	// ─── Manifest rebuild only ────────────────────────────────────────────────
	if opts.RebuildManifestOnly {
		finishRun(JobSuccess, "")
		return bumpManifestAndReturn(ctx, st, 0)
	}

	budget := opts.MaxPagesPerRun
	if budget <= 0 {
		budget = MaxPagesPerRun()
	}
	logPCGWSyncStart(runID, mode, opts, budget)

	// ─── Phase 1: Catalog scan ────────────────────────────────────────────────
	// ResumeCatalogScan=true is only set when checkpoint_phase=="ingest", meaning
	// a full Phase 1 already completed in a prior interrupted run.
	var phase1 types.Phase1Stats
	var catalogScanMode string
	var tailGrew bool // true when tail scan found new IDs; gates deferred rev-check
	if opts.SkipCatalogPhase {
		catalogScanMode = "skipped"
		if prior, err := st.GetLastSuccessfulPhase1Stats(ctx); err == nil && prior != nil {
			phase1 = *prior
		}
		if runID != "" && phase1.RemoteTotalIDs > 0 {
			_ = st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, phase1, catalogScanMode)
		}
		logx.Logger().Info().Str("component", "pcgw").
			Int("remote", phase1.RemoteTotalIDs).
			Msg("pcgw sync: skipping Phase 1 catalog refresh")
		logPhase1Complete(catalogScanMode, phase1, false)
	} else if !opts.ResumeCatalogScan {
		var err error
		phase1, tailGrew, catalogScanMode, err = runCatalogPhase(ctx, st, client, runID, opts, reportEx, &stats)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				_ = st.UpdatePCGWSyncRunCheckpoint(ctx, runID, 0, stats)
				finishRun(ctxStatus(ctx), ctx.Err().Error())
				return totalUpserted, ctx.Err()
			}
			finishRun(JobFailed, err.Error())
			return totalUpserted, err
		}
		stats.GamesTotal = phase1.RemoteTotalIDs
		logPhase1Complete(catalogScanMode, phase1, tailGrew)
	} else {
		// Resuming into Phase 2: load Phase 1 stats from the run we resumed from.
		// Invariant: ResumeCatalogScan=true iff checkpoint_phase=="ingest" (runner guarantees this).
		catalogScanMode = "resumed"
		resumeFromID := opts.ResumedFromRunID
		if resumeFromID != "" {
			if prior, err := st.GetPCGWSyncRunByID(ctx, resumeFromID); err == nil && prior != nil {
				phase1.RemoteTotalIDs = prior.RemoteTotalIDs
				phase1.MissingLocalIDs = prior.MissingLocalIDs
			}
		} else if latest, err := st.GetLatestPCGWSyncRun(ctx); err == nil && latest != nil {
			phase1.RemoteTotalIDs = latest.RemoteTotalIDs
			phase1.MissingLocalIDs = latest.MissingLocalIDs
		}
		logPhase1Complete(catalogScanMode, phase1, false)
	}

	if !opts.SkipCatalogPhase {
		if incomplete, catStats, _ := catalogIncomplete(ctx, st, phase1.RemoteTotalIDs); incomplete {
			logPhase1Decision(phase1ReasonCatalogIncomplete, func(e *zerolog.Event) {
				e.Int("catalog_rows", catStats.RemoteTotal).Int("remote_total", phase1.RemoteTotalIDs)
			})
			var err error
			phase1, err = RunCatalogScan(ctx, st, client, runID, phase1ReasonCatalogIncomplete, reportEx)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					_ = st.UpdatePCGWSyncRunCheckpoint(ctx, runID, 0, stats)
					finishRun(ctxStatus(ctx), ctx.Err().Error())
					return totalUpserted, ctx.Err()
				}
				finishRun(JobFailed, err.Error())
				return totalUpserted, err
			}
			stats.GamesTotal = phase1.RemoteTotalIDs
			catalogScanMode = "full"
			tailGrew = true // treat as full growth so rev-check runs
			opts.ResumeCatalogScan = false
			opts.ResumeQueueCursor = 0
		}
	}

	if opts.SkipIngestPhase {
		finishRun(JobSuccess, "")
		return 0, nil
	}

	// ─── Catalog hash no-op optimization ─────────────────────────────────────
	incompleteCatalog, catStats, _ := catalogIncomplete(ctx, st, phase1.RemoteTotalIDs)
	if incompleteCatalog {
		logx.Logger().Info().Str("component", "pcgw").
			Int("catalog_rows", catStats.RemoteTotal).Int("remote_total", phase1.RemoteTotalIDs).
			Msg("pcgw sync: catalog incomplete vs remote — skipping no-op gate")
	} else if !opts.Full && !opts.RetryFailedOnly && !opts.MissingOnly && phase1.CatalogHash != "" {
		if prev, err := getPreviousCatalogHash(ctx, st); err == nil && prev == phase1.CatalogHash {
			// Catalog is unchanged — skip Phase 2 only when there is no backlog at all.
			failedCount := 0
			if ids, err := st.ListPCGWCatalogFailedPartial(ctx, 1, 0); err == nil {
				failedCount = len(ids)
			}
			titleBackfillCount := 0
			if rows, err := st.ListPCGWCatalogTitleBackfill(ctx, 1, 0); err == nil {
				titleBackfillCount = len(rows)
			}
			missingCount := 0
			if ids, err := st.ListPCGWCatalogMissing(ctx, 1, 0); err == nil {
				missingCount = len(ids)
			}
			if missingCount == 0 && failedCount == 0 && titleBackfillCount == 0 {
				logPhase2Skip("catalog_hash_unchanged_no_backlog")
				finishRun(JobSuccess, "")
				return bumpManifestAndReturn(ctx, st, 0)
			}
			logx.Logger().Info().Str("component", "pcgw").
				Int("missing", missingCount).Int("failed", failedCount).Int("title_backfill", titleBackfillCount).
				Msg("pcgw sync: catalog hash unchanged but backlog non-empty — proceeding to Phase 2")
		}
	}

	// ─── Phase 2: Build targeted queue ───────────────────────────────────────
	var queue []int64
	titleHints := map[int64]string{}
	revHints := map[int64]pcgw.PageRevision{}
	inQueue := map[int64]bool{}
	enqueue := func(id int64) {
		if inQueue[id] {
			return
		}
		queue = append(queue, id)
		inQueue[id] = true
	}

	var missing []int64
	var titleBackfill []types.PCGWCatalogEntry
	var failedPartial []int64
	var changedIDs []int64

	if !opts.RetryFailedOnly {
		// Priority 1: missing IDs
		var err error
		missing, err = st.ListPCGWCatalogMissing(ctx, 0, 0)
		if err != nil {
			logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("pcgw sync: list missing")
			missing = nil
		}
		for _, id := range missing {
			enqueue(id)
		}
	}

	if !opts.RetryFailedOnly && !opts.MissingOnly {
		// Priority 2: rows with blank local title/page_name but non-empty catalog title.
		var err error
		titleBackfill, err = st.ListPCGWCatalogTitleBackfill(ctx, 0, 0)
		if err != nil {
			logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("pcgw sync: list title-backfill")
			titleBackfill = nil
		}
		for _, row := range titleBackfill {
			enqueue(row.PageID)
			if strings.TrimSpace(row.Title) != "" {
				titleHints[row.PageID] = strings.TrimSpace(row.Title)
			}
		}
	}

	// Priority 3: failed/partial IDs
	if !opts.MissingOnly {
		var err error
		failedPartial, err = st.ListPCGWCatalogFailedPartial(ctx, 0, 0)
		if err != nil {
			logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("pcgw sync: list failed/partial")
			failedPartial = nil
		}
		for _, id := range failedPartial {
			enqueue(id)
		}
	}

	// Priority 4: changed rev IDs — deferred when catalog is unchanged and not stale.
	// Run when: full sync, tail scan found new IDs, or last rev-check was >7 days ago.
	if !opts.RetryFailedOnly && !opts.MissingOnly {
		shouldRunRevCheck := opts.Full || tailGrew
		revCheckReason := ""
		lastRevCheckAt := ""
		var revCheckAge time.Duration
		if opts.Full {
			revCheckReason = "full_sync"
		} else if tailGrew {
			revCheckReason = "tail_grew"
		}
		if !shouldRunRevCheck {
			if meta, err := st.GetPCGWManifestMeta(ctx); err == nil && meta != nil {
				lastRevCheckAt = meta.LastRevCheckAt
				if meta.LastRevCheckAt == "" {
					shouldRunRevCheck = true
					revCheckReason = "never_checked"
				} else if t, err := time.Parse(time.RFC3339, meta.LastRevCheckAt); err == nil {
					revCheckAge = time.Since(t)
					if revCheckAge > 7*24*time.Hour {
						shouldRunRevCheck = true
						revCheckReason = "interval_elapsed"
					} else {
						revCheckReason = "interval_not_elapsed"
					}
				} else {
					shouldRunRevCheck = true
					revCheckReason = "invalid_last_rev_check_at"
				}
			} else {
				shouldRunRevCheck = true
				revCheckReason = "no_manifest_meta"
			}
		}
		logRevCheckDecision(shouldRunRevCheck, revCheckReason, lastRevCheckAt, revCheckAge)
		if shouldRunRevCheck {
			res, err := buildChangedQueue(ctx, st, client, runID, filters, lastRevCheckAt)
			if err != nil {
				logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("pcgw sync: build changed queue")
				changedIDs = nil
			} else {
				changedIDs = res.PageIDs
				for id, rev := range res.RevHints {
					revHints[id] = rev
				}
				for id, title := range res.TitleHints {
					if strings.TrimSpace(titleHints[id]) == "" {
						titleHints[id] = title
					}
				}
			}
			_ = st.SetLastRevCheckAt(ctx, time.Now())
		}
		for _, id := range changedIDs {
			enqueue(id)
		}
	}

	queueSize := len(queue)
	_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, 0, 0)

	// Persist queue size into targeted_queue_size column (preserves catalog_scan_mode).
	if runID != "" {
		_ = st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, types.Phase1Stats{
			RemoteTotalIDs:  phase1.RemoteTotalIDs,
			MissingLocalIDs: len(queue),
			ExtraLocalIDs:   phase1.ExtraLocalIDs,
			CatalogHash:     phase1.CatalogHash,
			CompletedAt:     phase1.CompletedAt,
		}, catalogScanMode)
	}

	logx.Logger().Info().Str("component", "pcgw").
		Int("queue", queueSize).Int("missing", len(missing)).Int("title_backfill", len(titleBackfill)).
		Int("failed", len(failedPartial)).Int("changed", len(changedIDs)).
		Int("budget", budget).Int("resume_cursor", opts.ResumeQueueCursor).
		Msg("pcgw sync phase2: queue built")

	// ─── Phase 2: Process queue with budget ───────────────────────────────────
	startCursor := phase2StartCursor(opts.ResumeQueueCursor, queueSize, opts.ResumeCatalogScan)
	if opts.ResumeCatalogScan && opts.ResumeQueueCursor > 0 && startCursor == 0 {
		logx.Logger().Warn().Str("component", "pcgw").Int("resume_cursor", opts.ResumeQueueCursor).
			Msg("pcgw sync phase2: ignoring stale resume cursor because queue is rebuilt from current backlog")
	}
	processed := 0

	for i := startCursor; i < len(queue); i++ {
		select {
		case <-ctx.Done():
			_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, processed, i)
			finishRun(ctxStatus(ctx), ctx.Err().Error())
			return totalUpserted, ctx.Err()
		default:
		}

		if budget > 0 && processed >= budget {
			// Budget exhausted — save checkpoint for resume.
			_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, processed, i)
			finishRun(JobInterrupted, "budget exhausted")
			logx.Logger().Info().Str("component", "pcgw").
				Int("budget", budget).Int("cursor", i).Int("queue_size", queueSize).
				Msg("pcgw sync: budget exhausted at cursor")
			return bumpManifestAndReturn(ctx, st, totalUpserted)
		}

		pageID := queue[i]

		if reportProgress != nil {
			reportProgress(processed)
		}
		if reportEx != nil {
			reportEx(PCGWSyncProgress{
				PagesProcessed: processed,
				TotalEstimate:  queueSize,
				Phase:          "ingest",
				GamesSkipped:   stats.GamesSkipped,
				QueueSize:      queueSize,
				QueueCursor:    i,
			})
		}

		pageInfo := pcgw.PageInfo{PageID: pageID}
		// Seed title from catalog so blank-title rows heal even if parse payload omits title.
		if hint := strings.TrimSpace(titleHints[pageID]); hint != "" {
			pageInfo.Title = hint
		}

		var knownRev *pcgw.PageRevision
		if rev, ok := revHints[pageID]; ok && rev.RevID > 0 {
			knownRev = &rev
		}

		start := time.Now()
		n, err := syncOnePageRev(ctx, st, client, runID, pageID, pageInfo, knownRev, &stats, filters)
		parseMsTotal += int(time.Since(start).Milliseconds())
		parseCount++
		processed++
		logPhase2IngestProgress(processed, queueSize, i)

		if err != nil {
			logx.Logger().Error().Str("component", "pcgw").Int64("page_id", pageID).Err(err).
				Msg("pcgw sync: page")
			stats.GamesFailed++
			_ = st.IncrementCatalogRetry(ctx, pageID, err.Error())
			continue
		}
		// Clear dead-letter on success.
		_ = st.ClearCatalogDeadLetter(ctx, pageID)
		totalUpserted += n

		// Checkpoint every 100 pages.
		if processed%100 == 0 {
			_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, processed, i+1)
		}
	}

	if parseCount > 0 {
		stats.AvgParseMs = parseMsTotal / parseCount
	}
	_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, processed, len(queue))

	status := JobSuccess
	if stats.GamesFailed > 0 {
		status = "partial"
	}
	finishRun(status, "")
	logx.Logger().Info().Str("component", "pcgw").
		Int("upserted", totalUpserted).Int("ok", stats.GamesOK).
		Int("partial", stats.GamesPartial).Int("failed", stats.GamesFailed).Int("skipped", stats.GamesSkipped).
		Msg("pcgw sync: done")
	return bumpManifestAndReturn(ctx, st, totalUpserted)
}

func phase2StartCursor(resumeCursor, queueSize int, resumeCatalogScan bool) int {
	if !resumeCatalogScan || resumeCursor <= 0 || queueSize <= 0 {
		return 0
	}
	// Phase 2 queue is rebuilt from current DB state each run (missing/failed/changed).
	// Reusing a stale absolute index can skip entries after queue membership shifts.
	return 0
}

// ctxStatus returns the job status string based on context error.
func ctxStatus(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return JobCanceled
	}
	return JobInterrupted
}

// runCatalogPhase decides whether to run a full catalog scan, a tail scan, or skip entirely.
// It returns the resulting Phase1Stats, whether the tail scan found new IDs (tailGrew),
// the catalogScanMode string, and any error.
//
// Full scan is used when:
//   - opts.Full or opts.ForceFull
//   - catalog is empty (localCount == 0)
//   - no prior successful Phase 1 exists
//
// Fast path (routine incremental):
//   - Load last successful Phase 1 stats
//   - Probe: call ListGamePages(ctx, 1, localCount)
//   - If probe empty: use cached stats, mode="fast_probe", tailGrew=false
//   - If probe non-empty: run ScanCatalogTail, mode="tail", tailGrew=true
func runCatalogPhase(ctx context.Context, st store.Store, client *pcgw.Client, runID string, opts PCGWSyncOptions, reportEx ReportProgressEx, stats *store.PCGWSyncRunStats) (phase1 types.Phase1Stats, tailGrew bool, catalogScanMode string, err error) {
	// Always do a full scan for full/force-full runs.
	if opts.Full || opts.ForceFull {
		reason := phase1ReasonFullSync
		if opts.ForceFull {
			reason = phase1ReasonForceFull
		}
		logPhase1Decision(reason)
		phase1, err = RunCatalogScan(ctx, st, client, runID, reason, reportEx)
		if err == nil {
			_ = st.UpdateLastFullSyncAt(ctx)
		}
		return phase1, true, "full", err
	}

	// Check local catalog count.
	catStats, statsErr := st.GetPCGWCatalogStats(ctx)
	if statsErr != nil {
		logPhase1Decision(phase1ReasonCatalogStatsError, func(e *zerolog.Event) { e.Err(statsErr) })
		phase1, err = RunCatalogScan(ctx, st, client, runID, phase1ReasonCatalogStatsError, reportEx)
		if err == nil {
			_ = st.UpdateLastFullSyncAt(ctx)
		}
		return phase1, true, "full", err
	}
	localCount := catStats.RemoteTotal

	// No prior successful Phase 1 → full scan.
	prior, priorErr := st.GetLastSuccessfulPhase1Stats(ctx)
	if priorErr != nil || prior == nil || localCount == 0 {
		reason := phase1ReasonNoPriorPhase1
		if localCount == 0 {
			reason = phase1ReasonEmptyCatalog
		}
		logPhase1Decision(reason, func(e *zerolog.Event) {
			e.Int("local_count", localCount).Bool("has_prior_phase1", prior != nil)
			if priorErr != nil {
				e.Err(priorErr)
			}
		})
		phase1, err = RunCatalogScan(ctx, st, client, runID, reason, reportEx)
		if err == nil {
			_ = st.UpdateLastFullSyncAt(ctx)
		}
		return phase1, true, "full", err
	}

	// Fast path: probe for growth beyond local count.
	grew, probeErr := ProbeCatalogGrowth(ctx, client, localCount)
	if probeErr != nil {
		logPhase1Decision(phase1ReasonProbeFailed, func(e *zerolog.Event) {
			e.Err(probeErr).Int("local_count", localCount)
		})
		phase1, err = RunCatalogScan(ctx, st, client, runID, phase1ReasonProbeFailed, reportEx)
		if err == nil {
			_ = st.UpdateLastFullSyncAt(ctx)
		}
		return phase1, true, "full", err
	}

	if !grew {
		// No new pages beyond local count — use cached stats.
		phase1 = *prior
		if runID != "" {
			_ = st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, phase1, "fast_probe")
		}
		logPhase1Decision("fast_probe", func(e *zerolog.Event) {
			e.Int("local_count", localCount).Int("cached_remote_total", prior.RemoteTotalIDs)
		})
		return phase1, false, "fast_probe", nil
	}

	// Tail growth found: scan only the new tail.
	logPhase1Decision("tail", func(e *zerolog.Event) {
		e.Int("local_count", localCount).Int("cached_remote_total", prior.RemoteTotalIDs)
	})
	phase1, err = ScanCatalogTail(ctx, st, client, runID, localCount, prior.RemoteTotalIDs, reportEx)
	if err != nil {
		return phase1, false, "tail", err
	}
	return phase1, true, "tail", nil
}

// catalogIncomplete is true when pcgw_catalog has fewer rows than the last Phase 1 remote count.
func catalogIncomplete(ctx context.Context, st store.Store, remoteTotal int) (bool, types.PCGWCatalogStats, error) {
	catStats, err := st.GetPCGWCatalogStats(ctx)
	if err != nil {
		return false, catStats, err
	}
	if remoteTotal <= 0 {
		return false, catStats, nil
	}
	return catStats.RemoteTotal < remoteTotal, catStats, nil
}

// getPreviousCatalogHash retrieves the catalog_hash from the last completed sync run.
func getPreviousCatalogHash(ctx context.Context, st store.Store) (string, error) {
	runs, err := st.ListPCGWSyncRuns(ctx, 5)
	if err != nil {
		return "", err
	}
	for _, r := range runs {
		if r.Status == JobSuccess || r.Status == "partial" {
			if r.CatalogHash != "" {
				return r.CatalogHash, nil
			}
		}
	}
	return "", nil
}

// rcWindowMax bounds how old a rev-check window may be for the cheap
// recentchanges path. MediaWiki's recent-changes retention ($wgRCMaxAge) is
// typically 30-90 days; staying well inside it guarantees no edit can fall
// between the window and the feed. Older windows use the batched revision
// sweep, which is correct for any gap size.
const rcWindowMax = 30 * 24 * time.Hour

// rcOverlap re-reads a little of the already-covered window so boundary
// timestamps and clock skew can never drop an edit.
const rcOverlap = 2 * time.Hour

// changedQueueResult is the outcome of change detection: pages to re-ingest,
// their already-known latest revisions (saves one API call each during
// ingest), title hints for brand-new pages, and how many upstream deletions
// were applied.
type changedQueueResult struct {
	PageIDs    []int64
	RevHints   map[int64]pcgw.PageRevision
	TitleHints map[int64]string
	Deleted    int
	Method     string // "recentchanges" or "sweep"
}

// buildChangedQueue finds catalog pages whose wiki content changed since the
// last check. It prefers the recentchanges feed (a handful of requests for a
// typical week) and falls back to the batched revision sweep when the window
// is missing, stale beyond rcWindowMax, or the feed errors.
func buildChangedQueue(ctx context.Context, st store.Store, client *pcgw.Client, runID string, filters PCGWFilters, lastRevCheckAt string) (changedQueueResult, error) {
	since, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(lastRevCheckAt))
	if strings.TrimSpace(lastRevCheckAt) == "" || parseErr != nil || time.Since(since) > rcWindowMax {
		reason := "window_missing"
		if parseErr == nil && strings.TrimSpace(lastRevCheckAt) != "" {
			reason = "window_stale"
		}
		logx.Logger().Info().Str("component", "pcgw").Str("event", "pcgw.rev_check.method").
			Str("method", "sweep").Str("reason", reason).
			Msg("pcgw sync: change detection via batched revision sweep")
		return buildChangedQueueSweep(ctx, st, client, filters)
	}

	changes, err := client.RecentChangesSince(ctx, since.Add(-rcOverlap))
	if err != nil {
		logx.Logger().Warn().Str("component", "pcgw").Err(err).
			Msg("pcgw sync: recentchanges failed — falling back to batched revision sweep")
		return buildChangedQueueSweep(ctx, st, client, filters)
	}
	logx.Logger().Info().Str("component", "pcgw").Str("event", "pcgw.rev_check.method").
		Str("method", "recentchanges").Int("entries", len(changes)).
		Str("since", since.Add(-rcOverlap).UTC().Format(time.RFC3339)).
		Msg("pcgw sync: change detection via recentchanges feed")
	return applyRecentChanges(ctx, st, runID, filters, changes)
}

// applyRecentChanges folds a recentchanges window into a change queue:
// edits/creations/moves become re-ingest candidates (skipped when the stored
// revision is already current), page creations gain a catalog row so exports
// stay consistent, and wiki deletions cascade locally (tombstoned, so bundle
// consumers delete too) behind the same safety valve as bundle imports.
func applyRecentChanges(ctx context.Context, st store.Store, runID string, filters PCGWFilters, changes []pcgw.RecentChange) (changedQueueResult, error) {
	res := changedQueueResult{
		RevHints:   map[int64]pcgw.PageRevision{},
		TitleHints: map[int64]string{},
		Method:     "recentchanges",
	}

	// Latest state per page across the window (entries are oldest-first).
	type pageState struct {
		title   string
		revID   int64
		revTS   string
		deleted bool
	}
	pages := map[int64]*pageState{}
	deletedTitles := map[string]bool{}
	stateFor := func(id int64) *pageState {
		ps := pages[id]
		if ps == nil {
			ps = &pageState{}
			pages[id] = ps
		}
		return ps
	}
	for _, rc := range changes {
		switch rc.Type {
		case "edit", "new":
			if rc.PageID <= 0 {
				continue
			}
			ps := stateFor(rc.PageID)
			ps.title = rc.Title
			ps.deleted = false
			delete(deletedTitles, rc.Title)
			if rc.RevID > 0 {
				ps.revID = rc.RevID
				ps.revTS = rc.Timestamp
			}
		case "log":
			switch rc.LogType {
			case "delete":
				if rc.LogAction == "restore" || rc.LogAction == "undelete" {
					continue
				}
				if rc.PageID > 0 {
					ps := stateFor(rc.PageID)
					ps.title = rc.Title
					ps.deleted = true
				} else if rc.Title != "" {
					deletedTitles[rc.Title] = true
				}
			case "move":
				// The moved page keeps its ID; re-ingest refreshes the title.
				if rc.PageID > 0 {
					ps := stateFor(rc.PageID)
					ps.title = rc.Title
					ps.deleted = false
				}
			}
		}
	}

	// Resolve title-only deletions against the local catalog.
	var deleteIDs []int64
	for title := range deletedTitles {
		id, err := st.GetPCGWCatalogPageIDByTitle(ctx, title)
		if err != nil || id == 0 {
			continue
		}
		if ps, ok := pages[id]; ok && !ps.deleted {
			continue // page was edited/recreated after the deletion event
		}
		deleteIDs = append(deleteIDs, id)
	}
	for id, ps := range pages {
		if ps.deleted {
			deleteIDs = append(deleteIDs, id)
		}
	}

	// Safety valve mirroring pcgwReconcileMaxDeleteFraction on import: a
	// deletion burst larger than a quarter of the mirror means the feed (or
	// our window) is suspect — skip deletions rather than gut the mirror.
	if len(deleteIDs) > 0 {
		catStats, statErr := st.GetPCGWCatalogStats(ctx)
		if statErr == nil && catStats.RemoteTotal > 0 &&
			float64(len(deleteIDs))/float64(catStats.RemoteTotal) > 0.25 {
			logx.Logger().Warn().Str("component", "pcgw").
				Int("would_delete", len(deleteIDs)).Int("catalog", catStats.RemoteTotal).
				Msg("pcgw sync: skipping recentchanges deletions — removal set too large")
			deleteIDs = nil
		}
	}
	for _, id := range deleteIDs {
		if err := st.DeletePCGWGameCascade(ctx, id); err != nil {
			logx.Logger().Warn().Str("component", "pcgw").Int64("page_id", id).Err(err).
				Msg("pcgw sync: delete upstream-removed game")
			continue
		}
		res.Deleted++
	}
	if res.Deleted > 0 {
		logx.Logger().Info().Str("component", "pcgw").Int("deleted", res.Deleted).
			Msg("pcgw sync: propagated upstream wiki deletions")
	}

	// Changed/new pages -> queue (skip when stored revision already current).
	var newCatalogEntries []types.PCGWCatalogEntry
	now := time.Now().UTC().Format(time.RFC3339)
	for id, ps := range pages {
		if ps.deleted {
			continue
		}
		if filters.ShouldSkipTitle(ps.title) {
			continue
		}
		if g, err := st.GetPCGWGame(ctx, id); err == nil && g != nil &&
			ps.revID > 0 && g.LastRevID == ps.revID && g.ParseStatus == "ok" {
			continue
		}
		if catID, err := st.GetPCGWCatalogPageIDByTitle(ctx, ps.title); err == nil && catID == 0 {
			// Brand-new page: give it a catalog row now so the exported
			// catalog (which drives consumer-side deletion reconciliation)
			// includes it from the first publish.
			newCatalogEntries = append(newCatalogEntries, types.PCGWCatalogEntry{
				PageID: id, Title: ps.title,
				FirstSeenAt: now, LastSeenAt: now, LastSeenRunID: runID,
			})
		}
		res.PageIDs = append(res.PageIDs, id)
		if ps.revID > 0 {
			res.RevHints[id] = pcgw.PageRevision{RevID: ps.revID, Timestamp: ps.revTS}
		}
		if strings.TrimSpace(ps.title) != "" {
			res.TitleHints[id] = ps.title
		}
	}
	if len(newCatalogEntries) > 0 {
		if err := st.UpsertPCGWCatalogBatch(ctx, newCatalogEntries); err != nil {
			logx.Logger().Warn().Str("component", "pcgw").Err(err).
				Msg("pcgw sync: upsert catalog rows for new pages")
		}
	}
	sort.Slice(res.PageIDs, func(i, j int) bool { return res.PageIDs[i] < res.PageIDs[j] })

	logx.Logger().Info().Str("component", "pcgw").Str("event", "pcgw.rev_check.complete").
		Int("games_checked", len(pages)).Int("changed_found", len(res.PageIDs)).
		Int("deleted", res.Deleted).Str("method", res.Method).
		Msg("pcgw sync: recentchanges change detection complete")
	return res, nil
}

// buildChangedQueueSweep compares stored revision IDs against the wiki for
// every OK game, 50 pages per request (the MediaWiki batch maximum). It is the
// correctness fallback for windows older than the wiki's recent-changes
// retention; ~55k games cost ~1100 requests instead of 55k.
func buildChangedQueueSweep(ctx context.Context, st store.Store, client *pcgw.Client, filters PCGWFilters) (changedQueueResult, error) {
	res := changedQueueResult{
		RevHints:   map[int64]pcgw.PageRevision{},
		TitleHints: map[int64]string{},
		Method:     "sweep",
	}
	offset := 0
	checked := 0
	const chunkSize = 200
	logx.Logger().Info().Str("component", "pcgw").Str("event", "pcgw.rev_check.start").
		Msg("pcgw sync: batched revision sweep started — comparing wiki rev IDs for stored OK games")
	for {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}
		rows, _, err := st.ListPCGWGames(ctx, store.PCGWGameListFilter{
			ParseStatus: "ok",
			Limit:       chunkSize,
			Offset:      offset,
		})
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			break
		}
		var ids []int64
		storedRev := map[int64]int64{}
		for _, g := range rows {
			if filters.ShouldSkipTitle(g.Title) {
				continue
			}
			ids = append(ids, g.PageID)
			storedRev[g.PageID] = g.LastRevID
		}
		revs, err := client.GetPageRevisionsBatch(ctx, ids)
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			checked++
			rev, ok := revs[id]
			if !ok || rev.RevID != storedRev[id] {
				res.PageIDs = append(res.PageIDs, id)
				if ok {
					res.RevHints[id] = rev
				}
			}
			logRevCheckProgress(checked, len(res.PageIDs))
		}
		if len(rows) < chunkSize {
			break
		}
		offset += len(rows)
	}
	logx.Logger().Info().Str("component", "pcgw").Str("event", "pcgw.rev_check.complete").
		Int("games_checked", checked).Int("changed_found", len(res.PageIDs)).
		Str("method", res.Method).
		Msg("pcgw sync: batched revision sweep complete")
	return res, nil
}

func bumpManifestAndReturn(ctx context.Context, st store.Store, n int) (int, error) {
	// Derive a stable ETag from the next version number so identical content
	// always produces the same ETag (no time.Now() noise).
	newVersion := 1
	if meta, err := st.GetPCGWManifestMeta(ctx); err == nil && meta != nil {
		newVersion = meta.ManifestVersion + 1
	}
	etag := store.ManifestETagFromGames(newVersion)
	if v, err := st.BumpManifestVersion(ctx, etag); err == nil {
		logx.Logger().Info().Str("component", "pcgw").Int("version", v).
			Msg("pcgw sync: manifest version bumped")
	}
	return n, nil
}

func syncOnePage(ctx context.Context, st store.Store, client *pcgw.Client, runID string, pageID int64, p pcgw.PageInfo, stats *store.PCGWSyncRunStats, filters PCGWFilters) (int, error) {
	return syncOnePageRev(ctx, st, client, runID, pageID, p, nil, stats, filters)
}

// syncOnePageRev is syncOnePage with an optional already-known latest revision
// from change detection, saving one API request per page.
func syncOnePageRev(ctx context.Context, st store.Store, client *pcgw.Client, runID string, pageID int64, p pcgw.PageInfo, knownRev *pcgw.PageRevision, stats *store.PCGWSyncRunStats, filters PCGWFilters) (int, error) {
	result, err := pcgw.IngestPageWithRevision(ctx, client, pageID, p, knownRev)
	if err != nil {
		// IngestPage returns a non-nil partial result even on fetch error (PageID and
		// ParseStatus="failed" are set). Persist the stub so the page moves from
		// "missing" to "failed" in pcgw_games and becomes visible to the retry queue
		// (ListPCGWCatalogFailedPartial). Without this, every failed page stays
		// "missing", accumulates retries, dead-letters, and becomes unretriable.
		if result != nil {
			_, _ = PersistIngestResult(ctx, st, runID, result, filters)
		}
		return 0, err
	}
	n, persistErr := PersistIngestResult(ctx, st, runID, result, filters)
	if persistErr != nil {
		return n, persistErr
	}
	switch result.Bundle.ParseStatus {
	case "ok":
		stats.GamesOK++
	case "partial":
		stats.GamesPartial++
	default:
		if len(result.Errors) > 0 {
			stats.GamesFailed++
		} else {
			stats.GamesOK++
		}
	}
	return n, nil
}

// PersistIngestResult writes ingest bundle to pcgw_* tables and projects manifest paths.
func PersistIngestResult(ctx context.Context, st store.Store, syncRunID string, result *pcgw.IngestResult, filters PCGWFilters) (int, error) {
	if result == nil {
		return 0, nil
	}
	b := result.Bundle
	gameID := strconv.FormatInt(b.PageID, 10)
	gameTitle := strings.TrimSpace(b.PageInfo.Title)
	if gameTitle == "" {
		// Preserve a previously known title so partial refreshes never blank it out.
		if existing, err := st.GetPCGWGame(ctx, b.PageID); err == nil && existing != nil {
			gameTitle = strings.TrimSpace(existing.Title)
			if gameTitle == "" {
				gameTitle = strings.TrimSpace(existing.PageName)
			}
		}
	}
	pageName := gameTitle
	now := time.Now().UTC().Format(time.RFC3339)

	// PCGW's Cargo Steam_AppID is sometimes empty even when the infobox carries
	// the ID; fall back to the infobox so Steam App IDs (needed for Linux/Proton
	// save-path resolution) are persisted and projected into the manifest.
	steamAppIDs := b.PageInfo.SteamAppIDs
	if len(steamAppIDs) == 0 {
		steamAppIDs = pcgw.SteamAppIDsFromInfobox(b.Infobox)
	}

	platforms := []string{}
	gameDataOK := false

	// Upsert game row
	g := &types.PCGWGame{
		PageID:           b.PageID,
		PageName:         pageName,
		Title:            gameTitle,
		SteamAppIDs:      steamAppIDs,
		GOGID:            b.PageInfo.GOGID,
		EpicID:           b.PageInfo.EpicID,
		UbisoftID:        b.PageInfo.UbisoftID,
		HLTBID:           b.PageInfo.HLTBID,
		IGDBID:           b.PageInfo.IGDBID,
		CoverURL:         b.PageInfo.CoverURL,
		LastRevID:        b.RevisionID,
		LastRevTimestamp: b.RevisionTimestamp,
		LastFetchedAt:    now,
		ParseStatus:      b.ParseStatus,
		PlatformsPresent: platforms,
	}
	if len(b.Infobox) > 0 {
		infobox := map[string]interface{}{}
		for k, v := range b.Infobox {
			infobox[k] = v
		}
		g.Infobox = infobox
	}
	if err := st.UpsertPCGWGame(ctx, g); err != nil {
		return 0, err
	}

	// Metadata
	contentHash := hashWikitext(b.FullWikitext)
	sectionHashes := map[string]string{}
	for k, sr := range b.Sections {
		sectionHashes[k] = hashWikitext(sr.SectionWikitext)
	}
	storeFull := os.Getenv("GSBS_PCGW_STORE_FULL_WIKITEXT")
	if storeFull == "" || storeFull == "true" || storeFull == "1" {
		// default true
	} else {
		b.FullWikitextZstd = nil
	}
	meta := &types.PCGWMetadata{
		PageID:           b.PageID,
		FullWikitextZstd: b.FullWikitextZstd,
		ContentHash:      contentHash,
		SectionHashes:    sectionHashes,
		UncompressedSize: len(b.FullWikitext),
		LastFetchedAt:    now,
	}
	if err := st.UpsertPCGWMetadata(ctx, meta); err != nil {
		return 0, err
	}

	// Sections — only upsert on success (§4 resilience)
	for key, sr := range b.Sections {
		if sr.ParseError != "" {
			_ = st.InsertPCGWParseFailure(ctx, &types.PCGWParseFailure{
				PageID: b.PageID, SyncRunID: syncRunID, Section: key,
				ErrorMessage: sr.ParseError, WikitextSnippet: truncate(snippet(sr.SectionWikitext), 500),
			})
			continue
		}
		row := &types.PCGWSectionRow{
			PageID: b.PageID, Data: sr.Data, AllTemplates: sr.AllTemplates,
			SectionWikitext: sr.SectionWikitext, UpdatedAt: now,
		}
		if err := upsertSection(ctx, st, key, row); err != nil {
			logx.Logger().Error().Str("component", "pcgw").Str("section", key).
				Int64("page_id", b.PageID).Err(err).Msg("pcgw persist: section")
		}
		if key == "game_data" {
			gameDataOK = true
			if err := persistGameDataPlatforms(ctx, st, b.PageID, sr, now, &platforms); err != nil {
				logx.Logger().Error().Str("component", "pcgw").Int64("page_id", b.PageID).Err(err).
					Msg("pcgw persist: game_data platforms")
			}
		}
	}

	// Update platforms on game
	g2, _ := st.GetPCGWGame(ctx, b.PageID)
	if g2 != nil {
		g2.PlatformsPresent = platforms
		g2.ParseStatus = b.ParseStatus
		_ = st.UpsertPCGWGame(ctx, g2)
	}

	// Project manifest v1 — only if game_data parsed OK or we have save locations
	var entries []types.GameSaveLocation
	if gameDataOK || len(b.SaveLocations) > 0 {
		// slotCounts tracks per-(platform, isConfig) position so that the Windows
		// and Linux templates for the same logical save slot share the same SlotLabel.
		slotCounts := map[string]int{}
		for _, t := range b.SaveLocations {
			platform := pcgw.SystemToPlatform(t.System)
			sk := platform + "\x00" + strconv.FormatBool(t.IsConfig)
			slotLabel := strconv.Itoa(slotCounts[sk])
			slotCounts[sk]++
			for _, path := range t.Paths {
				rules := pcgw.ParseSaveRules(path, platform, t.IsConfig)
				for _, rule := range rules {
					if filters.ShouldExcludePath(rule.Directory) {
						continue
					}
					rule.SlotLabel = slotLabel
					entries = append(entries, types.GameSaveLocation{
						GameID: gameID, PCGWPageID: b.PageID, GameTitle: gameTitle,
						Platform: platform, PathTemplate: rule.Directory, IsConfig: t.IsConfig,
						SaveRules:   []types.SaveRule{rule},
						Source:      "pcgw",
						Notes:       "https://www.pcgamingwiki.com/wiki/?curid=" + gameID,
						SteamAppIDs: steamAppIDs, GOGID: b.PageInfo.GOGID,
						EpicID: b.PageInfo.EpicID, UbisoftID: b.PageInfo.UbisoftID,
						UpdatedAt: now,
					})
				}
			}
		}
		if gameDataOK && len(entries) > 0 {
			if err := st.ReplaceGameSaveLocationsForGame(ctx, gameID, entries); err != nil {
				return 0, err
			}
			return len(entries), nil
		}
	}
	return 0, nil
}

func upsertSection(ctx context.Context, st store.Store, key string, row *types.PCGWSectionRow) error {
	switch key {
	case "availability":
		return st.UpsertPCGWAvailability(ctx, row)
	case "monetization":
		return st.UpsertPCGWMonetization(ctx, row)
	case "video":
		return st.UpsertPCGWVideo(ctx, row)
	case "input":
		return st.UpsertPCGWInput(ctx, row)
	case "audio":
		return st.UpsertPCGWAudio(ctx, row)
	case "network":
		return st.UpsertPCGWNetwork(ctx, row)
	case "notes":
		return st.UpsertPCGWNotes(ctx, row)
	case "references":
		return st.UpsertPCGWReferences(ctx, row)
	case "external_links":
		return st.UpsertPCGWExternalLinks(ctx, row)
	case "other", "lead", "system_requirements":
		return st.UpsertPCGWOther(ctx, row)
	default:
		if row.Data == nil {
			row.Data = map[string]interface{}{}
		}
		row.Data["section_key"] = key
		return st.UpsertPCGWOther(ctx, row)
	}
}

func persistGameDataPlatforms(ctx context.Context, st store.Store, pageID int64, sr pcgw.SectionResult, now string, platforms *[]string) error {
	byPlatform := map[string]*types.PCGWGameData{}

	addPaths := func(system string, paths []string, isConfig bool) {
		if len(paths) == 0 {
			return
		}
		pk := pcgw.SystemToPlatform(system)
		if *platforms != nil {
			found := false
			for _, p := range *platforms {
				if p == pk {
					found = true
					break
				}
			}
			if !found {
				*platforms = append(*platforms, pk)
			}
		}
		gd, ok := byPlatform[pk]
		if !ok {
			gd = &types.PCGWGameData{PageID: pageID, PlatformKey: pk, PlatformRawLabel: system}
			byPlatform[pk] = gd
		}
		entry := types.PCGWPathEntry{PathTemplates: paths, IsConfig: isConfig}
		if isConfig {
			gd.ConfigLocations = append(gd.ConfigLocations, entry)
		} else {
			gd.SaveLocations = append(gd.SaveLocations, entry)
		}
	}

	if templates, ok := sr.Data["templates"].([]pcgw.SaveLocationTemplate); ok {
		for _, t := range templates {
			addPaths(t.System, t.Paths, t.IsConfig)
		}
	}

	keep := make([]string, 0, len(byPlatform))
	for pk, gd := range byPlatform {
		gd.AllTemplates = sr.AllTemplates
		gd.SectionWikitext = sr.SectionWikitext
		gd.UpdatedAt = now
		if err := st.UpsertPCGWGameData(ctx, gd); err != nil {
			return err
		}
		keep = append(keep, pk)
	}
	return st.DeletePCGWGameDataExcept(ctx, pageID, keep)
}

func hashWikitext(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func snippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

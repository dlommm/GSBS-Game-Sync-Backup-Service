package job

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
)

// ErrJobAlreadyRunning is returned when a sync is requested while one is in progress.
var ErrJobAlreadyRunning = errors.New("job already running")

const (
	autoCatchUpMaxCycles       = 25
	autoCatchUpNoProgressLimit = 2
)

type phase2BacklogSnapshot struct {
	Missing       int
	TitleBackfill int
	FailedPartial int
	Total         int
}

func (b phase2BacklogSnapshot) summary() string {
	return fmt.Sprintf("remaining backlog=%d (missing=%d, title_backfill=%d, failed_partial=%d)", b.Total, b.Missing, b.TitleBackfill, b.FailedPartial)
}

func autoCatchUpProgressError(streak, limit int, backlog phase2BacklogSnapshot) error {
	if streak < limit {
		return nil
	}
	return fmt.Errorf("auto catch-up stopped: no backlog progress after %d consecutive cycle(s); %s", streak, backlog.summary())
}

// Runner manages background job execution with tracking and deduplication.
type Runner struct {
	store       store.Store
	hub         *sse.Hub
	invalidator ManifestCacheInvalidator

	mu             sync.Mutex
	wg             sync.WaitGroup
	running        map[string]bool               // job name -> is running
	cancelFuncs    map[string]context.CancelFunc // job name -> cancel
	jobRunIDs      map[string]string             // job name -> job_runs id
	pcgwSyncRunID  string
	progressPages  int    // pages processed by current pcgw_sync run (when running)
	progressPhase  string // current phase: "catalog" or "ingest"
	progressQueue  int    // queue size for current run
	progressCursor int    // queue cursor for current run
	progressMode   string // current mode: "single-run" or "auto-catch-up"
}

// NewRunner creates a Runner. hub may be nil if SSE is not needed.
// invalidator may be nil; when set, manifest cache is cleared after successful PCGW sync.
func NewRunner(st store.Store, hub *sse.Hub, invalidator ManifestCacheInvalidator) *Runner {
	return &Runner{
		store:       st,
		hub:         hub,
		invalidator: invalidator,
		running:     make(map[string]bool),
		cancelFuncs: make(map[string]context.CancelFunc),
		jobRunIDs:   make(map[string]string),
	}
}

// IsRunning reports whether a job is currently executing.
func (r *Runner) IsRunning(jobName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[jobName]
}

// ProgressPages returns the number of pages processed so far by the current pcgw_sync run (0 if not running).
func (r *Runner) ProgressPages(jobName string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if jobName != "pcgw_sync" {
		return 0
	}
	return r.progressPages
}

// ProgressPhase returns the current sync phase ("catalog" or "ingest") for the running pcgw_sync.
func (r *Runner) ProgressPhase() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.progressPhase
}

// ProgressMode returns the currently running PCGW sync mode ("single-run" or "auto-catch-up").
func (r *Runner) ProgressMode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.progressMode
}

// RunPCGWSyncCatalogOnly runs a catalog-scan-only sync (Phase 1, no ingest).
func (r *Runner) RunPCGWSyncCatalogOnly(ctx context.Context) (bool, error) {
	return r.tryRunPCGWSync(ctx, PCGWSyncOptions{SkipIngestPhase: true})
}

// RunPCGWSyncRetryFailed runs a sync that only processes failed/partial pages.
func (r *Runner) RunPCGWSyncRetryFailed(ctx context.Context) (bool, error) {
	return r.tryRunPCGWSync(ctx, PCGWSyncOptions{RetryFailedOnly: true})
}

// RunPCGWSyncRebuildManifest bumps the manifest without fetching any pages.
func (r *Runner) RunPCGWSyncRebuildManifest(ctx context.Context) (bool, error) {
	return r.tryRunPCGWSync(ctx, PCGWSyncOptions{RebuildManifestOnly: true})
}

// TryRunPCGWSync starts an incremental PCGW sync if none is running.
func (r *Runner) TryRunPCGWSync(ctx context.Context) (bool, error) {
	return r.tryRunPCGWSync(ctx, PCGWSyncOptions{})
}

// RunPCGWSync starts an incremental PCGW sync in the background.
func (r *Runner) RunPCGWSync(ctx context.Context) (bool, error) {
	return r.tryRunPCGWSync(ctx, PCGWSyncOptions{})
}

// RunPCGWSyncFull starts a full PCGW resync in the background (no resume).
func (r *Runner) RunPCGWSyncFull(ctx context.Context) (bool, error) {
	return r.tryRunPCGWSync(ctx, PCGWSyncOptions{Full: true, ForceFull: true})
}

// RunPCGWSyncAutoCatchUp runs repeated budgeted sync cycles until Phase 2 backlog reaches zero.
func (r *Runner) RunPCGWSyncAutoCatchUp(ctx context.Context) (bool, error) {
	return r.tryRunPCGWSync(ctx, PCGWSyncOptions{AutoCatchUp: true})
}

// CancelAll cancels every in-flight job. Used during graceful shutdown.
func (r *Runner) CancelAll() {
	r.mu.Lock()
	for _, cancel := range r.cancelFuncs {
		cancel()
	}
	r.mu.Unlock()
}

// WaitJobs blocks until all running job goroutines have exited or ctx is done.
func (r *Runner) WaitJobs(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CancelPCGWSync cancels the in-flight PCGW sync. Returns true if a cancel was sent.
func (r *Runner) CancelPCGWSync(ctx context.Context) bool {
	r.mu.Lock()
	cancel, ok := r.cancelFuncs["pcgw_sync"]
	r.mu.Unlock()
	if !ok || cancel == nil {
		return false
	}
	cancel()
	return true
}

func (r *Runner) tryRunPCGWSync(ctx context.Context, opts PCGWSyncOptions) (bool, error) {
	const jobName = "pcgw_sync"
	r.mu.Lock()
	if r.running[jobName] {
		r.mu.Unlock()
		return false, ErrJobAlreadyRunning
	}
	r.mu.Unlock()

	if r.store.HasRunningPCGWSync(ctx) || r.store.HasRunningJob(ctx, jobName) {
		return false, ErrJobAlreadyRunning
	}

	r.mu.Lock()
	if r.running[jobName] {
		r.mu.Unlock()
		return false, ErrJobAlreadyRunning
	}
	r.running[jobName] = true
	r.mu.Unlock()

	r.wg.Add(1)
	go r.runPCGWSync(ctx, jobName, opts)
	return true, nil
}

func (r *Runner) runPCGWSync(parentCtx context.Context, jobName string, opts PCGWSyncOptions) {
	defer r.wg.Done()
	defer func() {
		r.mu.Lock()
		r.running[jobName] = false
		delete(r.cancelFuncs, jobName)
		delete(r.jobRunIDs, jobName)
		r.pcgwSyncRunID = ""
		r.progressPages = 0
		r.progressPhase = ""
		r.progressQueue = 0
		r.progressCursor = 0
		r.progressMode = ""
		r.mu.Unlock()
	}()

	log.Printf("job: %s started (full=%v force_full=%v)", jobName, opts.Full, opts.ForceFull)
	timeoutCtx, timeoutCancel := context.WithTimeout(parentCtx, 24*time.Hour)
	defer timeoutCancel()
	jobCtx, cancel := context.WithCancel(timeoutCtx)

	r.mu.Lock()
	r.cancelFuncs[jobName] = cancel
	if opts.AutoCatchUp {
		r.progressMode = "auto-catch-up"
	} else {
		r.progressMode = "single-run"
	}
	r.mu.Unlock()
	defer cancel()

	runID, err := r.store.LogJobStart(jobCtx, jobName)
	if err != nil {
		log.Printf("job runner: log start: %v", err)
	} else {
		r.mu.Lock()
		r.jobRunIDs[jobName] = runID
		r.mu.Unlock()
	}

	syncOpts := opts
	mode := "incremental"
	if opts.Full {
		mode = "full"
	}
	if !opts.ForceFull && !opts.Full && !opts.RetryFailedOnly && !opts.SkipIngestPhase && !opts.RebuildManifestOnly {
		if resumable, err := r.store.GetResumablePCGWSyncRun(jobCtx, mode); err != nil {
			log.Printf("job runner: get resumable pcgw sync: %v", err)
		} else if resumable != nil {
			notes := fmt.Sprintf("resumed from %s", resumable.ID)
			if resumable.CheckpointPhase == "ingest" {
				notes = fmt.Sprintf("resumed from %s at ingest cursor %d", resumable.ID, resumable.CheckpointQueueCursor)
			} else if resumable.CheckpointOffset > 0 {
				notes = fmt.Sprintf("resumed from %s at offset %d", resumable.ID, resumable.CheckpointOffset)
			}
			syncRunID, err := r.store.StartPCGWSyncRunWithResume(jobCtx, mode, resumable.ID, notes)
			if err != nil {
				log.Printf("job runner: start resume pcgw sync run: %v", err)
			} else {
				syncOpts.SyncRunID = syncRunID
				syncOpts.SkipStartRun = true
				syncOpts.ResumedFromRunID = resumable.ID
				syncOpts.Notes = notes
				syncOpts.Offset = resumable.CheckpointOffset
				// Two-phase resume: if checkpoint_phase is "ingest", skip catalog scan.
				if resumable.CheckpointPhase == "ingest" {
					syncOpts.ResumeCatalogScan = true
					syncOpts.ResumeQueueCursor = resumable.CheckpointQueueCursor
				}
				r.mu.Lock()
				r.pcgwSyncRunID = syncRunID
				r.mu.Unlock()
				log.Printf("job runner: resuming pcgw sync from run %s (phase=%q cursor=%d)",
					resumable.ID, resumable.CheckpointPhase, resumable.CheckpointQueueCursor)
			}
		}
	}

	pcgwClient := pcgw.NewClient()
	progressFn := func(pages int) {
		r.mu.Lock()
		prev := r.progressPages
		r.progressPages = pages
		r.mu.Unlock()
		if r.hub != nil && pages > 0 && pages%5 == 0 && pages != prev {
			r.hub.Broadcast(sse.Event{Type: "job-progress", Data: fmt.Sprintf(`{"job":"pcgw_sync","pages":%d}`, pages)})
		}
	}
	var lastPages int
	var lastReport time.Time
	var avgPerPage time.Duration
	reportEx := func(p PCGWSyncProgress) {
		r.mu.Lock()
		r.progressPages = p.PagesProcessed
		r.progressPhase = p.Phase
		r.progressQueue = p.QueueSize
		r.progressCursor = p.QueueCursor
		mode := r.progressMode
		r.mu.Unlock()
		if r.hub != nil {
			now := time.Now()
			if lastReport.IsZero() {
				lastReport = now
				lastPages = p.PagesProcessed
			} else if p.PagesProcessed > lastPages {
				delta := p.PagesProcessed - lastPages
				sample := now.Sub(lastReport) / time.Duration(delta)
				if avgPerPage == 0 {
					avgPerPage = sample
				} else {
					avgPerPage = (avgPerPage*3 + sample) / 4
				}
				lastPages = p.PagesProcessed
				lastReport = now
			}
			eta := 0
			if avgPerPage > 0 && p.TotalEstimate > p.PagesProcessed {
				eta = int(avgPerPage.Seconds() * float64(p.TotalEstimate-p.PagesProcessed))
			}
			p.ETASeconds = eta
			r.hub.Broadcast(sse.Event{Type: "job-progress", Data: fmt.Sprintf(
				`{"job":"pcgw_sync","pages":%d,"total":%d,"phase":%q,"games_skipped":%d,"eta_seconds":%d,"queue_size":%d,"queue_cursor":%d,"mode":%q}`,
				p.PagesProcessed, p.TotalEstimate, p.Phase, p.GamesSkipped, p.ETASeconds, p.QueueSize, p.QueueCursor, mode)})
		}
	}
	runCycle := func(cycleOpts PCGWSyncOptions) (int, error) {
		return PCGWSyncEx(jobCtx, r.store, pcgwClient, progressFn, reportEx, cycleOpts)
	}
	var (
		count   int
		syncErr error
	)
	if opts.AutoCatchUp {
		count, syncErr = r.runPCGWSyncAutoCatchUp(jobCtx, syncOpts, runCycle)
	} else {
		count, syncErr = runCycle(syncOpts)
	}

	status := JobSuccess
	errMsg := ""
	if syncErr != nil {
		if errors.Is(syncErr, context.Canceled) {
			status = JobCanceled
		} else {
			status = JobFailed
			errMsg = syncErr.Error()
			log.Printf("job runner: pcgw_sync failed: %v", syncErr)
		}
	} else {
		log.Printf("job runner: pcgw_sync success (%d entries)", count)
	}

	if runID != "" {
		if err := r.store.LogJobFinish(jobCtx, runID, status, errMsg, count); err != nil {
			log.Printf("job runner: log finish: %v", err)
		}
	}

	if r.hub != nil {
		r.hub.Broadcast(sse.Event{Type: "job-finished", Data: `{"job":"pcgw_sync","status":"` + status + `"}`})
		if status == JobSuccess || status == "partial" {
			LogSyncComplete(r.invalidator, count)
			r.hub.Broadcast(sse.Event{Type: "manifest-updated", Data: "{}"})
		}
	}
}

func (r *Runner) runPCGWSyncAutoCatchUp(ctx context.Context, baseOpts PCGWSyncOptions, runCycle func(PCGWSyncOptions) (int, error)) (int, error) {
	totalEntries := 0
	backlog, err := r.currentPhase2Backlog(ctx)
	if err != nil {
		return totalEntries, fmt.Errorf("auto catch-up failed to check initial backlog: %w", err)
	}
	if backlog.Total == 0 {
		return totalEntries, nil
	}

	noProgressStreak := 0
	for cycle := 1; cycle <= autoCatchUpMaxCycles; cycle++ {
		select {
		case <-ctx.Done():
			return totalEntries, ctx.Err()
		default:
		}

		cycleEntries, cycleErr := runCycle(baseOpts)
		totalEntries += cycleEntries
		if cycleErr != nil {
			return totalEntries, fmt.Errorf("auto catch-up cycle %d failed: %w", cycle, cycleErr)
		}

		nextBacklog, err := r.currentPhase2Backlog(ctx)
		if err != nil {
			return totalEntries, fmt.Errorf("auto catch-up cycle %d: backlog recheck failed: %w", cycle, err)
		}
		if nextBacklog.Total == 0 {
			return totalEntries, nil
		}
		if nextBacklog.Total < backlog.Total {
			noProgressStreak = 0
		} else {
			noProgressStreak++
			if progressErr := autoCatchUpProgressError(noProgressStreak, autoCatchUpNoProgressLimit, nextBacklog); progressErr != nil {
				return totalEntries, progressErr
			}
		}
		backlog = nextBacklog
	}
	latestBacklog, err := r.currentPhase2Backlog(ctx)
	if err != nil {
		return totalEntries, fmt.Errorf("auto catch-up stopped after %d cycles (safety limit reached): backlog recheck failed: %w", autoCatchUpMaxCycles, err)
	}
	return totalEntries, fmt.Errorf("auto catch-up stopped after %d cycles (safety limit reached); %s", autoCatchUpMaxCycles, latestBacklog.summary())
}

func (r *Runner) currentPhase2Backlog(ctx context.Context) (phase2BacklogSnapshot, error) {
	backlog := phase2BacklogSnapshot{}
	missing, err := r.store.ListPCGWCatalogMissing(ctx, 0, 0)
	if err != nil {
		return backlog, err
	}
	failedPartial, err := r.store.ListPCGWCatalogFailedPartial(ctx, 0, 0)
	if err != nil {
		return backlog, err
	}
	titleBackfill, err := r.store.ListPCGWCatalogTitleBackfill(ctx, 0, 0)
	if err != nil {
		return backlog, err
	}

	backlog.Missing = len(missing)
	backlog.FailedPartial = len(failedPartial)
	backlog.TitleBackfill = len(titleBackfill)
	unique := make(map[int64]struct{}, len(missing)+len(failedPartial)+len(titleBackfill))
	for _, id := range missing {
		unique[id] = struct{}{}
	}
	for _, id := range failedPartial {
		unique[id] = struct{}{}
	}
	for _, row := range titleBackfill {
		unique[row.PageID] = struct{}{}
	}
	backlog.Total = len(unique)
	return backlog, nil
}

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

// Runner manages background job execution with tracking and deduplication.
type Runner struct {
	store       store.Store
	hub         *sse.Hub
	invalidator ManifestCacheInvalidator

	mu            sync.Mutex
	running       map[string]bool               // job name -> is running
	cancelFuncs   map[string]context.CancelFunc // job name -> cancel
	jobRunIDs     map[string]string             // job name -> job_runs id
	pcgwSyncRunID string
	progressPages int // pages processed by current pcgw_sync run (when running)
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

	go r.runPCGWSync(ctx, jobName, opts)
	return true, nil
}

func (r *Runner) runPCGWSync(parentCtx context.Context, jobName string, opts PCGWSyncOptions) {
	defer func() {
		r.mu.Lock()
		r.running[jobName] = false
		delete(r.cancelFuncs, jobName)
		delete(r.jobRunIDs, jobName)
		r.pcgwSyncRunID = ""
		r.progressPages = 0
		r.mu.Unlock()
	}()

	log.Printf("job: %s started (full=%v force_full=%v)", jobName, opts.Full, opts.ForceFull)
	timeoutCtx, timeoutCancel := context.WithTimeout(parentCtx, 24*time.Hour)
	defer timeoutCancel()
	jobCtx, cancel := context.WithCancel(timeoutCtx)

	r.mu.Lock()
	r.cancelFuncs[jobName] = cancel
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
	if !opts.ForceFull && !opts.Full {
		if resumable, err := r.store.GetResumablePCGWSyncRun(jobCtx, mode); err != nil {
			log.Printf("job runner: get resumable pcgw sync: %v", err)
		} else if resumable != nil {
			notes := fmt.Sprintf("resumed from %s at offset %d", resumable.ID, resumable.CheckpointOffset)
			syncRunID, err := r.store.StartPCGWSyncRunWithResume(jobCtx, mode, resumable.ID, notes)
			if err != nil {
				log.Printf("job runner: start resume pcgw sync run: %v", err)
			} else {
				syncOpts.SyncRunID = syncRunID
				syncOpts.SkipStartRun = true
				syncOpts.ResumedFromRunID = resumable.ID
				syncOpts.Notes = notes
				syncOpts.Offset = resumable.CheckpointOffset
				r.mu.Lock()
				r.pcgwSyncRunID = syncRunID
				r.mu.Unlock()
				log.Printf("job runner: resuming pcgw sync from run %s at offset %d", resumable.ID, resumable.CheckpointOffset)
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
				`{"job":"pcgw_sync","pages":%d,"total":%d,"phase":%q,"games_skipped":%d,"eta_seconds":%d}`,
				p.PagesProcessed, p.TotalEstimate, p.Phase, p.GamesSkipped, eta)})
		}
	}
	count, syncErr := PCGWSyncEx(jobCtx, r.store, pcgwClient, progressFn, reportEx, syncOpts)

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

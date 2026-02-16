package job

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
)

// Runner manages background job execution with tracking and deduplication.
type Runner struct {
	store store.Store
	hub   *sse.Hub

	mu             sync.Mutex
	running        map[string]bool // job name -> is running
	progressPages  int             // pages processed by current pcgw_sync run (when running)
}

// NewRunner creates a Runner. hub may be nil if SSE is not needed.
func NewRunner(st store.Store, hub *sse.Hub) *Runner {
	return &Runner{
		store:   st,
		hub:     hub,
		running: make(map[string]bool),
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

// RunPCGWSync runs the PCGW sync job with tracking and dedup.
// Returns immediately with false if the job is already running.
// Otherwise starts the job in the background and returns true.
func (r *Runner) RunPCGWSync(ctx context.Context) bool {
	const jobName = "pcgw_sync"
	r.mu.Lock()
	if r.running[jobName] {
		r.mu.Unlock()
		return false
	}
	r.running[jobName] = true
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			r.running[jobName] = false
			r.progressPages = 0
			r.mu.Unlock()
		}()

		log.Printf("job: %s started", jobName)
		jobCtx, cancel := context.WithTimeout(ctx, 24*time.Hour)
		defer cancel()

		runID, err := r.store.LogJobStart(jobCtx, jobName)
		if err != nil {
			log.Printf("job runner: log start: %v", err)
		}

		pcgwClient := pcgw.NewClient()
		progressFn := func(pages int) {
			r.mu.Lock()
			r.progressPages = pages
			r.mu.Unlock()
		}
		count, syncErr := PCGWSync(jobCtx, r.store, pcgwClient, progressFn)

		status := "success"
		errMsg := ""
		if syncErr != nil {
			status = "failed"
			errMsg = syncErr.Error()
			log.Printf("job runner: pcgw_sync failed: %v", syncErr)
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
			// Only notify clients of manifest update on success; failed syncs
			// leave the manifest unchanged and would cause unnecessary re-fetches.
			if status == "success" {
				r.hub.Broadcast(sse.Event{Type: "manifest-updated", Data: "{}"})
			}
		}
	}()
	return true
}

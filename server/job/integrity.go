package job

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/sse"
)

const integrityJobName = "integrity_check"

// TryRunIntegrityCheck starts a blob-integrity verification if none is
// running. The job re-hashes stored unencrypted saves against their recorded
// content hash and records findings for the admin overview.
func (r *Runner) TryRunIntegrityCheck(ctx context.Context) (bool, error) {
	r.mu.Lock()
	if r.running[integrityJobName] {
		r.mu.Unlock()
		return false, ErrJobAlreadyRunning
	}
	r.mu.Unlock()

	if r.store.HasRunningJob(ctx, integrityJobName) {
		return false, ErrJobAlreadyRunning
	}

	r.mu.Lock()
	if r.running[integrityJobName] {
		r.mu.Unlock()
		return false, ErrJobAlreadyRunning
	}
	r.running[integrityJobName] = true
	r.mu.Unlock()

	r.wg.Add(1)
	go r.runIntegrityCheck(ctx)
	return true, nil
}

func (r *Runner) runIntegrityCheck(parentCtx context.Context) {
	defer r.wg.Done()
	defer func() {
		r.mu.Lock()
		r.running[integrityJobName] = false
		delete(r.cancelFuncs, integrityJobName)
		delete(r.jobRunIDs, integrityJobName)
		r.mu.Unlock()
	}()

	logx.Logger().Info().Str("component", "job").Str("job", integrityJobName).Msg("job: started")
	jobCtx, cancel := context.WithTimeout(parentCtx, 2*time.Hour)
	defer cancel()

	r.mu.Lock()
	r.cancelFuncs[integrityJobName] = cancel
	r.mu.Unlock()

	runID, err := r.store.LogJobStart(jobCtx, integrityJobName)
	if err != nil {
		logx.Logger().Error().Str("component", "job").Str("job", integrityJobName).Err(err).Msg("job runner: log start")
	} else {
		r.mu.Lock()
		r.jobRunIDs[integrityJobName] = runID
		r.mu.Unlock()
	}

	result, checkErr := r.store.RunIntegrityCheck(jobCtx)

	status := JobSuccess
	errMsg := ""
	switch {
	case checkErr != nil && errors.Is(checkErr, context.Canceled):
		status = JobCanceled
	case checkErr != nil:
		status = JobFailed
		errMsg = checkErr.Error()
		logx.Logger().Error().Str("component", "job").Err(checkErr).Msg("job runner: integrity_check failed")
	default:
		if result.Problems() > 0 {
			errMsg = fmt.Sprintf("%d problem slot(s): %d hash mismatch, %d missing file, %d unreadable",
				result.Problems(), result.Mismatched, result.MissingFile, result.Unreadable)
		}
		logx.Logger().Info().Str("component", "job").
			Int("checked", result.Checked).Int("skipped_encrypted", result.SkippedEncrypted).
			Int("mismatched", result.Mismatched).Int("missing_file", result.MissingFile).
			Int("unreadable", result.Unreadable).
			Msg("job runner: integrity_check finished")
	}

	if runID != "" {
		if err := r.store.LogJobFinish(jobCtx, runID, status, errMsg, result.Checked); err != nil {
			logx.Logger().Error().Str("component", "job").Str("job", integrityJobName).Err(err).Msg("job runner: log finish")
		}
	}
	if r.hub != nil {
		r.hub.Broadcast(sse.Event{Type: "job-finished", Data: `{"job":"` + integrityJobName + `","status":"` + status + `"}`})
	}
}

package job

// Job run status values stored in job_runs and pcgw_sync_runs.
const (
	JobRunning     = "running"
	JobSuccess     = "success"
	JobFailed      = "failed"
	JobCanceled    = "canceled"
	JobInterrupted = "interrupted"
)

package sync

// SaveDirection indicates push or pull for tray events.
type SaveDirection string

const (
	SaveDirPush SaveDirection = "push"
	SaveDirPull SaveDirection = "pull"
)

// OnSaveEvent is called after a successful push/pull or on outbox enqueue.
// gameTitle may be empty; tray resolves display names from manifest/discovery.
var OnSaveEvent func(gameID, pathKey, gameTitle string, direction SaveDirection, err error)

// OnPullProgress is called during summary-based pull (current, total saves considered).
var OnPullProgress func(current, total int)

// OnOutboxEnqueued is called when a failed push is queued for later retry.
var OnOutboxEnqueued func(gameID, pathKey string)

// OnQuotaError is called when push is rejected with HTTP 413 (size or storage quota).
var OnQuotaError func(msg string)

// OnAuthError is called when push or pull is rejected with HTTP 401 after token reload fails.
var OnAuthError func(msg string)

// OnPushError is called when a push fails with a non-retryable error (excluding quota/auth).
var OnPushError func(gameID, pathKey, msg string)

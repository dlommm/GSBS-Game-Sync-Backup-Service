package main

import (
	"errors"

	clientsync "github.com/gsbs/gsbs/client/sync"
)

func wireSyncTrayHooks() {
	clientsync.OnSaveEvent = func(gameID, pathKey, gameTitle string, direction clientsync.SaveDirection, err error) {
		dir := SaveDirPull
		if direction == clientsync.SaveDirPush {
			dir = SaveDirPush
			if err == nil {
				notifyPushDebounced(gameID)
			}
		}
		if gameTitle != "" {
			cacheGameTitle(gameID, gameTitle)
		}
		RecordSaveEvent(gameID, pathKey, dir, err)
	}
	clientsync.OnPullProgress = UpdateSyncProgress
	clientsync.OnOutboxEnqueued = RecordPendingUpload
	clientsync.OnQuotaError = notifyQuotaError
	clientsync.OnAuthError = notifyAuthError
	clientsync.OnPushError = func(gameID, pathKey, msg string) {
		notifyPushError(gameID, pathKey, msg)
		// Persist the failure on the game row: without this a device whose
		// pushes always fail still shows green everywhere after the one-shot
		// toast disappears.
		RecordSaveEvent(gameID, pathKey, SaveDirPush, errors.New(msg))
	}
}

func cacheGameTitle(gameID, title string) {
	if gameID == "" || title == "" {
		return
	}
	globalTrayState.mu.Lock()
	globalTrayState.titleCache[gameID] = title
	if row, ok := globalTrayState.games[gameID]; ok {
		row.Title = title
	}
	globalTrayState.mu.Unlock()
}

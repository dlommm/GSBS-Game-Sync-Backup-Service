package main

import (
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

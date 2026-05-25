package job

import (
	"context"
	"log"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/store"
)

// PCGWSyncPage syncs a single game page by ID.
func PCGWSyncPage(ctx context.Context, st store.Store, client *pcgw.Client, pageID int64) (int, error) {
	return PCGWSync(ctx, st, client, nil, PCGWSyncOptions{SinglePage: pageID})
}

// Legacy wrapper for runner compatibility.
func PCGWSyncLegacy(ctx context.Context, st store.Store, client *pcgw.Client, reportProgress ReportProgress) (int, error) {
	return PCGWSync(ctx, st, client, reportProgress, PCGWSyncOptions{})
}

// PCGWSyncFull runs a full resync (no incremental skip).
func PCGWSyncFull(ctx context.Context, st store.Store, client *pcgw.Client, reportProgress ReportProgress, reportEx ReportProgressEx) (int, error) {
	return PCGWSyncEx(ctx, st, client, reportProgress, reportEx, PCGWSyncOptions{Full: true})
}

// OnSyncComplete should be called after successful sync to invalidate API manifest cache.
type ManifestCacheInvalidator interface {
	InvalidateManifestCache()
}

func LogSyncComplete(invalidator ManifestCacheInvalidator, count int) {
	if invalidator != nil {
		invalidator.InvalidateManifestCache()
		log.Printf("pcgw sync: manifest cache invalidated")
	}
}

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

package job

import (
	"context"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/logx"
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
		logx.Logger().Info().Str("component", "pcgw").Msg("pcgw sync: manifest cache invalidated")
	}
}

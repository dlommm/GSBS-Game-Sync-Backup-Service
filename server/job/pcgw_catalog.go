package job

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

const catalogChunkSize = 100

// DefaultMaxPagesPerRun is the default budget for Phase 2 ingest per run.
// Override with GSBS_PCGW_MAX_PAGES_PER_RUN.
const DefaultMaxPagesPerRun = 5000

const (
	MaxPagesPerRunSourceDefault    = "default"
	MaxPagesPerRunSourceEnv        = "GSBS_PCGW_MAX_PAGES_PER_RUN"
	MaxPagesPerRunSourceInvalidEnv = "default (invalid GSBS_PCGW_MAX_PAGES_PER_RUN)"
)

// MaxPagesPerRun reads the budget from GSBS_PCGW_MAX_PAGES_PER_RUN or falls back to the default.
func MaxPagesPerRun() int {
	n, _ := MaxPagesPerRunWithSource()
	return n
}

// MaxPagesPerRunWithSource returns the ingest budget and the source string used by UI/status.
func MaxPagesPerRunWithSource() (int, string) {
	if raw, ok := os.LookupEnv(MaxPagesPerRunSourceEnv); ok {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return DefaultMaxPagesPerRun, MaxPagesPerRunSourceDefault
		}
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n, MaxPagesPerRunSourceEnv
		}
		return DefaultMaxPagesPerRun, MaxPagesPerRunSourceInvalidEnv
	}
	return DefaultMaxPagesPerRun, MaxPagesPerRunSourceDefault
}

// RunCatalogScan performs Phase 1: enumerate all PCGW game IDs via ListGamePages,
// upsert them into pcgw_catalog, and compute the catalog hash.
// It returns Phase1Stats and updates the sync run row in the store.
func RunCatalogScan(ctx context.Context, st store.Store, client *pcgw.Client, runID string, reportEx ReportProgressEx) (types.Phase1Stats, error) {
	var stats types.Phase1Stats
	offset := 0

	for {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		pages, err := client.ListGamePages(ctx, catalogChunkSize, offset)
		if err != nil {
			return stats, err
		}
		if len(pages) == 0 {
			break
		}

		// Convert to catalog entries.
		now := time.Now().UTC().Format(time.RFC3339)
		entries := make([]types.PCGWCatalogEntry, 0, len(pages))
		for _, p := range pages {
			entries = append(entries, types.PCGWCatalogEntry{
				PageID:        p.PageID,
				Title:         p.Title,
				FirstSeenAt:   now,
				LastSeenAt:    now,
				LastSeenRunID: runID,
			})
		}
		if err := st.UpsertPCGWCatalogBatch(ctx, entries); err != nil {
			logx.Logger().Error().Str("component", "pcgw").Int("offset", offset).Err(err).
				Msg("catalog scan: upsert batch")
			return stats, err
		}

		stats.RemoteTotalIDs = offset + len(pages)
		if reportEx != nil {
			reportEx(PCGWSyncProgress{
				PagesProcessed: stats.RemoteTotalIDs,
				Phase:          "catalog",
			})
		}

		if len(pages) < catalogChunkSize {
			break
		}
		offset += len(pages)
	}

	// Compute catalog hash and stats.
	hash, err := st.ComputeCatalogHash(ctx)
	if err != nil {
		logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("catalog scan: compute hash")
	}
	stats.CatalogHash = hash
	stats.CompletedAt = time.Now().UTC().Format(time.RFC3339)

	// Compute missing/extra counts.
	catStats, err := st.GetPCGWCatalogStats(ctx)
	if err != nil {
		logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("catalog scan: get stats")
	} else {
		stats.MissingLocalIDs = catStats.MissingLocal
		stats.ExtraLocalIDs = catStats.ExtraLocal
	}

	if runID != "" {
		if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, stats); err != nil {
			logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("catalog scan: update phase1 stats")
		}
	}

	logx.Logger().Info().Str("component", "pcgw").
		Int("remote", stats.RemoteTotalIDs).Int("missing", stats.MissingLocalIDs).
		Int("extra", stats.ExtraLocalIDs).Str("hash", truncateHash(stats.CatalogHash)).
		Msg("catalog scan: done")
	return stats, nil
}

func truncateHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

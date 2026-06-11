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
		if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, stats, "full"); err != nil {
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

// ProbeCatalogGrowth checks whether the remote PCGW catalog has grown beyond localCount.
// It makes a single Cargo API call (limit=1, offset=localCount).
// Returns true if new pages exist beyond the current local count.
func ProbeCatalogGrowth(ctx context.Context, client *pcgw.Client, localCount int) (bool, error) {
	pages, err := client.ListGamePages(ctx, 1, localCount)
	if err != nil {
		return false, err
	}
	return len(pages) > 0, nil
}

// ScanCatalogTail paginates from startOffset, upserts new entries, and returns updated Phase1Stats.
// Unlike RunCatalogScan it starts partway through the catalog — use for tail-only growth.
// It uses chunk size 500 (Cargo max) to minimise round trips.
func ScanCatalogTail(ctx context.Context, st store.Store, client *pcgw.Client, runID string, startOffset int, cachedTotal int, reportEx ReportProgressEx) (types.Phase1Stats, error) {
	const tailChunkSize = 500

	stats := types.Phase1Stats{
		RemoteTotalIDs: cachedTotal,
	}

	newCount := 0
	offset := startOffset

	for {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		pages, err := client.ListGamePages(ctx, tailChunkSize, offset)
		if err != nil {
			return stats, err
		}
		if len(pages) == 0 {
			break
		}

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
				Msg("catalog tail scan: upsert batch")
			return stats, err
		}

		newCount += len(pages)
		stats.RemoteTotalIDs = cachedTotal + newCount

		if reportEx != nil {
			reportEx(PCGWSyncProgress{
				PagesProcessed: stats.RemoteTotalIDs,
				TotalEstimate:  stats.RemoteTotalIDs,
				Phase:          "catalog",
			})
		}

		if len(pages) < tailChunkSize {
			break
		}
		offset += len(pages)
	}

	if newCount == 0 {
		// Probe was wrong / race — return cached stats unchanged.
		return stats, nil
	}

	// Recompute hash after tail growth.
	hash, err := st.ComputeCatalogHash(ctx)
	if err != nil {
		logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("catalog tail scan: compute hash")
	}
	stats.CatalogHash = hash
	stats.CompletedAt = time.Now().UTC().Format(time.RFC3339)

	// Recompute missing/extra counts.
	catStats, err := st.GetPCGWCatalogStats(ctx)
	if err != nil {
		logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("catalog tail scan: get stats")
	} else {
		stats.MissingLocalIDs = catStats.MissingLocal
		stats.ExtraLocalIDs = catStats.ExtraLocal
	}

	if runID != "" {
		if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, stats, "tail"); err != nil {
			logx.Logger().Error().Str("component", "pcgw").Err(err).Msg("catalog tail scan: update phase1 stats")
		}
	}

	logx.Logger().Info().Str("component", "pcgw").
		Int("new_count", newCount).Int("total", stats.RemoteTotalIDs).
		Str("hash", truncateHash(stats.CatalogHash)).
		Msg("catalog tail scan: done")
	return stats, nil
}

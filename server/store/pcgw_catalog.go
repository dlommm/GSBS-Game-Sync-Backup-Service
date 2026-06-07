package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
)

const deadLetterThreshold = 5

// UpsertPCGWCatalogBatch upserts a batch of catalog entries. On first seen the
// first_seen_at is set; on every call last_seen_at and last_seen_run_id are updated.
func (s *sqliteStore) UpsertPCGWCatalogBatch(ctx context.Context, entries []types.PCGWCatalogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO pcgw_catalog (page_id, title, first_seen_at, last_seen_at, last_seen_run_id, last_seen_rev_id)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(page_id) DO UPDATE SET
			title=excluded.title,
			last_seen_at=excluded.last_seen_at,
			last_seen_run_id=excluded.last_seen_run_id,
			last_seen_rev_id=excluded.last_seen_rev_id`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		firstSeen := e.FirstSeenAt
		if firstSeen == "" {
			firstSeen = now
		}
		lastSeen := e.LastSeenAt
		if lastSeen == "" {
			lastSeen = now
		}
		if _, err := stmt.ExecContext(ctx, e.PageID, e.Title, firstSeen, lastSeen, e.LastSeenRunID, e.LastSeenRevID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetPCGWCatalogStats returns aggregate counts comparing pcgw_catalog vs pcgw_games.
func (s *sqliteStore) GetPCGWCatalogStats(ctx context.Context) (types.PCGWCatalogStats, error) {
	var st types.PCGWCatalogStats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_catalog`).Scan(&st.RemoteTotal); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_games`).Scan(&st.LocalTotal); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pcgw_catalog c
		WHERE NOT EXISTS (SELECT 1 FROM pcgw_games g WHERE g.page_id = c.page_id)
		  AND c.dead_letter = 0`).Scan(&st.MissingLocal); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pcgw_games g
		WHERE NOT EXISTS (SELECT 1 FROM pcgw_catalog c WHERE c.page_id = g.page_id)`).Scan(&st.ExtraLocal); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_catalog WHERE dead_letter = 1`).Scan(&st.DeadLetter); err != nil {
		return st, err
	}
	return st, nil
}

// ListPCGWCatalogMissing returns page IDs that are in pcgw_catalog but not in pcgw_games (excluding dead-letter).
func (s *sqliteStore) ListPCGWCatalogMissing(ctx context.Context, limit, offset int) ([]int64, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.page_id FROM pcgw_catalog c
		WHERE NOT EXISTS (SELECT 1 FROM pcgw_games g WHERE g.page_id = c.page_id)
		  AND c.dead_letter = 0
		ORDER BY c.page_id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInt64Rows(rows)
}

// ListPCGWCatalogFailedPartial returns page IDs with failed or partial parse status.
func (s *sqliteStore) ListPCGWCatalogFailedPartial(ctx context.Context, limit, offset int) ([]int64, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.page_id FROM pcgw_games g
		JOIN pcgw_catalog c ON c.page_id = g.page_id
		WHERE g.parse_status IN ('failed', 'partial')
		  AND c.dead_letter = 0
		ORDER BY g.page_id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInt64Rows(rows)
}

// ListPCGWCatalogTitleBackfill returns catalog-backed rows where local title/page_name is blank.
// These rows should be re-ingested so title fields are healed without a full resync.
func (s *sqliteStore) ListPCGWCatalogTitleBackfill(ctx context.Context, limit, offset int) ([]types.PCGWCatalogEntry, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.page_id, c.title, c.first_seen_at, c.last_seen_at, c.last_seen_run_id, c.last_seen_rev_id,
			c.dead_letter, c.dead_letter_reason, c.retry_count
		FROM pcgw_catalog c
		JOIN pcgw_games g ON g.page_id = c.page_id
		WHERE c.dead_letter = 0
		  AND TRIM(COALESCE(c.title, '')) != ''
		  AND (
			TRIM(COALESCE(g.title, '')) = ''
			OR TRIM(COALESCE(g.page_name, '')) = ''
		  )
		ORDER BY c.page_id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCatalogEntries(rows)
}

func scanInt64Rows(rows *sql.Rows) ([]int64, error) {
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListPCGWCatalogDeadLetter returns dead-letter entries.
func (s *sqliteStore) ListPCGWCatalogDeadLetter(ctx context.Context, limit int) ([]types.PCGWCatalogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, title, first_seen_at, last_seen_at, last_seen_run_id, last_seen_rev_id,
			dead_letter, dead_letter_reason, retry_count
		FROM pcgw_catalog WHERE dead_letter = 1 ORDER BY page_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCatalogEntries(rows)
}

func scanCatalogEntries(rows *sql.Rows) ([]types.PCGWCatalogEntry, error) {
	var out []types.PCGWCatalogEntry
	for rows.Next() {
		var e types.PCGWCatalogEntry
		var dl int
		if err := rows.Scan(&e.PageID, &e.Title, &e.FirstSeenAt, &e.LastSeenAt,
			&e.LastSeenRunID, &e.LastSeenRevID, &dl, &e.DeadLetterReason, &e.RetryCount); err != nil {
			return nil, err
		}
		e.DeadLetter = dl != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

// IncrementCatalogRetry increments retry_count for a page. Auto-parks in dead_letter after deadLetterThreshold failures.
func (s *sqliteStore) IncrementCatalogRetry(ctx context.Context, pageID int64, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pcgw_catalog (page_id, title, first_seen_at, last_seen_at, retry_count, dead_letter_reason)
		VALUES (?,''  ,?,?,1,?)
		ON CONFLICT(page_id) DO UPDATE SET
			retry_count = retry_count + 1,
			dead_letter_reason = excluded.dead_letter_reason,
			dead_letter = CASE WHEN retry_count + 1 >= ? THEN 1 ELSE dead_letter END,
			last_seen_at = excluded.last_seen_at`,
		pageID, now, now, reason, deadLetterThreshold)
	return err
}

// ClearCatalogDeadLetter resets dead_letter flag and retry_count for a page.
func (s *sqliteStore) ClearCatalogDeadLetter(ctx context.Context, pageID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pcgw_catalog SET dead_letter=0, retry_count=0, dead_letter_reason='' WHERE page_id=?`, pageID)
	return err
}

// ComputeCatalogHash computes a SHA-256 hash of sorted page_id list from pcgw_catalog.
// Two identical sets of page IDs always produce the same hash.
func (s *sqliteStore) ComputeCatalogHash(ctx context.Context) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT page_id FROM pcgw_catalog ORDER BY page_id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	// Ensure sorted (ORDER BY guarantees it, but be defensive).
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	h := sha256.New()
	for _, id := range ids {
		fmt.Fprintf(h, "%d\n", id)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// UpdatePCGWSyncRunPhase1Stats persists Phase 1 output onto an existing sync run row.
func (s *sqliteStore) UpdatePCGWSyncRunPhase1Stats(ctx context.Context, runID string, stats types.Phase1Stats) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE pcgw_sync_runs SET
			remote_total_ids=?, missing_local_ids=?, extra_local_ids=?,
			catalog_hash=?, phase1_completed_at=?,
			checkpoint_phase='ingest'
		WHERE id=?`,
		stats.RemoteTotalIDs, stats.MissingLocalIDs, stats.ExtraLocalIDs,
		stats.CatalogHash, stats.CompletedAt,
		runID)
	return err
}

// UpdatePCGWSyncRunPhase2Progress updates the phase 2 targeted-ingest counters.
func (s *sqliteStore) UpdatePCGWSyncRunPhase2Progress(ctx context.Context, runID string, processed, cursor int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE pcgw_sync_runs SET
			targeted_processed=?,
			checkpoint_queue_cursor=?,
			checkpoint_phase='ingest'
		WHERE id=?`,
		processed, cursor, runID)
	return err
}

// WipePCGWMirrorOnly deletes all PCGW mirror data tables except pcgw_catalog.
// It does NOT delete game_save_locations rows.
func (s *sqliteStore) WipePCGWMirrorOnly(ctx context.Context) error {
	tables := []string{
		"pcgw_game_data",
		"pcgw_availability",
		"pcgw_monetization",
		"pcgw_video",
		"pcgw_input",
		"pcgw_audio",
		"pcgw_network",
		"pcgw_other",
		"pcgw_notes",
		"pcgw_references",
		"pcgw_external_links",
		"pcgw_system_requirements",
		"pcgw_metadata",
		"pcgw_parse_failures",
		"pcgw_games",
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Disable triggers temporarily to avoid manifest_deletions noise.
	if _, err := tx.ExecContext(ctx, `DELETE FROM pcgw_manifest_deletions`); err != nil {
		return fmt.Errorf("wipe: clear manifest_deletions: %w", err)
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return fmt.Errorf("wipe: delete %s: %w", t, err)
		}
	}
	// Also reset pcgw_manifest_meta wikitext size.
	if _, err := tx.ExecContext(ctx, `UPDATE pcgw_manifest_meta SET db_wikitext_bytes=0 WHERE id=1`); err != nil {
		return fmt.Errorf("wipe: reset manifest_meta: %w", err)
	}
	return tx.Commit()
}

// WipePCGWMirrorAndManifest deletes PCGW mirror data AND game_save_locations rows sourced from PCGW.
func (s *sqliteStore) WipePCGWMirrorAndManifest(ctx context.Context) error {
	if err := s.WipePCGWMirrorOnly(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM game_save_locations WHERE source='pcgw'`)
	return err
}

// GetPCGWWipePreflightCounts returns row counts that would be affected by a wipe.
func (s *sqliteStore) GetPCGWWipePreflightCounts(ctx context.Context) (types.WipePreflightCounts, error) {
	var c types.WipePreflightCounts
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_games`).Scan(&c.PCGWGames); err != nil {
		return c, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_game_data`).Scan(&c.PCGWGameData); err != nil {
		return c, err
	}
	// Count all section rows across all section tables.
	sectionTables := []string{
		"pcgw_availability", "pcgw_monetization", "pcgw_video", "pcgw_input",
		"pcgw_audio", "pcgw_network", "pcgw_other", "pcgw_notes", "pcgw_references", "pcgw_external_links",
	}
	var total int
	for _, t := range sectionTables {
		var n int
		_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&n)
		total += n
	}
	c.PCGWSections = total
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_metadata`).Scan(&c.PCGWMetadata); err != nil {
		return c, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_catalog`).Scan(&c.PCGWCatalog); err != nil {
		return c, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_save_locations WHERE source='pcgw'`).Scan(&c.GameSaveLocations); err != nil {
		return c, err
	}
	return c, nil
}

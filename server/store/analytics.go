package store

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) CountActiveClientsSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM clients WHERE COALESCE(last_seen, created_at) >= ?`,
		since.UTC().Format(time.RFC3339)).Scan(&n)
	return n, err
}

func (s *sqliteStore) CountSyncVolume7d(ctx context.Context) (int, error) {
	since := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM saves WHERE updated_at >= ?`, since).Scan(&n)
	return n, err
}

func (s *sqliteStore) CountDistinctManifestGames(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT game_id) FROM game_save_locations`).Scan(&n)
	return n, err
}

func (s *sqliteStore) CountDistinctSaveGames(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT game_id) FROM saves`).Scan(&n)
	return n, err
}

func (s *sqliteStore) CountTotalSaves(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM saves`).Scan(&n)
	return n, err
}

func (s *sqliteStore) ListTopSaveGames(ctx context.Context, limit int) ([]SaveGameStatRow, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.game_id,
			COALESCE(
				(SELECT gsl.game_title FROM game_save_locations gsl WHERE gsl.game_id = s.game_id LIMIT 1),
				s.game_id
			) AS game_title,
			COUNT(*) AS save_count,
			COALESCE(SUM(COALESCE(s.content_size, LENGTH(s.content), 0)), 0) AS storage_bytes
		FROM saves s
		GROUP BY s.game_id
		ORDER BY save_count DESC, storage_bytes DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SaveGameStatRow
	for rows.Next() {
		var row SaveGameStatRow
		if err := rows.Scan(&row.GameID, &row.GameTitle, &row.SaveCount, &row.StorageBytes); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListRecentPCGWParseFailures(ctx context.Context, limit int) ([]PCGWParseFailureRow, error) {
	if limit <= 0 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.page_id, f.sync_run_id, f.section, f.error_message, f.wikitext_snippet, f.created_at,
			COALESCE(g.title, '') AS game_title
		FROM pcgw_parse_failures f
		LEFT JOIN pcgw_games g ON g.page_id = f.page_id
		ORDER BY f.created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PCGWParseFailureRow
	for rows.Next() {
		var row PCGWParseFailureRow
		var runID, snippet sql.NullString
		if err := rows.Scan(
			&row.ID, &row.PageID, &runID, &row.Section, &row.ErrorMessage, &snippet, &row.CreatedAt, &row.GameTitle,
		); err != nil {
			return nil, err
		}
		row.SyncRunID = runID.String
		row.WikitextSnippet = snippet.String
		out = append(out, row)
	}
	return out, rows.Err()
}

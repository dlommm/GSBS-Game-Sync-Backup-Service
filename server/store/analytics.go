package store

import (
	"context"
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

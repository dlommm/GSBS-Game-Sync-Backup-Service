package store

import (
	"context"
	"time"
)

// ConflictRecord is the write-side payload when a push hits a 409 precondition.
type ConflictRecord struct {
	UserID        string
	GameID        string
	PathKey       string
	ClientID      string // device whose push was rejected ("" for unknown)
	Kind          string // "if_hash" | "if_absent" | "legacy_strict"
	IncomingHash  string
	ServerHash    string
	ServerVersion int
}

// ConflictRow is a stored conflict enriched for display (device name, game title).
type ConflictRow struct {
	ID            string
	GameID        string
	GameTitle     string // manifest title; falls back to GameID in the UI
	PathKey       string
	RelativePath  string // display path of the contested save, when known
	ClientID      string
	ClientName    string // "" when the device was revoked/unknown
	Kind          string
	IncomingHash  string
	ServerHash    string
	ServerVersion int
	DetectedAt    string
	Occurrences   int // outbox retries collapse into one open row; this counts them
}

// RecordConflict stores a 409 for the web Conflict Center. Outbox retries
// re-hit the same precondition every couple of minutes, so an existing OPEN
// row for the same slot+device is refreshed (occurrence count bumped) rather
// than duplicated. Returns the row ID.
func (s *sqliteStore) RecordConflict(ctx context.Context, c ConflictRecord) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM conflicts
		 WHERE user_id = ? AND game_id = ? AND path_key = ? AND COALESCE(client_id,'') = ? AND resolved_at IS NULL
		 LIMIT 1`, c.UserID, c.GameID, c.PathKey, c.ClientID).Scan(&id)
	if err == nil {
		_, uerr := s.db.ExecContext(ctx,
			`UPDATE conflicts SET detected_at = ?, kind = ?, incoming_hash = ?, server_hash = ?, server_version = ?,
			        occurrences = occurrences + 1
			 WHERE id = ?`, now, c.Kind, c.IncomingHash, c.ServerHash, c.ServerVersion, id)
		return id, uerr
	}
	id, err = genID()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO conflicts (id, user_id, game_id, path_key, client_id, kind, incoming_hash, server_hash, server_version, detected_at, occurrences)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		id, c.UserID, c.GameID, c.PathKey, c.ClientID, c.Kind, c.IncomingHash, c.ServerHash, c.ServerVersion, now)
	return id, err
}

// ListOpenConflicts returns a user's unresolved conflicts, newest first,
// enriched with device name, manifest title, and the save's display path.
func (s *sqliteStore) ListOpenConflicts(ctx context.Context, userID string) ([]ConflictRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.game_id,
		        COALESCE((SELECT gsl.game_title FROM game_save_locations gsl WHERE gsl.game_id = c.game_id LIMIT 1), ''),
		        c.path_key,
		        COALESCE((SELECT s2.relative_path FROM saves s2 WHERE s2.user_id = c.user_id AND s2.game_id = c.game_id AND s2.path_key = c.path_key), ''),
		        COALESCE(c.client_id, ''),
		        COALESCE((SELECT cl.name FROM clients cl WHERE cl.id = c.client_id), ''),
		        c.kind, COALESCE(c.incoming_hash, ''), COALESCE(c.server_hash, ''), c.server_version,
		        c.detected_at, c.occurrences
		 FROM conflicts c
		 WHERE c.user_id = ? AND c.resolved_at IS NULL
		 ORDER BY c.detected_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConflictRow
	for rows.Next() {
		var c ConflictRow
		if err := rows.Scan(&c.ID, &c.GameID, &c.GameTitle, &c.PathKey, &c.RelativePath,
			&c.ClientID, &c.ClientName, &c.Kind, &c.IncomingHash, &c.ServerHash,
			&c.ServerVersion, &c.DetectedAt, &c.Occurrences); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountOpenConflicts backs the sidebar badge.
func (s *sqliteStore) CountOpenConflicts(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM conflicts WHERE user_id = ? AND resolved_at IS NULL`, userID).Scan(&n)
	return n, err
}

// ResolveConflict marks one conflict resolved (scoped to the owning user).
func (s *sqliteStore) ResolveConflict(ctx context.Context, userID, id, resolution string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE conflicts SET resolved_at = ?, resolution = ? WHERE id = ? AND user_id = ? AND resolved_at IS NULL`,
		now, resolution, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResolveConflictsForSlot closes all open conflicts on a slot — called when a
// successful push lands there (the collision is over; the new content stands).
func (s *sqliteStore) ResolveConflictsForSlot(ctx context.Context, userID, gameID, pathKey, resolution string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE conflicts SET resolved_at = ?, resolution = ?
		 WHERE user_id = ? AND game_id = ? AND path_key = ? AND resolved_at IS NULL`,
		now, resolution, userID, gameID, pathKey)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

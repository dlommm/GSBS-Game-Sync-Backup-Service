package store

import (
	"context"
	"time"
)

// GameSession is one recorded play session (v5.2).
type GameSession struct {
	ID         string
	GameID     string
	ClientID   string
	ClientName string // "" when the device is gone
	StartedAt  string
	EndedAt    string
}

// gameSessionsPerSlotCap bounds stored sessions per (user, game).
const gameSessionsPerSlotCap = 50

// RecordGameSession stores one play session and trims the per-game history.
func (s *sqliteStore) RecordGameSession(ctx context.Context, userID, clientID, gameID, startedAt, endedAt string) (string, error) {
	id, err := genID()
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO game_sessions (id, user_id, client_id, game_id, started_at, ended_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, clientID, gameID, startedAt, endedAt); err != nil {
		return "", err
	}
	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM game_sessions WHERE user_id = ?1 AND game_id = ?2 AND id NOT IN (
			SELECT id FROM game_sessions WHERE user_id = ?1 AND game_id = ?2 ORDER BY ended_at DESC, id DESC LIMIT ?3
		)`, userID, gameID, gameSessionsPerSlotCap)
	return id, nil
}

// ListGameSessions returns a game's newest sessions with device names.
func (s *sqliteStore) ListGameSessions(ctx context.Context, userID, gameID string, limit int) ([]GameSession, error) {
	if limit <= 0 || limit > gameSessionsPerSlotCap {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT gs.id, gs.game_id, COALESCE(gs.client_id, ''),
		        COALESCE((SELECT c.name FROM clients c WHERE c.id = gs.client_id), ''),
		        gs.started_at, gs.ended_at
		 FROM game_sessions gs
		 WHERE gs.user_id = ? AND gs.game_id = ?
		 ORDER BY gs.ended_at DESC, gs.id DESC LIMIT ?`, userID, gameID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GameSession
	for rows.Next() {
		var g GameSession
		if err := rows.Scan(&g.ID, &g.GameID, &g.ClientID, &g.ClientName, &g.StartedAt, &g.EndedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SessionDuration parses a session's bounds; zero on malformed timestamps.
func (g GameSession) Duration() time.Duration {
	start, err1 := time.Parse(time.RFC3339, g.StartedAt)
	end, err2 := time.Parse(time.RFC3339, g.EndedAt)
	if err1 != nil || err2 != nil || end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

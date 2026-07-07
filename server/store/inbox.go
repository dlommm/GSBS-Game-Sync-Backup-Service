package store

import (
	"context"
	"time"
)

// InboxItem is one in-app notification (v5.2). Every notify event lands here
// for the affected user(s), regardless of whether external sinks are set.
type InboxItem struct {
	ID        string
	EventType string
	Title     string
	Body      string
	Link      string // in-app deep link ("" = no link)
	CreatedAt string
	Read      bool
}

// inboxPerUserCap keeps the inbox self-limiting: oldest rows beyond this are
// dropped at write time.
const inboxPerUserCap = 100

// AddInboxItem stores one inbox row for a user and enforces the per-user cap.
func (s *sqliteStore) AddInboxItem(ctx context.Context, userID, eventType, title, body, link string) (string, error) {
	id, err := genID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO inbox_items (id, user_id, event_type, title, body, link, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, userID, eventType, title, body, link, now); err != nil {
		return "", err
	}
	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM inbox_items WHERE user_id = ?1 AND id NOT IN (
			SELECT id FROM inbox_items WHERE user_id = ?1 ORDER BY created_at DESC, id DESC LIMIT ?2
		)`, userID, inboxPerUserCap)
	return id, nil
}

// ListInbox returns a user's newest inbox items (read and unread).
func (s *sqliteStore) ListInbox(ctx context.Context, userID string, limit int) ([]InboxItem, error) {
	if limit <= 0 || limit > inboxPerUserCap {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, title, body, link, created_at, read_at IS NOT NULL
		 FROM inbox_items WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboxItem
	for rows.Next() {
		var it InboxItem
		if err := rows.Scan(&it.ID, &it.EventType, &it.Title, &it.Body, &it.Link, &it.CreatedAt, &it.Read); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// CountUnreadInbox backs the bell badge.
func (s *sqliteStore) CountUnreadInbox(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM inbox_items WHERE user_id = ? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

// MarkInboxRead marks one item ("" or "all" for id marks everything) read.
func (s *sqliteStore) MarkInboxRead(ctx context.Context, userID, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if id == "" || id == "all" {
		_, err := s.db.ExecContext(ctx,
			`UPDATE inbox_items SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, now, userID)
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE inbox_items SET read_at = ? WHERE user_id = ? AND id = ? AND read_at IS NULL`, now, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAdminUserIDs returns the IDs of all enabled admin users — global
// notification events fan out to their inboxes.
func (s *sqliteStore) ListAdminUserIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM users WHERE role = 'admin' AND COALESCE(disabled, 0) = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

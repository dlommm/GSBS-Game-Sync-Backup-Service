package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Admin-settings keys for server-level notification sinks.
const (
	AdminSettingNotifyWebhookURL = "notify_webhook_url"
	AdminSettingNotifyDiscordURL = "notify_discord_url"
	AdminSettingNotifyNtfyURL    = "notify_ntfy_url"
	AdminSettingNotifyEvents     = "notify_events"
	AdminSettingNotifyStaleDays  = "notify_stale_days"
)

// UserNotifySettings is one user's notification delivery configuration.
type UserNotifySettings struct {
	WebhookURL string
	DiscordURL string
	NtfyURL    string
	EventsJSON string // JSON array of enabled event types; empty = all
}

// GetUserNotifySettings returns a user's sinks (zero value when unset).
func (s *sqliteStore) GetUserNotifySettings(ctx context.Context, userID string) (UserNotifySettings, error) {
	var out UserNotifySettings
	err := s.db.QueryRowContext(ctx,
		`SELECT webhook_url, discord_url, ntfy_url, events FROM user_notify_settings WHERE user_id = ?`,
		userID,
	).Scan(&out.WebhookURL, &out.DiscordURL, &out.NtfyURL, &out.EventsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return UserNotifySettings{}, nil
	}
	return out, err
}

// SetUserNotifySettings stores a user's sinks.
func (s *sqliteStore) SetUserNotifySettings(ctx context.Context, userID string, ns UserNotifySettings) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_notify_settings (user_id, webhook_url, discord_url, ntfy_url, events, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			webhook_url = excluded.webhook_url, discord_url = excluded.discord_url,
			ntfy_url = excluded.ntfy_url, events = excluded.events, updated_at = excluded.updated_at`,
		userID, ns.WebhookURL, ns.DiscordURL, ns.NtfyURL, ns.EventsJSON, time.Now().UTC().Format(time.RFC3339))
	return err
}

// StaleClient is a device that has not synced within the alert window.
type StaleClient struct {
	ClientID string
	Name     string
	UserID   string
	Username string
	LastSeen string
}

// ListStaleClientsNeedingAlert returns devices unseen for more than days whose
// stale alert has not fired for the current stale period (stale_notified_at
// is cleared implicitly by being older than last_seen after the device
// reappears).
func (s *sqliteStore) ListStaleClientsNeedingAlert(ctx context.Context, days int) ([]StaleClient, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.name, c.user_id, COALESCE(u.username, ''), COALESCE(c.last_seen, c.created_at)
		FROM clients c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE COALESCE(c.last_seen, c.created_at) < ?
		  AND (c.stale_notified_at IS NULL OR c.stale_notified_at < COALESCE(c.last_seen, c.created_at))`,
		cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StaleClient
	for rows.Next() {
		var c StaleClient
		if err := rows.Scan(&c.ClientID, &c.Name, &c.UserID, &c.Username, &c.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkClientStaleNotified records that the stale alert fired for a device.
func (s *sqliteStore) MarkClientStaleNotified(ctx context.Context, clientID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE clients SET stale_notified_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), clientID)
	return err
}

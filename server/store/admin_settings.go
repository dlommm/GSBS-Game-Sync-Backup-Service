package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Admin setting keys (admin_settings table).
const (
	AdminSettingPCGWCron              = "pcgw_cron"
	AdminSettingPCGWTitleExcludes     = "pcgw_title_excludes"
	AdminSettingPCGWPathExcludes      = "pcgw_path_excludes"
	AdminSettingPCGWAutoRunFirstStart = "pcgw_auto_run_on_first_start"
	AdminSettingPCGWFirstRunDone      = "pcgw_first_run_done"
)

const (
	DefaultPCGWCron             = "0 3 * * 0"
	DefaultPCGWPathExcludesJSON = `["home",".exe",".dll","steamapps","common"]`
)

func (s *sqliteStore) GetAdminSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM admin_settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *sqliteStore) SetAdminSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

func (s *sqliteStore) ListAdminSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM admin_settings`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// PCGWCronFromSettings returns the cron expression from admin_settings (default if unset).
func PCGWCronFromSettings(settings map[string]string) string {
	if v, ok := settings[AdminSettingPCGWCron]; ok {
		return v
	}
	return DefaultPCGWCron
}

// PCGWTitleExcludesFromSettings parses title exclude substrings (JSON array).
func PCGWTitleExcludesFromSettings(settings map[string]string) []string {
	return parseExcludeJSON(settings[AdminSettingPCGWTitleExcludes])
}

// PCGWPathExcludesFromSettings parses path exclude substrings with defaults.
func PCGWPathExcludesFromSettings(settings map[string]string) []string {
	raw := settings[AdminSettingPCGWPathExcludes]
	if raw == "" {
		var defaults []string
		_ = json.Unmarshal([]byte(DefaultPCGWPathExcludesJSON), &defaults)
		return defaults
	}
	ex := parseExcludeJSON(raw)
	if len(ex) == 0 {
		var defaults []string
		_ = json.Unmarshal([]byte(DefaultPCGWPathExcludesJSON), &defaults)
		return defaults
	}
	return ex
}

func parseExcludeJSON(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

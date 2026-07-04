package store

import (
	"context"
	"database/sql"
	"time"
)

// DayCount is a per-day aggregate; Day is formatted "YYYY-MM-DD".
type DayCount struct {
	Day   string
	Count int
}

// SyncVolumeByDay returns the number of save versions written per day for a user
// over the trailing `days` days, oldest first, with gaps zero-filled. Each save
// version corresponds to one accepted push, so this is a true sync-volume series.
func (s *sqliteStore) SyncVolumeByDay(ctx context.Context, userID string, days int) ([]DayCount, error) {
	if days <= 0 {
		days = 30
	}
	start := time.Now().UTC().AddDate(0, 0, -(days - 1))
	since := start.Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx, `
		SELECT substr(updated_at, 1, 10) AS day, COUNT(*) AS n
		FROM save_versions
		WHERE user_id = ? AND substr(updated_at, 1, 10) >= ?
		GROUP BY day`, userID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]int)
	for rows.Next() {
		var day string
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			return nil, err
		}
		counts[day] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DayCount, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, DayCount{Day: d, Count: counts[d]})
	}
	return out, nil
}

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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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

// ── WebUI insights overhaul aggregates ───────────────────────────────────────
// All day-series queries below follow the SyncVolumeByDay pattern: group in
// SQL, zero-fill gaps in Go. Fleet-wide variants pass userID == "" (the
// save_versions(updated_at) index from migration 29 covers those scans).

// DayBytes is a per-day byte aggregate; Day is formatted "YYYY-MM-DD".
type DayBytes struct {
	Day   string
	Bytes int64
}

// ClientVolumeRow attributes sync activity to a device.
type ClientVolumeRow struct {
	ClientID   string
	ClientName string
	Versions   int
	Bytes      int64
}

// LabelCount is a generic label→count aggregate (app versions, OS, audit actions…).
type LabelCount struct {
	Label string
	Count int
}

// MonthCount is a per-month aggregate; Month is formatted "YYYY-MM".
type MonthCount struct {
	Month string
	Count int
}

// AdoptionStats summarises account-security adoption across all users.
type AdoptionStats struct {
	Users             int
	TOTPEnabled       int
	EncryptionEnabled int
	Admins            int
	Disabled          int
}

// JobStatRow aggregates job_runs reliability per job.
type JobStatRow struct {
	JobName        string
	Runs           int
	Succeeded      int
	AvgDurationSec float64
	LastStatus     string
	LastStartedAt  string
}

// SlotDepthStats describes version-history depth for a user.
type SlotDepthStats struct {
	Slots        int
	Versions     int
	AvgPerSlot   float64
	TopGameID    string
	TopGameTitle string
	TopPathKey   string
	TopCount     int
}

func dayWindow(days, def int) (time.Time, string, int) {
	if days <= 0 {
		days = def
	}
	start := time.Now().UTC().AddDate(0, 0, -(days - 1))
	return start, start.Format("2006-01-02"), days
}

func (s *sqliteStore) scanDaySeries(ctx context.Context, query string, start time.Time, days int, args ...interface{}) ([]DayCount, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]int)
	for rows.Next() {
		var day string
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			return nil, err
		}
		counts[day] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DayCount, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, DayCount{Day: d, Count: counts[d]})
	}
	return out, nil
}

// SyncVolumeByDayAll is the fleet-wide version of SyncVolumeByDay (all users).
func (s *sqliteStore) SyncVolumeByDayAll(ctx context.Context, days int) ([]DayCount, error) {
	start, since, days := dayWindow(days, 30)
	return s.scanDaySeries(ctx, `
		SELECT substr(updated_at, 1, 10) AS day, COUNT(*) AS n
		FROM save_versions
		WHERE substr(updated_at, 1, 10) >= ?
		GROUP BY day`, start, days, since)
}

// SyncBytesByDay sums the bytes actually pushed per day (size of each stored
// version). userID == "" aggregates the whole fleet.
func (s *sqliteStore) SyncBytesByDay(ctx context.Context, userID string, days int) ([]DayBytes, error) {
	start, since, days := dayWindow(days, 30)
	q := `
		SELECT substr(updated_at, 1, 10) AS day, COALESCE(SUM(LENGTH(content)), 0) AS b
		FROM save_versions
		WHERE substr(updated_at, 1, 10) >= ?`
	args := []interface{}{since}
	if userID != "" {
		q += ` AND user_id = ?`
		args = append(args, userID)
	}
	q += ` GROUP BY day`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	sums := make(map[string]int64)
	for rows.Next() {
		var day string
		var b int64
		if err := rows.Scan(&day, &b); err != nil {
			return nil, err
		}
		sums[day] = b
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DayBytes, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, DayBytes{Day: d, Bytes: sums[d]})
	}
	return out, nil
}

// VersionsByClient attributes a user's version writes to devices over the
// trailing window (most active first). Versions from revoked/unknown devices
// group under an empty ClientID.
func (s *sqliteStore) VersionsByClient(ctx context.Context, userID string, days int) ([]ClientVolumeRow, error) {
	_, since, _ := dayWindow(days, 30)
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(v.client_id, '') AS cid,
			COALESCE(c.name, '') AS cname,
			COUNT(*) AS n,
			COALESCE(SUM(LENGTH(v.content)), 0) AS b
		FROM save_versions v
		LEFT JOIN clients c ON c.id = v.client_id
		WHERE v.user_id = ? AND substr(v.updated_at, 1, 10) >= ?
		GROUP BY cid
		ORDER BY n DESC`, userID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ClientVolumeRow
	for rows.Next() {
		var row ClientVolumeRow
		if err := rows.Scan(&row.ClientID, &row.ClientName, &row.Versions, &row.Bytes); err != nil {
			return nil, err
		}
		if row.ClientName == "" {
			if row.ClientID == "" {
				row.ClientName = "Unknown device"
			} else {
				row.ClientName = "Removed device"
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ActivityByWeekday buckets a user's version writes by day of week (0=Sunday,
// UTC) over the trailing window. Always returns 7 buckets.
func (s *sqliteStore) ActivityByWeekday(ctx context.Context, userID string, days int) ([]int, error) {
	_, since, _ := dayWindow(days, 90)
	rows, err := s.db.QueryContext(ctx, `
		SELECT CAST(strftime('%w', substr(updated_at, 1, 10)) AS INTEGER) AS wd, COUNT(*)
		FROM save_versions
		WHERE user_id = ? AND substr(updated_at, 1, 10) >= ?
		GROUP BY wd`, userID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]int, 7)
	for rows.Next() {
		var wd, n int
		if err := rows.Scan(&wd, &n); err != nil {
			return nil, err
		}
		if wd >= 0 && wd < 7 {
			out[wd] = n
		}
	}
	return out, rows.Err()
}

// ActivityByHour buckets a user's version writes by hour of day (UTC) over the
// trailing window. Always returns 24 buckets.
func (s *sqliteStore) ActivityByHour(ctx context.Context, userID string, days int) ([]int, error) {
	_, since, _ := dayWindow(days, 90)
	rows, err := s.db.QueryContext(ctx, `
		SELECT CAST(substr(updated_at, 12, 2) AS INTEGER) AS hh, COUNT(*)
		FROM save_versions
		WHERE user_id = ? AND substr(updated_at, 1, 10) >= ?
		GROUP BY hh`, userID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]int, 24)
	for rows.Next() {
		var hh, n int
		if err := rows.Scan(&hh, &n); err != nil {
			return nil, err
		}
		if hh >= 0 && hh < 24 {
			out[hh] = n
		}
	}
	return out, rows.Err()
}

// MostActiveGames ranks a user's games by version writes in the window.
func (s *sqliteStore) MostActiveGames(ctx context.Context, userID string, days, limit int) ([]SaveGameStatRow, error) {
	_, since, _ := dayWindow(days, 30)
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.game_id,
			COALESCE(
				(SELECT gsl.game_title FROM game_save_locations gsl WHERE gsl.game_id = v.game_id LIMIT 1),
				v.game_id
			) AS game_title,
			COUNT(*) AS n,
			COALESCE(SUM(LENGTH(v.content)), 0) AS b
		FROM save_versions v
		WHERE v.user_id = ? AND substr(v.updated_at, 1, 10) >= ?
		GROUP BY v.game_id
		ORDER BY n DESC
		LIMIT ?`, userID, since, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

// VersionDepth reports version-history depth for a user: total slots,
// versions, average, and the deepest slot.
func (s *sqliteStore) VersionDepth(ctx context.Context, userID string) (SlotDepthStats, error) {
	var st SlotDepthStats
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT game_id || x'1f' || path_key), COUNT(*)
		FROM save_versions WHERE user_id = ?`, userID).Scan(&st.Slots, &st.Versions)
	if err != nil {
		return st, err
	}
	if st.Slots > 0 {
		st.AvgPerSlot = float64(st.Versions) / float64(st.Slots)
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT v.game_id,
			COALESCE(
				(SELECT gsl.game_title FROM game_save_locations gsl WHERE gsl.game_id = v.game_id LIMIT 1),
				v.game_id
			),
			v.path_key, COUNT(*) AS n
		FROM save_versions v
		WHERE v.user_id = ?
		GROUP BY v.game_id, v.path_key
		ORDER BY n DESC
		LIMIT 1`, userID)
	if err := row.Scan(&st.TopGameID, &st.TopGameTitle, &st.TopPathKey, &st.TopCount); err != nil && err != sql.ErrNoRows {
		return st, err
	}
	return st, nil
}

// CountEncryptedSaves reports how many of the user's saves are E2E-encrypted.
func (s *sqliteStore) CountEncryptedSaves(ctx context.Context, userID string) (encrypted, total int, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN encrypted = 1 THEN 1 ELSE 0 END), 0), COUNT(*)
		FROM saves WHERE user_id = ?`, userID).Scan(&encrypted, &total)
	return encrypted, total, err
}

// ClientVersionCounts is the fleet app-version distribution (upgrade tracking).
func (s *sqliteStore) ClientVersionCounts(ctx context.Context) ([]LabelCount, error) {
	return s.labelCounts(ctx, `
		SELECT COALESCE(NULLIF(app_version, ''), 'unknown') AS label, COUNT(*)
		FROM clients GROUP BY label ORDER BY COUNT(*) DESC`)
}

// ClientOSCounts is the fleet OS distribution.
func (s *sqliteStore) ClientOSCounts(ctx context.Context) ([]LabelCount, error) {
	return s.labelCounts(ctx, `
		SELECT COALESCE(NULLIF(os, ''), 'unknown') AS label, COUNT(*)
		FROM clients GROUP BY label ORDER BY COUNT(*) DESC`)
}

// AuditActionCounts ranks audit actions over the trailing window.
func (s *sqliteStore) AuditActionCounts(ctx context.Context, days, limit int) ([]LabelCount, error) {
	_, since, _ := dayWindow(days, 30)
	if limit <= 0 {
		limit = 10
	}
	return s.labelCounts(ctx, `
		SELECT action AS label, COUNT(*)
		FROM audit_log
		WHERE substr(at, 1, 10) >= ?
		GROUP BY action ORDER BY COUNT(*) DESC LIMIT ?`, since, limit)
}

func (s *sqliteStore) labelCounts(ctx context.Context, query string, args ...interface{}) ([]LabelCount, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []LabelCount
	for rows.Next() {
		var lc LabelCount
		if err := rows.Scan(&lc.Label, &lc.Count); err != nil {
			return nil, err
		}
		out = append(out, lc)
	}
	return out, rows.Err()
}

// AuditVolumeByDay is the audit-log activity series (all actors).
func (s *sqliteStore) AuditVolumeByDay(ctx context.Context, days int) ([]DayCount, error) {
	start, since, days := dayWindow(days, 30)
	return s.scanDaySeries(ctx, `
		SELECT substr(at, 1, 10) AS day, COUNT(*) AS n
		FROM audit_log
		WHERE substr(at, 1, 10) >= ?
		GROUP BY day`, start, days, since)
}

// ManifestFetchByDay is the manifest-download activity series.
func (s *sqliteStore) ManifestFetchByDay(ctx context.Context, days int) ([]DayCount, error) {
	start, since, days := dayWindow(days, 30)
	return s.scanDaySeries(ctx, `
		SELECT substr(fetched_at, 1, 10) AS day, COUNT(*) AS n
		FROM manifest_fetches
		WHERE substr(fetched_at, 1, 10) >= ?
		GROUP BY day`, start, days, since)
}

// ActiveUsersByDay counts distinct users with at least one version write per day.
func (s *sqliteStore) ActiveUsersByDay(ctx context.Context, days int) ([]DayCount, error) {
	start, since, days := dayWindow(days, 30)
	return s.scanDaySeries(ctx, `
		SELECT substr(updated_at, 1, 10) AS day, COUNT(DISTINCT user_id) AS n
		FROM save_versions
		WHERE substr(updated_at, 1, 10) >= ?
		GROUP BY day`, start, days, since)
}

// SignupsByMonth counts account creations per month over the trailing months.
func (s *sqliteStore) SignupsByMonth(ctx context.Context, months int) ([]MonthCount, error) {
	if months <= 0 {
		months = 12
	}
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -(months - 1), 0)
	rows, err := s.db.QueryContext(ctx, `
		SELECT substr(created_at, 1, 7) AS m, COUNT(*)
		FROM users
		WHERE substr(created_at, 1, 7) >= ?
		GROUP BY m`, start.Format("2006-01"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := make(map[string]int)
	for rows.Next() {
		var m string
		var n int
		if err := rows.Scan(&m, &n); err != nil {
			return nil, err
		}
		counts[m] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]MonthCount, 0, months)
	for i := 0; i < months; i++ {
		m := start.AddDate(0, i, 0).Format("2006-01")
		out = append(out, MonthCount{Month: m, Count: counts[m]})
	}
	return out, nil
}

// UserAdoptionStats summarises 2FA/encryption/role adoption fleet-wide.
func (s *sqliteStore) UserAdoptionStats(ctx context.Context) (AdoptionStats, error) {
	var st AdoptionStats
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN totp_enabled = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN encryption_enabled = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN role = 'admin' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN disabled = 1 THEN 1 ELSE 0 END), 0)
		FROM users`).Scan(&st.Users, &st.TOTPEnabled, &st.EncryptionEnabled, &st.Admins, &st.Disabled)
	return st, err
}

// JobRunStats aggregates reliability per job over the retained job_runs rows.
func (s *sqliteStore) JobRunStats(ctx context.Context) ([]JobStatRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_name,
			COUNT(*) AS runs,
			COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0) AS ok,
			COALESCE(AVG(
				CASE WHEN finished_at IS NOT NULL AND finished_at != ''
				THEN (julianday(finished_at) - julianday(started_at)) * 86400.0
				END), 0) AS avg_sec
		FROM job_runs
		GROUP BY job_name
		ORDER BY job_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []JobStatRow
	for rows.Next() {
		var row JobStatRow
		if err := rows.Scan(&row.JobName, &row.Runs, &row.Succeeded, &row.AvgDurationSec); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		var status, started sql.NullString
		err := s.db.QueryRowContext(ctx, `
			SELECT status, started_at FROM job_runs
			WHERE job_name = ? ORDER BY started_at DESC LIMIT 1`, out[i].JobName).Scan(&status, &started)
		if err == nil {
			out[i].LastStatus = status.String
			out[i].LastStartedAt = started.String
		}
	}
	return out, nil
}

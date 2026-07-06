package store

import (
	"context"
	"strings"
)

// Paged variants of the admin activity queries (jobs, manifest fetches, audit
// log, stats snapshots). These back the paginated tables on the admin WebUI;
// the unpaged List* methods remain for callers that only need "the N most
// recent" (dashboards, job runners).

// ListJobRunsPage returns a page of job runs, newest first. jobName and status
// filter when non-empty.
func (s *sqliteStore) ListJobRunsPage(ctx context.Context, jobName, status string, limit, offset int) ([]JobRun, error) {
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	where, args := jobRunsWhere(jobName, status)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_name, started_at, COALESCE(finished_at, ''), status, COALESCE(error_message, ''), entries_count
		 FROM job_runs`+where+` ORDER BY started_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobRun
	for rows.Next() {
		var j JobRun
		if err := rows.Scan(&j.ID, &j.JobName, &j.StartedAt, &j.FinishedAt, &j.Status, &j.ErrorMessage, &j.EntriesCount); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// CountJobRuns counts job runs matching the optional jobName/status filters.
func (s *sqliteStore) CountJobRuns(ctx context.Context, jobName, status string) (int, error) {
	where, args := jobRunsWhere(jobName, status)
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_runs`+where, args...).Scan(&n)
	return n, err
}

func jobRunsWhere(jobName, status string) (string, []interface{}) {
	var conds []string
	var args []interface{}
	if jobName != "" {
		conds = append(conds, "job_name = ?")
		args = append(args, jobName)
	}
	if status != "" {
		conds = append(conds, "status = ?")
		args = append(args, status)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListJobNames returns the distinct job names recorded in job_runs.
func (s *sqliteStore) ListJobNames(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, `SELECT DISTINCT job_name FROM job_runs ORDER BY job_name`)
}

// ListManifestFetchesPage returns a page of manifest fetches, newest first.
func (s *sqliteStore) ListManifestFetchesPage(ctx context.Context, limit, offset int) ([]ManifestFetchRow, error) {
	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(client_id, ''), COALESCE(client_name, ''), COALESCE(username, ''), entries_count, fetched_at
		 FROM manifest_fetches ORDER BY fetched_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManifestFetchRow
	for rows.Next() {
		var f ManifestFetchRow
		if err := rows.Scan(&f.ID, &f.ClientID, &f.ClientName, &f.Username, &f.EntriesCount, &f.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CountManifestFetches counts all recorded manifest fetches.
func (s *sqliteStore) CountManifestFetches(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM manifest_fetches`).Scan(&n)
	return n, err
}

// ListAuditLogPage returns a filtered page of audit entries, newest first.
func (s *sqliteStore) ListAuditLogPage(ctx context.Context, f AuditLogFilter, limit, offset int) ([]AuditRow, error) {
	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	where, args := auditLogWhere(f)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, actor_user_id, actor_username, action, COALESCE(target_id, ''), COALESCE(details, '')
		 FROM audit_log`+where+` ORDER BY at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var a AuditRow
		if err := rows.Scan(&a.ID, &a.At, &a.ActorUserID, &a.ActorUsername, &a.Action, &a.TargetID, &a.Details); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountAuditLog counts audit entries matching the filter.
func (s *sqliteStore) CountAuditLog(ctx context.Context, f AuditLogFilter) (int, error) {
	where, args := auditLogWhere(f)
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`+where, args...).Scan(&n)
	return n, err
}

func auditLogWhere(f AuditLogFilter) (string, []interface{}) {
	var conds []string
	var args []interface{}
	if f.Action != "" {
		conds = append(conds, "action = ?")
		args = append(args, f.Action)
	}
	if text := strings.TrimSpace(f.Text); text != "" {
		// instr avoids LIKE-pattern escaping for user-supplied text.
		conds = append(conds,
			`(instr(lower(actor_username), lower(?)) > 0
			  OR instr(lower(action), lower(?)) > 0
			  OR instr(lower(COALESCE(target_id, '')), lower(?)) > 0
			  OR instr(lower(COALESCE(details, '')), lower(?)) > 0)`)
		args = append(args, text, text, text, text)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListAuditActions returns the distinct actions recorded in audit_log.
func (s *sqliteStore) ListAuditActions(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, `SELECT DISTINCT action FROM audit_log ORDER BY action`)
}

// ListStatsSnapshotsPage returns a page of stats snapshots, newest first.
func (s *sqliteStore) ListStatsSnapshotsPage(ctx context.Context, limit, offset int) ([]StatsSnapshotRow, error) {
	if limit <= 0 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, user_count, client_count, save_count, storage_bytes
		 FROM stats_snapshots ORDER BY at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatsSnapshotRow
	for rows.Next() {
		var r StatsSnapshotRow
		if err := rows.Scan(&r.ID, &r.At, &r.UserCount, &r.ClientCount, &r.SaveCount, &r.StorageBytes); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountStatsSnapshots counts all recorded stats snapshots.
func (s *sqliteStore) CountStatsSnapshots(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stats_snapshots`).Scan(&n)
	return n, err
}

func (s *sqliteStore) listDistinctStrings(ctx context.Context, query string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

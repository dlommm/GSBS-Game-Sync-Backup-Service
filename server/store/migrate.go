package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/savepath"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/logx"
)

// schemaVersion is the current database schema version.
// To add a new migration: append a migrationStep to migrationSteps() and increment this constant.
const schemaVersion = 26

// errMigDryRun is returned by a migration step that was invoked with GSBS_DRY_RUN_MIGRATION=1.
// runMigrationStep rolls back the transaction and treats this as a non-fatal skip (user_version
// is NOT advanced), so the step will re-run on the next startup when the env var is absent.
var errMigDryRun = errors.New("migration dry-run complete")

type migrationStep struct {
	version int
	fn      func(tx *sql.Tx) error
}

// migrate reads PRAGMA user_version, warns if migrations are pending, then runs each
// pending step inside its own transaction. On success each step sets PRAGMA user_version = N.
func (s *sqliteStore) migrate() error {
	var current int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	steps := s.migrationSteps()
	var pending int
	for _, step := range steps {
		if step.version > current {
			pending++
		}
	}

	if pending > 0 {
		// Skip the warning banner and sleep for in-memory databases used in tests.
		if !isInMemoryPath(s.dbPath) {
			logx.Logger().Warn().Str("component", "migration").
				Int("from_version", current).Int("to_version", schemaVersion).
				Msg("GSBS: DB migration required. Back up your database before proceeding. Continuing in 3 seconds...")
			time.Sleep(3 * time.Second)
		}
	}

	for _, step := range steps {
		if step.version <= current {
			continue
		}
		if err := s.runMigrationStep(step); err != nil {
			return fmt.Errorf("migration step %d: %w", step.version, err)
		}
	}
	return nil
}

// runMigrationStep executes a single migration step inside a transaction.
// On success it commits PRAGMA user_version = step.version in the same transaction.
// On failure it rolls back — all previously committed steps are unaffected.
// If the step returns errMigDryRun the transaction is rolled back and the version is NOT
// advanced; the server continues normally and the step will re-run on the next startup.
func (s *sqliteStore) runMigrationStep(step migrationStep) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := step.fn(tx); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, errMigDryRun) {
			logx.Logger().Info().Str("component", "migration").Int("step", step.version).
				Msg("GSBS: migration step dry-run complete (not applied; remove GSBS_DRY_RUN_MIGRATION=1 to apply)")
			return nil
		}
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", step.version)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("set user_version=%d: %w", step.version, err)
	}
	return tx.Commit()
}

// migrationSteps returns all schema migration steps in version order.
// Adding a new migration: append a step here and increment schemaVersion.
func (s *sqliteStore) migrationSteps() []migrationStep {
	return []migrationStep{
		{1, stepCoreTables},
		{2, stepNotesColumn},
		{3, stepUserRoleColumn},
		{4, stepUserDisabledColumn},
		{5, stepUserStorageQuotaColumn},
		{6, stepAuditLog},
		{7, stepStatsSnapshots},
		{8, stepSessions},
		{9, stepTOTPSecretColumn},
		{10, stepTOTPEnabledColumn},
		{11, stepBatchAlters},
		{12, s.stepTokenHashes},
		{13, s.stepPCGW},
		{14, s.stepSaveFilesystem},
		{15, s.stepAdminSettings},
		{16, s.stepMergeOSSlots},
		{17, s.stepPCGWCatalog},
		{18, s.stepPCGWIncrementalSpeed},
		{19, s.stepPCGWBundleSettings},
		{20, s.stepPCGWSyncSourceS3},
		{21, stepSaveVersionChangeMeta},
		{22, stepIntegrityFindings},
		{23, s.stepEncryptTOTPSecrets},
		{24, stepClientAppVersion},
		{25, stepClientStaleNotified},
		{26, stepUserNotifySettings},
	}
}

// ── Step implementations ──────────────────────────────────────────────────────

// stepClientAppVersion records each device's reported app version
// (X-GSBS-Client-Version). Drives crypto-v2 fleet auto-negotiation: a user's
// clients switch to the Argon2id save-encryption format only once every
// recently-seen device reports a version that can read it.
func stepClientAppVersion(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE clients ADD COLUMN app_version TEXT`)
	return err
}

// stepClientStaleNotified deduplicates stale-device notifications: set when a
// "device hasn't synced in N days" alert fires, cleared when the device
// reappears, so the daily check alerts once per stale period.
func stepClientStaleNotified(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE clients ADD COLUMN stale_notified_at TEXT`)
	return err
}

// stepUserNotifySettings holds per-user notification sinks (webhook/Discord/
// ntfy URLs + event filter); admin-level sinks live in admin_settings.
func stepUserNotifySettings(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS user_notify_settings (
			user_id TEXT PRIMARY KEY,
			webhook_url TEXT NOT NULL DEFAULT '',
			discord_url TEXT NOT NULL DEFAULT '',
			ntfy_url TEXT NOT NULL DEFAULT '',
			events TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
	`)
	return err
}

// stepEncryptTOTPSecrets seals existing plaintext TOTP secrets with the
// server's local key file (see secretbox.go). Rows already sealed (enc:v1:
// prefix) are left alone, so the step is idempotent and safe on restored
// databases. After this step the database alone no longer contains usable
// 2FA seeds — back up gsbs-keys/ together with the database.
func (s *sqliteStore) stepEncryptTOTPSecrets(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, totp_secret FROM users
		WHERE totp_secret IS NOT NULL AND totp_secret != '' AND totp_secret NOT LIKE 'enc:v1:%'`)
	if err != nil {
		return err
	}
	type row struct{ id, secret string }
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.secret); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if os.Getenv("GSBS_DRY_RUN_MIGRATION") == "1" {
		logx.Logger().Info().Str("component", "migration").Int("rows", len(pending)).
			Msg("GSBS migrate step 23 (dry-run): would encrypt TOTP secret(s) at rest")
		return errMigDryRun
	}
	if len(pending) == 0 {
		return nil
	}
	key, err := s.totpKey()
	if err != nil {
		return fmt.Errorf("step 23: load totp key: %w", err)
	}
	for _, r := range pending {
		sealed, err := sealColumn(key, r.secret)
		if err != nil {
			return fmt.Errorf("step 23: seal secret: %w", err)
		}
		if _, err := tx.Exec(`UPDATE users SET totp_secret = ? WHERE id = ?`, sealed, r.id); err != nil {
			return fmt.Errorf("step 23: update user %s: %w", r.id, err)
		}
	}
	logx.Logger().Info().Str("component", "migration").Int("rows", len(pending)).
		Msg("GSBS migrate step 23: encrypted TOTP secret(s) at rest — back up the gsbs-keys directory with your database")
	return nil
}

// stepIntegrityFindings records blob-corruption findings from the weekly
// integrity_check job: one row per save slot with a problem, replaced on
// re-check and removed when the slot verifies clean again.
func stepIntegrityFindings(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS integrity_findings (
			id TEXT PRIMARY KEY,
			at TEXT NOT NULL,
			user_id TEXT NOT NULL,
			game_id TEXT NOT NULL,
			path_key TEXT NOT NULL,
			kind TEXT NOT NULL,
			expected_hash TEXT,
			actual_hash TEXT
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_integrity_findings_slot
			ON integrity_findings(user_id, game_id, path_key);
		CREATE INDEX IF NOT EXISTS idx_integrity_findings_at ON integrity_findings(at);
	`)
	return err
}

func stepCoreTables(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS clients (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL,
			os TEXT NOT NULL,
			token TEXT UNIQUE,
			last_seen TEXT,
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS saves (
			user_id TEXT NOT NULL,
			game_id TEXT NOT NULL,
			path_key TEXT NOT NULL,
			content BLOB NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (user_id, game_id, path_key),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE TABLE IF NOT EXISTS game_save_locations (
			id TEXT PRIMARY KEY,
			game_id TEXT NOT NULL,
			pcgw_page_id INTEGER NOT NULL,
			game_title TEXT NOT NULL,
			platform TEXT NOT NULL,
			path_template TEXT NOT NULL,
			is_config INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			source TEXT NOT NULL,
			notes TEXT,
			UNIQUE(game_id, platform, path_template)
		);
		CREATE TABLE IF NOT EXISTS job_runs (
			id TEXT PRIMARY KEY,
			job_name TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			status TEXT NOT NULL,
			error_message TEXT,
			entries_count INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS manifest_fetches (
			id TEXT PRIMARY KEY,
			client_id TEXT,
			client_name TEXT,
			username TEXT,
			entries_count INTEGER NOT NULL DEFAULT 0,
			fetched_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS save_versions (
			user_id TEXT NOT NULL,
			game_id TEXT NOT NULL,
			path_key TEXT NOT NULL,
			version INTEGER NOT NULL,
			content BLOB NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (user_id, game_id, path_key, version),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
		CREATE INDEX IF NOT EXISTS idx_save_versions_slot ON save_versions(user_id, game_id, path_key);
		CREATE INDEX IF NOT EXISTS idx_clients_token ON clients(token);
		CREATE INDEX IF NOT EXISTS idx_saves_user ON saves(user_id);
		CREATE INDEX IF NOT EXISTS idx_manifest_updated ON game_save_locations(updated_at);
		CREATE INDEX IF NOT EXISTS idx_job_runs_name ON job_runs(job_name, started_at);
		CREATE INDEX IF NOT EXISTS idx_manifest_fetches_at ON manifest_fetches(fetched_at);
	`)
	return err
}

// stepSaveVersionChangeMeta records, per save version, which client wrote it and
// the byte delta vs the previous version — powering change-size insights.
func stepSaveVersionChangeMeta(tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE save_versions ADD COLUMN client_id TEXT`,
		`ALTER TABLE save_versions ADD COLUMN change_bytes INTEGER`,
	} {
		if _, err := tx.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate") {
			return err
		}
	}
	return nil
}

func stepNotesColumn(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE game_save_locations ADD COLUMN notes TEXT`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	return nil
}

func stepUserRoleColumn(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE users ADD COLUMN role TEXT DEFAULT 'user'`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	return nil
}

func stepUserDisabledColumn(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	return nil
}

func stepUserStorageQuotaColumn(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE users ADD COLUMN storage_quota_bytes INTEGER`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	return nil
}

func stepAuditLog(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS audit_log (
			id TEXT PRIMARY KEY,
			at TEXT NOT NULL,
			actor_user_id TEXT NOT NULL,
			actor_username TEXT NOT NULL,
			action TEXT NOT NULL,
			target_id TEXT,
			details TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_audit_log_at ON audit_log(at);
	`)
	return err
}

func stepStatsSnapshots(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS stats_snapshots (
			id TEXT PRIMARY KEY,
			at TEXT NOT NULL,
			user_count INTEGER NOT NULL,
			client_count INTEGER NOT NULL,
			save_count INTEGER NOT NULL,
			storage_bytes INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_stats_snapshots_at ON stats_snapshots(at);
	`)
	return err
}

func stepSessions(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			created_at TEXT NOT NULL,
			last_seen TEXT NOT NULL,
			user_agent TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
	`)
	return err
}

func stepTOTPSecretColumn(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE users ADD COLUMN totp_secret TEXT`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	return nil
}

func stepTOTPEnabledColumn(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0`)
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		return err
	}
	return nil
}

func stepBatchAlters(tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE saves ADD COLUMN content_hash TEXT`,
		`ALTER TABLE saves ADD COLUMN content_size INTEGER`,
		`ALTER TABLE saves ADD COLUMN client_id TEXT`,
		`ALTER TABLE save_versions ADD COLUMN content_hash TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN steam_app_ids TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN gog_id TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN epic_id TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN ubisoft_id TEXT`,
		`ALTER TABLE game_save_locations ADD COLUMN save_rules_json TEXT`,
		`ALTER TABLE users ADD COLUMN encryption_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE saves ADD COLUMN encrypted INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE clients ADD COLUMN token_created_at TEXT`,
	} {
		_, err := tx.Exec(stmt)
		if err != nil && !strings.Contains(err.Error(), "duplicate") {
			return err
		}
	}
	return nil
}

// stepTokenHashes was previously migrateTokenHashes. It hashes any plaintext tokens
// that pre-date the SHA-256 token hashing introduced in an earlier release.
func (s *sqliteStore) stepTokenHashes(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE clients SET token_created_at = COALESCE(token_created_at, created_at) WHERE token_created_at IS NULL OR token_created_at = ''`)
	if err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id, token FROM clients WHERE token IS NOT NULL AND token != ''`)
	if err != nil {
		return err
	}
	type clientRow struct{ id, token string }
	var unhashed []clientRow
	for rows.Next() {
		var id, token string
		if err := rows.Scan(&id, &token); err != nil {
			rows.Close()
			return err
		}
		if !isTokenHashed(token) {
			unhashed = append(unhashed, clientRow{id, token})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, c := range unhashed {
		if _, err := tx.Exec(`UPDATE clients SET token = ? WHERE id = ?`, hashToken(c.token), c.id); err != nil {
			return err
		}
	}
	return nil
}

// stepPCGW was previously migratePCGW. Creates all PCGW-related tables, indexes,
// triggers and backfills from existing manifest rows.
func (s *sqliteStore) stepPCGW(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS pcgw_games (
			page_id INTEGER PRIMARY KEY,
			page_name TEXT NOT NULL,
			title TEXT NOT NULL,
			is_disambiguation INTEGER NOT NULL DEFAULT 0,
			redirects_to TEXT,
			steam_appids TEXT NOT NULL DEFAULT '[]',
			gog_id TEXT,
			epic_id TEXT,
			ubisoft_id TEXT,
			microsoft_id TEXT,
			battlenet_id TEXT,
			itch_id TEXT,
			other_ids TEXT NOT NULL DEFAULT '{}',
			developers TEXT NOT NULL DEFAULT '[]',
			publishers TEXT NOT NULL DEFAULT '[]',
			release_dates TEXT NOT NULL DEFAULT '[]',
			engines TEXT NOT NULL DEFAULT '[]',
			taxonomy TEXT NOT NULL DEFAULT '{}',
			infobox TEXT NOT NULL DEFAULT '{}',
			cover_url TEXT,
			hltb_id TEXT,
			igdb_id TEXT,
			cargo_last_updated TEXT,
			platforms_present TEXT NOT NULL DEFAULT '[]',
			last_rev_id INTEGER,
			last_rev_timestamp TEXT,
			last_fetched_at TEXT,
			parse_status TEXT NOT NULL DEFAULT 'pending',
			parse_error TEXT,
			parse_duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_games_title ON pcgw_games(title)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_games_page_name ON pcgw_games(page_name)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_games_parse_status ON pcgw_games(parse_status)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_games_last_fetched ON pcgw_games(last_fetched_at)`,
		`CREATE TABLE IF NOT EXISTS pcgw_game_data (
			page_id INTEGER NOT NULL REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			platform_key TEXT NOT NULL,
			platform_raw_label TEXT,
			save_locations TEXT NOT NULL DEFAULT '[]',
			config_locations TEXT NOT NULL DEFAULT '[]',
			save_game_cloud_sync TEXT NOT NULL DEFAULT '{}',
			install_locations TEXT NOT NULL DEFAULT '[]',
			registry_keys TEXT NOT NULL DEFAULT '[]',
			save_file_info TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			structured TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (page_id, platform_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_game_data_page ON pcgw_game_data(page_id)`,
		`CREATE TABLE IF NOT EXISTS pcgw_availability (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_monetization (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_video (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_input (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_audio (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_network (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_other (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_notes (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_references (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_external_links (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			data TEXT NOT NULL DEFAULT '{}',
			all_templates TEXT NOT NULL DEFAULT '[]',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_system_requirements (
			page_id INTEGER NOT NULL REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			platform_key TEXT NOT NULL,
			requirement_type TEXT NOT NULL,
			specs TEXT NOT NULL DEFAULT '{}',
			section_wikitext TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (page_id, platform_key, requirement_type)
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_metadata (
			page_id INTEGER PRIMARY KEY REFERENCES pcgw_games(page_id) ON DELETE CASCADE,
			full_wikitext_zstd BLOB,
			content_hash TEXT,
			section_hashes TEXT NOT NULL DEFAULT '{}',
			parsed_sections TEXT NOT NULL DEFAULT '{}',
			uncompressed_size INTEGER NOT NULL DEFAULT 0,
			last_fetched_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pcgw_parse_failures (
			id TEXT PRIMARY KEY,
			page_id INTEGER NOT NULL,
			sync_run_id TEXT,
			section TEXT NOT NULL,
			error_message TEXT NOT NULL,
			wikitext_snippet TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_parse_failures_page ON pcgw_parse_failures(page_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_parse_failures_run ON pcgw_parse_failures(sync_run_id)`,
		`CREATE TABLE IF NOT EXISTS pcgw_sync_runs (
			id TEXT PRIMARY KEY,
			mode TEXT NOT NULL,
			status TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			checkpoint_offset INTEGER NOT NULL DEFAULT 0,
			games_total INTEGER NOT NULL DEFAULT 0,
			games_ok INTEGER NOT NULL DEFAULT 0,
			games_partial INTEGER NOT NULL DEFAULT 0,
			games_failed INTEGER NOT NULL DEFAULT 0,
			games_skipped INTEGER NOT NULL DEFAULT 0,
			avg_parse_ms INTEGER NOT NULL DEFAULT 0,
			error_message TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_sync_runs_started ON pcgw_sync_runs(started_at)`,
		`CREATE TABLE IF NOT EXISTS pcgw_manifest_meta (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			manifest_version INTEGER NOT NULL DEFAULT 0,
			manifest_etag TEXT NOT NULL DEFAULT '',
			last_incremental_at TEXT,
			last_full_sync_at TEXT,
			db_wikitext_bytes INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT OR IGNORE INTO pcgw_manifest_meta (id, manifest_version, manifest_etag) VALUES (1, 0, '')`,
		`CREATE TABLE IF NOT EXISTS pcgw_manifest_deletions (
			game_id TEXT NOT NULL PRIMARY KEY,
			deleted_at TEXT NOT NULL
		)`,
		`CREATE TRIGGER IF NOT EXISTS pcgw_games_ad_manifest AFTER DELETE ON pcgw_games BEGIN
			INSERT OR REPLACE INTO pcgw_manifest_deletions (game_id, deleted_at)
			VALUES (CAST(old.page_id AS TEXT), datetime('now'));
		END`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migrate pcgw: %w", err)
		}
	}
	for _, col := range []string{
		`ALTER TABLE pcgw_sync_runs ADD COLUMN resumed_from_run_id TEXT`,
		`ALTER TABLE pcgw_sync_runs ADD COLUMN notes TEXT`,
	} {
		if _, err := tx.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate") {
			return fmt.Errorf("migrate pcgw: %w", err)
		}
	}

	// FTS5 virtual table (optional — some SQLite builds lack fts5)
	ftsOK := false
	if _, err := tx.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS pcgw_games_fts USING fts5(
		title, page_name, content='pcgw_games', content_rowid='page_id'
	)`); err == nil {
		ftsOK = true
	}

	// Backfill from existing manifest rows (before triggers so inserts don't fire FTS yet)
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO pcgw_games (
			page_id, page_name, title, steam_appids, gog_id, epic_id, ubisoft_id,
			parse_status, created_at, updated_at
		)
		SELECT DISTINCT
			pcgw_page_id, game_title, game_title,
			COALESCE(steam_app_ids, '[]'), COALESCE(gog_id, ''), COALESCE(epic_id, ''), COALESCE(ubisoft_id, ''),
			'pending', datetime('now'), datetime('now')
		FROM game_save_locations
		WHERE pcgw_page_id > 0`); err != nil {
		return fmt.Errorf("migrate pcgw backfill: %w", err)
	}

	if ftsOK {
		triggers := []string{
			`CREATE TRIGGER IF NOT EXISTS pcgw_games_ai AFTER INSERT ON pcgw_games BEGIN
			INSERT INTO pcgw_games_fts(rowid, title, page_name) VALUES (new.page_id, new.title, new.page_name);
		END`,
			`CREATE TRIGGER IF NOT EXISTS pcgw_games_ad AFTER DELETE ON pcgw_games BEGIN
			INSERT INTO pcgw_games_fts(pcgw_games_fts, rowid, title, page_name)
			VALUES ('delete', old.page_id, old.title, old.page_name);
		END`,
			`CREATE TRIGGER IF NOT EXISTS pcgw_games_au AFTER UPDATE ON pcgw_games BEGIN
			INSERT INTO pcgw_games_fts(pcgw_games_fts, rowid, title, page_name)
			VALUES ('delete', old.page_id, old.title, old.page_name);
			INSERT INTO pcgw_games_fts(rowid, title, page_name)
			VALUES (new.page_id, new.title, new.page_name);
		END`,
		}
		for _, t := range triggers {
			if _, err := tx.Exec(t); err != nil {
				return fmt.Errorf("migrate pcgw fts trigger: %w", err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO pcgw_games_fts(pcgw_games_fts) VALUES('rebuild')`); err != nil {
			logx.Logger().Warn().Str("component", "migration").Err(err).
				Msg("GSBS: pcgw fts rebuild warning (non-fatal)")
		}
	}
	return nil
}

// stepSaveFilesystem was previously migrateSaveFilesystem. It rebuilds the saves
// table to add the relative_path / storage_path columns and make content nullable.
// The entire CREATE / INSERT-SELECT / DROP / RENAME sequence runs inside the step's
// transaction, so a crash mid-rebuild cannot leave saves dropped but saves_fs unrennamed.
func (s *sqliteStore) stepSaveFilesystem(tx *sql.Tx) error {
	// Check current column state using the transaction to avoid a second connection.
	hasRelative, contentNotNull, err := savesColumnStateTx(tx)
	if err != nil {
		return err
	}
	if hasRelative && !contentNotNull {
		// Already migrated.
		return nil
	}

	if _, err := tx.Exec(`
		CREATE TABLE saves_fs (
			user_id TEXT NOT NULL,
			game_id TEXT NOT NULL,
			path_key TEXT NOT NULL,
			content BLOB,
			relative_path TEXT,
			storage_path TEXT,
			updated_at TEXT NOT NULL,
			content_hash TEXT,
			content_size INTEGER,
			client_id TEXT,
			encrypted INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, game_id, path_key),
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO saves_fs (user_id, game_id, path_key, content, relative_path, storage_path, updated_at, content_hash, content_size, client_id, encrypted)
		SELECT user_id, game_id, path_key, content, NULL, NULL, updated_at, content_hash, content_size, client_id, COALESCE(encrypted, 0)
		FROM saves`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE saves`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE saves_fs RENAME TO saves`); err != nil {
		return err
	}
	_, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_saves_user ON saves(user_id)`)
	return err
}

// savesColumnStateTx inspects the saves table columns using a transaction handle.
func savesColumnStateTx(tx *sql.Tx) (hasRelative bool, contentNotNull bool, err error) {
	rows, err := tx.Query(`PRAGMA table_info(saves)`)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, false, err
		}
		switch name {
		case "relative_path":
			hasRelative = true
		case "content":
			contentNotNull = notnull != 0
		}
	}
	return hasRelative, contentNotNull, rows.Err()
}

// stepAdminSettings creates the admin_settings table and seeds default values.
func (s *sqliteStore) stepAdminSettings(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS admin_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO admin_settings (key, value) VALUES (?, ?)`,
		AdminSettingPCGWCron, DefaultPCGWCron); err != nil {
		return err
	}
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO admin_settings (key, value) VALUES (?, ?)`,
		AdminSettingPCGWPathExcludes, DefaultPCGWPathExcludesJSON)
	return err
}

// ── Step 16: cross-OS slot merge ─────────────────────────────────────────────

// migSlotKey computes the new (OS-neutral) path key for a PCGW-sourced rule slot.
// This inlines saverule.RuleKey's SlotLabel branch to avoid import-cycle risk.
func migSlotKey(gameID, slotLabel string, isConfig bool) string {
	cfg := "0"
	if isConfig {
		cfg = "1"
	}
	h := sha256.Sum256([]byte(gameID + "\x00" + slotLabel + "\x00" + cfg))
	return hex.EncodeToString(h[:])[:16]
}

// migLegacyKey computes the pre-2.0 path key for a rule, matching saverule.RuleKey when
// SlotLabel was empty. The rule's SlotLabel is zeroed before marshaling so the JSON
// matches what old clients produced.
func migLegacyKey(gameID string, rule types.SaveRule) string {
	rule.SlotLabel = ""
	payload, _ := json.Marshal(struct {
		GameID string `json:"game_id"`
		types.SaveRule
	}{GameID: gameID, SaveRule: rule})
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])[:16]
}

// migVersionRow is a row from save_versions used during merge.
type migVersionRow struct {
	version     int
	content     []byte
	updatedAt   string
	contentHash string
}

// migLoadVersions reads all save_versions rows for a single (user, game, path_key) slot.
func migLoadVersions(tx *sql.Tx, userID, gameID, pathKey string) ([]migVersionRow, error) {
	rows, err := tx.Query(
		`SELECT version, content, updated_at, COALESCE(content_hash,'')
		 FROM save_versions WHERE user_id=? AND game_id=? AND path_key=? ORDER BY version ASC`,
		userID, gameID, pathKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []migVersionRow
	for rows.Next() {
		var v migVersionRow
		if err := rows.Scan(&v.version, &v.content, &v.updatedAt, &v.contentHash); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// migReadSaveContent reads a save's content from the DB BLOB or, if stored on the
// filesystem (storage_path is non-empty), from disk.
func migReadSaveContent(tx *sql.Tx, userID, gameID, pathKey string) ([]byte, error) {
	var content []byte
	var storagePath sql.NullString
	if err := tx.QueryRow(
		`SELECT content, storage_path FROM saves WHERE user_id=? AND game_id=? AND path_key=?`,
		userID, gameID, pathKey).Scan(&content, &storagePath); err != nil {
		return nil, err
	}
	if storagePath.Valid && storagePath.String != "" && len(content) == 0 {
		data, err := os.ReadFile(storagePath.String)
		if err != nil {
			return nil, fmt.Errorf("read blob %s: %w", storagePath.String, err)
		}
		return data, nil
	}
	return content, nil
}

// migRenameBlobFile moves a save blob from its current absolute path to the canonical
// path for the new path_key under saveRoot. Returns the new absolute path on success.
func migRenameBlobFile(saveRoot, userID, gameID, oldPath, newPathKey string) (string, error) {
	newRel := filepath.Join(gameID, newPathKey)
	newAbs, err := savepath.JoinUserGamePath(saveRoot, userID, gameID, newRel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o750); err != nil {
		return "", err
	}
	return newAbs, os.Rename(oldPath, newAbs)
}

// stepMergeOSSlots is migration step 16.
//
// For every (user_id, game_id) pair, it finds saves rows that were stored under
// different per-OS path_keys (pre-2.0) but now map to the same OS-neutral path_key
// (because the underlying PCGW rules share a SlotLabel). The rows are collapsed:
// the newest row becomes the survivor; older rows are preserved in save_versions.
//
// If GSBS_DRY_RUN_MIGRATION=1 is set, the step logs what it would do and returns
// errMigDryRun, which causes runMigrationStep to roll back without advancing the
// schema version. Remove the env var and restart to apply for real.
func (s *sqliteStore) stepMergeOSSlots(tx *sql.Tx) error {
	dryRun := os.Getenv("GSBS_DRY_RUN_MIGRATION") == "1"

	// ── 1. Build remap table: (game_id+":"+old_path_key) → new_path_key ────────
	// We query every distinct (game_id, save_rules_json) combination. For each rule
	// that has a SlotLabel, we compute both the legacy key (pre-2.0, no SlotLabel)
	// and the new OS-neutral key. Rows whose path_key matches an old key need to be
	// re-keyed; rows that already carry the new key are left alone.
	remap := make(map[string]string) // "gameID:oldKey" → newKey

	locRows, err := tx.Query(
		`SELECT DISTINCT game_id, save_rules_json FROM game_save_locations
		 WHERE save_rules_json IS NOT NULL AND save_rules_json != ''`)
	if err != nil {
		return fmt.Errorf("stepMergeOSSlots: query locations: %w", err)
	}
	for locRows.Next() {
		var gameID, rulesJSON string
		if err := locRows.Scan(&gameID, &rulesJSON); err != nil {
			locRows.Close()
			return fmt.Errorf("stepMergeOSSlots: scan location: %w", err)
		}
		var rules []types.SaveRule
		if json.Unmarshal([]byte(rulesJSON), &rules) != nil {
			continue
		}
		for _, rule := range rules {
			if rule.SlotLabel == "" {
				continue // user-defined rule — key already stable, nothing to remap
			}
			newKey := migSlotKey(gameID, rule.SlotLabel, rule.IsConfig)
			oldKey := migLegacyKey(gameID, rule)
			if oldKey == newKey {
				continue
			}
			k := gameID + ":" + oldKey
			if prev, ok := remap[k]; ok && prev != newKey {
				logx.Logger().Warn().Str("component", "migration").Str("game_id", gameID).
					Str("old_key", oldKey).Str("prev", prev).Str("new_key", newKey).
					Msg("GSBS migrate step 16: conflicting remap (keeping first)")
				continue
			}
			remap[k] = newKey
		}
	}
	locRows.Close()
	if err := locRows.Err(); err != nil {
		return fmt.Errorf("stepMergeOSSlots: locations iter: %w", err)
	}

	if len(remap) == 0 {
		logx.Logger().Info().Str("component", "migration").
			Msg("GSBS migrate step 16: no PCGW slot labels found — step is a no-op")
		return nil
	}

	// ── 2. Load saves metadata ────────────────────────────────────────────────
	type savesMeta struct {
		userID, gameID, pathKey string
		updatedAt               string
		contentHash             string
		contentSize             int64
		encrypted               int
		storagePath             string
	}

	svRows, err := tx.Query(
		`SELECT user_id, game_id, path_key, updated_at,
		        COALESCE(content_hash,''), COALESCE(content_size,0),
		        COALESCE(encrypted,0), COALESCE(storage_path,'')
		 FROM saves ORDER BY user_id, game_id, updated_at ASC`)
	if err != nil {
		return fmt.Errorf("stepMergeOSSlots: query saves: %w", err)
	}
	var allSaves []savesMeta
	for svRows.Next() {
		var m savesMeta
		if err := svRows.Scan(&m.userID, &m.gameID, &m.pathKey, &m.updatedAt,
			&m.contentHash, &m.contentSize, &m.encrypted, &m.storagePath); err != nil {
			svRows.Close()
			return fmt.Errorf("stepMergeOSSlots: scan save: %w", err)
		}
		allSaves = append(allSaves, m)
	}
	svRows.Close()
	if err := svRows.Err(); err != nil {
		return fmt.Errorf("stepMergeOSSlots: saves iter: %w", err)
	}

	if len(allSaves) == 0 {
		logx.Logger().Info().Str("component", "migration").
			Msg("GSBS migrate step 16: saves table is empty — step is a no-op")
		return nil
	}

	// ── 3. Group by (user_id, game_id, target_path_key) ─────────────────────
	type groupKey struct{ userID, gameID, targetKey string }
	groups := make(map[groupKey][]savesMeta)
	for _, m := range allSaves {
		targetKey := m.pathKey
		if newKey, ok := remap[m.gameID+":"+m.pathKey]; ok {
			targetKey = newKey
		}
		gk := groupKey{m.userID, m.gameID, targetKey}
		groups[gk] = append(groups[gk], m)
	}

	// ── 4. Process each group ────────────────────────────────────────────────
	var totalMerged, totalVersions, totalUpdated int
	perGameMerge := make(map[string]int) // for dry-run report

	for gk, group := range groups {
		// Check if anything needs to change.
		if len(group) == 1 && group[0].pathKey == gk.targetKey {
			continue // already at correct key, nothing to do
		}

		if dryRun {
			if len(group) > 1 {
				totalMerged++
				totalVersions += len(group) - 1
				perGameMerge[gk.gameID]++
			} else {
				totalUpdated++
			}
			continue
		}

		if len(group) == 1 {
			// ── Simple re-key (no collision) ──────────────────────────────
			m := group[0]
			if _, err := tx.Exec(
				`UPDATE save_versions SET path_key=? WHERE user_id=? AND game_id=? AND path_key=?`,
				gk.targetKey, m.userID, m.gameID, m.pathKey); err != nil {
				return fmt.Errorf("stepMergeOSSlots: update version path_key: %w", err)
			}
			newRelPath := filepath.Join(m.gameID, gk.targetKey)
			newStoragePath := m.storagePath
			if m.storagePath != "" && s.saveRoot != "" {
				if abs, err := migRenameBlobFile(s.saveRoot, m.userID, m.gameID, m.storagePath, gk.targetKey); err == nil {
					newStoragePath = abs
				} else {
					logx.Logger().Warn().Str("component", "migration").Err(err).
						Msg("GSBS migrate step 16: rename blob (non-fatal)")
				}
			}
			if _, err := tx.Exec(
				`UPDATE saves SET path_key=?, relative_path=?, storage_path=? WHERE user_id=? AND game_id=? AND path_key=?`,
				gk.targetKey, newRelPath, nullIfEmpty(newStoragePath), m.userID, m.gameID, m.pathKey); err != nil {
				return fmt.Errorf("stepMergeOSSlots: update save path_key: %w", err)
			}
			totalUpdated++
			continue
		}

		// ── Merge: pick survivor (newest updated_at), archive losers ─────
		sort.Slice(group, func(i, j int) bool {
			return group[i].updatedAt > group[j].updatedAt // descending — [0] is newest
		})
		survivor := group[0]
		losers := group[1:]

		// Determine the highest version number already in use for any path_key in this group.
		maxVersion := 0
		for _, m := range group {
			var v int
			if scanErr := tx.QueryRow(
				`SELECT COALESCE(MAX(version),0) FROM save_versions WHERE user_id=? AND game_id=? AND path_key=?`,
				m.userID, m.gameID, m.pathKey).Scan(&v); scanErr == nil && v > maxVersion {
				maxVersion = v
			}
		}

		// Archive each loser: move its existing history + current content under target key.
		for _, loser := range losers {
			// Re-key any existing save_versions for this loser.
			loserVers, err := migLoadVersions(tx, loser.userID, loser.gameID, loser.pathKey)
			if err != nil {
				return fmt.Errorf("stepMergeOSSlots: load loser versions game=%s key=%s: %w", loser.gameID, loser.pathKey, err)
			}
			if _, err := tx.Exec(
				`DELETE FROM save_versions WHERE user_id=? AND game_id=? AND path_key=?`,
				loser.userID, loser.gameID, loser.pathKey); err != nil {
				return fmt.Errorf("stepMergeOSSlots: delete loser versions: %w", err)
			}
			for _, lv := range loserVers {
				maxVersion++
				if _, err := tx.Exec(
					`INSERT INTO save_versions (user_id,game_id,path_key,version,content,updated_at,content_hash) VALUES (?,?,?,?,?,?,?)`,
					gk.userID, gk.gameID, gk.targetKey, maxVersion, lv.content, lv.updatedAt, nullIfEmpty(lv.contentHash)); err != nil {
					return fmt.Errorf("stepMergeOSSlots: insert loser history version: %w", err)
				}
			}

			// Preserve loser's current content as the next version.
			content, err := migReadSaveContent(tx, loser.userID, loser.gameID, loser.pathKey)
			if err != nil {
				return fmt.Errorf("stepMergeOSSlots: read loser content game=%s key=%s: %w", loser.gameID, loser.pathKey, err)
			}
			maxVersion++
			if _, err := tx.Exec(
				`INSERT INTO save_versions (user_id,game_id,path_key,version,content,updated_at,content_hash) VALUES (?,?,?,?,?,?,?)`,
				gk.userID, gk.gameID, gk.targetKey, maxVersion, content, loser.updatedAt, nullIfEmpty(loser.contentHash)); err != nil {
				return fmt.Errorf("stepMergeOSSlots: insert loser current as version: %w", err)
			}

			// Remove loser from saves (and clean up its FS blob if present).
			if _, err := tx.Exec(
				`DELETE FROM saves WHERE user_id=? AND game_id=? AND path_key=?`,
				loser.userID, loser.gameID, loser.pathKey); err != nil {
				return fmt.Errorf("stepMergeOSSlots: delete loser save: %w", err)
			}
			if loser.storagePath != "" {
				_ = os.Remove(loser.storagePath)
			}
			totalVersions++
		}

		// Re-key survivor's existing save_versions to target key (if needed).
		if survivor.pathKey != gk.targetKey {
			survVers, err := migLoadVersions(tx, survivor.userID, survivor.gameID, survivor.pathKey)
			if err != nil {
				return fmt.Errorf("stepMergeOSSlots: load survivor versions: %w", err)
			}
			if _, err := tx.Exec(
				`DELETE FROM save_versions WHERE user_id=? AND game_id=? AND path_key=?`,
				survivor.userID, survivor.gameID, survivor.pathKey); err != nil {
				return fmt.Errorf("stepMergeOSSlots: delete survivor old versions: %w", err)
			}
			for _, sv := range survVers {
				maxVersion++
				if _, err := tx.Exec(
					`INSERT INTO save_versions (user_id,game_id,path_key,version,content,updated_at,content_hash) VALUES (?,?,?,?,?,?,?)`,
					gk.userID, gk.gameID, gk.targetKey, maxVersion, sv.content, sv.updatedAt, nullIfEmpty(sv.contentHash)); err != nil {
					return fmt.Errorf("stepMergeOSSlots: insert survivor history: %w", err)
				}
			}

			// Update survivor's path_key in saves.
			newRelPath := filepath.Join(survivor.gameID, gk.targetKey)
			newStoragePath := survivor.storagePath
			if survivor.storagePath != "" && s.saveRoot != "" {
				if abs, err := migRenameBlobFile(s.saveRoot, survivor.userID, survivor.gameID, survivor.storagePath, gk.targetKey); err == nil {
					newStoragePath = abs
				} else {
					logx.Logger().Warn().Str("component", "migration").Err(err).
						Msg("GSBS migrate step 16: rename survivor blob (non-fatal)")
				}
			}
			if _, err := tx.Exec(
				`UPDATE saves SET path_key=?, relative_path=?, storage_path=? WHERE user_id=? AND game_id=? AND path_key=?`,
				gk.targetKey, newRelPath, nullIfEmpty(newStoragePath), survivor.userID, survivor.gameID, survivor.pathKey); err != nil {
				return fmt.Errorf("stepMergeOSSlots: update survivor path_key: %w", err)
			}
		}

		totalMerged++
		perGameMerge[gk.gameID]++
	}

	if dryRun {
		for gameID, n := range perGameMerge {
			logx.Logger().Info().Str("component", "migration").Str("game_id", gameID).Int("slots", n).
				Msg("GSBS migrate step 16 (dry-run): game would merge slot(s)")
		}
		logx.Logger().Info().Str("component", "migration").
			Int("merged", totalMerged).Int("versions", totalVersions).Int("updated", totalUpdated).
			Msg("GSBS migrate step 16 (dry-run): summary")
		return errMigDryRun
	}

	logx.Logger().Info().Str("component", "migration").
		Int("merged", totalMerged).Int("versions", totalVersions).Int("updated", totalUpdated).
		Msg("GSBS migrate step 16: merged slot(s)")
	return nil
}

// stepPCGWIncrementalSpeed is migration step 18.
// Adds catalog_scan_mode to pcgw_sync_runs and last_rev_check_at to pcgw_manifest_meta
// to support the fast catalog probe / tail scan optimisation.
func (s *sqliteStore) stepPCGWIncrementalSpeed(tx *sql.Tx) error {
	for _, col := range []string{
		`ALTER TABLE pcgw_sync_runs ADD COLUMN catalog_scan_mode TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE pcgw_manifest_meta ADD COLUMN last_rev_check_at TEXT`,
	} {
		if _, err := tx.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate") {
			return fmt.Errorf("step pcgw_incremental_speed: %w", err)
		}
	}
	return nil
}

// stepPCGWCatalog is migration step 17.
// Creates the pcgw_catalog table and extends pcgw_sync_runs with two-phase fields.
func (s *sqliteStore) stepPCGWCatalog(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS pcgw_catalog (
			page_id INTEGER PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			last_seen_run_id TEXT NOT NULL DEFAULT '',
			last_seen_rev_id INTEGER NOT NULL DEFAULT 0,
			dead_letter INTEGER NOT NULL DEFAULT 0,
			dead_letter_reason TEXT NOT NULL DEFAULT '',
			retry_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_catalog_dead_letter ON pcgw_catalog(dead_letter)`,
		`CREATE INDEX IF NOT EXISTS idx_pcgw_catalog_last_seen ON pcgw_catalog(last_seen_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("step pcgw_catalog create: %w", err)
		}
	}
	for _, col := range []string{
		`ALTER TABLE pcgw_sync_runs ADD COLUMN remote_total_ids INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE pcgw_sync_runs ADD COLUMN missing_local_ids INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE pcgw_sync_runs ADD COLUMN extra_local_ids INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE pcgw_sync_runs ADD COLUMN targeted_queue_size INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE pcgw_sync_runs ADD COLUMN targeted_processed INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE pcgw_sync_runs ADD COLUMN phase1_completed_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE pcgw_sync_runs ADD COLUMN catalog_hash TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE pcgw_sync_runs ADD COLUMN checkpoint_phase TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE pcgw_sync_runs ADD COLUMN checkpoint_queue_cursor INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := tx.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate") {
			return fmt.Errorf("step pcgw_catalog alter: %w", err)
		}
	}
	return nil
}

// stepPCGWBundleSettings seeds admin settings for the prebuilt manifest bundle
// (S3) sync. Fresh installs default to S3; an install that already has crawled
// PCGW data defaults to manual API so it does not get overwritten by a bundle.
func (s *sqliteStore) stepPCGWBundleSettings(tx *sql.Tx) error {
	var gameCount int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM pcgw_games`).Scan(&gameCount)
	syncSource := PCGWSyncSourceS3
	if gameCount > 0 {
		syncSource = PCGWSyncSourceAPI
	}
	defaults := map[string]string{
		AdminSettingPCGWSyncSource:          syncSource,
		AdminSettingPCGWBundleURL:           DefaultPCGWBundleURL,
		AdminSettingPCGWBundleCron:          DefaultPCGWBundleCron,
		AdminSettingPCGWBundleIncrementalFB: "false",
	}
	for k, v := range defaults {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO admin_settings (key, value) VALUES (?, ?)`, k, v); err != nil {
			return err
		}
	}
	return nil
}

// stepPCGWSyncSourceS3 migrates existing installs from the legacy "github"
// bundle-source value to the canonical "s3". The "api" (manual) value and any
// stale delta-URL setting are left untouched (the latter is simply ignored now).
func (s *sqliteStore) stepPCGWSyncSourceS3(tx *sql.Tx) error {
	_, err := tx.Exec(
		`UPDATE admin_settings SET value = ? WHERE key = ? AND value = ?`,
		PCGWSyncSourceS3, AdminSettingPCGWSyncSource, PCGWSyncSourceGitHub,
	)
	return err
}

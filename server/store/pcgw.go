package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
)

func (s *sqliteStore) migratePCGW() error {
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
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate pcgw: %w", err)
		}
	}

	// FTS5 virtual table (optional — some SQLite builds lack fts5)
	ftsOK := false
	if _, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS pcgw_games_fts USING fts5(
		title, page_name, content='pcgw_games', content_rowid='page_id'
	)`); err == nil {
		ftsOK = true
	}

	// Backfill from existing manifest rows (before triggers so inserts don't fire FTS yet)
	if _, err := s.db.Exec(`
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
			if _, err := s.db.Exec(t); err != nil {
				return fmt.Errorf("migrate pcgw fts trigger: %w", err)
			}
		}
		if _, err := s.db.Exec(`INSERT INTO pcgw_games_fts(pcgw_games_fts) VALUES('rebuild')`); err != nil {
			return fmt.Errorf("migrate pcgw fts rebuild: %w", err)
		}
	}
	return nil
}

func pcgwJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func pcgwDecodeJSON(s string, dest interface{}) {
	if s == "" {
		return
	}
	_ = json.Unmarshal([]byte(s), dest)
}

func (s *sqliteStore) UpsertPCGWGame(ctx context.Context, g *types.PCGWGame) error {
	if g == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if g.CreatedAt == "" {
		g.CreatedAt = now
	}
	g.UpdatedAt = now
	if g.ParseStatus == "" {
		g.ParseStatus = "pending"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pcgw_games (
			page_id, page_name, title, is_disambiguation, redirects_to,
			steam_appids, gog_id, epic_id, ubisoft_id, microsoft_id, battlenet_id, itch_id, other_ids,
			developers, publishers, release_dates, engines, taxonomy, infobox,
			cover_url, hltb_id, igdb_id, cargo_last_updated, platforms_present,
			last_rev_id, last_rev_timestamp, last_fetched_at,
			parse_status, parse_error, parse_duration_ms, created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(page_id) DO UPDATE SET
			page_name=excluded.page_name, title=excluded.title,
			is_disambiguation=excluded.is_disambiguation, redirects_to=excluded.redirects_to,
			steam_appids=excluded.steam_appids, gog_id=excluded.gog_id, epic_id=excluded.epic_id,
			ubisoft_id=excluded.ubisoft_id, microsoft_id=excluded.microsoft_id,
			battlenet_id=excluded.battlenet_id, itch_id=excluded.itch_id, other_ids=excluded.other_ids,
			developers=excluded.developers, publishers=excluded.publishers,
			release_dates=excluded.release_dates, engines=excluded.engines,
			taxonomy=excluded.taxonomy, infobox=excluded.infobox,
			cover_url=excluded.cover_url, hltb_id=excluded.hltb_id, igdb_id=excluded.igdb_id,
			cargo_last_updated=excluded.cargo_last_updated, platforms_present=excluded.platforms_present,
			last_rev_id=excluded.last_rev_id, last_rev_timestamp=excluded.last_rev_timestamp,
			last_fetched_at=excluded.last_fetched_at,
			parse_status=excluded.parse_status, parse_error=excluded.parse_error,
			parse_duration_ms=excluded.parse_duration_ms, updated_at=excluded.updated_at`,
		g.PageID, g.PageName, g.Title, boolToInt(g.IsDisambiguation), nullIfEmpty(g.RedirectsTo),
		pcgwJSON(g.SteamAppIDs), nullIfEmpty(g.GOGID), nullIfEmpty(g.EpicID), nullIfEmpty(g.UbisoftID),
		nullIfEmpty(g.MicrosoftID), nullIfEmpty(g.BattlenetID), nullIfEmpty(g.ItchID), pcgwJSON(g.OtherIDs),
		pcgwJSON(g.Developers), pcgwJSON(g.Publishers), pcgwJSON(g.ReleaseDates), pcgwJSON(g.Engines),
		pcgwJSON(g.Taxonomy), pcgwJSON(g.Infobox),
		nullIfEmpty(g.CoverURL), nullIfEmpty(g.HLTBID), nullIfEmpty(g.IGDBID), nullIfEmpty(g.CargoLastUpdated),
		pcgwJSON(g.PlatformsPresent),
		nullInt64(g.LastRevID), nullIfEmpty(g.LastRevTimestamp), nullIfEmpty(g.LastFetchedAt),
		g.ParseStatus, nullIfEmpty(g.ParseError), g.ParseDurationMs, g.CreatedAt, g.UpdatedAt,
	)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullInt64(v int64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func scanPCGWGame(row interface {
	Scan(dest ...interface{}) error
}) (*types.PCGWGame, error) {
	var g types.PCGWGame
	var isDisamb int
	var steamJSON, otherIDs, devs, pubs, dates, engines, tax, infobox, platforms string
	var redirects, gog, epic, ubi, ms, bn, itch, cover, hltb, igdb, cargo, revTS, fetched, parseErr sql.NullString
	var lastRev sql.NullInt64
	err := row.Scan(
		&g.PageID, &g.PageName, &g.Title, &isDisamb, &redirects,
		&steamJSON, &gog, &epic, &ubi, &ms, &bn, &itch, &otherIDs,
		&devs, &pubs, &dates, &engines, &tax, &infobox,
		&cover, &hltb, &igdb, &cargo, &platforms,
		&lastRev, &revTS, &fetched,
		&g.ParseStatus, &parseErr, &g.ParseDurationMs, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	g.IsDisambiguation = isDisamb != 0
	g.RedirectsTo = redirects.String
	pcgwDecodeJSON(steamJSON, &g.SteamAppIDs)
	g.GOGID = gog.String
	g.EpicID = epic.String
	g.UbisoftID = ubi.String
	g.MicrosoftID = ms.String
	g.BattlenetID = bn.String
	g.ItchID = itch.String
	pcgwDecodeJSON(otherIDs, &g.OtherIDs)
	pcgwDecodeJSON(devs, &g.Developers)
	pcgwDecodeJSON(pubs, &g.Publishers)
	pcgwDecodeJSON(dates, &g.ReleaseDates)
	pcgwDecodeJSON(engines, &g.Engines)
	pcgwDecodeJSON(tax, &g.Taxonomy)
	pcgwDecodeJSON(infobox, &g.Infobox)
	g.CoverURL = cover.String
	g.HLTBID = hltb.String
	g.IGDBID = igdb.String
	g.CargoLastUpdated = cargo.String
	pcgwDecodeJSON(platforms, &g.PlatformsPresent)
	if lastRev.Valid {
		g.LastRevID = lastRev.Int64
	}
	g.LastRevTimestamp = revTS.String
	g.LastFetchedAt = fetched.String
	g.ParseError = parseErr.String
	return &g, nil
}

const pcgwGameSelect = `SELECT page_id, page_name, title, is_disambiguation, redirects_to,
	steam_appids, gog_id, epic_id, ubisoft_id, microsoft_id, battlenet_id, itch_id, other_ids,
	developers, publishers, release_dates, engines, taxonomy, infobox,
	cover_url, hltb_id, igdb_id, cargo_last_updated, platforms_present,
	last_rev_id, last_rev_timestamp, last_fetched_at,
	parse_status, parse_error, parse_duration_ms, created_at, updated_at FROM pcgw_games`

func (s *sqliteStore) GetPCGWGame(ctx context.Context, pageID int64) (*types.PCGWGame, error) {
	row := s.db.QueryRowContext(ctx, pcgwGameSelect+` WHERE page_id = ?`, pageID)
	return scanPCGWGame(row)
}

func (s *sqliteStore) GetPCGWGameByPageName(ctx context.Context, pageName string) (*types.PCGWGame, error) {
	row := s.db.QueryRowContext(ctx, pcgwGameSelect+` WHERE page_name = ?`, pageName)
	return scanPCGWGame(row)
}

func (s *sqliteStore) ListPCGWGames(ctx context.Context, filter PCGWGameListFilter) ([]types.PCGWGame, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	var where []string
	var args []interface{}
	if filter.ParseStatus != "" {
		where = append(where, "parse_status = ?")
		args = append(args, filter.ParseStatus)
	}
	if filter.SteamAppID != "" {
		where = append(where, "steam_appids LIKE ?")
		args = append(args, "%"+filter.SteamAppID+"%")
	}
	if filter.UpdatedAfter != "" {
		where = append(where, "updated_at > ?")
		args = append(args, filter.UpdatedAfter)
	}
	if filter.Platform != "" {
		where = append(where, "platforms_present LIKE ?")
		args = append(args, "%"+filter.Platform+"%")
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pcgw_games"+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qargs := append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, pcgwGameSelect+whereSQL+` ORDER BY title LIMIT ? OFFSET ?`, qargs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []types.PCGWGame
	for rows.Next() {
		g, err := scanPCGWGame(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *g)
	}
	return out, total, rows.Err()
}

func (s *sqliteStore) SearchPCGWGamesFTS(ctx context.Context, query string, limit, offset int) ([]types.PCGWGame, int, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return s.ListPCGWGames(ctx, PCGWGameListFilter{Limit: limit, Offset: offset})
	}
	if limit <= 0 {
		limit = 50
	}
	// Try FTS5; fall back to LIKE when fts5 module unavailable
	ftsQ := strings.ReplaceAll(q, `"`, `""`)
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pcgw_games_fts WHERE pcgw_games_fts MATCH ?`, `"`+ftsQ+`"`).Scan(&total)
	if err != nil {
		pattern := "%" + q + "%"
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pcgw_games WHERE title LIKE ? OR page_name LIKE ?`, pattern, pattern).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err := s.db.QueryContext(ctx, pcgwGameSelect+`
			WHERE title LIKE ? OR page_name LIKE ? ORDER BY title LIMIT ? OFFSET ?`, pattern, pattern, limit, offset)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		out, _, err := scanPCGWGameRows(rows)
		return out, total, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.page_id, g.page_name, g.title, g.is_disambiguation, g.redirects_to,
			g.steam_appids, g.gog_id, g.epic_id, g.ubisoft_id, g.microsoft_id, g.battlenet_id, g.itch_id, g.other_ids,
			g.developers, g.publishers, g.release_dates, g.engines, g.taxonomy, g.infobox,
			g.cover_url, g.hltb_id, g.igdb_id, g.cargo_last_updated, g.platforms_present,
			g.last_rev_id, g.last_rev_timestamp, g.last_fetched_at,
			g.parse_status, g.parse_error, g.parse_duration_ms, g.created_at, g.updated_at
		FROM pcgw_games_fts f
		JOIN pcgw_games g ON g.page_id = f.rowid
		WHERE pcgw_games_fts MATCH ?
		ORDER BY rank LIMIT ? OFFSET ?`, `"`+ftsQ+`"`, limit, offset)
	if err != nil {
		return s.ListPCGWGames(ctx, PCGWGameListFilter{Limit: limit, Offset: offset})
	}
	defer rows.Close()
	return scanPCGWGameRows(rows, total)
}

func scanPCGWGameRows(rows *sql.Rows, totalOpt ...int) ([]types.PCGWGame, int, error) {
	var out []types.PCGWGame
	for rows.Next() {
		g, err := scanPCGWGame(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *g)
	}
	total := len(out)
	if len(totalOpt) > 0 {
		total = totalOpt[0]
	}
	return out, total, rows.Err()
}

func (s *sqliteStore) UpdatePCGWGameSyncState(ctx context.Context, pageID int64, revID int64, revTS, status, parseErr string, durationMs int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE pcgw_games SET last_rev_id=?, last_rev_timestamp=?, last_fetched_at=?,
			parse_status=?, parse_error=?, parse_duration_ms=?, updated_at=?
		WHERE page_id=?`, revID, revTS, now, status, nullIfEmpty(parseErr), durationMs, now, pageID)
	return err
}

func (s *sqliteStore) UpsertPCGWGameData(ctx context.Context, row *types.PCGWGameData) error {
	if row == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if row.UpdatedAt == "" {
		row.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pcgw_game_data (
			page_id, platform_key, platform_raw_label, save_locations, config_locations,
			save_game_cloud_sync, install_locations, registry_keys, save_file_info,
			all_templates, section_wikitext, structured, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(page_id, platform_key) DO UPDATE SET
			platform_raw_label=excluded.platform_raw_label,
			save_locations=excluded.save_locations, config_locations=excluded.config_locations,
			save_game_cloud_sync=excluded.save_game_cloud_sync, install_locations=excluded.install_locations,
			registry_keys=excluded.registry_keys, save_file_info=excluded.save_file_info,
			all_templates=excluded.all_templates, section_wikitext=excluded.section_wikitext,
			structured=excluded.structured, updated_at=excluded.updated_at`,
		row.PageID, row.PlatformKey, nullIfEmpty(row.PlatformRawLabel),
		pcgwJSON(row.SaveLocations), pcgwJSON(row.ConfigLocations),
		pcgwJSON(row.SaveGameCloudSync), pcgwJSON(row.InstallLocations), pcgwJSON(row.RegistryKeys),
		pcgwJSON(row.SaveFileInfo), pcgwJSON(row.AllTemplates), nullIfEmpty(row.SectionWikitext),
		pcgwJSON(row.Structured), row.UpdatedAt,
	)
	return err
}

func (s *sqliteStore) DeletePCGWGameDataExcept(ctx context.Context, pageID int64, keepPlatformKeys []string) error {
	if len(keepPlatformKeys) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM pcgw_game_data WHERE page_id = ?`, pageID)
		return err
	}
	placeholders := strings.Repeat("?,", len(keepPlatformKeys))
	placeholders = placeholders[:len(placeholders)-1]
	args := []interface{}{pageID}
	for _, k := range keepPlatformKeys {
		args = append(args, k)
	}
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM pcgw_game_data WHERE page_id = ? AND platform_key NOT IN (%s)`, placeholders), args...)
	return err
}

func (s *sqliteStore) ListPCGWGameData(ctx context.Context, pageID int64) ([]types.PCGWGameData, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, platform_key, platform_raw_label, save_locations, config_locations,
			save_game_cloud_sync, install_locations, registry_keys, save_file_info,
			all_templates, section_wikitext, structured, updated_at
		FROM pcgw_game_data WHERE page_id = ? ORDER BY platform_key`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.PCGWGameData
	for rows.Next() {
		var row types.PCGWGameData
		var saves, configs, cloud, installs, reg, fileInfo, templates, structured string
		var rawLabel, wiki sql.NullString
		if err := rows.Scan(&row.PageID, &row.PlatformKey, &rawLabel, &saves, &configs, &cloud, &installs, &reg, &fileInfo, &templates, &wiki, &structured, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.PlatformRawLabel = rawLabel.String
		row.SectionWikitext = wiki.String
		pcgwDecodeJSON(saves, &row.SaveLocations)
		pcgwDecodeJSON(configs, &row.ConfigLocations)
		pcgwDecodeJSON(cloud, &row.SaveGameCloudSync)
		pcgwDecodeJSON(installs, &row.InstallLocations)
		pcgwDecodeJSON(reg, &row.RegistryKeys)
		pcgwDecodeJSON(fileInfo, &row.SaveFileInfo)
		pcgwDecodeJSON(templates, &row.AllTemplates)
		pcgwDecodeJSON(structured, &row.Structured)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *sqliteStore) upsertPCGWSection(ctx context.Context, table string, row *types.PCGWSectionRow) error {
	if row == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if row.UpdatedAt == "" {
		row.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (page_id, data, all_templates, section_wikitext, updated_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(page_id) DO UPDATE SET
			data=excluded.data, all_templates=excluded.all_templates,
			section_wikitext=excluded.section_wikitext, updated_at=excluded.updated_at`, table),
		row.PageID, pcgwJSON(row.Data), pcgwJSON(row.AllTemplates), nullIfEmpty(row.SectionWikitext), row.UpdatedAt)
	return err
}

func (s *sqliteStore) UpsertPCGWAvailability(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_availability", row)
}
func (s *sqliteStore) UpsertPCGWMonetization(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_monetization", row)
}
func (s *sqliteStore) UpsertPCGWVideo(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_video", row)
}
func (s *sqliteStore) UpsertPCGWInput(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_input", row)
}
func (s *sqliteStore) UpsertPCGWAudio(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_audio", row)
}
func (s *sqliteStore) UpsertPCGWNetwork(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_network", row)
}
func (s *sqliteStore) UpsertPCGWOther(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_other", row)
}
func (s *sqliteStore) UpsertPCGWNotes(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_notes", row)
}
func (s *sqliteStore) UpsertPCGWReferences(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_references", row)
}
func (s *sqliteStore) UpsertPCGWExternalLinks(ctx context.Context, row *types.PCGWSectionRow) error {
	return s.upsertPCGWSection(ctx, "pcgw_external_links", row)
}

var pcgwSectionTables = map[string]string{
	"availability":   "pcgw_availability",
	"monetization":   "pcgw_monetization",
	"video":          "pcgw_video",
	"input":          "pcgw_input",
	"audio":          "pcgw_audio",
	"network":        "pcgw_network",
	"other":          "pcgw_other",
	"notes":          "pcgw_notes",
	"references":     "pcgw_references",
	"external_links": "pcgw_external_links",
}

func (s *sqliteStore) GetPCGWSection(ctx context.Context, pageID int64, section string) (*types.PCGWSectionRow, error) {
	table, ok := pcgwSectionTables[strings.ToLower(section)]
	if !ok {
		return nil, fmt.Errorf("unknown section %q", section)
	}
	var row types.PCGWSectionRow
	var data, templates string
	var wiki sql.NullString
	row.PageID = pageID
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT data, all_templates, section_wikitext, updated_at FROM %s WHERE page_id = ?`, table), pageID).
		Scan(&data, &templates, &wiki, &row.UpdatedAt)
	if err != nil {
		return nil, err
	}
	row.SectionWikitext = wiki.String
	pcgwDecodeJSON(data, &row.Data)
	pcgwDecodeJSON(templates, &row.AllTemplates)
	return &row, nil
}

func (s *sqliteStore) ReplacePCGWSystemRequirements(ctx context.Context, pageID int64, rows []types.PCGWSystemRequirement) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM pcgw_system_requirements WHERE page_id = ?`, pageID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, r := range rows {
		up := r.UpdatedAt
		if up == "" {
			up = now
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pcgw_system_requirements (page_id, platform_key, requirement_type, specs, section_wikitext, updated_at)
			VALUES (?,?,?,?,?,?)`, pageID, r.PlatformKey, r.RequirementType, pcgwJSON(r.Specs), nullIfEmpty(r.SectionWikitext), up); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) ListPCGWSystemRequirements(ctx context.Context, pageID int64) ([]types.PCGWSystemRequirement, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, platform_key, requirement_type, specs, section_wikitext, updated_at
		FROM pcgw_system_requirements WHERE page_id = ?`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.PCGWSystemRequirement
	for rows.Next() {
		var r types.PCGWSystemRequirement
		var specs string
		var wiki sql.NullString
		if err := rows.Scan(&r.PageID, &r.PlatformKey, &r.RequirementType, &specs, &wiki, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.SectionWikitext = wiki.String
		pcgwDecodeJSON(specs, &r.Specs)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpsertPCGWMetadata(ctx context.Context, m *types.PCGWMetadata) error {
	if m == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if m.LastFetchedAt == "" {
		m.LastFetchedAt = now
	}
	var blob interface{}
	if len(m.FullWikitextZstd) > 0 {
		blob = m.FullWikitextZstd
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pcgw_metadata (page_id, full_wikitext_zstd, content_hash, section_hashes, parsed_sections, uncompressed_size, last_fetched_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(page_id) DO UPDATE SET
			full_wikitext_zstd=excluded.full_wikitext_zstd,
			content_hash=excluded.content_hash,
			section_hashes=excluded.section_hashes,
			parsed_sections=excluded.parsed_sections,
			uncompressed_size=excluded.uncompressed_size,
			last_fetched_at=excluded.last_fetched_at`,
		m.PageID, blob, nullIfEmpty(m.ContentHash), pcgwJSON(m.SectionHashes), pcgwJSON(m.ParsedSections),
		m.UncompressedSize, m.LastFetchedAt)
	if err == nil && m.UncompressedSize > 0 {
		_, _ = s.db.ExecContext(ctx, `
			UPDATE pcgw_manifest_meta SET db_wikitext_bytes = (
				SELECT COALESCE(SUM(uncompressed_size), 0) FROM pcgw_metadata
			) WHERE id = 1`)
	}
	return err
}

func (s *sqliteStore) GetPCGWMetadata(ctx context.Context, pageID int64) (*types.PCGWMetadata, error) {
	var m types.PCGWMetadata
	var blob []byte
	var hash sql.NullString
	var sections, parsed string
	err := s.db.QueryRowContext(ctx, `
		SELECT page_id, full_wikitext_zstd, content_hash, section_hashes, parsed_sections, uncompressed_size, last_fetched_at
		FROM pcgw_metadata WHERE page_id = ?`, pageID).
		Scan(&m.PageID, &blob, &hash, &sections, &parsed, &m.UncompressedSize, &m.LastFetchedAt)
	if err != nil {
		return nil, err
	}
	m.FullWikitextZstd = blob
	m.ContentHash = hash.String
	pcgwDecodeJSON(sections, &m.SectionHashes)
	pcgwDecodeJSON(parsed, &m.ParsedSections)
	return &m, nil
}

func (s *sqliteStore) GetPCGWContentHash(ctx context.Context, pageID int64) (string, map[string]string, error) {
	var hash sql.NullString
	var sections string
	err := s.db.QueryRowContext(ctx, `SELECT content_hash, section_hashes FROM pcgw_metadata WHERE page_id = ?`, pageID).
		Scan(&hash, &sections)
	if err == sql.ErrNoRows {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	sh := map[string]string{}
	pcgwDecodeJSON(sections, &sh)
	return hash.String, sh, nil
}

func (s *sqliteStore) PurgePCGWFullWikitext(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE pcgw_metadata SET full_wikitext_zstd = NULL`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	_, _ = s.db.ExecContext(ctx, `UPDATE pcgw_manifest_meta SET db_wikitext_bytes = 0 WHERE id = 1`)
	return n, nil
}

func (s *sqliteStore) InsertPCGWParseFailure(ctx context.Context, f *types.PCGWParseFailure) error {
	if f == nil {
		return nil
	}
	id := f.ID
	if id == "" {
		var err error
		id, err = genID()
		if err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if f.CreatedAt == "" {
		f.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pcgw_parse_failures (id, page_id, sync_run_id, section, error_message, wikitext_snippet, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		id, f.PageID, nullIfEmpty(f.SyncRunID), f.Section, f.ErrorMessage, nullIfEmpty(f.WikitextSnippet), f.CreatedAt)
	return err
}

func (s *sqliteStore) ListPCGWParseFailures(ctx context.Context, pageID int64, limit int) ([]types.PCGWParseFailure, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, page_id, sync_run_id, section, error_message, wikitext_snippet, created_at
		FROM pcgw_parse_failures WHERE page_id = ? ORDER BY created_at DESC LIMIT ?`, pageID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.PCGWParseFailure
	for rows.Next() {
		var f types.PCGWParseFailure
		var runID, snippet sql.NullString
		if err := rows.Scan(&f.ID, &f.PageID, &runID, &f.Section, &f.ErrorMessage, &snippet, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.SyncRunID = runID.String
		f.WikitextSnippet = snippet.String
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *sqliteStore) StartPCGWSyncRun(ctx context.Context, mode string) (string, error) {
	id, err := genID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO pcgw_sync_runs (id, mode, status, started_at) VALUES (?,?,'running',?)`, id, mode, now)
	return id, err
}

func (s *sqliteStore) UpdatePCGWSyncRunCheckpoint(ctx context.Context, runID string, offset int, stats PCGWSyncRunStats) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE pcgw_sync_runs SET checkpoint_offset=?, games_total=?, games_ok=?, games_partial=?,
			games_failed=?, games_skipped=?, avg_parse_ms=? WHERE id=?`,
		offset, stats.GamesTotal, stats.GamesOK, stats.GamesPartial, stats.GamesFailed, stats.GamesSkipped, stats.AvgParseMs, runID)
	return err
}

func (s *sqliteStore) FinishPCGWSyncRun(ctx context.Context, runID, status, errMsg string, stats PCGWSyncRunStats) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE pcgw_sync_runs SET status=?, finished_at=?, error_message=?,
			games_total=?, games_ok=?, games_partial=?, games_failed=?, games_skipped=?, avg_parse_ms=?
		WHERE id=?`,
		status, now, nullIfEmpty(errMsg), stats.GamesTotal, stats.GamesOK, stats.GamesPartial, stats.GamesFailed, stats.GamesSkipped, stats.AvgParseMs, runID)
	return err
}

func (s *sqliteStore) GetLatestPCGWSyncRun(ctx context.Context) (*types.PCGWSyncRun, error) {
	var r types.PCGWSyncRun
	var finished, errMsg sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, mode, status, started_at, finished_at, checkpoint_offset,
			games_total, games_ok, games_partial, games_failed, games_skipped, avg_parse_ms, error_message
		FROM pcgw_sync_runs ORDER BY started_at DESC LIMIT 1`).
		Scan(&r.ID, &r.Mode, &r.Status, &r.StartedAt, &finished, &r.CheckpointOffset,
			&r.GamesTotal, &r.GamesOK, &r.GamesPartial, &r.GamesFailed, &r.GamesSkipped, &r.AvgParseMs, &errMsg)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.FinishedAt = finished.String
	r.ErrorMessage = errMsg.String
	return &r, nil
}

func (s *sqliteStore) GetPCGWManifestMeta(ctx context.Context) (*types.PCGWManifestMeta, error) {
	var m types.PCGWManifestMeta
	var inc, full sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT manifest_version, manifest_etag, last_incremental_at, last_full_sync_at, db_wikitext_bytes
		FROM pcgw_manifest_meta WHERE id = 1`).
		Scan(&m.ManifestVersion, &m.ManifestETag, &inc, &full, &m.DBWikitextBytes)
	if err != nil {
		return nil, err
	}
	m.LastIncrementalAt = inc.String
	m.LastFullSyncAt = full.String
	return &m, nil
}

func (s *sqliteStore) BumpManifestVersion(ctx context.Context, newETag string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE pcgw_manifest_meta SET manifest_version = manifest_version + 1, manifest_etag = ?, last_incremental_at = ? WHERE id = 1`,
		newETag, now)
	if err != nil {
		return 0, err
	}
	var v int
	err = s.db.QueryRowContext(ctx, `SELECT manifest_version FROM pcgw_manifest_meta WHERE id = 1`).Scan(&v)
	return v, err
}

func (s *sqliteStore) ReplaceGameSaveLocationsForGame(ctx context.Context, gameID string, entries []types.GameSaveLocation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM game_save_locations WHERE game_id = ?`, gameID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range entries {
		id, err := genID()
		if err != nil {
			return err
		}
		isConfig := 0
		if e.IsConfig {
			isConfig = 1
		}
		if e.UpdatedAt == "" {
			e.UpdatedAt = now
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO game_save_locations (id, game_id, pcgw_page_id, game_title, platform, path_template, is_config, updated_at, source, notes, steam_app_ids, gog_id, epic_id, ubisoft_id)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, e.GameID, e.PCGWPageID, e.GameTitle, e.Platform, e.PathTemplate, isConfig, e.UpdatedAt, e.Source, e.Notes,
			encodeSteamAppIDs(e.SteamAppIDs), nullIfEmpty(e.GOGID), nullIfEmpty(e.EpicID), nullIfEmpty(e.UbisoftID)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func deriveProtonSupport(platforms []string, gameData []types.PCGWGameData) string {
	for _, p := range platforms {
		if p == "linux" || p == "steam_play" {
			for _, gd := range gameData {
				if gd.PlatformKey == "linux" || gd.PlatformKey == "steam_play" {
					return "supported"
				}
			}
		}
	}
	for _, gd := range gameData {
		if strings.Contains(strings.ToLower(gd.PlatformRawLabel), "steam play") {
			return "supported"
		}
	}
	if len(gameData) > 0 {
		return "none"
	}
	return "unknown"
}

func (s *sqliteStore) BuildManifestV2(ctx context.Context, since, platform string, limit, offset int) (*types.ManifestV2Response, error) {
	meta, err := s.GetPCGWManifestMeta(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10000
	}
	where := "parse_status IN ('ok','partial')"
	var args []interface{}
	if since != "" {
		where += " AND updated_at > ?"
		args = append(args, since)
	}
	if platform != "" {
		where += " AND platforms_present LIKE ?"
		args = append(args, "%"+platform+"%")
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, pcgwGameSelect+` WHERE `+where+` ORDER BY title LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	var gameList []*types.PCGWGame
	for rows.Next() {
		g, err := scanPCGWGame(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		gameList = append(gameList, g)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var games []types.ManifestV2Game
	for _, g := range gameList {
		gameData, _ := s.ListPCGWGameData(ctx, g.PageID)
		avail, _ := s.GetPCGWSection(ctx, g.PageID, "availability")

		mg := types.ManifestV2Game{
			GameID:           strconv.FormatInt(g.PageID, 10),
			PageName:         g.PageName,
			Title:            g.Title,
			SteamAppIDs:      g.SteamAppIDs,
			OtherIDs:         g.OtherIDs,
			PlatformsPresent: g.PlatformsPresent,
			Platforms:        g.PlatformsPresent,
			Taxonomy:         g.Taxonomy,
			Engines:          g.Engines,
			CoverURL:         g.CoverURL,
			HLTBID:           g.HLTBID,
			IGDBID:           g.IGDBID,
			ParseStatus:      g.ParseStatus,
			LastUpdated:      g.UpdatedAt,
			GOGID:            g.GOGID,
			EpicID:           g.EpicID,
			UbisoftID:        g.UbisoftID,
			ProtonSupportLevel: deriveProtonSupport(g.PlatformsPresent, gameData),
		}
		var installPaths []string
		cloud := map[string]interface{}{}
		for _, gd := range gameData {
			if platform != "" && gd.PlatformKey != platform {
				continue
			}
			for _, sl := range gd.SaveLocations {
				mg.SaveLocations = append(mg.SaveLocations, types.ManifestV2Location{
					Platform: gd.PlatformKey, PlatformRawLabel: gd.PlatformRawLabel,
					PathTemplates: sl.PathTemplates, IsConfig: false, Notes: sl.Notes,
				})
				if len(sl.PathTemplates) > 0 {
					mg.HasSaveData = true
				}
			}
			for _, cl := range gd.ConfigLocations {
				mg.ConfigLocations = append(mg.ConfigLocations, types.ManifestV2Location{
					Platform: gd.PlatformKey, PlatformRawLabel: gd.PlatformRawLabel,
					PathTemplates: cl.PathTemplates, IsConfig: true, Notes: cl.Notes,
				})
			}
			for _, inst := range gd.InstallLocations {
				if str, ok := inst.(string); ok && str != "" {
					installPaths = append(installPaths, str)
				}
			}
			for k, v := range gd.SaveGameCloudSync {
				cloud[k] = v
			}
		}
		mg.CommonInstallPaths = dedupeStrings(installPaths)
		if len(cloud) > 0 {
			mg.CloudSync = cloud
		}
		if avail != nil && avail.Data != nil {
			mg.AvailabilitySummary = avail.Data
		}
		games = append(games, mg)
	}
	return &types.ManifestV2Response{
		Version:     meta.ManifestVersion,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ETag:        meta.ManifestETag,
		Games:       games,
	}, nil
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (s *sqliteStore) GetPCGWStats(ctx context.Context) (PCGWStats, error) {
	var st PCGWStats
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_games`).Scan(&st.TotalGames)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_games WHERE parse_status='ok'`).Scan(&st.OK)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_games WHERE parse_status='partial'`).Scan(&st.Partial)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_games WHERE parse_status='failed'`).Scan(&st.Failed)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_games WHERE parse_status='pending'`).Scan(&st.Pending)
	run, _ := s.GetLatestPCGWSyncRun(ctx)
	if run != nil {
		st.LastSyncAt = run.FinishedAt
		if st.LastSyncAt == "" {
			st.LastSyncAt = run.StartedAt
		}
		st.AvgParseMs = run.AvgParseMs
	}
	meta, _ := s.GetPCGWManifestMeta(ctx)
	if meta != nil {
		st.DBWikitextBytes = meta.DBWikitextBytes
		st.ManifestVersion = meta.ManifestVersion
	}
	return st, nil
}

func (s *sqliteStore) ExportPCGWGameJSON(ctx context.Context, pageID int64) ([]byte, error) {
	g, err := s.GetPCGWGame(ctx, pageID)
	if err != nil {
		return nil, err
	}
	gameData, _ := s.ListPCGWGameData(ctx, pageID)
	sysReq, _ := s.ListPCGWSystemRequirements(ctx, pageID)
	meta, _ := s.GetPCGWMetadata(ctx, pageID)
	export := map[string]interface{}{
		"game": g, "game_data": gameData, "system_requirements": sysReq,
	}
	if meta != nil {
		export["metadata"] = map[string]interface{}{
			"content_hash": meta.ContentHash, "section_hashes": meta.SectionHashes,
			"uncompressed_size": meta.UncompressedSize, "last_fetched_at": meta.LastFetchedAt,
		}
	}
	for name := range pcgwSectionTables {
		sec, err := s.GetPCGWSection(ctx, pageID, name)
		if err == nil {
			export[name] = sec
		}
	}
	return json.MarshalIndent(export, "", "  ")
}

// ManifestETagFromGames computes etag hash for manifest bump.
func ManifestETagFromGames(version int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("manifest-v%d-%d", version, time.Now().UnixNano())))
	return "sha256:" + hex.EncodeToString(h[:16])
}

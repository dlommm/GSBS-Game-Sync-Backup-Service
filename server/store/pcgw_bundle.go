package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/logx"
)

const (
	pcgwBundleSchemaVersionV1 = 1
	pcgwBundleSchemaVersionV2 = 2
)

type pcgwManifestBundle struct {
	SchemaVersion      int                           `json:"schema_version"`
	GSBSVersion        string                        `json:"gsbs_version"`
	ExportedAt         string                        `json:"exported_at"`
	FullExportedAt     string                        `json:"full_exported_at,omitempty"`
	PreviousExportedAt string                        `json:"previous_exported_at,omitempty"`
	Counts             pcgwBundleCounts              `json:"counts"`
	GameSaveLocations  []types.GameSaveLocation      `json:"game_save_locations"`
	Games              []types.PCGWGame              `json:"games"`
	GameData           []types.PCGWGameData          `json:"game_data"`
	Metadata           []types.PCGWMetadata          `json:"metadata"`
	Sections           map[string][]sectionExport    `json:"sections"`
	SystemRequirements []types.PCGWSystemRequirement `json:"system_requirements"`
	Catalog            []types.PCGWCatalogEntry      `json:"catalog,omitempty"`
	DeletedGameIDs     []string                      `json:"deleted_game_ids,omitempty"`
}

type pcgwBundleCounts struct {
	GameSaveLocations  int `json:"game_save_locations"`
	Games              int `json:"games"`
	GameData           int `json:"game_data"`
	Metadata           int `json:"metadata"`
	Sections           int `json:"sections"`
	SystemRequirements int `json:"system_requirements"`
	Catalog            int `json:"catalog,omitempty"`
}

type sectionExport struct {
	PageID          int64                  `json:"page_id"`
	Section         string                 `json:"section"`
	Data            map[string]interface{} `json:"data"`
	AllTemplates    []string               `json:"all_templates"`
	SectionWikitext string                 `json:"section_wikitext"`
	UpdatedAt       string                 `json:"updated_at"`
}

func (s *sqliteStore) ExportPCGWManifestBundle(ctx context.Context, gsbsVersion string) ([]byte, error) {
	data, _, err := s.ExportPCGWManifestBundleWithOpts(ctx, gsbsVersion, PCGWBundleExportOpts{})
	return data, err
}

func (s *sqliteStore) ExportPCGWManifestBundleWithOpts(ctx context.Context, gsbsVersion string, opts PCGWBundleExportOpts) ([]byte, *PCGWBundleMeta, error) {
	since := strings.TrimSpace(opts.Since)
	delta := since != ""

	locations, err := s.listGameSaveLocationsForExport(ctx, since)
	if err != nil {
		return nil, nil, err
	}

	var games []types.PCGWGame
	offset := 0
	filter := PCGWGameListFilter{Limit: 500}
	if since != "" {
		filter.UpdatedAfter = since
	}
	for {
		batch, total, err := s.ListPCGWGames(ctx, filter)
		if err != nil {
			return nil, nil, err
		}
		games = append(games, batch...)
		offset += len(batch)
		if offset >= total || len(batch) == 0 {
			break
		}
		filter.Offset = offset
	}

	gameData, err := s.listPCGWGameDataForExport(ctx, since)
	if err != nil {
		return nil, nil, err
	}

	var metadata []types.PCGWMetadata
	if !opts.Lite {
		metadata, err = s.listAllPCGWMetadata(ctx)
		if err != nil {
			return nil, nil, err
		}
	}

	sections, sectionCount, err := s.exportPCGWSectionsForExport(ctx, since, opts.Lite)
	if err != nil {
		return nil, nil, err
	}

	sysReqs, err := s.listPCGWSystemRequirementsForExport(ctx, since)
	if err != nil {
		return nil, nil, err
	}

	catalog, err := s.listPCGWCatalogForExport(ctx, since)
	if err != nil {
		return nil, nil, err
	}

	var deleted []string
	if delta {
		deleted, err = s.listManifestDeletionsSince(ctx, since)
		if err != nil {
			return nil, nil, err
		}
	} else {
		catalog, err = s.listAllPCGWCatalog(ctx)
		if err != nil {
			return nil, nil, err
		}
	}

	exportedAt := time.Now().UTC().Format(time.RFC3339)
	fullExportedAt := strings.TrimSpace(opts.FullExportedAt)
	if !delta {
		fullExportedAt = exportedAt
	}
	schemaVer := pcgwBundleSchemaVersionV2
	bundle := pcgwManifestBundle{
		SchemaVersion:      schemaVer,
		GSBSVersion:        gsbsVersion,
		ExportedAt:         exportedAt,
		FullExportedAt:     fullExportedAt,
		PreviousExportedAt: opts.PreviousExportedAt,
		GameSaveLocations:  locations,
		Games:              games,
		GameData:           gameData,
		Metadata:           metadata,
		Sections:           sections,
		SystemRequirements: sysReqs,
		Catalog:            catalog,
		DeletedGameIDs:     deleted,
		Counts: pcgwBundleCounts{
			GameSaveLocations:  len(locations),
			Games:              len(games),
			GameData:           len(gameData),
			Metadata:           len(metadata),
			Sections:           sectionCount,
			SystemRequirements: len(sysReqs),
			Catalog:            len(catalog),
		},
	}

	raw, err := json.Marshal(bundle)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		return nil, nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, nil, err
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	meta := &PCGWBundleMeta{
		SchemaVersion:      schemaVer,
		GSBSVersion:        gsbsVersion,
		ExportedAt:         exportedAt,
		FullExportedAt:     fullExportedAt,
		PreviousExportedAt: opts.PreviousExportedAt,
		FullBytes:          len(data),
		FullSHA256:         hex.EncodeToString(sum[:]),
		Counts: PCGWBundleMetaCounts{
			GameSaveLocations: len(locations),
			Games:             len(games),
			GameData:          len(gameData),
			Catalog:           len(catalog),
		},
	}
	return data, meta, nil
}

func (s *sqliteStore) ImportPCGWManifestBundle(ctx context.Context, data []byte, mode string) (PCGWImportResult, error) {
	switch mode {
	case "merge", "full_replace", "merge_skip_unchanged":
	default:
		return PCGWImportResult{}, fmt.Errorf("invalid import mode %q", mode)
	}

	raw, err := gunzipMaybe(data)
	if err != nil {
		return PCGWImportResult{}, err
	}

	var bundle pcgwManifestBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return PCGWImportResult{}, fmt.Errorf("invalid bundle JSON: %w", err)
	}
	if bundle.SchemaVersion != pcgwBundleSchemaVersionV1 && bundle.SchemaVersion != pcgwBundleSchemaVersionV2 {
		return PCGWImportResult{}, fmt.Errorf("unsupported schema_version %d", bundle.SchemaVersion)
	}

	skipUnchanged := mode == "merge_skip_unchanged"

	fullExportedAt := strings.TrimSpace(bundle.FullExportedAt)
	if fullExportedAt == "" {
		fullExportedAt = bundle.ExportedAt
	}
	result := PCGWImportResult{Mode: mode, ExportedAt: bundle.ExportedAt, FullExportedAt: fullExportedAt}
	var changed int
	var skipped int

	if mode == "full_replace" {
		if err := s.clearPCGWBundleTables(ctx); err != nil {
			return result, err
		}
	}

	if len(bundle.Catalog) > 0 {
		if err := s.UpsertPCGWCatalogBatch(ctx, bundle.Catalog); err != nil {
			return result, err
		}
		result.PCGWCatalog = len(bundle.Catalog)
		changed += len(bundle.Catalog)
	}

	locChanged, locSkipped, err := s.importGameSaveLocationsSmart(ctx, bundle.GameSaveLocations, skipUnchanged)
	if err != nil {
		return result, err
	}
	result.GameSaveLocations = locChanged
	changed += locChanged
	skipped += locSkipped

	for i := range bundle.Games {
		ok, err := s.importPCGWGameSmart(ctx, &bundle.Games[i], skipUnchanged)
		if err != nil {
			return result, err
		}
		if ok {
			changed++
			result.PCGWGames++
		} else {
			skipped++
		}
	}

	// The child tables (game_data, metadata, sections, system_requirements) have
	// a FOREIGN KEY to pcgw_games(page_id). A bundle can contain orphan child
	// rows that reference a page_id with no game row (source-DB drift). Inserting
	// one would fail the whole import on a FK constraint. Load the set of valid
	// page_ids once and skip orphans — this keeps the DB referentially consistent
	// and loses no real data (an orphan row for a non-existent game is unusable
	// and is never save data).
	validPages, err := s.loadPCGWGamePageIDs(ctx)
	if err != nil {
		return result, err
	}

	for i := range bundle.GameData {
		if !validPages[bundle.GameData[i].PageID] {
			result.SkippedOrphans++
			continue
		}
		ok, err := s.importPCGWGameDataSmart(ctx, &bundle.GameData[i], skipUnchanged)
		if err != nil {
			return result, err
		}
		if ok {
			changed++
			result.PCGWGameData++
		} else {
			skipped++
		}
	}

	for i := range bundle.Metadata {
		if !validPages[bundle.Metadata[i].PageID] {
			result.SkippedOrphans++
			continue
		}
		if err := s.UpsertPCGWMetadata(ctx, &bundle.Metadata[i]); err != nil {
			return result, err
		}
		result.PCGWMetadata++
		changed++
	}

	for _, rows := range bundle.Sections {
		for _, row := range rows {
			if !validPages[row.PageID] {
				result.SkippedOrphans++
				continue
			}
			sec := &types.PCGWSectionRow{
				PageID: row.PageID, Data: row.Data, AllTemplates: row.AllTemplates,
				SectionWikitext: row.SectionWikitext, UpdatedAt: row.UpdatedAt,
			}
			if err := s.upsertSectionByName(ctx, row.Section, sec); err != nil {
				return result, err
			}
			result.PCGWSections++
			changed++
		}
	}

	byPage := map[int64][]types.PCGWSystemRequirement{}
	for _, r := range bundle.SystemRequirements {
		if !validPages[r.PageID] {
			result.SkippedOrphans++
			continue
		}
		byPage[r.PageID] = append(byPage[r.PageID], r)
	}
	for pageID, rows := range byPage {
		if err := s.ReplacePCGWSystemRequirements(ctx, pageID, rows); err != nil {
			return result, err
		}
		result.PCGWSystemReqs += len(rows)
		changed += len(rows)
	}
	if result.SkippedOrphans > 0 {
		logx.Logger().Warn().Int("count", result.SkippedOrphans).
			Msg("pcgw import: skipped orphan child rows referencing a missing game")
	}

	// Reconcile upstream deletions: the full bundle's catalog is the authoritative
	// complete set, so any local game whose page_id is absent from it was deleted
	// upstream. full_replace already starts from an empty mirror, so it is exempt.
	if mode != "full_replace" {
		deleted, err := s.reconcilePCGWCatalogDeletions(ctx, bundle.Catalog)
		if err != nil {
			return result, err
		}
		result.Deleted = deleted
		changed += deleted
	}

	if bundle.SchemaVersion >= pcgwBundleSchemaVersionV2 && len(bundle.Catalog) > 0 {
		if _, err := s.ComputeCatalogHash(ctx); err != nil {
			return result, err
		}
	}

	result.RowsChanged = changed
	result.SkippedUnchanged = skipped
	result.NoOp = changed == 0

	if changed > 0 {
		newVersion := 1
		if meta, err := s.GetPCGWManifestMeta(ctx); err == nil && meta != nil {
			newVersion = meta.ManifestVersion + 1
		}
		etag := ManifestETagFromGames(newVersion)
		if _, err := s.BumpManifestVersion(ctx, etag); err != nil {
			return result, err
		}
	}
	return result, nil
}

// loadPCGWGamePageIDs returns the set of page_ids that currently have a
// pcgw_games row, used to filter out orphan child rows before import.
func (s *sqliteStore) loadPCGWGamePageIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT page_id FROM pcgw_games`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}

func (s *sqliteStore) importGameSaveLocationsSmart(ctx context.Context, entries []types.GameSaveLocation, skipUnchanged bool) (changed, skipped int, err error) {
	if len(entries) == 0 {
		return 0, 0, nil
	}
	var toUpsert []types.GameSaveLocation
	for _, e := range entries {
		if skipUnchanged {
			same, err := s.gameSaveLocationUnchanged(ctx, e)
			if err != nil {
				return 0, 0, err
			}
			if same {
				skipped++
				continue
			}
		}
		toUpsert = append(toUpsert, e)
	}
	if len(toUpsert) == 0 {
		return 0, skipped, nil
	}
	if err := s.importGameSaveLocations(ctx, toUpsert); err != nil {
		return 0, skipped, err
	}
	return len(toUpsert), skipped, nil
}

func (s *sqliteStore) gameSaveLocationUnchanged(ctx context.Context, e types.GameSaveLocation) (bool, error) {
	var updatedAt, rules string
	err := s.db.QueryRowContext(ctx,
		`SELECT updated_at, COALESCE(save_rules_json,'') FROM game_save_locations
		 WHERE game_id = ? AND platform = ? AND path_template = ?`,
		e.GameID, e.Platform, e.PathTemplate).Scan(&updatedAt, &rules)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if updatedAt != e.UpdatedAt {
		return false, nil
	}
	wantRules := encodeSaveRules(e.SaveRules)
	if rules != wantRules {
		return false, nil
	}
	return true, nil
}

func (s *sqliteStore) importPCGWGameSmart(ctx context.Context, g *types.PCGWGame, skipUnchanged bool) (changed bool, err error) {
	if skipUnchanged && g.LastRevID > 0 {
		local, err := s.GetPCGWGame(ctx, g.PageID)
		// A missing local row (sql.ErrNoRows) is the normal case on a fresh
		// server — it means "not present yet", not a failure. Only a real query
		// error should abort the import.
		if err != nil && err != sql.ErrNoRows {
			return false, err
		}
		if local != nil && local.LastRevID == g.LastRevID && local.ParseStatus == g.ParseStatus {
			return false, nil
		}
	}
	if err := s.UpsertPCGWGame(ctx, g); err != nil {
		return false, err
	}
	return true, nil
}

func (s *sqliteStore) importPCGWGameDataSmart(ctx context.Context, row *types.PCGWGameData, skipUnchanged bool) (changed bool, err error) {
	if skipUnchanged {
		var updatedAt string
		err := s.db.QueryRowContext(ctx,
			`SELECT updated_at FROM pcgw_game_data WHERE page_id = ? AND platform_key = ?`,
			row.PageID, row.PlatformKey).Scan(&updatedAt)
		if err == nil && updatedAt == row.UpdatedAt {
			return false, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return false, err
		}
	}
	if err := s.UpsertPCGWGameData(ctx, row); err != nil {
		return false, err
	}
	return true, nil
}

// pcgwReconcileMaxDeleteFraction is a safety valve: if reconciling a full bundle
// would remove more than this fraction of the local games, the removal is skipped
// and logged instead — a guard against a truncated or malformed bundle wiping the
// mirror. Legitimate upstream deletions are a tiny fraction of the catalog.
const pcgwReconcileMaxDeleteFraction = 0.25

// reconcilePCGWCatalogDeletions removes local games whose page_id is absent from
// the full bundle's catalog (deleted upstream), cascading to save locations and
// the PCGW child tables and the catalog row. It is a no-op when the bundle
// carries no catalog (e.g. a schema-v1 bundle) or the mirror is empty. The
// pcgw_games delete trigger records a tombstone in pcgw_manifest_deletions.
func (s *sqliteStore) reconcilePCGWCatalogDeletions(ctx context.Context, catalog []types.PCGWCatalogEntry) (int, error) {
	if len(catalog) == 0 {
		return 0, nil
	}
	keep := make(map[int64]bool, len(catalog))
	for _, c := range catalog {
		keep[c.PageID] = true
	}
	local, err := s.loadPCGWGamePageIDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(local) == 0 {
		return 0, nil
	}
	var toDelete []int64
	for pid := range local {
		if !keep[pid] {
			toDelete = append(toDelete, pid)
		}
	}
	if len(toDelete) == 0 {
		return 0, nil
	}
	if frac := float64(len(toDelete)) / float64(len(local)); frac > pcgwReconcileMaxDeleteFraction {
		logx.Logger().Warn().
			Int("would_delete", len(toDelete)).Int("local_games", len(local)).
			Int("bundle_catalog", len(catalog)).Float64("fraction", frac).
			Msg("pcgw import: skipping deletion reconciliation — removal set too large (possible partial/bad bundle)")
		return 0, nil
	}
	for _, pid := range toDelete {
		if err := s.deletePCGWGameByPageID(ctx, pid); err != nil {
			return 0, err
		}
	}
	logx.Logger().Info().Int("deleted", len(toDelete)).Msg("pcgw import: reconciled upstream deletions")
	return len(toDelete), nil
}

// deletePCGWGameByPageID removes a single game and all of its dependent rows.
func (s *sqliteStore) deletePCGWGameByPageID(ctx context.Context, pageID int64) error {
	childTables := []string{
		"pcgw_parse_failures", "pcgw_system_requirements", "pcgw_metadata", "pcgw_game_data",
		"pcgw_availability", "pcgw_monetization", "pcgw_video", "pcgw_input",
		"pcgw_audio", "pcgw_network", "pcgw_other", "pcgw_notes",
		"pcgw_references", "pcgw_external_links",
	}
	for _, t := range childTables {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM `+t+` WHERE page_id = ?`, pageID); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM game_save_locations WHERE pcgw_page_id = ?`, pageID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM pcgw_catalog WHERE page_id = ?`, pageID); err != nil {
		return err
	}
	// The pcgw_games AFTER DELETE trigger writes the manifest-deletion tombstone.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM pcgw_games WHERE page_id = ?`, pageID); err != nil {
		return err
	}
	return nil
}

func (s *sqliteStore) listGameSaveLocationsForExport(ctx context.Context, since string) ([]types.GameSaveLocation, error) {
	if since == "" {
		return s.ListGameSaveLocations(ctx)
	}
	rows, err := s.db.QueryContext(ctx, gameSaveLocationSelect+` WHERE updated_at > ? ORDER BY game_id, platform`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.GameSaveLocation
	for rows.Next() {
		e, err := scanGameSaveLocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) listPCGWGameDataForExport(ctx context.Context, since string) ([]types.PCGWGameData, error) {
	q := `
		SELECT page_id, platform_key, platform_raw_label, save_locations, config_locations,
		       save_game_cloud_sync, install_locations, registry_keys, save_file_info,
		       all_templates, section_wikitext, structured, updated_at
		FROM pcgw_game_data`
	var args []interface{}
	if since != "" {
		q += ` WHERE updated_at > ?`
		args = append(args, since)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

func (s *sqliteStore) listPCGWSystemRequirementsForExport(ctx context.Context, since string) ([]types.PCGWSystemRequirement, error) {
	q := `SELECT page_id, platform_key, requirement_type, specs, section_wikitext, updated_at FROM pcgw_system_requirements`
	var args []interface{}
	if since != "" {
		q += ` WHERE updated_at > ?`
		args = append(args, since)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

func (s *sqliteStore) exportPCGWSectionsForExport(ctx context.Context, since string, lite bool) (map[string][]sectionExport, int, error) {
	out := map[string][]sectionExport{}
	total := 0
	for name, table := range pcgwSectionTables {
		q := fmt.Sprintf(`SELECT page_id, data, all_templates, section_wikitext, updated_at FROM %s`, table)
		var args []interface{}
		if since != "" {
			q += ` WHERE updated_at > ?`
			args = append(args, since)
		}
		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, 0, err
		}
		for rows.Next() {
			var se sectionExport
			se.Section = name
			var data, templates string
			var wiki sql.NullString
			if err := rows.Scan(&se.PageID, &data, &templates, &wiki, &se.UpdatedAt); err != nil {
				_ = rows.Close()
				return nil, 0, err
			}
			if lite {
				se.SectionWikitext = ""
			} else {
				se.SectionWikitext = wiki.String
			}
			pcgwDecodeJSON(data, &se.Data)
			pcgwDecodeJSON(templates, &se.AllTemplates)
			out[name] = append(out[name], se)
			total++
		}
		if err := rows.Close(); err != nil {
			return nil, 0, err
		}
		if err := rows.Err(); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

func (s *sqliteStore) listAllPCGWCatalog(ctx context.Context) ([]types.PCGWCatalogEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, title, first_seen_at, last_seen_at, last_seen_run_id, last_seen_rev_id,
		       dead_letter, COALESCE(dead_letter_reason,''), retry_count
		FROM pcgw_catalog ORDER BY page_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPCGWCatalogRows(rows)
}

func (s *sqliteStore) listPCGWCatalogForExport(ctx context.Context, since string) ([]types.PCGWCatalogEntry, error) {
	if since == "" {
		return s.listAllPCGWCatalog(ctx)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, title, first_seen_at, last_seen_at, last_seen_run_id, last_seen_rev_id,
		       dead_letter, COALESCE(dead_letter_reason,''), retry_count
		FROM pcgw_catalog WHERE first_seen_at > ? OR last_seen_at > ? ORDER BY page_id`, since, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPCGWCatalogRows(rows)
}

func scanPCGWCatalogRows(rows *sql.Rows) ([]types.PCGWCatalogEntry, error) {
	var out []types.PCGWCatalogEntry
	for rows.Next() {
		var e types.PCGWCatalogEntry
		var dead int
		if err := rows.Scan(&e.PageID, &e.Title, &e.FirstSeenAt, &e.LastSeenAt, &e.LastSeenRunID, &e.LastSeenRevID,
			&dead, &e.DeadLetterReason, &e.RetryCount); err != nil {
			return nil, err
		}
		e.DeadLetter = dead != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) listManifestDeletionsSince(ctx context.Context, since string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT game_id FROM pcgw_manifest_deletions WHERE deleted_at > ? ORDER BY game_id`, since)
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

func (s *sqliteStore) ValidatePCGWImport(ctx context.Context) (PCGWImportValidation, error) {
	var v PCGWImportValidation
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_save_locations`).Scan(&v.GameSaveLocations)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_games`).Scan(&v.PCGWGames)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_game_data`).Scan(&v.PCGWGameData)

	var samplePageID int64
	err := s.db.QueryRowContext(ctx, `SELECT page_id FROM pcgw_games WHERE parse_status = 'ok' LIMIT 1`).Scan(&samplePageID)
	if err != nil {
		v.SampleOK = v.PCGWGames == 0
		return v, nil
	}
	var gdCount int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pcgw_game_data WHERE page_id = ?`, samplePageID).Scan(&gdCount)
	v.SampleOK = gdCount > 0
	if !v.SampleOK {
		v.Errors = append(v.Errors, fmt.Sprintf("sample game %d has no game_data rows", samplePageID))
	}
	if v.PCGWGames > 0 && v.GameSaveLocations == 0 {
		v.Errors = append(v.Errors, "pcgw games present but manifest entries empty")
	}
	return v, nil
}

func gunzipMaybe(data []byte) ([]byte, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(r); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return data, nil
}

func (s *sqliteStore) importGameSaveLocations(ctx context.Context, entries []types.GameSaveLocation) error {
	if len(entries) == 0 {
		return nil
	}
	return s.UpsertGameSaveLocations(ctx, entries)
}

func (s *sqliteStore) clearPCGWBundleTables(ctx context.Context) error {
	tables := []string{
		"game_save_locations",
		"pcgw_parse_failures",
		"pcgw_system_requirements",
		"pcgw_metadata",
		"pcgw_game_data",
		"pcgw_availability", "pcgw_monetization", "pcgw_video", "pcgw_input",
		"pcgw_audio", "pcgw_network", "pcgw_other", "pcgw_notes",
		"pcgw_references", "pcgw_external_links",
		"pcgw_games",
	}
	for _, t := range tables {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM `+t); err != nil {
			return err
		}
	}
	return nil
}

func (s *sqliteStore) upsertSectionByName(ctx context.Context, section string, row *types.PCGWSectionRow) error {
	switch section {
	case "availability":
		return s.UpsertPCGWAvailability(ctx, row)
	case "monetization":
		return s.UpsertPCGWMonetization(ctx, row)
	case "video":
		return s.UpsertPCGWVideo(ctx, row)
	case "input":
		return s.UpsertPCGWInput(ctx, row)
	case "audio":
		return s.UpsertPCGWAudio(ctx, row)
	case "network":
		return s.UpsertPCGWNetwork(ctx, row)
	case "notes":
		return s.UpsertPCGWNotes(ctx, row)
	case "references":
		return s.UpsertPCGWReferences(ctx, row)
	case "external_links":
		return s.UpsertPCGWExternalLinks(ctx, row)
	default:
		return s.UpsertPCGWOther(ctx, row)
	}
}

func (s *sqliteStore) listAllPCGWMetadata(ctx context.Context) ([]types.PCGWMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, full_wikitext_zstd, COALESCE(content_hash, ''), COALESCE(section_hashes, ''), uncompressed_size, last_fetched_at
		FROM pcgw_metadata`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []types.PCGWMetadata
	for rows.Next() {
		var m types.PCGWMetadata
		var zstd []byte
		var hashes string
		if err := rows.Scan(&m.PageID, &zstd, &m.ContentHash, &hashes, &m.UncompressedSize, &m.LastFetchedAt); err != nil {
			return nil, err
		}
		m.FullWikitextZstd = zstd
		pcgwDecodeJSON(hashes, &m.SectionHashes)
		out = append(out, m)
	}
	return out, rows.Err()
}

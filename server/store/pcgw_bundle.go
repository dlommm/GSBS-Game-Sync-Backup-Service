package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
)

const pcgwBundleSchemaVersion = 1

type pcgwManifestBundle struct {
	SchemaVersion      int                           `json:"schema_version"`
	GSBSVersion        string                        `json:"gsbs_version"`
	ExportedAt         string                        `json:"exported_at"`
	Counts             pcgwBundleCounts              `json:"counts"`
	GameSaveLocations  []types.GameSaveLocation      `json:"game_save_locations"`
	Games              []types.PCGWGame              `json:"games"`
	GameData           []types.PCGWGameData          `json:"game_data"`
	Metadata           []types.PCGWMetadata          `json:"metadata"`
	Sections           map[string][]sectionExport    `json:"sections"`
	SystemRequirements []types.PCGWSystemRequirement `json:"system_requirements"`
}

type pcgwBundleCounts struct {
	GameSaveLocations  int `json:"game_save_locations"`
	Games              int `json:"games"`
	GameData           int `json:"game_data"`
	Metadata           int `json:"metadata"`
	Sections           int `json:"sections"`
	SystemRequirements int `json:"system_requirements"`
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
	locations, err := s.ListGameSaveLocations(ctx)
	if err != nil {
		return nil, err
	}

	var games []types.PCGWGame
	offset := 0
	for {
		batch, total, err := s.ListPCGWGames(ctx, PCGWGameListFilter{Limit: 500, Offset: offset})
		if err != nil {
			return nil, err
		}
		games = append(games, batch...)
		offset += len(batch)
		if offset >= total || len(batch) == 0 {
			break
		}
	}

	gameData, err := s.listAllPCGWGameData(ctx)
	if err != nil {
		return nil, err
	}
	metadata, err := s.listAllPCGWMetadata(ctx)
	if err != nil {
		return nil, err
	}
	sections, sectionCount, err := s.exportAllPCGWSections(ctx)
	if err != nil {
		return nil, err
	}
	sysReqs, err := s.listAllPCGWSystemRequirements(ctx)
	if err != nil {
		return nil, err
	}

	bundle := pcgwManifestBundle{
		SchemaVersion:      pcgwBundleSchemaVersion,
		GSBSVersion:        gsbsVersion,
		ExportedAt:         time.Now().UTC().Format(time.RFC3339),
		GameSaveLocations:  locations,
		Games:              games,
		GameData:           gameData,
		Metadata:           metadata,
		Sections:           sections,
		SystemRequirements: sysReqs,
		Counts: pcgwBundleCounts{
			GameSaveLocations:  len(locations),
			Games:              len(games),
			GameData:           len(gameData),
			Metadata:           len(metadata),
			Sections:           sectionCount,
			SystemRequirements: len(sysReqs),
		},
	}

	raw, err := json.Marshal(bundle)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *sqliteStore) ImportPCGWManifestBundle(ctx context.Context, data []byte, mode string) (PCGWImportResult, error) {
	if mode != "merge" && mode != "full_replace" {
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
	if bundle.SchemaVersion != pcgwBundleSchemaVersion {
		return PCGWImportResult{}, fmt.Errorf("unsupported schema_version %d", bundle.SchemaVersion)
	}

	result := PCGWImportResult{Mode: mode}

	if mode == "full_replace" {
		if err := s.clearPCGWBundleTables(ctx); err != nil {
			return result, err
		}
	}

	if err := s.importGameSaveLocations(ctx, bundle.GameSaveLocations); err != nil {
		return result, err
	}
	result.GameSaveLocations = len(bundle.GameSaveLocations)

	for i := range bundle.Games {
		if err := s.UpsertPCGWGame(ctx, &bundle.Games[i]); err != nil {
			return result, err
		}
	}
	result.PCGWGames = len(bundle.Games)

	for i := range bundle.GameData {
		if err := s.UpsertPCGWGameData(ctx, &bundle.GameData[i]); err != nil {
			return result, err
		}
	}
	result.PCGWGameData = len(bundle.GameData)

	for i := range bundle.Metadata {
		if err := s.UpsertPCGWMetadata(ctx, &bundle.Metadata[i]); err != nil {
			return result, err
		}
	}
	result.PCGWMetadata = len(bundle.Metadata)

	for _, rows := range bundle.Sections {
		for _, row := range rows {
			sec := &types.PCGWSectionRow{
				PageID: row.PageID, Data: row.Data, AllTemplates: row.AllTemplates,
				SectionWikitext: row.SectionWikitext, UpdatedAt: row.UpdatedAt,
			}
			if err := s.upsertSectionByName(ctx, row.Section, sec); err != nil {
				return result, err
			}
			result.PCGWSections++
		}
	}

	byPage := map[int64][]types.PCGWSystemRequirement{}
	for _, r := range bundle.SystemRequirements {
		byPage[r.PageID] = append(byPage[r.PageID], r)
	}
	for pageID, rows := range byPage {
		if err := s.ReplacePCGWSystemRequirements(ctx, pageID, rows); err != nil {
			return result, err
		}
	}
	result.PCGWSystemReqs = len(bundle.SystemRequirements)

	newVersion := 1
	if meta, err := s.GetPCGWManifestMeta(ctx); err == nil && meta != nil {
		newVersion = meta.ManifestVersion + 1
	}
	etag := ManifestETagFromGames(newVersion)
	if _, err := s.BumpManifestVersion(ctx, etag); err != nil {
		return result, err
	}
	return result, nil
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

func (s *sqliteStore) listAllPCGWGameData(ctx context.Context) ([]types.PCGWGameData, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, platform_key, platform_raw_label, save_locations, config_locations,
		       save_game_cloud_sync, install_locations, registry_keys, save_file_info,
		       all_templates, section_wikitext, structured, updated_at
		FROM pcgw_game_data`)
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

func (s *sqliteStore) listAllPCGWMetadata(ctx context.Context) ([]types.PCGWMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, full_wikitext_zstd, content_hash, section_hashes, uncompressed_size, last_fetched_at
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

func (s *sqliteStore) listAllPCGWSystemRequirements(ctx context.Context) ([]types.PCGWSystemRequirement, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT page_id, platform_key, requirement_type, specs, section_wikitext, updated_at
		FROM pcgw_system_requirements`)
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

func (s *sqliteStore) exportAllPCGWSections(ctx context.Context) (map[string][]sectionExport, int, error) {
	out := map[string][]sectionExport{}
	total := 0
	for name, table := range pcgwSectionTables {
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
			`SELECT page_id, data, all_templates, section_wikitext, updated_at FROM %s`, table))
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
			se.SectionWikitext = wiki.String
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

package job

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/store"
)

// PCGWSyncOptions configures a sync run.
type PCGWSyncOptions struct {
	Full             bool
	ForceFull        bool // bypass resume checkpoint
	SinglePage       int64 // if >0, sync only this page
	Offset           int   // resume offset for full/incremental list
	SyncRunID        string
	ResumedFromRunID string
	Notes            string
	SkipStartRun     bool // use SyncRunID instead of StartPCGWSyncRun
}

// PCGWSyncProgress reports sync progress for SSE.
type PCGWSyncProgress struct {
	PagesProcessed int
	TotalEstimate  int
	Phase          string
	GamesSkipped   int
	ETASeconds     int // set by runner when broadcasting
}

// ReportProgress is an optional callback (pages processed).
type ReportProgress func(pagesProcessed int)

// ReportProgressEx reports richer progress when non-nil.
type ReportProgressEx func(PCGWSyncProgress)

// PCGWSync runs PCGW ingest: list pages, incremental skip, persist full mirror, project manifest v1.
func PCGWSync(ctx context.Context, st store.Store, client *pcgw.Client, reportProgress ReportProgress, opts PCGWSyncOptions) (int, error) {
	return PCGWSyncEx(ctx, st, client, reportProgress, nil, opts)
}

// PCGWSyncEx is PCGWSync with optional extended progress reporting.
func PCGWSyncEx(ctx context.Context, st store.Store, client *pcgw.Client, reportProgress ReportProgress, reportEx ReportProgressEx, opts PCGWSyncOptions) (int, error) {
	const chunkSize = 100
	mode := "incremental"
	if opts.Full {
		mode = "full"
	}
	if opts.SinglePage > 0 {
		mode = "single"
	}

	runID := opts.SyncRunID
	if runID == "" && !opts.SkipStartRun {
		var err error
		if opts.ResumedFromRunID != "" || opts.Notes != "" {
			runID, err = st.StartPCGWSyncRunWithResume(ctx, mode, opts.ResumedFromRunID, opts.Notes)
		} else {
			runID, err = st.StartPCGWSyncRun(ctx, mode)
		}
		if err != nil {
			return 0, err
		}
	}

	stats := store.PCGWSyncRunStats{}
	totalUpserted := 0
	offset := opts.Offset
	parseMsTotal := 0
	parseCount := 0
	filters := LoadPCGWFilters(ctx, st)

	finishRun := func(status, errMsg string) {
		_ = st.FinishPCGWSyncRun(context.Background(), runID, status, errMsg, stats)
	}

	report := func(pages, total int, phase string) {
		if reportProgress != nil {
			reportProgress(pages)
		}
		if reportEx != nil {
			reportEx(PCGWSyncProgress{PagesProcessed: pages, TotalEstimate: total, Phase: phase, GamesSkipped: stats.GamesSkipped})
		}
		_ = st.UpdatePCGWSyncRunCheckpoint(ctx, runID, offset, stats)
	}

	if opts.SinglePage > 0 {
		n, err := syncOnePage(ctx, st, client, runID, opts.SinglePage, pcgw.PageInfo{PageID: opts.SinglePage}, &stats, filters)
		if err != nil {
			finishRun(JobFailed, err.Error())
			return n, err
		}
		stats.GamesOK++
		finishRun(JobSuccess, "")
		return bumpManifestAndReturn(ctx, st, n)
	}

	for {
		select {
		case <-ctx.Done():
			status := JobFailed
			errMsg := ctx.Err().Error()
			if errors.Is(ctx.Err(), context.Canceled) {
				status = JobCanceled
			}
			finishRun(status, errMsg)
			return totalUpserted, ctx.Err()
		default:
		}

		pages, err := client.ListGamePages(chunkSize, offset)
		if err != nil {
			finishRun(JobFailed, err.Error())
			return totalUpserted, err
		}
		if len(pages) == 0 {
			break
		}
		stats.GamesTotal = offset + len(pages)
		report(offset+len(pages), stats.GamesTotal, "listing")

		for _, p := range pages {
			select {
			case <-ctx.Done():
				status := JobFailed
				errMsg := ctx.Err().Error()
				if errors.Is(ctx.Err(), context.Canceled) {
					status = JobCanceled
				}
				finishRun(status, errMsg)
				return totalUpserted, ctx.Err()
			default:
			}

			if !opts.Full {
				if skip, err := shouldSkipPage(ctx, st, client, p.PageID); err == nil && skip {
					stats.GamesSkipped++
					continue
				}
			}

			if filters.ShouldSkipTitle(p.Title) {
				stats.GamesSkipped++
				continue
			}

			start := time.Now()
			n, err := syncOnePage(ctx, st, client, runID, p.PageID, p, &stats, filters)
			parseMsTotal += int(time.Since(start).Milliseconds())
			parseCount++
			if err != nil {
				log.Printf("pcgw sync: page %d %q: %v", p.PageID, p.Title, err)
				stats.GamesFailed++
				continue
			}
			totalUpserted += n
			switch {
			case n >= 0:
				// status set in persist
			}
		}

		report(offset+len(pages), stats.GamesTotal, "syncing")
		if len(pages) < chunkSize {
			break
		}
		offset += len(pages)
	}

	if parseCount > 0 {
		stats.AvgParseMs = parseMsTotal / parseCount
	}
	status := JobSuccess
	if stats.GamesFailed > 0 {
		status = "partial"
	}
	finishRun(status, "")
	log.Printf("pcgw sync: done, upserted %d location entries (ok=%d partial=%d failed=%d skipped=%d)",
		totalUpserted, stats.GamesOK, stats.GamesPartial, stats.GamesFailed, stats.GamesSkipped)
	return bumpManifestAndReturn(ctx, st, totalUpserted)
}

func bumpManifestAndReturn(ctx context.Context, st store.Store, n int) (int, error) {
	etag := store.ManifestETagFromGames(0)
	if v, err := st.BumpManifestVersion(ctx, etag); err == nil {
		log.Printf("pcgw sync: manifest version bumped to %d", v)
	}
	return n, nil
}

func shouldSkipPage(ctx context.Context, st store.Store, client *pcgw.Client, pageID int64) (bool, error) {
	g, err := st.GetPCGWGame(ctx, pageID)
	if err != nil {
		return false, nil
	}
	rev, err := client.GetPageRevision(strconv.FormatInt(pageID, 10))
	if err != nil {
		return false, err
	}
	if g.LastRevID == rev.RevID && g.ParseStatus == "ok" {
		return true, nil
	}
	contentHash, _, _ := st.GetPCGWContentHash(ctx, pageID)
	if contentHash != "" && rev.RevID == g.LastRevID {
		return true, nil
	}
	return false, nil
}

func syncOnePage(ctx context.Context, st store.Store, client *pcgw.Client, runID string, pageID int64, p pcgw.PageInfo, stats *store.PCGWSyncRunStats, filters PCGWFilters) (int, error) {
	result, err := pcgw.IngestPage(client, pageID, p)
	if err != nil {
		return 0, err
	}
	n, persistErr := PersistIngestResult(ctx, st, runID, result, filters)
	if persistErr != nil {
		return n, persistErr
	}
	switch result.Bundle.ParseStatus {
	case "ok":
		stats.GamesOK++
	case "partial":
		stats.GamesPartial++
	default:
		if len(result.Errors) > 0 {
			stats.GamesFailed++
		} else {
			stats.GamesOK++
		}
	}
	return n, nil
}

// PersistIngestResult writes ingest bundle to pcgw_* tables and projects manifest paths.
func PersistIngestResult(ctx context.Context, st store.Store, syncRunID string, result *pcgw.IngestResult, filters PCGWFilters) (int, error) {
	if result == nil {
		return 0, nil
	}
	b := result.Bundle
	gameID := strconv.FormatInt(b.PageID, 10)
	now := time.Now().UTC().Format(time.RFC3339)

	platforms := []string{}
	gameDataOK := false

	// Upsert game row
	g := &types.PCGWGame{
		PageID:           b.PageID,
		PageName:         b.PageInfo.Title,
		Title:            b.PageInfo.Title,
		SteamAppIDs:      b.PageInfo.SteamAppIDs,
		GOGID:            b.PageInfo.GOGID,
		EpicID:           b.PageInfo.EpicID,
		UbisoftID:        b.PageInfo.UbisoftID,
		HLTBID:           b.PageInfo.HLTBID,
		IGDBID:           b.PageInfo.IGDBID,
		CoverURL:         b.PageInfo.CoverURL,
		LastRevID:        b.RevisionID,
		LastRevTimestamp: b.RevisionTimestamp,
		LastFetchedAt:    now,
		ParseStatus:      b.ParseStatus,
		PlatformsPresent: platforms,
	}
	if len(b.Infobox) > 0 {
		infobox := map[string]interface{}{}
		for k, v := range b.Infobox {
			infobox[k] = v
		}
		g.Infobox = infobox
	}
	if err := st.UpsertPCGWGame(ctx, g); err != nil {
		return 0, err
	}

	// Metadata
	contentHash := hashWikitext(b.FullWikitext)
	sectionHashes := map[string]string{}
	for k, sr := range b.Sections {
		sectionHashes[k] = hashWikitext(sr.SectionWikitext)
	}
	storeFull := os.Getenv("GSBS_PCGW_STORE_FULL_WIKITEXT")
	if storeFull == "" || storeFull == "true" || storeFull == "1" {
		// default true
	} else {
		b.FullWikitextZstd = nil
	}
	meta := &types.PCGWMetadata{
		PageID:           b.PageID,
		FullWikitextZstd: b.FullWikitextZstd,
		ContentHash:      contentHash,
		SectionHashes:    sectionHashes,
		UncompressedSize: len(b.FullWikitext),
		LastFetchedAt:    now,
	}
	if err := st.UpsertPCGWMetadata(ctx, meta); err != nil {
		return 0, err
	}

	// Sections — only upsert on success (§4 resilience)
	for key, sr := range b.Sections {
		if sr.ParseError != "" {
			_ = st.InsertPCGWParseFailure(ctx, &types.PCGWParseFailure{
				PageID: b.PageID, SyncRunID: syncRunID, Section: key,
				ErrorMessage: sr.ParseError, WikitextSnippet: truncate(snippet(sr.SectionWikitext), 500),
			})
			continue
		}
		row := &types.PCGWSectionRow{
			PageID: b.PageID, Data: sr.Data, AllTemplates: sr.AllTemplates,
			SectionWikitext: sr.SectionWikitext, UpdatedAt: now,
		}
		if err := upsertSection(ctx, st, key, row); err != nil {
			log.Printf("pcgw persist: section %s page %d: %v", key, b.PageID, err)
		}
		if key == "game_data" {
			gameDataOK = true
			if err := persistGameDataPlatforms(ctx, st, b.PageID, sr, now, &platforms); err != nil {
				log.Printf("pcgw persist: game_data platforms page %d: %v", b.PageID, err)
			}
		}
	}

	// Update platforms on game
	g2, _ := st.GetPCGWGame(ctx, b.PageID)
	if g2 != nil {
		g2.PlatformsPresent = platforms
		g2.ParseStatus = b.ParseStatus
		_ = st.UpsertPCGWGame(ctx, g2)
	}

	// Project manifest v1 — only if game_data parsed OK or we have save locations
	var entries []types.GameSaveLocation
	if gameDataOK || len(b.SaveLocations) > 0 {
		for _, t := range b.SaveLocations {
			platform := pcgw.SystemToPlatform(t.System)
			for _, path := range t.Paths {
				rules := pcgw.ParseSaveRules(path, platform, t.IsConfig)
				for _, rule := range rules {
					if filters.ShouldExcludePath(rule.Directory) {
						continue
					}
					entries = append(entries, types.GameSaveLocation{
						GameID: gameID, PCGWPageID: b.PageID, GameTitle: b.PageInfo.Title,
						Platform: platform, PathTemplate: rule.Directory, IsConfig: t.IsConfig,
						SaveRules:   []types.SaveRule{rule},
						Source:      "pcgw",
						Notes:       "https://www.pcgamingwiki.com/wiki/?curid=" + gameID,
						SteamAppIDs: b.PageInfo.SteamAppIDs, GOGID: b.PageInfo.GOGID,
						EpicID: b.PageInfo.EpicID, UbisoftID: b.PageInfo.UbisoftID,
						UpdatedAt: now,
					})
				}
			}
		}
		if gameDataOK && len(entries) > 0 {
			if err := st.ReplaceGameSaveLocationsForGame(ctx, gameID, entries); err != nil {
				return 0, err
			}
			return len(entries), nil
		}
	}
	return 0, nil
}

func upsertSection(ctx context.Context, st store.Store, key string, row *types.PCGWSectionRow) error {
	switch key {
	case "availability":
		return st.UpsertPCGWAvailability(ctx, row)
	case "monetization":
		return st.UpsertPCGWMonetization(ctx, row)
	case "video":
		return st.UpsertPCGWVideo(ctx, row)
	case "input":
		return st.UpsertPCGWInput(ctx, row)
	case "audio":
		return st.UpsertPCGWAudio(ctx, row)
	case "network":
		return st.UpsertPCGWNetwork(ctx, row)
	case "notes":
		return st.UpsertPCGWNotes(ctx, row)
	case "references":
		return st.UpsertPCGWReferences(ctx, row)
	case "external_links":
		return st.UpsertPCGWExternalLinks(ctx, row)
	case "other", "lead", "system_requirements":
		return st.UpsertPCGWOther(ctx, row)
	default:
		if row.Data == nil {
			row.Data = map[string]interface{}{}
		}
		row.Data["section_key"] = key
		return st.UpsertPCGWOther(ctx, row)
	}
}

func persistGameDataPlatforms(ctx context.Context, st store.Store, pageID int64, sr pcgw.SectionResult, now string, platforms *[]string) error {
	byPlatform := map[string]*types.PCGWGameData{}

	addPaths := func(system string, paths []string, isConfig bool) {
		if len(paths) == 0 {
			return
		}
		pk := pcgw.SystemToPlatform(system)
		if *platforms != nil {
			found := false
			for _, p := range *platforms {
				if p == pk {
					found = true
					break
				}
			}
			if !found {
				*platforms = append(*platforms, pk)
			}
		}
		gd, ok := byPlatform[pk]
		if !ok {
			gd = &types.PCGWGameData{PageID: pageID, PlatformKey: pk, PlatformRawLabel: system}
			byPlatform[pk] = gd
		}
		entry := types.PCGWPathEntry{PathTemplates: paths, IsConfig: isConfig}
		if isConfig {
			gd.ConfigLocations = append(gd.ConfigLocations, entry)
		} else {
			gd.SaveLocations = append(gd.SaveLocations, entry)
		}
	}

	if templates, ok := sr.Data["templates"].([]pcgw.SaveLocationTemplate); ok {
		for _, t := range templates {
			addPaths(t.System, t.Paths, t.IsConfig)
		}
	}

	keep := make([]string, 0, len(byPlatform))
	for pk, gd := range byPlatform {
		gd.AllTemplates = sr.AllTemplates
		gd.SectionWikitext = sr.SectionWikitext
		gd.UpdatedAt = now
		if err := st.UpsertPCGWGameData(ctx, gd); err != nil {
			return err
		}
		keep = append(keep, pk)
	}
	return st.DeletePCGWGameDataExcept(ctx, pageID, keep)
}

func hashWikitext(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func snippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

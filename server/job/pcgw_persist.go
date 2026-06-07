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
	ForceFull        bool  // bypass resume checkpoint
	SinglePage       int64 // if >0, sync only this page
	Offset           int   // resume offset for full/incremental list
	SyncRunID        string
	ResumedFromRunID string
	Notes            string
	SkipStartRun     bool // use SyncRunID instead of StartPCGWSyncRun
	// MaxPagesPerRun caps Phase 2 ingest budget per run (0 = use env/default).
	MaxPagesPerRun int
	// ResumeCatalogScan skips Phase 1 and goes straight to Phase 2 (resume from checkpoint).
	ResumeCatalogScan bool
	// ResumeQueueCursor is the Phase 2 queue position to resume from.
	ResumeQueueCursor int
	// SkipCatalogScan is true when running catalog-only (no Phase 2).
	SkipIngestPhase bool
	// RetryFailedOnly limits Phase 2 queue to failed/partial IDs only.
	RetryFailedOnly bool
	// RebuildManifestOnly skips all ingest and just bumps the manifest.
	RebuildManifestOnly bool
}

// PCGWSyncProgress reports sync progress for SSE.
type PCGWSyncProgress struct {
	PagesProcessed int
	TotalEstimate  int
	Phase          string
	GamesSkipped   int
	QueueSize      int
	QueueCursor    int
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
// It implements the two-phase pipeline: Phase 1 = catalog scan, Phase 2 = targeted ingest.
func PCGWSyncEx(ctx context.Context, st store.Store, client *pcgw.Client, reportProgress ReportProgress, reportEx ReportProgressEx, opts PCGWSyncOptions) (int, error) {
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
	parseMsTotal := 0
	parseCount := 0
	filters := LoadPCGWFilters(ctx, st)

	finishRun := func(status, errMsg string) {
		_ = st.FinishPCGWSyncRun(context.Background(), runID, status, errMsg, stats)
	}

	// ─── Single-page mode (unchanged) ────────────────────────────────────────
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

	// ─── Manifest rebuild only ────────────────────────────────────────────────
	if opts.RebuildManifestOnly {
		finishRun(JobSuccess, "")
		return bumpManifestAndReturn(ctx, st, 0)
	}

	budget := opts.MaxPagesPerRun
	if budget <= 0 {
		budget = MaxPagesPerRun()
	}

	// ─── Phase 1: Catalog scan ────────────────────────────────────────────────
	var phase1 types.Phase1Stats
	if !opts.ResumeCatalogScan {
		var err error
		phase1, err = RunCatalogScan(ctx, st, client, runID, reportEx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				_ = st.UpdatePCGWSyncRunCheckpoint(ctx, runID, 0, stats)
				finishRun(ctxStatus(ctx), ctx.Err().Error())
				return totalUpserted, ctx.Err()
			}
			finishRun(JobFailed, err.Error())
			return totalUpserted, err
		}
		stats.GamesTotal = phase1.RemoteTotalIDs
	} else {
		// Resuming into Phase 2: load Phase 1 stats from prior run.
		if latest, err := st.GetLatestPCGWSyncRun(ctx); err == nil && latest != nil {
			phase1.RemoteTotalIDs = latest.RemoteTotalIDs
			phase1.CatalogHash = latest.CatalogHash
			phase1.MissingLocalIDs = latest.MissingLocalIDs
		}
	}

	if opts.SkipIngestPhase {
		finishRun(JobSuccess, "")
		return 0, nil
	}

	// ─── Catalog hash no-op optimization ─────────────────────────────────────
	if !opts.Full && !opts.RetryFailedOnly && phase1.CatalogHash != "" {
		if prev, err := getPreviousCatalogHash(ctx, st); err == nil && prev == phase1.CatalogHash {
			// No new IDs and no known failures — skip Phase 2.
			failedCount := 0
			if ids, err := st.ListPCGWCatalogFailedPartial(ctx, 1, 0); err == nil {
				failedCount = len(ids)
			}
			if failedCount == 0 {
				log.Printf("pcgw sync: catalog hash unchanged, no pending failures — skipping Phase 2")
				finishRun(JobSuccess, "")
				return bumpManifestAndReturn(ctx, st, 0)
			}
		}
	}

	// ─── Phase 2: Build targeted queue ───────────────────────────────────────
	var queue []int64

	if !opts.RetryFailedOnly {
		// Priority 1: missing IDs
		missing, err := st.ListPCGWCatalogMissing(ctx, 0, 0)
		if err != nil {
			log.Printf("pcgw sync: list missing: %v", err)
		} else {
			queue = append(queue, missing...)
		}
	}

	// Priority 2: failed/partial IDs
	failedPartial, err := st.ListPCGWCatalogFailedPartial(ctx, 0, 0)
	if err != nil {
		log.Printf("pcgw sync: list failed/partial: %v", err)
	} else {
		// Deduplicate against already-queued IDs.
		inQueue := make(map[int64]bool, len(queue))
		for _, id := range queue {
			inQueue[id] = true
		}
		for _, id := range failedPartial {
			if !inQueue[id] {
				queue = append(queue, id)
				inQueue[id] = true
			}
		}
	}

	// Priority 3: changed rev IDs (only on full or incremental when not retry-only)
	if !opts.RetryFailedOnly {
		changedIDs, err := buildChangedQueue(ctx, st, client, filters)
		if err != nil {
			log.Printf("pcgw sync: build changed queue: %v", err)
		} else {
			inQueue := make(map[int64]bool, len(queue))
			for _, id := range queue {
				inQueue[id] = true
			}
			for _, id := range changedIDs {
				if !inQueue[id] {
					queue = append(queue, id)
				}
			}
		}
	}

	queueSize := len(queue)
	_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, 0, 0)

	// Persist queue size into targeted_queue_size column.
	if runID != "" {
		_ = st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, types.Phase1Stats{
			RemoteTotalIDs:  phase1.RemoteTotalIDs,
			MissingLocalIDs: len(queue),
			ExtraLocalIDs:   phase1.ExtraLocalIDs,
			CatalogHash:     phase1.CatalogHash,
			CompletedAt:     phase1.CompletedAt,
		})
	}

	log.Printf("pcgw sync phase2: queue=%d budget=%d (resume_cursor=%d)", queueSize, budget, opts.ResumeQueueCursor)

	// ─── Phase 2: Process queue with budget ───────────────────────────────────
	startCursor := opts.ResumeQueueCursor
	processed := 0

	for i := startCursor; i < len(queue); i++ {
		select {
		case <-ctx.Done():
			_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, processed, i)
			finishRun(ctxStatus(ctx), ctx.Err().Error())
			return totalUpserted, ctx.Err()
		default:
		}

		if budget > 0 && processed >= budget {
			// Budget exhausted — save checkpoint for resume.
			_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, processed, i)
			finishRun(JobInterrupted, "budget exhausted")
			log.Printf("pcgw sync: budget %d exhausted at cursor %d/%d", budget, i, queueSize)
			return bumpManifestAndReturn(ctx, st, totalUpserted)
		}

		pageID := queue[i]

		if reportProgress != nil {
			reportProgress(processed)
		}
		if reportEx != nil {
			reportEx(PCGWSyncProgress{
				PagesProcessed: processed,
				TotalEstimate:  queueSize,
				Phase:          "ingest",
				GamesSkipped:   stats.GamesSkipped,
				QueueSize:      queueSize,
				QueueCursor:    i,
			})
		}

		// Determine title from catalog (best-effort).
		pageInfo := pcgw.PageInfo{PageID: pageID}

		start := time.Now()
		n, err := syncOnePage(ctx, st, client, runID, pageID, pageInfo, &stats, filters)
		parseMsTotal += int(time.Since(start).Milliseconds())
		parseCount++
		processed++

		if err != nil {
			log.Printf("pcgw sync: page %d: %v", pageID, err)
			stats.GamesFailed++
			_ = st.IncrementCatalogRetry(ctx, pageID, err.Error())
			continue
		}
		// Clear dead-letter on success.
		_ = st.ClearCatalogDeadLetter(ctx, pageID)
		totalUpserted += n

		// Checkpoint every 100 pages.
		if processed%100 == 0 {
			_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, processed, i+1)
		}
	}

	if parseCount > 0 {
		stats.AvgParseMs = parseMsTotal / parseCount
	}
	_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID, processed, len(queue))

	status := JobSuccess
	if stats.GamesFailed > 0 {
		status = "partial"
	}
	finishRun(status, "")
	log.Printf("pcgw sync: done, upserted %d location entries (ok=%d partial=%d failed=%d skipped=%d)",
		totalUpserted, stats.GamesOK, stats.GamesPartial, stats.GamesFailed, stats.GamesSkipped)
	return bumpManifestAndReturn(ctx, st, totalUpserted)
}

// ctxStatus returns the job status string based on context error.
func ctxStatus(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return JobCanceled
	}
	return JobInterrupted
}

// getPreviousCatalogHash retrieves the catalog_hash from the last completed sync run.
func getPreviousCatalogHash(ctx context.Context, st store.Store) (string, error) {
	runs, err := st.ListPCGWSyncRuns(ctx, 5)
	if err != nil {
		return "", err
	}
	for _, r := range runs {
		if r.Status == JobSuccess || r.Status == "partial" {
			if r.CatalogHash != "" {
				return r.CatalogHash, nil
			}
		}
	}
	return "", nil
}

// buildChangedQueue returns page IDs from the catalog that have changed rev IDs
// or are not yet in pcgw_games (the "rev-check changed" set, priority 3).
// This is the set where shouldSkipPage returns false — i.e. pages we do NOT skip.
func buildChangedQueue(ctx context.Context, st store.Store, client *pcgw.Client, filters PCGWFilters) ([]int64, error) {
	// We retrieve pages from the catalog that exist in pcgw_games but may have changed.
	// Use an offset-based scan; for very large catalogs this is the slow path.
	var changed []int64
	offset := 0
	const chunkSize = 200
	for {
		select {
		case <-ctx.Done():
			return changed, ctx.Err()
		default:
		}
		rows, _, err := st.ListPCGWGames(ctx, store.PCGWGameListFilter{
			ParseStatus: "ok",
			Limit:       chunkSize,
			Offset:      offset,
		})
		if err != nil {
			return changed, err
		}
		if len(rows) == 0 {
			break
		}
		for _, g := range rows {
			if filters.ShouldSkipTitle(g.Title) {
				continue
			}
			skip, err := shouldSkipPage(ctx, st, client, g.PageID)
			if err != nil || !skip {
				changed = append(changed, g.PageID)
			}
		}
		if len(rows) < chunkSize {
			break
		}
		offset += len(rows)
	}
	return changed, nil
}

func bumpManifestAndReturn(ctx context.Context, st store.Store, n int) (int, error) {
	// Derive a stable ETag from the next version number so identical content
	// always produces the same ETag (no time.Now() noise).
	newVersion := 1
	if meta, err := st.GetPCGWManifestMeta(ctx); err == nil && meta != nil {
		newVersion = meta.ManifestVersion + 1
	}
	etag := store.ManifestETagFromGames(newVersion)
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
		// slotCounts tracks per-(platform, isConfig) position so that the Windows
		// and Linux templates for the same logical save slot share the same SlotLabel.
		slotCounts := map[string]int{}
		for _, t := range b.SaveLocations {
			platform := pcgw.SystemToPlatform(t.System)
			sk := platform + "\x00" + strconv.FormatBool(t.IsConfig)
			slotLabel := strconv.Itoa(slotCounts[sk])
			slotCounts[sk]++
			for _, path := range t.Paths {
				rules := pcgw.ParseSaveRules(path, platform, t.IsConfig)
				for _, rule := range rules {
					if filters.ShouldExcludePath(rule.Directory) {
						continue
					}
					rule.SlotLabel = slotLabel
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

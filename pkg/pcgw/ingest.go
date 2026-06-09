package pcgw

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// IngestPage fetches and parses a PCGW game page with section-level resilience.
func IngestPage(ctx context.Context, client *Client, pageID int64, pageInfo PageInfo) (*IngestResult, error) {
	if client == nil {
		return nil, fmt.Errorf("client is nil")
	}
	pageIDStr := strconv.FormatInt(pageID, 10)
	gameID := pageIDStr
	if pageInfo.PageID == 0 {
		pageInfo.PageID = pageID
	}

	result := &IngestResult{
		Bundle: GameBundle{
			PageID:   pageID,
			PageInfo: pageInfo,
			Sections: make(map[string]SectionResult),
		},
	}

	wikitext, pageTitle, err := client.ParsePageWikitext(ctx, pageIDStr)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Bundle.ParseStatus = "failed"
		return result, err
	}
	if strings.TrimSpace(result.Bundle.PageInfo.Title) == "" {
		result.Bundle.PageInfo.Title = strings.TrimSpace(pageTitle)
	}
	result.Bundle.FullWikitext = wikitext
	result.Bundle.AllTemplates = ExtractAllTemplates(wikitext)

	if storeFull := os.Getenv("GSBS_PCGW_STORE_FULL_WIKITEXT"); storeFull == "false" || storeFull == "0" {
		// skip compression
	} else {
		if compressed, err := CompressWikitext([]byte(wikitext)); err == nil {
			result.Bundle.FullWikitextZstd = compressed
		} else {
			result.Errors = append(result.Errors, "compress: "+err.Error())
		}
	}

	if rev, err := client.GetPageRevision(ctx, pageIDStr); err == nil {
		result.Bundle.RevisionID = rev.RevID
		result.Bundle.RevisionTimestamp = rev.Timestamp
	} else {
		result.Errors = append(result.Errors, "revision: "+err.Error())
	}

	result.Bundle.Infobox = ParseInfoboxGame(wikitext)
	if hltb := result.Bundle.Infobox["HLTB"]; hltb != "" {
		result.Bundle.PageInfo.HLTBID = hltb
	}
	if igdb := result.Bundle.Infobox["IGDB"]; igdb != "" {
		result.Bundle.PageInfo.IGDBID = igdb
	}

	rawSections := SplitWikiSections(wikitext)
	var failed []string
	for key, sec := range rawSections {
		sr := SectionResult{
			Key:             key,
			RawTitle:        sec.rawTitle,
			SectionWikitext: sec.body,
			AllTemplates:    ExtractAllTemplates(sec.body),
		}
		data, parseErr := parseSectionStructured(key, sec.body, gameID)
		if parseErr != nil {
			sr.ParseError = parseErr.Error()
			failed = append(failed, key)
			result.Errors = append(result.Errors, fmt.Sprintf("section %s: %s", key, parseErr.Error()))
		} else {
			sr.Data = data
		}
		result.Bundle.Sections[key] = sr
	}

	if gd, ok := result.Bundle.Sections["game_data"]; ok && gd.ParseError == "" {
		if locs, ok := gd.Data["templates"].([]SaveLocationTemplate); ok {
			result.Bundle.SaveLocations = locs
		}
	}
	if len(result.Bundle.SaveLocations) == 0 {
		result.Bundle.SaveLocations = ParseSaveLocationsFromWikitext(wikitext, gameID)
	}

	result.FailedSections = failed
	result.Bundle.FailedSections = failed
	switch {
	case len(failed) == 0:
		result.Bundle.ParseStatus = "ok"
	case len(failed) < len(rawSections):
		result.Bundle.ParseStatus = "partial"
	default:
		result.Bundle.ParseStatus = "partial"
	}
	return result, nil
}

func parseSectionStructured(key, body, gameID string) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"all_templates": ExtractAllTemplates(body),
	}
	switch key {
	case "game_data":
		templates, structured := ParseGameDataSection(body, gameID)
		data["templates"] = templates
		for k, v := range structured {
			data[k] = v
		}
		return data, nil
	case "availability":
		data["raw"] = body
		return data, nil
	case "lead":
		infobox := ParseInfoboxGame(body)
		if len(infobox) > 0 {
			data["infobox"] = infobox
		}
		return data, nil
	default:
		data["raw"] = body
		return data, nil
	}
}

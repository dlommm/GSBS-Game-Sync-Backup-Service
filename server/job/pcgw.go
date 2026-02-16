package job

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/store"
)

// ReportProgress is an optional callback to report progress (e.g. pages processed). May be nil.
type ReportProgress func(pagesProcessed int)

// PCGWSync runs a full sync: list game pages from PCGW (Cargo Infobox_game), fetch wikitext,
// parse save locations, upsert into store. Rate limits to 1 request per second.
// If reportProgress is non-nil, it is called with the number of pages processed so far.
// Returns the number of upserted entries and any error.
func PCGWSync(ctx context.Context, st store.Store, client *pcgw.Client, reportProgress ReportProgress) (int, error) {
	const rateLimit = time.Second
	const chunkSize = 100
	offset := 0
	totalUpserted := 0
	for {
		pages, err := client.ListGamePages(chunkSize, offset)
		if err != nil {
			return totalUpserted, err
		}
		if len(pages) == 0 {
			break
		}
		if reportProgress != nil {
			reportProgress(offset + len(pages))
		}
		for _, p := range pages {
			select {
			case <-ctx.Done():
				return totalUpserted, ctx.Err()
			default:
			}
			wikitext, err := client.ParsePageWikitext(strconv.FormatInt(p.PageID, 10))
			if err != nil {
				log.Printf("pcgw sync: page %d %q: %v", p.PageID, p.Title, err)
				time.Sleep(rateLimit)
				continue
			}
			templates := pcgw.ParseSaveLocationsFromWikitext(wikitext, strconv.FormatInt(p.PageID, 10))
			var entries []types.GameSaveLocation
			for _, t := range templates {
				platform := pcgw.SystemToPlatform(t.System)
				for _, path := range t.Paths {
					if path == "" {
						continue
					}
					entries = append(entries, types.GameSaveLocation{
						GameID:       strconv.FormatInt(p.PageID, 10),
						PCGWPageID:   p.PageID,
						GameTitle:    p.Title,
						Platform:     platform,
						PathTemplate: path,
						IsConfig:     t.IsConfig,
						Source:       "pcgw",
						Notes:        "https://www.pcgamingwiki.com/wiki/?curid=" + strconv.FormatInt(p.PageID, 10),
					})
				}
			}
			if len(entries) > 0 {
				if err := st.UpsertGameSaveLocations(ctx, entries); err != nil {
					log.Printf("pcgw sync: upsert %d entries for page %d: %v", len(entries), p.PageID, err)
				} else {
					totalUpserted += len(entries)
				}
			}
			time.Sleep(rateLimit)
		}
		if len(pages) < chunkSize {
			break
		}
		offset += len(pages)
	}
	log.Printf("pcgw sync: done, upserted %d location entries", totalUpserted)
	return totalUpserted, nil
}

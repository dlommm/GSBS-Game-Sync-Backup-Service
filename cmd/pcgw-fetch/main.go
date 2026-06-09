// Command pcgw-fetch ingests one or more PCGW game pages (debug / backfill).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/store"
)

func main() {
	steamAppID := flag.String("steam-appid", "", "Steam App ID")
	pageID := flag.Int64("page-id", 0, "PCGW page ID")
	batchSteam := flag.String("batch-steam", "", "Comma-separated Steam App IDs")
	dbPath := flag.String("db", "", "SQLite path (optional; if set, persist to DB)")
	flag.Parse()

	client := pcgw.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var st store.Store
	if *dbPath != "" {
		s, err := store.NewSQLite(*dbPath)
		if err != nil {
			log.Fatal(err)
		}
		defer s.Close()
		st = s
	}

	ids := parseBatch(*batchSteam)
	if *steamAppID != "" {
		ids = append(ids, *steamAppID)
	}

	if len(ids) > 0 {
		for _, id := range ids {
			pid, err := client.GetPageIDBySteamAppID(ctx, id)
			if err != nil {
				log.Printf("steam %s: %v", id, err)
				continue
			}
			pageIDInt, _ := strconv.ParseInt(pid, 10, 64)
			runOne(ctx, client, st, pageIDInt)
		}
		return
	}

	if *pageID > 0 {
		runOne(ctx, client, st, *pageID)
		return
	}

	log.Fatal("specify -steam-appid, -page-id, or -batch-steam")
}

func parseBatch(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runOne(ctx context.Context, client *pcgw.Client, st store.Store, pageID int64) {
	if st != nil {
		n, err := job.PCGWSyncPage(ctx, st, client, pageID)
		if err != nil {
			log.Printf("page %d: %v", pageID, err)
			return
		}
		fmt.Printf("page %d: persisted %d manifest paths\n", pageID, n)
		return
	}
	result, err := pcgw.IngestPage(ctx, client, pageID, pcgw.PageInfo{PageID: pageID})
	if err != nil {
		log.Fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

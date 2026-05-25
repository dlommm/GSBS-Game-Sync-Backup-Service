// Command pcgw-sync syncs PCGamingWiki game data into a GSBS SQLite database.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/store"
)

func main() {
	dbPath := flag.String("db", envOr("GSBS_DB", "gsbs.db"), "SQLite database path")
	full := flag.Bool("full", false, "Full resync (skip incremental)")
	offset := flag.Int("offset", 0, "Resume list offset")
	flag.Parse()

	st, err := store.NewSQLite(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()

	client := pcgw.NewClient()
	runID, err := st.StartPCGWSyncRun(ctx, syncMode(*full))
	if err != nil {
		log.Fatal(err)
	}
	n, err := job.PCGWSync(ctx, st, client, func(pages int) {
		if pages%10 == 0 {
			fmt.Printf("pages: %d\n", pages)
		}
	}, job.PCGWSyncOptions{Full: *full, Offset: *offset, SyncRunID: runID})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("done: %d manifest path rows upserted\n", n)
}

func syncMode(full bool) string {
	if full {
		return "full"
	}
	return "incremental"
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

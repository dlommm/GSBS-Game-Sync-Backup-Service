// Command pcgw-sync runs a one-off PCGW manifest sync: list game pages, fetch wikitext,
// parse save locations, upsert into the server DB. For use from cron or manually.
// Usage: pcgw-sync [options]
// Env: GSBS_DB path to SQLite DB (default gsbs.db).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/store"
)

func main() {
	dbPath := os.Getenv("GSBS_DB")
	if dbPath == "" {
		dbPath = "gsbs.db"
	}
	st, err := store.NewSQLite(dbPath)
	if err != nil {
		log.Fatal("store:", err)
	}
	defer st.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		cancel()
	}()

	ctx, timeoutCancel := context.WithTimeout(ctx, 24*time.Hour)
	defer timeoutCancel()

	client := pcgw.NewClient()
	if err := job.PCGWSync(ctx, st, client); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
	log.Println("pcgw-sync finished")
}

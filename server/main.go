package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/api"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/store"
	"github.com/gsbs/gsbs/server/webui"
	"github.com/robfig/cron/v3"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version" || os.Args[1] == "-v") {
		println("gsbs-server", Version)
		return
	}
	dbPath := os.Getenv("GSBS_DB")
	if dbPath == "" {
		dbPath = "gsbs.db"
	}
	st, err := store.NewSQLite(dbPath)
	if err != nil {
		log.Fatal("store:", err)
	}
	defer st.Close()

	authSvc := auth.NewService(st)
	apiHandler := api.NewHandler(st, authSvc)
	// GSBS_ADMIN_USERNAME: if set, only this user can access /admin (stats and revoke client tokens).
	webHandler := webui.NewWebHandler(st, authSvc, os.Getenv("GSBS_SESSION_SECRET"), os.Getenv("GSBS_ADMIN_USERNAME"))
	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(webui.StaticFiles())))
	mux.Handle("/", webHandler)
	handler := mux

	// Weekly PCGW manifest sync (Sunday 03:00)
	c := cron.New()
	_, _ = c.AddFunc("0 3 * * 0", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
		defer cancel()
		pcgwClient := pcgw.NewClient()
		if err := job.PCGWSync(ctx, st, pcgwClient); err != nil {
			log.Printf("pcgw weekly sync: %v", err)
		}
	})
	c.Start()
	defer c.Stop()

	addr := os.Getenv("GSBS_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Println("GSBS server listening on", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

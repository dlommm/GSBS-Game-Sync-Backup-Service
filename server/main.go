package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
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
		printVersion()
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

	// GSBS_ALLOW_REGISTER: "false" disables public registration; default is true.
	allowRegister := true
	if v := os.Getenv("GSBS_ALLOW_REGISTER"); strings.EqualFold(v, "false") || v == "0" {
		allowRegister = false
		log.Println("Public registration is DISABLED (set GSBS_ALLOW_REGISTER=true to enable)")
	}

	sessionSecret := os.Getenv("GSBS_SESSION_SECRET")
	if sessionSecret == "" || sessionSecret == "gsbs-default-secret-change-me" {
		log.Println("WARNING: GSBS_SESSION_SECRET is not set or is default; set a strong secret in production")
	}

	apiHandler := api.NewHandler(st, authSvc, allowRegister)
	webHandler := webui.NewWebHandler(st, authSvc, sessionSecret, os.Getenv("GSBS_ADMIN_USERNAME"), allowRegister)
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
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		log.Println("GSBS server listening on", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
	log.Println("server stopped")
}

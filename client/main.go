package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"runtime"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "login":
			runLogin()
			return
		case "login-dialog":
			runLoginDialogProcess()
			return
		case "list":
			runList()
			return
		case "--version", "-version", "-v":
			println("gsbs-client", Version)
			return
		}
	}

	// On Windows, run tray (setup/login is via browser; see tray "Login / Setup...").
	if runtime.GOOS == "windows" && !consoleMode() {
		runTray()
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal("config:", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	syncNow := make(chan struct{})
	go func() {
		if err := runSync(ctx, cfg, syncNow); err != nil {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	cancel()
	log.Println("shutdown")
}

func consoleMode() bool {
	for _, a := range os.Args[1:] {
		if a == "--console" || a == "-console" {
			return true
		}
	}
	return false
}

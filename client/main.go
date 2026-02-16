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
		case "--version", "-version", "-v":
			printVersion()
			return
		}
	}

	// Init log to file (and optionally stderr) so all client activity is recorded.
	alsoStderr := runtime.GOOS != "windows" || consoleMode()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "login", "login-dialog", "list":
			alsoStderr = true
		}
	}
	InitClientLog(alsoStderr)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "login":
			runLogin()
			return
		case "login-dialog":
			runLoginDialogProcess()
			return
		case "list":
			dryRunPull := false
			for _, a := range os.Args[2:] {
				if a == "--dry-run-pull" {
					dryRunPull = true
					break
				}
			}
			runList(dryRunPull)
			return
		}
	}

	// On Windows or Linux, run tray when not in console mode (setup/login via browser or Login menu).
	if (runtime.GOOS == "windows" || runtime.GOOS == "linux") && !consoleMode() {
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
	refreshManifest := make(chan struct{})
	go func() {
		if err := runSync(ctx, cfg, syncNow, refreshManifest); err != nil {
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

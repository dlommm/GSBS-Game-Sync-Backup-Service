//go:build !windows && !linux && !darwin

package main

import "os"

// runTray is implemented on Windows (tray_windows.go), Linux (tray_linux.go),
// and macOS (tray_darwin.go). This stub is for any other OS.
func runTray() {
	panic("tray only on Windows, Linux, and macOS")
}

// runLoginDialogProcess runs the GUI login dialog; only implemented on Windows.
func runLoginDialogProcess() {
	os.Exit(1)
}

// runFirstTimeSetupIfNeeded runs console login before tray when config is missing; only on Windows.
func runFirstTimeSetupIfNeeded() {}

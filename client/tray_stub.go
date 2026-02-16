//go:build !windows && !linux

package main

import "os"

// runTray is implemented on Windows (tray_windows.go) and Linux (tray_linux.go). This stub is for other OSes.
func runTray() {
	panic("tray only on Windows and Linux")
}

// runLoginDialogProcess runs the GUI login dialog; only implemented on Windows.
func runLoginDialogProcess() {
	os.Exit(1)
}

// runFirstTimeSetupIfNeeded runs console login before tray when config is missing; only on Windows.
func runFirstTimeSetupIfNeeded() {}

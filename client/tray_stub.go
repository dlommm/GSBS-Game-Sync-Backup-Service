//go:build !windows

package main

import "os"

// runTray is only implemented on Windows (tray_windows.go). This stub is never
// called because main only invokes runTray when runtime.GOOS == "windows".
func runTray() {
	panic("tray only on Windows")
}

// runLoginDialogProcess runs the GUI login dialog; only implemented on Windows.
func runLoginDialogProcess() {
	os.Exit(1)
}

// runFirstTimeSetupIfNeeded runs console login before tray when config is missing; only on Windows.
func runFirstTimeSetupIfNeeded() {}

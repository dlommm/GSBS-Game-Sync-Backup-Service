//go:build !windows

package main

// runTray is only implemented on Windows (tray_windows.go). This stub is never
// called because main only invokes runTray when runtime.GOOS == "windows".
func runTray() {
	panic("tray only on Windows")
}

package main

import (
	"os"
	"sync"
)

// flatpakAppID is the GSBS Flatpak application ID (see flatpak/ and packaging).
const flatpakAppID = "io.github.dlommm.GSBS"

var (
	flatpakOnce sync.Once
	flatpakVal  bool
)

// isFlatpak reports whether the client is running inside a Flatpak sandbox.
// Detected via the FLATPAK_ID env var or the /.flatpak-info file the runtime
// always mounts. Cached after the first call.
func isFlatpak() bool {
	flatpakOnce.Do(func() {
		if os.Getenv("FLATPAK_ID") != "" {
			flatpakVal = true
			return
		}
		if _, err := os.Stat("/.flatpak-info"); err == nil {
			flatpakVal = true
		}
	})
	return flatpakVal
}

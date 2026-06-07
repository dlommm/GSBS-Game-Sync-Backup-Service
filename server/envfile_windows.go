//go:build windows

package main

import (
	"os"
	"path/filepath"
)

func defaultEnvFilePath() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "GSBS", "server.env")
}

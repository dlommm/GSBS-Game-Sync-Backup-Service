//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func applyUpdatePlatform(stagedPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable path: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("abs path: %w", err)
	}
	stagedPath, err = filepath.Abs(stagedPath)
	if err != nil {
		return fmt.Errorf("abs staged path: %w", err)
	}

	dir := filepath.Join(ClientDataDir(), "updates")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	batch := filepath.Join(dir, "apply-update.bat")
	script := fmt.Sprintf(`@echo off
timeout /t 2 /nobreak >nul
move /Y "%s" "%s"
start "" "%s" --minimized
del "%%~f0"
`, stagedPath, exe, exe)
	if err := os.WriteFile(batch, []byte(script), 0600); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/C", batch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start updater: %w", err)
	}
	// NOTE: The .bat runs detached; we cannot observe whether the `move` or
	// restart succeeded from here. Failures (e.g. file locked) are silent on
	// the Go side. The user will see the old binary on next launch if it failed.
	log.Printf("update: Windows apply script launched; current process will exit for restart")
	return nil
}

func applyStagedBinary(stagedPath string) error {
	return fmt.Errorf("apply-update mode is not used on Windows")
}

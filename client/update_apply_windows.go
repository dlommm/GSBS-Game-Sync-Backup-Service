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
	// Retry taking ownership of the exe (the exiting process may still hold
	// it), swap in the staged binary, and roll back to the previous binary if
	// the swap fails. <exe>.old is kept for manual rollback; the client
	// removes it on the next normal start (CleanupOldUpdateBinary).
	script := fmt.Sprintf(`@echo off
timeout /t 2 /nobreak >nul
set /a tries=0
:movecur
move /Y "%[2]s" "%[3]s" >nul 2>&1
if not errorlevel 1 goto moved
set /a tries+=1
if %%tries%% lss 15 (
  timeout /t 1 /nobreak >nul
  goto movecur
)
rem Could not take ownership of the current binary; leave it in place.
goto launch
:moved
move /Y "%[1]s" "%[2]s" >nul 2>&1
if not errorlevel 1 goto launch
rem Swap failed; restore the previous binary.
move /Y "%[3]s" "%[2]s" >nul 2>&1
:launch
start "" "%[2]s" --minimized
del "%%~f0"
`, stagedPath, exe, exe+".old")
	if err := os.WriteFile(batch, []byte(script), 0600); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/C", batch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start updater: %w", err)
	}
	// The .bat runs detached; the Go side cannot observe the outcome, but the
	// script itself now retries the locked-file window and rolls back to the
	// previous binary on a failed swap, so the client always relaunches in a
	// working state.
	log.Printf("update: Windows apply script launched; current process will exit for restart")
	return nil
}

func applyStagedBinary(stagedPath string) error {
	return fmt.Errorf("apply-update mode is not used on Windows")
}

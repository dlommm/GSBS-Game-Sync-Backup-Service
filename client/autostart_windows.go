//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	runKeyName = "GSBS"
)

// RunAtStartupEnabled returns whether the client is registered to run at Windows startup.
func RunAtStartupEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.READ)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runKeyName)
	return err == nil
}

// SetRunAtStartup enables or disables running at Windows startup (Registry Run key).
func SetRunAtStartup(enabled bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open registry key: %w", err)
	}
	defer k.Close()
	if enabled {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("executable path: %w", err)
		}
		exe, err = filepath.Abs(exe)
		if err != nil {
			return fmt.Errorf("abs path: %w", err)
		}
		if err := k.SetStringValue(runKeyName, exe); err != nil {
			return fmt.Errorf("set run key: %w", err)
		}
	} else {
		if err := k.DeleteValue(runKeyName); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("delete run key: %w", err)
		}
	}
	return nil
}

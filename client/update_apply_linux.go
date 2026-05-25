//go:build linux

package main

import (
	"fmt"
	"io"
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

	// Relaunch self in apply mode so systray can exit and release the binary.
	cmd := exec.Command(exe, "--apply-update="+stagedPath, "--minimized")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start apply helper: %w", err)
	}
	return nil
}

func applyStagedBinary(stagedPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	stagedPath, err = filepath.Abs(stagedPath)
	if err != nil {
		return err
	}

	oldPath := exe + ".old"
	_ = os.Remove(oldPath)

	in, err := os.Open(stagedPath)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	tmpPath := exe + ".new"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	_ = os.Rename(exe, oldPath)
	if err := os.Rename(tmpPath, exe); err != nil {
		_ = os.Rename(oldPath, exe)
		return err
	}
	_ = os.Remove(oldPath)
	_ = os.Remove(stagedPath)

	cmd := exec.Command(exe, "--minimized")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restart client: %w", err)
	}
	return nil
}

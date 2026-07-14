//go:build darwin

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// appBundleRoot returns the .app bundle root for a binary at
// <bundle>/Contents/MacOS/<name>, or "" when running outside a bundle.
func appBundleRoot(exe string) string {
	sep := string(filepath.Separator)
	marker := sep + "Contents" + sep + "MacOS" + sep
	i := strings.LastIndex(exe, marker)
	if i < 0 {
		return ""
	}
	root := exe[:i]
	if !strings.HasSuffix(root, ".app") {
		return ""
	}
	return root
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

	// Replacing the binary breaks the bundle's ad-hoc signature seal; re-sign
	// the same way the DMG packaging does or Gatekeeper kills the next launch.
	// The bundle holds a single Mach-O in Contents/MacOS, so no --deep (Apple
	// deprecated it in macOS 13 and signing the bundle covers the binary).
	bundle := appBundleRoot(exe)
	if bundle != "" {
		if _, err := exec.LookPath("codesign"); err != nil {
			_ = os.Rename(oldPath, exe) // roll back to the still-signed old binary
			return fmt.Errorf("codesign not found on PATH; cannot re-seal the app bundle: %w", err)
		}
		if signOut, err := exec.Command("codesign", "--force", "--sign", "-", bundle).CombinedOutput(); err != nil {
			_ = os.Rename(oldPath, exe) // roll back to the still-signed old binary
			return fmt.Errorf("codesign: %v: %s", err, strings.TrimSpace(string(signOut)))
		}
	}
	_ = os.Remove(oldPath)
	_ = os.Remove(stagedPath)

	// Relaunch: through LaunchServices for a bundle, directly otherwise.
	var cmd *exec.Cmd
	if bundle != "" {
		cmd = exec.Command("open", bundle, "--args", "--minimized")
	} else {
		cmd = exec.Command(exe, "--minimized")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restart client: %w", err)
	}
	log.Printf("update: macOS apply complete; new binary at %s (bundle=%q)", exe, bundle)
	return nil
}

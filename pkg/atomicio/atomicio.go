// Package atomicio provides a crash-safe file write shared by the client's
// main package and its sync package. A write-fsync-rename sequence guarantees
// that a crash, disk-full event, or power loss never leaves a partially
// written file at the destination.
//
// The temporary file uses a unique name (os.CreateTemp) rather than a fixed
// suffix, so two goroutines writing the same destination concurrently never
// race on one temp inode (which previously tore config.json). It is created in
// the destination's directory so os.Rename stays on one filesystem (atomic).
package atomicio

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// TempSuffix marks a temporary file created by WriteFile.
//
// It is deliberately self-identifying rather than a bare ".tmp": the sync
// client's artifact filter looks for GSBS-owned files by name, and a plain
// "<base>-<rand>.tmp" matched nothing — a rescan racing a pull could pick a
// half-written temp file up and upload it as a junk server slot.
const TempSuffix = ".gsbs.tmp"

// IsTempName reports whether a file name is one of WriteFile's temporaries.
func IsTempName(name string) bool {
	return strings.HasSuffix(strings.ToLower(filepath.Base(name)), TempSuffix)
}

// SweepOrphans removes WriteFile temporaries in dir that are older than
// olderThan, and returns how many it removed.
//
// A crash between CreateTemp and Rename leaves the temp file behind forever;
// nothing ever cleaned those up. The age gate keeps a sweep from deleting a
// temp file another process is actively writing.
func SweepOrphans(dir string, olderThan time.Duration) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-olderThan)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !IsTempName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			removed++
		}
	}
	return removed
}

// WriteFile atomically writes data to path with the given permissions.
//
// perm is applied exactly, without the process umask — the caller states the
// mode it wants for a file it is replacing, and silently loosening or
// tightening it based on ambient umask would be worse than surprising.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	f, err := os.CreateTemp(dir, base+"-*"+TempSuffix)
	if err != nil {
		return err
	}
	tmp := f.Name()
	// os.CreateTemp makes the file 0600; match the caller's requested mode.
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	// Without the fsync, the rename can hit disk before the data blocks,
	// leaving a truncated or empty file after power loss.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Best-effort: make the rename itself durable. Directories cannot be
	// fsynced on Windows, and some filesystems reject it elsewhere too.
	if runtime.GOOS != "windows" {
		if d, err := os.Open(dir); err == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	return nil
}

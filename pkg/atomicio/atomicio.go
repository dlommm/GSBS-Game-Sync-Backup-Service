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
)

// WriteFile atomically writes data to path with the given permissions.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	f, err := os.CreateTemp(dir, base+"-*.tmp")
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

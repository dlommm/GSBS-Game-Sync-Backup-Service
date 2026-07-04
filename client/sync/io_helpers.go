package sync

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

func closeIO(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

// atomicWriteFile writes data to path using a write-fsync-rename pattern so
// that a crash, disk-full event, or power loss never leaves a partially
// written file at path. The temporary file is created in the same directory
// as path so that os.Rename is guaranteed to be atomic (same filesystem).
func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	tmp := path + ".gsbs.tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	// Without the fsync, the rename can hit disk before the data blocks,
	// leaving a truncated or empty save after power loss.
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
		if d, err := os.Open(filepath.Dir(path)); err == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	return nil
}

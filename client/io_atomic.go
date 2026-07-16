package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// atomicWriteFile writes data to path using a write-fsync-rename pattern so a
// crash, disk-full event, or power loss never leaves a partially written file
// at path (mirrors client/sync's helper; config.json corruption previously
// blanked server_url/watch_paths on the next load). The temp file is created
// in the same directory so os.Rename stays on one filesystem (atomic).
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
	if runtime.GOOS != "windows" {
		if d, err := os.Open(filepath.Dir(path)); err == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	return nil
}

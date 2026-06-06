package sync

import (
	"io"
	"io/fs"
	"os"
)

func closeIO(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

// atomicWriteFile writes data to path using a write-then-rename pattern so that
// a crash or disk-full event never leaves a partially-written file at path.
// The temporary file is created in the same directory as path so that os.Rename
// is guaranteed to be atomic (same filesystem).
func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	tmp := path + ".gsbs.tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

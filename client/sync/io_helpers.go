package sync

import (
	"io"
	"io/fs"

	"github.com/gsbs/gsbs/pkg/atomicio"
)

func closeIO(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}

// atomicWriteFile writes data to path using a crash-safe write-fsync-rename
// pattern with a unique temp file (see pkg/atomicio), so a crash, disk-full
// event, or power loss never leaves a partially written file at path, and two
// concurrent savers never race on one temp inode.
func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	return atomicio.WriteFile(path, data, perm)
}

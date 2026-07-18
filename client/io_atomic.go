package main

import (
	"io/fs"

	"github.com/gsbs/gsbs/pkg/atomicio"
)

// atomicWriteFile writes data to path using a crash-safe write-fsync-rename
// pattern with a unique temp file (see pkg/atomicio). config.json corruption
// previously blanked server_url/watch_paths on the next load; a fixed temp name
// also let two concurrent savers tear each other's temp.
func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	return atomicio.WriteFile(path, data, perm)
}

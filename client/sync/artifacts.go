package sync

import (
	"path/filepath"
	"strings"
)

// IsGSBSArtifact reports whether the file is a GSBS-generated artifact
// (*.gsbs.bak pull backups, *.gsbs.tmp atomic-write temps) that must never be
// synced, regardless of user exclude patterns: BackupOnPull writes .gsbs.bak
// next to the real save inside the watched directory, so without this guard
// the watcher and reconcile would upload our own backups as new save slots.
func IsGSBSArtifact(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, ".gsbs.bak") || strings.HasSuffix(base, ".gsbs.tmp")
}

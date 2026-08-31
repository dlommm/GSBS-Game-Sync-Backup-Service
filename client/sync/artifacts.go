package sync

import (
	"path/filepath"
	"strings"

	"github.com/gsbs/gsbs/pkg/atomicio"
)

// IsGSBSArtifact reports whether the file is a GSBS-generated artifact
// (*.gsbs.bak pull backups, *.gsbs.tmp atomic-write temps) that must never be
// synced, regardless of user exclude patterns: BackupOnPull writes .gsbs.bak
// next to the real save inside the watched directory, so without this guard
// the watcher and reconcile would upload our own backups as new save slots.
func IsGSBSArtifact(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	// The temp suffix comes from atomicio itself so the two cannot drift: they
	// already had, and the filter matched no real temp file at all.
	return strings.HasSuffix(base, ".gsbs.bak") || strings.HasSuffix(base, atomicio.TempSuffix)
}

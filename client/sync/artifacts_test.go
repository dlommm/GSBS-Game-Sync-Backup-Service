package sync

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsGSBSArtifact(t *testing.T) {
	assert.True(t, IsGSBSArtifact("save.dat.gsbs.bak"))
	assert.True(t, IsGSBSArtifact("save.dat.gsbs.tmp"))
	assert.True(t, IsGSBSArtifact("SAVE.DAT.GSBS.BAK"), "case-insensitive (Windows/macOS filesystems)")
	assert.True(t, IsGSBSArtifact("nested/dir/slot0.sav.gsbs.bak"))
	assert.True(t, IsGSBSArtifact(filepath.Join("/abs", "path", "save.gsbs.tmp")))

	assert.False(t, IsGSBSArtifact("save.dat"))
	assert.False(t, IsGSBSArtifact("save.bak"), "plain .bak files belong to the game")
	assert.False(t, IsGSBSArtifact("save.tmp"))
	assert.False(t, IsGSBSArtifact("gsbs.bak.sav"), "suffix only — not a substring match")
}

// The artifact exclusion is hardcoded in matchInclude, the single choke point
// for watcher events, overflow rescans, and the reconcile walk — it must hold
// even under SyncAll with no user exclude patterns.
func TestMatchIncludeExcludesGSBSArtifacts(t *testing.T) {
	assert.False(t, matchInclude("save.dat.gsbs.bak", nil, true))
	assert.False(t, matchInclude("save.dat.gsbs.tmp", nil, true))
	assert.False(t, matchInclude("nested/save.dat.gsbs.bak", nil, true))
	assert.False(t, matchInclude("save.dat.gsbs.bak", []string{"*.bak"}, false), "even when a user pattern would match")

	assert.True(t, matchInclude("save.dat", nil, true))
	assert.True(t, matchInclude("save.bak", []string{"*.bak"}, false))
}

package sync

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPullTestClient(t *testing.T) *Client {
	t.Helper()
	ResetPushHashCacheForTest()
	c, err := NewClient("http://127.0.0.1:0", "test-token", paths.NewResolver(), paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)
	return c
}

// ageFile pushes a file's mtime far enough into the past that a "now"-ish
// server timestamp is definitively newer (outside the skew window).
func ageFile(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-24 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func serverNow() string { return time.Now().UTC().Format(time.RFC3339) }

// A downloaded blob that does not match the server-advertised content hash
// must fail before anything touches the filesystem.
func TestApplyOneSave_IntegrityMismatchWritesNothing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(target, []byte("local"), 0644))
	ageFile(t, target)

	c := newPullTestClient(t)
	opts := DefaultPullOptions()
	opts.BackupBeforeOverwrite = true

	_, err := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), b64("server-data"), target, opts, false, FileHash([]byte("something-else")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pull integrity")

	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "local", string(data), "local file must be untouched on integrity failure")
	assert.NoFileExists(t, target+".gsbs.bak", "no backup may be written on integrity failure")
}

// The matching hash is accepted and the file is written.
func TestApplyOneSave_IntegrityMatchApplies(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(target, []byte("local"), 0644))
	ageFile(t, target)

	c := newPullTestClient(t)
	_, err := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), b64("server-data"), target, DefaultPullOptions(), false, FileHash([]byte("server-data")))
	require.NoError(t, err)

	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "server-data", string(data))
}

// A save whose resolved path escapes its watch root must not create
// directories or a .gsbs.bak outside the root: validation runs BEFORE any
// filesystem mutation (regression test for the old MkdirAll/backup-then-
// validate ordering).
func TestApplyOneSave_EscapeGuardRunsBeforeAnyWrite(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	require.NoError(t, os.MkdirAll(root, 0755))
	outside := filepath.Join(tmp, "outside")
	require.NoError(t, os.MkdirAll(outside, 0755))
	target := filepath.Join(outside, "save.dat")
	require.NoError(t, os.WriteFile(target, []byte("local"), 0644))
	ageFile(t, target)

	c := newPullTestClient(t)
	opts := DefaultPullOptions()
	opts.BackupBeforeOverwrite = true
	opts.WatchRoot = func(gameID, pathKey string) string { return root }

	_, err := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), b64("server-data"), target, opts, false, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes watch root")

	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "local", string(data), "escaping write must not modify the target")
	assert.NoFileExists(t, target+".gsbs.bak", "escaping write must not drop a backup outside the root")
}

// When the watch root cannot be resolved, creating a NEW file is refused
// (fail closed) — nothing may appear on disk.
func TestApplyOneSave_NoWatchRootBlocksNewFile(t *testing.T) {
	tmp := t.TempDir()
	// Non-legacy pull context with a matching Steam app makes eligibility
	// ApplyCreateDir for a missing compatdata path — the only case where a
	// brand-new file would be created.
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "steamapps"), 0755))
	target := filepath.Join(tmp, "steamapps", "compatdata", "123", "pfx", "save.dat")

	c := newPullTestClient(t)
	opts := DefaultPullOptions()
	opts.PullContext = paths.PullContext{InstalledSteamApps: []string{"123"}}
	opts.WatchRoot = func(gameID, pathKey string) string { return "" }

	_, err := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), b64("server-data"), target, opts, false, "")
	require.NoError(t, err, "blocked pull is a skip, not an error")
	assert.NoFileExists(t, target)
	assert.NoDirExists(t, filepath.Join(tmp, "steamapps", "compatdata"), "no directories may be created when the root is unresolved")
}

// Overwriting a file that already exists at the resolved path stays allowed
// when the root anchor is unavailable (the resolver itself located the file).
func TestApplyOneSave_NoWatchRootAllowsOverwriteInPlace(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(target, []byte("local"), 0644))
	ageFile(t, target)

	c := newPullTestClient(t)
	opts := DefaultPullOptions()
	opts.WatchRoot = func(gameID, pathKey string) string { return "" }

	applied, applyErr := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), b64("server-data"), target, opts, false, "")
	require.NoError(t, applyErr)
	assert.True(t, applied)
	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "server-data", string(data))
}

// Empty server content must never clobber existing local data.
func TestApplyOneSave_EmptyServerContentSkipped(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(target, []byte("local"), 0644))
	ageFile(t, target)

	c := newPullTestClient(t)
	applied, applyErr := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), "", target, DefaultPullOptions(), false, "")
	require.NoError(t, applyErr)
	assert.False(t, applied, "empty server content is a skip, not an apply")

	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "local", string(data))
}

// Server slots that resolve to our own artifacts (uploaded by pre-5.3
// clients) are never restored.
func TestApplyOneSave_SkipsGSBSArtifactTargets(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "save.dat.gsbs.bak")
	require.NoError(t, os.WriteFile(target, []byte("local-backup"), 0644))
	ageFile(t, target)

	c := newPullTestClient(t)
	applied, applyErr := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), b64("server-data"), target, DefaultPullOptions(), false, "")
	require.NoError(t, applyErr)
	assert.False(t, applied, "artifact targets are never restored")

	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "local-backup", string(data))
}

// A failing backup aborts the overwrite: BackupBeforeOverwrite promises the
// previous local state survives every pull.
func TestApplyOneSave_BackupFailureAbortsOverwrite(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("directory permissions do not block root")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0755))
	target := filepath.Join(sub, "save.dat")
	require.NoError(t, os.WriteFile(target, []byte("local"), 0644))
	ageFile(t, target)
	// Read-only directory: the .gsbs.bak temp file cannot be created.
	require.NoError(t, os.Chmod(sub, 0555))
	t.Cleanup(func() { _ = os.Chmod(sub, 0755) })

	c := newPullTestClient(t)
	opts := DefaultPullOptions()
	opts.BackupBeforeOverwrite = true

	_, err := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), b64("server-data"), target, opts, false, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup before overwrite")

	require.NoError(t, os.Chmod(sub, 0755))
	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "local", string(data), "overwrite must not proceed when the backup failed")
}

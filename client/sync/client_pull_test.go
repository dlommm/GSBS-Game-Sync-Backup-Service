package sync

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
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

func TestApplyOneSave_SymlinkEscapeWritesNothing(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	target := filepath.Join(link, "save.dat")
	require.NoError(t, os.WriteFile(target, []byte("local"), 0644))
	ageFile(t, target)

	c := newPullTestClient(t)
	opts := DefaultPullOptions()
	opts.BackupBeforeOverwrite = true
	opts.WatchRoot = func(gameID, pathKey string) string { return root }
	applied, err := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), b64("server-data"), target, opts, false, "")
	require.ErrorContains(t, err, "escapes watch root")
	assert.False(t, applied)
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "local", string(data))
	assert.NoFileExists(t, target+".gsbs.bak")
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
	if runtime.GOOS == "windows" {
		// The failure is injected with a 0555 directory, which os.Chmod
		// cannot enforce on Windows (ACL-based) — file creation succeeds
		// and no backup failure occurs. The invariant is covered on POSIX.
		t.Skip("read-only directory chmod does not block file creation on Windows")
	}
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

// "Use server version" must write the server copy even when the local file is
// definitively newer. keep_server on its own returns PullConflict there — the
// resolve wrote nothing while ResolveConflict cleared the conflict anyway, so
// the user saw the conflict vanish with the local file untouched.
func TestApplyOneSave_ForceApplyOverridesConflict(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(target, []byte("local-newer"), 0644))

	// Local mtime is now; the server copy is a day old and differs.
	serverTime := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)

	c := newPullTestClient(t)
	opts := DefaultPullOptions()
	opts.ConflictPolicy = "keep_server"

	applied, err := c.applyOneSaveEncrypted("g1", "pk1", serverTime, b64("server-data"), target, opts, false, "")
	require.NoError(t, err)
	require.False(t, applied, "keep_server alone must surface a conflict, not overwrite newer local data")
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "local-newer", string(data))

	opts.ForceApply = true
	opts.BackupBeforeOverwrite = true
	applied, err = c.applyOneSaveEncrypted("g1", "pk1", serverTime, b64("server-data"), target, opts, false, "")
	require.NoError(t, err)
	require.True(t, applied, "ForceApply must write the server copy the user explicitly chose")
	data, err = os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "server-data", string(data))
}

// An existing but unreadable local file must never be treated as absent: doing
// so skipped the conflict check, the skew window, and BackupBeforeOverwrite, so
// a newer local save was overwritten with no backup.
func TestApplyOneSave_UnreadableLocalIsNotTreatedAsAbsent(t *testing.T) {
	dir := t.TempDir()
	// A directory standing where the save file should be is unreadable as a
	// file for every user, so this also covers the root-in-container case where
	// chmod 0000 would be bypassed.
	target := filepath.Join(dir, "save.dat")
	require.NoError(t, os.Mkdir(target, 0755))

	c := newPullTestClient(t)
	opts := DefaultPullOptions()
	opts.BackupBeforeOverwrite = true

	applied, err := c.applyOneSaveEncrypted("g1", "pk1", serverNow(), b64("server-data"), target, opts, false, "")
	require.NoError(t, err)
	assert.False(t, applied, "must not overwrite local data it cannot read or back up")

	fi, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, fi.IsDir(), "the existing path must be left alone")
}

// readLocalForPull must keep the three states apart: a genuinely absent file is
// still "absent" (first pulls must not be blocked), a readable one returns its
// bytes, and only a present-but-unreadable one sets unreadable.
func TestReadLocalForPullDistinguishesAbsentFromUnreadable(t *testing.T) {
	dir := t.TempDir()

	_, exists, _, unreadable := readLocalForPull(filepath.Join(dir, "missing.dat"))
	assert.False(t, exists, "absent file must report absent")
	assert.False(t, unreadable, "absent file is not unreadable")

	readable := filepath.Join(dir, "readable.dat")
	require.NoError(t, os.WriteFile(readable, []byte("hello"), 0644))
	data, exists, mtime, unreadable := readLocalForPull(readable)
	assert.True(t, exists)
	assert.False(t, unreadable)
	assert.Equal(t, "hello", string(data))
	assert.False(t, mtime.IsZero())

	t.Run("permission denied", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			// os.Chmod only toggles the read-only attribute on Windows; it
			// cannot make a file unreadable, so there is nothing to assert.
			// The directory case below covers unreadable on every platform.
			t.Skip("mode 0000 does not deny reads on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("root bypasses file permissions")
		}
		locked := filepath.Join(dir, "locked.dat")
		require.NoError(t, os.WriteFile(locked, []byte("secret"), 0644))
		require.NoError(t, os.Chmod(locked, 0000))
		t.Cleanup(func() { _ = os.Chmod(locked, 0644) })
		_, exists, _, unreadable := readLocalForPull(locked)
		assert.True(t, exists, "an unreadable file still exists")
		assert.True(t, unreadable)
	})

	// A directory in the save's place is unreadable as a file for everyone.
	asDir := filepath.Join(dir, "dir.dat")
	require.NoError(t, os.Mkdir(asDir, 0755))
	_, exists, _, unreadable = readLocalForPull(asDir)
	assert.True(t, exists)
	assert.True(t, unreadable)
}

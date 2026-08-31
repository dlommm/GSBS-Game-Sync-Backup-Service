package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// isolateClientDataDir points ClientDataDir (os.UserConfigDir) at a temp dir so
// the lock tests never touch the developer's real client state.
func isolateClientDataDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("AppData", dir)
}

// The lock file must survive release: unlinking it let a later instance create
// a fresh inode and flock that instead, so two processes could each hold "the"
// lock through different inodes.
func TestSingleInstance_ReleaseKeepsLockFileInode(t *testing.T) {
	isolateClientDataDir(t)

	first, err := tryAcquireSingleInstance()
	require.NoError(t, err)

	_, err = tryAcquireSingleInstance()
	require.ErrorIs(t, err, errAlreadyRunning, "second acquire must be refused while the first holds the lock")

	first()

	second, err := tryAcquireSingleInstance()
	require.NoError(t, err, "lock must be reacquirable after release")
	second()
}

// Releasing twice must not unlock a lock a later instance now holds.
func TestSingleInstance_ReleaseIsIdempotent(t *testing.T) {
	isolateClientDataDir(t)

	release, err := tryAcquireSingleInstance()
	require.NoError(t, err)
	release()
	release()

	again, err := tryAcquireSingleInstance()
	require.NoError(t, err)
	defer again()
}

// A post-update relaunch waits the outgoing instance out instead of quitting.
func TestSingleInstance_WaitAcquiresAfterHolderExits(t *testing.T) {
	isolateClientDataDir(t)

	release, err := tryAcquireSingleInstance()
	require.NoError(t, err)

	go func() {
		time.Sleep(300 * time.Millisecond)
		release()
	}()

	start := time.Now()
	got, err := acquireSingleInstanceWait(5 * time.Second)
	require.NoError(t, err)
	defer got()
	require.GreaterOrEqual(t, time.Since(start), 250*time.Millisecond)
}

// With no wait budget, a duplicate launch is refused immediately.
func TestSingleInstance_WaitZeroFailsFast(t *testing.T) {
	isolateClientDataDir(t)

	release, err := tryAcquireSingleInstance()
	require.NoError(t, err)
	defer release()

	_, err = acquireSingleInstanceWait(0)
	require.True(t, errors.Is(err, errAlreadyRunning))
}

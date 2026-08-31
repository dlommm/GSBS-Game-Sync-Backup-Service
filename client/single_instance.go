//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const clientLockName = "client.lock"

// errAlreadyRunning means another instance holds the single-instance lock. It
// is deliberately distinct from every other failure: "someone else has it" is a
// clean exit, while "we could not tell" must not be silently treated as either
// holding the lock or as a duplicate launch.
var errAlreadyRunning = errors.New("another GSBS client instance is already running")

// tryAcquireSingleInstance takes the lock without waiting.
func tryAcquireSingleInstance() (func(), error) {
	dir := ClientDataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("single instance: create data dir: %w", err)
	}
	path := filepath.Join(dir, clientLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("single instance: open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, errAlreadyRunning
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	var once sync.Once
	return func() {
		once.Do(func() {
			// The lock file is deliberately never unlinked. Removing it lets a
			// later instance create a fresh inode and flock *that*, so two
			// processes could each hold "the" lock through different inodes.
			// An empty leftover file costs nothing.
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
		})
	}, nil
}

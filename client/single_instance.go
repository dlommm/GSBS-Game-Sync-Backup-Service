//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const clientLockName = "client.lock"

// acquireSingleInstance returns a release function, or nil if another instance holds the lock.
func acquireSingleInstance() func() {
	dir := ClientDataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return func() {}
	}
	path := filepath.Join(dir, clientLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return func() {}
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		return nil
	}
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		_ = os.Remove(path)
	}
}

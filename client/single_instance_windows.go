//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
)

const clientLockName = "client.lock"

// errAlreadyRunning means another instance holds the single-instance lock. See
// the POSIX implementation for why it is distinct from other failures.
var errAlreadyRunning = errors.New("another GSBS client instance is already running")

// tryAcquireSingleInstance takes the lock without waiting.
//
// The DECISION mutex is Local\ (per login session): a Global\ mutex blocked a
// second user on the same machine (RDP, fast user switching, family PCs) even
// though all state — lock file, config, data dir — is per-user. A Global\
// mutex is still created purely as a beacon for the installer's AppMutex
// check (its already-exists result is deliberately ignored).
func tryAcquireSingleInstance() (func(), error) {
	dir := ClientDataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("single instance: create data dir: %w", err)
	}
	name, err := syscall.UTF16PtrFromString("Local\\GSBSClientSingleInstance")
	if err != nil {
		return nil, fmt.Errorf("single instance: mutex name: %w", err)
	}
	r, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if r == 0 {
		return nil, fmt.Errorf("single instance: create mutex: %w", callErr)
	}
	if callErr == syscall.ERROR_ALREADY_EXISTS {
		syscall.CloseHandle(syscall.Handle(r))
		return nil, errAlreadyRunning
	}
	beaconName, _ := syscall.UTF16PtrFromString("Global\\GSBSClientSingleInstance")
	beacon, _, _ := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(beaconName)))
	path := filepath.Join(dir, clientLockName)
	_ = os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
	var once sync.Once
	return func() {
		once.Do(func() {
			syscall.CloseHandle(syscall.Handle(r))
			if beacon != 0 {
				syscall.CloseHandle(syscall.Handle(beacon))
			}
			_ = os.Remove(path)
		})
	}, nil
}

//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
)

const clientLockName = "client.lock"

// acquireSingleInstance returns a release function, or nil if another instance holds the lock.
// The DECISION mutex is Local\ (per login session): a Global\ mutex blocked a
// second user on the same machine (RDP, fast user switching, family PCs) even
// though all state — lock file, config, data dir — is per-user. A Global\
// mutex is still created purely as a beacon for the installer's AppMutex
// check (its already-exists result is deliberately ignored).
func acquireSingleInstance() func() {
	dir := ClientDataDir()
	_ = os.MkdirAll(dir, 0755)
	name, _ := syscall.UTF16PtrFromString("Local\\GSBSClientSingleInstance")
	r, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if r == 0 {
		return func() {}
	}
	if err == syscall.ERROR_ALREADY_EXISTS {
		return nil
	}
	beaconName, _ := syscall.UTF16PtrFromString("Global\\GSBSClientSingleInstance")
	beacon, _, _ := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(beaconName)))
	path := filepath.Join(dir, clientLockName)
	_ = os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
	return func() {
		syscall.CloseHandle(syscall.Handle(r))
		if beacon != 0 {
			syscall.CloseHandle(syscall.Handle(beacon))
		}
		_ = os.Remove(path)
	}
}

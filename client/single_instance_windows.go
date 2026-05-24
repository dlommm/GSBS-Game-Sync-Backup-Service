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
func acquireSingleInstance() func() {
	dir := ClientDataDir()
	_ = os.MkdirAll(dir, 0755)
	name, _ := syscall.UTF16PtrFromString("Global\\GSBSClientSingleInstance")
	r, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if r == 0 {
		return func() {}
	}
	if err == syscall.ERROR_ALREADY_EXISTS {
		return nil
	}
	path := filepath.Join(dir, clientLockName)
	_ = os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
	return func() {
		syscall.CloseHandle(syscall.Handle(r))
		_ = os.Remove(path)
	}
}

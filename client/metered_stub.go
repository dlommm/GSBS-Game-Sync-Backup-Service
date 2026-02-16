//go:build !windows

package main

// IsMeteredConnection is only implemented on Windows. On other platforms it always returns false.
func IsMeteredConnection() bool {
	return false
}

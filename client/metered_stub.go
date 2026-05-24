//go:build !windows

package main

// IsMeteredConnection returns false on non-Windows platforms.
func IsMeteredConnection() bool {
	return false
}

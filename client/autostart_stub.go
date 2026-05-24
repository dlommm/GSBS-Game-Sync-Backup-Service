//go:build !windows && !linux

package main

// RunAtStartupEnabled is only implemented on Windows.
func RunAtStartupEnabled() bool {
	return false
}

// SetRunAtStartup is only implemented on Windows.
func SetRunAtStartup(enabled bool) error {
	return nil
}

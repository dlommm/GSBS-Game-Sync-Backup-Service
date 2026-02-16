//go:build !windows

package main

// DetectLauncherPaths is only implemented on Windows.
func DetectLauncherPaths() DetectedLauncherPaths {
	return DetectedLauncherPaths{}
}

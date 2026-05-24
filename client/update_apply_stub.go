//go:build !windows && !linux

package main

import "fmt"

func applyUpdatePlatform(stagedPath string) error {
	return fmt.Errorf("client auto-update is not supported on this platform")
}

func applyStagedBinary(stagedPath string) error {
	return fmt.Errorf("client auto-update is not supported on this platform")
}

//go:build !windows

package main

import "fmt"

func runWindowsServiceHost() error {
	return fmt.Errorf("--service is only supported on Windows")
}

func manageWindowsService(opts cliOptions) error {
	_ = opts
	return fmt.Errorf("service control flags are only supported on Windows")
}

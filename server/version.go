package main

import "fmt"

// Version, BuildDate, and Commit are set at build time via ldflags for releases.
// Example: go build -ldflags "-X main.Version=1.0.3 -X main.BuildDate=... -X main.Commit=..."
var (
	Version   = "dev"
	BuildDate = ""
	Commit    = ""
)

func printVersion() {
	fmt.Println("gsbs-server", Version)
	if BuildDate != "" {
		fmt.Println("  build:", BuildDate)
	}
	if Commit != "" {
		fmt.Println("  commit:", Commit)
	}
}

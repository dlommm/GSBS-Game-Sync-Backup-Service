package paths

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PullEligibility indicates whether a pulled save should be applied.
type PullEligibility int

const (
	SkipNotInstalled PullEligibility = iota
	SkipNoAnchor
	ApplyReady
	ApplyCreateDir
)

// PullContext holds install-aware pull decisions.
type PullContext struct {
	LegacyMode         bool
	InstalledGameIDs   map[string]bool
	InstalledSteamApps []string
}

// EvaluatePullEligibility decides whether to apply a pulled save at absPath.
func EvaluatePullEligibility(absPath string, gameID string, ctx PullContext) PullEligibility {
	if absPath == "" {
		return SkipNoAnchor
	}
	if !ctx.LegacyMode && len(ctx.InstalledGameIDs) > 0 && !ctx.InstalledGameIDs[gameID] {
		return SkipNotInstalled
	}
	dir := absPath
	if info, err := os.Stat(absPath); err == nil && !info.IsDir() {
		dir = filepath.Dir(absPath)
	}
	if _, err := os.Stat(dir); err == nil {
		return ApplyReady
	}
	if ctx.LegacyMode {
		return SkipNoAnchor
	}
	if hasInstallAnchor(absPath, ctx.InstalledSteamApps) {
		return ApplyCreateDir
	}
	return SkipNoAnchor
}

var compatdataRe = regexp.MustCompile(`compatdata[/\\](\d+)[/\\]`)

func hasInstallAnchor(absPath string, steamApps []string) bool {
	if m := compatdataRe.FindStringSubmatch(absPath); len(m) >= 2 {
		appID := m[1]
		for _, id := range steamApps {
			if id == appID {
				pfx := strings.Split(absPath, "compatdata"+string(filepath.Separator)+appID)[0]
				if pfx == "" {
					pfx = strings.Split(absPath, "compatdata/"+appID)[0]
				}
				anchor := filepath.Join(pfx, "compatdata", appID, "pfx")
				if _, err := os.Stat(anchor); err == nil {
					return true
				}
			}
		}
	}
	// Parent chain exists up to a known anchor (e.g. save root under home)
	parent := filepath.Dir(absPath)
	for i := 0; i < 5; i++ {
		if parent == "" || parent == "." || parent == string(filepath.Separator) {
			break
		}
		if _, err := os.Stat(parent); err == nil {
			return true
		}
		parent = filepath.Dir(parent)
	}
	return false
}

// WatchDirExists returns true if the directory to watch for a path exists.
func WatchDirExists(absPath string) bool {
	if absPath == "" {
		return false
	}
	dir := absPath
	if info, err := os.Stat(absPath); err == nil && !info.IsDir() {
		dir = filepath.Dir(absPath)
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

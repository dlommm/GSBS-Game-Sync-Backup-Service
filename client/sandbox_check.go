package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"sync"

	"github.com/gen2brain/beeep"
)

var sandboxWarnOnce sync.Once

// blockedDirsState holds the latest permission-blocked watch dirs so the
// local dashboard can list them with exact fix instructions (a single vague
// toast was the only signal before 5.4).
var blockedDirsState struct {
	mu   sync.Mutex
	dirs []string
}

// BlockedWatchDirs returns the last detected permission-blocked directories.
func BlockedWatchDirs() []string {
	blockedDirsState.mu.Lock()
	defer blockedDirsState.mu.Unlock()
	return append([]string(nil), blockedDirsState.dirs...)
}

// warnInaccessibleWatchPaths checks resolved watch directories for permission
// problems — most often a Flatpak sandbox that hasn't been granted a particular
// save folder — records them for the dashboard, and surfaces a single tray
// notification instead of silently failing to watch them. A directory that
// simply doesn't exist (game not installed, or no saves written yet) is
// normal and ignored.
//
// Safe to call from a goroutine: it only reads the snapshot slice passed in.
func warnInaccessibleWatchPaths(paths []watchPath) {
	var blocked []string
	seen := make(map[string]bool)
	for _, wp := range paths {
		dir := wp.Directory
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		if _, err := os.Stat(dir); err != nil && errors.Is(err, fs.ErrPermission) {
			blocked = append(blocked, dir)
		}
	}
	blockedDirsState.mu.Lock()
	blockedDirsState.dirs = blocked
	blockedDirsState.mu.Unlock()
	if len(blocked) == 0 {
		return
	}
	for _, d := range blocked {
		log.Printf("sync: watch path not accessible (permission denied): %s", d)
	}
	sandboxWarnOnce.Do(func() {
		msg := fmt.Sprintf("%d game save folder(s) couldn't be accessed.", len(blocked))
		if isFlatpak() {
			msg += " Grant access with Flatseal, then restart GSBS."
		} else {
			msg += " Check folder permissions — see gsbs.log."
		}
		_ = beeep.Notify("GSBS — limited access", msg, "")
	})
}

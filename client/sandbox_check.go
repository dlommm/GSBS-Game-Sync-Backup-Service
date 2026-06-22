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

// warnInaccessibleWatchPaths checks resolved watch directories for permission
// problems — most often a Flatpak sandbox that hasn't been granted a particular
// save folder — and surfaces a single tray notification instead of silently
// failing to watch them. A directory that simply doesn't exist (game not
// installed, or no saves written yet) is normal and ignored.
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

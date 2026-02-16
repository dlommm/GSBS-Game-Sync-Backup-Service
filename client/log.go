package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// MaxLogBytes is the size at which the client log is rotated (keep one .old file).
const MaxLogBytes = 5 * 1024 * 1024 // 5 MiB

// ClientDataDir returns the directory where the client stores config, manifest, and log.
// Windows: %APPDATA%\gsbs (e.g. C:\Users\<user>\AppData\Roaming\gsbs)
// Linux: ~/.config/gsbs
func ClientDataDir() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs")
}

// ClientLogPath returns the path to the client log file (same directory as config).
func ClientLogPath() string {
	return filepath.Join(ClientDataDir(), "gsbs.log")
}

// InitClientLog initializes the default log to write to the client log file.
// If the log file is larger than MaxLogBytes, it is rotated to gsbs.log.old and a new file is started.
// If alsoStderr is true, log output is written to both the file and os.Stderr
// (e.g. when running in a console). When false (e.g. Windows tray), only the file is used.
func InitClientLog(alsoStderr bool) {
	dir := ClientDataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.SetOutput(os.Stderr)
		log.Printf("client log: could not create dir %s: %v", dir, err)
		return
	}
	path := ClientLogPath()
	// Rotate if existing log is too large.
	if info, err := os.Stat(path); err == nil && info.Size() >= MaxLogBytes {
		oldPath := path + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(path, oldPath); err != nil {
			_ = os.Remove(path) // truncate by removing; new file will be created below
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.SetOutput(os.Stderr)
		log.Printf("client log: could not open %s: %v", path, err)
		return
	}
	var w io.Writer = f
	if alsoStderr {
		w = io.MultiWriter(f, os.Stderr)
	}
	log.SetOutput(w)
	log.SetFlags(log.Ldate | log.Ltime)
	log.Printf("client log: writing to %s", path)
}

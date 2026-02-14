package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type config struct {
	ServerURL            string        `json:"server_url"`
	Token                string        `json:"token"`
	SyncInterval         time.Duration `json:"sync_interval"`
	UbisoftConnectFolder string        `json:"ubisoft_connect_folder,omitempty"` // e.g. C:\Program Files (x86)\Ubisoft\Ubisoft Game Launcher
	LauncherUserID       string        `json:"launcher_user_id,omitempty"`         // launcher user ID for paths like savegames\<user-id>\895
	WatchPaths           []watchPath   `json:"watch_paths"`
}

type watchPath struct {
	GameID   string   `json:"game_id"`
	PathKey  string   `json:"path_key"`
	PathTemplates []string `json:"path_templates"` // OS-specific templates; client resolves for current OS
}

func loadConfig() (*config, error) {
	dir, _ := os.UserConfigDir()
	path := filepath.Join(dir, "gsbs", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// First launch: blank config so user is prompted to login (server + credentials).
			return blankConfig(), nil
		}
		return nil, err
	}
	var c config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.ServerURL == "" {
		c.ServerURL = "http://localhost:8080"
	}
	return &c, nil
}

// blankConfig is used when no config file exists (first launch). Empty server and token so user must login.
func blankConfig() *config {
	return &config{
		ServerURL:    "",
		Token:        "",
		SyncInterval: 5 * time.Minute,
		WatchPaths:   []watchPath{},
	}
}

func defaultConfig(_ string) *config {
	return &config{
		ServerURL:    "http://localhost:8080",
		SyncInterval: 5 * time.Minute,
		WatchPaths:   []watchPath{},
	}
}

func saveConfig(c *config) error {
	dir, _ := os.UserConfigDir()
	gsbsDir := filepath.Join(dir, "gsbs")
	if err := os.MkdirAll(gsbsDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(gsbsDir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// mergeWatchPaths merges manifest-derived paths with config watch_paths. Config entries are added first (user override), then manifest entries not already present.
func mergeWatchPaths(manifestPaths, configPaths []watchPath) []watchPath {
	seen := make(map[string]bool)
	var out []watchPath
	for _, wp := range configPaths {
		key := wp.GameID + "\x00" + wp.PathKey
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, wp)
	}
	for _, wp := range manifestPaths {
		key := wp.GameID + "\x00" + wp.PathKey
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, wp)
	}
	return out
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Duration wraps time.Duration with human-friendly JSON marshaling.
// Serializes as a string like "5m", "30s", "1h". Accepts strings ("5m") or
// numbers (nanoseconds, for backward compatibility) when unmarshaling.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch val := v.(type) {
	case string:
		dur, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", val, err)
		}
		*d = Duration(dur)
	case float64:
		// Backward compat: accept raw nanoseconds (old configs)
		*d = Duration(time.Duration(int64(val)))
	default:
		return fmt.Errorf("invalid duration type %T", v)
	}
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// String returns a concise human-readable duration like "5m0s".
func (d Duration) String() string {
	dur := time.Duration(d)
	if dur == 0 {
		return "0s"
	}
	s := dur.String()
	// Trim unnecessary trailing "0s" for cleaner display (e.g. "5m" instead of "5m0s")
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	return s
}

// parseDurationFlex parses a human-friendly duration string like "5m", "30s", "1h",
// or a plain integer (treated as seconds for user convenience).
func parseDurationFlex(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	// If it's a bare integer, treat as seconds
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return time.ParseDuration(s)
}

type config struct {
	ServerURL            string    `json:"server_url"`
	Token                string    `json:"token"`
	ClientName           string    `json:"client_name,omitempty"` // name shown on server for this machine
	SyncInterval         Duration  `json:"sync_interval"`
	UbisoftConnectFolder string    `json:"ubisoft_connect_folder,omitempty"` // e.g. C:\Program Files (x86)\Ubisoft\Ubisoft Game Launcher
	LauncherUserID       string    `json:"launcher_user_id,omitempty"`       // launcher user ID for paths like savegames\<user-id>\895
	WatchPaths           []watchPath `json:"watch_paths"`
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
		SyncInterval: Duration(5 * time.Minute),
		WatchPaths:   []watchPath{},
	}
}

func defaultConfig(_ string) *config {
	return &config{
		ServerURL:    "http://localhost:8080",
		SyncInterval: Duration(5 * time.Minute),
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

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// cfgMu serializes saveConfig (its keyring writes + atomic file replace) so
// concurrent savers can't interleave, and guards the handful of config fields
// read or written from more than one goroutine at runtime — the device token
// during monthly rotation. Most config access is single-threaded setup/CLI
// code that does not touch it.
var cfgMu sync.RWMutex

// setTokenAndRefresh publishes a rotated device token to the shared config so
// cross-goroutine readers (authSnapshot) see it without a data race.
func (c *config) setTokenAndRefresh(token, refreshedAt string) {
	cfgMu.Lock()
	c.Token = token
	c.TokenRefreshedAt = refreshedAt
	cfgMu.Unlock()
}

// authSnapshot returns the server URL and device token under the read lock,
// safe to call from a goroutine that may race token rotation.
func (c *config) authSnapshot() (serverURL, token string) {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return c.ServerURL, c.Token
}

// defaultSyncInterval is the periodic pull interval when none is configured.
// File changes are pushed immediately regardless, so this only bounds how
// stale a pull-only client can get.
const defaultSyncInterval = 6 * time.Hour

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

type config struct {
	ServerURL                   string            `json:"server_url"`
	Token                       string            `json:"token"`
	TokenRefreshedAt            string            `json:"token_refreshed_at,omitempty"` // last proactive /api/token/refresh rotation (RFC3339)
	ClientName                  string            `json:"client_name,omitempty"`        // name shown on server for this machine
	SyncInterval                Duration          `json:"sync_interval"`
	UbisoftConnectFolder        string            `json:"ubisoft_connect_folder,omitempty"`          // e.g. C:\Program Files (x86)\Ubisoft\Ubisoft Game Launcher
	GOGGalaxyFolder             string            `json:"gog_galaxy_folder,omitempty"`               // e.g. C:\Program Files (x86)\GOG Galaxy
	EpicGamesFolder             string            `json:"epic_games_folder,omitempty"`               // e.g. C:\Program Files\Epic Games
	XboxAppFolder               string            `json:"xbox_app_folder,omitempty"`                 // e.g. C:\XboxGames
	LauncherUserID              string            `json:"launcher_user_id,omitempty"`                // launcher user ID for paths like savegames\<user-id>\895
	BackupOnPull                bool              `json:"backup_on_pull,omitempty"`                  // if true, copy existing file to .gsbs.bak before overwriting on pull
	SkipOverwriteWhenLocalNewer bool              `json:"skip_overwrite_when_local_newer,omitempty"` // if true, on pull do not overwrite when local file is newer than server
	ManifestInclude             string            `json:"manifest_include,omitempty"`                // "saves", "config", or "both" (default) — which manifest entries to fetch
	MaxSyncKbps                 int               `json:"max_sync_kbps,omitempty"`                   // optional max sync bandwidth in KiB/s; 0 = no limit
	SyncPaused                  bool              `json:"sync_paused,omitempty"`                     // if true, do not run periodic pull or watcher push until resumed
	SkipSyncWhenMetered         bool              `json:"skip_sync_when_metered,omitempty"`          // Windows: skip pull/push when connection is metered
	WatchExclude                []string          `json:"watch_exclude,omitempty"`                   // glob patterns for files to ignore when watching (e.g. "*.tmp", "*.bak")
	UseCompression              bool              `json:"use_compression,omitempty"`                 // use gzip for push/pull request and response bodies
	VerboseLog                  bool              `json:"verbose_log,omitempty"`                     // when true, log extra detail (per-file sync, resolved paths)
	HeroicFolder                string            `json:"heroic_folder,omitempty"`
	LutrisFolder                string            `json:"lutris_folder,omitempty"`
	EAAppFolder                 string            `json:"ea_app_folder,omitempty"`
	BottlesFolder               string            `json:"bottles_folder,omitempty"`
	PrismFolder                 string            `json:"prism_folder,omitempty"`
	FlatpakSteamFolder          string            `json:"flatpak_steam_folder,omitempty"`
	SteamLibraryFolders         []string          `json:"steam_library_folders,omitempty"`     // extra Steam library roots (e.g. D:\SteamLibrary)
	GameInstallPaths            map[string]string `json:"game_install_paths,omitempty"`        // manifest game_id -> absolute install folder override
	DiscoveryInterval           Duration          `json:"discovery_interval,omitempty"`        // default 4h; re-scan installed games
	AutoWatchMode               string            `json:"auto_watch_mode,omitempty"`           // "legacy" (default) or "discovered"
	ConflictPolicy              string            `json:"conflict_policy,omitempty"`           // last_write_wins, keep_local, keep_server
	EncryptionPassphrase        string            `json:"encryption_passphrase,omitempty"`     // local E2E key; never sent to server
	CryptoV2                    *bool             `json:"crypto_v2,omitempty"`                 // nil=auto (server fleet signal), true=force Argon2id format, false=pin legacy
	GameAwareSync               *bool             `json:"game_aware_sync,omitempty"`           // default true: defer sync for a game while it is running
	GameScanInterval            Duration          `json:"game_scan_interval,omitempty"`        // process-scan interval for game detection (default 15s)
	ConflictPolicyOverrides     map[string]string `json:"conflict_policy_overrides,omitempty"` // game_id -> policy (last_write_wins, keep_local, keep_server)
	UpdateCheckEnabled          *bool             `json:"update_check_enabled,omitempty"`      // default true; set false to disable client update checks
	UpdateRepo                  string            `json:"update_repo,omitempty"`               // GitHub owner/repo override for release checks
	NotificationLevel           string            `json:"notification_level,omitempty"`        // "all" (default), "errors" (errors+conflicts only), "silent"
	NotifyPerUpload             *bool             `json:"notify_per_upload,omitempty"`         // default true: toast per game upload (debounced)
	QuietHoursStart             string            `json:"quiet_hours_start,omitempty"`         // "22:30" local; with quiet_hours_end, sync defers in the window
	QuietHoursEnd               string            `json:"quiet_hours_end,omitempty"`
	WatchPaths                  []watchPath       `json:"watch_paths"`
}

// notifyPerUploadEnabled returns the per-upload toast setting (default true —
// the pre-5.4 behavior).
func (c *config) notifyPerUploadEnabled() bool {
	return c == nil || c.NotifyPerUpload == nil || *c.NotifyPerUpload
}

// effectiveNotificationLevel normalizes the notification level.
func (c *config) effectiveNotificationLevel() string {
	if c == nil {
		return "all"
	}
	switch c.NotificationLevel {
	case "errors", "silent":
		return c.NotificationLevel
	default:
		return "all"
	}
}

type watchPath struct {
	GameID          string   `json:"game_id"`
	PathKey         string   `json:"path_key"`
	PathTemplates   []string `json:"path_templates,omitempty"` // legacy; client resolves for current OS
	Directory       string   `json:"directory,omitempty"`
	IncludePatterns []string `json:"include_patterns,omitempty"`
	Recursive       bool     `json:"recursive,omitempty"`
	SyncAll         bool     `json:"sync_all,omitempty"`
	RuleKey         string   `json:"rule_key,omitempty"`
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
	c.Token = strings.TrimSpace(c.Token)
	reconcileSecrets(&c)
	return &c, nil
}

// reconcileSecrets moves any on-disk secrets into the OS keyring (one-time
// migration for upgrades) and loads keyring-stored secrets into c. When the
// keyring is unavailable, secrets simply stay in the config file (0600) — a
// graceful fallback for headless or sandboxed setups.
func reconcileSecrets(c *config) {
	migrated := false
	if c.Token != "" {
		if err := secretSet(secretToken, c.Token); err == nil {
			migrated = true
		}
	} else if v, ok := secretGet(secretToken); ok {
		c.Token = v
	}
	if c.EncryptionPassphrase != "" {
		if err := secretSet(secretPassphrase, c.EncryptionPassphrase); err == nil {
			migrated = true
		}
	} else if v, ok := secretGet(secretPassphrase); ok {
		c.EncryptionPassphrase = v
	}
	// If secrets were just migrated into the keyring, rewrite the file without
	// them. saveConfig re-stores to the keyring (idempotent) and strips them.
	if migrated {
		_ = saveConfig(c)
	}
}

// blankConfig is used when no config file exists (first launch). Empty server and token so user must login.
func blankConfig() *config {
	return &config{
		ServerURL:      "",
		Token:          "",
		SyncInterval:   Duration(defaultSyncInterval),
		AutoWatchMode:  "discovered",
		ConflictPolicy: "last_write_wins",
		BackupOnPull:   true,
		WatchPaths:     []watchPath{},
	}
}

func defaultConfig(_ string) *config {
	return &config{
		ServerURL:    "http://localhost:8080",
		SyncInterval: Duration(defaultSyncInterval),
		WatchPaths:   []watchPath{},
	}
}

func saveConfig(c *config) error {
	// Serialize the whole keyring-write + atomic-file-replace so two concurrent
	// savers (e.g. token rotation and a settings save) can't interleave their
	// keyring set/delete calls or clobber each other's file with a stale copy.
	cfgMu.Lock()
	defer cfgMu.Unlock()
	dir, _ := os.UserConfigDir()
	gsbsDir := filepath.Join(dir, "gsbs")
	if err := os.MkdirAll(gsbsDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(gsbsDir, "config.json")

	// Persist secrets to the OS keyring when available and keep them out of the
	// on-disk JSON. If the keyring is unavailable, secretSet returns an error
	// and the secret stays in the file (still 0600). A shallow copy keeps the
	// caller's in-memory config (with the live token) intact.
	toWrite := *c
	if c.Token != "" {
		if err := secretSet(secretToken, c.Token); err == nil {
			toWrite.Token = ""
		}
	} else {
		secretDelete(secretToken)
	}
	if c.EncryptionPassphrase != "" {
		if err := secretSet(secretPassphrase, c.EncryptionPassphrase); err == nil {
			toWrite.EncryptionPassphrase = ""
		}
	} else {
		secretDelete(secretPassphrase)
	}

	data, err := json.MarshalIndent(&toWrite, "", "  ")
	if err != nil {
		return err
	}
	// Atomic: a crash mid-write must never corrupt config.json — a corrupt
	// file makes loadConfig fail and callers fall back to a blank config,
	// losing server_url/watch_paths (and a later save persists the blanks).
	return atomicWriteFile(path, data, 0600)
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
		// Drop any previously-saved watch path that would sync a home/system root
		// (e.g. added before the safety guard existed) so it can never sync the
		// whole home directory. Specific named-file rules in such roots are kept.
		if filepath.IsAbs(wp.Directory) && unsafeWatchTargetAbs(wp.Directory, wp.SyncAll, wp.Recursive, wp.IncludePatterns) {
			seen[key] = true
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

// effectiveAutoWatchMode returns legacy for existing configs without the field set.
func (c *config) effectiveAutoWatchMode() string {
	if c.AutoWatchMode == "discovered" {
		return "discovered"
	}
	return "legacy"
}

// effectiveConflictPolicy maps deprecated skip_overwrite_when_local_newer to keep_local.
func (c *config) effectiveConflictPolicy() string {
	if c.SkipOverwriteWhenLocalNewer {
		return "keep_local"
	}
	switch c.ConflictPolicy {
	case "keep_local", "keep_server":
		return c.ConflictPolicy
	default:
		return "last_write_wins"
	}
}

// effectiveConflictPolicyFor returns the conflict policy for one game: a
// per-game override from conflict_policy_overrides when valid, otherwise the
// global policy.
func (c *config) effectiveConflictPolicyFor(gameID string) string {
	if p, ok := c.ConflictPolicyOverrides[gameID]; ok {
		switch p {
		case "keep_local", "keep_server", "last_write_wins":
			return p
		}
	}
	return c.effectiveConflictPolicy()
}

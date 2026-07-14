package main

import (
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	clientwebui "github.com/gsbs/gsbs/client/webui"
)

const minSyncInterval = 30 * time.Second

// manifestIncludeOrDefault returns the configured manifest_include or "both".
func manifestIncludeOrDefault(cfg *config) string {
	switch cfg.ManifestInclude {
	case "saves", "config", "both":
		return cfg.ManifestInclude
	default:
		return "both"
	}
}

// settingsPageData builds the Settings form state from the current config.
// Secrets (token, encryption passphrase) are never exposed here.
func settingsPageData(cfg *config) clientwebui.SettingsPageData {
	overrides := make([]clientwebui.PolicyOverride, 0, len(cfg.ConflictPolicyOverrides))
	for gameID, policy := range cfg.ConflictPolicyOverrides {
		overrides = append(overrides, clientwebui.PolicyOverride{
			GameID: gameID, Title: gameTitleFor(gameID), Policy: policy,
		})
	}
	sort.Slice(overrides, func(i, j int) bool {
		an, bn := overrides[i].Title, overrides[j].Title
		if an == "" {
			an = overrides[i].GameID
		}
		if bn == "" {
			bn = overrides[j].GameID
		}
		return strings.ToLower(an) < strings.ToLower(bn)
	})
	return clientwebui.SettingsPageData{
		PageData: clientwebui.PageData{
			NavActive: "settings",
			Title:     "Settings",
			Version:   Version,
		},
		SyncInterval:        cfg.SyncInterval.String(),
		ConflictPolicy:      cfg.effectiveConflictPolicy(),
		ManifestInclude:     manifestIncludeOrDefault(cfg),
		MaxSyncKbps:         cfg.MaxSyncKbps,
		BackupOnPull:        cfg.BackupOnPull,
		UseCompression:      cfg.UseCompression,
		SkipSyncWhenMetered: cfg.SkipSyncWhenMetered,
		MeteredSupported:    runtime.GOOS == "windows",
		PolicyOverrides:     overrides,
	}
}

// validConflictPolicy reports whether p is a supported conflict policy.
func validConflictPolicy(p string) bool {
	return p == "last_write_wins" || p == "keep_local" || p == "keep_server"
}

// applyPolicyOverrideForm mutates cfg.ConflictPolicyOverrides from the
// Settings form (v5.2): existing rows post `override_policy::<gameID>`
// (value "remove" deletes the override); a new row posts
// override_add_game + override_add_policy.
func applyPolicyOverrideForm(cfg *config, form map[string][]string) {
	const prefix = "override_policy::"
	for key, vals := range form {
		if !strings.HasPrefix(key, prefix) || len(vals) == 0 {
			continue
		}
		gameID := strings.TrimSpace(strings.TrimPrefix(key, prefix))
		if gameID == "" {
			continue
		}
		switch v := vals[0]; {
		case v == "remove":
			delete(cfg.ConflictPolicyOverrides, gameID)
		case validConflictPolicy(v):
			if cfg.ConflictPolicyOverrides == nil {
				cfg.ConflictPolicyOverrides = map[string]string{}
			}
			cfg.ConflictPolicyOverrides[gameID] = v
		}
	}
	addGame := ""
	if vals := form["override_add_game"]; len(vals) > 0 {
		addGame = strings.TrimSpace(vals[0])
	}
	addPolicy := ""
	if vals := form["override_add_policy"]; len(vals) > 0 {
		addPolicy = vals[0]
	}
	if addGame != "" && validConflictPolicy(addPolicy) {
		if cfg.ConflictPolicyOverrides == nil {
			cfg.ConflictPolicyOverrides = map[string]string{}
		}
		cfg.ConflictPolicyOverrides[addGame] = addPolicy
	}
}

func handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	clientwebui.RenderSettingsPage(w, settingsPageData(cfg))
}

func handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	cfg, err := loadConfig()
	if err != nil || cfg == nil {
		cfg = blankConfig()
	}
	if err := r.ParseForm(); err != nil {
		data := settingsPageData(cfg)
		data.Error = "Invalid form submission."
		clientwebui.RenderSettingsPage(w, data)
		return
	}

	// Sync interval (duration string, min 30s).
	if v := strings.TrimSpace(r.Form.Get("sync_interval")); v != "" {
		d, perr := time.ParseDuration(v)
		if perr != nil || d < minSyncInterval {
			data := settingsPageData(cfg)
			data.Error = "Sync interval must be a duration of at least 30s (e.g. 30s, 5m, 1h)."
			clientwebui.RenderSettingsPage(w, data)
			return
		}
		cfg.SyncInterval = Duration(d)
	}

	// Conflict policy (explicit choice clears the deprecated skip flag).
	switch r.Form.Get("conflict_policy") {
	case "last_write_wins", "keep_local", "keep_server":
		cfg.ConflictPolicy = r.Form.Get("conflict_policy")
		cfg.SkipOverwriteWhenLocalNewer = false
	}

	// Manifest include.
	switch r.Form.Get("manifest_include") {
	case "saves", "config", "both":
		cfg.ManifestInclude = r.Form.Get("manifest_include")
	}

	// Max bandwidth (KiB/s; empty or 0 = unlimited).
	if v := strings.TrimSpace(r.Form.Get("max_sync_kbps")); v != "" {
		n, cerr := strconv.Atoi(v)
		if cerr != nil || n < 0 {
			data := settingsPageData(cfg)
			data.Error = "Max bandwidth must be a non-negative whole number (KiB/s), or blank for unlimited."
			clientwebui.RenderSettingsPage(w, data)
			return
		}
		cfg.MaxSyncKbps = n
	} else {
		cfg.MaxSyncKbps = 0
	}

	// Toggles (unchecked checkboxes are absent from the form).
	cfg.BackupOnPull = r.Form.Get("backup_on_pull") != ""
	cfg.UseCompression = r.Form.Get("use_compression") != ""
	// The metered checkbox renders disabled on non-Windows (no detection
	// there), and disabled inputs never submit — leave the stored value alone
	// so it survives if the config ever moves to a Windows machine.
	if runtime.GOOS == "windows" {
		cfg.SkipSyncWhenMetered = r.Form.Get("skip_sync_when_metered") != ""
	}

	// Per-game conflict-policy overrides (v5.2).
	applyPolicyOverrideForm(cfg, r.Form)

	if err := saveConfig(cfg); err != nil {
		data := settingsPageData(cfg)
		data.Error = "Could not save settings: " + err.Error()
		clientwebui.RenderSettingsPage(w, data)
		return
	}

	// Apply immediately by restarting sync with the new config.
	restartSync(cfg)

	data := settingsPageData(cfg)
	data.Success = "Settings saved. Sync restarted with the new configuration."
	clientwebui.RenderSettingsPage(w, data)
}

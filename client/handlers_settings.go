package main

import (
	"context"
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
	data := clientwebui.SettingsPageData{
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
		NotificationLevel:   cfg.effectiveNotificationLevel(),
		NotifyPerUpload:     cfg.notifyPerUploadEnabled(),
		QuietHoursStart:     cfg.QuietHoursStart,
		QuietHoursEnd:       cfg.QuietHoursEnd,
		PolicyOverrides:     overrides,
		PassphraseSet:       cfg.EncryptionPassphrase != "",
	}
	if c := getSyncClient(); c != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if enc, err := c.FetchAccountSettings(ctx); err == nil {
			data.EncryptionKnown = true
			data.EncryptionAccountEnabled = enc
		}
		cancel()
	}
	return data
}

// handleEncryptionEnable is the guided E2E-encryption onboarding: store the
// passphrase (keyring via saveConfig/secretSet), enable encryption on the
// account (enable-only API), restart sync. The passphrase value is never
// rendered back, logged, or echoed on validation errors.
func handleEncryptionEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	cfg, err := loadConfig()
	if err != nil || cfg == nil {
		http.Error(w, "config unavailable", http.StatusInternalServerError)
		return
	}
	renderErr := func(msg string) {
		data := settingsPageData(cfg)
		data.Error = msg
		clientwebui.RenderSettingsPage(w, data)
	}
	pass := r.Form.Get("passphrase")
	confirm := r.Form.Get("passphrase_confirm")
	if len(pass) < 8 {
		renderErr("Passphrase must be at least 8 characters.")
		return
	}
	if pass != confirm {
		renderErr("Passphrases do not match.")
		return
	}
	c := getSyncClient()
	if c == nil {
		renderErr("Not connected — log in and let sync start before enabling encryption.")
		return
	}
	cfg.EncryptionPassphrase = pass
	if err := saveConfig(cfg); err != nil {
		renderErr("Could not store the passphrase: " + err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := c.EnableEncryption(ctx); err != nil {
		renderErr("The server rejected enabling encryption: " + err.Error())
		return
	}
	saveAccountSettingsCache(cfg.ServerURL, true)
	restartSync(cfg)
	data := settingsPageData(cfg)
	data.Success = "End-to-end encryption enabled. Set the SAME passphrase on your other devices; existing saves convert as they next change and sync."
	clientwebui.RenderSettingsPage(w, data)
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

	// Notifications (v5.4).
	switch lvl := r.Form.Get("notification_level"); lvl {
	case "all", "errors", "silent":
		cfg.NotificationLevel = lvl
	}
	perUpload := r.Form.Get("notify_per_upload") != ""
	cfg.NotifyPerUpload = &perUpload

	// Quiet hours (v5.4): both bounds must parse or the pair clears.
	qs, qe := strings.TrimSpace(r.Form.Get("quiet_hours_start")), strings.TrimSpace(r.Form.Get("quiet_hours_end"))
	if qs == "" && qe == "" {
		cfg.QuietHoursStart, cfg.QuietHoursEnd = "", ""
	} else {
		_, ok1 := parseClock(qs)
		_, ok2 := parseClock(qe)
		if !ok1 || !ok2 {
			data := settingsPageData(cfg)
			data.Error = "Quiet hours must be HH:MM times (e.g. 22:30 to 07:00), or both empty."
			clientwebui.RenderSettingsPage(w, data)
			return
		}
		cfg.QuietHoursStart, cfg.QuietHoursEnd = qs, qe
	}

	// Per-game conflict-policy overrides (v5.2).
	applyPolicyOverrideForm(cfg, r.Form)

	// Apply notification prefs immediately (restartSync also does, but the
	// toast gate should not wait for the sync loop swap).
	SetNotifyPrefs(cfg.effectiveNotificationLevel(), cfg.notifyPerUploadEnabled())

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

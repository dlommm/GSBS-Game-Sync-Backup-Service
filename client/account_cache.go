package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	clientwebui "github.com/gsbs/gsbs/client/webui"
)

// accountSettingsCache is the last account state successfully fetched from
// the server. It exists so a transient /api/account failure at startup cannot
// silently flip an encryption-enabled account to plaintext uploads for the
// whole session: when the server is unreachable we trust the last known
// answer instead of assuming "disabled".
type accountSettingsCache struct {
	ServerURL         string `json:"server_url"`
	EncryptionEnabled bool   `json:"encryption_enabled"`
	// Appearance prefs synced from the account (v5.6) so the local UI keeps
	// the user's look across offline restarts.
	Design    string `json:"design,omitempty"`
	Layout    string `json:"layout,omitempty"`
	FetchedAt string `json:"fetched_at"`
}

// readAccountSettingsCache returns the raw cache file contents when readable.
func readAccountSettingsCache() (accountSettingsCache, bool) {
	var c accountSettingsCache
	data, err := os.ReadFile(accountSettingsCachePath())
	if err != nil || json.Unmarshal(data, &c) != nil {
		return accountSettingsCache{}, false
	}
	return c, true
}

func accountSettingsCachePath() string {
	return filepath.Join(ClientDataDir(), "account_settings.json")
}

func saveAccountSettingsCache(serverURL string, encryptionEnabled bool) {
	c := accountSettingsCache{
		ServerURL:         serverURL,
		EncryptionEnabled: encryptionEnabled,
		FetchedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	// Preserve the synced appearance across encryption-only updates.
	if prev, ok := readAccountSettingsCache(); ok && prev.ServerURL == serverURL {
		c.Design, c.Layout = prev.Design, prev.Layout
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(ClientDataDir(), 0755)
	if err := atomicWriteFile(accountSettingsCachePath(), data, 0600); err != nil {
		log.Printf("account settings cache: save failed: %v", err)
	}
}

// loadAccountSettingsCache returns the cached state for serverURL, or
// (false, false) when no usable cache exists (different server, unreadable).
func loadAccountSettingsCache(serverURL string) (encryptionEnabled, ok bool) {
	data, err := os.ReadFile(accountSettingsCachePath())
	if err != nil {
		return false, false
	}
	var c accountSettingsCache
	if json.Unmarshal(data, &c) != nil || c.ServerURL != serverURL {
		return false, false
	}
	return c.EncryptionEnabled, true
}

// saveAppearanceCache stores the synced appearance prefs, preserving the
// cached encryption state, and applies them to the local WebUI.
func saveAppearanceCache(serverURL, design, layout string) {
	c := accountSettingsCache{
		ServerURL: serverURL,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Design:    design,
		Layout:    layout,
	}
	if prev, ok := readAccountSettingsCache(); ok && prev.ServerURL == serverURL {
		c.EncryptionEnabled = prev.EncryptionEnabled
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(ClientDataDir(), 0755)
	if err := atomicWriteFile(accountSettingsCachePath(), data, 0600); err != nil {
		log.Printf("account settings cache: save failed: %v", err)
	}
	clientwebui.SetAppearance(design, layout)
}

// applyCachedAppearance pushes the last synced appearance into the local
// WebUI at startup (before any network), so the look survives offline runs.
func applyCachedAppearance() {
	if c, ok := readAccountSettingsCache(); ok {
		clientwebui.SetAppearance(c.Design, c.Layout)
	}
}

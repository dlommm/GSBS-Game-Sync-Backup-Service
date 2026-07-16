package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

// accountSettingsCache is the last account state successfully fetched from
// the server. It exists so a transient /api/account failure at startup cannot
// silently flip an encryption-enabled account to plaintext uploads for the
// whole session: when the server is unreachable we trust the last known
// answer instead of assuming "disabled".
type accountSettingsCache struct {
	ServerURL         string `json:"server_url"`
	EncryptionEnabled bool   `json:"encryption_enabled"`
	FetchedAt         string `json:"fetched_at"`
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

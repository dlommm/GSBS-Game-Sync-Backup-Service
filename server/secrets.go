package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadOrCreateSessionSecret returns a persistent session secret from
// <db dir>/gsbs-keys/session.secret, generating a strong random one on first
// run. This removes GSBS_SESSION_SECRET from the list of required env vars
// (the setup-wizard / zero-config deployment path) while keeping the secret
// stable across restarts. An explicitly-set env value bypasses this entirely.
func loadOrCreateSessionSecret(dbPath string) (string, error) {
	path := filepath.Join(filepath.Dir(dbPath), "gsbs-keys", "session.secret")
	if b, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(b))
		if len(s) >= 32 {
			return s, nil
		}
		// Too short to be safe (hand-edited?) — regenerate.
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	raw := make([]byte, 48)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return secret, nil
}

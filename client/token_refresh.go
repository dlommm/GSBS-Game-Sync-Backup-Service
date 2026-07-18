package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Device tokens expire after 90 days of not being refreshed (server-enforced).
// The client proactively rotates its token monthly so a device that syncs at
// least once a month never hits the expiry → silent-401 → manual re-login
// path. Mid-run rotation is safe: the old token is invalidated server-side,
// and any in-flight request that races it gets a 401 and reloads the new
// token from config (sync.Client.TokenReload).
const tokenRefreshEvery = 30 * 24 * time.Hour

// maybeRefreshToken rotates the device token via POST /api/token/refresh when
// the last rotation is older than tokenRefreshEvery (or unknown). On success
// it persists the new token + timestamp and updates cfg in place so callers
// constructing a sync client afterwards use the fresh token. Failures are
// logged and non-fatal — the current token keeps working until server expiry.
func maybeRefreshToken(ctx context.Context, cfg *config) {
	if cfg == nil || strings.TrimSpace(cfg.Token) == "" || strings.TrimSpace(cfg.ServerURL) == "" {
		return
	}
	if cfg.TokenRefreshedAt != "" {
		if t, err := time.Parse(time.RFC3339, cfg.TokenRefreshedAt); err == nil && time.Since(t) < tokenRefreshEvery {
			return
		}
	}
	newToken, err := requestTokenRefresh(ctx, cfg.ServerURL, cfg.Token)
	if err != nil {
		log.Printf("token refresh: skipped (%v) — current token remains valid until server expiry", err)
		return
	}
	// Persist against the freshest config on disk (another process/tray action
	// may have written settings meanwhile), then update the in-memory copy.
	now := time.Now().UTC().Format(time.RFC3339)
	onDisk, loadErr := loadConfig()
	if loadErr != nil || onDisk == nil {
		onDisk = cfg
	}
	onDisk.Token = newToken
	onDisk.TokenRefreshedAt = now
	if err := saveConfig(onDisk); err != nil {
		// Do NOT adopt the new token in memory if it couldn't be persisted:
		// after a restart the old (now invalid) token would be loaded and the
		// user forced to re-login. The old token was already invalidated, so
		// surface loudly.
		log.Printf("token refresh: FAILED to save rotated token: %v — re-login may be required", err)
		return
	}
	cfg.setTokenAndRefresh(newToken, now)
	log.Printf("token refresh: device token rotated (next rotation in ~%dd)", int(tokenRefreshEvery.Hours()/24))
}

func requestTokenRefresh(ctx context.Context, serverURL, token string) (string, error) {
	url := strings.TrimRight(serverURL, "/") + "/api/token/refresh"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GSBS-Client-Version", Version)
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if strings.TrimSpace(body.Token) == "" {
		return "", fmt.Errorf("empty token in response")
	}
	return body.Token, nil
}

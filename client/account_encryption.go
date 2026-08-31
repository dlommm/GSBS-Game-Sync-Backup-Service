package main

import (
	"context"
	"log"

	clientsync "github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/retry"
)

// applyAccountEncryption configures a one-shot CLI client's E2E encryption
// state from the server, falling back to the cached answer and finally to the
// presence of a configured passphrase.
//
// The CLI paths used to do `if enc, err := FetchAccountSettings(ctx); err == nil`
// and silently skip the call on error, which fails OPEN: a transient /api/account
// failure left encryption disabled and the next push uploaded plaintext for an
// encrypted account. runSync already solves this with retry-plus-cache; this is
// the same logic for the commands that run once and exit.
//
// The passphrase is always installed even when encryption is believed to be
// off, because pull-side decryption keys off each save's own encrypted flag —
// without it, downloading an encrypted save fails with "no passphrase
// configured" even though one is set in the config.
func applyAccountEncryption(ctx context.Context, client *clientsync.Client, cfg *config) {
	err := retry.Do(ctx, retry.DefaultBackoff(), 3, func() error {
		info, ferr := client.FetchAccountInfo(ctx)
		if ferr != nil {
			return ferr
		}
		client.SetEncryption(info.EncryptionEnabled, cfg.EncryptionPassphrase)
		saveAccountSettingsCache(cfg.ServerURL, info.EncryptionEnabled)
		return nil
	})
	if err == nil {
		return
	}
	if cached, ok := loadAccountSettingsCache(cfg.ServerURL); ok {
		log.Printf("account settings: unreachable (%v) — using cached state (encryption=%v)", err, cached)
		client.SetEncryption(cached, cfg.EncryptionPassphrase)
		return
	}
	// No answer and no cache: assume encryption is on whenever a passphrase is
	// configured. Encrypting a save the server would have accepted in plaintext
	// is recoverable; uploading plaintext for an encrypted account is not.
	assumed := cfg.EncryptionPassphrase != ""
	log.Printf("account settings: unreachable (%v), no cache — assuming encryption=%v (passphrase %sconfigured)",
		err, assumed, map[bool]string{true: "", false: "not "}[assumed])
	client.SetEncryption(assumed, cfg.EncryptionPassphrase)
}

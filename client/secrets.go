package main

import (
	"errors"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

// keyringService namespaces GSBS secrets in the OS credential store
// (Windows Credential Manager, macOS Keychain, Linux Secret Service).
const keyringService = "io.github.dlommm.GSBS"

// Secret keys stored under keyringService.
const (
	secretToken      = "token"
	secretPassphrase = "encryption_passphrase"
)

// keyringDisabled reports whether the user has forced file-based secret
// storage via GSBS_TOKEN_STORE=file (escape hatch for headless or sandboxed
// environments without a working Secret Service).
func keyringDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GSBS_TOKEN_STORE")), "file")
}

// secretSet stores a secret. It prefers the OS keyring; when the keyring is
// disabled or unavailable it falls back to the machine-key-encrypted file store
// (secret_file.go). It returns nil on success either way, so saveConfig always
// strips the cleartext secret from config.json — a secret is never persisted in
// plaintext.
func secretSet(key, value string) error {
	if !keyringDisabled() {
		if err := keyring.Set(keyringService, key, value); err == nil {
			return nil
		}
		// Keyring present but the write failed → encrypted file fallback.
	}
	return fileSecretSet(key, value)
}

// secretGet returns a secret from the keyring, falling back to the encrypted
// file store. ok is true only when the secret was found.
func secretGet(key string) (value string, ok bool) {
	v, ok, _ := secretGetStatus(key)
	return v, ok
}

// secretGetStatus is secretGet with the extra answer callers need before they
// treat a missing secret as "the user has none": unavailable reports that the
// credential store could not be READ, as opposed to the secret genuinely not
// being there.
//
// Conflating the two was destructive. On Linux, an autostarted client racing
// the Secret Service at login saw a read failure, loaded a blank token, and a
// later auto-save then called secretDelete — permanently destroying the real
// stored token and force-logging the user out.
func secretGetStatus(key string) (value string, ok bool, unavailable bool) {
	if !keyringDisabled() {
		v, err := keyring.Get(keyringService, key)
		if err == nil {
			return v, true, false
		}
		if !errors.Is(err, keyring.ErrNotFound) {
			unavailable = true
		}
	}
	if v, found := fileSecretGet(key); found {
		return v, true, false
	}
	return "", false, unavailable
}

// secretDelete removes a secret from both the keyring and the encrypted file
// store (best effort).
func secretDelete(key string) {
	if !keyringDisabled() {
		if err := keyring.Delete(keyringService, key); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			// Non-fatal: the secret may not exist or the keyring is absent.
			_ = err
		}
	}
	fileSecretDelete(key)
}

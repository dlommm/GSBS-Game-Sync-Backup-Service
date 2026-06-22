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

// secretSet stores a secret in the OS keyring. A non-nil error means the
// keyring is unavailable and callers should fall back to file storage.
func secretSet(key, value string) error {
	if keyringDisabled() {
		return errors.New("keyring disabled via GSBS_TOKEN_STORE=file")
	}
	return keyring.Set(keyringService, key, value)
}

// secretGet returns a secret from the OS keyring. ok is true only when the
// keyring is available and the secret was found.
func secretGet(key string) (value string, ok bool) {
	if keyringDisabled() {
		return "", false
	}
	v, err := keyring.Get(keyringService, key)
	if err != nil {
		return "", false
	}
	return v, true
}

// secretDelete removes a secret from the OS keyring (best effort).
func secretDelete(key string) {
	if keyringDisabled() {
		return
	}
	if err := keyring.Delete(keyringService, key); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		// Non-fatal: the secret may simply not exist or the keyring is absent.
		return
	}
}

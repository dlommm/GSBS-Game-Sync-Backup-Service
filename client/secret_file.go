package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/gsbs/gsbs/pkg/atomicio"
)

// When the OS keyring is unavailable (headless Linux, some Flatpak sandboxes, or
// GSBS_TOKEN_STORE=file), secrets are stored encrypted in secrets.enc rather than
// left in cleartext inside config.json. They are wrapped with a random 32-byte
// key kept in a sibling 0600 secret.key file, so config.json (or a stray backup
// of it) never contains a usable token or E2E passphrase — the key file must be
// present too.

func secretsDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(dir, "gsbs")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// machineWrapKey loads the local 32-byte wrap key, creating it on first use.
func machineWrapKey() ([]byte, error) {
	d, err := secretsDir()
	if err != nil {
		return nil, err
	}
	keyPath := filepath.Join(d, "secret.key")
	if b, err := os.ReadFile(keyPath); err == nil && len(b) == 32 {
		return b, nil
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := atomicio.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func secretAEAD() (cipher.AEAD, error) {
	key, err := machineWrapKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func fileSecrets() (map[string]string, error) {
	d, err := secretsDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(d, "secrets.enc"))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	gcm, err := secretAEAD()
	if err != nil {
		return nil, err
	}
	blob, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, errors.New("secrets.enc too short")
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(pt, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func saveFileSecrets(m map[string]string) error {
	d, err := secretsDir()
	if err != nil {
		return err
	}
	gcm, err := secretAEAD()
	if err != nil {
		return err
	}
	pt, err := json.Marshal(m)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ct := gcm.Seal(nonce, nonce, pt, nil)
	return atomicio.WriteFile(filepath.Join(d, "secrets.enc"),
		[]byte(base64.StdEncoding.EncodeToString(ct)), 0o600)
}

func fileSecretSet(k, v string) error {
	m, err := fileSecrets()
	if err != nil {
		m = map[string]string{} // unreadable/corrupt → start fresh
	}
	m[k] = v
	return saveFileSecrets(m)
}

func fileSecretGet(k string) (string, bool) {
	m, err := fileSecrets()
	if err != nil {
		return "", false
	}
	v, ok := m[k]
	return v, ok
}

func fileSecretDelete(k string) {
	m, err := fileSecrets()
	if err != nil || len(m) == 0 {
		return
	}
	if _, ok := m[k]; !ok {
		return
	}
	delete(m, k)
	_ = saveFileSecrets(m)
}

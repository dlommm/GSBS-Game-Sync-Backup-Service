package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/hkdf"
)

// Column encryption at rest. Threat model: exfiltration of the database file
// or a database-only backup must not hand an attacker usable 2FA seeds. The
// key material lives OUTSIDE the database in a root-owned file — back up
// gsbs-keys/ together with the database, or 2FA fails closed after a restore
// (recovery runbook in docs/TROUBLESHOOTING.md).
const (
	// encPrefixV1 marks a column value sealed with AES-256-GCM under the
	// server's local key file. Values without the prefix are legacy
	// plaintext and are still readable (and re-sealed by migration 23).
	encPrefixV1 = "enc:v1:"

	// totpKeyInfo is the HKDF info string for the TOTP-secret column. Future
	// encrypted columns reuse the same key file with their own info string,
	// yielding independent keys.
	totpKeyInfo = "gsbs-totp-v1"

	keyFileEnv  = "GSBS_TOTP_KEY_FILE"
	keyFileName = "totp.key"
	keyDirName  = "gsbs-keys"
)

// loadOrCreateColumnKey returns the 32-byte AES key for a column, deriving it
// via HKDF-SHA256(keyfile bytes, info). The key file is created on first use
// (0600 inside a 0700 dir beside the database). In-memory databases get an
// ephemeral key — nothing outlives the process there anyway.
func loadOrCreateColumnKey(dbPath, info string) ([]byte, error) {
	raw, err := loadOrCreateKeyFile(dbPath)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, raw, nil, []byte(info)), key); err != nil {
		return nil, fmt.Errorf("derive column key: %w", err)
	}
	return key, nil
}

func loadOrCreateKeyFile(dbPath string) ([]byte, error) {
	path := strings.TrimSpace(os.Getenv(keyFileEnv))
	if path == "" {
		if isInMemoryPath(dbPath) {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return nil, err
			}
			return raw, nil
		}
		path = filepath.Join(filepath.Dir(dbPath), keyDirName, keyFileName)
	}
	if raw, err := os.ReadFile(path); err == nil {
		if len(raw) < 16 {
			return nil, fmt.Errorf("key file %s is too short (%d bytes); refusing to use it", path, len(raw))
		}
		return raw, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read key file %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create key dir: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, fmt.Errorf("write key file %s: %w", path, err)
	}
	return raw, nil
}

// sealColumn encrypts a column value: enc:v1: + base64(nonce‖ciphertext).
func sealColumn(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return encPrefixV1 + base64.StdEncoding.EncodeToString(append(nonce, ct...)), nil
}

// openColumn decrypts a sealed column value; legacy plaintext (no prefix)
// passes through unchanged so pre-4.0 rows keep working until migrated.
func openColumn(key []byte, stored string) (string, error) {
	if !strings.HasPrefix(stored, encPrefixV1) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefixV1))
	if err != nil {
		return "", fmt.Errorf("decode sealed column: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("sealed column too short")
	}
	pt, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("open sealed column (wrong or missing gsbs-keys/%s?): %w", keyFileName, err)
	}
	return string(pt), nil
}

// totpKey lazily loads (or creates) the TOTP column key, caching the result
// for the store's lifetime.
func (s *sqliteStore) totpKey() ([]byte, error) {
	s.totpKeyOnce.Do(func() {
		s.totpKeyBytes, s.totpKeyErr = loadOrCreateColumnKey(s.dbPath, totpKeyInfo)
	})
	return s.totpKeyBytes, s.totpKeyErr
}

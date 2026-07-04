package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

const (
	saltLen  = 16
	nonceLen = 12
	keyLen   = 32
	iter     = 100000
)

// V2Prefix marks ciphertext produced by EncryptV2 (Argon2id KDF). Legacy
// ciphertext (no prefix, PBKDF2-SHA256/100k) stays decryptable forever;
// Decrypt auto-detects the format. Writers switch to v2 only once every
// device on the account can read it (fleet auto-negotiation), because a
// pre-4.0 client cannot decrypt a v2 blob.
const V2Prefix = "gsbs2:"

// Argon2id parameters (interactive-login class, OWASP-recommended shape).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB → 64 MiB
	argonThreads = 1
)

// DeriveKey derives a 256-bit key from passphrase and salt using PBKDF2
// (legacy v1 format).
func DeriveKey(passphrase string, salt []byte) []byte {
	return pbkdf2.Key([]byte(passphrase), salt, iter, keyLen, sha256.New)
}

// DeriveKeyV2 derives a 256-bit key using Argon2id (v2 format).
func DeriveKeyV2(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThreads, keyLen)
}

// Encrypt encrypts plaintext with AES-GCM using a passphrase in the legacy v1
// format: base64(salt||nonce||ciphertext), PBKDF2 key derivation.
func Encrypt(passphrase string, plaintext []byte) (string, error) {
	body, err := seal(passphrase, plaintext, DeriveKey)
	if err != nil {
		return "", err
	}
	return body, nil
}

// EncryptV2 encrypts plaintext in the v2 format: "gsbs2:" +
// base64(salt||nonce||ciphertext) with an Argon2id-derived key.
func EncryptV2(passphrase string, plaintext []byte) (string, error) {
	body, err := seal(passphrase, plaintext, DeriveKeyV2)
	if err != nil {
		return "", err
	}
	return V2Prefix + body, nil
}

func seal(passphrase string, plaintext []byte, kdf func(string, []byte) []byte) (string, error) {
	if passphrase == "" {
		return "", errors.New("passphrase required")
	}
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}
	key := kdf(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	out := append(salt, nonce...)
	out = append(out, ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt decrypts data produced by Encrypt or EncryptV2, auto-detecting the
// format from the prefix.
func Decrypt(passphrase string, encoded string) ([]byte, error) {
	kdf := DeriveKey
	if strings.HasPrefix(encoded, V2Prefix) {
		kdf = DeriveKeyV2
		encoded = strings.TrimPrefix(encoded, V2Prefix)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(raw) < saltLen+nonceLen {
		return nil, errors.New("ciphertext too short")
	}
	salt := raw[:saltLen]
	nonce := raw[saltLen : saltLen+nonceLen]
	ct := raw[saltLen+nonceLen:]
	key := kdf(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ct, nil)
}

// IsV2 reports whether an encrypted payload uses the v2 (Argon2id) format.
func IsV2(encoded string) bool {
	return strings.HasPrefix(encoded, V2Prefix)
}

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	saltLen  = 16
	nonceLen = 12
	keyLen   = 32
	iter     = 100000
)

// DeriveKey derives a 256-bit key from passphrase and salt using PBKDF2.
func DeriveKey(passphrase string, salt []byte) []byte {
	return pbkdf2.Key([]byte(passphrase), salt, iter, keyLen, sha256.New)
}

// Encrypt encrypts plaintext with AES-GCM using a passphrase. Returns base64(salt||nonce||ciphertext).
func Encrypt(passphrase string, plaintext []byte) (string, error) {
	if passphrase == "" {
		return "", errors.New("passphrase required")
	}
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}
	key := DeriveKey(passphrase, salt)
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

// Decrypt decrypts data produced by Encrypt.
func Decrypt(passphrase string, encoded string) ([]byte, error) {
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
	key := DeriveKey(passphrase, salt)
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

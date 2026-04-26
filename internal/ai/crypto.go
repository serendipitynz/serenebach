package ai

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// SecretEnvVar is the environment variable callers read when they
// need the master key. Exposed as a constant so settings pages and
// startup diagnostics reference the same name.
const SecretEnvVar = "SB_AI_SECRET"

// MasterKey returns the AES-256 key derived from SB_AI_SECRET. The
// env value is hashed with SHA-256 so any length of passphrase maps
// to a 32-byte key — operators can paste a password, a base64 blob,
// or anything in between. Returns (nil, nil) when the env is unset
// so callers can render a "AI features disabled until SB_AI_SECRET
// is configured" flash instead of crashing the process.
func MasterKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(SecretEnvVar))
	if raw == "" {
		return nil, nil
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

// SecretConfigured is a cheap predicate the admin UI calls to decide
// whether to render the AI settings form at all. Returns false when
// SB_AI_SECRET is unset or empty.
func SecretConfigured() bool {
	return strings.TrimSpace(os.Getenv(SecretEnvVar)) != ""
}

// Encrypt produces ciphertext for plaintext using AES-256-GCM with
// SB_AI_SECRET as the master key. Output layout: hex(nonce || ct).
// Empty plaintext returns an empty string so callers can treat
// "no key configured" as a valid state without special-casing.
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key, err := MasterKey()
	if err != nil {
		return "", err
	}
	if key == nil {
		return "", fmt.Errorf("ai: %s is not set; cannot encrypt", SecretEnvVar)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("ai: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("ai: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("ai: nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(nonce) + hex.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt. Returns ("", nil) for empty input so
// the zero-value db row round-trips cleanly.
func Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	key, err := MasterKey()
	if err != nil {
		return "", err
	}
	if key == nil {
		return "", fmt.Errorf("ai: %s is not set; cannot decrypt", SecretEnvVar)
	}

	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("ai: decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("ai: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("ai: gcm: %w", err)
	}
	ns := gcm.NonceSize()
	if len(raw) < ns+gcm.Overhead() {
		return "", errors.New("ai: ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("ai: decrypt: %w", err)
	}
	return string(pt), nil
}

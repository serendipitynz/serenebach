package ai

import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	t.Setenv(SecretEnvVar, "correct horse battery staple")

	want := "sk-ant-xxx-very-secret"
	enc, err := Encrypt(want)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == want {
		t.Fatal("ciphertext equals plaintext — encryption not applied")
	}
	if strings.TrimSpace(enc) == "" {
		t.Fatal("ciphertext empty")
	}

	got, err := Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != want {
		t.Errorf("roundtrip = %q, want %q", got, want)
	}
}

func TestEncryptDifferentNoncesPerCall(t *testing.T) {
	// Two encrypts of the same plaintext must produce different
	// ciphertext — otherwise a pattern leak lets an observer infer
	// which users share an API key.
	t.Setenv(SecretEnvVar, "s")
	a, err := Encrypt("same")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	b, err := Encrypt("same")
	if err != nil {
		t.Fatalf("Encrypt 2: %v", err)
	}
	if a == b {
		t.Fatal("nonce reuse: ciphertexts collide")
	}
}

func TestEncryptEmptyPlaintextReturnsEmpty(t *testing.T) {
	// Empty plaintext short-circuits so callers can round-trip a
	// "no key configured" user row without pre-checking.
	t.Setenv(SecretEnvVar, "s")
	enc, err := Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	if enc != "" {
		t.Fatalf("Encrypt empty = %q, want empty", enc)
	}
}

func TestEncryptRequiresSecret(t *testing.T) {
	t.Setenv(SecretEnvVar, "")
	_, err := Encrypt("x")
	if err == nil {
		t.Fatal("Encrypt without secret should fail")
	}
	if SecretConfigured() {
		t.Fatal("SecretConfigured should be false when env is empty")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	t.Setenv(SecretEnvVar, "s")
	enc, err := Encrypt("payload")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Flip the last hex digit. GCM's auth tag should fail the open.
	tampered := enc[:len(enc)-1]
	if enc[len(enc)-1] == 'a' {
		tampered += "b"
	} else {
		tampered += "a"
	}
	if _, err := Decrypt(tampered); err == nil {
		t.Fatal("Decrypt accepted tampered ciphertext")
	}
}

// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plaintext := []byte("DATABASE_URL=postgres://localhost:5432/mydb\nAPI_KEY=sk_test_12345\n")

	// Generate a test key
	key := [32]byte{}
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}

	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}

	// Verify magic bytes
	if string(encrypted[:8]) != MagicBytes {
		t.Errorf("magic bytes mismatch: got %q", encrypted[:8])
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	plaintext := []byte("secret data")

	key1 := [32]byte{1}
	key2 := [32]byte{2}

	encrypted, err := Encrypt(plaintext, key1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decrypt(encrypted, key2)
	if err == nil {
		t.Error("expected error decrypting with wrong key")
	}
}

func TestDecryptCorruptData(t *testing.T) {
	plaintext := []byte("secret data")
	key := [32]byte{1}

	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt a byte in the ciphertext
	encrypted[len(encrypted)-5] ^= 0xff

	_, err = Decrypt(encrypted, key)
	if err == nil {
		t.Error("expected error decrypting corrupted data")
	}
}

func TestDecryptTooShort(t *testing.T) {
	key := [32]byte{1}

	_, err := Decrypt([]byte("short"), key)
	if err == nil {
		t.Error("expected error for short data")
	}
}

func TestDecryptBadMagic(t *testing.T) {
	key := [32]byte{1}
	data := make([]byte, 100)
	copy(data, "NOTMAGIC")

	_, err := Decrypt(data, key)
	if err == nil {
		t.Error("expected error for bad magic bytes")
	}
}

func TestDeriveAtRestKey(t *testing.T) {
	keyMaterial := []byte("test ssh private key bytes for derivation testing purposes here")

	key1, err := DeriveAtRestKey(keyMaterial)
	if err != nil {
		t.Fatal(err)
	}

	// Same input should produce same output (deterministic)
	key2, err := DeriveAtRestKey(keyMaterial)
	if err != nil {
		t.Fatal(err)
	}

	if key1 != key2 {
		t.Error("HKDF not deterministic")
	}

	// Different input should produce different output
	key3, err := DeriveAtRestKey([]byte("different key material"))
	if err != nil {
		t.Fatal(err)
	}

	if key1 == key3 {
		t.Error("different inputs produced same key")
	}
}

func TestDeriveAtRestKeyEmpty(t *testing.T) {
	_, err := DeriveAtRestKey([]byte{})
	if err == nil {
		t.Error("expected error for empty key material")
	}
}

func TestRecipientEncryptDecrypt(t *testing.T) {
	// Generate recipient's Ed25519 keypair
	_, recipientEd25519Priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	recipientKP, err := NewKeyPairFromEd25519(recipientEd25519Priv)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("API_KEY=sk_live_supersecret")

	// Encrypt for recipient
	ephPub, encrypted, err := EncryptForRecipient(plaintext, recipientKP.X25519Public)
	if err != nil {
		t.Fatalf("EncryptForRecipient error: %v", err)
	}

	// Decrypt as recipient
	decrypted, err := DecryptFromSender(encrypted, ephPub, recipientKP.X25519Private, recipientKP.X25519Public)
	if err != nil {
		t.Fatalf("DecryptFromSender error: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("relay round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestRecipientEncryptWrongRecipient(t *testing.T) {
	// Generate two keypairs
	_, priv1, _ := ed25519.GenerateKey(rand.Reader)
	_, priv2, _ := ed25519.GenerateKey(rand.Reader)

	kp1, _ := NewKeyPairFromEd25519(priv1)
	kp2, _ := NewKeyPairFromEd25519(priv2)

	// Encrypt for kp1
	ephPub, encrypted, err := EncryptForRecipient([]byte("secret"), kp1.X25519Public)
	if err != nil {
		t.Fatal(err)
	}

	// Try to decrypt as kp2 (wrong recipient)
	_, err = DecryptFromSender(encrypted, ephPub, kp2.X25519Private, kp2.X25519Public)
	if err == nil {
		t.Error("expected error when wrong recipient decrypts")
	}
}

func TestSignatureRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	body := []byte(`{"team_id": "test-team"}`)
	fingerprint := "SHA256:testfingerprint"

	authHeader := SignRequest(priv, fingerprint, "POST", "/invites", body)

	err := VerifyRequestSignature(pub, authHeader, "POST", "/invites", body)
	if err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestSignatureTamperedBody(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	body := []byte(`{"team_id": "test-team"}`)
	fingerprint := "SHA256:testfingerprint"

	authHeader := SignRequest(priv, fingerprint, "POST", "/invites", body)

	// Tamper with body
	err := VerifyRequestSignature(pub, authHeader, "POST", "/invites", []byte(`{"team_id": "hacked"}`))
	if err == nil {
		t.Error("expected error for tampered body")
	}
}

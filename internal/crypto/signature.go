// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SignRequest signs a relay API request using Ed25519.
// Returns the Authorization header value.
// Includes the public key for TOFU key-pinning on first contact.
func SignRequest(privateKey ed25519.PrivateKey, fingerprint, method, path string, bodyHash []byte) string {
	timestamp := time.Now().Unix()
	bodyHashHex := fmt.Sprintf("%x", sha256.Sum256(bodyHash))
	payload := fmt.Sprintf("%s\n%s\n%d\n%s", method, path, timestamp, bodyHashHex)

	signature := ed25519.Sign(privateKey, []byte(payload))
	sigBase64 := base64.StdEncoding.EncodeToString(signature)
	pubKeyBase64 := base64.StdEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey))

	return fmt.Sprintf("ES-SIG timestamp=%d,fingerprint=%s,signature=%s,public_key=%s",
		timestamp, fingerprint, sigBase64, pubKeyBase64)
}

// VerifyRequestSignature verifies a relay API request signature.
func VerifyRequestSignature(publicKey ed25519.PublicKey, authHeader, method, path string, bodyHash []byte) error {
	// Parse header: "ES-SIG timestamp=...,fingerprint=...,signature=..."
	if !strings.HasPrefix(authHeader, "ES-SIG ") {
		return fmt.Errorf("invalid auth header format")
	}

	params := parseAuthParams(authHeader[7:])

	timestampStr, ok := params["timestamp"]
	if !ok {
		return fmt.Errorf("missing timestamp in auth header")
	}

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	// Check timestamp window (5 minutes)
	now := time.Now().Unix()
	if abs64(now-timestamp) > 300 {
		return fmt.Errorf("request timestamp expired (>5 minute window)")
	}

	sigBase64, ok := params["signature"]
	if !ok {
		return fmt.Errorf("missing signature in auth header")
	}

	signature, err := base64.StdEncoding.DecodeString(sigBase64)
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}

	// Reconstruct the signed payload
	bodyHashHex := fmt.Sprintf("%x", sha256.Sum256(bodyHash))
	payload := fmt.Sprintf("%s\n%s\n%d\n%s", method, path, timestamp, bodyHashHex)

	if !ed25519.Verify(publicKey, []byte(payload), signature) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

func parseAuthParams(s string) map[string]string {
	params := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			params[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return params
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// SignBlob signs an encrypted blob for sender authentication on relay uploads.
// Signs SHA-256(encrypted_data || ephemeral_key || sender_fingerprint).
func SignBlob(privateKey ed25519.PrivateKey, encryptedData, ephemeralKey []byte, senderFP string) []byte {
	h := sha256.New()
	h.Write(encryptedData)
	h.Write(ephemeralKey)
	h.Write([]byte(senderFP))
	digest := h.Sum(nil)
	return ed25519.Sign(privateKey, digest)
}

// VerifyBlobSignature verifies a sender's signature on a relay blob.
func VerifyBlobSignature(publicKey ed25519.PublicKey, encryptedData, ephemeralKey []byte, senderFP string, signature []byte) bool {
	h := sha256.New()
	h.Write(encryptedData)
	h.Write(ephemeralKey)
	h.Write([]byte(senderFP))
	digest := h.Sum(nil)
	return ed25519.Verify(publicKey, digest, signature)
}

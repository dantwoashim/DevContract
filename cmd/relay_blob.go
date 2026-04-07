// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"

	"github.com/dantwoashim/Env_sync/internal/crypto"
)

func verifyRelayBlobSignature(memberKeyMap map[string]ed25519.PublicKey, senderFingerprint string, data []byte, ephKey [32]byte, sigB64 string) error {
	if sigB64 == "" {
		return fmt.Errorf("missing signature for relay blob from %s", senderFingerprint)
	}

	pubKey := memberKeyMap[senderFingerprint]
	if pubKey == nil {
		return fmt.Errorf("missing sender public key for %s", senderFingerprint)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("invalid signature encoding for %s: %w", senderFingerprint, err)
	}

	if !crypto.VerifyBlobSignature(pubKey, data, ephKey[:], senderFingerprint, sigBytes) {
		return fmt.Errorf("signature verification failed for %s", senderFingerprint)
	}

	return nil
}

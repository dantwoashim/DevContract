// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package relay

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dantwoashim/Env_sync/internal/crypto"
)

func TestListPendingSignsPathWithoutQueryString(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	kp, err := crypto.NewKeyPairFromEd25519(privateKey)
	if err != nil {
		t.Fatalf("new keypair: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		publicKeyB64 := parseAuthField(authHeader, "public_key")
		publicKey, err := base64.StdEncoding.DecodeString(publicKeyB64)
		if err != nil {
			t.Fatalf("decode public key: %v", err)
		}

		if err := crypto.VerifyRequestSignature(ed25519.PublicKey(publicKey), authHeader, r.Method, r.URL.Path, nil); err != nil {
			t.Fatalf("verify signature: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pending":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, kp)
	if _, err := client.ListPending("project-1"); err != nil {
		t.Fatalf("list pending: %v", err)
	}
}

func parseAuthField(header, key string) string {
	parts := strings.Split(strings.TrimPrefix(header, "ES-SIG "), ",")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

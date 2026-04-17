// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dantwoashim/devcontract/internal/crypto"
	"github.com/dantwoashim/devcontract/internal/relay"
	"github.com/dantwoashim/devcontract/internal/revision"
	devcontract "github.com/dantwoashim/devcontract/internal/sync"
)

func main() {
	var relayURL string
	var serviceKeyPath string
	var projectID string
	var fileName string

	flag.StringVar(&relayURL, "relay-url", "http://127.0.0.1:8787", "Relay base URL")
	flag.StringVar(&serviceKeyPath, "service-key", "", "Path to the DevContract service key")
	flag.StringVar(&projectID, "project", "smoke-team", "Project ID to seed")
	flag.StringVar(&fileName, "file", ".env.test", "Filename to attach to the relay blob")
	flag.Parse()

	if serviceKeyPath == "" {
		exitf("missing --service-key")
	}

	if err := waitForRelay(relayURL); err != nil {
		exitf("relay unavailable: %v", err)
	}

	serviceKey, err := crypto.LoadServiceKeyFromFile(serviceKeyPath)
	if err != nil {
		exitf("load service key: %v", err)
	}
	serviceKP, err := crypto.NewKeyPairFromEd25519(serviceKey.PrivateKey)
	if err != nil {
		exitf("derive service identity: %v", err)
	}

	_, ownerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		exitf("generate owner identity: %v", err)
	}
	ownerKP, err := crypto.NewKeyPairFromEd25519(ownerPriv)
	if err != nil {
		exitf("derive owner identity: %v", err)
	}

	ownerClient := relay.NewClient(relayURL, ownerKP)
	if err := ownerClient.BootstrapTeam(projectID, relay.BootstrapTeamRequest{
		Username:             "owner",
		Fingerprint:          ownerKP.Fingerprint,
		PublicKey:            base64.StdEncoding.EncodeToString(ownerKP.Ed25519Public),
		TransportPublicKey:   base64.StdEncoding.EncodeToString(ownerKP.X25519Public[:]),
		TransportFingerprint: crypto.ComputeFingerprint(ownerKP.X25519Public),
		TeamName:             projectID,
		BootstrapNonce:       fmt.Sprintf("action-smoke-%d", time.Now().UnixNano()),
	}); err != nil {
		exitf("bootstrap owner: %v", err)
	}

	if err := ownerClient.UpsertTeamMember(projectID, relay.UpsertTeamMemberRequest{
		Username:             "ci",
		Fingerprint:          serviceKP.Fingerprint,
		PublicKey:            base64.StdEncoding.EncodeToString(serviceKP.Ed25519Public),
		TransportPublicKey:   base64.StdEncoding.EncodeToString(serviceKP.X25519Public[:]),
		TransportFingerprint: crypto.ComputeFingerprint(serviceKP.X25519Public),
		Role:                 "member",
		PrincipalType:        "service_principal",
		Scopes:               []string{"relay.pull", "member.read"},
	}); err != nil {
		exitf("register service key: %v", err)
	}

	plaintext := []byte("API_KEY=abc123\nMULTILINE=\"line1\\nline2\"\n")
	payload := devcontract.NewEnvPayload(fileName, plaintext, time.Now().UnixMilli(), "", revision.RevisionID(plaintext))
	encodedPayload, err := devcontract.EncodeEnvPayload(payload)
	if err != nil {
		exitf("encode payload: %v", err)
	}
	ephPub, encrypted, err := crypto.EncryptForRecipient(encodedPayload, serviceKP.X25519Public)
	if err != nil {
		exitf("encrypt payload: %v", err)
	}
	signature := crypto.SignBlob(ownerKP.Ed25519Private, encrypted, ephPub[:], ownerKP.Fingerprint)

	blobID := fmt.Sprintf("action-smoke-%d", time.Now().UnixMilli())
	if err := ownerClient.UploadBlob(
		projectID,
		blobID,
		encrypted,
		ownerKP.Fingerprint,
		serviceKP.Fingerprint,
		base64.StdEncoding.EncodeToString(ephPub[:]),
		fileName,
		base64.StdEncoding.EncodeToString(signature),
	); err != nil {
		exitf("upload smoke blob: %v", err)
	}
}

func waitForRelay(relayURL string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for range 60 {
		resp, err := client.Get(relayURL + "/health")
		if err == nil && resp.StatusCode == 200 {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return lastErr
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

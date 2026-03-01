// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package relay

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/envsync/envsync/internal/crypto"
)

// Client is an HTTP client for the EnvSync relay API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	privateKey ed25519.PrivateKey
	fingerprint string
}

// NewClient creates a new relay client.
func NewClient(baseURL string, kp *crypto.KeyPair) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		privateKey:  kp.Ed25519Private,
		fingerprint: kp.Fingerprint,
	}
}

// doRequest performs a signed HTTP request with retry.
func (c *Client) doRequest(method, path string, body []byte) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}

		url := c.baseURL + path

		var reqBody io.Reader
		if body != nil {
			reqBody = bytes.NewReader(body)
		}

		req, err := http.NewRequest(method, url, reqBody)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}

		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		// Sign the request
		bodyHash := body
		if bodyHash == nil {
			bodyHash = []byte{}
		}
		authHeader := crypto.SignRequest(c.privateKey, c.fingerprint, method, path, bodyHash)
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("X-EnvSync-Fingerprint", c.fingerprint)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		// Retry on 5xx
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("relay returned HTTP %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("relay request failed after 3 attempts: %w", lastErr)
}

// doUploadRequest is like doRequest but adds custom headers for blob uploads.
func (c *Client) doUploadRequest(method, path string, body []byte, headers map[string]string) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}

		url := c.baseURL + path

		var reqBody io.Reader
		if body != nil {
			reqBody = bytes.NewReader(body)
		}

		req, err := http.NewRequest(method, url, reqBody)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}

		// Set custom headers
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		// Sign the request
		bodyHash := body
		if bodyHash == nil {
			bodyHash = []byte{}
		}
		authHeader := crypto.SignRequest(c.privateKey, c.fingerprint, method, path, bodyHash)
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("X-EnvSync-Fingerprint", c.fingerprint)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("relay returned HTTP %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("relay upload failed after 3 attempts: %w", lastErr)
}

// Health checks the relay health.
func (c *Client) Health() (map[string]interface{}, error) {
	resp, err := c.doRequest("GET", "/health", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// --- Invite operations ---

// InviteRequest is the request body for creating an invite.
type InviteRequest struct {
	TokenHash           string `json:"token_hash"`
	TeamID              string `json:"team_id"`
	Inviter             string `json:"inviter"`
	InviterFingerprint  string `json:"inviter_fingerprint"`
	Invitee             string `json:"invitee"`
	ExpectedFingerprint string `json:"expected_fingerprint"`
}

// InviteResponse is the response from the invite endpoint.
type InviteResponse struct {
	TeamID              string `json:"team_id"`
	Inviter             string `json:"inviter"`
	InviterFingerprint  string `json:"inviter_fingerprint"`
	ExpectedFingerprint string `json:"expected_fingerprint"`
	ExpiresAt           int64  `json:"expires_at"`
}

// CreateInvite creates a new invite on the relay.
func (c *Client) CreateInvite(req InviteRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := c.doRequest("POST", "/invites", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return readError(resp)
	}

	return nil
}

// GetInvite retrieves an invite by token hash.
func (c *Client) GetInvite(tokenHash string) (*InviteResponse, error) {
	resp, err := c.doRequest("GET", "/invites/"+tokenHash, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var invite InviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&invite); err != nil {
		return nil, err
	}
	return &invite, nil
}

// ConsumeInvite consumes (redeems) an invite via doRequest for retry + signing.
func (c *Client) ConsumeInvite(tokenHash string) (*InviteResponse, error) {
	resp, err := c.doRequest("DELETE", "/invites/"+tokenHash, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var result InviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// --- Blob operations ---

// UploadBlob uploads an encrypted blob to the relay.
// Uses doRequest for retry and consistent signing, then adds blob-specific headers.
func (c *Client) UploadBlob(teamID, blobID string, data []byte, senderFP, recipientFP, ephemeralKey, filename string) error {
	path := "/relay/" + teamID + "/" + blobID

	// Use doRequest for retry + signing consistency
	// We need to temporarily override httpClient to inject headers
	resp, err := c.doUploadRequest("PUT", path, data, map[string]string{
		"Content-Type":          "application/octet-stream",
		"X-EnvSync-Sender":     senderFP,
		"X-EnvSync-Recipient":  recipientFP,
		"X-EnvSync-EphemeralKey": ephemeralKey,
		"X-EnvSync-Filename":   filename,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return readError(resp)
	}

	return nil
}

// BlobInfo describes a pending blob.
type BlobInfo struct {
	BlobID              string `json:"blob_id"`
	TeamID              string `json:"team_id"`
	SenderFingerprint   string `json:"sender_fingerprint"`
	EphemeralPublicKey  string `json:"ephemeral_public_key"`
	Size                int    `json:"size"`
	UploadedAt          int64  `json:"uploaded_at"`
	Filename            string `json:"filename"`
}

// ListPending lists pending blobs for the current identity.
func (c *Client) ListPending(teamID string) ([]BlobInfo, error) {
	path := fmt.Sprintf("/relay/%s/pending?for=%s", teamID, c.fingerprint)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var result struct {
		Pending []BlobInfo `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Pending, nil
}

// DownloadBlob downloads an encrypted blob from the relay.
func (c *Client) DownloadBlob(teamID, blobID string) ([]byte, string, string, error) {
	path := fmt.Sprintf("/relay/%s/%s", teamID, blobID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, "", "", readError(resp)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", "", err
	}

	ephemeralKey := resp.Header.Get("X-EnvSync-EphemeralKey")
	filename := resp.Header.Get("X-EnvSync-Filename")

	return data, ephemeralKey, filename, nil
}

// DeleteBlob removes a blob after download.
func (c *Client) DeleteBlob(teamID, blobID string) error {
	path := fmt.Sprintf("/relay/%s/%s", teamID, blobID)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return readError(resp)
	}
	return nil
}

// --- Team operations ---

// AddTeamMember adds a member to a team on the relay.
func (c *Client) AddTeamMember(teamID, username, fingerprint, publicKey string) error {
	body, _ := json.Marshal(map[string]string{
		"fingerprint": fingerprint,
		"public_key":  publicKey,
	})

	path := fmt.Sprintf("/teams/%s/members/%s", teamID, username)
	resp, err := c.doRequest("PUT", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return readError(resp)
	}
	return nil
}

// RemoveTeamMember removes a member from a team on the relay.
func (c *Client) RemoveTeamMember(teamID, username string) error {
	path := fmt.Sprintf("/teams/%s/members/%s", teamID, username)
	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return readError(resp)
	}
	return nil
}

// --- Helpers ---

func readError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
		return fmt.Errorf("relay error: %s — %s", errResp.Error, errResp.Message)
	}
	return fmt.Errorf("relay returned HTTP %d: %s", resp.StatusCode, string(body))
}

// HashToken computes the SHA-256 hash of a mnemonic token for relay storage.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

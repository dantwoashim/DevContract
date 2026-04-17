// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package relay

import (
	"bytes"
	"crypto/ed25519"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"github.com/dantwoashim/devcontract/internal/crypto"
)

// Client is an HTTP client for the DevContract relay API.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	privateKey  ed25519.PrivateKey
	fingerprint string
}

// TeamMember represents a registered project member on the relay.
type TeamMember struct {
	Username             string   `json:"username"`
	Fingerprint          string   `json:"fingerprint"`
	PublicKey            string   `json:"public_key"`
	TransportPublicKey   string   `json:"transport_public_key"`
	TransportFingerprint string   `json:"transport_fingerprint"`
	Role                 string   `json:"role"`
	PrincipalType        string   `json:"principal_type,omitempty"`
	Scopes               []string `json:"scopes,omitempty"`
	AddedAt              int64    `json:"added_at"`
}

type TeamMetrics struct {
	TeamID             string         `json:"team_id"`
	MemberCount        int            `json:"member_count"`
	PendingCount       int            `json:"pending_count"`
	PendingByRecipient map[string]int `json:"pending_by_recipient"`
	UploadsToday       int            `json:"uploads_today"`
	EventTotals        map[string]int `json:"event_totals"`
	EventsToday        map[string]int `json:"events_today"`
	RecordedAt         string         `json:"recorded_at"`
}

type TeamInvite struct {
	TokenHash             string `json:"token_hash"`
	TeamID                string `json:"team_id"`
	Inviter               string `json:"inviter"`
	InviterFingerprint    string `json:"inviter_fingerprint"`
	Invitee               string `json:"invitee"`
	CreatedAt             int64  `json:"created_at"`
	ExpiresAt             int64  `json:"expires_at"`
	Consumed              bool   `json:"consumed"`
	ConsumedAt            int64  `json:"consumed_at,omitempty"`
	ConsumedByFingerprint string `json:"consumed_by_fingerprint,omitempty"`
	RevokedAt             int64  `json:"revoked_at,omitempty"`
	RevokedByFingerprint  string `json:"revoked_by_fingerprint,omitempty"`
	RevokeReason          string `json:"revoke_reason,omitempty"`
	Status                string `json:"status,omitempty"`
}

type TeamAuditEvent struct {
	ID                 string   `json:"id"`
	TeamID             string   `json:"team_id"`
	Action             string   `json:"action"`
	ActorFingerprint   string   `json:"actor_fingerprint,omitempty"`
	ActorPrincipalType string   `json:"actor_principal_type,omitempty"`
	ActorScopes        []string `json:"actor_scopes,omitempty"`
	TargetFingerprint  string   `json:"target_fingerprint,omitempty"`
	InviteHash         string   `json:"invite_hash,omitempty"`
	BlobID             string   `json:"blob_id,omitempty"`
	Result             string   `json:"result"`
	Details            string   `json:"details,omitempty"`
	CreatedAt          int64    `json:"created_at"`
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

func (c *Client) doRequest(method, path string, body []byte) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay(attempt))
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

		bodyHash := body
		if bodyHash == nil {
			bodyHash = []byte{}
		}
		authHeader := crypto.SignRequest(c.privateKey, c.fingerprint, method, signingPath(path), bodyHash)
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("X-DevContract-Fingerprint", c.fingerprint)

		// #nosec G704 -- the request target is the configured DevContract relay endpoint.
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("relay returned HTTP %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("relay request failed after 3 attempts: %w", lastErr)
}

func (c *Client) doUploadRequest(method, path string, body []byte, headers map[string]string) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay(attempt))
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

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		bodyHash := body
		if bodyHash == nil {
			bodyHash = []byte{}
		}
		authHeader := crypto.SignRequest(c.privateKey, c.fingerprint, method, signingPath(path), bodyHash)
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("X-DevContract-Fingerprint", c.fingerprint)

		// #nosec G704 -- the request target is the configured DevContract relay endpoint.
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			_ = resp.Body.Close()
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

// InviteRequest is the request body for creating an invite.
type InviteRequest struct {
	TokenHash          string `json:"token_hash"`
	TeamID             string `json:"team_id"`
	Inviter            string `json:"inviter"`
	InviterFingerprint string `json:"inviter_fingerprint"`
	Invitee            string `json:"invitee"`
}

// InviteResponse is the response from the invite endpoint.
type InviteResponse struct {
	TeamID             string `json:"team_id"`
	Inviter            string `json:"inviter"`
	InviterFingerprint string `json:"inviter_fingerprint"`
	ExpiresAt          int64  `json:"expires_at"`
}

type JoinInviteRequest struct {
	Username             string `json:"username"`
	Fingerprint          string `json:"fingerprint"`
	PublicKey            string `json:"public_key"`
	TransportPublicKey   string `json:"transport_public_key"`
	TransportFingerprint string `json:"transport_fingerprint"`
}

type BootstrapTeamRequest struct {
	Username             string `json:"username"`
	Fingerprint          string `json:"fingerprint"`
	PublicKey            string `json:"public_key"`
	TransportPublicKey   string `json:"transport_public_key"`
	TransportFingerprint string `json:"transport_fingerprint"`
	TeamName             string `json:"team_name,omitempty"`
	BootstrapNonce       string `json:"bootstrap_nonce"`
	ContractHash         string `json:"contract_hash,omitempty"`
}

type UpsertTeamMemberRequest struct {
	Username             string   `json:"username"`
	Fingerprint          string   `json:"fingerprint"`
	PublicKey            string   `json:"public_key"`
	TransportPublicKey   string   `json:"transport_public_key"`
	TransportFingerprint string   `json:"transport_fingerprint"`
	Role                 string   `json:"role,omitempty"`
	PrincipalType        string   `json:"principal_type,omitempty"`
	Scopes               []string `json:"scopes,omitempty"`
}

type RejectedBlobInfo struct {
	BlobID               string `json:"blob_id"`
	TeamID               string `json:"team_id"`
	SenderFingerprint    string `json:"sender_fingerprint"`
	RecipientFingerprint string `json:"recipient_fingerprint"`
	Status               string `json:"status"`
	FailureReason        string `json:"failure_reason,omitempty"`
	RejectedAt           int64  `json:"rejected_at,omitempty"`
	Filename             string `json:"filename"`
}

type JoinInviteResponse struct {
	TeamID             string       `json:"team_id"`
	Inviter            string       `json:"inviter"`
	InviterFingerprint string       `json:"inviter_fingerprint"`
	JoinerFingerprint  string       `json:"joiner_fingerprint"`
	Members            []TeamMember `json:"members"`
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

// ConsumeInvite consumes (redeems) an invite.
func (c *Client) ConsumeInvite(tokenHash, joinerLabel string) (*InviteResponse, error) {
	path := "/invites/" + tokenHash
	if joinerLabel != "" {
		path = fmt.Sprintf("%s?joiner=%s", path, url.QueryEscape(joinerLabel))
	}
	resp, err := c.doRequest("DELETE", path, nil)
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

// JoinInvite atomically redeems an invite and registers the authenticated member.
func (c *Client) JoinInvite(tokenHash string, req JoinInviteRequest) (*JoinInviteResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest("POST", "/invites/"+tokenHash+"/join", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var result JoinInviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// BootstrapTeam creates the initial relay-side project record with an explicit founder.
func (c *Client) BootstrapTeam(teamID string, req BootstrapTeamRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := c.doRequest("POST", "/teams/"+teamID+"/bootstrap", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return readError(resp)
	}
	return nil
}

// UploadBlob uploads an encrypted blob to the relay.
func (c *Client) UploadBlob(teamID, blobID string, data []byte, senderFP, recipientFP, ephemeralKey, filename, senderSig string) error {
	path := "/relay/" + teamID + "/" + blobID
	resp, err := c.doUploadRequest("PUT", path, data, map[string]string{
		"Content-Type":               "application/octet-stream",
		"X-DevContract-Sender":       senderFP,
		"X-DevContract-Recipient":    recipientFP,
		"X-DevContract-EphemeralKey": ephemeralKey,
		"X-DevContract-Filename":     filename,
		"X-DevContract-Signature":    senderSig,
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
	BlobID             string `json:"blob_id"`
	TeamID             string `json:"team_id"`
	SenderFingerprint  string `json:"sender_fingerprint"`
	EphemeralPublicKey string `json:"ephemeral_public_key"`
	Size               int    `json:"size"`
	UploadedAt         int64  `json:"uploaded_at"`
	Filename           string `json:"filename"`
}

// ListPending lists pending blobs for the current identity.
func (c *Client) ListPending(teamID string) ([]BlobInfo, error) {
	path := fmt.Sprintf("/relay/%s/pending", teamID)
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
func (c *Client) DownloadBlob(teamID, blobID string) ([]byte, string, string, string, error) {
	path := fmt.Sprintf("/relay/%s/%s", teamID, blobID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, "", "", "", readError(resp)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", "", "", err
	}

	return data,
		resp.Header.Get("X-DevContract-EphemeralKey"),
		resp.Header.Get("X-DevContract-Filename"),
		resp.Header.Get("X-DevContract-Signature"),
		nil
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

// RejectBlob retires a malformed relay payload so it does not keep resurfacing.
func (c *Client) RejectBlob(teamID, blobID, reason string) error {
	body, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/relay/%s/%s/reject", teamID, blobID)
	resp, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return readError(resp)
	}
	return nil
}

// ListRejectedBlobs returns rejected/quarantined relay metadata for operators.
func (c *Client) ListRejectedBlobs(teamID string) ([]RejectedBlobInfo, error) {
	path := fmt.Sprintf("/relay/%s/rejected", teamID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var result struct {
		Rejected []RejectedBlobInfo `json:"rejected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Rejected, nil
}

// AddTeamMember adds or updates a member on the relay.
func (c *Client) AddTeamMember(teamID, username, fingerprint, publicKey, transportPublicKey, transportFingerprint, role string) error {
	return c.UpsertTeamMember(teamID, UpsertTeamMemberRequest{
		Username:             username,
		Fingerprint:          fingerprint,
		PublicKey:            publicKey,
		TransportPublicKey:   transportPublicKey,
		TransportFingerprint: transportFingerprint,
		Role:                 role,
		PrincipalType:        "human_member",
	})
}

// UpsertTeamMember adds or updates a principal on the relay.
func (c *Client) UpsertTeamMember(teamID string, req UpsertTeamMemberRequest) error {
	body, _ := json.Marshal(map[string]string{
		"fingerprint":           req.Fingerprint,
		"public_key":            req.PublicKey,
		"transport_public_key":  req.TransportPublicKey,
		"transport_fingerprint": req.TransportFingerprint,
		"role":                  req.Role,
		"principal_type":        req.PrincipalType,
	})
	if len(req.Scopes) > 0 {
		payload := map[string]any{
			"fingerprint":           req.Fingerprint,
			"public_key":            req.PublicKey,
			"transport_public_key":  req.TransportPublicKey,
			"transport_fingerprint": req.TransportFingerprint,
			"role":                  req.Role,
			"principal_type":        req.PrincipalType,
			"scopes":                req.Scopes,
		}
		body, _ = json.Marshal(payload)
	}

	path := fmt.Sprintf("/teams/%s/members/%s", teamID, req.Username)
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

// RemoveTeamMemberByFingerprint removes a member from a team using the stable identity fingerprint.
func (c *Client) RemoveTeamMemberByFingerprint(teamID, fingerprint string) error {
	path := fmt.Sprintf("/teams/%s/members/by-fingerprint/%s", teamID, url.PathEscape(fingerprint))
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

type RotateSelfRequest struct {
	Username             string `json:"username"`
	Fingerprint          string `json:"fingerprint"`
	PublicKey            string `json:"public_key"`
	TransportPublicKey   string `json:"transport_public_key"`
	TransportFingerprint string `json:"transport_fingerprint"`
	Proof                string `json:"proof"`
}

// RotateSelf atomically swaps the authenticated member to a replacement identity.
func (c *Client) RotateSelf(teamID string, req RotateSelfRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/teams/%s/rotate-self", teamID)
	resp, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return readError(resp)
	}
	return nil
}

// TransferOwnership promotes another human member and demotes the caller to member.
func (c *Client) TransferOwnership(teamID, fingerprint string) error {
	body, err := json.Marshal(map[string]string{"fingerprint": fingerprint})
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/teams/%s/transfer-ownership", teamID)
	resp, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return readError(resp)
	}
	return nil
}

// ListTeamMembers returns the current relay-side project member list.
func (c *Client) ListTeamMembers(teamID string) ([]TeamMember, error) {
	path := fmt.Sprintf("/teams/%s/members", teamID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var result struct {
		Members []TeamMember `json:"members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Members, nil
}

// GetTeamMetrics returns operator-facing relay metrics for a project team.
func (c *Client) GetTeamMetrics(teamID string) (*TeamMetrics, error) {
	path := fmt.Sprintf("/teams/%s/metrics", teamID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var result TeamMetrics
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTeamInvites returns relay-side invite lifecycle data for project administrators.
func (c *Client) ListTeamInvites(teamID string) ([]TeamInvite, error) {
	path := fmt.Sprintf("/teams/%s/invites", teamID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var result struct {
		Invites []TeamInvite `json:"invites"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Invites, nil
}

// RevokeInvite retires a relay invite before it is consumed.
func (c *Client) RevokeInvite(teamID, tokenHash, reason string) error {
	body, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/teams/%s/invites/%s/revoke", teamID, url.PathEscape(tokenHash))
	resp, err := c.doRequest("POST", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return readError(resp)
	}
	return nil
}

// ListTeamAudit returns relay-side administrative history for the current project.
func (c *Client) ListTeamAudit(teamID string, limit int) ([]TeamAuditEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	path := fmt.Sprintf("/teams/%s/audit?limit=%d", teamID, limit)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var result struct {
		Events []TeamAuditEvent `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Events, nil
}

func readError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
		return fmt.Errorf("relay error: %s - %s", errResp.Error, errResp.Message)
	}
	return fmt.Errorf("relay returned HTTP %d: %s", resp.StatusCode, string(body))
}

func retryDelay(attempt int) time.Duration {
	base := time.Duration(attempt) * 300 * time.Millisecond
	jitter := time.Duration(0)
	if n, err := crand.Int(crand.Reader, big.NewInt(150)); err == nil {
		jitter = time.Duration(n.Int64()) * time.Millisecond
	}
	return base + jitter
}

func signingPath(path string) string {
	parsed, err := url.Parse(path)
	if err != nil || parsed.Path == "" {
		return path
	}
	return parsed.Path
}

// HashToken computes the SHA-256 hash of a mnemonic token for relay storage.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

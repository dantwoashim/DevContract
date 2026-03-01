// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package relay

// This file re-exports blob operations from the Client.
// The actual implementations are in client.go.

// BlobFlow encapsulates blob upload/download lifecycle.
type BlobFlow struct {
	client *Client
}

// NewBlobFlow creates a blob flow helper.
func NewBlobFlow(client *Client) *BlobFlow {
	return &BlobFlow{client: client}
}

// Upload encrypts and uploads a blob to the relay.
func (f *BlobFlow) Upload(teamID, blobID string, data []byte, senderFP, recipientFP, ephemeralKey, filename, senderSig string) error {
	return f.client.UploadBlob(teamID, blobID, data, senderFP, recipientFP, ephemeralKey, filename, senderSig)
}

// ListPending returns pending blobs for the current identity.
func (f *BlobFlow) ListPending(teamID string) ([]BlobInfo, error) {
	return f.client.ListPending(teamID)
}

// Download retrieves an encrypted blob from the relay.
func (f *BlobFlow) Download(teamID, blobID string) ([]byte, string, string, error) {
	return f.client.DownloadBlob(teamID, blobID)
}

// Delete removes a blob after download.
func (f *BlobFlow) Delete(teamID, blobID string) error {
	return f.client.DeleteBlob(teamID, blobID)
}

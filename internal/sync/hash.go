// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package sync

import "crypto/sha256"

// sha256Digest computes SHA-256 of input.
func sha256Digest(data []byte) [32]byte {
	return sha256.Sum256(data)
}

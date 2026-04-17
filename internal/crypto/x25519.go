// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"golang.org/x/crypto/curve25519"
)

// curveX25519 wraps curve25519.X25519 for internal use.
func curveX25519(scalar, point []byte) ([]byte, error) {
	return curve25519.X25519(scalar, point)
}

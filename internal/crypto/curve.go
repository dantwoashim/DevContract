// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package crypto

import (
	"crypto/sha512"
	"math/big"
)

// edwardsToMontgomery converts an Ed25519 public key (Edwards form)
// to an X25519 public key (Montgomery form).
//
// The conversion formula is: u = (1 + y) / (1 - y) mod p
// where y is the Edwards y-coordinate and p = 2^255 - 19.
func edwardsToMontgomery(montgomery, edwards *[32]byte) bool {
	// p = 2^255 - 19
	p := new(big.Int).Sub(
		new(big.Int).Exp(big.NewInt(2), big.NewInt(255), nil),
		big.NewInt(19),
	)

	// Decode the Edwards y-coordinate (little-endian, clear top bit)
	var yBytes [32]byte
	copy(yBytes[:], edwards[:])
	yBytes[31] &= 0x7f // Clear the sign bit

	y := new(big.Int).SetBytes(reverseBytes(yBytes[:]))

	// Compute: u = (1 + y) * inverse(1 - y) mod p
	one := big.NewInt(1)

	numerator := new(big.Int).Add(one, y)
	numerator.Mod(numerator, p)

	denominator := new(big.Int).Sub(one, y)
	denominator.Mod(denominator, p)

	// Check denominator is not zero
	if denominator.Sign() == 0 {
		return false
	}

	// Modular inverse of denominator
	denominatorInv := new(big.Int).ModInverse(denominator, p)
	if denominatorInv == nil {
		return false
	}

	u := new(big.Int).Mul(numerator, denominatorInv)
	u.Mod(u, p)

	// Encode u as little-endian 32 bytes
	uBytes := u.Bytes()
	reversed := reverseBytes(uBytes)

	// Pad or truncate to 32 bytes
	copy(montgomery[:], make([]byte, 32))
	copy(montgomery[:], reversed)

	return true
}

// reverseBytes returns a new slice with bytes reversed (for big-endian ↔ little-endian).
func reverseBytes(b []byte) []byte {
	result := make([]byte, len(b))
	for i, v := range b {
		result[len(b)-1-i] = v
	}
	return result
}

// sha512Sum computes SHA-512 of input.
func sha512Sum(input []byte) [64]byte {
	return sha512.Sum512(input)
}

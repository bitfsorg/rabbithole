// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package contract

import "errors"

var (
	// ErrInvalidPubKey indicates the public key is not a valid 33-byte
	// compressed secp256k1 key.
	ErrInvalidPubKey = errors.New("contract: invalid compressed public key")

	// ErrZeroPeriods indicates NumPeriods is zero.
	ErrZeroPeriods = errors.New("contract: number of periods must be positive")

	// ErrZeroPayment indicates TokenPerPeriod is zero.
	ErrZeroPayment = errors.New("contract: payment per period must be positive")

	// ErrInvalidChunkCount indicates NumChunks is zero or inconsistent.
	ErrInvalidChunkCount = errors.New("contract: invalid chunk count")

	// ErrPeriodTooShort indicates BlocksPerPeriod is below the minimum.
	ErrPeriodTooShort = errors.New("contract: period too short (minimum 6 blocks)")

	// ErrChunkIndexOutOfRange indicates a computed chunk index exceeds NumChunks.
	ErrChunkIndexOutOfRange = errors.New("contract: chunk index out of range")

	// ErrMerkleRootMismatch indicates the computed Merkle root does not
	// match the expected value.
	ErrMerkleRootMismatch = errors.New("contract: merkle root mismatch")

	// ErrScriptTooLarge indicates the generated script exceeds acceptable size.
	ErrScriptTooLarge = errors.New("contract: script too large")

	// ErrInsufficientChunks indicates not enough chunks were provided for
	// proof pre-computation.
	ErrInsufficientChunks = errors.New("contract: insufficient chunks for pre-computation")
)

// MinBlocksPerPeriod is the minimum number of blocks per challenge period.
// This gives the Metanet Node at least ~1 hour to submit a proof.
const MinBlocksPerPeriod = 6

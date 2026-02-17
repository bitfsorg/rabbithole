// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package chain

import "errors"

var (
	// ErrInvalidPrevHash indicates the PrevHash does not reference a known block.
	ErrInvalidPrevHash = errors.New("chain: invalid previous block hash")

	// ErrTimestampTooOld indicates the block timestamp is before the median
	// of the last 11 blocks.
	ErrTimestampTooOld = errors.New("chain: block timestamp too old")

	// ErrTimestampTooNew indicates the block timestamp is more than 2 hours
	// in the future.
	ErrTimestampTooNew = errors.New("chain: block timestamp too far in the future")

	// ErrInvalidDifficulty indicates the Bits field does not match the
	// expected difficulty for this block height.
	ErrInvalidDifficulty = errors.New("chain: invalid difficulty bits")

	// ErrInsufficientPoW indicates the block hash does not meet the
	// difficulty target.
	ErrInsufficientPoW = errors.New("chain: insufficient proof of work")

	// ErrBlockTooLarge indicates the serialized block exceeds MaxBlockSize.
	ErrBlockTooLarge = errors.New("chain: block exceeds maximum size")

	// ErrInvalidMerkleRoot indicates the MerkleRoot field does not match
	// the computed Merkle tree of the block's transactions.
	ErrInvalidMerkleRoot = errors.New("chain: invalid merkle root")

	// ErrDuplicateTx indicates the block contains duplicate transaction IDs.
	ErrDuplicateTx = errors.New("chain: duplicate transaction in block")

	// ErrNoTransactions indicates the block contains no transactions.
	ErrNoTransactions = errors.New("chain: block contains no transactions")
)

// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package mining

import "errors"

var (
	// ErrNoAuxPoWCommitment indicates the coinbase transaction does not
	// contain the MNMP merged mining marker.
	ErrNoAuxPoWCommitment = errors.New("mining: no AuxPoW commitment (MNMP) in coinbase")

	// ErrInvalidCoinbaseBranch indicates the Merkle branch does not prove
	// the coinbase is included in the parent block.
	ErrInvalidCoinbaseBranch = errors.New("mining: invalid coinbase Merkle branch")

	// ErrAuxPoWHashMismatch indicates the block hash committed in the coinbase
	// does not match the actual Metanet Chain block header hash.
	ErrAuxPoWHashMismatch = errors.New("mining: AuxPoW committed hash does not match block header")

	// ErrInsufficientAuxPoW indicates the parent chain header hash does not
	// meet the Metanet Chain difficulty target.
	ErrInsufficientAuxPoW = errors.New("mining: parent header does not meet difficulty target")

	// ErrInvalidParentHeader indicates the parent chain header is malformed.
	ErrInvalidParentHeader = errors.New("mining: invalid parent chain header")

	// ErrAnchorHeightMismatch indicates the anchor block range is not contiguous.
	ErrAnchorHeightMismatch = errors.New("mining: anchor block range is not contiguous")

	// ErrAnchorChainBroken indicates the anchor PrevAnchorTx does not reference
	// the previous anchor transaction.
	ErrAnchorChainBroken = errors.New("mining: anchor chain linkage is broken")

	// ErrDifficultyOverflow indicates the computed difficulty exceeds the
	// representable range.
	ErrDifficultyOverflow = errors.New("mining: difficulty target overflow")

	// ErrInvalidAnchorMagic indicates the anchor data does not start with
	// the expected MNTA magic bytes.
	ErrInvalidAnchorMagic = errors.New("mining: invalid anchor magic bytes")

	// ErrAnchorDataTooShort indicates the anchor data is shorter than the
	// minimum required length.
	ErrAnchorDataTooShort = errors.New("mining: anchor data too short")

	// ErrNoBlockHeaders indicates an empty block header list was provided.
	ErrNoBlockHeaders = errors.New("mining: no block headers provided")
)

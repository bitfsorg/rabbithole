// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package contract

import (
	"crypto/sha256"
)

// StorageDeal represents the parameters of a storage contract between
// an Owner and a Metanet Node on the Metanet Chain.
type StorageDeal struct {
	OwnerPubKey     []byte     // Owner's compressed public key (33 bytes).
	NodePubKey      []byte     // Metanet Node's compressed public key (33 bytes).
	NumPeriods      uint32     // Number of challenge periods (N).
	TokenPerPeriod  uint64     // MNT satoshis locked per period.
	BlocksPerPeriod uint32     // Number of blocks per challenge period.
	StartBlock      uint32     // Block height when contract begins.
	MerkleRoot      [32]byte   // Merkle root of the node-specific encrypted data.
	NumChunks       uint32     // Total number of data chunks.
	ExpectedHashes  [][32]byte // Pre-computed expected_hash_k for each period k.
}

// StorageDealUTXO represents a single period's UTXO in the contract.
type StorageDealUTXO struct {
	Period       uint32   // Period index.
	Value        uint64   // Locked MNT satoshis.
	ExpireBlock  uint32   // CLTV expiry block height.
	ExpectedHash [32]byte // Expected SHA256(proof_data).
	Script       []byte   // Locking script.
}

// ValidateDealParams checks that storage deal parameters are within acceptable bounds.
func ValidateDealParams(deal *StorageDeal) error {
	if err := validatePubKey(deal.OwnerPubKey); err != nil {
		return err
	}
	if err := validatePubKey(deal.NodePubKey); err != nil {
		return err
	}
	if deal.NumPeriods == 0 {
		return ErrZeroPeriods
	}
	if deal.TokenPerPeriod == 0 {
		return ErrZeroPayment
	}
	if deal.NumChunks == 0 {
		return ErrInvalidChunkCount
	}
	if deal.BlocksPerPeriod < MinBlocksPerPeriod {
		return ErrPeriodTooShort
	}
	return nil
}

// BuildStorageDealUTXOs generates the N UTXO outputs for a storage deal,
// each with its own locking script containing the expected proof hash
// and CLTV expiry.
func BuildStorageDealUTXOs(deal *StorageDeal) ([]*StorageDealUTXO, error) {
	if err := ValidateDealParams(deal); err != nil {
		return nil, err
	}

	if uint32(len(deal.ExpectedHashes)) != deal.NumPeriods {
		return nil, ErrInvalidChunkCount
	}

	utxos := make([]*StorageDealUTXO, deal.NumPeriods)
	for k := uint32(0); k < deal.NumPeriods; k++ {
		expireBlock := deal.StartBlock + (k+1)*deal.BlocksPerPeriod

		script, err := BuildDealScript(
			deal.NodePubKey,
			deal.OwnerPubKey,
			deal.ExpectedHashes[k],
			expireBlock,
		)
		if err != nil {
			return nil, err
		}

		utxos[k] = &StorageDealUTXO{
			Period:       k,
			Value:        deal.TokenPerPeriod,
			ExpireBlock:  expireBlock,
			ExpectedHash: deal.ExpectedHashes[k],
			Script:       script,
		}
	}

	return utxos, nil
}

// PrecomputeExpectedHashes pre-computes all N expected proof hashes for a
// storage deal. This requires the actual data chunks and the Merkle tree.
//
// For each period k:
//  1. Compute the deterministic challenge: challenge_k = ComputeChallenge(txid, k, numChunks)
//  2. Get the challenged chunk data
//  3. Generate the Merkle proof for the challenged chunk
//  4. expected_hash_k = SHA256(merkle_proof_bytes || chunk_data)
func PrecomputeExpectedHashes(
	contractTxID [32]byte,
	numPeriods uint32,
	chunks [][]byte,
	merkleLeaves [][32]byte,
) ([][32]byte, error) {
	numChunks := uint32(len(chunks))
	if numChunks == 0 {
		return nil, ErrInvalidChunkCount
	}

	// Build a simple Merkle tree for proof generation.
	tree := buildSimpleMerkleTree(merkleLeaves)

	expectedHashes := make([][32]byte, numPeriods)
	for k := uint32(0); k < numPeriods; k++ {
		challenge := ComputeChallenge(contractTxID, k, numChunks)

		if challenge.ChunkIndex >= numChunks {
			return nil, ErrChunkIndexOutOfRange
		}

		// Generate Merkle proof for the challenged chunk.
		proofBytes := generateMerkleProofBytes(tree, merkleLeaves, challenge.ChunkIndex)

		// Compute expected hash.
		expectedHashes[k] = ComputeExpectedHash(chunks[challenge.ChunkIndex], proofBytes)
	}

	return expectedHashes, nil
}

// buildSimpleMerkleTree builds a flat array representation of a Merkle tree.
// Returns all levels from leaves to root.
func buildSimpleMerkleTree(leaves [][32]byte) [][][32]byte {
	if len(leaves) == 0 {
		return nil
	}

	levels := [][][32]byte{leaves}
	current := leaves

	for len(current) > 1 {
		// Duplicate last element if odd.
		if len(current)%2 != 0 {
			current = append(current, current[len(current)-1])
		}

		var next [][32]byte
		for i := 0; i < len(current); i += 2 {
			var combined [64]byte
			copy(combined[0:32], current[i][:])
			copy(combined[32:64], current[i+1][:])
			hash := sha256.Sum256(combined[:])
			next = append(next, hash)
		}
		levels = append(levels, next)
		current = next
	}

	return levels
}

// generateMerkleProofBytes generates a serialized Merkle proof for a leaf.
// Returns the concatenated sibling hashes.
func generateMerkleProofBytes(tree [][][32]byte, leaves [][32]byte, index uint32) []byte {
	if len(tree) == 0 {
		return nil
	}

	var proof []byte
	idx := index

	for level := 0; level < len(tree)-1; level++ {
		currentLevel := tree[level]

		// Handle odd-length levels.
		if len(currentLevel)%2 != 0 {
			expanded := make([][32]byte, len(currentLevel)+1)
			copy(expanded, currentLevel)
			expanded[len(currentLevel)] = currentLevel[len(currentLevel)-1]
			currentLevel = expanded
		}

		var siblingIdx uint32
		if idx%2 == 0 {
			siblingIdx = idx + 1
		} else {
			siblingIdx = idx - 1
		}

		if siblingIdx < uint32(len(currentLevel)) {
			proof = append(proof, currentLevel[siblingIdx][:]...)
		}
		idx /= 2
	}

	return proof
}

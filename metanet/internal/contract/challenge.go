// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package contract

import (
	"crypto/sha256"
	"encoding/binary"
)

// Challenge represents a deterministic challenge for a specific period
// of a storage contract.
type Challenge struct {
	Period     uint32   // Challenge period index (0-based).
	Seed       [32]byte // SHA256(contract_txid || uint32_le(k)).
	ChunkIndex uint32   // Which chunk to prove: uint32_le(seed[0:4]) % num_chunks.
}

// ComputeChallenge deterministically computes the challenge for period k
// given a contract TxID and the total number of chunks.
//
// Algorithm:
//
//	challenge_seed = SHA256(contract_txid || uint32_le(k))
//	chunk_index    = uint32_le(seed[0:4]) % num_chunks
//
// The entropy comes from contract_txid, which is unpredictable before
// the transaction is created.
func ComputeChallenge(contractTxID [32]byte, period uint32, numChunks uint32) *Challenge {
	// Build the preimage: contract_txid (32 bytes) || period (4 bytes LE).
	var preimage [36]byte
	copy(preimage[0:32], contractTxID[:])
	binary.LittleEndian.PutUint32(preimage[32:36], period)

	// Compute the challenge seed.
	seed := sha256.Sum256(preimage[:])

	// Derive the chunk index from the first 4 bytes of the seed.
	rawIndex := binary.LittleEndian.Uint32(seed[0:4])
	chunkIndex := rawIndex % numChunks

	return &Challenge{
		Period:     period,
		Seed:       seed,
		ChunkIndex: chunkIndex,
	}
}

// ComputeExpectedHash computes the expected proof hash for a given challenge.
//
// expected_hash = SHA256(merkle_proof_bytes || chunk_data)
//
// This is the value stored in the contract UTXO for on-chain verification.
// The Metanet Node must produce proof_data such that SHA256(proof_data) equals
// this expected hash.
func ComputeExpectedHash(chunkData []byte, merkleProofBytes []byte) [32]byte {
	// Concatenate: merkle_proof_bytes || chunk_data
	combined := make([]byte, len(merkleProofBytes)+len(chunkData))
	copy(combined, merkleProofBytes)
	copy(combined[len(merkleProofBytes):], chunkData)
	return sha256.Sum256(combined)
}

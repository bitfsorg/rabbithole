// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package proof

import (
	"crypto/sha256"

	"github.com/tongxiaofeng/metanet/internal/contract"
)

// ProofData is what the Metanet Node submits to claim a period's payment.
type ProofData struct {
	ChunkIndex  uint32       // Index of the challenged chunk.
	ChunkData   []byte       // Raw chunk data.
	MerkleProof *MerkleProof // Proof of chunk inclusion.
}

// ChallengeResponse bundles a challenge with its corresponding proof
// for verification.
type ChallengeResponse struct {
	ContractTxID [32]byte
	Period       uint32
	NumChunks    uint32
	Proof        *ProofData
}

// ComputeProofHash computes the hash that must match expected_hash_k
// in the storage contract.
//
// proof_hash = SHA256(serialize(merkle_proof) || chunk_data)
func ComputeProofHash(proof *ProofData) [32]byte {
	serialized := SerializeProofData(proof)
	return sha256.Sum256(serialized)
}

// VerifyStorageProof performs full verification of a storage proof:
//  1. Compute challenge for the given period.
//  2. Verify chunk index matches challenge.
//  3. Verify Merkle proof against expected root.
//  4. Verify proof hash matches expected hash.
func VerifyStorageProof(
	contractTxID [32]byte,
	period uint32,
	numChunks uint32,
	expectedHash [32]byte,
	merkleRoot [32]byte,
	proof *ProofData,
) error {
	// Step 1: Compute the deterministic challenge.
	challenge := contract.ComputeChallenge(contractTxID, period, numChunks)

	// Step 2: Verify chunk index matches challenge.
	if proof.ChunkIndex != challenge.ChunkIndex {
		return ErrChallengeMismatch
	}

	// Step 3: Verify Merkle proof against the expected root.
	proofWithRoot := &MerkleProof{
		ChunkIndex: proof.MerkleProof.ChunkIndex,
		Siblings:   proof.MerkleProof.Siblings,
		Root:       merkleRoot,
	}
	if !VerifyMerkleProof(proof.ChunkData, proofWithRoot) {
		return ErrMerkleProofInvalid
	}

	// Step 4: Verify proof hash matches expected hash.
	computedHash := ComputeProofHash(proof)
	if computedHash != expectedHash {
		return ErrProofHashMismatch
	}

	return nil
}

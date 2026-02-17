// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package proof

import "errors"

var (
	// ErrInvalidPrivateKey indicates the owner private key is not a valid
	// 32-byte scalar.
	ErrInvalidPrivateKey = errors.New("proof: invalid private key (must be 32 bytes)")

	// ErrInvalidPublicKey indicates the node public key is not a valid
	// 33-byte compressed public key.
	ErrInvalidPublicKey = errors.New("proof: invalid public key (must be 33 bytes, prefix 0x02 or 0x03)")

	// ErrEmptyData indicates the input data is empty.
	ErrEmptyData = errors.New("proof: input data is empty")

	// ErrChunkIndexOutOfRange indicates the chunk index exceeds the number
	// of leaves in the Merkle tree.
	ErrChunkIndexOutOfRange = errors.New("proof: chunk index out of range")

	// ErrMerkleProofInvalid indicates the Merkle proof does not verify
	// against the expected root.
	ErrMerkleProofInvalid = errors.New("proof: merkle proof verification failed")

	// ErrProofHashMismatch indicates the computed proof hash does not match
	// the expected hash from the contract.
	ErrProofHashMismatch = errors.New("proof: proof hash does not match expected hash")

	// ErrChallengeMismatch indicates the submitted chunk index does not match
	// the deterministically computed challenge.
	ErrChallengeMismatch = errors.New("proof: chunk index does not match challenge")

	// ErrEncryptionFailed indicates AES-256-GCM encryption failed.
	ErrEncryptionFailed = errors.New("proof: encryption failed")

	// ErrDecryptionFailed indicates AES-256-GCM decryption failed.
	ErrDecryptionFailed = errors.New("proof: decryption failed")

	// ErrDeserialize indicates proof data deserialization failed.
	ErrDeserialize = errors.New("proof: deserialization failed")
)

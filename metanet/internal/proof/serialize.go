// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package proof

import (
	"encoding/binary"
)

// SerializeProofData serializes a ProofData for on-chain submission.
//
// Format:
//
//	chunk_index  (4 bytes LE)
//	chunk_size   (4 bytes LE)
//	chunk_data   (chunk_size bytes)
//	num_siblings (4 bytes LE)
//	siblings     (num_siblings * 32 bytes)
func SerializeProofData(proof *ProofData) []byte {
	chunkSize := uint32(len(proof.ChunkData))
	numSiblings := uint32(0)
	if proof.MerkleProof != nil {
		numSiblings = uint32(len(proof.MerkleProof.Siblings))
	}

	// Calculate total size.
	size := 4 + 4 + int(chunkSize) + 4 + int(numSiblings)*32
	buf := make([]byte, size)

	offset := 0

	// chunk_index (4 bytes LE).
	binary.LittleEndian.PutUint32(buf[offset:offset+4], proof.ChunkIndex)
	offset += 4

	// chunk_size (4 bytes LE).
	binary.LittleEndian.PutUint32(buf[offset:offset+4], chunkSize)
	offset += 4

	// chunk_data.
	copy(buf[offset:offset+int(chunkSize)], proof.ChunkData)
	offset += int(chunkSize)

	// num_siblings (4 bytes LE).
	binary.LittleEndian.PutUint32(buf[offset:offset+4], numSiblings)
	offset += 4

	// siblings.
	if proof.MerkleProof != nil {
		for _, sibling := range proof.MerkleProof.Siblings {
			copy(buf[offset:offset+32], sibling[:])
			offset += 32
		}
	}

	return buf
}

// DeserializeProofData deserializes proof data from on-chain format.
func DeserializeProofData(data []byte) (*ProofData, error) {
	// Minimum size: chunk_index(4) + chunk_size(4) + num_siblings(4) = 12
	if len(data) < 12 {
		return nil, ErrDeserialize
	}

	offset := 0

	// chunk_index.
	chunkIndex := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// chunk_size.
	chunkSize := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// chunk_data.
	if offset+int(chunkSize) > len(data) {
		return nil, ErrDeserialize
	}
	chunkData := make([]byte, chunkSize)
	copy(chunkData, data[offset:offset+int(chunkSize)])
	offset += int(chunkSize)

	// num_siblings.
	if offset+4 > len(data) {
		return nil, ErrDeserialize
	}
	numSiblings := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// siblings.
	if offset+int(numSiblings)*32 > len(data) {
		return nil, ErrDeserialize
	}
	siblings := make([][32]byte, numSiblings)
	for i := uint32(0); i < numSiblings; i++ {
		copy(siblings[i][:], data[offset:offset+32])
		offset += 32
	}

	return &ProofData{
		ChunkIndex: chunkIndex,
		ChunkData:  chunkData,
		MerkleProof: &MerkleProof{
			ChunkIndex: chunkIndex,
			Siblings:   siblings,
		},
	}, nil
}

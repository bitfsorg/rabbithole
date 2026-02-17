// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package proof

import (
	"crypto/sha256"
)

// MerkleTree represents a SHA256-based binary Merkle tree built
// over data chunks.
type MerkleTree struct {
	Leaves    [][32]byte // SHA256 hash of each chunk.
	Nodes     [][32]byte // All tree nodes (level-order, leaves first).
	NumLeaves uint32     // Number of leaf nodes.
	Root      [32]byte   // Merkle root.
}

// MerkleProof contains the sibling hashes needed to prove a leaf
// is included in the tree.
type MerkleProof struct {
	ChunkIndex uint32     // Index of the proven chunk.
	Siblings   [][32]byte // Sibling hashes from leaf to root.
	Root       [32]byte   // Expected Merkle root.
}

// SplitIntoChunks splits data into fixed-size chunks.
// The last chunk may be smaller than chunkSize.
func SplitIntoChunks(data []byte, chunkSize uint32) [][]byte {
	if len(data) == 0 || chunkSize == 0 {
		return nil
	}

	var chunks [][]byte
	for i := 0; i < len(data); i += int(chunkSize) {
		end := i + int(chunkSize)
		if end > len(data) {
			end = len(data)
		}
		chunk := make([]byte, end-i)
		copy(chunk, data[i:end])
		chunks = append(chunks, chunk)
	}
	return chunks
}

// BuildMerkleTree constructs a SHA256-based Merkle tree from data chunks.
//
// Leaf hashes: SHA256(chunk_i)
// Internal nodes: SHA256(left || right)
// If odd number of leaves, the last leaf is duplicated.
func BuildMerkleTree(chunks [][]byte) (*MerkleTree, error) {
	if len(chunks) == 0 {
		return nil, ErrEmptyData
	}

	// Compute leaf hashes.
	leaves := make([][32]byte, len(chunks))
	for i, chunk := range chunks {
		leaves[i] = sha256.Sum256(chunk)
	}

	// Build tree level by level.
	tree := &MerkleTree{
		Leaves:    leaves,
		NumLeaves: uint32(len(leaves)),
	}

	// Collect all nodes in level-order.
	tree.Nodes = make([][32]byte, len(leaves))
	copy(tree.Nodes, leaves)

	current := make([][32]byte, len(leaves))
	copy(current, leaves)

	for len(current) > 1 {
		// Duplicate last if odd.
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
		tree.Nodes = append(tree.Nodes, next...)
		current = next
	}

	tree.Root = current[0]
	return tree, nil
}

// GenerateMerkleProof generates a Merkle inclusion proof for a specific chunk.
func GenerateMerkleProof(tree *MerkleTree, chunkIndex uint32) (*MerkleProof, error) {
	if chunkIndex >= tree.NumLeaves {
		return nil, ErrChunkIndexOutOfRange
	}

	// Rebuild levels for proof generation.
	levels := buildLevels(tree.Leaves)

	var siblings [][32]byte
	idx := chunkIndex

	for level := 0; level < len(levels)-1; level++ {
		currentLevel := levels[level]

		// Handle odd-length levels by duplicating last element.
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
			siblings = append(siblings, currentLevel[siblingIdx])
		}
		idx /= 2
	}

	return &MerkleProof{
		ChunkIndex: chunkIndex,
		Siblings:   siblings,
		Root:       tree.Root,
	}, nil
}

// VerifyMerkleProof verifies that a chunk is included in a Merkle tree
// with the given root.
func VerifyMerkleProof(chunkData []byte, proof *MerkleProof) bool {
	if proof == nil || len(chunkData) == 0 {
		return false
	}

	// Compute leaf hash.
	current := sha256.Sum256(chunkData)
	idx := proof.ChunkIndex

	for _, sibling := range proof.Siblings {
		var combined [64]byte
		if idx%2 == 0 {
			// Current is left child.
			copy(combined[0:32], current[:])
			copy(combined[32:64], sibling[:])
		} else {
			// Current is right child.
			copy(combined[0:32], sibling[:])
			copy(combined[32:64], current[:])
		}
		current = sha256.Sum256(combined[:])
		idx /= 2
	}

	return current == proof.Root
}

// buildLevels reconstructs all levels of a Merkle tree from leaves.
func buildLevels(leaves [][32]byte) [][][32]byte {
	if len(leaves) == 0 {
		return nil
	}

	levels := [][][32]byte{leaves}
	current := make([][32]byte, len(leaves))
	copy(current, leaves)

	for len(current) > 1 {
		// Duplicate last if odd.
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

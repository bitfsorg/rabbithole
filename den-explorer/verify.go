package main

import (
	"encoding/hex"
	"fmt"

	"github.com/bitfsorg/libbitfs-go/metanet"
	"github.com/bitfsorg/libbitfs-go/network"
	"github.com/bitfsorg/libbitfs-go/spv"
)

// SPVVerification holds the result of SPV proof verification.
type SPVVerification struct {
	TxID         string
	BlockHash    string
	Index        int
	Branches     []SPVBranch
	ComputedRoot string
	ExpectedRoot string
	Valid        bool
	Error        string
}

// SPVBranch holds a single branch in the Merkle proof path for display.
type SPVBranch struct {
	Level int
	Hash  string
	Side  string // "left" or "right"
}

// VerifySPVProof verifies a Merkle inclusion proof and returns display data.
func VerifySPVProof(proof *network.MerkleProof, headerBytes []byte) *SPVVerification {
	result := &SPVVerification{
		TxID:      proof.TxID,
		BlockHash: proof.BlockHash,
		Index:     proof.Index,
	}

	// Build branch display
	index := uint32(proof.Index)
	for i, branch := range proof.Branches {
		side := "right"
		if index%2 == 1 {
			side = "left"
		}
		result.Branches = append(result.Branches, SPVBranch{
			Level: i,
			Hash:  hex.EncodeToString(branch),
			Side:  side,
		})
		index /= 2
	}

	// Compute Merkle root from proof
	txidBytes, err := hex.DecodeString(proof.TxID)
	if err != nil {
		result.Error = fmt.Sprintf("invalid txid: %v", err)
		return result
	}
	if len(txidBytes) != 32 {
		result.Error = "invalid txid: expected 32 bytes"
		return result
	}
	// Reverse for internal byte order
	txidInternal := make([]byte, 32)
	for i, b := range txidBytes {
		txidInternal[31-i] = b
	}

	computedRoot := spv.ComputeMerkleRoot(txidInternal, uint32(proof.Index), proof.Branches)
	result.ComputedRoot = hex.EncodeToString(computedRoot)

	// Extract expected root from header
	if len(headerBytes) >= 80 {
		header, err := spv.DeserializeHeader(headerBytes)
		if err == nil {
			result.ExpectedRoot = hex.EncodeToString(header.MerkleRoot)
			if result.ComputedRoot == result.ExpectedRoot {
				result.Valid = true
			}
		}
	}

	return result
}

// DirMerkleVerification holds directory Merkle root verification results.
type DirMerkleVerification struct {
	Children     []metanet.ChildEntry
	ComputedRoot string
	StoredRoot   string
	Valid        bool
}

// VerifyDirMerkleRoot verifies a directory's MerkleRoot against its children.
func VerifyDirMerkleRoot(node *metanet.Node) *DirMerkleVerification {
	if node == nil || !node.IsDir() {
		return nil
	}

	result := &DirMerkleVerification{
		Children: node.Children,
	}

	if len(node.MerkleRoot) > 0 {
		result.StoredRoot = hex.EncodeToString(node.MerkleRoot)
	}

	computed := metanet.ComputeDirectoryMerkleRoot(node.Children)
	if computed != nil {
		result.ComputedRoot = hex.EncodeToString(computed)
	}

	if result.StoredRoot != "" && result.ComputedRoot != "" {
		result.Valid = result.StoredRoot == result.ComputedRoot
	}

	return result
}

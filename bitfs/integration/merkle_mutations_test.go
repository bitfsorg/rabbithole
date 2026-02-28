//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/metanet"
)

// makeChildEntry creates a deterministic ChildEntry for testing.
// PubKey is a fake 33-byte compressed key: [0x02, byte(index), 0, 0, ..., 0].
func makeChildEntry(name string, index uint32) metanet.ChildEntry {
	pubKey := make([]byte, 33)
	pubKey[0] = 0x02
	pubKey[1] = byte(index & 0xFF)
	return metanet.ChildEntry{
		Index:    index,
		Name:     name,
		Type:     metanet.NodeTypeFile,
		PubKey:   pubKey,
		Hardened: true,
	}
}

// TestMerkleRootChangesOnAddChild verifies that the directory Merkle root
// changes when a new child is added.
func TestMerkleRootChangesOnAddChild(t *testing.T) {
	child1 := makeChildEntry("file-a.txt", 0)
	children1 := []metanet.ChildEntry{child1}
	root1 := metanet.ComputeDirectoryMerkleRoot(children1)
	require.NotNil(t, root1, "root for 1 child should not be nil")
	require.Len(t, root1, 32, "root should be 32 bytes")

	// Add a second child
	child2 := makeChildEntry("file-b.txt", 1)
	children2 := []metanet.ChildEntry{child1, child2}
	root2 := metanet.ComputeDirectoryMerkleRoot(children2)
	require.NotNil(t, root2, "root for 2 children should not be nil")
	require.Len(t, root2, 32, "root should be 32 bytes")

	assert.False(t, bytes.Equal(root1, root2),
		"Merkle root must change when a child is added")

	// Verify proofs still work for both children in the new tree
	proof0, err := metanet.BuildDirectoryMerkleProof(children2, 0)
	require.NoError(t, err)
	assert.True(t, metanet.VerifyChildMembership(&children2[0], proof0, 0, root2),
		"child 0 should verify in 2-child tree")

	proof1, err := metanet.BuildDirectoryMerkleProof(children2, 1)
	require.NoError(t, err)
	assert.True(t, metanet.VerifyChildMembership(&children2[1], proof1, 1, root2),
		"child 1 should verify in 2-child tree")
}

// TestMerkleRootChangesOnRemoveChild verifies that removing a child changes
// the root and invalidates old proofs.
func TestMerkleRootChangesOnRemoveChild(t *testing.T) {
	children3 := []metanet.ChildEntry{
		makeChildEntry("alpha.txt", 0),
		makeChildEntry("beta.txt", 1),
		makeChildEntry("gamma.txt", 2),
	}

	rootBefore := metanet.ComputeDirectoryMerkleRoot(children3)
	require.NotNil(t, rootBefore)

	// Build proof for child 0 in the 3-child tree
	proofChild0Before, err := metanet.BuildDirectoryMerkleProof(children3, 0)
	require.NoError(t, err)
	assert.True(t, metanet.VerifyChildMembership(&children3[0], proofChild0Before, 0, rootBefore),
		"child 0 proof should verify before removal")

	// Remove child 1 (beta.txt) — leaving alpha and gamma
	childrenAfter := []metanet.ChildEntry{children3[0], children3[2]}
	rootAfter := metanet.ComputeDirectoryMerkleRoot(childrenAfter)
	require.NotNil(t, rootAfter)

	// Root must have changed
	assert.False(t, bytes.Equal(rootBefore, rootAfter),
		"Merkle root must change when a child is removed")

	// Old proof for child 0 must be invalid against the NEW root
	assert.False(t, metanet.VerifyChildMembership(&children3[0], proofChild0Before, 0, rootAfter),
		"old proof for child 0 should NOT verify against new root")

	// New proof for child 0 in the reduced tree must be valid
	proofChild0After, err := metanet.BuildDirectoryMerkleProof(childrenAfter, 0)
	require.NoError(t, err)
	assert.True(t, metanet.VerifyChildMembership(&childrenAfter[0], proofChild0After, 0, rootAfter),
		"new proof for child 0 should verify against new root")
}

// TestMerkleRootChangesOnRename verifies that renaming a child changes
// the directory Merkle root (since the name is part of the leaf hash).
func TestMerkleRootChangesOnRename(t *testing.T) {
	children := []metanet.ChildEntry{
		makeChildEntry("old-name.txt", 0),
		makeChildEntry("other.txt", 1),
	}

	rootBefore := metanet.ComputeDirectoryMerkleRoot(children)
	require.NotNil(t, rootBefore)

	// Rename child 0
	childrenRenamed := []metanet.ChildEntry{
		makeChildEntry("new-name.txt", 0), // same index, different name
		children[1],
	}

	rootAfter := metanet.ComputeDirectoryMerkleRoot(childrenRenamed)
	require.NotNil(t, rootAfter)

	assert.False(t, bytes.Equal(rootBefore, rootAfter),
		"Merkle root must change when a child is renamed")

	// Verify the renamed entry works with a fresh proof
	proof, err := metanet.BuildDirectoryMerkleProof(childrenRenamed, 0)
	require.NoError(t, err)
	assert.True(t, metanet.VerifyChildMembership(&childrenRenamed[0], proof, 0, rootAfter),
		"renamed child should verify with new proof")
}

// TestMerkleProofLargeDirectory verifies Merkle proof properties for a
// directory with 1000 children: proof size is O(log2(n)) and verification works.
func TestMerkleProofLargeDirectory(t *testing.T) {
	const n = 1000
	children := make([]metanet.ChildEntry, n)
	for i := 0; i < n; i++ {
		children[i] = makeChildEntry(fmt.Sprintf("file-%04d", i), uint32(i))
	}

	root := metanet.ComputeDirectoryMerkleRoot(children)
	require.NotNil(t, root)
	require.Len(t, root, 32)

	// Build proof for child at index 500
	const targetIndex = 500
	proof, err := metanet.BuildDirectoryMerkleProof(children, targetIndex)
	require.NoError(t, err)
	require.NotNil(t, proof)

	// Proof size should be O(log2(n)) — for 1000, ceil(log2(1000)) = 10
	// Allow a small margin: expected range is 8..12
	expectedLog := math.Ceil(math.Log2(float64(n)))
	t.Logf("n=%d, proof length=%d, ceil(log2(n))=%.0f", n, len(proof), expectedLog)
	assert.InDelta(t, expectedLog, float64(len(proof)), 2.0,
		"proof size should be approximately ceil(log2(%d)) = %.0f, got %d", n, expectedLog, len(proof))

	// Verify the proof
	assert.True(t, metanet.VerifyChildMembership(&children[targetIndex], proof, targetIndex, root),
		"proof for child 500 should verify in 1000-child tree")

	// Cross-check: wrong entry should NOT verify with this proof
	wrongEntry := makeChildEntry("wrong-file", 999)
	assert.False(t, metanet.VerifyChildMembership(&wrongEntry, proof, targetIndex, root),
		"wrong entry should NOT verify with proof for child 500")
}

// TestMerkleEmptyAndSingleChild verifies edge cases:
// - Empty children → nil root
// - Single child → root equals the leaf hash
// - Single child proof is nil (leaf IS the root)
func TestMerkleEmptyAndSingleChild(t *testing.T) {
	// Empty directory: nil root
	t.Run("empty", func(t *testing.T) {
		root := metanet.ComputeDirectoryMerkleRoot(nil)
		assert.Nil(t, root, "empty children should produce nil root")

		root2 := metanet.ComputeDirectoryMerkleRoot([]metanet.ChildEntry{})
		assert.Nil(t, root2, "zero-length children should produce nil root")

		// Building proof for empty children should error
		_, err := metanet.BuildDirectoryMerkleProof(nil, 0)
		assert.Error(t, err, "proof for empty children should error")

		_, err = metanet.BuildDirectoryMerkleProof([]metanet.ChildEntry{}, 0)
		assert.Error(t, err, "proof for zero-length children should error")
	})

	// Single child: root == leaf hash, proof is nil
	t.Run("single_child", func(t *testing.T) {
		child := makeChildEntry("only-file.txt", 0)
		children := []metanet.ChildEntry{child}

		root := metanet.ComputeDirectoryMerkleRoot(children)
		require.NotNil(t, root)
		require.Len(t, root, 32)

		// Root should equal the leaf hash directly
		leafHash := metanet.ComputeChildLeafHash(&child)
		assert.True(t, bytes.Equal(root, leafHash),
			"single child root should equal its leaf hash")

		// Proof should be nil (no sibling hashes needed)
		proof, err := metanet.BuildDirectoryMerkleProof(children, 0)
		require.NoError(t, err)
		assert.Nil(t, proof, "single child proof should be nil")

		// Verification should still pass
		assert.True(t, metanet.VerifyChildMembership(&child, proof, 0, root),
			"single child should verify with nil proof")
	})
}

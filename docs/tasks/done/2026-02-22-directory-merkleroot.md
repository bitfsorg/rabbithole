# Directory MerkleRoot Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a MerkleRoot field to directory nodes that commits to all children via a Merkle tree, enabling compact membership proofs.

**Architecture:** New `metanet/merkle.go` file with Merkle computation and proof functions, reusing the same DoubleHash algorithm from `spv/merkle.go`. Add `MerkleRoot []byte` to Node struct, TLV tag `0x1A` for serialization, and auto-recompute on directory mutations (AddChild/RemoveChild/RenameChild).

**Tech Stack:** Go, `crypto/sha256`, existing `libbitfs/spv` DoubleHash, `testify` for assertions.

---

### Task 1: Create `merkle.go` with `ComputeChildLeafHash`

**Files:**
- Create: `libbitfs/metanet/merkle.go`
- Create: `libbitfs/metanet/merkle_test.go`

**Step 1: Write the failing test**

Create `libbitfs/metanet/merkle_test.go`:

```go
package metanet

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/libbitfs/spv"
)

func TestComputeChildLeafHash(t *testing.T) {
	entry := ChildEntry{
		Index:    0,
		Name:     "hello.txt",
		Type:     NodeTypeFile,
		PubKey:   makePubKey(0x01),
		Hardened: false,
	}

	hash := ComputeChildLeafHash(&entry)
	require.Len(t, hash, 32)

	// Must equal DoubleHash of the serialized ChildEntry
	serialized := serializeChildEntry(&entry)
	expected := spv.DoubleHash(serialized)
	assert.Equal(t, expected, hash)
}

func TestComputeChildLeafHash_Deterministic(t *testing.T) {
	entry := ChildEntry{
		Index:    5,
		Name:     "doc.pdf",
		Type:     NodeTypeDir,
		PubKey:   makePubKey(0x42),
		Hardened: true,
	}

	h1 := ComputeChildLeafHash(&entry)
	h2 := ComputeChildLeafHash(&entry)
	assert.Equal(t, h1, h2, "same entry must produce same hash")
}

func TestComputeChildLeafHash_DifferentEntries(t *testing.T) {
	e1 := ChildEntry{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)}
	e2 := ChildEntry{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)}

	h1 := ComputeChildLeafHash(&e1)
	h2 := ComputeChildLeafHash(&e2)
	assert.NotEqual(t, h1, h2, "different entries must produce different hashes")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run TestComputeChildLeafHash -v`
Expected: FAIL — `ComputeChildLeafHash` undefined

**Step 3: Write minimal implementation**

Create `libbitfs/metanet/merkle.go`:

```go
package metanet

import (
	"github.com/tongxiaofeng/libbitfs/spv"
)

// ComputeChildLeafHash computes the Merkle leaf hash for a single ChildEntry.
// The leaf hash is DoubleHash(serialize(entry)), reusing the existing
// ChildEntry binary format and Bitcoin's double-SHA256.
func ComputeChildLeafHash(entry *ChildEntry) []byte {
	serialized := serializeChildEntry(entry)
	return spv.DoubleHash(serialized)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run TestComputeChildLeafHash -v`
Expected: PASS (3 tests)

**Step 5: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add metanet/merkle.go metanet/merkle_test.go
git commit -m "feat(metanet): add ComputeChildLeafHash for directory Merkle tree"
```

---

### Task 2: Add `ComputeDirectoryMerkleRoot`

**Files:**
- Modify: `libbitfs/metanet/merkle.go`
- Modify: `libbitfs/metanet/merkle_test.go`

**Step 1: Write the failing tests**

Append to `libbitfs/metanet/merkle_test.go`:

```go
func TestComputeDirectoryMerkleRoot_Empty(t *testing.T) {
	root := ComputeDirectoryMerkleRoot(nil)
	assert.Nil(t, root, "empty children → nil")

	root2 := ComputeDirectoryMerkleRoot([]ChildEntry{})
	assert.Nil(t, root2, "zero-length children → nil")
}

func TestComputeDirectoryMerkleRoot_SingleChild(t *testing.T) {
	child := ChildEntry{
		Index:  0,
		Name:   "only.txt",
		Type:   NodeTypeFile,
		PubKey: makePubKey(0x01),
	}

	root := ComputeDirectoryMerkleRoot([]ChildEntry{child})
	require.Len(t, root, 32)

	// Single child: root = leaf hash
	leafHash := ComputeChildLeafHash(&child)
	assert.Equal(t, leafHash, root, "single child: root equals leaf hash")
}

func TestComputeDirectoryMerkleRoot_TwoChildren(t *testing.T) {
	c1 := ChildEntry{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)}
	c2 := ChildEntry{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)}

	root := ComputeDirectoryMerkleRoot([]ChildEntry{c1, c2})
	require.Len(t, root, 32)

	// Manual verification: root = DoubleHash(leaf1 || leaf2)
	leaf1 := ComputeChildLeafHash(&c1)
	leaf2 := ComputeChildLeafHash(&c2)
	combined := make([]byte, 64)
	copy(combined[:32], leaf1)
	copy(combined[32:], leaf2)
	expected := spv.DoubleHash(combined)
	assert.Equal(t, expected, root)
}

func TestComputeDirectoryMerkleRoot_ThreeChildren_OddPadding(t *testing.T) {
	c1 := ChildEntry{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)}
	c2 := ChildEntry{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)}
	c3 := ChildEntry{Index: 2, Name: "c.txt", Type: NodeTypeDir, PubKey: makePubKey(0x03)}

	root := ComputeDirectoryMerkleRoot([]ChildEntry{c1, c2, c3})
	require.Len(t, root, 32)

	// Manual: 3 leaves → pad to 4 by duplicating last
	leaf1 := ComputeChildLeafHash(&c1)
	leaf2 := ComputeChildLeafHash(&c2)
	leaf3 := ComputeChildLeafHash(&c3)

	// Level 1: hash(leaf1||leaf2), hash(leaf3||leaf3)
	combined12 := make([]byte, 64)
	copy(combined12[:32], leaf1)
	copy(combined12[32:], leaf2)
	h12 := spv.DoubleHash(combined12)

	combined33 := make([]byte, 64)
	copy(combined33[:32], leaf3)
	copy(combined33[32:], leaf3)
	h33 := spv.DoubleHash(combined33)

	// Level 2 (root): hash(h12||h33)
	combinedRoot := make([]byte, 64)
	copy(combinedRoot[:32], h12)
	copy(combinedRoot[32:], h33)
	expected := spv.DoubleHash(combinedRoot)

	assert.Equal(t, expected, root)
}

func TestComputeDirectoryMerkleRoot_Deterministic(t *testing.T) {
	children := []ChildEntry{
		{Index: 0, Name: "x.txt", Type: NodeTypeFile, PubKey: makePubKey(0x10)},
		{Index: 1, Name: "y.txt", Type: NodeTypeFile, PubKey: makePubKey(0x20)},
	}

	r1 := ComputeDirectoryMerkleRoot(children)
	r2 := ComputeDirectoryMerkleRoot(children)
	assert.Equal(t, r1, r2, "same children → same root")
}

func TestComputeDirectoryMerkleRoot_CrossVerifyWithSPV(t *testing.T) {
	// Verify our directory Merkle tree matches spv.BuildMerkleTree for same inputs
	children := []ChildEntry{
		{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
		{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)},
		{Index: 2, Name: "c.txt", Type: NodeTypeDir, PubKey: makePubKey(0x03)},
		{Index: 3, Name: "d.txt", Type: NodeTypeFile, PubKey: makePubKey(0x04)},
	}

	// Compute leaf hashes
	leafHashes := make([][]byte, len(children))
	for i := range children {
		leafHashes[i] = ComputeChildLeafHash(&children[i])
	}

	// spv.BuildMerkleTree should give same root
	spvTree := spv.BuildMerkleTree(leafHashes)
	require.NotNil(t, spvTree)
	spvRoot := spvTree[0]

	dirRoot := ComputeDirectoryMerkleRoot(children)
	assert.Equal(t, spvRoot, dirRoot, "directory Merkle root must match spv.BuildMerkleTree")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run TestComputeDirectoryMerkleRoot -v`
Expected: FAIL — `ComputeDirectoryMerkleRoot` undefined

**Step 3: Write minimal implementation**

Add to `libbitfs/metanet/merkle.go`:

```go
// ComputeDirectoryMerkleRoot computes the Merkle root from a directory's
// children list. Returns nil for empty or nil children slice.
//
// Algorithm (identical to Bitcoin block Merkle tree):
//  1. Compute leaf hashes: leaf[i] = DoubleHash(serialize(child[i]))
//  2. If odd count, duplicate last leaf
//  3. Pair adjacent and hash: parent = DoubleHash(left || right)
//  4. Repeat until one root remains
func ComputeDirectoryMerkleRoot(children []ChildEntry) []byte {
	if len(children) == 0 {
		return nil
	}

	// Compute leaf hashes
	leafHashes := make([][]byte, len(children))
	for i := range children {
		leafHashes[i] = ComputeChildLeafHash(&children[i])
	}

	// Build Merkle tree (same algorithm as spv.BuildMerkleTree)
	level := leafHashes
	for len(level) > 1 {
		if len(level)%2 != 0 {
			dup := make([]byte, 32)
			copy(dup, level[len(level)-1])
			level = append(level, dup)
		}

		nextLevel := make([][]byte, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			combined := make([]byte, 64)
			copy(combined[:32], level[i])
			copy(combined[32:], level[i+1])
			nextLevel[i/2] = spv.DoubleHash(combined)
		}
		level = nextLevel
	}

	return level[0]
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run TestComputeDirectoryMerkleRoot -v`
Expected: PASS (6 tests)

**Step 5: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add metanet/merkle.go metanet/merkle_test.go
git commit -m "feat(metanet): add ComputeDirectoryMerkleRoot"
```

---

### Task 3: Add `BuildDirectoryMerkleProof` and `VerifyChildMembership`

**Files:**
- Modify: `libbitfs/metanet/merkle.go`
- Modify: `libbitfs/metanet/merkle_test.go`

**Step 1: Write the failing tests**

Append to `libbitfs/metanet/merkle_test.go`:

```go
func TestBuildDirectoryMerkleProof_InvalidIndex(t *testing.T) {
	children := []ChildEntry{
		{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
	}

	_, err := BuildDirectoryMerkleProof(children, -1)
	assert.Error(t, err)

	_, err = BuildDirectoryMerkleProof(children, 1)
	assert.Error(t, err)

	_, err = BuildDirectoryMerkleProof(nil, 0)
	assert.Error(t, err)
}

func TestBuildDirectoryMerkleProof_SingleChild(t *testing.T) {
	children := []ChildEntry{
		{Index: 0, Name: "only.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
	}

	proof, err := BuildDirectoryMerkleProof(children, 0)
	require.NoError(t, err)
	assert.Empty(t, proof, "single child: proof is empty (leaf IS the root)")
}

func TestBuildAndVerify_TwoChildren(t *testing.T) {
	children := []ChildEntry{
		{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
		{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)},
	}
	merkleRoot := ComputeDirectoryMerkleRoot(children)

	for idx := 0; idx < 2; idx++ {
		proof, err := BuildDirectoryMerkleProof(children, idx)
		require.NoError(t, err)
		assert.Len(t, proof, 1, "two children: proof has 1 sibling")

		ok := VerifyChildMembership(&children[idx], proof, idx, merkleRoot)
		assert.True(t, ok, "valid proof for child %d", idx)
	}
}

func TestBuildAndVerify_FourChildren(t *testing.T) {
	children := make([]ChildEntry, 4)
	for i := range children {
		children[i] = ChildEntry{
			Index:  uint32(i),
			Name:   fmt.Sprintf("file%d.txt", i),
			Type:   NodeTypeFile,
			PubKey: makePubKey(byte(i + 1)),
		}
	}
	merkleRoot := ComputeDirectoryMerkleRoot(children)

	for idx := 0; idx < 4; idx++ {
		proof, err := BuildDirectoryMerkleProof(children, idx)
		require.NoError(t, err)
		assert.Len(t, proof, 2, "4 children: proof depth is 2")

		ok := VerifyChildMembership(&children[idx], proof, idx, merkleRoot)
		assert.True(t, ok, "valid proof for child %d", idx)
	}
}

func TestBuildAndVerify_FiveChildren_OddPadding(t *testing.T) {
	children := make([]ChildEntry, 5)
	for i := range children {
		children[i] = ChildEntry{
			Index:  uint32(i),
			Name:   fmt.Sprintf("f%d", i),
			Type:   NodeTypeFile,
			PubKey: makePubKey(byte(i + 1)),
		}
	}
	merkleRoot := ComputeDirectoryMerkleRoot(children)

	for idx := 0; idx < 5; idx++ {
		proof, err := BuildDirectoryMerkleProof(children, idx)
		require.NoError(t, err)

		ok := VerifyChildMembership(&children[idx], proof, idx, merkleRoot)
		assert.True(t, ok, "valid proof for child %d", idx)
	}
}

func TestVerifyChildMembership_TamperedEntry(t *testing.T) {
	children := []ChildEntry{
		{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
		{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)},
	}
	merkleRoot := ComputeDirectoryMerkleRoot(children)

	proof, err := BuildDirectoryMerkleProof(children, 0)
	require.NoError(t, err)

	// Tamper: change the entry name
	tampered := children[0]
	tampered.Name = "evil.txt"
	ok := VerifyChildMembership(&tampered, proof, 0, merkleRoot)
	assert.False(t, ok, "tampered entry must fail verification")
}

func TestVerifyChildMembership_WrongProof(t *testing.T) {
	children := []ChildEntry{
		{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
		{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)},
		{Index: 2, Name: "c.txt", Type: NodeTypeFile, PubKey: makePubKey(0x03)},
	}
	merkleRoot := ComputeDirectoryMerkleRoot(children)

	// Get proof for child 0 but try to verify child 1 with it
	proof0, err := BuildDirectoryMerkleProof(children, 0)
	require.NoError(t, err)

	ok := VerifyChildMembership(&children[1], proof0, 0, merkleRoot)
	assert.False(t, ok, "wrong proof must fail")
}

func TestVerifyChildMembership_WrongIndex(t *testing.T) {
	children := []ChildEntry{
		{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
		{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)},
	}
	merkleRoot := ComputeDirectoryMerkleRoot(children)

	proof, err := BuildDirectoryMerkleProof(children, 0)
	require.NoError(t, err)

	// Use correct entry and proof but wrong index
	ok := VerifyChildMembership(&children[0], proof, 1, merkleRoot)
	assert.False(t, ok, "wrong index must fail")
}

func TestVerifyChildMembership_WrongMerkleRoot(t *testing.T) {
	children := []ChildEntry{
		{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
		{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)},
	}
	merkleRoot := ComputeDirectoryMerkleRoot(children)

	proof, err := BuildDirectoryMerkleProof(children, 0)
	require.NoError(t, err)

	// Use a different merkle root
	fakeRoot := make([]byte, 32)
	fakeRoot[0] = 0xFF
	ok := VerifyChildMembership(&children[0], proof, 0, fakeRoot)
	assert.False(t, ok, "wrong merkle root must fail")

	// Nil merkle root
	ok = VerifyChildMembership(&children[0], proof, 0, nil)
	assert.False(t, ok, "nil merkle root must fail")
}
```

Note: add `"fmt"` to the import block in merkle_test.go if not already present.

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run "TestBuild|TestVerifyChild" -v`
Expected: FAIL — `BuildDirectoryMerkleProof` and `VerifyChildMembership` undefined

**Step 3: Write minimal implementation**

Add to `libbitfs/metanet/merkle.go`:

```go
// BuildDirectoryMerkleProof builds a Merkle proof for a child at the given
// position index. Returns the sibling hashes needed to recompute the root.
// For a single child, returns an empty proof (the leaf IS the root).
func BuildDirectoryMerkleProof(children []ChildEntry, childIndex int) ([][]byte, error) {
	if len(children) == 0 {
		return nil, fmt.Errorf("metanet: cannot build proof for empty children")
	}
	if childIndex < 0 || childIndex >= len(children) {
		return nil, fmt.Errorf("metanet: child index %d out of range [0, %d)", childIndex, len(children))
	}

	// Single child: no proof needed (leaf is root)
	if len(children) == 1 {
		return nil, nil
	}

	// Compute all leaf hashes
	level := make([][]byte, len(children))
	for i := range children {
		level[i] = ComputeChildLeafHash(&children[i])
	}

	var proof [][]byte
	idx := childIndex

	for len(level) > 1 {
		// Pad if odd
		if len(level)%2 != 0 {
			dup := make([]byte, 32)
			copy(dup, level[len(level)-1])
			level = append(level, dup)
		}

		// Collect sibling
		if idx%2 == 0 {
			sibling := make([]byte, 32)
			copy(sibling, level[idx+1])
			proof = append(proof, sibling)
		} else {
			sibling := make([]byte, 32)
			copy(sibling, level[idx-1])
			proof = append(proof, sibling)
		}

		// Build next level
		nextLevel := make([][]byte, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			combined := make([]byte, 64)
			copy(combined[:32], level[i])
			copy(combined[32:], level[i+1])
			nextLevel[i/2] = spv.DoubleHash(combined)
		}
		level = nextLevel
		idx /= 2
	}

	return proof, nil
}

// VerifyChildMembership verifies that a ChildEntry belongs to a directory
// with the given MerkleRoot, using the provided proof path and position index.
func VerifyChildMembership(entry *ChildEntry, proof [][]byte, index int, merkleRoot []byte) bool {
	if entry == nil || len(merkleRoot) != 32 {
		return false
	}

	leafHash := ComputeChildLeafHash(entry)

	// Use spv.ComputeMerkleRoot to walk the proof
	computed := spv.ComputeMerkleRoot(leafHash, uint32(index), proof)
	if computed == nil {
		// Single child case: no proof nodes, leaf is root
		if len(proof) == 0 && index == 0 {
			for i := 0; i < 32; i++ {
				if leafHash[i] != merkleRoot[i] {
					return false
				}
			}
			return true
		}
		return false
	}

	for i := 0; i < 32; i++ {
		if computed[i] != merkleRoot[i] {
			return false
		}
	}
	return true
}
```

Also add `"fmt"` to the imports in `merkle.go`.

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run "TestBuild|TestVerifyChild" -v`
Expected: PASS (all 9 new tests)

**Step 5: Run all existing tests to check for regressions**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -v`
Expected: All tests pass

**Step 6: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add metanet/merkle.go metanet/merkle_test.go
git commit -m "feat(metanet): add BuildDirectoryMerkleProof and VerifyChildMembership"
```

---

### Task 4: Add `MerkleRoot` field to Node struct

**Files:**
- Modify: `libbitfs/metanet/node.go:98-134` — add field to Node struct

**Step 1: Add the field**

In `libbitfs/metanet/node.go`, add `MerkleRoot []byte` to the Node struct after the `Children` field (around line 118):

```go
	Children       []ChildEntry
	MerkleRoot     []byte // Merkle root of Children (32 bytes, nil for non-dir or empty dir)
	NextChildIndex uint32
```

**Step 2: Run all tests to verify no regressions**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -v`
Expected: All tests pass (adding a field doesn't break anything)

**Step 3: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add metanet/node.go
git commit -m "feat(metanet): add MerkleRoot field to Node struct"
```

---

### Task 5: Add TLV tag `0x1A` serialization (write)

**Files:**
- Modify: `libbitfs/metanet/parser.go:12-38` — add tag constant
- Modify: `libbitfs/metanet/parser.go:79-203` — serialize MerkleRoot in `SerializePayload`
- Modify: `libbitfs/metanet/merkle_test.go` — add serialization test

**Step 1: Write the failing test**

Append to `libbitfs/metanet/merkle_test.go`:

```go
func TestSerializePayload_MerkleRoot(t *testing.T) {
	node := &Node{
		Type:     NodeTypeDir,
		Children: []ChildEntry{
			{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
			{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)},
		},
		MerkleRoot: ComputeDirectoryMerkleRoot([]ChildEntry{
			{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
			{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)},
		}),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	// Tag 0x1A should be present in the serialized output
	found := false
	offset := 0
	for offset < len(payload) {
		if offset+3 > len(payload) {
			break
		}
		tag := payload[offset]
		length := int(payload[offset+1]) | int(payload[offset+2])<<8
		offset += 3
		if tag == 0x1A {
			found = true
			assert.Equal(t, 32, length, "MerkleRoot TLV length must be 32")
			assert.Equal(t, node.MerkleRoot, payload[offset:offset+length])
		}
		offset += length
	}
	assert.True(t, found, "tag 0x1A must be present in serialized payload")
}

func TestSerializePayload_MerkleRoot_NilSkipped(t *testing.T) {
	node := &Node{
		Type: NodeTypeDir,
		// No children, MerkleRoot is nil
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	// Tag 0x1A should NOT be present
	offset := 0
	for offset < len(payload) {
		if offset+3 > len(payload) {
			break
		}
		tag := payload[offset]
		length := int(payload[offset+1]) | int(payload[offset+2])<<8
		offset += 3
		assert.NotEqual(t, byte(0x1A), tag, "tag 0x1A must not appear when MerkleRoot is nil")
		offset += length
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run TestSerializePayload_MerkleRoot -v`
Expected: FAIL — tag 0x1A not found in serialized output

**Step 3: Add the tag constant and serialization**

In `libbitfs/metanet/parser.go`, add the constant after `tagNetworkName`:

```go
	tagNetworkName    = 0x19
	tagMerkleRoot     = 0x1A // 32 bytes, directory Merkle root of children
```

In `SerializePayload()`, add after the `NetworkName` block (before `return buf, nil`):

```go
	// MerkleRoot (directory only, present only when non-nil)
	if len(node.MerkleRoot) > 0 {
		buf = appendBytesField(buf, tagMerkleRoot, node.MerkleRoot)
	}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run TestSerializePayload_MerkleRoot -v`
Expected: PASS (2 tests)

**Step 5: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add metanet/parser.go metanet/merkle_test.go
git commit -m "feat(metanet): serialize MerkleRoot as TLV tag 0x1A"
```

---

### Task 6: Add TLV tag `0x1A` deserialization (read) and round-trip test

**Files:**
- Modify: `libbitfs/metanet/parser.go:270-384` — parse tag 0x1A in `deserializePayload`
- Modify: `libbitfs/metanet/merkle_test.go` — add round-trip and backward-compat tests

**Step 1: Write the failing tests**

Append to `libbitfs/metanet/merkle_test.go`:

```go
func TestSerializeDeserialize_MerkleRoot_RoundTrip(t *testing.T) {
	children := []ChildEntry{
		{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x01)},
		{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x02)},
		{Index: 2, Name: "c/", Type: NodeTypeDir, PubKey: makePubKey(0x03)},
	}
	merkleRoot := ComputeDirectoryMerkleRoot(children)

	original := &Node{
		Type:       NodeTypeDir,
		Children:   children,
		MerkleRoot: merkleRoot,
	}

	payload, err := SerializePayload(original)
	require.NoError(t, err)

	parsed := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, parsed)
	require.NoError(t, err)

	assert.Equal(t, original.MerkleRoot, parsed.MerkleRoot, "MerkleRoot must survive round-trip")
	assert.Len(t, parsed.MerkleRoot, 32)
}

func TestDeserializePayload_NoMerkleRoot_BackwardCompat(t *testing.T) {
	// Simulate old node without tag 0x1A: just version + type
	node := &Node{
		Type: NodeTypeDir,
	}
	payload, err := SerializePayload(node)
	require.NoError(t, err)

	parsed := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, parsed)
	require.NoError(t, err)

	assert.Nil(t, parsed.MerkleRoot, "old nodes without tag 0x1A must have nil MerkleRoot")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run "TestSerializeDeserialize_MerkleRoot|TestDeserializePayload_NoMerkleRoot" -v`
Expected: FAIL — `TestSerializeDeserialize_MerkleRoot_RoundTrip` fails because `parsed.MerkleRoot` is nil (tag not parsed yet)

**Step 3: Add deserialization**

In `deserializePayload()` in `libbitfs/metanet/parser.go`, add a case before the `default:` branch:

```go
		case tagMerkleRoot:
			node.MerkleRoot = make([]byte, length)
			copy(node.MerkleRoot, value)
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run "TestSerializeDeserialize_MerkleRoot|TestDeserializePayload_NoMerkleRoot" -v`
Expected: PASS (2 tests)

**Step 5: Run all metanet tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -v`
Expected: All tests pass

**Step 6: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add metanet/parser.go metanet/merkle_test.go
git commit -m "feat(metanet): deserialize MerkleRoot from TLV tag 0x1A"
```

---

### Task 7: Wire `recomputeMerkleRoot` into directory operations

**Files:**
- Modify: `libbitfs/metanet/directory.go:39-83` — AddChild
- Modify: `libbitfs/metanet/directory.go:87-103` — RemoveChild
- Modify: `libbitfs/metanet/directory.go:106-133` — RenameChild
- Modify: `libbitfs/metanet/merkle_test.go` — add integration tests

**Step 1: Write the failing tests**

Append to `libbitfs/metanet/merkle_test.go`:

```go
func TestAddChild_UpdatesMerkleRoot(t *testing.T) {
	dir := &Node{
		Type:     NodeTypeDir,
		PNode:    makePubKey(0xAA),
		Children: nil,
	}

	// Initially nil
	assert.Nil(t, dir.MerkleRoot)

	// Add first child
	_, err := AddChild(dir, "a.txt", NodeTypeFile, makePubKey(0x01), false)
	require.NoError(t, err)
	require.Len(t, dir.MerkleRoot, 32, "MerkleRoot must be set after AddChild")

	// Must match manual computation
	expected := ComputeDirectoryMerkleRoot(dir.Children)
	assert.Equal(t, expected, dir.MerkleRoot)

	// Add second child — root changes
	oldRoot := make([]byte, 32)
	copy(oldRoot, dir.MerkleRoot)

	_, err = AddChild(dir, "b.txt", NodeTypeFile, makePubKey(0x02), false)
	require.NoError(t, err)
	assert.NotEqual(t, oldRoot, dir.MerkleRoot, "MerkleRoot must change when children change")

	expected = ComputeDirectoryMerkleRoot(dir.Children)
	assert.Equal(t, expected, dir.MerkleRoot)
}

func TestRemoveChild_UpdatesMerkleRoot(t *testing.T) {
	dir := &Node{
		Type:  NodeTypeDir,
		PNode: makePubKey(0xAA),
	}

	_, err := AddChild(dir, "a.txt", NodeTypeFile, makePubKey(0x01), false)
	require.NoError(t, err)
	_, err = AddChild(dir, "b.txt", NodeTypeFile, makePubKey(0x02), false)
	require.NoError(t, err)

	rootBefore := make([]byte, 32)
	copy(rootBefore, dir.MerkleRoot)

	// Remove one child
	err = RemoveChild(dir, "a.txt")
	require.NoError(t, err)
	assert.NotEqual(t, rootBefore, dir.MerkleRoot, "MerkleRoot must change after removal")

	expected := ComputeDirectoryMerkleRoot(dir.Children)
	assert.Equal(t, expected, dir.MerkleRoot)

	// Remove last child — MerkleRoot becomes nil
	err = RemoveChild(dir, "b.txt")
	require.NoError(t, err)
	assert.Nil(t, dir.MerkleRoot, "empty dir has nil MerkleRoot")
}

func TestRenameChild_UpdatesMerkleRoot(t *testing.T) {
	dir := &Node{
		Type:  NodeTypeDir,
		PNode: makePubKey(0xAA),
	}

	_, err := AddChild(dir, "old.txt", NodeTypeFile, makePubKey(0x01), false)
	require.NoError(t, err)
	_, err = AddChild(dir, "other.txt", NodeTypeFile, makePubKey(0x02), false)
	require.NoError(t, err)

	rootBefore := make([]byte, 32)
	copy(rootBefore, dir.MerkleRoot)

	err = RenameChild(dir, "old.txt", "new.txt")
	require.NoError(t, err)
	assert.NotEqual(t, rootBefore, dir.MerkleRoot, "MerkleRoot must change after rename")

	expected := ComputeDirectoryMerkleRoot(dir.Children)
	assert.Equal(t, expected, dir.MerkleRoot)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run "TestAddChild_UpdatesMerkleRoot|TestRemoveChild_UpdatesMerkleRoot|TestRenameChild_UpdatesMerkleRoot" -v`
Expected: FAIL — `dir.MerkleRoot` is nil after AddChild (not wired yet)

**Step 3: Wire recomputation into directory operations**

In `libbitfs/metanet/directory.go`, add a helper function at the bottom:

```go
// recomputeMerkleRoot updates the node's MerkleRoot from its current Children.
func recomputeMerkleRoot(node *Node) {
	node.MerkleRoot = ComputeDirectoryMerkleRoot(node.Children)
}
```

Then add `recomputeMerkleRoot(dirNode)` calls to three functions:

**AddChild** — add just before the `return` statement (after `dirNode.NextChildIndex++`):

```go
	dirNode.NextChildIndex++
	recomputeMerkleRoot(dirNode)

	return &dirNode.Children[len(dirNode.Children)-1], nil
```

**RemoveChild** — add just before `return nil` inside the name-match `if` block:

```go
		if child.Name == name {
			dirNode.Children = append(dirNode.Children[:i], dirNode.Children[i+1:]...)
			recomputeMerkleRoot(dirNode)
			return nil
		}
```

**RenameChild** — add just before `return nil` inside the name-match `if` block:

```go
		if dirNode.Children[i].Name == oldName {
			dirNode.Children[i].Name = newName
			recomputeMerkleRoot(dirNode)
			return nil
		}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run "TestAddChild_UpdatesMerkleRoot|TestRemoveChild_UpdatesMerkleRoot|TestRenameChild_UpdatesMerkleRoot" -v`
Expected: PASS (3 tests)

**Step 5: Run ALL tests across the whole repo**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./... -v`
Expected: All tests pass (no regressions)

**Step 6: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add metanet/directory.go metanet/merkle_test.go
git commit -m "feat(metanet): auto-recompute MerkleRoot on directory mutations"
```

---

### Task 8: Final verification and cross-package test

**Files:**
- Modify: `libbitfs/metanet/merkle_test.go` — add full end-to-end test

**Step 1: Write the end-to-end integration test**

Append to `libbitfs/metanet/merkle_test.go`:

```go
func TestMerkleRoot_EndToEnd(t *testing.T) {
	// Simulate a realistic directory lifecycle:
	// 1. Create dir, add children
	// 2. Verify MerkleRoot is set and correct
	// 3. Build proof for each child and verify membership
	// 4. Serialize and deserialize — MerkleRoot preserved
	// 5. Remove a child — MerkleRoot updates, old proof fails

	dir := &Node{
		Type:  NodeTypeDir,
		PNode: makePubKey(0xDD),
	}

	// Add 3 children
	_, err := AddChild(dir, "readme.md", NodeTypeFile, makePubKey(0x01), false)
	require.NoError(t, err)
	_, err = AddChild(dir, "src", NodeTypeDir, makePubKey(0x02), false)
	require.NoError(t, err)
	_, err = AddChild(dir, "go.mod", NodeTypeFile, makePubKey(0x03), false)
	require.NoError(t, err)

	require.Len(t, dir.MerkleRoot, 32)
	assert.Equal(t, ComputeDirectoryMerkleRoot(dir.Children), dir.MerkleRoot)

	// Build and verify proofs for each child
	for idx, child := range dir.Children {
		proof, err := BuildDirectoryMerkleProof(dir.Children, idx)
		require.NoError(t, err)

		ok := VerifyChildMembership(&child, proof, idx, dir.MerkleRoot)
		assert.True(t, ok, "proof for %q at index %d", child.Name, idx)
	}

	// Serialize round-trip
	payload, err := SerializePayload(dir)
	require.NoError(t, err)

	parsed := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, parsed)
	require.NoError(t, err)
	assert.Equal(t, dir.MerkleRoot, parsed.MerkleRoot)

	// Save proof for child 0 before removal
	proof0, err := BuildDirectoryMerkleProof(dir.Children, 0)
	require.NoError(t, err)
	child0 := dir.Children[0]
	oldRoot := make([]byte, 32)
	copy(oldRoot, dir.MerkleRoot)

	// Remove child 1 ("src") — MerkleRoot changes
	err = RemoveChild(dir, "src")
	require.NoError(t, err)
	assert.NotEqual(t, oldRoot, dir.MerkleRoot, "root must change after removal")

	// Old proof for child 0 against the OLD root still works
	ok := VerifyChildMembership(&child0, proof0, 0, oldRoot)
	assert.True(t, ok, "old proof against old root still valid")

	// Old proof for child 0 against the NEW root does NOT work
	ok = VerifyChildMembership(&child0, proof0, 0, dir.MerkleRoot)
	assert.False(t, ok, "old proof against new root must fail")
}
```

**Step 2: Run the test**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./metanet/ -run TestMerkleRoot_EndToEnd -v`
Expected: PASS

**Step 3: Run full test suite across both repos**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./... -v`
Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -v`
Expected: All tests pass in both repos

**Step 4: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add metanet/merkle_test.go
git commit -m "test(metanet): add end-to-end MerkleRoot lifecycle test"
```

---

## Summary

| Task | Description | Files | Tests |
|------|-------------|-------|-------|
| 1 | `ComputeChildLeafHash` | merkle.go, merkle_test.go | 3 |
| 2 | `ComputeDirectoryMerkleRoot` | merkle.go, merkle_test.go | 6 |
| 3 | `BuildDirectoryMerkleProof` + `VerifyChildMembership` | merkle.go, merkle_test.go | 9 |
| 4 | Add `MerkleRoot` field to Node | node.go | 0 (structural) |
| 5 | TLV tag `0x1A` serialization | parser.go, merkle_test.go | 2 |
| 6 | TLV tag `0x1A` deserialization + round-trip | parser.go, merkle_test.go | 2 |
| 7 | Wire into AddChild/RemoveChild/RenameChild | directory.go, merkle_test.go | 3 |
| 8 | End-to-end lifecycle test | merkle_test.go | 1 |
| **Total** | | **4 files modified, 2 created** | **26 tests** |

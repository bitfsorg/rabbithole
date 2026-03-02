# Directory MerkleRoot Design

## Motivation

BitFS stores per-file content hashes (`key_hash = SHA256d(plaintext)`) on-chain, but has no structural integrity commitment for directories. A client verifying that a file belongs to a directory must fetch the full directory node and scan its children list. There is no compact proof of membership.

This design adds a **MerkleRoot** field to directory nodes, directly analogous to Bitcoin's block header MerkleRoot. Just as a Bitcoin block header commits to all transactions in the block via a Merkle tree, a BitFS directory node commits to all its children via a Merkle tree of ChildEntry records.

## Analogy to Bitcoin

| Bitcoin | BitFS |
|---------|-------|
| Block | Directory |
| Transactions in block | ChildEntry records in directory |
| Leaf hash = DoubleHash(raw_tx) | Leaf hash = DoubleHash(serialize(ChildEntry)) |
| Block header stores MerkleRoot | Directory node payload stores MerkleRoot |
| Merkle proof proves tx in block | Merkle proof proves child in directory |

## Design Decisions

**D1: Scope — per-directory, not recursive.** Each directory's MerkleRoot covers only its direct children. A subdirectory's MerkleRoot is NOT included in the parent's leaf hash. This means:
- Updating a deep file only requires updating its direct parent (O(1) chain transactions).
- No cascading updates to ancestor directories.
- Each directory is an independently verifiable unit.

**D2: Leaf hash — DoubleHash of serialized ChildEntry.** The leaf hash is `SHA256(SHA256(serialize(ChildEntry)))`, reusing the existing ChildEntry binary format: `index(4) || nameLen(2) || name(N) || type(4) || pubkeyLen(1) || pubkey(33) || hardened(1)`. This is stable — child self-updates (content changes, access changes) do not alter the ChildEntry in the parent.

**D3: Computation strategy — compute on write.** MerkleRoot is recomputed whenever the Children list changes (AddChild, RemoveChild, RenameChild) and stored in the Node struct. It is always in sync with Children.

**D4: Backward compatibility.** Old directory nodes without tag 0x1A have MerkleRoot = nil. Verification is skipped for such nodes.

## Data Structure Changes

### Node struct (metanet/node.go)

Add one field:

```go
type Node struct {
    // ... existing fields ...
    MerkleRoot []byte // Merkle root of Children (32 bytes, nil for non-dir or empty dir)
}
```

### TLV tag (metanet/parser.go)

New constant:

```go
tagMerkleRoot = 0x1A // 32 bytes, present only on DIR nodes with children
```

## Merkle Tree Construction

### Leaf ordering

Leaves are ordered by position in the Children slice (insertion order). This matches the on-chain serialization order.

### Algorithm

Reuse the same algorithm as `spv.BuildMerkleTree`:

1. Compute leaf hashes: for each child, `leaf = DoubleHash(serialize(child))`
2. If odd number of leaves, duplicate the last leaf
3. Pair adjacent leaves and hash: `parent = DoubleHash(left || right)`
4. Repeat until one root remains

### Edge cases

| Children count | MerkleRoot |
|---------------|------------|
| 0 (empty dir) | nil (tag 0x1A not written) |
| 1 | DoubleHash(serialize(child)) — the single leaf IS the root |
| N (even) | Standard Merkle tree |
| N (odd) | Last leaf duplicated to make even |

## Affected Code

### metanet/merkle.go (new file)

```go
// ComputeDirectoryMerkleRoot computes the Merkle root from a directory's children.
// Returns nil for empty children slice.
func ComputeDirectoryMerkleRoot(children []ChildEntry) []byte

// VerifyChildMembership verifies a ChildEntry belongs to a directory
// given the directory's MerkleRoot and a Merkle proof path.
func VerifyChildMembership(entry ChildEntry, proof [][]byte, index int, merkleRoot []byte) bool

// ComputeChildLeafHash computes the leaf hash for a single ChildEntry.
func ComputeChildLeafHash(entry ChildEntry) []byte

// BuildDirectoryMerkleProof builds a Merkle proof for a child at the given index.
func BuildDirectoryMerkleProof(children []ChildEntry, childIndex int) ([][]byte, error)
```

### metanet/directory.go (modify)

- `AddChild()`: call `recomputeMerkleRoot(dirNode)` after appending
- `RemoveChild()`: call `recomputeMerkleRoot(dirNode)` after removing
- `RenameChild()`: call `recomputeMerkleRoot(dirNode)` after renaming
- Add internal `recomputeMerkleRoot(node *Node)` helper

### metanet/parser.go (modify)

- `SerializePayload()`: write tag 0x1A with MerkleRoot bytes (skip if nil)
- `ParseNode()`: parse tag 0x1A into `Node.MerkleRoot`

### metanet/node.go (modify)

- Add `MerkleRoot []byte` field to Node struct

## Verification Flow

To verify that `paper.pdf` belongs to directory `/docs/`:

```
1. Obtain /docs/ directory node → read MerkleRoot field
2. Obtain the ChildEntry for paper.pdf from the directory
3. Compute leaf_hash = DoubleHash(serialize(ChildEntry))
4. Obtain Merkle proof (sibling hashes along the path)
5. Recompute root from leaf_hash + proof path
6. Compare against directory's MerkleRoot
```

## What Does NOT Change

- **Child self-updates**: A child updating its content, access level, or metadata does NOT change its ChildEntry in the parent directory. Parent's MerkleRoot stays stable.
- **Transaction templates**: MerkleRoot is carried in the existing Payload field via TLV. No new transaction types needed.
- **key_hash**: File-level content hashing is unchanged.
- **SPV verification**: Block-level SPV is unrelated and unaffected.

## Testing Strategy

1. **Unit tests** for `ComputeDirectoryMerkleRoot`:
   - Empty children → nil
   - Single child → leaf hash equals root
   - Two children → manual calculation verification
   - Odd number → duplication padding
   - Deterministic: same children → same root

2. **Unit tests** for `VerifyChildMembership`:
   - Valid proof → true
   - Tampered entry → false
   - Wrong proof → false
   - Wrong index → false

3. **Unit tests** for `BuildDirectoryMerkleProof`:
   - Proof for each position in a tree
   - Round-trip: build proof → verify → pass

4. **Integration with directory operations**:
   - AddChild → MerkleRoot updated
   - RemoveChild → MerkleRoot updated
   - RenameChild → MerkleRoot updated
   - MerkleRoot matches manual computation

5. **Serialization round-trip**:
   - Serialize node with MerkleRoot → parse back → MerkleRoot preserved
   - Old nodes without tag 0x1A → MerkleRoot is nil

6. **Cross-verification with spv package**:
   - Verify that the Merkle algorithm produces identical results to `spv.BuildMerkleTree` for the same inputs

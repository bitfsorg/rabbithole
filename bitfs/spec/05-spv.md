# Module Specification: internal/spv

## PURPOSE

SPV (Simplified Payment Verification) light client for BitFS. Stores transactions locally with Merkle proofs, verifies inclusion in the blockchain without downloading full blocks. Implements the core principle: "all transaction info + Merkle proof saved locally, never query blockchain."

Design references: ConceptDesign #3; SystemDesign section 7; DetailedDesign implied by SPV verification chain.

## PUBLIC API

### Types

```go
// BlockHeader represents a BSV block header (80 bytes).
type BlockHeader struct {
    Version       int32
    PrevBlock     []byte  // 32 bytes
    MerkleRoot    []byte  // 32 bytes
    Timestamp     uint32
    Bits          uint32
    Nonce         uint32
    Height        uint32  // Not in raw header; tracked separately
    Hash          []byte  // Computed: double-SHA256 of 80-byte header
}

// MerkleProof represents a Merkle inclusion proof for a transaction.
type MerkleProof struct {
    TxID       []byte     // Transaction hash (32 bytes)
    Index      uint32     // Position in the block's transaction list
    Nodes      [][]byte   // Merkle branch hashes, bottom-up
    BlockHash  []byte     // Block header hash this proof is for
}

// StoredTx represents a transaction stored with its Merkle proof.
type StoredTx struct {
    TxID        []byte       // 32 bytes
    RawTx       []byte       // Full serialized transaction
    Proof       *MerkleProof // Merkle proof (nil = unconfirmed)
    BlockHeight uint32       // 0 = unconfirmed
    Timestamp   uint64       // Time added to store
}
```

### Interfaces

```go
// HeaderStore persists block headers for chain verification.
type HeaderStore interface {
    // PutHeader stores a block header.
    PutHeader(header *BlockHeader) error

    // GetHeader retrieves a header by block hash.
    GetHeader(blockHash []byte) (*BlockHeader, error)

    // GetHeaderByHeight retrieves a header by block height.
    GetHeaderByHeight(height uint32) (*BlockHeader, error)

    // GetTip returns the header with the greatest height.
    GetTip() (*BlockHeader, error)

    // GetHeaderCount returns the total number of stored headers.
    GetHeaderCount() (uint64, error)
}

// TxStore persists transactions with Merkle proofs.
type TxStore interface {
    // PutTx stores a transaction with optional Merkle proof.
    PutTx(tx *StoredTx) error

    // GetTx retrieves a transaction by TxID.
    GetTx(txID []byte) (*StoredTx, error)

    // GetTxsByPubKey returns all transactions related to a P_node public key.
    GetTxsByPubKey(pNode []byte) ([]*StoredTx, error)

    // DeleteTx removes a transaction from the store.
    DeleteTx(txID []byte) error

    // ListTxs returns all stored transactions (for backup/export).
    ListTxs() ([]*StoredTx, error)
}
```

### Functions

```go
// VerifyMerkleProof verifies that a transaction is included in a block.
// Recomputes the Merkle path from TxID + proof nodes and checks against
// the expected Merkle root from the block header.
func VerifyMerkleProof(proof *MerkleProof, expectedMerkleRoot []byte) (bool, error)

// VerifyTransaction performs the full SPV verification chain:
//   1. Transaction integrity: raw tx deserializes correctly
//   2. Merkle proof: tx is included in a block (via VerifyMerkleProof)
//   3. Block header: Merkle root matches the stored block header
//   4. Chain verification: block header is on the longest chain
func VerifyTransaction(tx *StoredTx, headers HeaderStore) error

// VerifyHeaderChain checks that a sequence of headers forms a valid chain
// (each header's PrevBlock matches the previous header's hash).
func VerifyHeaderChain(headers []*BlockHeader) error

// ComputeMerkleRoot computes the Merkle root from a transaction hash
// and its proof branch.
func ComputeMerkleRoot(txHash []byte, index uint32, proofNodes [][]byte) []byte

// SerializeHeader serializes a BlockHeader to 80 bytes.
func SerializeHeader(h *BlockHeader) []byte

// DeserializeHeader deserializes 80 bytes into a BlockHeader.
func DeserializeHeader(data []byte) (*BlockHeader, error)

// DoubleHash computes SHA256(SHA256(data)), used for block/tx hashing.
func DoubleHash(data []byte) []byte
```

## DEPENDENCIES

- `crypto/sha256` -- Double-SHA256 hashing
- `encoding/binary` -- Header serialization

## DATA STRUCTURES

### Merkle Proof Verification
```
Given: TxID, Index, ProofNodes[]

hash = TxID
for i, node in ProofNodes:
    if bit i of Index is 0:
        hash = SHA256d(hash || node)
    else:
        hash = SHA256d(node || hash)

verify: hash == block.MerkleRoot
```

### SPV Verification Chain
```
1. Raw TX -> deserialize -> extract OP_RETURN
2. MerkleProof(TxHash, nodes) -> computed MerkleRoot
3. BlockHeader.MerkleRoot == computed MerkleRoot
4. BlockHeader is on longest chain (height check)
5. Content: SHA256(SHA256(plaintext)) == key_hash
```

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrMerkleProofInvalid` | Computed root does not match expected |
| `ErrHeaderNotFound` | Block header not in local store |
| `ErrTxNotFound` | Transaction not in local store |
| `ErrUnconfirmed` | Transaction has no Merkle proof yet |
| `ErrChainBroken` | Headers do not form a valid chain |
| `ErrInvalidHeader` | Header fails deserialization or hash check |

## SECURITY CONSIDERATIONS

1. **No network queries**: SPV module itself never makes network calls. Headers and proofs are provided by callers (typically from daemon or P2P sync).
2. **Checkpoint validation**: For initial sync, hardcoded checkpoints can validate the header chain without downloading all headers from genesis.
3. **Longest chain**: The module trusts the longest chain of valid headers. Eclipse attacks are mitigated by connecting to multiple peers (handled at network layer, not in this module).

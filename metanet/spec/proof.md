# Module Specification: internal/proof

## PURPOSE

The `proof` package implements storage proofs for the Metanet Chain. It handles the ECDH double-layer encryption scheme that ensures each Metanet Node holds a cryptographically unique copy of the data, and the Merkle challenge-response protocol that verifies continued data possession.

The double-layer encryption works as follows: the Owner's data is already encrypted with Method 42 (first layer). The Owner then uses ECDH with the Metanet Node's public key to derive a node-specific AES-256-GCM key and encrypts the entire ciphertext again (second layer). This produces a unique ciphertext per node, making it impossible for nodes to share proofs or copy each other's data.

The Merkle tree is built over fixed-size chunks of the double-encrypted data. Challenge-response verification uses SHA256-based Merkle proofs to prove possession of specific chunks.

## PUBLIC API

### Types

```go
// MerkleTree represents a SHA256-based binary Merkle tree built
// over data chunks.
type MerkleTree struct {
    Leaves    [][32]byte   // SHA256 hash of each chunk
    Nodes     [][32]byte   // All tree nodes (leaves + internal)
    NumLeaves uint32       // Number of leaf nodes
    Root      [32]byte     // Merkle root
}

// MerkleProof contains the sibling hashes needed to prove a leaf
// is included in the tree.
type MerkleProof struct {
    ChunkIndex uint32     // Index of the proven chunk
    Siblings   [][32]byte // Sibling hashes from leaf to root
    Root       [32]byte   // Expected Merkle root
}

// DoubleEncryptedData holds the result of ECDH double-layer encryption
// for a specific Metanet Node.
type DoubleEncryptedData struct {
    Ciphertext []byte     // Double-encrypted data
    Nonce      [12]byte   // AES-256-GCM nonce
    MerkleRoot [32]byte   // Merkle root of the chunked ciphertext
    NumChunks  uint32     // Number of chunks
    ChunkSize  uint32     // Size of each chunk in bytes
}

// ProofData is what the Metanet Node submits to claim a period's payment.
type ProofData struct {
    ChunkIndex  uint32     // Index of the challenged chunk
    ChunkData   []byte     // Raw chunk data
    MerkleProof *MerkleProof // Proof of chunk inclusion
}

// ChallengeResponse bundles a challenge with its corresponding proof
// for verification.
type ChallengeResponse struct {
    ContractTxID [32]byte
    Period       uint32
    NumChunks    uint32
    Proof        *ProofData
}
```

### Constants

```go
const (
    // DefaultChunkSize is the standard chunk size for Merkle tree construction.
    // 256 KB = 262,144 bytes
    DefaultChunkSize = 256 * 1024

    // KDFContext is the context string for ECDH key derivation.
    // providerKey = SHA256(shared_x || "storage")
    KDFContext = "storage"
)
```

### Functions

```go
// EncryptForNode performs ECDH double-layer encryption of data for a
// specific Metanet Node.
//
// Steps:
//   1. ECDH: sharedSecret = ownerPrivKey * nodePubKey
//   2. KDF:  nodeKey = SHA256(sharedSecret.X || "storage")
//   3. Encrypt: ciphertext = AES-256-GCM(nodeKey, nonce, encryptedData)
//   4. Chunk and build Merkle tree over the double-encrypted ciphertext
//
// The input encryptedData is already Method 42 encrypted (first layer).
// This adds the second layer without decrypting.
func EncryptForNode(
    ownerPrivKey []byte,    // 32-byte private key (D_node from Method 42 path)
    nodePubKey []byte,      // 33-byte compressed public key of Metanet Node
    encryptedData []byte,   // First-layer encrypted data
) (*DoubleEncryptedData, error)

// DeriveNodeKey performs ECDH key derivation for a specific Metanet Node.
// Returns the AES-256-GCM key: SHA256(ECDH_shared_x || "storage")
func DeriveNodeKey(ownerPrivKey []byte, nodePubKey []byte) ([]byte, error)

// BuildMerkleTree constructs a SHA256-based Merkle tree from data chunks.
func BuildMerkleTree(chunks [][]byte) (*MerkleTree, error)

// SplitIntoChunks splits data into fixed-size chunks.
// The last chunk may be smaller than chunkSize.
func SplitIntoChunks(data []byte, chunkSize uint32) [][]byte

// GenerateMerkleProof generates a Merkle inclusion proof for a specific chunk.
func GenerateMerkleProof(tree *MerkleTree, chunkIndex uint32) (*MerkleProof, error)

// VerifyMerkleProof verifies that a chunk is included in a Merkle tree
// with the given root.
func VerifyMerkleProof(
    chunkData []byte,
    proof *MerkleProof,
) bool

// ComputeProofHash computes the hash that must match expected_hash_k
// in the storage contract.
// proof_hash = SHA256(serialize(merkle_proof) || chunk_data)
func ComputeProofHash(proof *ProofData) [32]byte

// SerializeProofData serializes a ProofData for on-chain submission.
func SerializeProofData(proof *ProofData) []byte

// DeserializeProofData deserializes proof data from on-chain format.
func DeserializeProofData(data []byte) (*ProofData, error)

// VerifyStorageProof performs full verification of a storage proof:
//   1. Compute challenge for the given period
//   2. Verify chunk index matches challenge
//   3. Verify Merkle proof against expected root
//   4. Verify proof hash matches expected hash
func VerifyStorageProof(
    contractTxID [32]byte,
    period uint32,
    numChunks uint32,
    expectedHash [32]byte,
    merkleRoot [32]byte,
    proof *ProofData,
) error
```

## DEPENDENCIES

- `internal/contract` -- Challenge computation (ComputeChallenge)
- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- ECDH key exchange (secp256k1)
- `crypto/sha256` -- SHA256 hashing
- `crypto/aes` -- AES-256-GCM encryption
- `crypto/cipher` -- GCM mode
- `crypto/rand` -- Nonce generation

## DATA STRUCTURES

### Merkle Tree Structure

```
Binary tree with SHA256 leaves and internal nodes:

        root
       /    \
      h01    h23
     / \    / \
    h0  h1 h2  h3    <-- leaf hashes = SHA256(chunk_i)

If odd number of leaves, the last leaf is duplicated.
```

### Double-Layer Encryption

```
Layer 1 (Method 42): ciphertext_1 = AES-256-GCM(method42_key, nonce_1, plaintext)
Layer 2 (Node-specific): ciphertext_2 = AES-256-GCM(node_key, nonce_2, ciphertext_1)

Where:
    node_key = SHA256(ECDH(owner_priv, node_pub).X || "storage")

Properties:
    - Each node gets unique ciphertext_2 (different ECDH shared secret)
    - Node cannot decrypt to plaintext (does not know method42_key)
    - Node cannot share proofs with other nodes (different ciphertext_2)
    - Owner can verify any node (holds all ECDH keys)
```

### Proof Serialization Format

```
proof_data = chunk_index (4 bytes LE)
           || chunk_size (4 bytes LE)
           || chunk_data (chunk_size bytes)
           || num_siblings (4 bytes LE)
           || siblings (num_siblings * 32 bytes)
```

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrInvalidPrivateKey` | Owner private key is not valid secp256k1 |
| `ErrInvalidPublicKey` | Node public key is not valid compressed secp256k1 |
| `ErrEmptyData` | Input data is empty |
| `ErrChunkIndexOutOfRange` | ChunkIndex >= NumLeaves |
| `ErrMerkleProofInvalid` | Merkle proof does not verify against root |
| `ErrProofHashMismatch` | ComputeProofHash does not match expected_hash_k |
| `ErrChallengeMismatch` | Submitted chunk index does not match computed challenge |
| `ErrEncryptionFailed` | AES-256-GCM encryption failed |

## SECURITY CONSIDERATIONS

1. **Per-node uniqueness**: ECDH with each node's public key produces a unique shared secret, ensuring each node's ciphertext is different. Nodes cannot collude to share proofs.
2. **Two-layer opacity**: The Metanet Node only has the outer encryption key. It can verify chunk integrity (Merkle proof) but cannot decrypt the underlying content. Only the Owner (who knows both keys) can decrypt.
3. **Merkle tree integrity**: SHA256-based binary Merkle tree. Chunk hashes are computed over the double-encrypted data, binding the proof to the specific node's copy.
4. **Nonce uniqueness**: Each EncryptForNode call generates a fresh random 12-byte nonce for AES-256-GCM. Nonce reuse with the same key would compromise confidentiality.
5. **Chunk size trade-off**: 256KB default chunk size balances proof size (log2(N) * 32 bytes for Merkle siblings) against granularity. Smaller chunks mean more precise verification but larger proofs.
6. **Performance note**: For a 1GB file with N nodes, double-layer encryption requires N independent AES-GCM passes plus N network uploads. Future optimization may use lazy encryption or chunk-level encryption.

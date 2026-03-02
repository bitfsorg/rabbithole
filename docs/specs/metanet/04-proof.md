# 模块规格说明：internal/proof

## 目的

`proof` 包实现 Metanet Chain 的存储证明。它处理 ECDH 双层加密方案——确保每个 Metanet Node 持有数据的密码学唯一副本，以及验证持续数据持有的 Merkle 挑战-响应协议。

双层加密的工作原理如下：所有者的数据已经使用 Method 42 加密（第一层）。然后所有者使用与 Metanet Node 公钥的 ECDH 推导出节点专用的 AES-256-GCM 密钥，并对整个密文再次加密（第二层）。这为每个节点生成唯一的密文，使得节点之间无法共享证明或复制彼此的数据。

Merkle 树建立在双重加密数据的固定大小分片之上。挑战-响应验证使用基于 SHA256 的 Merkle 证明来验证对特定分片的持有。

## 公开 API

### 类型

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

### 常量

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

### 函数

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

## 依赖

- `internal/contract` -- 挑战计算（ComputeChallenge）
- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- ECDH 密钥交换（secp256k1）
- `crypto/sha256` -- SHA256 哈希
- `crypto/aes` -- AES-256-GCM 加密
- `crypto/cipher` -- GCM 模式
- `crypto/rand` -- 随机数生成

## 数据结构

### Merkle 树结构

```
Binary tree with SHA256 leaves and internal nodes:

        root
       /    \
      h01    h23
     / \    / \
    h0  h1 h2  h3    <-- leaf hashes = SHA256(chunk_i)

If odd number of leaves, the last leaf is duplicated.
```

### 双层加密（Double-Layer Encryption）

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

### 证明序列化格式（Proof Serialization Format）

```
proof_data = chunk_index (4 bytes LE)
           || chunk_size (4 bytes LE)
           || chunk_data (chunk_size bytes)
           || num_siblings (4 bytes LE)
           || siblings (num_siblings * 32 bytes)
```

## 错误处理

| 错误 | 条件 |
|-------|-----------|
| `ErrInvalidPrivateKey` | 所有者私钥不是有效的 secp256k1 密钥 |
| `ErrInvalidPublicKey` | 节点公钥不是有效的压缩 secp256k1 密钥 |
| `ErrEmptyData` | 输入数据为空 |
| `ErrChunkIndexOutOfRange` | ChunkIndex >= NumLeaves |
| `ErrMerkleProofInvalid` | Merkle 证明未能通过根验证 |
| `ErrProofHashMismatch` | ComputeProofHash 与 expected_hash_k 不匹配 |
| `ErrChallengeMismatch` | 提交的分片索引与计算出的挑战不匹配 |
| `ErrEncryptionFailed` | AES-256-GCM 加密失败 |

## 安全考量

1. **每节点唯一性**：与每个节点公钥的 ECDH 产生唯一的共享密钥，确保每个节点的密文不同。节点之间无法共谋共享证明。
2. **双层不透明性**：Metanet Node 仅持有外层加密密钥。它可以验证分片完整性（Merkle 证明），但无法解密底层内容。只有所有者（同时知道两个密钥）才能解密。
3. **Merkle 树完整性**：基于 SHA256 的二叉 Merkle 树。分片哈希在双重加密数据上计算，将证明绑定到特定节点的副本。
4. **Nonce 唯一性**：每次 EncryptForNode 调用都会生成新的随机 12 字节 nonce 用于 AES-256-GCM。使用相同密钥重复使用 nonce 将危害机密性。
5. **分片大小权衡**：默认 256KB 的分片大小在证明大小（log2(N) * 32 字节的 Merkle 兄弟节点）和粒度之间取得平衡。更小的分片意味着更精确的验证，但证明更大。
6. **性能说明**：对于 1GB 文件和 N 个节点，双层加密需要 N 次独立的 AES-GCM 处理以及 N 次网络上传。未来可优化为延迟加密或分片级加密。

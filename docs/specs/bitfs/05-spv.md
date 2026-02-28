# 模块规范：libbitfs-go/spv

## 目的

BitFS 的 SPV（简易支付验证，Simplified Payment Verification）轻客户端。在本地存储带有 Merkle 证明的交易，无需下载完整区块即可验证交易是否包含在区块链中。实现核心原则："所有交易信息 + Merkle 证明保存在本地，永不查询区块链。"

设计参考：ConceptDesign #3; SystemDesign 第 7 节; DetailedDesign 中 SPV 验证链相关内容。

## 公共 API

### 常量

```go
const (
    // BlockHeaderSize is the size of a serialized BSV block header in bytes.
    BlockHeaderSize = 80

    // HashSize is the size of a SHA256 hash in bytes.
    HashSize = 32
)
```

### 类型

```go
// BlockHeader represents a BSV block header (80 bytes serialized).
type BlockHeader struct {
    Version    int32  // 4 bytes, little-endian
    PrevBlock  []byte // 32 bytes
    MerkleRoot []byte // 32 bytes
    Timestamp  uint32 // 4 bytes, little-endian (Unix timestamp)
    Bits       uint32 // 4 bytes, little-endian (compact target)
    Nonce      uint32 // 4 bytes, little-endian
    Height     uint32 // Not in raw header; tracked separately
    Hash       []byte // Computed: double-SHA256 of 80-byte header
}

// MerkleProof represents a Merkle inclusion proof for a transaction.
type MerkleProof struct {
    TxID      []byte   // Transaction hash (32 bytes)
    Index     uint32   // Position in the block's transaction list
    Nodes     [][]byte // Merkle branch hashes, bottom-up
    BlockHash []byte   // Block header hash this proof is for
}

// StoredTx represents a transaction stored with its Merkle proof.
type StoredTx struct {
    TxID        []byte       // 32 bytes
    RawTx       []byte       // Full serialized transaction
    Proof       *MerkleProof // Merkle proof (nil = unconfirmed)
    BlockHeight uint32       // 0 = unconfirmed
    Timestamp   uint64       // Time added to store
}

// Network identifies the BSV network for difficulty validation.
type Network int

const (
    Mainnet Network = iota // BSV production network
    Testnet                // BSV test network
    Regtest                // BSV regression test network
)

// Minimum difficulty (nBits) for each network.
const (
    MainnetMinBits uint32 = 0x1d00ffff // Genesis difficulty
    TestnetMinBits uint32 = 0x1d00ffff // Mirrors mainnet genesis
    RegtestMinBits uint32 = 0x207fffff // Standard regtest minimum
)

// ChainVerificationResult holds the output of VerifyHeaderChainWithWork.
type ChainVerificationResult struct {
    // CumulativeWork is the total chain work across all verified headers.
    CumulativeWork *big.Int
}
```

### 接口

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

注意：具体实现（`MemTxStore`、`BoltTxStore`）还提供以下不属于接口的方法：

```go
// PutTxWithPubKey stores a transaction and indexes it by a P_node public key.
// Available on MemTxStore and BoltTxStore (not part of TxStore interface).
func (*MemTxStore) PutTxWithPubKey(tx *StoredTx, pNode []byte) error
func (*BoltTxStore) PutTxWithPubKey(tx *StoredTx, pNode []byte) error
```

### 函数

#### 哈希与序列化

```go
// DoubleHash computes SHA256(SHA256(data)), matching Bitcoin's hash function.
func DoubleHash(data []byte) []byte

// ComputeHeaderHash computes and returns the double-SHA256 hash of a block header.
func ComputeHeaderHash(h *BlockHeader) []byte

// SerializeHeader serializes a BlockHeader to 80 bytes in BSV wire format.
// Layout: version(4) | prevBlock(32) | merkleRoot(32) | timestamp(4) | bits(4) | nonce(4)
func SerializeHeader(h *BlockHeader) []byte

// DeserializeHeader deserializes 80 bytes into a BlockHeader.
// The Hash field is computed from the serialized data.
func DeserializeHeader(data []byte) (*BlockHeader, error)
```

#### Merkle 树

```go
// ComputeMerkleRoot computes the Merkle root from a transaction hash,
// its index position in the block, and the proof branch nodes (bottom-up).
func ComputeMerkleRoot(txHash []byte, index uint32, proofNodes [][]byte) []byte

// VerifyMerkleProof verifies that a transaction is included in a block.
// Recomputes the Merkle path from TxID + proof nodes and checks against
// the expected Merkle root from the block header.
func VerifyMerkleProof(proof *MerkleProof, expectedMerkleRoot []byte) (bool, error)

// BuildMerkleTree builds a full Merkle tree from a list of transaction hashes.
// Returns all tree levels, where level 0 is leaves and the last level is the root.
// Each level is padded by duplicating the last element if odd.
func BuildMerkleTree(txHashes [][]byte) [][]byte

// ComputeMerkleRootFromTxList computes the Merkle root from a list of transaction IDs.
// This is used when you have all transactions in a block and want to verify
// the block header's Merkle root.
func ComputeMerkleRootFromTxList(txIDs [][]byte) []byte
```

#### 难度与工作量

```go
// CompactToTarget converts a Bitcoin "compact" (nBits) representation to a 32-byte
// big-endian target value. Format: 0xEEMMMMMM where EE=exponent, MMMMMM=mantissa.
func CompactToTarget(bits uint32) []byte

// CompactToBig converts a Bitcoin compact (nBits) representation to a big.Int target value.
func CompactToBig(bits uint32) *big.Int

// WorkForTarget computes the expected number of hashes to find a block
// at the given compact difficulty: work = 2^256 / (target + 1).
// Returns zero work for a zero or negative target.
func WorkForTarget(bits uint32) *big.Int

// CumulativeWork computes the total chain work for a sequence of headers.
// Each header contributes WorkForTarget(header.Bits) to the sum.
func CumulativeWork(headers []*BlockHeader) *big.Int

// MinBitsForNetwork returns the minimum nBits (easiest target) for the given network.
func MinBitsForNetwork(net Network) uint32
```

#### PoW 验证

```go
// VerifyPoW checks that a block header's hash meets its stated difficulty target.
// The header hash (interpreted as a big-endian 256-bit integer) must be
// numerically <= the target derived from Bits.
func VerifyPoW(h *BlockHeader) error

// ValidateMinDifficulty checks that a header's nBits meets the minimum
// difficulty for the given network. Higher nBits target value = less work = less security.
func ValidateMinDifficulty(header *BlockHeader, net Network) error

// ValidateDifficultyTransition checks that the difficulty change between two
// consecutive headers does not exceed the allowed bounds (factor of 4).
// This is a simplified check for a light client.
func ValidateDifficultyTransition(prev, curr *BlockHeader) error
```

#### SPV 验证

```go
// VerifyTransaction performs the full SPV verification chain:
//   1. Transaction integrity: TxID is valid (32 bytes), RawTx hash matches TxID
//   2. Merkle proof: tx is included in a block (via VerifyMerkleProof)
//   3. Block header: PoW meets stated difficulty, Merkle root matches
//   4. Chain verification: block header exists in the header store
//
// Note: This function does not check minimum network difficulty. Use
// VerifyTransactionWithNetwork for network-aware difficulty validation.
func VerifyTransaction(tx *StoredTx, headers HeaderStore) error

// VerifyTransactionWithNetwork performs the full SPV verification chain with
// network-aware minimum difficulty validation:
//   1. Transaction integrity: TxID is valid (32 bytes), RawTx hash matches TxID
//   2. Merkle proof: tx is included in a block (via VerifyMerkleProof)
//   3. Block header: PoW meets stated difficulty AND minimum network difficulty
//   4. Merkle root verification: proof matches the header's Merkle root
func VerifyTransactionWithNetwork(tx *StoredTx, headers HeaderStore, net Network) error

// VerifyHeaderChain checks that a sequence of headers forms a valid chain.
// Each header's PrevBlock must match the previous header's Hash, and each
// header's PoW is verified. Headers must be in ascending order (index 0 is earliest).
func VerifyHeaderChain(headers []*BlockHeader) error

// VerifyHeaderChainWithWork verifies a header chain (PoW + linkage + difficulty)
// and returns the cumulative work. Validates PoW, minimum network difficulty,
// difficulty transitions, and PrevBlock linkage for each header.
func VerifyHeaderChainWithWork(headers []*BlockHeader, net Network) (*ChainVerificationResult, error)
```

## 依赖

- `crypto/sha256` -- 双重 SHA-256 哈希
- `encoding/binary` -- 区块头序列化
- `math/big` -- 大整数运算（难度目标、累积工作量）
- `go.etcd.io/bbolt` -- BoltDB 持久化存储（BoltStore 实现）

## 数据结构

### Merkle 证明验证
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

### SPV 验证链
```
1. Raw TX -> deserialize -> extract OP_RETURN
2. MerkleProof(TxHash, nodes) -> computed MerkleRoot
3. BlockHeader.MerkleRoot == computed MerkleRoot
4. BlockHeader is on longest chain (height check)
5. Content: SHA256(SHA256(plaintext)) == key_hash
```

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrMerkleProofInvalid` | 计算出的根与期望值不匹配 |
| `ErrHeaderNotFound` | 区块头不在本地存储中 |
| `ErrTxNotFound` | 交易不在本地存储中 |
| `ErrUnconfirmed` | 交易尚无 Merkle 证明 |
| `ErrChainBroken` | 区块头未形成有效链 |
| `ErrInvalidHeader` | 区块头反序列化或哈希检查失败 |
| `ErrNilParam` | 必需参数为 nil |
| `ErrInvalidTxID` | 交易 ID 不是 32 字节 |
| `ErrDuplicateHeader` | 具有相同哈希的区块头已存在 |
| `ErrDuplicateTx` | 具有相同 TxID 的交易已存在 |
| `ErrInsufficientPoW` | 区块头哈希未达到目标难度 |
| `ErrDifficultyTooLow` | 区块头 nBits 低于网络最低难度 |
| `ErrDifficultyChange` | 相邻区块头难度变化超过允许范围（4 倍） |

## 安全考量

1. **无网络查询**：SPV 模块本身不发起网络调用。区块头和证明由调用者提供（通常来自守护进程或 P2P 同步）。
2. **检查点验证**：初始同步时，硬编码检查点可以在不从创世块下载所有区块头的情况下验证头链。
3. **最长链**：模块信任最长的有效区块头链。日蚀攻击（Eclipse Attack）通过连接多个对等节点来缓解（在网络层处理，不在本模块中）。

# 模块规范：libbitfs/spv

## 目的

BitFS 的 SPV（简易支付验证，Simplified Payment Verification）轻客户端。在本地存储带有 Merkle 证明的交易，无需下载完整区块即可验证交易是否包含在区块链中。实现核心原则："所有交易信息 + Merkle 证明保存在本地，永不查询区块链。"

设计参考：ConceptDesign #3; SystemDesign 第 7 节; DetailedDesign 中 SPV 验证链相关内容。

## 公共 API

### 类型

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

### 函数

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

## 依赖

- `crypto/sha256` -- 双重 SHA-256 哈希
- `encoding/binary` -- 区块头序列化

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

## 安全考量

1. **无网络查询**：SPV 模块本身不发起网络调用。区块头和证明由调用者提供（通常来自守护进程或 P2P 同步）。
2. **检查点验证**：初始同步时，硬编码检查点可以在不从创世块下载所有区块头的情况下验证头链。
3. **最长链**：模块信任最长的有效区块头链。日蚀攻击（Eclipse Attack）通过连接多个对等节点来缓解（在网络层处理，不在本模块中）。

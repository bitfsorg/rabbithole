# 模块规格说明：internal/contract

## 目的

`contract` 包使用标准 Bitcoin Script 在 Metanet Chain 上实现存储合约（StorageDeal）。StorageDeal 是内容所有者（Owner）与 Metanet Node 之间的一对一协议：所有者将 MNT 代币锁定到 N 个 UTXO 输出中（每个挑战周期一个），每个输出包含预计算的期望证明哈希。Metanet Node 必须提交正确的存储证明才能领取每个周期的付款；否则，所有者可以在过期区块后取回代币。

所有合约逻辑使用标准 Bitcoin Script 操作码（OP_IF/ELSE、OP_SHA256、OP_CHECKSIG、OP_CHECKLOCKTIMEVERIFY）——不引入任何自定义操作码或虚拟机。

## 公开 API

### 类型

```go
// StorageDeal represents the parameters of a storage contract between
// an Owner and a Metanet Node.
type StorageDeal struct {
    OwnerPubKey    []byte   // Owner's compressed public key (33 bytes)
    NodePubKey     []byte   // Metanet Node's compressed public key (33 bytes)
    NumPeriods     uint32   // Number of challenge periods (N)
    TokenPerPeriod uint64   // MNT satoshis locked per period
    BlocksPerPeriod uint32  // Number of blocks per challenge period
    StartBlock     uint32   // Block height when contract begins
    MerkleRoot     [32]byte // Merkle root of the node-specific encrypted data
    NumChunks      uint32   // Total number of data chunks
    ExpectedHashes [][32]byte // Pre-computed expected_hash_k for each period k
}

// Challenge represents a deterministic challenge for period k.
type Challenge struct {
    Period      uint32   // Challenge period index (0-based)
    Seed        [32]byte // SHA256(contract_txid || uint32_le(k))
    ChunkIndex  uint32   // Which chunk to prove: uint32(seed[0:4]) % num_chunks
}

// StorageDealUTXO represents a single period's UTXO in the contract.
type StorageDealUTXO struct {
    Period       uint32   // Period index
    Value        uint64   // Locked MNT satoshis
    ExpireBlock  uint32   // CLTV expiry block height
    ExpectedHash [32]byte // Expected SHA256(proof_data)
    Script       []byte   // Locking script
}
```

### 函数

```go
// NewStorageDeal creates a new StorageDeal with all challenge parameters
// pre-computed. The contractTxID is the TxID of the funding transaction
// (must be known or predicted).
//
// The function pre-computes all N expected hashes by:
//   For each period k:
//     challenge_k = SHA256(contractTxID || uint32_le(k))
//     chunk_index = uint32(challenge_k[0:4]) % numChunks
//     expected_hash_k = SHA256(merkle_proof_k || chunks[chunk_index])
func NewStorageDeal(
    ownerPubKey, nodePubKey []byte,
    numPeriods uint32,
    tokenPerPeriod uint64,
    blocksPerPeriod uint32,
    startBlock uint32,
    merkleRoot [32]byte,
    numChunks uint32,
    chunks [][]byte,        // All data chunks (for pre-computing proofs)
    merkleTree *MerkleTree, // Pre-built Merkle tree
    contractTxID [32]byte,
) (*StorageDeal, error)

// ComputeChallenge deterministically computes the challenge for period k
// given a contract TxID.
func ComputeChallenge(contractTxID [32]byte, period uint32, numChunks uint32) *Challenge

// ComputeExpectedHash computes the expected proof hash for a given challenge.
// expected_hash = SHA256(merkle_proof || chunk_data)
func ComputeExpectedHash(
    chunkIndex uint32,
    chunkData []byte,
    merkleProof [][]byte,
) [32]byte

// BuildDealScript constructs the Bitcoin Script for a single period's
// StorageDeal UTXO.
//
// Script:
//   OP_IF
//     <node_pubkey> OP_CHECKSIGVERIFY
//     OP_SHA256 <expected_hash_k> OP_EQUALVERIFY
//     OP_TRUE
//   OP_ELSE
//     <expire_block> OP_CHECKLOCKTIMEVERIFY OP_DROP
//     <owner_pubkey> OP_CHECKSIG
//   OP_ENDIF
func BuildDealScript(
    nodePubKey []byte,
    ownerPubKey []byte,
    expectedHash [32]byte,
    expireBlock uint32,
) ([]byte, error)

// BuildClaimInput constructs the unlocking script for a Metanet Node
// to claim a period's payment.
// Input: <node_sig> <proof_data> OP_TRUE
func BuildClaimInput(nodeSig []byte, proofData []byte) []byte

// BuildRefundInput constructs the unlocking script for an Owner to
// reclaim tokens after expiry.
// Input: <owner_sig> OP_FALSE
func BuildRefundInput(ownerSig []byte) []byte

// BuildStorageDealTx constructs the complete StorageDeal transaction
// with N outputs (one per period), each containing the appropriate
// locking script.
func BuildStorageDealTx(deal *StorageDeal) ([]*StorageDealUTXO, error)

// ValidateDealParams checks that deal parameters are within acceptable bounds.
func ValidateDealParams(deal *StorageDeal) error
```

## 依赖

- `internal/chain` -- 链参数（用于区块高度计算）
- `github.com/bsv-blockchain/go-sdk/transaction` -- 交易构造
- `github.com/bsv-blockchain/go-sdk/script` -- 脚本构造
- `crypto/sha256` -- SHA256 哈希
- `encoding/binary` -- uint32 小端序列化

## 数据结构

### StorageDeal 脚本（每周期 UTXO）

```
OP_IF
    // Node claim path: <node_sig> <proof_data> 1
    <33-byte node_pubkey> OP_CHECKSIGVERIFY
    OP_SHA256 <32-byte expected_hash_k> OP_EQUALVERIFY
    OP_TRUE
OP_ELSE
    // Owner refund path: <owner_sig> 0
    <4-byte expire_block_le> OP_CHECKLOCKTIMEVERIFY OP_DROP
    <33-byte owner_pubkey> OP_CHECKSIG
OP_ENDIF
```

### 确定性挑战推导（Deterministic Challenge Derivation）

```
For period k (0-indexed):
    challenge_seed = SHA256(contract_txid || uint32_le(k))
    chunk_index    = uint32_le(challenge_seed[0:4]) % num_chunks
```

熵来源于 `contract_txid`，它在交易创建前是不可预测的（取决于交易内容的哈希）。一旦合约上链，所有挑战都是确定性的且可公开验证。

### 期望证明哈希（Expected Proof Hash）

```
For period k:
    challenge = ComputeChallenge(contract_txid, k, num_chunks)
    merkle_proof = MerkleProof(tree, challenge.ChunkIndex)
    proof_data = Serialize(merkle_proof) || chunk_data[challenge.ChunkIndex]
    expected_hash_k = SHA256(proof_data)
```

## 错误处理

| 错误 | 条件 |
|-------|-----------|
| `ErrInvalidPubKey` | 公钥不是有效的 33 字节压缩密钥 |
| `ErrZeroPeriods` | NumPeriods 为零 |
| `ErrZeroPayment` | TokenPerPeriod 为零 |
| `ErrInvalidChunkCount` | NumChunks 为零或与数据不匹配 |
| `ErrPeriodTooShort` | BlocksPerPeriod 小于最小值（例如 6 个区块） |
| `ErrChunkIndexOutOfRange` | 计算出的分片索引超过 NumChunks |
| `ErrMerkleRootMismatch` | 计算出的 Merkle 根与合约中的 MerkleRoot 不匹配 |
| `ErrScriptTooLarge` | 生成的脚本超过可接受的大小 |

## 安全考量

1. **确定性挑战**：所有挑战均来源于 `SHA256(contract_txid || period)`。合约 TxID 在创建前不可预测（取决于交易内容哈希），因此 Metanet Node 无法有选择地只存储被挑战的分片。
2. **不使用区块哈希作为随机源**：设计刻意避免使用区块哈希作为熵源，以防止矿工操纵挑战。
3. **CLTV 过期**：所有者退款路径使用带有绝对区块高度的 OP_CHECKLOCKTIMEVERIFY，确保 Metanet Node 在所有者可以取回之前有充足的时间提交证明。
4. **签名绑定**：两条路径都需要密码学签名（OP_CHECKSIG/VERIFY），防止未授权的第三方领取或退款。
5. **预计算**：所有期望哈希在合约创建时嵌入。这确保合约完全自包含且无需外部预言机即可验证。
6. **每周期一个 UTXO**：每个周期的付款隔离在各自的 UTXO 中，因此 Metanet Node 可以独立领取每个周期的付款。一个周期的失败不影响其他周期。

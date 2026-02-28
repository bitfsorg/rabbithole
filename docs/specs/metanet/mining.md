# 模块规格说明：internal/mining

## 目的

`mining` 包实现了 Metanet Chain 的合并挖矿（Merged Mining，AuxPoW），允许 BTC/BSV SHA256 矿工在不增加额外工作量的情况下同时挖掘 Metanet Chain 区块。该包还负责 BSV 锚定——定期将 Metanet Chain 的 Merkle 根发布到 BSV，以防止远程攻击（Long-range Attack）。

合并挖矿是 Metanet Chain 的主要安全机制。矿工在其 BTC/BSV 的 coinbase 交易中嵌入对 Metanet Chain 区块哈希的承诺。Metanet Chain 随后通过验证父链区块头、coinbase 交易和 Merkle 分支来验证辅助工作量证明。

## 公开 API

### 类型

```go
// AuxPoWHeader extends a standard block header with merged mining proof.
type AuxPoWHeader struct {
    // Standard Metanet Chain block header (80 bytes)
    Header chain.BlockHeader

    // Parent chain (BTC/BSV) block header (80 bytes)
    ParentHeader [80]byte

    // Coinbase transaction from the parent chain containing the MNMP marker
    CoinbaseTx []byte

    // Merkle branch proving the coinbase is in the parent block
    CoinbaseBranch [][32]byte

    // Index of coinbase in the parent Merkle tree
    CoinbaseIndex uint32
}

// AnchorTx represents a BSV anchor transaction for Metanet Chain.
type AnchorTx struct {
    Version       uint8    // Protocol version (0x01)
    Flag          [4]byte  // "MNTA"
    StartHeight   uint32   // First Metanet Chain block in range
    EndHeight     uint32   // Last Metanet Chain block in range
    MerkleRoot    [32]byte // Merkle root of the block range
    BlockCount    uint16   // Number of blocks (typically 100)
    PrevAnchorTx  [32]byte // Previous anchor TxID (chain linkage)
}

// DifficultyAdjustment holds the parameters for a difficulty recalculation.
type DifficultyAdjustment struct {
    OldBits         uint32
    ActualTimespan  int64 // Actual seconds for the last 2016 blocks
    ExpectedTimespan int64 // Expected seconds (2016 * 600)
    NewBits         uint32
}
```

### 函数

```go
// ValidateAuxPoW validates the auxiliary proof-of-work for a Metanet Chain block.
// It verifies:
//   1. ParentHeader hash meets Metanet Chain difficulty target
//   2. CoinbaseTx is in ParentHeader's Merkle tree (via CoinbaseBranch)
//   3. CoinbaseTx contains the MNMP magic followed by the Metanet block hash
//   4. The embedded block hash matches SHA256d(Header)
func ValidateAuxPoW(auxpow *AuxPoWHeader, params *chain.Params) error

// FindAuxPoWCommitment scans a coinbase transaction for the MNMP marker
// and returns the committed Metanet Chain block hash.
// Returns nil if no commitment found.
func FindAuxPoWCommitment(coinbaseTx []byte) ([]byte, error)

// BuildCoinbaseCommitment constructs the OP_RETURN output data for
// embedding in a parent chain coinbase transaction.
// Format: OP_RETURN <MNMP> <metanet_block_hash>
func BuildCoinbaseCommitment(blockHash [32]byte) []byte

// VerifyCoinbaseBranch verifies a Merkle branch proving a coinbase
// transaction is included in a block with the given Merkle root.
func VerifyCoinbaseBranch(coinbaseTxHash [32]byte, branch [][32]byte, index uint32, merkleRoot [32]byte) bool

// CalcNextDifficulty computes the new difficulty target for the next
// 2016-block retarget period.
// Uses the Bitcoin algorithm:
//   - Clamp actual timespan to [expectedTimespan/4, expectedTimespan*4]
//   - new_target = old_target * actual_timespan / expected_timespan
func CalcNextDifficulty(oldBits uint32, actualTimespan int64) uint32

// CompactToBig converts a Bitcoin compact target representation to a big.Int.
func CompactToBig(compact uint32) *big.Int

// BigToCompact converts a big.Int target to Bitcoin compact representation.
func BigToCompact(target *big.Int) uint32

// HashMeetsTarget checks whether a block hash meets the given difficulty target.
func HashMeetsTarget(hash [32]byte, bits uint32) bool

// BuildAnchorTx constructs a BSV anchor transaction for a range of
// Metanet Chain blocks.
func BuildAnchorTx(startHeight, endHeight uint32, blockHeaders []chain.BlockHeader, prevAnchorTxID [32]byte) (*AnchorTx, error)

// SerializeAnchorData serializes an AnchorTx into the OP_RETURN payload.
func SerializeAnchorData(anchor *AnchorTx) []byte

// DeserializeAnchorData deserializes an OP_RETURN payload into an AnchorTx.
func DeserializeAnchorData(data []byte) (*AnchorTx, error)

// ValidateAnchorChain validates that a sequence of anchor transactions
// forms a valid chain (each references the previous).
func ValidateAnchorChain(anchors []*AnchorTx) error

// BuildBlockRangeMerkleRoot computes the Merkle root for a range of
// Metanet Chain block headers.
func BuildBlockRangeMerkleRoot(headers []chain.BlockHeader) [32]byte
```

## 依赖

- `internal/chain` -- 区块头类型、链参数
- `github.com/bsv-blockchain/go-sdk/transaction` -- BSV 交易构造（用于锚定）
- `crypto/sha256` -- SHA256 哈希
- `encoding/binary` -- 序列化
- `math/big` -- 目标算术运算

## 数据结构

### Coinbase 承诺格式

```
OP_RETURN <MNMP> <block_hash>

Where:
  OP_RETURN   = 0x6a
  MNMP        = 4 bytes: 0x4d4e4d50 ("MNMP" = Metanet Merged PoW)
  block_hash  = 32 bytes: SHA256d(metanet_chain_block_header)
```

### BSV 锚定 OP_RETURN 格式

```
OP_RETURN <MNTA> <version> <start_height> <end_height> <merkle_root> <block_count> <prev_anchor_txid>

Where:
  OP_RETURN       = 0x6a
  MNTA            = 4 bytes: 0x4d4e5441 ("MNTA" = Metanet Anchor)
  version         = 1 byte: 0x01
  start_height    = 4 bytes LE: first Metanet Chain block height
  end_height      = 4 bytes LE: last Metanet Chain block height
  merkle_root     = 32 bytes: Merkle root of block range
  block_count     = 2 bytes LE: number of blocks
  prev_anchor_txid = 32 bytes: previous BSV anchor TxID
```

### 难度调整算法（Difficulty Adjustment Algorithm）

```
Every 2016 blocks:
  actual_timespan = block[N].timestamp - block[N-2016].timestamp
  clamped_timespan = clamp(actual_timespan, expected/4, expected*4)
  new_target = old_target * clamped_timespan / expected_timespan

Where expected_timespan = 2016 * 600 seconds = 1,209,600 seconds
```

## 错误处理

| 错误 | 条件 |
|-------|-----------|
| `ErrNoAuxPoWCommitment` | Coinbase 不包含 MNMP 标记 |
| `ErrInvalidCoinbaseBranch` | Merkle 分支无法证明 coinbase 包含在区块中 |
| `ErrAuxPoWHashMismatch` | 提交的区块哈希与实际区块头哈希不匹配 |
| `ErrInsufficientAuxPoW` | 父链区块头哈希未达到 Metanet 难度目标 |
| `ErrInvalidParentHeader` | 父链区块头基本验证失败 |
| `ErrAnchorHeightMismatch` | 锚定区块范围不连续 |
| `ErrAnchorChainBroken` | 锚定的 PrevAnchorTx 未引用前一个锚定交易 |
| `ErrDifficultyOverflow` | 计算出的难度超出可表示范围 |

## 安全考量

1. **父链区块头验证**：仅要求父链区块头的哈希满足 Metanet 难度要求——我们不按照父链规则验证父链区块头（那需要 BTC/BSV 全节点）。这是标准的合并挖矿行为。
2. **Coinbase 分支验证**：Merkle 分支必须以密码学方式证明 coinbase 包含在父链区块中，防止伪造 AuxPoW 证明。
3. **MNMP 唯一性**：每个 coinbase 只允许一个 MNMP 承诺。多个承诺表示存在错误。
4. **难度限幅（Difficulty Clamping）**：每个调整周期 4 倍的变化上限，防止难度突变导致链不稳定。
5. **BSV 锚定完整性**：锚定交易形成链式结构（每个引用前一个 TxID），使得在不被检测到的情况下无法插入或删除锚定。
6. **时间劫持缓解（Timejacking Mitigation）**：矿工无法通过父链时间戳操纵 Metanet Chain 难度，因为难度使用的是 Metanet Chain 的区块时间戳，而非父链时间戳。

# 模块规格说明：internal/chain

## 目的

`chain` 包实现了 Metanet Chain 核心——一条与 BSV 同构的侧链（Sidechain），用于 CDN 经济。它使用与 BSV 完全相同的交易格式、Bitcoin Script 引擎和 UTXO 模型，但拥有不同的创世区块和为 Metanet CDN 激励层调优的链参数。

Metanet Chain 的原生代币为 MNT（总供应量 2100 万），遵循与 Bitcoin 完全相同的发行计划（初始奖励 50 个代币，每 210,000 个区块减半）。该链通过与 BTC/BSV 矿工共享的 SHA256 合并挖矿（AuxPoW）来保障安全。

## 公开 API

### 类型

```go
// Params defines the consensus parameters for the Metanet Chain.
type Params struct {
    // Genesis block definition
    GenesisBlock *Block

    // Block parameters
    MaxBlockSize       uint32 // 32MB (33,554,432 bytes)
    TargetBlockTime    time.Duration // 10 minutes
    DifficultyAdjustmentInterval uint32 // 2016 blocks

    // Token economics
    InitialReward      uint64 // 50 MNT in satoshis (5,000,000,000)
    HalvingInterval    uint32 // 210,000 blocks
    MaxTotalSupply     uint64 // 21,000,000 MNT in satoshis (2,100,000,000,000,000)
    SatoshiPerToken    uint64 // 100,000,000

    // Fee parameters
    MinFeePerByte      uint64 // 1 sat/byte

    // BSV anchoring
    AnchorInterval     uint32 // ~100 blocks
    AnchorMagic        []byte // "MNTA"

    // Merged mining
    AuxPowMagic        []byte // "MNMP"
}

// Block represents a Metanet Chain block.
type Block struct {
    Header     BlockHeader
    AuxPoW     *AuxPoW     // nil for genesis block
    Txs        []*tx.Tx    // BSV-format transactions
}

// BlockHeader is the standard 80-byte block header (BSV-identical).
type BlockHeader struct {
    Version    uint32
    PrevHash   [32]byte
    MerkleRoot [32]byte
    Timestamp  uint32
    Bits       uint32
    Nonce      uint32
}

// AuxPoW contains the auxiliary proof-of-work data for merged mining.
type AuxPoW struct {
    ParentHeader    [80]byte   // BTC/BSV block header
    CoinbaseTx      []byte     // Coinbase transaction containing MNMP marker
    CoinbaseBranch  [][32]byte // Merkle branch proving coinbase is in parent block
}
```

### 函数

```go
// MainNetParams returns the Metanet Chain mainnet consensus parameters.
func MainNetParams() *Params

// TestNetParams returns the Metanet Chain testnet consensus parameters.
func TestNetParams() *Params

// GenesisBlock returns the Metanet Chain genesis block.
func GenesisBlock() *Block

// GenesisBlockHeader returns the genesis block header.
func GenesisBlockHeader() *BlockHeader

// GenesisBlockHash returns the double-SHA256 hash of the genesis block header.
func GenesisBlockHash() [32]byte

// BlockReward computes the block reward for a given block height,
// accounting for halving schedule.
func BlockReward(height uint32) uint64

// TotalSupplyAtHeight computes the cumulative MNT supply mined up to height.
func TotalSupplyAtHeight(height uint32) uint64

// HalvingEra returns the halving era (0-indexed) for a given block height.
func HalvingEra(height uint32) uint32

// HashBlockHeader computes the double-SHA256 hash of a block header.
func HashBlockHeader(h *BlockHeader) [32]byte

// SerializeBlockHeader serializes a block header to its 80-byte form.
func SerializeBlockHeader(h *BlockHeader) [80]byte

// DeserializeBlockHeader deserializes an 80-byte block header.
func DeserializeBlockHeader(data [80]byte) *BlockHeader

// ValidateBlockHeader checks that a block header meets consensus rules:
// - PrevHash references a known block
// - Timestamp is within acceptable range
// - Bits matches expected difficulty
// - PoW hash meets the target
func ValidateBlockHeader(h *BlockHeader, params *Params) error
```

## 依赖

- `github.com/bsv-blockchain/go-sdk/transaction` -- BSV 交易类型
- `crypto/sha256` -- SHA256 哈希
- `encoding/binary` -- 小端序列化
- `math/big` -- 难度目标算术运算

## 数据结构

### 创世区块（Genesis Block）

```
Timestamp:    TBD (mainnet launch)
Message:      "Metanet: Decentralized CDN on Bitcoin"
Reward:       50 MNT to foundation multisig (3-of-5)
PrevHash:     0x0000...0000 (all zeros)
Bits:         Initial difficulty target
Nonce:        TBD (mined at launch)
```

### 减半计划（Halving Schedule）

| 纪元 | 区块范围 | 每块奖励 | 纪元总量 |
|-----|-------------|--------------|-----------|
| 0 | 0 - 209,999 | 50.0 MNT | 10,500,000 MNT |
| 1 | 210,000 - 419,999 | 25.0 MNT | 5,250,000 MNT |
| 2 | 420,000 - 629,999 | 12.5 MNT | 2,625,000 MNT |
| 3 | 630,000 - 839,999 | 6.25 MNT | 1,312,500 MNT |
| ... | ... | ... | ... |
| 32+ | 6,720,000+ | 0 MNT | 0 MNT |

### 难度编码（Difficulty Encoding）

`Bits` 字段使用 Bitcoin 的紧凑目标表示法（与 BSV 相同）。难度每 2016 个区块调整一次，以维持约 10 分钟的平均出块时间。

## 错误处理

| 错误 | 条件 |
|-------|-----------|
| `ErrInvalidPrevHash` | PrevHash 未引用已知区块 |
| `ErrTimestampTooOld` | 区块时间戳早于最近 11 个区块的中位数 |
| `ErrTimestampTooNew` | 区块时间戳超过未来 2 小时 |
| `ErrInvalidDifficulty` | Bits 与预期难度不匹配 |
| `ErrInsufficientPoW` | 区块哈希未达到难度目标 |
| `ErrBlockTooLarge` | 序列化后的区块超过 MaxBlockSize |
| `ErrInvalidMerkleRoot` | MerkleRoot 与交易 Merkle 树不匹配 |
| `ErrDuplicateTx` | 区块包含重复的交易 ID |

## 安全考量

1. **双重 SHA256（Double-SHA256）**：所有区块哈希使用 SHA256d（SHA256(SHA256(x))），与 BSV 保持一致。
2. **时间戳验证**：区块的时间戳必须晚于最近 11 个区块的中位数，且不超过未来 2 小时，以防止时间戳操纵。
3. **难度完整性**：难度调整使用与 Bitcoin 完全相同的算法（每 2016 个区块调整，每个周期最大变化限制在 4 倍）。
4. **创世区块不可变性**：创世区块是编译时常量；其哈希锚定整条链。
5. **BSV 锚定**：定期将 Merkle 根锚定到 BSV，防止对 Metanet Chain 的远程攻击（Long-range Attack）。

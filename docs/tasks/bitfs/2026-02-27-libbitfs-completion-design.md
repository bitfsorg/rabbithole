# libbitfs-go 功能完善设计

> 日期: 2026-02-27
> 范围: libbitfs-go 全部未实现功能
> 前置: 协议正确性审计 (2026-02-26), 原子交易构建器 (2026-02-27)

## 一、现状分析

libbitfs-go 10 个包已实现核心功能 (8,100 LOC, 799 tests, 0 open audit findings)。
以下功能在设计文档中定义但未实现:

| 功能 | 设计成熟度 | 当前代码状态 |
|------|-----------|-------------|
| TLV 新字段 S/D | 完备 | Node 有部分 struct 字段, parser 无 S/D |
| Metadata map | 完备 | struct 字段存在, 无 S/D |
| 内容压缩 | 完备 | Compression 字段已 S/D, 无实际压缩逻辑 |
| 内容分片 | 完备 | 无 |
| Rabin 签名 | 完备 | 无 |
| RevShare/ISO | 完备 (最成熟) | revshare/ 空目录 |
| CLTV 时间锁 | 完备 | CltvHeight 字段已 S/D, 无验证逻辑 |
| Version Log | 草案 (20%) | 无 |
| Share List | 草案 (10%) | 无 |
| ACL/群签名 | 部分 (40%) | 无 |

## 二、TLV Tag 冲突解决

### 问题

设计文档字段编号 = tag 字节值 (field N → tag 0xN)。Fields 32-38 的自然 tag (0x20-0x26) 与已部署的 Anchor 节点 tags 冲突:

```
已用: 0x01-0x1B (主字段), 0x20-0x26 (Anchor 专用)
冲突: field 32 (share_list) → 0x20 = tagTreeRootPNode
```

### 解决方案

Fields 30-31 保留自然映射; Fields 32+ 跳过 Anchor 范围:

```
范围 0x01-0x1B: 主字段 (已部署, 不变)
范围 0x1C-0x1D: 保留 (原 field 28-29, 已废弃)
范围 0x1E-0x1F: 扩展字段 (field 30-31)
范围 0x20-0x26: Anchor 专用 (已部署, 不变)
范围 0x27-0x30: 扩展字段 (field 32+, 跳过 Anchor 范围)
```

完整映射表:

| 设计 Field | Tag | 名称 | 类型 | 说明 |
|-----------|------|------|------|------|
| 30 | 0x1E | metadata | bytes (sub-TLV) | map<string,string> |
| 31 | 0x1F | version_log | bytes(33) | P_node → 版本记录节点 |
| 32 | 0x27 | share_list | bytes(33) | P_node → 共享列表节点 |
| 33 | 0x28 | chunk_index | uint32 | 分片序号 (0-based) |
| 34 | 0x29 | total_chunks | uint32 | 总分片数 (0=非分片) |
| 35 | 0x2A | recombination_hash | bytes(32) | SHA256(chunk₀‖chunk₁‖...) |
| 36 | 0x2B | rabin_signature | bytes | (S, U) 序列化 |
| 37 | 0x2C | rabin_pubkey | bytes | 模数 n |
| 38 | 0x2D | registry_txid | bytes(32) | Registry UTXO 所在 TxID |
| 39 | 0x2E | registry_vout | uint32 | Registry UTXO 输出索引 |
| 40 | 0x2F | iso_config | bytes (sub-TLV) | ISO 配置 |
| 41 | 0x30 | acl_ref | bytes | 群签名公钥哈希或 ACL TxID |

需同步更新设计文档 (2-SystemDesign.zh.md 第 337 行注释) 注明 tag 字节不再等于 field 编号 (fields 32+ 跳过 Anchor 范围)。

### Metadata 序列化格式

```
metadata TLV (tag 0x1E):
  length = total bytes of all entries
  value = repeated { key_len(2B LE) + key(UTF-8) + val_len(2B LE) + val(UTF-8) }
```

限制: 单个 key 最大 255 字节, 单个 value 最大 65535 字节, 总条目数最大 256。

### ISOConfig 序列化格式

```
iso_config TLV (tag 0x2F):
  value = total_shares(8B LE uint64) +
          price_per_share(8B LE uint64) +
          creator_addr(20B P2PKH hash) +
          status(1B: 0=NONE, 1=OPEN, 2=PARTIAL, 3=CLOSED)
```

## 三、四阶段实施

### Phase 1: 协议层补全

**目标**: TLV 二进制格式完整, 所有设计文档中的字段可序列化/反序列化。

**1.1 Node struct 扩展** (metanet/node.go)

新增字段:
```go
// 扩展字段 (Phase 1)
VersionLog        []byte            // P_node → 版本记录节点 (33 bytes)
ShareList         []byte            // P_node → 共享列表节点 (33 bytes)
ChunkIndex        uint32            // 分片序号 (0-based)
TotalChunks       uint32            // 总分片数 (0=非分片)
RecombinationHash []byte            // SHA256(chunk₀‖chunk₁‖...) (32 bytes)
RabinSignature    []byte            // Rabin 签名 (S, U) 序列化
RabinPubKey       []byte            // Rabin 公钥 n
RegistryTxID      []byte            // Registry UTXO TxID (32 bytes)
RegistryVout      uint32            // Registry UTXO 输出索引
ISOConfig         *ISOConfig        // ISO 配置 (nil = 无 ISO)
ACLRef            []byte            // ACL 引用
```

**1.2 ISOConfig 类型** (metanet/node.go)

```go
type ISOStatus uint8

const (
    ISOStatusNone    ISOStatus = 0
    ISOStatusOpen    ISOStatus = 1
    ISOStatusPartial ISOStatus = 2
    ISOStatusClosed  ISOStatus = 3
)

type ISOConfig struct {
    TotalShares   uint64
    PricePerShare uint64
    CreatorAddr   []byte    // 20 bytes P2PKH hash
    Status        ISOStatus
}
```

**1.3 CompressionScheme 枚举** (metanet/node.go)

```go
const (
    CompressNone int32 = 0
    CompressLZW  int32 = 1
    CompressGZIP int32 = 2
    CompressZSTD int32 = 3
)
```

**1.4 parser.go 更新**

- 新增 11 个 tag 常量 (0x1E-0x30, 跳过 0x20-0x26)
- SerializePayload: 新增 11 个字段的序列化
- deserializePayload: 新增 11 个字段的反序列化
- Metadata 专用 S/D 函数 (serializeMetadata / deserializeMetadata)
- ISOConfig 专用 S/D 函数 (serializeISOConfig / deserializeISOConfig)

**1.5 CLTV 验证函数** (metanet/cltv.go, 新文件)

```go
type CLTVResult int

const (
    CLTVAllowed        CLTVResult = 0
    CLTVDenied         CLTVResult = 1
)

// CheckCLTVAccess checks if content is accessible at the given block height.
func CheckCLTVAccess(node *Node, currentHeight uint32) CLTVResult
```

**1.6 测试**

- parser round-trip tests: 每个新字段一个, 含边界值
- Metadata S/D: 空 map, 单条目, 多条目, UTF-8 key/value, 最大长度
- ISOConfig S/D: 各 status 值, 边界值
- CLTV: height=0 (无限制), height>current (拒绝), height<=current (允许)
- 向后兼容: 旧格式 (无新字段) 的 payload 仍能正确解析

**预估**: ~400 LOC code + ~400 LOC tests

---

### Phase 2: 内容处理

**目标**: 压缩、分片、Rabin 签名三个功能独立可用。

**2.1 内容压缩** (新文件: storage/compress.go)

```go
// Compress compresses data using the specified scheme.
func Compress(data []byte, scheme int32) ([]byte, error)

// Decompress decompresses data using the specified scheme.
func Decompress(data []byte, scheme int32) ([]byte, error)
```

实现:
- CompressNone: 直接返回
- CompressLZW: compress/lzw (stdlib)
- CompressGZIP: compress/gzip (stdlib)
- CompressZSTD: github.com/klauspost/compress/zstd (外部依赖)

**设计决策**: ZSTD 需要外部依赖。如果不想引入新依赖, 可以只支持 LZW + GZIP (stdlib), ZSTD 返回 ErrUnsupportedCompression。

建议: 先只实现 LZW + GZIP, ZSTD 后续按需引入。

**2.2 内容分片** (新文件: storage/chunk.go)

```go
const DefaultChunkSize = 1 << 20 // 1MB

// SplitIntoChunks splits ciphertext into fixed-size chunks.
func SplitIntoChunks(ciphertext []byte, chunkSize int) [][]byte

// RecombineChunks concatenates chunks and verifies recombination hash.
func RecombineChunks(chunks [][]byte, expectedHash []byte) ([]byte, error)

// ComputeRecombinationHash computes SHA256(chunk₀ ‖ chunk₁ ‖ ...).
func ComputeRecombinationHash(chunks [][]byte) []byte
```

**流程** (与设计文档一致):

上传: plaintext → key_hash=SHA256(SHA256(plaintext)) → compress → encrypt(AES-GCM) → split → 每个 chunk 独立 DataTx → 元节点记录 content_txids + total_chunks + recombination_hash

下载: 按 content_txids 获取 chunks → RecombineChunks (验证 recombination_hash) → decrypt → decompress → 验证 key_hash

**2.3 Rabin 签名** (新文件: method42/rabin.go)

```go
// RabinKeyPair holds a Rabin signature key pair.
type RabinKeyPair struct {
    P *big.Int // private prime p ≡ 3 (mod 4)
    Q *big.Int // private prime q ≡ 3 (mod 4)
    N *big.Int // public modulus n = p * q
}

// GenerateRabinKey generates a Rabin key pair with primes of bitSize bits each.
func GenerateRabinKey(bitSize int) (*RabinKeyPair, error)

// RabinSign signs a message, returning (S, U).
func RabinSign(key *RabinKeyPair, message []byte) (S *big.Int, U []byte, err error)

// RabinVerify verifies a Rabin signature using only the public modulus n.
func RabinVerify(n *big.Int, message []byte, S *big.Int, U []byte) bool

// SerializeRabinSignature encodes (S, U) for TLV storage.
func SerializeRabinSignature(S *big.Int, U []byte) []byte

// DeserializeRabinSignature decodes (S, U) from TLV.
func DeserializeRabinSignature(data []byte) (S *big.Int, U []byte, err error)

// SerializeRabinPubKey encodes modulus n for TLV storage.
func SerializeRabinPubKey(n *big.Int) []byte

// DeserializeRabinPubKey decodes modulus n from TLV.
func DeserializeRabinPubKey(data []byte) (*big.Int, error)
```

**算法** (设计文档 3-DetailedDesign §6-B):
1. 密钥生成: 生成两个 ≥1024-bit 素数 p, q (p≡3 mod 4, q≡3 mod 4), n=p×q
2. 签名: 找 padding U 使 H=SHA256(message‖U) 是 mod n 的二次剩余, S = H^{(n-p-q+5)/8} mod n
3. 验证: S² mod n == SHA256(message‖U)

**无外部依赖**: 使用 crypto/rand + math/big (stdlib)。

**2.4 测试**

- 压缩: 各 scheme round-trip, 空数据, 大数据 (1MB+), 无效 scheme
- 分片: 1 chunk (小文件), 多 chunk, 精确倍数大小, 非整除大小, recombination hash 验证/篡改检测
- Rabin: key generation (1024-bit, 2048-bit), sign/verify round-trip, 篡改消息检测, S/D round-trip, 性能 benchmark

**预估**: ~600 LOC code + ~600 LOC tests

---

### Phase 3: 经济系统

**目标**: revshare 包完整实现, 覆盖 Registry/Share/ISO Pool 的编解码和 4 种核心交易构建。

**3.1 包结构** (libbitfs-go/revshare/)

```
revshare/
├── types.go        // 核心类型定义
├── registry.go     // Registry UTXO 编解码
├── share.go        // Share UTXO 编解码
├── pool.go         // ISO Pool UTXO 编解码
├── distribute.go   // 收益分配算法
├── tx.go           // 4 种交易构建器
├── validate.go     // 验证规则
└── errors.go       // 错误定义
```

**3.2 核心类型** (types.go)

```go
// RevShareEntry represents a shareholder's record in the registry.
type RevShareEntry struct {
    Address [20]byte // P2PKH address hash
    Share   uint64   // Number of shares held
}

// RegistryState represents the current state of a revenue share registry.
type RegistryState struct {
    NodeID      [32]byte         // SHA256(P_node || TxID) of the Metanet node
    TotalShares uint64           // Total shares issued
    Entries     []RevShareEntry  // Current shareholders
    ModeFlags   uint8            // bit 0: ISO active, bit 1: locked
}

// ShareData represents the data embedded in a Share UTXO.
type ShareData struct {
    NodeID [32]byte // Bound Metanet node
    Amount uint64   // Number of shares
}

// ISOPoolState represents the state of an ISO pool UTXO.
type ISOPoolState struct {
    NodeID          [32]byte // Bound Metanet node
    RemainingShares uint64   // Unsold shares
    PricePerShare   uint64   // Price in satoshis
    CreatorAddr     [20]byte // Creator's P2PKH address
}

// Distribution represents a single payout in revenue distribution.
type Distribution struct {
    Address [20]byte
    Amount  uint64
}
```

**3.3 Registry UTXO** (registry.go)

编码格式 (设计文档 3-DetailedDesign §十四-B-B):
```
node_id(32B) + total_shares(8B BE) + num_entries(4B BE) +
  entries[]: address(20B) + share(8B BE) × N +
mode_flags(1B)
总大小: 45 + 24 × N bytes
```

```go
func SerializeRegistry(state *RegistryState) ([]byte, error)
func DeserializeRegistry(data []byte) (*RegistryState, error)
func (s *RegistryState) Validate() error
func (s *RegistryState) FindEntry(addr [20]byte) (int, *RevShareEntry)
func (s *RegistryState) IsISOActive() bool
func (s *RegistryState) IsLocked() bool
```

**3.4 Share UTXO** (share.go)

编码格式: `node_id(32B) + share_amount(8B BE)`, 总 40 bytes。

```go
func SerializeShare(data *ShareData) []byte
func DeserializeShare(raw []byte) (*ShareData, error)
```

**3.5 ISO Pool UTXO** (pool.go)

编码格式: `node_id(32B) + remaining_shares(8B BE) + price_per_share(8B BE) + creator_addr(20B)`, 总 68 bytes。

```go
func SerializeISOPool(state *ISOPoolState) []byte
func DeserializeISOPool(raw []byte) (*ISOPoolState, error)
```

**3.6 收益分配** (distribute.go)

```go
// DistributeRevenue calculates per-shareholder payouts.
// Last entry gets the remainder to avoid rounding loss.
func DistributeRevenue(totalPayment uint64, entries []RevShareEntry, totalShares uint64) ([]Distribution, error)
```

算法来自设计文档 3-DetailedDesign §十四-B-E:
- 前 N-1 个股东: amount = totalPayment × share / totalShares (整除)
- 最后一个股东: totalPayment - sum(前 N-1 个)
- 边界: totalPayment < len(entries) 时报错 (无法每人至少 1 sat)

**3.7 交易构建器** (tx.go)

4 种核心交易, 全部基于 tx.MutationBatch 模式 (不直接构建 tx, 返回输入/输出描述):

```go
// ISOGenesisParams defines parameters for creating an ISO.
type ISOGenesisParams struct {
    NodeID       [32]byte
    TotalShares  uint64
    ReserveShares uint64  // Creator keeps these
    PricePerShare uint64
    CreatorAddr  [20]byte
    FeeUTXO      tx.UTXO
}

// BuildISOGenesis builds the ISO genesis transaction outputs.
func BuildISOGenesis(params *ISOGenesisParams) (*ISOGenesisTx, error)

// BuildISOBuy builds an ISO purchase transaction.
func BuildISOBuy(params *ISOBuyParams) (*ISOBuyTx, error)

// BuildShareTransfer builds a share transfer/split transaction.
func BuildShareTransfer(params *ShareTransferParams) (*ShareTransferTx, error)

// BuildPurchaseDistribution builds a purchase+distribution transaction.
func BuildPurchaseDistribution(params *PurchaseDistributionParams) (*PurchaseDistributionTx, error)
```

**注意**: Covenant 脚本构建是 Phase 3 的核心复杂度。需要实现:
- OP_PUSH_TX (sighash preimage introspection)
- Share Covenant Script 模板
- Registry Covenant Script 模板 (MODE_TRANSFER + MODE_DISTRIBUTE)
- ISO Pool Covenant Script 模板

Covenant 脚本使用 go-sdk 的 `script.Script` 构建。BSV 支持 OP_PUSH_TX 模式 (通过 OP_CHECKSIGPREIMAGE), 这是 Covenant 的基础。

**3.8 验证** (validate.go)

```go
// ValidateShareConservation checks sum(output shares) == sum(input shares).
func ValidateShareConservation(inputs []ShareData, outputs []ShareData) error

// ValidateRegistryConsistency checks registry entries match share UTXOs.
func ValidateRegistryConsistency(registry *RegistryState, shares []ShareData) error

// ValidateDistribution checks payout amounts match registry proportions.
func ValidateDistribution(distributions []Distribution, entries []RevShareEntry, totalPayment, totalShares uint64) error
```

**3.9 测试**

- Registry S/D round-trip (空/单/多条目, 最大条目数)
- Share S/D round-trip
- ISO Pool S/D round-trip
- DistributeRevenue: 精确整除, 有余数, 最小金额, 单股东, 多股东
- ShareConservation: 转让/拆分/合并/不守恒
- ISO Genesis → Buy → Close 全流程
- 份额转让 → 购买分配全流程

**预估**: ~1,200 LOC code + ~800 LOC tests

---

### Phase 4: 访问控制

**目标**: 补全 ACL/群签名设计, 然后实现。Version Log 和 Share List 同步完成。

**4.1 需补充的设计** (修改 design/bitfs/2-SystemDesign.zh.md 和 3-DetailedDesign.zh.md)

#### A. ACL / 群签名

需要补充:
1. **BLS12-381 库选择**: 推荐 `github.com/kilic/bls12-381` (纯 Go, 无 CGO)
2. **密钥格式**: BLS public key 48 bytes, signature 96 bytes, serialization
3. **凭证结构**: Group credential = BLS signature over (member_pubkey, attributes, expiry)
4. **凭证颁发协议**: Owner generates group key → issues signed credentials → distributes via Method 42 ECDH
5. **凭证撤销**: 通过 SelfUpdate 更新 ACL 引用 → 新 Registry 不含被撤销成员
6. **ACL 存储**: ACLRef 指向一个 Metanet 节点, 该节点的 payload 包含 group public key + 成员列表

#### B. Version Log

需要补充:
1. **版本节点结构**: 独立 Metanet 节点, payload 包含 `prev_version_txid` (链表)
2. **遍历算法**: 从 version_log P_node 获取最新版本节点 → 沿 prev_version_txid 回溯
3. **剪枝策略**: 不剪枝 (区块链数据不可删除), 客户端可选择只获取最近 N 个版本
4. **创建时机**: 每次 SelfUpdate 时, 如果 version_log 字段非空, 自动创建新版本记录节点

#### C. Share List

需要补充:
1. **与 ACL 的关系**: Share List 是 ACL 的简化版 (无签名验证, 仅地址列表)
2. **节点结构**: 独立 Metanet 节点, payload 包含 repeated P2PKH 地址
3. **查询**: Daemon 检查请求者地址是否在列表中
4. **更新**: Owner 通过 SelfUpdate 修改列表节点

**4.2 实现** (待设计评审后)

新文件:
- metanet/acl.go — ACL 解析和验证
- metanet/version.go — 版本链遍历
- method42/bls.go — BLS12-381 封装 (如果引入 BLS 依赖)

**预估**: ~800 LOC code + ~700 LOC tests + 设计文档更新

---

## 四、依赖变更

| 依赖 | 阶段 | 用途 | 可选? |
|------|------|------|-------|
| (无新依赖) | Phase 1 | — | — |
| (无新依赖) | Phase 2 | LZW/GZIP=stdlib, Rabin=math/big | — |
| (无新依赖) | Phase 3 | go-sdk script 构建 | — |
| github.com/kilic/bls12-381 | Phase 4 | 群签名 | 是, 可推迟 |

Phase 1-3 零新依赖。Phase 4 的 BLS 库在设计评审时决定。

## 五、设计文档同步更新

每个 Phase 开始前, 先更新 design/bitfs/ 下的设计文档:

**Phase 1**: 更新 2-SystemDesign.zh.md §4 — 添加 tag 冲突解决说明, 更新字段编号注释
**Phase 2**: 无需更新 (设计已完备)
**Phase 3**: 无需更新 (设计已完备)
**Phase 4**: 补充 2-SystemDesign.zh.md §22 (ACL) 和 3-DetailedDesign.zh.md (Version Log, Share List, BLS)

## 六、验收标准

1. 所有新字段可 round-trip 序列化/反序列化 (parser tests)
2. 向后兼容: 旧格式 payload 正常解析 (未知 tag 跳过)
3. 压缩 round-trip: LZW, GZIP 各 scheme 正确
4. 分片: 1-chunk 和 multi-chunk 的 split → recombine → verify 正确
5. Rabin: sign → verify 正确, 篡改检测正确
6. RevShare: DistributeRevenue 精度正确 (last-gets-remainder)
7. Registry/Share/ISOPool S/D round-trip 正确
8. 全部测试 pass with `-race`
9. golangci-lint 零 issue
10. 设计文档与实现一致

# Protocol Finalization + Library Maturity + Test Coverage — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Finalize all BitFS protocol specs, expand revshare test coverage to 60+, and add shell integration tests for all 22 commands.

**Architecture:** Three independent workstreams executed in parallel via subagents. Workstream A updates spec markdown files in `bitfs/docs/spec/`. Workstream B adds tests in `libbitfs-go/revshare/`. Workstream C adds integration tests in `bitfs/integration/`.

**Tech Stack:** Go 1.25.6, testify v1.11.1, Markdown specs

---

## Workstream A: Protocol Spec Finalization

### Task 1: Update 03-metanet.md — Extended Node Fields + NodeTypeAnchor + TLV Reference

**Files:**
- Modify: `bitfs/docs/spec/03-metanet.md`

**Reference code:** `libbitfs-go/metanet/node.go:128-185`, `libbitfs-go/metanet/parser.go:10-63`

**Step 1: Add NodeTypeAnchor to type enum (after line 21)**

Add `NodeTypeAnchor NodeType = 3` to the NodeType constants:

```go
const (
    NodeTypeFile   NodeType = 0
    NodeTypeDir    NodeType = 1
    NodeTypeLink   NodeType = 2
    NodeTypeAnchor NodeType = 3 // Git commit anchor
)
```

**Step 2: Add AccessLevel enum (replace raw int32 at line 63)**

Replace `Access int32 // 0=PRIVATE, 1=FREE, 2=PAID` with:

```go
// AccessLevel represents content access control modes.
type AccessLevel int32

const (
    AccessPrivate AccessLevel = 0 // Only owner can decrypt
    AccessFree    AccessLevel = 1 // Anyone can decrypt (D_node = scalar 1)
    AccessPaid    AccessLevel = 2 // Requires payment via x402
)
```

And change the Node field to `Access AccessLevel`.

**Step 3: Add extended fields to Node struct (after line 83, before closing brace)**

Add these fields after `NetworkName string`:

```go
    // --- Extended Fields (Protocol Layer Completion) ---

    // Content chunking
    ChunkIndex        uint32   // Chunk index (0-based) for chunked content
    TotalChunks       uint32   // Total number of chunks (0 = not chunked)
    RecombinationHash []byte   // SHA256(chunk₀ ‖ chunk₁ ‖ ...), 32 bytes

    // Cryptographic signatures
    RabinSignature    []byte   // Rabin signature (S, U) serialized
    RabinPubKey       []byte   // Rabin public key modulus n

    // Version history
    VersionLog        []byte   // P_node pointing to version log node (33 bytes)

    // Revenue sharing
    ShareList         []byte   // P_node pointing to share list node (33 bytes)
    RegistryTxID      []byte   // Registry UTXO TxID (32 bytes)
    RegistryVout      uint32   // Registry UTXO output index
    ISO               *ISOConfig // Initial Share Offering config (nil = no ISO)

    // Access control
    ACLRef            []byte   // ACL reference: group pubkey hash or ACL TxID
```

**Step 4: Add ISOConfig type (before Node struct)**

```go
// ISOStatus represents the lifecycle state of an Initial Share Offering.
type ISOStatus uint8

const (
    ISOStatusNone    ISOStatus = 0
    ISOStatusOpen    ISOStatus = 1
    ISOStatusPartial ISOStatus = 2 // Some shares sold
    ISOStatusClosed  ISOStatus = 3
)

// ISOConfig holds Initial Share Offering parameters.
// Serialized size: 37 bytes (8 + 8 + 20 + 1).
type ISOConfig struct {
    TotalShares   uint64    // Total shares offered
    PricePerShare uint64    // Price per share in satoshis
    CreatorAddr   []byte    // Creator's P2PKH address (20 bytes)
    Status        ISOStatus // Current ISO state
}
```

**Step 5: Add Anchor-specific fields to Node struct**

```go
    // --- Anchor-Specific Fields (NodeTypeAnchor only) ---
    TreeRootPNode    []byte   // Root directory's P_node (33 bytes)
    TreeRootTxID     []byte   // Root directory's latest TxID (32 bytes)
    ParentAnchorTxID [][]byte // Parent anchor TxIDs (merge commits have multiple)
    Author           string   // Git commit author
    CommitMessage    string   // Git commit message
    GitCommitSHA     []byte   // Git commit SHA for cross-reference (20 bytes)
    FileMode         uint32   // Git file mode (100644, 100755, 120000)
```

**Step 6: Add complete TLV Tag Reference Table (replace vague section at lines 170-172)**

Replace the TLV section with a full reference table:

```markdown
### TLV 协议参考

所有字段使用 Tag-Length-Value 编码：Tag (1 byte) + Length (LEB128 varint) + Value。
多字节整数在 TLV payload 中使用 **小端序** (Little-Endian)。
零值字段通常被省略（不序列化）。未知 Tag 在反序列化时被跳过（前向兼容）。

#### 核心字段 (0x01-0x1B)

| Tag  | 名称 | 值类型 | 长度 | 说明 |
|------|------|--------|------|------|
| 0x01 | Version | uint32 | 4 | 协议版本 |
| 0x02 | Type | int32 | 4 | NodeType (FILE=0, DIR=1, LINK=2, ANCHOR=3) |
| 0x03 | Op | int32 | 4 | OpType (CREATE=0, UPDATE=1, DELETE=2) |
| 0x04 | MimeType | string | var | MIME 类型 (e.g. "text/plain") |
| 0x05 | FileSize | uint64 | 8 | 明文文件大小 (字节) |
| 0x06 | KeyHash | bytes | 32 | SHA256(SHA256(plaintext)) |
| 0x07 | Access | int32 | 4 | AccessLevel (PRIVATE=0, FREE=1, PAID=2) |
| 0x08 | PricePerKB | uint64 | 8 | 价格 (satoshis/KB) |
| 0x09 | LinkTarget | bytes | 33 | 软链接目标 P_node |
| 0x0A | LinkType | int32 | 4 | SOFT=0, SOFT_REMOTE=1 |
| 0x0B | Timestamp | uint64 | 8 | Unix 时间戳 (秒) |
| 0x0C | Parent | bytes | 33 | 父目录 P_node |
| 0x0D | Index | uint32 | 4 | 在父目录中的索引 |
| 0x0E | ChildEntry | bytes | var | 目录子项 (可重复) |
| 0x0F | NextChildIndex | uint32 | 4 | 下一个可分配的子索引 |
| 0x10 | Domain | string | var | DNSLink 绑定域名 |
| 0x11 | Keywords | string | var | 搜索关键字 |
| 0x12 | Description | string | var | 描述文本 |
| 0x13 | Encrypted | bool | 1 | true = EncPayload 已加密 |
| 0x14 | OnChain | bool | 1 | true = 内容在链上 |
| 0x15 | ContentTxID | bytes | 32 | 数据交易 TxID (可重复) |
| 0x16 | Compression | int32 | 4 | 压缩方案 (NONE=0, LZW=1, GZIP=2, ZSTD=3) |
| 0x17 | CltvHeight | uint32 | 4 | CLTV 时间锁区块高度 |
| 0x18 | RevenueShare | uint32 | 4 | 分成比例 (basis points) |
| 0x19 | NetworkName | string | var | 网络名称 ("mainnet", "testnet", "regtest") |
| 0x1A | MerkleRoot | bytes | 32 | 目录 Children 的 Merkle root |
| 0x1B | EncPayload | bytes | var | PRIVATE 模式加密负载 |

#### 元数据 (0x1E)

| Tag  | 名称 | 值类型 | 编码 |
|------|------|--------|------|
| 0x1E | Metadata | map | 子 TLV: 每项 keyLen(2 LE) + key + valLen(2 LE) + value |

#### 扩展字段 (0x1F, 0x27-0x30)

| Tag  | 名称 | 值类型 | 长度 | 说明 |
|------|------|--------|------|------|
| 0x1F | VersionLog | bytes | 33 | 版本日志节点的 P_node |
| 0x27 | ShareList | bytes | 33 | 份额列表节点的 P_node |
| 0x28 | ChunkIndex | uint32 | 4 | 分片索引 (0-based) |
| 0x29 | TotalChunks | uint32 | 4 | 总分片数 |
| 0x2A | RecombinationHash | bytes | 32 | SHA256(chunk₀ ‖ chunk₁ ‖ ...) |
| 0x2B | RabinSignature | bytes | var | Rabin 签名 (S, U) |
| 0x2C | RabinPubKey | bytes | var | Rabin 公钥模数 n |
| 0x2D | RegistryTxID | bytes | 32 | 收入分成注册表 UTXO TxID |
| 0x2E | RegistryVout | uint32 | 4 | 注册表 UTXO 输出索引 |
| 0x2F | ISOConfig | bytes | 37 | ISO 配置 (子 TLV) |
| 0x30 | ACLRef | bytes | var | ACL 引用 |

#### Anchor 节点字段 (0x20-0x26)

| Tag  | 名称 | 值类型 | 长度 | 说明 |
|------|------|--------|------|------|
| 0x20 | TreeRootPNode | bytes | 33 | 根目录 P_node |
| 0x21 | TreeRootTxID | bytes | 32 | 根目录最新 TxID |
| 0x22 | ParentAnchorTxID | bytes | 32 | 父锚点 TxID (可重复, merge commit) |
| 0x23 | Author | string | var | Git 提交作者 |
| 0x24 | CommitMessage | string | var | Git 提交消息 |
| 0x25 | GitCommitSHA | bytes | 20 | Git commit SHA |
| 0x26 | FileMode | uint32 | 4 | Git file mode |

#### ChildEntry 子 TLV 编码

每个 ChildEntry (tag 0x0E) 的值按以下格式编码：
```
Index(4 LE) + NameLen(2 LE) + Name + Type(4 LE) + PubKeyLen(1) + PubKey + Hardened(1)
```

#### ISOConfig 子 TLV 编码 (tag 0x2F)

固定 37 字节：
```
TotalShares(8 LE) + PricePerShare(8 LE) + CreatorAddr(20) + Status(1)
```
```

**Step 7: Add new error entries**

Add to error table:
```markdown
| `ErrInvalidAnchor` | Anchor 节点缺少必需字段 (TreeRootPNode) |
| `ErrISOInactive` | ISO 操作但 ISO 未激活 |
| `ErrACLDenied` | ACL 检查拒绝访问 |
```

**Step 8: Add Anchor security note**

Add to security section:
```markdown
5. **Anchor 节点安全**：Anchor 的 ParentAnchorTxID 可有多个值（merge commit），客户端应验证所有父锚点的有效性。
6. **ISO 状态机**：ISO 状态只能单向转换 (None→Open→Partial→Closed)，防止回滚攻击。
```

**Step 9: Commit**

```bash
git add bitfs/docs/spec/03-metanet.md
git commit -m "spec: add extended fields, NodeTypeAnchor, and TLV reference to 03-metanet"
```

---

### Task 2: Update 01-method42.md — Rabin Signature Scheme

**Files:**
- Modify: `bitfs/docs/spec/01-method42.md`

**Reference code:** `libbitfs-go/method42/rabin.go:1-164`

**Step 1: Add Rabin types after EncryptResult (after line 36)**

```go
// RabinKeyPair holds the private and public keys for Rabin signatures.
type RabinKeyPair struct {
    P *big.Int // Private Blum prime p ≡ 3 (mod 4)
    Q *big.Int // Private Blum prime q ≡ 3 (mod 4)
    N *big.Int // Public modulus n = p * q
}
```

**Step 2: Add Rabin functions after ComputeCapsuleHash (after line 94)**

```go
// --- Rabin Signature Scheme ---
// Rabin signatures provide content authenticity using the computational
// difficulty of finding modular square roots without factoring n.

// GenerateRabinKey generates a new Rabin key pair.
// bitSize is the bit length of each prime (e.g., 1024 for 2048-bit modulus).
// Both primes are Blum primes: p ≡ 3 (mod 4), q ≡ 3 (mod 4).
func GenerateRabinKey(bitSize int) (*RabinKeyPair, error)

// RabinSign signs a message using the Rabin signature scheme.
// Finds padding U such that H(message || U) is a quadratic residue mod N.
// H = SHA256 mapped to [0, N).
// Returns (S, U) where S² ≡ H(message || U) (mod N).
// Uses CRT for efficient square root computation.
func RabinSign(key *RabinKeyPair, message []byte) (sig *big.Int, pad []byte, err error)

// RabinVerify verifies a Rabin signature.
// Checks: sig² ≡ H(message || pad) (mod n).
// Requires only the public modulus n.
func RabinVerify(n *big.Int, message []byte, sig *big.Int, pad []byte) bool

// SerializeRabinSignature encodes (sig, pad) to binary.
// Format: sigLen(4 BE) || sig(big-endian) || padLen(4 BE) || pad
func SerializeRabinSignature(sig *big.Int, pad []byte) []byte

// DeserializeRabinSignature decodes binary to (sig, pad).
func DeserializeRabinSignature(data []byte) (*big.Int, []byte, error)

// SerializeRabinPubKey encodes the public modulus n.
// Returns n.Bytes() (big-endian).
func SerializeRabinPubKey(n *big.Int) []byte

// DeserializeRabinPubKey decodes the public modulus.
func DeserializeRabinPubKey(data []byte) (*big.Int, error)
```

**Step 3: Add Rabin data structure section (after HKDF 参数 section)**

```markdown
### Rabin 签名方案

Rabin 签名基于二次剩余的计算困难性。无需椭圆曲线——安全性归约到整数分解。

#### 密钥生成
- 生成两个 Blum 素数 p, q：p ≡ 3 (mod 4), q ≡ 3 (mod 4)
- 公钥: n = p × q
- 私钥: (p, q)

#### 签名
1. 尝试随机填充 U (最多 256 次)
2. 计算 h = SHA256(message || U) mod n
3. 检查 h 是否为模 p 和模 q 的二次剩余 (Legendre 符号)
4. 若是，通过 CRT 计算平方根: S = sqrt(h) mod n
5. 返回 (S, U)

#### 验证
- 验证: S² mod n == SHA256(message || pad) mod n
- 仅需公钥 n

#### 序列化格式

**签名**: `sigLen(4 BE) || sig(big-endian bytes) || padLen(4 BE) || pad`
**公钥**: `n.Bytes()` (大端序)
```

**Step 4: Add Rabin errors**

```markdown
| `ErrRabinKeyGeneration` | 素数生成失败 |
| `ErrRabinNoQuadraticResidue` | 256 次填充尝试均未找到二次剩余 |
| `ErrInvalidRabinSignature` | 签名反序列化失败 |
```

**Step 5: Add Rabin security note**

```markdown
7. **Rabin 安全性**：2048-bit 模数 (1024-bit 素数) 提供 ~112-bit 安全强度。签名伪造等价于分解 n。
8. **填充重试**：签名需要找到使哈希为二次剩余的填充。期望值约 4 次尝试，最坏 256 次。
```

**Step 6: Commit**

```bash
git add bitfs/docs/spec/01-method42.md
git commit -m "spec: add Rabin signature scheme to 01-method42"
```

---

### Task 3: Update 06-storage.md — Compression + Chunking APIs

**Files:**
- Modify: `bitfs/docs/spec/06-storage.md`

**Reference code:** `libbitfs-go/storage/compress.go:12-83`, `libbitfs-go/storage/chunk.go:8-53`

**Step 1: Add CompressionScheme constants (before Store interface, after 目的 section)**

```go
// CompressionScheme identifies the compression algorithm.
type CompressionScheme int32

const (
    CompressNone CompressionScheme = 0 // No compression
    CompressLZW  CompressionScheme = 1 // compress/lzw (LSB byte order)
    CompressGZIP CompressionScheme = 2 // compress/gzip
    CompressZSTD CompressionScheme = 3 // [PLANNED] Zstandard
)
```

**Step 2: Add Compression functions (after NewFileStore)**

```go
// --- Content Processing ---

// Compress compresses data using the specified scheme.
// CompressNone returns data unchanged.
// CompressZSTD is defined but not yet implemented (returns ErrUnsupportedCompression).
func Compress(data []byte, scheme CompressionScheme) ([]byte, error)

// Decompress decompresses data using the specified scheme.
// Mirrors Compress: CompressNone returns data unchanged.
func Decompress(data []byte, scheme CompressionScheme) ([]byte, error)
```

**Step 3: Add Chunking functions**

```go
// --- Content Chunking ---

const DefaultChunkSize = 1 << 20 // 1 MB

// SplitIntoChunks splits data into fixed-size chunks.
// Last chunk may be smaller. Returns nil if data is empty.
func SplitIntoChunks(data []byte, chunkSize int) [][]byte

// ComputeRecombinationHash computes SHA256(chunk₀ ‖ chunk₁ ‖ ...).
// This hash verifies that all chunks recombine to the original content.
func ComputeRecombinationHash(chunks [][]byte) []byte

// RecombineChunks concatenates chunks and verifies the recombination hash.
// Returns ErrRecombinationHashMismatch if hash doesn't match.
func RecombineChunks(chunks [][]byte, expectedHash []byte) ([]byte, error)
```

**Step 4: Add new error entries**

```markdown
| `ErrUnsupportedCompression` | 不支持的压缩方案 (ZSTD) |
| `ErrDecompressionFailed` | 解压缩失败 (损坏数据) |
| `ErrRecombinationHashMismatch` | 分片重组后哈希不匹配 |
```

**Step 5: Add data structure section for chunking**

```markdown
### 内容分片模型

大文件 (>1MB) 拆分为固定大小分片，每个分片独立存储和传输。
Node 的 `ContentTxIDs` 字段 (TLV tag 0x15, 可重复) 记录每个分片的链上 TxID。

```
原始文件 (3.5 MB)
  ├── chunk_0 (1 MB)  → ContentTxID[0]
  ├── chunk_1 (1 MB)  → ContentTxID[1]
  ├── chunk_2 (1 MB)  → ContentTxID[2]
  └── chunk_3 (0.5 MB) → ContentTxID[3]

Node fields:
  TotalChunks = 4
  ChunkIndex  = (per-chunk node only, 0-based)
  RecombinationHash = SHA256(chunk_0 || chunk_1 || chunk_2 || chunk_3)
  Compression = scheme applied BEFORE chunking
```

处理流程：plaintext → Compress → SplitIntoChunks → Encrypt each → Store
还原流程：Retrieve → Decrypt each → RecombineChunks (verify hash) → Decompress
```

**Step 6: Commit**

```bash
git add bitfs/docs/spec/06-storage.md
git commit -m "spec: add compression and chunking APIs to 06-storage"
```

---

### Task 4: Create 12-revshare.md — Revenue Sharing Spec

**Files:**
- Create: `bitfs/docs/spec/12-revshare.md`

**Reference code:** All files in `libbitfs-go/revshare/`

**Step 1: Write complete spec**

Create the file with these sections (full content — write all of it):

```markdown
# 模块规范：libbitfs/revshare

## 目的

BitFS 的收入分成系统。允许文件所有者将付费内容的检索收入分配给多个股东。支持初始份额发行 (ISO)、份额转让、以及收入自动分配。

## 公共 API

### 类型

[Include all types from types.go verbatim: RevShareEntry, RegistryState, ShareData, ISOPoolState, Distribution, with all methods (IsISOActive, IsLocked, FindEntry)]

### 函数

[Include all exported functions from registry.go, share.go, pool.go, distribute.go, validate.go with complete signatures and doc comments]

## 依赖

- `encoding/binary` -- 大端序序列化
- `fmt` -- 错误包装

## 数据结构

### Registry UTXO 二进制格式

固定布局，所有多字节字段使用 **大端序** (Big-Endian)：

```
Header (44 bytes):
  NodeID       [32]byte  // SHA256(P_node || TxID)
  TotalShares  uint64    // 总份额数
  NumEntries   uint32    // 股东数量

Entries (28 bytes × NumEntries):
  Address      [20]byte  // P2PKH 地址哈希
  Share        uint64    // 持有份额数

Trailer (1 byte):
  ModeFlags    uint8     // bit 0: ISO active, bit 1: locked
```

最小大小: 44 + 0×28 + 1 = 45 bytes (无股东)
典型大小: 44 + N×28 + 1 bytes

### Share UTXO 二进制格式

固定 40 bytes:
```
NodeID  [32]byte  // 绑定的 Metanet 节点
Amount  uint64    // 份额数量
```

### ISO Pool UTXO 二进制格式

固定 68 bytes:
```
NodeID          [32]byte  // 绑定的 Metanet 节点
RemainingShares uint64    // 未售份额
PricePerShare   uint64    // 每份价格 (satoshis)
CreatorAddr     [20]byte  // 创建者 P2PKH 地址
```

### 收入分配算法

```
DistributeRevenue(totalPayment, entries, totalShares):
  distributed = 0
  for i = 0 to len(entries)-2:
    amount[i] = totalPayment × entries[i].Share / totalShares
    distributed += amount[i]
  amount[last] = totalPayment - distributed  // 余数归末位
  return amounts
```

保证: sum(amounts) == totalPayment (无精度损失)。

### ModeFlags 位定义

| Bit | 名称 | 说明 |
|-----|------|------|
| 0 | ISO_ACTIVE | ISO 池处于活跃状态 |
| 1 | LOCKED | 份额转让被锁定 |

### ISO 生命周期

```
None → Open (创建 ISO 池, 设定总份额和价格)
Open → Partial (首笔购买后)
Partial → Closed (份额售罄 或 创建者手动关闭)
Closed → (终态, 不可回滚)
```

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrInvalidRegistryData` | Registry UTXO 数据格式错误或截断 |
| `ErrInvalidShareData` | Share UTXO 数据不是 40 字节 |
| `ErrInvalidISOPoolData` | ISO Pool UTXO 数据不是 68 字节 |
| `ErrShareConservationViolation` | 输入份额总和 ≠ 输出份额总和 |
| `ErrInsufficientPayment` | totalPayment == 0 |
| `ErrNoEntries` | 注册表无股东 |
| `ErrZeroShares` | 份额数量为零 |
| `ErrZeroTotalShares` | 总份额为零 |
| `ErrEntryNotFound` | 地址不在注册表中 |
| `ErrRegistryLocked` | 注册表已锁定，拒绝份额转让 |

## 安全考量

1. **份额守恒**: ValidateShareConservation 确保份额不被凭空创造或销毁。输入总和必须等于输出总和。
2. **余数归末位**: 整数除法余数给最后一个股东，避免精度损失。最大误差: len(entries)-1 satoshis。
3. **锁定机制**: LOCKED 标志防止 ISO 进行中的份额转让，确保 ISO 池的份额计数一致。
4. **大端序**: 选择大端序 (网络字节序) 以便跨平台序列化一致性。
```

**Step 2: Commit**

```bash
git add bitfs/docs/spec/12-revshare.md
git commit -m "spec: add 12-revshare.md revenue sharing specification"
```

---

### Task 5: Create 13-network.md — Network Abstraction Spec

**Files:**
- Create: `bitfs/docs/spec/13-network.md`

**Reference code:** `libbitfs-go/network/service.go:5-36`, `libbitfs-go/network/rpc_blockchain.go`

**Step 1: Write complete spec**

Write a spec covering:
- BlockchainService interface (all 9 methods with doc comments)
- UTXO, TxStatus, MerkleProof types
- RPCClient implementation notes (JSON-RPC over HTTP, uses standard bitcoind RPC)
- SPVClient implementation notes (bridges to spv package for header/proof verification)
- Network presets: mainnet/testnet/regtest/teratestnet (port, host, genesis block hash)
- NetworkConfig type

Follow the same format as other specs (目的, 公共 API, 依赖, 数据结构, 错误处理, 安全考量).

Key errors: `ErrNodeUnreachable`, `ErrTxNotFound`, `ErrBroadcastFailed`, `ErrInvalidNetwork`

Security: TLS for mainnet/testnet, timeout defaults (30s connect, 60s read), retry with exponential backoff.

**Step 2: Commit**

```bash
git add bitfs/docs/spec/13-network.md
git commit -m "spec: add 13-network.md blockchain service specification"
```

---

### Task 6: Update 10-cmd-bitfs.md — Sales + Shell Commands

**Files:**
- Modify: `bitfs/docs/spec/10-cmd-bitfs.md`

**Step 1: Verify `sales` is already listed**

Check line 33 — `bitfs sales [path]` is already in the spec. Good.

**Step 2: Add shell command reference (after 子命令 section, before 全局标志)**

Add a subsection documenting all 22 shell commands:

```markdown
### Shell 命令

`bitfs shell` 进入 FTP 风格交互式 REPL，支持以下命令：

| 命令 | 语法 | 说明 |
|------|------|------|
| help | `help` | 显示命令帮助 |
| quit/exit | `quit` / `exit` | 退出 shell |
| pwd | `pwd` | 显示当前远程目录 |
| cd | `cd <path>` | 切换远程目录 |
| lcd | `lcd <path>` | 切换本地目录 |
| ls | `ls [path]` | 列出目录内容 |
| mkdir | `mkdir <path>` | 创建目录 |
| put | `put <local> <remote> [access]` | 上传文件 (access: free/private/paid) |
| rm | `rm [-r] <path>` | 删除文件或目录 (-r 递归) |
| mv | `mv <src> <dst>` | 移动/重命名 |
| cp | `cp <src> <dst>` | 复制文件 |
| link | `link [-s] <target> <name>` | 创建链接 (-s 软链接, 默认硬链接) |
| sell | `sell <path> <price> [--recursive]` | 设置价格 |
| cat | `cat <path> [--force]` | 显示文件内容 |
| get | `get <remote> [local]` | 下载文件 |
| mget | `mget <dir> [local-dir]` | 批量下载 |
| mput | `mput <dir> [remote-dir]` | 批量上传 |
| publish | `publish [domain]` | 发布/列出 DNSLink 绑定 |
| unpublish | `unpublish <domain>` | 解绑域名 |
| encrypt | `encrypt <path>` | Free → Private |
| decrypt | `decrypt <path>` | Private → Free |
| sales | `sales` | 查看销售记录 |

Shell 特性：
- Tab 补全（命令名 + 路径）
- 命令历史 (`~/.bitfs/shell_history`, 0600 权限, 500 行上限)
- 路径解析：支持 `.` / `..` / 绝对路径 / 相对路径
```

**Step 3: Commit**

```bash
git add bitfs/docs/spec/10-cmd-bitfs.md
git commit -m "spec: add shell command reference to 10-cmd-bitfs"
```

---

### Task 7: Update 11-cmd-btools.md — New Flags

**Files:**
- Modify: `bitfs/docs/spec/11-cmd-btools.md`

**Step 1: Add `--version N` to bget options**

In the bget section, confirm `--version N` is listed (it should be at line 58). Verify the description says "下载指定版本".

**Step 2: Add `--versions` to bstat options**

In the bstat section, confirm `--versions` is listed (line 76). Verify description says "显示所有版本".

**Step 3: Add `--offline` to all tools**

Verify that `--offline` appears in the shared implementation section (line 119). It should already be listed there. If not, add it.

**Step 4: Add `--keyword` to bls**

Verify `--keyword <kw>` is in bls options (line 18). It should already be there.

**Step 5: Verify completeness against code**

Cross-check all flags in each tool's main.go against what's in the spec. The spec appears to already have these flags based on the current content. Run a diff if unsure.

**Step 6: Commit (only if changes were needed)**

```bash
git add bitfs/docs/spec/11-cmd-btools.md
git commit -m "spec: verify btools flags match implementation"
```

---

### Task 8: Unify Access type across all specs

**Files:**
- Modify: `bitfs/docs/spec/01-method42.md`

**Step 1: Rename Access to AccessLevel in method42 spec**

At lines 17-24, change:
```go
// Access represents the three access control modes...
type Access int
```
to:
```go
// AccessLevel represents the three access control modes for encrypted content.
type AccessLevel int32
```

And update the constants from `Access` to `AccessLevel`.

**Step 2: Update function signatures**

Replace all `access Access` parameters with `access AccessLevel` in the function signatures (lines 66, 73, 82).

**Step 3: Commit**

```bash
git add bitfs/docs/spec/01-method42.md
git commit -m "spec: unify Access to AccessLevel across all specs"
```

---

## Workstream B: libbitfs-go Maturity

### Task 9: Expand revshare tests from 12 to 60+

**Files:**
- Modify: `libbitfs-go/revshare/revshare_test.go`

**Reference:** Existing tests in the file. Follow the same table-driven pattern with `makeAddr()` and `makeNodeID()` helpers.

**Step 1: Add Registry serialization edge case tests**

Add these test cases to existing `TestSerializeRegistry_RoundTrip` or as new functions:

```go
func TestSerializeRegistry_ZeroEntries(t *testing.T) {
	state := &RegistryState{
		NodeID: makeNodeID(0x01), TotalShares: 0,
		Entries: []RevShareEntry{}, ModeFlags: 0,
	}
	data, err := SerializeRegistry(state)
	require.NoError(t, err)
	assert.Len(t, data, 45) // header(44) + trailer(1)

	decoded, err := DeserializeRegistry(data)
	require.NoError(t, err)
	assert.Empty(t, decoded.Entries)
}

func TestSerializeRegistry_MaxEntries(t *testing.T) {
	entries := make([]RevShareEntry, 100)
	for i := range entries {
		entries[i] = RevShareEntry{Address: makeAddr(byte(i)), Share: 100}
	}
	state := &RegistryState{
		NodeID: makeNodeID(0x01), TotalShares: 10000,
		Entries: entries, ModeFlags: 0,
	}
	data, err := SerializeRegistry(state)
	require.NoError(t, err)
	assert.Len(t, data, 44+28*100+1)

	decoded, err := DeserializeRegistry(data)
	require.NoError(t, err)
	assert.Len(t, decoded.Entries, 100)
}

func TestDeserializeRegistry_TruncatedEntries(t *testing.T) {
	// Header claims 2 entries but only 1 is present
	state := &RegistryState{
		NodeID: makeNodeID(0x01), TotalShares: 10000,
		Entries: []RevShareEntry{
			{Address: makeAddr(0xAA), Share: 5000},
			{Address: makeAddr(0xBB), Share: 5000},
		},
		ModeFlags: 0,
	}
	data, err := SerializeRegistry(state)
	require.NoError(t, err)
	// Truncate: remove last entry (28 bytes) + trailer (1 byte)
	_, err = DeserializeRegistry(data[:len(data)-29])
	assert.ErrorIs(t, err, ErrInvalidRegistryData)
}

func TestDeserializeRegistry_ExactMinimum(t *testing.T) {
	// Exactly 45 bytes: header(44) + trailer(1), 0 entries
	data := make([]byte, 45)
	decoded, err := DeserializeRegistry(data)
	require.NoError(t, err)
	assert.Empty(t, decoded.Entries)
	assert.Equal(t, uint64(0), decoded.TotalShares)
}

func TestSerializeRegistry_AllModeFlags(t *testing.T) {
	tests := []struct {
		name  string
		flags uint8
		iso   bool
		lock  bool
	}{
		{"none", 0x00, false, false},
		{"iso only", 0x01, true, false},
		{"locked only", 0x02, false, true},
		{"iso+locked", 0x03, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &RegistryState{
				NodeID: makeNodeID(0x01), TotalShares: 100,
				Entries:   []RevShareEntry{{Address: makeAddr(0xAA), Share: 100}},
				ModeFlags: tt.flags,
			}
			data, err := SerializeRegistry(state)
			require.NoError(t, err)

			decoded, err := DeserializeRegistry(data)
			require.NoError(t, err)
			assert.Equal(t, tt.iso, decoded.IsISOActive())
			assert.Equal(t, tt.lock, decoded.IsLocked())
		})
	}
}
```

**Step 2: Add Distribution edge case tests**

```go
func TestDistributeRevenue_LargePayment(t *testing.T) {
	entries := []RevShareEntry{
		{Address: makeAddr(0xAA), Share: 5000},
		{Address: makeAddr(0xBB), Share: 5000},
	}
	// Max uint64 / 2 to avoid overflow in the multiplication
	dists, err := DistributeRevenue(1_000_000_000, entries, 10000)
	require.NoError(t, err)
	assert.Equal(t, uint64(500_000_000), dists[0].Amount)
	assert.Equal(t, uint64(500_000_000), dists[1].Amount)
}

func TestDistributeRevenue_SingleSatoshi(t *testing.T) {
	entries := []RevShareEntry{
		{Address: makeAddr(0xAA), Share: 3333},
		{Address: makeAddr(0xBB), Share: 3333},
		{Address: makeAddr(0xCC), Share: 3334},
	}
	// 1 satoshi cannot be split — all goes to last entry
	dists, err := DistributeRevenue(1, entries, 10000)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), dists[0].Amount)
	assert.Equal(t, uint64(0), dists[1].Amount)
	assert.Equal(t, uint64(1), dists[2].Amount)
}

func TestDistributeRevenue_UnevenSplit(t *testing.T) {
	entries := []RevShareEntry{
		{Address: makeAddr(0xAA), Share: 1},
		{Address: makeAddr(0xBB), Share: 1},
		{Address: makeAddr(0xCC), Share: 1},
	}
	// 100 / 3 = 33 remainder 1 → last gets 34
	dists, err := DistributeRevenue(100, entries, 3)
	require.NoError(t, err)
	assert.Equal(t, uint64(33), dists[0].Amount)
	assert.Equal(t, uint64(33), dists[1].Amount)
	assert.Equal(t, uint64(34), dists[2].Amount)
}

func TestDistributeRevenue_TinyShareVsLarge(t *testing.T) {
	entries := []RevShareEntry{
		{Address: makeAddr(0xAA), Share: 1},     // 0.01%
		{Address: makeAddr(0xBB), Share: 9999},   // 99.99%
	}
	dists, err := DistributeRevenue(10000, entries, 10000)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), dists[0].Amount)
	assert.Equal(t, uint64(9999), dists[1].Amount)
}

func TestDistributeRevenue_ManyEntries(t *testing.T) {
	const n = 1000
	entries := make([]RevShareEntry, n)
	for i := range entries {
		entries[i] = RevShareEntry{Address: makeAddr(byte(i % 256)), Share: 10}
	}
	dists, err := DistributeRevenue(10000, entries, uint64(n*10))
	require.NoError(t, err)
	require.Len(t, dists, n)

	var total uint64
	for _, d := range dists {
		total += d.Amount
	}
	assert.Equal(t, uint64(10000), total)
}

func TestDistributeRevenue_EmptyEntries(t *testing.T) {
	_, err := DistributeRevenue(100, []RevShareEntry{}, 10000)
	assert.ErrorIs(t, err, ErrNoEntries)
}
```

**Step 3: Add Share round-trip edge cases**

```go
func TestSerializeShare_ZeroAmount(t *testing.T) {
	share := &ShareData{NodeID: makeNodeID(0x01), Amount: 0}
	data := SerializeShare(share)
	decoded, err := DeserializeShare(data)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), decoded.Amount)
}

func TestSerializeShare_MaxAmount(t *testing.T) {
	share := &ShareData{NodeID: makeNodeID(0xFF), Amount: ^uint64(0)}
	data := SerializeShare(share)
	decoded, err := DeserializeShare(data)
	require.NoError(t, err)
	assert.Equal(t, ^uint64(0), decoded.Amount)
}

func TestDeserializeShare_TooLong(t *testing.T) {
	// 41 bytes — should fail (expects exactly 40)
	_, err := DeserializeShare(make([]byte, 41))
	assert.ErrorIs(t, err, ErrInvalidShareData)
}
```

**Step 4: Add ISOPool edge cases**

```go
func TestSerializeISOPool_ZeroValues(t *testing.T) {
	pool := &ISOPoolState{}
	data := SerializeISOPool(pool)
	assert.Len(t, data, 68)

	decoded, err := DeserializeISOPool(data)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), decoded.RemainingShares)
	assert.Equal(t, uint64(0), decoded.PricePerShare)
}

func TestSerializeISOPool_MaxValues(t *testing.T) {
	pool := &ISOPoolState{
		NodeID:          makeNodeID(0xFF),
		RemainingShares: ^uint64(0),
		PricePerShare:   ^uint64(0),
		CreatorAddr:     makeAddr(0xFF),
	}
	data := SerializeISOPool(pool)
	decoded, err := DeserializeISOPool(data)
	require.NoError(t, err)
	assert.Equal(t, ^uint64(0), decoded.RemainingShares)
	assert.Equal(t, ^uint64(0), decoded.PricePerShare)
}

func TestDeserializeISOPool_TooShort(t *testing.T) {
	_, err := DeserializeISOPool(make([]byte, 67))
	assert.ErrorIs(t, err, ErrInvalidISOPoolData)
}

func TestDeserializeISOPool_TooLong(t *testing.T) {
	_, err := DeserializeISOPool(make([]byte, 69))
	assert.ErrorIs(t, err, ErrInvalidISOPoolData)
}
```

**Step 5: Add Validation edge cases**

```go
func TestValidateShareConservation_Empty(t *testing.T) {
	// Both empty — valid (0 == 0)
	assert.NoError(t, ValidateShareConservation(nil, nil))
}

func TestValidateShareConservation_ManyToMany(t *testing.T) {
	nodeID := makeNodeID(0x01)
	inputs := []ShareData{
		{NodeID: nodeID, Amount: 1000},
		{NodeID: nodeID, Amount: 2000},
		{NodeID: nodeID, Amount: 3000},
	}
	outputs := []ShareData{
		{NodeID: nodeID, Amount: 2500},
		{NodeID: nodeID, Amount: 3500},
	}
	assert.NoError(t, ValidateShareConservation(inputs, outputs))
}

func TestValidateDistribution_LengthMismatch(t *testing.T) {
	entries := []RevShareEntry{{Address: makeAddr(0xAA), Share: 10000}}
	dists := []Distribution{
		{Address: makeAddr(0xAA), Amount: 5000},
		{Address: makeAddr(0xBB), Amount: 5000},
	}
	assert.Error(t, ValidateDistribution(dists, entries, 10000, 10000))
}

func TestRegistryState_FindEntry_NotFound(t *testing.T) {
	state := &RegistryState{
		Entries: []RevShareEntry{
			{Address: makeAddr(0xAA), Share: 10000},
		},
	}
	idx, entry := state.FindEntry(makeAddr(0xFF))
	assert.Equal(t, -1, idx)
	assert.Nil(t, entry)
}

func TestRegistryState_FindEntry_First(t *testing.T) {
	state := &RegistryState{
		Entries: []RevShareEntry{
			{Address: makeAddr(0xAA), Share: 5000},
			{Address: makeAddr(0xBB), Share: 5000},
		},
	}
	idx, entry := state.FindEntry(makeAddr(0xAA))
	assert.Equal(t, 0, idx)
	assert.Equal(t, uint64(5000), entry.Share)
}

func TestRegistryState_IsISOActive_AllCombinations(t *testing.T) {
	for flags := uint8(0); flags < 4; flags++ {
		state := &RegistryState{ModeFlags: flags}
		assert.Equal(t, flags&0x01 != 0, state.IsISOActive(), "flags=%d", flags)
		assert.Equal(t, flags&0x02 != 0, state.IsLocked(), "flags=%d", flags)
	}
}
```

**Step 6: Run tests**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./revshare/ -v -count=1
```

Expected: All tests pass (old 12 + new ~50 = ~62 tests).

**Step 7: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
git add revshare/revshare_test.go
git commit -m "test(revshare): expand test coverage from 12 to 60+ cases"
```

---

### Task 10: Fix bare return err in SPV boltstore

**Files:**
- Modify: `libbitfs-go/spv/boltstore.go`

**Step 1: Find and fix bare return err statements**

Search for `return err` (without `fmt.Errorf` wrapping) in `spv/boltstore.go`. Wrap each with context:

```go
// Before:
return err
// After:
return fmt.Errorf("boltstore: <operation context>: %w", err)
```

Do the same for any bare `return err` found in `spv/` or `revshare/` packages.

**Step 2: Run tests**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./spv/ -v -count=1
```

**Step 3: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
git add spv/boltstore.go
git commit -m "fix(spv): wrap bare return err with context in boltstore"
```

---

## Workstream C: Shell Integration Tests

### Task 11: Create shell integration tests — Navigation + File Operations

**Files:**
- Create: `bitfs/integration/shell_commands_test.go`

**Reference:** `bitfs/integration/engine_workflow_test.go` and `bitfs/integration/engine_helpers_test.go` for the setup pattern.

**Step 1: Write test file with build tag and imports**

```go
//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

**Step 2: Write navigation tests**

These test the engine operations that the shell dispatches to. Since shell is a REPL wrapper around engine, we test the engine paths that shell uses (path resolution, cwd tracking, etc.):

```go
func TestShell_MkdirAndLs(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create root
	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	// Create nested dirs
	_, err = eng.Mkdir("/photos")
	require.NoError(t, err)
	_, err = eng.Mkdir("/docs")
	require.NoError(t, err)

	// Verify root has 2 children
	root := eng.State.FindNodeByPath("/")
	require.NotNil(t, root)
	assert.Len(t, root.Children, 2)
}

func TestShell_PutAndCat(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	content := []byte("Hello BitFS from shell test")
	localFile := createTempFile(t, content)

	_, err = eng.PutFile(localFile, "/hello.txt", "free")
	require.NoError(t, err)

	// Verify node exists
	node := eng.State.FindNodeByPath("/hello.txt")
	require.NotNil(t, node)
	assert.Equal(t, uint64(len(content)), node.FileSize)

	// Cat should decrypt and return content
	plaintext, err := eng.Cat("/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, content, plaintext)
}

func TestShell_PutWithAccessModes(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	tests := []struct {
		name   string
		path   string
		access string
	}{
		{"free", "/free.txt", "free"},
		{"private", "/private.txt", "private"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte("content for " + tt.name)
			localFile := createTempFile(t, content)
			_, err := eng.PutFile(localFile, tt.path, tt.access)
			require.NoError(t, err)

			node := eng.State.FindNodeByPath(tt.path)
			require.NotNil(t, node)
		})
	}
}
```

**Step 3: Write file operation tests**

```go
func TestShell_RmFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	content := []byte("to be deleted")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/temp.txt", "free")
	require.NoError(t, err)

	// Remove
	_, err = eng.Remove("/temp.txt")
	require.NoError(t, err)

	// Verify gone
	node := eng.State.FindNodeByPath("/temp.txt")
	assert.Nil(t, node)
}

func TestShell_RmRecursive(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)
	_, err = eng.Mkdir("/dir")
	require.NoError(t, err)

	content := []byte("nested file")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/dir/file.txt", "free")
	require.NoError(t, err)

	// Remove child first, then dir (simulating rm -r)
	_, err = eng.Remove("/dir/file.txt")
	require.NoError(t, err)
	_, err = eng.Remove("/dir")
	require.NoError(t, err)

	assert.Nil(t, eng.State.FindNodeByPath("/dir"))
}

func TestShell_MvSameDir(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	content := []byte("to be moved")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/old.txt", "free")
	require.NoError(t, err)

	_, err = eng.Move("/old.txt", "/new.txt")
	require.NoError(t, err)

	assert.Nil(t, eng.State.FindNodeByPath("/old.txt"))
	assert.NotNil(t, eng.State.FindNodeByPath("/new.txt"))
}

func TestShell_MvCrossDir(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)
	_, err = eng.Mkdir("/src")
	require.NoError(t, err)
	_, err = eng.Mkdir("/dst")
	require.NoError(t, err)

	content := []byte("cross dir move")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/src/file.txt", "free")
	require.NoError(t, err)

	_, err = eng.Move("/src/file.txt", "/dst/file.txt")
	require.NoError(t, err)

	assert.Nil(t, eng.State.FindNodeByPath("/src/file.txt"))
	assert.NotNil(t, eng.State.FindNodeByPath("/dst/file.txt"))
}

func TestShell_CpFile(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	content := []byte("to be copied")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/orig.txt", "free")
	require.NoError(t, err)

	_, err = eng.Copy("/orig.txt", "/copy.txt")
	require.NoError(t, err)

	// Both should exist
	assert.NotNil(t, eng.State.FindNodeByPath("/orig.txt"))
	assert.NotNil(t, eng.State.FindNodeByPath("/copy.txt"))
}
```

**Step 4: Write link tests**

```go
func TestShell_SoftLink(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	content := []byte("link target")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/target.txt", "free")
	require.NoError(t, err)

	_, err = eng.Link("/target.txt", "/link.txt", true) // soft=true
	require.NoError(t, err)

	link := eng.State.FindNodeByPath("/link.txt")
	require.NotNil(t, link)
}

func TestShell_HardLink(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	content := []byte("hard link target")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/target.txt", "free")
	require.NoError(t, err)

	_, err = eng.Link("/target.txt", "/hardlink.txt", false) // soft=false
	require.NoError(t, err)

	hardlink := eng.State.FindNodeByPath("/hardlink.txt")
	require.NotNil(t, hardlink)
}
```

**Step 5: Run tests**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -run TestShell -v -count=1
```

Expected: All new tests pass.

**Step 6: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
git add integration/shell_commands_test.go
git commit -m "test(integration): add shell command tests — navigation and file ops"
```

---

### Task 12: Shell integration tests — Access Control + Advanced Operations

**Files:**
- Modify: `bitfs/integration/shell_commands_test.go`

**Step 1: Write access control tests**

```go
func TestShell_EncryptDecrypt(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	content := []byte("encrypt me")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/secret.txt", "free")
	require.NoError(t, err)

	// Encrypt: Free → Private
	_, err = eng.EncryptNode("/secret.txt")
	require.NoError(t, err)

	node := eng.State.FindNodeByPath("/secret.txt")
	require.NotNil(t, node)
	// Access should now be private (0)

	// Decrypt: Private → Free
	_, err = eng.DecryptNode("/secret.txt")
	require.NoError(t, err)
}

func TestShell_SellFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	content := []byte("premium content")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/premium.txt", "free")
	require.NoError(t, err)

	_, err = eng.Sell("/premium.txt", 50) // 50 sat/KB
	require.NoError(t, err)

	node := eng.State.FindNodeByPath("/premium.txt")
	require.NotNil(t, node)
	assert.Equal(t, uint64(50), node.PricePerKB)
}

func TestShell_SellRecursive(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 40, 10_000)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)
	_, err = eng.Mkdir("/premium")
	require.NoError(t, err)

	for _, name := range []string{"a.txt", "b.txt"} {
		content := []byte("content of " + name)
		localFile := createTempFile(t, content)
		_, err = eng.PutFile(localFile, "/premium/"+name, "free")
		require.NoError(t, err)
	}

	// Sell recursively — the shell does this by walking children
	dir := eng.State.FindNodeByPath("/premium")
	require.NotNil(t, dir)
	for _, child := range dir.Children {
		_, err = eng.Sell("/premium/"+child.Name, 100)
		require.NoError(t, err)
	}

	for _, name := range []string{"a.txt", "b.txt"} {
		node := eng.State.FindNodeByPath("/premium/" + name)
		require.NotNil(t, node)
		assert.Equal(t, uint64(100), node.PricePerKB)
	}
}
```

**Step 2: Write batch operation tests (mget/mput simulate)**

```go
func TestShell_MputMultipleFiles(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)
	_, err = eng.Mkdir("/uploads")
	require.NoError(t, err)

	// Create local directory with files
	localDir := t.TempDir()
	for _, name := range []string{"file1.txt", "file2.txt", "file3.txt"} {
		err := os.WriteFile(filepath.Join(localDir, name), []byte("content of "+name), 0644)
		require.NoError(t, err)
	}

	// Mput all files
	_, err = eng.Mput(localDir, "/uploads")
	require.NoError(t, err)

	// Verify all uploaded
	for _, name := range []string{"file1.txt", "file2.txt", "file3.txt"} {
		node := eng.State.FindNodeByPath("/uploads/" + name)
		require.NotNil(t, node, "expected /uploads/%s to exist", name)
	}
}

func TestShell_GetFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	content := []byte("download me")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(localFile, "/download.txt", "free")
	require.NoError(t, err)

	// Get to local
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "downloaded.txt")
	err = eng.Get("/download.txt", outPath)
	require.NoError(t, err)

	downloaded, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, content, downloaded)
}
```

**Step 3: Write error path tests**

```go
func TestShell_MkdirExistingPath(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)
	_, err = eng.Mkdir("/existing")
	require.NoError(t, err)

	// Creating same dir again should error
	_, err = eng.Mkdir("/existing")
	assert.Error(t, err)
}

func TestShell_RmNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	_, err = eng.Remove("/ghost.txt")
	assert.Error(t, err)
}

func TestShell_CatNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	_, err = eng.Cat("/nope.txt")
	assert.Error(t, err)
}

func TestShell_MvNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	_, err = eng.Move("/nope.txt", "/dest.txt")
	assert.Error(t, err)
}

func TestShell_CpNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir("/")
	require.NoError(t, err)

	_, err = eng.Copy("/nope.txt", "/dest.txt")
	assert.Error(t, err)
}
```

**Step 4: Run all shell tests**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -run TestShell -v -count=1
```

Expected: All tests pass.

**Step 5: Run full integration suite to verify no regressions**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -v -count=1 -race
```

Expected: All 270 existing + ~25 new tests pass.

**Step 6: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
git add integration/shell_commands_test.go
git commit -m "test(integration): add shell access control, batch, and error path tests"
```

---

## Final Verification

### Task 13: Cross-verify all specs against code

**Step 1: Read each updated spec and verify no TODOs/TBDs remain**

```bash
grep -rn "TODO\|TBD\|FIXME\|PLACEHOLDER" bitfs/docs/spec/
```
Expected: No matches (or only intentional [PLANNED] markers for ZSTD).

**Step 2: Run all tests in both repos**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./... -count=1
cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1
cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -count=1 -race
```

Expected: All pass.

**Step 3: Run linter**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go && golangci-lint run ./...
cd /Users/alex/Codes/RabbitHole/bitfs && golangci-lint run ./...
```

Expected: No new warnings.

**Step 4: Final commit (if any cleanup needed)**

```bash
git add -A && git commit -m "chore: final verification cleanup"
```

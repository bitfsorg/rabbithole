# 模块规范：libbitfs-go/metanet

## 目的

在 BSV 区块链上实现 Unix 文件系统模型的 Metanet DAG 解析器。将 Metanet 协议概念映射到 Unix 文件系统原语：inode = P_node，dirent = ChildEntry，支持软链接（本地和远程）以及目录遍历。

设计参考：ConceptDesign #5, #6, #16, #17; SystemDesign 第 3, 4 节; DetailedDesign 第 4-B 节。

## 公共 API

### 类型

```go
// NodeType represents the three Metanet node types.
type NodeType int32

const (
    NodeTypeFile   NodeType = 0
    NodeTypeDir    NodeType = 1
    NodeTypeLink   NodeType = 2
    NodeTypeAnchor NodeType = 3 // Git commit anchor
)

// OpType represents the filesystem operation type.
type OpType int32

const (
    OpCreate OpType = 0
    OpUpdate OpType = 1
    OpDelete OpType = 2
)

// LinkType represents soft link subtypes.
type LinkType int32

const (
    LinkTypeSoft       LinkType = 0 // Points to P_node within same vault
    LinkTypeSoftRemote LinkType = 1 // Points to domain/path across vaults
)

// AccessLevel represents content access control modes.
type AccessLevel int32

const (
    AccessPrivate AccessLevel = 0 // Only owner can decrypt
    AccessFree    AccessLevel = 1 // Anyone can decrypt (D_node = scalar 1)
    AccessPaid    AccessLevel = 2 // Requires payment via payment protocol
)

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

// ChildEntry represents a directory entry (Unix dirent).
type ChildEntry struct {
    Index    uint32    // Child's index within parent directory
    Name     string    // File/directory name (stored only in parent)
    Type     NodeType  // FILE / DIR / LINK
    PubKey   []byte    // Child's P_node (33 bytes compressed)
    Hardened bool      // true = hardened BIP32 derivation (excluded from dir purchase)
}

// Node represents a parsed Metanet node with its payload.
type Node struct {
    TxID       []byte       // Transaction ID (32 bytes)
    PNode      []byte       // P_node compressed public key (33 bytes)
    ParentTxID []byte       // Parent's TxID (empty for root)
    BlockHeight uint32      // Block height (0 = unconfirmed)

    // Parsed payload fields
    Version         uint32
    Type            NodeType
    Op              OpType
    MimeType        string
    FileSize        uint64
    KeyHash         []byte        // SHA256(SHA256(plaintext))
    Access          AccessLevel
    PricePerKB      uint64
    LinkTarget      []byte
    LinkType        LinkType
    Timestamp       uint64
    Parent          []byte        // Parent P_node
    Index           uint32        // File index within parent
    Children        []ChildEntry
    MerkleRoot      []byte        // Merkle root of Children (32 bytes, nil for non-dir or empty dir)
    NextChildIndex  uint32
    Domain          string
    Keywords        string
    Description     string
    Metadata        map[string]string
    Encrypted       bool
    EncPayload      []byte  // PRIVATE mode: salt(16B) || nonce(12B) || AES-256-GCM(full TLV) || tag(16B)
    OnChain         bool
    ContentTxIDs    [][]byte
    Compression     int32
    CltvHeight      uint32
    RevenueShare    uint32
    NetworkName     string

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

    // --- Anchor-Specific Fields (NodeTypeAnchor only) ---
    TreeRootPNode    []byte   // Root directory's P_node (33 bytes)
    TreeRootTxID     []byte   // Root directory's latest TxID (32 bytes)
    ParentAnchorTxID [][]byte // Parent anchor TxIDs (merge commits have multiple)
    Author           string   // Git commit author
    CommitMessage    string   // Git commit message
    GitCommitSHA     []byte   // Git commit SHA for cross-reference (20 bytes)
    FileMode         uint32   // Git file mode (100644, 100755, 120000)
}

// ResolveResult holds the outcome of a path resolution.
type ResolveResult struct {
    Node     *Node       // Resolved target node
    Entry    *ChildEntry // ChildEntry in parent that references this node
    Parent   *Node       // Parent directory node
    Path     []string    // Fully resolved path components
}
```

### 接口

```go
// NodeStore provides access to Metanet node data.
// Implementations may read from local txstore or remote daemon.
type NodeStore interface {
    // GetNodeByPubKey returns the latest version of a node by its P_node.
    // "Latest" = highest block height, then TTOR ordering within same block.
    GetNodeByPubKey(pNode []byte) (*Node, error)

    // GetNodeByTxID returns a specific version of a node.
    GetNodeByTxID(txID []byte) (*Node, error)

    // GetNodeVersions returns all versions of a node, ordered by block height desc.
    GetNodeVersions(pNode []byte) ([]*Node, error)

    // GetChildNodes returns all child nodes referenced in a directory's ChildEntry list.
    GetChildNodes(dirNode *Node) ([]*Node, error)
}
```

### 函数

```go
// ParseNode parses OP_RETURN push data (as produced by tx.ParseOPReturnData)
// into a Metanet Node. The pushes should be the 4-element array:
// [MetaFlag, P_node, TxID_parent, Payload].
func ParseNode(pushes [][]byte) (*Node, error)

// ResolvePath resolves a filesystem path starting from a root node.
// Handles directory traversal, soft link following (max depth 10),
// and "." / ".." navigation.
func ResolvePath(store NodeStore, root *Node, pathComponents []string) (*ResolveResult, error)

// ListDirectory returns the ChildEntry list of a directory node.
// Returns error if node is not a directory.
func ListDirectory(node *Node) ([]ChildEntry, error)

// FollowLink resolves a soft link to its target node.
// SOFT: looks up target P_node for latest version.
// SOFT_REMOTE: returns error (requires external resolution via DNS/Paymail).
// Follows chains up to maxDepth=10.
func FollowLink(store NodeStore, linkNode *Node, maxDepth int) (*Node, error)

// FindChild finds a child by name in a directory node's children list.
func FindChild(dirNode *Node, name string) (*ChildEntry, bool)

// AddChild adds a new ChildEntry to a directory node.
// Allocates the next child index and increments NextChildIndex.
func AddChild(dirNode *Node, name string, nodeType NodeType, pubKey []byte, hardened bool) (*ChildEntry, error)

// RemoveChild removes a ChildEntry by name from a directory node.
// Does NOT decrement NextChildIndex (deleted indices are never reused).
func RemoveChild(dirNode *Node, name string) error

// RenameChild renames a ChildEntry within a directory.
func RenameChild(dirNode *Node, oldName, newName string) error

// LatestVersion selects the latest version from a list of nodes with the same P_node.
// Ordering: highest block height wins; within same block, TTOR (last in block) wins.
func LatestVersion(nodes []*Node) *Node

// InheritPricePerKB walks up the directory tree to find the effective price.
// Checks current node, then parent, then grandparent, etc. until root.
// Returns 0 if no price is set anywhere in the ancestry.
func InheritPricePerKB(store NodeStore, node *Node) (uint64, error)

// SerializePayload serializes Node fields into the simple TLV binary format
// that can be used as the payload in OP_RETURN.
func SerializePayload(node *Node) ([]byte, error)

// CheckCLTVAccess checks if content is accessible at the given block height.
// Returns CLTVAllowed if cltv_height is 0 (no restriction) or currentHeight >= cltv_height.
func CheckCLTVAccess(node *Node, currentHeight uint32) CLTVResult

// ComputeDirectoryMerkleRoot computes the Merkle root from a directory's
// children list. Returns nil for empty or nil children slice.
// Algorithm: Bitcoin-style double-SHA256 Merkle tree over serialized ChildEntry leaves.
func ComputeDirectoryMerkleRoot(children []ChildEntry) []byte

// BuildDirectoryMerkleProof builds a Merkle proof for a child at the given
// position index. Returns the sibling hashes needed to recompute the root.
func BuildDirectoryMerkleProof(children []ChildEntry, childIndex int) ([][]byte, error)

// VerifyChildMembership verifies that a ChildEntry belongs to a directory
// with the given MerkleRoot, using the provided proof path and position index.
func VerifyChildMembership(entry *ChildEntry, proof [][]byte, index int, merkleRoot []byte) bool

// ComputeChildLeafHash computes the Merkle leaf hash for a single ChildEntry.
// The leaf hash is DoubleHash(serialize(entry)).
func ComputeChildLeafHash(entry *ChildEntry) []byte
```

## 依赖

- `libbitfs-go/tx` -- OP_RETURN 解析，交易格式
- `libbitfs-go/metanet/tlv` -- TLV 序列化/反序列化
- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- 公钥处理

## 数据结构

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
| 0x13 | Encrypted | uint32 | 4 | 非零 = EncPayload 已加密 (小端序) |
| 0x14 | OnChain | uint32 | 4 | 非零 = 内容在链上 (小端序) |
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

### 路径解析算法

```
ResolvePath(root, ["docs", "report.pdf"]):
  1. current = root
  2. for each component:
     a. if component == "." -> continue
     b. if component == ".." -> current = current.parent (by traversal history)
     c. FindChild(current, component) -> entry
     d. GetNodeByPubKey(entry.PubKey) -> node
     e. if node.Type == LINK -> FollowLink(node, 10)
     f. current = resolved node
  3. return current
```

### 版本解析

同一 P_node 可能有多个 TxID（版本）。解析规则：
1. 不同区块：区块高度更高的为当前版本
2. 同一区块：TTOR（拓扑交易排序规则，Topological Transaction Ordering Rule）——区块中靠后的胜出

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrNotDirectory` | 操作需要 DIR 但节点是 FILE/LINK |
| `ErrNotFile` | 操作需要 FILE 但节点是 DIR/LINK |
| `ErrChildNotFound` | 目录中不存在指定名称的子项 |
| `ErrChildExists` | 同名子项已存在（创建操作时） |
| `ErrLinkDepthExceeded` | 软链接链超过最大深度 (10) |
| `ErrRemoteLinkNotSupported` | SOFT_REMOTE 链接需要外部解析 |
| `ErrInvalidPath` | 路径包含无效字符或为空 |
| `ErrNodeNotFound` | 未找到给定 P_node 或 TxID 对应的节点 |
| `ErrInvalidPayload` | 载荷无法反序列化 |
| `ErrHardLinkToDirectory` | 尝试对目录创建硬链接 |
| `ErrAboveRoot` | ".." 导航尝试越过根节点之上 |

## 安全考量

1. **链接循环防护**：软链接跟踪限制最大深度为 10，防止无限循环。
2. **路径遍历防护**：".." 不能跳出根节点之上。
3. **硬链接限制**：只有 FILE 节点可以被硬链接。禁止目录硬链接以防止 DAG 环。
4. **版本排序**：始终使用 LatestVersion() 获取当前状态。过时版本可能包含过期的子项列表。
5. **Anchor 节点安全**：Anchor 的 ParentAnchorTxID 可有多个值（merge commit），客户端应验证所有父锚点的有效性。
6. **ISO 状态机**：ISO 状态只能单向转换 (None->Open->Partial->Closed)，防止回滚攻击。

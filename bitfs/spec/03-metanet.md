# 模块规范：libbitfs/metanet

## 目的

在 BSV 区块链上实现 Unix 文件系统模型的 Metanet DAG 解析器。将 Metanet 协议概念映射到 Unix 文件系统原语：inode = P_node，dirent = ChildEntry，支持硬链接、软链接（本地和远程）以及目录遍历。

设计参考：ConceptDesign #5, #6, #16, #17; SystemDesign 第 3, 4 节; DetailedDesign 第 4-B 节。

## 公共 API

### 类型

```go
// NodeType represents the three Metanet node types.
type NodeType int32

const (
    NodeTypeFile NodeType = 0
    NodeTypeDir  NodeType = 1
    NodeTypeLink NodeType = 2
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
    Access          int32         // 0=PRIVATE, 1=FREE, 2=PAID
    PricePerKB      uint64
    LinkTarget      []byte
    LinkType        LinkType
    Timestamp       uint64
    Parent          []byte        // Parent P_node
    Index           uint32        // File index within parent
    Children        []ChildEntry
    NextChildIndex  uint32
    Domain          string
    Keywords        string
    Description     string
    Metadata        map[string]string
    Encrypted       bool
    PrivateKeyHash  []byte
    EncPayload      []byte
    PrivateFileIdx  uint32
    OnChain         bool
    ContentTxIDs    [][]byte
    Compression     int32
    CltvHeight      uint32
    RevenueShare    uint32
    NetworkName     string
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
// ParseNode parses a raw BSV transaction into a Metanet Node.
// Extracts OP_RETURN fields and deserializes the Protobuf payload.
func ParseNode(txBytes []byte) (*Node, error)

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
```

## 依赖

- `libbitfs/tx` -- OP_RETURN 解析，交易格式
- `google.golang.org/protobuf` -- Protobuf 反序列化
- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- 公钥处理

## 数据结构

### Protobuf 模式（外部定义，此处消费）

本模块反序列化 `BitFSPayload` protobuf 消息。`.proto` 文件为权威来源；本模块提供 Go 层面的 `Node` 结构体作为解析后的视图。

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
| `ErrInvalidProtobuf` | 载荷无法反序列化 |
| `ErrHardLinkToDirectory` | 尝试对目录创建硬链接 |

## 安全考量

1. **链接循环防护**：软链接跟踪限制最大深度为 10，防止无限循环。
2. **路径遍历防护**：".." 不能跳出根节点之上。
3. **硬链接限制**：只有 FILE 节点可以被硬链接。禁止目录硬链接以防止 DAG 环。
4. **版本排序**：始终使用 LatestVersion() 获取当前状态。过时版本可能包含过期的子项列表。

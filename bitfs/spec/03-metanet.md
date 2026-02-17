# Module Specification: internal/metanet

## PURPOSE

Metanet DAG parser implementing the Unix filesystem model on BSV blockchain. Maps Metanet protocol concepts to Unix filesystem primitives: inode = P_node, dirent = ChildEntry, with support for hard links, soft links (local and remote), and directory traversal.

Design references: ConceptDesign #5, #6, #16, #17; SystemDesign section 3, 4; DetailedDesign section 4-B.

## PUBLIC API

### Types

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

### Interfaces

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

### Functions

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

## DEPENDENCIES

- `internal/tx` -- OP_RETURN parsing, transaction format
- `google.golang.org/protobuf` -- Protobuf deserialization
- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- Public key handling

## DATA STRUCTURES

### Protobuf Schema (defined externally, consumed here)

The module deserializes `BitFSPayload` protobuf messages. The `.proto` file is the source of truth; this module provides the Go-level `Node` struct as a parsed view.

### Path Resolution Algorithm

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

### Version Resolution

Same P_node may have multiple TxIDs (versions). Resolution:
1. Different blocks: higher block height = current version
2. Same block: TTOR (Topological Transaction Ordering Rule) -- later in block wins

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrNotDirectory` | Operation requires DIR but node is FILE/LINK |
| `ErrNotFile` | Operation requires FILE but node is DIR/LINK |
| `ErrChildNotFound` | Named child does not exist in directory |
| `ErrChildExists` | Named child already exists (for create operations) |
| `ErrLinkDepthExceeded` | Soft link chain exceeds max depth (10) |
| `ErrRemoteLinkNotSupported` | SOFT_REMOTE link requires external resolution |
| `ErrInvalidPath` | Path contains invalid characters or is empty |
| `ErrNodeNotFound` | No node found for given P_node or TxID |
| `ErrInvalidProtobuf` | Payload cannot be deserialized |
| `ErrHardLinkToDirectory` | Attempt to create hard link to a directory |

## SECURITY CONSIDERATIONS

1. **Link loop prevention**: Soft link following is capped at depth 10 to prevent infinite loops.
2. **Path traversal**: ".." cannot escape above the root node.
3. **Hard link restrictions**: Only FILE nodes can be hard-linked. Directory hard links are prohibited to prevent DAG cycles.
4. **Version ordering**: Always use LatestVersion() to get the current state. Stale versions may contain outdated children lists.

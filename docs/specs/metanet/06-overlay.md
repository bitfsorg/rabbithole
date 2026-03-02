# 模块规格说明：internal/overlay

## 目的

`overlay` 包实现用于 Metanet Node 发现、内容路由和服务广告的 BRC 覆盖网络（Overlay Network）。它使节点能够相互发现、广告各自的能力，以及定位哪些节点存储了特定内容——所有这些都遵循 BSV 协会的 BRC 标准规范。

覆盖网络提供两个主要功能：
1. **节点发现与路由**：Metanet Node 通过 BRC-31（Overlay Network）、BRC-22（SHIP）和 BRC-23（SLAP）广告其存在、能力和支持的内容主题。
2. **内容发现**：用户和节点可以查询覆盖网络，查找哪些 Metanet Node 存储了特定文件，从而实现高效的检索路由。

## 公开 API

### 类型

```go
// Node represents a Metanet Node's overlay network identity.
type Node struct {
    PubKey     []byte   // 33-byte compressed public key (node identity)
    Endpoint   string   // HTTP endpoint URL (e.g., "https://node1.metanet.org:8080")
    Topics     []string // Supported overlay topics
    Capacity   uint64   // Advertised storage capacity in bytes
    Uptime     float64  // Self-reported uptime percentage
    LastSeen   int64    // Unix timestamp of last heartbeat
}

// TopicManager manages subscriptions and queries for an overlay topic.
type TopicManager struct {
    Topic       string
    Nodes       []*Node
    UpdatedAt   int64
}

// ContentLocation maps a content hash to the nodes that store it.
type ContentLocation struct {
    ContentHash [32]byte   // SHA256 of the content (from Metanet DAG)
    Nodes       []*Node    // Nodes that have this content
    DealTxIDs   [][32]byte // StorageDeal TxIDs (if archived, not just cached)
}

// OverlayService represents the local node's overlay network service.
type OverlayService struct {
    LocalNode    *Node
    PeerStore    PeerStore
    TopicStore   TopicStore
}

// PeerStore is the interface for persistent peer storage.
type PeerStore interface {
    AddPeer(node *Node) error
    RemovePeer(pubKey []byte) error
    GetPeer(pubKey []byte) (*Node, error)
    ListPeers() ([]*Node, error)
    UpdateLastSeen(pubKey []byte, timestamp int64) error
}

// TopicStore is the interface for topic subscription storage.
type TopicStore interface {
    Subscribe(topic string, node *Node) error
    Unsubscribe(topic string, pubKey []byte) error
    GetSubscribers(topic string) ([]*Node, error)
    ListTopics() ([]string, error)
}

// LookupRequest is a query to the overlay network.
type LookupRequest struct {
    ContentHash *[32]byte  // Find nodes storing this content (optional)
    Topic       string     // Filter by topic (optional)
    MaxResults  int        // Maximum number of results
}

// LookupResponse contains the results of an overlay query.
type LookupResponse struct {
    Nodes    []*Node
    Locations []*ContentLocation
}

// AdvertiseRequest is a node's advertisement to the overlay.
type AdvertiseRequest struct {
    Node          *Node
    Signature     []byte      // Signed by node's private key
    ContentHashes [][32]byte  // Content this node has available
}
```

### 函数

```go
// NewOverlayService creates a new overlay network service for the local node.
func NewOverlayService(
    localNode *Node,
    peerStore PeerStore,
    topicStore TopicStore,
) *OverlayService

// Advertise broadcasts the local node's presence and capabilities
// to known peers (BRC-87 Overlay Ads).
func (s *OverlayService) Advertise(privKey []byte) error

// Discover queries the overlay network for peers matching the given
// criteria (BRC-23 SLAP + BRC-25 Lookup Service).
func (s *OverlayService) Discover(req *LookupRequest) (*LookupResponse, error)

// LocateContent finds Metanet Nodes that store a specific content hash.
// Queries both the overlay network and on-chain StorageDeal records.
func (s *OverlayService) LocateContent(contentHash [32]byte) (*ContentLocation, error)

// SubmitTx submits a transaction to the overlay network for propagation
// (BRC-22 SHIP).
func (s *OverlayService) SubmitTx(txData []byte, topic string) error

// RegisterTopic registers the local node as a subscriber for a topic
// (BRC-24 Topic Manager).
func (s *OverlayService) RegisterTopic(topic string) error

// UnregisterTopic removes the local node from a topic.
func (s *OverlayService) UnregisterTopic(topic string) error

// HandlePeerMessage processes an incoming message from a peer node.
func (s *OverlayService) HandlePeerMessage(msg []byte) error

// Heartbeat sends a keepalive to all known peers, updating LastSeen.
func (s *OverlayService) Heartbeat(privKey []byte) error

// PruneStalePeers removes peers that have not been seen within the given duration.
func (s *OverlayService) PruneStalePeers(maxAge int64) (int, error)

// VerifyAdvertisement verifies the signature on a node advertisement.
func VerifyAdvertisement(adv *AdvertiseRequest) error

// SignAdvertisement signs a node advertisement with the node's private key.
func SignAdvertisement(adv *AdvertiseRequest, privKey []byte) error
```

## 依赖

- `internal/chain` -- 链参数
- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- 密钥操作、签名
- `crypto/sha256` -- 哈希
- `net/http` -- 覆盖网络通信的 HTTP 客户端/服务器
- `encoding/json` -- 消息序列化

## 数据结构

### BRC 标准映射

| BRC | 名称 | 实现 |
|-----|------|----------------|
| BRC-31 | Overlay Network | `OverlayService` -- 核心点对点覆盖网络 |
| BRC-22 | SHIP | `SubmitTx` -- 向覆盖网络提交交易 |
| BRC-23 | SLAP | `Discover` -- 查询覆盖网络服务 |
| BRC-24 | Topic Manager | `RegisterTopic`/`UnregisterTopic` -- 管理订阅 |
| BRC-25 | Lookup Service | `LocateContent` -- 内容寻址查找 |
| BRC-87 | Overlay Ads | `Advertise` -- 广播节点能力 |

### 覆盖网络主题（Overlay Topics）

```
Predefined topics:
    "metanet.storage"   -- Storage deal announcements
    "metanet.proof"     -- Storage proof submissions
    "metanet.payment"   -- Payment channel state
    "metanet.content"   -- Content availability announcements
```

### 节点广告格式（Node Advertisement Format）

```json
{
    "pubkey": "<hex-encoded 33-byte compressed public key>",
    "endpoint": "https://node.example.com:8080",
    "topics": ["metanet.storage", "metanet.content"],
    "capacity": 1099511627776,
    "uptime": 99.5,
    "timestamp": 1700000000,
    "signature": "<hex-encoded signature>"
}
```

## 错误处理

| 错误 | 条件 |
|-------|-----------|
| `ErrPeerNotFound` | 请求的对等节点不在对等存储中 |
| `ErrInvalidSignature` | 广告签名验证失败 |
| `ErrTopicNotFound` | 请求的主题不存在 |
| `ErrNoNodesFound` | 内容查询未找到任何节点 |
| `ErrPeerUnreachable` | 无法连接到对等节点端点 |
| `ErrMessageTooLarge` | 覆盖网络消息超过大小限制 |
| `ErrSelfAdvertise` | 节点尝试将自己添加为对等节点 |

## 安全考量

1. **签名广告**：所有节点广告必须使用节点的私钥签名。这防止了身份冒充，确保只有节点本身可以广告其能力。
2. **对等节点验证**：连接到对等节点时，节点验证对方的公钥是否与其广告的身份匹配。
3. **女巫攻击抵抗（Sybil Resistance）**：虽然覆盖网络本身不能防止女巫攻击，但经济层（存储合约、支付通道）提供了天然的女巫攻击抵抗——节点必须拥有真实的容量并提供真实的内容才能获取收入。
4. **过期对等节点清理**：未在配置间隔内发送心跳的对等节点将被移除，防止路由过期。
5. **内容验证**：内容哈希通过与链上 Metanet DAG 记录核对进行验证，防止节点广告其并不拥有的内容。
6. **速率限制**：覆盖网络端点应实现速率限制，以防止通过过量查询或广告进行 DoS 攻击。

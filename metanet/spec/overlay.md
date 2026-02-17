# Module Specification: internal/overlay

## PURPOSE

The `overlay` package implements the BRC Overlay Network for Metanet Node discovery, content routing, and service advertisement. It enables nodes to discover each other, advertise their capabilities, and locate which nodes store specific content -- all following BSV Association's BRC standard specifications.

The overlay network serves two primary functions:
1. **Node discovery and routing**: Metanet Nodes advertise their presence, capabilities, and supported content topics via BRC-31 (Overlay Network), BRC-22 (SHIP), and BRC-23 (SLAP).
2. **Content discovery**: Users and nodes can query the overlay to find which Metanet Nodes store a particular file, enabling efficient retrieval routing.

## PUBLIC API

### Types

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

### Functions

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

## DEPENDENCIES

- `internal/chain` -- Chain parameters
- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- Key operations, signing
- `crypto/sha256` -- Hashing
- `net/http` -- HTTP client/server for overlay communication
- `encoding/json` -- Message serialization

## DATA STRUCTURES

### BRC Standards Mapping

| BRC | Name | Implementation |
|-----|------|----------------|
| BRC-31 | Overlay Network | `OverlayService` -- core peer-to-peer overlay |
| BRC-22 | SHIP | `SubmitTx` -- submit transactions to overlay |
| BRC-23 | SLAP | `Discover` -- query overlay services |
| BRC-24 | Topic Manager | `RegisterTopic`/`UnregisterTopic` -- manage subscriptions |
| BRC-25 | Lookup Service | `LocateContent` -- content-addressable lookups |
| BRC-87 | Overlay Ads | `Advertise` -- broadcast node capabilities |

### Overlay Topics

```
Predefined topics:
    "metanet.storage"   -- Storage deal announcements
    "metanet.proof"     -- Storage proof submissions
    "metanet.payment"   -- Payment channel state
    "metanet.content"   -- Content availability announcements
```

### Node Advertisement Format

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

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrPeerNotFound` | Requested peer not in peer store |
| `ErrInvalidSignature` | Advertisement signature verification failed |
| `ErrTopicNotFound` | Requested topic does not exist |
| `ErrNoNodesFound` | No nodes found for content query |
| `ErrPeerUnreachable` | Cannot connect to peer endpoint |
| `ErrMessageTooLarge` | Overlay message exceeds size limit |
| `ErrSelfAdvertise` | Node attempted to add itself as peer |

## SECURITY CONSIDERATIONS

1. **Signed advertisements**: All node advertisements must be signed with the node's private key. This prevents impersonation and ensures only the node itself can advertise its capabilities.
2. **Peer verification**: When connecting to a peer, the node verifies the peer's public key matches its advertised identity.
3. **Sybil resistance**: While the overlay itself does not prevent Sybil attacks, the economic layer (storage deals, payment channels) provides natural Sybil resistance -- nodes must have real capacity and serve real content to earn revenue.
4. **Stale peer pruning**: Peers that do not heartbeat within the configured interval are removed, preventing stale routing.
5. **Content verification**: Content hashes are verified against on-chain Metanet DAG records, preventing nodes from advertising content they do not have.
6. **Rate limiting**: Overlay endpoints should implement rate limiting to prevent DoS via excessive queries or advertisements.

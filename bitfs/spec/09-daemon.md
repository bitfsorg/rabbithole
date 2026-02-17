# Module Specification: internal/daemon

## PURPOSE

BitFS daemon implementing LFCP (Local Full-Copy Peer) functionality. Serves as an HTTP server providing content retrieval, Metanet metadata queries, Method 42 ECDH handshake for identity verification, x402 payment handling for paid content, and WebMCP for browser-based AI agents.

Design references: ConceptDesign #8, #21, #27, #33; SystemDesign section 13; DetailedDesign section 13-B.

## PUBLIC API

### Types

```go
// Config holds daemon configuration.
type Config struct {
    ListenAddr   string         `toml:"listen"`
    TLS          TLSConfig      `toml:"tls"`
    X402         X402Config     `toml:"x402"`
    Security     SecurityConfig `toml:"security"`
    Storage      StorageConfig  `toml:"storage"`
    Log          LogConfig      `toml:"log"`
}

type TLSConfig struct {
    Enabled  bool   `toml:"enabled"`
    CertFile string `toml:"cert"`
    KeyFile  string `toml:"key"`
}

type X402Config struct {
    Enabled       bool   `toml:"enabled"`
    PricePerMB    uint64 `toml:"price_per_mb"`    // CDN bandwidth fee (sat)
    FreeQuotaMB   uint64 `toml:"free_quota_mb"`
    InvoiceExpiry int64  `toml:"invoice_expiry"`  // seconds
}

type SecurityConfig struct {
    RateLimit      RateLimitConfig `toml:"rate_limit"`
    CORS           CORSConfig      `toml:"cors"`
    MaxRequestSize string          `toml:"max_request_size"`
}

type RateLimitConfig struct {
    RPM   int `toml:"rpm"`
    Burst int `toml:"burst"`
}

type CORSConfig struct {
    Origins []string `toml:"origins"`
    Methods []string `toml:"methods"`
}

type StorageConfig struct {
    DataDir   string `toml:"data_dir"`
    DBPath    string `toml:"db_path"`
    CacheSize string `toml:"cache_size"`
}

type LogConfig struct {
    Level string `toml:"level"`
    File  string `toml:"file"`
}

// Daemon is the main daemon server.
type Daemon struct {
    config  *Config
    wallet  *wallet.Wallet
    store   storage.Store
    spv     *spv.Client
    metanet *metanet.Service
    server  *http.Server
}

// Session represents an authenticated Method 42 session.
type Session struct {
    ID         string
    BuyerPub   []byte         // Buyer's public key
    SellerPub  []byte         // Seller's public key (P_node)
    SessionKey []byte         // Derived session encryption key
    CreatedAt  time.Time
    ExpiresAt  time.Time
}
```

### Functions

```go
// New creates a new Daemon instance.
func New(config *Config, w *wallet.Wallet, store storage.Store) (*Daemon, error)

// Start starts the daemon HTTP server.
func (d *Daemon) Start() error

// Stop gracefully shuts down the daemon.
func (d *Daemon) Stop(ctx context.Context) error

// RegisterRoutes registers all HTTP handlers on the provided mux.
func (d *Daemon) RegisterRoutes(mux *http.ServeMux)
```

### HTTP Endpoints

```
GET  /                              Root (Content Negotiation: HTML/Markdown/JSON)
GET  /{path}                        Path access (Content Negotiation)
GET  /_bitfs/data/{hash}            Encrypted data retrieval (may trigger x402)
GET  /_bitfs/meta/{pnode}/{path}    Metanet metadata query
GET  /_bitfs/health                 Health check

POST /_bitfs/handshake              Method 42 ECDH handshake
POST /_bitfs/pay/{invoice_id}       Submit BSV payment (x402)
GET  /_bitfs/buy/{txid}             Get purchase info (capsule_hash, price)
POST /_bitfs/buy/{txid}             Submit HTLC, receive capsule

POST /_bitfs/git/push               Git remote helper push endpoint
GET  /_bitfs/git/refs/{path}        Git refs retrieval

GET  /.well-known/bsvalias          Paymail capability discovery
GET  /api/v1/pki/{alias}@{domain}   Paymail PKI endpoint
```

## DEPENDENCIES

- `net/http` -- HTTP server
- `internal/wallet` -- Key management
- `internal/storage` -- Content storage
- `internal/method42` -- Encryption/decryption and handshake
- `internal/metanet` -- DAG traversal
- `internal/x402` -- Payment protocol
- `internal/paymail` -- Paymail server capabilities
- `internal/spv` -- Transaction verification

## DATA STRUCTURES

### Method 42 Handshake Flow
```
1. Buyer  -> POST /_bitfs/handshake: { P_buyer, nonce_b, timestamp }
2. Seller <- Response: { P_seller, nonce_s, timestamp }
3. Both compute: shared = ECDH(D_self, P_other)
   session_key = SHA256(shared.x || nonce_b || nonce_s)
4. Both verify: HMAC(session_key, "verify")
5. Subsequent communication encrypted with session_key (AES-256-GCM)
```

### Content Negotiation
```
Accept: text/html       -> HTML page (+ WebMCP declarations)
Accept: text/markdown   -> Markdown agent guide
Accept: application/json -> JSON metadata
```

## ERROR HANDLING

HTTP error codes:
- 400 Bad Request
- 402 Payment Required (x402)
- 404 Not Found
- 408 Timeout
- 409 Transaction Conflict
- 429 Rate Limited
- 500 Internal Error
- 503 Storage Unavailable

JSON error format:
```json
{"error": {"code": "NETWORK_TIMEOUT", "message": "...", "retry": true, "cached": false}}
```

## SECURITY CONSIDERATIONS

1. **Method 42 handshake**: P_seller must match DNSLink-published P_node. ECDH prevents MITM.
2. **Rate limiting**: Per-IP rate limits (default 60 RPM, burst 20) prevent abuse.
3. **CORS**: Configurable origins for browser access.
4. **TLS**: Production deployments must use TLS (reverse proxy recommended).
5. **Session expiry**: Handshake sessions expire after configurable TTL.
6. **Invoice verification**: Payment proofs are verified against mempool or Merkle proof before releasing content.

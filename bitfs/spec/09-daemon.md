# 模块规范：internal/daemon

## 目的

BitFS 守护进程，实现 LFCP（本地全拷贝对等节点，Local Full-Copy Peer）功能。作为 HTTP 服务器提供内容检索、Metanet 元数据查询、Method 42 ECDH 握手身份验证、x402 付费内容支付处理，以及面向浏览器 AI 代理的 WebMCP。

设计参考：ConceptDesign #8, #21, #27, #33; SystemDesign 第 13 节; DetailedDesign 第 13-B 节。

## 公共 API

### 类型

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

### 函数

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

### HTTP 端点

```
GET  /                              根路径（内容协商：HTML/Markdown/JSON）
GET  /{path}                        路径访问（内容协商）
GET  /_bitfs/data/{hash}            加密数据检索（可能触发 x402）
GET  /_bitfs/meta/{pnode}/{path}    Metanet 元数据查询
GET  /_bitfs/health                 健康检查

POST /_bitfs/handshake              Method 42 ECDH 握手
POST /_bitfs/pay/{invoice_id}       提交 BSV 支付（x402）
GET  /_bitfs/buy/{txid}             获取购买信息（capsule_hash，价格）
POST /_bitfs/buy/{txid}             提交 HTLC，接收胶囊（Capsule）

POST /_bitfs/git/push               Git 远程助手推送端点
GET  /_bitfs/git/refs/{path}        Git 引用检索

GET  /.well-known/bsvalias          Paymail 能力发现
GET  /api/v1/pki/{alias}@{domain}   Paymail PKI 端点
```

## 依赖

- `net/http` -- HTTP 服务器
- `libbitfs/wallet` -- 密钥管理
- `libbitfs/storage` -- 内容存储
- `libbitfs/method42` -- 加密/解密与握手
- `libbitfs/metanet` -- DAG 遍历
- `libbitfs/x402` -- 支付协议
- `libbitfs/paymail` -- Paymail 服务器能力
- `libbitfs/spv` -- 交易验证

## 数据结构

### Method 42 握手流程
```
1. Buyer  -> POST /_bitfs/handshake: { P_buyer, nonce_b, timestamp }
2. Seller <- Response: { P_seller, nonce_s, timestamp }
3. Both compute: shared = ECDH(D_self, P_other)
   session_key = SHA256(shared.x || nonce_b || nonce_s)
4. Both verify: HMAC(session_key, "verify")
5. Subsequent communication encrypted with session_key (AES-256-GCM)
```

### 内容协商
```
Accept: text/html       -> HTML page (+ WebMCP declarations)
Accept: text/markdown   -> Markdown agent guide
Accept: application/json -> JSON metadata
```

## 错误处理

HTTP 错误码：
- 400 Bad Request（错误请求）
- 402 Payment Required（需要支付，x402）
- 404 Not Found（未找到）
- 408 Timeout（超时）
- 409 Transaction Conflict（交易冲突）
- 429 Rate Limited（速率限制）
- 500 Internal Error（内部错误）
- 503 Storage Unavailable（存储不可用）

JSON 错误格式：
```json
{"error": {"code": "NETWORK_TIMEOUT", "message": "...", "retry": true, "cached": false}}
```

## 安全考量

1. **Method 42 握手**：P_seller 必须匹配 DNSLink 发布的 P_node。ECDH 防止中间人攻击（MITM）。
2. **速率限制**：每 IP 速率限制（默认 60 RPM，突发 20）防止滥用。
3. **CORS**：可配置的来源列表用于浏览器访问。
4. **TLS**：生产环境部署必须使用 TLS（建议使用反向代理）。
5. **会话过期**：握手会话在可配置的 TTL 后过期。
6. **发票验证**：在释放内容之前，支付证明需要通过内存池或 Merkle 证明进行验证。

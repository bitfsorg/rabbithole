# 模块规范：internal/daemon

## 目的

BitFS 守护进程，实现 LFCP（本地全拷贝对等节点，Local Full-Copy Peer）功能。作为 HTTP 服务器提供内容检索、Metanet 元数据查询、Method 42 ECDH 握手身份验证、payment 付费内容支付处理，以及面向浏览器 AI 代理的 WebMCP。

设计参考：ConceptDesign #8, #21, #27, #33; SystemDesign 第 13 节; DetailedDesign 第 13-B 节。

## 公共 API

### 类型

```go
// Config holds daemon configuration.
type Config struct {
    ListenAddr   string         `toml:"listen"`
    TLS          TLSConfig      `toml:"tls"`
    Payment      PaymentConfig  `toml:"payment"`
    Security     SecurityConfig `toml:"security"`
    Storage      StorageConfig  `toml:"storage"`
    Log          LogConfig      `toml:"log"`
    Mainnet      bool           `toml:"mainnet"` // true = mainnet addresses, false = testnet/regtest
}

type TLSConfig struct {
    Enabled  bool   `toml:"enabled"`
    CertFile string `toml:"cert"`
    KeyFile  string `toml:"key"`
}

type PaymentConfig struct {
    Enabled       bool   `toml:"enabled"`
    PricePerMB    uint64 `toml:"price_per_mb"`    // CDN bandwidth fee (sat)
    FreeQuotaMB   uint64 `toml:"free_quota_mb"`
    InvoiceExpiry int64  `toml:"invoice_expiry"`  // seconds
}

type SecurityConfig struct {
    RateLimit      RateLimitConfig `toml:"rate_limit"`
    CORS           CORSConfig      `toml:"cors"`
    MaxRequestSize string          `toml:"max_request_size"`
    TrustProxy     bool            `toml:"trust_proxy"` // trust X-Forwarded-For / X-Real-IP
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
    wallet  WalletService
    store   ContentStore
    metanet MetanetService
    spv     SPVService   // optional; nil = SPV endpoints disabled
    chain   ChainService // optional; nil = skip broadcast
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
func New(config *Config, wallet WalletService, store ContentStore, metanet MetanetService) (*Daemon, error)

// Start starts the daemon HTTP server.
func (d *Daemon) Start() error

// Stop gracefully shuts down the daemon.
func (d *Daemon) Stop(ctx context.Context) error

// SetSPV attaches an SPV verification service. Must be called before Start.
func (d *Daemon) SetSPV(spv SPVService)

// SetChain attaches a blockchain service for payment broadcast. Must be called before Start.
func (d *Daemon) SetChain(c ChainService)

// RegisterRoutes registers all HTTP handlers on the provided mux.
func (d *Daemon) RegisterRoutes(mux *http.ServeMux)
```

### HTTP 端点

```
GET  /                                      根路径（内容协商：HTML/Markdown/JSON）
GET  /{path}                                路径访问（内容协商）
GET  /_bitfs/health                         健康检查

GET  /_bitfs/data/{hash}                    加密数据检索（可能触发 402 支付）
GET  /_bitfs/meta/{pnode}/{path...}         Metanet 元数据查询
GET  /_bitfs/versions/{pnode}/{path...}     版本历史查询

POST /_bitfs/handshake                      Method 42 ECDH 握手
GET  /_bitfs/buy/{txid}                     获取购买信息（capsule_hash，价格）
POST /_bitfs/buy/{txid}                     提交 HTLC，接收胶囊（Capsule）
POST /_bitfs/pay/{invoice_id}               带宽支付（payment 发票结算）
GET  /_bitfs/sales                          发票/销售记录列表
GET  /_bitfs/spv/proof/{txid}               SPV Merkle 证明检索

GET  /_bitfs/dashboard/status               仪表盘：守护进程状态
GET  /_bitfs/dashboard/storage              仪表盘：存储统计
GET  /_bitfs/dashboard/wallet               仪表盘：钱包信息
GET  /_bitfs/dashboard/network              仪表盘：网络配置
GET  /_bitfs/dashboard/logs                 仪表盘：最近日志（?limit=N&level=L）

GET  /.well-known/bsvalias                  BSV Alias 能力发现
GET  /api/v1/pki/{handle}                   BSV Alias PKI 端点（handle = alias@domain）
GET  /api/v1/public-profile/{handle}        BSV Alias 公开资料
GET  /api/v1/verify/{handle}/{pubkey}       BSV Alias 公钥验证
```

所有 POST 端点和部分 GET 端点均注册对应的 `OPTIONS` 处理器以支持 CORS 预检请求。中间件自动设置 `Access-Control-Allow-Origin`、`Access-Control-Allow-Methods`、`Access-Control-Allow-Headers` 响应头。

## 依赖

- `net/http` -- HTTP 服务器
- `go-sdk/primitives/ec` -- ECDH 计算与公钥解析
- `go-sdk/script` -- 地址派生（P2PKH）
- `go-sdk/transaction` -- 交易解析（HTLC 验证、重放保护）
- `libbitfs-go/method42` -- 胶囊计算（ComputeCapsuleWithNonce, ComputeCapsuleHash）
- `libbitfs-go/payment` -- 支付协议（发票、HTLC 构建/验证、Payment Headers）
- `libbitfs-go/paymail` -- BRFC 常量（BRFCBitFSBrowse, BRFCBitFSBuy, BRFCBitFSSell）

外部服务通过接口注入（WalletService, ContentStore, MetanetService, SPVService, ChainService），不直接依赖 `libbitfs-go/wallet`、`libbitfs-go/storage`、`libbitfs-go/spv` 的具体类型。

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
- 402 Payment Required（需要支付）
- 404 Not Found（未找到）
- 408 Timeout（超时）
- 409 Transaction Conflict（交易冲突 / HTLC 重放）
- 410 Gone（发票已过期）
- 429 Rate Limited（速率限制）
- 500 Internal Error（内部错误）
- 502 Bad Gateway（SPV 验证失败）
- 503 Service Unavailable（存储不可用 / 发票数量上限）

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

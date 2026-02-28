# 模块规范：libbitfs-go/paymail

## 目的

BitFS 的 Paymail 身份解析模块。解析 `bitfs://alias@domain/path` URI，通过 `.well-known/bsvalias` 发现 Paymail 能力，并将别名解析为 Metanet 根公钥。实现三种寻址模式：Paymail（`@`）、DNSLink（域名）和裸公钥（十六进制）。

设计参考：ConceptDesign #13, #69, #70, #71; SystemDesign 第 6 节; DetailedDesign 第 16-B 节。

## 公共 API

### 类型

```go
// AddressType represents the three BitFS addressing modes.
type AddressType int

const (
    AddressPaymail  AddressType = iota // bitfs://alias@domain/path
    AddressDNSLink                      // bitfs://domain/path
    AddressPubKey                       // bitfs://02abcdef.../path
)

// ParsedURI holds a parsed bitfs:// URI.
type ParsedURI struct {
    Type      AddressType
    Alias     string   // Paymail alias (empty for non-Paymail)
    Domain    string   // Domain name
    PubKey    []byte   // Raw public key bytes (for AddressPubKey)
    Path      string   // Path component after authority
    RawURI    string   // Original URI string
}

// PaymailCapabilities holds discovered Paymail server capabilities.
type PaymailCapabilities struct {
    PKI                string // URL template for public key infrastructure
    PublicProfile      string // URL template for profile info
    VerifyPubKey       string // URL template for key verification
    PaymentDestination string // URL template for P2P payment destination (BRFC 2a40af698840)
}

// PaymentOutput represents a single output in a P2P payment destination response.
type PaymentOutput struct {
    Script   string `json:"script"`
    Satoshis uint64 `json:"satoshis"`
}

// HTTPClient defines the interface for HTTP requests.
// This allows tests to mock HTTP calls.
type HTTPClient interface {
    Get(url string) (*http.Response, error)
}

// PostClient extends HTTPClient with POST capability.
// This is needed for P2P payment destination resolution, which requires
// a POST request after capability discovery (GET).
type PostClient interface {
    HTTPClient
    Post(url, contentType string, body io.Reader) (*http.Response, error)
}

// DNSResolver defines the interface for DNS lookups.
// This allows tests to mock DNS resolution.
type DNSResolver interface {
    LookupSRV(service, proto, name string) (string, []*net.SRV, error)
    LookupTXT(name string) ([]string, error)
}

// DNSSECResolver implements DNSResolver with DNSSEC validation.
// It relies on the upstream recursive resolver to perform DNSSEC validation
// and checks the AD (Authenticated Data) flag in responses.
type DNSSECResolver struct {
    Upstream string // Recursive resolver address (e.g., "8.8.8.8:53")
}

// PKIResponse holds the response from a Paymail PKI endpoint.
type PKIResponse struct {
    BSVAlias string `json:"bsvalias"`
    Handle   string `json:"handle"`
    PubKey   string `json:"pubkey"` // Hex-encoded compressed public key
}
```

### 常量

```go
// MaxPaymailResponseSize is the maximum allowed response body size for Paymail
// HTTP requests (1 MB). This prevents memory exhaustion from malicious servers.
const MaxPaymailResponseSize = 1 << 20

// SRV record types for BitFS.
const (
    SRVPaymail = "bsvalias" // _bsvalias._tcp.{domain}
    SRVBitFS   = "bitfs"    // _bitfs._tcp.{domain}
)
```

### BRFC 常量

```go
// ComputeBRFCID computes a BRFC (Bitcoin Request for Comments) ID.
// ID = hex(SHA256d(title + author + version))[:12]
func ComputeBRFCID(title, author, version string) string

// BitFS-specific BRFC capability IDs, advertised in the Paymail
// .well-known/bsvalias response to signal BitFS protocol support.
var (
    BRFCBitFSBrowse = ComputeBRFCID("BitFS Browse", "BitFS", "1.0")
    BRFCBitFSBuy    = ComputeBRFCID("BitFS Buy", "BitFS", "1.0")
    BRFCBitFSSell   = ComputeBRFCID("BitFS Sell", "BitFS", "1.0")
)
```

### 函数

```go
// ParseURI parses a bitfs:// URI into its components.
// Detects address type based on:
//   - Contains '@' in authority -> Paymail
//   - Authority starts with hex pubkey prefix (02/03) and is 66 hex chars -> PubKey
//   - Otherwise -> DNSLink
func ParseURI(uri string) (*ParsedURI, error)

// DiscoverCapabilities fetches .well-known/bsvalias from a domain
// and returns the Paymail server capabilities.
func DiscoverCapabilities(domain string) (*PaymailCapabilities, error)

// DiscoverCapabilitiesWithClient fetches capabilities using the provided HTTP client.
func DiscoverCapabilitiesWithClient(domain string, client HTTPClient) (*PaymailCapabilities, error)

// ResolvePKI resolves a Paymail alias to its public key using the PKI capability.
// Returns the P_root compressed public key bytes for the alias's vault.
func ResolvePKI(alias, domain string) ([]byte, error)

// ResolvePKIWithClient resolves PKI using the provided HTTP client.
func ResolvePKIWithClient(alias, domain string, client HTTPClient) ([]byte, error)

// ResolvePaymentDestination resolves a Paymail alias to P2P payment destination outputs
// using the default HTTP client.
func ResolvePaymentDestination(alias, domain string) ([]PaymentOutput, error)

// ResolvePaymentDestinationWithClient resolves a Paymail alias to P2P payment
// destination outputs using the provided PostClient.
// It performs capability discovery (GET), then POSTs to the payment destination
// endpoint to obtain output scripts for payment.
func ResolvePaymentDestinationWithClient(alias, domain string, client PostClient) ([]PaymentOutput, error)

// ResolveEndpoints resolves SRV records for a domain.
// recordType should be SRVPaymail ("bsvalias") or SRVBitFS ("bitfs").
// Returns endpoint addresses (host:port) sorted by priority then weight.
func ResolveEndpoints(domain string, recordType string) ([]string, error)

// ResolveEndpointsWithResolver resolves SRV records using the provided DNS resolver.
func ResolveEndpointsWithResolver(domain string, recordType string, resolver DNSResolver) ([]string, error)

// ResolveDNSLinkPubKey resolves _bitfs.{domain} TXT record with bitfs= prefix.
// Returns the P_node compressed public key bytes.
func ResolveDNSLinkPubKey(domain string) ([]byte, error)

// ResolveDNSLinkPubKeyWithResolver resolves the DNSLink public key using the provided DNS resolver.
func ResolveDNSLinkPubKeyWithResolver(domain string, resolver DNSResolver) ([]byte, error)

// ResolveURI performs full URI resolution:
//   1. Parse URI
//   2. Resolve public key (via Paymail PKI, DNSLink TXT, or direct from URI)
//   3. Resolve endpoints (via SRV records)
// Returns (pubkey, endpoints, error).
func ResolveURI(uri string) (pubKey []byte, endpoints []string, err error)

// ResolveURIWith performs full URI resolution with provided client and resolver.
func ResolveURIWith(uri string, client HTTPClient, resolver DNSResolver) ([]byte, []string, error)

// NewDNSSECResolver creates a new DNSSECResolver.
// If upstream is empty, it defaults to "8.8.8.8:53".
func NewDNSSECResolver(upstream string) *DNSSECResolver
```

## 依赖

- `net` -- DNS SRV/TXT 查询
- `net/http` -- 用于 .well-known 发现和 PKI 解析的 HTTP 客户端
- `encoding/json` -- JSON 解析
- `net/url` -- URI 解析
- `crypto/sha256` -- BRFC ID 计算（双 SHA256）
- `io` -- 响应体读取（LimitReader 防护）
- `github.com/miekg/dns` -- DNSSEC 验证查询

## 数据结构

### URI 格式
```
bitfs://[alias@]<authority>/<path>

authority:
  - alias@domain  -> Paymail resolution
  - domain        -> DNSLink resolution
  - 02abcdef...   -> Direct public key

Examples:
  bitfs://alice@example.com/docs/paper.pdf
  bitfs://example.com/docs/paper.pdf
  bitfs://02a1b2c3d4e5f6.../docs/paper.pdf
```

### DNS 记录
```
;; DNSLink identity
_bitfs.example.com          TXT  "bitfs=02a1b2c3d4e5f6..."

;; Service endpoints
_bitfs._tcp.example.com     SRV  10 60 443 cdn1.example.com

;; Paymail endpoints
_bsvalias._tcp.example.com  SRV  10 60 443 cdn1.example.com
```

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrInvalidURI` | URI 不匹配 bitfs:// 方案或格式错误 |
| `ErrDNSLookupFailed` | DNS SRV/TXT 查询失败 |
| `ErrPaymailDiscovery` | .well-known/bsvalias 获取失败 |
| `ErrPKIResolution` | Paymail PKI 端点返回错误 |
| `ErrNoEndpoints` | 未找到域名的 SRV 记录 |
| `ErrInvalidPubKey` | 从 DNS/Paymail 获取的公钥不是有效的压缩 secp256k1 点 |
| `ErrAddressResolution` | P2P 支付目的地解析失败 |
| `ErrDNSSECValidationFailed` | DNS 响应未通过 DNSSEC 验证（AD 标志未设置） |

## DNSSEC 支持

`DNSSECResolver` 类型实现 `DNSResolver` 接口，通过上游递归解析器（默认 `8.8.8.8:53`）验证 DNSSEC。查询设置 EDNS0 DO（DNSSEC OK）标志，并要求响应中的 AD（Authenticated Data）标志。如果 AD 标志未设置，返回 `ErrDNSSECValidationFailed`。

依赖外部包 `github.com/miekg/dns` 进行底层 DNS 协议操作。

## 安全考量

1. **DNS 验证**：对于 DNSLink，双向必须匹配——DNS TXT 指向 P_node 且 Metanet 节点的 domain 字段匹配该域名。防止 DNS 劫持。
2. **Paymail 使用 HTTPS**：所有 Paymail 能力发现和 PKI 解析必须使用 HTTPS。
3. **公钥验证**：从 DNS 或 Paymail 接收的所有公钥必须在使用前验证为有效的 secp256k1 压缩点。
4. **响应大小限制**：所有 HTTP 响应通过 `io.LimitReader` 限制为 `MaxPaymailResponseSize`（1 MB），防止恶意服务器内存耗尽攻击。
5. **路径遍历防护**：PKI 和支付目的地 URL 模板中的 `{alias}` 和 `{domain.tld}` 变量通过 `url.PathEscape` 转义。
6. **DNSSEC 验证**：`DNSSECResolver` 通过检查 AD 标志确保 DNS 响应经过 DNSSEC 验证，防止 DNS 欺骗攻击。

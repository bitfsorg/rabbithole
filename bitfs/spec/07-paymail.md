# 模块规范：internal/paymail

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
    PKI           string // URL template for public key infrastructure
    PublicProfile string // URL template for profile info
    VerifyPubKey  string // URL template for key verification
}

// PKIResponse holds the response from a Paymail PKI endpoint.
type PKIResponse struct {
    BSVAlias string `json:"bsvalias"`
    Handle   string `json:"handle"`
    PubKey   string `json:"pubkey"` // Hex-encoded compressed public key
}
```

### 函数

```go
// ParseURI parses a bitfs:// URI into its components.
// Detects address type based on:
//   - Contains '@' -> Paymail
//   - Starts with hex pubkey prefix (02/03) -> PubKey
//   - Otherwise -> DNSLink
func ParseURI(uri string) (*ParsedURI, error)

// DiscoverCapabilities fetches .well-known/bsvalias from a domain
// and returns the Paymail server capabilities.
func DiscoverCapabilities(domain string) (*PaymailCapabilities, error)

// ResolvePKI resolves a Paymail alias to its public key using the PKI capability.
// Returns the P_root public key for the alias's vault.
func ResolvePKI(alias, domain string) ([]byte, error)

// ResolveEndpoints resolves SRV records for a domain.
// For Paymail: _bsvalias._tcp.{domain}
// For DNSLink: _bitfs._tcp.{domain}
// Returns endpoints sorted by priority/weight.
func ResolveEndpoints(domain string, recordType string) ([]string, error)

// ResolveDNSLinkPubKey resolves _bitfs_pubkey.{domain} TXT record.
// Returns the P_node compressed public key bytes.
func ResolveDNSLinkPubKey(domain string) ([]byte, error)

// ResolveURI performs full URI resolution:
//   1. Parse URI
//   2. Resolve public key (via Paymail PKI or DNSLink TXT)
//   3. Resolve endpoints (via SRV records)
// Returns (pubkey, endpoints, error)
func ResolveURI(uri string) (pubKey []byte, endpoints []string, err error)
```

## 依赖

- `net` -- DNS SRV/TXT 查询
- `net/http` -- 用于 .well-known 发现的 HTTP 客户端
- `encoding/json` -- JSON 解析
- `net/url` -- URI 解析

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
_bitfs_pubkey.example.com   TXT  "02a1b2c3d4e5f6..."

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

## 安全考量

1. **DNS 验证**：对于 DNSLink，双向必须匹配——DNS TXT 指向 P_node 且 Metanet 节点的 domain 字段匹配该域名。防止 DNS 劫持。
2. **Paymail 使用 HTTPS**：所有 Paymail 能力发现和 PKI 解析必须使用 HTTPS。
3. **公钥验证**：从 DNS 或 Paymail 接收的所有公钥必须在使用前验证为有效的 secp256k1 压缩点。

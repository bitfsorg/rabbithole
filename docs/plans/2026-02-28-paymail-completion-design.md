# Paymail 功能完善设计

## 背景

设计文档 §16-B/D/E/F 定义了完整的 Paymail 集成方案，当前实现缺失 5 项功能：

| # | 功能 | 来源 | 状态 |
|---|---|---|---|
| 1 | PublicProfile handler | §16-B, H6 审计 | daemon 广播了 capability 但无 handler (404) |
| 2 | VerifyPubKey handler | §16-B, H6 审计 | 未广播也无 handler |
| 3 | Address Resolution | §16-D | 未实现 |
| 4 | Custom BRFC | §16-E | 未实现 |
| 5 | DNSSEC 验证 | §16-F | 未实现 |

## 设计决策

- PublicProfile 返回 Paymail 标准字段 (name, domain, avatar)，avatar 可以是 `bitfs://` URI
- DNSSEC 使用 `miekg/dns` 完整实现签名链验证
- Custom BRFC 只做能力广播 + BRFC ID 生成，指向现有 daemon 端点（方案 A）
- Address Resolution 实现标准 Paymail P2P Payment Destination 协议（BRFC `2a40af698840`）

## 1. PublicProfile Handler

### 端点

`GET /api/v1/public-profile/{handle}` — handle 格式 `alias@domain`

### 响应

```json
{
  "name": "alice",
  "domain": "example.com",
  "avatar": "bitfs://02a1b2c3.../avatar.jpg"
}
```

### 实现

- `daemon/paymail.go` 新增 `handlePublicProfile()`
- alias → vault 映射复用现有 `d.wallet.GetVaultPubKey(alias)` 逻辑
- avatar 来源：daemon 配置 `paymail.avatars.<alias>`，无配置时返回空字符串
- 404 如果 alias 不存在

## 2. VerifyPubKey Handler

### 端点

`GET /api/v1/verify/{handle}/{pubkey}` — handle 格式 `alias@domain`

### 响应

```json
{
  "handle": "alice@example.com",
  "pubkey": "02a1b2c3...",
  "match": true
}
```

### 实现

- `daemon/paymail.go` 新增 `handleVerifyPubKey()`
- 调用 `d.wallet.GetVaultPubKey(alias)` 获取真实公钥
- 比较 hex 编码后的公钥是否相等
- alias 不存在时 `match: false`（不暴露 alias 是否存在）
- `handleBSVAlias()` 补上 `a9f510c16bde` capability 广播

## 3. Address Resolution（客户端）

### 用途

revshare 分红时，将股东 Paymail handle 解析为 BSV 支付地址。每次调用返回不同 HD 派生地址，增强隐私。

### API

```go
// paymail/address.go
func ResolvePaymentDestination(alias, domain string, opts ...ResolveOption) (string, error)
```

### 协议

标准 Paymail P2P Payment Destination（BRFC `2a40af698840`）：

1. `DiscoverCapabilities(domain)` → 获取 `paymentDestination` URL 模板
2. POST 该 URL，body: `{ "senderName": "...", "senderHandle": "...", "dt": "...", "amount": 0, "purpose": "revshare" }`
3. 响应包含 output script 或 address

### PaymailCapabilities 扩展

```go
type PaymailCapabilities struct {
    PKI                string // 已有
    PublicProfile      string // 已有
    VerifyPubKey       string // 已有
    PaymentDestination string // 新增: BRFC 2a40af698840
}
```

## 4. Custom BRFC Capabilities

### BRFC ID 生成

按 BRC 标准：`ID = hex(SHA256d(title + author + version))[:12]`

```go
// paymail/brfc.go
func ComputeBRFCID(title, author, version string) string

const (
    BRFCBitFSBrowse = ComputeBRFCID("BitFS Browse", "BitFS", "1.0")
    BRFCBitFSBuy    = ComputeBRFCID("BitFS Buy", "BitFS", "1.0")
    BRFCBitFSSell   = ComputeBRFCID("BitFS Sell", "BitFS", "1.0")
)
```

### 能力广播

`handleBSVAlias()` 新增 3 个 capability 条目，指向现有端点：

```json
{
  "<bitfs-browse-id>": "https://{host}/_bitfs/meta/{pnode}/{path}",
  "<bitfs-buy-id>":    "https://{host}/_bitfs/buy/{txid}",
  "<bitfs-sell-id>":   "https://{host}/_bitfs/sales"
}
```

无需新建 handler。

## 5. DNSSEC 验证

### 方案

使用 `miekg/dns` 库实现完整 DNSSEC 签名链验证。

### 接口

```go
// paymail/dnssec.go
type DNSSECResolver struct {
    Upstream string // 递归解析器地址 (默认 "8.8.8.8:53")
}

// 实现现有 DNSResolver 接口
func (r *DNSSECResolver) LookupSRV(service, proto, domain string) ([]*net.SRV, error)
func (r *DNSSECResolver) LookupTXT(domain string) ([]string, error)
```

### 验证流程

1. 发送 DNS 查询，设置 DO (DNSSEC OK) flag
2. 获取 RRSIG + DNSKEY 记录
3. 验证签名链：根信任锚 → KSK → ZSK → RRSet
4. 验证通过返回结果，失败返回 `ErrDNSSECValidationFailed`

### 集成

现有代码通过 `DNSResolver` 接口抽象 DNS 查询。`DNSSECResolver` 实现该接口，通过构造参数注入：

- 不启用 DNSSEC：使用标准库 resolver（默认，行为不变）
- 启用 DNSSEC：注入 `DNSSECResolver`

### 根信任锚

硬编码 IANA root KSK（`Kjqmt7v.`，ID 20326），与 `miekg/dns` 示例一致。

## 文件变更清单

| 文件 | 变更类型 | 内容 |
|---|---|---|
| `libbitfs-go/paymail/brfc.go` | 新建 | BRFC ID 计算函数 + 3 个常量 |
| `libbitfs-go/paymail/brfc_test.go` | 新建 | BRFC ID 测试 |
| `libbitfs-go/paymail/address.go` | 新建 | ResolvePaymentDestination() |
| `libbitfs-go/paymail/address_test.go` | 新建 | Address Resolution 测试 |
| `libbitfs-go/paymail/dnssec.go` | 新建 | DNSSECResolver 实现 |
| `libbitfs-go/paymail/dnssec_test.go` | 新建 | DNSSEC 验证测试 |
| `libbitfs-go/paymail/resolve.go` | 修改 | PaymailCapabilities 新增 PaymentDestination 字段 |
| `libbitfs-go/paymail/errors.go` | 修改 | 新增 ErrDNSSECValidationFailed 等错误 |
| `libbitfs-go/go.mod` | 修改 | 新增 `github.com/miekg/dns` 依赖 |
| `bitfs/internal/daemon/paymail.go` | 修改 | 新增 handlePublicProfile, handleVerifyPubKey |
| `bitfs/internal/daemon/routes.go` | 修改 | 新增路由注册 + BRFC + VerifyPubKey 广播 |

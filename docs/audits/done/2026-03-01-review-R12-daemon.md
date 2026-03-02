# Review R12: daemon

> **Status**: All findings confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md` §2.2. Archived 2026-03-03.

## Overview

| Metric | Value |
|--------|-------|
| Package | `bitfs/internal/daemon` |
| Source files | 10 (`daemon.go`, `payment.go`, `routes.go`, `content.go`, `handshake.go`, `dashboard.go`, `paymail.go`, `logbuf.go`, `errors.go`, `spv.go`) |
| Test files | 9 |
| Source LOC | ~2,504 |
| Test LOC | ~4,674 |
| Test count | 205 |
| Coverage | 85.4% |
| Race detector | PASS |
| Spec | `docs/specs/bitfs/09-daemon.md` |

### Public HTTP Endpoints (20)

| Method | Pattern | Handler |
|--------|---------|---------|
| GET | `/_bitfs/health` | `handleHealth` |
| POST | `/_bitfs/handshake` | `handleHandshake` |
| GET | `/_bitfs/data/{hash}` | `handleData` |
| GET | `/_bitfs/meta/{pnode}/{path...}` | `handleMeta` |
| GET | `/_bitfs/versions/{pnode}/{path...}` | `handleVersions` |
| GET | `/_bitfs/buy/{txid}` | `handleGetBuyInfo` |
| POST | `/_bitfs/buy/{txid}` | `handleSubmitHTLC` |
| POST | `/_bitfs/pay/{invoice_id}` | `handlePayInvoice` |
| GET | `/_bitfs/sales` | `handleSales` |
| GET | `/_bitfs/spv/proof/{txid}` | `handleSPVProof` |
| GET | `/_bitfs/dashboard/status` | `handleDashboardStatus` |
| GET | `/_bitfs/dashboard/storage` | `handleDashboardStorage` |
| GET | `/_bitfs/dashboard/wallet` | `handleDashboardWallet` |
| GET | `/_bitfs/dashboard/network` | `handleDashboardNetwork` |
| GET | `/_bitfs/dashboard/logs` | `handleDashboardLogs` |
| GET | `/.well-known/bsvalias` | `handleBSVAlias` |
| GET | `/api/v1/pki/{handle}` | `handlePKI` |
| GET | `/api/v1/public-profile/{handle}` | `handlePublicProfile` |
| GET | `/api/v1/verify/{handle}/{pubkey}` | `handleVerifyPubKey` |
| GET | `/` | `handleRootOrPath` |

## Findings

### CRITICAL

无。

### HIGH

**H-1: `handleSubmitHTLC` 静默回退 P2PKH — HTLC 构建失败时安全降级**

`payment.go` L200-213: `x402.BuildHTLC` 返回 `([]byte, error)`，error 被 `_` 抑制。构建失败时 `htlcScript` 为 nil，`invoice.HTLCScript` 不设置。`handleSubmitHTLC` L298-323 检查 `len(invoice.HTLCScript) > 0`，为 false 时回退到更弱的 P2PKH 验证。HTLC 脚本构建错误与合法的向后兼容场景不可区分，安全性静默降级。

**建议**: 记录 `BuildHTLC` 错误日志，或将错误传播为 invoice 创建失败。

**H-2: `handleSales` 和全部 5 个 dashboard 端点无认证**

`payment.go` L535: `handleSales` 暴露所有 invoice 的 `invoice_id`、`price`、`key_hash`。`dashboard.go` L16-120: 5 个 dashboard 端点暴露 `vault_pnode`、seller pubkey、`storage_path`、`listen_addr`。所有端点仅有 CORS + 限速中间件，无任何认证检查。网络上任何客户端均可枚举敏感状态。

**建议**: 添加 bearer token 或 localhost-only 绑定。

### MEDIUM

**M-1: `handleMeta` 泄露 paid/private 节点的 `key_hash`**

`content.go` L130-132: `handleMeta` 对所有 access level 的节点返回 `key_hash`。对比 `routes.go` L293-295 的 `serveJSON` 正确限制为 `node.Access == "free"` 时才返回。不一致的门控导致 paid/private 内容的加密材料可被未认证客户端获取。

**建议**: 添加 access level 检查，与 `serveJSON` 一致。

**M-2: `handleGetBuyInfo` 在全局写锁内执行所有加密操作**

`payment.go` L184-225: `invoicesMu.Lock()` 持有期间执行 `ec.PublicKeyFromBytes`、`DeriveNodeKeyPair`（钱包 I/O）、`ComputeCapsuleWithNonce`（ECDH ~71µs）、`ComputeCapsuleHash`、`GetSellerKeyPair`、`BuildHTLC`。所有 CPU 密集型加密和潜在阻塞的钱包操作在全局写锁下运行，并发 buy 请求完全串行化。

**建议**: 将加密计算移出锁范围，仅锁定状态更新。

**M-3: `Start()` 吞没 `ListenAndServe` 错误**

`daemon.go` L344-356: `ListenAndServe` 在 goroutine 中调用。端口被占用或其他绑定错误时，`Start()` 返回 nil（成功），错误静默丢弃。goroutine 仅将 `d.running` 翻回 false，无通道、回调或日志通知。Daemon 看似已启动实则未启动。

**建议**: 使用 channel 传递 goroutine 内的首次错误到 `Start()` 调用者。

**M-4: `invoiceIDBytes` hex 解码错误静默丢弃**

`payment.go` L195: `invoiceIDBytes, _ := hex.DecodeString(invoice.ID)`。`generateInvoiceID` 目前返回 16 字节随机 hex 可正确解码。但若 ID 格式变化，解码失败导致 `invoiceIDBytes` 为 nil，capsule nonce 为空 — 破坏注释声明的不可关联性保证。

**建议**: 检查错误，解码失败时返回 500。

**M-5: 成功响应体无大小限制**

`payment.go` 多处: JSON 成功响应路径的 `json.NewDecoder(resp.Body)` 无 `io.LimitReader` 包装。错误路径已正确限制到 1024 字节。恶意 RPC 节点可发送超大 JSON 载荷导致 OOM。

**建议**: 用 `io.LimitReader` 包装成功响应体（如 64MB 上限）。

**M-6: Context 取消后可能发生支付回滚**

`payment.go` L356, L509: `BroadcastTx(r.Context(), txHex)` 使用 HTTP 连接上下文。客户端在交易提交到网络后、收到响应前断开，上下文取消触发错误路径，`invoice.Paid` 回滚为 false。交易可能已被网络接受，导致重复支付风险。

**建议**: 广播使用 `context.Background()` 的派生 context，或将广播状态与连接生命周期解耦。

### LOW

**L-1: `handlePayInvoice` 覆盖率最低 (51.3%)**: 多个分支未测试。

**L-2: `computeECDH` padding 分支未覆盖**: 正确实现但测试未触发 <32 字节 X 坐标路径。

**L-3: `handleData` Size/Get 操作间存在 TOCTOU**: 先查大小再读内容，中间文件可能被修改。

**L-4: `MaxRequestSize` config 声明但从未应用**: 路由注册未使用此配置。

**L-5: `handleSales` limit 参数无上限**: 可请求任意大的 limit 值。

**L-6: SPV txid 未验证格式**: 直接传递到 RPC 查询。

### SUGGESTIONS

**S-1.** 添加认证中间件（至少 dashboard + sales 端点），支持 Bearer token。

**S-2.** `handleGetBuyInfo` 的 capsule 计算可缓存 — 同一 buyer pubkey 重复请求无需重新计算。

**S-3.** 添加结构化日志替代静默错误丢弃。

## Code Quality Assessment

**优点**:
1. 20 个端点清晰分文件组织
2. 共享 `withMiddleware` 包装器（CORS + 限速）
3. HTLC 验证后的回滚模式（`rollbackPaid` + `usedTxIDs` 清理）
4. 85.4% 覆盖率，205 个测试
5. 正确的 `io.LimitReader` 错误响应体保护

**不足**:
1. 无任何认证机制（dashboard/sales 公开暴露敏感状态）
2. 全局写锁内执行 CPU 密集加密操作
3. 多处错误静默丢弃（`BuildHTLC`、`hex.DecodeString`）
4. `ListenAndServe` 错误不传播到调用者
5. `handleMeta` 与 `serveJSON` 的 key_hash 门控不一致

## Summary

daemon 是 BitFS 的核心 HTTP 服务层，20 个端点覆盖内容服务、Method 42 握手、HTLC 支付、Paymail。代码组织清晰，测试覆盖良好。

**必须修复**:
- H-1: HTLC 构建失败时的静默 P2PKH 回退
- H-2: Dashboard 和 sales 端点无认证

**应该修复**:
- M-1: key_hash 泄露不一致
- M-2: 全局锁内的加密操作瓶颈
- M-3: Start() 吞没绑定错误

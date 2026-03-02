# Review R14: client

> **Status**: All findings confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md` §2.4. Archived 2026-03-03.

## Overview

| Metric | Value |
|--------|-------|
| Package | `bitfs/internal/client` |
| Source files | 4 (`client.go`, `resolve.go`, `cache.go`, `cached.go`) |
| Test files | 4 (`client_test.go`, `resolve_test.go`, `cache_test.go`, `cached_test.go`) |
| Source LOC | ~589 |
| Test LOC | ~1,102 |
| Test count | 66 |
| Coverage | 97.3% |
| Race detector | PASS |

## Findings

### CRITICAL

无。

### HIGH

无。

### MEDIUM

**M-1: txid 未验证/转义 — URL 路径注入**

`client.go` L175, L203, L282: `GetBuyInfo`、`SubmitHTLC`、`VerifySPV` 三个方法将 `txid` 直接拼入 URL 路径，无验证和转义：
```go
reqURL := fmt.Sprintf("%s/_bitfs/buy/%s", c.BaseURL, txid)
```

对比 `GetMeta` (L117) 和 `GetVersions` (L242) 对 `pnode` 使用 `validateHex` 验证 + `url.PathEscape` 转义。含 `/`、`?`、`#` 的 txid 会破坏 URL 路径。

**建议**: 统一对所有路径参数使用 `validateHex(txid, 32)` + `url.PathEscape`。

**M-2: JSON 成功响应无大小限制**

`client.go` L140, L193, L222, L265, L295, L317: 所有 6 个 JSON 端点的 `json.NewDecoder(resp.Body)` 直接解码无大小限制。错误路径 `checkStatus` (L331) 正确使用 `io.LimitReader(resp.Body, 1024)`。恶意 daemon 可发送任意大 JSON 导致 OOM。`GetData` (L149-168) 正确免除 — 返回流式 `ReadCloser`。

**建议**: 使用 `json.NewDecoder(io.LimitReader(resp.Body, maxSize))`。

**M-3: Paymail 解析使用无超时的 `http.DefaultClient`**

`resolve.go` L29-31: `httpClient == nil` 时回退到 `paymail.DefaultHTTPClient`（即 `http.DefaultClient`，无超时）。所有 5 个 b-tools 以 `nil` 调用，继承此风险。挂起的 `.well-known/bsvalias` 端点会无限阻塞。`Client` 自身有 30s 超时但仅用于 daemon API 调用，不用于 paymail 解析。

**建议**: 创建带 30s 超时的 paymail HTTP 客户端。

**M-4: SRV 解析端点无 HTTPS 强制**

`resolve.go` L73-78: `endpointToBaseURL` 对已有 `http://` 前缀的端点直接放行。DNS SRV 或 paymail 服务器返回 HTTP URL 时静默降级为明文传输。后续 API 调用传输加密材料（capsule、公钥），中间人可拦截。

**建议**: 拒绝非 HTTPS 端点或至少记录警告。

### LOW

**L-1: `cacheKey` 拼接使用 `/` 分隔符**: `cache.go` L32-35: `pnode + "/" + path`。由于 pnode 固定为 66 字符 hex 字符串，`/` 不可能出现在 pnode 中，无实际碰撞风险。安全。

**L-2: `GetSales` limit 参数未验证**: `client.go` — 负值或极大值直接传入 URL。

### SUGGESTIONS

**S-1.** 为所有方法添加 `context.Context` 参数。

**S-2.** `GetBuyInfo` 返回的 `BuyInfo` 缺少 `InvoiceID` 字段（见 R13 C-1）。

**S-3.** 考虑为 `MetaCache` 添加最大缓存大小限制。

## Code Quality Assessment

**优点**:
1. 97.3% 覆盖率 — 本代码库最高
2. 清晰的 MetaCache 实现（hash-sharded 目录、TTL 过期、条件跳过）
3. CachedClient 装饰器模式优雅（仅缓存 meta 查询，透传其余）
4. 双重 `%w` 错误包装（Go 1.20+）
5. `validateHex` 辅助函数统一输入验证（部分使用）

**不足**:
1. txid 验证不一致（3 个方法未验证）
2. JSON 成功路径无大小限制
3. Paymail 解析继承全局无超时客户端
4. SRV 端点不强制 HTTPS

## Summary

client 包质量高，97.3% 覆盖率，架构清晰。无 CRITICAL/HIGH 问题。4 个 MEDIUM 均为防御性编程缺失（输入验证不一致、响应大小、超时、HTTPS 强制）。最可操作的是 M-1（统一 txid 验证，三行修改）和 M-2（添加 LimitReader）。

注意 `BuyInfo` 缺少 `InvoiceID` 字段 — 此问题的 CRITICAL 影响在 R13 (buy) 中记录。

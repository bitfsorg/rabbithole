# Review R09: paymail

## Overview

| Metric | Value |
|--------|-------|
| Package | `libbitfs-go/paymail` |
| Source files | 7 (`address.go`, `brfc.go`, `dns.go`, `dnssec.go`, `errors.go`, `resolve.go`, `uri.go`) |
| Test files | 5 (`address_test.go`, `brfc_test.go`, `coverage_supplement_test.go`, `dnssec_test.go`, `paymail_test.go`) |
| Source LOC | ~664 |
| Test LOC | ~1,626 |
| Test count | ~50 |
| Coverage | 80.4% |
| Race detector | PASS |
| Spec | `docs/specs/bitfs/07-paymail.md` |

## Findings

### CRITICAL

无。

### HIGH

**H-1: SSRF — 服务器控制的 URL 模板无域名验证**

`resolve.go` L142-146, `address.go` L71-77: Capability 发现从远程服务器获取 URL 模板，代码替换 `{alias}` 和 `{domain.tld}` 并使用 `url.PathEscape`。但模板 URL 本身完全由服务器控制，恶意服务器可返回指向内部服务的 URL。

HTTPS scheme 检查过滤非 HTTPS，但不验证 URL 指向原始查询的域。

**建议**: 模板展开后验证 hostname 匹配原始域。或文档化为已知信任边界（Paymail 协议设计如此 — 服务器被信任返回自己的 URL）。

**H-2: HTTPS 验证逻辑漏洞 — 空 scheme 通过**

`resolve.go` L101-103:
```go
if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "") {
    continue
}
```

空 scheme (`""`) 通过检查。协议相对 URL（`//evil.com/...`）或相对路径（`/api/pki/`）会被接受。

**建议**: 改为 `parsed.Scheme != "https"`，严格只接受 HTTPS。一行修改。

### MEDIUM

**M-1: 无 HTTP 请求超时 — 连接可能无限挂起**

`address.go` L23-29, `resolve.go` L34-35: 使用 `http.DefaultClient`，无超时。恶意 Paymail 服务器可无限持有连接。2026-02-26 审计 (H3) 已标识此问题，仍未修复。

**建议**: 使用 `http.Client{Timeout: 30 * time.Second}`。

**M-2: Capability key 匹配过于宽泛（`strings.Contains`）**

`resolve.go` L106-114: `strings.Contains(key, "pki")` 过于宽泛。`"custom-spki-pinning"` 或 `"pkibackup"` 会错误匹配。Map 迭代无序，结果不确定。

**建议**: 仅使用精确匹配。

**M-3: SRV 权重选择为确定性排序，非概率性**

`dns.go` L67-72: RFC 2782 规定同优先级内按权重概率选择，当前实现总是选最高权重，违反 load-distribution 目的。

**建议**: 实现权重随机选择或文档化简化行为。

**M-4: DNSSEC 验证仅依赖上游 resolver 的 AD 标志**

`dnssec.go` L65-69: 信任到上游 resolver（默认 `8.8.8.8:53`）的 UDP 路径。MITM 可伪造 AD 标志。代码已文档化此限制。

**建议**: 考虑支持 DNS-over-HTTPS。

### LOW

**L-1: 默认客户端为可变包级 `var`**: 可被外部代码修改影响全局。

**L-2: `ResolvePaymentDestination` 硬编码 sender metadata**: `"senderName":"BitFS","purpose":"revshare"` — 作为通用库应参数化。

**L-3: `ResolveDNSLinkPubKey` 验证顺序正确**: 无实际 bug。

**L-4: 双重 trailing dot 去除**: `dnssec.go` L89 和 `dns.go` L76 都调用 `TrimSuffix(".")`，幂等但冗余。

### SUGGESTIONS

**S-1.** 测试中 URL 重写逻辑重复，应提取共享。

**S-2.** BRFC 常量可预计算为 `const`，避免 init 时间计算和可变性。

**S-3.** 添加 `context.Context` 支持 — 网络请求包的标准做法。

**S-4.** 公钥验证仅检查格式（33 字节 + 02/03 前缀），不验证点在 secp256k1 曲线上。Spec 安全要求 "valid compressed secp256k1 points"。

## Spec Consistency

| Spec 项目 | 状态 |
|---|---|
| AddressType 枚举 | MATCH |
| ParsedURI / PaymailCapabilities / PaymentOutput 结构体 | MATCH |
| HTTPClient / PostClient / DNSResolver 接口 | MATCH |
| DNSSECResolver / PKIResponse 结构体 | MATCH |
| MaxPaymailResponseSize = 1<<20 | MATCH |
| SRVPaymail / SRVBitFS 常量 | MATCH |
| ComputeBRFCID + BRFCBitFS* 变量 | MATCH |
| ParseURI (三种模式) | MATCH |
| DiscoverCapabilities / ResolvePKI / ResolvePaymentDestination | MATCH |
| ResolveEndpoints / ResolveDNSLinkPubKey / ResolveURI | MATCH |
| NewDNSSECResolver (默认 8.8.8.8:53) | MATCH |
| 8 个 Error sentinels | MATCH |
| HTTPS 强制 | **MINOR** — 空 scheme 漏洞 (H-2) |
| 响应大小限制 | MATCH |
| PubKey 验证 | **MINOR** — 仅格式验证，非曲线验证 (S-4) |

## Code Quality Assessment

**优点**:
1. 优秀的依赖注入（`*WithClient/*WithResolver` 变体）
2. 一致的 `io.LimitReader` 防御
3. 强错误设计（8 个 sentinel，`%w` 包装）
4. 80.4% 覆盖率，含边界测试
5. 文件职责清晰分离

**不足**:
1. 无 `context.Context` 支持
2. 无 HTTP 超时（审计已标识，仍未修复）
3. `dnssec.go` 函数 0% 覆盖率（仅 integration test）
4. 测试基础设施重复

## Summary

paymail 包设计良好，spec 一致性高。H-2（空 scheme）是一行修复。M-1（HTTP 超时）是之前审计已标识的遗留问题。H-1（SSRF）可能是 Paymail 协议设计的固有特性，但应显式文档化为信任边界。

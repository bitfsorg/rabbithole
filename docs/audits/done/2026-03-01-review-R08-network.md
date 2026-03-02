# Review R08: network

> **Status**: All findings confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md` §1.8. Archived 2026-03-03.

## Overview

| Metric | Value |
|--------|-------|
| Package | `libbitfs-go/network` |
| Source files | 7 (`service.go`, `rpc.go`, `rpc_blockchain.go`, `spvclient.go`, `mock.go`, `config.go`, `errors.go`) |
| Test files | 6 |
| Source LOC | ~984 |
| Test LOC | ~1,694 |
| Test count | 59 |
| Coverage | 90.7% |
| Race detector | PASS |
| Spec | `docs/specs/bitfs/13-network.md` |

## Findings

### CRITICAL

无。

### HIGH

无。

### MEDIUM

**M-1: 成功响应体无大小限制 — 潜在内存耗尽**

`rpc.go` L110: 错误路径正确限制到 1024 字节，但成功路径的 JSON 解码无大小限制。恶意 RPC 节点可发送超大 JSON 载荷导致 OOM。

**建议**: 用 `io.LimitReader` 包装成功响应体（如 64MB 上限）。

**M-2: HTTP 401 未映射到 `ErrAuthFailed`**

`rpc.go` L104-107: `ErrAuthFailed` 已声明但从未使用。401 被包装为 `ErrConnectionFailed`，调用者无法区分认证失败和连接问题。

**建议**: 401/403 时返回 `ErrAuthFailed`。

**M-3: `bytesEqual` 重复标准库 `bytes.Equal`**

`spvclient.go` L189-199: 与同文件已导入的 `bytes.Equal` 完全重复。L101 用 `bytesEqual`，L176 用 `bytes.Equal`，不一致。

**建议**: 删除 `bytesEqual`，替换为 `bytes.Equal`。

**M-4: `SyncHeaders` 通过类型断言紧耦合 `RPCClient`**

`spvclient.go` L36-43: `getBlockHash` 仅在 `chain` 为 `*RPCClient` 时自动配置。其他 `BlockchainService` 实现无法使用 `SyncHeaders`。

**建议**: 将 `GetBlockHash` 加入接口，或作为显式参数。

### LOW

**L-1: 无 HTTP 重定向策略**: RPC 客户端使用默认策略（最多 10 次重定向），可能导致凭证转发。建议拒绝所有重定向。

**L-2: `uint64` 到 `uint32` 高度截断无溢出检查**: `spvclient.go` L79, L162 — 静默截断。建议添加 `> MaxUint32` 检查。

**L-3: `GetTxStatus` 不填充 `TxIndex`**: 始终为 0。BSV RPC 不提供此字段。建议文档化。

**L-4: `Mock.ListUnspent` 等方法 nil 函数时 panic**: 除 `ImportAddress` 外无 nil 保护，不一致。

**L-5: `ResolveConfig` 不验证 URL 格式**: 仅检查非空。无效 URL 在连接时才失败。

### SUGGESTIONS

**S-1.** 为 `RPCConfig` 添加 `String()` 方法隐藏密码。

**S-2.** `GetBestBlockHeight` 简化 — 直接解码 float64，无需 `json.RawMessage` 中间步骤。

**S-3.** 考虑用 `json.Number` 替代 `float64` 解析区块高度，避免精度损失。

## Spec Consistency

| Spec 项目 | 状态 |
|---|---|
| BlockchainService 接口 (9 methods) | MATCH |
| UTXO / TxStatus / MerkleProof / RPCConfig / VerifyResult 结构体 | MATCH |
| RPCClient + NewRPCClient (30s timeout, 10 max idle) | MATCH |
| Call 方法 | MATCH |
| 9 个 RPC 方法实现 | MATCH |
| SPVClient / NewSPVClient / VerifyTx / SyncHeaders | MATCH |
| MockBlockchainService (9 fields) | MATCH |
| NetworkPresets (3 entries) | MATCH |
| ResolveConfig (3-layer priority) | MATCH |
| ErrAuthFailed | **DEVIATION** — 声明但未使用 |
| 其他 4 个 Error sentinels | MATCH |
| BIP37 maxTxs = 1<<20 | MATCH |

**结论**: 31/32 项匹配。1 个偏差（ErrAuthFailed 未使用）。

## Code Quality Assessment

**优点**:
1. 干净的接口设计 + 编译时接口检查
2. 彻底的 BIP37 CMerkleBlock 解析器 + OOM 保护
3. 强防御编码（readVarInt 截断处理、hash count 验证）
4. 90.7% 覆盖率，含 BIP37 往返测试
5. JSON-RPC 响应 ID 验证
6. Config 解析层次清晰，mainnet 无默认预设

**不足**:
1. SyncHeaders 紧耦合 RPCClient
2. bytesEqual 死代码
3. ErrAuthFailed 死代码
4. 成功响应无大小限制

## Summary

network 包实现良好，spec 一致性 31/32，测试覆盖 90.7%。无 CRITICAL/HIGH 问题。最可操作的是 M-1（成功响应无大小限制）和 M-2（ErrAuthFailed 死代码）。BIP37 解析、字节序处理、BTC-satoshi 转换等 Bitcoin 协议细节正确。

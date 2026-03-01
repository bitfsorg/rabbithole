# Review R06: metanet

## Overview

| Metric | Value |
|--------|-------|
| Package | `libbitfs-go/metanet` |
| Source files | 8 (`node.go`, `directory.go`, `parser.go`, `merkle.go`, `link.go`, `resolve.go`, `cltv.go`, `errors.go`) |
| Test files | 6 (`metanet_test.go`, `merkle_test.go`, `parser_extended_test.go`, `anchor_test.go`, `cltv_test.go`, `fuzz_test.go`) |
| Source LOC | ~1,513 |
| Test LOC | ~3,774 |
| Test count | 170 + 4 fuzz targets |
| Coverage | 94.7% |
| Race detector | PASS |
| Spec | `docs/specs/bitfs/03-metanet.md` |

## Findings

### CRITICAL

无。

### HIGH

无。

### MEDIUM

**M-1: TLV 反序列化静默忽略已知字段的错误长度**

`parser.go` L479-486 及整个 switch 块: 当已知固定长度字段（Version, Type, Op, FileSize, Access 等）收到错误长度时，静默保留零值。损坏或恶意载荷可导致 `Type=0 (FILE)` 被接受，而实际意图是 DIR。

```go
case tagVersion:
    if length == 4 {
        node.Version = binary.LittleEndian.Uint32(value)
    }
    // else: 静默忽略 — 使用零值
```

**建议**: 已知字段错误长度时返回错误。仅对未知 tag 保留静默跳过。

**M-2: `CheckCLTVAccess` 不防护 nil node**

`cltv.go` L15: nil `node` 导致 panic。其他所有公共函数都有 nil 保护。

**建议**: 添加 `if node == nil { return CLTVAllowed }`。

**M-3: `VerifyChildMembership` 使用手动字节比较 + 脆弱的 nil-proof fallthrough**

`merkle.go` L119-147: 手动字节循环应用 `bytes.Equal`。单子节点特例通过 `spv.ComputeMerkleRoot` 返回 nil 来触发，耦合到实现细节。

**建议**: 在调用 `spv.ComputeMerkleRoot` 前显式处理单子节点。

**M-4: `serializeChildEntry` 复用已分配的 `b` slice**

`parser.go` L418-449: Index 和 Type 字段复用同一 `b` slice。功能正确（append 会复制），但模式脆弱。

**建议**: 每个字段分配新 slice 或文档化安全性。

**M-5: `deserializePayload` 无最大载荷大小检查**

`parser.go` L454: 无 Children 或 ContentTxIDs 数量上限。LEB128 varint 可编码 2^63。BSV OP_RETURN ~100KB 限制了实际风险，但代码无显式保护。

**建议**: 添加重复字段数量限制（如 10,000）。

### LOW

**L-1: 两个声明的 error sentinel 从未使用**: `ErrNotFile` 和 `ErrAboveRoot` — 死代码。

**L-2: `ErrTotalLinkBudgetExceeded` 用 `fmt.Errorf` 而非 `errors.New`**: 且定义在 `resolve.go` 而非 `errors.go`。

**L-3: `deserializePayload` L457 有冗余边界检查**: for 循环条件已保证 `offset < len(data)`，内部再检查永远为 false。

**L-4: `deserializeMetadata` 不限制条目数量**: 可创建大 map。

**L-5: `serializeMetadata` map 迭代顺序不确定**: 相同 Node 的两次序列化可能产生不同字节。影响签名/哈希的可重现性。

**建议**: 序列化前对 key 排序。

**L-6: `InheritPricePerKB` 复用 `MaxLinkDepth` 作遍历深度**: 概念上目录树深度与链接深度不同。

### SUGGESTIONS

**S-1.** `ListDirectory` 返回浅拷贝 — `PubKey` slice 共享底层数组。应深拷贝或文档化。

**S-2.** `NodeType` 为 int32，接受负数。反序列化无验证。

**S-3.** Fuzz target `FuzzSerializeParseRoundTrip` 限制 `nodeType % 3`，排除 ANCHOR (type 3)。改为 `% 4`。

## Spec Consistency

| Spec 项目 | 状态 |
|---|---|
| NodeType / OpType / LinkType / AccessLevel / ISOStatus 常量 | MATCH |
| ISOConfig / ChildEntry / Node / ResolveResult 结构体 | MATCH |
| NodeStore 接口 (4 methods) | MATCH |
| ParseNode / ResolvePath / ListDirectory | MATCH |
| FollowLink / FindChild / AddChild / RemoveChild / RenameChild | MATCH |
| LatestVersion / InheritPricePerKB / SerializePayload | MATCH |
| CheckCLTVAccess | MATCH |
| Merkle 函数 (4) | MATCH |
| 27+ TLV tags | MATCH |
| ChildEntry / ISOConfig sub-TLV 编码 | MATCH |
| ParseNodeFromPushesWithTxID | **DEVIATION** — 不在 spec 中，便利包装 |
| SplitPath / NextChildIndex / MaxPathComponents 等 | **DEVIATION** — 不在 spec 中，应补充 |
| `metanet/tlv` 子包 | **DEVIATION** — TLV 内联在 parser.go，非独立包 |
| ISO 状态机验证 | **DEVIATION** — spec 说 "单向转换" 但代码无强制 |
| ErrNotFile / ErrAboveRoot | DEAD CODE |

## Code Quality Assessment

**优点**:
1. 94.7% 覆盖率 — 本代码库最高
2. 4 个 fuzz target 防止畸形输入崩溃
3. `validateChildName` 彻底（拒绝 `/`, `.`, `..`, null, 控制字符）
4. MerkleRoot 在每次目录变更时自动重算
5. Merkle 实现与 `spv.BuildMerkleTree` 交叉验证
6. 未知 tag 的前向兼容性

**不足**:
1. 已知字段错误长度静默忽略
2. Metadata 序列化不确定性
3. 两个死 error sentinel
4. ISO 状态转换无验证

## Summary

metanet 是本代码库中质量最高的包。核心 DAG 数据模型、TLV 协议、目录操作、Merkle 树、路径解析全部正确且彻底测试。无 CRITICAL/HIGH 发现。

M-1（已知字段错误长度静默接受）是最有架构意义的发现 — 可能掩盖数据损坏。L-5（非确定性序列化）在载荷需要签名/哈希时会成为问题。其余为防御性改进和死代码清理。

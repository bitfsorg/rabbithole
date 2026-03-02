# Review R10: revshare

## Overview

| Metric | Value |
|--------|-------|
| Package | `libbitfs-go/revshare` |
| Source files | 7 (`types.go`, `errors.go`, `distribute.go`, `validate.go`, `registry.go`, `share.go`, `pool.go`) |
| Test files | 1 (`revshare_test.go`) |
| Source LOC | 293 |
| Test LOC | 903 |
| Test count | 65 |
| Coverage | 100.0% |
| Race detector | PASS |
| Spec | `docs/specs/bitfs/12-revshare.md` |

## Findings

### CRITICAL

**C-1: `DistributeRevenue` 乘法整数溢出 — 静默错误分配资金**

`distribute.go` L25:
```go
amount := totalPayment * entry.Share / totalShares
```

`totalPayment * entry.Share` 在 uint64 中无溢出保护。当乘积超过 2^64 时静默回绕，产生错误的（可能极小的）金额。

安全阈值：`totalPayment < MaxUint64 / maxShare`。maxShare=10000 时，安全最大值 ~18,446,744 BSV。BSV 总供应量 21,000,000 BSV — **溢出在极端但合法值下可触达**。

**建议**: 使用 `math/bits.Mul64` 做 128-bit 中间乘法，或重构为 `quotient*share + remainder*share/total`。

**C-2: 无符号下溢 — 余额计算回绕到 ~2^64**

`distribute.go` L23:
```go
distributions[i].Amount = totalPayment - distributed
```

当 `distributed > totalPayment`（由 C-3 触发）时，无符号减法回绕到巨大值。最后一个分配方收到天文数字般的金额。

**证明**: `totalPayment=1000, totalShares=100, entries=[{Share:80},{Share:80},{Share:80}]`
- Entry 0: 800, Entry 1: 800, distributed=1600 > 1000
- Entry 2 (last): `1000 - 1600 = 18446744073709551016` (uint64 wrap)

**建议**: 添加 `if distributed > totalPayment { return error }`。

**C-3: 无验证 sum(shares) == totalShares**

`distribute.go` L5: `entries` 和 `totalShares` 作为独立参数，从未验证份额总和等于 `totalShares`。单个 entry 的 Share > totalShares 时可获得 >100% 的支付。多个 entry 份额总和 > totalShares 时触发 C-2 的下溢。

**建议**: 前置验证 share sum == totalShares。

### HIGH

**H-1: `ValidateShareConservation` 求和溢出**

`validate.go` L8-13:
```go
for _, in := range inputs {
    inputTotal += in.Amount
}
```

两个求和循环均可溢出 uint64。攻击者可构造 inputs `[MaxUint64, 1]` 求和回绕到 0，然后 outputs 为空，保守性检查通过但实际销毁了份额。

**建议**: 求和时检查溢出。

**H-2: `SerializeRegistry` 静默截断 entry count 到 uint32**

`registry.go` L26: `uint32(len(state.Entries))` — 超过 MaxUint32 时静默截断。实际影响低但违反正确性。

**建议**: 添加边界检查。

### MEDIUM

**M-1: `DeserializeRegistry` 接受尾随字节无错误**: 宽容解析，可能掩盖数据损坏。

**M-2: 反序列化严格性不一致**: `DeserializeShare` 要求精确长度，`DeserializeRegistry` 允许多余字节，`DeserializeISOPool` 要求精确长度。

**M-3: 3 个 error sentinel 从未使用**: `ErrZeroShares`, `ErrEntryNotFound`, `ErrRegistryLocked` — 为未来功能预留的死代码。

### LOW

**L-1: `DistributeRevenue` 不验证 entry.Share > 0**: Share=0 的条目收到 0，数学上无害但语义无意义。

**L-2: 无最大 entry 数量限制**: `DistributeRevenue` 接受任意大 slice。

**L-3: `FindEntry` 为 O(n) 线性扫描**: 少量股东可接受，无复杂度文档。

### SUGGESTIONS

**S-1.** 考虑使用 `math/big` 做分配算法 — 消除所有溢出问题。

**S-2.** 添加 fuzz 测试覆盖溢出场景。

**S-3.** 文档化 "余额给最后一个" 的公平性属性 — 小金额系统性偏向最后股东。考虑轮换。

## Spec Consistency

| Spec 项目 | 状态 |
|---|---|
| RevShareEntry 类型 (Address [20]byte, Share uint64) | MATCH |
| RegistryState 类型 (4 fields) | MATCH |
| IsISOActive() / IsLocked() | MATCH |
| FindEntry() | MATCH |
| ShareData 类型 | MATCH |
| ISOPoolState 类型 | MATCH |
| Distribution 类型 | MATCH |
| SerializeRegistry / DeserializeRegistry | MATCH |
| SerializeShare / DeserializeShare | MATCH |
| SerializeISOPool / DeserializeISOPool | MATCH |
| DistributeRevenue (remainder-to-last) | MATCH |
| ValidateShareConservation / ValidateDistribution | MATCH |
| Registry 二进制格式 (44 + 28*N + 1 bytes) | MATCH |
| Share 二进制格式 (40 bytes) | MATCH |
| ISO Pool 二进制格式 (68 bytes) | MATCH |
| ModeFlags bits | MATCH |
| 10 个 Error sentinels | MATCH (3 未使用) |
| Big-endian 编码 | MATCH |

**Spec 一致性**: 100% — 所有类型、函数、格式完全匹配。Spec 同样缺少溢出保护要求。

## Code Quality Assessment

**优点**:
1. 100% 语句覆盖率 + 65 个测试
2. 干净的文件组织（一个关注点/文件）
3. 一致的编码风格
4. 彻底的边界测试（零值、最大值、往返验证）
5. 无外部依赖
6. 简洁的二进制格式（网络字节序）

**不足**:
1. 核心金融算法有 3 个 CRITICAL 整数安全问题
2. 验证函数有同类溢出漏洞
3. 反序列化严格性不一致
4. 3 个未使用的 error sentinel

## Summary

revshare 包结构良好、spec 一致、100% 测试覆盖。但核心金融算法有 **3 个 CRITICAL 整数安全问题**：

1. **C-1**: 乘法溢出产生静默错误分配
2. **C-2**: 无符号下溢使最后股东获得 ~2^64 金额
3. **C-3**: 无 share sum 验证是 C-2 的根因

修复优先级：C-3（添加 share sum 验证）→ C-2（添加下溢保护）→ C-1（溢出安全乘法）→ H-1（验证函数溢出检查）。修复 C-3 可阻止 C-2 通过正常代码路径触达，但深度防御要求全部修复。

**这是本次审查中首次发现 CRITICAL 级别问题。**

# 代码审查总结报告

**日期**: 2026-03-01
**范围**: libbitfs-go 全部 11 包 + bitfs 应用层 4 模块
**方法**: 逐模块纵切，三维度审查（安全性、Spec 一致性、代码质量）
**设计文档**: `docs/plans/2026-03-01-continuous-review-design.md`
**状态**: All 172 findings (4C + 24H + 74M + 70L) across 15 modules confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md`. Archived 2026-03-03.

## 审查指标汇总

| Round | Package | Source LOC | Test LOC | Tests | Coverage | C | H | M | L | S |
|-------|---------|-----------|---------|-------|---------|---|---|---|---|---|
| R01 | method42 | 458 | 1,612 | 84 | 91.2% | 0 | 2 | 6 | 4 | 5 |
| R02 | x402 | 1,148 | 3,212 | 156 | 87.3% | 0 | 2 | 5 | 4 | 3 |
| R03 | spv | 644 | 1,837 | 67 | 85.3% | 0 | 0 | 5 | 5 | 3 |
| R04 | wallet | 485 | 1,410 | 64 | 81.5% | 0 | 1 | 6 | 5 | 3 |
| R05 | tx | 1,039 | 2,862 | 112 | 84.1% | 0 | 3 | 6 | 6 | 3 |
| R06 | metanet | 1,513 | 3,774 | 174 | 94.7% | 0 | 0 | 5 | 6 | 3 |
| R07 | storage | 531 | 1,347 | 56 | 88.2% | 0 | 2 | 5 | 4 | 4 |
| R08 | network | 984 | 1,694 | 59 | 90.7% | 0 | 0 | 4 | 5 | 3 |
| R09 | paymail | 664 | 1,626 | 50 | 80.4% | 0 | 2 | 4 | 4 | 4 |
| R10 | revshare | 293 | 903 | 65 | 100% | **3** | 2 | 3 | 3 | 3 |
| R11 | vault | 3,305 | 3,170 | 144 | 80.5% | 0 | 3 | 6 | 5 | 4 |
| R12 | daemon | 2,504 | 4,674 | 205 | 85.4% | 0 | 2 | 6 | 6 | 3 |
| R13 | buy | 561 | 841 | 37 | 92.0% | **1** | 2 | 4 | 5 | 2 |
| R14 | client | 589 | 1,102 | 66 | 97.3% | 0 | 0 | 4 | 2 | 3 |
| R15 | cmd | 5,911 | 13,011 | 711 | 52-81% | 0 | 3 | 5 | 6 | 3 |
| **合计** | **15 模块** | **~20,629** | **~43,075** | **2,050** | **~87%** | **4** | **24** | **74** | **70** | **51** |

## CRITICAL 级别发现 (4)

| ID | 包 | 描述 | 修复复杂度 |
|----|---|------|-----------|
| R10-C1 | revshare | `DistributeRevenue` 乘法溢出 — `totalPayment * entry.Share` 在 uint64 中无溢出保护，可静默产生错误分配 | 中 — 需 128-bit 中间乘法 |
| R10-C2 | revshare | 无符号下溢 — `totalPayment - distributed` 当 `distributed > totalPayment` 时回绕到 ~2^64 | 低 — 添加下溢检查 |
| R10-C3 | revshare | 无 share sum 验证 — entries 的 Share 总和不验证等于 totalShares，是 C2 的根因 | 低 — 添加前置验证 |
| R13-C1 | buy | InvoiceID 传递断裂 — P0 修复仅修改服务端（daemon 构建带 InvoiceID 的 HTLC），遗漏客户端（BuyInfo 无 InvoiceID 字段），导致所有 HTLC 付费购买必然失败 | 低 — 两行修改 |

### 修复优先级

1. **R13-C1** — 最高优先级。所有付费内容购买当前不可用。两行修复：添加 `BuyInfo.InvoiceID` 字段 + 传递到 `HTLCFundingParams`。
2. **R10-C3 → C2 → C1** — 按依赖顺序修复。C3 阻止 C2 通过正常路径触达，但深度防御要求全部修复。

## HIGH 级别发现 (24)

### 安全类 (10)

| ID | 包 | 描述 |
|----|---|------|
| R01-H1 | method42 | AES-GCM 无 AAD — 跨上下文密文替换风险 |
| R01-H2 | method42 | 密钥材料使用后不清零 |
| R02-H1 | x402 | `VerifyPayment` 不验证交易签名 — 支付伪造风险 |
| R02-H2 | x402 | `VerifyPayment` 不绑定 Invoice ID — P2PKH 路径跨 invoice 重用 |
| R04-H1 | wallet | `KeyPair.PrivateKey` 缺 `json:"-"` — 意外序列化泄露私钥 |
| R09-H1 | paymail | SSRF — 服务端控制的 URL 模板无域名验证 |
| R09-H2 | paymail | HTTPS 验证漏洞 — 空 scheme 通过检查 |
| R12-H1 | daemon | HTLC 构建失败时静默回退 P2PKH — 安全降级 |
| R12-H2 | daemon | Dashboard/sales 端点无认证 — 暴露敏感状态 |
| R15-H2 | cmd | 私钥通过 CLI flag 暴露 — `ps aux` 可见 |

### 正确性类 (9)

| ID | 包 | 描述 |
|----|---|------|
| R05-H1 | tx | `EstimateFee` 大输入时整数溢出 |
| R05-H2 | tx | `BuildDataTransaction` 为 stub — 返回 nil RawTx |
| R05-H3 | tx | Fee 输入可与 node 输入重叠 — 潜在双花 |
| R07-H1 | storage | 解压无大小限制 — zip bomb 漏洞 |
| R07-H2 | storage | `SplitIntoChunks` chunkSize≤0 时死循环 |
| R10-H1 | revshare | `ValidateShareConservation` 求和溢出 |
| R10-H2 | revshare | `SerializeRegistry` 静默截断 entry count 到 uint32 |
| R11-H1 | vault | `mustDecompressPubKey` 返回 nil + 调用点错误抑制 |
| R11-H2 | vault | `Remove` 非原子 — 产生两个独立交易 |

### 平台/可用性类 (5)

| ID | 包 | 描述 |
|----|---|------|
| R11-H3 | vault | `flock.go` 仅 Unix — Windows 编译失败 |
| R13-H1 | buy | 付款后未验证 capsule hash |
| R13-H2 | buy | 手续费估算假设 P2PKH 输出 — HTLC 低估 |
| R15-H1 | cmd | `bmget` 路径遍历 — 恶意 daemon 可写任意文件 |
| R15-H3 | cmd | 钱包密码 string slice 副本不清零 |

## 跨模块模式

### 模式 1: 整数安全

revshare (3C)、tx (H1)、buy (M1) — uint64 溢出/下溢无保护。**影响**: 金融计算错误。**修复**: 统一使用溢出安全的算术辅助函数或 `math/big`。

### 模式 2: 错误抑制

vault (`_, _ :=` 5 处)、daemon (`BuildHTLC` 错误丢弃)、buy (`CapsuleNonce` 静默忽略)、cmd (多处) — `_, _ :=` 模式在整个代码库中反复出现。**影响**: 静默故障、安全降级。**修复**: 对已知关键路径逐个审查并处理错误。

### 模式 3: HTTP 超时缺失

paymail (M-1)、network (已修复)、client (M-3)、daemon 对外连接 — `http.DefaultClient` 无超时。多次审计标识但部分仍未修复。**影响**: 挂起连接。**修复**: 统一创建带 30s 超时的 HTTP 客户端。

### 模式 4: 输入验证不一致

client (txid 3/6 方法验证)、daemon (key_hash 门控不一致)、cmd (部分工具有路径遍历防护、部分无) — 同一代码库中验证标准不统一。**影响**: 安全防线有漏洞。**修复**: 建立统一的输入验证层。

### 模式 5: 认证缺失

daemon 所有 20 个端点无认证、shell sales 绕过配置 — 内部/调试端点公开暴露。**影响**: 信息泄露。**修复**: 至少为管理端点添加 bearer token 或 localhost 绑定。

## 包质量排名

### 最高质量

1. **metanet** (R06) — 94.7% 覆盖率，174 测试 + 4 fuzz，0C/0H，最高分
2. **client** (R14) — 97.3% 覆盖率，0C/0H，架构清晰
3. **network** (R08) — 90.7% 覆盖率，0C/0H，BIP37 实现正确

### 需要关注

4. **revshare** (R10) — 100% 覆盖率但 3 个 CRITICAL — 覆盖率不等于正确性
5. **buy** (R13) — 92% 覆盖率但 1 个 CRITICAL — P0 修复不完整
6. **vault** (R11) — 3 个 HIGH，非原子 Remove，flock 平台限制

## Spec 一致性

| 包 | 一致率 | 偏差说明 |
|---|--------|---------|
| method42 | 100% | — |
| x402 | 98% | VerifyPayment 不验签名 |
| spv | 100% | — |
| wallet | 100% | — |
| tx | 95% | BuildDataTransaction 为 stub |
| metanet | 96% | 4 个便利函数不在 spec、ISO 状态机无强制 |
| storage | 100% | — |
| network | 97% | ErrAuthFailed 未使用 |
| paymail | 98% | 空 scheme 漏洞、PubKey 仅格式验证 |
| revshare | 100% | Spec 同样缺少溢出保护要求 |
| vault | 96% | Remove 非原子偏离 spec "1 transaction" |

## 推荐修复路径

### P0 — 必须立即修复 (阻塞发布)

1. **R13-C1**: `BuyInfo` 添加 InvoiceID + `buy.go` 传递 — 2 行修改
2. **R10-C3→C2→C1**: revshare 整数安全 — share sum 验证 + 下溢检查 + 溢出安全乘法
3. **R15-H1**: `bmget` 路径遍历 — 添加 childEntry.Name 验证

### P1 — 应该尽快修复

4. **R01-H1**: AES-GCM 添加 AAD
5. **R04-H1**: `KeyPair.PrivateKey` 添加 `json:"-"`
6. **R12-H2**: Dashboard/sales 添加认证
7. **R11-H2**: Remove 重构为单 MutationBatch
8. **R13-H1**: 付款后验证 capsule hash

### P2 — 计划修复

9. **R02-H1/H2**: VerifyPayment 签名验证 + InvoiceID 绑定
10. **R07-H1**: 解压大小限制
11. **R15-H2**: 私钥改用文件/环境变量
12. **R09-H2**: paymail HTTPS 空 scheme 修复（一行）
13. **M-***: 所有 HTTP 超时、输入验证、错误处理统一

## 总结

代码库整体质量高：~20,629 行源码配有 ~43,075 行测试（2.1:1 测试密度），平均 87% 覆盖率，2,050 个测试函数，race detector 全部通过。架构决策（MutationBatch 原子操作、build-then-apply 状态突变、ECDH+HKDF 密钥派生）均正确实现。

4 个 CRITICAL 集中在两个包：revshare 的金融算法整数安全（3 个）和 buy 的 InvoiceID 传递断裂（1 个）。后者是最紧急的 — P0 安全修复的不完整实施导致付费内容购买完全不可用，但仅需两行代码修复。

24 个 HIGH 中 10 个为安全相关，需在发布前解决。其余为正确性和平台兼容性问题。74 个 MEDIUM 和 70 个 LOW 为防御性改进，可按模块逐步处理。

**关键洞察**: revshare 包以 100% 测试覆盖率获得最高覆盖分数，但同时持有最多 CRITICAL 发现。这证明覆盖率衡量执行路径，不衡量正确性 — 测试覆盖了所有分支但未测试溢出边界。应添加 fuzz 测试补充。

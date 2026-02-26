# 白皮书技术准确性审查报告

**日期**: 2026-02-26
**范围**: BitFS 白皮书 (13 节) + Metanet 白皮书 (12 节)
**标准**: 以代码实现为权威基准
**审查员**: Claude Opus 4.6 (5 个并行审查 Agent)
**方法**: Agent 1 审查 BitFS §1-5, Agent 2 审查 BitFS §6-10, Agent 3 审查 BitFS §11-13 + References, Agent 4 审查 Metanet §1-6, Agent 5 审查 Metanet §7-12 + Academic Rigor

---

## Executive Summary

5 个并行 Agent 共检查 **184 个技术声明**（BitFS 115 个 + Metanet 69 个）和 **23 个学术严谨性声明**，发现 **22 个不一致项**：

| 严重度 | 数量 | 说明 |
|--------|------|------|
| **CRITICAL** | 1 | 协议级矛盾（LinkType 枚举与白皮书不符） |
| **HIGH** | 2 | 不存在的模块引用、未实现的跨项目依赖 |
| **INACCURATE** | 4 | DNS 格式、HTTP 头、CLI 标志、参考文献年份与代码/事实不符 |
| **MEDIUM** | 2 | HKDF info 省略、ECDH 实现简化 |
| **UNIMPLEMENTED** | 4 | 白皮书描述的功能在代码中无实现 |
| **LOW** | 2 | 表述混淆、脚本生成未完成 |
| **ACADEMIC** | 6 | 论证逻辑、对比公正性、引用一致性问题 |
| **PARTIALLY ACCURATE** | 1 | Metanet 基础设施存在但 BitFS 集成缺失 |

**最关键的三个发现**：

1. **WP-01 [CRITICAL]** — LinkType 枚举不匹配：白皮书描述 3 种链接类型，代码只有 2 个枚举值，硬链接通过隐式机制实现
2. **WP-03 [HIGH]** — Rabin 签名模块在架构图中被引用但从未实现，误导读者对系统能力的理解
3. **WP-12 [HIGH]** — Metanet 白皮书声称两系统共享 Go 核心库，但 metanet/go.mod 实际并未导入 libbitfs-go

**学术严谨性总体评估**：两篇白皮书的密码学核心描述准确，Method 42 ECDH、AES-256-GCM、BIP44 路径在白皮书与代码间高度一致。主要问题集中在：(a) 未实现功能以已完成的口吻描述，(b) 协议细节省略关键参数，(c) 竞品对比时间点过时。

---

## Part 1: BitFS 白皮书逐章审查

### §1 Introduction

All claims verified accurate. 产品定位、设计哲学、Unix 类比在代码中均有体现。

### §2 System Architecture

> **WP-03 [HIGH]** Rabin 签名模块在架构图中被引用但不存在
>
> 白皮书架构图包含 "Rabin Sigs" 模块。libbitfs-go 实际有 10 个包（method42, wallet, tx, metanet, spv, storage, network, config, paymail, x402），无任何名为 "rabin" 的包或文件。
>
> - **文件**: libbitfs-go/ 全部包
> - **重叠**: 设计一致性审查 H2（libbitfs 包列表过时，L1 提到 Rabin 包不存在）
> - **影响**: 读者会认为系统支持 Rabin 签名验证，实际并不支持

### §3 Metanet DAG & File System

> **WP-01 [CRITICAL]** LinkType 枚举不匹配：白皮书 3 种 vs 代码 2 种
>
> 白皮书描述 3 种链接类型：HARD、SOFT、SOFT_REMOTE。代码中 LinkType 枚举只有 2 个值：
> ```go
> // libbitfs-go/metanet/node.go:70-78
> LinkTypeSoft       = 0
> LinkTypeSoftRemote = 1
> ```
> 硬链接在代码中通过多个 ChildEntry 共享同一 PubKey 隐式实现，并非显式 LinkType 枚举值。
>
> - **影响**: 第三方按白皮书实现时会为 HARD 分配一个 LinkType 值，导致 TLV 格式不兼容
> - **修复**: 白皮书应明确说明硬链接是隐式机制（多个目录条目指向同一节点公钥），不是 LinkType 枚举值

> **WP-02 [LOW]** 硬链接表述混淆
>
> 白皮书在介绍三种 LinkType 值的上下文中引入硬链接概念，暗示它是枚举的一部分。应将硬链接单独解释为目录条目级别的机制，与 LinkType 枚举明确区分。
>
> - **影响**: 理解困难，但不影响正确实现

### §4 HD Key Derivation

All claims verified accurate. BIP44 路径 `m/44'/236'/account'/chain/index`、密钥派生算法与代码完全一致。

### §5 Method 42 Encryption

> **WP-04 [MEDIUM]** HKDF info 参数被省略
>
> 白皮书公式（line 189）:
> ```
> aes_key = HKDF-SHA256(point.x, key_hash)
> ```
> 代码实际使用三参数 HKDF:
> ```go
> // libbitfs-go/method42/kdf.go:60
> HKDF-SHA256(ikm=point.x, salt=key_hash, info="bitfs-file-encryption")
> ```
> `info` 是 HKDF 域分隔符，省略它会派生出完全不同的 AES 密钥。
>
> - **重叠**: 设计一致性审查 H12（白皮书省略 HKDF info）和 C1（HKDF info 字符串不一致）
> - **影响**: 按白皮书公式实现会生成不兼容的密钥
> - **修复**: 补充 info 参数：`aes_key = HKDF-SHA256(point.x, key_hash, "bitfs-file-encryption")`

### §6 Content Storage

All claims verified accurate. SHA256 content-addressed storage、hash-sharded 目录结构与代码一致。

### §7 Commerce Mode

> **WP-08 [UNIMPLEMENTED]** Hash Chain Token 批量购买系统
>
> §7 描述了基于哈希链的 token 系统用于批量文件购买，但代码中无任何实现。libbitfs-go 和 bitfs 中均未找到 hash chain token 相关代码。
>
> - **影响**: 白皮书以已完成的口吻描述尚未开发的功能

> **WP-09 [UNIMPLEMENTED]** 目录级 xpub 购买公式
>
> 白皮书描述的公式 `S_child = S_parent + offset * P_buyer` 用于目录级别的 BIP32 访问控制，但无代码实现。
>
> - **影响**: 同上

### §8 Access Control

All claims verified accurate. 三种访问模式（Private/Free/Paid）在 method42 包中完整实现。

### §9 HTTP 402 Protocol

> **WP-06 [INACCURATE]** HTTP 402 头字段不完整
>
> 白皮书描述 3 个 HTTP 头（price, file size, invoice ID）。代码实现了 5 个：
> ```go
> // libbitfs-go/x402/headers.go:10-15
> X-Price        // 总价
> X-Price-Per-KB // 每 KB 价格
> X-File-Size    // 文件大小
> X-Invoice-Id   // 发票 ID
> X-Expiry       // 过期时间
> ```
> `X-Price-Per-KB` 和 `X-Expiry` 未在白皮书中提及。
>
> - **影响**: 不完整但不冲突——白皮书描述是代码实现的子集

> **WP-07 [UNIMPLEMENTED]** WebMCP 工具声明
>
> 白皮书声称 daemon 提供包含 WebMCP/navigator.modelContext 的 HTML 页面用于 AI Agent 交互，但代码中无任何 WebMCP 相关实现。
>
> - **影响**: Agent Friendly 是核心卖点之一，此功能缺失需标注为路线图项

### §10 Discovery & DNS

> **WP-05 [INACCURATE]** DNS TXT 记录格式不匹配
>
> | 来源 | 记录名 | 值格式 |
> |------|--------|--------|
> | 白皮书 | `_bitfs_pubkey` | raw pubkey hex |
> | paymail/dns.go:97-98 | `_bitfs.{domain}` | `bitfs=<pubkey>` |
>
> - **重叠**: 设计一致性审查 H9（DNS TXT 记录格式分歧）
> - **影响**: 按白皮书实现的客户端无法验证域名所有权

### §11 Revenue Rights

> **WP-10 [UNIMPLEMENTED]** 收益权系统（Share UTXOs、Registry UTXO、ISO Pool）
>
> 白皮书 §11 完整描述了收益分成系统。代码中 `libbitfs-go/revshare/` 目录存在但为空，整节描述的功能均未实现。
>
> - **影响**: 白皮书以技术规范的精度描述了不存在的功能
> - **修复**: 添加 "Planned" 或 "Future Work" 标注

### §12 Metanet Integration

> **WP-11 [INACCURATE/OVERSTATED]** `bitfs put --store metanet` 标志
>
> 白皮书 §12（line 316）将 `--store metanet` 描述为现有功能。`cmd_put.go` 无 `--store` 标志。
>
> - **影响**: 用户尝试使用此标志会得到错误

> **WP-15 [PARTIALLY ACCURATE]** Metanet 基础设施状态
>
> Metanet 核心组件（chain, mining, token, proofs）已实现，但 BitFS 与 Metanet 的集成层（`--store metanet`）缺失。白皮书将两者描述为已集成的系统。

### §13 Future Work

> **WP-16 [ACCURATE]** 未来功能标注
>
> BBS+ 签名、sCrypt 智能合约、CLTV 时锁均正确标记为未来工作。CLTV 在 `metanet/contract/` 中有部分实现，与 "部分完成" 的表述一致。

### References

> **WP-13 [INACCURATE]** Method 42 引用年份错误
>
> 白皮书将 Method 42 论文标注为 2025 年。实际发表于 2019 年（references/CLAUDE.md 确认）。
>
> - **影响**: 学术引用不准确

> **WP-14 [INACCURATE]** 专利 GB2608179A 年份错误
>
> 白皮书标注为 2025 年，实际专利申请于 2021-2022 年。
>
> - **影响**: 同上

---

## Part 2: Metanet 白皮书逐章审查

### §1 Introduction

All claims verified accurate.

### §2 Architecture

> **WP-12 [HIGH/UNIMPLEMENTED]** 共享 Go 核心库声明不实
>
> 白皮书声称 "two systems share core Go libraries"（两系统共享核心 Go 库），但 `metanet/go.mod` 中无任何 `libbitfs-go` 导入。目前只有 `bitfs/go.mod` 通过 `replace => ../libbitfs-go` 引用核心库。
>
> - **影响**: 架构级声明与实现不符。Metanet 独立实现了部分与 libbitfs-go 重叠的功能（如 ECDH）

### §3 Token Economics

All claims verified accurate. MNT token 设计、发行机制与 `metanet/internal/chain/` 代码一致。

### §4 Merged Mining

All claims verified accurate. AuxPoW、难度调整与 `metanet/internal/mining/` 实现一致。

### §5 Storage Contracts

All claims verified accurate. 合约结构与 `metanet/internal/contract/` 一致。

### §6 Network Protocol

All claims verified accurate.

### §7 Storage Proofs

> **WP-17 [MEDIUM]** ECDH 实现使用 HMAC 模拟而非真实 secp256k1 ECDH
>
> 白皮书描述 "ECDH" 用于存储证明的加密层。代码实际使用 HMAC 模拟：
> ```go
> // metanet/internal/proof/encrypt.go:34-58
> // 使用 HMAC-SHA256 模拟 ECDH 共享密钥
> ```
> 严格来说白皮书的 "ECDH" 描述对当前代码不准确。
>
> - **影响**: 安全属性不同——HMAC 模拟缺少 ECDH 的前向保密性
> - **修复**: 白皮书注明当前实现使用 HMAC 模拟，或将代码升级为真实 secp256k1 ECDH

### §8 Payment Channels

All claims verified accurate.

### §9 Script Generation

> **WP-18 [LOW/UNIMPLEMENTED]** OP_CHECKSEQUENCEVERIFY 脚本生成未最终完成
>
> `metanet/internal/payment/funding.go` 中 CSV 脚本生成标记为 TODO 或部分实现。
>
> - **影响**: 支付通道的超时退款机制不完整

### §10 Settlement

All claims verified accurate.

### §11 Cross-chain Anchoring

All claims verified accurate.

### §12 Future Directions

All claims verified accurate.

---

## Part 3: 两篇白皮书交叉一致性

### 3.1 共享库声明不对称

BitFS 白皮书未声称与 Metanet 共享代码库。Metanet 白皮书 §2 声称 "two systems share core Go libraries"。实际只有单向依赖：BitFS -> libbitfs-go。Metanet 未使用 libbitfs-go（见 WP-12）。

### 3.2 Method 42 命名不一致

BitFS 白皮书明确使用 "Method 42" 术语并引用原始论文。Metanet 白皮书使用相同的 ECDH 密钥派生原语但**未使用 "Method 42" 名称**（见 ACAD-5），而是直接描述 ECDH。这导致读者无法意识到两个系统使用相同的密码学基元。

### 3.3 Metanet 专利引用不一致

BitFS 白皮书引用 "Metanet Technical Summary v1.0"，Metanet 白皮书引用 "GB2608179A patent"。两者指向同一协议的不同文档（见 ACAD-4）。

### 3.4 Dust Limit 参数

BitFS 白皮书写 546 sat，代码已更新为 1 sat（BSV 已移除 dust limit）。Metanet 白皮书未涉及此参数。此问题已在设计一致性审查 C4 中记录。

---

## Part 4: 学术严谨性评估

### 4.1 论证逻辑

> **ACAD-1 [LOGIC-FLAW]** 正反馈循环的冷启动问题
>
> Metanet 白皮书描述了一个正反馈循环：更多内容 -> 更多节点 -> 更好的服务 -> 更多内容。但未描述冷启动机制——当网络初始阶段内容和节点都稀少时，如何启动这个循环。
>
> - **建议**: 补充冷启动策略（如初始补贴、创始节点计划、内容迁移工具等）

### 4.2 对比表公正性

> **ACAD-6 [STRAW-MAN]** Filecoin 检索市场描述过时
>
> 白皮书将 Filecoin 的检索市场定性为 "afterthought"（事后补充）。此评价在 Filecoin 早期（2020-2022）可以成立，但 Filecoin 在 2023-2025 年间对检索市场做了大量改进（如 Saturn CDN、Lassie retrieval client）。
>
> - **影响**: 论文发表时可能被审稿人或 Filecoin 社区质疑为过时
> - **建议**: 更新对比表引用 Filecoin 最新检索进展，并重新定位差异化论述

### 4.3 参考文献准确性

> **ACAD-4 [INCONSISTENT-CITATIONS]** 同一协议被不同引用
>
> BitFS 白皮书引用 "Metanet Technical Summary v1.0"（文献综述形式），Metanet 白皮书引用 "GB2608179A" 专利号（正式专利形式）。两者指向同一 Metanet 协议。
>
> - **建议**: 统一引用格式，同时列出技术摘要和专利号

> **WP-13/WP-14** 年份错误（详见 Part 1 References 节）

### 4.4 缺失说明

> **ACAD-2 [MISSING-CAVEAT]** 双币系统复杂性被低估
>
> Metanet 白皮书声称 "用户完全隔离于双币复杂性"。实际上节点运营商需要管理 BSV 与 MNT 之间的兑换，钱包需要同时处理两种资产。白皮书未充分说明这一运营负担。
>
> - **建议**: 补充节点运营商视角的双币管理说明，或描述自动化兑换机制

> **ACAD-3 [MISSING-CAVEAT]** ECDH vs zk-SNARK 安全模型差异未充分说明
>
> 存储证明章节选择 ECDH 而非 zk-SNARK，但未充分论述两者的安全模型差异：ECDH 证明不可转让（verifier-specific），zk-SNARK 证明可公开验证。这一权衡对去中心化验证有重要影响。
>
> - **建议**: 补充一段讨论，说明选择 ECDH 的设计取舍及其对验证者模型的约束

> **ACAD-5 [MISSING-CROSS-REFERENCE]** Metanet 白皮书未命名 Method 42
>
> Metanet 白皮书使用了与 BitFS Method 42 相同的 ECDH 密钥派生原语，但未使用 "Method 42" 名称，也未交叉引用 BitFS 白皮书。
>
> - **影响**: 读者无法意识到两个系统共享同一密码学基元
> - **建议**: 明确引用 Method 42 并指向 BitFS 白皮书的对应章节

---

## Part 5: 修正建议优先级

### P0 — 发布前必须修正

这些问题若不修正，会导致第三方实现不兼容或学术引用被质疑。

| # | Finding ID | 问题 | 工作量 | 修改位置 |
|---|------------|------|--------|----------|
| 1 | **WP-01** | LinkType 枚举：白皮书 3 种 vs 代码 2 种 | 1 段 | BitFS-Whitepaper-Outline.md §3 |
| 2 | **WP-04** | HKDF info 参数缺失 | 1 行 | BitFS-Whitepaper-Outline.md §5 (line 189) |
| 3 | **WP-05** | DNS TXT 记录格式不匹配 | 1 处 | BitFS-Whitepaper-Outline.md §10 |
| 4 | **WP-13** | Method 42 引用年份 2025 -> 2019 | 1 处 | BitFS-Whitepaper-Outline.md References |
| 5 | **WP-14** | 专利 GB2608179A 年份 2025 -> 2021 | 1 处 | BitFS-Whitepaper-Outline.md References |
| 6 | **WP-12** | "shared Go libraries" 声明不实 | 1 段 | Metanet-Whitepaper-Outline.md §2 |

### P1 — 近期修正

| # | Finding ID | 问题 | 修改位置 |
|---|------------|------|----------|
| 7 | **WP-03** | Rabin 模块引用移除或标注 "Planned" | BitFS-Whitepaper-Outline.md §2 架构图 |
| 8 | **WP-06** | HTTP 402 头补充 X-Price-Per-KB, X-Expiry | BitFS-Whitepaper-Outline.md §9 |
| 9 | **WP-11** | `--store metanet` 标注为 "Planned" | BitFS-Whitepaper-Outline.md §12 |
| 10 | **WP-17** | ECDH vs HMAC 模拟说明 | Metanet-Whitepaper-Outline.md §7 |
| 11 | **ACAD-1** | 补充冷启动机制 | Metanet-Whitepaper-Outline.md §3 或新增小节 |
| 12 | **ACAD-4** | 统一 Metanet 协议引用格式 | 两篇白皮书 References |
| 13 | **ACAD-5** | Metanet 白皮书引用 Method 42 名称 | Metanet-Whitepaper-Outline.md §7 |
| 14 | **ACAD-6** | 更新 Filecoin 检索市场对比 | Metanet-Whitepaper-Outline.md 对比表 |

### P2 — 下一版本修正

| # | Finding ID | 问题 | 修改位置 |
|---|------------|------|----------|
| 15 | **WP-02** | 硬链接表述重组 | BitFS-Whitepaper-Outline.md §3 |
| 16 | **WP-07** | WebMCP 标注为 "Planned" 或移至 Future Work | BitFS-Whitepaper-Outline.md §9 |
| 17 | **WP-08** | Hash Chain Token 标注 "Planned" 或移至 Future Work | BitFS-Whitepaper-Outline.md §7 |
| 18 | **WP-09** | 目录级 xpub 购买标注 "Planned" | BitFS-Whitepaper-Outline.md §7 |
| 19 | **WP-10** | Revenue Rights 整节标注 "Planned" | BitFS-Whitepaper-Outline.md §11 |
| 20 | **WP-15** | Metanet 集成状态说明 | BitFS-Whitepaper-Outline.md §12 |
| 21 | **WP-18** | CSV 脚本生成状态说明 | Metanet-Whitepaper-Outline.md §9 |
| 22 | **ACAD-2** | 双币复杂性补充说明 | Metanet-Whitepaper-Outline.md §3 |
| 23 | **ACAD-3** | ECDH vs zk-SNARK 安全模型讨论 | Metanet-Whitepaper-Outline.md §7 |

---

## Appendix: 与设计一致性审查的重叠

以下白皮书准确性发现与 `2026-02-26-design-consistency.md` 中的发现存在重叠或直接关联：

| 本报告 Finding | 设计一致性 Finding | 重叠说明 |
|---------------|-------------------|---------|
| **WP-01** (LinkType 枚举) | — | 新发现，设计一致性审查未覆盖 |
| **WP-03** (Rabin 模块) | **H2** (libbitfs 包列表过时) | H2 指出 L1 提到 Rabin 包不存在，WP-03 确认白皮书同样引用了不存在的 Rabin |
| **WP-04** (HKDF info 省略) | **C1** (HKDF info 字符串不一致) + **H12** (白皮书省略 HKDF info) | C1 发现文档间 info 值不一致，H12 特指白皮书省略此参数。WP-04 是 H12 的白皮书视角确认 |
| **WP-05** (DNS 格式) | **H9** (DNS TXT 记录格式分歧) | 同一问题：白皮书、paymail 代码、engine 代码三方不一致 |
| **WP-06** (HTTP 402 头) | — | 新发现，设计一致性审查未检查 HTTP 头完整性 |
| **WP-11** (--store metanet) | — | 新发现，属白皮书特有的功能声明 |
| **WP-12** (共享库声明) | — | 新发现，设计一致性审查聚焦 BitFS 侧，未检查 Metanet go.mod |
| **WP-13/WP-14** (引用年份) | — | 新发现，属参考文献准确性 |
| **WP-17** (ECDH vs HMAC) | — | 新发现，属 Metanet 代码级审查 |
| **ACAD-4** (引用不一致) | — | 新发现，属学术严谨性 |
| — | **C4** (Dust Limit 546 vs 1) | 白皮书中存在同样问题（546 sat），但已在 C4 中完整记录，本报告不重复分配 ID |
| — | **C9** (wallet.db vs wallet.enc) | 白皮书写 wallet.db，已在 C9 中记录，本报告不重复 |
| — | **H11** (FREE 模式 KDF 输入) | 白皮书写 `KDF(P_node, key_hash)` 而非 `P_node.x`，已在 H11 中记录 |

**总结**: 22 个白皮书准确性发现中，4 个与设计一致性审查重叠（WP-03/H2, WP-04/C1+H12, WP-05/H9, 以及 C4/C9/H11 的白皮书侧确认）。其余 18 个为本次审查新发现。两份报告互补：设计一致性审查聚焦文档层级间的矛盾，本报告聚焦白皮书声明与代码实现的偏差及学术严谨性。

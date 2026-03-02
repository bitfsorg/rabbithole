# 白皮书技术准确性审查设计

**日期**: 2026-02-26
**范围**: BitFS 白皮书 (13 节) + Metanet 白皮书 (12 节)
**方法**: 逐章提取技术声明，对照代码验证 + 学术严谨性审查

---

## 目标

对两篇白皮书的每一条技术声明进行代码对照验证，同时审查学术严谨性（论证逻辑、对比表公正性、参考文献准确性）。

## 评估维度

| 标记 | 含义 |
|------|------|
| **ACCURATE** | 声明与代码实现完全一致 |
| **INACCURATE** | 声明与代码实现矛盾 |
| **OUTDATED** | 声明曾正确但代码已演进 |
| **UNIMPLEMENTED** | 声明描述的功能尚未实现 |
| **OVERSTATED** | 声明夸大了实际能力 |

学术严谨性额外维度：
- 论证逻辑自洽性
- 对比表公正性（有无 straw-man）
- 参考文献准确性

## 并行 Agent 分工

| Agent | 负责 | 白皮书章节 |
|-------|------|-----------|
| 1 | BitFS 核心协议 | §1-4（引言、Metanet DAG、HD 密钥、Method 42） |
| 2 | BitFS 应用层 | §5-9（存储、交易、HTLC、SPV、DNS） |
| 3 | BitFS 接口 + 激励 | §10-13（Agent 接口、CLI、激励层、结论） |
| 4 | Metanet 架构 + 经济 | §1-6（引言、BitFS 关系、三层架构、Chain、代币、热数据） |
| 5 | Metanet 合约 + 学术 | §7-12（冷数据、收入分成、支付通道、对比表、CLI、结论）+ 两篇白皮书参考文献和对比表公正性 |

## 交付物

`docs/audits/2026-02-26-whitepaper-accuracy.md`，结构：

1. Executive Summary（统计 + 关键发现）
2. BitFS 白皮书逐章审查
3. Metanet 白皮书逐章审查
4. 两篇白皮书交叉一致性
5. 学术严谨性评估（论证、对比表、引用）
6. 修正建议优先级排序

## 已知重叠

设计一致性审查（`docs/audits/2026-02-26-design-consistency.md`）已发现白皮书相关问题：
- C4: DustLimit 546 sat vs 代码 1 sat
- C8: HTLC 发起方 Seller vs Buyer
- C9: wallet.db vs wallet.enc

本次审查会覆盖这些并扩展到所有技术声明。

## 源文件

- `whitepaper/BitFS-Whitepaper-Outline.md` — BitFS 白皮书大纲（唯一源）
- `whitepaper/Metanet-Whitepaper-Outline.md` — Metanet 白皮书大纲（唯一源）
- `whitepaper/BitFS-Whitepaper.en.tex` — BitFS 英文 LaTeX
- `whitepaper/Metanet-Whitepaper.en.tex` — Metanet 英文 LaTeX

代码验证目标：
- `libbitfs-go/` — 10 个核心库包
- `bitfs/` — CLI + daemon + engine
- `metanet/` — CDN 节点实现

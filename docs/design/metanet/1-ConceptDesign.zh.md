# Metanet 概念设计

> 本文档为 Metanet 设计文档体系的第一层：产品定位、核心设计理念、架构概览。
>
> **文档体系**:
> - [整体设计](../0-OverallDesign.zh.md) — 两产品生态、三层架构、界面划分
> - **概念设计** (本文档) — 产品定位、核心理念、设计原则
> - [系统设计](2-SystemDesign.zh.md) — 节点架构、合约、支付通道
> - [详细设计](3-DetailedDesign.zh.md) — 共识、挖矿、结算协议细节
> - [测试设计](4-TestDesign.zh.md) — 测试用例设计

---

## 一、定位

**Metanet** (metanet.org) 是基于 BSV 构建的 Overlay Network (ON) 的去中心化 CDN 网络。核心激励机制是**检索**而非存储——Metanet Node 的主要收入来自 x402 按次付费的内容检索服务，存储合约只是冷数据的保底机制。

---

## 二、与 BitFS 的关系

BitFS 与 Metanet 的关系类似于 IPFS 与 Filecoin，但**反转了 Filecoin 模型**:

| 维度 | IPFS + Filecoin | BitFS + Metanet |
|------|----------------|-----------------|
| 协议层 | IPFS (内容寻址, P2P) | BitFS (Metanet DAG, 加密文件系统) |
| 激励层 | Filecoin (激励**存储**) | Metanet (激励**检索**) |
| 协议 CLI | `ipfs` | `bitfs` |
| 节点 CLI | `lotus` | `metanet` |
| 核心证明 | PoRep + PoSt (zk-SNARK, GPU 密集) | ECDH 双层加密 + Merkle 挑战 (毫秒级) |

**关键反转**: Filecoin 激励存储 (Proof-of-Replication)，检索市场薄弱；Metanet 激励检索 (x402 按次付费)，热门内容自组织复制，冷数据靠 1-to-1 合约保底。

---

## 三、核心设计理念

### 3.1 热数据自组织 (x402 正反馈)

<table style="width:100%; border-collapse:collapse; margin:0.8em 0; font-size:10pt; border:2px solid #333;">
<tr>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#eaf0f7; font-weight:600;">文件热度高</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#f0f7ea; font-weight:600;">x402 收入高</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#f7f0ea; font-weight:600;">更多 Node 缓存</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#f5eaf7; font-weight:600;">可用性更好</td>
</tr>
<tr>
<td colspan="7" style="border:1px solid #999; padding:0.3em; text-align:center; font-size:9pt; color:#555; background:#fafafa;">↻ 正反馈循环: 用户体验提升 → 文件热度更高</td>
</tr>
</table>

不需要协议层管理副本——市场自动调节。越热门的内容越多 Node 缓存，类似传统 CDN 的缓存逻辑，但由利润而非中心化策略驱动。

### 3.2 冷数据一对一合约

Owner 与 Metanet Node 签 1-to-1 存储合约 (Bitcoin Script)，Owner 付 MNT Token。ECDH 双层加密保证每个 Node 的副本独特，Merkle 挑战-响应验证持续持有。协议不管副本策略——Owner 自己决定冗余度。

### 3.3 基于 BSV 的 Overlay Network

将 Metanet 构建为 BSV 上的 Overlay Network (ON)。每笔 ON 交易都是合法的 BSV 交易（遵循 Carrier Pair 模型），并在 BSV 交易内嵌入 ML Block。此设计不需要 fork BSV 代码，不需要独立的链基础设施，完全依赖 BSV 提供最终性和不可篡改性，所有"智能合约"均为标准 Bitcoin Script（Verify-Then-Pay 原子模型）。

### 3.4 双币种分工

| 币种 | 面向 | 用途 |
|------|------|------|
| **BSV** | 终端用户、AI Agent | x402 检索费、HTLC 文件购买 |
| **MNT Token** | Owner、Node 运营商 | CDN 托管费、挖矿奖励、节点间批发 |

**普通用户不需要接触 Metanet Chain/Token**——只用 BSV 即可使用 BitFS。

---

## 四、与现有系统对比

| 维度 | Metanet | Filecoin | IPFS | Arweave | 传统 CDN |
|------|---------|----------|------|---------|----------|
| 核心激励 | 检索 (x402) | 存储 (PoRep) | 无 (志愿) | 存储 (一次付费永存) | 无 (中心化) |
| 存储证明 | ECDH+Merkle (毫秒级) | zk-SNARK (GPU 数小时) | 无 | SPoRA | N/A |
| 副本管理 | Owner 自决 | 协议强制 (≥N 副本) | 无保证 | 协议保证 | 运营商决定 |
| 共识 | 纯 SHA256 PoW (非合并) | 预期共识 (EC) | 无共识 | RandomX | 无共识 |
| 代币 | MNT (仅 B2B) + BSV (B2C) | FIL (全用途) | 无 | AR | 法币 |
| 链架构 | BSV Overlay Network | 自定义 VM | 无链 | 自定义 | 无链 |
| 准入门槛 | 低 (普通服务器) | 高 (GPU+大存储) | 低 | 中 | 高 (资本密集) |
| 冷/热数据 | 热数据自组织 + 冷数据合约 | 统一存储合约 | 无保证 | 永久存储 | CDN 缓存 |

---

## 五、设计原则

1. **Overlay Network** — 每笔 ON 交易都是合法的 BSV 交易，ON 节点是轻量 Go 服务（通过 SPV/API 连接），无需运行独立的 C++ 全节点或 fork 链。
2. **Bitcoin Script 即合约** — 存储合约采用 Verify-then-pay 脚本原子执行替代信任，不引入新操作码。
3. **轻量存储证明** — ECDH 双层加密实现防串通加密副本 + Merkle 挑战替代 zk-SNARK，毫秒级验证，无需 GPU。
4. **纯 SHA256 PoW** — 双倍比特币心跳（5 分钟出块，2 年减半），挖矿与存储解耦，无需依赖 BSV 矿池配合即可发行 MNT Token。
5. **低准入门槛** — 矿工参与要求极低，同时网络节点也可以是轻量服务。
6. **市场驱动** — 热数据靠 x402 正反馈自组织，协议不做中心化调度。
7. **BSV 作为底层信任根** — MNT Token 的账本通过 ML Block 嵌入 BSV 交易中，BSV 主链直接保证最终性和不可篡改性，避免了独立独立链算力不足时的安全风险。
8. **BRC 标准兼容** — 遵循 BSV Association 的 Overlay BRC 标准体系，网络节点通过 `nServices` (NODE_METANET) 标识实现 BSV P2P 网络的节点发现。

# 整体设计：两产品生态与界面划分

> 本文档为设计文档体系的顶层文件，定义 BitFS 与 Metanet 两产品的职责边界、三层架构和共享设计原则。
>
> **设计文档体系**：
> - **整体设计** (本文档) — 两产品生态、三层架构、界面划分
> - [BitFS 设计](bitfs/) — 去中心化加密文件系统协议
> - [Metanet 设计](metanet/) — 去中心化 CDN 网络

---

## 一、两产品生态概览

| 维度 | BitFS | Metanet |
|------|-------|---------|
| 定位 | 去中心化加密文件系统协议 | 去中心化 CDN 网络 |
| 域名 | bitfs.org | metanet.org |
| CLI | `bitfs` (文件所有者/访问者) | `metanet` (CDN 节点运营商) |
| 用户 | 终端用户、AI Agent | 内容所有者、节点运营商 |
| 货币 | BSV | MNT Token |
| 类比 | IPFS (协议层) | Filecoin (激励层) |

**关键区别**：Filecoin 激励存储 (Proof-of-Replication)；Metanet 激励检索（下载计费按次付费）。热门内容自组织复制，冷数据自然淘汰——市场驱动而非强制冗余。

---

## 二、三层架构

<table style="width:100%; border-collapse:collapse; margin:0.8em 0; font-size:10pt; border:2px solid #333;">
<tr style="background:#eaf0f7;">
<td style="border:1px solid #999; padding:0.5em; width:22%; font-weight:600;">Layer 1: BSV 主链</td>
<td style="border:1px solid #999; padding:0.5em; width:38%;">文件元数据、所有权证明、HTLC 交易、下载计费结算</td>
<td style="border:1px solid #999; padding:0.5em; width:22%;">终端用户、AI Agent</td>
<td style="border:1px solid #999; padding:0.5em; width:18%; text-align:center;">BSV</td>
</tr>
<tr style="background:#f0f7ea;">
<td style="border:1px solid #999; padding:0.5em; font-weight:600;">Layer 2: 自托管 Daemon</td>
<td style="border:1px solid #999; padding:0.5em;">本地内容存储、LFCP 直接服务、无第三方依赖</td>
<td style="border:1px solid #999; padding:0.5em;">文件所有者 (自服务)</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center;">无</td>
</tr>
<tr style="background:#f7f0ea;">
<td style="border:1px solid #999; padding:0.5em; font-weight:600;">Layer 3: Metanet Overlay Network</td>
<td style="border:1px solid #999; padding:0.5em;">CDN 托管、节点激励、支付通道结算、ML 共识排序 (5 分钟 PoW)</td>
<td style="border:1px solid #999; padding:0.5em;">内容所有者、节点运营商</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center;">MNT</td>
</tr>
</table>

**数据无需跨链**：BSV 主链只存元数据 (Metanet DAG 交易)；内容数据始终在链下流转 (Daemon 或 CDN 节点)。Metanet Overlay Network 管理经济激励与 ON 层共识排序（CSW Multilevel Blockchain，Bitcoin 风格 PoW，5 分钟出块），不承载文件内容。

### 2.1 数据三种存储/获取方式

| 方式 | 所在层 | 优点 | 代价与风险 |
|------|--------|------|-----------|
| 嵌入链上交易 | Layer 1 | 永久可审计、无需额外服务端 | 成本高，体积受限，且可能被矿工裁剪 |
| BitFS Daemon 自托管 | Layer 2 | 完全自控、协议最简、可直接服务 | 需要持续运维与在线 |
| Metanet SP 托管 | Layer 3 | CDN 可用性高、可扩展、可市场化分发 | 依赖 SP 服务质量与结算策略 |

三种方式可组合：例如“元数据上链 + 内容在 L2/L3 提供 + 支付按对象分层结算”。

### 2.2 链上/链下职责边界（审查基线）

| 对象 | 主放置层 | 边界定义 | 越层判定（需整改） |
|------|----------|----------|-------------------|
| 元数据 | Layer 1 | 文件系统结构、版本、哈希承诺上链；L2/L3 仅缓存 | 将权威元数据仅放链下 |
| 内容 | Layer 2/3（默认） | 文件实体默认链下；仅在必要时选择链上嵌入 | 将大内容默认塞入链上 |
| 权限 | Layer 1 + Layer 2/3 | 所有权/可售性等链上可验证；会话态鉴权在服务层 | 仅靠链下 ACL 且无链上背书 |
| 支付 | Layer 1 + Layer 2/3 | 购买结算走链上 HTLC；下载计费发票在服务层生成并结算可上链 | 将结算状态长期仅保留在内存或仅链下不可追溯 |

---

## 三、界面划分

### 3.1 共享核心库 (libbitfs-go)

两产品共享同一 Go 库，避免重复实现：

```
libbitfs-go/
├── method42/     # Method 42 ECDH 加密 (secp256k1 + AES-256-GCM)
├── wallet/       # HD 钱包 (BIP39/44, Argon2id 种子加密)
├── tx/           # BSV 交易构造 (go-sdk)
├── metanet/      # Metanet DAG 解析 (inode, dirent, 软/硬链接)
├── spv/          # SPV 轻节点 (本地 tx + Merkle proof)
├── storage/      # 内容存储抽象 (链下/链上)
├── paymail/      # Paymail 身份解析 + bitfs:// URI
├── payment/      # 下载计费协议 + Token 预购
├── network/      # 区块链服务抽象 (RPC/SPV 客户端)
├── config/       # 配置文件解析 (key=value)
├── vault/        # Vault 状态管理、交易构建、文件读取操作
├── engine/       # Wallet-state 变更操作，进程级文件锁
└── revshare/     # Revenue Share / ISO 证券化
```

### 3.2 独立二进制

| 二进制 | 面向 | 核心功能 |
|--------|------|----------|
| `bitfs` | 文件所有者、访问者、AI Agent | `put/get/ls/cat/rm/mv/cp`、`sell`、`wallet`、`daemon` |
| `metanet` | CDN 节点运营商 | `init/start/stop`、`status`、`contracts`、`peers`、`mine` |

`bitfs` 用户无需安装 `metanet`；`metanet` 节点内嵌 libbitfs-go 以解密和服务内容。

### 3.3 链间职责

<table style="width:100%; border-collapse:collapse; margin:0.8em 0; font-size:10pt; border:2px solid #333;">
<tr>
<td style="border:1px solid #999; padding:0.5em; width:30%; text-align:right; background:#eaf0f7;">终端用户 / Agent</td>
<td style="border:1px solid #999; padding:0.5em; width:10%; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; width:20%; text-align:center; background:#e8e8e8; font-weight:600;">BSV 主链</td>
<td style="border:1px solid #999; padding:0.5em; width:10%; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; width:30%; background:#fafafa;">文件元数据 + 下载计费/HTLC 支付</td>
</tr>
<tr>
<td colspan="5" style="border:1px solid #999; padding:0.3em; text-align:center; font-size:9pt; color:#555; background:#fff;">↕ 内容哈希引用 (非跨链)</td>
</tr>
<tr>
<td style="border:1px solid #999; padding:0.5em; text-align:right; background:#f7f0ea;">内容所有者</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#e8e8e8; font-weight:600;">Metanet Overlay Network</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; background:#fafafa;">CDN 托管合约 + MNT 结算</td>
</tr>
<tr>
<td colspan="2" style="border:1px solid #999; padding:0.5em; text-align:right; background:#f0f7ea;"></td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#e8e8e8; font-weight:600;">Metanet Node</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; background:#fafafa;">实际内容存储与检索服务</td>
</tr>
</table>

---

## 四、双币种模型

| 币种 | 使用场景 | 接触者 |
|------|----------|--------|
| **BSV** | 下载计费、HTLC 文件购买、Metanet DAG 交易手续费 | 所有用户 |
| **MNT** | CDN 托管费、挖矿奖励、节点间批发结算 | 仅内容所有者和节点运营商 |

**设计目标**：普通用户只接触 BSV。MNT 是运营商侧的内部结算代币，对终端用户完全透明。

---

## 五、设计文档体系

```
design/
├── OverallDesign.zh.md          ← 本文档: 整体设计, 界面划分
├── bitfs/                       ← BitFS 文件系统协议设计
│   ├── 1-ConceptDesign.zh.md       概念设计 (愿景, 架构, 设计决策)
│   ├── 2-SystemDesign.zh.md        系统设计 (模块, 接口, 数据流)
│   ├── 3-DetailedDesign.zh.md      详细设计 (算法, 数据结构, 协议)
│   └── 4-TestDesign.zh.md          测试设计 (~1022 测试用例, 32 categories)
└── metanet/                     ← Metanet CDN 网络设计
    ├── 1-ConceptDesign.zh.md       概念设计 (CDN 模型, 经济设计)
    ├── 2-SystemDesign.zh.md        系统设计 (节点, 合约, 支付通道)
    ├── 3-DetailedDesign.zh.md      详细设计 (共识, 挖矿, 结算)
    └── 4-TestDesign.zh.md          测试设计
```

每个产品的设计文档遵循四层结构：概念设计 → 系统设计 → 详细设计 → 测试设计。

---

## 六、共享设计原则

1. **Unix 哲学** — 每个工具做一件事，可管道组合 (`bcat txid | jq .`)
2. **Agent-first** — CLI 输出结构化 (JSON)，下载计费付费墙即可编程支付接口
3. **默认加密** — Method 42 (ECDH + BIP32)，所有数据加密存储，密钥由文件路径确定性派生
4. **SPV 模式** — 本地保存交易 + Merkle proof，从不查询区块链全节点
5. **Overlay + ML 共识** — Metanet 是 BSV 上的 Overlay Network；ON 层按 CSW Multilevel Blockchain 采用 Bitcoin 风格 PoW（5 分钟出块）排序，区块通过合法 BSV 交易确认
6. **BRC 标准兼容** — 遵循 BSV Association 的 BRC 标准体系
7. **BSV 官方库** — 唯一 BSV 依赖为 `github.com/bsv-blockchain/go-sdk`
8. **元数据与内容分离** — 链上只存元数据 (Metanet DAG)，内容独立存储
9. **交易与合约优先** — 系统设计与详细设计中，Bitcoin 交易结构 (输入/输出/锁定脚本/状态迁移) 与合约脚本验证路径是核心主线；功能设计必须可映射到可审计交易与可验证 Script

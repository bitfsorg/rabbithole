# Metanet 官网内容大纲

> Single Source of Truth — 修改此文件后重新生成 index.html / index.zh.html

---

## 一、视觉识别 (VI)

### 配色

| 变量 | 色值 | 用途 |
|------|------|------|
| `--m-bg` | `#1e1e1e` | 主背景（中性深灰） |
| `--m-bg2` | `#2a2a2a` | 次级背景 |
| `--m-bg3` | `#333333` | 第三级背景 |
| `--m-border` | `#404040` | 边框/分割线 |
| `--m-amber` | `#e8983e` | 主强调色（按钮、CTA） |
| `--m-amber-light` | `#f0ad5e` | Hover/高亮 |
| `--m-amber-dark` | `#c47a28` | 暗色变体 |
| `--m-red` | `#d44030` | 装饰红条/次强调 |
| `--m-text` | `#d4d4d4` | 正文 |
| `--m-text-dim` | `#888888` | 次要文字 |
| `--m-text-bright` | `#ffffff` | 标题/强调 |

### 字体

| 用途 | 字体栈 |
|------|--------|
| Sans | Inter, Noto Sans SC, -apple-system, BlinkMacSystemFont, sans-serif |
| Mono | JetBrains Mono, SF Mono, Fira Code, monospace |

> 注意：Metanet 不使用衬线体，与 BitFS 的 Dark Botanical 风格区分。纯工业/技术风。

### 风格：Industrial Tech

- 中性深灰背景 + 琥珀色强调
- Ghost Number 装饰（巨大半透明数字，opacity 0.04）
- 红色装饰短条（48px × 3px）用于章节标记前
- 全大写标题（font-weight 900, letter-spacing -0.04em）
- 琥珀色填充卡片（特殊高亮用）
- 无衬线全栈，硬朗工业感

### 按钮样式

| 类名 | 样式 | 用途 |
|------|------|------|
| `.btn-primary` | 琥珀填充 + 黑色文字, weight 800 | 主 CTA |
| `.btn-secondary` | 琥珀描边 2px + 琥珀文字 | 次 CTA |
| `.btn-ghost` | 琥珀文字 + 悬停箭头 "→" | 辅助链接 |

### Logo

- "META" = Inter, font-weight: 900, 琥珀色
- "NET" = Inter, font-weight: 900, 白色

---

## 二、页面结构（10 个板块）

---

### 板块 1：导航栏

**固定顶部** | 毛玻璃效果 | Z-index 1000

| 元素 | EN | ZH |
|------|----|----|
| Logo | METANET | METANET |
| 链接 1 | Network → `#network` | 网络 → `#network` |
| 链接 2 | Nodes → `#nodes` | 节点 → `#nodes` |
| 链接 3 | Token → `#token` | 代币 → `#token` |
| 链接 4 | Docs → `#docs` | 文档 → `#docs` |
| 链接 5 | GitHub → external | GitHub → external |

- 移动端：汉堡菜单（三横线 → X 动画）
- 滚动时底部出现边框线

---

### 板块 2：Hero

**全屏首屏** | 两栏布局（左文字 + 右琥珀卡片）| Ghost Number "01"

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | RETRIEVAL-INCENTIVIZED CDN | 检索激励型 CDN |
| 主标题 | CACHE.<br>EARN.<br>**DELIVER.** | 缓存。<br>获利。<br>**分发。** |
| 副标题 | Node operators profit from serving popular content. A self-organizing delivery network powered by market incentives. | 节点运营者通过分发热门内容获取收益。一个由市场激励驱动的自组织内容分发网络。 |
| 段标记 | PART 01 — THE NETWORK | 第一章 — 网络 |
| 按钮 1（primary） | Read the Docs → `#docs` | 阅读文档 → `#docs` |
| 按钮 2（secondary） | Run a Node → `#nodes` | 运行节点 → `#nodes` |

主标题最后一行 "DELIVER." / "分发。" 用琥珀色。全大写，72px（桌面）。

**右侧琥珀卡片：**

| 元素 | EN | ZH |
|------|----|----|
| 标题 | Paradigm Shift | 范式革命 |
| 内容 | Not a new blockchain. Built on top of Bitcoin — a new internet infrastructure. | 不是新的区块链。在比特币之上，构建全新的互联网基础设施。 |

---

### 板块 3：什么是 Metanet

**id="network"** | Ghost Number "02"

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | PART 02 — WHAT IS METANET | 第二章 — 什么是 METANET |
| 标题 | THE RETRIEVAL<br>NETWORK | 检索网络 |
| 导语（大字） | Metanet is a decentralized content delivery network where node operators cache and serve data for profit. | Metanet 是一个去中心化内容分发网络，节点运营者通过缓存和分发数据获取收益。 |
| 描述 | Unlike traditional storage networks (Filecoin, Arweave), Metanet incentivizes *retrieval*, not storage. Popular content naturally replicates as nodes compete to serve it. The result is a self-organizing CDN that routes around failures and optimizes for demand. | 与传统存储网络（Filecoin、Arweave）不同，Metanet 激励的是*检索*而非存储。热门内容会随着节点争相分发而自然复制扩散。最终形成一个自组织的 CDN 网络，能够自动绕过故障节点，并根据需求优化路由。 |

**三张技术卡片：**

| # | 标签 | 描述 EN | 描述 ZH |
|---|------|---------|---------|
| 1 | x402 | HTTP-native micropayments for every file retrieval. Pay-per-byte with no subscription overhead. | HTTP 原生微支付，每次文件检索即时结算。按字节付费，无需订阅。 |
| 2 | HTLC | Trustless atomic swaps between buyers and sellers. No escrow, no intermediary, no counterparty risk. | 买卖双方之间的无信任原子交换。无托管、无中介、无交易对手风险。 |
| 3 | SPV | Lightweight verification without full blockchain sync. Nodes validate payments instantly with Merkle proofs. | 无需同步完整区块链即可完成轻量级验证。节点通过 Merkle 证明即时验证支付。 |

---

### 板块 4：三层架构

**居中布局** | Ghost Number "◊"

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | THREE-LAYER ARCHITECTURE | 三层架构 |
| 标题 | BUILT ON<br>BITCOIN | 构建于<br>比特币之上 |
| 副标题 | Three layers, each with a clear responsibility. No reinvented consensus. No unnecessary complexity. | 三层架构，各司其职。不重新发明共识机制，不引入不必要的复杂性。 |

**三个层次（垂直堆叠，箭头连接）：**

| # | 名称 EN | 名称 ZH | 描述 EN | 描述 ZH |
|---|---------|---------|---------|---------|
| 01 | BSV Layer | BSV 层 | Ownership, payments, and anchoring. The immutable foundation for all Metanet operations. | 所有权、支付与锚定。所有 Metanet 操作的不可变基础层。 |
| 02 | Daemon Layer | Daemon 层 | LFCP content serving, x402 payment handling, and Metanet metadata. The bridge between blockchain and CDN. | LFCP 内容服务、x402 支付处理与 Metanet 元数据。区块链与 CDN 之间的桥梁层。 |
| 03 | Metanet Chain | Metanet Chain | BSV-homomorphic sidechain — identical tx format, Bitcoin Script, MNT Token. Handles node economics and CDN incentives. | BSV 同构侧链 — 相同的交易格式、Bitcoin Script、MNT Token。处理节点经济与 CDN 激励。 |

---

### 板块 5：节点运营

**id="nodes"** | Ghost Number "03"

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | PART 03 — NODE OPERATORS | 第三章 — 节点运营 |
| 标题 | RUN A NODE.<br>**EARN REVENUE.** | 运行节点。<br>**获取收益。** |
| 副标题 | The network rewards operators who serve content users actually want. No wasted resources on cold storage. | 网络奖励分发用户真正需要的内容的运营者。不在冷存储上浪费资源。 |

**四张特性卡片（2×2 网格）：**

| # | 标题 EN | 标题 ZH | 描述 EN | 描述 ZH |
|---|---------|---------|---------|---------|
| 1 | Earn from Popular Content | 从热门内容中获利 | Cache trending files, serve them to users, collect retrieval fees automatically. Demand drives your revenue. | 缓存热门文件，分发给用户，自动收取检索费用。需求驱动你的收入。 |
| 2 | Low Barrier to Entry | 低门槛入场 | Run the daemon, start earning. No specialized hardware required, no minimum stake. A standard server is all you need. | 运行守护进程，即刻开始赚取收益。无需专用硬件，无最低质押要求，一台标准服务器即可。 |
| 3 | Revenue Sharing | 收益分成 | Content owners set revenue splits. Earn passive income from content you helped distribute across the network. | 内容所有者设定收益分成比例。通过参与内容分发，获取被动收入。 |
| 4 | Dual Payment Channels | 双支付通道 | BSV channels for user payments, MNT channels for node economics. Two rails, one seamless experience. | BSV 通道处理用户支付，MNT 通道处理节点经济。双轨运行，无缝体验。 |

---

### 板块 6：代币经济

**id="token"** | Ghost Number "04"

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | PART 04 — MNT TOKEN | 第四章 — MNT 代币 |
| 标题 | TOKEN<br>ECONOMICS | 代币经济 |
| 副标题 | MNT is the native token of the Metanet Chain. It aligns incentives between content owners, node operators, and users. | MNT 是 Metanet Chain 的原生代币。它在内容所有者、节点运营者和用户之间实现激励对齐。 |

**三张代币卡片（3 列）：**

| # | 图标 | 标题 EN | 标题 ZH | 描述 EN | 描述 ZH |
|---|------|---------|---------|---------|---------|
| 1 | △ | MINING | 挖矿 | Node operators mine MNT through merged mining with BSV. No minimum stake, no slashing. Organic growth like Bitcoin. | 节点运营者通过与 BSV 合并挖矿获取 MNT。无最低质押，无罚没机制，如比特币般有机增长。 |
| 2 | ⚙ | RETRIEVAL FEES | 检索费用 | Micro-fees paid in BSV for each content retrieval via x402 protocol. Prices set by market forces between competing node operators. | 每次内容检索通过 x402 协议支付 BSV 微费用。价格由竞争中的节点运营者通过市场力量决定。 |
| 3 | ⚘ | REVENUE SHARE | 收益分成 | Content owners configure ISO revenue splits. Operators earn a percentage of all retrieval fees for content they serve. | 内容所有者配置 ISO 收益分成比例。运营者从其分发内容的所有检索费用中赚取一定比例。 |

---

### 板块 7：命令行

**id="docs"** | Ghost Number "05"

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | PART 05 — COMMAND LINE | 第五章 — 命令行 |
| 标题 | UNIX-NATIVE<br>TOOLING | Unix 原生工具 |

**终端窗口内容（EN/ZH 共用）：**

标题栏：`metanet — terminal`（三色点：红 #d44030、黄 #e8983e、绿 #4caf50）

```bash
# Start a Metanet node
$ metanet node start

# Check node earnings
$ metanet earnings --period 7d

# View cached content stats
$ metanet cache stats --top 20

# Withdraw earnings
$ metanet withdraw --amount 50 --to 1A1zP1...
```

语法高亮：`#` 注释 dim，`$` 琥珀，命令 bright，`--flag` 琥珀浅色，数值 绿色 #8bc68b

---

### 板块 8：生态系统

**id="ecosystem"** | 两栏布局 | Ghost Number "06"

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | PART 06 — ECOSYSTEM | 第六章 — 生态系统 |
| 标题 | METANET<br>**DELIVERS.**<br>BITFS CREATES. | METANET<br>**负责分发。**<br>BITFS 负责创建。 |
| 描述 | Two protocols, one mission. BitFS provides the encrypted decentralized filesystem. Metanet provides the delivery infrastructure. Together, they form a complete stack for decentralized data. | 两个协议，一个使命。BitFS 提供加密的去中心化文件系统。Metanet 提供分发基础设施。两者共同构成去中心化数据的完整技术栈。 |

**右侧琥珀描边卡片：**

| 元素 | EN | ZH |
|------|----|----|
| 标签 | SISTER PROTOCOL | 姊妹协议 |
| Logo | BITFS | BITFS |
| 描述 | Unix-style encrypted filesystem on BSV blockchain. Create, encrypt, and manage files with familiar CLI tools. Metanet handles the rest — caching, delivery, and monetization. | 基于 BSV 区块链的 Unix 风格加密文件系统。使用熟悉的 CLI 工具创建、加密和管理文件。剩下的交给 Metanet — 缓存、分发和变现。 |
| 链接（ghost） | Visit bitfs.org → external | 访问 bitfs.org → external |

---

### 板块 9：白皮书 CTA

**居中布局** | 120px 上下 padding

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | DEEP DIVE | 深入了解 |
| 标题 | READ THE<br>WHITEPAPER | 阅读白皮书 |
| 描述 | A complete technical specification covering architecture, token economics, consensus mechanisms, and the retrieval-incentive model. | 完整的技术规范，涵盖架构设计、代币经济、共识机制及检索激励模型。 |
| 按钮（primary, 大号） | Download Whitepaper | 下载白皮书 |

---

### 板块 10：页脚

| 元素 | EN | ZH |
|------|----|----|
| Logo | METANET | METANET |
| 版权 | © 2026 Metanet. All rights reserved. | © 2026 Metanet. 保留所有权利。 |
| 链接 1 | Whitepaper → `#whitepaper` | 白皮书 → `#whitepaper` |
| 链接 2 | GitHub → external | GitHub → external |
| 链接 3 | bitfs.org → external | bitfs.org → external |

---

## 三、技术规格

### 响应式断点

| 断点 | 变化 |
|------|------|
| >768px | 桌面布局：多列网格、Hero 两栏 |
| ≤768px | 移动布局：单列、汉堡菜单、按钮堆叠 |
| ≤480px | 小屏：标题 72→48→40px、Ghost Number 120→80px |

### 动画系统

| 类名 | 效果 | 时长 |
|------|------|------|
| `.reveal` | fade + translateY(32px) | 0.7s cubic-bezier(0.22, 1, 0.36, 1) |
| `.reveal-delay-N` | 延迟 N×0.1s | — |

触发方式：IntersectionObserver

### 外部链接

| 目标 | URL |
|------|-----|
| GitHub | `https://github.com` (placeholder) |
| 白皮书 | `#whitepaper` (页内锚点) |
| BitFS | `https://bitfs.org` |

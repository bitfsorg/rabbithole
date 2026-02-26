# BitFS 官网内容大纲

> Single Source of Truth — 修改此文件后重新生成 index.html / index.zh.html

---

## 一、视觉识别 (VI)

### 配色

| 变量 | 色值 | 用途 |
|------|------|------|
| `--b-bg` | `#110f0d` | 主背景（深炭） |
| `--b-bg2` | `#1a1714` | 卡片背景 |
| `--b-bg3` | `#231f1b` | 次级区域 |
| `--b-border` | `#2e2924` | 边框/分割线 |
| `--b-gold` | `#c9956b` | 主强调色（按钮、链接） |
| `--b-gold-light` | `#dbb08a` | Hover/高亮 |
| `--b-gold-dark` | `#a07548` | 暗色变体 |
| `--b-text` | `#d4cdc4` | 正文 |
| `--b-text-dim` | `#8a8078` | 次要文字 |
| `--b-text-bright` | `#f0ebe5` | 标题/强调 |

### 字体

| 用途 | 字体栈 |
|------|--------|
| Sans | Inter, Noto Sans SC, -apple-system, sans-serif |
| Serif | Cormorant Garamond, Noto Serif SC, Georgia, serif |
| Mono | JetBrains Mono, SF Mono, monospace |

### 风格：Dark Botanical

- 深色暖调背景 + 金色强调
- Bokeh 光斑装饰（radial-gradient 圆形光晕）
- 金色渐变分割线
- 衬线体用于标题和引用（italic），无衬线用于正文
- Scroll-reveal 动画（fade + translateY, 0.8s）

### 按钮样式

| 类名 | 样式 | 用途 |
|------|------|------|
| `.btn-primary` | 金色填充 + 深色文字 | 主 CTA |
| `.btn-secondary` | 金色描边 + 金色文字 | 次 CTA |
| `.btn-ghost` | 衬线斜体 + 下划线 | 辅助链接 |

### Logo

- "Bit" = Inter sans-serif, font-weight: 300, 亮色
- "FS" = Cormorant Garamond, italic bold, 金色

---

## 二、页面结构（9 个板块）

---

### 板块 1：导航栏

**固定顶部** | 高度 64px | 毛玻璃效果（backdrop-filter: blur）

| 元素 | EN | ZH |
|------|----|----|
| Logo | BitFS | BitFS |
| 链接 1 | Whitepaper | 白皮书 |
| 链接 2 | GitHub | GitHub |

- 移动端：汉堡菜单 → 全屏覆盖导航
- GitHub 链接地址：`https://github.com/nicklaus4/bitfs`

---

### 板块 2：Hero

**全屏首屏** | 最小高度 100vh | 居中排列

装饰元素：4 个 Bokeh 光斑（左上、右下、右上微弱、左下微弱）

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | Part 01 — Paradigm Shift | 第一章 — 范式革命 |
| 主标题 | Your keys, **your data.**<br>No middleman. | 你的密钥，**你的数据。**<br>无需中间人。 |
| 副标题 | A decentralized file system that maps Unix semantics onto blockchain, encrypts everything by default, and enables trustless data commerce through atomic swaps. | 一个将 Unix 文件系统语义映射到区块链的去中心化文件系统，默认加密所有数据，并通过原子交换实现无信任的数据交易。 |
| 按钮 1（primary） | Read Whitepaper → `whitepaper/` | 阅读白皮书 → `whitepaper/` |
| 按钮 2（secondary） | View on GitHub → external | 查看源码 → external |

主标题中加粗部分用 `--b-gold` 色（衬线斜体，42-72px 响应式）

---

### 板块 3：什么是 BitFS

**三栏概览** | 3 列网格（移动端 1 列）

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | Part 02 — What is BitFS | 第二章 — 什么是 BitFS |

**三张卡片：**

| # | 符号 | 标题 EN | 标题 ZH | 描述 EN | 描述 ZH |
|---|------|---------|---------|---------|---------|
| 1 | `/—` | Metanet DAG | Metanet DAG | Unix filesystem on blockchain — inodes, directories, symbolic links, all as native transactions. | 区块链上的 Unix 文件系统 — inode、目录、符号链接，全部作为原生交易实现。 |
| 2 | `∴` | Method 42 Encryption | Method 42 加密 | Every file encrypted by default using deterministic ECDH. No key management headaches. | 所有文件默认加密，基于确定性 ECDH 算法。无需繁琐的密钥管理。 |
| 3 | `⊢` | SPV Mode | SPV 模式 | Local transactions + Merkle proofs. Never queries the blockchain directly. | 本地交易 + Merkle 证明。永远不需要直接查询区块链。 |

---

### 板块 4：系统架构

**三层堆叠** | 垂直布局 + 连接线

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | Part 03 — Architecture | 第三章 — 系统架构 |
| 标题 | Three layers, one filesystem | 三层架构，一个文件系统 |
| 描述 | BitFS separates concerns into distinct layers — ownership lives on-chain, logic runs locally, and delivery scales through an incentivized network. | BitFS 将关注点分离到不同层次 — 所有权存储在链上，逻辑在本地运行，分发通过激励网络扩展。 |

**三个层次（从上到下）：**

| 层 | 标签 | 标题 EN | 标题 ZH | 描述 EN | 描述 ZH |
|----|------|---------|---------|---------|---------|
| 3 | LAYER 3 | Metanet Chain | Metanet Chain | Decentralized CDN. A BSV-homomorphic chain that incentivizes data retrieval across a global network of nodes. Separate product at metanet.org. | 去中心化 CDN。一条 BSV 同构链，通过全球节点网络激励数据检索。独立产品，详见 metanet.org。 |
| 2 | LAYER 2 | BitFS Daemon | BitFS 守护进程 | Local-first control plane. Manages encryption, transaction construction, SPV validation, and the Unix filesystem abstraction. Your keys never leave your machine. | 本地优先的控制平面。管理加密、交易构建、SPV 验证以及 Unix 文件系统抽象层。你的密钥永远不会离开你的设备。 |
| 1 | LAYER 1 | BSV Blockchain | BSV 区块链 | Immutable ownership layer. Stores Metanet DAG transactions, payment channels, and HTLC atomic swap contracts. Unbounded scaling for data at rest. | 不可变的所有权层。存储 Metanet DAG 交易、支付通道和 HTLC 原子交换合约。为静态数据提供无限扩展能力。 |

---

### 板块 5：核心特性

**左右交替布局** | 4 个特性，每个含文字+可视化演示

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | Part 04 — Key Features | 第四章 — 核心特性 |
| 标题 | Built for sovereignty | 为数据主权而生 |

#### 特性 1：HTLC 原子交换

| 元素 | EN | ZH |
|------|----|----|
| 编号 | 01 | 01 |
| 标题 | HTLC Atomic Swap | HTLC 原子交换 |
| 描述 | Trustless buy and sell. Hash time-locked contracts ensure fair exchange without intermediaries. If either party fails to act, funds return automatically. | 无信任的买卖交易。哈希时间锁合约确保公平交换，无需中间人。如果任何一方未能履约，资金将自动退回。 |

**可视化：SWAP PROTOCOL 流程图**

| 步骤 | EN | ZH |
|------|----|----|
| 1 | Buyer creates HTLC, locking payment to file hash | 买方创建 HTLC，将付款锁定到文件哈希 |
| 2 | Seller reveals preimage (decryption key), claims payment | 卖方揭示原像（解密密钥），领取付款 |
| 3 | Buyer uses preimage to decrypt file | 买方使用原像解密文件 |

#### 特性 2：Unix CLI

| 元素 | EN | ZH |
|------|----|----|
| 编号 | 02 | 02 |
| 标题 | Unix CLI | Unix 命令行 |
| 描述 | ls, cat, put, get — familiar commands for a revolutionary filesystem. Agent-first design means every operation is scriptable. | ls、cat、put、get — 用熟悉的命令操作革命性的文件系统。Agent 优先设计意味着每个操作都可脚本化。 |

**可视化：TERMINAL 代码块**

```bash
# Navigate like any Unix system
$ bitfs ls -la /shared/docs/
# Pipe, redirect, compose
$ bitfs cat /notes/draft.md | wc -l
# Upload with encryption
$ bitfs put --encrypt ./file.pdf
```

#### 特性 3：Paymail 集成

| 元素 | EN | ZH |
|------|----|----|
| 编号 | 03 | 03 |
| 标题 | Paymail Integration | Paymail 集成 |
| 描述 | Human-readable addresses. Send files to user@bitfs.org instead of cryptographic hashes. Built on the Paymail protocol for seamless identity resolution. | 人类可读的地址。向 user@bitfs.org 发送文件，而不是使用晦涩的加密哈希。基于 Paymail 协议实现无缝身份解析。 |

**可视化：ADDRESS RESOLUTION**

```
02a1b3c4d5e6f7...8a9b0c1d2e3f4
        ↓ becomes
    alice@bitfs.org
```

#### 特性 4：BIP32 访问控制

| 元素 | EN | ZH |
|------|----|----|
| 编号 | 04 | 04 |
| 标题 | BIP32 Access Control | BIP32 访问控制 |
| 描述 | Hierarchical key derivation for fine-grained file permissions. Share access to a directory without exposing parent keys. Revoke by rotating a single derivation path. | 分层密钥派生实现细粒度的文件权限管理。共享目录访问权限而不暴露父密钥。通过轮换单个派生路径即可撤销权限。 |

**可视化：KEY HIERARCHY 密钥树**

```
m — master key (BIP44: m/44'/236'/...)
 ├─ m/44'/236'/0' — vault 0 (filesystem root)
 │  ├─ m/44'/236'/0'/0/0 — root directory
 │  ├─ m/44'/236'/0'/0/1 — /home/
 │  ├─ m/44'/236'/0'/0/2 — /shared/
 ├─ m/44'/236'/1' — vault 1
 ├─ m/44'/236'/2' — payment keys
```

---

### 板块 6：CLI 实战演示

**终端窗口组件** | 最大宽度 720px

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | Part 05 — In Practice | 第五章 — 实战演示 |
| 标题 | See it in action | 实战演示 |

**终端内容（EN/ZH 共用）：**

```bash
# Upload and encrypt a file
$ bitfs put --encrypt ./report.pdf /shared/docs/
  encrypted → tx:a3f8...c2d1  [2 inputs, 3 outputs]

# List with Merkle proofs
$ bitfs ls -l --spv /shared/docs/
  -rw-r--  42.3K  report.pdf  [spv: verified]
  -rw-r--  1.8K   notes.md    [spv: verified]

# Trustless sale via HTLC
$ bitfs sell --htlc /shared/docs/report.pdf --price 0.001BSV
  htlc published → hash:7b2e...f194  expires: 144 blocks
```

语法高亮：`#` 注释为 dim，`$` 为 gold，命令为 bright，`--flag` 为 gold-light，输出为 dim

---

### 板块 7：白皮书 CTA

**居中文字** | 上下 120px padding

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | Part 06 — Deep Dive | 第六章 — 深入了解 |
| 标题 | Read the Whitepaper | 阅读白皮书 |
| 描述 | Explore the full technical design — from Metanet DAG construction to Method 42 encryption, HTLC protocols, and the Metanet Chain incentive model. | 深入了解完整技术设计 — 从 Metanet DAG 构建到 Method 42 加密、HTLC 协议，以及 Metanet Chain 激励模型。 |
| 按钮（primary） | Download Whitepaper → `whitepaper/` | 下载白皮书 → `whitepaper/` |
| 辅助链接（ghost） | Available in English & Chinese | 提供中英文版本 |

---

### 板块 8：生态系统

**两栏布局** | 左文字 + 右卡片对比

| 元素 | EN | ZH |
|------|----|----|
| 章节标记 | Part 07 — Ecosystem | 第七章 — 生态系统 |
| 标题 | Two products, one vision | 两个产品，一个愿景 |
| 高亮语 | BitFS stores your files.<br>Metanet delivers them to the world. | BitFS 存储你的文件。<br>Metanet 将它们传递到全世界。 |
| 描述 | Think of BitFS as the filesystem and Metanet as the delivery network. Together they form a complete decentralized storage-and-retrieval stack — analogous to how IPFS and Filecoin complement each other, but built on Bitcoin. | 可以把 BitFS 理解为文件系统，Metanet 理解为分发网络。两者共同构成完整的去中心化存储与检索技术栈 — 类似于 IPFS 与 Filecoin 的互补关系，但构建在比特币之上。 |
| 按钮 | Visit metanet.org → external | 访问 metanet.org → external |

**右侧两张对比卡片：**

| # | 标签 EN | 标签 ZH | 标题 | 描述 EN | 描述 ZH |
|---|---------|---------|------|---------|---------|
| 1 | FILESYSTEM | 文件系统 | BitFS | Decentralized encrypted file system. Unix CLI, Method 42 encryption, HTLC atomic swaps. Your keys, your data. | 去中心化加密文件系统。Unix 命令行、Method 42 加密、HTLC 原子交换。你的密钥，你的数据。 |
| 2 | DELIVERY NETWORK | 分发网络 | Metanet | Decentralized CDN powered by the Metanet Chain. Incentivized retrieval, global node network, MNT token economics. | 由 Metanet Chain 驱动的去中心化 CDN。激励检索、全球节点网络、MNT 代币经济。 |

---

### 板块 9：页脚

| 元素 | EN | ZH |
|------|----|----|
| Logo | BitFS | BitFS |
| 版权 | © 2026 BitFS | © 2026 BitFS |
| 链接 1 | Whitepaper | 白皮书 |
| 链接 2 | GitHub | GitHub |
| 链接 3 | metanet.org | metanet.org |

---

## 三、技术规格

### 响应式断点

| 断点 | 变化 |
|------|------|
| >768px | 桌面布局：多列网格、左右交替特性行 |
| ≤768px | 移动布局：单列、汉堡菜单、堆叠按钮 |
| ≤480px | 小屏优化：缩小字号、CLI 字号降至 11px |

### 动画系统

| 类名 | 效果 | 时长 |
|------|------|------|
| `.reveal` | fade + translateY(28px) | 0.8s ease |
| `.reveal-delay-N` | 延迟 N×0.1s | — |

触发方式：IntersectionObserver（threshold: 0.12, rootMargin: -40px）

### 外部链接

| 目标 | URL |
|------|-----|
| GitHub | `https://github.com/nicklaus4/bitfs` |
| 白皮书 | `whitepaper/` (相对路径) |
| Metanet | `https://metanet.org` |

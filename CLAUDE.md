# RabbitHole 工作区

本仓库是 BitFS + Metanet 两产品生态的单一工作区（monorepo）。

## 两产品生态

| | BitFS | Metanet |
|---|---|---|
| 定位 | 去中心化加密文件系统协议 | 去中心化 CDN 网络 |
| 域名 | bitfs.org | metanet.org |
| CLI | `bitfs` (文件所有者/访问者) | `metanet` (CDN 节点运营商) |
| 用户 | 终端用户、AI Agent | 内容所有者、节点运营商 |
| 货币 | BSV | MNT Token |
| 类比 | IPFS (协议层) | Filecoin (激励层) |

两个核心卖点：**Agent Friendly** + **数据可以不上链**。

## 目录结构

```
RabbitHole/
├── docs/              ← 统一文档中心
│   ├── design/        ← 设计文档 (md 源文件 + HTML→PDF)
│   ├── whitepaper/    ← 白皮书 (md 大纲 → LaTeX → PDF)
│   ├── slides/        ← 演示文稿 (HTML5)
│   ├── references/    ← 研究论文 (6 篇 PDF)
│   ├── vi/            ← 视觉识别系统
│   ├── plans/         ← 设计与实施计划 (按项目分子目录)
│   ├── specs/         ← 模块规格说明 (按项目分子目录)
│   ├── audits/        ← 审查报告
│   └── tasks/         ← 任务追踪
├── websites/          ← 官网 (bitfs.org + metanet.org)
├── bitfs/             ← BitFS Go 实现 (CLI + daemon)
├── metanet/           ← Metanet Go 实现 (CDN 节点)
├── libbitfs-go/       ← 共享核心库 Go (独立 repo)
├── libbitfs-ts/       ← 共享核心库 TypeScript (待开发)
├── den-explorer/      ← BitFS 区块链浏览器 (Go + htmx)
├── git-remote-bitfs/  ← Git remote helper (独立 repo)
├── bitfs-app/         ← BitFS 桌面/移动客户端 (Flutter, 独立 repo)
├── bitfs-extension/   ← BitFS 浏览器扩展 (TypeScript, 独立 repo)
└── tools/             ← 构建工具
```

## 各目录详细介绍

### bitfs/ — BitFS CLI + Daemon

Unix 风格的去中心化加密文件系统，Go 实现。Module: `github.com/bitfsorg/bitfs`

**架构三层**:
- `cmd/bitfs/` — 主 CLI（wallet/vault/put/mkdir/rm/mv/cp/link/sell/encrypt/publish/shell/daemon）
- `cmd/b*/` — 只读工具集（bls/bcat/bget/bstat/btree），通过 HTTP 连接 daemon
- `internal/engine/` — 统一业务逻辑层（所有 CLI 命令、shell REPL、daemon 适配器共用）
- `internal/daemon/` — LFCP HTTP 服务器（内容服务、Metanet 元数据、Method 42 握手、x402 支付）
- `internal/client/` — b-tools 的 HTTP 客户端

**其他目录**:
- `docs/` — 用户文档 (api-reference, user-guide, CLI-MANUAL-TEST-GUIDE)
- `integration/` — 276 个集成测试（19 文件，`-tags=integration`）
- `e2e/` — Docker regtest 端到端测试（`-tags e2e`，需要 Docker Desktop）
- `dashboard/` — React SPA，通过 embed.go 嵌入 daemon 的 `/_dashboard/*`

**规模**: ~4,157 行代码，1,046 个测试函数，67 个测试文件

### libbitfs-go/ — 共享核心库 (Go)

独立 Git 仓库。Module: `github.com/bitfsorg/libbitfs-go`。bitfs/go.mod 通过 `replace => ../libbitfs-go` 引用。

**10 个包**:

| 包 | 用途 |
|---|------|
| method42 | ECDH 加密引擎（AES-256-GCM，三种访问模式: Private/Free/Paid） |
| wallet | HD 钱包（BIP44 m/44'/236'/account'/chain/index，Argon2id 种子加密） |
| tx | Metanet 交易构建器（CreateRoot/CreateChild/SelfUpdate/DataTx 四模板） |
| metanet | Metanet DAG + Unix 文件系统操作（目录增删改、链接、Merkle root、TLV 序列化） |
| spv | SPV 轻客户端（80 字节区块头、Merkle path 验证、头链验证） |
| storage | 内容寻址存储接口（SHA256 key_hash，hash-sharded 目录 ~/.bitfs/storage/） |
| network | 区块链服务抽象（BlockchainService 接口、RPCClient、SPVClient、网络预设） |
| config | 配置文件解析（key=value 格式） |
| paymail | Paymail 协议（.well-known/bsvalias 发现、PKI 端点解析） |
| x402 | HTTP 402 支付协议（X-Price/X-Invoice-Id 头、HTLC 构建、支付验证） |

**规模**: ~8,025 行代码（不含测试）

### metanet/ — Metanet CDN 节点

去中心化 CDN 网络，激励检索而非存储。Go 实现，开发中。

**7 个内部包**:
- `chain/` — Metanet Chain 核心（区块、Token、创世配置）
- `mining/` — 合并挖矿（AuxPoW、难度调整、BSV 锚定）
- `contract/` — 存储合约（交易、挑战、脚本）
- `proof/` — 存储证明（ECDH 加密、Merkle tree）
- `payment/` — 支付通道（BSV + MNT）
- `overlay/` — BRC Overlay 网络
- `config/` — 配置管理

**状态**: Phase 1（chain/mining）完成，Phase 2（contract）进行中。规格说明见 `docs/specs/metanet/`。

### den-explorer/ — 区块链浏览器

BSV 区块链浏览器，支持 BitFS/Metanet 协议解码。用于 regtest/testnet 调试。

**技术**: Go + htmx + libbitfs-go，单二进制无需构建工具
**功能**: 区块/交易浏览、UTXO 查询、Metanet OP_RETURN 解码、DAG 可视化、Method 42 分析、SPV 验证

### git-remote-bitfs/ — Git Remote Helper

独立 Git 仓库。将 Git 对象模型翻译为 Metanet DAG，支持 `bitfs://<address>[@network]` URL 协议。

**6 个内部包**: helper（Git 协议）、stream（fast-import/export）、mapper（Git SHA↔Metanet 映射）、chain（DAG 读写+加密+广播）、config（URL 解析+钱包加载）、utxo（UTXO 状态管理）

### bitfs-app/ — 跨平台客户端 (Flutter)

独立 Git 仓库。iOS/Android/macOS/Windows/Linux 全平台客户端。

**技术**: Flutter 3.27+ / Go 1.25+ (FFI 桥接) / Riverpod 2 (状态管理) / GoRouter (路由)
**结构**: `lib/`（Dart: providers/models/screens/widgets）、`native/`（Go cgo FFI）、平台壳（android/ios/macos/windows/linux）
**版本**: v0.1.0

### bitfs-extension/ — 浏览器扩展

独立 Git 仓库。MetaMask 模型的 Chrome 扩展，本地执行加密操作。

**技术**: React 19 / TypeScript / Vite / @noble/secp256k1 / @noble/hashes / @scure/bip32+bip39
**结构**: `src/popup/`（React 钱包 UI）、`src/background/`（Service Worker 密钥管理）、`src/content-script/`（bitfs:// 链接检测）、`src/bitfs-core/`（TypeScript 加密核心）
**特点**: 纯 JS 加密（无 WASM），~200KB bundle。版本 v0.1.0

### libbitfs-ts/ — 共享核心库 (TypeScript) [待开发]

libbitfs-go 的 TypeScript 镜像，目标: 浏览器 + Node.js 环境。ESM 优先，依赖 @bsv/sdk。
**状态**: 仅有 README.md 占位，10 个包与 libbitfs-go 对应，尚未实现。

### docs/design/ — 设计文档

四层设计文档体系，每个产品各 4 章：

| 层级 | BitFS | Metanet |
|------|-------|---------|
| 1-概念设计 | 愿景、架构、b* 工具、HD 钱包、Method 42 (17KB) | 产品定位、经济模型、Agent Friendly (6KB) |
| 2-系统设计 | 模块划分、接口定义、交易格式、CLI 命令 (103KB) | 三层架构 (L1:BSV/L2:Daemon/L3:Chain)、智能合约 (11KB) |
| 3-详细设计 | 算法细节、Bitcoin Script、x402 协议、BIP32 访问控制 (103KB) | 共识机制、挖矿协议、结算流程 (14KB) |
| 4-测试设计 | ~980 测试用例 (55KB) | ~20 测试用例 (8KB) |

**其他**: `0-OverallDesign.zh.md`（总体设计）、`diagrams/`（8 个 Mermaid .mmd + 7 个 SVG）、`pdf/`（9 个生成的 PDF + HTML 模板）

### docs/whitepaper/ — 白皮书

两篇学术论文，中英文双语：
- **BitFS**: "A Peer-to-Peer Encrypted File System on Blockchain" — Unix 映射、HD 密钥派生、Method 42、HTLC 原子交换
- **Metanet**: "The Metanet Network: A Decentralized CDN That Incentivizes Retrieval" — 激励层、存档合约、存储证明、支付通道

**管线**: `*-Outline.md`（大纲）→ `.tex`（LaTeX）→ tectonic (XeTeX) → `.pdf`
**字体**: Songti SC（中文衬线）、Times New Roman（英文正文）、Menlo（代码）

### websites/ — 官网

两个单页官网，纯 HTML+CSS+JS（无框架），中英文双语：
- **bitfs.org/** — 暗色植物系风格（深炭灰底 + 铜金强调色 `#c9956b`），9 个 section
- **metanet.org/** — 工业科技风格（中性暗灰底 + 琥珀强调色 `#e8983e`），10 个 section

**管线**: `Website-Content-Outline.md`（大纲）→ `index.html` + `index.zh.html`

### docs/slides/ — 演示文稿

BitFS 25 页 HTML5 幻灯片，暗色植物系设计（与 docs/vi/ 一致）。
6 个部分: 范式革命 → 所有权与密码学 → 交易 → 核心功能 → Token 经济 → 网络骨干

**管线**: `Slides-Outline.md`（大纲）→ `BitFS-presentation.html`

### docs/references/ — 参考论文

6 篇研究论文和专利，详见 `docs/references/CLAUDE.md`。核心参考: Paper #0（Metanet 专利）和 Paper #5（分布式存储验证）。

### docs/vi/ — 视觉识别系统

`vi-system.html` — 两产品完整 VI 规范：
- **BitFS Dark Botanical**: 金铜色调（`--b-gold: #c9956b`），Cormorant Garamond + Inter + JetBrains Mono
- **Metanet Bold Signal**: 琥珀色调（`--m-amber: #e8983e`），Inter 全字重 + JetBrains Mono

包含色板、Logo 系统、字体规范、组件样式。所有输出物（网站/幻灯片/PDF）遵循此 VI。

### tools/ — 构建工具

`tools/mermaid/` — Mermaid 图表渲染器（TypeScript/Bun CLI），将 `.mmd` 转换为 SVG。
**用法**: `bun tools/mermaid/cli.ts <dir> [--theme bitfs] [--no-ascii]`

## 文档生成工作流

本项目所有输出物均由 Markdown 大纲驱动，Markdown 是唯一的源文件：

| 目录 | 源文件 | 生成管线 | 输出 |
|------|--------|----------|------|
| docs/design/ | `.zh.md` | pandoc + HTML 模板 + weasyprint | 内部设计 PDF |
| docs/whitepaper/ | `*-Outline.md` | 大纲 → `.tex` → tectonic (XeTeX) | 学术论文 PDF |
| websites/ | `Website-Content-Outline.md` | 大纲 → `index.html` + `index.zh.html` | 单页官网 |
| docs/slides/ | `Slides-Outline.md` | 大纲 → `*-presentation.html` | HTML 幻灯片 |

**关键原则**：修改大纲/源文件，然后重新生成输出物。不要直接编辑生成的文件。

## 命名约定

- Metanet Chain / MNT Token / Metanet Node / Metanet DAG
- 注意："Metanet" 商标已被注册（USPTO #7300182，第42类），发布前需决定是否改名

## 技术要点

- BSV SDK: `github.com/bsv-blockchain/go-sdk` (唯一 BSV 依赖)
- go-sdk `compat/bip32` 包名是 `compat`，需要别名导入
- Method 42: `aes_key = HKDF-SHA256(ECDH(D_node, P_node).x, key_hash)`
- 设计文档中文，代码和 spec 英文

## 工作流规则

- **实施计划前清空上下文**：完成设计/计划阶段后，在开始执行实施计划之前，必须先使用 `/clear` 清空对话上下文，然后在新的上下文中加载计划文件并逐任务执行。避免在一个超长对话中同时完成设计和全部实施。
- **任务文件管理**：`docs/tasks/` 下的任务清单及其子目录（如 `docs/tasks/bitfs/` 等）都适用相同的规则：当一个 task 文件中的所有条目都（全部是 `[x]`）完成后，必须将该文件移动到该文件**当前所在目录**的 `done/` 目录下（如 `docs/tasks/done/` 或 `docs/tasks/bitfs/done/`）。若 `done/` 目录不存在则创建它。

## 许可证

- 源代码: OpenBSV License
- websites/, docs/whitepaper/, docs/design/: 单独许可

## 技术栈
- Go 1.25.6 + `github.com/bsv-blockchain/go-sdk` v1.2.18 (唯一 BSV 依赖)
- `github.com/stretchr/testify` v1.11.1, `golang.org/x/crypto` v0.47.0
- Content-addressed file store with hash-sharded directories (~/.bitfs/storage/)

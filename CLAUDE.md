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

## 快速参考

### 构建与测试

```bash
# Go 项目 (bitfs/, libbitfs-go/, metanet/, den-explorer/, git-remote-bitfs/)
go build ./...                                          # 构建
go test ./...                                           # 单元测试
go test -tags=integration ./integration/ -count=1       # 集成测试 (bitfs/)
go test -tags e2e ./e2e/                                # E2E 测试 (需 Docker Desktop)

# TypeScript 项目 (libbitfs-ts/, bitfs-extension/)
bun install && bun test

# 移动端 (bitfs-app/)
npm install && npm test

# 文档工具
bun tools/mermaid/cli.ts <dir> [--theme bitfs]          # Mermaid → SVG
```

### 独立 Git 仓库

`libbitfs-go/`、`libbitfs-ts/`、`den-explorer/`、`bitfs-app/`、`bitfs-desktop/`、`bitfs-extension/`、`bitfs-explorer/`、`git-remote-bitfs/` 各有自己的 `.git`，被 RabbitHole `.gitignore` 排除。`bitfs/go.mod` 通过 `replace => ../libbitfs-go` 引用共享库。

### 任务管理

- 任务文件: `docs/tasks/YYYY-MM-DD-feature-name{,-design}.md`
- 完成后移入: `docs/tasks/done/`
- 路线图: `docs/tasks/roadmap.md`
- 新功能开发前用 speckit 生成 spec

## 目录结构

```
RabbitHole/
├── docs/              ← 统一文档中心 (design/whitepaper/slides/specs/audits/tasks)
├── websites/          ← 官网 (bitfs.org + metanet.org, 纯 HTML)
├── bitfs/             ← BitFS Go 实现 (CLI + daemon), ~4K LOC
├── metanet/           ← Metanet Go 实现 (CDN 节点), 开发中
├── libbitfs-go/       ← 共享核心库 Go, ~8K LOC (独立 repo)
├── libbitfs-ts/       ← 共享核心库 TypeScript, ~11.5K LOC (独立 repo)
├── den-explorer/      ← 区块链浏览器 (Go + htmx, 独立 repo)
├── git-remote-bitfs/  ← Git remote helper (独立 repo)
├── bitfs-app/         ← 移动客户端 Expo/React Native (独立 repo)
├── bitfs-desktop/     ← 桌面客户端 Wails/Go (独立 repo)
├── bitfs-extension/   ← Chrome 扩展 MV3 (独立 repo)
├── bitfs-explorer/    ← BitFS Explorer Chrome 扩展 (独立 repo)
└── tools/             ← Mermaid 渲染器 (Bun CLI)
```

## 核心代码库

### bitfs/ — BitFS CLI + Daemon

Module: `github.com/bitfsorg/bitfs`

- `cmd/bitfs/` — 主 CLI（wallet/vault/put/mkdir/rm/mv/cp/link/sell/encrypt/publish/shell/daemon）
- `cmd/b*/` — 只读工具集（bls/bcat/bget/bmget/bstat/btree），通过 HTTP 连接 daemon
- `internal/engine/` — 统一业务逻辑层（所有 CLI 命令、shell REPL、daemon 适配器共用）
- `internal/daemon/` — LFCP HTTP 服务器（内容服务、Metanet 元数据、Method 42 握手、payment 支付）
- `internal/client/` — b-tools 的 HTTP 客户端
- `integration/` — 276 个集成测试, `e2e/` — Docker regtest 测试
- `dashboard/` — React SPA，embed.go 嵌入 daemon

### libbitfs-go/ — 共享核心库 (Go)

Module: `github.com/bitfsorg/libbitfs-go`

| 包 | 用途 |
|---|------|
| method42 | ECDH 加密引擎（AES-256-GCM，三种访问模式: Private/Free/Paid） |
| wallet | HD 钱包（BIP44 m/44'/236'/account'/chain/index，Argon2id 种子加密） |
| tx | Metanet 交易构建器（CreateRoot/CreateChild/SelfUpdate/DataTx 四模板） |
| metanet | Metanet DAG + Unix 文件系统操作（目录增删改、链接、Merkle root、TLV 序列化） |
| spv | SPV 轻客户端（80 字节区块头、Merkle path 验证、头链验证） |
| storage | 内容寻址存储（SHA256 key_hash，hash-sharded ~/.bitfs/storage/） |
| network | 区块链服务抽象（BlockchainService 接口、RPCClient、SPVClient） |
| config | 配置文件解析（key=value 格式） |
| paymail | Paymail 协议（.well-known/bsvalias 发现、PKI 端点解析） |
| payment | HTTP 402 支付协议（HTLC 构建、支付验证） |

### metanet/ — Metanet CDN 节点 (开发中)

7 个包: chain（区块/Token）、mining（AuxPoW）、contract（存储合约）、proof（存储证明）、payment（支付通道）、overlay（BRC Overlay）、config。Phase 1 完成，Phase 2 进行中。

### 其他子项目

| 目录 | 技术 | 说明 |
|------|------|------|
| libbitfs-ts/ | TypeScript, ESM, @bsv/sdk | libbitfs-go 的 TS 镜像，11 包 |
| den-explorer/ | Go + htmx | BSV 浏览器，Metanet 协议解码 |
| git-remote-bitfs/ | Go | Git ↔ Metanet DAG 翻译，`bitfs://` 协议 |
| bitfs-app/ | Expo SDK 55, RN Paper, Zustand | iOS/Android 客户端 |
| bitfs-extension/ | React 19, Vite, Chrome MV3 | 浏览器钱包扩展 |

## 文档体系

### 设计文档 (docs/design/)

四层设计文档，每产品各 4 章（概念/系统/详细/测试）。另有 `OverallDesign.zh.md` 总体设计。

### 文档生成工作流

Markdown 是唯一源文件，不要直接编辑生成物：

| 源 | 管线 | 输出 |
|----|------|------|
| `docs/design/*.zh.md` | pandoc + weasyprint | 内部 PDF |
| `docs/whitepaper/*-Outline.md` | LaTeX (tectonic) | 学术论文 PDF |
| `websites/*-Content-Outline.md` | 手工生成 | 单页官网 HTML |
| `docs/slides/Slides-Outline.md` | 手工生成 | HTML5 幻灯片 |

### VI 系统 (docs/vi/)

- **BitFS Dark Botanical**: `--b-gold: #c9956b`, Cormorant Garamond + Inter + JetBrains Mono
- **Metanet Bold Signal**: `--m-amber: #e8983e`, Inter + JetBrains Mono

## 技术要点

- BSV SDK: `github.com/bsv-blockchain/go-sdk` (唯一 BSV 依赖)
- go-sdk `compat/bip32` 包名是 `compat`，需要别名导入
- Method 42: `aes_key = HKDF-SHA256(ECDH(D_node, P_node).x, key_hash)`
- 设计文档中文，代码和 spec 英文
- "Metanet" 商标已注册（USPTO #7300182），发布前需决定是否改名

## 工作流规则

- **实施计划前清空上下文**：完成设计/计划阶段后，`/clear` 清空对话，新上下文中加载计划逐任务执行。避免超长对话中同时完成设计和全部实施。
- **任务文件管理**：`docs/tasks/` 下，完成后移入同级 `done/`。

## 技术栈

- Go 1.25.6 + `github.com/bsv-blockchain/go-sdk` v1.2.18
- `github.com/stretchr/testify` v1.11.1, `golang.org/x/crypto` v0.48.0
- Content-addressed file store with hash-sharded directories (~/.bitfs/storage/)

## 许可证

- 源代码: OpenBSV License
- websites/, docs/whitepaper/, docs/design/: 单独许可

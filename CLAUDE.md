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
├── design/            ← 设计文档 (md 源文件 + HTML→PDF)
│   ├── 0-OverallDesign.zh.md
│   ├── bitfs/         ← BitFS 四层设计文档
│   ├── metanet/       ← Metanet 四层设计文档
│   ├── diagrams/      ← Mermaid 图表源文件 + SVG
│   └── pdf/           ← 生成的 PDF + HTML 模板
├── whitepaper/        ← 白皮书 (md 大纲 → LaTeX → PDF)
├── website/           ← 官网 (bitfs.org + metanet.org)
├── slides/            ← 演示文稿
├── references/        ← 研究论文 (6 篇 PDF)
├── vi/                ← 视觉识别系统
├── bitfs/             ← BitFS Go 实现 (CLI + daemon)
├── metanet/           ← Metanet Go 实现 (CDN 节点)
├── libbitfs-go/       ← 共享核心库 Go (独立 repo, module: github.com/tongxiaofeng/libbitfs-go)
├── libbitfs-ts/       ← 共享核心库 TypeScript (待开发，目标: 浏览器 + Node.js)
├── den/               ← BitFS 区块链浏览器 (Go + htmx, regtest/testnet 调试工具)
├── git-remote-bitfs/  ← Git remote helper (独立 repo, bitfs:// 协议)
├── bitfs-app/         ← BitFS 桌面/移动客户端 (Flutter, 独立 repo)
├── bitfs-extension/   ← BitFS 浏览器扩展 (TypeScript, 独立 repo)
└── tools/             ← 构建工具 (Mermaid 图表渲染等)
```

## 文档生成工作流

本项目所有输出物均由 Markdown 大纲驱动，Markdown 是唯一的源文件：

| 目录 | 源文件 | 生成管线 | 输出 |
|------|--------|----------|------|
| design/ | `.zh.md` | pandoc + HTML 模板 + weasyprint | 内部设计 PDF |
| whitepaper/ | `*-Outline.md` | 大纲 → `.tex` → tectonic (XeTeX) | 学术论文 PDF |
| website/ | `Website-Content-Outline.md` | 大纲 → `index.html` + `index.zh.html` | 单页官网 |
| slides/ | `Slides-Outline.md` | 大纲 → `*-presentation.html` | HTML 幻灯片 |

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

## 许可证

- 源代码: OpenBSV License
- website/, whitepaper/, design/: 单独许可

## 技术栈
- Go 1.25.6 + `github.com/bsv-blockchain/go-sdk` v1.2.18 (唯一 BSV 依赖)
- `github.com/stretchr/testify` v1.11.1, `golang.org/x/crypto` v0.47.0
- Content-addressed file store with hash-sharded directories (~/.bitfs/storage/)

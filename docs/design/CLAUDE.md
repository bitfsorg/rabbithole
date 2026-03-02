# 设计文档 (Design Documents)

本目录包含 BitFS 和 Metanet 两个产品的完整设计文档，供内部使用。

## 目录结构

```
design/
├── CLAUDE.md                          ← 本文件
├── OverallDesign.zh.md              ← 整体设计: 两产品生态、界面划分
├── bitfs/                             ← BitFS 文件系统协议设计
│   ├── 1-ConceptDesign.zh.md            概念设计 (愿景, 架构, 设计决策)
│   ├── 2-SystemDesign.zh.md             系统设计 (模块, 接口, 数据流)
│   ├── 3-DetailedDesign.zh.md           详细设计 (算法, 数据结构, 协议)
│   ├── 4-TestDesign.zh.md              测试设计 (~980 测试用例)
│   └── 5-TransactionSpec.zh.md         交易规范 (权威参考, 13 项设计决策)
├── metanet/                           ← Metanet CDN 网络设计
│   ├── 1-ConceptDesign.zh.md            概念设计 (CDN 模型, 经济设计)
│   ├── 2-SystemDesign.zh.md             系统设计 (节点, 合约, 支付通道)
│   ├── 3-DetailedDesign.zh.md           详细设计 (共识, 挖矿, 结算)
│   └── 4-TestDesign.zh.md              测试设计 (~20 测试用例)
└── pdf/                               ← PDF 输出 + HTML 模板
    ├── template.html                    pandoc HTML 模板 (中文排版)
    ├── style.css                        PDF 样式表
    └── *.pdf                            生成的 PDF 文件 (9 个)
```

## 文档工作流

**Markdown 是唯一的源文件。** `.zh.md` 文件供人类直接编辑，修改后重新生成 PDF。

### PDF 生成

设计文档使用 pandoc + HTML 模板 + weasyprint 生成 PDF：

```
.zh.md → pandoc (--template pdf/template.html) → HTML → weasyprint → .pdf
```

这与 `whitepaper/` 目录下的白皮书不同——白皮书按学术论文标准用 LaTeX (tectonic) 排版。

## 文档体系

每个产品的设计文档遵循四层结构：

1. **概念设计** — 愿景、定位、核心理念、与竞品对比
2. **系统设计** — 模块划分、接口定义、数据流、CLI 命令
3. **详细设计** — 算法细节、数据结构、协议规范、交易格式
4. **测试设计** — 按模块组织的测试用例，覆盖正常/边界/异常场景

`OverallDesign.zh.md` 是顶层文件，定义两产品的职责边界和共享设计原则。

## Markdown 编写规范

- 使用 Markdown pipe 表格（`| col |` 格式）
- HTML `<table>` 可用于复杂布局（架构图、流程图等），HTML 模板支持完整渲染
- 使用 fenced code blocks（带语言标注）
- 使用 `---` 作为分隔线
- 每个文档开头的 blockquote 包含文档体系导航链接

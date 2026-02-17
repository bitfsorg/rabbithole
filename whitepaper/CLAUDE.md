# 白皮书 (Whitepapers)

学术论文格式的白皮书，使用 LaTeX 排版。

## 文件结构

```
whitepaper/
├── BitFS-Whitepaper-Outline.md      ← BitFS 白皮书大纲 (中文，源文件)
├── BitFS-Whitepaper.en.tex          ← 英文 LaTeX
├── BitFS-Whitepaper.en.pdf          ← 英文 PDF
├── BitFS-Whitepaper.zh.pdf          ← 中文 PDF (从 design/ 的中文设计文档生成)
├── Metanet-Whitepaper-Outline.md    ← Metanet 白皮书大纲 (中文，源文件)
├── Metanet-Whitepaper.en.tex        ← 英文 LaTeX
├── Metanet-Whitepaper.en.pdf        ← 英文 PDF
├── Metanet-Whitepaper.zh.tex        ← 中文 LaTeX
└── Metanet-Whitepaper.zh.pdf        ← 中文 PDF
```

## 工作流

**大纲 (Outline.md) 是唯一的源文件。**

```
*-Outline.md (中文大纲，人类编辑)
  → 生成 .en.tex (英文 LaTeX)
  → 生成 .zh.tex (中文 LaTeX)
  → tectonic 编译 → .pdf
```

修改内容时编辑 Outline.md，然后重新生成 .tex 和 .pdf。

## LaTeX 编译

使用 `tectonic`（XeTeX 引擎）编译，支持中文字体：

```bash
tectonic BitFS-Whitepaper.en.tex
tectonic Metanet-Whitepaper.zh.tex
```

## 中文字体配置

```latex
\setCJKmainfont{Songti SC}[BoldFont={Heiti SC}, ItalicFont={STKaiti}]
\setCJKsansfont{Heiti SC}
\setCJKmonofont{Heiti SC}    % 不要用 Menlo，Menlo 没有 CJK 字形
\setmainfont{Times New Roman}
\setmonofont{Menlo}[Scale=0.85]
```

**注意**：`CJKmonofont` 必须用 `Heiti SC` 而非 `Menlo`，否则代码块中的中文字符会缺失。

## 论文风格

简洁学术风格：标题居中在首页顶部，摘要用斜体缩进块，编号章节，无花哨封面页。与设计文档的 HTML 排版风格不同。

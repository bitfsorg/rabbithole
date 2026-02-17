# 官网 (Websites)

BitFS 和 Metanet 两个产品的单页官网。

## 文件结构

```
website/
├── bitfs.org/
│   ├── Website-Content-Outline.md   ← 内容大纲 (源文件)
│   ├── index.html                   ← 英文官网
│   └── index.zh.html                ← 中文官网
└── metanet.org/
    ├── Website-Content-Outline.md   ← 内容大纲 (源文件)
    ├── index.html                   ← 英文官网
    └── index.zh.html                ← 中文官网
```

## 工作流

**Website-Content-Outline.md 是唯一的源文件。**

大纲中定义了：配色方案 (CSS 变量)、字体、每个 section 的内容要点、CTA 文案。
修改大纲后重新生成 `index.html` 和 `index.zh.html`。

## 设计风格

### bitfs.org
- Dark Botanical 风格：深炭背景 + 铜金强调色
- CSS 变量以 `--b-` 前缀命名

### metanet.org
- 独立配色方案，定义在对应的 Outline.md 中

## 技术实现

- 纯 HTML + CSS + 少量 JS（无框架）
- 单页响应式设计
- 中英文各一个独立 HTML 文件

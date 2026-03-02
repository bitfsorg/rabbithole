# Den Explorer v2 — 功能补全设计

日期: 2026-03-02
状态: 已批准
范围: 在现有 den-explorer v1 架构上补全功能，保持开发工具定位

## 概述

Den Explorer v1 (~1,277 行 Go + 9 HTML 模板) 已完成标准区块链浏览功能 + 基础 BitFS 协议解码。v2 在此基础上补全三个方向：

1. **DAG 可视化** — D3.js 客户端渲染 Metanet DAG 树形图
2. **协议解码深化** — TLV 全量解析、x402 支付协议、hex dump、交易链追踪
3. **UI 对齐 BitFS VI** — Dark Botanical 风格统一

架构方案：**渐进增强**。保持 Go+htmx 服务端渲染，仅 DAG 页引入 D3.js。单二进制 embed 部署不变。

## 1. UI 对齐 BitFS VI

### 色板迁移

| 现有 | BitFS VI | 变量名 |
|------|----------|--------|
| `#1a1a2e` (bg) | `#110f0d` | `--b-bg` |
| `#16213e` (card) | `#1a1714` | `--b-bg2` |
| `#0f3460` (border) | `#2e2924` | `--b-border` |
| `#e0e0e0` (text) | `#d4cdc4` | `--b-text` |
| `#e94560` (accent) | `#c9956b` | `--b-gold` |
| `#64b5f6` (link) | `#dbb08a` | `--b-gold-light` |
| `#999` (label) | `#8a8078` | `--b-text-dim` |

### 字体

- 正文/代码: JetBrains Mono (CDN), SF Mono / Menlo fallback
- 标题: Cormorant Garamond (CDN)
- UI 文本: Inter (CDN)

### 实现

- `base.html` inline `<style>` 抽出为 `static/style.css`（embed 进二进制）
- CSS 变量统一管理色板
- tag 颜色保留语义但调整色值与 Botanical 风格协调

## 2. DAG 可视化

### 新路由

```
GET /dag/{txid}       DAG 可视化页面（D3.js 渲染）
GET /api/dag/{txid}   JSON API（D3.js 数据源）
```

### JSON API 数据结构

```json
{
  "root": "txid_hex",
  "nodes": [
    {
      "txid": "...",
      "pnode": "...",
      "name": "docs",
      "type": "DIR",
      "access": "FREE",
      "parent": "parent_txid_or_null",
      "children": ["child_txid_1", "child_txid_2"]
    }
  ]
}
```

### DAG 构建

Go 端从 root txid 出发，递归解析 ChildEntry 中的 PubKey，通过 RPC 查找对应交易（遍历最近区块），构建完整 DAG。regtest 数据量小，递归深度有限。

### D3.js 渲染

- **布局**: `d3.tree()` 层次树，根节点在上，子节点向下展开
- **节点样式**: 圆角矩形，icon（DIR=文件夹、FILE=文件、LINK=链接）+ 名称 + 类型 tag
- **颜色编码**: Access 类型（PRIVATE=红铜、FREE=绿、PAID=金）
- **交互**: 点击→导航 `/metanet/:txid`、hover tooltip、折叠/展开子树、滚轮缩放+拖拽

### 嵌入方式

- D3.js v7 通过 CDN 引入（仅 dag.html）
- DAG JS 代码放在 `static/dag.js`，embed 进二进制

### Metanet 详情页增强

`/metanet/:txid` Children 表格上方加 mini DAG 预览（200px），显示当前节点+直接子节点。"View Full DAG" 链接跳转 `/dag/:root_txid`。

### 限制

- DAG 构建依赖 RPC 遍历区块查找子节点交易
- 最大递归深度 10 层
- 一次性请求，无 WebSocket

## 3. 协议解码深化

### 3.1 TLV 完整解析

v1 覆盖 0x01-0x1A（26 个 tag），遗漏 25 个。补全如下：

| Tag | 名称 | 解读方式 |
|-----|------|---------|
| 0x1B | EncPayload | salt(16)+nonce(12)+ciphertext+tag(16) 各段长度 |
| 0x1E | Metadata | 递归解析 sub-TLV，嵌套表格 |
| 0x1F | VersionLog | P_node hex (33 bytes) |
| 0x20 | TreeRootPNode | hex 33 bytes |
| 0x21 | TreeRootTxID | hex 32 bytes + `/metanet/` 链接 |
| 0x22 | ParentAnchorTxID | hex 32 bytes + `/tx/` 链接 |
| 0x23 | Author | UTF-8 string |
| 0x24 | CommitMessage | UTF-8 string |
| 0x25 | GitCommitSHA | hex 20 bytes |
| 0x26 | FileMode | 八进制文件权限 |
| 0x27 | ShareList | hex 33 bytes P_node |
| 0x28 | ChunkIndex | uint32 |
| 0x29 | TotalChunks | uint32 |
| 0x2A | RecombinationHash | hex 32 bytes |
| 0x2B | RabinSignature | hex + 长度 |
| 0x2C | RabinPubKey | hex + 长度 |
| 0x2D | RegistryTxID | hex 32 bytes + `/tx/` 链接 |
| 0x2E | RegistryVout | uint32 |
| 0x2F | ISOConfig | TotalShares(8)+PricePerShare(8)+CreatorAddr(20)+Status(1) |
| 0x30 | ACLRef | hex bytes |

TxID 类型字段自动生成超链接。

### 3.2 x402 支付协议展示

**新路由**: `GET /x402/{txid}`

**检测逻辑**: 扫描交易输出 ScriptPubKey，匹配 HTLC 模式：
```
[<invoice_id_16> OP_DROP]
OP_IF OP_SHA256 <capsule_hash_32> OP_EQUALVERIFY
  OP_DUP OP_HASH160 <seller_addr_20> OP_EQUALVERIFY OP_CHECKSIG
OP_ELSE
  OP_2 <buyer_33> <seller_33> OP_2 OP_CHECKMULTISIG
OP_ENDIF
```

**展示内容**:
- CapsuleHash (32 bytes hex)
- SellerAddr (20 bytes → Base58 地址)
- BuyerPubKey / SellerPubKey (33 bytes hex)
- Amount (satoshis)
- InvoiceID (16 bytes, 若存在)
- Timeout 状态（当前区块高度 vs 锁定高度）
- HTLC 状态：已赎回 / 已退款 / 等待中

**模板**: `x402.html`，从 `/metanet/:txid` 和 `/tx/:txid` 添加链接入口。

### 3.3 原始数据查看 (Hex Dump)

**新路由**: `GET /raw/{txid}` + `GET /raw/{txid}?format=hex`

Hex dump 格式：
```
Offset   00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F   ASCII
000000   6d 65 74 61 02 21 03 ab  cd ef 01 23 45 67 89 ab   meta.!.....#Eg..
```

Go 端返回 OP_RETURN push data 列表，模板用 `<pre>` 渲染。`?format=hex` 返回 `text/plain` 原始交易 hex。

### 3.4 交易链追踪

**新路由**: `GET /history/{txid}`

**逻辑**: Metanet SelfUpdate 花费前一版本 UTXO。从当前 txid 出发，检查 vin[0] prevout，若为相同 P_node 的 Metanet 交易则为前一版本，递归至 CreateRoot/CreateChild。

**展示**: 纵向时间线，最新到最早：
```
v3 (current)  txid: abc...  Op: UPDATE   2024-01-03
    ↑
v2            txid: def...  Op: UPDATE   2024-01-02
    ↑
v1 (create)   txid: 789...  Op: CREATE   2024-01-01
```

每版本显示 Op、时间戳、TLV 变更摘要，可点击跳转 `/metanet/:txid`。

## 4. 文件变更总览

### 新增文件

```
static/style.css       BitFS VI CSS 变量 + 全局样式
static/dag.js          D3.js DAG 渲染逻辑
templates/dag.html     DAG 可视化页面
templates/x402.html    HTLC 支付分析页面
templates/raw.html     Hex dump 页面
templates/history.html 版本历史时间线页面
dag.go                 DAG 构建 + JSON API handler
x402.go                HTLC 脚本解析
raw.go                 Hex dump 格式化
history.go             交易链追踪
```

### 修改文件

| 文件 | 改动 |
|------|------|
| `main.go` | 注册静态文件路由 |
| `handlers.go` | 新增 5 个 handler（dag page、dag API、x402、raw、history） |
| `decode.go` | `tlvTagNames` 扩展到 49 个 tag，`interpretTLVValue` 全量解读 |
| `templates.go` | 注册 4 个新模板，static embed |
| `templates/base.html` | inline CSS → `style.css` 引用，D3.js CDN |
| `templates/metanet.html` | mini DAG 预览 + 新功能链接 |
| `templates/tx.html` | View Raw / x402 链接 |

### 新增路由

```
GET /dag/{txid}            DAG 可视化页面
GET /api/dag/{txid}        DAG JSON API
GET /x402/{txid}           x402 HTLC 分析
GET /raw/{txid}            Hex dump
GET /raw/{txid}?format=hex 原始交易 hex (text/plain)
GET /history/{txid}        版本历史时间线
GET /static/*              静态资源
```

### 外部依赖

- D3.js v7 (CDN, 仅 dag.html)
- htmx (已有, CDN)
- 无新 Go 依赖

## 5. 不做的事情

- 不加数据库索引
- 不加 WebSocket/SSE 实时更新
- 不加 mainnet 支持
- 不重写为 SPA
- 保持单二进制 embed 部署

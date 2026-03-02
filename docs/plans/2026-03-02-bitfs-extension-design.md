# BitFS Chrome Extension 设计文档

**日期**: 2026-03-02
**状态**: Approved
**目标**: 完成 BitFS Chrome Extension — 独立 MetaMask 模型钱包

## 概述

BitFS Chrome Extension 是一个 MV3 浏览器扩展，采用 MetaMask 模型：所有加密运算在本地执行，通过公共区块链 API 直连 BSV 网络，不依赖本地 daemon。

**核心定位**: Extension = 独立钱包 + 文件浏览器，区块链 = 后端，API provider 可配置。

## 架构

### 三层结构

```
┌─────────────────────────────────────────────┐
│              Chrome Extension               │
│                                             │
│  ┌─────────┐  ┌───────────┐  ┌───────────┐ │
│  │  Popup  │  │ Background│  │ Content   │ │
│  │ React   │←→│ Service   │←→│ Script    │ │
│  │ UI      │  │ Worker    │  │ (inject)  │ │
│  └────┬────┘  └─────┬─────┘  └───────────┘ │
│       │             │                       │
│       └──────┬──────┘                       │
│              │                              │
│  ┌───────────▼────────────────────────────┐ │
│  │         @bitfs/libbitfs               │ │
│  │  method42 | wallet | metanet | tx     │ │
│  │  spv | storage | network | x402      │ │
│  │  paymail | revshare | config         │ │
│  └───────────┬────────────────────────────┘ │
└──────────────┼──────────────────────────────┘
               │ HTTP (fetch)
    ┌──────────▼──────────┐
    │  Blockchain API     │
    │  (WhatsOnChain etc) │
    └─────────────────────┘
```

- **Popup** — React 19 UI（钱包、文件浏览、设置）
- **Service Worker** — 密钥管理、交易签名、DAG 状态管理
- **Content Script** — `bitfs://` 链接检测、402 拦截、`window.bitfs` 注入

### 依赖

| 依赖 | 用途 | 来源 |
|------|------|------|
| `@bitfs/libbitfs` | 加密核心 (11 模块) | `file:../libbitfs-ts` 本地链接 |
| `react` + `react-dom` | Popup UI | npm |
| `react-router-dom` | 路由 | npm |
| `tailwindcss` | 样式 | npm |

删除 `src/bitfs-core/` 中的重复实现，全部改用 `@bitfs/libbitfs`。
删除 `@noble/secp256k1`, `@noble/hashes`, `@scure/bip32`, `@scure/bip39`（已被 libbitfs-ts 内部依赖）。

### 为什么不依赖 daemon

| 场景 | 独立能力 | 说明 |
|------|----------|------|
| 管理自己的文件 | ✅ | 种子推导密钥 → 扫描链上 Metanet 交易 → 重建 DAG |
| 解密自己的文件 | ✅ | Method42 + 本地密钥 |
| 上传文件 | ✅ | 构建 Metanet 交易 → 公共 API 广播 |
| 浏览别人的文件 | ✅ | 输入根地址 → 扫描重建对方 DAG |
| 下载免费文件 | ✅ | 从链上读 DataTx |
| 付费下载 | ✅ | x402 支付发给内容提供者的 daemon（不是自己的） |

Daemon 的定位是"让别人能通过 HTTP 访问你的文件"（内容服务器），不是 Extension 的后端。

## Service Worker

### 消息协议

Popup 和 Content Script 通过 `chrome.runtime.sendMessage` 与 Service Worker 通信。

| Action | 发起方 | 说明 |
|--------|--------|------|
| `WALLET_CREATE` | Popup | 生成助记词 → 创建 HD 钱包 |
| `WALLET_IMPORT` | Popup | 导入助记词 → 恢复钱包 |
| `WALLET_UNLOCK` | Popup | 密码验证 → 解锁会话 |
| `WALLET_LOCK` | Popup | 锁定钱包 |
| `GET_STATUS` | Popup/CS | 返回钱包状态 (locked/unlocked/none) |
| `SCAN_DAG` | Popup | 扫描区块链重建 DAG → 缓存 IndexedDB |
| `LIST_FILES` | Popup | 从缓存读目录内容 |
| `READ_FILE` | Popup/CS | 读取文件内容（链上 DataTx → 解密） |
| `SIGN_TX` | Popup | 签名交易（用户确认后） |
| `BROADCAST_TX` | Popup | 广播交易到区块链 |
| `PAY_INVOICE` | CS | x402 支付流程 |

### 密钥管理

- 助记词 → Argon2id 加密 → `chrome.storage.local` 持久化
- 解锁后 HD root key 存 Service Worker 内存（不持久化）
- 自动锁定（可配置超时，默认 15 分钟）
- `chrome.storage.session`（MV3）存会话状态

### DAG 同步

1. 从种子推导根地址
2. 调用 API 获取该地址的所有交易
3. 解析 Metanet OP_RETURN → 重建 Node 树
4. 递归处理子节点地址的交易
5. 缓存到 IndexedDB（`dag_nodes` object store）
6. 增量更新：记录最后扫描的区块高度，后续只查新交易

### 文件读取

1. 从 DAG 缓存找到文件的 Node
2. 获取 Node 指向的 DataTx（txid）
3. 从 API 获取原始交易
4. 提取 OP_RETURN 数据
5. 如果加密：用 Method42 + 本地密钥解密
6. 返回明文内容

## Popup UI

### 页面结构

React Router (MemoryRouter), 360×500px popup:

```
/ (Home)           — 钱包状态、余额、快捷操作
/create            — 创建钱包（助记词生成 → 确认 → 设密码）
/import            — 导入钱包（输入助记词 → 设密码）
/unlock            — 解锁（输入密码）
/files             — 文件浏览器（目录树、文件列表）
/files/:path       — 文件详情（内容预览、元数据、解密）
/send              — 发送交易（上传文件/创建目录）
/tx-confirm        — 交易确认弹窗
/settings          — 设置（API provider、网络、自动锁定时间）
```

### 状态管理

React Context:
- `WalletContext` — 钱包状态 (none/locked/unlocked)、地址、余额
- `FilesContext` — 当前目录、文件列表、DAG 同步状态
- `SettingsContext` — API provider URL、网络选择、自动锁定时间

### 样式

Tailwind CSS，暗色主题（与 BitFS VI 一致）:
- 背景: 深炭灰 `#1a1a1a`
- 强调: 铜金 `#c9956b`
- 文字: `#e5e5e5` (主) / `#999` (次)
- 字体: Inter (UI) + JetBrains Mono (地址/哈希)

## Content Script

### 功能

1. **`bitfs://` 链接检测**: 扫描页面 DOM，将 `bitfs://address/path` 链接转换为可点击元素，点击后触发 Extension 处理
2. **HTTP 402 拦截**: 检测 402 响应 → 解析 X-Price/X-Invoice-Id → 弹出支付确认 → 完成支付
3. **`window.bitfs` API 注入**: DApp 接口（类似 `window.ethereum`），支持 `requestAccounts()`, `signTransaction()`, `decrypt()` 等

### 安全

Content Script 不持有密钥。所有敏感操作通过 `chrome.runtime.sendMessage` 转发给 Service Worker。用户敏感操作需要 Popup 弹窗确认。

## API Provider 抽象

```typescript
interface BlockchainProvider {
  getUTXOs(address: string): Promise<UTXO[]>
  getTransaction(txid: string): Promise<RawTx>
  getAddressHistory(address: string, since?: number): Promise<TxRef[]>
  broadcast(rawTx: string): Promise<string>
}
```

默认实现: `WhatsOnChainProvider` (BSV mainnet)。Settings 可配置 URL + provider 类型。

与 libbitfs-ts `network/` 模块的 `BlockchainService` 接口对齐。

## MV3 Manifest

```json
{
  "manifest_version": 3,
  "name": "BitFS",
  "version": "0.1.0",
  "permissions": ["storage", "activeTab"],
  "background": {
    "service_worker": "service-worker.js",
    "type": "module"
  },
  "action": {
    "default_popup": "popup.html"
  },
  "content_scripts": [{
    "matches": ["<all_urls>"],
    "js": ["content-script.js"],
    "run_at": "document_idle"
  }]
}
```

## 目录结构

```
bitfs-extension/
├── src/
│   ├── popup/
│   │   ├── index.html
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── contexts/
│   │   │   ├── WalletContext.tsx
│   │   │   ├── FilesContext.tsx
│   │   │   └── SettingsContext.tsx
│   │   ├── pages/
│   │   │   ├── Home.tsx
│   │   │   ├── CreateWallet.tsx
│   │   │   ├── ImportWallet.tsx
│   │   │   ├── Unlock.tsx
│   │   │   ├── Files.tsx
│   │   │   ├── FileDetail.tsx
│   │   │   ├── Send.tsx
│   │   │   ├── TxConfirm.tsx
│   │   │   └── Settings.tsx
│   │   └── components/
│   │       ├── Header.tsx
│   │       ├── FileList.tsx
│   │       ├── FileIcon.tsx
│   │       └── Button.tsx
│   ├── background/
│   │   ├── service-worker.ts
│   │   ├── wallet-manager.ts
│   │   ├── dag-manager.ts
│   │   ├── file-reader.ts
│   │   └── tx-builder.ts
│   ├── content-script/
│   │   ├── index.ts
│   │   ├── link-detector.ts
│   │   ├── payment-handler.ts
│   │   └── injected-api.ts
│   ├── lib/
│   │   ├── storage.ts
│   │   ├── messages.ts
│   │   └── provider.ts
│   └── vite-env.d.ts
├── test/
│   ├── wallet-manager.test.ts
│   ├── dag-manager.test.ts
│   ├── file-reader.test.ts
│   └── provider.test.ts
├── public/
│   └── manifest.json
├── package.json
├── tsconfig.json
├── vite.config.ts
├── tailwind.config.ts
└── postcss.config.ts
```

## 技术要点

- `src/bitfs-core/` 删除，改用 `@bitfs/libbitfs`
- Chrome Extension MV3 Service Worker 有 5 分钟空闲超时，需要通过 `chrome.alarms` 维持活跃（如果有定时任务）
- IndexedDB 在 Service Worker 中可用（用于 DAG 缓存）
- `chrome.storage.session` 仅 MV3，适合会话级数据
- Tailwind CSS 需要 PostCSS 插件，Vite 内置支持

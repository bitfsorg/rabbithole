# BitFS Mobile App 设计文档

> 日期: 2026-03-02
> 状态: ✅ 完成 (20/20 tasks done)

## 实现进度

| 阶段 | 任务 | 状态 |
|------|------|------|
| Phase 1-3 (Foundation+Services+State) | Tasks 1-9 | ✅ 完成 |
| Phase 4 (UI Shell) | Tasks 10-11 | ✅ 完成 |
| Phase 5 (Main Screens) | Tasks 12-17 | ✅ 完成 |
| Phase 6 (Integration) | Task 18: Service wiring | ✅ 完成 — ServiceProvider + useFileOps hook + MutationBatch broadcast for put/mkdir/rm + buy flow wired to PaymentService |
| Phase 6 (Integration) | Task 19: Auto-lock | ✅ 完成 — AppState 监听 + 阈值锁定 |
| Phase 6 (Integration) | Task 20: Polish | ✅ 完成 — OfflineBanner + classifyError for all service calls + loading states |

**关键变更** (Task 18+20):
- 新增 `src/adapters/WoCProvider.ts` — WhatsOnChain REST API 实现 BlockchainService (mainnet/testnet)
- 新增 `src/providers/ServiceProvider.tsx` — React Context 持有服务单例，网络切换时自动重建
- 新增 `src/hooks/useFileOps.ts` — 协调 FileService + TxService + vaultStore 的文件操作 hook
- 修复 `TxService.buildAndBroadcast` — 添加 `sign()` 调用（原来遗漏签名步骤）
- 所有 UI 组件 (CreateMenu/FileActionSheet/file/wallet/buy) 通过 useFileOps 接入服务层
- _layout.tsx 新增 ServiceProvider 包装 + OfflineBanner + AutoLockGuard
- 修复 libbitfs-ts 链接路径 (worktree `../../../` → main `../`)
- Buy flow 接入 PaymentService（requestInvoice + fundHTLC），但完整 HTLC 需要卖方 daemon 端点

## 概述

BitFS 移动客户端（iOS + Android），文件管理器优先定位。使用 React Native + libbitfs-ts 架构，完全本地运行，无需连接 daemon。

## 决策记录

| 维度 | 决策 | 理由 |
|------|------|------|
| 框架 | React Native (Expo SDK 55) | libbitfs-ts 可直接 import，无需 Go FFI 桥接 |
| 定位 | 文件管理器优先，钱包辅助 | 用户核心需求是管理加密文件 |
| 数据架构 | 完全本地 (libbitfs-ts) | self-contained，无需 daemon 进程 |
| UI 库 | React Native Paper 5.x (Material 3) | 开箱即用组件库，Expo 官方推荐 |
| 安全存储 | 系统 Keychain/Keystore | 硬件级安全，支持生物识别 |
| V1 范围 | 完整文件管理 | 包含加密模式切换、sell、x402 购买 |

## 技术栈

| 层面 | 选择 | 说明 |
|------|------|------|
| Runtime | Expo SDK 55 | 构建/开发/OTA |
| UI | React Native Paper 5.x | Material 3 组件库 |
| Navigation | Expo Router 4.x | File-based routing |
| State | Zustand 5.x | 轻量状态管理 |
| Core | @bitfs/libbitfs 0.1.0 | 传递依赖: @bsv/sdk, @noble/hashes |
| Secure Store | expo-secure-store | iOS Keychain / Android Keystore |
| File System | expo-file-system | App sandbox 存储 |
| Biometric | expo-local-authentication | FaceID/TouchID/Fingerprint |

## 项目结构

```
bitfs-app/
├── app/                            # Expo Router (file-based routing)
│   ├── _layout.tsx                 # Root layout (Paper theme + providers)
│   ├── (tabs)/                     # Bottom tab navigator
│   │   ├── _layout.tsx             # Tab config
│   │   ├── index.tsx               # Files tab (home)
│   │   ├── wallet.tsx              # Wallet tab
│   │   └── settings.tsx            # Settings tab
│   ├── file/[path].tsx             # File detail / preview
│   ├── wallet/
│   │   ├── create.tsx              # Create wallet (mnemonic)
│   │   ├── import.tsx              # Import wallet
│   │   └── backup.tsx              # Backup mnemonic
│   └── buy/[txid].tsx              # x402 purchase flow
├── src/
│   ├── services/                   # 业务逻辑层 (薄封装 libbitfs-ts)
│   │   ├── WalletService.ts        # 钱包管理 (create/import/lock/unlock)
│   │   ├── VaultService.ts         # Vault CRUD + 文件树状态
│   │   ├── FileService.ts          # put/mkdir/rm/mv/cp/link/encrypt/sell
│   │   ├── TxService.ts            # MutationBatch 构建 + 广播
│   │   ├── PaymentService.ts       # x402 invoice/HTLC/购买
│   │   └── StorageService.ts       # 本地存储适配器 (expo-file-system)
│   ├── stores/                     # Zustand stores
│   │   ├── walletStore.ts          # 钱包状态
│   │   ├── vaultStore.ts           # Vault + 文件树
│   │   └── settingsStore.ts        # 配置 + 网络选择
│   ├── components/                 # 可复用 UI 组件
│   │   ├── FileListItem.tsx        # 文件/目录行
│   │   ├── DirectoryBreadcrumb.tsx  # 路径导航
│   │   ├── FileActionSheet.tsx     # 长按操作菜单
│   │   ├── AccessBadge.tsx         # Private/Free/Paid 标识
│   │   └── ...
│   ├── hooks/                      # React hooks
│   │   ├── useWallet.ts
│   │   ├── useVault.ts
│   │   └── useFileOps.ts
│   └── adapters/                   # 平台适配
│       ├── SecureStorage.ts        # expo-secure-store 封装
│       ├── FileSystemStore.ts      # 实现 libbitfs-ts Store 接口
│       └── BiometricAuth.ts        # expo-local-authentication
├── package.json
├── app.json                        # Expo config
└── tsconfig.json
```

## Service Layer 设计

### StorageService — 平台存储适配

实现 libbitfs-ts 的 `Store` 接口，底层用 expo-file-system。

```typescript
class FileSystemStore implements Store {
  // 存储路径: ${documentDirectory}/bitfs-storage/${ab}/${abcdef...}
  // 与 Go/Node 版本相同的 hash-sharded 目录结构
  put(keyHash, ciphertext): Promise<void>
  get(keyHash): Promise<Uint8Array>
  has(keyHash): Promise<boolean>
  delete(keyHash): Promise<void>
  size(keyHash): Promise<number>
  list(): Promise<Uint8Array[]>
}
```

### WalletService — 钱包生命周期

```typescript
class WalletService {
  create(password: string): Promise<{ mnemonic: string }>
  import(mnemonic: string, password: string): Promise<void>
  unlock(password: string): Promise<Wallet>
  lock(): void
  unlockWithBiometric(): Promise<Wallet>
}
```

存储分层:
- `expo-secure-store` key `bitfs.encrypted_seed` → 加密种子（Argon2id + AES-256-GCM）
- `expo-secure-store` key `bitfs.bio_password` → 密码（仅开启生物识别时，biometric gate 保护）

### VaultService — Vault + 文件树

```typescript
class VaultService {
  createVault(name: string): Promise<Vault>
  listVaults(): Vault[]
  switchVault(name: string): void
  loadTree(vault: Vault): Promise<Node>
}
```

WalletState 持久化在 expo-file-system（JSON 序列化）。文件树从链上同步并缓存本地。

### FileService — 文件操作

```typescript
class FileService {
  put(parentNode, name, data, access): Promise<Node>
  mkdir(parentNode, name): Promise<Node>
  rm(node): Promise<void>
  mv(node, newParent, newName?): Promise<void>
  cp(node, destParent, newName?): Promise<Node>
  link(targetNode, parentNode, name, type): Promise<Node>
  encrypt(node, access: AccessLevel): Promise<void>
  sell(node, pricePerKB: bigint): Promise<void>
  read(node): Promise<Uint8Array>
  shareLink(node): string
}
```

### PaymentService — x402 购买

```typescript
class PaymentService {
  buy(sellerNode: Node): Promise<Uint8Array>  // 返回解密后的内容
  listSales(): Promise<Sale[]>
}
```

### TxService — 交易构建

```typescript
class TxService {
  buildAndBroadcast(ops: BatchNodeOp[]): Promise<BatchResult>
  getBalance(): Promise<bigint>
  refreshUTXOs(): Promise<UTXO[]>
}
```

**设计原则**: Service 无状态，状态在 Zustand store 里。Service 直接调用 libbitfs-ts API，不重新实现逻辑。

## 屏幕与导航

### 底部 Tab 结构（3 个 Tab）

| Tab | 屏幕 | 功能 |
|-----|------|------|
| Files | `(tabs)/index` | 文件浏览器，目录树导航，长按操作 |
| Wallet | `(tabs)/wallet` | 余额、地址、交易历史 |
| Settings | `(tabs)/settings` | 网络切换、生物识别、备份、缓存 |

### Files Tab（主屏）

- 顶部: vault 切换器 + 新建菜单（上传文件/新建目录/新建链接）
- 面包屑导航条
- 文件列表: 每行显示名称、大小、AccessBadge (Private/Free/Paid)
- 点击目录 → 进入子目录
- 点击文件 → `file/[path]` 详情页
- 长按 → ActionSheet（移动/复制/重命名/删除/加密/出售/分享链接）

### Wallet Tab

- 余额卡片（BSV 金额 + 地址，点击复制）
- Receive / Send 按钮
- 交易历史列表

### Settings Tab

- 网络选择（MainNet/TestNet/RegTest）
- 生物识别开关
- 自动锁定时间
- 备份助记词 / 修改密码
- 存储用量 / 清除缓存

### 非 Tab 页面

| 路由 | 用途 |
|------|------|
| `file/[path]` | 文件详情：元数据 + 操作按钮 |
| `wallet/create` | 创建钱包：生成助记词 → 确认 → 设密码 |
| `wallet/import` | 导入钱包：输入助记词 → 设密码 |
| `wallet/backup` | 显示助记词（需验证） |
| `buy/[txid]` | x402 购买流程 |

### 首次启动流程

```
App 启动 → 有钱包？
  ├─ No → Welcome 页 → [创建钱包] / [导入钱包]
  └─ Yes → 已解锁？
      ├─ No → 解锁页（密码 or 生物识别）
      └─ Yes → Files Tab (主屏)
```

## 数据流

### Zustand Stores

```typescript
// walletStore
{ locked, wallet, address, balance, utxos }

// vaultStore
{ currentVault, vaults, rootNode, currentPath, children, nodeCache, syncing }

// settingsStore
{ network, biometricEnabled, autoLockMinutes, rpcConfig }
```

### 文件上传 (put) 流程

1. 用户选择文件 (expo-document-picker)
2. method42.encrypt(data, nodePriv, nodePub, access) → { ciphertext, keyHash }
3. StorageService.put(keyHash, ciphertext) → 写入 app sandbox
4. 构造 Node: type=File, fileSize, mimeType, keyHash, access
5. serializePayload(node) → TLV bytes
6. TxService.buildAndBroadcast([createChild op]) → MutationBatch → broadcast
7. 更新 vaultStore: addChild, refresh nodeCache
8. UI 刷新

### 离线策略

| 操作 | 离线 | 说明 |
|------|------|------|
| 浏览已缓存文件树 | ✅ | nodeCache 持久化本地 |
| 查看/解密已下载文件 | ✅ | ciphertext 在 StorageService |
| 创建/修改/删除文件 | ❌ | 需广播交易 |
| 查询余额/UTXO | ❌ | 需 RPC 查询 |
| 购买付费文件 | ❌ | 需 HTLC 交互 |

### 缓存持久化

| 数据 | 位置 | 方式 |
|------|------|------|
| 加密种子 | expo-secure-store | 系统 Keychain/Keystore |
| 设置 | MMKV | 同步高性能 KV |
| nodeCache | `${documentDirectory}/bitfs-cache/nodes.json` | JSON |
| walletState | `${documentDirectory}/bitfs-cache/wallet-state.json` | JSON |
| 文件内容 | `${documentDirectory}/bitfs-storage/` | hash-sharded |

### 同步机制

App 启动/手动刷新/切换 vault 时:
1. 加载本地 nodeCache（即时显示）
2. 后台查询链上 root node 最新版本
3. 对比 txID，有变化则递归同步子树 → 更新 nodeCache → UI 刷新

## 安全模型

### 存储分层

- **内存 (Zustand)**: 解锁时持有 Wallet 实例，锁定时 = null，GC 回收
- **expo-secure-store (硬件保护)**: 加密种子 (Argon2id + AES-256-GCM)，可选生物识别密码
- **expo-file-system (app sandbox)**: 加密文件内容 (Method 42 密文)，节点元数据缓存

### 安全要点

- 私钥永不离开进程内存，永不写入普通文件
- 种子双重保护：Argon2id 硬化 + 系统 Keychain/Keystore
- 自动锁定：后台超过 N 分钟 → 清除内存中 Wallet
- 文件内容 at rest 全部是 Method 42 密文

## V1 功能范围

### 包含

- 钱包创建 / 导入（12 词助记词）
- 密码 + 生物识别解锁
- 多 Vault 管理
- 文件浏览器（目录树导航、面包屑）
- 文件操作：put / mkdir / rm / mv / cp / link
- 加密模式切换：Private / Free / Paid
- sell（设置价格）
- x402 购买付费文件
- 文件分享（bitfs:// 链接）
- 余额显示 + 交易历史
- 网络切换（mainnet / testnet / regtest）
- 本地缓存 + 增量同步

### 不包含（V2+）

- 多钱包管理
- SPV 验证（V1 信任 RPC 节点）
- RevShare 收入分成
- CDN 节点连接
- 推送通知
- 文件内容预览（图片/PDF/文本渲染）
- 深度链接处理（bitfs:// URL scheme）

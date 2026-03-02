# BitFS Desktop — 设计文档

Date: 2026-03-02
Status: COMPLETED (2026-03-02) — MVP implemented in `bitfs-desktop/`

## Overview

BitFS Desktop 是基于 Wails v2 的跨平台桌面客户端（macOS/Windows/Linux），Go 后端直接使用 libbitfs-go，React 前端遵循 BitFS Dark Botanical VI。

**核心定位**：独立的 BitFS 文件管理器 + 钱包，不依赖 daemon 运行。Daemon 模式可选，仅用于文件自托管。

## Architecture

```
bitfs-desktop/  (Wails v2 应用)
├── frontend/                  # React 19 + Vite 6 + Tailwind CSS 4
│   ├── src/
│   │   ├── main.tsx           # Entry point
│   │   ├── App.tsx            # Router + Theme
│   │   ├── index.css          # BitFS Dark Botanical 主题
│   │   ├── pages/
│   │   │   ├── Onboarding.tsx # 创建/恢复钱包
│   │   │   ├── Unlock.tsx     # 密码解锁
│   │   │   ├── Files.tsx      # 文件浏览器
│   │   │   ├── Wallet.tsx     # 钱包管理
│   │   │   └── Settings.tsx   # 设置
│   │   ├── components/        # UI 组件
│   │   ├── hooks/             # React hooks
│   │   └── lib/               # 工具函数、类型
│   ├── package.json
│   ├── vite.config.ts
│   └── wailsjs/               # Wails 自动生成的 Go 绑定
├── app.go                     # Wails App struct (暴露给前端的方法)
├── wallet.go                  # 钱包相关方法
├── files.go                   # 文件操作方法
├── daemon.go                  # Daemon 启停控制
├── main.go                    # Wails 入口
├── go.mod                     # 依赖 libbitfs-go
└── build/                     # Wails 构建配置 + 图标
```

### 数据流

```
React 前端
  ↕ (Wails JS bindings, 自动生成)
Go 后端 (App struct)
  ↕ (直接函数调用)
libbitfs-go
  ├── vault/    → 文件系统操作 (ls/put/mkdir/rm/mv/cp)
  ├── wallet/   → HD 钱包管理 (BIP39/BIP32/BIP44)
  ├── method42/ → 加密/解密 (ECDH + AES-256-GCM)
  ├── network/  → 区块链交互 (broadcast, UTXO)
  ├── metanet/  → DAG 操作 + 元数据
  ├── spv/      → 轻客户端验证
  └── tx/       → 交易构建 (MutationBatch)
```

## Technology Stack

| 层 | 选型 | 版本 |
|---|---|---|
| 框架 | Wails | v2 (stable) |
| 后端 | Go | 1.25+ |
| 核心库 | libbitfs-go | latest (replace => ../libbitfs-go) |
| 前端 | React + TypeScript | 19 / 5.7 |
| 构建 | Vite | 6 |
| 样式 | Tailwind CSS | 4 |
| 路由 | React Router | v7 |
| 状态 | TanStack Query v5 | 服务端状态缓存 |
| 图标 | Lucide React | latest |

## Go Backend API

### App Struct

```go
type App struct {
    ctx     context.Context
    vault   *vault.Vault
    dataDir string
}
```

### 钱包方法

| 方法 | 签名 | 描述 |
|------|------|------|
| HasWallet | `() bool` | 检查是否已创建钱包 |
| CreateWallet | `(password string) (string, error)` | 创建钱包，返回助记词 |
| RestoreWallet | `(mnemonic, password string) error` | 从助记词恢复 |
| UnlockWallet | `(password string) error` | 解锁钱包 |
| LockWallet | `() error` | 锁定钱包 |
| IsUnlocked | `() bool` | 是否已解锁 |
| GetWalletInfo | `() (*WalletInfo, error)` | 地址、余额、公钥 |

### 文件方法

| 方法 | 签名 | 描述 |
|------|------|------|
| ListFiles | `(path string) ([]*FileEntry, error)` | 列出目录内容 |
| GetFileInfo | `(path string) (*FileInfo, error)` | 文件/目录元数据 |
| ReadFile | `(path string) ([]byte, error)` | 读取文件内容（解密） |
| PutFile | `(localPath, remotePath string) error` | 上传文件 |
| MakeDir | `(path string) error` | 创建目录 |
| Remove | `(path string, recursive bool) error` | 删除文件/目录 |
| Move | `(src, dst string) error` | 移动/重命名 |
| Copy | `(src, dst string) error` | 复制 |
| DownloadFile | `(remotePath, localPath string) error` | 下载到本地 |

### Daemon 方法（Phase 2）

| 方法 | 签名 | 描述 |
|------|------|------|
| StartDaemon | `(port int) error` | 启动 daemon 子进程 |
| StopDaemon | `() error` | 停止 daemon |
| IsDaemonRunning | `() bool` | 检查状态 |
| GetDaemonStatus | `() (*DaemonStatus, error)` | 通过 HTTP 获取状态 |

### 网络方法

| 方法 | 签名 | 描述 |
|------|------|------|
| GetNetwork | `() string` | 当前网络 (mainnet/testnet/regtest) |
| SetNetwork | `(network string) error` | 切换网络 |
| GetBalance | `() (int64, error)` | 查询余额（通过公开 API） |

## Frontend Pages

### 1. Onboarding（首次启动）

**路径**: `/onboarding`

两个流程：
- **创建钱包**: 生成助记词 → 确认备份 → 设置密码 → 完成
- **恢复钱包**: 输入助记词 → 设置密码 → 完成

UI: 分步向导（stepper），每步一个清晰操作。助记词显示用 12 宫格。

### 2. Unlock（解锁）

**路径**: `/unlock`

密码输入 → 解锁 vault → 跳转主界面。
支持错误提示（密码错误）和 loading 状态。

### 3. Files（文件浏览器，主页面）

**路径**: `/files`

- 左侧：目录树（可折叠）
- 右侧：文件列表（表格视图）
  - 列: 名称、类型、大小、访问模式、修改时间
  - 操作: 双击进入目录/预览文件
- 工具栏: 上传、新建文件夹、刷新
- 右键菜单: 下载、重命名、移动、复制、删除、属性

### 4. Wallet（钱包）

**路径**: `/wallet`

- 地址显示 + 二维码 + 复制按钮
- 余额（BSV + 法币换算）
- 公钥信息
- 派生路径 (m/44'/236'/0'/...)

### 5. Settings（设置）

**路径**: `/settings`

- 网络选择: Mainnet / Testnet / Regtest
- 数据目录: ~/.bitfs/ 路径显示
- 主题: Dark (默认) / System
- Daemon: 连接配置（Phase 2）
- 关于: 版本信息

## Visual Design

遵循 BitFS Dark Botanical VI 系统（与 Dashboard 一致）：

| 变量 | 值 | 用途 |
|------|-----|------|
| bg-primary | #1a1a1a | 主背景 |
| bg-card | #242424 | 卡片背景 |
| bg-sidebar | #1e1e1e | 侧边栏 |
| border | #333333 | 边框 |
| accent | #c9956b | 铜金强调色 |
| text-primary | #e5e5e5 | 主文字 |
| text-secondary | #999999 | 次文字 |
| font-sans | Inter | 界面字体 |
| font-mono | JetBrains Mono | 代码/地址 |

## Go Module 配置

```
module github.com/bitfsorg/bitfs-desktop

go 1.25

require (
    github.com/bitfsorg/libbitfs-go v0.1.0
    github.com/wailsapp/wails/v2 v2.x.x
)

replace github.com/bitfsorg/libbitfs-go => ../libbitfs-go
```

## 构建与分发

| 平台 | 构建命令 | 输出 |
|------|----------|------|
| macOS | `wails build -platform darwin/universal` | BitFS.app (~20MB) |
| Windows | `wails build -platform windows/amd64` | BitFS.exe |
| Linux | `wails build -platform linux/amd64` | bitfs-desktop |

## MVP 范围（Phase 1）

- [x] Wails 项目脚手架 + React 前端
- [ ] 钱包创建/恢复/解锁流程
- [ ] 文件浏览器（列表 + 导航）
- [ ] 钱包信息展示
- [ ] 设置页面
- [ ] BitFS Dark Botanical 主题

## Phase 2（后续）

- [ ] 文件上传（拖放 + 原生对话框）
- [ ] 文件下载到本地
- [ ] Daemon 启停控制
- [ ] 加密/解密文件操作
- [ ] 系统托盘 + 后台运行
- [ ] 自动更新

## Key Decisions

| 决策 | 选择 | 理由 |
|------|------|------|
| 框架 | Wails v2 (非 v3 alpha) | v2 稳定可靠，v3 仍在 alpha |
| Go 集成 | 直接 import libbitfs-go | 最简单直接，无中间层 |
| Daemon | 不嵌入，Phase 2 作为子进程 | 解耦，vault 操作不需要 daemon |
| 状态管理 | TanStack Query + React state | 服务端状态用 TQ，本地状态用 React |
| libbitfs-ts | 不使用 | 后端已是 Go，无需 TS 加密库 |
| 平台优先 | macOS 先行 | 开发环境即 macOS |

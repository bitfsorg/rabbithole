# BitFS Dashboard — 设计文档

> **定位**: Daemon 运维监控面板（只读），面向节点运营商和开发者。
> **不做**: 文件浏览/上传/下载、钱包操作（send/receive）、认证。这些留给 Flutter app / browser extension。

## 技术栈

- **前端**: React 19 + Vite 6 + TailwindCSS 4 + react-router-dom 7 + lucide-react
- **后端**: Go daemon 新增 5 个 `/_bitfs/dashboard/*` 端点
- **嵌入**: `go:embed dist/` 嵌入 Go 二进制
- **主题**: BitFS Dark Botanical（VI 系统）

## 视觉风格

| Token | 值 | 用途 |
|-------|-----|------|
| bg-primary | `#1a1a1a` | 页面/sidebar 背景 |
| bg-card | `#242424` | 卡片/面板背景 |
| bg-card-hover | `#2a2a2a` | 卡片 hover |
| border | `#333333` | 细边框 |
| accent | `#c9956b` | 铜金强调色（链接、active 状态、重点数据） |
| accent-hover | `#d4a57a` | accent hover |
| text-primary | `#e5e5e5` | 主文字 |
| text-secondary | `#999999` | 次要文字/标签 |
| text-muted | `#666666` | 弱文字 |
| success | `#4ade80` | 健康/在线 |
| warning | `#fbbf24` | 警告 |
| error | `#f87171` | 错误 |

**字体**: Inter (UI 文字), JetBrains Mono (数据/日志/哈希/地址)

## 后端 API 设计

新增文件: `bitfs/internal/daemon/dashboard.go`

### 1. GET /_bitfs/dashboard/status

```json
{
  "version": "0.1.0",
  "uptime_seconds": 3600,
  "vault_pnode": "02abc...def",
  "listen_addr": ":8080",
  "network": "mainnet",
  "started_at": "2026-02-28T00:00:00Z"
}
```

数据来源: daemon 启动时记录 `startTime`，version 硬编码，其余从 config/wallet 读取。

### 2. GET /_bitfs/dashboard/storage

```json
{
  "file_count": 42,
  "total_size_bytes": 10485760,
  "storage_path": "/Users/alex/.bitfs/storage"
}
```

数据来源: 遍历 storage 目录统计。可能较慢，加 5s 内存缓存。

### 3. GET /_bitfs/dashboard/wallet

```json
{
  "address": "1ABC...xyz",
  "balance_satoshis": 50000,
  "utxo_count": 3,
  "derivation_path": "m/44'/236'/0'/0/0"
}
```

数据来源: wallet 对象。balance 和 UTXO count 从 UTXO set 读取。

### 4. GET /_bitfs/dashboard/network

```json
{
  "network": "mainnet",
  "spv_enabled": true,
  "chain_tip_height": 800000,
  "chain_tip_hash": "00000000000000000002a7c4c1e48d76...",
  "blockchain_service": "rpc"
}
```

数据来源: blockchain service + SPV store。无 SPV 时返回 `spv_enabled: false` + 空 chain tip。

### 5. GET /_bitfs/dashboard/logs

```json
{
  "entries": [
    {"timestamp": "2026-02-28T01:23:45Z", "level": "info", "message": "daemon started on :8080"},
    {"timestamp": "2026-02-28T01:23:46Z", "level": "warn", "message": "SPV service not configured"}
  ]
}
```

实现: daemon 内部 ring buffer（200 条），`log.Logger` 同时写 ring buffer + 原有输出。查询参数: `?limit=50&level=warn`。

## 前端页面设计

### DashboardHome (/)

- 4 个 stat cards 横排: Uptime | Files | Balance | Chain Height
- 下方: 最近 Sales 列表（复用 `GET /_bitfs/sales`）

### Storage (/storage)

- Stat cards: File Count | Total Size | Storage Path
- 数据来自 `/_bitfs/dashboard/storage`

### Network (/network)

- Cards: Network | Blockchain Service | SPV Status
- Chain tip: height + hash (mono 字体)
- 数据来自 `/_bitfs/dashboard/network`

### Wallet (/wallet)

- Cards: Address (mono, 可复制) | Balance | UTXO Count
- Derivation path 信息
- 数据来自 `/_bitfs/dashboard/wallet`

### Logs (/logs)

- 日志表格: Timestamp | Level | Message
- Level 颜色标记: info=default, warn=amber, error=red
- 自动刷新 5s
- 数据来自 `/_bitfs/dashboard/logs`

## 共享组件

- `StatCard` — 标签 + 大数字/文本 + 可选图标
- `DataTable` — 简单表格，支持 mono 字体列
- `Badge` — level/status 标签（颜色变体）
- `usePolling(url, interval)` — 通用轮询 hook，5s 间隔

## 数据流

```
Dashboard Page → usePolling("/_bitfs/dashboard/xxx", 5000) → fetch → setState → render
```

无全局状态管理，每页独立 fetch。简单直接。

## 文件清单

**Go (daemon)**:
- 新增: `internal/daemon/dashboard.go` — 5 个 handler + ring buffer logger
- 修改: `internal/daemon/routes.go` — 注册 5 条新路由
- 修改: `internal/daemon/daemon.go` — 添加 startTime 字段 + ring buffer 初始化

**React (dashboard/src)**:
- 修改: `lib/api.ts` — 替换 stub 为真实 API 调用
- 新增: `hooks/usePolling.ts` — 通用轮询 hook
- 新增: `components/StatCard.tsx` — stat card 组件
- 新增: `components/DataTable.tsx` — 数据表格组件
- 新增: `components/Badge.tsx` — 状态标签组件
- 修改: `index.css` — Dark Botanical 主题 CSS 变量
- 修改: `layouts/MainLayout.tsx` — 深色主题样式
- 重写: 5 个 `pages/*.tsx` — 实际内容

## 不做的事

- 文件浏览/上传/下载 → 留给 Flutter app / browser extension
- 钱包操作 (send/receive) → 同上
- Authentication → daemon 是本地服务
- WebSocket/SSE → 轮询够用
- 图表库 → 纯数字 + 文本，YAGNI

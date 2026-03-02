# 暂缓工作项

> 整合自: 2026-02-25 设计审查报告、客户端生态设计、Den 浏览器设计。
> 所有项目当前暂缓，等底层协议定型 + libbitfs-go 完全成熟后再启动。

---

## 一、设计审查遗留 TODO

来源: `2026-02-25-design-review.md` 中未完成项。

### P1

- [ ] **3.7 Metanet 商标** — "Metanet" 已被注册（USPTO #7300182，第42类）。需法律审查，评估是否需要授权或改名。

### P2

- [ ] **2.3 Agent Friendly 落地** — 核心卖点但实现差距大：b* 工具 `--json` 缺 JSON Schema；无 SDK 层（Agent 必须调 CLI/HTTP）；x402 购买流程需多轮交互；无 MCP 适配。建议：定义 JSON Schema → 推进 libbitfs-ts → x402 单步 API → MCP server。
- [ ] **3.5 存储证明批量提交** — 每 SP × 每 epoch 需一笔链上交易。1000 合约时 12,000 笔/天。建议批量 Merkle root 提交或 rollup。
- [ ] **3.6 早期 PoW 安全性** — 独立 PoW 早期算力低，51% 攻击成本低。建议：初期 PoA 过渡 / 最低难度阈值 / BSV checkpoint 锚定。

### P3

- [ ] **1.4 大目录快照机制** — 100 万文件 ≈ 1-1.5GB 纯元数据。建议定期快照交易（Merkle root + 子项列表），客户端从快照重放增量。
- [ ] **4.2 "数据可以不上链"决策矩阵** — 链上 DataTx vs 链下 Daemon LFCP 的选择标准不清晰，需在设计文档中补充决策矩阵。

### DEFERRED

- [ ] **3.1 Oracle 角色定位** — Oracle 两职责（ECDH 分发 / 挑战管理）均可消除或合并。三种方案待深入分析后决策。

---

## 二、客户端生态（全部暂缓）

来源: `bitfs/2026-02-23-client-ecosystem-design.md`

等协议定型 + libbitfs-go 成熟 + b*/shell 充分测试后启动。优先级顺序：

### 1. Web Dashboard（最高优先）
- React 19 + TypeScript + Vite 6 + TailwindCSS 4
- Go `embed.FS` 嵌入 daemon，路由 `/_dashboard/*`
- 5 页面: Home / Storage / Network / Wallet / Logs
- 位置: `bitfs/dashboard/`

### 2. Chrome Extension
- MetaMask 模型，纯 TS 加密（@noble/* + Web Crypto）
- Popup (React) / Service Worker / Content Script
- 功能: bitfs:// 链接检测、HTTP 402 拦截、钱包管理
- 位置: `bitfs-extension/`（独立 repo）

### 3. Flutter App
- 跨平台 5 端（iOS/Android/macOS/Windows/Linux）
- Riverpod 2 + GoRouter + Go FFI (cgo C ABI)
- 位置: `bitfs-app/`（独立 repo）

### 4. Daemon Web Serving
- `/_dashboard/*` SPA 路由 + 静态站点托管
- 依赖 Dashboard 先完成

---

## 三、Den 区块链浏览器（暂缓）

来源: `den-explorer/2026-02-22-den-explorer-design.md` + 实施计划

- BSV 区块链浏览器，支持 BitFS/Metanet 协议解码
- Go + htmx + libbitfs-go，单二进制无需构建工具
- 功能: 区块/交易浏览、UTXO 查询、Metanet OP_RETURN 解码、DAG 可视化、Method 42 分析、SPV 验证
- 位置: `den-explorer/`
- 实施计划: 14 个 Task，详见原文件（已归档到 done/）

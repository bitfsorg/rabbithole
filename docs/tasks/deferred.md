# 暂缓工作项

> 底层协议已定型，libbitfs-go/ts 均已完成。客户端生态大部分已完成，剩余 Metanet 侧架构问题待解决。

## P1 — 阻塞性

- [ ] **Metanet 商标** — "Metanet" 已被注册（USPTO #7300182，第42类），需法律审查决定授权或改名

## P2 — 协议/架构

- [x] **Agent Friendly 落地** — b* `--json` 缺 JSON Schema、无 SDK 层、x402 多轮交互、无 MCP 适配。路径: JSON Schema → libbitfs-ts → x402 单步 API → MCP server ✅ 已完成
- [ ] **早期 PoW 安全性** — Metanet Chain 早期算力低，51% 攻击成本低。方案: 初期 PoA / 最低难度阈值 / BSV checkpoint 锚定
- [ ] **存储证明批量提交** — 1000 合约时 12,000 笔/天链上交易，需批量 Merkle root 或 rollup
- [ ] **Oracle 角色定位** — ECDH 分发 / 挑战管理两职责均可消除或合并，三种方案待深入分析

## P3 — 优化/文档

- [ ] **大目录快照机制** — 100 万文件 ≈ 1.5GB 元数据，需快照交易 + 增量重放

## 客户端生态

1. ~~**Web Dashboard** — React 19 + Vite + TailwindCSS，Go embed 嵌入 daemon `/_dashboard/*`~~ ✅ 已完成
2. ~~**Chrome Extension** — MetaMask 模型，纯 TS 加密（@bitfs/libbitfs），bitfs:// 检测 + x402 拦截~~ ✅ 已完成（审计 + 修复完毕）
3. ~~**Desktop App** — Wails v2（Go + React），连接 libbitfs-go，macOS/Windows/Linux~~ ✅ 已完成（10/10 任务）
4. ~~**Den 区块链浏览器** — Go + htmx，BitFS/Metanet 协议解码 + DAG 可视化~~ ✅ 已完成（v2 全功能）
5. **Mobile App** — React Native (Expo SDK 55) + libbitfs-ts，iOS/Android — 进行中（6/20 任务）

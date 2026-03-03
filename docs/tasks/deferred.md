# 暂缓工作项

## P1 — 协议/架构

- [ ] **BSV 兑换工具** — 独立库 + CLI，让用户/Agent 获取 BSV 以使用 BitFS。BitFS 支付层保持纯 BSV（不引入多币种支付），兑换作为独立入金工具。方案待定。
- [ ] **早期 PoW 安全性** — Metanet Chain 早期算力低，51% 攻击成本低。方案: 初期 PoA / 最低难度阈值 / BSV checkpoint 锚定
- [ ] **存储证明批量提交** — 1000 合约时 12,000 笔/天链上交易，需批量 Merkle root 或 rollup
- [ ] **Oracle 角色定位** — ECDH 分发 / 挑战管理两职责均可消除或合并，三种方案待深入分析

## P2 — ✅ 多输出批量交易·节点身份层（完成 2026-03-03）

节点身份从 `(P_node, TxID)` 扩展为 `(P_node, TxID, Vout)`。已实现:
- [x] libbitfs-go: `Node.Vout` 字段、`OutpointStore` 接口、`ParseNodeFromPushesWithOutpoint`、`ParseTxToNodes`
- [x] libbitfs-ts: 镜像变更（`vout` 字段、`OutpointStore`、`parseNodeFromPushesWithOutpoint`）
- [x] git-remote-bitfs: ChainReader 设置 Vout、PathEntry 含 Vout
- [x] bitfs: 集成测试 mock 实现 OutpointStore
- [x] 文档更新

**剩余（Metanet spec 阶段再做）**：ChildEntry 格式变更、SelfUpdate 紧凑 vout 引用、sCrypt MetanetBatch 合约、MutationBatch commit 单交易构建、engine 层适配。

## P3 — 优化

- [ ] **大目录 O(N) 遍历优化** — directory.go FindChild/AddChild/RemoveChild 线性遍历 []ChildEntry，万文件目录下性能差。建议加惰性 nameIndex map *(Antigravity #4)*

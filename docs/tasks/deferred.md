# 暂缓工作项

## P1 — 协议/架构

- [ ] **BSV 入金引导** — 不自建兑换服务，在余额不足等场景提示用户通过第三方购买 BSV：
  - **ChangeNow** (https://changenow.io/currencies/bitcoin-sv) — USDT→BSV 等 150+ 交易对，小额无需注册，~2 分钟到账
  - **Changelly** (https://changelly.com/exchange/bsv) — 支持信用卡购买 BSV，700+ 币种
  - 工作量: 在 CLI/App/Extension 余额不足时显示购买链接提示
- [ ] **早期 PoW 安全性** — Metanet Chain 早期算力低，51% 攻击成本低。方案: 初期 PoA / 最低难度阈值 / BSV checkpoint 锚定
- [ ] **存储证明批量提交** — 1000 合约时 12,000 笔/天链上交易，需批量 Merkle root 或 rollup
- [ ] **Oracle 角色定位** — ECDH 分发 / 挑战管理两职责均可消除或合并，三种方案待深入分析
- [ ] **多输出批量交易·剩余工作** — 节点身份层已完成 (P_node, TxID, Vout)，剩余: ChildEntry 格式变更、SelfUpdate 紧凑 vout 引用、sCrypt MetanetBatch 合约、MutationBatch commit 单交易构建、engine 层适配。Metanet spec 阶段统一设计

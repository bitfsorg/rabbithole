# 暂缓工作项

## P1 — 阻塞性

- [ ] **Metanet 商标** — "Metanet" 已被注册（USPTO #7300182，第42类），需法律审查决定授权或改名

## P2 — 协议/架构

- [ ] **BSV ↔ 稳定币兑换工具** — 独立库 + CLI，让用户/Agent 在 BSV 与 USDC/USDT 之间兑换（EVM 兼容链 + Solana）。解决 BSV 流动性和入金问题。可参考 purl.dev (Stripe/Coinbase x402) 的三方模型。实现路径: HTLC 原子跨链交换（BSV Script ↔ EVM Solidity ↔ Solana Anchor）或 CEX API 聚合，底层做通用兑换库，上层可集成进 BitFS daemon 多币支付流程
- [ ] **早期 PoW 安全性** — Metanet Chain 早期算力低，51% 攻击成本低。方案: 初期 PoA / 最低难度阈值 / BSV checkpoint 锚定
- [ ] **存储证明批量提交** — 1000 合约时 12,000 笔/天链上交易，需批量 Merkle root 或 rollup
- [ ] **Oracle 角色定位** — ECDH 分发 / 挑战管理两职责均可消除或合并，三种方案待深入分析
- [ ] **HTLC 链上退款路径** — 当前 OP_ELSE 分支是 2-of-2 多签，无 OP_CHECKLOCKTIMEVERIFY。预签名交易丢失则资金永久锁定。需加入链上 CLTV 退款路径 *(Antigravity #3)*

## P3 — 安全/健壮性

- [ ] **VerifyPayment 签名验证** — x402/verify.go 不验证 Input 签名也不绑定 InvoiceID（WARNING 注释明确标注）。需在上层封装中增加 TxID 去重 + 集成测试 *(Antigravity #1)*
- [ ] **NodeTypeAnchor 代码残留** — 设计文档已移除 Anchor，但 node.go 仍有 NodeTypeAnchor=3、7 个 TLV tag、6 个 struct 字段、完整序列化/反序列化。需统一 *(Antigravity #2)*
- [ ] **ComputeCapsuleHash 错误处理** — ecdh.go 已有 len(capsule)!=32 校验，但返回 nil 而非 error，调用方难以区分有效哈希和无效输入 *(Antigravity #5, 部分修复)*
- [ ] **bitfs-extension 审计修复** — 3 CRITICAL (Content Script 地址泄露, PRIVATE 解密用零 keyHash, OP_RETURN 解析越界) + 4 HIGH。详见 `docs/audits/2026-03-02-bitfs-extension-audit.md`

## P4 — 优化

- [ ] **大目录快照机制** — 100 万文件 ≈ 1.5GB 元数据，需快照交易 + 增量重放
- [ ] **大目录 O(N) 遍历优化** — directory.go FindChild/AddChild/RemoveChild 线性遍历 []ChildEntry，万文件目录下性能差。建议加惰性 nameIndex map *(Antigravity #4)*

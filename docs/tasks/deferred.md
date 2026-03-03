# 暂缓工作项

## P1 — 协议/架构

- [ ] **BSV 兑换工具** — 独立库 + CLI，让用户/Agent 获取 BSV 以使用 BitFS。BitFS 支付层保持纯 BSV（不引入多币种支付），兑换作为独立入金工具。方案待定。
- [ ] **早期 PoW 安全性** — Metanet Chain 早期算力低，51% 攻击成本低。方案: 初期 PoA / 最低难度阈值 / BSV checkpoint 锚定
- [ ] **存储证明批量提交** — 1000 合约时 12,000 笔/天链上交易，需批量 Merkle root 或 rollup
- [ ] **Oracle 角色定位** — ECDH 分发 / 挑战管理两职责均可消除或合并，三种方案待深入分析

## P2 — 多输出批量交易（Multi-Output Batch Transaction）

**决策 (2026-03-03)**：替代原 "大目录快照机制"。用多输出批量交易解决交易数量和数据效率问题。

### 方案概要

**核心变更**：一笔 BSV 交易包含多个 Metanet 节点操作（多 OP_RETURN 输出 + 对应 P2PKH 输出）。

```
Single Tx:
  Input 0:  P_dir UTXO (签名授权所有子操作)
  Output 0: OP_RETURN [CreateChild P_file1 metadata]
  Output 1: OP_RETURN [CreateChild P_file2 metadata]
  Output 2: OP_RETURN [SelfUpdate P_dir {ADD vout:0, ADD vout:1}]
  Output 3: P2PKH(P_dir)   ← 目录 UTXO 续链
  Output 4: P2PKH(P_file1) ← file1 初始 UTXO
  Output 5: P2PKH(P_file2) ← file2 初始 UTXO
```

### 关键设计点

1. **节点身份**：从 `(P_node, TxID)` 变为 `(P_node, TxID:Vout)` — 更 Bitcoin-native（UTXO 基本单位本就是 outpoint）
2. **目录更新紧凑**：SelfUpdate 引用同一交易内的 vout 索引，无需重复 TxID 或完整元数据
3. **原子性**：一笔交易要么全上链要么全不上链，不存在不一致状态
4. **跨目录操作**：`mv /dir1/file /dir2/file` = 一笔交易，多输入签名 = 多密钥授权
5. **MutationBatch 演进**：`batch.Commit()` 从 N 笔交易变为 1 笔交易 N 个输出
6. **不需要增量编码**：多输出批量已解决交易数量问题；正常目录（< 几千文件）全量列表完全够用；超大目录应使用子目录分层

### 影响范围

| 模块 | 影响 |
|------|------|
| `contracts/` | MetanetBatch.ts — sCrypt 定义批量交易约束 |
| `libbitfs-go/tx/` | 交易模板从单输出改为多输出 builder |
| `libbitfs-go/metanet/` | 节点引用从 TxID 改为 Outpoint；目录格式适配 |
| `bitfs/internal/engine/` | MutationBatch commit 改为单交易构建 |
| `git-remote-bitfs/` | mapper 的 SHA↔Metanet 映射需适配 outpoint |
| SPV | 一次 Merkle proof 覆盖整批操作 |

**时机**：Metanet spec 阶段统一设计。这是协议层变更。

## P3 — 优化

- [ ] **大目录 O(N) 遍历优化** — directory.go FindChild/AddChild/RemoveChild 线性遍历 []ChildEntry，万文件目录下性能差。建议加惰性 nameIndex map *(Antigravity #4)*

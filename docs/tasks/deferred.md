# 暂缓工作项

## P1 — HTLC 重构 (x402 → payment + sCrypt + 链上退款)

**决策 (2026-03-03)**：将 P1 重命名和 P2 HTLC 退款合并为一项统一重构。

### 方案概要

1. **x402 → payment** — 新包名确定为 `payment`。全面替换：libbitfs-go/x402/→payment/、libbitfs-ts/src/x402/→payment/、bitfs/internal/daemon/、bitfs/internal/engine/、docs/design/、docs/specs/bitfs/08-x402.md→08-payment.md、docs/whitepaper/、websites/。**Phase A 已完成 (Tasks 1-5)。**
2. **sCrypt 智能合约** — 用 sCrypt TypeScript DSL 重写 HTLC 锁定脚本，替代手工 opcode 拼接。合约源码放 `RabbitHole/contracts/`（顶层独立目录，所有链上逻辑的唯一源），编译产物 (artifact JSON) 分发到 libbitfs-ts 和 libbitfs-go。
3. **链上退款路径** — 通过 sCrypt 的 `this.ctx.locktime`（编译为 OP_PUSH_TX + nLockTime 提取），实现 buyer 单方面链上超时退款。完全消除预签名退款交易 (`BuildSellerPreSignedRefund`) 及其丢失风险。*(Antigravity #3, resolved by design)*
4. **Invoice ID mandatory** — 原可选 `<invoice_id> OP_DROP` 改为必选 `@prop()`，每个 HTLC 实例 invoiceId 不同 → 脚本 hash 不同 → 天然防重放。

### 技术要点

- **BSV 无 OP_CLTV**：Genesis 升级 (2020-02) 将 OP_CHECKLOCKTIMEVERIFY 还原为 NOP。go-sdk interpreter 在 `afterGenesis=true` 时跳过 CLTV 执行。
- **OP_PUSH_TX 替代**：sCrypt 利用 BIP143 sighash preimage + ECDSA 验证技巧，在脚本内提取 spending tx 的 nLockTime 字段并与 timeout 比较。密码学绑定，安全性等价于 OP_CLTV。
- **go-sdk 支持**：`transaction.CalcInputPreimage()` 已有完整 BIP143 preimage 构建，Go 侧构造退款交易时直接调用。

### 目录结构

```
RabbitHole/
  contracts/                        ← 新：所有 BSV 链上逻辑的唯一源（sCrypt）
    src/
      BitfsHTLC.ts                  ← 付费内容 HTLC
      MetanetNode.ts               ← 节点创建/更新（替代 tx/builder.go 手工模板）
      MetanetBatch.ts              ← 多输出批量交易约束
      StorageContract.ts           ← Metanet Phase 2：存储证明
      PaymentChannel.ts            ← 支付通道
    artifacts/
      *.json                        ← 编译产物（committed）
    package.json                    ← scrypt-ts + scrypt-cli
    tsconfig.json                   ← experimentalDecorators + ts-patch

  libbitfs-ts/src/payment/          ← 原 x402/ → payment/
    artifacts/BitfsHTLC.json        ← copy from contracts/
  libbitfs-go/payment/              ← 原 x402/ → payment/
    artifacts/BitfsHTLC.json        ← copy from contracts/
```

**`contracts/` 定位**：BitFS/Metanet 所有链上逻辑的唯一源。不只是 HTLC，未来 Metanet 节点操作、存储合约、支付通道等都用 sCrypt 定义。链本身强制协议合规（而非仅靠客户端软件）。`libbitfs-go/tx/builder.go` 和 `libbitfs-ts/src/tx/` 中的手工交易构建逻辑最终都迁移到 sCrypt 合约。

### 合约代码

```typescript
export class BitfsHTLC extends SmartContract {
  @prop() readonly invoiceId: ByteString    // 16B, mandatory
  @prop() readonly capsuleHash: Sha256      // 32B
  @prop() readonly sellerPkh: PubKeyHash    // 20B
  @prop() readonly buyerPkh: PubKeyHash     // 20B
  @prop() readonly timeout: bigint          // block height

  @method() public claim(preimage: ByteString, sig: Sig, pubkey: PubKey) {
    assert(sha256(preimage) == this.capsuleHash, 'wrong capsule')
    assert(hash160(pubkey) == this.sellerPkh, 'wrong seller')
    assert(checkSig(sig, pubkey), 'invalid sig')
  }

  @method() public refund(sig: Sig, pubkey: PubKey) {
    assert(this.ctx.locktime >= this.timeout, 'too early')
    assert(hash160(pubkey) == this.buyerPkh, 'wrong buyer')
    assert(checkSig(sig, pubkey), 'invalid sig')
  }
}
```

### 删除项

- `BuildSellerPreSignedRefund` (Go + TS) — 预签名退款不再需要
- `BuildBuyerRefundTx` 中的 seller 签名依赖 — buyer 自主构造退款
- `SellerPreSignParams`, `SellerPreSignResult` 类型
- 2-of-2 multisig OP_ELSE 分支

## P2 — 协议/架构

- [ ] **BSV 兑换工具** — 独立库 + CLI，让用户/Agent 获取 BSV 以使用 BitFS。BitFS 支付层保持纯 BSV（不引入多币种支付），兑换作为独立入金工具。方案待定。
- [ ] **早期 PoW 安全性** — Metanet Chain 早期算力低，51% 攻击成本低。方案: 初期 PoA / 最低难度阈值 / BSV checkpoint 锚定
- [ ] **存储证明批量提交** — 1000 合约时 12,000 笔/天链上交易，需批量 Merkle root 或 rollup
- [ ] **Oracle 角色定位** — ECDH 分发 / 挑战管理两职责均可消除或合并，三种方案待深入分析

## P3 — 多输出批量交易（Multi-Output Batch Transaction）

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

## P4 — 优化

- [ ] **大目录 O(N) 遍历优化** — directory.go FindChild/AddChild/RemoveChild 线性遍历 []ChildEntry，万文件目录下性能差。建议加惰性 nameIndex map *(Antigravity #4)*

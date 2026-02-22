# HTLC 原子交换端到端 — 设计文档

## 目标

补齐 HTLC 原子交换核心缺失，让 sell → invoice → HTLC funding → seller claim → buyer decrypt 整条路径真正跑通。包括 seller claim 和 buyer refund 两条 spending path 的完整交易构建+签名。

## 现状分析

### 已实现 (~60%)

| 组件 | 文件 | 状态 |
|------|------|------|
| HTLC locking script | `libbitfs/x402/htlc.go:BuildHTLC()` | 完整 |
| Preimage 提取 | `libbitfs/x402/htlc.go:ParseHTLCPreimage()` | 完整 |
| Invoice 管理 | `libbitfs/x402/invoice.go` | 完整 |
| P2PKH 支付验证 | `libbitfs/x402/verify.go:VerifyPayment()` | 完整但不适用 HTLC |
| HTTP 402 headers | `libbitfs/x402/headers.go` | 完整 |
| Daemon 买入端点 | `bitfs/internal/daemon/payment.go` | 有 4 个 bug |
| bget --buy 客户端 | `bitfs/cmd/bget/main.go` | 有 2 个 bug |
| Method 42 capsule | `libbitfs/method42/ecdh.go` | 完整 |
| E2E regtest 测试 | `bitfs/e2e/06_paid_purchase_test.go` | 用 dummy sig |

### 4 个 Bug

1. **daemon/payment.go:43** — capsuleHash = SHA256(keyHash)，应该是 SHA256(ECDH(D_node, P_node).x)
2. **bget/main.go:254** — 提交 HTLC script 而非完整交易
3. **daemon/payment.go:195** — 用 P2PKH VerifyPayment 验证 HTLC output
4. **daemon/payment.go:229** — 返回存储的加密数据，不是 ECDH capsule

### 缺失

- `BuildHTLCFundingTx()` — 完整 HTLC funding 交易
- `BuildSellerClaimTx()` — seller claim path 签名 (`<sig> <pubkey> <capsule> OP_TRUE`)
- `BuildBuyerRefundTx()` — buyer refund path 签名 (`<sig> OP_FALSE`)
- `VerifyHTLCFunding()` — 验证 funding tx 中的 HTLC output
- Daemon 正确的 capsule 计算和返回
- E2E regtest 中的真实 claim tx（当前用 dummy signature）

## 技术要点

### go-sdk 签名 API

HTLC 不是 P2PKH，需要自定义 `UnlockingScriptTemplate`：

```go
// go-sdk 接口
type UnlockingScriptTemplate interface {
    Sign(tx *Transaction, inputIndex uint32) (*script.Script, error)
    EstimateLength(tx *Transaction, inputIndex uint32) uint32
}

// sighash 计算
sigHash, _ := tx.CalcInputSignatureHash(inputIndex, sighash.AllForkID)
sig, _ := privateKey.Sign(sigHash)
// sig 末尾附加 sighash flag: append(sig.Serialize(), uint8(sighash.AllForkID))
```

### HTLC Seller Claim 解锁脚本

```
<sig+flag> <seller_pubkey> <capsule> OP_TRUE
```

对应 HTLC locking script 的 IF 分支：
```
OP_IF
  OP_SHA256 <capsule_hash> OP_EQUALVERIFY
  OP_DUP OP_HASH160 <seller_addr> OP_EQUALVERIFY OP_CHECKSIG
```

执行流程：OP_TRUE 进入 IF → SHA256(capsule) == capsule_hash ✓ → HASH160(seller_pubkey) == seller_addr ✓ → CHECKSIG ✓

### HTLC Buyer Refund 解锁脚本

```
<sig+flag> OP_FALSE
```

对应 ELSE 分支，nLockTime 必须 >= timeout：
```
OP_ELSE
  <timeout> OP_CHECKLOCKTIMEVERIFY OP_DROP
  <buyer_pubkey> OP_CHECKSIG
```

注意：buyer refund 路径中 buyer_pubkey 已硬编码在 locking script 中，所以 unlocking script 只需要 sig（不需要 push pubkey）。

### Capsule 协议流程

1. Content 加密：`aes_key = HKDF(ECDH(D_file, P_file).x, key_hash)` → AES-GCM
2. Capsule = `ECDH(D_file, P_file).x`（32 bytes，content owner 的 shared secret）
3. CapsuleHash = `SHA256(capsule)`（锁在 HTLC 中）
4. Seller 在 claim tx 中揭示 capsule → buyer 从链上提取
5. Buyer 解密：`aes_key = HKDF(capsule, key_hash)` → AES-GCM-Decrypt

## 范围决策

- Core E2E flow only（不含 key caching、token hash chain、directory xpub）
- Unit + integration tests（mock UTXO）+ E2E regtest tests（真实链上）
- 新代码全部放在 `libbitfs/x402/`（Approach A）

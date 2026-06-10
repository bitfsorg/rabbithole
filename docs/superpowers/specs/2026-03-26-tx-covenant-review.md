# BitFS 交易与 Covenant 设计现状审查

> 目的：梳理现有 BitFS 在交易结构和 covenant 方面的全部设计决策，供重新设计时作为输入。
> 每个部分末尾留有 **审查意见** 栏位，请标注：✅ 保留 / ⚠️ 需修改 / ❌ 废弃 / 💡 新想法

---

## 一、交易结构

### 当前设计

每个 Metanet 节点操作对应一笔 BSV 交易，结构固定：

```
Inputs:
  [0] P_parent UTXO (D_parent 签名) — 形成 Metanet Edge
  [1+] Fee inputs (fee wallet 签名)

Outputs:
  [0] OP_FALSE OP_RETURN <MetaFlag> <P_node> <TxID_parent> <TLV_payload>
  [1] P2PKH dust (1 sat) → P_child — Node UTXO
  [N] P2PKH change
```

**关键特征：**
- OP_RETURN 承载元数据（MetaFlag 4B + P_node 33B + TxID_parent 32B + TLV payload）
- 每个节点有一个 P2PKH UTXO（1 sat dust），花费此 UTXO = 证明对节点的控制权
- 父节点签名输入 = Metanet Edge（DAG 关系的链上证明）
- 版本控制：同一 P_node 多次出现时，最新 = 最高区块高度 + TTOR 排序

**审查意见：**
1、如何指明这是BitFS协议的交易
2、需要结合Metanet生态中已经有的其他协议,比如b://

---

## 二、四种交易模板

| 模板 | 用途 | 输入 | 输出 |
|------|------|------|------|
| CreateRoot | 创建 Vault 根目录 | 无 Node UTXO（新树引导） | OP_RETURN + P2PKH(P_root) |
| CreateChild | 创建子节点 | 花费 P_parent UTXO | OP_RETURN + P2PKH(P_child) |
| SelfUpdate | 更新节点 | 花费自身 P_node UTXO | OP_RETURN + P2PKH(P_node, 刷新) |
| DataTx | 存储加密内容 | 花费任意 UTXO | `<content> OP_DROP P2PKH(P_node)` |

**实际使用组合：**
- `put`（上传文件）= CreateChild + SelfUpdate(parent)
- `mkdir` = CreateChild + SelfUpdate(parent)
- `mv`（跨目录）= CreateChild(dest) + Delete(src) + SelfUpdate(srcParent) + SelfUpdate(dstParent)
- `sell`（定价）= SelfUpdate(node)

**审查意见：**
1、CreateRoot 交易和 CreateChild 交易的结构可以合并？
2、SelfUpdate是否可以改名为Update？
3、为什么没有Delete操作？


---

## 三、TLV 二进制序列化

### 格式
`Tag(1B) + Length(unsigned varint/LEB128) + Value`

### 核心 Tag（摘要）

| Tag | 名称 | 类型 | 说明 |
|-----|------|------|------|
| 0x01 | Version | uint32 | 协议版本 |
| 0x02 | Type | uint32 | FILE=0, DIR=1, LINK=2, ANCHOR=3 |
| 0x03 | Op | uint32 | CREATE=0, UPDATE=1, DELETE=2 |
| 0x06 | KeyHash | 32B | SHA256(SHA256(plaintext))，双重用途：KDF salt + 完整性校验 |
| 0x07 | Access | uint32 | PRIVATE=0, FREE=1, PAID=2 |
| 0x08 | PricePerKB | uint64 | 付费价格 |
| 0x0D | Index | uint32 | BIP32 路径段 |
| 0x0E | ChildEntry | sub-TLV | index + name + type + pubkey + hardened |
| 0x1A | MerkleRoot | 32B | 目录子节点哈希 |
| 0x1B | EncPayload | bytes | PRIVATE 模式加密元数据 |

**设计特征：**
- Tag 升序排列
- 固定长度字段严格校验（uint32=4B, hash=32B, pubkey=33B）
- 最大 payload 64MB
- PRIVATE 模式：`salt(16B) || nonce(12B) || AES-256-GCM(full_tlv) || tag(16B)`
- 预留了 48+ 个 tag，包括压缩、Rabin 签名、ISO、ACL 等扩展

**审查意见：**

---

## 四、HTLC 支付脚本

### 当前设计：106 字节固定长度纯 Bitcoin Script

```
<invoiceId(16B)> OP_DROP
OP_IF
  OP_SHA256 <capsuleHash(32B)> OP_EQUALVERIFY
  OP_DUP OP_HASH160 <sellerPkh(20B)> OP_EQUALVERIFY OP_CHECKSIG
OP_ELSE
  OP_DUP OP_HASH160 <buyerPkh(20B)> OP_EQUALVERIFY OP_CHECKSIG
OP_ENDIF
```

**关键设计决策：**
- **纯脚本**：不依赖 sCrypt，不使用 covenant，可审计
- **固定长度**：106 字节，偏移量固定，费用可预测
- **invoiceId**：16B 唯一标识，防止跨文件重放
- **capsuleHash** = SHA256(fileTxID || capsule)，将 capsule 绑定到特定文件
- **无 OP_CLTV**：BSV post-Genesis 拒绝 OP_NOP2，超时通过 nLockTime 在交易级别强制
- 超时范围：[6, 288] 区块，默认 72 区块（~12 小时）

**两条路径：**
- Claim（卖方）：`<sig> <pubkey> <fileTxID||capsule> OP_TRUE`
- Refund（买方）：`<sig> <pubkey> OP_FALSE`（nLockTime ≥ timeout）

**审查意见：**

---

## 五、Method 42 加密

### ECDH 密钥派生

```
shared_secret = ECDH(D_node, P_node).x          // secp256k1
aes_key = HKDF-SHA256(shared_secret, key_hash, "bitfs-file-encryption")
```

### 三种访问模式

| 模式 | D_node | 谁能解密 | 用途 |
|------|--------|---------|------|
| PRIVATE | 节点私钥 | 仅 owner | 私人文件 |
| FREE | 1（标量 1） | 任何人（P_node 公开） | 公开内容 |
| PAID | 节点私钥 | 购买者（通过 capsule） | 付费内容 |

### Capsule 机制（PAID 模式）

```
buyer_mask = HKDF-SHA256(ECDH(D_node, P_buyer).x, key_hash, "bitfs-buyer-mask")
capsule = aes_key XOR buyer_mask                  // 32 bytes

// 买方解密：
buyer_mask = HKDF-SHA256(ECDH(D_buyer, P_node).x, key_hash, "bitfs-buyer-mask")
aes_key = capsule XOR buyer_mask
```

**设计特征：**
- secp256k1 ECDH + HKDF-SHA256 + AES-256-GCM
- key_hash 双重用途：KDF salt + 内容完整性承诺
- key_hash = SHA256(SHA256(plaintext))，不直接暴露明文哈希
- Capsule 可选 nonce（invoice_nonce）增强不可链接性

**审查意见：**

---

## 六、节点身份模型

### 演进
- 早期：`(P_node, TxID)` — 一笔交易一个操作
- 当前：`(P_node, TxID, Vout)` — 支持多输出批量交易

### BIP32 派生
- 路径：`m/44'/236'/account'/chain/index`
- Hardened child：需要私钥派生，独立 HTLC 购买
- Non-hardened child：xpub 可派生，支持目录级整体购买（设计完成，未完全实现）

**审查意见：**

---

## 七、多输出批量交易（MutationBatch）

### 设计

单笔交易包含多个节点操作，交替排列 OP_RETURN + P2PKH 对：

```
Inputs:  [Node UTXOs (去重)] + [Fee inputs (去重)]
Outputs: [OP_RETURN #1] [P2PKH #1] [OP_RETURN #2] [P2PKH #2] ... [Change]
```

**关键特征：**
- 所有 engine 操作通过 MutationBatch 构建（唯一的交易构建路径）
- 输入 UTXO 自动去重
- 费用估算公式：`10 + inputs*148 + outputs*34 + ops*89 + payload_bytes`
- Build() 返回每个操作的 Vout 索引
- **非线程安全**，调用方需串行化

**实现状态：** Builder API 完成，部分 engine 层适配仍在进行中。

**审查意见：**

---

## 八、Covenant / 链上强制执行

### 已实现
1. HTLC 脚本 — 支付路径链上强制
2. OP_DROP 内容锁定 — P_node 持有者才能花费
3. 签名要求 — 每条 Metanet Edge 需要 D_parent 签名

### 未实现（已设计）

| 能力 | 优先级 | 状态 | 说明 |
|------|--------|------|------|
| 写权限 covenant | P3 | 设计阶段 | sCrypt / OP_PUSH_TX 强制写权限 |
| 收益分配 covenant | P3 | 设计完成 | Registry UTXO + Share UTXO + ISO Pool |
| CLTV 时间锁 | P2 | 部分实现 | 基础验证完成，daemon 行为缺失 |
| BLS12-381 群签名 | P4 | 设计阶段 | 多人读写权限 |

**当前策略：** v0.0.1 保持精简，高级 covenant 推迟。链上只做最基本的约束，策略层在 daemon。

**审查意见：**

---

## 九、Capsule Hash 绑定

### HTLC 中的 capsule 验证

```
capsule_hash = SHA256(fileTxID || capsule)
```

- HTLC 脚本锁定此哈希
- 卖方 claim 时公开 `fileTxID || capsule` 作为 preimage
- 买方从链上提取 capsule，结合 ECDH 恢复 aes_key
- **可选不可链接性**：加入 invoice_nonce 使同一买方多次购买产生不同 capsule

**审查意见：**

---

## 十、已知问题与技术债

| 问题 | 影响 | 现状 |
|------|------|------|
| Multi-output engine 适配未完成 | ChildEntry 格式、SelfUpdate vout 引用 | P1 roadmap |
| 早期 PoW 51% 攻击 | Metanet Chain 安全性 | 方案待选（PoA/checkpoint） |
| 存储证明 12K tx/day | 链上成本 | 需 Merkle rollup |
| 写权限仅应用层 | 安全模型不完整 | v0.1 defer |
| BSV OP_CLTV 不可用 | 只能用 nLockTime 替代 | 设计限制 |
| Metanet 包独立于 libbitfs-go | 代码重复风险 | 待评估整合 |
| Session lock/unlock 未实现 | 每次操作输密码 | BITFS_PASSWORD 临时方案 |

**审查意见：**

---

## 总体审查

### 带过来的（你认为对的设计）

_（请列出）_

### 需要根本性重新设计的

_（请列出）_

### 新项目应该首先确定的原则

_（请列出）_

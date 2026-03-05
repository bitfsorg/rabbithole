# Metanet 详细设计

> 本文档为 Metanet 设计文档体系的第三层：算法、数据结构、协议细节。
>
> **文档体系**:
> - [整体设计](../OverallDesign.zh.md) — 两产品生态、三层架构、界面划分
> - [概念设计](1-ConceptDesign.zh.md) — 产品定位、核心理念、设计原则
> - [系统设计](2-SystemDesign.zh.md) — 节点架构、合约、支付通道
> - **详细设计** (本文档) — 共识、挖矿、结算协议细节
> - [测试设计](4-TestDesign.zh.md) — 测试用例设计

---

本文档补充 Metanet Overlay Network 的精确协议细节、Script 设计和经济参数, 与系统设计各节对应。

---

## 一、Verify-Then-Pay 存储合约 Script

存储合约依赖于 Challenge UTXO 和押金 UTXO 的配合：

```
Challenge UTXO 锁定脚本 (每个 epoch k 一个):

OP_IF
  // 路径 A: Storage Provider 提交有效 Merkle 证明 → 获得当期 MNT
  <provider_pubkey> OP_CHECKSIGVERIFY

  // Merkle proof 验证
  // 解锁脚本提供: chunk_data(b_i), dir_1, s_1, ..., dir_d, s_d
  OP_SHA256                                         // hash(b_i) → h
  // Repeat for level j = 1 to d:
    OP_SWAP                                         // bring dir_j
    OP_NOTIF                                        // if dir=0 (left child)
      OP_SWAP                                       // reorder for left
    OP_ENDIF
    OP_CAT                                          // concatenate pair
    OP_SHA256                                       // hash → parent
  // End repeat
  <r_M,j> OP_EQUAL                                 // verify Merkle root

OP_ELSE
  // 路径 B: 超时未响应 → 回退给 Oracle
  <τ> OP_CHECKSEQUENCEVERIFY OP_DROP
  <oracle_pubkey> OP_CHECKSIG

OP_ENDIF

// 挑战索引生成 (确定性):
challenge_index_k = H(block_hash_he || epoch_k || provider_id) mod num_chunks
// block_hash_he 是当前最新 BSV 区块哈希, 保证随机性
```

```
押金 UTXO 脚本:

OP_IF
  // 路径 A: 合约正常完成 → 退还给 Storage Provider
  <contract_end_height> OP_CHECKLOCKTIMEVERIFY OP_DROP
  <provider_pubkey> OP_CHECKSIG

OP_ELSE
  // 路径 B: Provider 违约 (连续未响应) → Oracle 提交违约证据, 押金退回 Publisher
  <oracle_pubkey> OP_CHECKSIGVERIFY
  <publisher_pubkey> OP_CHECKSIG

OP_ENDIF
```

脚本执行与原来不同的地方在于，完整 Merkle 验证被完全放在了 Bitcoin Script 系统内（因为 BSV 允许大脚本）。

---

## 三、ECDH 双层加密流程

```go
// 伪代码: Owner 为 Metanet Node 重加密数据

func ReencryptForProvider(
    ownerPrivKey *ec.PrivateKey,    // D_node
    providerPubKey *ec.PublicKey,   // P_provider
    encryptedData []byte,            // 已用 Method 42 加密的数据
) (providerEncrypted []byte, merkleRoot []byte, err error) {

    // Step 1: ECDH 派生 Metanet Node 专用密钥
    sharedSecret := ECDH(ownerPrivKey, providerPubKey)
    providerKey := KDF(sharedSecret)  // SHA256(shared_x || "storage")

    // Step 2: 用 Metanet Node 专用密钥再加密 (双层)
    // 注意: 不解密原始数据, 直接在密文上再加一层
    nonce := random(12)  // AES-256-GCM nonce
    providerEncrypted = AES256GCM_Encrypt(providerKey, nonce, encryptedData)

    // Step 3: 分块构建 Merkle 树
    chunks := SplitIntoChunks(providerEncrypted, CHUNK_SIZE)  // CHUNK_SIZE = 256KB
    leaves := make([][]byte, len(chunks))
    for i, chunk := range chunks {
        leaves[i] = SHA256(chunk)
    }
    merkleRoot = BuildMerkleTree(leaves)

    return providerEncrypted, merkleRoot, nil
}

// 验证存储证明
func VerifyStorageProof(
    merkleRoot []byte,
    chunkIndex int,
    chunkData []byte,
    siblings [][]byte,
) bool {
    hash := SHA256(chunkData)
    for i, sibling := range siblings {
        if chunkIndex>>i&1 == 0 {
            hash = SHA256(append(hash, sibling...))
        } else {
            hash = SHA256(append(sibling, hash...))
        }
    }
    return bytes.Equal(hash, merkleRoot)
}

关键性质:
  - 每个 Metanet Node 的密文不同 (不同 ECDH 共享密钥)
  - Metanet Node 无法解密内容 (不知道 Owner 的第一层密钥)
  - Metanet Node 无法伪造证明 (需要实际持有数据才能回答 Merkle 挑战)
  - Owner 可验证任何 Metanet Node 的证明 (持有所有 ECDH 密钥)
```

> **性能说明**: 大文件 (如 1GB) 双层加密开销显著 — N 个 Metanet Node 需 N 次独立 AES-GCM 加密
> + N 次网络上传。优化方向: (1) 延迟加密 — Metanet Node 请求时 Owner 实时提供 Provider 密钥,
> Metanet Node 自行加密; (2) 分片级加密 — 仅对被挑战的 chunk 执行双层加密。

---

## 四、支付通道协议

> **适用范围**: 本节描述的 2-of-2 多签支付通道协议适用于所有三种通道类型（BSV User↔Node 通道、MNT Owner↔Node 通道、MNT Node↔Node 通道）。三者使用相同的脚本结构和撤销逻辑，唯一区别是锁定的币种（BSV 或 MNT）和所在链（BSV 主链或 Metanet Overlay）。

```
支付通道交易结构:

1. 开通道 (Funding TX):
   Input:  User 的 UTXO (BSV 或 Token)
   Output: 2-of-2 多签 (User + Metanet Node)
           Value: channel_capacity

   Script: OP_2 <user_pubkey> <node_pubkey> OP_2 OP_CHECKMULTISIG

2. 链下更新 (Commitment TX, 不广播):
   Input:  Funding TX output
   Output 0: Node   → node_balance
   Output 1: User   → user_balance
   其中 node_balance + user_balance = channel_capacity

   每次下载计费请求:
     node_balance   += price
     user_balance -= price
     双方签名新的 Commitment TX

3. 关通道 (Settlement TX):
   任一方广播最新 Commitment TX

   争议解决详细机制:
   - 状态版本: 每次通道更新附带递增 sequence_number
   - 撤销密钥: 每次状态更新, 双方交换上一状态的 revocation_key
   - 旧状态广播: 若一方广播旧版本 (sequence_number < latest):
     - 另一方在 T 个块 (CSV 时间锁) 内使用 revocation_key 提交惩罚交易
     - 惩罚: 广播旧状态方损失全部通道余额
   - 正常关闭: 双方签署最终状态, 无需等待时间锁
   - 使用 OP_CHECKSEQUENCEVERIFY 实现时间锁

通道参数:
  min_deposit:       10000 sat (最小存款)
  max_duration:      144 blocks (约 1 天, 最长通道寿命)
  dispute_window:    6 blocks (争议窗口)
  update_frequency:  每次下载计费请求

Node↔Node MNT 通道结算说明:
  - 场景: Node_A 从 Node_B 批发热门数据 (系统设计第四节 "Metanet Node 间批发")
  - 通道位于 Metanet Overlay (MNT Token), 非 BSV 主链
  - Funding: 买方 Node_A 锁入 MNT Token
  - 更新: 每次数据传输 (chunk 级别), 双方签署新的余额分配
  - 结算: 通道到期或余额耗尽时, 广播最新 Commitment TX 到 Metanet Overlay
  - 定价: 由 Node_B 自行设定 (通常低于下载计费零售价, 体现批发折扣)
  - 与 Owner↔Node 通道的区别仅在于双方角色 — 脚本结构和争议机制完全相同
```

---

## 五、下载计费支付通道 HTTP 协议扩展

> **前置依赖**: 本节是下载计费基础协议的支付通道扩展。下载计费基础带宽计费规则见 [BitFS 详细设计 十三-B.C](../bitfs/3-DetailedDesign.zh.md#c-下载计费支付流程)。

```
新增 HTTP Headers:

请求端:
  X-Accept-Channel: true                    # 客户端支持通道支付
  X-Channel-ID: <funding_txid>:<vout>       # 已有通道 ID
  X-Payment-Voucher: <base64(signed_ctx)>   # 签名的通道更新

响应端:
  X-Accept-Channel: true                    # 服务端支持通道支付
  X-Channel-Price: <sats_per_kb>            # 通道内价格 (可低于单次价)
  X-Channel-Min-Deposit: <sats>             # 最低通道存款
  X-Channel-Balance: <remaining_sats>       # 通道剩余余额
  X-Channel-Expiry: <block_height>          # 通道过期高度

通道内支付流程:
  1. Client: GET /data/{hash}  (X-Channel-ID + X-Payment-Voucher)
  2. Server: 验证签名, 更新本地余额
  3. Server: 200 + data + X-Channel-Balance
  4. 无需等待链上确认, 即时完成

通道开启流程:
  1. Client: POST /channel/open  (funding_tx)
  2. Server: 验证 funding_tx, 返回 channel_id
  3. Client: 广播 funding_tx 到链上
  4. Server: 等待确认后激活通道

通道关闭流程:
  1. Client: POST /channel/close  或 Server 主动关闭
  2. 广播最新 Commitment TX
  3. 等待 dispute_window 后结算

错误处理:
  - 余额不足: 402 + X-Channel-Balance: 0 + X-Channel-TopUp-Required: true
  - 通道过期: 402 + X-Channel-Expired: true
  - 签名无效: 400 Bad Request
```

---

## 六、ML Block 结构与挖矿

基于 Multilevel Blockchain 专利，ON 网络通过载体交易嵌入 BSV 主网，抛弃了合并挖矿模型，采用纯 SHA256 独立竞争出块，并通过 BSV 提供最终性防篡改。

### 1. BSV 交易内嵌入
每个 ML Block 均打包为一笔合法的 BSV 交易：
- **Chain Input**: 花费上一个 Block 的 Chain Output，以形成链式结构
- **Carrier Pairs**: ON 网络的交易通过 SIGHASH_SINGLE | ANYONECANPAY 附加到交易随行输入中
- **OP_RETURN Output**: `Output[1]` 中使用 `OP_RETURN` 放置 ML Block Header
- **Coinbase Output**: `Output[2]` 铸造当前 epoch 规定的 MNT 出块奖励发放给矿工
- **ON Tx Outputs**: 处理 ON 网络内转移（如 MNT 合约等）的其他合法输出

### 2. ML Block Header 格式
精确的 96 字节固定数据在 `OP_RETURN` 之后：

| 字段 | 大小 | 说明 |
|---|---|---|
| Magic | 4 B | `0x4d4e4d4c` ("MNML") |
| Version | 4 B | 协议版本，初始为 1 |
| PrevHash | 32 B | 上一个 ML Block Header 哈希 (SHA256d) |
| TxMerkleRoot| 32 B | 当前区块打包的所有 ON 交易 Merkle 根 |
| Timestamp | 4 B | 出块时间 Unix 时间戳 |
| Difficulty | 4 B | 当前难度目标 (nBits) |
| Nonce | 4 B | 工作量证明 Nonce 值 |
| Height | 4 B | 当前区块高度 |
| Coinbase | 8 B | 本周期应当增发的 MNT 数量 (satoshis) |

### 3. POW 机制
- 采用纯 SHA256d (与 Bitcoin 同构，使得相关矿机兼容或直接被再利用，但不需 BSV 矿池特别搭线)
- 目标出块时间为 5 分钟 (比特币的两倍体量心跳，提供更快资金结算)
- 难度调整策略同为每 2016 个块执行，约等价于现实世界 1 周

---

## 七、Token 经济参数

```
MNT Token 参数:

总供应量:   21,000,000 MNT
最小单位:   1 satoshi = 0.00000001 MNT
初始区块奖励: 50 MNT
减半周期:   210,000 块 (日历减半周期约 2 年)
目标区块时间: ~5 分钟
难度调整:   每 2016 块 

减半时间表 (Bitcoin 2× Speed 出块逻辑下):
  Era 0:  块 0 - 209,999       50.0  MNT/块   总计 10,500,000 (Y0-2)
  Era 1:  块 210,000 - 419,999 25.0  MNT/块   总计 5,250,000 (Y2-4)
  Era 2:  块 420,000 - 629,999 12.5  MNT/块   总计 2,625,000 (Y4-6)
  Era 3:  块 630,000 - 839,999  6.25 MNT/块   总计 1,312,500 (Y6-8)
  ...

区块大小:
  初始上限: 32 MB (与 BSV 一致)
  未来: 可通过矿工投票提升

交易费:
  最低费率: 1 sat/byte
  与 BSV 费率策略一致
```

---

## 八、带宽加权 PoW (远期优化)

```
目标: 激励矿工同时提供存储和带宽, 不只是算力。

带宽加权难度调整:
  adjusted_difficulty = base_difficulty × bandwidth_factor

  bandwidth_factor = 1 / (1 + α × proven_bandwidth / target_bandwidth)
  α = 带宽权重系数 (初始 0, 可通过矿工投票调整)

proven_bandwidth:
  - Metanet Node 在过去 N 块内的下载计费交易总量 (链上可验证)
  - 更多下载计费 = 更多带宽贡献 = 更低挖矿难度

效果:
  - α = 0: 纯 PoW (初始状态, 与 Bitcoin 相同)
  - α > 0: 提供更多 CDN 带宽的矿工获得挖矿优势
  - 渐进调整: 网络成熟后逐步提高 α

注意:
  - 这是远期优化, 初始版本使用纯 SHA256 PoW
  - 需要充分的下载计费交易量才有意义
  - 防作弊: 下载计费交易必须有对应的数据哈希和签名, 无法伪造
```

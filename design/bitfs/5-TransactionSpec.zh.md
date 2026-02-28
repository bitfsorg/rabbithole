# BitFS Transaction Specification v1.0

> **文档体系导航**: [总体设计](../0-OverallDesign.zh.md) · [概念设计](1-ConceptDesign.zh.md) · [系统设计](2-SystemDesign.zh.md) · [详细设计](3-DetailedDesign.zh.md) · [测试设计](4-TestDesign.zh.md) · **交易规范** (本文档)
>
> **状态**: DRAFT v1.1
> **日期**: 2026-02-26
> **权威性**: 本文档是 BitFS 交易结构的唯一权威参考 (Single Source of Truth)。
> 代码实现、白皮书、设计文档如有矛盾，均以本规范为准。

---

## 目录

- [第 1 层：密码学基础](#第-1-层密码学基础-cryptographic-primitives) — secp256k1, ECDH, HKDF, AES-GCM, BIP32, Argon2id
- [第 2 层：Metanet 协议](#第-2-层metanet-协议-protocol-layer) — 节点模型, 边模型, TLV 编码, 验证规则
- [第 3 层：交易结构](#第-3-层交易结构-transaction-templates) — CreateRoot, CreateChild, SelfUpdate, DataTx
- [第 4 层：加密协议 Method 42](#第-4-层加密协议-method-42) — 三种访问模式, PRIVATE 信封, Capsule 交换
- [第 5 层：文件系统操作映射](#第-5-层文件系统操作映射) — 操作→交易映射, UTXO 管理, 索引管理
- [第 6 层：命令行接口](#第-6-层命令行接口-cli) — bitfs Owner 命令, b\* Visitor 工具
- [附录 A：TLV Tag 常量表](#附录-atlv-tag-常量表)
- [附录 B：Bitcoin Script 模板](#附录-bbitcoin-script-模板)
- [附录 C：设计决策记录](#附录-c设计决策记录) — 13 项设计决策
- [附录 D：与 CSW Method 42 原始论文的关系](#附录-d与-csw-method-42-原始论文的关系)

---

## 第 1 层：密码学基础 (Cryptographic Primitives)

### 1.1 椭圆曲线 secp256k1

BitFS 使用 Bitcoin 的 secp256k1 椭圆曲线。

| 参数 | 格式 | 长度 |
|------|------|------|
| Private Key (D) | 标量 | 32 bytes |
| Public Key (P = D × G) | 压缩格式 (02/03 前缀) | 33 bytes |
| Shared Point (S = D × P) | 椭圆曲线点 | 33 bytes |
| x-coordinate (S.x) | 大端序整数 | 32 bytes |

公钥必须使用压缩格式 (compressed)。所有涉及公钥的字段长度均为 33 bytes。

### 1.2 ECDH 共享秘密 (Shared Secret)

两方通过 ECDH 计算共享秘密：

```
Alice 持有 (D_a, P_a)，Bob 持有 (D_b, P_b)

S = D_a × P_b = D_b × P_a          // 对称性
shared_secret = S.x                  // 取 x 坐标，32 bytes
```

BitFS 中 ECDH 的两种用途：
1. **自加密**：`ECDH(D_node, P_node)` — 节点使用自己的密钥对
2. **买家掩码**：`ECDH(D_seller, P_buyer)` = `ECDH(D_buyer, P_seller)` — 双方计算

### 1.3 HKDF-SHA256 密钥派生

BitFS 使用 HKDF (RFC 5869) 从 ECDH 共享秘密派生 AES 密钥。

```
derived_key = HKDF-SHA256(ikm, salt, info, length=32)
```

| 参数 | 说明 | 来源 |
|------|------|------|
| `ikm` | 输入密钥材料 | ECDH 共享秘密的 x 坐标 (32 bytes) |
| `salt` | 盐 | `key_hash` = SHA256(SHA256(plaintext)) (32 bytes) |
| `info` | 域分隔符 | 见下表 |
| `length` | 输出长度 | 固定 32 bytes (AES-256) |

**域分隔符常量表**：

| info 值 | 用途 | 常量名 |
|---------|------|--------|
| `"bitfs-file-encryption"` | 文件内容加密密钥派生 | `HKDFInfo` |
| `"bitfs-buyer-mask"` | 买家掩码密钥派生 | `HKDFBuyerMaskInfo` |
| `"bitfs-metadata-encryption"` | PRIVATE 元数据信封加密 | `HKDFMetadataInfo` |

**盐的选择**：

| 用途 | salt 值 | 来源 |
|------|---------|------|
| 文件内容加密 | `SHA256(SHA256(plaintext))` | 文件内容的双重哈希 |
| 买家掩码 | `SHA256(SHA256(plaintext))` | 同上 |
| 元数据信封加密 | `random(16B)` | 随机盐，由 `crypto/rand` 生成，存储为 EncPayload 前缀 |

> **设计决策 #5**: info 使用 `"bitfs-file-encryption"` 而非 `"bitfs-method42"`，
> 因为域分隔符应标识密钥用途而非协议名称，便于未来扩展新用途。

### 1.4 AES-256-GCM 对称加密

内容加密使用 AES-256-GCM (NIST SP 800-38D)。

| 参数 | 值 |
|------|-----|
| 密钥长度 | 32 bytes (AES-256) |
| Nonce 长度 | 12 bytes (随机生成) |
| GCM Tag 长度 | 16 bytes |
| 最小密文长度 | 28 bytes (12 + 16) |

**密文格式**：

```
ciphertext = nonce(12B) || AES-GCM-encrypted-data || tag(16B)
```

Nonce 为每次加密随机生成。由于每个文件使用不同的 AES 密钥（通过不同的 key_hash 派生），
nonce 碰撞风险可忽略。

### 1.5 BIP32 HD 密钥派生

BitFS 使用 BIP44 标准派生密钥层级。

**路径结构**：

```
m / 44' / 236' / account' / chain / index
     │      │       │          │       │
     │      │       │          │       └─ 地址序号
     │      │       │          └─ 0=外部(接收) / 1=内部(找零)
     │      │       └─ 0=手续费账户 / 1+=文件库(vault)账户
     │      └─ BitFS CoinType = 236
     └─ BIP44 标准
```

**账户分配**：

| Account | 用途 | 路径示例 |
|---------|------|---------|
| 0 | 手续费 (Fee) | `m/44'/236'/0'/0/0` |
| 1 | 默认文件库 (Vault 0) | `m/44'/236'/1'/0/0` |
| 2 | 文件库 1 (Vault 1) | `m/44'/236'/2'/0/0` |
| N+1 | 文件库 N (Vault N) | `m/44'/236'/(N+1)'/0/0` |

**文件系统节点路径**：

文件系统中的每个节点通过从 vault 根逐级派生子密钥定位：

```
Vault 根:   m/44'/236'/(V+1)'/0/0
子目录 3:   m/44'/236'/(V+1)'/0/0/3
文件 1:     m/44'/236'/(V+1)'/0/0/3/1
```

| 常量 | 值 | 说明 |
|------|-----|------|
| `PurposeBIP44` | 44 | BIP44 标准 |
| `CoinTypeBitFS` | 236 | BitFS 专用 |
| `FeeAccount` | 0 | 手续费账户 |
| `DefaultVaultAccount` | 1 | 默认文件库偏移 |
| `ExternalChain` | 0 | 外部链 |
| `InternalChain` | 1 | 内部链 |
| `Hardened` | 0x80000000 | 硬化偏移量 |
| `MaxFileIndex` | 2^31 - 1 | 非硬化索引最大值 |
| `MaxPathDepth` | 64 | 最大目录嵌套深度 |

### 1.6 Argon2id 种子加密

钱包种子文件 (`wallet.enc`) 使用 Argon2id 加密保护。

```
encrypted_seed = Argon2id(password, salt) → AES-256-GCM(seed)
```

参数详见 wallet 模块规格说明。

### 1.7 内容哈希 (key_hash)

BitFS 使用双重 SHA256 作为内容承诺和 KDF 盐：

```
key_hash = SHA256(SHA256(plaintext))
```

key_hash 的双重作用：
1. **KDF 盐**：参与 HKDF 密钥派生，确保不同文件产生不同密钥
2. **完整性校验**：解密后可验证 `SHA256(SHA256(decrypted)) == key_hash`

---

## 第 2 层：Metanet 协议 (Protocol Layer)

### 2.1 节点模型 (Node Model)

每个 Metanet 节点是一笔 BSV 交易，包含 OP_RETURN 元数据。

**节点类型 (NodeType)**：

| 值 | 名称 | 说明 |
|----|------|------|
| 0 | `File` | 普通文件 |
| 1 | `Dir` | 目录 |
| 2 | `Link` | 软链接 |

> **设计决策 #9**: 移除 `Anchor` 类型 (原值 3)。Anchor 是 git-remote-bitfs 的特定需求，
> 不属于核心文件系统规范。git-remote-bitfs 可使用扩展 TLV tag 实现。

**节点身份**：
- `P_node`：节点公钥 (33 bytes, 压缩格式)，是节点的永久身份标识
- `TxID_node`：节点交易 ID (32 bytes)，随每次 SelfUpdate 变化
- `ID_node`：节点唯一 ID = `SHA256(P_node || TxID_node)`

**操作类型 (OpType)**：

| 值 | 名称 | 说明 |
|----|------|------|
| 0 | `Create` | 创建新节点 |
| 1 | `Update` | 更新现有节点 |
| 2 | `Delete` | 标记删除 |

**访问级别 (AccessLevel)**：

| 值 | 名称 | 说明 |
|----|------|------|
| 0 | `Private` | 仅 owner 可解密 |
| 1 | `Free` | 任何人可解密（D=1） |
| 2 | `Paid` | 付费解密（通过 HTLC 获取 capsule） |

### 2.2 边模型 (Edge Model)

Metanet DAG 是**严格的树结构**：每个节点恰好有一个父节点（根节点除外）。

**Metanet 边的定义**：
- 子交易的 Input 0 花费 `P_parent` 的 UTXO
- Input 0 的 scriptSig 包含 `D_parent` 的签名
- 子交易 OP_RETURN 中的 `TxID_parent` 指向父交易

```
Parent Node (P_parent)
    │
    │── Input 0 签名: Sig(D_parent)
    │── TxID_parent 引用
    ▼
Child Node (P_child)
```

> **设计决策 #8**: Metanet DAG 为严格树，不支持多父节点。
> 因此删除 hard link（hard link 隐式要求多父节点）。
> 跨目录引用统一使用 Soft Link。

### 2.3 TLV 编码格式

Metanet 节点的 payload 使用 TLV (Tag-Length-Value) 编码。

```
Field = Tag(1B) + Length(uvarint) + Value(Length bytes)

Payload = Field₁ || Field₂ || ... || Fieldₙ
```

- **Tag**: 1 byte，标识字段类型
- **Length**: 无符号 LEB128 变长整数（1-9 bytes）
- **Value**: Length 字节的数据

**编码约定**：
- 整数字段使用 **little-endian** 字节序
- 字符串字段使用 **UTF-8** 编码
- 布尔字段编码为 uint32：0=false, 1=true
- 未知 tag 必须跳过（前向兼容）

完整 tag 列表见 [附录 A](#附录-atlv-tag-常量表)。

### 2.4 ChildEntry 序列化

目录节点的子节点列表通过 `ChildEntry` 结构序列化，使用 tag `0x0E`。
每个 ChildEntry 独立编码为一个 TLV 字段（tag 可重复出现）。

**二进制格式**：

```
Offset        Type      Size      Field
─────────────────────────────────────────
0-3           uint32    4B        Index (little-endian)
4-5           uint16    2B        Name length (little-endian)
6..6+N-1      string    N bytes   Name (UTF-8)
6+N..9+N      uint32    4B        Type (NodeType, little-endian)
10+N          uint8     1B        PubKey length
11+N..10+N+K  bytes     K bytes   PubKey (compressed, usually 33B)
11+N+K        uint8     1B        Hardened flag (0 or 1)
```

**字段说明**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `Index` | uint32 | 子节点在父目录中的 BIP32 派生索引 |
| `Name` | string | 文件/目录名 (最大 255 bytes) |
| `Type` | NodeType | File(0) / Dir(1) / Link(2) |
| `PubKey` | bytes | 子节点的 P_node (33 bytes 压缩公钥) |
| `Hardened` | bool | 是否使用硬化 BIP32 派生 |

> `MaxChildNameLen = 255`

### 2.5 目录 Merkle Root

目录节点包含一个 `MerkleRoot` 字段 (tag `0x1A`)，是其所有 ChildEntry 的 Merkle 树根。

**计算方法** (Bitcoin-style 双哈希)：

```
1. 对每个 ChildEntry 计算叶子哈希:
   leaf_hash = SHA256d(serialize(ChildEntry))

2. 构建 Merkle 树:
   - 将叶子哈希两两配对
   - parent = SHA256d(left || right)
   - 奇数个叶子时，复制最后一个

3. 树根即为 MerkleRoot (32 bytes)
```

其中 `SHA256d(x) = SHA256(SHA256(x))`。

**用途**：
- 目录完整性验证
- 轻量级子节点存在性证明 (membership proof)

每次目录发生变更（AddChild / RemoveChild / RenameChild）时，MerkleRoot 自动重新计算。

### 2.6 链接语义 (Link Semantics)

**LinkType 定义**：

| 值 | 名称 | 说明 |
|----|------|------|
| 0 | `Soft` | 目标在同一 vault 内 |
| 1 | `SoftRemote` | 目标在其他 vault 或其他用户 |

Link 节点的 `LinkTarget` 字段 (tag `0x09`) 存储目标节点的 `P_node`。

**链接与路径解析**：

| 常量 | 值 | 说明 |
|------|-----|------|
| `MaxLinkDepth` | 10 | 单条软链接链的最大跟随次数 |
| `MaxTotalLinkFollows` | 40 | 单次路径解析中所有链接跟随次数之和 |
| `MaxPathComponents` | 256 | 路径中最大组件数 |

- 解析时跟随 `LinkTarget` 找到目标节点
- 如果目标也是 Link，递归解析（受 `MaxLinkDepth` 限制）
- `SoftRemote` 链接需要通过网络请求远程 daemon 解析
- 路径中的 `.` 表示当前目录（跳过），`..` 返回父目录（不可超过根节点）

---

### 2.7 协议验证规则

实现必须遵守以下验证规则。未通过验证的交易或节点应被拒绝。

#### 交易结构验证

1. OP_RETURN 必须包含恰好 **4 个 push data**（MetaFlag, P_node, TxID_parent, Payload）
2. Push[0] (MetaFlag) 必须等于 `0x6d657461`
3. Push[1] (P_node) 必须为 33 字节压缩公钥，且为有效 secp256k1 曲线点
4. Push[2] (TxID_parent) 必须为 0 字节（根节点）或 32 字节
5. Push[3] (Payload) 不得为空
6. CreateChild: Input 0 必须由 `D_parent` 签名（建立 Metanet 边）
7. SelfUpdate: Input 0 必须由 `D_node` 签名（证明节点所有权）
8. 所有 P2PKH 输出金额 ≥ `DustLimit` (1 sat)
9. 找零金额 < `DustLimit` 时，不创建找零输出

#### TLV 编码验证

10. 未知 tag 必须跳过，不得报错（前向兼容）
11. 固定长度字段（uint32=4B, uint64=8B）长度不匹配时忽略该字段
12. ChildEntry 二进制格式必须严格遵循 §2.4 定义
13. ChildEntry 的 PubKey 长度必须为 33 字节
14. ChildEntry 的 Name 长度不得超过 255 字节

#### 文件系统约束

15. 子节点名称禁止字符：`/`、`\x00`、控制字符 (Unicode IsControl)、格式字符 (Unicode Cf)
16. 保留名称 `.` 和 `..` 不可用作子节点名称
17. 同一目录下子节点名称不得重复
18. 子索引 (NextChildIndex) 单调递增，删除后**不重用**
19. 软链接解析深度限制：单链 ≤ 10 次，全路径 ≤ 40 次
20. 路径组件数上限 256

#### SPV 验证

21. 区块头必须为 80 字节
22. PoW 验证：`DoubleHash(header) ≤ CompactToTarget(header.Bits)`
23. 头链连续性：`header[i].PrevBlock == Hash(header[i-1])`
24. Merkle 证明：`ComputeMerkleRoot(TxID, index, proofNodes) == header.MerkleRoot`
25. Merkle 成员性证明使用**常数时间比较** (constant-time compare) 防止时序攻击

---

## 第 3 层：交易结构 (Transaction Templates)

### 3.0 通用常量

| 常量 | 值 | 说明 |
|------|-----|------|
| `MetaFlag` | `0x6d657461` ("meta") | OP_RETURN 协议标识 |
| `DustLimit` | 1 satoshi | 最小 P2PKH 输出金额 |
| `DefaultFeeRate` | 1 sat/KB | 默认手续费率 |

> BSV 已移除 dust limit 限制。P2PKH 输出可持有任意金额（≥1 sat）。

### 3.1 OP_RETURN 格式

所有 Metanet 交易的 Output 0 为 OP_RETURN，包含 4 个 push data：

```
OP_FALSE OP_RETURN
  Push[0]: MetaFlag         (4 bytes: 0x6d657461)
  Push[1]: P_node           (33 bytes: 压缩公钥)
  Push[2]: TxID_parent      (0 bytes=根节点 | 32 bytes=子/更新节点)
  Push[3]: Payload          (variable: TLV 编码)
```

- `OP_FALSE OP_RETURN` 确保输出不可花费 (provably unspendable)
- `Push[2]` 为空 (0 bytes) 表示根节点（无父节点）

### 3.2 Template 1: CreateRoot (创建根节点)

创建 vault 的根节点，无父节点。

```
Inputs:
  [0] FeeUTXO              签名: Sig(D_fee)

Outputs:
  [0] OP_FALSE OP_RETURN <MetaFlag> <P_node> <empty> <Payload>    (0 sat)
  [1] P2PKH → P_node                                              (1 sat, NodeUTXO)
  [2] P2PKH → ChangeAddr                                          (余额, ChangeUTXO)
```

**产出 UTXO**：
- `NodeUTXO` = Output 1 (Vout=1)，用于后续 SelfUpdate 或 CreateChild 的 parent
- `ChangeUTXO` = Output 2 (Vout=2)，找零（金额低于 DustLimit 时省略）

### 3.3 Template 2: CreateChild (创建子节点)

创建子节点，建立 Metanet 边。

```
Inputs:
  [0] ParentUTXO            签名: Sig(D_parent)  ← Metanet 边
  [1] FeeUTXO               签名: Sig(D_fee)

Outputs:
  [0] OP_FALSE OP_RETURN <MetaFlag> <P_child> <TxID_parent> <Payload>    (0 sat)
  [1] P2PKH → P_child                                                    (1 sat, NodeUTXO)
  [2] P2PKH → P_parent                                                   (1 sat, ParentUTXO 刷新)
  [3] P2PKH → ChangeAddr                                                 (余额, ChangeUTXO)
```

**关键语义**：
- Input 0 花费父节点的 UTXO，由 `D_parent` 签名 → 建立 Metanet 边
- Output 2 将 1 sat 返还给 `P_parent` → **UTXO 刷新**（自维持链）
- `TxID_parent` = 父节点的交易 ID

**UTXO 刷新机制**：
```
CreateRoot  → Output 1 (NodeUTXO for P_root)
                │
CreateChild₁ → Input 0 花费 P_root 的 NodeUTXO
              → Output 2 产生新的 P_root UTXO (刷新)
                │
CreateChild₂ → Input 0 花费 P_root 的刷新 UTXO
              → Output 2 再次刷新
              → ... (可无限延续)
```

### 3.4 Template 3: SelfUpdate (自更新)

更新现有节点的元数据，P_node 不变。

```
Inputs:
  [0] NodeUTXO              签名: Sig(D_node)
  [1] FeeUTXO               签名: Sig(D_fee)

Outputs:
  [0] OP_FALSE OP_RETURN <MetaFlag> <P_node> <TxID_parent> <Payload>    (0 sat)
  [1] P2PKH → P_node                                                    (1 sat, NodeUTXO 刷新)
  [2] P2PKH → ChangeAddr                                                (余额, ChangeUTXO)
```

**关键语义**：
- `TxID_parent` 保持不变（从创建时确定，不随更新改变）
- `TxID_node` 随每次 SelfUpdate 变化 → 实现版本控制
- 同一 `P_node` 的最新交易为当前版本

### 3.5 Template 4: DataTx (链上内容存储)

将加密内容嵌入区块链交易。

```
Inputs:
  [0] SourceUTXO             签名: Sig(D_node)

Outputs:
  [0] <content> OP_DROP OP_DUP OP_HASH160 <H160(P_node)> OP_EQUALVERIFY OP_CHECKSIG
  [1] P2PKH → ChangeAddr                                                (余额)
```

**Output 0 脚本说明**：
- `<content>` 作为 push data 嵌入脚本
- `OP_DROP` 丢弃栈顶的 content（不参与验证）
- 剩余部分是标准 P2PKH，输出锁定给 `P_node`
- 这使得内容永久记录在链上，同时 UTXO 可正常花费

**内容关联**：
- Metanet 节点通过 `OnChain=true` (tag `0x14`) 和 `ContentTxIDs` (tag `0x15`) 关联 DataTx
- 大文件可拆分为多个 DataTx，通过 `Compression`、`ChunkIndex`、`TotalChunks` 等字段管理

### 3.6 费用计算

**交易大小估算**：

```
size = 10                                    // 版本(4B) + locktime(4B) + 计数(2B)
     + numInputs × 148                      // 每个 P2PKH input ~148B
     + numOutputs × 34                      // 每个 P2PKH output ~34B
     + 13 + pushdata_overhead               // OP_RETURN 基础
     + 4 + 33 + len(TxID_parent) + payloadSize  // OP_RETURN 数据
```

**手续费计算**：

```
fee = ⌈size × feeRate / 1000⌉

其中 feeRate 单位为 sat/KB，默认值 = 1
```

**找零规则**：
- 如果找零金额 < `DustLimit` (1 sat)，不创建找零输出
- 差额归入矿工手续费

---

## 第 4 层：加密协议 Method 42

### 4.1 三种访问模式

BitFS 改进了 CSW 的 Method 42 方案，使用 HKDF 标准化密钥派生。

#### AccessPrivate (0) — 仅 Owner 可解密

```
shared_secret = ECDH(D_node, P_node).x      // 自身密钥对 ECDH
aes_key = HKDF-SHA256(
    ikm  = shared_secret,                    // 32 bytes
    salt = key_hash,                          // SHA256(SHA256(plaintext))
    info = "bitfs-file-encryption"
)
```

只有持有 `D_node` 的 owner 能计算 `shared_secret`。

#### AccessFree (1) — 任何人可解密

```
shared_secret = ECDH(1, P_node).x            // 使用标量 1 作为私钥
              = P_node.x                      // 等价于公钥的 x 坐标
aes_key = HKDF-SHA256(
    ikm  = P_node.x,                         // 任何人都能从 P_node 计算
    salt = key_hash,
    info = "bitfs-file-encryption"
)
```

`P_node` 通过 DNSLink 或链上元数据公开，任何人都能解密。

#### AccessPaid (2) — 付费解密

加密方式与 AccessPrivate 相同：

```
aes_key = HKDF-SHA256(ECDH(D_node, P_node).x, key_hash, "bitfs-file-encryption")
```

买家通过 HTLC 原子交换获取 capsule（见 §4.4）。

### 4.2 文件加密流程

```
                    plaintext
                       │
                       ▼
              SHA256(SHA256(·))
                       │
                       ▼
                   key_hash ──────────────────┐
                       │                      │
                       ▼                      ▼
    ECDH(D_node, P_node).x ──→ HKDF ──→ aes_key
                                              │
                                              ▼
                      random(12B) ──→ AES-256-GCM.Seal(plaintext)
                                              │
                                              ▼
                                    nonce(12B) || ciphertext || tag(16B)
```

**输出**：
- `ciphertext`: 加密后的密文 (含 nonce + tag)
- `key_hash`: 32 bytes，写入 Metanet 节点的 tag `0x06`

### 4.3 PRIVATE 模式信封加密

当节点为 PRIVATE 模式 (`Encrypted=true`)，整个 TLV payload 使用独立的元数据加密密钥加密。

#### 4.3.1 元数据加密密钥

PRIVATE 模式使用**两层密钥**：元数据加密密钥（解密 TLV payload）和文件加密密钥（解密文件内容）。
两者使用不同的 HKDF info 和 salt，实现密钥隔离。

**元数据加密密钥**：

```
salt = random(16B)                            // 由 crypto/rand 生成
metadata_key = HKDF-SHA256(
    ikm  = ECDH(D_node, P_node).x,          // 自身密钥对 ECDH，32 bytes
    salt = salt,                              // 随机盐，16 bytes
    info = "bitfs-metadata-encryption"        // 元数据域分隔符
)
```

盐使用随机 16 字节而非 `key_hash` 或确定性值，因为：
- `key_hash` 位于加密 payload 内部，加密前不可知
- 随机盐避免了确定性加密（同一节点每次更新产生不同密文），提高安全性
- 随机盐作为 EncPayload 的前缀存储，owner 解密时先读取 salt 再派生密钥

#### 4.3.2 信封格式

```
加密前的 Payload:
  version, type, op, mime_type, file_size, key_hash, access, children, ...

加密后的 OP_RETURN Payload（仅 2 个 TLV 字段）:
  tag 0x13: encrypted = true
  tag 0x1B: enc_payload = salt(16B) || nonce(12B) || AES-GCM(原始 Payload, metadata_key) || tag(16B)
```

**链上可见信息**：

| 字段 | 位置 | 可见性 |
|------|------|--------|
| `P_node` | OP_RETURN Push[1] | 明文（节点身份） |
| `TxID_parent` | OP_RETURN Push[2] | 明文（父节点引用） |
| `encrypted` | TLV tag 0x13 | 明文（标志位） |
| `enc_payload` | TLV tag 0x1B | 密文 |

所有元数据（文件名、类型、大小、key_hash、子节点列表）均在密文内部，链上不可见。

#### 4.3.3 两层解密流程

Owner 解密 PRIVATE 文件的完整流程：

```
步骤 1: 解密元数据
  salt         = enc_payload[:16]               // 读取前 16 字节随机盐
  metadata_key = HKDF(ECDH(D_node, P_node).x, salt, "bitfs-metadata-encryption")
  raw_payload  = AES-GCM.Open(enc_payload[16:], metadata_key)

步骤 2: 从解密后的 TLV 中提取 key_hash
  key_hash = raw_payload.KeyHash                  // tag 0x06

步骤 3: 计算文件加密密钥
  aes_key = HKDF(ECDH(D_node, P_node).x, key_hash, "bitfs-file-encryption")

步骤 4: 解密文件内容
  plaintext = AES-GCM.Open(ciphertext, aes_key)

步骤 5: 完整性校验
  assert SHA256(SHA256(plaintext)) == key_hash
```

#### 4.3.4 钱包恢复算法

恢复依赖两个性质：
1. **P_node 始终明文**：OP_RETURN 中的 P_node 不受加密影响
2. **树结构自描述**：目录节点解密后，ChildEntry 包含子节点的 P_node 和 BIP32 索引

**Phase 1: Vault 发现**

```
GAP_LIMIT = 20

for account = 1, 2, 3, ...:
    P_root = PubKey(m/44'/236'/account'/0/0)
    搜索区块链: OP_RETURN 中 P_node == P_root 的交易
    if found:
        discovered_vaults.append(account)
        gap_count = 0
    else:
        gap_count++
    if gap_count >= GAP_LIMIT:
        break
```

类似 BIP44 标准的地址发现：连续 20 个未使用的 account 后停止扫描。

**Phase 2: 树重建**

```
for each discovered vault (account):
    D_root = PrivKey(m/44'/236'/account'/0/0)
    P_root = PubKey(D_root)

    // 找到根节点最新交易
    root_tx = 查找 P_root 的最新 Metanet 交易

    // 解密根节点元数据
    salt = root_tx.enc_payload[:16]               // 读取前 16 字节随机盐
    metadata_key = HKDF(ECDH(D_root, P_root).x, salt, "bitfs-metadata-encryption")
    root_payload = AES-GCM.Open(root_tx.enc_payload[16:], metadata_key)

    // 从 ChildEntry 列表递归恢复
    for each child in root_payload.Children:
        // child 包含: Index, Name, Type, PubKey, Hardened
        D_child = BIP32_Derive(D_root, child.Index, child.Hardened)
        child_tx = 查找 child.PubKey 的最新 Metanet 交易
        解密子节点 metadata (同上方法)
        if child.Type == Dir:
            递归处理子目录
```

**恢复性质**：

| 性质 | 说明 |
|------|------|
| 仅需助记词 | 不依赖任何链外数据 |
| 确定性 | 同一助记词始终恢复相同的文件树 |
| 无需明文索引 | 不在链上泄露 file_index 或 key_hash |
| 增量扫描 | 只需扫描 vault 根公钥（有限个），子树通过递归解密发现 |

**不可恢复的数据**（需用户备份）：

| 数据 | 原因 |
|------|------|
| `~/.bitfs/storage/` | 本地缓存的解密文件内容 |
| `~/.bitfs/txstore/` | 交易数据 + Merkle proof（可从网络重新获取） |
| `~/.bitfs/headers/` | 区块头链（可从网络重新同步） |
| UTXO 状态 | 可通过扫描恢复后的交易重建 |

> **设计决策 #10** (已解决): 不存储明文 `key_hash` / `file_index`。
> 通过独立的元数据加密密钥（`info="bitfs-metadata-encryption"`, `salt=random(16B)`）
> 实现 PRIVATE 信封加密，随机盐存储为 EncPayload 前缀。配合 BIP32 确定性派生 + 目录 ChildEntry 递归解密实现恢复。

### 4.4 Capsule 交换 (Paid 模式)

Paid 模式下，买家通过 HTLC 获取解密能力。

**Capsule 计算 (Seller 端)**：

```
buyer_mask = HKDF-SHA256(
    ikm  = ECDH(D_seller, P_buyer).x,
    salt = key_hash,
    info = "bitfs-buyer-mask"
)

capsule = aes_key XOR buyer_mask             // 32 bytes
capsule_hash = SHA256(capsule)               // HTLC 锁
```

**Capsule 恢复 (Buyer 端)**：

```
buyer_mask = HKDF-SHA256(
    ikm  = ECDH(D_buyer, P_seller).x,        // ECDH 对称性
    salt = key_hash,
    info = "bitfs-buyer-mask"
)

aes_key = capsule XOR buyer_mask             // 恢复加密密钥
```

**安全性**：
- 每个 buyer 获得唯一的 capsule（因为 buyer_mask 与 P_buyer 相关）
- capsule 在 HTLC claim 时链上公开，但只有对应 buyer 能恢复 aes_key
- Seller 不需要暴露 D_node，只需 D_seller（可以是不同密钥）

---

## 第 5 层：文件系统操作映射

### 5.1 操作到交易的映射

| 操作 | 交易序列 | 说明 |
|------|---------|------|
| `put` (新文件) | CreateChild + SelfUpdate(parent) | 创建文件节点 + 更新父目录 children |
| `put` (更新) | SelfUpdate | 同一 P_node，新 TxID = 新版本 |
| `mkdir` | CreateChild + SelfUpdate(parent) | Type=Dir，空 children |
| `rm` | SelfUpdate(parent) | 从父目录 children 中移除 ChildEntry |
| `mv` (同目录) | SelfUpdate(parent) | 修改 ChildEntry.Name |
| `mv` (跨目录) | SelfUpdate(src_parent) + SelfUpdate(dst_parent) | 两个目录各更新一次 |
| `cp` | CreateChild + SelfUpdate(parent) | 新 P_node，重新加密 |
| `link -s` (软链接) | CreateChild + SelfUpdate(parent) | Type=Link，设置 LinkTarget |

### 5.2 UTXO 状态管理

BitFS 管理三类 UTXO：

| UTXO 类型 | 来源 | 用途 | 生命周期 |
|-----------|------|------|---------|
| `NodeUTXO` | Template 1/2/3 的 Output 1 | SelfUpdate 的 Input 0 | 创建 → 下次 SelfUpdate 花费 |
| `ParentUTXO` | Template 2 的 Output 2 | CreateChild 的 Input 0 | 创建 → 下次 CreateChild 花费 → 刷新 |
| `FeeUTXO` | 外部充值或找零 | 所有交易的手续费 Input | 充值 → 花费（找零产生新 FeeUTXO） |

**UTXO 结构**：

```
UTXO {
    TxID:         [32]byte      // 交易 ID
    Vout:         uint32        // 输出索引
    Amount:       uint64        // 金额 (satoshis)
    ScriptPubKey: []byte        // 锁定脚本
    PrivateKey:   *PrivateKey   // 签名密钥 (不序列化)
}
```

### 5.3 版本控制

同一 P_node 的多笔交易形成版本链：

```
P_node (不变)
    │
    ├── TxID_v1 (CreateChild) ← 首个版本
    ├── TxID_v2 (SelfUpdate)  ← 第二版本
    └── TxID_v3 (SelfUpdate)  ← 当前版本 (最新交易)
```

最新版本 = 使用相同 P_node 的最近一笔未花费交易。

### 5.4 子索引管理

每个目录维护 `NextChildIndex` 计数器（tag `0x0F`），用于为新建子节点分配 BIP32 派生索引。

**初始值**：1（索引 0 保留）

**分配规则**：

| 操作 | 索引行为 |
|------|---------|
| `mkdir` / `put` (新建) | 分配 `NextChildIndex`，然后递增 |
| `rm` (删除) | 从 ChildEntry 列表移除，`NextChildIndex` **不减少** |
| `mv` (同目录) | 保留原索引，仅修改 `ChildEntry.Name` |
| `mv` (跨目录) | 源目录移除 ChildEntry；目标目录分配新索引 |
| `cp` (复制) | 目标目录分配新索引 + 新 BIP32 密钥对 |

**索引不重用的原因**：

如果新文件获得已删除文件的索引，它将通过 BIP32 派生出**相同的密钥对**。
这意味着：
- 新文件的 `P_node` 与已删除文件相同
- 区块链上无法区分两者的 Metanet 交易
- 旧文件的加密内容可能被新密钥持有者解密

因此，已分配的索引永远不回收。这确保每个文件获得唯一的密钥对和链上身份。

### 5.5 目录变更的原子性

单个文件系统操作可能涉及多笔交易。例如：

| 操作 | 交易数 | 原子性保证 |
|------|--------|-----------|
| `put` (新文件) | 2 (CreateChild + SelfUpdate) | 两笔交易同时广播 |
| `mv` (跨目录) | 2 (SelfUpdate×2) | 两笔交易同时广播 |
| `rm` | 1 (SelfUpdate) | 单笔交易，天然原子 |
| `mv` (同目录) | 1 (SelfUpdate) | 单笔交易，天然原子 |

多交易操作应在同一批次中广播，但 BSV 无跨交易原子性保证。
如果部分广播失败（例如 CreateChild 成功但 SelfUpdate 失败），
系统通过**幂等重试**恢复：检测已有子节点 UTXO，仅重试失败的交易。

---

## 第 6 层：命令行接口 (CLI)

BitFS 提供两类命令行工具：

- **bitfs** — Owner 工具：管理自己的 vault、文件、钱包、daemon
- **b\* tools** — Visitor 工具：只读访问他人的文件系统（通过 daemon HTTP）

### 6.1 通用约定

**全局 flags**（适用于所有 bitfs 子命令）：

| Flag | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `-datadir` | string | `~/.bitfs/` | 数据目录 |
| `-password` | string | (交互提示) | 钱包密码（仅用于测试/脚本） |
| `-vault` | string | (默认 vault) | 目标文件库名称 |

**bitfs URI 格式**（b\* 工具使用）：

| 格式 | 示例 | 说明 |
|------|------|------|
| 域名 | `bitfs://example.com/docs/file.txt` | 通过 DNS TXT 解析 P_node |
| Paymail | `bitfs://alice@example.com/docs/` | 通过 Paymail 协议解析 |
| 直接公钥 | `bitfs://02abc...66hex.../path` | 需配合 `--host` 指定 daemon |

**退出码**：

| 码 | 含义 |
|----|------|
| 0 | 成功 |
| 1 | 一般错误 |
| 2 | 用法错误 / 未找到 |
| 3 | 钱包错误 |
| 4 | 网络/超时错误 |
| 5 | 权限/支付错误 |
| 6 | 未找到 |
| 7 | 冲突 |

### 6.2 bitfs — Owner 命令

#### 6.2.1 钱包管理 (wallet)

```
bitfs wallet init [-words 12|24] [-network mainnet|testnet|regtest]
bitfs wallet show
bitfs wallet balance [-refresh]
bitfs wallet fund
```

| 命令 | 说明 |
|------|------|
| `init` | 创建新 HD 钱包（BIP39 助记词） |
| `show` | 显示钱包公钥和地址信息 |
| `balance` | 查询余额（`-refresh` 从网络同步） |
| `fund` | 显示充值地址和二维码 |

| Flag | 适用 | 类型 | 默认值 | 说明 |
|------|------|------|--------|------|
| `-words` | init | int | 12 | 助记词长度 (12 或 24) |
| `-network` | init | string | mainnet | 网络: mainnet/testnet/regtest |
| `-refresh` | balance | bool | false | 从网络同步余额 |

#### 6.2.2 文件库管理 (vault)

```
bitfs vault create <name>
bitfs vault list
bitfs vault rename <old-name> <new-name>
bitfs vault delete <name>
```

#### 6.2.3 文件读取

```
bitfs cat <path> [-force]
bitfs get <remote-path> [local-path]
bitfs mget <remote-dir> [local-dir]
```

| 命令 | 说明 |
|------|------|
| `cat` | 输出文件内容到 stdout |
| `get` | 下载单个文件到本地 |
| `mget` | 递归下载目录 |

| Flag | 适用 | 类型 | 默认值 | 说明 |
|------|------|------|--------|------|
| `-force` | cat | bool | false | 强制输出二进制文件（不提示警告） |

#### 6.2.4 文件写入

```
bitfs put <local-file> <remote-path> [-access free|private]
bitfs mput <local-dir> [remote-dir] [-access free|private]
bitfs mkdir <path>
bitfs rm <path>
```

| Flag | 适用 | 类型 | 默认值 | 说明 |
|------|------|------|--------|------|
| `-access` | put, mput | string | `free` | 访问模式: free / private |

`put` 额外支持的 engine 参数（未暴露为 CLI flag，供 shell/API 使用）：

| 参数 | 类型 | 说明 |
|------|------|------|
| `Keywords` | string | 逗号分隔的搜索关键词 |
| `Description` | string | 文件描述 |
| `Domain` | string | 关联域名 |
| `OnChain` | bool | 是否将内容存储到链上 (DataTx) |
| `Compression` | int32 | 压缩算法: None(0) / Gzip(1) |

#### 6.2.5 文件管理

```
bitfs cp <src-path> <dst-path>
bitfs mv <src-path> <dst-path>
bitfs link -s <target-path> <link-path>
```

| 命令 | 交易 | 说明 |
|------|------|------|
| `cp` | CreateChild + SelfUpdate(parent) | 创建独立副本，新密钥对，重新加密 |
| `mv` (同目录) | SelfUpdate(parent) | 重命名 ChildEntry |
| `mv` (跨目录) | SelfUpdate(src) + SelfUpdate(dst) | 两个目录各更新 |
| `link -s` | CreateChild + SelfUpdate(parent) | 创建 Soft Link 节点 |

> **设计决策 #8**: 只支持 `-s` (soft link)，不支持 hard link。

#### 6.2.6 内容交易

```
bitfs sell <path> --price <sats/KB>
bitfs encrypt <path>
```

| 命令 | 说明 |
|------|------|
| `sell` | 将文件设为 PAID 模式并设定价格（通过 SelfUpdate） |
| `encrypt` | 将 FREE 文件转为 PRIVATE（重新加密） |

#### 6.2.7 发布

```
bitfs publish [domain]          # 无参数 = 列出所有绑定
bitfs unpublish <domain>
```

发布将 vault 根节点绑定到 DNS 域名。用户需在 DNS 中添加 TXT 记录：

```
_bitfs.example.com  TXT  "bitfs=<hex_pubkey>"
```

#### 6.2.8 Daemon

```
bitfs daemon start [-listen :8080] [-network regtest] [-rpc-url URL] [-rpc-user USER] [-rpc-pass PASS]
bitfs daemon stop
```

Daemon 提供：
- LFCP HTTP 内容服务（供 b\* 工具访问）
- Metanet 元数据 API
- Method 42 握手和 x402 支付

#### 6.2.9 Shell (交互式 REPL)

```
bitfs shell [-vault NAME]
```

进入交互式 shell，支持以下命令（与 CLI 子命令对应）：

```
bitfs> ls [path]              # 列出目录
bitfs> cd [path]              # 切换远程目录
bitfs> lcd [path]             # 切换本地目录
bitfs> pwd                    # 显示远程工作目录
bitfs> cat <path>             # 查看文件
bitfs> get <remote> [local]   # 下载文件
bitfs> mget <dir> [local]     # 下载目录
bitfs> mput <dir> [remote]    # 上传目录
bitfs> mkdir <path>           # 创建目录
bitfs> put <local> <remote>   # 上传文件
bitfs> rm <path>              # 删除
bitfs> mv <src> <dst>         # 移动/重命名
bitfs> cp <src> <dst>         # 复制
bitfs> link -s <target> <path> # 创建软链接
bitfs> sell <path> <price>    # 设定价格
bitfs> sales [path]           # 销售历史
bitfs> encrypt <path>         # 加密
bitfs> decrypt <path>         # 解密
bitfs> publish [domain]       # 发布
bitfs> unpublish <domain>     # 取消发布
bitfs> help                   # 帮助
bitfs> quit / exit            # 退出
```

Shell 特有行为：
- 支持路径补全 (Tab)
- `cd` 维护远程工作目录状态，路径可使用相对路径
- `lcd` 维护本地工作目录状态
- Shell 命令中 `sell` 的参数顺序为 `<path> <price>`（无 `--price` flag）
- 历史记录保存至 `~/.bitfs/shell_history`（权限 0600）

### 6.3 b\* — Visitor 只读工具

b\* 工具通过 HTTP 连接 owner 的 daemon，无需钱包即可使用（除购买外）。

**通用 flags**：

| Flag | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--host` | string | (从 URI 解析) | Daemon URL 覆盖 |
| `--timeout` | duration | (无超时) | 请求超时，如 `10s`、`1m` |
| `--json` | bool | false | JSON 格式输出 |

#### 6.3.1 bls — 列出目录

```
bls [--long|-l] [--json] <bitfs-uri>
```

| Flag | 类型 | 说明 |
|------|------|------|
| `--long` / `-l` | bool | 详细列表（type, access, size, name） |

输出格式：
- 默认：每行一个文件名
- `--long`：制表符分隔的 `type  access  size  name`
- `--json`：JSON 数组

#### 6.3.2 bcat — 查看文件内容

```
bcat [--buy] [--verify] [--wallet-key KEY] [--utxo SPEC] <bitfs-uri>
```

| Flag | 类型 | 说明 |
|------|------|------|
| `--buy` | bool | 购买付费内容 |
| `--verify` | bool | SPV 验证 Metanet 交易 |
| `--wallet-key` | string | 买家私钥 (32 bytes hex) |
| `--utxo` | string | 购买用 UTXO (`txid:vout:amount`) |

#### 6.3.3 bget — 下载文件

```
bget [-o FILE] [--buy] [--verify] [--wallet-key KEY] [--utxo SPEC] <bitfs-uri>
```

| Flag | 类型 | 说明 |
|------|------|------|
| `-o` / `--output` | string | 输出文件名（默认从 URI 推断） |

其他 flags 同 bcat。

#### 6.3.4 bstat — 文件元数据

```
bstat [--json] <bitfs-uri>
```

输出：Path, Type, Owner (P_node), Access, MIME, Size, KeyHash, Price, TxID

#### 6.3.5 btree — 目录树

```
btree [-d|--depth N] [--json] <bitfs-uri>
```

| Flag | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `-d` / `--depth` | int | 0 (无限) | 最大展示深度 |

#### 6.3.6 bmget — 批量下载

```
bmget [--concurrency N] [--fail-fast] [--buy] [--wallet-key KEY] [--utxo SPEC] <bitfs-uri> [local-dir]
```

| Flag | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--concurrency` | int | 4 | 最大并发下载数 |
| `--fail-fast` | bool | false | 首个错误即停止 |

### 6.4 工具关系图

```
┌─────────────────────────────────────────────────────┐
│  Owner Layer (需要钱包)                               │
│                                                     │
│  bitfs CLI ──┐                                      │
│  bitfs shell ─┤──→ Engine Layer ──→ libbitfs-go      │
│               │                      ↓              │
│               └──→ Daemon (HTTP Server)              │
└─────────────────────────────────────────────────────┘
                           ↕ HTTP (LFCP / x402)
┌─────────────────────────────────────────────────────┐
│  Visitor Layer (无需钱包，除购买外)                     │
│                                                     │
│  bls ─────┐                                         │
│  bcat ────┤                                         │
│  bget ────┤──→ HTTP Client ──→ Daemon               │
│  bstat ───┤                                         │
│  btree ───┤                                         │
│  bmget ───┘                                         │
└─────────────────────────────────────────────────────┘
```

---

## 附录 A：TLV Tag 常量表

| Tag (hex) | Tag (dec) | 常量名 | 类型 | 说明 |
|-----------|-----------|--------|------|------|
| `0x01` | 1 | `tagVersion` | uint32 | 协议版本 |
| `0x02` | 2 | `tagType` | uint32 | 节点类型: File(0)/Dir(1)/Link(2)/Anchor(3) |
| `0x03` | 3 | `tagOp` | uint32 | 操作: Create(0)/Update(1)/Delete(2) |
| `0x04` | 4 | `tagMimeType` | string | MIME 类型 |
| `0x05` | 5 | `tagFileSize` | uint64 | 文件大小 (明文，bytes) |
| `0x06` | 6 | `tagKeyHash` | bytes | SHA256(SHA256(plaintext))，32B |
| `0x07` | 7 | `tagAccess` | uint32 | 访问级别: Private(0)/Free(1)/Paid(2) |
| `0x08` | 8 | `tagPricePerKB` | uint64 | 价格 (satoshis/KB)，Paid 模式 |
| `0x09` | 9 | `tagLinkTarget` | bytes | 软链接目标 P_node，33B |
| `0x0A` | 10 | `tagLinkType` | uint32 | 链接类型: Soft(0)/SoftRemote(1) |
| `0x0B` | 11 | `tagTimestamp` | uint64 | Unix 时间戳 (秒) |
| `0x0C` | 12 | `tagParent` | bytes | 父节点 P_node |
| `0x0D` | 13 | `tagIndex` | uint32 | 节点在父目录中的索引 |
| `0x0E` | 14 | `tagChildEntry` | binary | ChildEntry 序列化 (可重复) |
| `0x0F` | 15 | `tagNextChildIndex` | uint32 | 下一个可用子索引 |
| `0x10` | 16 | `tagDomain` | string | DNS 域名绑定 |
| `0x11` | 17 | `tagKeywords` | string | 搜索关键词 |
| `0x12` | 18 | `tagDescription` | string | 文件描述 |
| `0x13` | 19 | `tagEncrypted` | uint32 | 是否加密: 0=否, 1=是 |
| `0x14` | 20 | `tagOnChain` | uint32 | 内容是否在链上: 0=否, 1=是 |
| `0x15` | 21 | `tagContentTxID` | bytes | DataTx 的 TxID (可重复) |
| `0x16` | 22 | `tagCompression` | uint32 | 压缩算法: None(0)/Gzip(1) |
| `0x17` | 23 | `tagCltvHeight` | uint32 | CLTV 时间锁区块高度 |
| `0x18` | 24 | `tagRevenueShare` | uint32 | 收入分成比例 (0-10000 = 0-100%) |
| `0x19` | 25 | `tagNetworkName` | string | 网络名称 |
| `0x1A` | 26 | `tagMerkleRoot` | bytes | 目录 Merkle 根，32B |
| `0x1B` | 27 | `tagEncPayload` | bytes | 加密 payload (PRIVATE 模式) |

**扩展字段** (0x1E-0x1F, 0x27-0x30)：

| Tag (hex) | Tag (dec) | 常量名 | 类型 | 说明 |
|-----------|-----------|--------|------|------|
| `0x1E` | 30 | `tagMetadata` | map | 自定义键值对 (子 TLV 编码) |
| `0x1F` | 31 | `tagVersionLog` | bytes | 版本日志节点 P_node，33B |
| `0x27` | 39 | `tagShareList` | bytes | 份额列表节点 P_node，33B |
| `0x28` | 40 | `tagChunkIndex` | uint32 | 分片索引 (0-based) |
| `0x29` | 41 | `tagTotalChunks` | uint32 | 总分片数 |
| `0x2A` | 42 | `tagRecombinationHash` | bytes | SHA256(chunk₀ ‖ chunk₁ ‖ ...)，32B |
| `0x2B` | 43 | `tagRabinSignature` | bytes | Rabin 签名 (S, U) |
| `0x2C` | 44 | `tagRabinPubKey` | bytes | Rabin 公钥模数 n |
| `0x2D` | 45 | `tagRegistryTxID` | bytes | 收入分成注册表 TxID，32B |
| `0x2E` | 46 | `tagRegistryVout` | uint32 | 注册表输出索引 |
| `0x2F` | 47 | `tagISOConfig` | bytes | ISO 配置 (子 TLV) |
| `0x30` | 48 | `tagACLRef` | bytes | ACL 引用 |

**Anchor 节点专用字段** (NodeType=3, git-remote-bitfs 使用)：

| Tag (hex) | Tag (dec) | 常量名 | 类型 | 说明 |
|-----------|-----------|--------|------|------|
| `0x20` | 32 | `tagTreeRootPNode` | bytes | 根目录 P_node，33B |
| `0x21` | 33 | `tagTreeRootTxID` | bytes | 根目录最新 TxID，32B |
| `0x22` | 34 | `tagParentAnchorTxID` | bytes | 父锚点 TxID，32B (可重复, merge commit) |
| `0x23` | 35 | `tagAuthor` | string | Git 提交作者 |
| `0x24` | 36 | `tagCommitMessage` | string | Git 提交消息 |
| `0x25` | 37 | `tagGitCommitSHA` | bytes | Git commit SHA，20B |
| `0x26` | 38 | `tagFileMode` | uint32 | Git file mode |

> 完整 Anchor 节点规范见 `docs/spec/03-metanet.md` §Anchor 节点字段。

**预留范围**：
- `0x31+`: 未来使用

> **设计决策 #6**: Tag 编号以代码实现为准。设计文档中 field 5 的 reserved 位已取消。

### 附录 B：Bitcoin Script 模板

#### B.1 P2PKH (Pay-to-Public-Key-Hash)

用于所有标准输出（NodeUTXO、ParentUTXO、Change）。

```
Locking Script (scriptPubKey):
  OP_DUP OP_HASH160 <H160(pubkey)> OP_EQUALVERIFY OP_CHECKSIG

Unlocking Script (scriptSig):
  <signature> <pubkey>
```

#### B.2 OP_DROP 数据嵌入

用于 DataTx (Template 4) 的链上内容存储。

```
Locking Script:
  <encrypted_content> OP_DROP
  OP_DUP OP_HASH160 <H160(P_node)> OP_EQUALVERIFY OP_CHECKSIG

Unlocking Script:
  <signature> <P_node>
```

内容以 push data 嵌入，`OP_DROP` 将其从栈中移除，剩余部分为标准 P2PKH。

#### B.3 HTLC (Hash Time-Locked Contract) — 预留

> HTLC 脚本结构将在支付模型规范中详细定义。以下为参考模板。

```
Locking Script:
  OP_IF
    OP_SHA256 <capsule_hash(32B)> OP_EQUALVERIFY
    OP_DUP OP_HASH160 <seller_addr(20B)> OP_EQUALVERIFY OP_CHECKSIG
  OP_ELSE
    OP_2 <buyer_pubkey(33B)> <seller_pubkey(33B)> OP_2 OP_CHECKMULTISIG
  OP_ENDIF

Seller Claim (IF branch):
  <seller_sig> <seller_pubkey> <capsule> OP_TRUE

Buyer Refund (ELSE branch, after timeout):
  OP_0 <buyer_sig> <seller_sig> OP_FALSE
  (交易 nLockTime >= timeout_height)
```

---

## 附录 C：设计决策记录

| # | 决策 | 结果 | 理由 |
|---|------|------|------|
| 1 | 规范权威性 | 本文档为唯一 source of truth | 消除多文档矛盾 |
| 2 | 矛盾时的权威 | 以设计意图为准 | 代码可能有实现偏差 |
| 3 | 规范语言 | 中英双语 | 中文正文 + 英文技术术语/表格 |
| 4 | KDF 方案 | 保留 HKDF (RFC 5869) | 在 CSW Method 42 概念上升级为标准化框架 |
| 5 | HKDF info string | `"bitfs-file-encryption"` | 域分隔符应标识用途而非协议名 |
| 6 | TLV tag 编号 | 以代码为准 | 避免迁移已有测试数据 |
| 7 | HTLC 发起方 | 待定 | 支付模型需单独设计 |
| 8 | Hard Link | 删除 | Metanet DAG 是严格树，不支持多父节点 |
| 9 | NodeType | File/Dir/Link (移除 Anchor) | Anchor 是扩展类型，不属于核心规范 |
| 10 | PRIVATE 恢复 | 两层密钥 + 递归解密（已解决） | 元数据用独立密钥（salt=random(16B)，存储为 EncPayload 前缀），通过目录 ChildEntry 递归恢复 |
| 11 | DNS TXT 格式 | `_bitfs.{domain}`, 值 `bitfs=<hex_pubkey>` | 可扩展格式，类似 DKIM |
| 12 | 元数据加密密钥 | `HKDF(ECDH.x, random(16B), "bitfs-metadata-encryption")` | 与文件加密密钥隔离，随机盐存储为 EncPayload 前缀 |
| 13 | 子索引不重用 | 删除后 NextChildIndex 不递减 | 防止密钥碰撞：重用索引会派生相同 BIP32 密钥对 |

---

## 附录 D：与 CSW Method 42 原始论文的关系

BitFS 的加密方案基于 Craig Wright 的 "An Immutable File and Data Store" (2019)，
但在以下方面进行了改进：

| 方面 | CSW 原始 | BitFS 实现 |
|------|---------|------------|
| 密钥派生 | `H(Da \| H(file) \| INDEX)` (简单 hash 拼接) | `HKDF-SHA256(ECDH.x, key_hash, info)` (RFC 5869) |
| INDEX 参数 | 显式 invoice number | 融入 BIP44 路径 (`m/44'/236'/...`) |
| 域分隔 | 无 | `info` 参数提供域分隔 |
| 文件标识 | `H(file)` (单次 hash) | `SHA256(SHA256(plaintext))` (双重 hash) |

**核心概念保留**：
- 每文件独立密钥（content-addressed）
- ECDH 双方共享（Alice-Bob 模式）
- BIP32 密钥层级映射文件系统

**安全性增强**：
- HKDF 有形式化安全证明（hash 拼接没有）
- 域分隔防止不同用途密钥碰撞
- 双重 hash 提供额外的原像抵抗

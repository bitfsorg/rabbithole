# 参考论文 (References)

项目相关的研究论文和专利文献。以下为每篇文档的详细内容摘要及其与 BitFS/Metanet 项目的关联。

## 论文列表

| # | 文件 | 内容 | 相关性 |
|---|------|------|--------|
| 0 | US20210399898A1.pdf | Metanet 专利 (nChain, 2018/2021) | **核心参考**：Metanet DAG 结构、MURL 寻址、数据插入方法、Rabin 签名、原子交换付费访问、浏览器钱包架构、LFCP 分发网络 |
| 1 | An immutable file and data store.pdf | Craig Wright 论文 (2019) | Method 42 文件加密、逐文件密钥派生、双方安全共享 |
| 2 | The-Metanet-Technical-Summary-v1.0.pdf | Metanet 技术概要 (nChain) | Metanet 协议正式规范：节点/边/DAG、域名、MURL、数据插入方法 |
| 3 | Threshold-Signatures-whitepaper-nchain.pdf | 门限签名 (nChain, Michaella Pettit) | JVRSS 协议、门限签名、分布式 ECDSA |
| 4 | GB2608179A-Multi-level blockchain.pdf | 多层区块链专利 (nChain, 2021/2022) | ML 协议：在核心区块链上嵌入二级数据链 |
| 5 | Blockchain Verified Distributed Storage.pdf | Craig Wright 新论文 (SSRN preprint, 2026) | **核心参考**：DHT 分区存储、Merkle proof-of-retention、链上挑战-响应验证、per-copy 加密防串谋、存款+惩罚经济模型、RAID 条带化并行检索 |

---

## Paper #0: US20210399898A1 — Storing Data on a Blockchain (Metanet 专利)

**来源**: nChain Holdings, 优先权日 2018-04-27, 公开 2021-12-30
**发明人**: Steve Shadders, Alexander Mackay, Craig Wright 等

### 核心概念

**Metanet DAG 结构**:
- 节点 (Node) = 一笔区块链交易，包含公钥 P_node 和父节点 TxID_parent
- 边 (Edge) = 父节点公钥 P_parent 对子交易的签名（在 input 的 scriptSig 中）
- 节点 ID: `ID_node = H(P_node || TxID_node)`，全局唯一
- BIP32 密钥层级结构映射到 DAG 树结构：父节点派生子节点公钥

**Metanet 交易格式**:
```
OP_RETURN <MetaFlag=0x6d657461> <P_node> <TxID_parent> [<attributes>...] [<content>]
```
- MetaFlag: 4 字节标识 `0x6d657461` ("meta")
- 属性字段: content_type, content_encoding, content_hash, filename 等

**数据插入三种方式**:
1. **OP_RETURN only**: 数据放在不可花费的 OP_RETURN 输出中（最简单）
2. **OP_RETURN + OP_DROP**: 内容放在可花费输出的 scriptPubKey 中（`<data> OP_DROP <P2PKH>`），属性放在 OP_RETURN 中。适合大文件分块
3. **多交易**: 大文件拆分为多笔交易，用 chunk_index 排序重组

**MURL 寻址**:
- 格式: `mnp://domain/path/file`（类似 HTTP URL）
- 域名 = Metanet DAG 的孤儿节点（无父节点的根）
- 路径 = DAG 中的节点层级
- 支持 vanity address 实现人类可读域名

**原子交换付费访问**:
- 付费内容通过 hash puzzle 锁定：`OP_SHA256 <H(S)> OP_EQUAL`
- 购买者通过 atomic swap 交换 token，获得 secret S 来解锁内容
- Token 交易和内容解锁在同一笔交易中原子完成

**Rabin 签名**:
- 用于 Metanet 节点的链上签名验证
- 比 ECDSA 更高效的链上验证（更少操作码）

**浏览器钱包架构**:
- 浏览器扩展 + 本地密钥管理
- 支持浏览 Metanet 内容、支付访问费用
- 整合 MURL 导航

**LFCP (Layered File Chunking Protocol)**:
- 大文件分层分块分发
- 多节点并行下载
- 类似 BitTorrent 的 CDN 网络

### 与项目的关联
- BitFS 的 Metanet DAG 实现直接基于此专利
- MetaFlag `0x6d657461` 已在代码中使用
- 数据插入方式（尤其 OP_RETURN + OP_DROP）是 BitFS capsule 的基础
- MURL 对应 BitFS 的寻址方案
- 浏览器钱包架构指导 bitfs-extension 设计

---

## Paper #1: An Immutable File and Data Store (Craig Wright, 2019)

**来源**: Craig Wright 博客文章/论文

### 核心概念

**Method 42 密钥派生**:
- 每个文件生成唯一密钥对，通过 ECDH 实现安全共享
- 密钥派生公式:
  ```
  s(file.1) = H[Da(0) | H(file) | INDEX]
  Pf(1) = s(file.1) × G          // 文件公钥
  Pa(1) = Pf(1) + Pa(0)           // 用户的文件特定公钥
  ```
- 加密密钥（ECDH 共享密钥）:
  ```
  s.f(1) = Da(0) × Pf(0) = Df(0) × Pa(0) = Da(0) × Df(0) × G
  ```

**逐文件加密**:
- 每个文件有独立的密钥对和加密密钥
- 文件内容哈希参与密钥派生 → content-addressed
- INDEX 字段允许同一文件的多个版本/副本

**双方安全共享 (Alice + Bob)**:
- Alice 存储文件，Bob 获得授权访问
- 通过 ECDH: Alice 用 Da(0) × Pb(0)，Bob 用 Db(0) × Pa(0)
- 两方可以独立计算相同的共享密钥
- 不需要传输密钥，只需交换公钥

**伪匿名存储**:
- 每个文件使用不同的公钥地址
- 外部观察者无法关联同一用户的不同文件
- 密钥层级只有拥有主密钥的用户可以追踪

### 与项目的关联
- libbitfs-go/method42 包直接实现了此论文的密钥派生方案
- `HKDF-SHA256(ECDH(D_node, P_node).x, key_hash)` 是实际代码中的简化版本
- BitFS capsule 的加密层基于此方案
- 文件共享（grant access）机制基于 ECDH 双方计算

---

## Paper #2: The Metanet Technical Summary v1.0 (nChain)

**来源**: nChain 官方技术文档

### 核心概念

**Metanet 协议正式规范**:
- **节点 (Node)**: 一笔包含 Metanet Flag 的交易
  - 格式: `OP_RETURN <0x6d657461> <P_node> <TxID_parent> [data...]`
  - 每个节点有唯一 ID: `ID_node = H(P_node || TxID_node)`
- **边 (Edge)**: 父节点对子交易的签名授权
  - 子交易的某个 input 必须由 P_parent 签名
  - 这确保只有父节点的私钥持有者可以创建子节点
- **DAG (有向无环图)**: 节点和边构成全局 DAG 结构

**版本控制**:
- 同一公钥 P_node 可以有多笔交易（不同 TxID）
- 最新版本 = 使用相同 P_node 的最新交易
- ID_node 随 TxID 变化，但 P_node 不变 → 可追踪版本历史

**域名与层级**:
- 孤儿节点（TxID_parent = 0x00...00）= 顶级域名
- 子节点形成路径层级: domain/path1/path2/file
- 支持 vanity address 创建人类可读域名

**MURL (Metanet URL)**:
- `mnp://` + domain + path + file
- domain = 孤儿节点的公钥地址或 vanity name
- path = DAG 中的层级路径

**内容可寻址网络**:
- content_hash 属性使数据可通过内容哈希寻址
- 类似 IPFS 的 CID，但锚定在区块链上

**关键字搜索**:
- 节点可包含 keyword 属性
- 支持跨 DAG 的关键字搜索和索引

### 与项目的关联
- BitFS Metanet DAG 实现的规范性参考
- libbitfs-go/metanet 包的节点/边模型基于此文档
- ID_node = H(P_node || TxID_node) 在代码中直接使用
- MURL 方案指导 BitFS 的路径寻址设计

---

## Paper #3: Shared Secrets and Threshold Signatures (nChain, Michaella Pettit)

**来源**: nChain 白皮书

### 核心概念

**JVRSS (Joint Verifiable Random Secret Sharing)**:
- N 个参与者共同生成共享密钥
- 每个参与者 i 创建 t 阶多项式: `f_i(x) = a_{i,0} + a_{i,1}x + ... + a_{i,t}x^t`
- 参与者 i 向参与者 j 发送 `f_i(j)` 作为份额
- 共享密钥: `a = Σ a_{i,0}`（所有常数项之和）
- 任何 t+1 个参与者可通过 Lagrange 插值恢复密钥

**可验证性**:
- 每个参与者公开承诺: `C_{i,k} = a_{i,k} × G`（椭圆曲线点）
- 其他参与者可验证收到的份额: `f_i(j) × G == Σ C_{i,k} × j^k`
- 作弊者可被检测和排除

**共享密钥运算**:
- **ADDSS**: 两个共享密钥相加 → 新共享密钥
- **PROSS**: 两个共享密钥相乘 → 新共享密钥（需要 degree reduction）
- **INVSS**: 共享密钥求逆 → 逆元的共享密钥

**门限签名 (Threshold Signatures)**:
- 分布式 ECDSA: 私钥从未在任何单一位置存在
- 签名过程:
  1. JVRSS 生成共享私钥 d 和临时密钥 k
  2. 计算 r = (k × G).x
  3. 每个参与者计算 s_i = k_i^{-1} × (hash + r × d_i) 的份额
  4. 通过 Lagrange 插值恢复 s
  5. 签名 (r, s) 可用标准 ECDSA 验证

### 与项目的关联
- Metanet 多方管理场景的参考（多签名节点管理）
- 未来可能用于 BitFS 的多方密钥管理
- 门限签名可用于 Metanet CDN 节点的分布式治理

---

## Paper #4: GB2608179A — Multi-level Blockchain (nChain, 2021/2022)

**来源**: 英国专利 GB2608179A, nChain Holdings

### 核心概念

**ML (Multi-Level) 协议**:
- 在核心区块链（如 BSV）上嵌入二级数据链
- 核心区块链提供时间戳、不可篡改性和全局排序
- 二级链利用核心链的安全性，无需独立共识

**Carrier Pair (载体对)**:
- ML 交易的基本单元: 一个 input + 一个 output 的配对
- Input 签名 = 二级链的授权
- Output = 二级链的状态承诺
- 签名类型: `SIGHASH_ALL | ANYONECANPAY` (S|ACP)
  - 签名覆盖所有 output 但只覆盖自己的 input
  - 允许多个 carrier pair 被灵活组装到同一笔交易中

**ML Block 结构**:
- 一笔核心区块链交易，包含:
  - **Chain Output**: 链接到下一个 ML block 的 UTXO（链的延续）
  - **Embedded Block Header**: 在 OP_RETURN 中，包含版本、前一 ML block hash、Merkle root、时间戳等
  - **Embedded Coinbase Tx**: ML block 生产者的奖励交易
  - **Embedded User Transactions**: 多个用户的 carrier pair 的 OP_RETURN 数据

**ML Block Producer**:
1. 收集 ML transactions（用户提交的 carrier pair）
2. 验证 (MLV) 每个交易的合法性
3. 组装候选 ML block
4. 发布到核心区块链

**版本控制与链接**:
- Chain output UTXO 连接连续的 ML blocks
- 花费前一个 chain output = 创建新 ML block
- 形成链式结构，可回溯完整历史

### 与项目的关联
- **Metanet Chain 侧链架构的直接参考**
- ML 协议可以实现 Metanet 的 MNT Token 链:
  - Core chain = BSV
  - Secondary chain = Metanet Chain (MNT Token 转账、节点注册等)
- Carrier pair 的 S|ACP 签名模式适合多方参与的 CDN 交易
- ML block producer 角色对应 Metanet Node 运营商
- 无需独立共识机制，继承 BSV 的安全性

---

## Paper #5: Blockchain Verified Distributed Storage (Craig Wright, 2026)

**来源**: SSRN preprint, 2026 (55 页)

### 核心架构

本论文提出了一个完整的去中心化存储验证系统，与 BitFS/Metanet 产品高度契合。

**1. DHT 数据分区 (Pastry)**:
- 128-bit ID 空间，节点和数据块都映射到此空间
- O(log₁₆ N) 路由跳数
- 每节点存储量: O(r × D / N)，r = 冗余因子
- 推荐 r ≥ 3 保证零数据丢失（即使在网络动荡时）

**2. Merkle Proof-of-Retention (链上验证)**:
- **挑战交易 (Challenge Tx)**: Oracle 创建，嵌入:
  - Merkle root (已知)
  - 随机叶子索引 (challenge)
  - 支付金额 (锁定在 scriptPubKey 中)
- **响应交易 (Response Tx)**: 存储提供者创建，在 scriptSig 中提供:
  - 叶子数据
  - Merkle path (兄弟节点哈希序列)
- **BSV Script 验证**: 仅需 5 个操作码:
  - `OP_SHA256`, `OP_CAT`, `OP_SWAP`, `OP_NOTIF/ENDIF`, `OP_EQUAL`
  - 深度 d 的树需要 `6d + 3` 个操作码
  - 完全在链上验证，无需信任第三方

**3. Per-copy 加密 (防串谋)**:
- 每个存储提供者收到不同的加密副本:
  ```
  c_{j,i} = b_i ⊕ K_{j,i}    // AES-CTR 加密
  ```
- 每个副本有独立的 Merkle tree
- 即使提供者串谋，也无法用一份数据回答所有挑战
- 确保每个节点真正存储了自己的完整副本

**4. EC-Point 隐私证明**:
- 叶子节点: `leaf = H(c × G)`，其中 c 是密文块
- 存储者用 ECDSA 签名证明持有 c（知道离散对数）
- 验证者可验证签名但无法得知 c 的值
- 形式化证明:
  - **Thm 8**: 完备性和可靠性（ε ≤ 2⁻¹²⁷）
  - **Thm 9**: 零知识性（可模拟）
  - **Thm 10**: 不可转移性

**5. 安全性形式化分析**:
- **Thm 2** (持有可靠性): 不持有数据的节点伪造证明的概率 ε ≤ 2⁻¹²⁷
- **Thm 3** (外包抵抗): 节点无法将存储外包给其他节点来通过验证
- 基于 SHA-256 的抗碰撞性和离散对数困难假设

**6. 经济模型**:
- **Verify-then-Pay**: 挑战交易锁定支付，只有有效 Merkle proof 才能解锁
- **存款 + 惩罚 (Slashing)**: 存储节点缴纳押金，连续错过挑战则扣除
- **挑战批处理**: 多个文件块可合并到一次挑战中
- 成本分析:
  - 中等规模: **$2.94/TB/year**
  - 大规模: 更低（规模效应）
  - BSV 交易费极低是关键优势

**7. DHT 可用性分析**:
- 节点 churn (加入/离开) 的数学模型
- r = 3 时数据存活率 > 99.99%
- 修复机制: 检测到副本不足时自动复制到新节点

**8. RAID 条带化并行检索**:
- 大文件分条带存储在多个节点
- 并行从多节点下载不同条带
- 类似 RAID-0 的吞吐量提升

**9. 仿真验证**:
- 所有理论模型的仿真结果与理论预测偏差在 2.2%-6.7% 内
- 验证了 DHT 路由、Merkle 验证、经济模型的正确性

### 与项目的关联

| 论文概念 | BitFS 产品 | Metanet 产品 |
|----------|-----------|--------------|
| DHT (Pastry) 数据分区 | — | Metanet Node 网络拓扑 |
| Merkle proof-of-retention | — | 节点存储验证机制 |
| BSV Script 链上验证 | capsule 脚本 | 挑战-响应交易 |
| Per-copy 加密 | capsule XOR masking | 节点防串谋 |
| EC-Point 隐私证明 | — | 隐私保护验证 |
| Verify-then-Pay | x402 支付层 | MNT Token 支付 |
| 存款+惩罚 | — | 节点经济模型 |
| RAID 条带化 | 大文件分块 | CDN 并行分发 |
| $2.94/TB/year | 存储成本参考 | 定价参考 |

---

## 跨论文关键概念对照

| 概念 | 论文来源 | 代码实现位置 |
|------|---------|-------------|
| MetaFlag `0x6d657461` | #0, #2 | libbitfs-go/metanet |
| ID_node = H(P_node \|\| TxID_node) | #0, #2 | libbitfs-go/metanet |
| Method 42 (ECDH 密钥派生) | #1 | libbitfs-go/method42 |
| OP_RETURN + OP_DROP 数据插入 | #0, #2 | libbitfs-go/tx |
| MURL 寻址 (`mnp://`) | #0, #2 | BitFS 路径系统 |
| BIP32 密钥层级 | #0, #1 | libbitfs-go/wallet |
| Merkle proof-of-retention | #5 | 待实现 (Metanet) |
| Per-copy 加密 | #5 | capsule XOR masking (BitFS) |
| DHT Pastry 路由 | #5 | 待实现 (Metanet) |
| ML 二级链协议 | #4 | 待实现 (Metanet Chain) |
| 门限签名 JVRSS | #3 | 待实现 (多方管理) |
| Carrier Pair (S\|ACP) | #4 | 待实现 (Metanet Chain) |
| 原子交换付费访问 | #0 | libbitfs-go/x402 |

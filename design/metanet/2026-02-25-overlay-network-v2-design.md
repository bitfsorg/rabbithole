# Metanet Overlay Network v2 — 架构设计

> 日期: 2026-02-25
> 状态: 已批准
> 依据: Multilevel Blockchain 专利 (GB2608179A), Blockchain Verified Distributed Storage (Craig Wright)

## 1. 设计背景与动机

### 1.1 旧设计问题

旧设计将 Metanet 定位为一条独立的 POW 链（BSV fork），通过合并挖矿 (AuxPoW) 锚定 BSV：
- 需要运行完整的 BSV-fork 节点（C++ 代码库维护成本高）
- 需要 BSV 矿池配合才能合并挖矿
- 本质上在维护两条区块链
- 独立链算力不足时安全性堪忧

### 1.2 新设计理念

基于 Multilevel Blockchain 专利的方法，将 Metanet 构建为 BSV 上的 **Overlay Network (ON)**：
- **每笔 ON 交易都是合法的 BSV 交易**
- **ML Block 嵌入 BSV 交易中**，BSV 提供最终性和不可篡改性
- 不需要 fork BSV 代码，不需要独立链基础设施
- ON 节点是轻量 Go 服务，通过 SPV/API 连接 BSV

## 2. 核心架构

### 2.1 总体架构

```
┌─────────────────────────────────────────────────────────────┐
│                    Metanet Overlay Network                   │
│                                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  Miner   │  │ Storage  │  │  Oracle  │  │Publisher │   │
│  │  (POW)   │  │ Provider │  │(挑战管理) │  │(非节点)  │   │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘   │
│       │             │             │              │          │
│  ┌────┴─────────────┴─────────────┴──────────────┘          │
│  │              ON P2P 网络                                  │
│  │  (nServices: NODE_METANET 标识)                           │
│  │  ML 交易传播 / ML Block 广播 / 存储证明 / 节点发现          │
│  └──────────────────────┬────────────────────────           │
│                         │                                   │
│  ┌──────────────────────┴────────────────────────┐          │
│  │           MNT Ledger (ML 链原生 UTXO)          │          │
│  │  · 出块奖励 Coinbase                           │          │
│  │  · 存储合约付款 & 押金                          │          │
│  │  · Oracle 服务费                                │          │
│  │  · ON 交易手续费                                │          │
│  └───────────────────────────────────────────────┘          │
└────────────────────────────┬────────────────────────────────┘
                             │ ML Block = BSV Transaction
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                      BSV 主链                                │
│  · ML Block 作为普通 BSV 交易被确认                           │
│  · BSV 提供最终性和不可篡改性                                 │
│  · x402 微支付 (Visitor → Node, BSV 直接支付)                │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 与旧设计的核心变化

| 维度 | 旧设计 | 新设计 (v2) |
|------|--------|-----------|
| 本质 | 独立 POW 链（BSV fork） | BSV 上的 ML overlay |
| 共识 | SHA256 POW + 合并挖矿 | 轻量 POW（出块权竞争），BSV 保证不可篡改 |
| 安全性来源 | 自身算力 + BSV 锚定 | BSV 交易确认 |
| MNT 表示 | 独立链原生货币 | ML 链原生 UTXO（嵌入 BSV 交易中） |
| 节点实现 | Fork BSV 节点 C++ | Go overlay 服务 + BSV SPV/API |
| 出块要求 | 仅需算力 | 算力 + 活跃存储合约 |
| 存储证明 | ON 节点共识验证 | Verify-then-pay 脚本原子执行 |

## 3. 四角色模型

ON 网络中有四种角色，一个节点可同时担任多个角色：

### 3.1 Miner（矿工）

- **职责**: 收集 ON 交易，构建 ML Block，执行轻量 POW
- **收入**: MNT 出块奖励 (Coinbase) + ON 交易手续费
- **前提条件**: 必须拥有 ≥ 1 个活跃存储合约（作为 Storage Provider 或 Oracle）
- **为什么**: 将出块权与实际存储服务绑定，防止纯算力矿工夺取所有 MNT 奖励

### 3.2 Storage Provider（存储节点）

- **职责**: 存储 Publisher 的加密数据副本，周期性提交 Merkle 存储证明
- **收入**: 逐期存储费 (MNT，来自合约 Challenge UTXO)
- **押金**: 锁定 D_dep MNT 作为履约保证
- **加密**: 每个 SP 持有的数据副本使用独立 ECDH 密钥加密（防串通）

### 3.3 Oracle（服务提供方）

- **职责**: 合约中介，负责：
  - 接收 Publisher 的数据
  - 为每个 Storage Provider 生成独立加密副本
  - 构建 Merkle 树
  - 创建 Challenge UTXOs（预资助付款）
  - 管理合约生命周期
- **收入**: Oracle 服务费 (MNT，从合约金额中抽取)
- **参考**: 论文 #5 中的 "Service Provider" 角色

### 3.4 Publisher（内容发布者）

- **职责**: 发布数据到 ON 网络
- **支出**: MNT（支付存储费给 Oracle + Storage Provider）
- **非节点角色**: Publisher 不需要运行 ON 节点，可以通过 API 与 Oracle 交互
- **自托管模式**: 如果 Publisher 运行 bitfs daemon（Layer 2），则不涉及 ON，Visitor 用 BSV 直接付费

## 4. ML Block 结构

基于 Multilevel Blockchain 专利的 Carrier Pair 模型。

### 4.1 BSV 交易格式

每个 ML Block 是一笔合法的 BSV 交易：

```
BSV Transaction (= 1 个 ML Block):
│
├── Input[0]:  Chain Input
│              花费前一个 ML Block 的 Chain Output
│              签名: SIGHASH_ALL (矿工签名)
│
├── Input[1..n]: ON Transaction Carrier Pairs
│              每个 carrier pair = 1 笔 ON 交易
│              签名: SIGHASH_SINGLE | ANYONECANPAY
│              (允许矿工自由组合 carrier pairs)
│
├── Output[0]: Chain Output
│              锁定给下一个 ML Block 的矿工
│              链接 ML Block 序列
│
├── Output[1]: OP_RETURN — ML Block Header
│              (见 4.2 节)
│
├── Output[2]: Coinbase Output
│              MNT 奖励给矿工
│
└── Output[3..m]: ON Transaction Outputs
                  MNT 转账、存储合约、Oracle 费用等
```

### 4.2 ML Block Header

```
Field                   Size    Description
─────────────────────── ─────── ─────────────────────────────
Magic                   4 B     "MNML" (0x4d4e4d4c)
Version                 4 B     协议版本
Prev ML Block Hash      32 B    前一个 ML Block header 的 SHA256d
ON Tx Merkle Root       32 B    所有 ON 交易的 Merkle root
Timestamp               4 B     Unix 时间戳
Difficulty Target       4 B     nBits 编码的难度目标
Nonce                   4 B     POW nonce
Height                  4 B     ML Block 高度
Coinbase Amount         8 B     本 Block 的 MNT 铸造量 (satoshis)
─────────────────────── ─────── ─────────────────────────────
Total                   96 B
```

### 4.3 出块流程

```
1. 矿工收集 mempool 中的待确认 ON 交易 (carrier pairs)
2. 验证出块资格: 矿工必须有 ≥ 1 个活跃存储合约
3. 构建 ML Block:
   a. 设置 Chain Input (花费前一个 ML Block 的 Chain Output)
   b. 计算 ON Tx Merkle Root
   c. 设置 Coinbase Amount (根据当前 halving epoch)
   d. 填入其他 header 字段
4. 执行 SHA256 POW: 枚举 Nonce 直到 SHA256d(header) < target
5. 构建完整的 BSV 交易 (ML Block + carrier pairs + outputs)
6. 广播 BSV 交易到 BSV 网络
7. BSV 矿工将此交易包含在 BSV 区块中
8. BSV 区块确认后, ML Block 不可篡改
```

### 4.4 POW 参数

```
算法:          SHA256d (与 Bitcoin 一致)
目标出块时间:   ~10 分钟
难度调整:       每 2016 个 ML Block (约 2 周)
初始难度:       较低 (允许 CPU 挖矿)
```

由于 BSV 提供最终性，POW 的目的仅是公平分配出块权，不需要 BSV 级别的算力来保护链安全。

## 5. MNT Token 经济

### 5.1 Token 参数

```
名称:          MNT (Metanet Token)
总量:          21,000,000 MNT
初始出块奖励:   50 MNT / ML Block
减半间隔:      210,000 ML Blocks (约 4 年)
最小单位:      1 satoshi = 0.00000001 MNT
```

### 5.2 MNT 是 ML 链原生 UTXO

MNT 不是 BSV 上的 colored coin 或 OP_RETURN token，而是 ML 链自身的 UTXO：
- ON 节点维护独立的 MNT UTXO 集
- ML Block 中的交易输入/输出跟踪 MNT 余额
- 虽然嵌入在 BSV 交易中，但 MNT 账本由 ON 独立维护
- BSV UTXO 集和 MNT UTXO 集完全独立

### 5.3 双币经济模型

| 场景 | 货币 | 说明 |
|------|------|------|
| 终端用户检索文件 | **BSV** | Visitor 通过 x402 微支付，直接 BSV 交易 |
| HTLC 原子交换 | **BSV** | 一次性文件购买，BSV 链上 |
| Publisher 自托管 | **BSV** | Publisher 运行 bitfs daemon，不涉及 ON |
| 长期存储合约 | **MNT** | Publisher → Oracle → Storage Provider |
| 出块奖励 | **MNT** | 矿工获得 Coinbase |
| ON 交易手续费 | **MNT** | 所有 ON 交易的 fee |
| Oracle 服务费 | **MNT** | 从合约金额中抽取 |
| Provider 押金 | **MNT** | 锁定在合约中 |

**设计原则**: 普通终端用户只接触 BSV。只有参与 ON 网络的 Publisher 和节点运营商需要 MNT。

## 6. 存储合约（Verify-Then-Pay 原子模型）

### 6.1 设计来源

基于论文 #5 (Blockchain-Verified Distributed Storage) Section 4.5 和 Section 9.3。

核心创新: **付款锁定在验证脚本中**。Storage Provider 只有在提交有效的 Merkle 存储证明时，才能解锁付款 UTXO。区块链脚本执行替代了所有信任假设。

### 6.2 三方角色

| 角色 | 论文术语 | 我们的系统 | 职责 |
|------|--------|---------|------|
| Data Provider | Data Provider | Publisher | 拥有数据, 支付存储费 |
| Storage Provider | Storage Provider (SP) | Storage Provider | 存储数据, 提交证明 |
| Service Provider | Service Provider (Oracle) | Oracle | 构建挑战, 管理加密, 中介 |

### 6.3 合约建立流程

```
Phase 1: 数据准备 (Oracle 执行)

  1. Publisher 将数据 Δ 提交给 Oracle
  2. Oracle 分片: chunks[0..n-1] = split(Δ, block_size)
     默认 block_size = 1 KB (论文建议值)
  3. 为每个 Storage Provider SP_j 生成独立加密副本:
     K_j = KDF(ECDH(oracle_priv, SP_j_pub))
     C_j,i = chunks[i] ⊕ K_j,i   (AES-CTR 模式, 论文 Section 5.2)
  4. 为每个 SP_j 构建独立 Merkle 树:
     leaf_j,i = SHA256(C_j,i)
     root r_M,j = MerkleRoot(leaf_j,0, ..., leaf_j,n-1)
  5. 分发: C_j → SP_j

  防串通: 每个 SP 持有不同加密副本, 无法共享证明 (论文 Theorem 1)

Phase 2: 合约创建 (ON 交易)

  Oracle 构建合约创建 ON 交易:

  Inputs:
    Publisher 的 MNT UTXO: α × N_epochs (合约总付款)
    SP 的 MNT UTXO: D_dep (押金)

  Outputs:
    [0..N-1] Challenge UTXOs (每个价值 α MNT):
      见 6.4 节锁定脚本

    [N] SP 押金 UTXO (D_dep MNT):
      见 6.5 节押金脚本

    [N+1] Oracle 服务费 UTXO

  合约参数:
    publisher_pubkey   — Publisher 公钥
    provider_pubkey    — Storage Provider 公钥
    oracle_pubkey      — Oracle 公钥
    data_merkle_root   — r_M,j (该 SP 的加密数据 Merkle root)
    num_chunks         — 分片数量 n
    proof_interval     — 证明间隔 (ML Block 高度数)
    max_missed         — 最大连续未响应次数 q
    total_epochs       — 合约总周期 N
    epoch_payment      — 每期付款 α (MNT satoshis)
    deposit            — Provider 押金 D_dep (MNT satoshis)
    deadline           — 响应截止时间 τ (ML Block 数)
```

### 6.4 Challenge UTXO 锁定脚本

基于论文 Section 4.5, 每个 epoch 的 Challenge UTXO:

```
OP_IF
  // 路径 A: Storage Provider 提交有效 Merkle 证明 → 获得付款
  <provider_pubkey> OP_CHECKSIGVERIFY

  // Merkle proof 验证 (论文 Section 4.5)
  // 解锁脚本提供: b_i, dir_1, s_1, ..., dir_d, s_d
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
```

**脚本大小**: 6d + 3 + 32 bytes (d = Merkle 树深度)
- 1 KB 分片, 1 TB 数据: d ≈ 30, 脚本 ≈ 215 bytes
- 论文 Table 4 验证了可行性

**挑战索引生成** (确定性, 论文 Section 4.4):
```
challenge_index_k = H(block_hash_he || epoch_k || provider_id) mod num_chunks
```
其中 block_hash_he 是挑战发起时的最新 BSV 区块哈希, 提供不可预测的随机性。

### 6.5 押金 UTXO 脚本

```
OP_IF
  // 路径 A: 合约正常完成 → 退还给 Storage Provider
  <contract_end_height> OP_CHECKLOCKTIMEVERIFY OP_DROP
  <provider_pubkey> OP_CHECKSIG

OP_ELSE
  // 路径 B: Provider 违约 → Oracle 提交违约证据, 退回 Publisher
  <oracle_pubkey> OP_CHECKSIGVERIFY
  <publisher_pubkey> OP_CHECKSIG

OP_ENDIF
```

**押金公式** (论文 Section 9.3):
```
D_dep = α × T_contract × f_epoch
```
- α = 每期付款金额
- T_contract = 合约总期数
- f_epoch = 挑战频率

### 6.6 合约执行与终止

```
═══ 正常执行 ═══

每个 epoch k:
  1. 挑战确定性生成: i_k = H(block_hash || k || SP_id) mod n
  2. SP 构建解锁脚本:
     push: C_j,i_k, dir_1, s_1, ..., dir_d, s_d, sig_SP
  3. SP 提交 ON 交易花费 Challenge UTXO[k]
  4. ON 节点验证脚本执行 → 通过则交易有效
  5. SP 获得 α MNT

═══ Provider 未响应 ═══

  τ 超时后, Oracle 花费 Challenge UTXO[k] (路径 B)
  记录为一次未响应

═══ Provider 违约 (连续 q 次未响应) ═══

  Oracle 提交违约证据:
  - 连续 q 个已超时的 Challenge UTXO 回退交易
  Oracle + Publisher 联合花费押金 UTXO (路径 B)
  剩余 Challenge UTXOs 超时后逐个退回 Oracle → 转给 Publisher

═══ 合约正常到期 ═══

  所有 N 个 Challenge UTXOs 已被 SP 花费
  押金 UTXO 时间锁到期 → SP 取回 D_dep
```

### 6.7 经济分析 (来自论文 Section 9)

| 参数 | 小规模 | 中规模 | 大规模 |
|------|--------|--------|--------|
| Storage Providers | 10 | 100 | 1,000 |
| 总数据量 | 100 GB | 10 TB | 1 PB |
| Merkle 树深度 | 27 | 34 | 40 |
| 每日挑战次数 | 10 | 100 | 1,000 |
| 年验证成本 (USD) | $2.44 | $29.35 | $336 |
| 每 TB/年成本 | $24.36 | **$2.94** | $0.33 |

对比 AWS S3: $276/TB/年。ON 存储验证成本仅为 S3 的 **1%**。

## 7. BSV 网络集成

### 7.1 nServices 标识

ON 节点通过 BSV P2P 网络的 `nServices` 字段标识自己：

```
nServices |= NODE_METANET  // 申请专用 ServiceFlags bit
```

BSV P2P 协议的 `version` 消息中包含 nServices，允许：
- ON 节点通过标准 `addr`/`getaddr` 消息发现其他 Metanet 节点
- 无需独立的节点发现协议
- 非 Metanet BSV 节点正常中继包含 ML Block 的交易

### 7.2 ON 节点的 BSV 交互

```
ON 节点 → BSV 网络:
  · 提交 ML Block (BSV 交易)
  · 监听 BSV 新区块 (发现新的 ML Block)
  · 获取 BSV 区块哈希 (用于挑战随机性)

ON 节点 → ON 节点:
  · 传播待确认 ON 交易 (ML Block 打包前)
  · ML Block 通知 (新块广播)
  · 合约协商 (Publisher ↔ Oracle ↔ SP)
```

### 7.3 连接方式

ON 节点通过 SPV 或 API 连接 BSV，不需要运行 BSV 全节点：
- **SPV**: 80 字节区块头 + Merkle path 验证
- **API**: 使用现有 BSV API 服务 (如 WhatsOnChain, GorillaPool)
- 类比: 类似 MetaMask 不需要运行 Ethereum 全节点

## 8. 双币支付场景

### 场景 1: 自托管 (Layer 2)

```
Publisher 运行 bitfs daemon (不涉及 ON)
Visitor ── BSV (x402) ──► Publisher daemon
全程 BSV, 无 MNT
```

### 场景 2: CDN 存储合约 (Layer 3)

```
Publisher ── MNT ──► Oracle ── MNT ──► Storage Provider
                                       (存储合约)
Visitor ── BSV (x402) ──► Storage Provider
                          (检索)
Publisher 用 MNT 签约, Visitor 用 BSV 检索
```

### 场景 3: 出块挖矿

```
Miner 构建 ML Block → BSV 交易
获得 MNT Coinbase 奖励
前提: 有活跃存储合约 (作为 SP 或 Oracle)
```

## 9. 反造假与安全机制

### 9.1 为什么无法造假存储 (vs Filecoin)

Filecoin 问题: 矿工用自生成垃圾数据"证明"容量，赚取 FIL。

我们的设计根本性不同:
- 存储合约是 **Publisher 与 Storage Provider 的双边协议**
- 合约需要 Publisher 签名（链上交易）
- Publisher 需要存入 MNT（没有理性 Publisher 付钱存无用数据）
- 出块资格检查的是 **"有真实 Publisher 付费的活跃合约"**，不是 "存了多少数据"

### 9.2 防串通 (论文 Section 5)

每个 Storage Provider 持有独立加密副本：
```
K_j = KDF(ECDH(oracle_priv, SP_j_pub))
C_j,i = chunks[i] ⊕ K_j,i
```
- SP_j 的 Merkle 树根 r_M,j 与其他 SP 不同
- SP_k 无法使用 SP_j 的数据伪造自己的存储证明
- 即使 m-1 个 SP 串通，也无法帮第 m 个 SP 伪造 (论文 Theorem 1)

### 9.3 数据丢失风险缓解

分层防御体系：

| 层级 | 机制 | 防护对象 |
|------|------|---------|
| 第一层 | Verify-then-pay 脚本 | 资金安全（超时自动退款） |
| 第二层 | 多副本冗余 (r ≥ 3) | 数据安全（单点失效不丢数据） |
| 第三层 | Provider 押金 (D_dep) | 违约赔偿（补偿迁移成本） |
| 第四层 | ON 链上声誉记录 | 长期信任（历史记录公开可查） |

## 10. ON 节点软件架构

```
metanet/ (Go 二进制)
├── cmd/metanet/           ← CLI 入口
│   ├── init               — 初始化节点 (生成密钥对)
│   ├── start [--mine]     — 启动节点, 可选挖矿
│   ├── stop               — 优雅关闭
│   ├── status             — 显示节点状态
│   ├── oracle             — Oracle 管理
│   └── contracts          — 查看存储合约
│
├── internal/
│   ├── ml/                ← ML Block 构建、解析、验证、链管理
│   ├── miner/             ← 轻量 POW 引擎 (SHA256 nonce 搜索)
│   ├── ledger/            ← MNT UTXO 集管理 (独立于 BSV UTXO)
│   ├── contract/          ← 存储合约生命周期 (创建/执行/终止)
│   ├── proof/             ← Merkle proof-of-retention 构建 & 验证
│   ├── oracle/            ← Oracle 服务 (挑战构建、加密管理、分发)
│   ├── overlay/           ← P2P 网络 (nServices 发现, ON 消息传播)
│   ├── spv/               ← BSV SPV 客户端 (监听 ML Block, 获取区块哈希)
│   └── storage/           ← 文件分片、加密存储、副本管理
│
└── spec/                  ← 模块规格说明
```

## 11. 参考文献

1. **GB2608179A** — Multi-level blockchain (Multilevel Blockchain 专利)
   - ML Block 结构, Carrier Pair 模型, Chain Input/Output 链接
2. **Craig Wright** — Blockchain-Verified Distributed Storage: A DHT-Based Architecture with Cryptographic Proof of Retention
   - Verify-then-pay 原子模型, Merkle 脚本验证, 押金调度, 防串通加密, 经济分析

## 附录 A: 与旧设计文件的对应关系

本设计替代以下旧设计中的相关章节：
- `design/metanet/1-ConceptDesign.zh.md` 中的 Layer 3 架构
- `design/metanet/2-SystemDesign.zh.md` 中的 Metanet Chain 章节
- `design/metanet/3-DetailedDesign.zh.md` 中的合并挖矿和区块结构章节
- `metanet/internal/chain/` 旧链代码（需要重写）
- `metanet/internal/mining/` 旧合并挖矿代码（需要重写）

旧设计中以下部分保持不变或仅需适配：
- 存储合约 (`internal/contract/`): 升级为 Verify-then-pay 脚本
- 存储证明 (`internal/proof/`): Merkle proof 逻辑保留, 适配新脚本格式
- Overlay 网络 (`internal/overlay/`): 适配 nServices 发现
- 配置 (`internal/config/`): 保留

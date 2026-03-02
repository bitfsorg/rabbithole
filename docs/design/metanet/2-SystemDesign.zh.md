# Metanet 系统设计

> 本文档为 Metanet 设计文档体系的第二层：模块划分、接口定义、数据流。
>
> **文档体系**:
> - [整体设计](../0-OverallDesign.zh.md) — 两产品生态、三层架构、界面划分
> - [概念设计](1-ConceptDesign.zh.md) — 产品定位、核心理念、设计原则
> - **系统设计** (本文档) — 节点架构、合约、支付通道
> - [详细设计](3-DetailedDesign.zh.md) — 共识、挖矿、结算协议细节
> - [测试设计](4-TestDesign.zh.md) — 测试用例设计

---

## 一、三层架构

| 层 | 名称 | 职责 | Token |
|---|------|------|-------|
| Layer 1 | BSV Main Chain | 文件所有权 (Metanet DAG), HTLC 购买, x402 下载 | BSV |
| Layer 2 | Self-hosted Daemon | 自托管文件服务, 现有 `bitfs daemon` | 无 (自己的服务器) |
| Layer 3 | Metanet Overlay Network (ON) | 去中心化 CDN, Verify-Then-Pay 存储合约, 支付通道 | MNT Token |

**用户三种选择**:

| 选择 | 适用场景 | 成本 | 可用性 |
|------|---------|------|--------|
| 仅上链 (BSV) | 小文件, 永久存储 | BSV 矿工费 (一次性) | 区块链级别 |
| 自托管 (Daemon) | 大文件, 完全控制 | 服务器成本 (持续) | 取决于自己的基础设施 |
| Metanet Chain 托管 | 大文件, 去中心化 | MNT Token (持续) | CDN 级别 (多 Metanet Node 缓存) |

三种方式可组合: 元数据上链 + 热门内容 Metanet Chain 托管 + 冷门内容自托管。

**主链与 Overlay 网络的职责划分**:

| | BSV Main Chain | Metanet Overlay Network (ON) |
|---|---|---|
| 本质 | L1 基础工作量证明链 | 构建在 BSV 上的叠加网络 |
| 共识 | SHA256 PoW | 纯 SHA256 PoW (独立挖矿竞赛 ML Block 出块权) |
| 交易格式 | 标准 Bitcoin 交易 | **ML Block 嵌入合法 BSV 交易中被确认** |
| 验证机制 | 矿工验证 Script | ON 节点验证 Verify-Then-Pay 脚本执行结果 |
| 代币 | BSV | MNT Token (记录为 ON 维护的底层 UTXO) |
| 用途 | 文件所有权, 最终性背书, x402 购买 | CDN 激励, MNT 存储合约 (Verify-Then-Pay) |
| 参与角色 | 终端用户, Agent, BSV 矿工 | Publisher, Storage Provider, Oracle, Miner |

---

## 二、Metanet Overlay Network (ON) 基本设计

**BSV 上的 ML Overlay**:
Metanet 基于 Carrier Pair 模型构建为 BSV 的叠加网络。每一笔 ON 交易以及每一个 ML Block (MNT 代币区块) 都是一笔合法的、标准的 BSV 交易。
- ON 节点无需 fork BSV C++ 代码，通过轻量 Go 服务与 SPV/API 交互。
- 采用 Verify-Then-Pay 原子模型进行存储合约结算，不需要自定义虚拟机，所有验证都在标准 Bitcoin Script 中完成。

**四角色模型**:
在 ON 网络中，引入四个解耦角色：
1. **Miner (矿工)**: 收集 ON 交易，构建 ML Block，执行 SHA256 POW（竞争出块，无任何存储前置条件）。
2. **Storage Provider (存储节点)**: 存储 Publisher 的加密数据副本，通过提交 Merkle 存储证明来解锁合约付款。
3. **Oracle (服务提供方)**: 协调建立合约，负责为每个 Provider 生成独立加密副本、构建挑战。
4. **Publisher (内容发布者)**: 提供数据，支付 MNT 以购买存储服务。

**MNT Token**:
MNT Token 是 ML 链原生 UTXO（账本随 ML Block 嵌入 BSV 交易中，由 ON 节点独立维护）：
- 总供应量: 21,000,000 MNT
- 初始区块奖励: 50 MNT (出块时长 5 分钟，是比特币的双倍心跳)
- 减半周期: 每 210,000 块 (约 2 年减半)，确保前 6 年有足够的 CDN 激励。
- 最小单位: 1 satoshi = 0.00000001 MNT

**安全假设**：
所有的 ML Block 都直接作为普通 BSV 交易被确认。因此最终性和不可篡改性由 BSV 主网提供，ON 网络不需要额外的合并挖矿或定期的 BSV 锚定。

---

## 三、Metanet Node 经济模型

**Metanet Node 三种收入**:

| 收入来源 | 支付方 | 币种 | 触发条件 |
|---------|-------|------|---------|
| x402 检索费 | 用户/Agent | BSV | 用户下载内容 |
| CDN 托管费 | Owner | MNT Token | Owner 签存储合约 |
| 挖矿奖励 | 协议 | MNT Token | 出块奖励 |

**双币种分工**:
- **BSV**: 面向终端用户和 Agent, 用于 x402 微支付和 HTLC 购买
- **MNT Token**: 面向 Metanet Node 市场, 用于 CDN 托管费和挖矿奖励
- **普通用户不需要接触 Metanet Chain/Token** — 只用 BSV 即可使用 BitFS
- Token 需求 = Owner 对 CDN 服务的需求 (不是用户的需求)

**独立 `metanet` CLI**:

```
bitfs daemon                  # BitFS 自托管模式 (Layer 2)
metanet start                 # Metanet Node 模式 (Layer 3, 对外提供 CDN 服务)
metanet start --mine          # Metanet Node + 矿工模式 (Layer 3, CDN + 挖矿)
```

`bitfs` 和 `metanet` 是独立二进制, 共享核心 Go 库。`bitfs daemon` 面向文件拥有者, `metanet` 面向 CDN 节点运营者。

---

## 四、热数据: CDN 自组织模式

**核心机制**: Metanet Node 自发缓存热门内容, 利润驱动, 无需协议层管理。

<table style="width:100%; border-collapse:collapse; margin:0.8em 0; font-size:10pt; border:2px solid #333;">
<tr>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#eaf0f7; font-weight:600;">文件热度高</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#f0f7ea; font-weight:600;">x402 收入高</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#f7f0ea; font-weight:600;">更多 Node 缓存</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#fff;">→</td>
<td style="border:1px solid #999; padding:0.5em; text-align:center; background:#f5eaf7; font-weight:600;">可用性更好</td>
</tr>
<tr>
<td colspan="7" style="border:1px solid #999; padding:0.3em; text-align:center; font-size:9pt; color:#555; background:#fafafa;">↻ 正反馈循环: 用户体验提升 → 文件热度更高</td>
</tr>
</table>

**特点**:
- 不需要存储合约 (Metanet Node 自愿缓存)
- 不需要存储证明 (x402 交易记录本身证明 Metanet Node 有数据)
- 不需要副本管理 (市场自动调节副本数)
- 越热门的内容, 越多 Metanet Node 缓存, 类似传统 CDN 的缓存逻辑

**Metanet Node 获取数据的方式**:
1. Owner 主动推送: `bitfs put --store metanet` 上传到 Metanet Chain
2. Metanet Node 从 Owner daemon 拉取: 支付 x402 费用获取数据
3. Metanet Node 间批发: Node_A 从 Node_B 购买热门数据 (Token 支付通道)

**Metanet Node 决策逻辑**:
```
if 文件 x402_revenue > storage_cost + bandwidth_cost:
    缓存该文件 (利润驱动)
else:
    不缓存 (除非有存储合约)
```

---

## 五、冷数据: Verify-Then-Pay 合约模式

**核心机制**: 存储付款预先锁定在具备验证逻辑的 UTXO 中。Storage Provider 只有在提交有效的 Merkle 存储证明时，才能直接触发 Bitcoin Script 原子解锁付款。

**三方协作流程**:

```
Phase 1: 数据准备 (Oracle 执行)
1. Publisher 将数据提交给 Oracle 中介
2. Oracle 进行数据分片，并为每个 Storage Provider 生成独立的防串通双层加密副本
3. Oracle 为每个副本构建独立的 Merkle 树，并将加密数据分发给对应 Node

Phase 2: 链上合约创建 (ON 交易)
Oracle 协调各方，创建一笔 ON 交易：
- 输入：Publisher 付出的总存储费 (MNT)，Storage Provider 存入的违约押金 (MNT)
- 输出：N 个包含 Verify-Then-Pay 脚本的 Challenge UTXOs（每期一个）；1个押金 UTXO；Oracle 服务费

Phase 3: 合约执行与证明
1. 每个期数 k 根据该时刻的 BSV 区块哈希提供挑战随机性，以选择被查验的块。
2. Storage Provider 构建包含其签名和 Merkle Proof 的花费交易。
3. ON 节点运行标准 Bitcoin Script，一旦证明哈希与承诺哈希匹配，即刻支付该期 Token。
4. 如果超时仍未提供证明，Oracle 可将此 Challenge UTXO 追回；连续超时将触发押金没收。
```

**副本策略**: 协议不管副本策略 — Publisher 可向 Oracle 购买不同级别的冗余度 (r ≥ 3)，市场决定。

**与 Filecoin 对比**:

| | Filecoin | Metanet Overlay Network |
|---|---|---|
| 副本独立性 | PoRep (zk-SNARK) | ECDH 防串通加密 (Method 42) |
| 持续存储证明 | PoSt (zk-SNARK) | Verify-Then-Pay 脚本挑战-响应 |
| 计算成本 | GPU 密集, 数小时 | 毫秒级 ECDH + Merkle 验证 |
| 挖矿前置条件 | 大量存储硬件与抵押 | 纯算力竞赛，完全与存储解耦 |
| 检索激励 | 薄弱 (检索矿工无激励) | 强 (x402 直接收入) |
| 代币用途 | 存储+检索+抵押 | 仅 CDN 托管+出块 (用户用 BSV) |

---

## 六、内容分成

**分成模式**: Metanet Node 与 Owner 分享 x402 收入。

Owner 在 Metanet payload 中设置 `revenue_share` 字段 (TLV tag 24, uint32):

- 值为 0-10000 的 basis point，表示 Owner 从 x402 收入中获得的分成比例
- 例如 `revenue_share = 3000` 表示 Owner 获得 30%，Metanet Node 获得 70%

Metanet Node 在服务内容时读取此字段，自动按比例分配 x402 收入。`min_price_per_kb` 等策略参数由 Metanet daemon 配置管理，不写入链上 TLV。

**两种合作模式**:

| 模式 | 适用场景 | Owner 付出 | Owner 收入 |
|------|---------|-----------|-----------|
| 分成模式 (热数据) | 热门内容 | 无 (Metanet Node 自愿缓存) | x402 收入的 owner_percent |
| 付费模式 (冷数据) | 冷门内容 | MNT Token (存储合约) | 无 x402 收入 (或极少) |

---

## 七、x402 支付通道

> **x402 基础协议**: x402 带宽计费规则、HTTP API (`POST /_bitfs/pay/{invoice_id}`)、免费配额逻辑、Invoice 验证流程等基础实现定义在 BitFS 设计文档中 — 见 [BitFS 系统设计 十三节](../bitfs/2-SystemDesign.zh.md#十三daemon-配置-lfcp) 和 [BitFS 详细设计 十三-B.C](../bitfs/3-DetailedDesign.zh.md#c-x402-支付流程)。本节仅描述 Metanet Chain 引入的支付通道扩展。

**两种支付通道**:

| 通道类型 | 方向 | 币种 | 用途 |
|---------|------|------|------|
| BSV 通道 | User <-> Metanet Node | BSV | x402 流媒体微支付 |
| Token 通道 | Owner <-> Metanet Node | MNT Token | CDN 托管费持续支付 |
| Token 通道 | Metanet Node <-> Metanet Node | MNT Token | 节点间数据批发 |

---

## 八、BSV <-> Overlay Network 交互

**文件发布 + CDN 托管流程**:
```
1. Publisher: bitfs put --store metanet myfile.txt
   ├── 仅需支付少量 MNT，委托给 Oracle
   ├── Oracle: 数据分片、独立加密、建立多份分发
   └── ON 交易: Oracle 创建 Verify-Then-Pay Challenge UTXO 集合

2. Storage Provider 存储数据，定期提交含数据块及 Merkle Proof 的解答，原子获取当期 MNT 奖励

3. User (Visitor): bget bitfs://example.com/myfile.txt
   ├── 查询 BSV: 解析 Metanet 路径
   ├── 查询 ON 交易 / DHT 路由: 发现持有该内容的 Storage Provider 列表
   └── 请求 Storage Provider: 使用 BSV x402 进行微支付 → 获取加密数据并本地解密
```

---

## 九、BRC 标准兼容

**Metanet Chain 采用 BRC (Overlay 扩展)**:

| BRC | 名称 | 用途 |
|-----|------|------|
| BRC-31 | Overlay Network | 节点发现与路由 |
| BRC-22 | SHIP | 提交交易到 Overlay |
| BRC-23 | SLAP | 查询 Overlay 服务 |
| BRC-24 | Topic Manager | 管理 Overlay topic |
| BRC-25 | Lookup Service | 查询 Overlay 数据 |
| BRC-87 | Overlay Ads | 广告可用服务 |
| BRC-88 | Overlay Tracking | 追踪 Overlay 状态 |
| BRC-64 | Overlay Host | 托管 Overlay 节点 |
| BRC-103 | Overlay Admin | 管理 Overlay 节点 |
| BRC-104 | Overlay Sync | 同步 Overlay 状态 |

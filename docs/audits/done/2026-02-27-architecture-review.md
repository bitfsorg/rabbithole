# BitFS/Metanet 架构审查报告

> **日期**: 2026-02-27
> **范围**: 全项目架构设计、密码学协议、经济模型、战略定位
> **审查人**: Claude Opus 4.6 (区块链协议 & 密码学视角)
> **输入**: design/ 全套设计文档、libbitfs-go 源码、bitfs/docs/spec/ 规格说明、2026-02-26 代码审计报告
> **状态**: All P0 security findings fixed. P1/P2 architecture and economic model items addressed or deferred per design decisions. HTLC timeout (3.6) deferred to `deferred.md`. Archived 2026-03-03.

---

## 一、总体评价

密码学和协议设计达到**生产级水准**（修复 P0 后）。经济模型**理论优雅但缺少落地机制**（节点发现、Sybil 抵抗、审计证明）。战略上需要聚焦一个 killer app 场景来打破冷启动困局。

---

## 二、架构优势

### 2.1 Retrieval-first 激励模型

Filecoin 激励存储（导致 zk-SNARK 硬件门槛极高），Arweave 一次性付费永存（不可持续）。BitFS/Metanet 激励**检索**——热数据自组织复制、冷数据显式合约——这是更自然的经济循环。

```
Hot data loop: popular → x402 revenue ↑ → more nodes cache → availability ↑ → demand ↑ → x402 ↑
```

这个正反馈循环不需要：
- Proof-of-Replication（昂贵的 zk-SNARK）
- 强制冗余（浪费容量）
- 中心化调度器

### 2.2 Method 42 密码学设计

BIP32 代数结构保留使得目录树级别的访问授权成为可能：

- `S_child = S_parent + offset × P_buyer` 允许级联访问，无需重新加密整棵子树
- `key_hash = SHA256(SHA256(plaintext))` 双重哈希：同时用于密钥派生和内容承诺
- 三种访问模式（PRIVATE/FREE/PAID）语义清晰，映射到 ECDH 参数

### 2.3 数据/元数据分离

链上只存元数据，内容可以在任何地方——这避免了链膨胀，也让 off-chain 存储成为一等公民。对比 Arweave 的全链存储，这是更务实的架构选择。

### 2.4 双币设计

终端用户只接触 BSV（文件操作），节点运营商处理 MNT（CDN 经济）——认知负担按角色分层。

### 2.5 竞品对比

| 方面 | BitFS/Metanet | Filecoin | Arweave |
|------|---------------|----------|---------|
| **证明复杂度** | ECDH+Merkle (ms) | zk-SNARK (数小时 GPU) | SPoRA (较慢) |
| **硬件门槛** | 低 (任何服务器) | 极高 (GPU 必需) | 中等 (CPU+存储) |
| **热/冷数据** | 自组织 / 显式合约 | 统一存储合约 | 永久存储 |
| **激励模型** | 检索 (x402) | 存储 (PoRep) | 存储 (一次性) |
| **数据隐私** | Method 42 ECDH | 不加密 | 可选加密 |
| **运营商收益** | 按访问计费 | 按字节/区块 | 按字节/永久 |

---

## 三、协议安全问题

### P0: 必须修复

#### 3.1 HTLC capsule_hash 未绑定文件身份

**风险**: 恶意 seller 可返回任意 capsule，只要哈希值匹配。buyer 拿到的 capsule 可能解密出错误的内容。

**当前实现**:
```
capsule_hash = SHA256(capsule)
```

**建议修复**:
```
capsule_hash = SHA256(file_txid ‖ capsule)
```

将 capsule 与特定文件的链上交易绑定，seller 无法用其他文件的 capsule 替换。

**影响范围**: libbitfs-go/method42/capsule.go, bitfs/internal/daemon/payment.go

#### 3.2 PRIVATE 模式元数据加密使用弱 salt

**风险**: P_node 是公开信息。有 HKDF oracle 访问权限的攻击者可以暴力破解 ikm。

**当前实现**:
```
meta_key = HKDF-SHA256(
    ikm  = ECDH(D_node, P_node).x,
    salt = SHA256(P_node),          // ← P_node 是公开的
    info = "bitfs-metadata-encryption"
)
```

**建议修复** (任选一):
- **方案 A**: `salt = random(16B)`，存储在密文旁边
- **方案 B**: `salt = ECDH(D_node, P_node).y`（椭圆曲线 y 坐标，不直接公开）

**影响范围**: libbitfs-go/method42/encrypt.go

**修复状态**: 已修复。采用方案 A — `salt = random(16B)`，存储为 EncPayload 前缀。
EncPayload 新格式: `salt(16B) || nonce(12B) || AES-GCM(TLV) || tag(16B)`。

#### 3.3 SPV 不验证 PoW 难度

**风险**: 攻击者可构造低难度假区块头（几毫秒即可生成），SPV 客户端会接受其中的虚假交易。

**当前状态**: 2026-02-26 代码审计已标记为 HIGH (#2)，尚未修复。

**建议修复**: 在 `spv.HeaderStore` 中存储最佳链累积难度，拒绝低于阈值的区块头。新头的 `nBits` 必须满足难度调整算法的预期值。

**影响范围**: libbitfs-go/spv/verify.go, libbitfs-go/spv/headerchain.go

#### 3.4 HTLC 无 replay protection

**风险**: buyer 可重用相同的 HTLC txid 向不同 seller 购买。

**建议修复**: 在 HTLC 脚本中添加 `buyer_nonce` 或 `invoice_id`，确保每个购买事务唯一。

**影响范围**: libbitfs-go/x402/htlc_tx.go

---

### P1: 应当修复

#### 3.5 Nonce 重用假设需要显式约束

**风险**: 设计假设 "同一文件不会重新加密 2^48 次"，但以下场景可能违反：
- 用户将加密文件复制到多个路径
- 同一文件被多次访问授权
- 缓存层重新加密

**建议**:
- 文档化显式约束：每个 `(file, D_node, P_node, key_hash)` 元组生成唯一 aes_key
- 或改用确定性 AEAD（如 ChaCha20-Poly1305 + 序列号）

#### 3.6 HTLC 退款超时风险

**风险**: 144 块（约 1 天）的退款窗口意味着：
- 用户钱包离线超过 24 小时就无法恢复资金
- Seller 可在第 143 块广播竞争交易

**建议**:
- 缩短默认超时至 72 块（约 12 小时）
- 提供可配置参数
- 文档化钱包必须在超时前保持在线

#### 3.7 x402 invoice 内存泄漏

**风险**: `Server.invoices` map 不清理过期 invoice，长时间运行后无限增长。

**建议**: 实现 TTL 淘汰机制或环形缓冲区。每 N 分钟扫描并删除过期 invoice。

**影响范围**: bitfs/internal/daemon/payment.go

#### 3.8 Capsule 分析导致的密钥泄漏风险

**风险**: 在 HTLC 流程中：
```
capsule = aes_key XOR buyer_mask
buyer_mask = HKDF-SHA256(ECDH(D_node, P_buyer).x, key_hash, "bitfs-buyer-mask")
```
如果 seller 对多个 buyer 重用 P_buyer，统计分析可能泄漏 buyer_mask。

**建议**: 添加密码学证明 buyer_mask 正确派生（零知识证明 ECDH 正确性），或确保每次交易使用唯一的 P_buyer。

---

## 四、经济模型盲区

### 4.1 Metanet 节点发现和冷启动

**现状**: 设计文档未定义节点发现机制。

**问题**: 没有节点就没有 CDN，没有 CDN 就没有用户——经典的鸡蛋问题。

**建议方案** (按阶段):
1. **Phase 0 (Bootstrap)**: 官方运营 3-5 个种子节点，提供免费存储配额
2. **Phase 1 (Discovery)**: DNS SRV 记录 `_bitfs._tcp.metanet.org` + 硬编码种子列表
3. **Phase 2 (DHT)**: Kademlia DHT 做节点发现，类似 BitTorrent 的 bootstrap 机制
4. **Phase 3 (Reputation)**: 链上声誉系统，基于历史服务质量和质押金额排名

### 4.2 Sybil 抵抗机制

**现状**: 节点身份仅是一个 BIP32 pubkey，攻击者可无限创建节点。

**风险场景**:
- 攻击者创建 1000 个节点，参与存储合约但不提供服务
- 攻击者用大量节点稀释其他节点的检索收入

**建议方案**:
- **质押机制**: 节点必须质押 N MNT 才能接单，作恶扣除
- **渐进信任**: 新节点只能接小合约，随服务历史逐步解锁大合约
- **工作量证明**: 加入网络需完成一次 PoW（类似 Hashcash），提高 Sybil 成本

### 4.3 收入分成审计

**现状**: 缺少检索计数的可信证据。

**问题**: 如果内容所有者说节点只服务了 1M 次请求，但节点声称 2M 次——谁是对的？

**建议方案**:
- **签名日志**: 每次检索生成 `sig(buyer_pubkey, timestamp, file_hash)`，buyer 和 node 各持一份
- **链上 checkpoint**: 每 N 次检索向链上提交一次累计哈希
- **挑战机制**: Owner 可随机挑战 node 出示特定时段的签名日志

### 4.4 冷存储担保模型

**现状**: 未定义节点丢失副本后的补偿机制。

**建议方案**:
- **故障罚金**: 存储合约包含质押金，challenge 失败扣除
- **自动迁移**: 合约包含 "fallback node" 列表，主节点失效后自动迁移
- **保险池**: 所有节点按比例向池中缴纳 MNT，用于补偿数据丢失

### 4.5 Revenue sharing 博弈论

**现状**: revshare 比例由内容所有者设定，缺少博弈分析。

**风险**: 如果一个节点运营商垄断了热门内容，可以任意定价。

**建议分析**:
- Owner 分成 10-30%、Node 分成 70-90% 是合理区间
- 需要机制防止内容垄断：同一内容至少 N 个节点可提供服务
- 考虑引入「最低复制数」要求

---

## 五、战略层面

### 5.1 BSV 链风险

BSV 生态极小——这既是优势（低竞争、大区块、低费用）也是风险（开发者少、交易所支持有限、品牌形象问题）。

**建议**: 即使初始实现在 BSV 上，协议层应做**链无关抽象**。`BlockchainService` 接口已经提供了这个基础——确保所有业务逻辑不直接依赖 BSV 特有特性（如 OP_RETURN 大小限制、特定 sighash 类型），而是通过接口层隔离。保留未来迁移到其他 UTXO 链（如 BCH、LTC）的可能性。

### 5.2 Killer App 场景

技术再好，没有第一个让用户 "不得不用" 的场景就不会起飞。两个最有潜力的方向：

**方向 A: AI Agent 数据存储**
- Agent 需要持久化、可寻址、可付费的数据存储
- `bitfs://` URI 天然适合 Agent 间数据交换
- Method 42 加密确保 Agent 数据隐私
- x402 支付协议让 Agent 可以自主购买数据
- **优势**: 当前最热赛道，竞品（IPFS/Filecoin）对 Agent 不友好

**方向 B: 付费内容分发**
- 创作者直接卖内容，x402 自动收款，revshare 自动分账
- 比 Patreon/Substack 去中心化，比 Lightning Network 更简单
- **挑战**: 需要前端生态（浏览器扩展、移动 App）

### 5.3 Metanet 商标风险

USPTO #7300182（第 42 类）已被注册。如果要公开发布产品，命名问题必须优先解决。建议提前准备备选名称。

---

## 六、修复优先级路线图

```
Phase 1 (P0 安全修复, 1-2 周):
  ├── 3.1 HTLC capsule_hash 绑定 file_txid
  ├── 3.2 PRIVATE 元数据加密 salt 修复
  ├── 3.3 SPV PoW 难度验证
  └── 3.4 HTLC replay protection

Phase 2 (P1 加固, 2-4 周):
  ├── 3.5 Nonce 重用约束文档化
  ├── 3.6 HTLC 超时可配置化
  ├── 3.7 x402 invoice TTL 淘汰
  └── 3.8 Capsule 分析防护

Phase 3 (经济模型落地, 4-8 周):
  ├── 4.1 节点发现 Phase 0-1 (种子节点 + DNS SRV)
  ├── 4.2 Sybil 抵抗 (质押 + 渐进信任)
  ├── 4.3 收入分成审计 (签名日志)
  └── 4.4 冷存储担保 (故障罚金)

Phase 4 (战略, 持续):
  ├── 5.1 协议层链无关抽象
  ├── 5.2 Killer App MVP (Agent 数据存储 或 付费内容)
  └── 5.3 商标问题解决
```

---

## 七、结论

BitFS/Metanet 在密码学和协议层面展现了高水平的设计能力。检索激励模型是相对于 Filecoin/Arweave 的差异化优势。当前最大的技术债务是 4 个 P0 安全问题和经济模型落地机制的缺失。战略上，建议以 AI Agent 数据存储作为切入点打破冷启动困局。

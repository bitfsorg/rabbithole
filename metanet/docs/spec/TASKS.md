# 实现任务分解

> 任务按依赖关系排序。每个阶段都建立在前一个阶段之上。
> 验收标准映射到测试设计文档（4-TestDesign.zh.md）。

---

## 第一阶段：链核心

基础层。除 go-sdk 外无外部依赖。必须在任何其他阶段之前完成。

### 任务 1：internal/chain — 链参数与代币经济

**包名**：`internal/chain`
**描述**：实现 Metanet Chain 共识参数、代币经济（MNT）、区块头类型和创世区块定义。这是整个系统的基石——所有其他包都依赖这些常量和类型。

**文件**：
- `params.go` — 链参数结构体和主网/测试网值
- `block.go` — Block 和 BlockHeader 类型、序列化、哈希
- `token.go` — 区块奖励计算、减半计划、供应量计算
- `genesis.go` — 创世区块定义和哈希
- `errors.go` — 包错误类型

**验收标准**：
- [ ] MainNetParams() 返回正确的参数（2100 万供应量、50 MNT 奖励、210K 减半、32MB 区块、10 分钟出块）
- [ ] TestNetParams() 返回适合测试的独立参数
- [ ] BlockReward(height) 对所有减半纪元（0 到 33+）返回正确的奖励
- [ ] BlockReward 在最后一个减半纪元之后返回 0
- [ ] TotalSupplyAtHeight 正确地将所有奖励累加到指定高度
- [ ] TotalSupplyAtHeight 在最终高度等于 2,100,000,000,000,000 聪（2100 万 MNT）
- [ ] GenesisBlock() 在 coinbase 中包含消息 "Metanet: Decentralized CDN on Bitcoin"
- [ ] GenesisBlockHash() 在不同构建之间是确定性且稳定的
- [ ] SerializeBlockHeader/DeserializeBlockHeader 往返正确
- [ ] HashBlockHeader 生成 80 字节序列化区块头的双重 SHA256

**预估测试数**：18

---

### 任务 2：internal/mining — 合并挖矿与 BSV 锚定

**包名**：`internal/mining`
**描述**：实现与 BTC/BSV 合并挖矿的 AuxPoW（辅助工作量证明）验证、coinbase 承诺构造/解析、难度调整算法，以及 BSV 锚定交易格式。

**文件**：
- `auxpow.go` — AuxPoW 区块头类型、coinbase 承诺构造
- `validate.go` — AuxPoW 验证逻辑（父链区块头、coinbase 分支、承诺）
- `difficulty.go` — 难度调整算法（每 2016 个区块，与 Bitcoin 相同）
- `anchor.go` — BSV 锚定交易构造和验证（MNTA 格式）
- `errors.go` — 包错误类型

**验收标准**：
- [ ] BuildCoinbaseCommitment 生成 `OP_RETURN MNMP <block_hash>` 格式
- [ ] FindAuxPoWCommitment 正确从 coinbase 中提取区块哈希
- [ ] FindAuxPoWCommitment 对不含 MNMP 标记的 coinbase 返回 nil
- [ ] ValidateAuxPoW 接受有效的合并挖矿证明
- [ ] ValidateAuxPoW 拒绝包含错误区块哈希的证明
- [ ] ValidateAuxPoW 拒绝包含无效 coinbase 分支的证明
- [ ] ValidateAuxPoW 拒绝未达到难度目标的证明
- [ ] VerifyCoinbaseBranch 正确验证 Merkle 包含性
- [ ] CalcNextDifficulty 与 Bitcoin 算法一致（限幅到 4 倍，相同公式）
- [ ] CalcNextDifficulty 将时间跨度限制在 [expected/4, expected*4]
- [ ] CompactToBig/BigToCompact 往返正确
- [ ] HashMeetsTarget 正确比较哈希与紧凑目标
- [ ] SerializeAnchorData/DeserializeAnchorData 往返正确
- [ ] BuildAnchorTx 生成正确的 MNTA 格式
- [ ] ValidateAnchorChain 检测断裂的链式连接
- [ ] BuildBlockRangeMerkleRoot 计算正确的 Merkle 根

**预估测试数**：22
**依赖**：任务 1

---

## 第二阶段：经济层

存储合约和证明。依赖第一阶段的链类型。

### 任务 3：internal/contract — 存储合约

**包名**：`internal/contract`
**描述**：使用 Bitcoin Script 实现 StorageDeal 交易构造。每个合约创建 N 个 UTXO（每个挑战周期一个），挑战从合约 TxID 确定性推导。Metanet Node 通过提交有效证明来领取付款；所有者在 CLTV 过期后可退款。

**文件**：
- `deal.go` — StorageDeal 类型、参数验证、UTXO 生成
- `challenge.go` — 从合约 TxID 确定性推导挑战
- `script.go` — Bitcoin Script 构造（OP_IF/ELSE 分支）
- `merkle.go` — 用于预计算期望哈希的简单 Merkle 树
- `errors.go` — 包错误类型

**验收标准**：
- [ ] ComputeChallenge 是确定性的：相同的 (txid, period, numChunks) 始终产生相同结果
- [ ] ComputeChallenge 对不同周期产生不同结果
- [ ] ComputeChallenge 的 chunk_index 始终在 [0, numChunks) 范围内
- [ ] BuildDealScript 生成正确的 OP_IF/ELSE/ENDIF 结构
- [ ] BuildDealScript 包含正确的 OP_CHECKSIGVERIFY 和 OP_SHA256
- [ ] BuildDealScript 包含正确的 OP_CHECKLOCKTIMEVERIFY
- [ ] BuildClaimInput 生成 `<sig> <proof_data> OP_TRUE`
- [ ] BuildRefundInput 生成 `<sig> OP_FALSE`
- [ ] ComputeExpectedHash 匹配 SHA256(proof || chunk_data)
- [ ] NewStorageDeal 创建正确数量的 UTXO
- [ ] ValidateDealParams 拒绝零周期、零付款、无效密钥
- [ ] StorageDeal -> T1.1（N 个包含预计算 expected_proof_hash 的 UTXO）
- [ ] 挑战确定性 -> T1.2（可复现，每个 k 不同）

**预估测试数**：16
**依赖**：任务 1

---

### 任务 4：internal/proof — 存储证明与 ECDH 加密

**包名**：`internal/proof`
**描述**：实现 ECDH 双层加密（Method 42 第一层 + 节点专用第二层）、对加密分片的 Merkle 树构造，以及挑战-响应证明的生成/验证。

**文件**：
- `encrypt.go` — ECDH 双层加密（DeriveNodeKey、EncryptForNode）
- `merkle.go` — SHA256 Merkle 树构造和证明生成
- `verify.go` — 存储证明验证（完整流水线）
- `serialize.go` — 证明数据序列化/反序列化
- `errors.go` — 包错误类型

**验收标准**：
- [ ] DeriveNodeKey 对不同的节点公钥产生不同的密钥
- [ ] EncryptForNode 对不同的节点产生不同的密文
- [ ] EncryptForNode 的密文与输入不同（已应用双重加密）
- [ ] BuildMerkleTree 对已知测试向量生成正确的根
- [ ] BuildMerkleTree 处理奇数叶子（复制最后一个）
- [ ] GenerateMerkleProof 为任意叶子生成有效证明
- [ ] VerifyMerkleProof 接受有效证明
- [ ] VerifyMerkleProof 拒绝包含错误分片数据的证明
- [ ] VerifyMerkleProof 拒绝包含错误兄弟节点的证明
- [ ] ComputeProofHash 匹配 expected_hash 格式
- [ ] VerifyStorageProof 接受完整的有效证明
- [ ] VerifyStorageProof 拒绝错误的分片索引
- [ ] VerifyStorageProof 拒绝被篡改的分片数据
- [ ] SerializeProofData/DeserializeProofData 往返正确
- [ ] ECDH 唯一性 -> T3.1（提供者密文 != 所有者密文）
- [ ] Merkle 证明 -> T2.1、T2.2、T2.3（正确/错误/不匹配的证明）

**预估测试数**：20
**依赖**：任务 1、任务 3（用于 ComputeChallenge）

---

## 第三阶段：网络层

支付通道和覆盖网络。依赖第一阶段的链类型和第二阶段的合约/证明类型。

### 任务 5：internal/payment — 支付通道

**包名**：`internal/payment`
**描述**：实现双支付通道（BSV 和 MNT）。包括 2-of-2 多重签名资金锁定、带序列号的链下承诺更新、基于撤销的争议解决，以及 x402 HTTP 头协议扩展。

**文件**：
- `channel.go` — 通道类型、状态管理、参数
- `funding.go` — 资金交易构造（2-of-2 多重签名）
- `commitment.go` — 承诺交易构造、状态更新
- `dispute.go` — 撤销密钥、惩罚交易
- `voucher.go` — x402 支付凭证编码/解码
- `errors.go` — 包错误类型

**验收标准**：
- [ ] BuildFundingTx 创建正确的 2-of-2 多重签名输出
- [ ] OpenChannel 初始化时全部容量在发起方
- [ ] UpdateChannel 转移正确金额，递增序列号
- [ ] UpdateChannel 返回前一个状态的撤销密钥
- [ ] CloseChannelCooperative 生成有效的结算交易，无争议窗口
- [ ] CloseChannelUnilateral 广播最新的承诺交易
- [ ] BuildPunishmentTx 使用撤销密钥领取全部余额
- [ ] VerifyVoucher 接受有效的签名承诺更新
- [ ] VerifyVoucher 拒绝过期或被篡改的凭证
- [ ] EncodeVoucher/DecodeVoucher 往返正确
- [ ] FormatChannelID/ParseChannelID 往返正确
- [ ] 支付通道开启 -> T4.2
- [ ] 支付通道更新 -> T4.3
- [ ] 支付通道关闭 -> T4.4

**预估测试数**：18
**依赖**：任务 1

---

### 任务 6：internal/overlay — BRC 覆盖网络

**包名**：`internal/overlay`
**描述**：实现符合 BRC 标准的覆盖网络，用于节点发现、内容路由和服务广告。使用 BRC-31、BRC-22、BRC-23、BRC-24、BRC-25 和 BRC-87。

**文件**：
- `service.go` — OverlayService、初始化、生命周期
- `discovery.go` — 对等节点发现、内容定位
- `advertise.go` — 节点广告、签名
- `topic.go` — 主题管理（订阅、取消订阅）
- `store.go` — PeerStore 和 TopicStore 接口 + 内存实现
- `errors.go` — 包错误类型

**验收标准**：
- [ ] NewOverlayService 使用本地节点正确初始化
- [ ] Advertise 生成签名广告
- [ ] VerifyAdvertisement 接受有效签名
- [ ] VerifyAdvertisement 拒绝伪造签名
- [ ] Discover 返回匹配的节点
- [ ] LocateContent 找到拥有特定内容的节点
- [ ] RegisterTopic/UnregisterTopic 正确管理订阅
- [ ] PruneStalePeers 移除过期对等节点
- [ ] 内存 PeerStore 的添加/移除/列表操作

**预估测试数**：14
**依赖**：任务 1

---

## 第四阶段：CLI

命令行界面。依赖所有其他阶段。

### 任务 7：cmd/metanet — Metanet Node CLI

**包名**：`cmd/metanet`
**描述**：实现 `metanet` CLI 二进制文件，包含子命令：init、start、stop、status、contracts、peers、mine。使用 cobra 或标准 flag 包。

**文件**：
- `main.go` — 入口点、命令注册
- `cmd_init.go` — `metanet init` 实现
- `cmd_start.go` — `metanet start` 实现
- `cmd_stop.go` — `metanet stop` 实现
- `cmd_status.go` — `metanet status` 实现
- `cmd_contracts.go` — `metanet contracts` 实现
- `cmd_peers.go` — `metanet peers` 实现
- `cmd_mine.go` — `metanet mine` 实现

**验收标准**：
- [ ] `metanet init` 创建数据目录、生成密钥对、写入配置
- [ ] `metanet init` 使用 `--testnet` 时使用测试网参数
- [ ] `metanet init` 在已初始化时优雅失败
- [ ] `metanet status --json` 生成有效的 JSON 输出
- [ ] `metanet contracts --json` 以 JSON 格式列出合约
- [ ] `metanet peers --json` 以 JSON 格式列出对等节点
- [ ] 所有命令使用一致的退出码
- [ ] `--help` 对所有子命令有效

**预估测试数**：12
**依赖**：任务 1-6、`internal/config`

---

### 任务 8：internal/config — 配置管理

**包名**：`internal/config`
**描述**：配置文件解析、验证和默认值。支持 TOML 格式。

**文件**：
- `config.go` — Config 结构体、Load/Save、默认值
- `validate.go` — 参数验证

**验收标准**：
- [ ] Load 解析有效的 TOML 配置
- [ ] Load 对缺失字段返回默认值
- [ ] Save 写入有效的 TOML
- [ ] Validate 拒绝无效的端口号、路径等

**预估测试数**：8
**依赖**：任务 1

---

## 汇总

| 阶段 | 任务 | 包名 | 预估测试数 | 依赖 |
|-------|------|---------|-----------|-------------|
| 1 | 1 | internal/chain | 18 | 无 |
| 1 | 2 | internal/mining | 22 | 任务 1 |
| 2 | 3 | internal/contract | 16 | 任务 1 |
| 2 | 4 | internal/proof | 20 | 任务 1、3 |
| 3 | 5 | internal/payment | 18 | 任务 1 |
| 3 | 6 | internal/overlay | 14 | 任务 1 |
| 4 | 7 | cmd/metanet | 12 | 任务 1-6、8 |
| 4 | 8 | internal/config | 8 | 任务 1 |
| | | **总计** | **128** | |

---

## 实现顺序

```
第 1 周：任务 1（chain）-> 任务 2（mining）
第 2 周：任务 3（contract）-> 任务 4（proof）
第 3 周：任务 5（payment）-> 任务 6（overlay）
第 4 周：任务 8（config）-> 任务 7（cmd/metanet）
```

所有包目标 >=80% 行覆盖率。测试使用表驱动模式，配合 testify/require + testify/assert。

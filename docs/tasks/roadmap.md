# Roadmap

## P0 — 品牌/命名

- [ ] **Metanet 品牌改名为 Penglai** — 去中心化 CDN 网络对外命名统一为 **Penglai**，官方域名统一为 **`penglai.network`**。包含文档、白皮书、站点文案、CLI/help 输出、仓库说明与对外材料的分阶段迁移与兼容策略。

## P1 — 协议/架构

- [ ] **BSV 入金引导** — 不自建兑换服务，在余额不足等场景提示用户通过第三方购买 BSV：
  - **ChangeNow** (https://changenow.io/currencies/bitcoin-sv) — USDT→BSV 等 150+ 交易对，小额无需注册，~2 分钟到账
  - **Changelly** (https://changelly.com/exchange/bsv) — 支持信用卡购买 BSV，700+ 币种
  - 工作量: 在 CLI/App/Extension 余额不足时显示购买链接提示
- [ ] **早期 PoW 安全性** — Metanet Chain 早期算力低，51% 攻击成本低。方案: 初期 PoA / 最低难度阈值 / BSV checkpoint 锚定
- [ ] **存储证明批量提交** — 1000 合约时 12,000 笔/天链上交易，需批量 Merkle root 或 rollup
- [ ] **Oracle 角色定位** — ECDH 分发 / 挑战管理两职责均可消除或合并，三种方案待深入分析
- [ ] **多输出批量交易·剩余工作** — 节点身份层已完成 (P_node, TxID, Vout)，剩余: ChildEntry 格式变更、SelfUpdate 紧凑 vout 引用、sCrypt MetanetBatch 合约、MutationBatch commit 单交易构建、engine 层适配。Metanet spec 阶段统一设计

## P2 — 生态工具 / 功能增强

- [ ] **WoC BitFS Plugin** — 为 WhatsOnChain 开发 BitFS/Metanet 协议解码插件（webhook 模式），让用户在 WoC 主站直接查看 BitFS 交易的 Metanet 节点信息、文件元数据、DAG 关系。核心是 OP_RETURN Data decoder plugin，复用 libbitfs 解码逻辑。发布通过 [woc-plugins-registry](https://github.com/teranode-group/woc-plugins-registry) PR。den-explorer 保持独立用于 regtest/testnet 调试
- [ ] **BIP32 目录购买** — 利用 BIP32 非硬化派生的 ECDH 传递性，一笔 HTLC 解锁整棵目录树。基础设施已就位（wallet/hd.go 支持 hardened/non-hardened 参数），需实现上层 BuyDirectory API 和购买流程整合。设计见 DetailedDesign 十五-B。
- [ ] **Token 预购系统 (Hash Chain)** — Hash chain 批量预购 token，减少频繁小额购买的链上交易次数。当前 HTLC per-file 已足够（BSV 手续费极低），但高频访问场景可能有价值。设计见 DetailedDesign 十一-B。
- [ ] **bsync/bput 同步工具** — 三阶段流水线（Scan→Diff→Apply）本地 ↔ BitFS 双向同步。mput/mget 覆盖单向批量操作，双向同步为进阶需求。设计见 ConceptDesign 决策 #41-44。
- [ ] **Session lock/unlock** — ssh-agent 模型的密码缓存。unlock 后避免每次操作输入密码。当前 BITFS_PASSWORD 环境变量已是过渡方案。
- [ ] **CLTV 时间锁 daemon 行为** — 基础 CheckCLTVAccess 已实现（metanet/cltv.go），需补充 daemon 行为：X-CLTV-Height headers、未到期 403 响应、三种模式（Embargo/Expiry/Subscription）。
- [ ] **ZSTD 压缩支持** — TLV 常量已预留（CompressZSTD=3），需补充 storage/compress.go 中的 ZSTD 编解码实现。实现成本低。

## P3 — 远期探索

- [ ] **写权限链上强制（下个版本）** — 通过 covenant / capability-UTXO 等机制把写权限从”应用层执法”升级到”链上硬约束”。`v0.0.1` 暂不纳入，后续版本单独评审交易体积、隐私与运维复杂度。
- [ ] **Covenant ISO 收益权证券化** — Registry UTXO + Share UTXO + ISO Pool，OP_PUSH_TX Covenant 强制分账。当前 libbitfs-go/revshare/ 仅有离线分配数学。链上 Covenant 是完整金融产品，需用户基数支撑。设计见 DetailedDesign 十四-B。
- [ ] **AMP (Agent Messaging Protocol)** — Agent 间点对点加密消息协议，独立于 BitFS 的协议规范（仅依赖 BSV + Paymail + ECDH）。双通道模型: 在线 HTTP 直连 (`/_amp/deliver`)，离线 BSV OP_RETURN (`0x616d70` 标识) 作为永久信箱。TLV 二进制格式，Method 42 E2E 加密。内置任务协作消息类型 (task-request/result/status/ack)。对标 Google A2A 协议但更精简、加密优先、支持离线投递和链上支付。设计文档: `docs/tasks/2026-03-04-amp-protocol-design.md`
- [ ] **收益分配/ISO/目录证明扩展字段（远期）** — `revenue_share`、`registry_txid`、`registry_vout`、`iso`、`merkle_root` 相关术语与功能暂不进入当前核心 TLV；后续在收益分配、代币化与不可信分发证明能力需要落地时，作为远期扩展专题单独设计与实现，避免影响当前核心文件系统元信息协议收敛。
- [ ] **Metanet 整合 libbitfs-go** — 当前 metanet/ 独立实现，不依赖 libbitfs-go。待 Metanet Phase 2 架构稳定后评估整合时机，避免 API 被两个产品需求拉扯。

## P4 — 长期规划

- [ ] **Group Signature / ACL (BLS12-381)** — 群签名实现 POSIX 风格多人权限、群加密实现多人读取。需引入 BLS12-381 密码学依赖。当前单用户模型够用，多人协作是下一阶段需求。设计见 DetailedDesign 二十二-B。

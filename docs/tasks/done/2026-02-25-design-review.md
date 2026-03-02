# 设计审查报告 — 2026-02-25

审查范围: BitFS 交易格式、Shell/b* 命令、Metanet Overlay Network

状态标记: `[FIXED]` 已修复 | `[TODO]` 待修改 | `[DECIDED]` 已决策待实施 | `[DEFERRED]` 暂缓 | `[INFO]` 仅记录

---

## 一、BitFS 交易格式

### 1.1 [FIXED] UTXO 并发写入描述错误

**问题**: 设计文档将"自持续 UTXO 链"的串行模式描述为协议级限制，误导读者认为多客户端并发写入同一目录**必然**冲突。实际上 Metanet Edge 只要求 Input 被 D_parent 签名，不限定具体 UTXO——父节点拥有多个 UTXO 时可完全并行。

**已修复文件**:
- `design/bitfs/2-SystemDesign.zh.md`: 第 220-221 行（"已知限制"→ 协议并行说明）、第 273 行（自持续链补充说明）、第 2011-2016 行（多设备章节重写，区分串行/预分裂模式）
- `design/bitfs/3-DetailedDesign.zh.md`: 第 568 行（"P_parent UTXO (Vout=1 或 Vout=2)"→ "任意 UTXO"）

### 1.2 [TODO] TLV Length 字段 uint16 溢出风险

**文件**: `libbitfs-go/metanet/parser.go`
**问题**: TLV 编码 Length 为 2 字节 uint16，最大 65535 字节。child_entry 约 45 字节/条，超过 ~1400 个直接子项时 child_entry 列表溢出。
**影响**: 大目录（>1400 子项）无法正确序列化。
**建议**: 改用变长编码（如 1/2/4 字节自适应），或为 child_entry 引入分片 TLV tag（0x0E 写满后续传到 0x0E'）。
**优先级**: P1

### 1.3 [DECIDED] mv 语义：逻辑删除+重建

**决策**: 方案 B — `mv` = 旧节点 SelfUpdate(op=DELETE) + 新位置 CreateChild(新 P_node)。
**关键影响**:
- 新 P_node → 新 HD 路径 → 新加密密钥 → 内容需重新加密
- **已购买用户的 capsule 失效**——需在 `mv` 命令中明确警告
- DAG 更干净，无幽灵 LINK 积累
- 可选：旧节点的 DELETE 元数据中记录 `moved_to: new_P_node`，便于客户端跟踪

**待修改文件**:
- `design/bitfs/2-SystemDesign.zh.md` 第 215 行（mv 操作说明）
- `design/bitfs/3-DetailedDesign.zh.md`（mv 详细流程）
- `bitfs/internal/engine/` 和 `bitfs/cmd/bitfs/cmd_shell.go`（mv 实现）
- `bitfs/docs/spec/10-cmd-bitfs.md`（mv spec）
**优先级**: P2

### 1.4 [TODO] 大目录区块链膨胀

**问题**: 每个子节点 = 0.5-1.5KB 交易。100 万文件 ≈ 1-1.5GB 纯元数据。
**现有缓解**: MutationBatch 批量操作。
**建议**: 考虑目录快照机制——定期将完整目录状态打包为单笔"快照交易"（只存 Merkle root + 子项列表），客户端从最近快照开始重放增量。类似 Git 的 packfile 思路。
**优先级**: P3 — 当前规模下不紧急

### 1.5 [TODO] DustLimit 硬编码 546 sat

**文件**: `libbitfs-go/tx/metanet_tx.go`
**问题**: BSV 已移除 dust limit，但代码中 NodeUTXO 和 ParentUTXO 仍使用 546 sat。每个节点多付 545 sat（可降至 1 sat）。
**影响**: 大规模使用时成本累积（10 万节点 × 545 sat × 2 UTXO = ~1 BSV 浪费）。
**建议**: 改为可配置常量，默认 1 sat。
**优先级**: P3

### 1.6 [INFO] Protobuf vs TLV 术语不一致

**问题**: 多处文档和代码注释提到 "Protobuf payload" / "protobuf_payload"，但实际编码格式为自定义 TLV。Protobuf 仅在早期设计中使用，已被替换。
**建议**: 全局搜索替换 "protobuf" → "TLV payload" 或统一术语。
**优先级**: P3

---

## 二、Shell 与 b* 命令

### 2.1 [TODO] Shell 与 b* 的访问路径不一致

**问题**:

| 组件 | 访问方式 | 需要 Wallet | 需要 Daemon |
|------|----------|------------|-------------|
| Shell | 直接调 engine | Yes | No |
| b* tools | HTTP → daemon | No | **Yes** |
| bitfs CLI | 直接调 engine | Yes | No |

Shell 和 CLI 直调 engine，b* 必须走 daemon HTTP。同一操作在不同组件中走完全不同代码路径。
**影响**: daemon 未启动时 b* 工具完全不可用；两条路径可能产生行为差异。
**建议**: 让 b* 工具支持双模式——优先连接 daemon，失败时回退到本地 engine（只读模式，不需要 wallet）。
**优先级**: P2

### 2.2 [FIXED] Shell 缺少关键命令

**问题**: 设计文档规划了 cat、rmdir、publish、decrypt、mget、mput 等命令，当前实现仅 16 个。关键缺口:
- `cat`: Shell 中无法查看文件内容，必须退出 shell 用 bcat——体验割裂
- `publish` / `unpublish`: Owner 核心功能，shell 不支持
- `mget` / `mput`: 批量操作，Agent 场景强需求

**已修复** (2026-02-26, feat/shell-commands):
- 新增 5 个 Engine 方法: Cat、Get、Mget、Mput、Unpublish（含完整测试）
- Shell REPL 新增 6 个命令: cat、get、mget、mput、publish、unpublish
- 新增 4 个独立 CLI 子命令: cmd_cat、cmd_get、cmd_mget、cmd_mput
- Tab 补全已覆盖所有新命令
- 计划文档: `bitfs/docs/plans/2026-02-26-shell-commands{-design,}.md`

### 2.3 [TODO] Agent Friendly 定位与实现差距

**问题**: 项目核心卖点之一是 "Agent Friendly"，但当前:
- b* 工具 `--json` 输出缺少统一的 JSON Schema 定义
- 无 SDK/Library 层——Agent 必须调 CLI 或发 HTTP
- x402 购买流程需多轮交互，对 Agent 不友好
- 无 MCP (Model Context Protocol) 适配
**建议**:
  1. 定义 b* JSON output schema（OpenAPI 或 JSON Schema）
  2. 推进 `libbitfs-ts`（已规划但未实现），浏览器/Node.js Agent 可直接调用
  3. x402 流程封装为单步 API（`bget --buy` 内部处理全部 HTLC 细节）
  4. 考虑提供 MCP server 封装 daemon API
**优先级**: P2 — 这是差异化卖点，应尽早落地

### 2.4 [FIXED] bitfs:// URI 对人类不友好

**问题**: `bitfs://02a1b2c3...66chars.../path` 要求写完整 66 字符公钥。Paymail 域名映射虽存在但文档不突出。
**建议**:
  - 文档和示例中优先使用域名形式: `bitfs://alice@example.com/docs/readme.txt`
  - b* 工具的帮助文本中先展示域名形式
  - 考虑支持短名: `bitfs://~alice/path`（本地别名 → pubkey 映射）
**优先级**: P3

**已修复** (2026-02-26):
- 5 个 b-tools 帮助文本新增域名/paymail/pubkey 三种 URI 示例，域名优先
- user-guide.md Section 5: 新增 URI 格式表（domain/paymail/pubkey），所有示例改用域名 URI
- user-guide.md Section 8: 新增 publish-to-access 完整示例

### 2.5 [FIXED] Tab 补全无缓存

**文件**: `bitfs/cmd/bitfs/completer.go`
**问题**: `completeRemotePath` 每次按键查询 DAG，大目录下可能延迟。
**建议**: 添加前缀缓存 + 5s TTL 失效。
**优先级**: P3

**已修复** (2026-02-26):
- shellCompleter 新增 cacheDir/cacheNode/cacheExpiry 字段，500ms TTL
- 同目录 + TTL 内复用缓存节点，跳过 FindNodeByPath() O(n) 查找
- 3 个新测试: CacheHit、CacheExpiry、CacheDifferentDir

---

## 三、Metanet Overlay Network

### 3.1 [DEFERRED] Oracle 角色定位

**分析**:
- Oracle 的两个职责：(A) 一次性 ECDH 加密分发，(B) 持续性挑战管理
- 职责 B 可完全自动化（确定性挑战 + 链上脚本验证）
- 职责 A 中 Publisher 可用自己的私钥替代 Oracle 私钥做 ECDH，反串通属性不变
- Oracle 本质是**可选的便利服务**，不是协议必须角色

**待定方案**:
  - A: 消除 Oracle，3 角色模型（Publisher / Miner / SP）
  - B: Oracle 作为协议定义的可选角色
  - C: Oracle 与 Miner 合并

**状态**: 暂缓决策，后续深入分析后确定

### 3.2 [DECIDED] MNT 冷启动：无需特殊机制

**决策**: 与比特币相同的有机增长模式——不需要预挖、基金池或特殊启动方案。
**理由**:
- 热数据 CDN 经济完全靠 BSV x402 运行，不依赖 MNT
- MNT 挖矿早期成本低（CPU mining），矿工可出于投机/信仰参与
- 存储合约（冷数据）是补充功能，网络成熟后自然发展
- OTC 市场会随矿工积累 MNT 后自然涌现
- 两个经济体（BSV x402 检索 / MNT 存储合约）独立运行，互不依赖

### 3.3 [DECIDED] 双币种结构：保留当前设计

**决策**: 保留 BSV + MNT 双币种。
**理由**:
- BSV 用于终端用户内容检索（x402），MNT 用于 CDN 运营激励（挖矿、存储合约）
- 普通用户只接触 BSV，MNT 仅面向 B2B（Publisher ↔ Node）
- MNT 铸币是 Metanet Chain 存在的核心理由
- 两个经济体独立运行，冷启动问题不存在（见 3.2）

### 3.4 [DECIDED] Halving 节奏：保持 210,000 blocks / 2 年

**决策**: 保持 210,000 blocks halving interval，5 分钟出块自然导致约 2 年 halving。
**理由**: 与比特币完全相同的 block 数参数，出块时间差异导致的年限差异是数学结果而非独立设计。

### 3.5 [TODO] 存储证明链上交易量

**问题**: 每个 SP × 每个 epoch 需一笔 ON 交易提交 Merkle 证明。
  - 10 合约 × 3 SP × 每日 4 次 = 120 笔/天（单 Publisher）
  - 1000 合约时 = 12,000 笔/天
  - BSV 能处理但产生费用负担

**建议**: 批量证明提交（一笔交易包含多个证明的 Merkle root），或 rollup。
**优先级**: P2

### 3.6 [TODO] 早期 PoW 安全性

**问题**: 独立 PoW（5 分钟出块），早期 MNT 价值低时算力可能极低。低算力 → 51% 攻击成本低 → 链不安全。
**建议**:
  - 考虑初期 PoA (Proof of Authority) 过渡，由核心团队运行出块节点
  - 或设置最低难度阈值，防止算力过低时出块过快
  - 或利用 BSV 锚定提供额外安全保障（每 N 个 ML Block 向 BSV 提交 checkpoint）
**优先级**: P2

### 3.7 [INFO] 商标风险

**问题**: "Metanet" 商标已被注册（USPTO #7300182，第42类）。
**建议**: 尽早进行商标审查，评估是否需要授权或改名。
**优先级**: P1 — 法律风险，越早处理越好

---

## 四、跨领域问题

### 4.1 [DECIDED] BSV 依赖：维持接口抽象，不主动扩展

**决策**: 当前 `libbitfs-go/network/BlockchainService` 接口已足够抽象。保持接口干净即可，不投入精力做多链适配。等真正需要迁移时再做。

### 4.2 [TODO] "数据可以不上链"缺少决策指引

**问题**: 链上存储 (DataTx) vs 链下存储 (Daemon LFCP) 的选择标准不清晰。用户何时应上链、何时不上链？
**建议**: 在设计文档中增加"存储决策矩阵":

| 维度 | 链上 (DataTx) | 链下 (Daemon) |
|------|--------------|---------------|
| 文件大小 | < 100KB | 不限 |
| 持久性需求 | 永久不可删 | 可删可改 |
| 访问频率 | 低频/归档 | 高频/热数据 |
| 成本 | 按字节付费（一次性） | 存储+带宽（持续） |
| 隐私 | 加密上链（永久） | 服务器控制 |

**优先级**: P3

---

## 五、优先级汇总

### P1 — 关键设计 (影响核心功能/可行性)
- [x] 1.2 TLV uint16 溢出 → 实现变长编码 (LEB128 varint)
- [ ] 3.7 Metanet 商标 → 法律审查

### P2 — 重要改进 (影响用户体验/竞争力)
- [x] 1.3 mv 逻辑删除+重建 → 更新设计文档 + 实现
- [x] 2.1 Shell/b* 访问路径统一 (b* tools 从 URI 解析远程 daemon 端点)
- [x] 2.2 Shell 补全 cat/publish 命令
- [ ] 2.3 Agent Friendly 落地 (JSON Schema + libbitfs-ts)
- [ ] 3.5 存储证明批量提交
- [ ] 3.6 早期 PoW 安全性过渡方案

### P3 — 优化项
- [ ] 1.4 大目录快照机制
- [x] 1.5 DustLimit 546→1 sat
- [x] 1.6 Protobuf/TLV 术语统一
- [x] 2.4 bitfs:// URI 域名优先
- [x] 2.5 Tab 补全缓存
- [ ] 4.2 存储决策矩阵文档

### 已决策
- [x] 3.2 MNT 冷启动 → 无需特殊机制，有机增长
- [x] 3.3 双币种 → 保留 BSV + MNT
- [x] 3.4 Halving → 210,000 blocks / 2 年
- [x] 4.1 BSV 依赖 → 维持接口抽象，不主动扩展

### 暂缓
- [ ] 3.1 Oracle 角色定位 → 后续深入分析

### 已完成
- [x] 1.1 UTXO 并发描述错误 (2-SystemDesign + 3-DetailedDesign)
- [x] 1.5 DustLimit 546→1 sat (libbitfs-go/tx 常量+测试，8 个测试修正)
- [x] 1.6 Protobuf/TLV 术语统一 (libbitfs-go/tx 代码、bitfs/spec、design 4 章、whitepaper、e2e 共 ~50 处替换)
- [x] 1.2 TLV uint16→varint (parser.go 序列化/反序列化改 LEB128，3 个测试修正，设计文档 546→1 sat 同步)
- [x] 2.2 Shell 补全 cat/publish 命令 (Cat/Get/Mget/Mput/Unpublish engine 方法 + shell/CLI 命令 + tab 补全，+1214 行)
- [x] 2.4 bitfs:// URI 域名优先 (b-tools 帮助文本 + user-guide 域名 URI 示例优先)
- [x] 2.5 Tab 补全缓存 (completer.go 500ms TTL 缓存 + 3 个新测试)

# 全项目深度审查报告（2026-06-10）

> 审查方式：6 个并行 subagent，分别覆盖 BitFS 设计合理性、Metanet 设计与经济模型、设计-实现一致性、密码学安全、产品战略、代码架构。本文为汇总。

## 总体结论

工程质量（测试文化、分层纪律、文档自省）属同类项目上乘，但审查发现 **4 个协议级硬伤、1 个跨端资金可用性 Critical bug、Metanet 证明/共识/经济三层全部未闭合**。问题集中在两大卖点（付费访问、Agent Friendly）的根基上，而非外围。

---

## 一、BitFS 协议级硬伤（🔴 改设计而非改代码）

### 1. 付费购买缺乏公平交换保证（fair exchange）
HTLC 只保证 "原像 ↔ 付款" 原子性，**不保证 capsule 能解出有效内容**。恶意卖家可提供 `capsule_hash = SHA256(fileTxID || 垃圾)`，买家付款、卖家领款、买家拿到垃圾且无退款路径。验证（key_hash 校验）发生在付款之后。文档只覆盖了"卖家崩溃"，没覆盖"卖家作恶"。
- **建议**：明确当前 HTLC 仅适用于低价值/信誉场景；把可验证交付（sCrypt EC 验证 / ZK）列为付费模式 GA 硬前置；写入威胁模型。

### 2. SPV 无法证明"当前版本"（新鲜度不可验证）
最新版本 = NodeUTXO 未被花费，而 SPV 做不到 unspent proof。恶意/过期 daemon 可隐藏版本 k+1 只给到版本 k，五步 SPV 验证闭环全部通过但拿到陈旧数据。文档的验证闭环缺"新鲜度"一项。
- **建议**：诚实声明版本新鲜度依赖对 daemon 的信任；或引入 UTXO 承诺/多源交叉验证；在 §七 补上边界声明。

### 3. 跨目录 mv 破坏 HD 确定性恢复不变量
mv 保持 P_node 不变 + 目标目录新 index ⇒ 节点实际派生路径与树中位置永久脱钩，且原始派生路径无处持久化 ⇒ **移动过的节点在纯助记词恢复下不可写/不可解密**。更糟：4-TestDesign T9.5.2 写的是另一套语义（新节点+软链接），与系统/详细设计直接矛盾。
- **建议**：二选一并全文统一——要么"新节点+软链接"保 HD 镜像，要么 payload 持久化原始派生路径。

### 4. 并发写目录会静默丢 ChildEntry
多设备并发 CreateChild 后都要 SelfUpdate 同一父 NodeUTXO（双花），输家的子节点成为链上孤儿。预分裂 UTXO 只解决 Edge 创建并行，没解决目录注册串行性。
- **建议**：明确并发语义（目录更新串行 + 乐观重试），孤儿回收纳入 pending_tx_group 恢复。

### 其他重要设计问题（🟠）
- **链下数据可用性无兜底**：买家付款后 daemon 宕机 = 钱货两失（与 #1 叠加）。
- **CLTV 限时/订阅对链下内容只是 daemon honor-system**，非密码学强制，文档语气需下调。
- **目录树购买卖点与默认硬化派生自相矛盾**：一笔 HTLC 解锁整树只在非硬化时成立，而非硬化有 capsule 连锁泄露风险（子泄露⇒父泄露）。
- **lock/unlock 在 daemon 离线时几乎无用**（session 文件只存 token 不存 key）。
- **群签名 BLS12-381/BBS+ 范围膨胀**，且"群加密=群签名对偶"的密码学表述不成立，建议拆独立 spec 降级远期。
- **PAID 模式 key_hash 明文上链**存在已知明文确认攻击（可猜内容无需付费即可确认）。
- 四层文档常量不一致：HTLC 超时 144 vs 72 块、capsule_hash 两种定义、Token 系统两套公式——需建"协议常量真值表"。

---

## 二、Metanet/Penglai：证明、共识、经济三层均未闭合（🔴）

1. **存储证明挑战完全可预测**：代码与测试用 `challenge_k = SHA256(contract_txid ‖ k)`，合约创建瞬间全部挑战确定；链上脚本只验预计算 hash 不验 Merkle。Provider 存 ~3MB proof blob 即可丢弃 1GB 数据领全款。测试 T1.2 在固化这个漏洞。（`metanet/internal/contract/challenge.go:30`、`deal.go:95`、`script.go:60`）
2. **"防串通双层加密"防不了串通**：外层密钥 Provider 自己能 ECDH 派生，串通节点可毫秒级按需重生成副本；且代码里 ECDH 实为 HMAC"模拟"（`proof/encrypt.go:37`）。
3. **共识三套矛盾叙事**：出块 5 vs 10 分钟（影响减半周期 2 年 vs 4 年）；文档声明放弃合并挖矿/锚定，代码却有完整 AuxPoW + AnchorTx；"BSV 嵌入提供最终性"论断不成立（BSV 矿工不验 ML 共识规则）。锚定权威未定义，任何人可伪造锚定链。
4. **BSV 侧支付通道依赖已禁用操作码**（OP_CSV/OP_CLTV，post-Genesis 被拒）——BitFS 这边 HTLC 重构已踩过的坑，Metanet 原样照搬 LN 设计。建议改 nLockTime 递减式单向通道（CDN 计费本来就是单向流）。
5. **MNT 经济闭环断裂**：无获取路径（只有挖矿发行，Publisher 第一天买不到 MNT）、无初始分配声明、需求侧死亡螺旋未分析、带宽加权 PoW 可自买自卖刷量、"普通用户只碰 BSV"对内容所有者不成立。"为什么不直接用 BSV"缺正面论证。
6. **SHA256 独立链 51% 成本趋零**（租用闲置算力即可），"矿机兼容"卖点恰是安全弱点。
7. **链上证明数据量级未测算**：1000 合约 ≈ 3GB/天进 BSV，经济性未算。
8. 816 行设计文档 vs 一条 PoW 链 + token + 存储证明 + 支付通道的野心，深度/野心比约为 BitFS 的 1/9；"Phase 1 完成"实为纯数据结构+序列化，无 UTXO 集/共识/P2P/持久化。
9. 法务：CSW Multilevel Blockchain 专利依赖未评估；改名 Penglai 已列 P0 但文档/代码/官网全未执行。

**建议路径**：热数据自组织 CDN（纯 BSV、无链无币）剥离为 v0.1 先上线；链+MNT+存储合约冻结代码，过 speckit 重写 spec，先回答上述 🔴 问题。

---

## 三、密码学安全审计

| 级别 | 发现 | 位置 |
|---|---|---|
| **Critical** | Go/TS Argon2id 参数不一致（Go t=3,m=64MB,p=4 符合 spec；TS t=10,m=256MB,p=1 违反 spec），wallet.enc 头部不存参数 ⇒ **跨端钱包"密码正确但解密失败"**，资金可用性事故 | `libbitfs-go/wallet/seed.go:36` vs `libbitfs-ts/src/wallet/seed.ts:33` |
| High | Go/TS HTLC funding 费用/找零算法分歧，同输入产出不同金额交易 | `payment/htlc_tx.go:192` vs `src/payment/htlc.ts:214` |
| Medium | sessionID 直接取 SessionKey 前 128 位（埋雷） | `daemon/handshake.go:133` |
| Medium | 买家 capsule 明文 JSON 落盘 | `daemon/payment.go:58` |
| Low | Go ECDH 缺无穷远点检查；ParseHTLCPreimage 只哈希前 64 字节；SPV Merkle 缺 CVE-2012-2459 式形状校验 | 各文件 |

已验证无问题：method42 KDF/AES-GCM 两侧一致、nonce 随机、BIP44 派生正确、HTLC 106 字节布局逐字节一致、daemon 无密钥泄漏到日志/响应、RNG 全部密码学安全。

**修复**：TS 改为 t=3/m=65536KiB/p=4 并提供旧钱包迁移路径；wallet.enc 头部写入 KDF 参数版本；TS funding 逻辑对齐 Go；跑 `/go-ts-parity` 字节级 diff。

---

## 四、设计-实现一致性（增量审计，基线 2026-03-26）

整体一致性：**中**。核心协议层（HTLC offsets、BIP44、Method 42 主公式、MutationBatch）已验证一致。Top 5 待修：

1. **Capsule 派生公式缺 nonce**（DetailedDesign:1940/1955）——代码实际是 `HKDF(ECDH.x, key_hash || invoice_nonce, ...)`，buy 响应缺 `capsule_nonce` 字段文档。第三方按文档实现必然解密失败。
2. **"internal/engine 统一业务逻辑层"描述失实**——真正统一层是 `libbitfs-go/vault`（28 处 import），`bitfs/internal/engine` 只剩 20 行别名壳。SystemDesign:1952 + 两份 CLAUDE.md 都要改。
3. **`replace => ../libbitfs-go` 已删除**（现为 require v0.0.2 + go.work），SystemDesign:1976 + 根 CLAUDE.md 过期。
4. **Shell 命令表虚胖 13 个幽灵命令**（SystemDesign:858、DetailedDesign:1529）。
5. **SystemDesign 全文零"未实现"标注**：Token 批量购买、BIP32 目录树购买、ISO/share 8 个 CLI、Lock/Unlock 整节均呈现为现状；HTLC "144 块" 残留（实际 72）。

其他：TLV 附录覆盖率仅 27/46；daemon 路由表缺 4 条；`BITFS_PASSWORD` 等环境变量未文档化；daemon 默认监听地址三处矛盾（CLI 默认 `:8080` 绑全接口，与"localhost"安全预期不符——先定设计再统一代码）；bget `--version` 语义两边正好相反。

**教训**：上次审计的残留全是"已完成批次"的同类漏网——修复后必须 grep 全文复验，应写入 lessons.md。

---

## 五、产品战略（YC partner 视角）

三句话：① 工程执行力 top 1%，但在用它回避最难的问题——第一个付钱的用户；② BSV 是生存赌注且无对冲，设计无链抽象层；③ 砍一半项目，Metanet 冻结在白皮书阶段，6 个月只回答"有没有 Agent 开发者愿意用 BitFS 存取数据并付费"。

要点：
- "数据可以不上链"不是差异化（IPFS/Filecoin 数据本来不在链上），真卖点是"元数据上链所有权 + 内容链下"；建议改口号。
- "Agent Friendly" 需求真实但支付轨道押错：Agent 生态在 x402/USDC、L402 上，没有 Agent 金库持有 BSV。漏斗第一步就漏光。建议评估支付适配层。
- 12 个仓库维护面过宽：建议归档 bitfs-app/bitfs-desktop，extension 与 explorer 二选一合并，git-remote-bitfs 留作最佳 demo，den-explorer 降级内部工具。
- roadmap 全是供给侧任务，没有需求侧任务。P0 应新增"首个收入闭环 demo"；WoC Plugin 从 P2 提到 P1（唯一获客任务）。
- ISO/RevShare 证券化 Howey test 四条全中，拿到法务意见前撤出公开材料。
- 改名 Penglai 须在一切对外动作之前；协议层术语"Metanet DAG"也要去 Metanet 化（白皮书术语权属跟一辈子）。

---

## 六、代码架构（B+，趋势向好）

- **bitfs/ B**：daemon ports-and-adapters 设计是亮点；daemon↔client HTTP 契约手写两遍无共享类型；shell 766 行 switch 与 CLI 平行实现（业务逻辑未分叉，仅表现层）；6 个 b-tools 脚手架复制，bget/bmget 基本重写版。
- **libbitfs-go/ B+**：依赖方向干净无循环；**engine 包是 vault 的退化重复且 syscall.Flock 无 build tag（Windows 不可编译）**，建议整包消灭；vault.Vault 导出面过大，并发契约靠注释执行；哨兵错误几乎缺席（全库仅 3 个）。
- **libbitfs-ts/ A-**：镜像度高，crosslang.test.ts 跨语言 vector 验证是真保障；唯一硬伤是 storage barrel 静态 import node:fs，浏览器可用靠 bundler 侥幸，需 conditional exports。
- **metanet/ A-**（早期标准）：地基健康非复制粘贴，错误处理纪律全工作区最好；警惕与 libbitfs-go 零共享导致第二套平行宇宙。
- **横切**：测试金字塔形状健康（约 12:3:1 + fuzz + cross-lang）；**daemon 可观测性空白**（处理真金白银却无结构化日志），v0.0.1 前应补最低限度。

重构投入产出比 Top 3：① 消灭 libbitfs-go/engine 包；② 清理 daemon 死 toml 配置 + 加 slog 请求日志；③ 抽 internal/api 共享契约包。

---

## 行动优先级建议

| 优先级 | 事项 |
|---|---|
| P0 | TS Argon2 参数修复 + 迁移路径（资金可用性 Critical） |
| P0 | Penglai 改名执行（一切对外动作前） |
| P0 | 协议三问定稿：公平交换边界、SPV 新鲜度边界、mv 语义统一 |
| P0 | 首个收入闭环 demo 进 roadmap |
| P1 | Metanet 代码冻结，热数据纯 BSV CDN 剥离为 v0.1；链+token 重过 speckit |
| P1 | 文档一致性 Top 5 回写（capsule nonce 公式最优先）+ 协议常量真值表 |
| P1 | TS funding 逻辑对齐 + /go-ts-parity；daemon 监听地址定案 |
| P2 | 消灭 engine 包、共享 API 契约、daemon 日志、子项目归档决策 |

> 各 subagent 完整报告未单独存档；本文件为权威汇总。复查时可按文件:行号引用直接验证。

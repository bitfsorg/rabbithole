# 设计文档一致性审查报告

**日期**: 2026-02-26
**范围**: 全部设计文档（L0-L4）、白皮书、网站、幻灯片、Spec、代码实现
**方法**: 5 个并行 Agent 分别审查不同层级，结果去重合并

---

## 统计摘要

| 严重度 | 数量 | 说明 |
|--------|------|------|
| CRITICAL | 9 | 协议级矛盾、安全影响、外部文档重大错误 |
| HIGH | 12 | 功能性缺口、架构描述分歧 |
| MEDIUM | 11 | 命名不一致、过时信息、缺失测试覆盖 |
| LOW | 8 | 文档维护、交叉引用缺失 |

---

## CRITICAL (9)

### C1. HKDF info 字符串不一致

`2-SystemDesign.zh.md:466` 使用 `"bitfs-method42"`，而 `3-DetailedDesign.zh.md`（10+ 处）、所有 Spec、代码 (`libbitfs-go/method42/kdf.go:21`) 统一使用 `"bitfs-file-encryption"`。

**影响**: HKDF info 是域分隔符。按 SystemDesign 实现会产生不兼容的密钥。
**修复**: 更新 `2-SystemDesign.zh.md:466` → `"bitfs-file-encryption"`

### C2. TLV Tag 编号偏移 (field 5 起全部错位)

设计文档 `2-SystemDesign.zh.md:311-394` 使用 Protobuf 风格编号，field 5 为 `reserved (encrypted_hash)`。代码 `libbitfs-go/metanet/parser.go:12-48` 跳过了 reserved field，从 field 5 起所有 tag 比设计少 1:

| 设计 Field # | 设计名称 | 代码 Tag |
|---|---|---|
| 5 | reserved | *不存在* |
| 6 | file_size | 0x05 |
| 7 | key_hash | 0x06 |
| ... | ... | ... |

**影响**: 第三方按设计文档实现的 parser 将产生不兼容的 TLV。
**修复**: 更新设计文档删除 reserved field 5，重新编号以匹配代码。

### C3. PRIVATE 模式 TLV 字段未序列化

设计文档定义了 `private_key_hash (field 24)` 和 `private_file_index (field 26)` 用于钱包恢复。代码 `libbitfs-go/metanet/node.go:130-132` 有对应的 struct 字段，但 `parser.go` 中无对应 TLV tag，无法序列化/反序列化。

**影响**: PRIVATE 模式的钱包恢复机制无法工作。
**修复**: 在 parser.go 中添加 tag 24/26 的序列化支持。

### C4. Dust Limit: 白皮书 546 vs 代码 1

`BitFS-Whitepaper-Outline.md:143-144` 写 `546 sat`。代码 `libbitfs-go/tx/opreturn.go:23` 和 `3-DetailedDesign.zh.md:1004` 都是 `DustLimit = 1`（BSV 已移除 dust limit）。`git-remote-bitfs/CLAUDE.md:64` 也过时写 546。

**影响**: 白皮书发布错误的协议参数。
**修复**: 更新白皮书和 git-remote-bitfs CLAUDE.md。

### C5. 网站：Staking 必需 vs 设计/白皮书：无需 Staking

Metanet 网站大纲 (`Website-Content-Outline.md`) 多处要求质押 MNT（line 140/160/180/201），甚至展示 `metanet node start --stake 1000`。但白皮书 (`Metanet-Whitepaper-Outline.md:315-317`) 明确写 "无最低质押、无罚没机制"，设计文档也强调低准入门槛。

**影响**: 外部文档呈现矛盾的产品设计。
**修复**: 统一网站与白皮书/设计的 Staking 立场。

### C6. 网站：检索费 MNT vs 设计：BSV

Metanet 网站 `Website-Content-Outline.md:181` 写 "Micro-fees paid in MNT"。设计 (`0-OverallDesign.zh.md:115`、`metanet/2-SystemDesign.zh.md:87-88`) 和白皮书都明确 x402 检索费用 BSV。

**影响**: 违反双币模型核心设计（用户付 BSV，MNT 仅用于运营商间）。
**修复**: 网站检索费改为 BSV。

### C7. Metanet 网站三层架构与所有其他文档不同

所有设计文档/白皮书: Layer 1=BSV, Layer 2=Daemon, Layer 3=Metanet Chain。
Metanet 网站: Layer 1=BSV, Layer 2=Metanet Chain, Layer 3=CDN Layer（**省略了 Daemon 层**）。

**影响**: 架构描述根本性矛盾。
**修复**: 统一网站的三层架构描述。

### C8. HTLC 发起方：网站说 Seller，白皮书/设计说 Buyer

BitFS 网站 `Website-Content-Outline.md:151` 写 "Seller publishes HTLC with file hash"。
白皮书 `BitFS-Whitepaper-Outline.md:158-168` 明确 "买方创建 HTLC 交易"。

**影响**: 协议流程根本性错误。
**修复**: 网站更正为 Buyer 发起 HTLC。

### C9. wallet.db vs wallet.enc

白皮书 `BitFS-Whitepaper-Outline.md:191` 和 `3-DetailedDesign.zh.md:1606` (孤立笔误) 使用 `wallet.db`。其余所有文档和代码使用 `wallet.enc`。

**影响**: 文件名和功能描述错误（白皮书还错误地说包含 UTXO 集合）。
**修复**: 统一为 `wallet.enc`。

---

## HIGH (12)

### H1. revenue_share 字段类型冲突

`bitfs/2-SystemDesign.zh.md:393` 定义为 `uint32 revenue_share`（basis point 百分比）。
`metanet/2-SystemDesign.zh.md:193-200` 定义为结构化对象 `{node_percent, owner_percent, min_price_per_kb}`。

**影响**: TLV 无法同时满足两种定义。

### H2. libbitfs 包列表过时

`0-OverallDesign.zh.md:60-69` 列 8 个包（含 `revshare`，缺 `wallet`、`config`、`network`）。
实际 `libbitfs-go/` 有 11 个包。`1-ConceptDesign.zh.md:31` 还提到 `Rabin` 包但不存在。

### H3. rm 交易数量矛盾

`2-SystemDesign.zh.md:213` 说 2 笔交易（SelfUpdate + 花费目标 UTXO）。
`3-DetailedDesign.zh.md:741-757` 说 1 笔交易并解释原因（硬链接）。L3 正确。

### H4. mv 跨目录操作：L3 内部自相矛盾

`3-DetailedDesign.zh.md` Section 4-B (line 808) 使用 `op=DELETE`。
同文档 Section 9-B (line 1519) 使用 `LINK SOFT`。L4 测试 T9.5.2 与 Section 9-B 一致。

### H5. 14+ 设计中定义的 TLV 字段未在代码中实现

包括: metadata(20), version_log(21), share_list(22), chunk_index(30), total_chunks(31), recombination_hash(32), rabin_signature(33), rabin_pubkey(34), registry_txid(36), registry_vout(37), iso(38), acl_ref(40) 等。

### H6. Daemon 缺失设计中的端点

- `POST /_bitfs/pay/{invoice_id}` (x402 CDN 带宽费)
- `POST /_bitfs/git/push` 和 `GET /_bitfs/git/refs/{path}`
- Paymail `a9f510c16bde` (Verify Public Key) capability

### H7. Exit Code 定义三方不一致

| Code | Spec | 代码/用户指南 |
|------|------|-------------|
| 3 | Network error | Wallet error |
| 4 | Data validation | Network error |
| 5 | Auth error | Permission |
| 7 | Payment error | Conflict |

### H8. Spec 中 11+ CLI 命令未实现

未实现: `init`, `rmdir`, `decrypt`, `sales`, `wallet restore`, `wallet info`(实际叫`show`), `vault info`, `vault use`, `daemon status`, `daemon config`。
实现但不在 Spec 中: `cat`, `get`, `mget`, `mput`, `verify`, `wallet balance`。

### H9. DNS TXT 记录格式分歧

`libbitfs-go/paymail/dns.go:95` 和 Spec: `_bitfs_pubkey.{domain}` (raw pubkey)。
`bitfs/internal/engine/publish.go:64` 和用户指南: `_bitfs.{domain}` (`bitfs=<pubkey>` 格式)。

**影响**: 运行时不兼容。

### H10. 网站 BIP32 路径简化错误

网站用 `m/0'`, `m/1'`, `m/2'`，实际是 `m/44'/236'/0'`, `m/44'/236'/1'`...
还错误标注 `m/0' = identity`（设计中无此概念）。

### H11. FREE 模式 KDF 输入：P_node vs P_node.x

白皮书 `BitFS-Whitepaper-Outline.md:114` 写 `KDF(P_node, key_hash)`。
设计文档正确写 `KDF(P_node.x, key_hash)`（只取 x 坐标，32 bytes）。

### H12. 白皮书省略 HKDF info 参数

白皮书 `BitFS-Whitepaper-Outline.md:99` 写 `HKDF-SHA256(point.x, key_hash)`，缺少 `info="bitfs-file-encryption"` 参数。按此实现会派生出不同的密钥。

---

## MEDIUM (11)

### M1. 项目结构过时

`2-SystemDesign.zh.md:1916-1943` 列出 pre-extraction 的 `internal/` 结构（method42/wallet/tx/metanet/spv 等），实际都已移至 `libbitfs-go/`。当前 `bitfs/internal/` 只有 `buyer`, `client`, `daemon`, `engine`。

### M2. 存储目录名：三个不同名称

`store/` (2-SystemDesign.zh.md:75, spec/06-storage.md)
`data/` (2-SystemDesign.zh.md:1891, whitepaper)
`storage/` (3-DetailedDesign.zh.md:495, 代码, CLAUDE.md) ← **正确**

### M3. 配置文件格式：三种不同说法

`config.toml` (2-SystemDesign.zh.md:64) / `config.yaml` (4-TestDesign.zh.md:513, 3-DetailedDesign.zh.md:3025) / `key=value` (代码, CLAUDE.md) ← **代码实际用 key=value，文件名 `config`**

### M4. Daemon 默认端口：80 vs 8080

Spec `10-cmd-bitfs.md:107` 和 ConceptDesign 说 80/443。代码默认 `:8080`，用户指南也是 8080。

### M5. DustLimit 546 在 TASKS.md + 1 个失败测试

`spec/TASKS.md:57` 仍写 546。`integration/tx_build_extra_test.go:547` 断言 `DustLimit==546` 会失败。

### M6. 用户指南缺失 7 个 Shell 命令

实现了 22 个 shell 命令，用户指南只列 12 个。缺失: cat, get, mget, mput, cp, publish, unpublish。

### M7. Go 版本：设计说 1.21+，实际 1.25.6

`2-SystemDesign.zh.md:1906` 写 "Go 1.21+"，实际需要 1.25.6。

### M8. Metanet 挑战 k 起始值：L3=0, L4=1

`metanet/3-DetailedDesign.zh.md:43` 用 `k := 0`。`metanet/4-TestDesign.zh.md:50` 用 `k=1..N`。会产生不同的 challenge 值。

### M9. 多项 L3 详细设计无对应 L4 测试

缺失测试覆盖: Koblitz 加密、内容压缩(4种方案)、CLTV 时锁访问、Hash Chain Token 批量购买、目录级 BIP32 xpub 解锁、BSV Anchor 交易。

### M10. Slides 字体/颜色偏离 VI 系统

Slides 用 IBM Plex Sans + `#d4a574`，VI 系统定义 Inter + `#c9956b`。

### M11. 代码中 SPV Proof 端点未在设计中记录

`daemon/routes.go:35` 实现了 `GET /_bitfs/spv/proof/{txid}`，但无任何设计文档提及。

---

## LOW (8)

### L1. NodeType 枚举缺 ANCHOR 类型

设计说 `FILE=0, DIR=1, LINK=2`。代码多了 `ANCHOR=3`（git-remote-bitfs 用）。

### L2. Anchor TLV Tags (0x20-0x26) 未写入设计文档

代码 `parser.go:42-48` 有 7 个 anchor 专用 tag，设计文档无记载。

### L3. CLI 命令列表：L0 混淆 bitfs 和 b-tools

`0-OverallDesign.zh.md:76` 将 `get/ls/cat` 列为 bitfs 子命令，实际是 b* tools 和 shell 命令。

### L4. bitfs put 默认 FREE vs bput 继承父目录

`put` 默认公开免费，`bput` 继承父目录设置。两种接口相同操作不同语义。

### L5. Paymail profile URL 路径不匹配

设计: `/api/v1/profile/{alias}@{domain.tld}`。代码: `/api/v1/public-profile/...`。且无对应 handler。

### L6. Section 编号跳过 19

`2-SystemDesign.zh.md` 从第 18 节跳到第 20 节。

### L7. Spec 包路径用 `libbitfs/` 而非 `libbitfs-go/`

TASKS.md 等 spec 文件引用 `libbitfs/method42/`，实际目录名 `libbitfs-go/`。

### L8. 交叉引用缺失

Metanet L1 多次引用 x402 但未链接到 BitFS 设计文档。`revshare/` 在 L0 列出但无描述。

---

## 修复优先级建议

### P0 — 立即修复（协议级/外部文档错误）— ✅ 全部完成 (2026-02-26)

1. ✅ **C1** HKDF info string → 改 SystemDesign "bitfs-method42" → "bitfs-file-encryption"
2. ✅ **C2** TLV tag 编号 → 删 reserved field 5, 重编号匹配 parser.go (1-18 已实现, 19-27 已实现, 28+ 预留)
3. ✅ **C4** Dust limit → 白皮书 546→1 sat, git-remote-bitfs/CLAUDE.md 同步
4. ✅ **C5** Staking 立场 → 网站删除质押要求, 改为"无最低质押"匹配白皮书
5. ✅ **C6** 检索费货币 → 网站 MNT→BSV (x402 协议)
6. ✅ **C7** 三层架构 → 网站改为 BSV/Daemon/Metanet Chain 三层, 匹配设计文档
7. ✅ **C8** HTLC 发起方 → 网站改为 Buyer 创建 HTLC, 3 步流程
8. ✅ **C9** wallet.db → 白皮书 + 3-DetailedDesign 改为 wallet.enc

### P1 — 近期修复（功能/架构一致性）

9. **H1** revenue_share 类型 → 确定一种定义
10. **H3** rm 交易数 → 改 SystemDesign 为 1 笔
11. **H4** mv 跨目录 → L3 内部统一为 LINK SOFT
12. **H7** Exit codes → 改 Spec 匹配代码
13. **H9** DNS 记录格式 → 统一 _bitfs vs _bitfs_pubkey
14. **H10** BIP32 路径 → 改网站为完整 BIP44
15. **H11/H12** 白皮书 KDF → 补 `.x` 和 info 参数
16. **M5** 修复失败测试 `tx_build_extra_test.go:547`

### P2 — 版本更新时修复（文档更新）

17. **H2** 更新 libbitfs 包列表
18. **M1** 更新项目结构
19. **M2/M3** 统一 storage/config 命名
20. **H8** 补齐 Spec 中缺失的 CLI 命令
21. **M6** 更新用户指南 shell 命令列表
22. **C3** 实现 PRIVATE TLV 序列化（需代码变更）

### P3 — 低优先级

23. L1-L8 和其余 MEDIUM 项

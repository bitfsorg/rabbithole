# 设计文档一致性审查报告

**日期**: 2026-02-26
**范围**: 全部设计文档（L0-L4）、白皮书、网站大纲、幻灯片、Spec 规格说明、代码实现
**标准**: 以代码实现和详细设计（L3）为权威基准，对照所有其他文档
**审查员**: Claude Opus 4.6 (5 个并行审查 Agent)
**方法**: Agent 1-2 审查设计文档层级间一致性，Agent 3 审查设计与代码，Agent 4 审查 Spec 与设计，Agent 5 审查外部文档

---

## Executive Summary

设计文档体系的**核心密码学**保持高度一致：Method 42 ECDH、AES-256-GCM、BIP44 路径 `m/44'/236'/...`、MetaFlag `0x6d657461`、Argon2id 参数 `m=64MB,t=3,p=4`、key_hash 双哈希 `SHA256(SHA256(plaintext))` 在所有文档中完全统一。

但在协议细节、外部文档、和历史演进方面发现 **40 个不一致项**：

| 严重度 | 数量 | 说明 |
|--------|------|------|
| **CRITICAL** | 9 | 协议级矛盾、安全影响、外部文档重大错误 |
| **HIGH** | 12 | 功能性缺口、架构描述分歧 |
| **MEDIUM** | 11 | 命名不一致、过时信息、缺失测试覆盖 |
| **LOW** | 8 | 文档维护、交叉引用缺失 |

主要矛盾集中在三个区域：
1. **SystemDesign (L2) 过时** — 多处未跟进 libbitfs 抽取重构和 DustLimit 变更
2. **外部文档 (网站/白皮书) 偏离设计** — Staking 立场、检索费货币、HTLC 流程、三层架构
3. **TLV 协议格式** — tag 编号偏移、PRIVATE 模式字段未实现

---

## CRITICAL — 协议级矛盾 (9)

### C1. HKDF info 字符串不一致

Method 42 密钥派生的 HKDF domain separator 在文档间不一致。

| 文档 | 位置 | info 值 |
|------|------|---------|
| 2-SystemDesign.zh.md | line 466 | `"bitfs-method42"` |
| 3-DetailedDesign.zh.md | line 223 等 10+ 处 | `"bitfs-file-encryption"` |
| spec/01-method42.md | line 7 | `"bitfs-file-encryption"` |
| libbitfs-go/method42/kdf.go | line 21 | `"bitfs-file-encryption"` |

**影响**: HKDF info 是域分隔符，不同值会产生不兼容的 AES 密钥，导致加密内容无法互通。
**权威来源**: 代码和 L3 详细设计一致使用 `"bitfs-file-encryption"`。
**修复**: 更新 `design/bitfs/2-SystemDesign.zh.md:466` 的 info 值。

---

### C2. TLV Tag 编号全面偏移 (field 5 起)

设计文档使用 Protobuf 风格的 field 编号，其中 field 5 为 `reserved (encrypted_hash)`。代码跳过了此保留字段，导致从 field 5 起所有 tag 编号比设计少 1。

**设计文档** (`2-SystemDesign.zh.md:311-394`):
```protobuf
uint32 version = 1;
Type type = 2;
Op op = 3;
string mime_type = 4;
bytes encrypted_hash = 5;  // reserved
uint64 file_size = 6;
bytes key_hash = 7;
Access access = 8;
uint64 price_per_kb = 9;
// ... 后续字段依此编号
```

**代码** (`libbitfs-go/metanet/parser.go:12-48`):
```go
tagVersion    = 0x01  // OK
tagType       = 0x02  // OK
tagOp         = 0x03  // OK
tagMimeType   = 0x04  // OK
tagFileSize   = 0x05  // 设计 = 6, 代码 = 5 ← 偏移开始
tagKeyHash    = 0x06  // 设计 = 7
tagAccess     = 0x07  // 设计 = 8
tagPricePerKB = 0x08  // 设计 = 9
// ... 所有后续 tag 均偏移 -1
```

**影响**: 第三方按设计文档实现的 TLV parser 将产生不兼容的二进制格式。
**修复**: 更新设计文档，删除 reserved field 5 并重新编号，以匹配代码实际使用的 tag 值。

---

### C3. PRIVATE 模式 TLV 字段未序列化

设计文档定义了两个用于 PRIVATE 模式钱包恢复的明文 TLV 字段：

| Field # | 名称 | 用途 |
|---------|------|------|
| 24 | `private_key_hash` | key_hash 明文副本，用于恢复 |
| 26 | `private_file_index` | file_index 明文副本 |

代码中 `libbitfs-go/metanet/node.go:130-132` 有对应的 struct 字段（`PrivateKeyHash`、`PrivateFileIdx`），但 `parser.go` 中**无对应 TLV tag 常量**，`SerializePayload` 和 `deserializePayload` 均不处理这两个字段。

**影响**: PRIVATE 模式文件的钱包恢复机制无法工作——即使数据写入链上，恢复时也无法从 TLV 中提取恢复所需的明文信息。
**修复**: 在 `parser.go` 中添加 tag 24/26 的序列化/反序列化支持。

---

### C4. Dust Limit: 白皮书 546 sat vs 代码 1 sat

| 文档 | 位置 | DustLimit |
|------|------|-----------|
| BitFS-Whitepaper-Outline.md | line 143-144 | `546 sat` |
| git-remote-bitfs/CLAUDE.md | line 64 | `546 satoshis` |
| spec/TASKS.md | line 57 | `546 聪` |
| 3-DetailedDesign.zh.md | line 1004 | `1` |
| libbitfs-go/tx/opreturn.go | line 23 | `DustLimit = uint64(1)` |

BSV 已移除 dust limit，项目代码和详细设计已更新为 1 sat，但白皮书、git-remote-bitfs 文档和 TASKS.md 仍写 546。

**影响**: 白皮书发布错误的协议参数。`integration/tx_build_extra_test.go:547` 的 `assert.Equal(t, uint64(546), tx.DustLimit)` 断言会失败。
**修复**: 更新白皮书、git-remote-bitfs/CLAUDE.md、spec/TASKS.md；修复失败的集成测试。

---

### C5. Metanet 网站：Staking 必需 vs 设计/白皮书：无需 Staking

**网站大纲** (`websites/metanet.org/Website-Content-Outline.md`):
- line 140: `"Handles node economics and staking"` / `"处理节点经济与质押"`
- line 160: `"Stake MNT tokens, run the daemon, start earning."`
- line 180: `"Node operators stake MNT to join the network. Stake size signals commitment..."`
- line 201: `$ metanet node start --stake 1000`

**白皮书** (`whitepaper/Metanet-Whitepaper-Outline.md:315-317`):
```
**低准入门槛**：
- 无最低质押、无罚没机制、无复杂设置
```

**设计文档** (`design/metanet/1-ConceptDesign.zh.md:97`):
```
5. **低准入门槛** -- 普通服务器即可运行 Metanet Node, 无需专用硬件
```

**影响**: 外部面向公众的文档与内部设计呈现完全相反的产品定位。
**修复**: 需要做产品决策——Staking 是否纳入设计。然后统一所有文档。

---

### C6. 检索费货币：网站说 MNT，设计/白皮书说 BSV

**网站** (`websites/metanet.org/Website-Content-Outline.md:181`):
```
Micro-fees paid in MNT for each content retrieval.
```

**设计** (`design/0-OverallDesign.zh.md:115`, `metanet/2-SystemDesign.zh.md:87-88`):
```
| BSV | x402 检索费、HTLC 文件购买、Metanet DAG 交易手续费 | 所有用户 |
```

**白皮书** (`whitepaper/Metanet-Whitepaper-Outline.md:142`):
```
1. x402 检索费（BSV）— 用户下载内容
```

**影响**: 违反双币模型核心设计原则。双币模型的设计意图是用户使用 BSV 支付（降低门槛），MNT 仅用于运营商间的激励/治理。
**修复**: 网站检索费描述改为 BSV。

---

### C7. Metanet 网站三层架构与所有其他文档不同

**所有设计文档 + 白皮书**:
```
Layer 1: BSV Main Chain
Layer 2: Self-hosted Daemon
Layer 3: Metanet Chain
```

**Metanet 网站** (`Website-Content-Outline.md:139-141`):
```
Layer 1: BSV Layer
Layer 2: Metanet Chain      ← 其他文档中是 Layer 3
Layer 3: CDN Layer           ← 其他文档中不存在此层
```

网站**省略了 Daemon 层**（L2），并将 Metanet Chain 从 L3 提到 L2，然后新增了一个 "CDN Layer"。

**影响**: 架构描述根本性矛盾。开发者和投资者看到两套不同的架构。
**修复**: 统一网站的三层架构描述，恢复 Daemon 层。

---

### C8. HTLC 发起方：网站说 Seller，白皮书/设计说 Buyer

**网站** (`websites/bitfs.org/Website-Content-Outline.md:151`):
```
| 1 | Seller publishes HTLC with file hash |
```

**白皮书** (`whitepaper/BitFS-Whitepaper-Outline.md:158-168`):
```
6. 买方创建 HTLC 交易（SHA256 原像验证 + 超时退款）
7. 买方广播 HTLC
```

**设计文档** (`design/bitfs/3-DetailedDesign.zh.md`): Buyer 创建并广播 HTLC。

**影响**: HTLC 协议流程的发起方是核心设计决策——谁承担链上手续费、谁先锁定资金。网站描述的是相反的协议。
**修复**: 网站更正为 Buyer 发起 HTLC。

---

### C9. 钱包文件名：wallet.db vs wallet.enc

| 文档 | 位置 | 文件名 |
|------|------|--------|
| BitFS-Whitepaper-Outline.md | line 191 | `wallet.db` |
| 3-DetailedDesign.zh.md | line 1606 (孤立笔误) | `wallet.db` |
| 2-SystemDesign.zh.md | line 63 | `wallet.enc` |
| 3-DetailedDesign.zh.md | line 333 | `wallet.enc` |
| spec/04-wallet.md | line 214 | `wallet.enc` |
| 代码 (libbitfs-go/wallet/) | — | `wallet.enc` |

白皮书还错误地描述 `wallet.db` 包含 "HD 密钥 + UTXO 集合"，而实际 `wallet.enc` 只包含 Argon2id 加密的 HD seed。

**修复**: 白皮书改为 `wallet.enc`，修正功能描述。`3-DetailedDesign.zh.md:1606` 修正笔误。

---

## HIGH — 功能性缺口与架构分歧 (12)

### H1. revenue_share 字段类型冲突

两个 L2 设计文档对同一字段给出不同类型定义：

**BitFS** (`2-SystemDesign.zh.md:393`):
```protobuf
uint32 revenue_share = 41;  // 收益分成比例 (0-10000, 表示 0.00%-100.00%)
```

**Metanet** (`metanet/2-SystemDesign.zh.md:193-200`):
```json
revenue_share: {
    node_percent: 70,
    owner_percent: 30,
    min_price_per_kb: 1
}
```

**影响**: `uint32` 无法表示结构化对象。TLV 格式只能选择一种编码方式。
**修复**: 确定一种定义——建议 BitFS 的 `uint32` 用于 TLV 编码，Metanet 的结构化定义用于上层业务逻辑。

---

### H2. libbitfs 包列表过时

| 文档 | 包数量 | 内容 |
|------|--------|------|
| 0-OverallDesign.zh.md:60-69 | 8 | method42, metanet, spv, storage, paymail, tx, x402, **revshare** |
| CLAUDE.md (项目根) | 10 | 上述 + wallet, config, network (无 revshare) |
| 实际 libbitfs-go/ | 11 | 10 + revshare |
| 1-ConceptDesign.zh.md:31 | — | 额外提到 **Rabin** 包 (不存在) |

**修复**: 更新 `0-OverallDesign.zh.md` 列出全部 11 个包。移除 L1 中对 Rabin 包的引用，或将其标记为 "计划中"。

---

### H3. rm 交易数量矛盾 (L2 vs L3)

| 文档 | 位置 | 交易数 |
|------|------|--------|
| 2-SystemDesign.zh.md | line 213, 834 | 2 笔 (SelfUpdate + 花费目标 UTXO) |
| 3-DetailedDesign.zh.md | line 741-757 | 1 笔 (仅 SelfUpdate) |
| 4-TestDesign.zh.md | T9.3.1 | 1 笔 (与 L3 一致) |

L3 解释了为何只需 1 笔：硬链接可能从其他目录引用同一 `P_node`，不能安全花费。

**修复**: 更新 `2-SystemDesign.zh.md:213,834` 为 1 笔交易。

---

### H4. mv 跨目录操作：L3 内部自相矛盾

同一文档 `3-DetailedDesign.zh.md` 中两处描述不同：

| Section | 位置 | 源节点处理 |
|---------|------|-----------|
| 4-B | line 808-841 | `op=DELETE` + `moved_to` 指针 |
| 9-B | line 1519-1526 | `LINK SOFT` 重定向 |

L4 测试 T9.5.2 与 Section 9-B 一致（SOFT 链接方案）。

**修复**: 更新 Section 4-B 的描述以匹配 Section 9-B 和 L4 测试。

---

### H5. 14+ 设计中定义的 TLV 字段未在代码中实现

以下 `2-SystemDesign.zh.md` 定义的 TLV 字段在 `libbitfs-go/metanet/parser.go` 中无对应实现：

| 字段组 | 涉及 Field # |
|--------|-------------|
| 文件元数据 | metadata(20), version_log(21), share_list(22) |
| PRIVATE 模式 | private_key_hash(24), private_file_index(26) — 见 C3 |
| 内容分片 | chunk_index(30), total_chunks(31), recombination_hash(32) |
| Rabin 签名 | rabin_signature(33), rabin_pubkey(34) |
| 注册表 | registry_txid(36), registry_vout(37) |
| ISO/RevShare | iso(38) |
| 访问控制 | acl_ref(40) |

**说明**: 部分为尚未实现的功能（Rabin、分片、ISO），属于路线图中的计划项。但 PRIVATE 模式字段 (C3) 是当前需要的。

---

### H6. Daemon 缺失设计中定义的端点

| 设计中的端点 | 用途 | 状态 |
|-------------|------|------|
| `POST /_bitfs/pay/{invoice_id}` | x402 CDN 带宽费 (简单 P2PKH) | 未实现 |
| `POST /_bitfs/git/push` | Git push 通过 daemon | 未实现 (git-remote-bitfs 独立) |
| `GET /_bitfs/git/refs/{path}` | Git refs 查询 | 未实现 |
| Paymail `a9f510c16bde` | Verify Public Key capability | 未实现 |

反向: 代码实现了 `GET /_bitfs/spv/proof/{txid}` (M11)，但设计文档中未记载。

---

### H7. Exit Code 定义三方不一致

| Code | Spec (10-cmd-bitfs.md) | 代码 (cmd/bitfs/main.go) | 用户指南 |
|------|----------------------|-------------------------|----------|
| 3 | Network error | Wallet error | Wallet error |
| 4 | Data validation error | Network error | Network error |
| 5 | Auth error | Permission | Permission/payment |
| 7 | Payment error | Conflict | Conflict |

代码和用户指南一致，Spec 过时。

**修复**: 更新 `spec/10-cmd-bitfs.md` exit code 定义以匹配代码。

---

### H8. Spec 中 11+ CLI 命令未实现

**Spec 中定义但未实现的命令**:

| 命令 | Spec 位置 | 状态 |
|------|----------|------|
| `bitfs init` | 10-cmd-bitfs.md:14 | 用 `bitfs wallet init` 替代 |
| `bitfs rmdir` | line 20 | 未实现 |
| `bitfs decrypt` | line 30 | 未实现 |
| `bitfs sales` | line 33 | 未实现 |
| `bitfs wallet restore` | line 44 | 未实现 |
| `bitfs wallet info` | line 45 | 实现为 `wallet show` |
| `bitfs vault info` | line 40 | 未实现 |
| `bitfs vault use` | line 39 | 未实现 |
| `bitfs daemon status` | line 49 | 未实现 |
| `bitfs daemon config` | line 50 | 未实现 |

**实现了但不在 Spec 中的命令**: `cat`, `get`, `mget`, `mput`, `verify`, `wallet balance`

---

### H9. DNS TXT 记录格式分歧

两套不兼容的 DNS 验证方案并存于代码库中：

| 层级 | 位置 | 记录名 | 值格式 |
|------|------|--------|--------|
| libbitfs-go | paymail/dns.go:95, spec/07-paymail.md:111 | `_bitfs_pubkey.{domain}` | raw pubkey hex |
| bitfs engine | engine/publish.go:64, user-guide.md:417 | `_bitfs.{domain}` | `bitfs=<pubkey>` |

**影响**: 运行时不兼容——publish 设置 `_bitfs.`，paymail 验证查找 `_bitfs_pubkey.`。
**修复**: 统一为一种方案，建议采用更语义化的 `_bitfs.{domain}` + `bitfs=<pubkey>` 格式。

---

### H10. 网站 BIP32 路径简化错误

**网站** (`websites/bitfs.org/Website-Content-Outline.md:202-207`):
```
m/0' -- identity
m/1' -- filesystem root
m/2' -- payment
```

**白皮书/设计/代码**: 使用完整 BIP44 路径
```
m/44'/236'/0' -- 费用密钥链
m/44'/236'/1' -- Vault #0 根目录
```

网站的简化路径缺少 purpose(`44'`) 和 coin_type(`236'`) 层级，且 "identity" 概念在设计中不存在。

---

### H11. FREE 模式 KDF 输入不精确

**白皮书** (`BitFS-Whitepaper-Outline.md:114`):
```
aes_key = KDF(P_node, key_hash)
```

**设计文档** (`3-DetailedDesign.zh.md:207-208`):
```
aes_key = KDF(P_node.x, key_hash)
```

`P_node` 是 33 字节压缩公钥，`P_node.x` 是 32 字节 x 坐标。KDF 输入不同会产生不同密钥。

---

### H12. 白皮书省略 HKDF info 参数

**白皮书** (`BitFS-Whitepaper-Outline.md:99`):
```
aes_key = HKDF-SHA256(point.x, key_hash)
```

**代码/设计**: HKDF 有三个输入 `(ikm, salt, info)`，白皮书缺少 `info="bitfs-file-encryption"`。
按白皮书实现（无 info）会派生出不同的密钥。

---

## MEDIUM — 命名不一致与过时信息 (11)

### M1. 项目结构过时

`2-SystemDesign.zh.md:1916-1943` 列出的 `bitfs/internal/` 包含 method42/wallet/tx/metanet/spv/storage/x402/paymail 等包，这些在 2026-02-21 libbitfs 抽取重构后已移至 `libbitfs-go/`。当前 `bitfs/internal/` 只有 `buyer/`, `client/`, `daemon/`, `engine/`。

---

### M2. 存储目录名：三个不同名称

| 名称 | 出处 |
|------|------|
| `~/.bitfs/store/` | 2-SystemDesign.zh.md:75, spec/06-storage.md |
| `~/.bitfs/data/` | 2-SystemDesign.zh.md:1891, whitepaper |
| `~/.bitfs/storage/` | 3-DetailedDesign.zh.md:495, 代码, CLAUDE.md |

**正确值**: `~/.bitfs/storage/` (代码实现)

---

### M3. 配置文件格式：三种不同说法

| 格式 | 出处 |
|------|------|
| `config.toml` | 2-SystemDesign.zh.md:64 |
| `config.yaml` | 4-TestDesign.zh.md:513, 3-DetailedDesign.zh.md:3025 |
| `config` (key=value) | 代码 libbitfs-go/config/config.go:172, CLAUDE.md |

**正确值**: `~/.bitfs/config`，key=value 格式 (代码实现)

---

### M4. Daemon 默认端口：80 vs 8080

| 值 | 出处 |
|----|------|
| `:80` / `:443` | spec/10-cmd-bitfs.md:107, 1-ConceptDesign.zh.md:93 |
| `:8080` | 代码 daemon.go:168, user-guide.md:482, api-reference.md:7 |

**正确值**: `:8080` (开发默认值)

---

### M5. DustLimit 546 残留 + 1 个失败测试

`spec/TASKS.md:57` 仍写 "546 聪"。`integration/tx_build_extra_test.go:547` 断言 `DustLimit==546` 会失败（实际为 1）。

---

### M6. 用户指南缺失 7 个 Shell 命令

代码实现了 22 个 shell 命令，用户指南只列 12 个。缺失: `cat`, `get`, `mget`, `mput`, `cp`, `publish`, `unpublish`。

---

### M7. Go 版本：设计说 1.21+，实际 1.25.6

`2-SystemDesign.zh.md:1906` 写 "Go 1.21+"，误导最低版本要求。实际 `go.mod` 声明 `go 1.25.6`。

---

### M8. Metanet 挑战 k 起始值不一致

| 文档 | 位置 | k 起始值 |
|------|------|----------|
| metanet/3-DetailedDesign.zh.md | line 43 | `k := 0` (0-based) |
| metanet/4-TestDesign.zh.md | line 50 | `k=1..N` (1-based) |

不同起始值会产生完全不同的 challenge hash 序列。

---

### M9. 多项 L3 详细设计无对应 L4 测试

以下 L3 功能点无 L4 测试用例覆盖：

| L3 功能 | 描述 |
|---------|------|
| Koblitz 加密 (Section 5-B) | 对称密钥的椭圆曲线点映射封装 |
| 内容压缩 (Section 8-B.D) | 4 种方案: NONE, LZW, GZIP, ZSTD |
| CLTV 时锁访问 (Section 7-B) | 3 种模式: Embargo, Expiry, Subscription |
| Hash Chain Token 批量购买 | Token 生成、兑换、验证 |
| 目录级 BIP32 xpub 解锁 | `S_child = S_parent + offset * P_buyer` |
| BSV Anchor 交易 (Metanet) | 锚定格式、字段验证 |

---

### M10. Slides 字体/颜色偏离 VI 系统

| 属性 | VI 系统定义 | Slides 实际 |
|------|-----------|-------------|
| 正文字体 | Inter | IBM Plex Sans |
| 强调色 | `#c9956b` | `#d4a574` |

Slides 还引入了 VI 中不存在的 `--accent-pink: #e8b4b8` 和 `--accent-gold: #c9b896`。

---

### M11. SPV Proof 端点：代码有但设计无

`bitfs/internal/daemon/routes.go:35` 实现了 `GET /_bitfs/spv/proof/{txid}`，但没有任何设计文档提及此端点。反向不一致。

---

## LOW — 文档维护问题 (8)

### L1. NodeType 枚举缺 ANCHOR 类型

设计文档定义 `FILE=0, DIR=1, LINK=2`。代码额外有 `ANCHOR=3`（git-remote-bitfs 用）。

### L2. Anchor TLV Tags (0x20-0x26) 未写入设计文档

`parser.go:42-48` 定义了 7 个 anchor 专用 TLV tag，设计文档中无记载。

### L3. CLI 命令列表：L0 混淆 bitfs 和 b-tools

`0-OverallDesign.zh.md:76` 将 `get/ls/cat` 列为 bitfs 子命令。实际 `ls/cat/get` 是 b* 工具和 shell 命令，不是 bitfs 顶层子命令（虽然最近 cat/get 也作为 bitfs 子命令添加了）。

### L4. bitfs put 默认 FREE vs bput 继承父目录

`bitfs put` 默认创建公开免费文件。`bput` 继承父目录设置。同一操作通过两种接口有不同默认行为。

### L5. Paymail profile URL 路径不匹配

设计: `/api/v1/profile/{alias}@{domain.tld}`。代码: `/api/v1/public-profile/...`。且代码中该路径无实际 handler。

### L6. Section 编号跳过 19

`2-SystemDesign.zh.md` 从第 18 节跳到第 20 节，第 19 节不存在。

### L7. Spec 包路径用 `libbitfs/` 而非 `libbitfs-go/`

TASKS.md 等 spec 文件引用 `libbitfs/method42/` 等路径，实际 Go module 名和目录名是 `libbitfs-go/`。

### L8. 交叉引用缺失

- Metanet L1 多次引用 x402 但未链接到 BitFS 设计文档中的 x402 定义
- `revshare/` 在 L0 整体设计中列出但无任何描述或链接
- L4 TestDesign 中的测试文件路径引用 pre-extraction 结构（如 `src/internal/method42/encrypt_test.go`）

---

## 修复优先级建议

### P0 — 立即修复 (协议级/外部文档错误)

这些只需修改一两处文档文本，但不修复可能导致互操作性问题或公开场合的尴尬。

| # | Issue | 工作量 | 修改位置 |
|---|-------|--------|----------|
| 1 | **C1** HKDF info | 1 行 | 2-SystemDesign.zh.md:466 |
| 2 | **C2** TLV tag 编号 | ~20 行 | 2-SystemDesign.zh.md:311-394 |
| 3 | **C4** Dust limit | 3 处 | 白皮书:143-144, TASKS.md:57, git-remote-bitfs CLAUDE.md:64 |
| 4 | **C5** Staking 立场 | 产品决策 | 统一网站或统一设计/白皮书 |
| 5 | **C6** 检索费货币 | 1 处 | metanet.org Website-Content-Outline.md:181 |
| 6 | **C7** 三层架构 | 3 处 | metanet.org Website-Content-Outline.md:139-141 |
| 7 | **C8** HTLC 发起方 | 1 处 | bitfs.org Website-Content-Outline.md:151 |
| 8 | **C9** wallet.db | 2 处 | 白皮书:191, 3-DetailedDesign.zh.md:1606 |

### P1 — 近期修复 (功能/架构一致性)

| # | Issue | 修改位置 |
|---|-------|----------|
| 9 | **H1** revenue_share 类型 | 确定一种定义，统一两个 L2 |
| 10 | **H3** rm 交易数 | 2-SystemDesign.zh.md:213,834 |
| 11 | **H4** mv 跨目录 | 3-DetailedDesign.zh.md Section 4-B |
| 12 | **H7** Exit codes | spec/10-cmd-bitfs.md:67-76 |
| 13 | **H9** DNS 记录格式 | 统一 paymail/dns.go 与 engine/publish.go |
| 14 | **H10** BIP32 路径 | bitfs.org Website-Content-Outline.md:202-207 |
| 15 | **H11/H12** 白皮书 KDF | BitFS-Whitepaper-Outline.md:99,114 |
| 16 | **M5** 失败测试 | tx_build_extra_test.go:547, TASKS.md:57 |

### P2 — 版本更新时修复 (文档更新)

| # | Issue | 修改位置 |
|---|-------|----------|
| 17 | **H2** libbitfs 包列表 | 0-OverallDesign.zh.md:60-69 |
| 18 | **M1** 项目结构 | 2-SystemDesign.zh.md:1916-1943 |
| 19 | **M2/M3** storage/config 命名 | 多处 |
| 20 | **H8** CLI 命令补齐 | spec/10-cmd-bitfs.md |
| 21 | **M6** Shell 命令文档 | user-guide.md |
| 22 | **C3** PRIVATE TLV 序列化 | 需代码变更: parser.go |

### P3 — 低优先级

L1-L8 和其余 MEDIUM 项。可在相关功能开发时顺便修复。

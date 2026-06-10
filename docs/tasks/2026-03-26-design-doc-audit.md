# 设计文档全面审计 — 差异清单

> 日期: 2026-03-26
> 方法: 5 个并行 subagent 对照代码逐项扫描全部 5 份设计文档
> 范围: OverallDesign + BitFS ConceptDesign/SystemDesign/DetailedDesign/TestDesign

---

## 统计摘要

| 优先级 | 数量 | 说明 |
|--------|------|------|
| P0 — 事实性错误 | 18 | 文档描述与代码行为矛盾 |
| P1 — 重要缺失 | 16 | 已实现功能在文档中未提及 |
| P2 — 过时内容 | 14 | 描述已废弃或已改变的设计 |
| P3 — 细节/结构 | 8 | 命名、路径、格式等小差异 |
| **总计** | **56** | |

---

## P0 — 事实性错误（文档与代码矛盾）

### P0-1. Exit codes 完全错误
- **文档**: SystemDesign L1980-1983: "3=网络错误, 4=数据验证, 5=认证, 6=未找到, 7=支付"
- **代码**: main.go:23-31: `exitWalletError=3, exitNetError=4, exitPermError=5, exitNotFound=6, exitConflict=7`
- **修正**: 重写 exit code 表

### P0-2. DefaultFeeRate 值错误
- **文档**: DetailedDesign L939: "DefaultFeeRate = 1 // sat/KB"
- **代码**: tx/opreturn.go: `DefaultFeeRate = 100` (sat/KB, 即 0.1 sat/byte)
- **修正**: 更新为 100

### P0-3. DefaultHTLCTimeout 值错误
- **文档**: DetailedDesign L1386: "timeout=144 blocks"
- **代码**: payment/htlc.go: `DefaultHTLCTimeout = 72` (~12h)
- **修正**: 更新为 72

### P0-4. Session TTL 值错误
- **文档**: DetailedDesign: "30 分钟有效"
- **代码**: handshake.go: `DefaultSessionTTL = 24 * time.Hour`
- **修正**: 更新为 24 小时

### P0-5. Handshake 协议字段名完全不一致
- **文档**: DetailedDesign L1778: 请求字段 `pubkey, nonce, timestamp`，响应含 `verify` (HMAC)
- **代码**: HandshakeRequest: `buyer_pub, nonce_b, timestamp`; HandshakeResponse: `seller_pub, nonce_s, timestamp, session_id, expires_at`。无 HMAC verify 字段
- **修正**: 重写 handshake 协议规范

### P0-6. Health endpoint 响应格式错误
- **文档**: DetailedDesign L1695: 返回 `{status, version, p_node, vault, domain, block_height}`
- **代码**: routes.go handleHealth: 仅返回 `{"status":"ok"}`
- **修正**: 更新为实际响应

### P0-7. Koblitz 加密描述了不存在的实现
- **文档**: SystemDesign L416-444 和 DetailedDesign 五-B: 描述 Koblitz 点映射加密 aes_key
- **代码**: method42/ 中没有任何 Koblitz 实现。实际使用 ECDH + HKDF + capsule XOR 机制
- **修正**: 删除 Koblitz 加密描述，改写为实际的 capsule 机制

### P0-8. RegTest RPC 端口错误
- **文档**: SystemDesign L39: "RegTest RPC 端口 18332"
- **代码**: wallet/network.go:48: `RegTest.RPCPort = 18443`
- **修正**: 更新为 18443

### P0-9. 环境变量名错误
- **文档**: SystemDesign L56-66: "BITFS_HOME"
- **代码**: main.go/datadir.go: `BITFS_DATADIR`
- **修正**: 全局替换 BITFS_HOME → BITFS_DATADIR

### P0-10. Wallet 子命令名错误
- **文档**: SystemDesign L129: `wallet info`; DetailedDesign L1543: `wallet info`
- **代码**: cmd_wallet.go: `wallet show` (非 info)。`balance` 是独立子命令
- **修正**: info → show，补充 balance

### P0-11. 多个文档中的 CLI 命令不存在
- **涉及多处**: `bitfs init`, `bitfs fsck`, `bitfs decrypt`(顶层), `bitfs sales`, `bitfs rmdir`, `bitfs wallet restore`, `bitfs vault use`, `bitfs vault info`, `bitfs daemon status`, `bitfs daemon config`, `bitfs lock`, `bitfs unlock`
- **代码**: main.go switch 中均不存在
- **修正**: 删除不存在的命令，标注为"计划中"或直接移除

### P0-12. `bitfs put` flags 完全不同
- **文档**: DetailedDesign L1433: `--encrypt, --keyword, --description, --mime`
- **代码**: cmd_put.go: `--vault, --access (free|private), --json, --datadir, --password, --network`
- **修正**: 重写 put 命令 flags

### P0-13. capsule_hash 公式内部矛盾
- **文档**: DetailedDesign L245: `capsule_hash = SHA256(capsule)`; L2045: `capsule_hash = SHA256(fileTxID || capsule)`
- **代码**: method42/ecdh.go: `ComputeCapsuleHash(fileTxID, capsule)` = SHA256(fileTxID || capsule)
- **修正**: 统一为 SHA256(fileTxID || capsule)

### P0-14. Buy endpoint 响应字段不一致
- **文档**: DetailedDesign L1800: GET /buy 返回 `txid, price_per_kb, file_size, total_price, access, capsule_hash`
- **代码**: handleGetBuyInfo 返回 `invoice_id, total_price, capsule_hash, price_per_kb, file_size, payment_addr, seller_pubkey, paid, capsule_nonce`
- **修正**: 重写 buy endpoint 规范

### P0-15. Metanet 出块时间错误
- **文档**: OverallDesign L44/L173: "5 分钟 PoW"
- **代码**: metanet/internal/chain/params.go: `TargetBlockTimeSec = 600` (10 分钟)
- **修正**: 更新为 10 分钟

### P0-16. Daemon 默认端口不一致
- **文档**: ConceptDesign L97 决策#21: "标准 HTTP 80/443"
- **代码**: config.go/daemon.go: 默认 `127.0.0.1:8080`
- **修正**: 更新为 8080

### P0-17. Node ID 描述与实际不符
- **文档**: SystemDesign L236: "Node ID = H(P_node || TxID_node || Vout)"
- **代码**: metanet/node.go: 没有 hash 函数，Node 直接用 (PNode, TxID, Vout) 三元组作为身份
- **修正**: 更新为三元组，删除 hash 函数描述

### P0-18. sell 命令 `--recursive` flag 不存在
- **文档**: SystemDesign L817, DetailedDesign L1498: `--recursive`
- **代码**: cmd_sell.go: 无 --recursive flag
- **修正**: 删除 --recursive

---

## P1 — 重要缺失（已实现但文档未提及）

### P1-1. 9 个已实现的 CLI 命令未在文档中
- `bitfs ls` (顶层 ls，非 shell), `bitfs status`, `bitfs verify`, `bitfs cat` (顶层), `bitfs get` (顶层), `bitfs mget`, `bitfs mput`, `bitfs paymail` (bind/unbind/list), `bitfs unpublish`
- **修正**: 补充所有命令到 ConceptDesign 架构图、SystemDesign 命令表、DetailedDesign CLI 参考

### P1-2. bmget 工具未在 b* 工具列表中
- ConceptDesign L15 和 OverallDesign 均只列 5 个 b* 工具
- **代码**: bitfs/cmd/bmget/ 存在
- **修正**: 补充 bmget

### P1-3. 环境变量支持未文档化
- `BITFS_PASSWORD`, `BITFS_NETWORK`, `BITFS_DATADIR` 在代码中已实现
- **修正**: 在 SystemDesign 中添加环境变量章节

### P1-4. --json 输出模式未作为设计原则
- 几乎所有命令支持 `--json` flag，这是 Agent-friendly 的核心特性
- **修正**: 在 ConceptDesign 设计原则中补充

### P1-5. stdin pipe 支持 (`put -`) 未文档化
- cmd_put.go:68-69: localFile == "-" 时读取 stdin
- **修正**: 在 SystemDesign put 命令和 ConceptDesign Unix 哲学原则中补充

### P1-6. `vault export` 子命令未文档化
- cmd_vault.go: 支持 wif/hex/seed-path 格式导出
- **修正**: 补充到 vault 命令表

### P1-7. AES-GCM AAD (Additional Authenticated Data) 未文档化
- method42/encrypt.go: keyHash 作为 AAD 传给 aesGCMEncrypt; 元数据加密用 salt 作 AAD
- **修正**: 这是安全关键细节，需补充到 SystemDesign 加密章节

### P1-8. Dashboard SPA 未提及
- bitfs/dashboard/ React SPA 嵌入 daemon (embed.go)
- **修正**: 补充到 OverallDesign Layer 2 描述和 SystemDesign daemon 章节

### P1-9. libbitfs-ts 未在 OverallDesign 提及
- TypeScript 镜像库 ~11.5K LOC, 11 包, 875 测试
- **修正**: 补充到 OverallDesign 共享库章节

### P1-10. 子项目生态未在 OverallDesign 提及
- git-remote-bitfs, bitfs-app, bitfs-extension, bitfs-explorer, bitfs-desktop, den-explorer
- **修正**: 在 OverallDesign 添加生态概览

### P1-11. libbitfs-go 缺少 vault/ 和 engine/ 包
- ConceptDesign 架构图和 OverallDesign 包列表均未列出 vault/ 和 engine/
- **修正**: 补充这两个重要包

### P1-12. Daemon 路由表不完整
- 缺少: `POST /_bitfs/admin/reload`, `GET /_dashboard/`, `GET /api/v1/public-profile/{handle}`, `GET /api/v1/verify/{handle}/{pubkey}`
- **修正**: 补充到 DetailedDesign daemon 章节

### P1-13. SPV VerifyHeaderChainWithWork 未文档化
- spv/header.go:337 提供网络感知的最小难度和难度转换验证
- **修正**: 补充到 SystemDesign SPV 章节

### P1-14. Metanet 不使用 libbitfs-go
- OverallDesign L77/L103 声称"两产品共享同一 Go 库"
- **代码**: metanet/go.mod 无 libbitfs-go 依赖
- **修正**: 标注为"计划中"，当前 metanet 独立实现

### P1-15. TestDesign 大量测试类别缺失
- vault/ (123 tests), tx/ (74), network/ (123), payment/ (91), internal/buy/ (55), internal/client/ (70) 均无对应 T-category
- **修正**: 添加新测试类别

### P1-16. TestDesign 测试计数全面过时
- 文档: 649 existing / 1022 target
- 实际: ~2700+ test functions across ~90 files
- **修正**: 重写所有计数和文件路径

---

## P2 — 过时内容（描述已改变的设计）

### P2-1. teratestnet 网络不存在
- SystemDesign L39, ConceptDesign L141: 列出 teratestnet
- **代码**: wallet/network.go 只有 mainnet/testnet/regtest
- **修正**: 删除 teratestnet

### P2-2. Rabin 标注为"计划中"但已实现
- ConceptDesign L28: "(计划中)"
- **代码**: method42/rabin.go 完整实现 (GenerateRabinKey, RabinSign, RabinVerify)
- **修正**: 更新状态

### P2-3. ZSTD 压缩未实现
- ConceptDesign L134: 列为压缩选项
- **代码**: storage/compress.go: CompressZSTD 返回 ErrUnsupportedCompression
- **修正**: 标注为未实现

### P2-4. Token 预购系统 (十一-B) 完全未实现
- DetailedDesign 整节描述 hash chain token
- **代码**: 无任何实现
- **修正**: 标注为"设计中/未实现"

### P2-5. Koblitz 点映射加密 (五-B) 完全未实现
- DetailedDesign 整节描述 Koblitz 加密
- **代码**: 零实现
- **修正**: 标注或删除（见 P0-7）

### P2-6. Covenant ISO 脚本未实现
- DetailedDesign 十四-B 描述 Covenant 互锁脚本
- **代码**: revshare/ 只有离线分配数学，无 Bitcoin Script Covenant
- **修正**: 标注为"设计中/未实现"

### P2-7. BIP32 目录购买 (十五-B) 未实现
- DetailedDesign 整节描述 ECDH 传递性目录购买
- **代码**: 无 BuyDirectory 或 DeriveChildECDH 函数
- **修正**: 标注为"设计中/未实现"

### P2-8. bsync/bput (二十三-B) 未实现
- DetailedDesign 整节、ConceptDesign 决策#41-44
- **代码**: 无 bsync, bput 命令或同步逻辑
- **修正**: 标注为"设计中/未实现"

### P2-9. Session lock/unlock 未实现
- SystemDesign L2098-2237 描述 `bitfs lock/unlock` 和 session 文件
- **代码**: 无实现
- **修正**: 标注为"设计中/未实现"

### P2-10. Group Signature / BLS12-381 ACL 未实现
- ConceptDesign 决策#22/35/36, DetailedDesign 二十二-B
- **代码**: 零实现
- **修正**: 标注为"Phase 4 计划"

### P2-11. Shell 命令列表过时
- ConceptDesign L95 列出 `lpwd`, `!` (escape to local shell)
- **代码**: cmd_shell.go 无这两个命令；有额外的 cd, decrypt, sales, publish, unpublish
- **修正**: 更新 shell 命令列表

### P2-12. `bitfs publish` 不支持 path 参数
- SystemDesign L533: `bitfs publish <domain> [path]`
- **代码**: cmd_publish.go: 只接受 domain，无 path 参数
- **修正**: 删除 [path]

### P2-13. 不使用 go-paymail 库
- SystemDesign L656: "使用 github.com/bsv-blockchain/go-paymail"
- **代码**: 自定义实现 libbitfs-go/paymail/
- **修正**: 更新为自定义实现

### P2-14. Session 管理是纯内存，非混合模式
- ConceptDesign L110 决策#34: "daemon 在线用 Unix socket, 离线用 session 文件"
- **代码**: daemon.go: 纯内存 map[string]*Session
- **修正**: 更新为内存模式

---

## P3 — 细节偏差

### P3-1. internal/buyer/ → internal/buy/
- SystemDesign L1948: "internal/buyer/"
- **代码**: 目录名是 "internal/buy/"

### P3-2. TLV 压缩常量表内部矛盾
- DetailedDesign L3245: "Compression: None(0)/Gzip(1)"
- DetailedDesign L1265: "NONE=0, LZW=1, GZIP=2, ZSTD=3"
- **代码**: 与 L1265 一致

### P3-3. ChildEntry 二进制格式未指定字节序
- SystemDesign L325: 未说明 endianness
- **代码**: parser.go 使用 LittleEndian

### P3-4. TLV Length 未明确说明 LEB128
- SystemDesign L295: 只说 "uvarint"
- **代码**: parser.go: binary.Uvarint (LEB128)

### P3-5. genesis_hash 带 0x 前缀 vs 代码不带
- SystemDesign L42: JSON 示例 "0x000..."
- **代码**: wallet/network.go 用纯 hex 不带前缀

### P3-6. TestDesign 几乎所有文件路径错误
- 如 `method42/encrypt_test.go` → 实际 `method42/method42_test.go`
- 几乎每个 T-category 都引用了不存在的文件名

### P3-7. 文档引用不存在的 spec 文件
- SystemDesign L218: "5-TransactionSpec.zh.md"
- DetailedDesign L2284: "implementation/src/cmd/git-remote-bitfs/"
- 这些路径不存在

### P3-8. OverallDesign 测试计数表述歧义
- L155: "~1022 测试用例" 读起来像当前数量，实际是目标
- 实际已有 ~2700+

---

## C 阶段待讨论（设计决策重审）

以下问题需要讨论"文档该改还是设计该改"：

1. **Koblitz 加密** — 从未实现。删除还是保留为远期设计？
2. **Token 预购系统** — 从未实现。当前 HTLC 是否已足够？
3. **bsync/bput** — 从未实现。是否还需要？
4. **Session lock/unlock** — 从未实现。daemon 的内存 session 是否已足够？
5. **Covenant ISO** — 只有离线数学。链上 Covenant 是否还在路线图上？
6. **BIP32 目录购买** — 从未实现。是否为远期特性？
7. **CLTV 时间锁** — 只有基础 height check。daemon 行为未实现。保留？
8. **Group Signature (BLS12-381)** — 零实现。Phase 4？
9. **ZSTD 压缩** — 返回 Unsupported。是否实现还是删除？
10. **Metanet 与 libbitfs-go 共享** — 当前独立。何时整合？

---

## 修正进度

### ✅ 批次 1: P0 事实性错误（18 项）— 已完成
4 个并行 subagent 修正了全部 4 份设计文档：
- **OverallDesign**: 出块时间 5→10 分钟、命令列表、包描述、b-tools 行、测试计数
- **ConceptDesign**: Koblitz→ECDH（6 处）、Rabin 状态、命令列表、3 个新设计原则、9 个设计决策更新
- **SystemDesign**: Exit codes、RPC 端口、BITFS_HOME→BITFS_DATADIR、钱包/vault 子命令、加密章节重写、Node ID、12 个不存在命令删除/修正、新增"其他命令"章节
- **DetailedDesign**: FeeRate、HTLC timeout、Session TTL、Handshake 协议重写、Health endpoint、capsule_hash 公式、Buy endpoint、CLI 命令标注、put flags、TLV 压缩常量

### ✅ 批次 2: P1 重要缺失（16 项）— 已完成
18/19 项在 P0 批次中附带完成。剩余 1 项（libbitfs-ts/子项目生态）归入 C 阶段讨论。

### ✅ 批次 3: P2 过时内容（14 项）— 已完成
- SystemDesign: teratestnet 残余清除、go-paymail 库描述更正
- DetailedDesign: 6 个未实现章节添加警告标注（Koblitz/CLTV/Token/Covenant/BIP32目录购买/ACL）
- TestDesign: 全面更新 32 个测试类别的计数（649→2700+）和文件路径、标注 6 个未实现类别

### ✅ 批次 4: P3 细节（8 项）— 已完成
在 P0/P2 批次中附带修正：buyer/→buy/、ChildEntry 字节序、LEB128 说明、TLV 压缩常量等

### ✅ 批次 5: C 阶段决策 — 已完成
10 个设计决策逐一讨论，结果:
- **删除**: Koblitz 点映射加密（五-B 整节删除，ECDH+HKDF 方案已取代）
- **写入 roadmap P2**: BIP32 目录购买、Token 预购、bsync/bput、Session lock/unlock、CLTV daemon 行为、ZSTD 压缩
- **写入 roadmap P3**: Covenant ISO 收益权证券化、Metanet 整合 libbitfs-go
- **写入 roadmap P4**: Group Signature / ACL (BLS12-381)
- **如实标注**: Metanet 独立实现（已在 P0 修正中完成）

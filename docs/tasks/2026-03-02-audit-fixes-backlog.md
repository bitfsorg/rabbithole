# 审计修复积压 — 全量汇总

**创建日期**: 2026-03-02
**来源**: 27 份未完全关闭的审计报告（去重合并后）
**目标**: 逐项修复后，在对应审计文档中标记，全部完成后移入 `done/`

> 标注格式: `[来源]` = 审计文件名（省略 `docs/audits/` 前缀和日期前缀）
> 同一问题多份报告提及时，列出所有来源

---

## 一、libbitfs-go — 核心库

### 1.1 method42/

- [ ] **H-2**: 密钥材料（aesKey, sharedX, buyerMask）使用后未清零，存在内存取证风险 `[R01-method42]`

> 其他项（H-1 AAD, M-1~M-6）均已修复

### 1.2 x402/

- [ ] **H-1**: `VerifyPayment` 不验证 Input 签名（已加 WARNING 注释，需上层封装 TxID 去重 + 上链确认） `[R02-x402, security-review_by_codex, deep-design-review_by_antigravity#1]`
- [ ] **H-2**: `VerifyPayment` 不绑定 InvoiceID（返回 TxID 供调用方追踪，但库级无保护） `[R02-x402, security-review_by_codex]`
- [ ] **M-3**: `CalculatePrice` 溢出时返回 `MaxUint64` 而非 error `[R02-x402]`
- [ ] **M-4**: `BuildHTLCFundingTx` 无 change output 时仍计算 change 手续费 `[R02-x402]`
- [ ] **M-5**: `BuildBuyerRefundTx` 信任 `FundingAmount` 不做交叉校验 `[R02-x402]`
- [ ] **HTLC 无 CLTV**: OP_ELSE 分支用 2-of-2 多签，预签名交易丢失则资金永久锁定，需加 OP_CHECKLOCKTIMEVERIFY `[R02-x402, deep-design-review_by_antigravity#3, libbitfs-ts-pre-release-audit]`

### 1.3 spv/

- [ ] **M-1**: `ValidateDifficultyTransition` 零 target 静默通过（返回 nil） `[R03-spv]`
- [ ] **M-2**: `VerifyTransaction` 不检查最低网络难度 `[R03-spv, architecture-review§3.3]`
- [ ] **M-3**: `VerifyHeaderChain` 同样缺少最低难度检查 `[R03-spv]`
- [ ] **M-4**: `CompactToTarget` 不校验 exponent 边界（>34 静默产生零 target） `[R03-spv]`
- [ ] **M-5**: `MemHeaderStore` 高度冲突时静默覆盖 `[R03-spv]`
- [ ] **L-1~L-5**: 5 项低优先级（详见审计文档） `[R03-spv]`

### 1.4 wallet/

- [ ] **M-1**: `DecryptSeed` 校验和比较非常量时间（应用 `subtle.ConstantTimeCompare`） `[R04-wallet]`
- [ ] **M-2**: `EncryptSeed` 敏感缓冲区（derivedKey, plaintext, seedHash）未清零 `[R04-wallet]`
- [ ] **M-3**: `DecryptSeed` 敏感缓冲区（derivedKey）未清零 `[R04-wallet]`
- [ ] **M-4**: `Wallet.masterKey` 无 `Close()`/`Destroy()` 方法 `[R04-wallet]`
- [ ] **M-6**: `EncryptSeed` 静默接受空密码 `[R04-wallet]`

> H-1 (json:"-") 和 M-5 (DeriveFeeKey chain 校验) 已修复

### 1.5 tx/

- [ ] **H-2**: `BuildDataTransaction` 仍是 stub，返回 `RawTx: nil` `[R05-tx]`
- [ ] **M-1**: `ParseTxNodeOps` 无法处理 OpDelete 输出格式 `[R05-tx]`
- [ ] **M-2**: `Build()` 对 OpUpdate/OpDelete 不检查 `InputUTXO != nil` `[R05-tx]`
- [ ] **M-3**: `EstimateTxSize` 多 op 批次公式不准（已注释说明偏高可接受） `[R05-tx]`
- [ ] **M-4**: 单 op 批次变更输出回退到节点密钥（多 op 已修复） `[R05-tx]`
- [ ] **M-5**: `Sign()` 用 `InputUTXO.PrivateKey`，`Build()` 校验 `op.PrivateKey`，两字段不一致 `[R05-tx]`
- [ ] **M-6**: `MetaFlagBytes` 是可变 `var []byte`，可被外部修改 `[R05-tx, concurrency-safety§M-13]`

> H-1 (EstimateFee 溢出) 和 H-3 (fee/node 输入去重) 已修复

### 1.6 metanet/

- [ ] **M-3**: `VerifyChildMembership` 实现问题 `[R06-metanet]`
- [ ] **L-1**: 死代码 sentinel error `[R06-metanet]`
- [ ] **L-5**: 元数据序列化非确定性 `[R06-metanet]`

> M-1, M-2, M-4, M-5, L-2~L-4, L-6 已修复

### 1.7 network/

- [ ] **M-4**: `SyncHeaders` 与 `RPCClient` 耦合 `[R08-network]`

> M-1~M-3, L-1~L-5 已修复

### 1.8 vault (libbitfs-go/vault/)

- [ ] **H-2**: `Remove` 用两个独立交易而非原子 `MutationBatch` `[R11-vault]`
- [ ] **H-3**: `flock.go` 仅 Unix 实现，Windows 构建失败 `[R11-vault]`
- [ ] **M-1~M-6**: 6 项中等问题（详见审计文档） `[R11-vault]`

> H-1 (mustDecompressPubKey 返回 nil) 已修复

---

## 二、bitfs — CLI + Daemon

### 2.1 internal/daemon/

- [ ] **M-1**: `handleMeta` 对 paid/private 节点泄露 `key_hash` `[R12-daemon]`
- [ ] **P2-1**: `handlePayInvoice` 持锁期间做 I/O `[pre-release-review_by_codex, code-only-review_by_codex]`
- [ ] **B-03**: Dashboard SPA `embed.go` 仍注释状态，静态文件未嵌入 `[full-spectrum-audit-03-01_by_codex]`

> H-1 (HTLC 回退到 P2PKH) 和 H-2 (dashboard/sales 无认证) 已修复

### 2.2 internal/engine/ (buy)

- [ ] **M-2**: `CostSatoshis` 死代码（仅影响整洁度） `[R13-buy]`

> C-1 (InvoiceID 缺失) 和 H-1 (capsule hash 校验) 已修复

### 2.3 internal/client/

- [ ] **M-4**: SRV 端点不拒绝 `http://`（仅对裸端点补 https） `[R14-client]`
- [ ] **API-H-2**: `metaNodeResponse` 不返回 TxID，SPV 验证无法使用 `[api-consistency-audit]`
- [ ] **API-H-3**: `ChildInfo` 缺少 json tags `[api-consistency-audit]`

> API-H-1 (total_price mismatch) 和 API-H-4 (base58 编码) 已修复

### 2.4 cmd/ (CLI + b-tools)

- [ ] **H-1**: `bmget` 路径穿越 — `childEntry.Name` 未校验 `..`/`/`/`\` `[R15-cmd]`
- [ ] **H-2**: `--wallet-key` 仍接受明文 hex（已有 `@filepath` 和 env var 替代） `[R15-cmd]`
- [ ] **H-3**: 钱包密码 string 切片拷贝未清零 `[R15-cmd]`
- [ ] **M-1**: `wallet.MainNet` 硬编码，testnet/regtest 无法使用 `[R15-cmd]`
- [ ] **M-2**: Shell `sales` 命令硬编码 `localhost:8080` `[R15-cmd]`
- [ ] **H-03**: 钱包网络信息未展示 `[full-spectrum-audit-03-01_by_codex]`

### 2.5 API 一致性

- [ ] **C-1**: bitfs 与 b-tools 退出码语义相反 `[api-consistency]`
- [ ] **C-2**: bitfs 命令无 JSON 输出选项 `[api-consistency]`
- [ ] **L-1**: `context.Context` 仅在 network 包使用，其他 I/O 函数缺失 `[api-consistency]`
- [ ] **API-M-1~M-9**: 错误信息泄露内部细节（9 项） `[api-consistency-audit]`

### 2.6 并发安全（架构级）

- [ ] **C-1**: `InvoiceRecord` 数据竞争 — 指针逃逸锁作用域 `[concurrency-safety]`
- [ ] **H-1**: Engine 无并发保护（daemon 多请求场景） `[concurrency-safety]`
- [ ] **H-2**: `WalletState.NextChangeIndex` 竞态 `[concurrency-safety]`
- [ ] **H-3**: UTXO 切片截断绕过锁 `[concurrency-safety]`
- [ ] **H-4**: `LocalState.mu` 直接访问（应封装） `[concurrency-safety]`
- [ ] **H-5**: `InvoiceRecord` 指针逃逸锁（同 C-1） `[concurrency-safety]`
- [ ] **H-9**: metanet Node/Directory 无锁修改 `[concurrency-safety]`
- [ ] **H-10**: `ErrDuplicateHeader` 导致 SPV 假失败 `[concurrency-safety]`
- [ ] **M-1~M-17**: 17 项中等并发问题（详见审计文档） `[concurrency-safety]`
- [ ] **L-1~L-12**: 12 项低优先级并发问题 `[concurrency-safety]`

---

## 三、设计文档一致性

- [ ] **H5**: 14+ TLV 字段在设计文档中定义但未实现 `[design-consistency]`
- [ ] **H6**: 缺少 daemon 端点（Paymail verify_pubkey 等） `[design-consistency]`
- [ ] **H9**: DNS TXT 记录格式不一致 `[design-consistency]`
- [ ] **M9**: L3 功能无 L4 测试设计 `[design-consistency]`
- [ ] **M10**: 幻灯片字体/颜色与 VI 不一致 `[design-consistency]`

---

## 四、Metanet（开发中，优先级较低）

- [ ] **P1-3**: Method 42 Session 已创建但未强制执行 `[full-spectrum-pre-release-audit_by_codex]`
- [ ] **P1-4**: Metanet CLI 仍是 stub `[full-spectrum-pre-release-audit_by_codex]`
- [ ] **P1-5**: Metanet 用 ed25519 vs secp256k1 矛盾 `[full-spectrum-pre-release-audit_by_codex]`
- [ ] **Medium-3**: Overlay 网络可伪造签名 `[security-review_by_codex]`

---

## 五、Den Explorer

- [ ] **Issue 2**: `verify.go` TxID 长度不校验可 panic `[code-only-review_by_codex]`
- [ ] **Issue 4/5**: 搜索高度解析 + RPC 错误吞掉 `[code-only-review_by_codex]`

---

## 六、bitfs-extension

- [ ] **H-2**: Auto-lock alarm 无 setTimeout 回退（Chrome alarm 最低 1 分钟） `[bitfs-extension-audit]`
- [ ] **M-1**: 助记词在 React state 中（应仅在 service worker） `[bitfs-extension-audit]`
- [ ] **M-2**: 密码无最低强度要求 `[bitfs-extension-audit]`
- [ ] **M-3**: IndexedDB 未加密（DAG 缓存明文存储） `[bitfs-extension-audit]`
- [ ] **M-4~M-6**: 3 项中等问题（详见审计文档） `[bitfs-extension-audit]`
- [ ] **L-1~L-5**: 5 项低优先级 `[bitfs-extension-audit]`

> C-1~C-3（3 CRITICAL）、H-1/H-3/H-4 已修复

---

## 七、libbitfs-ts

- [ ] **S-02**: HTLC script 缺 OP_CHECKLOCKTIMEVERIFY（同 Go 实现） `[libbitfs-ts-pre-release-audit]`
- [ ] **A-01**: x402 refund flow（`refund.ts` 已创建但 CLTV 未加入 `htlc.ts`） `[libbitfs-ts-pre-release-audit]`
- [ ] **LOW 若干**: Q-09 (config os/path), Q-15 (NaN chunkSize) 等 `[libbitfs-ts-pre-release-audit]`

> B-01~B-03, S-01, S-04, S-05, Q-01~Q-14 等 10 项已修复

---

## 八、git-remote-bitfs

- [ ] **A-10**: `findLastImportedAnchor` O(N) notes 扫描 `[git-remote-bitfs-audit]`
- [ ] **A-11**: `lookupCommitSHA` O(N*M) 扫描 `[git-remote-bitfs-audit]`
- [ ] **B-07**: 路径中间的 `**` 静默失败 `[git-remote-bitfs-audit]`
- [ ] **B-11**: UTXO store 无事务锁（单进程设计） `[git-remote-bitfs-audit]`
- [ ] **LOW×7**: A-19~A-22, B-10, B-13~B-16 `[git-remote-bitfs-audit]`

> 全部 5 CRITICAL + 8 HIGH 已修复

---

## 九、Antigravity 设计审查（去重后剩余）

- [ ] **#5**: `ComputeCapsuleHash` 返回 nil 而非 error（部分修复） `[deep-design-review_by_antigravity]`

> #1 (VerifyPayment) → 见 1.2 x402
> #3 (HTLC CLTV) → 见 1.2 x402
> #4 (O(N) 目录) → 见 deferred.md
> #2 (NodeTypeAnchor) → 已按设计保留
> #6~#8 → 已修复

---

## 统计

| 分类 | CRITICAL | HIGH | MEDIUM | LOW | 总计 |
|------|----------|------|--------|-----|------|
| libbitfs-go | 0 | 5 | 28 | 10 | 43 |
| bitfs | 1 | 7 | 15 | 12 | 35 |
| 设计文档 | 0 | 2 | 3 | 0 | 5 |
| Metanet | 0 | 0 | 4 | 0 | 4 |
| Den Explorer | 0 | 0 | 3 | 0 | 3 |
| bitfs-extension | 0 | 1 | 6 | 5 | 12 |
| libbitfs-ts | 0 | 1 | 1 | 2 | 4 |
| git-remote-bitfs | 0 | 0 | 4 | 7 | 11 |
| **总计** | **1** | **16** | **64** | **36** | **117** |

## 建议修复优先级

1. **P0 — 安全关键**（~10 项）: HTLC CLTV, bmget 路径穿越, VerifyPayment 封装, 密钥清零, InvoiceRecord 竞态
2. **P1 — 正确性**（~15 项）: BuildDataTransaction stub, ParseTxNodeOps OpDelete, MetaFlagBytes 不可变, 并发保护
3. **P2 — 健壮性**（~30 项）: SPV 难度校验, wallet 密码策略, 常量时间比较, API 一致性
4. **P3 — 代码质量**（~60 项）: LOW 级别, 文档一致性, 死代码清理

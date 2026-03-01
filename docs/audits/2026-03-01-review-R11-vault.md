# Review R11: vault

## Overview

| Metric | Value |
|--------|-------|
| Package | `libbitfs-go/vault` |
| Source files | 16 |
| Test files | 13 |
| Source LOC | ~3,305 |
| Test LOC | ~3,170 |
| Test count | 144 |
| Coverage | 80.5% |
| Race detector | PASS |
| Design doc | `docs/plans/2026-02-28-vault-extraction.md` |

## Findings

### CRITICAL

无。核心安全模型健全：HD 密钥派生、Method 42 加解密、原子 UTXO 分配与回滚、flock 并发控制。

### HIGH

**H-1: `mustDecompressPubKey` 返回 nil 而非 panic + 调用点错误被抑制**

`helpers.go` L48-58: 名为 "must" 但失败时返回 nil。`vault.go` 5 个调用点用 `scriptPK, _ := tx.BuildP2PKHScript(mustDecompressPubKey(...))` 抑制错误。nil 公钥可能产生畸形 P2PKH 脚本，锁定资金或创建不可花费输出。

**建议**: (a) 真正 panic（仅用验证过的数据调用），或 (b) 重命名返回 error 并处理。

**H-2: `Remove` 非原子 — 产生两个独立交易**

`remove.go` L96-161: 节点删除和父目录更新分别构建为两个独立交易。第一个广播后第二个失败会留下 DAG 不一致状态。其他所有操作都使用单个原子 MutationBatch。

**建议**: 重构为单 MutationBatch 含 AddDelete + AddSelfUpdate，与其他操作一致。

**H-3: `flock.go` 使用 `syscall.Flock` — 仅 Unix 可用**

`flock.go` L15: 直接调用 POSIX API。bitfs-app 目标 Windows（Flutter FFI）。Windows 上编译失败。

**建议**: 拆分为 `flock_unix.go` + `flock_windows.go` (使用 `LockFileEx`)。非紧急，应跟踪。

### MEDIUM

**M-1: UTXO slice 无界增长 — 已花费 UTXO 从不清理**

`state.go` L197-202: `AddUTXO` 只追加不紧凑。`AllocateFeeUTXO/GetNodeUTXO/FindUTXOByPubKey` 线性扫描含已花费项。nodes.json 随时间无界增长。

**建议**: 添加 `CompactUTXOs()` 方法，在 `Save()` 时或达到阈值时调用。

**M-2: `lookupPrivKey` 线性扫描 fallback 无界**

`vault.go` L314-332: 快速 O(1) 查找失败时，扫描 `NextReceiveIndex + 10` 个密钥。超出 gap limit 的 UTXO 永远找不到。

**建议**: 确保新 fee UTXO 始终有 `FeeChain/FeeDerivIdx`，文档化 fallback 为遗留行为。

**M-3: `NodeState` 指针逃逸 mutex**

`state.go` L217-228: `GetNode/FindNodeByPath` 返回内部 map 的可变指针。调用者在无锁状态下修改。当前单写者模型安全（flock 序列化），但破坏了 mutex 暗示的封装。

**建议**: 添加文档注释说明返回的指针仅可在 vault 写锁下修改。

**M-4: `DeriveChangeAddr` 在使用前递增 `NextChangeIndex`**

`vault.go` L238-247: 操作失败时索引已递增。部分缓解：错误时 state 不持久化。但 `TrackNewUTXOs` 依赖递增值，模式脆弱。

**M-5: `WalletState` (state.json) 从 vault 内从不持久化**

`vault.go` L129: `Close()` 只保存 `v.State`（nodes.json），不保存 `WalletState`。`NextChangeIndex` 递增可能在会话间丢失，导致找零地址复用。

**建议**: 在 `Close()` 中也持久化 WalletState，或明确文档化调用者责任。

**M-6: `RefreshFeeUTXOs` 不设置派生索引**

`vault.go` L498-535: 新发现的 UTXO 添加时 `FeeChain/FeeDerivIdx` 为零值，导致 `lookupPrivKey` 走慢速扫描路径。

### LOW

**L-1: 测试辅助仍命名 `initTestEngine` / `eng`**: 遗留命名。

**L-2: `mustDecodeHex` 同样违反 "must" 约定**: `mkdir.go` L219 — 返回 nil 而非 panic。

**L-3: `DetectMimeType` 未知扩展名返回 `text/plain`**: 应返回 `application/octet-stream`。

**L-4: `reverseBytes` / `reverseBytesCopy` 重复实现**: 应合并。

**L-5: `Get` 用 `os.Create` 创建文件（默认 0666）**: 处理加密数据应用 0600。

### SUGGESTIONS

**S-1.** 添加 `Stat` 方法返回 NodeState 元数据，不解密内容。

**S-2.** `buildParentUpdatePayload` 硬编码 `Access: AccessFree`，未来目录可能有非 free 访问。

**S-3.** `PutFile` 整文件读入内存。大文件应流式读取 + chunking。

**S-4.** 写操作添加 `context.Context` 参数支持取消。

## Design Consistency

| 设计决策 | 实现 | 状态 |
|---|---|---|
| 包名 vault | `package vault` | 一致 |
| 并发: flock(2) | `syscall.Flock` | 一致（仅 Unix, H-3） |
| 锁粒度: 整个 vault | `withWriteLock` | 一致 |
| 读操作无锁 | Cat, Get 不锁 | 一致 |
| Publish/Unpublish 不在 vault | 不存在 | 一致 |
| 所有 9 个写操作用 `withWriteLock` | 验证通过 | 一致 |
| State reload inside lock | `v.State.Reload()` | 一致 |
| MutationBatch 为唯一 tx 构建路径 | 所有操作使用 | **例外**: Remove 用 2 个独立 batch (H-2) |
| rm = 1 transaction (spec) | 实现为 2 transactions | **偏差** |
| mv 跨目录 = 4-op 原子 batch | 正确实现 | 一致（优于 spec） |

## Code Quality Assessment

**优点**:
1. 一致的 UTXO 回滚模式（success flag + deferred rollback）
2. Build-then-apply 状态突变模式
3. 正确区分 `path`（POSIX）vs `filepath`（OS）
4. 80.5% 覆盖率，144 个测试
5. 跨目录 move 的 4-op 原子 batch 实现正确

**不足**:
1. 错误抑制模式（`_, _ :=`）
2. 无路径输入验证
3. 遗留 Engine 命名
4. "must" 函数不 panic

## Summary

vault 是一个扎实的统一状态管理层。从 engine 的提取执行干净，正确委托给底层包。

**必须修复**:
- H-1: mustDecompressPubKey nil + 错误抑制 — 潜在畸形脚本
- H-2: Remove 非原子 — 与其他操作和 spec 不一致

**应该修复**:
- H-3: 仅 Unix 的 flock（跟踪 Windows 支持）
- M-1: UTXO 无界增长
- M-5: WalletState 不持久化

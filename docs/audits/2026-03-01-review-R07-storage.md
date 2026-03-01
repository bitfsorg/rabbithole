# Review R07: storage

## Overview

| Metric | Value |
|--------|-------|
| Package | `libbitfs-go/storage` |
| Source files | 6 (`store.go`, `filestore.go`, `chunk.go`, `compress.go`, `resolver.go`, `errors.go`) |
| Test files | 4 (`storage_test.go`, `chunk_test.go`, `compress_test.go`, `resolver_test.go`) |
| Source LOC | ~526 |
| Test LOC | ~1,162 |
| Test count | 50 |
| Coverage | 90.5% |
| Race detector | PASS |
| Spec | `docs/specs/bitfs/06-storage.md` |

## Findings

### CRITICAL

无。

### HIGH

**H-1: 解压缩无大小限制 — zip bomb 漏洞**

`compress.go` L52-59 (LZW), L73-83 (GZIP): 两者都使用 `io.ReadAll` 无大小限制。精心构造的小载荷可解压到 GB 级，导致 OOM。

```go
r := lzw.NewReader(bytes.NewReader(data), lzw.LSB, 8)
result, err := io.ReadAll(r)  // 无界读取
```

**建议**: 用 `io.LimitReader` 包装，设置最大解压大小（如 256MB）。

**H-2: `SplitIntoChunks` 不验证 chunkSize <= 0**

`chunk.go` L13: `chunkSize == 0` 导致无限循环（`i += 0` 不前进），负值导致 slice 越界 panic。

**建议**: 添加 `if chunkSize <= 0 { return nil }` 验证。

### MEDIUM

**M-1: FileStore 无 symlink 保护**

`filestore.go`: 使用 `os.WriteFile/ReadFile/Stat/Remove` 不检查路径是否为 symlink。攻击者若可在存储目录放置 symlink，可读写任意文件。key_hash 验证为 32 字节限制了 API 级利用，但文件系统级仍有风险。

**建议**: 写/读前用 `os.Lstat` 检查，或使用 `O_NOFOLLOW`。

**M-2: List 的 RLock 粒度不保证一致性**

`filestore.go` L183-230: `List` 持 RLock 期间使用两次 `os.ReadDir`，文件系统本身无事务保证。并发 `Put/Delete` 可能产生部分结果。当前设计对单用户 CLI 可接受，应文档化。

**M-3: FileStore.Put 不 fsync 文件/目录**

`filestore.go` L68-96: 原子写模式（写临时文件 → rename）正确，但无 `fsync` 时崩溃/断电可能丢数据。Rename 可能被文件系统重排到数据写之前。

**建议**: 生产环境添加 `f.Sync()` + 目录 fsync。CLI 工具可接受，但应文档化限制。

**M-4: ContentResolver.Fetch 缓存远程内容不验证**

`resolver.go` L69-70: 远程获取的数据通过 `store.Put` 缓存，未验证数据是否对应请求的 `keyHash`。恶意端点可返回任意数据。

缓解：这是内容寻址存储，值是密文非明文，store 层无法验证。解密失败由调用者检测。应文档化。

**M-5: MaxContentResponseSize 截断不可检测**

`resolver.go` L95: `io.LimitReader(resp.Body, MaxContentResponseSize)` 恰好限制到最大值，但若响应恰好等于限制，`io.ReadAll` 成功返回无截断指示。

**建议**: 用 `MaxContentResponseSize+1` 作限制，检查 `len(data) > MaxContentResponseSize`。

### LOW

**L-1: `KeyHashToPath` 导出但不验证长度**: 外部调用者可传入 1 字节 hash 产生语义错误路径。内部调用者通过 `validateKeyHash` 保护。

**L-2: `ErrStoreFull` 定义但未使用**: `errors.go` L13 — 前向声明，spec 确认。

**L-3: `Compress` 的 `CompressNone` 返回相同 slice 引用（非副本）**: `compress.go` L15-16 — 其他方案返回新 slice。不一致可能导致调用者突变输入。

**L-4: `List` 静默跳过不可读的 shard 目录**: `filestore.go` L209 — 权限问题时内容被排除，用户可能误认为数据丢失。

### SUGGESTIONS

**S-1.** `Put` 中使用 `os.CreateTemp` 替代手动 `.tmp` 后缀，避免跨进程写入冲突。

**S-2.** 超大响应测试 `TestContentResolver_OversizedResponse` 发送 1025 字节远低于 1GB 限制，应测试边界。

**S-3.** `SplitIntoChunks` 的 `copy` 复制整个输入。超大文件考虑流式方法或返回子 slice。

**S-4.** `DefaultChunkSize` 定义但包内无函数引用，考虑提供 `SplitIntoDefaultChunks` 便利包装。

## Spec Consistency

| Spec 项目 | 状态 | 备注 |
|---|---|---|
| KeyHashSize = 32 | MATCH | |
| Store 接口 (6 methods) | MATCH | |
| FileStore struct | MATCH | |
| Sharding scheme | MATCH | `{base}/{hex(keyHash[:1])}/{hex(keyHash)}` |
| NewFileStore | MATCH | |
| KeyHashToPath | MATCH | |
| Compress/Decompress | MATCH | ZSTD 返回错误（spec 确认） |
| DefaultChunkSize = 1<<20 | MATCH | |
| SplitIntoChunks / ComputeRecombinationHash / RecombineChunks | MATCH | |
| MaxContentResponseSize = 1<<30 | MATCH | |
| ContentResolver struct | MATCH | |
| NewContentResolver (30s timeout) | MATCH | |
| Fetch 优先级 (local → HTTP) | MATCH | |
| 8 个 Error sentinels | MATCH | |

模块路径偏差：spec 列出 `github.com/tongxiaofeng/libbitfs-go`，实际为 `github.com/bitfsorg/libbitfs-go`。代码正确，spec 需更新。

## Code Quality Assessment

**优点**:
1. 干净的接口设计，Store 接口最小且精确
2. 原子写（write-temp → rename）正确模式
3. 正确的 RWMutex 使用
4. 健壮的错误处理，sentinel error 完整
5. List 优雅处理非 hex 文件名、错误长度 hash、子目录
6. ContentResolver 正确的 fallback 模式
7. 90.5% 覆盖率，含并发测试和对抗性输入

**不足**:
1. 无解压缩炸弹保护（最显著）
2. SplitIntoChunks 零值无限循环
3. 无 symlink 感知
4. 响应截断不可检测

## Summary

storage 包实现良好，spec 一致性完整。所有 50 个测试通过，含 race detector。

**生产前需修复**:
- H-1 (zip bomb): 添加解压缩大小限制
- H-2 (无限循环): 验证 chunkSize <= 0（单行修复）

MEDIUM 发现值得处理但非阻塞。Spec 和实现在所有 API/类型/常量上完全一致。

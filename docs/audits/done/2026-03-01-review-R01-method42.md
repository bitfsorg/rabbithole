# Review R01: method42

> **Status**: All findings confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md` §1.1. Archived 2026-03-03.

## Overview

- **Package path**: `libbitfs-go/method42/`
- **Source files**: `access.go`, `ecdh.go`, `encrypt.go`, `errors.go`, `kdf.go`, `rabin.go`
- **Source lines**: 1,025
- **Test files**: `method42_test.go`, `coverage_supplement_test.go`, `fuzz_test.go`, `rabin_test.go`
- **Test count**: 164 (including sub-tests and fuzz seeds)
- **Coverage**: 89.3%
- **All tests pass**: Yes (including `-race`)
- **Corresponding spec**: `docs/specs/bitfs/01-method42.md`

## Findings

### CRITICAL

无。

加密核心是健全的。ECDH 委托 go-sdk 验证公钥在 secp256k1 曲线上。AES-256-GCM 使用 `crypto/rand` 生成 nonce。HKDF-SHA256 使用不同 info 字符串进行域分离。

### HIGH

**H-1. AES-GCM 未使用 Associated Data (AAD) — 潜在的跨上下文密文可替换性**

`encrypt.go` L375, L404:
```go
ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)    // L375
plaintext, err := gcm.Open(nil, nonce, encrypted, nil)  // L404
```

`nil` AAD 意味着 GCM 不绑定上下文。当前安全依赖 HKDF info 字符串的域分离（`bitfs-file-encryption` vs `bitfs-metadata-encryption` vs `bitfs-buyer-mask`），AES key 不同使跨上下文替换实际不可行。但 NIST SP 800-38D 建议在有上下文信息时使用 AAD 作为深度防御。

**建议**: 将 HKDF info 作为 AAD:
```go
gcm.Seal(nonce, nonce, plaintext, []byte(HKDFInfo))
gcm.Open(nil, nonce, encrypted, []byte(HKDFInfo))
```

**H-2. 密钥材料使用后未清零**

`DeriveAESKey`、`ECDH`、`DeriveBuyerMask`、`DeriveMetadataKey` 返回的敏感 byte slice 在使用后未被清零，留在堆内存中直到 GC 回收。

相关位置:
- `encrypt.go` L124-131: `aesKey` 使用后未清除
- `encrypt.go` L169-176: 同模式
- `encrypt.go` L251: XOR 生成的 `aesKey`
- `ecdh.go` L107-127: `ComputeCapsuleWithNonce` 中的 `aesKey` 和 `buyerMask`

Go 中显式清零不能保证对抗编译器优化/GC/swap，但可以缩小暴露窗口。

**建议**: 添加 `zeroize(b []byte)` 辅助函数并 defer 调用。

### MEDIUM

**M-1. `xorBytes` 长度不匹配时静默截断**

`ecdh.go` L131-141: 文档说 "XORs two byte slices of equal length"，但实现截断到较短长度。正常路径中两个输入都是 32 字节，但畸形 capsule 输入会产生错误的截断密钥而非返回错误。

**建议**: 验证长度匹配，不匹配时返回 error。

**M-2. `ComputeCapsuleHash` 不验证输入长度**

`ecdh.go` L152-157: `fileTxID` 和 `capsule` 均未验证。空 `fileTxID` 会使 capsule hash 退化为 `SHA256(capsule)`，丧失 txid 绑定。

**建议**: 验证 `fileTxID` 为 32 字节，`capsule` 为 `AESKeyLen` 字节。

**M-3. `ComputeCapsuleWithNonce` 不验证 `keyHash` 长度**

`ecdh.go` L100-128: `keyHash []byte` 未在入口验证为 32 字节。下游 `DeriveAESKey` 会报错，但入口验证能更快失败并给出更清晰的错误信息。

**M-4. `RabinSign` padding 搜索无上界**

`rabin.go` L51-69: 循环无限搜索二次剩余，counter 为 uint32，理论上会在 2^32 后回绕并进入无限循环。虽然概率极低（约 1/4^n），但应添加上界。

**建议**: 添加 `maxRabinPadAttempts = 1000` 限制。

**M-5. `GenerateRabinKey` 未检查 p ≠ q**

`rabin.go` L19-30: p == q 时 n = p²，Rabin 签名方案被平凡破解。对大 bitSize 概率极低，但小 bitSize 测试场景中可能发生。也无最小 bitSize 检查。

**建议**: 检查 p ≠ q 且 bitSize ≥ 512。

**M-6. `aesGCMEncrypt` 错误使用 `ErrDecryptionFailed` 哨兵**

`encrypt.go` L360-361: 加密失败时包装为 `ErrDecryptionFailed`，语义错误。

**建议**: 定义独立的 `ErrEncryptionFailed`。

### LOW

**L-1. `FreePrivateKey()` 丢弃第二个返回值**: `ecdh.go` L56 — `PrivateKeyFromBytes` 返回 `(*PrivateKey, *PublicKey)`，丢弃的是公钥非错误，但 `_` 可能误导读者。建议添加注释。

**L-2. 测试文件中重复的辅助函数**: `method42_test.go` 的 `generateKeyPair(t)` 和 `coverage_supplement_test.go` 的 `genKP(t)` 功能相同。

**L-3. `DeserializeRabinSignature` 不使用哨兵错误**: `rabin.go` L133-150 — 错误为字符串格式，调用者无法用 `errors.Is` 区分。

**L-4. `crt` 未检查 `ModInverse` 返回 nil**: `rabin.go` L103-116 — p、q 为不同素数时不可能发生，但防御性代码应检查。

### SUGGESTIONS

**S-1.** 考虑使用 `crypto/subtle.XORBytes` (Go 1.20+) 替代自定义 `xorBytes`，提供常量时间 XOR。

**S-2.** `ComputeCapsuleWithNonce` 入口处验证 `buyerPublicKey != nil`。

**S-3.** 添加 Rabin 签名 fuzz 测试（`FuzzRabinSignVerify`）。

**S-4.** 考虑定义 `type Capsule [32]byte` 类型，提供类型安全。

**S-5.** Metadata 加密 EncPayload 格式可加一个前导版本字节，支持未来格式升级。

## Spec Consistency

| Spec 项目 | 状态 |
|---|---|
| `Access` 类型 (Private=0, Free=1, Paid=2) | MATCH |
| `EncryptResult` / `DecryptResult` 结构体 | MATCH |
| `RabinKeyPair` 结构体 | MATCH |
| 所有常量 (HKDFInfo, AESKeyLen, MetadataSaltLen 等) | MATCH |
| ComputeKeyHash, ECDH, DeriveAESKey | MATCH |
| Encrypt, Decrypt, DecryptWithCapsule | MATCH |
| ComputeCapsule*, DeriveBuyerMask* | MATCH |
| DeriveMetadataKey*, EncryptMetadata, DecryptMetadata | MATCH |
| ReEncrypt, FreePrivateKey | MATCH |
| Rabin 签名全套 API | MATCH |
| 7 个 Error sentinels | MATCH |
| 密文格式: `nonce(12B) \|\| ciphertext \|\| tag(16B)` | MATCH |
| EncPayload 格式: `salt(16B) \|\| nonce(12B) \|\| ciphertext \|\| tag(16B)` | MATCH |
| HKDF 参数 (IKM=sharedX, Salt=keyHash, Info=constant) | MATCH |

**结论**: 100% spec-to-implementation 一致，无偏差，无遗漏，无多余功能。

## Code Quality Assessment

- **错误处理**: 彻底且一致。所有公共函数验证 nil key、检查长度、包装错误上下文。M-6 是唯一的语义错误。
- **命名**: 优秀。文件拆分（access/ecdh/encrypt/kdf/rabin/errors）逻辑清晰。
- **复杂度**: 无函数超过 40 行。最复杂的 `ComputeCapsuleWithNonce` 仅 28 行逻辑。
- **测试质量**: 优秀覆盖广度 — 3 种访问模式、完整加密往返、capsule 购买流程、nonce 不可链接性、元数据加密、错误条件、ReEncrypt 模式转换、5 个 fuzz target。
- **文档**: 每个导出函数有完整 godoc，包含数学公式和安全分析。

## Summary

`method42` 是一个工程质量优秀的加密核心。ECDH + HKDF + AES-256-GCM 管线正确构建，域分离充分，三模式访问模型实现清晰。100% spec 一致性。

两个 HIGH 发现（GCM 无 AAD、密钥未清零）属于深度防御改进而非可利用漏洞。MEDIUM 发现围绕输入验证（xorBytes 截断、ComputeCapsuleHash 无长度检查、Rabin p==q）可强化边界条件但不构成当前攻击向量。

测试套件 164 次运行、89.3% 覆盖率、5 个 fuzz target，全面覆盖错误路径和模式转换。唯一明显缺失是 Rabin fuzz 测试。整体达到生产安全关键代码的标准。

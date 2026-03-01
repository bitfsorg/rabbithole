# Review R04: wallet

## Overview

| Metric | Value |
|--------|-------|
| Package | `libbitfs-go/wallet` |
| Source files | 5 (`seed.go`, `hd.go`, `vault.go`, `network.go`, `errors.go`) |
| Test files | 3 (`wallet_test.go`, `coverage_supplement_test.go`, `coverage_supplement2_test.go`) |
| Source LOC | 711 |
| Test LOC | 1,612 |
| Test count | 66 + 6 benchmarks |
| Coverage | 87.7% |
| Race detector | PASS |
| Spec | `docs/specs/bitfs/04-wallet.md` |

## Findings

### CRITICAL

无。

### HIGH

**H-1: `KeyPair.PrivateKey` 缺少 `json:"-"` tag — 意外序列化泄露私钥**

`hd.go` L38-42:
```go
type KeyPair struct {
    PrivateKey *ec.PrivateKey
    PublicKey  *ec.PublicKey
    Path       string
}
```

之前审计 (L-NEW-14) 报告此问题已标记为 "FIXED"，但实际**未修复**。`PrivateKey` 字段无 `json:"-"` tag。任何 `json.Marshal(keyPair)` 调用（日志、调试端点、错误序列化、测试输出）都会序列化私钥。`KeyPair` 由所有派生函数返回并贯穿 engine/daemon/CLI 各层。

**建议**: 立即添加 tag:
```go
type KeyPair struct {
    PrivateKey *ec.PrivateKey `json:"-"`
    PublicKey  *ec.PublicKey  `json:"public_key"`
    Path       string         `json:"path"`
}
```

### MEDIUM

**M-1: `DecryptSeed` 校验和比较非常量时间**

`seed.go` L195-199: 字节逐位比较带 early return，教科书级别的时序侧信道。实际可利用性低（Argon2id 主导时间），但安全关键包应使用常量时间比较。

**建议**: 替换为 `crypto/subtle.ConstantTimeCompare`。

**M-2: `EncryptSeed` 敏感中间缓冲区未清零**

`seed.go` L95-137: `derivedKey`（32 字节 AES 密钥）、`plaintext`（seed || checksum）、`seedHash`（SHA256 of seed）使用后未清零，留在堆内存中。

**建议**: 添加 `zeroBytes` 辅助函数并 defer 调用。

**M-3: `DecryptSeed` 同样未清零**

`seed.go` L158-201: `derivedKey` 解密密钥使用后未清零。

**M-4: `Wallet.masterKey` 无清零/销毁方法**

`hd.go` L32-35: `Wallet` 持有 BIP32 master key 但无 `Close()` 或 `Destroy()` 方法。master key 在 wallet 整个生命周期内留在堆内存中。

**建议**: 添加 `Close()` 方法 nil 化 `masterKey`。

**M-5: `DeriveFeeKey` 不验证 `chain` 参数**

`hd.go` L106-125: `chain` 直接传给 `Child()` 无验证。BIP44 有效值为 0（external）和 1（internal）。值 2+ 产生有效但标准钱包无法恢复的密钥，可能导致资金不可逆锁定。

**建议**: 验证 `chain == 0 || chain == 1`。

**M-6: `EncryptSeed` 接受空密码无警告**

`seed.go` L83: 空密码被静默接受。`password=""` 时 Argon2id 产生仅依赖 salt 的确定性密钥，salt 存储在密文旁边，加密形同虚设。

**建议**: 要求最小密码长度，或在文档中显著标注风险。

### LOW

**L-1: `NewWallet` 不验证 seed 长度**: `hd.go` L45-51 只检查空 seed。BIP32 要求 16-64 字节。1 字节 seed 会在下游 `NewMaster` 失败，但错误信息不够清晰。

**L-2: `EncryptSeed` 同样不验证 seed 长度**: 会加密 1 字节 "seed"。

**L-3: `DeriveNodePubKey` 效率声明误导**: `hd.go` L204-212 文档说 "More efficient" 但实际只是 `DeriveNodeKey` 的包装。硬化派生下不可能只派生公钥。

**L-4: `NetworkConfig` 全局变量可变**: `network.go` L22-62 使用 `var` 而非不可变。外部代码可修改 `wallet.MainNet`，影响所有后续调用者。

**L-5: 错误路径覆盖率缺口**: `deriveAccount` 70%、`DeriveFeeKey` 70%、`extKeyToKeyPair` 71.4%、`GenerateMnemonic` 77.8%。主要是 go-sdk 内部错误路径，难以触发。

### SUGGESTIONS

**S-1.** 为 `KeyPair` 添加 `String()` 方法，打印时隐藏私钥。

**S-2.** 文档化 Argon2id 参数选择理由（time=3, memory=64MB, parallelism=4）。注意移动端（bitfs-app）可能需要调低 memory。

**S-3.** `CreateVault` 允许空名称 — 考虑拒绝或文档化。

## Spec Consistency

| Spec 项目 | 状态 | 备注 |
|---|---|---|
| 14 个常量 | MATCH | |
| Wallet / Vault / KeyPair / WalletState / NetworkConfig 结构体 | MATCH | KeyPair 无 json tag（H-1） |
| GenerateMnemonic / ValidateMnemonic / SeedFromMnemonic | MATCH | |
| NewWallet | MATCH | |
| EncryptSeed / DecryptSeed | MATCH | 格式 salt(16)\|\|nonce(12)\|\|ciphertext 正确 |
| DeriveNodeKey | MATCH | 默认硬化行为正确 |
| DeriveNodePubKey | **DEVIATION** | 文档称 "More efficient" 但实为包装 |
| DeriveFeeKey / DeriveVaultRootKey | MATCH | 路径正确 |
| Network() / NewWalletState() / Validate() | MATCH | |
| CreateVault / GetVault / ListVaults / RenameVault / DeleteVault | MATCH | |
| 预定义网络 (4) | MATCH | |
| GetNetwork / LoadCustomNetwork | MATCH | |
| 11 个 Error sentinels | MATCH | |
| HD 密钥树布局 | MATCH | Fee account=0, vaults 从 1+ |
| wallet.enc 格式 | MATCH | |
| 5 个安全考虑 | 部分满足 | 项目 1,3,5 满足；项目 2（助记词清零）无库支持；项目 4（密钥缓存加密）应用层 |

**结论**: 2 个偏差（均为文档/tag 级别），整体 spec 一致性良好。

## Previous Audit Follow-up

| 审计发现 | 声称状态 | 实际状态 |
|----------|---------|---------|
| M-NEW-10: BIP32 account index 整数溢出 | FIXED | **确认已修复** — `hd.go:154` 和 `vault.go:42,69` 有边界检查 |
| M-NEW-12: WalletState 未反序列化验证 | FIXED | **确认已修复** — `Validate()` 方法存在于 `vault.go:33-62` |
| L-NEW-14: KeyPair.PrivateKey 缺 `json:"-"` | FIXED | **未修复** — 字段无 json tag |

## Code Quality Assessment

**优点**:
1. 职责清晰分离（seed/hd/vault/network/errors 五个文件）
2. 生产代码无 `log` 或 `fmt.Print` — 无意外记录密钥风险
3. 一致使用 sentinel error + `errors.Is()`
4. `go vet` 干净，`-race` 通过
5. 熵源自 `crypto/rand.Read`（CSPRNG），无 `math/rand`
6. Argon2id 参数符合 OWASP 建议（time≥3, memory≥46MB）
7. AES-GCM nonce 12 字节来自 CSPRNG
8. vault account index 溢出保护已实现

**不足**:
1. L-NEW-14 修复缺失（json tag）
2. 系统性缺乏敏感内存清零
3. DeriveNodePubKey 效率声明误导

## Summary

wallet 包是一个扎实的 BIP32/BIP39 HD 钱包实现。加密基础健全：`crypto/rand` 熵源、BIP39 PBKDF2 种子派生、BIP32 HMAC-SHA512 主密钥生成、Argon2id 密码哈希、AES-256-GCM 认证加密均正确委托给可信库。

最紧急的是 **H-1**：`KeyPair.PrivateKey` 缺少 `json:"-"` tag，之前审计报告为已修复但实际缺失。这是单行修复，高价值防护。其次是 M-1（常量时间比较）和 M-2/M-3/M-4（内存清零），属于深度防御改进。M-5（chain 参数验证）和 M-6（空密码）防止调用者误用。

无发现构成正常使用下的可利用漏洞。

# libbitfs-ts 设计文档

**日期**: 2026-03-02
**状态**: Approved
**目标**: 实现 libbitfs-go 的 TypeScript 镜像，browser + Node.js 双平台

## 概述

libbitfs-ts 是 libbitfs-go 的 TypeScript 移植，提供 BitFS 协议的完整客户端能力。11 个模块与 Go 版 1:1 对应，Wire format 100% 兼容。

## 架构

### 单包多模块

一个 npm 包 `@bitfs/libbitfs`，内部 11 个模块通过 `package.json` exports map 提供子路径导入：

```
libbitfs-ts/
├── src/
│   ├── method42/    # ECDH 加密引擎 (AES-256-GCM, 3 种访问模式)
│   ├── wallet/      # HD 钱包 (BIP44 m/44'/236'/...)
│   ├── metanet/     # Metanet DAG + Unix FS + TLV 序列化 (49 tags)
│   ├── tx/          # MutationBatch 原子交易构建
│   ├── spv/         # SPV 轻客户端 (区块头 + Merkle proof)
│   ├── storage/     # 内容寻址存储 + 压缩 + 分块
│   ├── network/     # BlockchainService 接口 (RPC/SPV/Mock)
│   ├── x402/        # HTTP 402 支付协议 (HTLC)
│   ├── paymail/     # Paymail 发现 + PKI
│   ├── revshare/    # 收入分成注册表 + 分配算法
│   └── config/      # 配置解析 (key=value)
├── package.json
├── tsconfig.json
└── vitest.config.ts
```

导入: `import { encrypt, decrypt } from '@bitfs/libbitfs/method42'`

### 依赖

| 需求 | 方案 | 大小 |
|------|------|------|
| BSV 原语 | `@bsv/sdk` v2 (唯一重依赖) | ~300KB |
| HKDF-SHA256 | `@noble/hashes` (audited) | ~30KB |
| Argon2id | `hash-wasm` (WASM) | ~50KB |
| LZW/GZIP | 平台 API (CompressionStream / zlib) | 0 |
| Rabin 签名 | 自行实现 (BigInt) | ~200 行 |
| DNS (Paymail) | Node: dns/promises; Browser: HTTP fallback | 0 |

总计 3 个运行时依赖。

### 构建工具链

- **Runtime**: Bun
- **Test**: Vitest
- **Build**: `bun build` (ESM output)
- **Types**: `tsc --emitDeclarationOnly`
- **Target**: ESNext, browser + Node.js

## @bsv/sdk 映射

| Go (go-sdk) | TypeScript (@bsv/sdk) |
|---|---|
| `ec.PrivateKey` | `PrivateKey` (extends BigNumber) |
| `ec.PublicKey` | `PublicKey` (extends Point) |
| `compat.HDKey` / BIP32 | `HD` class |
| `primitives.NewTx()` | `new Transaction()` |
| `p2pkh.Lock/Unlock` | `P2PKH().lock/unlock` |
| `sha256.Sum256` | `Hash.sha256()` |
| SHA256d (double hash) | `Hash.hash256()` |
| ECDH(priv, pub) | `priv.deriveSharedSecret(pub)` |

**AES-GCM 注意**: @bsv/sdk 的 SymmetricKey 用非标准 32B IV。Method42 需要 12B nonce AES-256-GCM，须自行封装 `crypto.subtle`（浏览器）/ Node `crypto`。

## 模块依赖图

```
config (standalone)
  ↑
method42 (standalone crypto) ← @bsv/sdk, @noble/hashes
  ↑
wallet ← method42, @bsv/sdk HD
  ↑
storage ← method42 (加密), 压缩
  ↑
metanet ← method42 (TLV 中加密字段), wallet (路径)
  ↑
tx ← metanet (payload), @bsv/sdk Transaction
  ↑
network ← tx (broadcast), spv (验证)
  ↑
spv ← @bsv/sdk Hash
  ↑
x402 ← method42 (capsule), network
  ↑
paymail ← DNS/HTTP
  ↑
revshare (standalone binary S/D)
```

## 实现分期

### Phase 1: 加密核心 (method42 + wallet + config)

**method42/**:
- `ecdh()` — ECDH 共享密钥 (x 坐标)
- `computeKeyHash()` — SHA256(SHA256(plaintext))
- `deriveAESKey()` — HKDF-SHA256(sharedSecret, keyHash)
- `encrypt/decrypt()` — AES-256-GCM (12B nonce)
- `reEncrypt()` — 访问模式转换
- `computeCapsule/decryptWithCapsule()` — HTLC 原子交换
- `encryptMetadata/decryptMetadata()` — PRIVATE 元数据加密
- `generateRabinKey/rabinSign/rabinVerify()` — Rabin 签名
- 常量: NonceLen=12, GCMTagLen=16, AESKeyLen=32 等

**wallet/**:
- `Wallet` class — 从 BIP39 seed 创建
- `deriveFeeKey()` — m/44'/236'/0'/chain/index
- `deriveVaultRootKey()` — m/44'/236'/(vault+1)'/0/0
- `deriveNodeKey()` — 文件路径派生
- `generateSeed/seedFromMnemonic()` — BIP39
- `encryptSeed/decryptSeed()` — Argon2id

**config/**:
- `loadConfig/saveConfig()` — key=value 文件
- `defaultConfig()` — 默认值
- `validateConfig()`

### Phase 2: 数据层 (metanet + storage)

**metanet/**:
- `Node` type — ~50 个字段
- `ChildEntry`, `ISOConfig` types
- TLV 序列化/反序列化 (49 tags)
- `serializeNode/parseNode()`
- 目录操作: mkDir, put, rm, copy, move, link
- `computeMerkleRoot()` — 目录 Merkle 树
- `verifyChildMembership()` — 成员证明
- `checkCLTVAccess()` — 时间锁
- 常量: NodeType, OpType, LinkType, AccessLevel 等枚举

**storage/**:
- `Store` interface — put/get/has/delete/size/list
- `FileStore` — Node.js hash-sharded 目录
- `MemoryStore` — 测试用
- `compress/decompress()` — LZW, GZIP
- `splitIntoChunks/recombineChunks()` — 1MB 默认
- `computeRecombinationHash()`

### Phase 3: 区块链 (tx + spv + network)

**tx/**:
- `MutationBatch` — 原子多操作交易
- `BatchNodeOp` (OpCreate/OpUpdate/OpDelete/OpCreateRoot)
- `build()` — 构建签名交易
- `estimateTxSize/estimateFee()`

**spv/**:
- `BlockHeader` — 80 字节区块头
- `MerkleProof` — Merkle 分支
- `verifyTransaction()` — 完整 SPV 验证
- `verifyMerkleProof()` — Merkle path 验证
- `verifyPoW()` — 工作量证明

**network/**:
- `BlockchainService` interface
- `RPCClient` — JSON-RPC over HTTP (fetch)
- `SPVClient` — 轻客户端
- `MockBlockchainService` — 测试
- 网络预设: MainNet, TestNet, RegTest

### Phase 4: 协议层 (x402 + paymail + revshare)

**x402/**:
- `Invoice` type
- `calculatePrice()` — ceil(pricePerKB * fileSize / 1024)
- `newInvoice()` — 创建发票
- `parseX402Headers()` — HTTP 响应头解析
- `verifyPaymentProof()` — 支付验证

**paymail/**:
- `parsePaymailURI()` — "user@domain" 解析
- `resolvePaymail()` — .well-known/bsvalias → PKI
- HTTP fallback for browser (无 DNS)

**revshare/**:
- `RevShareEntry`, `RegistryState`, `ShareData`, `ISOPoolState`
- `distributeRevenue()` — 按比例分配 (remainder-to-last)
- `serializeRegistry/deserializeRegistry()` — 固定大小二进制
- `validateRegistry()` — share 守恒验证

## 关键设计决策

### 1. Wire Format 100% 兼容

TLV 编码、字节序 (big-endian)、哈希算法必须与 Go 版产出完全相同的字节。跨语言兼容通过共享 JSON test vectors 验证。

### 2. AES-GCM 自行封装

不使用 @bsv/sdk SymmetricKey（32B IV 不兼容）。用 `crypto.subtle`（浏览器）/ `crypto.createCipheriv`（Node）实现 12B nonce AES-256-GCM，确保与 Go 版 format 一致：`nonce(12B) || ciphertext || tag(16B)`。

### 3. Rabin 签名用原生 BigInt

1024+ bit 大数运算用 JS 原生 `BigInt`，无需额外依赖。从 Go 版 rabin.go 直接移植，~200 行。

### 4. 平台抽象

```typescript
// 统一 crypto 入口
const subtle = globalThis.crypto?.subtle ?? (await import('crypto')).webcrypto.subtle
```

压缩同理：浏览器用 `CompressionStream`，Node 用 `zlib`。FileStore 仅 Node.js（浏览器可用 IndexedDB 自行实现 Store interface）。

### 5. Export 风格

每个模块 `index.ts` re-export 所有公共 API。类型用 `export type`。

```typescript
// src/method42/index.ts
export { encrypt, decrypt, reEncrypt } from './encrypt.js'
export { computeCapsule, decryptWithCapsule } from './capsule.js'
export type { EncryptResult, DecryptResult } from './types.js'
```

### 6. Error 模式

```typescript
export class BitfsError extends Error {
  constructor(message: string, public code: string) { super(message) }
}
export class Method42Error extends BitfsError { /* ... */ }
export class WalletError extends BitfsError { /* ... */ }
```

## 测试策略

- Vitest, 目标覆盖率 >80%
- 每个模块 `__tests__/` 目录
- **跨语言 test vectors**: method42 加密结果、wallet 派生路径、metanet TLV 编码 — 生成 JSON fixtures，Go 和 TS 共用验证
- 模拟依赖用 Vitest mock

## 输出物

- npm 包: `@bitfs/libbitfs` (ESM)
- TypeScript 声明: `.d.ts`
- 浏览器 + Node.js 双平台

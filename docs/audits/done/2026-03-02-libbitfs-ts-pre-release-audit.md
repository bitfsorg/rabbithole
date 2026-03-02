# libbitfs-ts Pre-Release Audit Report

**Date**: 2026-03-02
**Scope**: Full-spectrum audit of `@bitfs/libbitfs` v0.1.0 (11 modules, 21,944 LoC, 875 tests)
**Auditor**: Claude Opus 4.6 (5 parallel agents)
**Status**: All 37 findings (3C + 6H + 14M + 14L) confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md` §七. S-02 HTLC CLTV deferred to `deferred.md`. Archived 2026-03-03.

## Executive Summary

libbitfs-ts is a well-implemented TypeScript mirror of libbitfs-go with strong crypto fundamentals and comprehensive test coverage. However, **it is NOT ready for npm publish** in its current state. The audit identified **3 CRITICAL**, **6 HIGH**, **14 MEDIUM**, and **14 LOW** issues across 5 dimensions.

**Must-fix before release** (CRITICAL + HIGH):
1. Build pipeline produces a broken 878KB bundled dist/index.js
2. HTLC refund flow completely missing (buyers cannot recover funds)
3. Argon2id parameters below specification (3x weaker than designed)
4. `node:zlib` static import breaks browser environments
5. No HTTP timeouts on any fetch() call
6. Package exports lack `types` conditions (breaks modern TS consumers)
7. No `files` field (tests/source would be published to npm)
8. x402 HTLC script missing CLTV timelock enforcement
9. Cross-language test vectors insufficient for wire-format guarantee

---

## Severity Summary

| Severity | Count | Category Breakdown |
|----------|-------|--------------------|
| CRITICAL | 3 | Build (1), API Parity (1), Browser (1) |
| HIGH | 6 | Security (2), Build (2), Code Quality (2) |
| MEDIUM | 14 | Security (4), Build (3), Code Quality (5), API (1), Test (1) |
| LOW | 14 | Security (4), Code Quality (7), Browser (1), API (2) |

---

## 1. Security Findings

### S-01 [HIGH] Argon2id Parameters Below Specification

**File**: `src/wallet/seed.ts:33-39`

Current: `t=3, m=65536 (64MB), p=4`. Specified: `t=10, m=262144 (256MB), p=1`.

The wallet seed encryption is 3.3x weaker on time-cost and 4x weaker on memory-cost than the design specification and the Go implementation. For a cryptocurrency wallet seed, stronger parameters are warranted.

**Fix**: Update constants to `ARGON2_TIME = 10`, `ARGON2_MEMORY = 262144`, `ARGON2_PARALLELISM = 1`. If browser performance is a concern, document the trade-off and allow parameter override.

### S-02 [HIGH] HTLC Script Missing CLTV Timelock

**File**: `src/x402/htlc.ts:125-132`

The `timeoutBlocks` parameter is validated but **never encoded into the Bitcoin Script**. The refund path uses 2-of-2 multisig (`OP_2 <buyer> <seller> OP_2 OP_CHECKMULTISIG`) without `OP_CHECKLOCKTIMEVERIFY`. This means:

- Buyer cannot unilaterally reclaim funds after timeout
- If seller goes offline, funds are permanently locked
- This is a fundamentally different security model from a standard HTLC

**Fix**: Either add CLTV to the script's OP_ELSE branch, or implement the pre-signed refund transaction flow (see S-06).

### S-03 [MEDIUM] Non-Constant-Time Byte Comparison (Systemic)

**Files**: 14 duplicate `uint8ArrayEqual`/`bytesEqual` implementations across 12+ files

All use short-circuit comparison (`if (a[i] !== b[i]) return false`). Used in cryptographic contexts: keyHash verification after decryption, capsule hash comparison, Merkle root comparison.

While timing attacks on post-authentication hash comparisons are low risk, this is a systemic pattern that should be hardened uniformly.

**Fix**: Create `src/util.ts` with a constant-time `timingSafeEqual()` and use everywhere:
```typescript
export function timingSafeEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false
  let diff = 0
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i]
  return diff === 0
}
```

### S-04 [MEDIUM] No Key Material Zeroing

**Files**: `src/method42/encrypt.ts`, `src/method42/capsule.ts`, `src/wallet/seed.ts`, `src/method42/ecdh.ts`

No cryptographic key material is zeroed after use. Derived AES keys, Argon2id output, decrypted seeds, ECDH shared secrets all remain in memory until GC. While JS cannot guarantee full memory erasure, `.fill(0)` significantly reduces the window of exposure.

**Fix**: Add `.fill(0)` in `finally` blocks for all intermediate key material, especially in `seed.ts` (derivedKey, plaintext) and `encrypt.ts` (aesKey).

### S-05 [MEDIUM] SPV verifyPoW Trusts Cached Hash

**File**: `src/spv/header.ts:174-176`

`verifyPoW` uses the pre-supplied `h.hash` field without recomputing it from header bytes. A compromised `HeaderStore` returning a fraudulent all-zero hash would pass PoW verification. In practice, headers enter through `deserializeHeader()` which computes the hash, but defense-in-depth warrants always recomputing.

**Fix**: Unconditionally compute `hash = computeHeaderHash(h)` in `verifyPoW`.

### S-06 [MEDIUM] parseHTLCPreimage Edge Case with Missing fileTxID

**File**: `src/x402/verify.ts:167-170`

When `expectedCapsuleHash` is provided but `fileTxID` is not, `computeCapsuleHash` receives 0-byte input and returns `null`. The subsequent null check silently skips verification instead of failing explicitly.

**Fix**: Require `fileTxID` when `expectedCapsuleHash` is provided.

### S-07 [LOW] ECDH No Point-at-Infinity Check

**File**: `src/method42/ecdh.ts:16-21`

If ECDH produces the point at infinity (astronomically unlikely), `sharedX` would be all-zeros. HKDF would still produce a "valid" key. Add a zero-check as defense-in-depth.

### S-08 [LOW] Seed Checksum Non-Constant-Time

**File**: `src/wallet/seed.ts:215-219`

The 4-byte checksum comparison uses early-return. Low risk since it follows GCM authentication, but should use constant-time comparison for consistency.

### S-09 [LOW] Hash.sha256(Array.from(plaintext)) Memory Copy

**File**: `src/method42/kdf.ts:33`

`Array.from()` creates a redundant copy of the entire plaintext before hashing. Not a security issue but inefficient for large files.

### S-10 [LOW] Rabin CRT Formula Unusual Form

**File**: `src/method42/rabin.ts:147-153`

Mathematically correct but written in unusual operator-precedence-dependent form. Not a bug.

---

## 2. API Parity Findings

### A-01 [CRITICAL] x402 Refund Flow Completely Missing

**Files**: `src/x402/`

Missing from TS that exists in Go:
- `BuildSellerPreSignedRefund()` + `SellerPreSignParams` + `SellerPreSignResult`
- `BuildBuyerRefundTx()` + `BuyerRefundParams`
- `VerifyHTLCFunding()`

Without these, buyers using the TS library for paid content have **no way to recover funds** if the seller disappears. Combined with S-02 (missing CLTV), the entire x402 payment safety mechanism is broken.

### A-02 [MEDIUM] tx.ParseTxNodeOps Missing

**File**: `src/tx/`

Missing: `BuildDataTransaction`, `ParseTxNodeOps`, `ParsedNodeOp`, `TxOutput`. Any TS-based indexer, explorer, or protocol parser cannot extract Metanet operations from raw transactions.

### A-03 [LOW] Paymail DNS Resolution Missing

**File**: `src/paymail/`

Missing: `DNSResolver`, `ResolveEndpoints`, `ResolveDNSLinkPubKey`, `DNSSECResolver`, `ResolveURI`. This is a platform limitation (no DNS API in browsers) but means `bitfs://domain/path` URIs cannot be resolved in TS.

### A-04 [LOW] Minor Missing Functions

- `wallet.DeriveNodePubKey()` — workaround: `deriveNodeKey()` then extract pubkey
- `metanet.ParseNode()` / `ParseNodeFromPushesWithTxID()` — composable from existing functions
- `revshare.ValidateDistribution()` — verification helper
- `spv.BoltStore` — platform-specific persistence (expected)

### API Parity Matrix

| Package | Status | Missing (significance) |
|---------|--------|----------------------|
| method42 | Full | — |
| wallet | Full | DeriveNodePubKey (low) |
| config | Full | — |
| metanet | Full | ParseNode thin wrappers (low) |
| storage | Full | + MemoryStore addition |
| tx | Partial | BuildDataTransaction, ParseTxNodeOps (medium) |
| spv | Full | BoltStore (platform, expected) |
| network | Full | ParseBIP37MerkleBlock (low) |
| x402 | **Partial** | **Refund flow (CRITICAL)** |
| paymail | Partial | DNS layer (platform limitation) |
| revshare | Full | ValidateDistribution (low) |

### Cross-Language Test Vector Gaps

Only 7 vectors in `go-vectors.json`. Missing coverage:
- Full Node TLV round-trip serialization
- AES-GCM encrypt/decrypt round-trip (Go-encrypted → TS-decrypted)
- HTLC script construction (byte-identical)
- Merkle root computation
- Revenue distribution
- LZW/GZIP compression round-trip
- Capsule computation
- Block header serialization

---

## 3. Package/Build Findings

### B-01 [CRITICAL] Build Pipeline Produces Broken Bundle

**File**: `package.json` scripts

`"build": "bun build ./src/index.ts --outdir ./dist --target node"` produces an 878KB flat bundle at `dist/index.js` that **inlines all dependencies** (@bsv/sdk, @noble/hashes). This:
- Causes massive duplication for consumers also using @bsv/sdk
- Overwrites the correct tsc-generated `dist/index.js` (which just re-exports sub-modules)
- Is the wrong output for a library (should be individual modules)

**Fix**: Replace build script with `"build": "tsc"`. Remove the bun build step entirely.

### B-02 [HIGH] Exports Missing `types` Conditions

**File**: `package.json` exports map

Sub-path exports use string shorthand (`"./method42": "./dist/method42/index.js"`) without `"types"` conditions. Consumers using `moduleResolution: "node16"` or `"nodenext"` cannot resolve type declarations.

**Fix**:
```json
"./method42": {
  "types": "./dist/method42/index.d.ts",
  "import": "./dist/method42/index.js"
}
```

### B-03 [HIGH] No files Field — Tests Would Be Published

**File**: `package.json`

No `"files"` field and no `.npmignore`. An `npm publish` would include: `src/` (868KB with all test files), `vitest.config.ts`, `bun.lock`, bringing total published size to ~3.7MB (should be ~600KB).

**Fix**: Add `"files": ["dist", "LICENSE", "README.md"]`.

### B-04 [MEDIUM] Unused `hash-wasm` Dependency

**File**: `package.json`

`hash-wasm` v4.12.0 is listed as a runtime dependency but **never imported** anywhere in `src/`. Dead weight. Argon2id actually uses `@noble/hashes/argon2`.

**Fix**: Remove from dependencies.

### B-05 [MEDIUM] @bsv/sdk Should Be Peer Dependency

Consumers will likely also depend on `@bsv/sdk` directly. Having it as a regular dependency risks version duplication and increased bundle size.

**Fix**: Move to `peerDependencies` with the same version range. Add `peerDependenciesMeta` to mark as optional for library-only consumers.

### B-06 [MEDIUM] README Stale

**File**: `README.md`

Says "Not yet implemented. Planned for a future release." and mentions "CommonJS compatibility shim" (package is ESM-only). No install instructions, usage examples, or browser/Node compatibility docs.

**Fix**: Rewrite README before publish.

### B-07 [LOW] declarationMap Not Enabled

**File**: `tsconfig.json`

No `.d.ts.map` files generated. Consumers cannot "Go to Source" through type declarations in their IDE.

**Fix**: Add `"declarationMap": true`.

### B-08 [LOW] Missing sideEffects Declaration

**File**: `package.json`

No `"sideEffects": false`. Bundlers cannot perform optimal tree-shaking.

### B-09 [LOW] Missing Package Metadata

No `repository`, `description`, `keywords`, `engines`, `author`, `homepage`, `bugs` fields.

---

## 4. Code Quality Findings

### Q-01 [HIGH] node:zlib Breaks Browser Environments

**File**: `src/storage/compress.ts:1`

`import { gzipSync, gunzipSync } from 'node:zlib'` is a top-level static import. Any browser bundler importing the `storage` module will fail. LZW is already pure JS; only GZIP is Node-dependent.

**Fix**: Use `CompressionStream`/`DecompressionStream` (Web API, Node 18+) or `fflate`/`pako`.

### Q-02 [HIGH] No HTTP Timeouts on fetch() Calls

**Files**: `src/network/rpc.ts:382`, `src/storage/resolver.ts:83`, `src/paymail/discover.ts`

All fetch() calls lack timeout. A slow server causes indefinite hangs. This was previously fixed in libbitfs-go.

**Fix**: Add `signal: AbortSignal.timeout(30_000)` to all fetch calls. Accept configurable timeout in RPCConfig.

### Q-03 [MEDIUM] Singleton Error Objects Have Wrong Stack Traces

**Files**: All `errors.ts` files (method42, metanet, spv, storage, x402, revshare)

Errors like `ErrNotFound` are singleton `const` objects. The `stack` property is captured at module load time, not at the throw site. Every throw shows the same useless stack trace. The wallet module already uses the correct pattern (individual error classes).

**Fix**: Either use factory functions `export const ErrNotFound = () => new StorageError(...)` or adopt the wallet module's per-error class pattern.

### Q-04 [MEDIUM] Utility Functions Duplicated 14+ Times

`uint8ArrayEqual`/`bytesEqual` duplicated 14 times across 12+ files. `toHex`/`hexToBytes` duplicated ~8 times.

**Fix**: Extract to `src/util.ts`.

### Q-05 [MEDIUM] Unbounded Response Body Read in Paymail

**File**: `src/paymail/discover.ts:101-126`

`resp.text()` reads the full response into memory before checking `MAX_PAYMAIL_RESPONSE_SIZE`. A malicious server can send unbounded data.

**Fix**: Check `Content-Length` before reading, or use streaming body reader with byte limit.

### Q-06 [MEDIUM] `as unknown as number[]` Unsafe Double Cast

**File**: `src/x402/htlc.ts:445`

Bypasses TypeScript type system entirely. Replace with `Array.from()`.

### Q-07 [MEDIUM] rpc.ts `call<T>` Returns Unvalidated JSON

**File**: `src/network/rpc.ts:416`

`return rpcResp.result as T` blindly trusts the RPC server. A malformed response propagates as a correctly-typed value.

### Q-08 [MEDIUM] Cross-Language Tests Insufficient

**File**: `src/__tests__/crosslang.test.ts`

Only 7 test vectors. Missing: TLV round-trip, encrypt/decrypt, HTLC script, Merkle root, revenue distribution, compression. Insufficient for guaranteeing wire-format compatibility between Go and TS implementations.

### Q-09 [LOW] config Top-Level os/path Imports Break Browser Bundles

**File**: `src/config/config.ts:8-9`

`import { homedir } from 'os'` and `import { join } from 'path'` are top-level. Even if `loadConfig()`/`saveConfig()` are never called, the import breaks browser bundlers.

**Fix**: Move to dynamic imports inside the functions that use them.

### Q-10 [LOW] const enum Incompatible with isolatedModules

**Files**: All enum definitions

`const enum` is erased at compile time. Consumers using Vite/esbuild/SWC default (`isolatedModules: true`) cannot use them. No runtime validation at deserialization boundaries.

**Fix**: Use regular `enum` or union types for a library.

### Q-11 [LOW] MemHeaderStore.putHeader Mutates Input

**File**: `src/spv/store.ts:40-64`

Computes and assigns `header.hash` in-place, mutating the caller's object.

### Q-12 [LOW] aesGCMDecryptInternal Duplicated in capsule.ts

**File**: `src/method42/capsule.ts:183-199`

Identical to `encrypt.ts` version. Extract to shared `method42/aes.ts`.

### Q-13 [LOW] Error Code Naming Inconsistency

Some modules use `ERR_` prefix, others use module prefix (`WALLET_`, `TX_`). Inconsistent.

### Q-14 [LOW] verifyTransaction/verifyTransactionWithNetwork ~90% Code Duplication

**File**: `src/spv/verify.ts:20-130`

### Q-15 [LOW] NaN chunkSize Causes Infinite Loop

**File**: `src/storage/chunk.ts:11-14`

`NaN <= 0` is `false`, so NaN passes the guard. `i += NaN` never advances.

**Fix**: Add `Number.isFinite(chunkSize)` check.

---

## 5. Browser Compatibility Matrix

| Module | Browser | Issue |
|--------|---------|-------|
| method42 | Pass | — |
| wallet | Pass | Argon2id slow (~seconds, pure JS 64MB) |
| config | **FAIL** | Top-level `os`, `path` imports |
| metanet | Pass | — |
| storage | **FAIL** | `node:zlib` in compress.ts |
| tx | Pass | — |
| spv | Pass | — |
| network | Pass | Uses global fetch + btoa |
| x402 | Pass | — |
| paymail | Pass | Uses global fetch |
| revshare | Pass | — |

**Score**: 9/11 modules browser-safe. 2 modules have Node.js static imports that break browser bundles.

---

## Prioritized Action Plan

### P0 — Must Fix Before Release (blocks publish)

| # | Issue | Effort |
|---|-------|--------|
| 1 | B-01: Replace bun build with tsc | 10 min |
| 2 | B-03: Add `"files"` field | 2 min |
| 3 | B-02: Add `types` conditions to exports | 15 min |
| 4 | B-06: Update README | 30 min |
| 5 | B-04: Remove unused hash-wasm | 2 min |

### P1 — Should Fix Before Release (safety/correctness)

| # | Issue | Effort |
|---|-------|--------|
| 6 | S-01: Argon2id params to match spec | 5 min |
| 7 | Q-01: Replace node:zlib with browser-compatible GZIP | 1 hr |
| 8 | Q-09: Move config os/path to dynamic imports | 15 min |
| 9 | Q-02: Add HTTP timeouts to all fetch calls | 30 min |
| 10 | S-03 + Q-04: Extract util.ts (timingSafeEqual, toHex, hexToBytes) | 1 hr |
| 11 | S-04: Add key material zeroing | 30 min |
| 12 | Q-03: Fix singleton error objects | 1 hr |
| 13 | Q-08: Expand cross-language test vectors | 2 hr |

### P2 — Should Fix Soon (API completeness)

| # | Issue | Effort |
|---|-------|--------|
| 14 | A-01 + S-02: Implement x402 refund flow + CLTV | 4 hr |
| 15 | A-02: Implement tx.ParseTxNodeOps | 2 hr |
| 16 | Q-05: Bounded response body reads | 30 min |
| 17 | Q-10: Replace const enum with regular enum | 30 min |

### P3 — Nice to Have

| # | Issue | Effort |
|---|-------|--------|
| 18 | B-05: Move @bsv/sdk to peerDependencies | 5 min |
| 19 | B-07: Enable declarationMap | 2 min |
| 20 | B-08: Add sideEffects: false | 2 min |
| 21 | B-09: Add package metadata fields | 5 min |
| 22 | Q-06: Fix unsafe double cast in htlc.ts | 5 min |
| 23 | Various LOW findings | 2 hr |

---

## Positive Observations

- **Zero `any` types**, zero `@ts-ignore`, strict mode throughout
- **875 passing tests** with comprehensive edge case coverage
- **Cross-language test vectors** validate core crypto compatibility with Go
- **WebCrypto API** used correctly via clean `subtle.ts` abstraction
- **SPV verification pipeline** is thorough and complete (PoW + chain + Merkle)
- **Capsule hash binding** to fileTxID prevents cross-file replay (P0 fix from Go audit)
- **Error handling** consistently prevents partial plaintext leakage on decryption failure
- **TLV parser** has thorough bounds checking with `validateTLVFieldLength()`
- **Input validation** is comprehensive at public API boundaries
- **Clean module architecture** with no circular dependencies
- **@noble/hashes** and WebCrypto are excellent crypto foundation choices

---

## Conclusion

The core cryptographic and protocol implementation is solid. The main blockers are **build/packaging issues** (trivially fixable) and the **missing x402 refund flow** (significant but scoped). With P0 fixes (~1 hour), the library can be published for early adopters. With P1 fixes (~7 hours), it meets production-grade standards. The x402 refund flow (P2) should be prioritized for any application handling real payments.

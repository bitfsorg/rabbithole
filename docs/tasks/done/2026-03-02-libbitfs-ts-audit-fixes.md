# libbitfs-ts Audit Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the 12 remaining findings from the libbitfs-ts pre-release audit (`docs/audits/2026-03-02-libbitfs-ts-pre-release-audit.md`).

**Architecture:** Work directly in `libbitfs-ts/` on main. All changes are isolated to the TS library. Tasks ordered small→large for momentum. Each task includes tests and a commit.

**Tech Stack:** TypeScript 5.7, @bsv/sdk ^2.0.5, @noble/hashes, vitest

**Test command:** `cd libbitfs-ts && npx vitest run`

**Status:** 10/12 tasks completed. Tasks 11-12 (ParseTxNodeOps, x402 refund flow) pending.

---

## ~~Task 1: Q-06 — Fix unsafe type cast in htlc.ts~~ ✅

**Files:**
- Modify: `libbitfs-ts/src/x402/htlc.ts:105-106,244,445-446`
- Modify: `libbitfs-ts/src/tx/opreturn.ts:63`

**Context:** `sig.toDER() as number[]` and `pNode.toDER() as number[]` are redundant casts. In @bsv/sdk, `toDER()` already returns `number[]`, but `encode(true)` returns `string | number[]`. Use `Array.from()` for explicit, safe conversion.

**Step 1: Fix htlc.ts casts**

Replace `Array.from(params.invoiceID)` pattern is already correct. Focus on:
- Line 105: `s.writeBin(Array.from(params.invoiceID))` — already fine
- Line 244: `const buyerPubKey = params.buyerPrivKey.toPublicKey().encode(true) as number[]` → `const buyerPubKey = params.buyerPrivKey.toPublicKey().toDER()`
- Line 445: `const sigBytes: number[] = [...(sig.toDER() as number[]), scope & 0xff]` → `const sigBytes: number[] = [...sig.toDER(), scope & 0xff]`
- Line 446: `const sellerPubKey = params.sellerPrivKey.toPublicKey().encode(true) as number[]` → `const sellerPubKey = params.sellerPrivKey.toPublicKey().toDER()`

**Step 2: Fix opreturn.ts cast**

Line 63: `const pNodeBytes = Uint8Array.from(pNode.toDER() as number[])` → `const pNodeBytes = Uint8Array.from(pNode.toDER())`

**Step 3: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/x402 src/tx`

**Step 4: Commit**

```
fix(libbitfs-ts): remove redundant type casts in htlc.ts and opreturn.ts [Q-06]
```

---

## ~~Task 2: Q-11 — Defensive copy in putHeader~~ ✅

**Files:**
- Modify: `libbitfs-ts/src/spv/store.ts:40-44`
- Test: `libbitfs-ts/src/spv/__tests__/store.test.ts` (add test)

**Context:** `MemHeaderStore.putHeader` mutates the caller's `header.hash` field in-place when hash is empty. Should deep-copy first.

**Step 1: Add test for mutation**

```typescript
it('putHeader does not mutate input header', async () => {
  const store = new MemHeaderStore()
  const header = { ...testHeader, hash: new Uint8Array(0) }
  const original = { ...header }
  await store.putHeader(header)
  // header should still have empty hash (store computed internally)
  expect(header.hash.length).toBe(0)
})
```

**Step 2: Run test — verify it fails**

**Step 3: Fix store.ts — deep copy before mutation**

Replace lines 40-44:
```typescript
async putHeader(header: BlockHeader): Promise<void> {
  // Deep copy to avoid mutating caller's object
  const h = copyBlockHeader(header)
  if (!h.hash || h.hash.length === 0) {
    h.hash = computeHeaderHash(h)
  }
```

Then use `h` instead of `header` for the rest of the method.

**Step 4: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/spv`

**Step 5: Commit**

```
fix(libbitfs-ts): defensive copy in putHeader to prevent input mutation [Q-11]
```

---

## ~~Task 3: S-04 — Key material zeroing in seed.ts~~ ✅

**Files:**
- Modify: `libbitfs-ts/src/wallet/seed.ts:120-158,173-225`

**Context:** `encryptSeed` and `decryptSeed` don't zero `derivedKey` or `plaintext` (which contains seed material) after use. The method42 module already does this correctly.

**Step 1: Fix encryptSeed — add try/finally with zeroing**

Wrap the encryption section in try/finally:
```typescript
export async function encryptSeed(seed: Uint8Array, password: string): Promise<Uint8Array> {
  // ... salt, derivedKey generation ...
  try {
    // ... checksum, plaintext, AES encryption ...
    return result
  } finally {
    derivedKey.fill(0)
    plaintext.fill(0)
  }
}
```

**Step 2: Fix decryptSeed — add try/finally with zeroing**

```typescript
export async function decryptSeed(encrypted: Uint8Array, password: string): Promise<Uint8Array> {
  // ... salt, nonce, ciphertext parsing ...
  const derivedKey = argon2id(...)
  try {
    // ... AES decryption, checksum verification ...
    return seed
  } finally {
    derivedKey.fill(0)
    if (plaintext) plaintext.fill(0)
  }
}
```

**Step 3: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/wallet`

**Step 4: Commit**

```
fix(libbitfs-ts): zero key material in seed encrypt/decrypt [S-04]
```

---

## ~~Task 4: S-05 — SPV verifyPoW recompute hash~~ ✅

**Files:**
- Modify: `libbitfs-ts/src/spv/header.ts` (find `verifyPoW` function)
- Test: `libbitfs-ts/src/spv/__tests__/header.test.ts` (add test)

**Context:** `verifyPoW` trusts the pre-supplied `h.hash` field without recomputing. A compromised HeaderStore returning fraudulent all-zero hash would pass PoW. Defense-in-depth: always recompute.

**Step 1: Add test**

```typescript
it('verifyPoW rejects header with pre-set fraudulent hash', () => {
  const bad = { ...validHeader, hash: new Uint8Array(32) } // all zeros
  expect(() => verifyPoW(bad)).toThrow()
})
```

**Step 2: Run test — verify it fails**

**Step 3: Fix verifyPoW — unconditionally recompute hash**

Add at the start of `verifyPoW`:
```typescript
export function verifyPoW(h: BlockHeader): void {
  const computedHash = computeHeaderHash(h)
  // ... use computedHash instead of h.hash for PoW comparison
}
```

**Step 4: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/spv`

**Step 5: Commit**

```
fix(libbitfs-ts): recompute header hash in verifyPoW for defense-in-depth [S-05]
```

---

## ~~Task 5: Q-12 — Extract shared AES-GCM decrypt helper~~ ✅

**Files:**
- Create: `libbitfs-ts/src/method42/aes.ts`
- Modify: `libbitfs-ts/src/method42/encrypt.ts`
- Modify: `libbitfs-ts/src/method42/capsule.ts`
- Modify: `libbitfs-ts/src/method42/index.ts`

**Context:** `aesGCMDecryptInternal` in capsule.ts (lines 193-209) is identical to `aesGCMDecrypt` in encrypt.ts. Extract to shared module. Also extract `aesGCMEncrypt` since both use identical AAD patterns.

**Step 1: Create aes.ts**

```typescript
// method42/aes.ts — shared AES-256-GCM helpers
import { importAESKey, aesGcmEncrypt, aesGcmDecrypt } from '../subtle.js'
import { ErrInvalidCiphertext, ErrDecryptionFailed } from './errors.js'
import { NONCE_LEN, GCM_TAG_LEN, MIN_CIPHERTEXT_LEN } from './encrypt.js'

export async function aesGCMEncrypt(plaintext: Uint8Array, key: Uint8Array, aad: Uint8Array): Promise<Uint8Array> { ... }
export async function aesGCMDecrypt(ciphertext: Uint8Array, key: Uint8Array, aad: Uint8Array): Promise<Uint8Array> { ... }
```

Move the shared implementations here.

**Step 2: Update encrypt.ts — import from aes.ts, remove local copies**

**Step 3: Update capsule.ts — import from aes.ts, remove `aesGCMDecryptInternal`**

**Step 4: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/method42`

**Step 5: Commit**

```
refactor(libbitfs-ts): extract shared AES-GCM helpers to method42/aes.ts [Q-12]
```

---

## ~~Task 6: Q-14 — Deduplicate verify.ts~~ ✅

**Files:**
- Modify: `libbitfs-ts/src/spv/verify.ts`

**Context:** `verifyTransaction` and `verifyTransactionWithNetwork` share ~90% identical code. Extract shared logic to an internal helper.

**Step 1: Extract shared verification logic**

```typescript
async function verifyTransactionCore(tx: StoredTx, headers: HeaderStore): Promise<BlockHeader> {
  // Steps 1-4 shared logic: validate TxID, rawTx hash, merkle proof, header lookup, PoW
  // Returns the verified header
}

export async function verifyTransaction(tx: StoredTx, headers: HeaderStore): Promise<void> {
  const header = await verifyTransactionCore(tx, headers)
  const valid = verifyMerkleProof(tx.merkleProof!, header.merkleRoot)
  if (!valid) throw new SpvError('spv: merkle proof invalid', 'ERR_MERKLE_PROOF_INVALID')
}

export async function verifyTransactionWithNetwork(tx: StoredTx, headers: HeaderStore, net: Network): Promise<void> {
  const header = await verifyTransactionCore(tx, headers)
  validateMinDifficulty(header, net)
  const valid = verifyMerkleProof(tx.merkleProof!, header.merkleRoot)
  if (!valid) throw new SpvError('spv: merkle proof invalid', 'ERR_MERKLE_PROOF_INVALID')
}
```

**Step 2: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/spv`

**Step 3: Commit**

```
refactor(libbitfs-ts): deduplicate SPV verify functions [Q-14]
```

---

## ~~Task 7: Q-05 — Bounded response body reads in paymail~~ ✅

**Files:**
- Modify: `libbitfs-ts/src/paymail/discover.ts`

**Context:** `resp.text()` reads entire body before size check. Content-Length header check exists but can be bypassed by a malicious server. Add streaming reader with byte limit.

**Step 1: Add bounded read helper**

```typescript
async function readBoundedBody(resp: Response, maxSize: number, label: string): Promise<string> {
  const contentLength = resp.headers.get('content-length')
  if (contentLength && parseInt(contentLength, 10) > maxSize) {
    throw new PaymailDiscoveryError(`${label} response exceeds maximum size`)
  }
  // Use streaming reader for defense against servers that omit Content-Length
  const reader = resp.body?.getReader()
  if (!reader) {
    return resp.text()
  }
  const chunks: Uint8Array[] = []
  let totalBytes = 0
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      totalBytes += value.length
      if (totalBytes > maxSize) {
        throw new PaymailDiscoveryError(`${label} response exceeds maximum size`)
      }
      chunks.push(value)
    }
  } finally {
    reader.releaseLock()
  }
  const combined = new Uint8Array(totalBytes)
  let offset = 0
  for (const chunk of chunks) {
    combined.set(chunk, offset)
    offset += chunk.length
  }
  return new TextDecoder().decode(combined)
}
```

**Step 2: Replace `resp.text()` calls in discoverCapabilities, resolvePKI, resolvePaymentDestination**

Replace the pattern:
```typescript
const contentLength = resp.headers.get('content-length')
if (contentLength && parseInt(contentLength, 10) > MAX_PAYMAIL_RESPONSE_SIZE) { ... }
let bodyText: string
try { bodyText = await resp.text() } catch ...
```

With:
```typescript
let bodyText: string
try { bodyText = await readBoundedBody(resp, MAX_PAYMAIL_RESPONSE_SIZE, 'discovery') } catch ...
```

**Step 3: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/paymail`

**Step 4: Commit**

```
fix(libbitfs-ts): bounded streaming reads for paymail responses [Q-05]
```

---

## ~~Task 8: Q-07 — RPC response validation~~ ✅

**Files:**
- Modify: `libbitfs-ts/src/network/rpc.ts:398-410`

**Context:** `call<T>` returns `rpcResp.result as T` without any runtime validation. Add basic validation that result is not undefined.

**Step 1: Add validation after JSON parse**

```typescript
const rpcResp = await resp.json()

// Validate response structure
if (typeof rpcResp !== 'object' || rpcResp === null) {
  throw new InvalidResponseError('malformed JSON-RPC response')
}
if (!('result' in rpcResp) && !('error' in rpcResp)) {
  throw new InvalidResponseError('response missing both result and error fields')
}

if (rpcResp.id !== reqBody.id) { ... }
if (rpcResp.error) { ... }

return rpcResp.result as T
```

**Step 2: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/network`

**Step 3: Commit**

```
fix(libbitfs-ts): validate JSON-RPC response structure before type cast [Q-07]
```

---

## ~~Task 9: Q-10 — Replace const enum with regular enum~~ ✅

**Files:**
- Modify: `libbitfs-ts/src/method42/access.ts` (1 enum: `Access`)
- Modify: `libbitfs-ts/src/metanet/types.ts` (7 enums: `NodeType`, `OpType`, `LinkType`, `AccessLevel`, `CompressionScheme`, `ISOStatus`, `CLTVResult`)
- Modify: `libbitfs-ts/src/tx/types.ts` (1 enum: `BatchOpType`)

**Context:** `const enum` is erased at compile time. Consumers using esbuild/Vite/SWC with `isolatedModules: true` cannot use them. For a library, use regular `enum` or string unions. Regular `enum` is the lowest-friction change (just remove `const` keyword).

**Step 1: Remove `const` from all 9 enum declarations**

In each file, change:
```typescript
export const enum Foo {
```
to:
```typescript
export enum Foo {
```

Files:
- `src/method42/access.ts:5`: `Access`
- `src/metanet/types.ts:6,14,21,29,39,47,55`: `NodeType`, `OpType`, `LinkType`, `AccessLevel`, `CompressionScheme`, `ISOStatus`, `CLTVResult`
- `src/tx/types.ts:22`: `BatchOpType`

**Step 2: Run full test suite**

Run: `cd libbitfs-ts && npx vitest run`

**Step 3: Commit**

```
fix(libbitfs-ts): replace const enum with regular enum for isolatedModules compat [Q-10]
```

---

## ~~Task 10: Q-08 — Expand cross-language test vectors~~ ✅

**Files:**
- Modify: `libbitfs-ts/src/__tests__/vectors/go-vectors.json` (add vectors)
- Modify: `libbitfs-ts/src/__tests__/crosslang.test.ts` (add test suites)

**Context:** Only 6 test suites exist. Audit wants coverage for: TLV round-trip, AES-GCM encrypt/decrypt, HTLC script construction, Merkle root, revenue distribution, LZW/GZIP compression, capsule computation, block header serialization.

**Approach:** Generate vectors from the Go implementation for each missing area. Create test entries in go-vectors.json with input/expected output hex strings, then verify TS produces identical results.

**New vector categories to add (8):**

1. **method42_encrypt**: plaintext + privkey + pubkey → ciphertext (verify keyHash, nonce from Go)
2. **method42_capsule**: nodePriv + nodePub + buyerPub + keyHash → capsule bytes
3. **metanet_tlv_roundtrip**: Node fields → TLV bytes → parse back → verify fields match
4. **metanet_merkle_root**: list of ChildEntry → Merkle root hash
5. **revshare_distribute**: totalPayment + entries → per-entry amounts
6. **storage_compress_lzw**: plaintext → LZW compressed bytes (Go-compatible)
7. **x402_htlc_script**: HTLCParams → script bytes (byte-identical to Go)
8. **spv_header_serialize**: BlockHeader fields → 80-byte header → hash

**Step 1: Generate test vectors from Go**

Run in `libbitfs-go/`: write a small Go test that outputs JSON vectors for each category. Save to `libbitfs-ts/src/__tests__/vectors/go-vectors.json`.

(Alternatively, hardcode known-good vectors from Go test output.)

**Step 2: Add test suites in crosslang.test.ts**

Each new test reads the vector, calls the TS function, and compares output bytes.

**Step 3: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/__tests__/crosslang`

**Step 4: Commit**

```
test(libbitfs-ts): expand cross-language test vectors for 8 missing areas [Q-08]
```

---

## Task 11: A-02 — Implement ParseTxNodeOps

**Files:**
- Create: `libbitfs-ts/src/tx/parse.ts`
- Modify: `libbitfs-ts/src/tx/index.ts`
- Create: `libbitfs-ts/src/tx/__tests__/parse.test.ts`

**Context:** Missing from TS that exists in Go: `ParseTxNodeOps` extracts Metanet node operations from a transaction's outputs. Needed by explorers, indexers, and protocol parsers.

**Types to add (in types.ts or parse.ts):**

```typescript
export interface TxOutput {
  value: bigint
  scriptPubKey: Uint8Array
}

export interface ParsedNodeOp {
  pNode: Uint8Array     // 33-byte compressed pubkey
  parentTxID: Uint8Array // 0 or 32 bytes
  payload: Uint8Array    // TLV-encoded
  vout: number           // OP_RETURN output index
  nodeVout: number       // Following P2PKH output index
}
```

**Core function:**

```typescript
export function parseTxNodeOps(outputs: TxOutput[]): ParsedNodeOp[]
```

**Algorithm (from Go reference):**
1. Iterate outputs by index `i`
2. Check if output[i] script starts with `OP_FALSE OP_RETURN`
3. Parse push data from script, check for "meta" flag prefix
4. Extract PNode (33 bytes), ParentTxID (0 or 32 bytes), Payload
5. nodeVout = i + 1 (the paired P2PKH dust output)
6. Skip next output (i++) to avoid re-processing the P2PKH
7. Return all parsed operations

**Internal helper:**

```typescript
function parseMetanetOPReturn(scriptBytes: Uint8Array): Uint8Array[] | null
```

Checks for `OP_FALSE(0x00) OP_RETURN(0x6a)` prefix, parses push data items, validates first push is META_FLAG.

**Step 1: Write tests**

```typescript
describe('parseTxNodeOps', () => {
  it('extracts single node op from transaction outputs', () => { ... })
  it('extracts multiple node ops from batch transaction', () => { ... })
  it('returns empty array for non-Metanet transaction', () => { ... })
  it('skips malformed OP_RETURN outputs', () => { ... })
})
```

**Step 2: Implement parse.ts**

**Step 3: Export from index.ts**

Add to `libbitfs-ts/src/tx/index.ts`:
```typescript
export type { TxOutput, ParsedNodeOp } from './parse.js'
export { parseTxNodeOps } from './parse.js'
```

**Step 4: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/tx`

**Step 5: Commit**

```
feat(libbitfs-ts): implement ParseTxNodeOps for extracting Metanet ops from transactions [A-02]
```

---

## Task 12: A-01 + S-02 — x402 refund flow + VerifyHTLCFunding

**Files:**
- Create: `libbitfs-ts/src/x402/refund.ts`
- Modify: `libbitfs-ts/src/x402/types.ts` (add types)
- Modify: `libbitfs-ts/src/x402/index.ts` (add exports)
- Create: `libbitfs-ts/src/x402/__tests__/refund.test.ts`

**Context:** The x402 module is missing the complete refund flow that exists in Go. Without it, buyers have no way to recover funds if the seller disappears. Three functions needed.

**Types to add to types.ts:**

```typescript
export interface SellerPreSignParams {
  fundingTxID: Uint8Array    // 32-byte HTLC funding tx hash
  fundingVout: number         // HTLC output index
  fundingAmount: bigint       // HTLC output amount
  htlcScript: Uint8Array      // HTLC locking script bytes
  sellerPrivKey: PrivateKey   // Signs refund (seller's half of 2-of-2)
  buyerOutputAddr: Uint8Array // 20-byte buyer P2PKH destination
  timeout: number             // Refund timeout in blocks (0 = default)
  feeRate: number             // Satoshis per byte (0 = default)
}

export interface SellerPreSignResult {
  txBytes: Uint8Array    // Serialized tx (without unlocking script)
  sellerSig: Uint8Array  // Seller's DER signature + sighash flag
}

export interface BuyerRefundParams {
  sellerPreSignedTx: Uint8Array  // From SellerPreSignResult.txBytes
  sellerSig: Uint8Array          // From SellerPreSignResult.sellerSig
  htlcScript: Uint8Array         // HTLC locking script bytes
  fundingAmount: bigint          // HTLC output amount (for sighash)
  buyerPrivKey: PrivateKey       // Signs refund (buyer's half of 2-of-2)
  fundingTxID?: Uint8Array       // Optional: verify expected funding UTXO
  fundingVout?: number           // Optional: expected output index
}
```

**Functions to implement in refund.ts:**

### 12a: verifyHTLCFunding

```typescript
export function verifyHTLCFunding(rawTx: Uint8Array, expectedScript: Uint8Array, minAmount: bigint): number
```

Deserialize tx, search outputs for one matching `expectedScript` with `satoshis >= minAmount`. Return the matching output index. Throw if none found or amount insufficient.

### 12b: buildSellerPreSignedRefund

```typescript
export async function buildSellerPreSignedRefund(params: SellerPreSignParams): Promise<SellerPreSignResult>
```

Key logic:
1. Validate params (seller privkey, script, timeout bounds)
2. Create transaction with **lockTime = timeout** (enforces nLockTime)
3. Input: references HTLC funding UTXO, **sequence = 0xfffffffe** (enables nLockTime)
4. Output: P2PKH to buyerOutputAddr, amount = fundingAmount - estFee
5. Compute sighash with SIGHASH_ALL | SIGHASH_FORKID
6. Sign with seller's key, return tx bytes + seller signature (DER + flag byte)

### 12c: buildBuyerRefundTx

```typescript
export async function buildBuyerRefundTx(params: BuyerRefundParams): Promise<Uint8Array>
```

Key logic:
1. Deserialize seller's pre-signed tx
2. Optionally verify it references expected funding UTXO
3. Re-attach source output for sighash computation
4. Compute sighash, sign with buyer's key
5. Build unlocking script: `OP_0 <buyer_sig> <seller_sig> OP_FALSE`
   - OP_0: CHECKMULTISIG dummy element
   - OP_FALSE: selects ELSE branch (refund path)
6. Return fully signed transaction bytes

### Tests:

```typescript
describe('x402 refund flow', () => {
  it('verifyHTLCFunding finds matching HTLC output', () => { ... })
  it('verifyHTLCFunding rejects insufficient amount', () => { ... })
  it('complete refund round-trip: fund → pre-sign → counter-sign', async () => {
    // 1. Build HTLC funding tx
    // 2. Seller pre-signs refund
    // 3. Buyer counter-signs
    // 4. Verify: lockTime = timeout, sequence = 0xfffffffe
    // 5. Verify: unlocking script has correct structure
  })
  it('buildBuyerRefundTx rejects mismatched funding txid', () => { ... })
})
```

**Step 1: Add types to types.ts**
**Step 2: Write tests in refund.test.ts**
**Step 3: Implement refund.ts**
**Step 4: Export from index.ts**
**Step 5: Run tests**

Run: `cd libbitfs-ts && npx vitest run src/x402`

**Step 6: Commit**

```
feat(libbitfs-ts): implement x402 refund flow with pre-signed 2-of-2 multisig [A-01, S-02]
```

---

## Post-Implementation

After all 12 tasks:

1. Run full test suite: `cd libbitfs-ts && npx vitest run`
2. Run typecheck: `cd libbitfs-ts && npx tsc --noEmit`
3. Run build: `cd libbitfs-ts && npm run build`
4. Verify all 875+ existing tests still pass
5. Count new tests added

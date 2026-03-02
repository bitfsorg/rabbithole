# libbitfs-ts Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the TypeScript mirror of libbitfs-go — 11 packages with 100% wire-format compatibility.

**Architecture:** Single npm package `@bitfs/libbitfs` with sub-path exports per module. ESM-first, browser + Node.js. Each module in `src/<module>/` with `index.ts` re-exporting public API.

**Tech Stack:** Bun runtime, Vitest tests, `@bsv/sdk` v2 (BSV primitives), `@noble/hashes` (HKDF), `hash-wasm` (Argon2id). TypeScript strict mode.

**Design Doc:** `docs/tasks/2026-03-02-libbitfs-ts-design.md`

**Reference Source:** `libbitfs-go/` — all implementations must produce identical binary output for the same inputs.

---

## Phase 0: Project Scaffolding

### Task 1: Initialize project structure

**Files:**
- Create: `libbitfs-ts/package.json`
- Create: `libbitfs-ts/tsconfig.json`
- Create: `libbitfs-ts/vitest.config.ts`
- Create: `libbitfs-ts/.gitignore`
- Create: `libbitfs-ts/src/index.ts`
- Create: `libbitfs-ts/src/errors.ts`

**Step 1: Create package.json**

```json
{
  "name": "@bitfs/libbitfs",
  "version": "0.1.0",
  "type": "module",
  "main": "./dist/index.js",
  "types": "./dist/index.d.ts",
  "exports": {
    ".": "./dist/index.js",
    "./method42": "./dist/method42/index.js",
    "./wallet": "./dist/wallet/index.js",
    "./config": "./dist/config/index.js",
    "./metanet": "./dist/metanet/index.js",
    "./storage": "./dist/storage/index.js",
    "./tx": "./dist/tx/index.js",
    "./spv": "./dist/spv/index.js",
    "./network": "./dist/network/index.js",
    "./x402": "./dist/x402/index.js",
    "./paymail": "./dist/paymail/index.js",
    "./revshare": "./dist/revshare/index.js"
  },
  "scripts": {
    "build": "bun build ./src/index.ts --outdir ./dist --target node",
    "test": "vitest run",
    "test:watch": "vitest",
    "typecheck": "tsc --noEmit"
  },
  "dependencies": {
    "@bsv/sdk": "^2.0.5",
    "@noble/hashes": "^1.7.0",
    "hash-wasm": "^4.12.0"
  },
  "devDependencies": {
    "typescript": "^5.7.0",
    "vitest": "^3.0.0"
  },
  "license": "SEE LICENSE IN LICENSE"
}
```

**Step 2: Create tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ESNext",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "esModuleInterop": true,
    "declaration": true,
    "outDir": "dist",
    "rootDir": "src",
    "sourceMap": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true
  },
  "include": ["src"],
  "exclude": ["node_modules", "dist", "**/__tests__"]
}
```

**Step 3: Create vitest.config.ts**

```typescript
import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    globals: true,
    include: ['src/**/__tests__/**/*.test.ts'],
  },
})
```

**Step 4: Create .gitignore**

```
node_modules/
dist/
*.tsbuildinfo
```

**Step 5: Create src/errors.ts** — Base error class

```typescript
export class BitfsError extends Error {
  constructor(message: string, public readonly code: string) {
    super(message)
    this.name = 'BitfsError'
  }
}
```

**Step 6: Create src/index.ts** — Root barrel

```typescript
export { BitfsError } from './errors.js'
```

**Step 7: Install dependencies and verify**

Run: `cd libbitfs-ts && bun install`
Run: `cd libbitfs-ts && bunx vitest run` (should pass with 0 tests)
Run: `cd libbitfs-ts && bun run typecheck` (should pass)

**Step 8: Commit**

```bash
git add -A && git commit -m "feat(libbitfs-ts): initialize project with Bun + Vitest + @bsv/sdk"
```

---

## Phase 1: Crypto Core

### Task 2: method42 — errors + access types

**Files:**
- Create: `src/method42/errors.ts`
- Create: `src/method42/access.ts`
- Create: `src/method42/index.ts`
- Test: `src/method42/__tests__/access.test.ts`

**Step 1: Write errors.ts**

```typescript
import { BitfsError } from '../errors.js'

export class Method42Error extends BitfsError {
  constructor(message: string, code: string) {
    super(message, code)
    this.name = 'Method42Error'
  }
}

export const ErrNilPrivateKey = new Method42Error('private key is nil', 'ERR_NIL_PRIVATE_KEY')
export const ErrNilPublicKey = new Method42Error('public key is nil', 'ERR_NIL_PUBLIC_KEY')
export const ErrInvalidCiphertext = new Method42Error('invalid ciphertext', 'ERR_INVALID_CIPHERTEXT')
export const ErrDecryptionFailed = new Method42Error('decryption failed', 'ERR_DECRYPTION_FAILED')
export const ErrKeyHashMismatch = new Method42Error('key hash mismatch after decryption', 'ERR_KEY_HASH_MISMATCH')
export const ErrInvalidAccess = new Method42Error('invalid access mode', 'ERR_INVALID_ACCESS')
export const ErrHKDFFailure = new Method42Error('HKDF key derivation failed', 'ERR_HKDF_FAILURE')
```

**Step 2: Write access.ts**

```typescript
import { PrivateKey } from '@bsv/sdk'
import { ErrNilPrivateKey, ErrInvalidAccess } from './errors.js'

export const enum Access {
  Private = 0,
  Free = 1,
  Paid = 2,
}

export function accessToString(a: Access): string {
  switch (a) {
    case Access.Private: return 'PRIVATE'
    case Access.Free: return 'FREE'
    case Access.Paid: return 'PAID'
    default: return 'UNKNOWN'
  }
}

/** Returns PrivateKey with scalar 1 for FREE access mode. */
export function freePrivateKey(): PrivateKey {
  return new PrivateKey(1)
}

/** Returns the effective private key for the given access mode. */
export function effectivePrivateKey(access: Access, nodePrivateKey: PrivateKey | null): PrivateKey {
  switch (access) {
    case Access.Free:
      return freePrivateKey()
    case Access.Private:
    case Access.Paid:
      if (!nodePrivateKey) throw ErrNilPrivateKey
      return nodePrivateKey
    default:
      throw ErrInvalidAccess
  }
}
```

**Step 3: Write test**

```typescript
import { describe, it, expect } from 'vitest'
import { Access, accessToString, freePrivateKey, effectivePrivateKey } from '../access.js'
import { PrivateKey } from '@bsv/sdk'

describe('Access', () => {
  it('accessToString returns correct names', () => {
    expect(accessToString(Access.Private)).toBe('PRIVATE')
    expect(accessToString(Access.Free)).toBe('FREE')
    expect(accessToString(Access.Paid)).toBe('PAID')
  })

  it('freePrivateKey returns scalar 1', () => {
    const key = freePrivateKey()
    expect(key).toBeInstanceOf(PrivateKey)
    // PrivateKey(1) should derive a valid public key
    expect(key.toPublicKey()).toBeDefined()
  })

  it('effectivePrivateKey returns free key for Free access', () => {
    const key = effectivePrivateKey(Access.Free, null)
    expect(key).toBeDefined()
  })

  it('effectivePrivateKey throws for Private without key', () => {
    expect(() => effectivePrivateKey(Access.Private, null)).toThrow('private key is nil')
  })

  it('effectivePrivateKey returns provided key for Private', () => {
    const priv = PrivateKey.fromRandom()
    const result = effectivePrivateKey(Access.Private, priv)
    expect(result).toBe(priv)
  })
})
```

**Step 4: Run tests**

Run: `cd libbitfs-ts && bunx vitest run src/method42/__tests__/access.test.ts`
Expected: All pass

**Step 5: Commit**

```bash
git add -A && git commit -m "feat(method42): add access types and error definitions"
```

---

### Task 3: method42 — ECDH + KDF

**Files:**
- Create: `src/method42/ecdh.ts`
- Create: `src/method42/kdf.ts`
- Test: `src/method42/__tests__/kdf.test.ts`

**Implementation notes:**
- `ECDH(priv, pub)` → uses `priv.deriveSharedSecret(pub)` from @bsv/sdk, returns x-coordinate as 32 bytes zero-padded big-endian
- `computeKeyHash(plaintext)` → SHA256(SHA256(plaintext)) using `Hash.sha256` from @bsv/sdk
- `deriveAESKey(sharedX, keyHash)` → HKDF-SHA256 from `@noble/hashes/hkdf` with info="bitfs-file-encryption"
- `deriveMetadataKey(sharedX)` → random 16B salt + HKDF with info="bitfs-metadata-encryption"
- `deriveMetadataKeyWithSalt(sharedX, salt)` → deterministic with provided salt
- `deriveBuyerMask(sharedX, keyHash)` → HKDF with info="bitfs-buyer-mask"
- `deriveBuyerMaskWithNonce(sharedX, keyHash, nonce)` → salt = keyHash || nonce

**Key constants:**

```typescript
export const HKDF_INFO = 'bitfs-file-encryption'
export const HKDF_BUYER_MASK_INFO = 'bitfs-buyer-mask'
export const HKDF_METADATA_INFO = 'bitfs-metadata-encryption'
export const AES_KEY_LEN = 32
export const METADATA_SALT_LEN = 16
export const HASH_SIZE = 32
```

**Test plan:**
- `computeKeyHash` produces 32 bytes, is deterministic
- `deriveAESKey` with known inputs produces known output (generate Go test vector)
- `ECDH` returns 32-byte x-coordinate
- `ECDH(FreePrivateKey(), pub)` equals pub's x-coordinate
- `deriveBuyerMask` vs `deriveBuyerMaskWithNonce(null)` produce same result
- `deriveMetadataKey` produces different keys each call (random salt)

**Step N: Commit**

```bash
git commit -m "feat(method42): add ECDH and KDF functions"
```

---

### Task 4: method42 — AES-GCM encrypt/decrypt

**Files:**
- Create: `src/method42/encrypt.ts`
- Test: `src/method42/__tests__/encrypt.test.ts`

**Implementation notes:**
- Use `crypto.subtle.encrypt/decrypt` with `AES-GCM` algorithm
- Nonce: 12 bytes random (`crypto.getRandomValues`)
- Output format: `nonce(12B) || ciphertext || tag(16B)` — Note: SubtleCrypto appends tag automatically
- AAD (additional data) = keyHash for file encryption, salt for metadata
- Constants: `NONCE_LEN = 12`, `GCM_TAG_LEN = 16`, `MIN_CIPHERTEXT_LEN = 28`, `MIN_ENC_PAYLOAD_LEN = 44`

**Functions:**
- `encrypt(plaintext, privKey, pubKey, access)` → `{ ciphertext, keyHash }`
- `decrypt(ciphertext, privKey, pubKey, keyHash, access)` → `{ plaintext, keyHash }`
- `reEncrypt(ciphertext, privKey, pubKey, keyHash, fromAccess, toAccess)` → `{ ciphertext, keyHash }`
- `encryptMetadata(tlvPayload, privKey, pubKey)` → `salt(16B) || nonce(12B) || encrypted || tag(16B)`
- `decryptMetadata(encPayload, privKey, pubKey)` → plaintext bytes

**Test plan:**
- Round-trip: encrypt then decrypt returns original plaintext
- keyHash verification: tampered keyHash causes decryption failure
- Access modes: PRIVATE, FREE produce different ciphertexts but both decrypt correctly
- Metadata: round-trip encryptMetadata/decryptMetadata
- Edge: empty plaintext, large plaintext (1MB)
- Error: ciphertext too short, wrong key

```bash
git commit -m "feat(method42): add AES-GCM encryption and decryption"
```

---

### Task 5: method42 — capsule + Rabin signature

**Files:**
- Create: `src/method42/capsule.ts`
- Create: `src/method42/rabin.ts`
- Test: `src/method42/__tests__/capsule.test.ts`
- Test: `src/method42/__tests__/rabin.test.ts`

**Capsule functions:**
- `computeCapsule(nodePriv, nodePub, buyerPub, keyHash)` → capsule bytes (32B)
- `computeCapsuleWithNonce(nodePriv, nodePub, buyerPub, keyHash, nonce)` → capsule bytes
- `computeCapsuleHash(fileTxID, capsule)` → SHA256(fileTxID || capsule)
- `decryptWithCapsule(ciphertext, capsule, keyHash, buyerPriv, nodePub)` → DecryptResult
- `decryptWithCapsuleNonce(ciphertext, capsule, keyHash, buyerPriv, nodePub, nonce)` → DecryptResult

**Capsule test plan:**
- Full buyer flow: encrypt(PAID) → computeCapsule → decryptWithCapsule → plaintext matches
- Nonce variant: computeCapsuleWithNonce → decryptWithCapsuleNonce
- capsuleHash binds to fileTxID
- Wrong buyer key fails decryption

**Rabin functions (use native BigInt):**
- `generateRabinKey(bitSize)` → `{ p, q, n }`
- `rabinSign(key, message)` → `{ sig, pad }`
- `rabinVerify(n, message, sig, pad)` → boolean
- `serializeRabinSignature(sig, pad)` → bytes
- `deserializeRabinSignature(data)` → `{ sig, pad }`
- `serializeRabinPubKey(n)` → bytes
- `deserializeRabinPubKey(data)` → bigint

Helper: `generateBlumPrime(bitSize)` — generate prime p where p ≡ 3 (mod 4). Use `crypto.getRandomValues` for entropy, Miller-Rabin primality test.

**Rabin test plan:**
- Generate 512-bit key, sign message, verify succeeds
- Tampered message fails verification
- Serialize/deserialize round-trip

```bash
git commit -m "feat(method42): add capsule operations and Rabin signature"
```

---

### Task 6: wallet — seed + mnemonic

**Files:**
- Create: `src/wallet/errors.ts`
- Create: `src/wallet/seed.ts`
- Create: `src/wallet/index.ts`
- Test: `src/wallet/__tests__/seed.test.ts`

**Implementation notes:**
- BIP39: use @bsv/sdk `HD` class or `@scure/bip39` if needed
- Mnemonic generation: `HD.fromRandom()` gives HD key directly; alternatively generate mnemonic first
- `seedFromMnemonic(mnemonic, passphrase)` → 64-byte seed via PBKDF2(SHA512, "mnemonic"+passphrase, 2048)
- `encryptSeed(seed, password)` → Argon2id(password, salt) → AES-GCM(seed || checksum)
  - Format: `salt(16B) || nonce(12B) || AES-GCM(seed || SHA256(seed)[:4])`
  - Argon2id params: time=3, memory=64MB, parallelism=4, keyLen=32
  - Use `hash-wasm` for Argon2id
- `decryptSeed(encrypted, password)` → seed bytes, verify checksum

**Constants:**

```typescript
export const MNEMONIC_12_WORDS = 128
export const MNEMONIC_24_WORDS = 256
export const ARGON2_TIME = 3
export const ARGON2_MEMORY = 65536 // 64 * 1024 KB
export const ARGON2_PARALLELISM = 4
export const ARGON2_KEY_LEN = 32
export const SALT_LEN = 16
export const NONCE_LEN = 12
export const CHECKSUM_LEN = 4
```

**Test plan:**
- Generate mnemonic → seedFromMnemonic produces 64 bytes
- Mnemonic validation
- encryptSeed/decryptSeed round-trip
- Wrong password → decryption error
- Corrupted data → checksum mismatch

```bash
git commit -m "feat(wallet): add seed generation and Argon2id encryption"
```

---

### Task 7: wallet — HD derivation

**Files:**
- Create: `src/wallet/network.ts`
- Create: `src/wallet/hd.ts`
- Create: `src/wallet/vault.ts`
- Test: `src/wallet/__tests__/hd.test.ts`
- Test: `src/wallet/__tests__/vault.test.ts`

**network.ts — Network configs:**

```typescript
export interface NetworkConfig {
  name: string
  addressVersion: number
  p2shVersion: number
  defaultPort: number
  rpcPort: number
  dnsSeeds: string[]
  genesisHash: string
}

export const MainNet: NetworkConfig = {
  name: 'mainnet',
  addressVersion: 0x00,
  p2shVersion: 0x05,
  defaultPort: 8333,
  rpcPort: 8332,
  dnsSeeds: ['seed.bitcoinsv.io', 'seed.satoshisvision.network'],
  genesisHash: '000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f',
}

export const TestNet: NetworkConfig = { /* ... */ }
export const RegTest: NetworkConfig = { /* ... */ }
```

**hd.ts — Wallet class:**

Uses `@bsv/sdk` `HD` class for BIP32 derivation.

```typescript
export interface KeyPair {
  privateKey: PrivateKey
  publicKey: PublicKey
  path: string
}

export class Wallet {
  private masterKey: HD
  readonly network: NetworkConfig

  constructor(seed: Uint8Array, network?: NetworkConfig)
  deriveFeeKey(chain: number, index: number): KeyPair
  deriveVaultRootKey(vaultIndex: number): KeyPair
  deriveNodeKey(vaultIndex: number, filePath?: number[], hardened?: boolean[]): KeyPair
}
```

**Path:** `m/44'/236'/(vaultIndex+1)'/0/0[/filePath...]`

**Constants:**
```typescript
export const PURPOSE_BIP44 = 44
export const COIN_TYPE_BITFS = 236
export const FEE_ACCOUNT = 0
export const DEFAULT_VAULT_ACCOUNT = 1
export const EXTERNAL_CHAIN = 0
export const INTERNAL_CHAIN = 1
export const MAX_FILE_INDEX = 0x7FFFFFFF
export const MAX_PATH_DEPTH = 64
export const HARDENED = 0x80000000
```

**vault.ts:**
```typescript
export interface Vault {
  name: string
  accountIndex: number
  rootTxID: Uint8Array | null
  deleted: boolean
}

export interface WalletState {
  nextReceiveIndex: number
  nextChangeIndex: number
  vaults: Vault[]
  nextVaultIndex: number
}

export function newWalletState(): WalletState
export function createVault(state: WalletState, name: string): Vault
export function getVault(state: WalletState, name: string): Vault
export function listVaults(state: WalletState): Vault[]
export function renameVault(state: WalletState, oldName: string, newName: string): void
export function deleteVault(state: WalletState, name: string): void
export function validateWalletState(state: WalletState): void
```

**Test plan (hd):**
- Derive fee key path matches `m/44'/236'/0'/chain/index`
- Derive vault root key matches `m/44'/236'/(vault+1)'/0/0`
- Derive node key with filePath `[3, 1, 7]` → correct path string
- Same seed produces same keys
- Different seeds produce different keys
- Path depth > 64 throws

**Test plan (vault):**
- Create vault assigns incrementing account indices
- Delete vault is soft-delete, index not reused
- Duplicate name throws
- Validate catches inconsistencies

```bash
git commit -m "feat(wallet): add HD derivation, network config, and vault state"
```

---

### Task 8: config — configuration management

**Files:**
- Create: `src/config/errors.ts`
- Create: `src/config/config.ts`
- Create: `src/config/index.ts`
- Test: `src/config/__tests__/config.test.ts`

**Implementation:** Direct port of Go config package — key=value parser, validation, default values.

```typescript
export interface Config {
  dataDir: string
  listenAddr: string
  network: string
  logLevel: string
  logFile: string
}

export function defaultDataDir(): string  // ~/.bitfs
export function defaultConfig(): Config
export function loadConfig(path: string): Promise<Config>  // async for Node.js fs
export function saveConfig(path: string, cfg: Config): Promise<void>
export function validateConfig(cfg: Config): void
export function configPath(dataDir: string): string
```

**Note:** File I/O uses Node.js `fs/promises`. Browser users won't call loadConfig/saveConfig.

**Test plan:**
- Parse valid key=value string
- Comments and blank lines ignored
- Unknown keys silently ignored
- Validation: invalid network throws, invalid log level throws
- Default config has sensible values
- Round-trip: save then load returns same config

```bash
git commit -m "feat(config): add configuration management"
```

---

## Phase 2: Data Layer

### Task 9: metanet — types + constants + errors

**Files:**
- Create: `src/metanet/types.ts`
- Create: `src/metanet/errors.ts`
- Create: `src/metanet/index.ts`

All node types, enums, interfaces. Direct translation from Go:

```typescript
export const enum NodeType { File = 0, Dir = 1, Link = 2, Anchor = 3 }
export const enum OpType { Create = 0, Update = 1, Delete = 2 }
export const enum LinkType { Soft = 0, SoftRemote = 1 }
export const enum AccessLevel { Private = 0, Free = 1, Paid = 2 }
export const enum ISOStatus { None = 0, Open = 1, Partial = 2, Closed = 3 }
export const enum CLTVResult { Allowed = 0, Denied = 1 }

export interface ISOConfig { totalShares: bigint; pricePerShare: bigint; creatorAddr: Uint8Array; status: ISOStatus }
export interface ChildEntry { index: number; name: string; type: NodeType; pubKey: Uint8Array; hardened: boolean }
export interface Node { /* ~50 fields, all matching Go struct */ }
export interface ResolveResult { node: Node; entry: ChildEntry | null; parent: Node | null; path: string[] }

export interface NodeStore {
  getNodeByPubKey(pNode: Uint8Array): Promise<Node | null>
  getNodeByTxID(txID: Uint8Array): Promise<Node | null>
  getNodeVersions(pNode: Uint8Array): Promise<Node[]>
  getChildNodes(dirNode: Node): Promise<Node[]>
}
```

**Constants:**
```typescript
export const COMPRESSED_PUBKEY_LEN = 33
export const TXID_LEN = 32
export const MAX_LINK_DEPTH = 10
export const MAX_PATH_COMPONENTS = 256
export const MAX_TOTAL_LINK_FOLLOWS = 40
export const MAX_CHILD_NAME_LEN = 255
export const MAX_PAYLOAD_SIZE = 64 * 1024 * 1024
```

No tests needed for pure type definitions. Commit types.

```bash
git commit -m "feat(metanet): add types, enums, and NodeStore interface"
```

---

### Task 10: metanet — TLV serialization

**Files:**
- Create: `src/metanet/tlv.ts` — low-level TLV encoding/decoding
- Create: `src/metanet/parser.ts` — Node ↔ TLV conversion
- Test: `src/metanet/__tests__/tlv.test.ts`
- Test: `src/metanet/__tests__/parser.test.ts`

**TLV format:** `tag(1B) + length(unsigned LEB128 varint) + value(length bytes)`

**49 TLV tags** — see design doc for complete list. Key implementation:

```typescript
// tlv.ts
export function appendUvarint(buf: Uint8Array[], value: number): void
export function readUvarint(data: Uint8Array, offset: number): [number, number] // [value, bytesRead]
export function appendField(buf: Uint8Array[], tag: number, value: Uint8Array): void
export function appendUint32Field(buf: Uint8Array[], tag: number, value: number): void
export function appendUint64Field(buf: Uint8Array[], tag: number, value: bigint): void
export function appendStringField(buf: Uint8Array[], tag: number, value: string): void

// parser.ts
export function serializePayload(node: Node): Uint8Array
export function parseNode(pushes: Uint8Array[]): Node
export function serializeChildEntry(entry: ChildEntry): Uint8Array
export function deserializeChildEntry(data: Uint8Array): ChildEntry
export function serializeMetadata(m: Map<string, string>): Uint8Array
export function deserializeMetadata(data: Uint8Array): Map<string, string>
export function serializeISOConfig(iso: ISOConfig): Uint8Array  // 37 bytes fixed
export function deserializeISOConfig(data: Uint8Array): ISOConfig
```

**Critical: all integers big-endian, varint is unsigned LEB128.**

**ChildEntry binary format:** `index(4B) || nameLen(2B) || name(var) || type(4B) || pubkeyLen(1B) || pubkey(var) || hardened(1B)`

**ISOConfig binary format (37B):** `totalShares(8B) || pricePerShare(8B) || creatorAddr(20B) || status(1B)`

**Metadata format:** `[keyLen(2B) || key(var) || valLen(2B) || val(var)]...`

**Test plan:**
- Varint round-trip for values 0, 127, 128, 16384, 2^32
- Serialize/deserialize ChildEntry round-trip
- Serialize/deserialize ISOConfig round-trip
- Serialize/deserialize Metadata round-trip
- Full Node serialize → parse round-trip (create Node with all fields populated)
- Unknown tags silently skipped
- **Cross-language vector test:** Serialize a known Node in Go, save hex output, verify TS produces identical bytes

```bash
git commit -m "feat(metanet): add TLV serialization and Node parser"
```

---

### Task 11: metanet — directory operations + Merkle tree

**Files:**
- Create: `src/metanet/directory.ts`
- Create: `src/metanet/merkle.ts`
- Test: `src/metanet/__tests__/directory.test.ts`
- Test: `src/metanet/__tests__/merkle.test.ts`

**directory.ts:**
```typescript
export function listDirectory(node: Node): ChildEntry[]
export function findChild(dirNode: Node, name: string): ChildEntry | null
export function addChild(dirNode: Node, name: string, nodeType: NodeType, pubKey: Uint8Array, hardened: boolean): ChildEntry
export function removeChild(dirNode: Node, name: string): void
export function renameChild(dirNode: Node, oldName: string, newName: string): void
export function nextChildIndex(dirNode: Node): number
```

`addChild` and `removeChild` auto-recompute MerkleRoot.

**merkle.ts — Directory Merkle tree (double-SHA256, even padding by duplication):**
```typescript
export function computeChildLeafHash(entry: ChildEntry): Uint8Array  // doubleHash(serialize(entry))
export function computeDirectoryMerkleRoot(children: ChildEntry[]): Uint8Array | null  // null for empty
export function buildDirectoryMerkleProof(children: ChildEntry[], childIndex: number): Uint8Array[]
export function verifyChildMembership(entry: ChildEntry, proof: Uint8Array[], index: number, merkleRoot: Uint8Array): boolean
```

**Test plan:**
- Empty dir → merkleRoot is null
- Single child → merkleRoot = hash of that child
- Add/remove child → merkleRoot changes
- Proof verification: build proof for child N, verify succeeds
- Tampered proof fails

```bash
git commit -m "feat(metanet): add directory operations and Merkle tree"
```

---

### Task 12: metanet — link resolution + path resolution + CLTV

**Files:**
- Create: `src/metanet/link.ts`
- Create: `src/metanet/resolve.ts`
- Create: `src/metanet/cltv.ts`
- Test: `src/metanet/__tests__/resolve.test.ts`

**link.ts:**
```typescript
export async function followLink(store: NodeStore, linkNode: Node, maxDepth?: number): Promise<Node>
export function latestVersion(nodes: Node[]): Node  // by blockHeight then TxID
export async function inheritPricePerKB(store: NodeStore, node: Node): Promise<bigint>
```

**resolve.ts:**
```typescript
export function splitPath(path: string): string[]
export async function resolvePath(store: NodeStore, root: Node, pathComponents: string[]): Promise<ResolveResult>
```

Path resolution: stack-based for `..` handling, global link budget of 40 total follows, per-link max depth of 10.

**cltv.ts:**
```typescript
export function checkCLTVAccess(node: Node, currentHeight: number): CLTVResult
```

Simple: if `node.cltvHeight > 0 && currentHeight < node.cltvHeight` → Denied, else Allowed.

**Test plan (with mock NodeStore):**
- Resolve simple path `/dir/file`
- Resolve with `..` components
- Link follow: soft link resolves to target node
- Link depth exceeded → error
- CLTV: height below threshold → Denied, at threshold → Allowed

```bash
git commit -m "feat(metanet): add link resolution, path resolver, and CLTV access"
```

---

### Task 13: storage — Store interface + FileStore + compression + chunking

**Files:**
- Create: `src/storage/errors.ts`
- Create: `src/storage/types.ts`
- Create: `src/storage/compress.ts`
- Create: `src/storage/chunk.ts`
- Create: `src/storage/filestore.ts`
- Create: `src/storage/memory.ts`
- Create: `src/storage/resolver.ts`
- Create: `src/storage/index.ts`
- Test: `src/storage/__tests__/compress.test.ts`
- Test: `src/storage/__tests__/chunk.test.ts`
- Test: `src/storage/__tests__/store.test.ts`

**types.ts — Store interface:**
```typescript
export interface Store {
  put(keyHash: Uint8Array, ciphertext: Uint8Array): Promise<void>
  get(keyHash: Uint8Array): Promise<Uint8Array>
  has(keyHash: Uint8Array): Promise<boolean>
  delete(keyHash: Uint8Array): Promise<void>
  size(keyHash: Uint8Array): Promise<number>
  list(): Promise<Uint8Array[]>
}
```

**compress.ts:**
```typescript
export const CompressNone = 0
export const CompressLZW = 1
export const CompressGZIP = 2
export const CompressZSTD = 3  // deferred
export const MAX_DECOMPRESSED_SIZE = 256 * 1024 * 1024

export async function compress(data: Uint8Array, scheme: number): Promise<Uint8Array>
export async function decompress(data: Uint8Array, scheme: number): Promise<Uint8Array>
```

GZIP: Node uses `zlib.gzipSync`/`gunzipSync`. Browser uses `CompressionStream`/`DecompressionStream`.
LZW: Implement simple LZW encoder/decoder (~100 lines each). Must match Go `compress/lzw` MSB ordering.

**chunk.ts:**
```typescript
export const DEFAULT_CHUNK_SIZE = 1024 * 1024  // 1MB

export function splitIntoChunks(data: Uint8Array, chunkSize?: number): Uint8Array[]
export function recombineChunks(chunks: Uint8Array[], expectedHash?: Uint8Array): Uint8Array
export function computeRecombinationHash(chunks: Uint8Array[]): Uint8Array  // SHA256(chunk0 || chunk1 || ...)
```

**filestore.ts — Node.js hash-sharded directory:**
```typescript
export class FileStore implements Store {
  constructor(baseDir: string)
  // Layout: {baseDir}/{ab}/{abcdef...hex}
}

export function keyHashToPath(baseDir: string, keyHash: Uint8Array): string
```

**memory.ts — In-memory store for testing:**
```typescript
export class MemoryStore implements Store { /* Map<string, Uint8Array> */ }
```

**resolver.ts — Content fetching:**
```typescript
export class ContentResolver {
  constructor(store: Store, endpoints?: string[])
  async fetch(keyHash: Uint8Array): Promise<Uint8Array>
  // Priority: local store → HTTP daemon endpoints
}
```

**Test plan:**
- Compress/decompress GZIP round-trip
- Compress/decompress LZW round-trip
- CompressNone is identity
- Chunk 3MB → 3 chunks of 1MB, recombine → original
- RecombinationHash matches across chunks
- MemoryStore: put/get/has/delete/list
- FileStore: put/get/has with tmp directory

```bash
git commit -m "feat(storage): add Store interface, FileStore, compression, and chunking"
```

---

## Phase 3: Blockchain

### Task 14: tx — UTXO types + OP_RETURN builder

**Files:**
- Create: `src/tx/errors.ts`
- Create: `src/tx/types.ts`
- Create: `src/tx/opreturn.ts`
- Create: `src/tx/index.ts`
- Test: `src/tx/__tests__/opreturn.test.ts`

**types.ts:**
```typescript
export interface UTXO {
  txID: Uint8Array       // 32 bytes
  vout: number
  amount: bigint         // satoshis
  scriptPubKey: Uint8Array
  privateKey?: PrivateKey // for signing
}

export const enum BatchOpType {
  Create = 0,
  Update = 1,
  Delete = 2,
  CreateRoot = 3,
}

export interface BatchNodeOp {
  type: BatchOpType
  pubKey: PublicKey
  parentTxID: Uint8Array
  payload: Uint8Array
  inputUTXO?: UTXO
  privateKey?: PrivateKey
}

export interface BatchResult {
  rawTx: Uint8Array
  txID: Uint8Array
  nodeOps: BatchNodeResult[]
  changeUTXO?: UTXO
}

export interface BatchNodeResult {
  opReturnVout: number
  nodeVout: number
  nodeUTXO?: UTXO
}
```

**opreturn.ts:**
```typescript
export const META_FLAG = new Uint8Array([0x6d, 0x65, 0x74, 0x61])  // "meta"
export const DUST_LIMIT = 1n  // 1 satoshi
export const DEFAULT_FEE_RATE = 1n  // 1 sat/KB

export function buildOPReturnData(pNode: PublicKey, parentTxID: Uint8Array, payload: Uint8Array): Uint8Array[]
export function parseOPReturnData(pushes: Uint8Array[]): { pNode: Uint8Array; parentTxID: Uint8Array; payload: Uint8Array }
export function estimateTxSize(numInputs: number, numOutputs: number, payloadSize: number): number
export function estimateFee(txSizeBytes: number, feeRate: bigint): bigint
```

**Test plan:**
- buildOPReturnData produces [MetaFlag, PNode(33B), ParentTxID(32B), Payload]
- parseOPReturnData round-trips
- Fee estimation: known inputs produce expected size

```bash
git commit -m "feat(tx): add UTXO types and OP_RETURN builder"
```

---

### Task 15: tx — MutationBatch

**Files:**
- Create: `src/tx/batch.ts`
- Create: `src/tx/sign.ts`
- Test: `src/tx/__tests__/batch.test.ts`

**batch.ts — Core atomic transaction builder:**

```typescript
export class MutationBatch {
  private ops: BatchNodeOp[] = []
  private feeInputs: UTXO[] = []
  private changeAddr: Uint8Array | null = null  // 20-byte P2PKH hash
  private feeRate: bigint = DEFAULT_FEE_RATE

  addNodeOp(op: BatchNodeOp): void
  addFeeInput(utxo: UTXO): void
  setChange(addr: Uint8Array): void
  setFeeRate(rate: bigint): void
  opCount(): number

  // Convenience
  addCreateChild(childPub: PublicKey, parentTxID: Uint8Array, payload: Uint8Array, parentUTXO: UTXO, parentPriv: PrivateKey): void
  addSelfUpdate(nodePub: PublicKey, parentTxID: Uint8Array, payload: Uint8Array, nodeUTXO: UTXO, nodePriv: PrivateKey): void
  addDelete(nodePub: PublicKey, parentTxID: Uint8Array, payload: Uint8Array, nodeUTXO: UTXO, nodePriv: PrivateKey): void
  addCreateRoot(rootPub: PublicKey, payload: Uint8Array): void

  async build(): Promise<BatchResult>
}
```

Uses `@bsv/sdk` `Transaction`, `P2PKH`, `Script` for building.

**TX Layout per op:**
- Create/Update: `[OP_RETURN(MetaFlag|PNode|ParentTxID|Payload)] + [P2PKH(PNode, 1 sat)]`
- Delete: `[OP_RETURN(MetaFlag|PNode|ParentTxID|Payload)]` (no dust output)
- CreateRoot: `[OP_RETURN(MetaFlag|PNode|empty|Payload)] + [P2PKH(PNode, 1 sat)]`
- Final output: change P2PKH

**sign.ts:**
```typescript
export function buildP2PKHScript(pubKey: PublicKey): Script
export function buildP2PKHOutput(pubKeyHash: Uint8Array, satoshis: bigint): TransactionOutput
```

**Test plan:**
- Single CreateRoot: produces valid TX with 1 OP_RETURN + 1 P2PKH + 1 change
- Multiple ops: 3 ops produce correct output order
- Fee estimation: total outputs + fee ≤ total inputs
- Insufficient funds throws

```bash
git commit -m "feat(tx): add MutationBatch atomic transaction builder"
```

---

### Task 16: spv — block headers + Merkle proofs

**Files:**
- Create: `src/spv/errors.ts`
- Create: `src/spv/types.ts`
- Create: `src/spv/header.ts`
- Create: `src/spv/merkle.ts`
- Create: `src/spv/verify.ts`
- Create: `src/spv/store.ts`
- Create: `src/spv/index.ts`
- Test: `src/spv/__tests__/header.test.ts`
- Test: `src/spv/__tests__/merkle.test.ts`

**header.ts:**
```typescript
export const BLOCK_HEADER_SIZE = 80
export const HASH_SIZE = 32

export interface BlockHeader {
  version: number
  prevBlock: Uint8Array  // 32B
  merkleRoot: Uint8Array // 32B
  timestamp: number
  bits: number
  nonce: number
  height: number
  hash: Uint8Array       // computed
}

export function serializeHeader(h: BlockHeader): Uint8Array  // 80 bytes, little-endian
export function deserializeHeader(data: Uint8Array): BlockHeader
export function computeHeaderHash(h: BlockHeader): Uint8Array  // doubleHash(serialize)
export function compactToTarget(bits: number): Uint8Array
export function verifyPoW(h: BlockHeader): void
export function verifyHeaderChain(headers: BlockHeader[]): void
```

**merkle.ts:**
```typescript
export interface MerkleProof {
  txID: Uint8Array    // 32B
  index: number
  nodes: Uint8Array[] // branch hashes
  blockHash: Uint8Array
}

export function doubleHash(data: Uint8Array): Uint8Array  // SHA256(SHA256(data))
export function computeMerkleRoot(txHash: Uint8Array, index: number, proofNodes: Uint8Array[]): Uint8Array
export function verifyMerkleProof(proof: MerkleProof, expectedMerkleRoot: Uint8Array): boolean
export function buildMerkleTree(txHashes: Uint8Array[]): Uint8Array[]
```

**store.ts:**
```typescript
export interface HeaderStore {
  putHeader(header: BlockHeader): Promise<void>
  getHeader(blockHash: Uint8Array): Promise<BlockHeader | null>
  getHeaderByHeight(height: number): Promise<BlockHeader | null>
  getTip(): Promise<BlockHeader | null>
  getHeaderCount(): Promise<number>
}

export interface TxStore {
  putTx(tx: StoredTx): Promise<void>
  getTx(txID: Uint8Array): Promise<StoredTx | null>
  deleteTx(txID: Uint8Array): Promise<void>
  listTxs(): Promise<StoredTx[]>
}

export class MemHeaderStore implements HeaderStore { /* ... */ }
export class MemTxStore implements TxStore { /* ... */ }
```

**verify.ts:**
```typescript
export function verifyTransaction(tx: StoredTx, headers: HeaderStore): Promise<void>
```

**Test plan:**
- Serialize/deserialize block header round-trip (80 bytes)
- PoW verification: valid header passes, tampered nonce fails
- Merkle proof: build tree from 4 tx hashes, verify proof for tx[2]
- Header chain: consecutive headers with correct prevBlock pass, broken chain fails
- MemHeaderStore/MemTxStore: basic CRUD

```bash
git commit -m "feat(spv): add block headers, Merkle proofs, and SPV verification"
```

---

### Task 17: network — BlockchainService + RPCClient

**Files:**
- Create: `src/network/errors.ts`
- Create: `src/network/types.ts`
- Create: `src/network/rpc.ts`
- Create: `src/network/spvclient.ts`
- Create: `src/network/mock.ts`
- Create: `src/network/index.ts`
- Test: `src/network/__tests__/rpc.test.ts`
- Test: `src/network/__tests__/mock.test.ts`

**types.ts — BlockchainService interface:**
```typescript
export interface BlockchainService {
  listUnspent(address: string): Promise<UTXO[]>
  getUTXO(txid: string, vout: number): Promise<UTXO | null>
  broadcastTx(rawTxHex: string): Promise<string>
  getRawTx(txid: string): Promise<Uint8Array>
  getTxStatus(txid: string): Promise<TxStatus>
  getBlockHeader(blockHash: string): Promise<Uint8Array>
  getMerkleProof(txid: string): Promise<MerkleProof>
  getBestBlockHeight(): Promise<number>
  importAddress(address: string): Promise<void>
}

export interface TxStatus { confirmed: boolean; blockHash: string; blockHeight: number; txIndex: number }
export interface RPCConfig { url: string; user: string; password: string; network: string }
```

**rpc.ts — JSON-RPC 1.0 client:**
```typescript
export class RPCClient implements BlockchainService {
  constructor(config: RPCConfig)
  async call<T>(method: string, params: unknown[]): Promise<T>
  // All BlockchainService methods implemented via JSON-RPC
}
```

Uses `fetch()` with Basic auth header.

**mock.ts — For testing:**
```typescript
export class MockBlockchainService implements BlockchainService {
  utxos: Map<string, UTXO[]>
  txs: Map<string, Uint8Array>
  // All methods return from in-memory maps
}
```

**Network presets:**
```typescript
export const NetworkPresets: Record<string, RPCConfig> = {
  regtest: { url: 'http://localhost:18332', user: 'bitfs', password: 'bitfs', network: 'regtest' },
  testnet: { url: 'http://localhost:18332', user: 'bitfs', password: 'bitfs', network: 'testnet' },
}
```

**Test plan:**
- MockBlockchainService: add UTXOs, listUnspent returns them
- RPCClient: test call method with mocked fetch (Vitest mock)
- Network presets contain regtest and testnet

```bash
git commit -m "feat(network): add BlockchainService interface, RPCClient, and mock"
```

---

## Phase 4: Protocol Layer

### Task 18: x402 — invoice + HTLC + payment

**Files:**
- Create: `src/x402/errors.ts`
- Create: `src/x402/types.ts`
- Create: `src/x402/invoice.ts`
- Create: `src/x402/htlc.ts`
- Create: `src/x402/headers.ts`
- Create: `src/x402/verify.ts`
- Create: `src/x402/index.ts`
- Test: `src/x402/__tests__/invoice.test.ts`
- Test: `src/x402/__tests__/htlc.test.ts`

**invoice.ts:**
```typescript
export function calculatePrice(pricePerKB: bigint, fileSize: bigint): bigint  // ceil division
export function newInvoice(pricePerKB: bigint, fileSize: bigint, paymentAddr: string, capsuleHash: Uint8Array, ttlSeconds: number): Invoice
```

**htlc.ts — HTLC script building:**
```typescript
export function buildHTLC(params: HTLCParams): Uint8Array
export function extractCapsuleHashFromHTLC(script: Uint8Array): Uint8Array
export function extractInvoiceIDFromHTLC(script: Uint8Array): Uint8Array | null
export async function buildHTLCFundingTx(params: HTLCFundingParams): Promise<HTLCFundingResult>
export async function buildSellerClaimTx(params: SellerClaimParams): Promise<Transaction>
```

**headers.ts:**
```typescript
export function parsePaymentHeaders(headers: Headers): PaymentHeaders
export function paymentHeadersFromInvoice(inv: Invoice): PaymentHeaders
```

**verify.ts:**
```typescript
export function verifyPayment(proof: PaymentProof, invoice: Invoice): string  // returns txid
export function parseHTLCPreimage(spendingTx: Uint8Array, expectedCapsuleHash: Uint8Array, fileTxID?: Uint8Array): Uint8Array
```

**Constants:**
```typescript
export const DEFAULT_HTLC_TIMEOUT = 72
export const MIN_HTLC_TIMEOUT = 6
export const MAX_HTLC_TIMEOUT = 288
export const CAPSULE_HASH_LEN = 32
export const INVOICE_ID_LEN = 16
```

**Test plan:**
- calculatePrice: ceil(500 * 2048 / 1024) = 1000
- Invoice creation with TTL, isExpired check
- buildHTLC produces valid script, extractCapsuleHash recovers hash
- Payment verification with mock tx

```bash
git commit -m "feat(x402): add invoice, HTLC script builder, and payment verification"
```

---

### Task 19: paymail — URI parsing + discovery

**Files:**
- Create: `src/paymail/errors.ts`
- Create: `src/paymail/types.ts`
- Create: `src/paymail/uri.ts`
- Create: `src/paymail/discover.ts`
- Create: `src/paymail/brfc.ts`
- Create: `src/paymail/index.ts`
- Test: `src/paymail/__tests__/uri.test.ts`
- Test: `src/paymail/__tests__/brfc.test.ts`

**uri.ts:**
```typescript
export interface ParsedURI {
  type: AddressType
  alias?: string
  domain?: string
  pubKey?: Uint8Array
  path?: string
  rawURI: string
}

export const enum AddressType { Paymail = 0, DNSLink = 1, PubKey = 2 }

export function parseURI(uri: string): ParsedURI
```

Parses: `bitfs://alice@example.com/path`, `bitfs://example.com/path` (DNS), `bitfs://02ab...cd/path` (pubkey)

**discover.ts:**
```typescript
export interface HTTPClient { get(url: string): Promise<Response> }

export async function discoverCapabilities(domain: string, client?: HTTPClient): Promise<PaymailCapabilities>
export async function resolvePKI(alias: string, domain: string, client?: HTTPClient): Promise<Uint8Array>
export async function resolvePaymentDestination(alias: string, domain: string, client?: HTTPClient): Promise<PaymentOutput[]>
```

**brfc.ts:**
```typescript
export function computeBRFCID(title: string, author: string, version: string): string
// Pre-computed
export const BRFC_BITFS_BROWSE = computeBRFCID('BitFS Browse', 'BitFS', '1')
export const BRFC_BITFS_BUY = computeBRFCID('BitFS Buy', 'BitFS', '1')
export const BRFC_BITFS_SELL = computeBRFCID('BitFS Sell', 'BitFS', '1')
```

**Test plan:**
- parseURI: paymail, DNS, pubkey variants
- Invalid URIs throw
- BRFC ID computation is deterministic (SHA256 first 12 hex chars)
- discoverCapabilities with mocked HTTP

```bash
git commit -m "feat(paymail): add URI parsing, capability discovery, and PKI resolution"
```

---

### Task 20: revshare — revenue distribution

**Files:**
- Create: `src/revshare/errors.ts`
- Create: `src/revshare/types.ts`
- Create: `src/revshare/distribute.ts`
- Create: `src/revshare/serialize.ts`
- Create: `src/revshare/index.ts`
- Test: `src/revshare/__tests__/distribute.test.ts`
- Test: `src/revshare/__tests__/serialize.test.ts`

**types.ts:**
```typescript
export interface RevShareEntry { address: Uint8Array; share: bigint }  // address: 20B
export interface RegistryState { nodeID: Uint8Array; totalShares: bigint; entries: RevShareEntry[]; modeFlags: number }
export interface ShareData { nodeID: Uint8Array; amount: bigint }
export interface ISOPoolState { nodeID: Uint8Array; remainingShares: bigint; pricePerShare: bigint; creatorAddr: Uint8Array }
export interface Distribution { address: Uint8Array; amount: bigint }
```

**distribute.ts:**
```typescript
export function distributeRevenue(totalPayment: bigint, entries: RevShareEntry[], totalShares: bigint): Distribution[]
export function validateShareConservation(inputs: ShareData[], outputs: ShareData[]): void
```

Distribution: `amount_i = (totalPayment * share_i) / totalShares`, last entry gets remainder.

**serialize.ts — Fixed-size binary encoding:**
```typescript
// RegistryState: header(44B) + entries(28B each) + trailer(1B)
export function serializeRegistry(state: RegistryState): Uint8Array
export function deserializeRegistry(data: Uint8Array): RegistryState

// ShareData: 40B (nodeID:32 + amount:8)
export function serializeShare(data: ShareData): Uint8Array
export function deserializeShare(data: Uint8Array): ShareData

// ISOPoolState: 68B (nodeID:32 + remaining:8 + price:8 + creator:20)
export function serializeISOPool(state: ISOPoolState): Uint8Array
export function deserializeISOPool(data: Uint8Array): ISOPoolState
```

**Test plan:**
- Distribute 1000 sat among 3 entries (40%, 30%, 30%) → [400, 300, 300]
- Distribute with remainder: 100 sat, 3 equal shares → [33, 33, 34]
- Zero shares throws
- Serialize/deserialize registry round-trip
- **Cross-language vector:** known registry state → identical bytes as Go

```bash
git commit -m "feat(revshare): add revenue distribution and binary serialization"
```

---

## Phase 5: Integration + Polish

### Task 21: Cross-language test vectors

**Files:**
- Create: `src/__tests__/vectors/` directory
- Create: `src/__tests__/crosslang.test.ts`

**Goal:** Generate test vectors from Go, verify TypeScript produces identical output.

**Step 1:** Write a Go program in `libbitfs-go/cmd/testvectors/main.go` that outputs JSON:
- method42: encrypt with known key pair + plaintext → { ciphertext_hex, key_hash_hex }
  (Note: can't compare ciphertext due to random nonce, but CAN verify decrypt)
- method42: ECDH with known keys → shared_secret_hex
- method42: deriveAESKey with known inputs → aes_key_hex
- wallet: derive path from known seed → { pubkey_hex, path }
- metanet: serialize known Node → payload_hex
- metanet: serialize known ChildEntry → entry_hex
- revshare: serialize known RegistryState → registry_hex
- spv: doubleHash of known data → hash_hex

**Step 2:** Run Go program, save output to `src/__tests__/vectors/go-vectors.json`

**Step 3:** Write crosslang.test.ts that loads vectors and verifies:
```typescript
import vectors from './vectors/go-vectors.json'

describe('cross-language compatibility', () => {
  it('ECDH produces same shared secret', () => { /* ... */ })
  it('deriveAESKey produces same key', () => { /* ... */ })
  it('wallet derivation produces same pubkey', () => { /* ... */ })
  it('TLV serialization produces same bytes', () => { /* ... */ })
  it('ChildEntry serialization matches', () => { /* ... */ })
  it('RegistryState serialization matches', () => { /* ... */ })
  it('doubleHash matches', () => { /* ... */ })
})
```

```bash
git commit -m "test: add cross-language compatibility test vectors"
```

---

### Task 22: Package exports + barrel files + final wiring

**Files:**
- Update: `src/index.ts` — re-export all modules
- Update: each module's `index.ts` — ensure all public APIs exported
- Update: `package.json` — verify exports map

**Step 1:** Ensure every module has `src/<module>/index.ts` that re-exports all public types and functions.

**Step 2:** Update root `src/index.ts`:
```typescript
export * as method42 from './method42/index.js'
export * as wallet from './wallet/index.js'
export * as config from './config/index.js'
export * as metanet from './metanet/index.js'
export * as storage from './storage/index.js'
export * as tx from './tx/index.js'
export * as spv from './spv/index.js'
export * as network from './network/index.js'
export * as x402 from './x402/index.js'
export * as paymail from './paymail/index.js'
export * as revshare from './revshare/index.js'
```

**Step 3:** Run full test suite:
```bash
cd libbitfs-ts && bunx vitest run --coverage
```
Expected: All tests pass, coverage >80% per module.

**Step 4:** Run typecheck:
```bash
cd libbitfs-ts && bun run typecheck
```
Expected: No errors.

**Step 5:** Commit
```bash
git commit -m "feat(libbitfs-ts): wire up all module exports and verify full test suite"
```

---

## Summary

| Phase | Tasks | Modules |
|-------|-------|---------|
| 0: Scaffold | 1 | project setup |
| 1: Crypto | 2-8 | method42, wallet, config |
| 2: Data | 9-13 | metanet, storage |
| 3: Blockchain | 14-17 | tx, spv, network |
| 4: Protocol | 18-20 | x402, paymail, revshare |
| 5: Polish | 21-22 | cross-lang vectors, exports |

**Total: 22 tasks, 11 modules, ~60 source files, ~40 test files.**

Each task is independently committable and testable. Wire-format compatibility with libbitfs-go is verified by cross-language test vectors in Task 21.

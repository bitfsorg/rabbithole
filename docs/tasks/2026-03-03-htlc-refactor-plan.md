# HTLC Refactor (x402 → payment + sCrypt + 链上退款) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rename x402 to payment across the entire codebase, replace the hand-built HTLC with an sCrypt smart contract, add on-chain timelock refund, remove pre-signed refund, and make InvoiceID mandatory.

**Architecture:** Three-phase refactor — (A) mechanical rename x402→payment across ~46 files with zero logic changes, (B) sCrypt BitfsHTLC contract in new `contracts/` directory compiled to artifact JSON, (C) replace hand-built HTLC scripts with artifact-loaded scripts in both Go and TS, add OP_PUSH_TX-based on-chain refund, remove 2-of-2 multisig pre-signed refund flow.

**Tech Stack:** Go 1.25 + go-sdk, TypeScript + scrypt-ts ^1.3.31, scrypt-cli, BIP143 sighash preimage

**Protocol Impact:** This is a protocol-breaking change — new sCrypt-compiled locking scripts are incompatible with old hand-built scripts. Acceptable because BitFS is pre-release.

---

## Phase A: Mechanical Rename (x402 → payment)

### Task 1: Rename libbitfs-go/x402/ → payment/

**Files:**
- Rename: `libbitfs-go/x402/` → `libbitfs-go/payment/`
- Rename: `libbitfs-go/x402/x402_test.go` → `libbitfs-go/payment/payment_test.go`
- Modify: all 10 `.go` files in the directory (package declaration)
- Modify: `libbitfs-go/README.md` (package table)

**Step 1: Move the directory**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
git mv x402 payment
```

**Step 2: Rename x402_test.go**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
git mv payment/x402_test.go payment/payment_test.go
```

**Step 3: Update package declarations**

In every `.go` file in `libbitfs-go/payment/`, change:
- `package x402` → `package payment`
- `package x402_test` → `package payment_test`

Files (10 total):
- `errors.go`, `headers.go`, `htlc.go`, `htlc_tx.go`, `invoice.go`, `verify.go`
- `payment_test.go`, `coverage_supplement_test.go`, `coverage_supplement2_test.go`, `htlc_tx_test.go`

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go/payment
sed -i '' 's/^package x402$/package payment/' *.go
sed -i '' 's/^package x402_test$/package payment_test/' *_test.go
```

**Step 4: Update README.md**

In `libbitfs-go/README.md`, replace `x402` with `payment` in the package table.

**Step 5: Update any in-package string references**

In `libbitfs-go/payment/coverage_supplement2_test.go` line 186 and any other string references mentioning "x402", update to "payment".

In `libbitfs-go/payment/errors.go`, the error string prefix "x402:" should become "payment:" if present. Check each error constant.

**Step 6: Run tests**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
go test ./payment/ -count=1 -v 2>&1 | tail -5
```

Expected: all tests pass (logic unchanged, only package name changed).

**Step 7: Commit**

```bash
git add -A && git commit -m "refactor(libbitfs-go): rename x402 package to payment"
```

---

### Task 2: Update Go consumers (bitfs, den-explorer)

**Files:**
- Modify: `bitfs/internal/buy/buy.go` (import)
- Modify: `bitfs/internal/buy/utxo.go` (import)
- Modify: `bitfs/internal/buy/config.go` (import)
- Modify: `bitfs/internal/daemon/payment.go` (import)
- Modify: `bitfs/internal/daemon/payment_test.go` (import)
- Modify: `bitfs/internal/buy/buy_test.go` (import)
- Modify: `bitfs/e2e/06_paid_purchase_test.go` (import)
- Modify: `bitfs/e2e/18_daemon_buy_api_test.go` (import)
- Modify: `bitfs/e2e/19_daemon_content_neg_test.go` (import + usage)
- Modify: `bitfs/cmd/bmget/main_test.go` (import)
- Modify: `bitfs/integration/payment_flow_test.go` (import)
- Modify: `bitfs/integration/payment_flow_extra_test.go` (import)
- Modify: `bitfs/integration/security_test.go` (import)
- Modify: `bitfs/CLAUDE.md` (text reference)
- Rename: `den-explorer/x402.go` → `den-explorer/payment_analysis.go`
- Rename: `den-explorer/x402_test.go` → `den-explorer/payment_analysis_test.go`
- Rename: `den-explorer/templates/x402.html` → `den-explorer/templates/payment.html`
- Modify: `den-explorer/handlers.go` (route, template name, title)
- Modify: `den-explorer/templates.go` (template file reference)
- Modify: `den-explorer/templates/tx.html` (link)
- Modify: `den-explorer/templates/metanet.html` (link)

**Step 1: Update all bitfs/ imports**

In every file listed above, change the import path:
```
"github.com/bitfsorg/libbitfs-go/x402"  →  "github.com/bitfsorg/libbitfs-go/payment"
```

And update all qualified references:
```
x402.NewInvoice       →  payment.NewInvoice
x402.SetPaymentHeaders →  payment.SetPaymentHeaders
x402.BuildHTLC        →  payment.BuildHTLC
x402.VerifyPayment    →  payment.VerifyPayment
x402.HTLCUTXO         →  payment.HTLCUTXO
```
(etc. for all `x402.` prefixes)

If the import uses an alias like `"github.com/bitfsorg/libbitfs-go/x402"` (qualified as `x402`), change to `"github.com/bitfsorg/libbitfs-go/payment"` (qualified as `payment`).

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
# Update import paths
find . -name '*.go' -exec grep -l 'libbitfs-go/x402' {} \; | \
  xargs sed -i '' 's|libbitfs-go/x402|libbitfs-go/payment|g'
# Update package qualifiers (x402. → payment.)
find . -name '*.go' -exec grep -l 'x402\.' {} \; | \
  xargs sed -i '' 's/x402\./payment./g'
```

**Step 2: Rename and update den-explorer files**

```bash
cd /Users/alex/Codes/RabbitHole/den-explorer
git mv x402.go payment_analysis.go
git mv x402_test.go payment_analysis_test.go
git mv templates/x402.html templates/payment.html
```

In `payment_analysis.go`:
- Update function name: `AnalyzeX402` → `AnalyzePayment`
- Update struct comment: "x402 HTLC analysis" → "payment HTLC analysis"
- Update error string: "no x402 HTLC output" → "no payment HTLC output"
- Update comment: "x402 HTLC script pattern" → "payment HTLC script pattern"

In `handlers.go`:
- Update route handler: reference `AnalyzePayment` instead of `AnalyzeX402`
- Update template name: `"x402.html"` → `"payment.html"`
- Update page title: `"x402 Payment Analysis"` → `"Payment Analysis"`

In `templates.go`:
- Update template list: `"x402.html"` → `"payment.html"`

In `templates/payment.html`:
- Update breadcrumb text and title

In `templates/tx.html`:
- Update link: `/x402/...` → `/payment/...` (if route changes)

In `templates/metanet.html`:
- Update link similarly

**Step 3: Update CLAUDE.md**

In `bitfs/CLAUDE.md`, replace "x402" references with "payment".

**Step 4: Verify Go builds and tests**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
go build ./...
go test ./internal/buy/ ./internal/daemon/ -count=1 -v 2>&1 | tail -10

cd /Users/alex/Codes/RabbitHole/den-explorer
go build ./...
go test ./... -count=1 -v 2>&1 | tail -5
```

Expected: all pass.

**Step 5: Commit**

```bash
cd /Users/alex/Codes/RabbitHole
git add bitfs/ den-explorer/
git commit -m "refactor(bitfs,den-explorer): update x402 → payment imports and references"
```

---

### Task 3: Rename libbitfs-ts/src/x402/ → payment/

**Files:**
- Rename: `libbitfs-ts/src/x402/` → `libbitfs-ts/src/payment/`
- Modify: `libbitfs-ts/src/index.ts` (export path)
- Modify: `libbitfs-ts/package.json` (exports map)
- Modify: `libbitfs-ts/src/__tests__/crosslang.test.ts` (imports)
- Modify: `libbitfs-ts/README.md`
- Modify: all files inside `payment/` that reference "x402" in strings/comments

**Step 1: Move directory**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-ts
git mv src/x402 src/payment
```

**Step 2: Update main export**

In `libbitfs-ts/src/index.ts` line 11:
```typescript
// Before:
export * as x402 from './x402/index.js'
// After:
export * as payment from './payment/index.js'
```

**Step 3: Update package.json exports**

In `libbitfs-ts/package.json`, replace the `"./x402"` export entry:
```json
"./payment": {
  "types": "./dist/payment/index.d.ts",
  "import": "./dist/payment/index.js"
}
```

Remove the `"./x402"` entry entirely.

**Step 4: Update cross-language test imports**

In `libbitfs-ts/src/__tests__/crosslang.test.ts`:
```typescript
// Before:
import { buildHTLC, extractCapsuleHashFromHTLC, extractInvoiceIDFromHTLC } from '../x402/htlc.js'
import type { HTLCParams } from '../x402/types.js'
// After:
import { buildHTLC, extractCapsuleHashFromHTLC, extractInvoiceIDFromHTLC } from '../payment/htlc.js'
import type { HTLCParams } from '../payment/types.js'
```

**Step 5: Update internal string references**

In `libbitfs-ts/src/payment/index.ts` (was x402/index.ts):
- Update doc comments mentioning "x402"

In `libbitfs-ts/src/payment/types.ts`:
- Update doc comment on line 174 mentioning "x402"

In `libbitfs-ts/src/payment/headers.ts`:
- Update doc comments

In `libbitfs-ts/src/payment/__tests__/refund.test.ts`:
- Update test suite description on line 135

In `libbitfs-ts/src/payment/errors.ts`:
- If error codes/messages contain "x402", update to "payment"

**Step 6: Update README.md**

In `libbitfs-ts/README.md`, replace "x402" with "payment" in the module table and examples.

**Step 7: Build and test**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-ts
npm run build
npm test 2>&1 | tail -10
```

Expected: all 875 tests pass.

**Step 8: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-ts
git add -A && git commit -m "refactor(libbitfs-ts): rename x402 package to payment"
```

---

### Task 4: Update TS consumers (bitfs-extension, bitfs-app)

**Files:**
- Modify: `bitfs-extension/src/lib/messages.ts` (comment on line 102)
- Check: `bitfs-app/` for any x402 references

**Step 1: Update bitfs-extension**

In `bitfs-extension/src/lib/messages.ts` line 102:
```typescript
// Before: // x402 Payment
// After:  // Payment
```

Search for any other x402 references:
```bash
cd /Users/alex/Codes/RabbitHole/bitfs-extension
grep -r 'x402' --include='*.ts' --include='*.tsx' --include='*.json' .
```

Update all found references.

**Step 2: Check bitfs-app**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs-app
grep -r 'x402' --include='*.ts' --include='*.tsx' --include='*.json' --include='*.dart' .
```

Update any references found.

**Step 3: Commit**

```bash
cd /Users/alex/Codes/RabbitHole
git add bitfs-extension/ bitfs-app/
git commit -m "refactor(extension,app): update x402 → payment references"
```

---

### Task 5: Update documentation (specs, design, whitepaper, websites)

**Files (12+ documents):**

**Specs:**
- Rename: `docs/specs/bitfs/08-x402.md` → `docs/specs/bitfs/08-payment.md`
- Modify: `docs/specs/bitfs/08-payment.md` (title, package references)
- Modify: `docs/specs/bitfs/09-daemon.md` (x402 → payment in config type names, dependency refs)
- Modify: `docs/specs/bitfs/10-cmd-bitfs.md` (dependency reference)
- Modify: `docs/specs/bitfs/03-metanet.md` (comment text)

**Design:**
- Modify: `docs/design/OverallDesign.zh.md` (6 references on lines 23, 32, 69, 92, 118, 149)

**Whitepaper:**
- Modify: `docs/whitepaper/BitFS-Whitepaper-Outline.md` (line 27)
- Modify: `docs/whitepaper/Metanet-Whitepaper-Outline.md` (~15 references)
- Modify: `docs/whitepaper/BitFS-Whitepaper.en.tex` (lines 112, 316, 320)
- Modify: `docs/whitepaper/Metanet-Whitepaper.en.tex` (line 120)

**Websites:**
- Modify: `websites/bitfs.org/bitfs-transactions.html` (7 references)
- Modify: `websites/metanet.org/Website-Content-Outline.md` (3 references)
- Modify: `websites/metanet.org/index.html` (3 references)
- Modify: `websites/metanet.org/index.zh.html` (3 references)

**Step 1: Rename spec file**

```bash
cd /Users/alex/Codes/RabbitHole
git mv docs/specs/bitfs/08-x402.md docs/specs/bitfs/08-payment.md
```

**Step 2: Batch text replacement**

For each file, replace:
- `x402` → `payment` (in module/package/path contexts)
- `X402` → `Payment` (in type name contexts, e.g. `X402Config` → `PaymentConfig`)
- Keep `HTTP 402` and `X-Price`/`X-Invoice-Id` header names unchanged (these are protocol standards)

Strategy: for each file, use targeted sed/edit commands. Do NOT blindly replace all "x402" — some contexts need `payment`, others need `Payment`, and HTTP 402 references must be preserved.

Specific replacements per file:

**docs/specs:**
- `08-payment.md`: title line `x402` → `payment`, package path `libbitfs-go/x402` → `libbitfs-go/payment`, type `X402Config` → `PaymentConfig`
- `09-daemon.md`: `X402Config` → `PaymentConfig`, `libbitfs-go/x402` → `libbitfs-go/payment`
- `10-cmd-bitfs.md`: `libbitfs-go/x402` → `libbitfs-go/payment`
- `03-metanet.md`: `via x402` → `via payment protocol`

**docs/design/OverallDesign.zh.md:**
- `x402 检索费` → `payment 检索费`
- `x402/HTLC` → `payment/HTLC`
- `├── x402/` → `├── payment/`
- `x402 付费墙` → `payment 付费墙`

**docs/whitepaper:** Replace `x402` with `payment` in all outline and tex files. Preserve "HTTP 402" if present.

**websites:** Replace display text `x402` with `Payment` or appropriate term in HTML.

**Step 3: Verify no stale references**

```bash
cd /Users/alex/Codes/RabbitHole
grep -r 'x402' --include='*.md' --include='*.tex' --include='*.html' docs/ websites/
```

Expected: zero results (except possibly deferred.md which documents the rename decision).

**Step 4: Update deferred.md P1 section header**

In `docs/tasks/deferred.md`, mark the rename as completed within the P1 section.

**Step 5: Commit**

```bash
cd /Users/alex/Codes/RabbitHole
git add docs/ websites/
git commit -m "docs: rename x402 → payment across all specs, design docs, whitepapers, and websites"
```

---

### Task 6: Update CLAUDE.md files and verify Phase A

**Files:**
- Modify: `CLAUDE.md` (root) — update references to x402 package, spec file name
- Modify: `metanet/CLAUDE.md` — update x402 references
- Modify: any other CLAUDE.md that mentions x402

**Step 1: Search and update**

```bash
grep -r 'x402' --include='CLAUDE.md' /Users/alex/Codes/RabbitHole/
```

Update all found references.

**Step 2: Full verification**

```bash
# Go
cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./payment/ -count=1
cd /Users/alex/Codes/RabbitHole/bitfs && go build ./... && go test ./internal/... -count=1
cd /Users/alex/Codes/RabbitHole/den-explorer && go build ./...

# TypeScript
cd /Users/alex/Codes/RabbitHole/libbitfs-ts && npm run build && npm test
```

**Step 3: Global check for stale x402 references**

```bash
cd /Users/alex/Codes/RabbitHole
grep -r 'x402' --include='*.go' --include='*.ts' --include='*.tsx' --include='*.json' \
  --include='*.md' --include='*.tex' --include='*.html' \
  --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git .
```

Only `docs/tasks/deferred.md` (documenting the decision) should remain.

**Step 4: Commit**

```bash
git add -A && git commit -m "refactor: complete x402 → payment rename across entire codebase"
```

---

## Phase B: sCrypt Contract

### Task 7: Set up contracts/ project

**Files:**
- Create: `contracts/package.json`
- Create: `contracts/tsconfig.json`
- Create: `contracts/src/` (directory)
- Create: `contracts/tests/` (directory)
- Create: `contracts/artifacts/` (directory, with .gitkeep)

**Step 1: Initialize project**

```bash
cd /Users/alex/Codes/RabbitHole
mkdir -p contracts/src contracts/tests contracts/artifacts
```

**Step 2: Create package.json**

```json
{
  "name": "@bitfs/contracts",
  "version": "0.1.0",
  "description": "BitFS/Metanet on-chain logic — sCrypt smart contracts",
  "private": true,
  "scripts": {
    "compile": "npx scrypt-cli compile",
    "test": "npx jest --config jest.config.json"
  },
  "dependencies": {
    "scrypt-ts": "^1.3.31"
  },
  "devDependencies": {
    "scrypt-cli": "^0.2.3",
    "@types/jest": "^29.5.0",
    "jest": "^29.7.0",
    "ts-jest": "^29.1.0",
    "typescript": "^5.3.3"
  }
}
```

**Step 3: Create tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "es2021",
    "lib": ["es2021"],
    "experimentalDecorators": true,
    "module": "commonjs",
    "moduleResolution": "node",
    "outDir": "./dist",
    "esModuleInterop": true,
    "strict": true,
    "skipLibCheck": true,
    "sourceMap": true,
    "declaration": true
  },
  "include": ["src/**/*.ts"]
}
```

**Step 4: Create jest.config.json**

```json
{
  "preset": "ts-jest",
  "testEnvironment": "node",
  "roots": ["<rootDir>/tests"]
}
```

**Step 5: Install dependencies**

```bash
cd /Users/alex/Codes/RabbitHole/contracts
npm install
```

**Step 6: Verify scrypt-cli works**

```bash
cd /Users/alex/Codes/RabbitHole/contracts
npx scrypt-cli compile --help
```

**Step 7: Commit**

```bash
cd /Users/alex/Codes/RabbitHole
git add contracts/
git commit -m "feat(contracts): scaffold sCrypt project for on-chain logic"
```

---

### Task 8: Write BitfsHTLC sCrypt contract

**Files:**
- Create: `contracts/src/bitfsHTLC.ts`

**Step 1: Write the contract**

```typescript
import {
    assert,
    ByteString,
    hash160,
    method,
    prop,
    PubKey,
    PubKeyHash,
    sha256,
    Sha256,
    Sig,
    SmartContract,
} from 'scrypt-ts'

export class BitfsHTLC extends SmartContract {
    // 16-byte invoice ID — mandatory, ensures unique script hash per payment
    @prop()
    readonly invoiceId: ByteString

    // SHA256(capsule) — the hash lock
    @prop()
    readonly capsuleHash: Sha256

    // Seller's public key hash (20 bytes)
    @prop()
    readonly sellerPkh: PubKeyHash

    // Buyer's public key hash (20 bytes)
    @prop()
    readonly buyerPkh: PubKeyHash

    // Refund timeout (block height)
    @prop()
    readonly timeout: bigint

    constructor(
        invoiceId: ByteString,
        capsuleHash: Sha256,
        sellerPkh: PubKeyHash,
        buyerPkh: PubKeyHash,
        timeout: bigint
    ) {
        super(...arguments)
        this.invoiceId = invoiceId
        this.capsuleHash = capsuleHash
        this.sellerPkh = sellerPkh
        this.buyerPkh = buyerPkh
        this.timeout = timeout
    }

    // Seller claims by revealing the capsule preimage
    @method()
    public claim(preimage: ByteString, sig: Sig, pubkey: PubKey) {
        // Verify hash lock
        assert(sha256(preimage) == this.capsuleHash, 'wrong capsule')
        // Verify seller identity
        assert(hash160(pubkey) == this.sellerPkh, 'wrong seller')
        // Verify signature
        assert(this.checkSig(sig, pubkey), 'invalid sig')
    }

    // Buyer refunds after timeout (on-chain, no seller cooperation needed)
    @method()
    public refund(sig: Sig, pubkey: PubKey) {
        // Verify timeout has passed (uses OP_PUSH_TX for nLockTime check)
        assert(this.timeLock(this.timeout), 'too early')
        // Verify buyer identity
        assert(hash160(pubkey) == this.buyerPkh, 'wrong buyer')
        // Verify signature
        assert(this.checkSig(sig, pubkey), 'invalid sig')
    }
}
```

**Step 2: Compile**

```bash
cd /Users/alex/Codes/RabbitHole/contracts
npx scrypt-cli compile
```

Expected: `artifacts/BitfsHTLC.json` is generated.

**Step 3: Verify artifact structure**

```bash
cd /Users/alex/Codes/RabbitHole/contracts
cat artifacts/BitfsHTLC.json | python3 -m json.tool | head -30
```

Verify the artifact contains:
- `hex` field with `<invoiceId>`, `<capsuleHash>`, `<sellerPkh>`, `<buyerPkh>`, `<timeout>` placeholders
- `abi` array with `constructor`, `claim`, and `refund` entries
- `abi[claim].params` = `[preimage: ByteString, sig: Sig, pubkey: PubKey]`
- `abi[refund].params` = `[sig: Sig, pubkey: PubKey]`

**Step 4: Commit**

```bash
cd /Users/alex/Codes/RabbitHole
git add contracts/
git commit -m "feat(contracts): add BitfsHTLC sCrypt contract with claim + on-chain refund"
```

---

### Task 9: Write BitfsHTLC contract tests

**Files:**
- Create: `contracts/tests/bitfsHTLC.test.ts`

**Step 1: Write tests**

```typescript
import { BitfsHTLC } from '../src/bitfsHTLC'
import {
    bsv,
    ByteString,
    findSig,
    MethodCallOptions,
    PubKey,
    PubKeyHash,
    sha256,
    Sha256,
    TestWallet,
    toByteString,
    toHex,
} from 'scrypt-ts'

describe('BitfsHTLC', () => {
    let seller: bsv.PrivateKey
    let buyer: bsv.PrivateKey
    let capsule: ByteString
    let capsuleHash: Sha256
    let invoiceId: ByteString
    let instance: BitfsHTLC

    beforeAll(async () => {
        await BitfsHTLC.loadArtifact()

        seller = bsv.PrivateKey.fromRandom(bsv.Networks.testnet)
        buyer = bsv.PrivateKey.fromRandom(bsv.Networks.testnet)

        // 32-byte capsule preimage
        capsule = toByteString('a'.repeat(64))
        capsuleHash = sha256(capsule)

        // 16-byte invoice ID
        invoiceId = toByteString('b'.repeat(32))
    })

    beforeEach(() => {
        instance = new BitfsHTLC(
            invoiceId,
            capsuleHash,
            PubKeyHash(toHex(bsv.crypto.Hash.sha256ripemd160(seller.publicKey.toBuffer()))),
            PubKeyHash(toHex(bsv.crypto.Hash.sha256ripemd160(buyer.publicKey.toBuffer()))),
            72n // timeout = 72 blocks
        )
    })

    describe('claim', () => {
        it('should succeed with correct capsule preimage and seller sig', async () => {
            // Test via local verification (no deployment needed)
            const result = instance.verify((self) => {
                self.claim(capsule, findSig(seller), PubKey(toHex(seller.publicKey)))
            })
            expect(result.success).toBe(true)
        })

        it('should fail with wrong capsule preimage', () => {
            const wrongCapsule = toByteString('c'.repeat(64))
            expect(() => {
                instance.verify((self) => {
                    self.claim(wrongCapsule, findSig(seller), PubKey(toHex(seller.publicKey)))
                })
            }).toThrow(/wrong capsule/)
        })

        it('should fail with wrong seller pubkey', () => {
            expect(() => {
                instance.verify((self) => {
                    self.claim(capsule, findSig(buyer), PubKey(toHex(buyer.publicKey)))
                })
            }).toThrow(/wrong seller/)
        })
    })

    describe('refund', () => {
        it('should succeed after timeout with buyer sig', async () => {
            const result = instance.verify((self) => {
                self.refund(findSig(buyer), PubKey(toHex(buyer.publicKey)))
            }, { lockTime: 100 }) // 100 > 72 (timeout)
            expect(result.success).toBe(true)
        })

        it('should fail before timeout', () => {
            expect(() => {
                instance.verify((self) => {
                    self.refund(findSig(buyer), PubKey(toHex(buyer.publicKey)))
                }, { lockTime: 50 }) // 50 < 72
            }).toThrow(/too early/)
        })

        it('should fail with wrong buyer pubkey', () => {
            expect(() => {
                instance.verify((self) => {
                    self.refund(findSig(seller), PubKey(toHex(seller.publicKey)))
                }, { lockTime: 100 })
            }).toThrow(/wrong buyer/)
        })
    })
})
```

> **Note:** The exact test API (`verify`, `findSig`, `MethodCallOptions.lockTime`) depends on the scrypt-ts version. Adjust based on actual SDK API after installation. The sCrypt docs at https://docs.scrypt.io/bsv-docs/how-to-test-a-contract/ are the authoritative reference.

**Step 2: Run tests**

```bash
cd /Users/alex/Codes/RabbitHole/contracts
npm test
```

Expected: all tests pass.

**Step 3: Commit**

```bash
cd /Users/alex/Codes/RabbitHole
git add contracts/tests/
git commit -m "test(contracts): add BitfsHTLC claim + refund tests"
```

---

## Phase C: Replace Go HTLC Logic

### Task 10: Go artifact loader

**Files:**
- Create: `libbitfs-go/payment/artifact.go`
- Create: `libbitfs-go/payment/artifact_test.go`
- Copy: `contracts/artifacts/BitfsHTLC.json` → `libbitfs-go/payment/artifacts/BitfsHTLC.json`

**Step 1: Copy artifact**

```bash
mkdir -p /Users/alex/Codes/RabbitHole/libbitfs-go/payment/artifacts
cp /Users/alex/Codes/RabbitHole/contracts/artifacts/BitfsHTLC.json \
   /Users/alex/Codes/RabbitHole/libbitfs-go/payment/artifacts/
```

**Step 2: Write failing test**

```go
// artifact_test.go
package payment

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestLoadArtifact(t *testing.T) {
    art, err := LoadArtifact()
    require.NoError(t, err)
    assert.NotEmpty(t, art.Hex)
    assert.NotEmpty(t, art.ABI)
}

func TestInstantiateHTLC(t *testing.T) {
    art, err := LoadArtifact()
    require.NoError(t, err)

    invoiceID := make([]byte, 16)
    capsuleHash := make([]byte, 32)
    sellerPkh := make([]byte, 20)
    buyerPkh := make([]byte, 20)
    timeout := uint32(72)

    script, err := art.Instantiate(invoiceID, capsuleHash, sellerPkh, buyerPkh, timeout)
    require.NoError(t, err)
    assert.NotEmpty(t, script)
}
```

**Step 3: Run test to verify it fails**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
go test ./payment/ -run TestLoadArtifact -v
```

Expected: FAIL (function not defined)

**Step 4: Write artifact loader**

```go
// artifact.go
package payment

import (
    _ "embed"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "strings"
)

//go:embed artifacts/BitfsHTLC.json
var htlcArtifactJSON []byte

// Artifact represents a compiled sCrypt contract artifact.
type Artifact struct {
    Hex string      `json:"hex"`
    ABI []ABIEntity `json:"abi"`
}

// ABIEntity describes a constructor or public function in the contract.
type ABIEntity struct {
    Name   string       `json:"name"`
    Type   string       `json:"type"` // "constructor" or "function"
    Params []ParamEntry `json:"params"`
    Index  int          `json:"index"`
}

// ParamEntry describes a parameter in the ABI.
type ParamEntry struct {
    Name string `json:"name"`
    Type string `json:"type"` // "bytes", "PubKeyHash", "Sha256", "int", etc.
}

// LoadArtifact loads the embedded BitfsHTLC artifact.
func LoadArtifact() (*Artifact, error) {
    var art Artifact
    if err := json.Unmarshal(htlcArtifactJSON, &art); err != nil {
        return nil, fmt.Errorf("parse artifact: %w", err)
    }
    if art.Hex == "" {
        return nil, fmt.Errorf("artifact has empty hex template")
    }
    return &art, nil
}

// Instantiate substitutes constructor parameters into the hex template
// and returns the locking script bytes.
//
// Parameter encoding follows sCrypt's convention:
//   - ByteString/PubKeyHash/Sha256: raw hex (no length prefix in template)
//   - int/bigint: little-endian signed magnitude
func (a *Artifact) Instantiate(
    invoiceID []byte, // 16 bytes
    capsuleHash []byte, // 32 bytes
    sellerPkh []byte, // 20 bytes
    buyerPkh []byte, // 20 bytes
    timeout uint32,
) ([]byte, error) {
    if len(invoiceID) != InvoiceIDLen {
        return nil, fmt.Errorf("invoiceID must be %d bytes", InvoiceIDLen)
    }
    if len(capsuleHash) != CapsuleHashLen {
        return nil, fmt.Errorf("capsuleHash must be %d bytes", CapsuleHashLen)
    }
    if len(sellerPkh) != PubKeyHashLen {
        return nil, fmt.Errorf("sellerPkh must be %d bytes", PubKeyHashLen)
    }
    if len(buyerPkh) != PubKeyHashLen {
        return nil, fmt.Errorf("buyerPkh must be %d bytes", PubKeyHashLen)
    }

    script := a.Hex
    script = strings.Replace(script, "<invoiceId>", hex.EncodeToString(invoiceID), 1)
    script = strings.Replace(script, "<capsuleHash>", hex.EncodeToString(capsuleHash), 1)
    script = strings.Replace(script, "<sellerPkh>", hex.EncodeToString(sellerPkh), 1)
    script = strings.Replace(script, "<buyerPkh>", hex.EncodeToString(buyerPkh), 1)
    script = strings.Replace(script, "<timeout>", encodeScryptInt(int64(timeout)), 1)

    return hex.DecodeString(script)
}

// encodeScryptInt encodes an integer as sCrypt's little-endian signed magnitude hex.
func encodeScryptInt(v int64) string {
    if v == 0 {
        return "00"
    }
    negative := v < 0
    if negative {
        v = -v
    }
    var buf []byte
    for v > 0 {
        buf = append(buf, byte(v&0xff))
        v >>= 8
    }
    // If high bit set, add padding byte
    if buf[len(buf)-1]&0x80 != 0 {
        if negative {
            buf = append(buf, 0x80)
        } else {
            buf = append(buf, 0x00)
        }
    } else if negative {
        buf[len(buf)-1] |= 0x80
    }
    return hex.EncodeToString(buf)
}
```

> **Note:** The exact placeholder names (`<invoiceId>`, `<capsuleHash>`, etc.) and encoding format must be verified against the actual compiled artifact from Task 8. The sCrypt compiler may use different naming conventions. Adjust the `Instantiate` method after examining the real artifact.

**Step 5: Run test**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
go test ./payment/ -run TestLoadArtifact -v
go test ./payment/ -run TestInstantiateHTLC -v
```

Expected: PASS

**Step 6: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
git add payment/artifact.go payment/artifact_test.go payment/artifacts/
git commit -m "feat(payment): add sCrypt artifact loader with go:embed"
```

---

### Task 11: Replace Go BuildHTLC with artifact-based script

**Files:**
- Modify: `libbitfs-go/payment/htlc.go`
- Modify: `libbitfs-go/payment/payment_test.go` (was x402_test.go)

**Step 1: Rewrite BuildHTLC**

Replace the hand-built opcode assembly in `BuildHTLC()` with:

```go
// BuildHTLC creates an HTLC locking script from the compiled sCrypt artifact.
// InvoiceID is mandatory (16 bytes).
func BuildHTLC(params *HTLCParams) ([]byte, error) {
    if params == nil {
        return nil, ErrInvalidParams
    }
    if len(params.InvoiceID) != InvoiceIDLen {
        return nil, fmt.Errorf("%w: invoiceID is mandatory (%d bytes)", ErrInvalidParams, InvoiceIDLen)
    }
    if len(params.SellerAddr) != PubKeyHashLen {
        return nil, fmt.Errorf("%w: invalid seller address hash length", ErrInvalidParams)
    }
    if len(params.BuyerPubKey) != CompressedPubKeyLen {
        return nil, fmt.Errorf("%w: invalid buyer public key length", ErrInvalidParams)
    }
    if len(params.CapsuleHash) != CapsuleHashLen {
        return nil, fmt.Errorf("%w: invalid capsule hash length", ErrInvalidParams)
    }
    if params.Amount == 0 {
        return nil, fmt.Errorf("%w: amount must be positive", ErrInvalidParams)
    }
    if params.Timeout < MinHTLCTimeout || params.Timeout > MaxHTLCTimeout {
        return nil, fmt.Errorf("%w: timeout must be between %d and %d", ErrInvalidParams, MinHTLCTimeout, MaxHTLCTimeout)
    }

    // Derive buyer PKH from buyer public key
    buyerPkh := hash160(params.BuyerPubKey)

    art, err := LoadArtifact()
    if err != nil {
        return nil, fmt.Errorf("%w: %v", ErrHTLCBuildFailed, err)
    }

    return art.Instantiate(params.InvoiceID, params.CapsuleHash, params.SellerAddr, buyerPkh, params.Timeout)
}
```

> **Key change:** `params.InvoiceID` is now **mandatory** (was optional). Callers that passed nil InvoiceID must be updated to always provide one.

**Step 2: Remove the old hand-built script assembly**

Delete the old opcode-by-opcode script building code from `htlc.go`. Keep `ExtractCapsuleHashFromHTLC` and `ExtractInvoiceIDFromHTLC` but update them to parse the sCrypt-compiled format (or remove them if no longer needed — consumers should use the artifact ABI instead).

> **Note:** `ExtractCapsuleHashFromHTLC` and `ExtractInvoiceIDFromHTLC` are used by the daemon to parse scripts from on-chain transactions. These functions need to be rewritten to parse the sCrypt-compiled script format. The exact byte offsets depend on the compiled artifact. This will be finalized during implementation.

**Step 3: Update tests**

All tests that verify specific byte patterns of the old hand-built script need updating. Tests that verify the *behavior* (correct capsule hash extraction, correct InvoiceID extraction) remain valid but may need new expected values.

**Step 4: Run tests**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
go test ./payment/ -count=1 -v 2>&1 | tail -20
```

**Step 5: Commit**

```bash
git add payment/htlc.go payment/payment_test.go
git commit -m "feat(payment): replace hand-built HTLC with sCrypt artifact-loaded script"
```

---

### Task 12: Go on-chain refund + remove pre-signed refund

**Files:**
- Modify: `libbitfs-go/payment/htlc_tx.go`
- Modify: `libbitfs-go/payment/htlc_tx_test.go`

**Step 1: Write failing test for on-chain refund**

```go
func TestBuildBuyerOnChainRefundTx(t *testing.T) {
    // Set up keys
    buyerPriv, _ := ec.NewPrivateKey()
    sellerPriv, _ := ec.NewPrivateKey()

    invoiceID := randomBytes(16)
    capsuleHash := sha256Hash(randomBytes(32))

    // Build HTLC
    htlcScript, err := BuildHTLC(&HTLCParams{
        BuyerPubKey:  buyerPriv.PubKey().SerialiseCompressed(),
        SellerPubKey: sellerPriv.PubKey().SerialiseCompressed(),
        SellerAddr:   hash160(sellerPriv.PubKey().SerialiseCompressed()),
        CapsuleHash:  capsuleHash,
        Amount:       10000,
        Timeout:      72,
        InvoiceID:    invoiceID,
    })
    require.NoError(t, err)

    // Build refund (buyer only, no seller involvement)
    refundTx, err := BuildBuyerRefundTx(&BuyerRefundParams{
        FundingTxID:   randomBytes(32),
        FundingVout:   0,
        FundingAmount: 10000,
        HTLCScript:    htlcScript,
        BuyerPrivKey:  buyerPriv,
        OutputAddr:    hash160(buyerPriv.PubKey().SerialiseCompressed()),
        Timeout:       72,
    })
    require.NoError(t, err)
    require.NotNil(t, refundTx)

    // Verify nLockTime is set
    assert.Equal(t, uint32(72), refundTx.LockTime)
    // Verify sequence enables nLockTime
    assert.Equal(t, uint32(0xfffffffe), refundTx.Inputs[0].SequenceNumber)
}
```

**Step 2: Rewrite BuyerRefundParams (remove seller dependency)**

```go
// BuyerRefundParams holds parameters for building an on-chain refund transaction.
// The buyer can refund unilaterally after the timeout — no seller cooperation needed.
type BuyerRefundParams struct {
    FundingTxID   []byte         // 32-byte HTLC funding tx hash
    FundingVout   uint32         // HTLC output index
    FundingAmount uint64         // HTLC output amount
    HTLCScript    []byte         // HTLC locking script bytes
    BuyerPrivKey  *ec.PrivateKey // Signs the refund
    OutputAddr    []byte         // 20-byte destination P2PKH hash
    Timeout       uint32         // Block height for nLockTime
    FeeRate       uint64         // Satoshis per byte (0 = default)
}
```

**Step 3: Implement BuildBuyerRefundTx (on-chain version)**

```go
// BuildBuyerRefundTx creates a refund transaction that spends the HTLC
// via the refund path. The buyer signs unilaterally — no seller cooperation needed.
// The transaction's nLockTime is set to the timeout, enforced by consensus.
//
// The unlocking script calls the contract's refund() method:
//   <sighash_preimage> <sig> <buyer_pubkey> <method_index=1>
func BuildBuyerRefundTx(params *BuyerRefundParams) (*transaction.Transaction, error) {
    // ... validation ...
    // ... build transaction with nLockTime = params.Timeout ...
    // ... set sequence = 0xfffffffe ...
    // ... compute BIP143 sighash preimage via CalcInputPreimage() ...
    // ... sign with buyer key ...
    // ... construct unlocking script with preimage + sig + pubkey + method index ...
    // Note: exact unlocking script format depends on sCrypt compilation output.
    // The method index for refund() and the sighash preimage placement will be
    // determined from the artifact ABI during implementation.
}
```

> **Critical:** The sighash preimage is obtained via `tx.CalcInputPreimage(inputIdx, sighashFlag)` from go-sdk. This must be included in the unlocking script for sCrypt's OP_PUSH_TX verification. The exact byte position in the unlocking script depends on how sCrypt compiles the `refund` method.

**Step 4: Delete pre-signed refund code**

Remove from `htlc_tx.go`:
- `SellerPreSignParams` struct
- `SellerPreSignResult` struct
- `BuildSellerPreSignedRefund()` function
- Old `BuildBuyerRefundTx()` function (that took `BuyerRefundParams` with `SellerPreSignedTx` and `SellerSig`)

**Step 5: Update BuildSellerClaimTx**

Update the claim transaction builder to produce unlocking scripts compatible with the sCrypt-compiled contract. The claim method doesn't use `this.ctx`, so no sighash preimage is needed:

```go
// Unlocking script for sCrypt claim() method:
//   <preimage_data> <sig> <seller_pubkey> <method_index=0>
```

> **Note:** Verify the exact unlocking script format by examining the compiled artifact and testing against the sCrypt contract tests.

**Step 6: Run tests**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
go test ./payment/ -count=1 -v 2>&1 | tail -20
```

**Step 7: Commit**

```bash
git add payment/htlc_tx.go payment/htlc_tx_test.go
git commit -m "feat(payment): on-chain buyer refund, remove pre-signed refund flow"
```

---

### Task 13: Go — mandatory InvoiceID + cleanup + tests

**Files:**
- Modify: `libbitfs-go/payment/htlc.go` (HTLCParams validation)
- Modify: `libbitfs-go/payment/htlc_tx.go` (HTLCFundingParams validation)
- Modify: all test files to always provide InvoiceID
- Delete: tests for optional InvoiceID behavior

**Step 1: Make InvoiceID required in HTLCParams**

In `htlc.go`, the `HTLCParams.InvoiceID` field becomes mandatory. Remove the "optional" comment and add validation:

```go
type HTLCParams struct {
    BuyerPubKey  []byte // 33-byte compressed public key
    SellerPubKey []byte // 33-byte compressed public key
    SellerAddr   []byte // 20-byte P2PKH hash
    CapsuleHash  []byte // 32 bytes
    Amount       uint64 // satoshis
    Timeout      uint32 // blocks [MinHTLCTimeout, MaxHTLCTimeout]
    InvoiceID    []byte // 16 bytes, mandatory — ensures unique script hash per payment
}
```

Similarly in `HTLCFundingParams`.

**Step 2: Remove legacy format handling**

In `ExtractCapsuleHashFromHTLC` and `ExtractInvoiceIDFromHTLC`, remove the "legacy format" (no invoice ID) code paths. Only the sCrypt format is supported going forward.

**Step 3: Update all tests**

Every test that creates `HTLCParams` without `InvoiceID` must add one. Tests that specifically test the "optional InvoiceID" behavior should be deleted.

**Step 4: Run full test suite**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
go test ./payment/ -count=1 -v
```

Expected: all tests pass.

**Step 5: Commit**

```bash
git add payment/
git commit -m "feat(payment): mandatory InvoiceID, remove legacy script format support"
```

---

## Phase D: Replace TS HTLC Logic

### Task 14: TS artifact loader + new buildHTLC

**Files:**
- Copy: `contracts/artifacts/BitfsHTLC.json` → `libbitfs-ts/src/payment/artifacts/BitfsHTLC.json`
- Create: `libbitfs-ts/src/payment/artifact.ts`
- Modify: `libbitfs-ts/src/payment/htlc.ts`
- Modify: `libbitfs-ts/src/payment/types.ts` (InvoiceID mandatory)
- Modify: `libbitfs-ts/src/payment/index.ts` (export artifact)

**Step 1: Copy artifact**

```bash
mkdir -p /Users/alex/Codes/RabbitHole/libbitfs-ts/src/payment/artifacts
cp /Users/alex/Codes/RabbitHole/contracts/artifacts/BitfsHTLC.json \
   /Users/alex/Codes/RabbitHole/libbitfs-ts/src/payment/artifacts/
```

**Step 2: Write artifact loader**

```typescript
// artifact.ts
import artifactJSON from './artifacts/BitfsHTLC.json' with { type: 'json' }

export interface Artifact {
    hex: string
    abi: ABIEntity[]
}

export interface ABIEntity {
    name: string
    type: 'constructor' | 'function'
    params: { name: string; type: string }[]
    index: number
}

export function loadArtifact(): Artifact {
    return artifactJSON as Artifact
}

/**
 * Instantiate the BitfsHTLC locking script from the compiled artifact.
 */
export function instantiateHTLC(
    invoiceId: Uint8Array,  // 16 bytes
    capsuleHash: Uint8Array, // 32 bytes
    sellerPkh: Uint8Array,   // 20 bytes
    buyerPkh: Uint8Array,    // 20 bytes
    timeout: bigint,
): Uint8Array {
    const art = loadArtifact()
    let hex = art.hex
    hex = hex.replace('<invoiceId>', toHex(invoiceId))
    hex = hex.replace('<capsuleHash>', toHex(capsuleHash))
    hex = hex.replace('<sellerPkh>', toHex(sellerPkh))
    hex = hex.replace('<buyerPkh>', toHex(buyerPkh))
    hex = hex.replace('<timeout>', encodeScryptInt(timeout))
    return fromHex(hex)
}

function toHex(bytes: Uint8Array): string {
    return Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('')
}

function fromHex(hex: string): Uint8Array {
    const bytes = new Uint8Array(hex.length / 2)
    for (let i = 0; i < hex.length; i += 2) {
        bytes[i / 2] = parseInt(hex.substring(i, i + 2), 16)
    }
    return bytes
}

function encodeScryptInt(v: bigint): string {
    if (v === 0n) return '00'
    const negative = v < 0n
    if (negative) v = -v
    const buf: number[] = []
    while (v > 0n) {
        buf.push(Number(v & 0xffn))
        v >>= 8n
    }
    if (buf[buf.length - 1] & 0x80) {
        buf.push(negative ? 0x80 : 0x00)
    } else if (negative) {
        buf[buf.length - 1] |= 0x80
    }
    return buf.map(b => b.toString(16).padStart(2, '0')).join('')
}
```

> **Note:** Same caveat as Go — placeholder names must match actual artifact output. JSON import syntax may need adjustment for the project's module settings.

**Step 3: Rewrite buildHTLC**

Replace hand-built opcode assembly in `htlc.ts` with artifact-based:

```typescript
export function buildHTLC(params: HTLCParams): Uint8Array {
    // Validation (keep existing validation logic)
    if (!params.invoiceID || params.invoiceID.length !== INVOICE_ID_LEN) {
        throw ErrInvalidParams('invoiceID is mandatory (16 bytes)')
    }
    // ... other validations ...

    const buyerPkh = hash160(params.buyerPubKey)
    return instantiateHTLC(
        params.invoiceID,
        params.capsuleHash,
        params.sellerPubKeyHash,
        buyerPkh,
        BigInt(params.timeoutBlocks),
    )
}
```

**Step 4: Make InvoiceID mandatory in types**

In `types.ts`, change `invoiceID` from optional to required:

```typescript
export interface HTLCParams {
    buyerPubKey: Uint8Array      // 33 bytes
    sellerPubKey: Uint8Array     // 33 bytes
    sellerPubKeyHash: Uint8Array // 20 bytes
    capsuleHash: Uint8Array      // 32 bytes
    amount: bigint
    timeoutBlocks: number        // [MIN_HTLC_TIMEOUT, MAX_HTLC_TIMEOUT]
    invoiceID: Uint8Array        // 16 bytes, MANDATORY
}
```

**Step 5: Build and test**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-ts
npm run build
npm test -- --grep "htlc"
```

**Step 6: Commit**

```bash
git add src/payment/
git commit -m "feat(payment): TS artifact loader, replace hand-built HTLC with sCrypt"
```

---

### Task 15: TS on-chain refund + remove pre-signed refund

**Files:**
- Modify: `libbitfs-ts/src/payment/refund.ts`
- Modify: `libbitfs-ts/src/payment/types.ts` (remove SellerPreSign types)
- Modify: `libbitfs-ts/src/payment/index.ts` (remove pre-sign exports)
- Modify: `libbitfs-ts/src/payment/__tests__/refund.test.ts`

**Step 1: Rewrite BuyerRefundParams**

```typescript
/** Parameters for building an on-chain refund (buyer only, no seller needed). */
export interface BuyerRefundParams {
    fundingTxID: Uint8Array     // 32 bytes
    fundingVout: number
    fundingAmount: bigint
    htlcScript: Uint8Array
    buyerPrivKey: PrivateKey
    outputAddr: Uint8Array      // 20-byte PKH
    timeout: number             // block height for nLockTime
    feeRate?: number            // sat/byte, default 1
}
```

**Step 2: Delete pre-signed refund functions**

From `refund.ts`, delete:
- `buildSellerPreSignedRefund()`
- `buildBuyerRefundTx()` (old version with seller's pre-sign)

From `types.ts`, delete:
- `SellerPreSignParams`
- `SellerPreSignResult`

From `index.ts`, remove exports:
- `buildSellerPreSignedRefund`
- `SellerPreSignParams`
- `SellerPreSignResult`

**Step 3: Write new buildBuyerRefundTx**

```typescript
/**
 * Build on-chain refund transaction. Buyer signs unilaterally after timeout.
 * Uses nLockTime + sighash preimage for sCrypt OP_PUSH_TX verification.
 */
export async function buildBuyerRefundTx(params: BuyerRefundParams): Promise<Uint8Array> {
    // ... build transaction with nLockTime = params.timeout ...
    // ... sequence = 0xfffffffe ...
    // ... compute BIP143 sighash preimage ...
    // ... construct unlocking script: <preimage> <sig> <pubkey> <method_index=1> ...
    // ... sign and return serialized tx ...
}
```

> **Note:** Exact unlocking script construction depends on sCrypt compilation output. Same approach as Go: include sighash preimage for `refund()` method's `this.ctx.locktime` check.

**Step 4: Update refund tests**

Rewrite `refund.test.ts` to test the new on-chain refund flow. Remove all tests for pre-signed refund.

**Step 5: Build and test**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-ts
npm run build && npm test
```

**Step 6: Commit**

```bash
git add src/payment/
git commit -m "feat(payment): TS on-chain buyer refund, remove pre-signed refund"
```

---

## Phase E: Consumer Updates

### Task 16: Update bitfs daemon for new payment API

**Files:**
- Modify: `bitfs/internal/daemon/payment.go`
- Modify: `bitfs/internal/daemon/payment_test.go`
- Modify: `bitfs/internal/buy/buy.go`
- Modify: `bitfs/internal/buy/config.go`

**Step 1: Remove pre-signed refund endpoints**

If the daemon exposes any endpoints for pre-signed refund exchange (seller sends pre-signed refund to buyer), remove them. The on-chain refund needs no daemon interaction.

**Step 2: Update InvoiceID handling**

All `payment.BuildHTLC()` calls must now provide a non-nil InvoiceID. The daemon already generates invoices with IDs, so this should already be the case. Verify and fix any code path that constructs HTLCParams without InvoiceID.

**Step 3: Update buy module**

In `bitfs/internal/buy/buy.go`, ensure `payment.HTLCFundingParams.InvoiceID` is always provided.

If the buy module had any pre-signed refund handling (receiving seller's pre-signed tx), remove it. The buyer now handles refunds independently via on-chain nLockTime.

**Step 4: Run bitfs tests**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
go build ./...
go test ./internal/... -count=1
```

**Step 5: Commit**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
git add -A && git commit -m "feat(bitfs): adapt daemon/buy to new payment API (mandatory InvoiceID, no pre-sign)"
```

---

### Task 17: Update den-explorer HTLC parser

**Files:**
- Modify: `den-explorer/payment_analysis.go` (was x402.go)
- Modify: `den-explorer/payment_analysis_test.go` (was x402_test.go)

**Step 1: Update parseHTLCScript**

The current `parseHTLCScript` matches the hand-built HTLC byte pattern. After sCrypt, the script structure is entirely different. The function needs to be rewritten to match the sCrypt-compiled format.

> **Note:** The exact sCrypt-compiled script layout will be known after Task 8 compilation. During implementation, examine the compiled hex template and write a new parser that extracts the constructor parameters (invoiceId, capsuleHash, sellerPkh, buyerPkh, timeout) from the sCrypt-compiled script.

Strategy: since the artifact JSON is the source of truth, the parser could:
1. Read the artifact hex template
2. Find the placeholder positions
3. Extract bytes at those positions from the actual script

Or more robustly: embed a simplified pattern matcher that knows the sCrypt output structure.

**Step 2: Update AnalyzePayment**

Rename `AnalyzeX402` → `AnalyzePayment` (already done in Task 2). Update the internal call to use the new parser.

**Step 3: Test**

```bash
cd /Users/alex/Codes/RabbitHole/den-explorer
go test ./... -count=1 -v
```

**Step 4: Commit**

```bash
git add -A && git commit -m "feat(den-explorer): update HTLC parser for sCrypt-compiled script format"
```

---

### Task 18: Update cross-language test vectors

**Files:**
- Modify: `libbitfs-go/cmd/testvectors/` (regenerate vectors)
- Modify: `libbitfs-ts/src/__tests__/vectors/go-vectors.json` (updated output)
- Modify: `libbitfs-ts/src/__tests__/crosslang.test.ts` (verify with new format)

**Step 1: Regenerate Go test vectors**

The Go test vector generator produces HTLC scripts among other outputs. With the new sCrypt-based scripts, these vectors will change.

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go
go run ./cmd/testvectors/ > /tmp/go-vectors.json
cp /tmp/go-vectors.json ../libbitfs-ts/src/__tests__/vectors/go-vectors.json
```

**Step 2: Run cross-language tests**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-ts
npm test -- --grep "cross-language"
```

Expected: all cross-language vectors match.

**Step 3: Commit**

```bash
cd /Users/alex/Codes/RabbitHole
git add libbitfs-go/cmd/testvectors/ libbitfs-ts/src/__tests__/
git commit -m "test: regenerate cross-language test vectors for sCrypt-based HTLC"
```

---

## Phase F: Integration & Verification

### Task 19: Integration and E2E tests

**Files:**
- Modify: `bitfs/integration/payment_flow_test.go`
- Modify: `bitfs/integration/payment_flow_extra_test.go`
- Modify: `bitfs/integration/security_test.go`
- Modify: `bitfs/e2e/06_paid_purchase_test.go`
- Modify: `bitfs/e2e/18_daemon_buy_api_test.go`

**Step 1: Update integration tests**

All integration tests that use `payment.BuildHTLC()` or `payment.BuildHTLCFundingTx()` should:
1. Always provide InvoiceID (mandatory)
2. Remove any pre-signed refund test flows
3. Verify new on-chain refund path (if applicable)

**Step 2: Update E2E tests**

E2E tests may need to adjust expected script sizes and patterns. Since sCrypt scripts are larger than hand-built ones, fee estimations may need updating.

**Step 3: Run full test suite**

```bash
# Integration tests (need integration build tag)
cd /Users/alex/Codes/RabbitHole/bitfs
go test -tags=integration ./integration/ -count=1 -v 2>&1 | tail -20

# E2E tests (need Docker)
go test -tags=e2e ./e2e/ -count=1 -v 2>&1 | tail -20
```

**Step 4: Commit**

```bash
git add integration/ e2e/
git commit -m "test(bitfs): update integration and e2e tests for sCrypt-based payment"
```

---

### Task 20: Final verification + cleanup

**Step 1: Global stale reference check**

```bash
cd /Users/alex/Codes/RabbitHole
# Check for any remaining "x402" in code (excluding deferred.md decision log)
grep -r 'x402' --include='*.go' --include='*.ts' --include='*.tsx' --include='*.json' \
  --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git .

# Check for any remaining pre-signed refund references
grep -r 'PreSign\|pre.sign\|pre_sign' --include='*.go' --include='*.ts' \
  --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git .
```

Expected: zero results (except deferred.md and historical comments).

**Step 2: Full build verification**

```bash
# All Go projects
cd /Users/alex/Codes/RabbitHole/libbitfs-go && go build ./... && go test ./... -count=1
cd /Users/alex/Codes/RabbitHole/bitfs && go build ./... && go test ./internal/... -count=1
cd /Users/alex/Codes/RabbitHole/den-explorer && go build ./...
cd /Users/alex/Codes/RabbitHole/git-remote-bitfs && go build ./...

# TypeScript
cd /Users/alex/Codes/RabbitHole/libbitfs-ts && npm run build && npm test
cd /Users/alex/Codes/RabbitHole/contracts && npm test

# Contracts compilation
cd /Users/alex/Codes/RabbitHole/contracts && npx scrypt-cli compile
```

**Step 3: Update deferred.md**

Mark P1 as completed in `docs/tasks/deferred.md`. Move the P1 section to a "done" status or reference the completed plan.

**Step 4: Update project MEMORY.md**

Update auto-memory with:
- HTLC refactor completed
- New `contracts/` directory purpose
- `payment` package replaces `x402`
- Pre-signed refund removed, on-chain refund via sCrypt OP_PUSH_TX
- InvoiceID is mandatory

**Step 5: Final commit**

```bash
cd /Users/alex/Codes/RabbitHole
git add -A && git commit -m "feat: complete HTLC refactor — x402→payment, sCrypt contract, on-chain refund"
```

---

## Summary

| Phase | Tasks | Scope |
|-------|-------|-------|
| A: Rename | 1-6 | Mechanical x402→payment across ~46 files, zero logic changes |
| B: sCrypt | 7-9 | New `contracts/` project, BitfsHTLC contract, compile artifact |
| C: Go Logic | 10-13 | Artifact loader, replace BuildHTLC, on-chain refund, remove pre-sign, mandatory InvoiceID |
| D: TS Logic | 14-15 | Same changes for TypeScript |
| E: Consumers | 16-18 | bitfs daemon, den-explorer, cross-language vectors |
| F: Verify | 19-20 | Integration/E2E tests, final cleanup |

**Total: 20 tasks across 6 phases.**

**Key risks:**
1. sCrypt artifact placeholder naming may differ from assumed `<paramName>` format — verify after first compilation (Task 8)
2. sCrypt-compiled unlocking script format (method dispatch, sighash preimage placement) needs empirical verification — adjust Tasks 11-12 and 14-15 based on actual output
3. Script size increase (sCrypt output is larger) may affect fee estimation in existing tests
4. den-explorer HTLC parser needs complete rewrite for new script format

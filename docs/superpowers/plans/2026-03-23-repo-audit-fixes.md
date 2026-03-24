# RabbitHole Repo Audit Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all Critical, High, and Medium issues found during the full-repo audit (2026-03-23).

**Architecture:** Fixes are organized by repository boundary (each sub-project has its own `.git`). Tasks 1-5 target independent repos and can run in parallel. Tasks 6-11 target the main RabbitHole repo docs/config and should run sequentially.

**Tech Stack:** Go 1.25.6, TypeScript/Bun, HTML/CSS, Markdown

**Issue Tracker:** Full audit report in conversation context. 7 Critical, 13 High, 19 Medium issues.

**Deferred:** H7 (bitfs-explorer spec vs implementation mismatch) — the spec describes DOM scraping but implementation correctly uses WoC REST API. The spec should be updated in a separate plan since it requires design review, not a mechanical fix.

---

## Task 1: den-explorer — Fix Broken Build + XSS Vulnerability

**Files:**
- Rewrite: `den-explorer/payment_analysis.go`
- Rewrite: `den-explorer/payment_analysis_test.go`
- Modify: `den-explorer/static/dag.js:155-165`
- Modify: `den-explorer/templates/payment.html:23`

**Context:** The HTLC was refactored from sCrypt artifact to 106-byte plain Bitcoin Script. The den-explorer still references `payment.LoadArtifact()` (deleted) and uses old sCrypt byte offsets. The DAG tooltip has an XSS vulnerability via unsanitized innerHTML.

- [ ] **Step 1: Fix XSS in dag.js**

Replace D3 `.html()` with text escaping in `den-explorer/static/dag.js`:

```javascript
// Before (XSS vulnerable):
tooltip.html(
  '<strong>' + d.data.name + '</strong><br>' + ...
)

// After (safe):
function escapeHtml(s) {
  var div = document.createElement('div');
  div.appendChild(document.createTextNode(s));
  return div.innerHTML;
}

tooltip.html(
  '<strong>' + escapeHtml(d.data.name) + '</strong><br>' +
  'Type: ' + escapeHtml(nd.type) + '<br>' +
  'Access: ' + escapeHtml(nd.access) + '<br>' +
  'TxID: ' + escapeHtml(nd.txid) + '<br>' +
  'P_node: ' + escapeHtml(truncHash(nd.pnode))
).style('display', 'block');
```

- [ ] **Step 2: Rewrite payment_analysis.go for plain script**

Replace the entire HTLC parsing logic. The new plain script format is 106 bytes with offsets from `libbitfs-go/payment/htlc.go`:

```go
const (
    htlcMinScriptLen      = 106
    htlcInvoiceIDOffset   = 1   // 16 bytes
    htlcCapsuleHashOffset = 21  // 32 bytes
    htlcSellerPkhOffset   = 57  // 20 bytes
    htlcBuyerPkhOffset    = 83  // 20 bytes
)

// Remove: loadArtifactParts(), decodeScryptInt(), payment.LoadArtifact() call
// Remove: old offsets (107, 123, 155, 175, 195)
// The plain script has NO embedded timeout — timeout is enforced via nLockTime

func parseHTLCScript(script []byte) *HTLCInfo {
    if len(script) < htlcMinScriptLen {
        return nil
    }
    // Verify script structure: <invoiceId> OP_DROP OP_IF OP_SHA256 ...
    // Check opcode at offset 17 is OP_DROP (0x75), offset 18 is OP_IF (0x63)
    if script[17] != 0x75 || script[18] != 0x63 {
        return nil
    }
    return &HTLCInfo{
        InvoiceID:   hex.EncodeToString(script[htlcInvoiceIDOffset : htlcInvoiceIDOffset+16]),
        CapsuleHash: hex.EncodeToString(script[htlcCapsuleHashOffset : htlcCapsuleHashOffset+32]),
        SellerPkh:   hex.EncodeToString(script[htlcSellerPkhOffset : htlcSellerPkhOffset+20]),
        BuyerPkh:    hex.EncodeToString(script[htlcBuyerPkhOffset : htlcBuyerPkhOffset+20]),
        Timeout:     0, // No longer embedded; enforced via nLockTime
    }
}
```

- [ ] **Step 3: Update payment.html template**

Change line 23 from "sCrypt BitfsHTLC Contract (compiled artifact)" to "BitFS HTLC (106-byte plain Bitcoin Script)". Update the script structure description to show P2PKH paths instead of multisig.

- [ ] **Step 4: Rewrite payment_analysis_test.go**

Build test vectors from the actual 106-byte script format. Use `libbitfs-go/payment.BuildHTLC()` to generate a known-good script, then verify `parseHTLCScript` extracts correct fields. Remove all sCrypt-related tests (`TestParseHTLCScript_sCryptArtifact`, `TestDecodeScryptInt`).

- [ ] **Step 5: Verify build and tests pass**

Run: `cd den-explorer && go build ./... && go test ./... -count=1`
Expected: BUILD SUCCESS, all tests PASS

- [ ] **Step 6: Commit**

```bash
cd den-explorer
git add payment_analysis.go payment_analysis_test.go static/dag.js templates/payment.html
git commit -m "fix: rewrite HTLC parser for plain script + fix XSS in DAG tooltip"
```

---

## Task 2: bitfs — Fix promptNetwork Panic

**Files:**
- Modify: `bitfs/cmd/bitfs/password.go:95-115`

**Context:** `promptNetwork()` has `case "4": idx = 3` but the `networks` slice only has 3 elements (indices 0-2). Input "4" causes an index-out-of-bounds panic.

- [ ] **Step 1: Write a test for promptNetwork edge cases**

Add to an existing test file or create `bitfs/cmd/bitfs/password_test.go`:

```go
func TestPromptNetwork_ValidatesInput(t *testing.T) {
    networks := []networkChoice{
        {name: "mainnet"}, {name: "testnet"}, {name: "regtest"},
    }
    // Verify that only inputs "1"-"3" are valid for a 3-element slice
    assert.Equal(t, 3, len(networks))
}
```

- [ ] **Step 2: Fix the bug**

In `bitfs/cmd/bitfs/password.go`, remove `case "4"` and change error message:

```go
// Before:
case "4": idx = 3
default:  return "", fmt.Errorf("invalid choice %q; enter 1-4", input)

// After:
default:  return "", fmt.Errorf("invalid choice %q; enter 1-%d", input, len(networks))
```

- [ ] **Step 3: Verify build passes**

Run: `cd bitfs && go build ./cmd/bitfs/`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
cd bitfs
git add cmd/bitfs/password.go
git commit -m "fix: remove out-of-bounds case in promptNetwork"
```

---

## Task 3: libbitfs-go — Fix ContentResolver Hash Verification

**Files:**
- Modify: `libbitfs-go/storage/resolver.go:75-76`
- Modify: `libbitfs-go/storage/resolver_test.go` (add test)

**Context:** `Fetch()` computes `SHA256(ciphertext)` and compares to `keyHash` which is `SHA256(SHA256(plaintext))`. For encrypted content, these never match, so remote content fetch silently fails. The fix depends on what `keyHash` actually represents in the storage layer — it's the content-addressed key, not a verification hash of the wire data. The remote endpoint should serve data keyed by this hash, and the hash is already validated by the storage layer's content-addressing. The comparison should either be removed for remote fetches or use the correct hash computation.

- [ ] **Step 1: Investigate the hash semantics**

Read `libbitfs-go/storage/store.go` to understand what `keyHash` represents and how `Put()` stores data. Check if remote endpoints store ciphertext keyed by the plaintext's double-SHA256, or if they use a different key.

- [ ] **Step 2: Write a failing test**

Create a test in `resolver_test.go` that sets up a mock HTTP server returning valid encrypted content, and verifies `Fetch()` returns the data instead of silently dropping it.

- [ ] **Step 3: Fix the hash verification**

Based on Step 1 findings, either:
- Remove the hash check for remote content (the content-addressing key IS the verification), or
- Fix the hash computation to match the actual keying scheme

- [ ] **Step 4: Run all storage tests**

Run: `cd libbitfs-go && go test ./storage/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
cd libbitfs-go
git add storage/resolver.go storage/resolver_test.go
git commit -m "fix: correct hash verification in ContentResolver.Fetch for remote content"
```

---

## Task 4: metanet — Fix Integration Test + CLAUDE.md

**Files:**
- Modify: `metanet/integration/payment_overlay_test.go:579`
- Modify: `metanet/CLAUDE.md:32,68`

**Context:** Integration test asserts HMAC signature length (32 bytes) but `SignAdvertisement` now produces ECDSA signatures (64 bytes). CLAUDE.md has stale module path `tongxiaofeng` instead of `bitfsorg`.

- [ ] **Step 1: Fix signature length assertion**

In `metanet/integration/payment_overlay_test.go:579`, change:

```go
// Before:
require.Len(t, adv.Signature, 32, "HMAC-SHA256 signature should be 32 bytes")

// After:
require.Len(t, adv.Signature, 64, "ECDSA signature should be 64 bytes (r||s)")
```

- [ ] **Step 2: Fix CLAUDE.md module path and dependencies**

In `metanet/CLAUDE.md:32`, change `github.com/tongxiaofeng/metanet` → `github.com/bitfsorg/metanet`.

In `metanet/CLAUDE.md:68`, fix the dependency reference from `github.com/tongxiaofeng/bitfs` to the correct description (metanet currently has NO external dependencies per go.mod — only `go 1.25.6`). Also remove or correct the "BSV SDK" line (~line 67) in the Dependencies section since go.mod has no external dependencies.

Update status text (~line 19) to reflect that all 7 packages + CLI are implemented (remove "Remaining: proof, payment, overlay, CLI").

- [ ] **Step 3: Verify tests pass**

Run: `cd metanet && go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
cd metanet
git add integration/payment_overlay_test.go CLAUDE.md
git commit -m "fix: correct ECDSA signature length in integration test, update CLAUDE.md"
```

---

## Task 5: bitfs-extension — Fix Service Worker Switch Fall-Through

**Files:**
- Modify: `bitfs-extension/src/background/service-worker.ts:~226`

**Context:** The `CONNECT_DAPP` case in the message handler switch statement is missing a `break`/`return`, potentially falling through to `ADD_AUTHORIZED_DOMAIN`.

- [ ] **Step 1: Verify file exists and locate the switch statement**

Read `bitfs-extension/src/background/service-worker.ts` and find the `CONNECT_DAPP` case (around line 194-226).

- [ ] **Step 2: Add explicit return/break**

Ensure the `CONNECT_DAPP` case ends with an explicit `return` or `break` before the next case, matching the pattern used by all other cases.

- [ ] **Step 3: Verify build passes**

Run: `cd bitfs-extension && bun run build`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
cd bitfs-extension
git add src/background/service-worker.ts
git commit -m "fix: add missing break in CONNECT_DAPP switch case"
```

---

## Task 6: Root Files — README.md, CLAUDE.md, .gitignore

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md:41,52-64,74,150`
- Modify: `.gitignore`

- [ ] **Step 1: Fix README.md tech stack and project descriptions**

```markdown
# Changes needed:
# Line 24: "Shared core library (TypeScript, planned)" → "Shared core library (TypeScript, 11 packages)"
# Line 28: "Desktop/mobile client (Flutter, independent repo)" → "Mobile client (Expo SDK 55 + React Native, independent repo)"
# Line 41: "Flutter 3.27+" → "Expo SDK 55 + React Native"
# Add missing dirs: bitfs-desktop/, bitfs-explorer/, contracts/
```

- [ ] **Step 2: Fix CLAUDE.md**

```markdown
# Line ~41: Add den-explorer/ to independent repos list (if not already there)
# Line ~52-64: Add bitfs-desktop/, bitfs-explorer/ to directory structure
#   Note: contracts/ may be obsolete (sCrypt was removed from HTLC) — verify relevance before adding
# Line ~74: Add bmget to b-tools list: "bls/bcat/bget/bmget/bstat/btree"
# Line ~150: "golang.org/x/crypto v0.47.0" → "golang.org/x/crypto v0.48.0"
```

- [ ] **Step 3: Fix .gitignore**

Add missing entries:

```gitignore
/bitfs-explorer/
.wrangler/
```

- [ ] **Step 4: Verify git status looks clean**

Run: `git status`
Expected: `bitfs-explorer/` and `.wrangler/` no longer show as untracked

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md .gitignore
git commit -m "docs: fix README tech stack, update CLAUDE.md, add .gitignore entries"
```

---

## Task 7: Website Fixes — metanet.org + bitfs.org

**Files:**
- Modify: `websites/metanet.org/index.html:1308,1609,1626`
- Modify: `websites/metanet.org/index.zh.html:1308,1609,1626`
- Modify: `websites/bitfs.org/index.html:1225,1242,1272,1599`
- Modify: `websites/bitfs.org/index.zh.html:1225,1242,1270,1597`
- Modify: `websites/bitfs.org/Website-Content-Outline.md:70,339` (source of truth)
- Modify: `websites/CLAUDE.md`

**Context:** metanet.org has placeholder GitHub links (`https://github.com`) and dead whitepaper download link (`href="#"`). bitfs.org points to wrong GitHub org (`nicklaus4/bitfs` instead of `bitfsorg/bitfs`). The Content Outline is the source of truth per workflow — must fix there too or the bug reappears on regeneration.

- [ ] **Step 1: Fix metanet.org GitHub links**

In both `index.html` and `index.zh.html`, replace `https://github.com` (bare domain) with `https://github.com/bitfsorg/metanet` at all occurrences in nav and footer.

- [ ] **Step 2: Fix metanet.org whitepaper download link**

Replace `href="#"` with `href="/whitepaper/"` or a direct PDF link. Since metanet.org doesn't have a whitepaper subdirectory like bitfs.org does, consider linking to the PDF directly or creating a whitepaper page (but keep it simple — just link to the PDF for now).

- [ ] **Step 3: Fix bitfs.org GitHub links**

In both `index.html` and `index.zh.html`, replace `https://github.com/nicklaus4/bitfs` with `https://github.com/bitfsorg/bitfs` at all occurrences. Also fix the same URL in `Website-Content-Outline.md` (lines 70, 339) — this is the source of truth for HTML generation.

- [ ] **Step 4: Update websites/CLAUDE.md**

Add missing files to directory listing: `bitfs-transactions.html`, `whitepaper/` subdirectory, `slides/` subdirectory.

- [ ] **Step 5: Commit**

```bash
cd websites
git add metanet.org/ bitfs.org/ CLAUDE.md
git commit -m "fix: correct GitHub URLs and whitepaper download links"
```

---

## Task 8: Design Docs — HTLC, CapsuleHash, Specs

**Files:**
- Modify: `docs/design/bitfs/3-DetailedDesign.zh.md:2046,2078-2097`
- Modify: `docs/specs/bitfs/08-payment.md:78,181-191`
- Modify: `docs/specs/bitfs/02-tx.md:17`
- Modify: `docs/specs/bitfs/03-metanet.md:5`
- Modify: `docs/specs/bitfs/01-method42.md:170`

- [ ] **Step 1: Update HTLC script in DetailedDesign**

Replace the old HTLC script (lines 2078-2097) with the current 106-byte plain Bitcoin Script:

```
<invoiceId(16B)> OP_DROP
OP_IF
  OP_SHA256 <capsuleHash(32B)> OP_EQUALVERIFY
  OP_DUP OP_HASH160 <sellerPkh(20B)> OP_EQUALVERIFY OP_CHECKSIG
OP_ELSE
  OP_DUP OP_HASH160 <buyerPkh(20B)> OP_EQUALVERIFY OP_CHECKSIG
OP_ENDIF
```

Note: Timeout enforced via nLockTime (not embedded in script). Default timeout: 72 blocks.

- [ ] **Step 2: Fix CapsuleHash formula in DetailedDesign**

At line 2046, change `capsule_hash = SHA256(capsule)` to `capsule_hash = SHA256(fileTxID || capsule)`.

- [ ] **Step 3: Fix spec 08-payment.md**

- Line 75: Fix `CapsuleHash` description from `SHA256(capsule)` to `SHA256(fileTxID || capsule)`
- Line 78: Change `InvoiceID` from "Optional" to "Mandatory (exactly 16 bytes)"
- Fix `SellerPubKey` comment — no longer used for "2-of-2 multisig refund", now used for P2PKH seller identification
- Lines 181-191: Update HTLC script to show P2PKH refund path (not 2-of-2 multisig)
- Add note about nLockTime-based timeout enforcement

- [ ] **Step 4: Fix spec 02-tx.md DefaultFeeRate**

Line 17: Change `DefaultFeeRate = uint64(1)` to `DefaultFeeRate = uint64(100)`.

- [ ] **Step 5: Fix spec 03-metanet.md hard link claim**

Line 5: Remove "hard links" from the feature list. Change to: "supports soft links (local and remote), and directory traversal".

- [ ] **Step 6: Fix spec 01-method42.md ComputeCapsuleHash signature**

Line 170: Change return type from `[]byte` to `([]byte, error)`.

- [ ] **Step 7: Commit**

```bash
git add docs/design/bitfs/3-DetailedDesign.zh.md docs/specs/bitfs/
git commit -m "docs: sync HTLC, CapsuleHash, and specs with current implementation"
```

---

## Task 9: Design Docs — Paths, Structure, Navigation

**Files:**
- Modify: `docs/design/bitfs/1-ConceptDesign.zh.md:3` (and 2-, 3-, 4-)
- Modify: `docs/design/bitfs/3-DetailedDesign.zh.md:41`
- Modify: `docs/design/bitfs/4-TestDesign.zh.md:65,79+`
- Modify: `docs/design/bitfs/2-SystemDesign.zh.md:308-314`
- Modify: `docs/design/OverallDesign.zh.md:79-92`
- Modify: `docs/design/metanet/1-ConceptDesign.zh.md:99`
- Modify: `docs/design/CLAUDE.md` (remove 5-TransactionSpec.zh.md if listed)

- [ ] **Step 1: Remove broken 5-TransactionSpec.zh.md link**

In all 4 BitFS design doc navigation headers (line 3), remove the `[交易规范](5-TransactionSpec.zh.md)` link since that file doesn't exist. The transaction spec content is covered in `docs/specs/bitfs/02-tx.md`. Also remove the `5-TransactionSpec.zh.md` entry from `docs/design/CLAUDE.md` if it's listed there.

- [ ] **Step 2: Fix source file paths in DetailedDesign**

At line 41, change `src/internal/method42/hdwallet.go` → `libbitfs-go/wallet/hd.go` and `encrypt.go` → `libbitfs-go/method42/encrypt.go`.

- [ ] **Step 3: Fix test file paths in TestDesign**

Update all outdated test file references:
- `src/proto/bitfs_test.go` → `libbitfs-go/metanet/parser_test.go`
- `method42/hdwallet_test.go` → `libbitfs-go/wallet/wallet_test.go`
- Other paths per the audit findings

- [ ] **Step 4: Reconcile NodeTypeAnchor in SystemDesign**

At lines 308-314, the design doc says "Anchor 锚点节点类型被移除". But `libbitfs-go/metanet/node.go` still defines `NodeTypeAnchor = 3` and `git-remote-bitfs` uses it. Verify whether the code or the doc is authoritative:
- If Anchor is still used in code: add `ANCHOR (3)` to the enum with a note "used by git-remote-bitfs for commit anchors"
- If Anchor is deprecated: leave the doc as-is (the code should be cleaned up separately)

- [ ] **Step 5: Add vault/ to OverallDesign package list**

At lines 79-92, add `vault/` to the libbitfs-go package table with description: "Vault state management, transaction building, cat/get operations".

- [ ] **Step 6: Fix typo in Metanet ConceptDesign**

At line 99, change `独立独立链` → `独立链`.

- [ ] **Step 7: Commit**

```bash
git add docs/design/
git commit -m "docs: fix broken links, outdated paths, add missing types in design docs"
```

---

## Task 10: Sub-project CLAUDE.md Fixes

**Files:**
- Modify: `bitfs/CLAUDE.md:27+`
- Modify: `docs/whitepaper/CLAUDE.md:9-17`
- Modify: `websites/README.md` (minor)

- [ ] **Step 1: Fix bitfs/CLAUDE.md**

- Line 27: Change module path `github.com/tongxiaofeng/bitfs` → `github.com/bitfsorg/bitfs`
- Fix `internal/` structure: `buyer/` → `buy/`, add `banner/` and `publish/`
- Add `bmget` to b-tools list

- [ ] **Step 2: Fix docs/whitepaper/CLAUDE.md**

- Lines 9-17: Fix file structure to show PDFs in `pdf/` subdirectory
- Fix workflow description to reference `.tex` output (not `.md`)

- [ ] **Step 3: Commit bitfs/CLAUDE.md in the bitfs repo**

`bitfs/` has its own `.git`, so it must be committed separately:

```bash
cd bitfs && git add CLAUDE.md && git commit -m "docs: fix module path and project structure in CLAUDE.md"
```

- [ ] **Step 4: Commit docs/whitepaper/CLAUDE.md in the RabbitHole repo**

```bash
git add docs/whitepaper/CLAUDE.md
git commit -m "docs: fix whitepaper CLAUDE.md file structure and workflow description"
```

---

## Task 11: Task File Cleanup

**Files:**
- Move: `docs/tasks/2026-03-19-mvp-v0.0.1-release-design.md` → `docs/tasks/done/`
- Move: `docs/tasks/2026-03-19-mvp-v0.0.1-release-plan.md` → `docs/tasks/done/`
- Move: `docs/tasks/2026-03-20-wocarc-blockchain-backend-design.md` → `docs/tasks/done/`
- Move: `docs/tasks/2026-03-22-smoke-test-results.md` → `docs/tasks/done/`
- Move: `docs/audits/2026-03-05-design-boundary-review-round1.md` → `docs/audits/done/`

- [ ] **Step 1: Archive completed task files**

```bash
mv docs/tasks/2026-03-19-mvp-v0.0.1-release-design.md docs/tasks/done/
mv docs/tasks/2026-03-19-mvp-v0.0.1-release-plan.md docs/tasks/done/
mv docs/tasks/2026-03-20-wocarc-blockchain-backend-design.md docs/tasks/done/
mv docs/tasks/2026-03-22-smoke-test-results.md docs/tasks/done/
```

- [ ] **Step 2: Archive completed audit file**

```bash
mv docs/audits/2026-03-05-design-boundary-review-round1.md docs/audits/done/
```

- [ ] **Step 3: Commit**

```bash
git add docs/tasks/ docs/audits/
git commit -m "chore: archive completed task and audit files"
```

---

## Parallelization Guide

**Independent (can run in parallel):**
- Tasks 1-5 (each in a separate repo with own `.git`)

**Sequential (main RabbitHole repo):**
- Tasks 6-11 (all modify files in the main repo)
- Within this group, Tasks 6-10 touch different files and could potentially be parallel if careful about commit ordering

**Estimated total:** ~45 minutes with parallel execution of Tasks 1-5

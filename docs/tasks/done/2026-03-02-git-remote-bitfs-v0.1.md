# git-remote-bitfs v0.1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete git-remote-bitfs v0.1 — fix all test failures, audit and fix code issues, add Docker regtest e2e tests, and update documentation.

**Architecture:** Git remote helper translating Git object model to Metanet DAG. 6 internal packages: helper (protocol), stream (fast-import/export), mapper (git notes), chain (DAG read/write + encryption), config (URL/attributes/wallet), utxo (state management). Uses libbitfs-go via replace directive.

**Tech Stack:** Go 1.25.6, libbitfs-go (method42/wallet/tx/metanet/network), go-sdk v1.2.18, testify, Docker (BSV regtest)

---

## Task 1: Fix failing tests — change address for multi-op batches

libbitfs-go `tx.MutationBatch.Build()` now requires a change address for multi-op batches. Two tests fail: `TestBuildMultiRefAnchor` and `TestPushMultipleFiles`.

**Files:**
- Modify: `internal/chain/anchor_test.go:221` (add ChangeAddr to MultiRefAnchorParams)
- Modify: `internal/chain/writer_test.go:188` (add ChangeAddr to PushParams)
- Modify: `internal/chain/writer.go:100` (auto-derive change from first fee UTXO when ChangeAddr not provided)

**Step 1: Fix writer.go to auto-derive change address**

When `PushParams.ChangeAddr` is empty but ops exist, derive it from the first fee UTXO's ScriptPubKey (extract the 20-byte pubkey hash from the P2PKH script). This matches what the export handler does — callers shouldn't need to pass ChangeAddr separately.

In `writer.go`, replace lines 100-102:

```go
if len(params.ChangeAddr) == 20 {
    batch.SetChange(params.ChangeAddr)
}
```

With:

```go
changeAddr := params.ChangeAddr
if len(changeAddr) != 20 && len(params.FeeUTXOs) > 0 {
    // Auto-derive change address from first fee UTXO's P2PKH script.
    // P2PKH script: OP_DUP OP_HASH160 <20-byte-hash> OP_EQUALVERIFY OP_CHECKSIG
    script := params.FeeUTXOs[0].ScriptPubKey
    if len(script) == 25 && script[0] == 0x76 && script[1] == 0xa9 && script[2] == 0x14 {
        changeAddr = script[3:23]
    }
}
if len(changeAddr) == 20 {
    batch.SetChange(changeAddr)
}
```

Apply the same pattern in `anchor.go` for both `BuildAnchor` (around line 122) and `BuildMultiRefAnchor` (around line 240).

**Step 2: Run tests to verify**

```bash
cd /Users/alex/Codes/RabbitHole/git-remote-bitfs
go test ./internal/chain/ -v -run "TestBuildMultiRefAnchor|TestPushMultipleFiles"
```

Expected: PASS

**Step 3: Run full test suite**

```bash
go test ./...
```

Expected: All pass.

**Step 4: Commit**

```bash
git add internal/chain/writer.go internal/chain/anchor.go
git commit -m "fix(chain): auto-derive change address for multi-op batches

MutationBatch.Build() now requires a change address for multi-op
batches. Derive it from the first fee UTXO's P2PKH script when
not explicitly provided."
```

---

## Task 2: Code audit — security and correctness review

Read every source file and identify issues. Create a checklist.

**Files to audit:**
- `cmd/git-remote-bitfs/main.go` (48 LOC)
- `internal/helper/helper.go` (170 LOC)
- `internal/helper/export.go` (742 LOC)
- `internal/helper/import.go` (291 LOC)
- `internal/helper/list.go` (130 LOC)
- `internal/chain/anchor.go` (336 LOC)
- `internal/chain/writer.go` (218 LOC)
- `internal/chain/reader.go` (273 LOC)
- `internal/config/url.go` (84 LOC)
- `internal/config/wallet.go` (70 LOC)
- `internal/config/attributes.go`
- `internal/mapper/index.go` (97 LOC)
- `internal/mapper/notes.go` (155 LOC)
- `internal/utxo/types.go` (39 LOC)
- `internal/utxo/store.go` (241 LOC)
- `internal/stream/types.go` (116 LOC)
- `internal/stream/parser.go` (398 LOC)
- `internal/stream/generator.go` (180 LOC)

**Known issues to check:**
- [ ] CLAUDE.md module name says `tongxiaofeng` but go.mod says `bitfsorg` — fix CLAUDE.md
- [ ] CLAUDE.md dust limit might still say 546 in one place
- [ ] Export handler: git notes not written during export (line 400-412, commented out as `_ = note`)
- [ ] Import handler: `findLastImportedAnchor()` iterates ALL notes (O(N)) — acceptable for MVP?
- [ ] Import handler: `importRef()` silently skips refs without UTXO store or refUTXO — should log warning?
- [ ] Export handler: file delete tracked but not pushed (`stream.FileDelete` case) — OK for MVP
- [ ] `initUTXOStore()` swallows load error with `debugf` — should this be fatal?
- [ ] `deriveBranchKey()` generates random key when wallet is nil — test-only, verify no production path
- [ ] `buildDirTree()`: all keys generated randomly per push — no key persistence between pushes
- [ ] `handleList()`: `lookupCommitSHA()` walks all notes per ref (O(N*M)) — acceptable for MVP
- [ ] `author` field in `importAnchor()`: logic for appending timestamp to author string may produce malformed fast-import lines if author already has `>` but no timestamp
- [ ] Security: no input size bounds on fast-export stream parsing (DoS via huge blobs in memory)
- [ ] Security: no validation of chain reader output (malicious node could return crafted TLV)

**Step 1: Audit all files**

Use subagents to audit in parallel:
- Agent A: helper/ + chain/ (security-critical: encryption, TX building, chain interaction)
- Agent B: config/ + mapper/ + utxo/ + stream/ + cmd/ (protocol, state, parsing)

Produce audit report at `docs/audits/2026-03-02-git-remote-bitfs-audit.md`.

**Step 2: Categorize findings**

- CRITICAL (C): Security vulnerabilities, data loss risks
- HIGH (H): Correctness issues that affect functionality
- MEDIUM (M): Design issues, missing validation
- LOW (L): Style, documentation, minor improvements

**Step 3: Prioritize for v0.1**

Fix all C and H issues. Document M/L for v0.2.

---

## Task 3: Fix audit findings

Implement fixes based on Task 2 findings. This task will be refined after the audit.

**Likely fixes (based on pre-audit review):**

**3a: Fix CLAUDE.md module name and dust limit**

```
internal CLAUDE.md: module name github.com/tongxiaofeng/git-remote-bitfs → github.com/bitfsorg/git-remote-bitfs
```

**3b: Export handler — write git notes for incremental fetch**

The export handler has dead code at lines 400-412 that should write notes. The note writing is needed for incremental fetch (import handler uses `findLastImportedAnchor()` which reads notes).

Problem: During export, git marks (`:1`, `:2`) haven't been resolved to real commit SHAs yet. Git resolves marks after fast-import completes. So we can't write git notes keyed by commit SHA during export.

Solution: After a successful export, write the anchor TxID to a simple file `~/.git/bitfs/refs/<refname>` instead of git notes. The import handler can use this file to find the last pushed anchor for incremental fetch. This avoids the mark→SHA resolution problem entirely.

**3c: Auto-derive change address in export handler**

`export.go:308` creates `PushParams` without `ChangeAddr`. Should derive from wallet or fee UTXO, similar to Task 1 fix.

**3d: Blob size limit**

Add a configurable max blob size (default 10MB) in the fast-export parser to prevent OOM from malicious or oversized content.

**Step 1: Implement fixes**

One commit per logical fix group.

**Step 2: Run tests after each fix**

```bash
go test ./...
```

**Step 3: Commit each fix**

---

## Task 4: E2E test infrastructure

Set up Docker regtest environment for end-to-end testing.

**Files:**
- Create: `e2e/docker-compose.yml`
- Create: `e2e/bitcoin.conf`
- Create: `e2e/testutil/config.go`
- Create: `e2e/testutil/rpc.go`
- Create: `e2e/testutil/funder.go`
- Create: `e2e/testutil/helpers.go`

**Step 1: Create docker-compose.yml**

```yaml
services:
  bsv-node:
    image: bitcoinsv/bitcoin-sv:1.0.11
    container_name: git-remote-bitfs-regtest
    ports:
      - "18332:18332"
      - "18444:18444"
    volumes:
      - ./bitcoin.conf:/data/bitcoin.conf
      - bsv-data:/data
    healthcheck:
      test: ["CMD", "/entrypoint.sh", "bitcoin-cli", "-regtest", "getinfo"]
      interval: 5s
      timeout: 3s
      retries: 10

volumes:
  bsv-data:
```

**Step 2: Create bitcoin.conf**

Same as bitfs/e2e/bitcoin.conf (regtest, txindex, rpcuser=bitfs/rpcpassword=bitfs).

**Step 3: Create testutil package**

`config.go`: Load RPC config from env vars with regtest defaults.
`rpc.go`: JSON-RPC client for Bitcoin node (reuse pattern from bitfs/e2e/testutil).
`funder.go`: Generate wallet, mine blocks, fund address.
`helpers.go`: Common test helpers:
- `SetupTestRepo(t)` — create temp git repo with initial commit
- `SetupRemoteHelper(t)` — configure git-remote-bitfs with regtest blockchain
- `MineBlocks(t, n)` — mine n blocks on regtest
- `FundWallet(t, addr, amount)` — send coins to an address
- `CloneAndVerify(t, url)` — clone from bitfs:// URL and verify content

All files tagged with `//go:build e2e`.

**Step 4: Verify Docker setup**

```bash
cd e2e && docker compose up -d && docker compose exec bsv-node bitcoin-cli -regtest getinfo
```

**Step 5: Commit**

```bash
git add e2e/
git commit -m "feat(e2e): add Docker regtest test infrastructure"
```

---

## Task 5: E2E test — push and clone

First e2e test: push a repo to bitfs:// and clone it back.

**Files:**
- Create: `e2e/01_push_clone_test.go`

**Step 1: Write the test**

```go
//go:build e2e

package e2e

func TestPushAndClone(t *testing.T) {
    // 1. Setup regtest RPC + mine 101 blocks (matured coinbase)
    // 2. Create wallet, fund it with regtest coins
    // 3. Create temp git repo with 3 files (README.md, main.go, .gitignore)
    // 4. git add + git commit
    // 5. Configure git remote: git remote add origin bitfs://<address>@regtest
    // 6. Run git-remote-bitfs export (push) via the helper protocol
    //    - Verify: broadcast succeeds, anchor TX on chain, UTXO state saved
    // 7. Create a new temp dir
    // 8. Run git-remote-bitfs import (clone) via the helper protocol
    //    - Verify: all 3 files restored with correct content
    //    - Verify: commit message matches
    //    - Verify: author matches
}
```

The test doesn't use `git push` directly (that would require the binary installed in PATH). Instead, it exercises the helper protocol programmatically via `helper.New()` + `helper.Run()`.

**Step 2: Run the test**

```bash
cd /Users/alex/Codes/RabbitHole/git-remote-bitfs
go test -tags e2e ./e2e/ -v -run TestPushAndClone -timeout 120s
```

Expected: PASS

**Step 3: Commit**

```bash
git add e2e/01_push_clone_test.go
git commit -m "test(e2e): push and clone roundtrip on regtest"
```

---

## Task 6: E2E test — incremental push and fetch

**Files:**
- Create: `e2e/02_incremental_test.go`

**Step 1: Write the test**

```go
//go:build e2e

package e2e

func TestIncrementalPushFetch(t *testing.T) {
    // 1. Setup (reuse testutil)
    // 2. Push initial commit (3 files)
    // 3. Make a second commit (add 1 file, modify 1 file)
    // 4. Push again
    //    - Verify: anchor chain has 2 entries (parent → child)
    //    - Verify: ref UTXO updated
    // 5. Fetch from a fresh helper (simulating another clone)
    //    - Verify: 2 commits in fast-import stream
    //    - Verify: all 4 files present with correct content
}
```

**Step 2: Run**

```bash
go test -tags e2e ./e2e/ -v -run TestIncrementalPushFetch -timeout 120s
```

**Step 3: Commit**

---

## Task 7: E2E test — private access (Method 42 encryption)

**Files:**
- Create: `e2e/03_private_access_test.go`

**Step 1: Write the test**

```go
//go:build e2e

package e2e

func TestPrivateAccessEncryption(t *testing.T) {
    // 1. Setup with .bitfsattributes: "* private"
    // 2. Push files
    //    - Verify: on-chain payload is encrypted (not plaintext)
    //    - Verify: key_hash present in TLV
    // 3. Clone with correct wallet
    //    - Verify: files decrypted correctly
    // 4. Attempt clone with different wallet
    //    - Verify: decryption fails or returns garbage
}
```

---

## Task 8: E2E test — branches and error handling

**Files:**
- Create: `e2e/04_branches_test.go`
- Create: `e2e/05_error_paths_test.go`

**Step 1: Write branch test**

```go
func TestMultipleBranches(t *testing.T) {
    // 1. Push refs/heads/main
    // 2. Push refs/heads/feature (different content)
    // 3. List refs — verify both branches visible
    // 4. Clone main — verify main content
    // 5. Clone feature — verify feature content
}
```

**Step 2: Write error handling test**

```go
func TestErrorPaths(t *testing.T) {
    // 1. Push with insufficient fee UTXOs → clear error
    // 2. Clone with no refs on remote → empty list, graceful
    // 3. Push with nil wallet → protocol-only mode succeeds
}
```

---

## Task 9: Update documentation

**Files:**
- Modify: `CLAUDE.md` — fix module name (`bitfsorg` not `tongxiaofeng`), add e2e test section
- Modify: `README.md` — add build/test/e2e instructions
- Modify: `/Users/alex/Codes/RabbitHole/docs/tasks/deferred.md` — update Anchor status (retained by design, not residual)

**Step 1: Fix CLAUDE.md**

```
Module: github.com/bitfsorg/git-remote-bitfs → already correct in go.mod
Remove "github.com/tongxiaofeng/git-remote-bitfs" reference
Add e2e test commands:
  go test -tags e2e ./e2e/ -v -timeout 120s   # requires Docker (see e2e/docker-compose.yml)
```

**Step 2: Update deferred.md**

Remove "Anchor code residual" item — Anchor is retained by design decision (2026-03-02).

**Step 3: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: update CLAUDE.md module name, add e2e instructions"
```

---

## Task 10: Final verification

**Step 1: Run full unit test suite**

```bash
go test ./... -count=1
```

Expected: All pass.

**Step 2: Run full e2e suite**

```bash
cd e2e && docker compose up -d
cd /Users/alex/Codes/RabbitHole/git-remote-bitfs
go test -tags e2e ./e2e/ -v -timeout 300s
cd e2e && docker compose down
```

Expected: All pass.

**Step 3: Build binary**

```bash
go build ./cmd/git-remote-bitfs
./git-remote-bitfs 2>&1 | head -5   # verify it runs
```

**Step 4: Review all changes**

```bash
git log --oneline main..HEAD
git diff --stat main..HEAD
```

Verify: clean, focused changes. No unnecessary modifications.

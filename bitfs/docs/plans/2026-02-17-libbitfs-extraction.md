# libbitfs Shared Core Library Extraction — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract 8 reusable packages from `bitfs/internal/` into `libbitfs/` as public Go packages.

**Architecture:** Copy 8 standalone packages (method42, wallet, spv, storage, config, paymail, x402, tx) from `bitfs/internal/` to `libbitfs/` top-level. Rewrite 20 import paths in bitfs. Use `replace` directive for local dev.

**Tech Stack:** Go 1.25.6, `github.com/bsv-blockchain/go-sdk` v1.2.18, `golang.org/x/crypto` v0.47.0

---

### Task 1: Initialize libbitfs go.mod

**Files:**
- Create: `../libbitfs/go.mod`
- Delete: `../libbitfs/cmd/`, `../libbitfs/doc/`, `../libbitfs/internal/`, `../libbitfs/spec/` (empty placeholder dirs)

**Step 1: Remove empty placeholder directories and init module**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
rm -rf cmd doc internal spec
go mod init github.com/tongxiaofeng/libbitfs
```

**Step 2: Set Go version**

Edit `go.mod` to set `go 1.25.6`.

**Step 3: Commit**

```bash
git add go.mod
git commit -m "chore: initialize go.mod for libbitfs shared library"
```

---

### Task 2: Copy 8 packages to libbitfs

**Files:**
- Copy from `bitfs/internal/{method42,wallet,spv,storage,config,paymail,x402,tx}/` → `libbitfs/{method42,wallet,spv,storage,config,paymail,x402,tx}/`

**Step 1: Copy all 8 package directories**

```bash
cd /Users/alex/Codes/RabbitHole
for pkg in method42 wallet spv storage config paymail x402 tx; do
  cp -r bitfs/internal/$pkg libbitfs/$pkg
done
```

**Step 2: Verify file count**

```bash
find libbitfs/ -name '*.go' | wc -l
```

Expected: ~55-60 Go files (source + test files across 8 packages).

**Step 3: Commit raw copy**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add .
git commit -m "chore: copy 8 packages from bitfs/internal (raw copy)"
```

---

### Task 3: Fix import paths in libbitfs and verify build

**Files:**
- Modify: ALL `.go` files in `libbitfs/` that import `github.com/tongxiaofeng/bitfs/internal/*`

**Step 1: Rewrite self-referencing import paths**

Search all Go files in libbitfs for imports of `github.com/tongxiaofeng/bitfs/internal/` and replace with `github.com/tongxiaofeng/libbitfs/`. There should be very few (the 8 packages are standalone), but test files may reference other packages.

The specific pattern:
```
OLD: "github.com/tongxiaofeng/bitfs/internal/method42"
NEW: "github.com/tongxiaofeng/libbitfs/method42"
```

(Repeat for all 8 package names.)

**Step 2: Add dependencies to go.mod**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
go mod tidy
```

This should pull in:
- `github.com/bsv-blockchain/go-sdk v1.2.18`
- `golang.org/x/crypto v0.47.0`
- `github.com/stretchr/testify v1.11.1`

**Step 3: Verify build**

```bash
go build ./...
```

Expected: Clean build, no errors.

**Step 4: Run all tests**

```bash
go test ./... -count=1
```

Expected: All tests pass. These are the same tests from bitfs, just in a new module.

**Step 5: Commit**

```bash
git add .
git commit -m "feat: fix import paths and verify libbitfs build + tests"
```

---

### Task 4: Add libbitfs dependency to bitfs go.mod

**Files:**
- Modify: `bitfs/go.mod`

**Step 1: Add require and replace directives**

Add to `bitfs/go.mod`:
```
require github.com/tongxiaofeng/libbitfs v0.0.0

replace github.com/tongxiaofeng/libbitfs => ../libbitfs
```

**Step 2: Verify go.mod is valid**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
go mod tidy
```

Expected: No errors. The replace directive points to the local libbitfs.

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add libbitfs dependency with local replace"
```

---

### Task 5: Rewrite imports in cmd/ (16 files)

**Files:**
- Modify: `cmd/bcat/main.go` — paymail
- Modify: `cmd/bget/main.go` — paymail
- Modify: `cmd/bls/main.go` — paymail
- Modify: `cmd/bstat/main.go` — paymail
- Modify: `cmd/btree/main.go` — paymail
- Modify: `cmd/bitfs/cmd_wallet.go` — config, wallet
- Modify: `cmd/bitfs/cmd_daemon.go` — config
- Modify: `cmd/bitfs/cmd_vault.go` — config
- Modify: `cmd/bitfs/cmd_put.go` — config
- Modify: `cmd/bitfs/cmd_sell.go` — config
- Modify: `cmd/bitfs/cmd_encrypt.go` — config
- Modify: `cmd/bitfs/cmd_link.go` — config
- Modify: `cmd/bitfs/cmd_mkdir.go` — config
- Modify: `cmd/bitfs/cmd_mv.go` — config
- Modify: `cmd/bitfs/cmd_rm.go` — config
- Modify: `cmd/bitfs/cmd_publish.go` — config

**Step 1: Bulk rewrite all cmd/ imports**

In every file under `cmd/`, replace:
```
"github.com/tongxiaofeng/bitfs/internal/config"   → "github.com/tongxiaofeng/libbitfs/config"
"github.com/tongxiaofeng/bitfs/internal/wallet"    → "github.com/tongxiaofeng/libbitfs/wallet"
"github.com/tongxiaofeng/bitfs/internal/paymail"   → "github.com/tongxiaofeng/libbitfs/paymail"
```

**Step 2: Verify cmd/ builds**

```bash
go build ./cmd/...
```

Expected: All 6 binaries build (bitfs, bls, bcat, bget, bstat, btree).

**Step 3: Commit**

```bash
git add cmd/
git commit -m "refactor: rewrite cmd/ imports to use libbitfs"
```

---

### Task 6: Rewrite imports in internal/metanet/ (2 files)

**Files:**
- Modify: `internal/metanet/parser.go` — tx import
- Modify: `internal/metanet/metanet_test.go` — tx import

**Step 1: Rewrite tx import**

In both files, replace:
```
"github.com/tongxiaofeng/bitfs/internal/tx" → "github.com/tongxiaofeng/libbitfs/tx"
```

**Step 2: Verify metanet package**

```bash
go test ./internal/metanet/ -count=1
```

Expected: All metanet tests pass.

**Step 3: Commit**

```bash
git add internal/metanet/
git commit -m "refactor: rewrite metanet imports to use libbitfs/tx"
```

---

### Task 7: Rewrite imports in integration/ (5 files)

**Files:**
- Modify: `integration/wallet_crypto_test.go` — method42, wallet
- Modify: `integration/payment_flow_test.go` — method42, wallet, x402 (daemon stays)
- Modify: `integration/tx_build_test.go` — method42, spv, tx, wallet (metanet stays)
- Modify: `integration/filesystem_test.go` — method42, storage, wallet (metanet stays)
- Modify: `integration/uri_resolve_test.go` — paymail, wallet

**Step 1: Rewrite all integration test imports**

In every file, apply the same substitution for all 8 package names:
```
"github.com/tongxiaofeng/bitfs/internal/{pkg}" → "github.com/tongxiaofeng/libbitfs/{pkg}"
```

Keep `internal/metanet` and `internal/daemon` imports unchanged (they stay in bitfs).

**Step 2: Run integration tests**

```bash
go test ./integration/ -count=1
```

Expected: All integration tests pass.

**Step 3: Commit**

```bash
git add integration/
git commit -m "refactor: rewrite integration test imports to use libbitfs"
```

---

### Task 8: Rewrite any remaining imports (coverage supplement tests, cmd/bitfs tests)

**Files:**
- Modify: `cmd/bitfs/coverage_supplement_test.go` — check for internal package imports

**Step 1: Search for any remaining old import paths**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
grep -r 'tongxiaofeng/bitfs/internal/\(method42\|wallet\|spv\|storage\|config\|paymail\|x402\|tx\)' --include='*.go' .
```

Expected: Zero matches. If any remain, rewrite them.

**Step 2: Full build and test verification**

```bash
go build ./...
go test ./... -count=1
```

Expected: Everything builds and all tests pass.

**Step 3: Commit if any changes**

```bash
git add .
git commit -m "refactor: fix remaining import paths to libbitfs"
```

---

### Task 9: Delete migrated packages from bitfs/internal/

**Files:**
- Delete: `internal/method42/`
- Delete: `internal/wallet/`
- Delete: `internal/spv/`
- Delete: `internal/storage/`
- Delete: `internal/config/`
- Delete: `internal/paymail/`
- Delete: `internal/x402/`
- Delete: `internal/tx/`

**Step 1: Delete the 8 package directories**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
rm -rf internal/method42 internal/wallet internal/spv internal/storage internal/config internal/paymail internal/x402 internal/tx
```

**Step 2: Tidy go.mod**

```bash
go mod tidy
```

**Step 3: Verify build and tests still pass**

```bash
go build ./...
go test ./... -count=1
```

Expected: All pass. bitfs now only has `internal/metanet/`, `internal/daemon/`, `internal/revshare/`.

**Step 4: Commit**

```bash
git add .
git commit -m "refactor: remove migrated packages from bitfs/internal"
```

---

### Task 10: Final verification — both repos green

**Step 1: Verify libbitfs**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
go build ./...
go test ./... -count=1 -v
```

Expected: All tests pass.

**Step 2: Verify bitfs**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
go build ./...
go test ./... -count=1 -v
```

Expected: All tests pass.

**Step 3: Verify remaining bitfs/internal/ structure**

```bash
ls /Users/alex/Codes/RabbitHole/bitfs/internal/
```

Expected: `daemon/  metanet/  revshare/` — only 3 directories remain.

**Step 4: Commit in libbitfs**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs
git add .
git commit -m "feat: libbitfs v0.1.0 — shared core library with 8 packages"
```

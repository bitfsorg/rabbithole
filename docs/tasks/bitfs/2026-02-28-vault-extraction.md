# Vault Extraction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract `bitfs/internal/engine/` to `libbitfs-go/vault/`, unifying state management with file locking, and refactor all bitfs consumers.

**Architecture:** Copy core engine operation files to `libbitfs-go/vault/`, rename `Engine` → `Vault`, add flock-based write serialization. Refactor bitfs to import `vault` directly — delete daemon adapters, create `internal/publish/`, rename `buyer/` → `buy/`, move mget/mput to CLI layer.

**Tech Stack:** Go 1.25, libbitfs-go (method42, wallet, tx, metanet, storage, network, spv), syscall/flock

---

## Phase 1: Create vault package in libbitfs-go

### Task 1: Copy engine operation files to libbitfs-go/vault/

**Files:**
- Create: `libbitfs-go/vault/` directory
- Source: `bitfs/internal/engine/` (selective copy)

**Step 1: Create vault directory and copy files**

Copy these files from `bitfs/internal/engine/` to `libbitfs-go/vault/`:

Core:
- `engine.go` → `vault.go`
- `state.go` → `state.go`
- `txbuild.go` → `txbuild.go`
- `helpers.go` → `helpers.go`
- `dir_update.go` → `dir_update.go`

Operations:
- `put.go`, `mkdir.go`, `copy.go`, `move.go`, `remove.go`
- `link.go`, `sell.go`, `encrypt.go`, `decrypt.go`
- `cat.go`, `get.go`

Tests:
- All corresponding `*_test.go` files for the above

Do NOT copy:
- `daemon_adapter.go`, `daemon_adapter_test.go` (deleted)
- `publish.go`, `publish_test.go` (→ `bitfs/internal/publish/`)
- `unpublish.go`, `unpublish_test.go` (→ `bitfs/internal/publish/`)
- `mget.go`, `mget_test.go` (→ CLI layer)
- `mput.go`, `mput_test.go` (→ CLI layer)

**Step 2: Rename package declarations**

In all copied files, change:
```go
// Before
package engine
// After
package vault
```

**Step 3: Commit**

```bash
cd libbitfs-go
git add vault/
git commit -m "vault: copy engine operation files (package rename only)"
```

---

### Task 2: Rename Engine → Vault and update internal references

**Files:**
- Modify: `libbitfs-go/vault/vault.go` (was engine.go)
- Modify: All other vault/*.go files that reference `Engine`

**Step 1: Rename types in vault.go**

In `vault.go`:
- Rename struct `Engine` → `Vault`
- Rename constructor `New(dataDir, password string) (*Engine, error)` → `New(dataDir, password string) (*Vault, error)`
- Update all method receivers from `(e *Engine)` to `(v *Vault)`
- Remove `DNS DNSResolver` field from Vault struct (moves to publish package)

**Step 2: Update all operation files**

In every `*.go` file under `vault/`:
- Replace receiver `(e *Engine)` → `(v *Vault)` (or `(e *Engine)` pattern)
- Replace all `e.Wallet` → `v.Wallet`, `e.Store` → `v.Store`, `e.State` → `v.State`, etc.
- Replace all `e.Chain` → `v.Chain`, `e.SPV` → `v.SPV`, etc.
- Update import paths: remove `bitfs/internal/engine` references if any

**Step 3: Update test files**

In all `*_test.go` files:
- Replace `engine.New(` → `vault.New(` (in test helpers)
- Replace `*engine.Engine` → `*vault.Vault`
- Replace `engine.Result` → `vault.Result`
- Replace `engine.MkdirOpts` → `vault.MkdirOpts` (all Opts types)
- Replace `engine.NodeState` → `vault.NodeState` (all state types)

**Step 4: Remove DNS-related code from vault.go**

Remove `DNSResolver` interface definition and `DNS` field from Vault struct.
Remove any `e.DNS` / `v.DNS` references (these only existed in publish.go which was not copied).

**Step 5: Verify compilation**

Run: `cd libbitfs-go && go build ./vault/`
Expected: clean build

**Step 6: Commit**

```bash
cd libbitfs-go
git add vault/
git commit -m "vault: rename Engine → Vault, update receivers and references"
```

---

### Task 3: Add file locking to state write operations

**Files:**
- Modify: `libbitfs-go/vault/state.go`
- Create: `libbitfs-go/vault/flock.go`
- Create: `libbitfs-go/vault/flock_test.go`

**Step 1: Write the flock test**

```go
// flock_test.go
package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileLock_ExclusiveAccess(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "vault.lock")

	fl, err := acquireLock(lockPath)
	require.NoError(t, err)
	defer releaseLock(fl)

	// Lock file should exist
	_, err = os.Stat(lockPath)
	assert.NoError(t, err)
}

func TestFileLock_BlocksSecondAcquire(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "vault.lock")

	fl1, err := acquireLock(lockPath)
	require.NoError(t, err)
	defer releaseLock(fl1)

	// Second acquire with tryLock should fail
	fl2, err := tryLock(lockPath)
	assert.Error(t, err)
	assert.Nil(t, fl2)
}

func TestFileLock_ReleaseThenReacquire(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "vault.lock")

	fl1, err := acquireLock(lockPath)
	require.NoError(t, err)
	releaseLock(fl1)

	fl2, err := acquireLock(lockPath)
	require.NoError(t, err)
	releaseLock(fl2)
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./vault/ -run TestFileLock -v`
Expected: FAIL (functions not defined)

**Step 3: Implement file locking**

```go
// flock.go
package vault

import (
	"fmt"
	"os"
	"syscall"
)

// acquireLock acquires an exclusive file lock (blocking).
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	return f, nil
}

// tryLock attempts a non-blocking exclusive lock. Returns error if already held.
func tryLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock held by another process: %w", err)
	}
	return f, nil
}

// releaseLock releases the file lock and closes the file.
func releaseLock(f *os.File) {
	if f == nil {
		return
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
}
```

**Step 4: Run tests to verify they pass**

Run: `cd libbitfs-go && go test ./vault/ -run TestFileLock -v`
Expected: PASS

**Step 5: Integrate locking into write operations**

Add a helper method to Vault that wraps write operations with the lock. In `vault.go`:

```go
// withWriteLock executes fn while holding an exclusive vault lock.
// It reloads state before fn and saves state after fn returns nil error.
func (v *Vault) withWriteLock(fn func() error) error {
	lockPath := filepath.Join(v.DataDir, "vault.lock")
	fl, err := acquireLock(lockPath)
	if err != nil {
		return fmt.Errorf("vault lock: %w", err)
	}
	defer releaseLock(fl)

	// Reload latest state to prevent stale reads
	if err := v.State.Reload(); err != nil {
		return fmt.Errorf("reload state: %w", err)
	}

	if err := fn(); err != nil {
		return err
	}

	return v.State.Save()
}
```

Add `Reload()` method to `LocalState` in `state.go`:

```go
// Reload re-reads the state file from disk (used after acquiring write lock).
func (s *LocalState) Reload() error {
	fresh, err := LoadLocalState(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Nodes = fresh.Nodes
	s.UTXOs = fresh.UTXOs
	s.RootTxID = fresh.RootTxID
	s.PublishBindings = fresh.PublishBindings
	return nil
}
```

**Step 6: Wrap each write operation with withWriteLock**

Each write operation (PutFile, Mkdir, Copy, Move, Remove, Link, Sell, EncryptNode, DecryptNode) should be wrapped. Example pattern for Mkdir:

```go
func (v *Vault) Mkdir(opts MkdirOpts) (*Result, error) {
	var result *Result
	err := v.withWriteLock(func() error {
		var err error
		result, err = v.mkdirInner(opts)
		return err
	})
	return result, err
}

// mkdirInner contains the actual mkdir logic (previously the body of Mkdir).
func (v *Vault) mkdirInner(opts MkdirOpts) (*Result, error) {
	// ... existing mkdir body ...
}
```

Apply this pattern to all 9 write operations. The inner functions contain the existing logic unchanged.

**Step 7: Run all vault tests**

Run: `cd libbitfs-go && go test ./vault/ -v -count=1`
Expected: all tests pass

**Step 8: Commit**

```bash
cd libbitfs-go
git add vault/
git commit -m "vault: add flock-based write serialization"
```

---

### Task 4: Verify vault package builds and tests pass

**Step 1: Run full libbitfs-go test suite**

Run: `cd libbitfs-go && go test ./... -count=1`
Expected: all packages pass (vault + existing 11 packages)

**Step 2: Run vault tests with race detector**

Run: `cd libbitfs-go && go test ./vault/ -race -count=1`
Expected: no race conditions

**Step 3: Run linter**

Run: `cd libbitfs-go && golangci-lint run ./vault/`
Expected: no issues

**Step 4: Commit any fixes**

```bash
cd libbitfs-go
git add -A && git commit -m "vault: fix lint and race issues"
```

---

## Phase 2: Refactor bitfs consumers

### Task 5: Create bitfs/internal/publish/ package

**Files:**
- Create: `bitfs/internal/publish/publish.go`
- Create: `bitfs/internal/publish/publish_test.go`
- Source: `bitfs/internal/engine/publish.go`, `unpublish.go`, and their tests

**Step 1: Create publish package**

Copy `publish.go` and `unpublish.go` from engine to `bitfs/internal/publish/`.

Change package to `publish`. The key change: functions take `*vault.Vault` as parameter instead of being methods on Engine.

```go
package publish

import (
	"github.com/tongxiaofeng/libbitfs-go/vault"
)

// DNSResolver abstracts DNS lookups for testing.
type DNSResolver interface {
	LookupTXT(domain string) ([]string, error)
}

// Publish binds a domain to a vault root node via DNSLink verification.
func Publish(v *vault.Vault, dns DNSResolver, opts PublishOpts) (*vault.Result, error) {
	// ... adapted from engine publish.go ...
}

// Unpublish removes a domain binding.
func Unpublish(v *vault.Vault, domain string) error {
	// ... adapted from engine unpublish.go ...
}
```

**Step 2: Copy and adapt tests**

Copy `publish_test.go` and `unpublish_test.go`, update to call `publish.Publish(v, dns, opts)` instead of `eng.Publish(opts)`.

**Step 3: Verify tests pass**

Run: `cd bitfs && go test ./internal/publish/ -v -count=1`
Expected: all 19 tests pass

**Step 4: Commit**

```bash
cd bitfs
git add internal/publish/
git commit -m "feat: create internal/publish package (extracted from engine)"
```

---

### Task 6: Rename bitfs/internal/buyer/ → bitfs/internal/buy/

**Files:**
- Rename: `bitfs/internal/buyer/` → `bitfs/internal/buy/`
- Modify: all files that import `internal/buyer`

**Step 1: Rename directory**

```bash
cd bitfs
mv internal/buyer internal/buy
```

**Step 2: Update package declaration**

In all files under `internal/buy/`:
```go
// Before
package buyer
// After
package buy
```

**Step 3: Update all imports**

Search for `internal/buyer` in the entire bitfs codebase and replace with `internal/buy`. This includes:
- `cmd/bget/main.go`
- `cmd/bcat/main.go`
- `cmd/bls/main.go`
- `cmd/bstat/main.go`
- `cmd/btree/main.go`
- `cmd/bmget/main.go`
- Any integration test files

Also update all `buyer.` references to `buy.`:
- `buyer.ExitCodeFromError` → `buy.ExitCodeFromError`
- `buyer.HandleError` → `buy.HandleError`
- etc.

**Step 4: Verify tests pass**

Run: `cd bitfs && go test ./internal/buy/ -v -count=1`
Expected: all buyer tests pass

Run: `cd bitfs && go build ./cmd/bget/ ./cmd/bcat/ ./cmd/bls/ ./cmd/bstat/ ./cmd/btree/ ./cmd/bmget/`
Expected: clean build

**Step 5: Commit**

```bash
cd bitfs
git add -A
git commit -m "refactor: rename internal/buyer → internal/buy"
```

---

### Task 7: Refactor cmd/bitfs/ to import vault instead of engine

**Files:**
- Modify: all `bitfs/cmd/bitfs/cmd_*.go` files
- Modify: `bitfs/cmd/bitfs/cmd_shell.go`

**Step 1: Update imports**

In all cmd/bitfs/*.go files, replace:
```go
"github.com/tongxiaofeng/bitfs/internal/engine"
```
with:
```go
"github.com/tongxiaofeng/libbitfs-go/vault"
```

**Step 2: Update type references**

Global replacements across cmd/bitfs/:
- `engine.New(` → `vault.New(`
- `*engine.Engine` → `*vault.Vault`
- `engine.Result` → `vault.Result`
- `engine.MkdirOpts` → `vault.MkdirOpts` (and all other Opts types)
- `engine.NodeState` → `vault.NodeState`
- `engine.LocalState` → `vault.LocalState`
- `eng.` variable references may stay as-is (or rename `eng` → `v` for clarity)

**Step 3: Update publish/unpublish command handlers**

In `cmd_publish.go` and `cmd_unpublish.go`, change from:
```go
result, err := eng.Publish(&engine.PublishOpts{...})
```
to:
```go
result, err := publish.Publish(v, dnsResolver, publish.PublishOpts{...})
```

Import `bitfs/internal/publish`.

**Step 4: Update mget/mput command handlers**

mget/mput logic stays in cmd layer. The functions currently in `engine/mget.go` and `engine/mput.go` need to be adapted to take `*vault.Vault` as parameter. Create these as functions in cmd_mget.go and cmd_mput.go (or a small helper file).

**Step 5: Update daemon startup (cmd_daemon.go)**

Change from:
```go
eng, err := engine.New(*dataDir, pass)
walletAdapter := engine.NewWalletAdapter(eng)
storeAdapter := engine.NewStoreAdapter(eng)
metanetAdapter := engine.NewMetanetAdapter(eng)
d, err := daemon.New(cfg, walletAdapter, storeAdapter, metanetAdapter)
```
to:
```go
v, err := vault.New(*dataDir, pass)
d, err := daemon.New(cfg, v)
```

**Step 6: Verify build**

Run: `cd bitfs && go build ./cmd/bitfs/`
Expected: clean build

**Step 7: Commit**

```bash
cd bitfs
git add cmd/bitfs/
git commit -m "refactor: cmd/bitfs uses vault instead of engine"
```

---

### Task 8: Refactor daemon to use vault directly

**Files:**
- Modify: `bitfs/internal/daemon/daemon.go`
- Modify: `bitfs/internal/daemon/routes.go`
- Modify: `bitfs/internal/daemon/handshake.go` (and other handler files)
- Delete: references to adapter interfaces

**Step 1: Change daemon.New() signature**

In `daemon.go`, change constructor from:
```go
func New(cfg Config, wallet WalletService, store ContentStore, meta MetanetService) (*Daemon, error)
```
to:
```go
func New(cfg Config, v *vault.Vault) (*Daemon, error)
```

Update the Daemon struct:
```go
type Daemon struct {
	cfg   Config
	vault *vault.Vault
	// ... other fields (logger, mux, etc.)
}
```

**Step 2: Remove interface definitions**

Delete `WalletService`, `ContentStore`, `MetanetService`, `SPVService`, `ChainService` interfaces from daemon.go. Keep supporting types (`NodeInfo`, `ChildInfo`, `SPVResult`) if still needed, or convert them to use vault types directly.

**Step 3: Update all handler methods**

Replace interface calls with direct vault access. Examples:

```go
// Before (via adapter)
pubKey, err := d.wallet.DeriveNodePubKey(vaultIndex, path, hardened)

// After (direct vault access)
pubKey, err := d.vault.Wallet.DeriveVaultRootKey(vaultIndex)
// (adjust based on actual wallet method signatures)
```

```go
// Before
data, err := d.store.Get(keyHash)

// After
data, err := d.vault.Store.Get(keyHash)
```

```go
// Before
node, err := d.meta.GetNodeByPath(path)

// After
nodeState := d.vault.State.FindNodeByPath(path)
```

```go
// Before
result, err := d.spv.VerifyTx(ctx, txid)

// After
result, err := d.vault.VerifyTx(ctx, txid)
```

```go
// Before
txid, err := d.chain.BroadcastTx(ctx, rawHex)

// After
txid, err := d.vault.BroadcastTx(ctx, rawHex)
```

**Step 4: Remove SetSPV/SetChain methods**

These were needed to inject optional adapters. Now vault already has SPV and Chain fields — daemon accesses them via `d.vault.SPV` and `d.vault.Chain` (nil-checking as needed).

**Step 5: Update daemon tests**

Daemon tests that previously created mock adapters should now create a `*vault.Vault` with test fixtures. Use `vault.New()` with a temp directory and test wallet.

**Step 6: Verify tests pass**

Run: `cd bitfs && go test ./internal/daemon/ -v -count=1`
Expected: all daemon tests pass

**Step 7: Commit**

```bash
cd bitfs
git add internal/daemon/
git commit -m "refactor: daemon uses vault directly, delete adapter interfaces"
```

---

### Task 9: Delete bitfs/internal/engine/

**Step 1: Delete the directory**

```bash
cd bitfs
rm -rf internal/engine/
```

**Step 2: Verify no remaining imports**

Search for any remaining `internal/engine` imports:
```bash
grep -r "internal/engine" --include="*.go" .
```
Expected: no matches

**Step 3: Verify build**

Run: `cd bitfs && go build ./...`
Expected: clean build

**Step 4: Commit**

```bash
cd bitfs
git add -A
git commit -m "refactor: delete internal/engine (replaced by libbitfs-go/vault)"
```

---

## Phase 3: Fix tests and verify

### Task 10: Fix integration tests

**Files:**
- Modify: `bitfs/integration/engine_state_test.go`
- Modify: `bitfs/integration/engine_workflow_test.go`
- Modify: `bitfs/integration/daemon_engine_test.go`
- Modify: `bitfs/integration/engine_helpers_test.go`
- Modify: `bitfs/integration/shell_commands_test.go`
- Modify: `bitfs/integration/client_roundtrip_test.go`

**Step 1: Update imports**

In all 6 integration test files, replace:
```go
"github.com/tongxiaofeng/bitfs/internal/engine"
```
with:
```go
"github.com/tongxiaofeng/libbitfs-go/vault"
```

**Step 2: Update type references**

Same pattern as Task 7:
- `engine.New(` → `vault.New(`
- `engine.*Opts` → `vault.*Opts`
- `engine.Result` → `vault.Result`
- `engine.NodeState` → `vault.NodeState`
- Remove adapter references (`engine.NewWalletAdapter`, etc.)

For `daemon_engine_test.go`: update to create daemon with `daemon.New(cfg, v)` instead of passing adapters.

**Step 3: Consider renaming test files**

Optionally rename for clarity:
- `engine_state_test.go` → `vault_state_test.go`
- `engine_workflow_test.go` → `vault_workflow_test.go`
- `daemon_engine_test.go` → `daemon_vault_test.go`
- `engine_helpers_test.go` → `vault_helpers_test.go`

**Step 4: Run integration tests**

Run: `cd bitfs && go test -tags=integration ./integration/ -v -count=1`
Expected: all 276 tests pass

**Step 5: Commit**

```bash
cd bitfs
git add integration/
git commit -m "test: update integration tests for vault extraction"
```

---

### Task 11: Fix e2e tests

**Files:**
- Modify: `bitfs/e2e/13_sell_pricing_test.go`
- Modify: `bitfs/e2e/16_fund_external_test.go`

**Step 1: Update imports and references**

Same pattern: `engine` → `vault`, adapter removals.

**Step 2: Verify build (no need to run — requires Docker)**

Run: `cd bitfs && go vet -tags=e2e ./e2e/`
Expected: clean

**Step 3: Commit**

```bash
cd bitfs
git add e2e/
git commit -m "test: update e2e tests for vault extraction"
```

---

### Task 12: Full verification

**Step 1: Run all libbitfs-go tests**

Run: `cd libbitfs-go && go test ./... -race -count=1`
Expected: all pass (including new vault package)

**Step 2: Run all bitfs unit tests**

Run: `cd bitfs && go test ./... -count=1`
Expected: all pass

**Step 3: Run bitfs integration tests**

Run: `cd bitfs && go test -tags=integration ./integration/ -race -count=1`
Expected: all 276 pass

**Step 4: Run linters**

Run: `cd libbitfs-go && golangci-lint run ./...`
Run: `cd bitfs && golangci-lint run ./...`
Expected: no issues

**Step 5: Verify binary builds**

Run: `cd bitfs && go build ./cmd/bitfs/ && go build ./cmd/bls/ && go build ./cmd/bcat/ && go build ./cmd/bget/ && go build ./cmd/bstat/ && go build ./cmd/btree/ && go build ./cmd/bmget/`
Expected: all binaries build

**Step 6: Final commit (if any remaining fixes)**

```bash
cd bitfs
git add -A && git commit -m "fix: vault extraction cleanup"
```

---

## Summary

| Phase | Tasks | Description |
|-------|-------|-------------|
| 1 | 1-4 | Create `libbitfs-go/vault/` package with file locking |
| 2 | 5-9 | Refactor bitfs: publish/, buy/, cmd/, daemon/, delete engine/ |
| 3 | 10-12 | Fix tests, full verification |

**Estimated scope:**
- ~3,500 lines moved to libbitfs-go/vault/
- ~190 lines deleted (daemon adapters)
- ~300 lines new (publish package, flock, mget/mput in CLI)
- ~2,500 lines import path updates (tests + cmd + daemon)

**Not in scope:**
- git-remote-bitfs refactoring (separate future task)
- Paymail verify endpoint
- Design doc L2/L3 updates

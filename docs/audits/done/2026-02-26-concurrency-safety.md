# Concurrency Safety Audit Report

**Date:** 2026-02-26
**Scope:** libbitfs-go (10 packages) + bitfs (engine, daemon, cmd, client)
**Method:** 4 parallel agents, full source code review (non-test .go files only)
**Branch:** fix/audit-medium-batch3 (post batch 3 fixes)

> **2026-03-03 Closure**: All 40 findings (1C + 10H + 17M + 12L) confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md` section 2.4 (Concurrency Safety). Engine-level mutex (H-1), daemon snapshot-by-value (C-1), SPV ErrDuplicateHeader tolerance (H-10), atomic state save (M-8), and all remaining items addressed. Report archived to `done/`.

---

## Executive Summary

The codebase has **40 concurrency findings**: 1 CRITICAL, 10 HIGH, 17 MEDIUM, 12 LOW.

Three architectural root causes account for the majority of findings:

1. **Engine lacks concurrency protection.** The engine was designed for single-writer access (CLI), but the daemon's HTTP server introduces concurrent goroutines that call engine methods without any serialization. This is the single largest source of risk.

2. **Pointer escapes lock scope in daemon.** The daemon extracts `*InvoiceRecord` (and `*Session`) pointers from locked maps, releases the lock, then reads/writes struct fields without protection. This is a confirmed data race detectable by `go test -race`.

3. **Library types lack concurrency contracts.** Core types like `Node`, `WalletState`, `MetanetTx` have no documented thread-safety guarantees. Callers cannot determine whether external locking is required.

| Severity | Count |
|----------|-------|
| CRITICAL | 1 |
| HIGH | 10 |
| MEDIUM | 17 |
| LOW | 12 |
| **Total** | **40** |

### Safe Packages

The following packages are **fully safe for concurrent use** — all functions are purely functional with no shared mutable state:

- `libbitfs-go/method42` — hash/cipher/ECDH operations
- `libbitfs-go/config` — LoadConfig/ValidateConfig (SaveConfig has LOW file-level concern)
- `libbitfs-go/paymail` — all `*WithClient`/`*WithResolver` variants
- `libbitfs-go/x402` — all transaction-building and verification functions
- `bitfs/internal/client` — http.Client usage is correct
- `bitfs/cmd/bcat`, `bget`, `bls`, `bstat`, `btree` — stateless CLI tools

---

## CRITICAL

### C-1: Data Race on InvoiceRecord Fields After Lock Release

| | |
|---|---|
| **File** | `bitfs/internal/daemon/payment.go:124-327` |
| **Severity** | CRITICAL |
| **Category** | Data race (confirmed by analysis, detectable by `-race`) |
| **Status** | FIXED |

**Description:**

`handleGetBuyInfo` (line 124-126) and `handleSubmitHTLC` (line 219-239) extract an `*InvoiceRecord` pointer from `d.invoices` map under `invoicesMu`, then immediately release the lock. All subsequent field accesses (`Paid`, `Expiry`, `Capsule`, `HTLCScript`, `NodePNode`, `TotalPrice`, `CapsuleHash`, etc.) occur **without holding any lock**.

Concurrent HTTP requests for the same invoice produce unsynchronized read/write pairs:

- `handleGetBuyInfo` writes `invoice.HTLCScript` (line 174) under Lock
- `handleSubmitHTLC` reads `invoice.HTLCScript` (line 249) without lock
- `handleSubmitHTLC` writes `invoice.Paid = true` (line 238) under Lock
- `handleGetBuyInfo` reads `invoice.Paid` (line 193) without lock
- `persistInvoice` calls `json.Marshal(inv)` (line 319) without lock — reads all fields

The rollback closure in `handleSubmitHTLC` (line 243-247) writes `invoice.Paid = false` under the lock, but other goroutines reading `invoice.*` fields between lock release and rollback observe inconsistent state.

**Root pattern:** Locking the map protects map-level operations (insert/delete/lookup) but does NOT protect the struct fields of the values stored in the map. Once the `*InvoiceRecord` pointer leaves the critical section, all field accesses on it are unprotected.

**Suggested fix:** Snapshot all needed fields into local variables while holding the lock before releasing it:

```go
d.invoicesMu.RLock()
inv, ok := d.invoices[txid]
if !ok {
    d.invoicesMu.RUnlock()
    writeJSONError(...)
    return
}
// Copy all needed fields under the lock.
snap := invoiceSnapshot{
    Expiry:     inv.Expiry,
    NodePNode:  inv.NodePNode,
    Capsule:    inv.Capsule,
    HTLCScript: inv.HTLCScript,
    Paid:       inv.Paid,
    // ...
}
d.invoicesMu.RUnlock()
// Use snap.* instead of invoice.* below.
```

Alternatively, add a per-record `sync.RWMutex` to `InvoiceRecord`.

---

## HIGH

### H-1: Engine Has No Concurrency Protection — Daemon Violates Single-Writer Assumption

| | |
|---|---|
| **File** | `bitfs/internal/engine/` (global), `bitfs/cmd/bitfs/cmd_daemon.go` |
| **Severity** | HIGH |
| **Category** | Missing synchronization (architectural) |
| **Status** | FIXED |

**Description:**

The Engine was designed for single-writer access. The comment at `mkdir.go:166` states:

> "NOTE: This function is not safe for concurrent use. The Engine assumes a single-writer model — concurrent callers must be serialized externally (e.g., the daemon HTTP server serializes write operations through a mutex)."

However, examining `cmd_daemon.go:90-108`, the daemon is constructed with adapters but **no engine-level mutex is established**. The daemon's HTTP server (`d.Start()`) runs inherently concurrent goroutines per request. There is no evidence of a daemon-level serialization mutex wrapping engine calls.

This means every engine write method (`PutFile`, `Mkdir`, `Remove`, `Move`, `Copy`, `Link`, `Sell`, `EncryptNode`) is unsafe under the daemon. All subsequent engine-level findings (H-2 through H-4, M-6 through M-10) are symptoms of this architectural gap.

**Suggested fix:** Add a `sync.Mutex` (or `sync.RWMutex`) to Engine. All write-path operations hold the write lock. Read-path operations hold the read lock.

---

### H-2: WalletState.NextChangeIndex Read-Increment Without Synchronization

| | |
|---|---|
| **File** | `bitfs/internal/engine/engine.go:217-226` |
| **Severity** | HIGH |
| **Category** | Race condition |
| **Status** | FIXED |

**Description:**

`DeriveChangeAddr()` reads and increments `e.WState.NextChangeIndex` without any lock:

```go
func (e *Engine) DeriveChangeAddr() ([]byte, *ec.PrivateKey, error) {
    idx := e.WState.NextChangeIndex   // unsynchronized read
    kp, err := e.Wallet.DeriveFeeKey(wallet.InternalChain, idx)
    ...
    e.WState.NextChangeIndex++        // unsynchronized increment
    ...
}
```

Concurrent calls (e.g., two simultaneous `put` requests via daemon) both read the same index, derive the same change key — a **key reuse** that is both a privacy failure and a potential double-spend from the same UTXO. Same issue for `e.WState.NextReceiveIndex` in `lookupPrivKey` (lines 294, 303).

**Suggested fix:** Protected by the engine-level mutex (H-1 fix), or wrap `WalletState` in its own `sync.Mutex`.

---

### H-3: Direct Unsynchronized Slice Truncation on LocalState.UTXOs

| | |
|---|---|
| **File** | `bitfs/internal/engine/move.go:273` |
| **Severity** | HIGH |
| **Category** | Race condition / data loss |
| **Status** | FIXED |

**Description:**

`crossDirectoryMove` captures a snapshot of UTXO count, then in a rollback defer truncates the slice directly:

```go
utxoSnapshot := len(e.State.UTXOs)   // unsynchronized read
// ... operations add UTXOs via e.State.AddUTXO (which acquires mu) ...
defer func() {
    if !allSuccess {
        e.State.UTXOs = e.State.UTXOs[:utxoSnapshot]  // bypasses LocalState.mu
    }
}()
```

This bypasses `LocalState.mu` entirely. If a concurrent operation appends UTXOs between snapshot and rollback, the truncation silently deletes them — permanent loss of fee UTXOs.

**Suggested fix:** Add a `TruncateUTXOs(snapshot int)` method to `LocalState` that acquires the mutex. Or redesign rollback to mark specific UTXOs rather than truncating.

---

### H-4: Direct Access to LocalState Internal Lock for RootTxID

| | |
|---|---|
| **File** | `bitfs/internal/engine/helpers.go:144-146` |
| **Severity** | HIGH |
| **Category** | Broken encapsulation / race risk |
| **Status** | FIXED |

**Description:**

`createRootNode` bypasses `LocalState`'s API to update `RootTxID`:

```go
e.State.SetNode(rootPubHex, rootState)
e.State.mu.Lock()               // direct access to unexported field
e.State.RootTxID[vaultIdx] = result.TxID
e.State.mu.Unlock()
```

This defeats the purpose of the guarded accessor pattern and makes lock ordering analysis impossible.

**Suggested fix:** Add a `SetRootTxID(vaultIdx uint32, txid string)` method to `LocalState`.

---

### H-5: Concurrent GET/POST Handlers Race on invoice.HTLCScript

| | |
|---|---|
| **File** | `bitfs/internal/daemon/payment.go:171-174 vs 249-267` |
| **Severity** | HIGH |
| **Category** | Data race |
| **Status** | FIXED |

**Description:**

`handleGetBuyInfo` writes `invoice.HTLCScript` (line 174) under `invoicesMu.Lock()`. `handleSubmitHTLC` reads `invoice.HTLCScript` (line 249) outside any lock. If these two handlers run concurrently on the same invoice, this is a genuine data race. Same issue for `invoice.Capsule` and `invoice.CapsuleHash`.

**Suggested fix:** Part of C-1 fix — snapshot fields under lock.

---

### H-6: Session Expiry Check TOCTOU

| | |
|---|---|
| **File** | `bitfs/internal/daemon/daemon.go:397-414` |
| **Severity** | HIGH |
| **Category** | TOCTOU |
| **Status** | FIXED |

**Description:**

`GetSession` releases `RLock` then checks `IsExpired()` without lock:

```go
d.sessionsMu.RLock()
session, ok := d.sessions[id]
d.sessionsMu.RUnlock()           // lock released

if session.IsExpired() {         // check without lock
    d.sessionsMu.Lock()
    delete(d.sessions, id)       // act under new lock
    d.sessionsMu.Unlock()
    return nil, ErrSessionExpired
}
```

Between RUnlock and IsExpired, `cleanupExpiredSessions` can delete the session. `Session.ExpiresAt` is a `time.Time` (non-atomic struct) — concurrent reads are technically a race even though Sessions are immutable after creation.

**Suggested fix:** Hold RLock through the `IsExpired()` check:

```go
d.sessionsMu.RLock()
session, ok := d.sessions[id]
expired := ok && session.IsExpired()
d.sessionsMu.RUnlock()
if expired { ... delete under Lock ... }
```

---

### H-7: SetSPV/SetChain Write Interface Fields Without Synchronization

| | |
|---|---|
| **File** | `bitfs/internal/daemon/daemon.go:287-293` |
| **Severity** | HIGH |
| **Category** | Race condition |
| **Status** | FIXED |

**Description:**

```go
func (d *Daemon) SetSPV(spv SPVService) { d.spv = spv }
func (d *Daemon) SetChain(c ChainService) { d.chain = c }
```

Both fields are read by HTTP handlers (`spv.go:25`, `payment.go:297`). Go interface values are not atomically assignable. Convention says "call before Start" but this is not enforced.

**Suggested fix:** Guard with `d.mu`, or enforce before-Start semantics with a runtime check.

---

### H-8: Double-Spend Window — Two-Phase Rollback Under Different Locks

| | |
|---|---|
| **File** | `bitfs/internal/daemon/payment.go:237-310` |
| **Severity** | HIGH |
| **Category** | Atomicity violation |
| **Status** | FIXED |

**Description:**

`handleSubmitHTLC` uses an optimistic lock pattern: set `invoice.Paid = true` under `invoicesMu`, then verify payment outside the lock, then rollback on failure. The rollback of `usedTxIDs` (lines 302-304) and `invoice.Paid` (line 305) are done under **different locks** with a gap between them, creating a window where combined state is temporarily inconsistent.

A concurrent request with the same transaction ID could slip through during the gap between the two rollbacks.

**Suggested fix:** Consolidate both state changes under a single lock, or merge `usedTxIDs` into the `invoicesMu`-protected structure.

---

### H-9: Node/Directory Mutation Without Synchronization

| | |
|---|---|
| **File** | `libbitfs-go/metanet/directory.go:39-136`, `node.go:128` |
| **Severity** | HIGH |
| **Category** | Race condition |
| **Status** | FIXED |

**Description:**

`AddChild`, `RemoveChild`, `RenameChild` all mutate `*Node.Children` and `MerkleRoot` without any locking:

```go
// directory.go:79
dirNode.Children = append(dirNode.Children, entry)
dirNode.NextChildIndex++
recomputeMerkleRoot(dirNode)

// directory.go:98
dirNode.Children = append(dirNode.Children[:i], dirNode.Children[i+1:]...)
```

`Node.Metadata map[string]string` is also an exported mutable map with no sync. If the daemon shares `*Node` objects between a write path and an HTTP serve path, a race exists. Maps are NOT safe for concurrent read+write in Go.

**Suggested fix:** Either (a) add a `sync.RWMutex` to `Node`, or (b) enforce single-writer access at the engine level (H-1 fix), or (c) treat nodes as immutable values (copy-on-write).

---

### H-10: SPV ErrDuplicateHeader Causes Spurious Failures

| | |
|---|---|
| **File** | `libbitfs-go/network/spvclient.go:68-83, 130-186` |
| **Severity** | HIGH |
| **Category** | TOCTOU / missing error handling |
| **Status** | FIXED |

**Description:**

Both `VerifyTx` and `SyncHeaders` perform check-then-act on the header store without guarding against concurrent execution:

- `VerifyTx`: Two goroutines verify transactions in the same block → both see "header not found" → both fetch and `PutHeader` → second call returns `ErrDuplicateHeader` → treated as fatal → valid verification aborted.
- `SyncHeaders`: Same pattern — both goroutines iterate the same range, both `PutHeader`, both get `ErrDuplicateHeader` partway through.

**Suggested fix:** Treat `ErrDuplicateHeader` as non-fatal:

```go
if storeErr := s.headers.PutHeader(header); storeErr != nil {
    if !errors.Is(storeErr, spv.ErrDuplicateHeader) {
        return fmt.Errorf("network: store header: %w", storeErr)
    }
    // Another goroutine stored it first — OK.
}
```

And add a `sync.Mutex` to `SPVClient.SyncHeaders` to prevent redundant concurrent syncs.

---

## MEDIUM

### M-1: Response Encoding Reads Invoice Fields Without Lock

| | |
|---|---|
| **File** | `bitfs/internal/daemon/payment.go:184-194` |
| **Severity** | MEDIUM |
| **Category** | Data race |

After the write lock is released at line 181, the JSON response at lines 184-194 reads `invoice.ID`, `invoice.TotalPrice`, `invoice.CapsuleHash`, `invoice.Paid`, etc. without any lock. Related to C-1 root cause.

---

### M-2: persistInvoice Reads Invoice Fields Outside Lock

| | |
|---|---|
| **File** | `bitfs/internal/daemon/daemon.go:447-468`, `payment.go:319` |
| **Severity** | MEDIUM |
| **Category** | Data race |

`persistInvoice(invoice)` calls `json.Marshal(inv)` which reads all exported fields. Called at `payment.go:319` outside any lock. Concurrent `handleGetBuyInfo` writing `invoice.Capsule` under its lock races with the marshal. Related to C-1 root cause.

---

### M-3: Invoice Expiry Check TOCTOU

| | |
|---|---|
| **File** | `bitfs/internal/daemon/payment.go:133-140` |
| **Severity** | MEDIUM |
| **Category** | TOCTOU |

After RUnlock, `time.Now().After(invoice.Expiry)` checks expiry without lock, then re-acquires Lock to delete. Between the check and deletion, `cleanupExpiredInvoices` or `handleSubmitHTLC` can modify the invoice state. Related to C-1 root cause.

---

### M-4: Stop() Signals Cleanup Goroutine Before Draining Handlers

| | |
|---|---|
| **File** | `bitfs/internal/daemon/daemon.go:354-358` |
| **Severity** | MEDIUM |
| **Category** | Lifecycle ordering |

`close(d.stopCleanup)` is called before `d.server.Shutdown(ctx)` returns. The cleanup goroutine exits while in-flight HTTP handlers are still running and accessing sessions/invoices/rate-limiter maps.

**Suggested fix:** Close `stopCleanup` after `Shutdown` returns.

---

### M-5: Package-Level Mutable Function Variables Race in Concurrent Tests

| | |
|---|---|
| **File** | `bitfs/internal/daemon/handshake.go:172`, `content.go:16` |
| **Severity** | MEDIUM |
| **Category** | Data race (test injection pattern) |

`var cryptoRandRead` and `var randRead` are package-level mutable function values. Tests overwrite them while HTTP handlers read them — races under `t.Parallel()`.

**Suggested fix:** Inject as `Daemon` struct fields rather than package-level variables.

---

### M-6: RefreshFeeUTXOs Iterates UTXOs Without Lock

| | |
|---|---|
| **File** | `bitfs/internal/engine/engine.go:452-468` |
| **Severity** | MEDIUM |
| **Category** | Data race |

`for _, u := range e.State.UTXOs` reads the slice header directly without holding `LocalState.mu`. A concurrent `AddUTXO` that triggers slice reallocation causes a data race.

**Suggested fix:** Add `ContainsUTXO(txid, vout)` method to `LocalState` that holds the mutex.

---

### M-7: GetNode/FindNodeByPath Return Raw Pointers — Callers Mutate Without Lock

| | |
|---|---|
| **File** | `bitfs/internal/engine/state.go` (all getters) |
| **Severity** | MEDIUM |
| **Category** | Pointer aliasing |

All `LocalState` getter methods return raw `*NodeState` pointers. Callers then mutate fields directly:

```go
// put.go:179-187
parent.Children = append(parent.Children, &ChildState{...})
parent.NextChildIdx = childIdx + 1

// sell.go:108-110
nodeState.TxID = txIDHex
nodeState.Access = "paid"
```

No lock is held during these mutations. Under concurrent daemon use, two handlers modifying the same node race.

**Suggested fix:** Return deep copies, or protect all writes with the engine-level mutex (H-1 fix).

---

### M-8: State Save() Uses Non-Atomic os.WriteFile

| | |
|---|---|
| **File** | `bitfs/internal/engine/state.go:125-138` |
| **Severity** | MEDIUM |
| **Category** | File system race / crash safety |

`Save()` calls `os.WriteFile(s.path, data, 0600)` which truncates then writes. Process crash during write leaves `nodes.json` corrupted (partially written or zero-byte). Two concurrent CLI processes both calling `Save()` produces last-writer-wins with data loss.

**Suggested fix:** Write to `.tmp` then `os.Rename`:

```go
tmp := s.path + ".tmp"
os.WriteFile(tmp, data, 0600)
os.Rename(tmp, s.path)
```

---

### M-9: VerifyTx SPV Cache Check-Then-Act Without Lock

| | |
|---|---|
| **File** | `bitfs/internal/engine/engine.go:135-188` |
| **Severity** | MEDIUM |
| **Category** | TOCTOU |

`VerifyTx` checks SPV cache, on miss does network call, then writes back — classic check-then-act. Two concurrent calls for the same txid produce redundant network calls and duplicate store writes.

**Suggested fix:** Use `singleflight.Group` keyed by txid.

---

### M-10: crossDirectoryMove Multi-Defer Rollback Interleaving

| | |
|---|---|
| **File** | `bitfs/internal/engine/move.go:240-347` |
| **Severity** | MEDIUM |
| **Category** | Incomplete rollback |

Multiple deferred closures with a shared `allSuccess` flag interact with UTXO slice truncation. Defers execute LIFO, so `srcNodeUS.Spent = false` runs after the slice truncation, but if new UTXOs were added between snapshot and rollback, the UTXO referenced by `srcNodeUS` may not be reachable through `AllocateFeeUTXO`.

**Suggested fix:** Consolidate into a single rollback defer that atomically reverts all changes.

---

### M-11: shellCompleter Fields Raced Between Readline and Main Loop

| | |
|---|---|
| **File** | `bitfs/cmd/bitfs/completer.go:19-28` |
| **Severity** | MEDIUM |
| **Category** | Data race |

`shellCompleter` has mutable fields (`cwd`, `localCwd`, `cacheDir`, `cacheNode`, `cacheExpiry`) written by the main REPL loop and read by the readline library's autocomplete callback (potentially from a separate goroutine).

**Suggested fix:** Add `sync.RWMutex` to `shellCompleter`, or confirm readline calls `Do()` synchronously.

---

### M-12: Wallet.masterKey.Child() Thread Safety Depends on go-sdk Internals

| | |
|---|---|
| **File** | `libbitfs-go/wallet/hd.go:79-99` |
| **Severity** | MEDIUM |
| **Category** | Potential race (third-party dependency) |

`deriveAccount` calls `w.masterKey.Child(...)`. Whether `bip32.ExtendedKey.Child` mutates the receiver's internal state is opaque. If it does, concurrent `DeriveNodeKey` calls on the same `Wallet` race on `masterKey` internals. The `Wallet` carries no mutex.

**Suggested fix:** Verify go-sdk `bip32.ExtendedKey.Child` is stateless, or add `sync.RWMutex` to `Wallet`, or document that `Wallet` requires external synchronization.

---

### M-13: MetaFlagBytes Is an Exported Mutable []byte Slice

| | |
|---|---|
| **File** | `libbitfs-go/tx/opreturn.go:12-14` |
| **Severity** | MEDIUM |
| **Category** | Latent corruption hazard |

`var MetaFlagBytes = []byte{0x6d, 0x65, 0x74, 0x61}` is an exported mutable slice. Any caller can `MetaFlagBytes[0] = 0x00`, corrupting all subsequent `BuildOPReturnData` and `ParseOPReturnData` calls.

**Suggested fix:** Change to `var MetaFlagBytes = [4]byte{...}` (array, not slice) or make unexported.

---

### M-14: MutationBatch Has No Concurrency Documentation

| | |
|---|---|
| **File** | `libbitfs-go/tx/batch.go:68-75` |
| **Severity** | MEDIUM |
| **Category** | Missing documentation |

`AddNodeOp` and `AddFeeInput` append to unexported slices. `Build` reads all of them. No synchronization, no documentation of the single-goroutine builder contract.

**Suggested fix:** Add doc comment: "MutationBatch is not safe for concurrent use."

---

### M-15: DefaultDNSResolver/DefaultHTTPClient Are Mutable Global Variables

| | |
|---|---|
| **File** | `libbitfs-go/paymail/dns.go:33`, `resolve.go:34` |
| **Severity** | MEDIUM |
| **Category** | Data race on global state |

Both are exported mutable interface variables. A caller assigning `paymail.DefaultHTTPClient = myClient` while another goroutine calls `DiscoverCapabilities()` creates a data race on the interface-typed variable.

**Suggested fix:** Protect with `sync.RWMutex`, or make unexported with a thread-safe setter, or recommend callers always use `*WithClient` variants.

---

### M-16: MemHeaderStore.GetHeaderByHeight / GetTip Return Interior Pointers

| | |
|---|---|
| **File** | `libbitfs-go/spv/store.go:117-138` |
| **Severity** | MEDIUM |
| **Category** | Data race (pointer aliasing) |

`GetHeaderByHeight` and `GetTip` return raw map pointers without copying. Compare with `GetHeader` which correctly returns `&cpy`. Callers (e.g., `SyncHeaders` reading `prevHeader.Hash`) can observe mutations made by a concurrent writer that replaced the map entry.

**Suggested fix:** Apply the same copy-on-return pattern used in `GetHeader`:

```go
cpy := *h
return &cpy, nil
```

---

### M-17: MemTxStore.GetTx Returns Interior Pointer

| | |
|---|---|
| **File** | `libbitfs-go/spv/store.go:210-224` |
| **Severity** | MEDIUM |
| **Category** | Data race (pointer aliasing) |

Same pattern as M-16 — returns `tx` directly without copying. Callers mutating `tx.Proof` or `tx.RawTx` concurrently with store operations creates a race.

**Suggested fix:** Return shallow copy of the `StoredTx` value.

---

## LOW

### L-1: Daemon.running Should Be atomic.Bool

| | |
|---|---|
| **File** | `bitfs/internal/daemon/daemon.go:217` |
| **Severity** | LOW |

`d.running` is a plain `bool` protected by `d.mu`. Currently correct but fragile — any future code reading `d.running` without `d.mu` becomes a race. An `atomic.Bool` makes intent explicit.

---

### L-2: SetInvoiceDir Unsynchronized Write

| | |
|---|---|
| **File** | `bitfs/internal/daemon/daemon.go:442-444` |
| **Severity** | LOW |

Same pattern as H-7 — `d.invoiceDir = dir` with no lock. Read from `persistInvoice` in HTTP handler.

---

### L-3: SPVStore.Close() Without Draining In-Flight VerifyTx

| | |
|---|---|
| **File** | `bitfs/internal/engine/engine.go:108-113` |
| **Severity** | LOW |

`Engine.Close()` calls `e.SPVStore.Close()` (closes BoltDB) before draining concurrent `VerifyTx` calls. A goroutine still holding a reference to the closed store gets a use-after-close error.

---

### L-4: Signal Handler + defer eng.Close() Race

| | |
|---|---|
| **File** | `bitfs/cmd/bitfs/cmd_daemon.go:131-133` |
| **Severity** | LOW |

`eng.Close()` is registered as a `defer` before the daemon starts. After signal, `d.Stop()` drains handlers, but the defer might run while `d.Stop()` is still draining (if `d.Stop` returns before all goroutines finish using the engine).

---

### L-5: No File-Level Lock on nodes.json

| | |
|---|---|
| **File** | All CLI invocations of `bitfs` |
| **Severity** | LOW |

Two simultaneous `bitfs put` commands both load `nodes.json`, modify in-memory, and `Save()`. Last writer wins — first writer's changes silently lost.

**Suggested fix:** Acquire `flock` on `nodes.json.lock` at `engine.New()`, release at `engine.Close()`.

---

### L-6: SaveConfig No File-Level Lock

| | |
|---|---|
| **File** | `libbitfs-go/config/config.go:139-155` |
| **Severity** | LOW |

Two goroutines calling `SaveConfig` with the same path can produce interleaved output. Not an in-process data race but an OS-level file corruption risk.

---

### L-7: FileStore Global Mutex Serializes All Put Operations

| | |
|---|---|
| **File** | `libbitfs-go/storage/filestore.go:76` |
| **Severity** | LOW (performance, not correctness) |

The full-lock strategy serializes Puts to different shard directories unnecessarily.

**Suggested fix:** Per-shard mutexes (`[256]sync.RWMutex` keyed on `keyHash[0]`).

---

### L-8: ContentResolver Public Mutable Fields

| | |
|---|---|
| **File** | `libbitfs-go/storage/resolver.go:59-62` |
| **Severity** | LOW |

`ContentResolver.Client` and `Endpoints` are public fields. Concurrent mutation while `Fetch` reads them is a race.

---

### L-9: PutHeader Mutates Input Hash Before Acquiring Lock

| | |
|---|---|
| **File** | `libbitfs-go/spv/store.go:73-75` |
| **Severity** | LOW |

`PutHeader` computes `header.Hash = ComputeHeaderHash(header)` before `s.mu.Lock()`. If the same `*BlockHeader` is passed from two goroutines simultaneously, the hash assignment races.

---

### L-10: DeleteTx In-Place Slice Rewrite — Currently Safe but Fragile

| | |
|---|---|
| **File** | `libbitfs-go/spv/store.go:263-268` |
| **Severity** | LOW |

`append(txs[:i], txs[i+1:]...)` modifies the underlying array in place under the write lock. Currently safe because `GetTxsByPubKey` copies the slice, but fragile if copy semantics change.

---

### L-11: Invoice.Expiry Is a Public Mutable int64

| | |
|---|---|
| **File** | `libbitfs-go/x402/invoice.go:74-77` |
| **Severity** | LOW |

`IsExpired()` reads `inv.Expiry`. If a caller modifies `inv.Expiry` concurrently, Go's race detector would flag it. In practice, `Invoice` is typically immutable after creation.

---

### L-12: bmget Semaphore Not Released in Skip Path

| | |
|---|---|
| **File** | `bitfs/cmd/bmget/main.go:174-225` |
| **Severity** | LOW |

Semaphore slot acquired before `wg.Add` in the `failFast` skip path — slot never released for skipped files. Benign for short-lived CLI but would leak in a long-running context.

---

## Root Cause Analysis

### Pattern 1: Engine — No Concurrency Protection (15 findings)

**Findings:** H-1, H-2, H-3, H-4, M-6, M-7, M-8, M-9, M-10, L-3, L-4, L-5

The engine was correctly designed for single-writer CLI use. The daemon introduces concurrency without adding the required serialization layer. A single `sync.RWMutex` on Engine would resolve most of these findings.

**Fix priority:** HIGH — this is the single most impactful change.

### Pattern 2: Pointer Escapes Lock Scope (8 findings)

**Findings:** C-1, H-5, H-6, H-8, M-1, M-2, M-3, M-5

The daemon extracts pointers from locked maps, releases the lock, then reads/writes the pointed-to struct's fields. The map lock only protects the map structure — not the values inside it.

**Fix priority:** CRITICAL — confirmed data race, easy to fix with snapshot-by-value.

### Pattern 3: Library Types Lack Concurrency Contracts (10 findings)

**Findings:** H-9, M-12, M-13, M-14, M-15, M-16, M-17, L-8, L-9, L-10

Core types like `Node`, `WalletState`, `MetanetTx`, `MetaFlagBytes`, `MemHeaderStore` have no documented thread-safety guarantees. Callers cannot determine whether external locking is required.

**Fix priority:** MEDIUM — add documentation and defensive copies where needed.

---

## Recommended Fix Order

1. **Engine-level mutex** (H-1) — resolves H-2, H-3, H-4, M-6, M-7, M-9, M-10
2. **Daemon snapshot-by-value** (C-1) — resolves H-5, H-8, M-1, M-2, M-3
3. **SPV ErrDuplicateHeader tolerance** (H-10) — simple error handling change
4. **MemHeaderStore/MemTxStore copy-on-return** (M-16, M-17) — mechanical fix
5. **State.Save() atomic write** (M-8) — standard write-tmp-rename pattern
6. **Remaining daemon fixes** (H-6, H-7, M-4, M-5) — individual targeted fixes
7. **Library type documentation** (H-9, M-12-M-15) — doc + defensive changes
8. **LOW items** — address as part of normal development

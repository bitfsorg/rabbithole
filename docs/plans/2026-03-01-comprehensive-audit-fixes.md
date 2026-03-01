# Comprehensive Audit Fixes Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all remaining issues from the 2026-03-01 code review across libbitfs-go and bitfs repos.

**Architecture:** 10 tasks across 2 repos (libbitfs-go: 7 tasks, bitfs: 3 tasks). Each task groups related fixes within a single package or module. Tasks are independent — no ordering dependency between them, though libbitfs-go tasks should be done before bitfs tasks since bitfs depends on libbitfs-go.

**Tech Stack:** Go 1.25, `math/bits`, `github.com/bsv-blockchain/go-sdk`

**Scope:** 37 issues (3 HIGH, 34 MEDIUM) remaining after prior fix rounds eliminated all 4 CRITICAL and 21 HIGH issues.

---

## Already Fixed Issues (Do NOT Re-fix)

The following were confirmed fixed via code inspection. Skip any audit finding referencing these:

| ID | Issue | Status |
|----|-------|--------|
| R10-C1/C2/C3 | revshare overflow chain | FIXED — `mulDiv64()` with `bits.Mul64`, share sum validation, underflow check |
| R10-H1/H2 | revshare validation + truncation | FIXED — `safeSum()` with carry detection, uint32 bounds check |
| R13-C1 | InvoiceID transmission | FIXED — `BuyInfo.InvoiceID` field exists, passed to `HTLCFundingParams` |
| R01-H1 | AES-GCM AAD | FIXED — `keyHash` passed as AAD to `Seal`/`Open` |
| R01-H2 | Key material zeroing | FIXED — `defer SecureZero()` on all sensitive vars |
| R04-H1 | KeyPair `json:"-"` | FIXED — tag present on `PrivateKey` field |
| R05-H2 | BuildDataTransaction stub | FIXED — fully implemented |
| R07-H1 | Decompression size limit | FIXED — `MaxDecompressedSize = 256 MB` enforced |
| R07-H2 | SplitIntoChunks validation | FIXED — `chunkSize <= 0` returns error |
| R09-H2 | HTTPS validation bypass | FIXED — `parsed.Scheme != "https"` check |
| R11-H1 | mustDecompressPubKey panic | FIXED — replaced with `buildScriptForPubKey` |
| R11-H2 | Remove non-atomic | FIXED — single MutationBatch |
| R12-H1 | HTLC fail explicitly | FIXED — returns HTTP 500 on BuildHTLC failure |
| R12-H2 | Dashboard no auth | FIXED — `withAdminAuth()` middleware |
| R13-H1 | Capsule hash verification | FIXED — `ComputeCapsuleHash` comparison in buy.go |
| R13-H2 | HTLC fee estimation | FIXED — `EstimateHTLCFee` with 200-byte HTLC output |
| R15-H1 | bmget path traversal | FIXED — `..`, `/`, `\` rejection |

---

## Task 1: tx — Fee Input Dedup + Overflow Safety

**Files:**
- Modify: `libbitfs-go/tx/batch.go` (Build method, fee input section)
- Modify: `libbitfs-go/tx/opreturn.go` (EstimateFee, EstimateTxSize)
- Test: existing tests must still pass + add new test cases

**Issues:** R05-H1, R05-H3, R05-M3, R05-M4

### R05-H3 — Fee input overlap with node inputs (HIGH)

In `batch.go` Build(), fee inputs (L227-237) are added without checking the `addedInputs` map used for node input dedup. If a fee UTXO matches a node UTXO, it gets added twice → double-spend error.

**Fix:** Check fee inputs against `addedInputs` before adding:

```go
// In Build(), fee input loop (~L227):
for _, fi := range b.feeInputs {
    key := utxoKey{hex.EncodeToString(fi.TxID), fi.Vout}
    if addedInputs[key] {
        continue // Already added as a node input
    }
    addedInputs[key] = true
    // ... add input as before
}
```

Also update the `totalAvailable` calculation (~L174-188) to deduplicate fee inputs against node inputs using the same map.

### R05-H1 — EstimateFee integer overflow

In `opreturn.go` EstimateFee (~L202):
```go
fee := uint64(txSizeBytes) * feeRate
```

For very large `txSizeBytes`, multiplication can overflow uint64.

**Fix:** Add overflow check:
```go
func EstimateFee(txSizeBytes int, feeRate uint64) uint64 {
    if feeRate == 0 {
        feeRate = DefaultFeeRate
    }
    size := uint64(txSizeBytes)
    if size > math.MaxUint64/feeRate {
        return math.MaxUint64 // Saturate on overflow
    }
    fee := size * feeRate
    return (fee + 999) / 1000
}
```

### R05-M3 — EstimateTxSize wrong for multi-op batches

`EstimateTxSize` (~L213-228) only accounts for 1 OP_RETURN output. Multi-op batches have 1 OP_RETURN per operation. The current callers (Build()) pass `totalPayloadSize` which sums all payloads — this produces one large OP_RETURN estimate instead of N smaller ones. The difference is N-1 extra output overhead bytes (varint + script prefix per OP_RETURN).

**Fix:** Add `numOps` parameter or document that the estimate is conservative (always overestimates payload, which is acceptable for fee estimation). Add a comment explaining this is intentional.

### R05-M4 — Change output sent to wrong address

In `batch.go` Build(), if no explicit change address is set, change goes to the first operation's node key. This is correct for single-op transactions but wrong for multi-op batches where the change should go to a fee wallet address.

**Fix:** Return error if `b.changeAddr` is nil and change is needed:
```go
if changeAmount > 0 && b.changeAddr == nil {
    return nil, nil, fmt.Errorf("%w: change address required for multi-input batch", ErrInvalidParams)
}
```

**Test:** Add test for fee input dedup, overflow saturation, and missing change address.

**Run:** `cd libbitfs-go && go test ./tx/ -v -count=1 -race`

**Commit:** `fix(tx): deduplicate fee inputs against node inputs and add overflow safety [R05-H1/H3/M3/M4]`

---

## Task 2: method42 — Input Validation + Error Fixes

**Files:**
- Modify: `libbitfs-go/method42/ecdh.go` (xorBytes, ComputeCapsuleHash, ComputeCapsuleWithNonce)
- Modify: `libbitfs-go/method42/encrypt.go` (aesGCMEncrypt error type)
- Modify: `libbitfs-go/method42/rabin.go` (GenerateRabinKey, RabinSign)
- Test: existing tests must still pass

**Issues:** R01-M1, R01-M2, R01-M3, R01-M4, R01-M5, R01-M6

### R01-M1 — xorBytes silently truncates mismatched lengths

In `ecdh.go` xorBytes: if `len(a) != len(b)`, result is silently truncated to shorter. All current callers use equal-length inputs, but the function should validate.

**Fix:** Add length check at top:
```go
func xorBytes(a, b []byte) []byte {
    if len(a) != len(b) {
        panic("method42: xorBytes called with mismatched lengths")
    }
    // ... rest unchanged
}
```

### R01-M2/M3 — ComputeCapsuleHash/ComputeCapsuleWithNonce missing input validation

**Fix:** Add length checks at function entry:
```go
// In ComputeCapsuleHash:
if len(fileTxID) != 32 {
    return nil // Or return error if signature allows
}

// In ComputeCapsuleWithNonce:
if len(keyHash) != 32 {
    return nil, fmt.Errorf("method42: keyHash must be 32 bytes, got %d", len(keyHash))
}
```

### R01-M4 — RabinSign padding search unbounded

In `rabin.go` RabinSign, the padding counter loops until a quadratic residue is found. In theory this could loop for a long time (uint32 wraparound).

**Fix:** Add max iteration limit:
```go
const maxRabinPaddingIterations = 1_000_000

for padding := uint32(0); padding < maxRabinPaddingIterations; padding++ {
    // ... existing logic
}
return nil, 0, fmt.Errorf("method42: rabin sign failed after %d padding iterations", maxRabinPaddingIterations)
```

### R01-M5 — GenerateRabinKey doesn't check p ≠ q

If `crypto/rand` generates the same prime twice (astronomically unlikely but technically possible), the Rabin scheme breaks.

**Fix:** Add equality check after generating both primes:
```go
if p.Cmp(q) == 0 {
    return nil, fmt.Errorf("method42: generated identical primes (retry)")
}
```

### R01-M6 — aesGCMEncrypt wraps error as ErrDecryptionFailed

In `encrypt.go` aesGCMEncrypt (~L370-375), AES cipher creation failure is wrapped with `ErrDecryptionFailed` — wrong semantic for an encryption function.

**Fix:** Use a generic error or create `ErrEncryptionFailed`:
```go
return nil, fmt.Errorf("method42: AES cipher creation failed: %w", err)
```

**Run:** `cd libbitfs-go && go test ./method42/ -v -count=1 -race`

**Commit:** `fix(method42): add input validation and fix error types [R01-M1/M2/M3/M4/M5/M6]`

---

## Task 3: x402 — CSPRNG Safety + Nil Guards

**Files:**
- Modify: `libbitfs-go/x402/invoice.go` (generateInvoiceID)
- Modify: `libbitfs-go/x402/headers.go` (SetPaymentHeaders)
- Test: existing tests must still pass

**Issues:** R02-M1, R02-M2

### R02-M1 — generateInvoiceID fallback to time.Now()

In `invoice.go` generateInvoiceID (~L79-87): if `rand.Read` fails, falls back to `time.Now().UnixNano()` — predictable, replayable invoice ID in a payment system.

**Fix:** If CSPRNG fails, return error (callers should handle):
```go
func generateInvoiceID() (string, error) {
    b := make([]byte, 16)
    if _, err := rand.Read(b); err != nil {
        return "", fmt.Errorf("x402: CSPRNG failure: %w", err)
    }
    return hex.EncodeToString(b), nil
}
```

Update `NewInvoice` to propagate the error:
```go
func NewInvoice(...) (*Invoice, error) {
    invoiceID, err := generateInvoiceID()
    if err != nil {
        return nil, err
    }
    // ... rest unchanged
    return &Invoice{ID: invoiceID, ...}, nil
}
```

Update all callers of `NewInvoice` to handle the error (check daemon/payment.go).

### R02-M2 — SetPaymentHeaders nil panic

In `headers.go` SetPaymentHeaders: if `headers` is nil, panics on dereference.

**Fix:** Add nil check:
```go
func SetPaymentHeaders(w http.ResponseWriter, headers *PaymentHeaders) {
    if headers == nil {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    // ... rest unchanged
}
```

**Run:** `cd libbitfs-go && go test ./x402/ -v -count=1 -race`

**Note:** After changing `NewInvoice` signature, also update callers in bitfs daemon (Task 8).

**Commit:** `fix(x402): return error on CSPRNG failure and add nil guards [R02-M1/M2]`

---

## Task 4: vault — Platform Split (flock)

**Files:**
- Rename: `libbitfs-go/vault/flock.go` → `libbitfs-go/vault/flock_unix.go`
- Create: `libbitfs-go/vault/flock_windows.go`
- Test: existing tests must still pass on macOS/Linux

**Issues:** R11-H3

### R11-H3 — flock uses POSIX-only syscall.Flock

`flock.go` uses `syscall.Flock` (LOCK_EX, LOCK_NB, LOCK_UN) which doesn't exist on Windows.

**Step 1:** Rename `flock.go` → `flock_unix.go` and add build tag:
```go
//go:build unix

package vault
// ... existing code unchanged
```

**Step 2:** Create `flock_windows.go` with Windows LockFileEx API:
```go
//go:build windows

package vault

import (
    "os"
    "golang.org/x/sys/windows"
)

func acquireLock(f *os.File) error {
    ol := new(windows.Overlapped)
    return windows.LockFileEx(
        windows.Handle(f.Fd()),
        windows.LOCKFILE_EXCLUSIVE_LOCK,
        0, 1, 0, ol,
    )
}

func tryLock(f *os.File) error {
    ol := new(windows.Overlapped)
    return windows.LockFileEx(
        windows.Handle(f.Fd()),
        windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
        0, 1, 0, ol,
    )
}

func releaseLock(f *os.File) {
    ol := new(windows.Overlapped)
    _ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
    _ = f.Close()
}
```

**Note:** This requires adding `golang.org/x/sys` dependency if not already present. Check go.mod first. If adding a new dependency is undesirable, use a no-op stub for Windows instead:

```go
//go:build windows

package vault

import "os"

// Windows stub: file locking not supported.
// Vault operations are process-safe via Go mutexes but not cross-process safe on Windows.
func acquireLock(f *os.File) error { return nil }
func tryLock(f *os.File) error     { return nil }
func releaseLock(f *os.File)       { _ = f.Close() }
```

**Run:** `cd libbitfs-go && go test ./vault/ -v -count=1 -race`

**Commit:** `fix(vault): split flock into platform-specific files for Windows compatibility [R11-H3]`

---

## Task 5: vault — State Management Fixes

**Files:**
- Modify: `libbitfs-go/vault/vault.go` (DeriveChangeAddr, lookupPrivKey, GetNode, FindNodeByPath, RefreshFeeUTXOs, Close)
- Modify: `libbitfs-go/wallet/hd.go` (DeriveFeeKey)
- Test: existing tests must still pass

**Issues:** R11-M1, R11-M3, R11-M4, R11-M5, R11-M6, R04-M5

### R11-M4 — DeriveChangeAddr pre-increment

`DeriveChangeAddr` increments `NextChangeIndex` before the caller uses the derived key. If the operation fails, the index is wasted.

**Fix:** Move increment after successful use — but this requires the caller to notify. Simpler fix: document that indices may have gaps (this is normal for HD wallets and doesn't cause bugs). Add a comment:
```go
// Note: index is incremented eagerly. If the caller's operation fails,
// the index gap is harmless — HD wallets tolerate gaps in derivation.
v.WState.NextChangeIndex++
```

### R11-M5 — WalletState not persisted on Close

If `Close()` doesn't persist `WalletState`, `NextChangeIndex` is lost between sessions.

**Fix:** Add state persistence in `Close()`:
```go
func (v *Vault) Close() error {
    v.mu.Lock()
    defer v.mu.Unlock()
    if err := v.saveState(); err != nil {
        return fmt.Errorf("vault: save state on close: %w", err)
    }
    // ... existing close logic
    return nil
}
```

Verify that `saveState()` (or equivalent method) exists. If not, implement it to write `WalletState` JSON to `~/.bitfs/state.json`.

### R11-M1 — UTXO slice unbounded growth

Spent UTXOs are never removed from the in-memory UTXO set. Over time, memory grows.

**Fix:** In UTXO tracking methods, when a node input is consumed, mark it as spent or remove it. The simplest approach: add a `removeUTXO(txid, vout)` helper called when inputs are consumed in `TrackBatchUTXOs`:
```go
func (v *Vault) removeUTXO(txid []byte, vout uint32) {
    key := hex.EncodeToString(txid) + ":" + strconv.FormatUint(uint64(vout), 10)
    delete(v.utxos, key)
}
```

### R11-M3 — Mutable pointer escape from mutex

`GetNode` and `FindNodeByPath` return `*metanet.Node` pointers that escape the mutex protection. Callers can mutate the node without holding the lock.

**Fix:** Return copies instead of pointers:
```go
func (v *Vault) GetNode(pnode string) (metanet.Node, bool) {
    v.mu.RLock()
    defer v.mu.RUnlock()
    n, ok := v.State.Nodes[pnode]
    if !ok {
        return metanet.Node{}, false
    }
    return *n, true // Return copy
}
```

**Note:** This changes the API signature. Check all callers and update them.

### R11-M6 — RefreshFeeUTXOs doesn't set derivation indices

Fee UTXOs created by `RefreshFeeUTXOs` lack `FeeChain`/`FeeDerivIdx` fields, forcing the slow scan path in `lookupPrivKey`.

**Fix:** Set the derivation info when creating fee UTXOs:
```go
utxo.FeeChain = chain
utxo.FeeDerivIdx = idx
```

### R04-M5 — DeriveFeeKey no chain validation

In `wallet/hd.go` DeriveFeeKey: `chain` parameter should be validated as 0 (external) or 1 (internal) per BIP44.

**Fix:**
```go
func (w *Wallet) DeriveFeeKey(chain, index uint32) (*KeyPair, error) {
    if chain > 1 {
        return nil, fmt.Errorf("wallet: invalid chain %d (must be 0 or 1)", chain)
    }
    // ... rest unchanged
}
```

**Run:** `cd libbitfs-go && go test ./vault/ ./wallet/ -v -count=1 -race`

**Commit:** `fix(vault): improve state persistence, UTXO lifecycle, and key derivation safety [R11-M1/M3/M4/M5/M6, R04-M5]`

---

## Task 6: paymail — SSRF Mitigation

**Files:**
- Modify: `libbitfs-go/paymail/resolve.go` (DiscoverCapabilities, capability matching)
- Test: existing tests must still pass

**Issues:** R09-H1, R09-M2

### R09-H1 — SSRF via server-controlled URL template

In capability discovery, the server returns URL templates that are used to construct follow-up requests. The URL host could point to internal services (SSRF).

**Fix:** After resolving capability URL, validate that the host matches the original domain:
```go
func validateCapabilityURL(capURL, originalDomain string) error {
    parsed, err := url.Parse(capURL)
    if err != nil {
        return fmt.Errorf("invalid capability URL: %w", err)
    }
    // Allow only the original domain (or subdomains)
    if !strings.HasSuffix(parsed.Hostname(), originalDomain) {
        return fmt.Errorf("capability URL host %q does not match domain %q", parsed.Hostname(), originalDomain)
    }
    return nil
}
```

Apply this check in `DiscoverCapabilitiesWithClient` after parsing capabilities, before storing URLs.

### R09-M2 — Capability key matching too broad

`strings.Contains(key, "pki")` matches keys like `"custom-spki"`. Use exact key matching or prefix matching.

**Fix:** Use exact capability key constants:
```go
const (
    CapPKI                = "pki"
    CapPaymentDestination = "5f1323cddf31"  // BRC-??? payment destination
)

// In capability parsing:
if key == CapPKI { ... }
```

**Run:** `cd libbitfs-go && go test ./paymail/ -v -count=1 -race`

**Commit:** `fix(paymail): validate capability URL domain and use exact key matching [R09-H1/M2]`

---

## Task 7: network + client — Response Size Limits

**Files:**
- Modify: `libbitfs-go/network/rpc.go` (Call method)
- Modify: `bitfs/internal/client/client.go` (all HTTP response reads)
- Test: existing tests must still pass

**Issues:** R08-M1, R08-M2, R14-M1, R14-M2, R14-M3

### R08-M1 — RPC response body no size limit

In `rpc.go` Call(), the JSON-RPC response is decoded directly from `resp.Body` without size limit.

**Fix:** Wrap in `io.LimitReader`:
```go
const maxRPCResponseSize = 10 << 20 // 10 MB

limitedBody := io.LimitReader(resp.Body, maxRPCResponseSize)
var rpcResp rpcResponse
if err := json.NewDecoder(limitedBody).Decode(&rpcResp); err != nil {
    return fmt.Errorf("%w: decode response: %w", ErrInvalidResponse, err)
}
```

### R08-M2 — HTTP 401 mapped to ErrConnectionFailed

In `rpc.go` Call(), HTTP 401 returns `ErrConnectionFailed`. Should return a more specific error.

**Fix:** Add status-specific error mapping:
```go
if resp.StatusCode == http.StatusUnauthorized {
    return fmt.Errorf("%w: HTTP 401 unauthorized", ErrAuthFailed)
}
if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    // ... existing error handling
}
```

Add `ErrAuthFailed` sentinel error to `errors.go` if not present.

### R14-M1 — Client txid not validated

In `client.go`, `GetBuyInfo`, `SubmitHTLC`, and `VerifySPV` use txid in URL paths without validation. Malicious txid could inject path components.

**Fix:** Add hex validation helper and call it before URL construction:
```go
func validateTxID(txid string) error {
    if len(txid) != 64 {
        return fmt.Errorf("invalid txid length: %d", len(txid))
    }
    if _, err := hex.DecodeString(txid); err != nil {
        return fmt.Errorf("invalid txid hex: %w", err)
    }
    return nil
}
```

### R14-M2 — JSON response no size limit

Client methods read full JSON responses without size limits. Apply `io.LimitReader` to all `json.NewDecoder(resp.Body)` calls.

**Fix:** Create a helper:
```go
const maxResponseSize = 10 << 20 // 10 MB

func decodeJSON(body io.ReadCloser, v interface{}) error {
    defer body.Close()
    return json.NewDecoder(io.LimitReader(body, maxResponseSize)).Decode(v)
}
```

Replace all `json.NewDecoder(resp.Body).Decode(&result)` calls with `decodeJSON(resp.Body, &result)`.

### R14-M3 — Paymail resolution uses default client

If client's paymail resolution path uses `http.DefaultClient` (no timeout), wrap it.

**Fix:** Verify if `client.go` has a paymail path. If so, ensure it uses a client with timeout. The paymail package already has `DefaultPostClient` with 30s timeout, so this may already be handled. Verify and skip if so.

**Run:**
```bash
cd libbitfs-go && go test ./network/ -v -count=1 -race
cd bitfs && go test ./internal/client/ -v -count=1 -race
```

**Commit (libbitfs-go):** `fix(network): add response size limit and auth error mapping [R08-M1/M2]`
**Commit (bitfs):** `fix(client): validate txid input and limit response sizes [R14-M1/M2/M3]`

---

## Task 8: daemon — Error Handling + NewInvoice API

**Files:**
- Modify: `bitfs/internal/daemon/payment.go` (NewInvoice caller, hex error, context rollback)
- Modify: `bitfs/internal/daemon/daemon.go` (Start error propagation)
- Test: existing tests must still pass

**Issues:** R12-M3, R12-M4, R12-M5, plus adapting to Task 3's `NewInvoice` signature change

### Adapt to NewInvoice error return (from Task 3)

After Task 3 changes `NewInvoice` to return `(*Invoice, error)`, update the daemon caller:
```go
// Before:
invoice := x402.NewInvoice(pricePerKB, fileSize, paymentAddr, capsuleHash, ttlSeconds)

// After:
invoice, err := x402.NewInvoice(pricePerKB, fileSize, paymentAddr, capsuleHash, ttlSeconds)
if err != nil {
    writeJSONError(w, http.StatusInternalServerError, "INVOICE_CREATE_FAILED", err.Error())
    return
}
```

### R12-M3 — Start() binding error lost

In `daemon.go` Start(), `ListenAndServe` runs in a goroutine. If binding fails (port in use), the error is logged but not returned to the caller.

**Fix:** Use an error channel:
```go
errCh := make(chan error, 1)
go func() {
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        errCh <- err
    }
}()

// Give server time to bind
select {
case err := <-errCh:
    return fmt.Errorf("daemon: start failed: %w", err)
case <-time.After(100 * time.Millisecond):
    // Server started successfully
}
```

### R12-M4 — hex.DecodeString error discarded

In `payment.go`, check all `hex.DecodeString` calls and ensure errors are handled (not assigned to `_`).

### R12-M5 — Context cancellation after broadcast

If context is cancelled after the HTLC transaction is broadcast but before the capsule is returned, `invoice.Paid` is rolled back but the tx is already on-chain.

**Fix:** Add comment documenting this is acceptable behavior (buyer can resubmit the same tx to get the capsule). Or: don't rollback `Paid` status after broadcast — the payment happened regardless of HTTP delivery.

```go
// After broadcast succeeds, payment is final regardless of HTTP response delivery.
// Do NOT rollback invoice.Paid after this point.
```

**Run:** `cd bitfs && go test ./internal/daemon/ -v -count=1 -race`

**Commit:** `fix(daemon): propagate start errors and handle NewInvoice failures [R12-M3/M4/M5]`

---

## Task 9: cmd — Credential Protection

**Files:**
- Modify: `bitfs/cmd/bcat/main.go`, `bitfs/cmd/bget/main.go`, `bitfs/cmd/bmget/main.go` (wallet-key flag)
- Modify: `bitfs/cmd/bitfs/password.go` (zeroString)
- Test: existing tests must still pass

**Issues:** R15-H2, R15-H3

### R15-H2 — Private key via CLI flag (HIGH)

`--wallet-key` flag exposes the private key in `ps aux` output and shell history.

**Fix:** Replace with environment variable and/or file-based input:
```go
// Before:
walletKey := fs.String("wallet-key", "", "hex-encoded buyer private key (32 bytes)")

// After:
walletKey := fs.String("wallet-key", "", "hex-encoded buyer private key (or set BITFS_WALLET_KEY env var, or use @filepath)")
```

In the key resolution logic:
```go
func resolveWalletKey(flagValue string) (string, error) {
    if flagValue != "" {
        if strings.HasPrefix(flagValue, "@") {
            // Read from file
            data, err := os.ReadFile(strings.TrimPrefix(flagValue, "@"))
            if err != nil {
                return "", fmt.Errorf("read wallet key file: %w", err)
            }
            return strings.TrimSpace(string(data)), nil
        }
        return flagValue, nil
    }
    // Fallback to env var
    if key := os.Getenv("BITFS_WALLET_KEY"); key != "" {
        return key, nil
    }
    return "", nil
}
```

Update all 3 tools (bcat, bget, bmget) to use this helper.

### R15-H3 — Password string zeroing incomplete

Go strings are immutable; `[]byte(*s)` creates a copy. The original string backing array is not zeroed.

**Fix:** This is a Go language limitation. The current implementation provides best-effort defense-in-depth. Add a comment documenting the limitation:
```go
// zeroString attempts to zero the string's byte content. Due to Go's
// immutable string semantics, []byte(*s) creates a copy — the original
// backing array cannot be zeroed. This provides defense-in-depth but
// not a guarantee. For true secret protection, use []byte throughout
// and avoid string conversions.
func zeroString(s *string) {
    // ... existing implementation
}
```

For a stronger fix, refactor password handling to use `[]byte` instead of `string` throughout the password flow. This is a larger change — scope it separately if desired.

**Run:** `cd bitfs && go test ./cmd/bcat/ ./cmd/bget/ ./cmd/bmget/ ./cmd/bitfs/ -v -count=1 -race`

**Commit:** `fix(cmd): support env var and file input for wallet key, document password zeroing limitation [R15-H2/H3]`

---

## Task 10: Protocol Robustness — storage, spv, metanet

**Files:**
- Modify: `libbitfs-go/storage/store.go` (FileStore.Put fsync, List concurrency)
- Modify: `libbitfs-go/storage/resolver.go` (hash verification)
- Modify: `libbitfs-go/spv/verify.go` (nil checks, TLV validation)
- Modify: `libbitfs-go/metanet/serialize.go` (payload size limits, byte slice safety)
- Test: existing tests must still pass

**Issues:** R07-M2, R07-M3, R07-M4, R03-M1, R03-M2, R06-M1, R06-M4, R06-M5

### R07-M3 — FileStore.Put no fsync

After writing content to disk, `Put` doesn't call `fsync`. Data can be lost on crash.

**Fix:** Add `f.Sync()` before `f.Close()`:
```go
if err := f.Sync(); err != nil {
    return fmt.Errorf("storage: fsync: %w", err)
}
```

### R07-M2 — FileStore.List concurrency

`List()` acquires RLock but directory contents can change between ReadDir and returning results. This is inherent to filesystem operations.

**Fix:** Document the limitation. The current RLock prevents concurrent writes via the same FileStore instance, which is sufficient:
```go
// List returns content hashes in storage. Results represent a snapshot;
// concurrent external modifications may not be reflected.
```

### R07-M4 — ContentResolver no hash verification

After fetching remote content, the resolver caches it without verifying the content hash matches the requested key.

**Fix:** Add hash verification before caching:
```go
actualHash := sha256.Sum256(data)
if !bytes.Equal(actualHash[:], keyHash) {
    return nil, fmt.Errorf("storage: content hash mismatch for remote data")
}
```

### R03-M1 — SPV TLV deserialization accepts wrong field lengths

TLV fields with known sizes (e.g., Type: 1 byte, FileSize: 8 bytes) should reject wrong lengths.

**Fix:** Add strict length validation for known TLV tags:
```go
case TagType:
    if len(value) != 1 {
        return fmt.Errorf("spv: invalid Type field length: %d", len(value))
    }
```

### R03-M2 — CheckCLTVAccess nil panic

`CheckCLTVAccess(nil)` panics. Add nil guard:
```go
func CheckCLTVAccess(node *metanet.Node, blockHeight uint32) error {
    if node == nil {
        return fmt.Errorf("spv: nil node")
    }
    // ... rest unchanged
}
```

### R06-M1 — metanet TLV error lengths accepted silently

Similar to R03-M1. Add length validation for metanet TLV fields.

### R06-M4 — serializeChildEntry byte slice reuse

If the function reuses a shared buffer, concurrent serialization could corrupt data. Verify and fix by allocating fresh slices.

### R06-M5 — No max payload size check

LEB128-encoded lengths can represent huge values. Add max payload constant:
```go
const MaxPayloadSize = 64 << 20 // 64 MB

if payloadLen > MaxPayloadSize {
    return nil, fmt.Errorf("metanet: payload too large: %d bytes", payloadLen)
}
```

**Run:**
```bash
cd libbitfs-go && go test ./storage/ ./spv/ ./metanet/ -v -count=1 -race
```

**Commit:** `fix(storage,spv,metanet): add fsync, hash verification, payload limits, and nil guards [R07-M2/M3/M4, R03-M1/M2, R06-M1/M4/M5]`

---

## Verification

After all tasks, run full test suites:

```bash
# libbitfs-go (all packages)
cd libbitfs-go && go test ./... -count=1 -race

# bitfs unit tests
cd bitfs && go test ./... -count=1 -race
```

All tests must pass with no race conditions.

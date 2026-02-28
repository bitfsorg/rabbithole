# Design: Fix CRITICAL + HIGH Audit Findings

**Date**: 2026-02-26
**Scope**: 9 findings (1 CRITICAL + 8 HIGH) from code audit report
**Source**: `docs/audits/2026-02-26-code-audit.md`

---

## Findings

| # | ID | Severity | File | Issue |
|---|-----|----------|------|-------|
| 1 | C-1 | CRITICAL | `libbitfs-go/spv/merkle.go` | VerifyMerkleProof rejects valid single-tx blocks |
| 2 | H-1 | HIGH | `libbitfs-go/spv/verify.go` | VerifyTransaction doesn't call VerifyPoW |
| 3 | H-3 | HIGH | `bitfs/internal/daemon/daemon.go` | HTTP server has no timeouts |
| 4 | H-4 | HIGH | `libbitfs-go/paymail/resolve.go` | `io.ReadAll` unbounded (2 locations) |
| 5 | H-NEW-1 | HIGH | `bitfs/internal/daemon/payment.go` | TOCTOU race in invoice payment flow |
| 6 | H-NEW-2 | HIGH | `bitfs/internal/engine/mget.go` | Path traversal via malicious child.Name |
| 7 | H-NEW-3 | HIGH | `libbitfs-go/storage/resolver.go` | `io.ReadAll` unbounded in content resolver |
| 8 | H-NEW-4 | HIGH | `libbitfs-go/x402/htlc_tx.go` | BuildBuyerRefundTx doesn't verify UTXO reference |
| 9 | H-NEW-5 | HIGH | `libbitfs-go/network/rpc_blockchain.go` | Merkle traversal OOM from malicious totalTxs |

---

## Approach: 4 Themed Batches

### Batch 1 — SPV Security (C-1 + H-1)

Both in `libbitfs-go/spv/`.

**C-1**: `VerifyMerkleProof` rejects single-tx blocks because `ComputeMerkleRoot` returns nil when proof nodes are empty. In a single-tx block, the merkle root equals the txid — empty proof nodes is valid. Fix: handle the empty-nodes case in `ComputeMerkleRoot` or `VerifyMerkleProof` to return txid directly.

**H-1**: `VerifyTransaction` retrieves the block header and checks the merkle proof but never calls `VerifyPoW`. The function already exists in `header.go`. Fix: add `VerifyPoW(header)` call after header retrieval.

### Batch 2 — Unbounded Reads (H-4 + H-NEW-3)

Three `io.ReadAll` calls without `io.LimitReader`:

- `libbitfs-go/paymail/resolve.go:75` — DiscoverCapabilitiesWithClient
- `libbitfs-go/paymail/resolve.go:142` — ResolvePKIWithClient
- `libbitfs-go/storage/resolver.go:90` — fetchFromEndpoint

Fix: wrap each with `io.LimitReader`. Paymail responses capped at 1MB (JSON metadata). Content resolver capped at 1GB (ciphertext).

### Batch 3 — Daemon Hardening (H-3 + H-NEW-1)

Both in `bitfs/internal/daemon/`.

**H-3**: `http.Server` created with zero timeouts. Fix: set ReadTimeout, WriteTimeout, IdleTimeout, ReadHeaderTimeout, MaxHeaderBytes.

**H-NEW-1**: Payment TOCTOU race — `handleGetBuyInfo` and `handleSubmitHTLC` use separate lock acquisitions. Concurrent capsule computation can overwrite. Concurrent HTLC submissions can double-deliver. Fix: hold write lock for entire check-then-set sequences in both handlers.

### Batch 4 — Trust Boundary Validation (H-NEW-2 + H-NEW-4 + H-NEW-5)

Input from untrusted sources needs validation.

**H-NEW-2**: `mgetRecurse` uses `child.Name` from Metanet DAG in `filepath.Join` without sanitization. Fix: reject names containing `..`, `/`, or `\`.

**H-NEW-4**: `BuildBuyerRefundTx` signs seller-provided pre-signed tx without verifying it references the expected HTLC UTXO. Fix: add `FundingTxID`/`FundingVout` to params and verify against `tx.Inputs[0]`.

**H-NEW-5**: `traversePartialMerkleTree` accepts arbitrary `totalTxs` from RPC. Fix: cap `totalTxs` at reasonable maximum, add recursion depth guard.

---

## Testing Strategy

Each fix includes a regression test proving the vulnerability is closed:
- C-1: Test single-tx block merkle proof verification succeeds
- H-1: Test that VerifyTransaction rejects header with invalid PoW
- H-3: Assert server timeouts are non-zero
- H-4/H-NEW-3: Test with oversized response bodies
- H-NEW-1: Concurrent goroutine test for capsule/payment races
- H-NEW-2: Test path traversal names are rejected
- H-NEW-4: Test mismatched UTXO reference is rejected
- H-NEW-5: Test large totalTxs is rejected

---

## Commit Strategy

One commit per batch, on a `fix/audit-critical-high` branch:
1. `fix(spv): handle single-tx blocks + add PoW validation to VerifyTransaction`
2. `fix(paymail,storage): add io.LimitReader to all unbounded reads`
3. `fix(daemon): add HTTP timeouts + fix payment TOCTOU race`
4. `fix(security): path traversal guard, UTXO verification, merkle depth limit`

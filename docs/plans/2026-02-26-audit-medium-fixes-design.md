# Design: Fix MEDIUM Audit Findings (3 Batches)

**Date**: 2026-02-26
**Scope**: 41 MEDIUM findings from code audit report
**Source**: `docs/audits/2026-02-26-code-audit.md`
**Approach**: 3 batches by security impact — high → medium → low

---

## Batch 1 — High Impact Security (13 items)

Injection, access control, race conditions, data integrity — exploitable vulnerabilities.

| # | ID | Repo | File | Issue | Fix |
|---|-----|------|------|-------|-----|
| 1 | M-NEW-7 | bitfs | `internal/client/client.go` | `GetBuyInfo` query param injection | Use `url.Values` |
| 2 | M-8 | libbitfs | `paymail/resolve.go` | PKI URL template injection | `url.PathEscape()` on alias/domain |
| 3 | M-7 | libbitfs | `paymail/resolve.go` | PKI template URL not validated HTTPS | Validate URL scheme |
| 4 | M-NEW-5 | bitfs | `internal/daemon/content.go` | `/_bitfs/data/{hash}` no access control | Cross-ref node access level |
| 5 | M-NEW-24 | bitfs | `internal/daemon/routes.go` | Private node metadata exposed in JSON | Filter sensitive fields |
| 6 | M-NEW-21 | bitfs | `internal/daemon/routes.go` | Markdown path injection | Escape special chars |
| 7 | M-NEW-1 | bitfs | `internal/daemon/payment.go` | Capsule overwrite race | Move checks inside write lock |
| 8 | M-NEW-8 | bitfs | `internal/engine/mput.go` | `Mput` follows symlinks | Skip `os.ModeSymlink` |
| 9 | M-NEW-6 | bitfs | `cmd/bcat/main.go`, `cmd/bget/main.go` | Unbounded `io.ReadAll` | Use `io.LimitReader` |
| 10 | M-NEW-11 | libbitfs | `x402/htlc_tx.go` | `sig.Serialize()` append may mutate | Use `make` + `copy` |
| 11 | M-NEW-13 | libbitfs | `method42/encrypt.go` | `EncryptResult.AESKey` exposes raw key | Remove field |
| 12 | M-NEW-14 | libbitfs | `network/rpc_blockchain.go` | Nil hash in merkle traversal → silent wrong result | Add nil checks, return error |
| 13 | M-NEW-19 | libbitfs | `spv/boltstore.go` | `PutTxWithPubKey` silently overwrites | Add duplicate check |

---

## Batch 2 — Overflow/Bounds/Atomicity (15 items)

| # | ID | Repo | File | Issue | Fix |
|---|-----|------|------|-------|-----|
| 1 | M-1 | libbitfs | `config/tlv.go` (or metanet TLV) | TLV uvarint overflow `int(length)` | Bounds check before cast |
| 2 | M-2 | libbitfs | `metanet/directory.go` | No max child name length | Add 255-byte limit |
| 3 | M-3 | bitfs | `internal/engine/sell.go` | `CalculatePrice` integer overflow | Checked multiplication |
| 4 | M-NEW-10 | libbitfs | `wallet/hd.go`, `wallet/vault.go` | BIP32 account index overflow | Bounds check < 0x80000000 |
| 5 | M-NEW-15 | libbitfs | `storage/filestore.go` | `KeyHashToPath` panics on empty | Length validation |
| 6 | M-NEW-17 | libbitfs | `x402/htlc_tx.go` | `BuildHTLCFundingTx` accepts Amount=0 | Explicit > 0 check |
| 7 | M-NEW-18 | libbitfs | `network/rpc_blockchain.go` | `btcToSat` negative → MaxUint64 | Guard `< 0` |
| 8 | M-NEW-20 | libbitfs | `metanet/resolve.go` | `ResolvePath` unbounded depth | Add MaxPathDepth check |
| 9 | M-15 | libbitfs | `storage/filestore.go` | Non-atomic writes | Write-to-temp + rename |
| 10 | M-6 | libbitfs | `x402/htlc_tx.go` | HTLC fee estimation too low | Use actual script size |
| 11 | M-NEW-16 | libbitfs | `x402/htlc_tx.go` | `VerifyHTLCFunding` first match only | Require explicit vout |
| 12 | M-NEW-2 | bitfs | `internal/engine/mkdir.go` | `GetNodeUTXO` TOCTOU double-spend | Atomic allocate-and-mark |
| 13 | M-NEW-4 | bitfs | `internal/engine/move.go` | Store before TX complete | Defer storage until success |
| 14 | M-NEW-9 | bitfs | `internal/engine/move.go` | Directory move fails silently | Guard non-file nodes |
| 15 | M-NEW-12 | libbitfs | `wallet/vault.go` | `WalletState` not validated on load | Add `Validate()` method |

---

## Batch 3 — Operational/Code Quality (13 items)

| # | ID | Repo | File | Issue | Fix |
|---|-----|------|------|-------|-----|
| 1 | M-12 | bitfs | `internal/daemon/daemon.go` | `cleanupExpiredSessions` never called | Start cleanup goroutine in `Start()` |
| 2 | M-13 | bitfs | `internal/daemon/daemon.go` | Unbounded maps (sessions/invoices/usedTxIDs) | Periodic cleanup + max size |
| 3 | M-14 | bitfs | `internal/daemon/handshake.go` | Handshake timestamp not validated | Check ±5 minute window |
| 4 | M-16 | libbitfs | `config/config.go` | `SaveConfig` file mode 0666 | Use `os.OpenFile` with 0600 |
| 5 | M-4 | libbitfs | `x402/htlc_tx.go` | `ParseHTLCPreimage` no hash verification | Verify SHA256(preimage) |
| 6 | M-5 | libbitfs | `x402/htlc_tx.go` | `BuildSellerClaimTx` no capsule hash check | Verify against HTLC script |
| 7 | M-9 | libbitfs | `network/rpc_blockchain.go` | RPC doesn't check HTTP status | Add status code check |
| 8 | M-10 | libbitfs | `network/rpc_blockchain.go` | RPC response ID not validated | Match request/response IDs |
| 9 | M-11 | libbitfs | `network/spvclient.go` | Header sync no chain continuity check | Verify PrevBlockHash linkage |
| 10 | M-NEW-3 | bitfs | `cmd/bitfs/cmd_shell.go` | Shell access mode unvalidated | Validate free/private/paid |
| 11 | M-NEW-23 | bitfs | `cmd/bitfs/cmd_shell.go` | History file world-readable | Set 0600 permissions |
| 12 | M-NEW-25 | bitfs | `internal/daemon/payment.go` | Capsule not persisted (crash loses it) | Persist invoice state to disk |
| 13 | M-NEW-22 | bitfs | `internal/engine/engine.go` | `lookupPrivKey` O(n) with hardcoded lookahead | Store derivation index with UTXOState |

---

## Testing Strategy

Each fix includes a regression test proving the vulnerability is closed. TDD approach: write failing test → implement fix → verify all existing tests pass.

## Commit Strategy

One commit per batch, separate repos:
- Batch 1: `fix(security): high-impact MEDIUM audit fixes (M-NEW-{1,5,6,7,8,11,13,14,19,21,24}, M-7, M-8)`
- Batch 2: `fix(bounds): overflow and atomicity MEDIUM audit fixes`
- Batch 3: `fix(ops): operational and code quality MEDIUM audit fixes`

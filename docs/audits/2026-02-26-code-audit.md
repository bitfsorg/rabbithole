# BitFS Protocol Correctness Audit Report (v2)

**Date**: 2026-02-26 (Re-Audit)
**Scope**: Full protocol correctness re-audit — 10 libbitfs-go packages + bitfs application layer + new code since last audit
**Standard**: Internal specs (bitfs/docs/spec/01-11) + external standards (NIST, RFC, BIP)
**Auditor**: Claude Opus 4.6 (automated protocol review, 5 parallel audit agents)
**Test Status**: 1647/1647 tests passing (all previously failing tests fixed)

---

## Executive Summary

This is a **re-audit** of the BitFS codebase. It confirms that the core cryptographic engine remains sound, while identifying significant new issues missed by the first audit. Of the 12 key findings from the first audit, **only 1 has been fixed** (H-2: payment replay prevention), 2 are partially addressed, and 9 remain unfixed.

The re-audit discovered **5 new HIGH**, **25 new MEDIUM**, and **23 new LOW** severity findings. The most impactful new findings are:

- **Payment race condition** (TOCTOU) allows concurrent HTLC submissions to the same invoice — both receive the capsule (double-delivery)
- **Path traversal in `mget`** via malicious child names from untrusted Metanet DAG state
- **Unbounded `io.ReadAll`** in content resolver and b-tools client — OOM from malicious endpoints
- **Pre-signed HTLC transaction not validated** against expected UTXO — malicious seller can redirect buyer signature
- **Merkle tree traversal OOM** from malicious RPC node with crafted `totalTxs`

---

## Test Status

| Suite | Total | Passed | Failed |
|-------|-------|--------|--------|
| libbitfs-go unit tests | 799 | 799 | 0 |
| bitfs unit tests | 573 | 573 | 0 |
| bitfs integration tests | 275 | 275 | 0 |
| **Total** | **1647** | **1647** | **0** |

All previously failing integration tests have been fixed in libbitfs-go:
- `TestDustLimitConstant`, `TestBuildCreateChildInsufficientFunds`, `TestBuildCreateRootInsufficientFundsWithWalletKeys` — fixed by `d9db6bb` (DustLimit 546→1)
- `TestSPVTamperedProof` — fixed by `8039e19` (single-tx block merkle proofs)
- `TestSPVVerifyMetanetTransaction` — fixed by `b1f37c5` (RawTx integrity check)

---

## Previous Findings Status

| ID | Severity | Status | Notes |
|----|----------|--------|-------|
| C-1 | CRITICAL | **FIXED** | Dead `ErrEmptyProofNodes` removed; single-tx blocks verified working |
| H-1 | HIGH | **FIXED** | `VerifyPoW` call added to `VerifyTransaction` |
| H-2 | HIGH | **FIXED** | TxID replay map added in daemon layer (`usedTxIDs` + `usedTxIDsMu`) |
| H-3 | HIGH | **FIXED** | `ReadTimeout=30s, WriteTimeout=60s, IdleTimeout=120s, ReadHeaderTimeout=10s, MaxHeaderBytes=1MB` |
| H-4 | HIGH | **FIXED** | `io.LimitReader(resp.Body, MaxPaymailResponseSize)` on both calls |
| M-1 | MEDIUM | **UNFIXED** | TLV Uvarint overflow — `int(length)` wraps negative for large values |
| M-2 | MEDIUM | **UNFIXED** | Child name length overflow — no max-length check |
| M-3 | MEDIUM | **UNFIXED** | `CalculatePrice` integer overflow — no overflow guard on multiplication |
| M-4 | MEDIUM | **UNFIXED** | `ParseHTLCPreimage` extracts from fragile position without hash verification |
| M-5 | MEDIUM | **UNFIXED** | `BuildSellerClaimTx` does not verify `SHA256(Capsule)` against HTLC script |
| M-6 | MEDIUM | **UNFIXED** | Fee estimation underestimates HTLC output size |
| M-7 | MEDIUM | **FIXED** | HTTPS validation on all capability URLs + template var escaping |
| M-8 | MEDIUM | **FIXED** | PKI URL template injection — `url.PathEscape()` applied |
| M-9 | MEDIUM | **UNFIXED** | RPC client does not check HTTP status code |
| M-10 | MEDIUM | **UNFIXED** | RPC response ID not validated |
| M-11 | MEDIUM | **UNFIXED** | SPV header sync does not validate chain continuity |
| M-12 | MEDIUM | **PARTIAL** | `cleanupExpiredSessions()` exists but never called from `Start()` |
| M-13 | MEDIUM | **UNFIXED** | Unbounded invoice and rate limiter maps |
| M-14 | MEDIUM | **UNFIXED** | Handshake timestamp not validated |
| M-15 | MEDIUM | **UNFIXED** | Non-atomic writes in FileStore |
| M-16 | MEDIUM | **UNFIXED** | `SaveConfig` creates files with 0666 mode |
| L-1 | LOW | **UNFIXED** | Paid access mode mapped to `AccessFree` in cat/copy/move |
| L-9 | LOW | **CHANGED** | DustLimit corrected to 1 sat; practical impact now negligible |

**Summary: 7 fixed, 0 partial, 14 unfixed, 1 changed** out of 22 previous findings + **10 fixed, 1 by-design, 14 unfixed** out of 25 new findings.

---

## New CRITICAL Findings

### C-1: VerifyMerkleProof Rejects Valid Single-Transaction Blocks (**FIXED**)

- **File**: `libbitfs-go/spv/merkle.go:65-67`
- **Status**: Dead `ErrEmptyProofNodes` sentinel removed. Single-tx block proofs verified working in tests.

---

## New HIGH Findings

### H-NEW-1: TOCTOU Race in Invoice Payment — Concurrent Capsule Double-Delivery (**FIXED** — commit `7c7e25c`)

- **Severity**: HIGH
- **File**: `bitfs/internal/daemon/payment.go:117-319`
- **Description**: `handleGetBuyInfo` and `handleSubmitHTLC` access `invoice.Paid` and `invoice.Capsule` under separate, non-contiguous lock acquisitions. Between `invoicesMu.RUnlock()` and the mutation at line 303, concurrent `handleSubmitHTLC` calls can all read `invoice.Paid == false`, pass the check, and all return the capsule. The `usedTxIDs` replay check only catches the **same** TxID — two different HTLC transactions targeting the same invoice both succeed. Direct financial impact: buyer pays once via two transactions, receives capsule from both.
- **Fix**: Use a single write lock for the entire check-then-set sequence on `invoice.Paid`:
  ```go
  d.invoicesMu.Lock()
  defer d.invoicesMu.Unlock()
  if invoice.Paid {
      writeJSONError(...); return
  }
  // ... verify payment ...
  invoice.Paid = true
  ```

### H-NEW-2: Path Traversal in `mget` via Malicious Remote Filenames (**FIXED**)

- **Severity**: HIGH
- **File**: `bitfs/internal/engine/mget.go:57-64`
- **Description**: `mgetRecurse` constructs local paths via `filepath.Join(localDir, child.Name)` where `child.Name` comes from `NodeState.Children` (populated from Metanet DAG or local state). A name like `../../.ssh/authorized_keys` resolves to an arbitrary path on the local filesystem. This is exploitable when downloading from another user's vault or with corrupted `nodes.json`.
- **Fix**: Validate `child.Name` before joining — reject names containing `..`, `/`, or `\`:
  ```go
  if strings.Contains(child.Name, "..") || strings.ContainsAny(child.Name, "/\\") {
      result.Errors = append(result.Errors, fmt.Sprintf("unsafe name %q, skipping", child.Name))
      continue
  }
  ```

### H-NEW-3: `ContentResolver.fetchFromEndpoint` Has No Response Body Size Limit (**FIXED**)

- **Severity**: HIGH
- **File**: `libbitfs-go/storage/resolver.go:90`
- **Description**: `io.ReadAll(resp.Body)` with no limit. Analogous to H-4 (paymail) but in the primary content-retrieval path. A compromised daemon endpoint can cause OOM. Endpoints can be user-configured or resolved from DNS/Paymail.
- **Fix**: `data, err := io.ReadAll(io.LimitReader(resp.Body, maxCiphertextSize))`

### H-NEW-4: `BuildBuyerRefundTx` Does Not Verify Pre-Signed Tx References Expected HTLC UTXO (**FIXED**)

- **Severity**: HIGH
- **File**: `libbitfs-go/x402/htlc_tx.go:452-519`
- **Description**: A malicious seller could provide a pre-signed transaction that spends a different output. The buyer signs it without verifying that input 0 references the expected `FundingTxID:FundingVout`. The buyer's signature is then valid for an unintended UTXO.
- **Fix**: Verify `tx.Inputs[0].SourceTxID == params.FundingTxID && tx.Inputs[0].SourceTxOutIndex == params.FundingVout`.

### H-NEW-5: Merkle Tree Traversal OOM from Malicious RPC Node (**FIXED**)

- **Severity**: HIGH
- **File**: `libbitfs-go/network/rpc_blockchain.go:124-127, 214-215`
- **Description**: `traversePartialMerkleTree` with large `totalTxs` (e.g., `MaxUint32`) causes up to `2^32` recursive calls via `traverse`. A malicious RPC node can trigger stack overflow or OOM. `calcTreeWidth` with `uint32` shift also has edge cases at depth >= 32.
- **Fix**: Add `if totalTxs > 1<<20 { return error }` and recursion depth guard in `traverse`.

---

## New MEDIUM Findings

### M-NEW-1: Capsule Overwrite Race — Concurrent Buyer Keys — **FIXED**

- **File**: `bitfs/internal/daemon/payment.go:143-180`
- **Description**: Concurrent `handleGetBuyInfo` calls with different `buyer_pubkey` can both observe `len(invoice.Capsule) == 0`, compute capsules for different buyers, and overwrite each other. Buyer A gets Buyer B's capsule, cannot decrypt.
- **Fix**: Re-check `len(invoice.Capsule) == 0` inside write lock. Regression test added.

### M-NEW-2: `GetNodeUTXO` Marks Spent Outside Lock

- **File**: `bitfs/internal/engine/mkdir.go:165-177`
- **Description**: `GetNodeUTXO` releases lock before caller sets `Spent = true`. Concurrent operations can double-spend the same UTXO. The daemon adapter exposes the same engine to concurrent HTTP handlers.
- **Fix**: Add `AllocateNodeUTXO` that atomically finds and marks a node UTXO as spent.

### M-NEW-3: Shell `mput` Access Mode Unvalidated

- **File**: `bitfs/cmd/bitfs/cmd_shell.go:374-381`
- **Description**: Unlike the CLI which validates `--access` against `"free"|"private"`, the shell accepts any string. A typo like `"prviate"` silently becomes `"free"`.
- **Fix**: Validate access mode in shell handler.

### M-NEW-4: `crossDirectoryMove` Stores Content Before TX Builds Complete

- **File**: `bitfs/internal/engine/move.go:177-179`
- **Description**: New ciphertext written to store at line 177 before Phase 1 TX builds begin. On TX build failure, orphaned ciphertext remains in store. Source content deleted at line 412 even though rollback may be needed.
- **Fix**: Defer `Store.Put` until after `allSuccess = true`, or add `Store.Delete(encResult.KeyHash)` to failure cleanup.

### M-NEW-5: `/_bitfs/data/{hash}` Serves Ciphertext Without Access Control — **BY DESIGN**

- **File**: `bitfs/internal/daemon/content.go:20-63`
- **Description**: Any caller who knows the key hash can fetch raw ciphertext of private/paid nodes. Key hashes are visible on-chain in OP_RETURN. While content is encrypted, this leaks ciphertext that could be decrypted if a Method 42 weakness is found.
- **Resolution**: Intentional design — ciphertext is AES-256-GCM encrypted and useless without Method 42 key exchange. Documented explicitly in code.

### M-NEW-6: Unbounded `io.ReadAll` in bcat and bget Client — **FIXED**

- **File**: `bitfs/cmd/bcat/main.go:123,281` and `bitfs/cmd/bget/main.go:142,345`
- **Description**: All four `io.ReadAll(reader)` calls on HTTP response body from `GetData()` have no size limit. Malicious daemon can exhaust client memory.
- **Fix**: `io.ReadAll(io.LimitReader(reader, maxContentSize))` — applied to all 6 sites (3 per binary).

### M-NEW-7: Query Parameter Injection in `GetBuyInfo` — **FIXED**

- **File**: `bitfs/internal/client/client.go:165`
- **Description**: `buyerPubKeyHex` appended raw to URL without `url.QueryEscape`. No hex validation in `GetBuyInfo` (unlike `GetMeta`/`GetData`).
- **Fix**: Replaced with `url.Values{}` + `q.Encode()` for proper percent-encoding.

### M-NEW-8: `Mput` Follows Symlinked Directories — **FIXED**

- **File**: `bitfs/internal/engine/mput.go:44`
- **Description**: `filepath.WalkDir` follows symlinked directories, potentially uploading contents of unintended paths (e.g., `/etc`).
- **Fix**: Added `d.Type()&os.ModeSymlink != 0` check in WalkDir callback (defense in depth).

### M-NEW-9: `crossDirectoryMove` Silently Fails for Directory Nodes

- **File**: `bitfs/internal/engine/move.go:133-139`
- **Description**: Unconditionally reads `srcNodeState.KeyHash` and decrypts content. Directories have no KeyHash — `hex.DecodeString("")` returns empty bytes, `Store.Get([]byte{})` errors with confusing message. Moving a directory would lose all children.
- **Fix**: Guard: `if srcNodeState.Type != "file" { return error }`.

### M-NEW-10: BIP32 Account Index Integer Overflow

- **File**: `libbitfs-go/wallet/hd.go:152-153`, `wallet/vault.go:51`
- **Description**: `accountIndex := vaultIndex + DefaultVaultAccount`. When `vaultIndex = MaxUint32`, overflows to 0, silently mapping to fee key account. `CreateVault` has no bounds check on `NextVaultIndex`.
- **Fix**: Add `if state.NextVaultIndex >= 0x80000000-1 { return ErrTooManyVaults }`.

### M-NEW-11: `sig.Serialize()` Append May Mutate Internal Buffer — **FIXED**

- **File**: `libbitfs-go/x402/htlc_tx.go:333,439,497`
- **Description**: `sigBytes := append(sig.Serialize(), byte(sighash.AllForkID))` may mutate go-sdk's internal signature buffer if it has spare capacity.
- **Fix**: Added `appendSighashFlag()` helper using `make` + `copy`. All 3 sites replaced.

### M-NEW-12: `WalletState` Not Validated on Deserialization

- **File**: `libbitfs-go/wallet/vault.go:35-53`
- **Description**: Corrupted or hand-edited `wallet.json` with wrong `NextVaultIndex` silently misdirects key derivation. No cross-validation of vault `AccountIndex` values.
- **Fix**: Add `Validate()` method to `WalletState`.

### M-NEW-13: `EncryptResult.AESKey` Exposes Raw Key Material — **FIXED**

- **File**: `libbitfs-go/method42/encrypt.go:33-36`
- **Description**: `EncryptResult` includes the raw 32-byte AES key. If struct is logged/serialized, key is disclosed. Key can be re-derived from private key + keyHash.
- **Fix**: Removed `AESKey` field from `EncryptResult`. All test references updated.

### M-NEW-14: Nil Hash in Merkle Traversal Produces Silent Wrong Result — **FIXED**

- **File**: `libbitfs-go/network/rpc_blockchain.go:158-203`
- **Description**: When `getHash()` returns nil (pool exhausted), `combined` becomes 64 zero bytes and `DoubleHash` returns deterministic wrong hash. Corrupted proof data silently swallowed.
- **Fix**: Added `hashErr` sentinel to `getHash()`, nil check, bounds check, and error propagation after traversal.

### M-NEW-15: `KeyHashToPath` Panics on Empty Input

- **File**: `libbitfs-go/storage/filestore.go:38-43`
- **Description**: `hexHash[:2]` panics if `keyHash` is empty. Exported function callable without prior validation.
- **Fix**: Add length validation inside `KeyHashToPath`.

### M-NEW-16: `VerifyHTLCFunding` Returns First Match Without Vout Validation

- **File**: `libbitfs-go/x402/htlc_tx.go:90-118`
- **Description**: Multi-output transactions with duplicate HTLC scripts return index 0. If the correct output is at index 1, seller claims wrong output.
- **Fix**: Validate expected vout directly or return all matching indices.

### M-NEW-17: `BuildHTLCFundingTx` Accepts Amount=0

- **File**: `libbitfs-go/x402/htlc_tx.go:145`
- **Description**: Zero amount passes validation, fails downstream with confusing "build HTLC script" error.
- **Fix**: Explicit `Amount > 0` check.

### M-NEW-18: `btcToSat` Does Not Guard Against Negative Input

- **File**: `libbitfs-go/network/rpc_blockchain.go:20-22`
- **Description**: Negative float from compromised RPC wraps to `MaxUint64 - abs(amount)`, making wallet believe it has massive balance.
- **Fix**: Add `if btc < 0 { return 0 }`.

### M-NEW-19: `PutTxWithPubKey` Silently Overwrites Existing Transactions — **FIXED**

- **File**: `libbitfs-go/spv/boltstore.go:231-258`
- **Description**: Unlike `PutTx` (which returns `ErrDuplicateTx`), `PutTxWithPubKey` silently overwrites, including SPV proofs. Could replace verified proof with invalid one.
- **Fix**: Added duplicate TxID check in both `BoltTxStore` and `MemTxStore`, returns `ErrDuplicateTx`.

### M-NEW-20: `ResolvePath` Stack Unbounded

- **File**: `libbitfs-go/metanet/resolve.go:49-115`
- **Description**: Path with 10,000 components allocates 10,000-element slice. No max depth check. Combined with symlink following: up to `MaxLinkDepth*pathDepth` lookups.
- **Fix**: `if len(pathComponents) > MaxPathDepth { return ErrPathTooDeep }`.

### M-NEW-21: Markdown Response Path Injection — **FIXED**

- **File**: `bitfs/internal/daemon/routes.go:198-207`
- **Description**: `serveBasicInfo` Markdown case does not escape `path`. If rendered to HTML downstream, enables XSS via crafted path.
- **Fix**: Added `markdownEscape()` helper, applied to path in `serveBasicInfo` and child names in `serveMarkdown`.

### M-NEW-22: `lookupPrivKey` O(n) with Hardcoded Lookahead

- **File**: `bitfs/internal/engine/engine.go:282-305`
- **Description**: Iterates 0 to `NextReceiveIndex+10` deriving and comparing keys. O(n) per UTXO lookup. Keys derived beyond `+10` range permanently locked.
- **Fix**: Store derivation index alongside `UTXOState`.

### M-NEW-23: Shell History File Contains Sensitive Commands

- **File**: `bitfs/cmd/bitfs/cmd_shell.go:71-72`
- **Description**: Shell history file stores vault paths and potentially `--password` flag values. History file permissions not explicitly set.
- **Fix**: Set history file mode to 0600. Filter sensitive commands.

### M-NEW-24: `handleData` Endpoint Serves Private Node Metadata — **FIXED**

- **File**: `bitfs/internal/daemon/routes.go:174-186`
- **Description**: `serveJSON` encodes entire `NodeInfo` including `PNode` and `KeyHash` for private nodes without checking session auth. Private node metadata leaked over HTTP.
- **Fix**: Replaced `serveJSON` with sanitized map builder that only exposes `key_hash` for `"free"` access nodes.

### M-NEW-25: Capsule Not Persisted — Process Crash Loses Delivery

- **File**: `bitfs/internal/daemon/payment.go:302-318`
- **Description**: After marking `invoice.Paid = true`, if process crashes before response is written, buyer paid but lost capsule. On restart, in-memory invoice and `usedTxIDs` are lost — buyer must re-pay.
- **Fix**: Persist invoice state to disk before responding.

---

## New LOW Findings

### L-NEW-1: `child.PubKey[:8]` Panic on Short Keys

- **File**: `bitfs/internal/engine/mget.go:51`
- **Description**: Panics if `child.PubKey` has fewer than 8 characters (corrupted state).
- **Fix**: `n := min(len(child.PubKey), 8)`.

### L-NEW-2: `Get` Engine Method Leaves Partial File on Error

- **File**: `bitfs/internal/engine/get.go:40-55`
- **Description**: If `io.Copy` fails mid-write, partially-written file remains. `bget` CLI properly removes on error, but engine method does not.
- **Fix**: `os.Remove(localPath)` on error.

### L-NEW-3: Semantically Wrong 33-byte Key Handling in bcat/bget

- **File**: `bitfs/cmd/bcat/main.go:188-194`, `bitfs/cmd/bget/main.go:239-244`
- **Description**: 33-byte input strips first byte (comment says "compressed pubkey prefix"). If user accidentally passes compressed public key, it's silently treated as private key.
- **Fix**: Only accept 32-byte private keys.

### L-NEW-4: Completer Test Missing New Shell Commands

- **File**: `bitfs/cmd/bitfs/completer_test.go:16-19`
- **Description**: `shellCommandsList` doesn't include `cat`, `get`, `mget`, `mput`, `publish`, `unpublish`.

### L-NEW-5: `Mput` Does Not Validate `RemoteDir` Path

- **File**: `bitfs/internal/engine/mput.go:63-67`
- **Description**: Empty `RemoteDir` creates paths from root. No `..` validation.

### L-NEW-6: `webserve.go` Is Empty Stub

- **File**: `bitfs/internal/daemon/webserve.go`
- **Description**: File contains only TODOs, references non-existent methods.

### L-NEW-7: `containsPathTraversal` Misses Double-Encoded Sequences

- **File**: `bitfs/internal/daemon/content.go:160-169`
- **Description**: `%252E%252E` bypasses the check. Low practical risk since `GetNodeByPath` would return not-found.

### L-NEW-8: `GetSession` TOCTOU Between Expiry Read and Delete

- **File**: `bitfs/internal/daemon/daemon.go:373-390`
- **Description**: Read lock released before write lock acquired for delete. Fresh session could be deleted if created between locks.

### L-NEW-9: Rollback Defer Doesn't Clean Orphaned Store Blobs

- **File**: `bitfs/internal/engine/move.go:241-272`
- **Description**: Phase 1 store write not reversed on Phase 2 failure. Overlaps with M-NEW-4.

### L-NEW-10: Shell `cd` Doesn't Validate Target Is Directory

- **File**: `bitfs/cmd/bitfs/cmd_shell.go:119-131`
- **Description**: `cd /nonexistent/path` silently accepted. Stale completions served.

### L-NEW-11: `handleBSVAlias` Trusts `r.Host` for URL Construction

- **File**: `bitfs/internal/daemon/routes.go:106-128`
- **Description**: Host header injection in Paymail capabilities endpoint. Malicious client can inject any hostname.
- **Fix**: Use configured `ListenAddr` or explicit `PublicBaseURL`.

### L-NEW-12: `zeroString` Zeroes Copy, Not Original

- **File**: `bitfs/cmd/bitfs/password.go:137-143`
- **Description**: `[]byte(*s)` creates a copy. Original string backing array unaffected. False sense of security.

### L-NEW-13: `Mput` Uploads Symlinked Directory Contents

- **File**: `bitfs/internal/engine/mput.go:79`
- **Description**: `WalkDir` follows symlinked directories, potentially uploading `/etc` or `~/.ssh`.

### L-NEW-14: `KeyPair.PrivateKey` Lacks `json:"-"` Tag

- **File**: `libbitfs-go/wallet/hd.go:38-42`
- **Description**: Accidental `json.Marshal(keyPair)` serializes private key.

### L-NEW-15: `MemHeaderStore.GetHeader` Returns Mutable Reference

- **File**: `libbitfs-go/spv/store.go:100-114`
- **Description**: Callers can corrupt store by mutating returned pointer.

### L-NEW-16: `validateChildName` Allows Control Characters

- **File**: `libbitfs-go/metanet/directory.go:156-170`
- **Description**: Only rejects `/`, `\x00`, `.`, `..`. Allows `\r`, `\n`, `\t`, Unicode RLO overrides. Terminal spoofing risk.

### L-NEW-17: `SyncHeaders` Treats `ErrDuplicateHeader` as Fatal

- **File**: `libbitfs-go/network/spvclient.go:164`
- **Description**: Second `SyncHeaders` call after restart fails at first stored block.
- **Fix**: Treat `ErrDuplicateHeader` as non-fatal.

### L-NEW-18: `VerifyChildMembership` Non-Constant-Time Comparison

- **File**: `libbitfs-go/metanet/merkle.go:141-146`
- **Description**: Byte-by-byte loop with early exit. Low practical risk for Merkle roots.
- **Fix**: `subtle.ConstantTimeCompare`.

### L-NEW-19: Per-Link Depth Limit Allows Unlimited Total Follows

- **File**: `libbitfs-go/metanet/resolve.go:98-103`
- **Description**: `MaxLinkDepth=10` is per-link, not total. Path `/link1/link2/.../link100` triggers 1000 lookups.

### L-NEW-20: `Unpublish` Does Not Persist State Immediately

- **File**: `bitfs/internal/engine/unpublish.go:16`
- **Description**: Binding removal only persisted on Engine close. Crash loses unpublish.

### L-NEW-21: `resolve.go` Always Prepends `https://` Without Scheme Check

- **File**: `bitfs/internal/client/resolve.go:49`
- **Description**: If endpoint already contains scheme, result is `https://http://...`.

### L-NEW-22: `CatOpts.VaultIndex` Accepted but Ignored

- **File**: `bitfs/internal/engine/cat.go:53`
- **Description**: API accepts `VaultIndex` field but uses `node.VaultIndex` instead. Misleading struct field.

### L-NEW-23: `handleSubmitHTLC` Returns Capsule After Broadcast Without Persistence

- **File**: `bitfs/internal/daemon/payment.go:302-318`
- **Description**: Same as M-NEW-25 from persistence angle. On crash + restart, both `usedTxIDs` and invoice state reset.

---

## Findings Summary

| Severity | First Audit | Unfixed | New Findings | **Total Open** |
|----------|-------------|---------|--------------|----------------|
| CRITICAL | 1 | 1 | 0 | **1** |
| HIGH | 4 | 3 | 5 | **8** |
| MEDIUM | 16 | 14 | 25 | **39** |
| LOW | 20 | ~18 | 23 | **~41** |
| **Total** | **41** | **36** | **53** | **~89** |

---

## Recommended Fix Priority

### Immediate (Before Any Production Use)

| Priority | ID | Impact | Effort |
|----------|----|--------|--------|
| 1 | C-1 | SPV breaks on single-tx blocks | 2-line fix |
| 2 | H-NEW-1 | Payment double-delivery | Lock refactor |
| 3 | H-NEW-2 | Arbitrary file write via mget | Input validation |
| 4 | H-1 | SPV security model bypassed | New function + integration |
| 5 | H-NEW-4 | Buyer signs unintended UTXO | UTXO reference check |
| 6 | H-NEW-3 | Client OOM from malicious endpoint | `io.LimitReader` |
| 7 | H-NEW-5 | Stack overflow from malicious RPC | Depth/size guards |
| 8 | H-3 | Daemon DoS via slowloris | Server timeout config |
| 9 | H-4 | Paymail OOM | `io.LimitReader` |

### Short-Term (Next Sprint)

| Priority | IDs | Theme |
|----------|-----|-------|
| 10 | M-NEW-2, M-NEW-22 | UTXO lock safety + key lookup |
| 11 | M-NEW-4, L-NEW-9 | Store consistency on move failure |
| 12 | M-NEW-5, M-NEW-24 | Access control on data/metadata endpoints |
| 13 | M-NEW-6, M-NEW-7 | Client-side input validation |
| 14 | M-NEW-8, M-NEW-9 | Mput/crossDirMove safety |
| 15 | M-1, M-3 | Integer overflow guards |
| 16 | M-12, M-13 | Bounded maps + cleanup goroutines |
| 17 | M-15 | Atomic writes in FileStore |
| 18 | L-1 | Paid access mode mapping |

### Long-Term (Technical Debt)

| IDs | Theme |
|-----|-------|
| M-NEW-10 thru M-NEW-20 | Wallet bounds, HTLC robustness, BoltStore consistency |
| L-NEW-14 thru L-NEW-23 | Key material safety, path validation, state persistence |
| All unfixed M-4 thru M-16 | Spec compliance, HTTPS enforcement, RPC validation |

---

## Architecture Assessment Update

### Strengths (unchanged)

1. **Sound cryptographic core**: AES-256-GCM, HKDF-SHA256, ECDH, Argon2id, BIP32/39/44 all correct
2. **Clean layered design**: libbitfs-go → engine → cmd separation is maintained
3. **Comprehensive test coverage**: 1647 tests, only 5 pre-existing failures
4. **Capsule protocol correctness**: XOR-masked capsule correctly leverages ECDH symmetry

### Weaknesses (updated)

1. **Concurrency safety gaps**: Payment flow (H-NEW-1, M-NEW-1), UTXO allocation (M-NEW-2), session management (L-NEW-8) all have race conditions. The daemon serves concurrent HTTP requests but the engine was designed for single-threaded CLI use.
2. **Trust boundary validation**: Content from untrusted sources (Metanet DAG, RPC nodes, HTTP endpoints) is insufficiently validated. Path traversal (H-NEW-2), unbounded reads (H-NEW-3), Merkle OOM (H-NEW-5).
3. **State persistence**: All critical state (invoices, payments, sessions, TxID replay set) is in-memory only. Any crash loses payment records.
4. **SPV security remains broken**: No PoW validation (H-1) + single-tx rejection (C-1) = SPV provides no real security.
5. **Unfixed findings accumulation**: 36 of 41 original findings still open. Velocity of new code (shell commands, mget/mput, cross-dir move) outpaces security remediation.

### Trust Boundaries (updated)

| Boundary | Trust Level | Key Gaps |
|----------|-------------|----------|
| RPC node | Trusted (should be Untrusted) | No PoW validation, Merkle OOM, negative amounts |
| Metanet DAG content | Untrusted | Path traversal in child names, control chars |
| HTTP endpoints | Untrusted | No body size limits (3 locations), no access control on data endpoint |
| Local state files | Trusted (fragile) | No WalletState validation, no integrity checks |
| Concurrent HTTP clients | Untrusted | Payment TOCTOU, session races, unbounded maps |

---

*Report generated by 5 parallel audit agents covering: (1) Previous findings verification, (2) New code review, (3) libbitfs-go deep audit, (4) Application layer audit, (5) Test status. All HIGH findings manually verified against source code.*

# Spec Audit Report — 2026-02-28

## Summary

Audited all 13 spec files (`bitfs/docs/spec/01-method42.md` through `13-network.md`) against their Go implementations in `libbitfs-go/` and `bitfs/`. Specs were updated in-place to match code (code is authoritative — tested, audited, and race-free).

**Result:** 10 of 13 specs required updates; 3 were fully compliant. All discrepancies resolved.

## Findings

| Spec | Status | Changes Made |
|------|--------|-------------|
| 01-method42 | FIXED | 14 discrepancies — Access type, ComputeCapsuleHash binding, 8 missing functions (capsule nonce, buyer mask, metadata encryption), missing constants, removed unused Rabin errors |
| 02-tx | REWRITTEN | Major rewrite — legacy Build* API replaced by MutationBatch, MetanetTx struct changed (RawTx+TxID), OP_RETURN functions renamed, 7 new functions added |
| 03-metanet | FIXED | ParseNode signature ([][]byte not []byte), TLV encoding (uint32 not bool), MerkleRoot added to Node struct, 6 missing functions, error table corrected, hard link TODO removed |
| 04-wallet | FIXED | Removed unimplemented DeriveKeyCacheKey/UTXOEntry, added Vault.Deleted + WalletState.NextVaultIndex fields, added 2 error types, added NewWalletState/Validate |
| 05-spv | FIXED | Added 12 missing functions (PoW, difficulty, Merkle), Network type, ChainVerificationResult, 7 constants, 6 error types, updated verification descriptions |
| 06-storage | FIXED | Sharding path corrected (1 byte not 2), Compress/Decompress signature, added ContentResolver, removed OnChainRef/ErrDecompressionFailed |
| 07-paymail | FIXED | Added PaymentDestination capability, DNSSEC support, 6 interface types, 8 functions, BRFC constants, 2 error types |
| 08-x402 | FIXED | Added InvoiceID replay protection, FileTxID capsule binding, 7 tx-building functions, 8 types, 7 constants, 4 error types, DefaultHTLCTimeout 144→72 |
| 09-daemon | FIXED | Added 10 missing endpoints (dashboard, sales, pay, versions, paymail), fixed PKI route, added Config fields (Mainnet, TrustProxy) |
| 10-cmd-bitfs | FIXED | Fixed put --encrypt→--access, link -s→--soft (CLI), removed 5 unimplemented global flags, documented shell-only features |
| 11-cmd-btools | PASS | Fully compliant — all flags, output formats, error handling match |
| 12-revshare | PASS | 100% compliant — all types, serialization, distribution algorithm match |
| 13-network | PASS | 100% compliant — BlockchainService, RPCClient, SPVClient, presets all match |

## Key Patterns

### Specs Lagging Behind Code
The most common issue was specs not reflecting features added during recent development cycles (btools-shell-compliance, atomic-tx-builder, P0 security fixes, vault extraction, dashboard, paymail completion). Code evolved; specs didn't keep up.

### Architecture Changes Not Reflected
- **02-tx**: MutationBatch replaced three separate Build* functions — the most significant architectural change
- **08-x402**: P0 security fixes added InvoiceID replay protection and capsule hash binding

### Code Fix
- **03-metanet**: Removed outdated TODO comment about hard link validation (already implemented in `directory.go:61-69`)

## Verification

All tests pass with race detector after spec updates:
- libbitfs-go: 12 packages, all pass (`go test ./... -race`)
- bitfs: 11 packages, all pass (`go test ./... -race`)

## Commits

1. `e2db64e` — docs(spec): audit and fix specs 01-05
2. `2c47f04` — fix(metanet): remove outdated hard link validation TODO (libbitfs-go)
3. `2e29e85` — docs(spec): audit and fix specs 06-10

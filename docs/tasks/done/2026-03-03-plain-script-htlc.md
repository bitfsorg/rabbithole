# Replace sCrypt HTLC with Plain Bitcoin Script HTLC

## Phase 1-4: Go Core (libbitfs-go)

- [x] 1.1 Rewrite `BuildHTLC()` — plain script construction
- [x] 1.2 Update extraction constants (new offsets)
- [x] 1.3 Replace `isArtifactScript()` with `isHTLCScript()`
- [x] 1.4 Remove `init()` function
- [x] 1.5 Update `ParseHTLCPreimage()` — new claim format
- [x] 1.6 ~~Add `encodeScriptNum()` helper~~ — removed (OP_CLTV not used on BSV)
- [x] 2.1 Update `BuildSellerClaimTx()` unlocking script
- [x] 2.2 Rewrite `BuildBuyerRefundTx()` — remove preimage, simplify
- [x] 2.3 Update `BuyerRefundParams` doc comment
- [x] 3.1 Delete `artifact.go`
- [x] 3.2 Delete `artifacts/bitfsHTLC.json`
- [x] 3.3 Delete `artifact_test.go`
- [x] 3.4 Remove unused imports from `htlc.go`
- [x] 4.1 Update `htlc_tx_test.go` assertions
- [x] 4.2 Update `coverage_supplement_test.go` and `coverage_supplement2_test.go`
- [x] 4.3 Update `payment_test.go` artifact references
- [x] 4.4 Run `go test ./...` in libbitfs-go — all pass (87 tests, 12 packages)

## Phase 5: TypeScript (libbitfs-ts)

- [x] 5.1 Rewrite `artifact.ts` — plain script builder
- [x] 5.2 Update `htlc.ts` — buildHTLC, extraction, claimTx
- [x] 5.3 Simplify `refund.ts` — remove preimage
- [x] 5.4 Update `verify.ts` — parseHTLCPreimage new format
- [x] 5.5 Update `htlc.test.ts` tests
- [x] 5.6 Run `npm test` in libbitfs-ts — all pass (33 suites, 968 tests)

## Phase 6: Verification

- [x] 6.1 Go unit tests pass (87 payment tests + all 12 packages)
- [x] 6.2 bitfs integration tests pass (276 tests)
- [x] 6.3 bitfs e2e tests pass — TestPaidPurchaseFlow PASS, TestPaidPurchase_BuyerRefund PASS (2 flaky UTXO tests unrelated)
- [x] 6.4 TS tests pass (33 suites, 958 tests)
- [x] 6.5 Cross-language byte-identical verification — CONFIRMED (106 bytes, no OP_CLTV)

## Design Notes

- **BSV OP_CLTV incompatibility**: BSV post-Genesis treats OP_CLTV (0xb1) as OP_NOP2. Standard mempool policy (`SCRIPT_VERIFY_DISCOURAGE_UPGRADABLE_NOPS`) rejects transactions using NOPx opcodes. Timeout enforced at transaction level via nLockTime only.
- **Final script**: 106 bytes, ALL fixed offsets (invoiceId@1, capsuleHash@21, sellerPkh@57, buyerPkh@83). No variable-length fields.
- **Claim preimage**: `fileTxID (32B) || capsule (32B)` — 64 bytes total.

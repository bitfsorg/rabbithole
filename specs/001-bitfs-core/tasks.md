# Tasks: BitFS Core Framework (Modules 1-6)

**Input**: Design documents from `specs/001-bitfs-core/`, module specs from `bitfs/spec/01-06`
**Prerequisites**: plan.md (required), spec.md (required), data-model.md, research.md
**Scope**: Core framework only — method42, wallet, tx, metanet, spv, storage
**Approach**: Audit existing tests, supplement gaps, then implement missing functionality

**Tests**: TDD enforced per constitution. Existing tests are audited; gaps
identified below are supplemented before any new implementation.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story (US1=Encrypted Storage, US5=SPV Verification)
- Include exact file paths in descriptions

## Current Test Status

| Package | Before | After | Delta | Status |
|---------|--------|-------|-------|--------|
| method42 | 52 | 89 | +37 | DONE |
| wallet | 56 | 77 | +21 | DONE |
| tx | 34 | 61 | +27 | DONE |
| metanet | 118 | 150 | +32 | DONE |
| spv | 73 | 93 | +20 | DONE |
| storage | 39 | 58 | +19 | DONE |
| integration | 47 | 56 | +9 | DONE |
| **Total** | **419** | **584** | **+165** | |

---

## Phase 1: Test Audit & Gap Analysis ✅ COMPLETE

**Purpose**: Review each module's test coverage against spec, identify
concrete missing test cases before writing any new code.

- [x] T001 [P] [US1] Audit method42 tests against spec/01-method42.md, document gaps in internal/method42/AUDIT.md
- [x] T002 [P] [US1] Audit wallet tests against spec/04-wallet.md, document gaps in internal/wallet/AUDIT.md
- [x] T003 [P] [US1] Audit tx tests against spec/02-tx.md, document gaps in internal/tx/AUDIT.md
- [x] T004 [P] [US1] Audit metanet tests against spec/03-metanet.md, document gaps in internal/metanet/AUDIT.md
- [x] T005 [P] [US5] Audit spv tests against spec/05-spv.md, document gaps in internal/spv/AUDIT.md
- [x] T006 [P] [US1] Audit storage tests against spec/06-storage.md, document gaps in internal/storage/AUDIT.md

**Checkpoint**: All AUDIT.md files reviewed, gap list finalized by user. ✅

---

## Phase 2: Supplement tx Tests (Critical — UNDER target) ✅ COMPLETE

**Purpose**: tx package was 6 tests under target. Brought from 34 to 61 tests (+27).

- [x] T007 [US1] Add test: verify BuildCreateChild Output[2] refreshes P_parent UTXO in internal/tx/tx_test.go
- [x] T008 [US1] Add test: verify change UTXO amount = input - outputs - fee in internal/tx/tx_test.go
- [x] T009 [US1] Add test: verify UTXO tracking across sequential CreateChild transactions in internal/tx/tx_test.go
- [x] T010 [US1] Add test: verify P2PKH script format in P2PKHScript() in internal/tx/tx_test.go
- [x] T011 [US1] Add test: verify extremely large payload (>64KB) handling in internal/tx/tx_test.go
- [x] T012 [US1] Add test: verify all P2PKH outputs >= 546 satoshis (dust limit) in internal/tx/tx_test.go

---

## Phase 3: Supplement method42 Tests (Minor gaps) ✅ COMPLETE

**Purpose**: method42 supplemented from 52 to 89 tests (+37).

- [x] T013 [P] [US1] Add test: verify HKDF info parameter is "bitfs-file-encryption" constant in internal/method42/method42_test.go
- [x] T014 [P] [US1] Add test: verify key derivation consistency (same D_node + P_node + key_hash always produces same AES key) in internal/method42/method42_test.go
- [x] T015 [P] [US1] Add test: verify large plaintext (1MB+) encrypt/decrypt round-trip in internal/method42/method42_test.go

---

## Phase 4: Supplement wallet Tests (Minor gaps) ✅ COMPLETE

**Purpose**: wallet supplemented from 56 to 77 tests (+21).

- [x] T016 [P] [US1] Add test: verify Argon2id parameters (time=3, memory=64MB, parallelism=4) in internal/wallet/wallet_test.go
- [x] T017 [P] [US1] Add test: verify vault account index starts at 1 (not 0) and max boundary in internal/wallet/wallet_test.go
- [x] T018 [P] [US1] Add test: verify DeriveKeyCacheKey function in internal/wallet/wallet_test.go

---

## Phase 5: Supplement metanet Tests (Edge cases) ✅ COMPLETE

**Purpose**: metanet supplemented from 118 to 150 tests (+32).
Implementation fixes applied: hard link directory rejection + InheritPricePerKB cycle guard.

- [x] T019 [P] [US1] Add test: verify hard links to directories are rejected in internal/metanet/metanet_test.go
- [x] T020 [P] [US1] Add test: verify symlink cycle detection (A→B→C→A) at depth 10 in internal/metanet/metanet_test.go
- [x] T021 [P] [US1] Add test: verify access mode inheritance (node inherits parent AccessMode if unset) in internal/metanet/metanet_test.go

---

## Phase 6: Supplement spv Tests (Verification edge cases) ✅ COMPLETE

**Purpose**: spv supplemented from 73 to 93 tests (+20).

- [x] T022 [P] [US5] Add test: verify checkpoint validation against hardcoded checkpoints in internal/spv/spv_test.go
- [x] T023 [P] [US5] Add test: verify longest chain selection between competing chains in internal/spv/spv_test.go
- [x] T024 [P] [US5] Add test: verify malformed raw transaction is rejected in internal/spv/spv_test.go

---

## Phase 7: Supplement storage Tests (Robustness) ✅ COMPLETE

**Purpose**: storage supplemented from 39 to 58 tests (+19).

- [x] T025 [P] [US1] Add test: verify OnChainRef type stores and retrieves on-chain content references in internal/storage/storage_test.go
- [x] T026 [P] [US1] Add test: verify atomic Put behavior (content not partially written on error) in internal/storage/storage_test.go

---

## Phase 8: Supplement Integration Tests ✅ COMPLETE

**Purpose**: Integration tests supplemented from 47 to 56 top-level tests (+9).
Pre-existing tests already covered T027, T028, T030, T033, T035.

### Integration: Wallet + Crypto (US1)

- [x] T027 [P] [US1] Add integration test: full wallet create → derive keys → encrypt → decrypt round-trip across mainnet/testnet/regtest in integration/wallet_crypto_test.go (pre-existing)
- [x] T028 [P] [US1] Add integration test: vault isolation — vault A keys cannot decrypt vault B content in integration/wallet_crypto_test.go (pre-existing)
- [x] T029 [P] [US1] Add integration test: fee chain key derivation separate from vault keys in integration/wallet_crypto_test.go

### Integration: Transaction Building (US1)

- [x] T030 [P] [US1] Add integration test: create root → add children → self-update → verify DAG structure in integration/tx_build_test.go (pre-existing)
- [x] T031 [P] [US1] Add integration test: UTXO chain continuity across multiple CreateChild txs in integration/tx_build_test.go
- [x] T032 [P] [US1] Add integration test: transaction size estimation accuracy within 5% of actual in integration/tx_build_test.go

### Integration: Filesystem Operations (US1)

- [x] T033 [P] [US1] Add integration test: mkdir → put files → ls → cat → rm → verify cleanup in integration/filesystem_test.go (pre-existing)
- [x] T034 [P] [US1] Add integration test: deep nested path (10+ levels) create and resolve in integration/filesystem_test.go
- [x] T035 [P] [US1] Add integration test: symlink and hardlink creation, traversal, and edge cases in integration/filesystem_test.go (pre-existing)
- [x] T036 [P] [US1] Add integration test: concurrent directory modifications (monotonic index) in integration/filesystem_test.go

### Integration: Storage + Encryption (US1)

- [x] T037 [P] [US1] Add integration test: encrypted store → retrieve → decrypt with all 3 access modes in integration/filesystem_test.go
- [x] T038 [P] [US1] Add integration test: content-addressed dedup — same plaintext, same key_hash in integration/filesystem_test.go

### Integration: SPV Verification (US5)

- [x] T039 [P] [US5] Add integration test: build Merkle tree → create proof → verify full chain in integration/tx_build_test.go
- [x] T040 [P] [US5] Add integration test: verify Metanet transactions via SPV after creating file tree in integration/tx_build_test.go

---

## Phase 9: Implement to Pass (Green Phase) ✅ COMPLETE

**Purpose**: All tests already pass. Two implementation fixes were needed:

- [x] T048 [US1] Implement missing metanet logic: hard link directory rejection in internal/metanet/directory.go
- [x] T048b [US1] Implement missing metanet logic: InheritPricePerKB cycle guard in internal/metanet/link.go
- [x] T041-T047, T049-T051: All supplemented tests passed without implementation changes (existing code already supported the tested behavior)

**Checkpoint**: All tests pass (Green phase). ✅
- `go test ./...` — 16 packages, all pass (528 unit tests)
- `go test -tags=integration ./integration/` — 56 integration tests, all pass

---

## Phase 10: Refactor & Polish ✅ COMPLETE

**Purpose**: Clean up after Green phase, maintain passing tests.

- [x] T052 [P] Remove AUDIT.md files (temporary artifacts) from internal/*/
- [x] T053 [P] Run `go vet ./...` and fix any warnings (clean, no warnings)
- [x] T054 [P] Run `staticcheck ./...` if available, fix issues (skipped — not installed)
- [x] T055 Verify all tests still pass after refactoring: `go test ./... -count=1`
- [x] T056 Update spec/TASKS.md checkboxes to reflect current completion status

---

## Dependencies & Execution Order

### Phase Dependencies

```
Phase 1 (Audit)       → COMPLETE ✅
Phase 2 (tx tests)    → COMPLETE ✅
Phase 3-7 (other tests) → COMPLETE ✅
Phase 8 (integration) → COMPLETE ✅
Phase 9 (implement)   → COMPLETE ✅
Phase 10 (refactor)   → COMPLETE ✅
```

---

## Notes

- All 10 phases complete. 584 total tests across unit + integration, all passing.
- Implementation fixes: 2 (hard link directory rejection, InheritPricePerKB cycle guard)
- `go vet ./...` clean. AUDIT.md artifacts removed.

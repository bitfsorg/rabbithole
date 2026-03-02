# Test Coverage Audit Report

**Date**: 2026-02-26
**Scope**: All Go projects in RabbitHole monorepo (libbitfs-go, bitfs, metanet)

---

## Executive Summary

| Project | Coverage | Unit Tests | Test LOC | Source LOC | Test:Code Ratio |
|---------|----------|-----------|----------|------------|-----------------|
| libbitfs-go | **85.7%** | 1,063 | 15,584 | 8,142 | 1.91:1 |
| bitfs | **58.6%** | 728 | 13,048 + 9,942 (integration) | 11,449 | 2.01:1 |
| metanet | **79.9%** | 728 | 5,670 | 4,579 | 1.24:1 |
| **Total** | — | **2,519** + 275 integration | **44,244** | **24,170** | **1.83:1** |

Integration tests: 275 tests in 19 files (`bitfs/integration/`), run with `-tags=integration`.
E2E tests: Docker regtest suite (`bitfs/e2e/`), run with `-tags e2e`.

---

## 1. libbitfs-go — Package Coverage

| Package | Coverage | Rating | Notes |
|---------|----------|--------|-------|
| paymail | **96.5%** | Excellent | DNS resolution, PKI, URI parsing fully covered |
| metanet | **95.6%** | Excellent | Directory ops, parser, Merkle, links, resolve |
| config | **94.5%** | Excellent | Load/save/validate |
| storage | **92.4%** | Excellent | Content-addressed filestore + resolver |
| spv | **91.7%** | Excellent | Headers, Merkle proofs, BoltDB + mem stores |
| method42 | **88.3%** | Good | ECDH, encrypt/decrypt, KDF |
| wallet | **87.0%** | Good | HD keys, vault CRUD, seed/mnemonic |
| x402 | **79.5%** | Adequate | Headers, invoice, HTLC tx |
| network | **74.3%** | **Needs Work** | RPC client partially tested, SPV client weak |
| tx | **72.7%** | **Needs Work** | OP_RETURN & batch OK, sign.go severely under-tested |

### Critical Gaps in libbitfs-go

**tx/sign.go — 4 functions at 0%:**
| Function | Line | Impact |
|----------|------|--------|
| `BuildUnsignedCreateChildTx` | 214 | HIGH — core transaction builder |
| `BuildUnsignedSelfUpdateTx` | 326 | HIGH — node update transactions |
| `BuildP2PKHOutput` | 427 | MEDIUM — utility |
| `TxHexFromBytes` | 443 | LOW — conversion utility |

These are exercised by e2e tests (Docker regtest) but have **zero unit test coverage**. The `BuildUnsignedCreateRootTx` is similarly in this file but covered via integration tests. Risk: regressions in transaction building could ship without detection in CI (if e2e tests aren't run on every commit).

**network/spvclient.go — Critical path under-tested:**
| Function | Coverage | Impact |
|----------|----------|--------|
| `NewSPVClient` | 42.9% | Initialization/validation paths missed |
| `VerifyTx` | 44.7% | HIGH — SPV verification is a security-critical function |
| `SyncHeaders` | 70.8% | Header sync edge cases not covered |

**network/mock.go — 5 mock methods at 0%:**
`ListUnspent`, `GetUTXO`, `BroadcastTx`, `GetRawTx`, `ImportAddress`. These are mock stubs returning `nil` — 0% is expected but inflates the gap.

**network/rpc_blockchain.go:**
| Function | Coverage | Notes |
|----------|----------|-------|
| `ImportAddress` | 0% | Not used in current flows |
| `readVarInt` | 28.6% | Only happy path tested |

**Other below-threshold functions:**
| Function | File | Coverage |
|----------|------|----------|
| `VerifyChildMembership` | metanet/merkle.go:119 | 60.0% |
| `buildP2PKHLockScript` | x402/htlc_tx.go:522 | 58.3% |
| `DeriveBuyerMask` | method42/kdf.go:80 | 66.7% |
| `bytesEqual` | network/spvclient.go:172 | 66.7% |

---

## 2. bitfs — Package Coverage

| Package | Coverage | Rating | Notes |
|---------|----------|--------|-------|
| bstat | **90.8%** | Excellent | |
| btree | **90.5%** | Excellent | |
| bls | **90.2%** | Excellent | |
| daemon | **87.6%** | Good | Routes, handshake, content serving |
| client | **84.5%** | Good | HTTP client for b-tools |
| engine | **68.9%** | **Needs Work** | Core business logic has significant gaps |
| bcat | **64.5%** | Adequate | Payment output paths untested |
| bget | **63.3%** | Adequate | Download+payment flows weak |
| buyer | **61.6%** | **Needs Work** | Buy() and resolveUTXOs() at 0% |
| bitfs (CLI) | **39.1%** | **Critical** | Most commands have 0% coverage |
| bmget | **5.8%** | **Critical** | Nearly untested |
| dashboard | N/A | — | No test files |

### Critical Gaps in bitfs

**cmd/bitfs/ — CLI commands at 0%:**

| Function | File | Description |
|----------|------|-------------|
| `runCat` | cmd_cat.go:19 | Owner's cat command |
| `runCp` | cmd_cp.go:17 | Copy command |
| `runGet` | cmd_get.go:17 | Owner's get command |
| `runMget` | cmd_mget.go:17 | Batch get |
| `runMput` | cmd_mput.go:17 | Batch put |
| `runUnpublish` | cmd_unpublish.go:17 | DNS unpublish |
| `runVerify` | cmd_verify.go:17 | SPV verification CLI |
| `runWalletBalance` | cmd_wallet.go:300 | Balance check |
| `runWalletFund` | cmd_fund.go:26 | Wallet funding |
| `runShell` | cmd_shell.go:32 | 3.8% — interactive REPL |
| `configureChain` | rpc.go:17 | RPC config |
| `promptPassword` | password.go:17 | 20.0% |

These are CLI entry points — hard to unit test since they interact with stdin/stdout, filesystem, and daemon. However, the underlying engine functions they call ARE partially tested. The gap is in argument parsing, error presentation, and I/O orchestration.

**cmd/bmget/ — 5.8% coverage (11 functions at 0%):**
The newest tool (`bmget`) has essentially no test coverage. All core functions (`run`, `downloadFile`, `downloadFreeFile`, `downloadPaidFile`, `writeFile`, error handling) are untested.

**internal/engine/ — Key gaps:**

| Function | Coverage | Impact |
|----------|----------|--------|
| `EncryptNode` | 10.3% | HIGH — Method 42 encryption integration |
| `Sell` | 5.4% | HIGH — Payment/pricing flow |
| `createSoftLink` | 0% | Symlink creation |
| `createHardLink` | 0% | Hard link creation |
| `resolveParentDir` | 45.5% | Directory resolution |
| `Link` | 50.0% | Link entry point |

**internal/buyer/ — Buy flow untested:**

| Function | Coverage | Impact |
|----------|----------|--------|
| `Buy` | 0% | CRITICAL — Entire purchase flow |
| `resolveUTXOs` | 0% | HIGH — UTXO selection for payments |

**internal/engine/daemon_adapter.go — 6 adapter functions at 0%:**
`DeriveNodePubKey`, `DeriveNodeKeyPair`, `GetVaultPubKey`, `NewSPVAdapter`, `VerifyTx`, `NewChainAdapter`, `BroadcastTx`. These bridge the engine to the daemon — untested because they require a running wallet/chain context.

---

## 3. metanet — Package Coverage

| Package | Coverage | Rating | Notes |
|---------|----------|--------|-------|
| chain | **97.7%** | Excellent | Blocks, genesis, tokens, params |
| mining | **97.5%** | Excellent | AuxPoW, anchoring, difficulty |
| proof | **92.2%** | Excellent | Merkle, encryption, verification |
| config | **85.2%** | Good | |
| overlay | **85.1%** | Good | Peer discovery, topics, service |
| payment | **84.1%** | Good | Channels, funding, vouchers |
| contract | **79.0%** | Adequate | Deals, scripts, challenges |
| cmd/metanet | **35.2%** | **Needs Work** | Daemon lifecycle commands untested |

### Gaps in metanet

**cmd/metanet/ — Process management at 0%:**
`cmdStart`, `cmdStop`, `cmdStatus`, `readPIDFile`, `main`. Expected for daemon lifecycle commands.

**Other under-tested functions:**
| Function | File | Coverage |
|----------|------|----------|
| `pushData` | contract/script.go:114 | 27.3% |
| `pushData` | payment/funding.go:227 | 35.3% |
| `VerifyVoucher` | payment/voucher.go:17 | 0% |
| `SaveConfig` | config/config.go:173 | 57.1% |

---

## 4. Priority Recommendations

### P0 — Critical (Security & Correctness)

1. **`buyer.Buy()` + `buyer.resolveUTXOs()`** — The entire purchase flow has zero test coverage. Any regression here means users lose money. Needs mock-based unit tests.

2. **`network.VerifyTx()` (SPV)** at 44.7% — Security-critical SPV verification. The CODE_AUDIT found this as a vulnerability. Edge cases (invalid proofs, malicious headers) must be tested.

3. **`tx.BuildUnsignedCreateChildTx` + `BuildUnsignedSelfUpdateTx`** at 0% — Core transaction builders. Should have unit tests independent of e2e Docker suite.

4. **`engine.EncryptNode`** at 10.3% — Method 42 encryption integration. Data confidentiality depends on this.

### P1 — High Priority

5. **`engine.Sell`** at 5.4% — Pricing/payment setup flow.
6. **`engine.createSoftLink` / `createHardLink`** at 0% — Symlink/hardlink creation untested.
7. **`cmd/bmget`** at 5.8% — Newest tool shipped with virtually no tests.
8. **`payment.VerifyVoucher`** (metanet) at 0% — Payment verification.
9. **`contract.pushData`** (metanet) at 27.3% — Script building for storage contracts.

### P2 — Moderate Priority

10. **CLI argument parsing** — `cmd/bitfs/` at 39.1%. Consider extracting testable logic from `run*` functions, or use table-driven tests with mock I/O.
11. **`daemon_adapter.go`** — 7 functions at 0%. Bridge functions need integration tests with mock wallet/chain.
12. **`bcat`/`bget` paid content paths** — Payment output formatting at 0-37%.

### P3 — Low Priority (Expected / Acceptable)

- `main()` functions at 0% — Standard for Go CLI entry points
- Mock stub methods at 0% — By design
- `dashboard` package — React SPA embedded via `embed.go`, no Go logic to test
- `cmd_start/stop/status` (metanet) — Daemon lifecycle, tested via e2e

---

## 5. Structural Observations

### Strengths
- **libbitfs-go** core packages are exceptionally well-tested (paymail 96.5%, metanet 95.6%, config 94.5%)
- **Test:code ratio** of 1.83:1 overall indicates thorough testing culture
- **Integration test suite** (275 tests) covers cross-package interactions
- **Race detection** passes on all tests (`-race` flag)
- **metanet chain/mining** at 97%+ — consensus and mining code is rock-solid

### Weaknesses
- **Payment flows** are the weakest vertical: `buyer.Buy()`, `engine.Sell`, `engine.EncryptNode`, paid content paths in bcat/bget — all under 10%
- **CLI layer** acts as untested glue: business logic in engine is tested, but the CLI orchestration (arg parsing, error formatting, I/O) is not
- **bmget** was added recently with minimal test investment
- **SPV verification** (both libbitfs-go and bitfs client) has the lowest coverage of any security-critical path

### Coverage Blind Spots
- Unit tests alone don't cover: transaction signing (needs go-sdk), blockchain broadcast, UTXO state management
- Integration tests (`-tags=integration`) add ~15-20% effective coverage not visible in unit test numbers
- E2E tests (`-tags e2e`) cover the full signing + broadcast path but require Docker

---

## 6. Metrics Summary

```
libbitfs-go:  85.7%  ████████████████░░  (target: 90%)
bitfs:        58.6%  ███████████░░░░░░░  (target: 70%)
metanet:      79.9%  ███████████████░░░  (target: 85%)
```

**Recommended targets** for next milestone:
- libbitfs-go: 90% (close 4 tx/sign.go gaps + SPV client)
- bitfs: 70% (buyer.Buy, engine.Sell/Encrypt/Link, bmget basics)
- metanet: 85% (VerifyVoucher, pushData, SaveConfig)

# v0.0.1 Smoke Test Results — 2026-03-22

## Environment

- **Machine**: macOS Darwin 25.3.0
- **Network**: BSV mainnet (WoC+ARC backend)
- **Wallet**: mainnet wallet, ~69k sats initial balance
- **Vault**: mainnet-test (account 1)
- **Binary**: bitfs v0.0.1 (built from source)

## Test Results

### Automated Tests (Task 4)

| Suite | Result | Duration |
|-------|--------|----------|
| libbitfs-go (14 packages) | PASS | ~60s |
| bitfs unit (12 packages) | PASS | ~24s |
| bitfs integration | PASS | ~7s |
| bitfs E2E (Docker regtest) | PASS | ~299s |

### Mainnet Smoke Tests

| # | Test | Result | Details |
|---|------|--------|---------|
| 1 | Build + version | PASS | `bitfs version 0.0.1` |
| 2 | Upload file | PASS | TxID: `56f25d08...`, 57 bytes, free access |
| 3 | Read-back (cat) | PASS | Content matches exactly |
| 4 | Create directory | PASS | TxID: `35249210...`, /docs/ created |
| 5 | Nested file put + cat | PASS | /docs/nested.txt, 17 bytes |
| 6 | SPV verify (confirmed) | PASS | Verified at block 941186 |
| 7 | SPV verify (unconfirmed) | PASS | Correctly reports "Unconfirmed" |
| 8 | Daemon LFCP metadata | PASS | JSON metadata at / and /path endpoints |
| 9 | Daemon btree | PASS | Tree renders via bitfs:// URI |
| 10 | Daemon data endpoint | PASS | Returns encrypted ciphertext by key_hash |
| 11 | Sell (set price) | PASS | TxID: `802cae53...`, 100 sats/KB, on-chain verified |

### Bugs Found & Fixed

#### BUG-1: Daemon shutdown clobbers vault state (Critical)

**Symptom**: After `bitfs sell`, nodes.json still showed `access: "free"`.

**Root cause**: Daemon's `Close()` called `State.Save()` unconditionally on shutdown (SIGTERM), overwriting concurrent CLI state updates with stale in-memory data.

**Timeline**: Daemon loaded state → CLI sell updated nodes.json → daemon killed → daemon's Close() overwrote with stale state.

**Fix**: `vault.Close()` no longer calls `State.Save()`. All vault write operations persist state via `withWriteLock`. Close() only saves wallet state (fee derivation indexes).

**Commit**: `libbitfs-go a78d36c`

#### BUG-2: Publish doesn't persist binding (Medium)

**Symptom**: `bitfs unpublish` couldn't find binding set by `bitfs publish`.

**Root cause**: `publishDomain()` mutated `State.PublishBindings` but relied on `Close()` to persist — same root cause as BUG-1.

**Fix**: Added explicit `v.State.Save()` after `SetPublishBinding`, matching the existing `Unpublish` pattern.

**Commit**: `bitfs 2b76a7e`

### Not Tested (Covered by E2E)

- Paid download (buy) flow: Fully covered by E2E test suite
- Invoice persistence & recovery: Covered by unit tests
- Daemon restart + invoice recovery: Covered by unit tests

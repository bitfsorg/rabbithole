# AGENTS.md — BitFS E2E Testing Guide

This document guides AI coding agents through running BitFS end-to-end tests across all three networks.

## Prerequisites

| Requirement | Regtest | Testnet | Mainnet |
|-------------|---------|---------|---------|
| Go 1.25.6+  | Yes     | Yes     | Yes     |
| Docker Desktop (running) | Yes | Optional | No |
| BSV node access | Via Docker | Docker or external | External only |
| Funding source | Auto (coinbase) | Faucet or WIF | WIF (real BSV) |

Working directory for all commands: `bitfs/` (not the repo root).

## Quick Reference

```bash
cd bitfs

# Unit tests (no external deps, ~1s)
go test ./...

# Integration tests (no external deps, ~30s)
go test -tags integration ./integration/... -v -count=1

# E2E regtest (requires Docker, ~2-3min)
make e2e

# E2E testnet
BITFS_E2E_NETWORK=testnet BITFS_E2E_RPC_URL=http://localhost:18333 \
  go test -tags e2e ./e2e/... -v -timeout 60m

# E2E mainnet
BITFS_E2E_NETWORK=mainnet BITFS_E2E_FUND_WIF=L... BITFS_E2E_RPC_URL=<rpc-url> \
  go test -tags e2e ./e2e/... -v -timeout 120m
```

---

## 1. Regtest (Local, Default)

Regtest is a private blockchain running entirely in Docker. No real money, instant block mining, fully deterministic. This is the primary test environment.

### Step-by-step

```bash
cd bitfs

# 1. Start the regtest BSV node
cd e2e && docker compose up -d && cd ..

# 2. Verify the node is healthy (wait for healthcheck)
docker inspect --format='{{.State.Health.Status}}' bitfs-regtest
# Should output: healthy

# 3. Run all e2e tests
go test -tags e2e ./e2e/... -v -timeout 120s

# 4. (Optional) Stop the node when done
cd e2e && docker compose down && cd ..
# Add -v to also remove the blockchain data volume:
# cd e2e && docker compose down -v && cd ..
```

Or use the Makefile shortcut:

```bash
make e2e          # Steps 1+3 combined
make regtest      # Just start the node
make regtest-down # Just stop the node
```

### Configuration

The Docker container runs `bitcoinsv/bitcoin-sv:1.0.11` with:
- **RPC endpoint**: `http://localhost:18332`
- **RPC auth**: user=`bitfs`, password=`bitfs`
- **P2P port**: 18444
- **Config**: `e2e/bitcoin.conf` (mounted into container)
- **Genesis activation at block 1** (all opcodes available immediately)

No environment variables needed for regtest — all defaults work.

### How it works

1. Tests call `testutil.NewTestNode(t)` which creates a `RegtestNode`
2. `node.Fund()` mines 101 blocks (coinbase maturity) + sends coins + mines 1 confirmation
3. Tests build and sign transactions offline using `vault` + `libbitfs-go`
4. `node.SendRawTransaction()` broadcasts to the regtest node
5. `node.WaitForConfirmation()` mines additional blocks as needed

### Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Tests skip (not fail) | Node unreachable | `make regtest` and wait for healthy |
| `docker compose up` fails | Docker Desktop not running | Start Docker Desktop first |
| Port 18332 in use | Another service on that port | `lsof -i :18332` to find and stop it |
| "coinbase maturity" error | Not enough blocks mined | Should not happen — test helpers auto-mine 101 blocks |
| Stale blockchain state | Previous test residue | `cd e2e && docker compose down -v` to reset |

---

## 2. Testnet

Testnet uses the BSV test network with test coins (no real value). Slower than regtest because blocks are mined by the network (~10min average), but tests real network behavior.

### Option A: Local testnet node (Docker)

```bash
cd bitfs

# 1. Start testnet node (will sync the testnet blockchain — may take hours on first run)
make testnet

# 2. Wait for initial block download (IBD) to complete
# Check sync progress:
docker exec bitfs-testnet bitcoin-cli -testnet getblockchaininfo | grep -E 'blocks|headers'
# "blocks" should equal "headers" when fully synced

# 3. Run tests
BITFS_E2E_NETWORK=testnet \
BITFS_E2E_RPC_URL=http://localhost:18333 \
BITFS_E2E_FAUCET_URL=https://faucet.bitcoincloud.net \
  go test -tags e2e ./e2e/... -v -timeout 60m

# 4. Stop when done
make testnet-down
```

Docker config for testnet:
- **RPC endpoint**: `http://localhost:18333` (mapped from container's 18332)
- **RPC auth**: user=`bitfs`, password=`bitfs`
- **P2P port**: 18335 (mapped from container's 18333)
- **Config**: `e2e/bitcoin-testnet.conf`

### Option B: External testnet node

If you have access to an external BSV testnet node:

```bash
BITFS_E2E_NETWORK=testnet \
BITFS_E2E_RPC_URL=http://<host>:<port> \
BITFS_E2E_RPC_USER=<user> \
BITFS_E2E_RPC_PASS=<pass> \
BITFS_E2E_FAUCET_URL=https://faucet.bitcoincloud.net \
  go test -tags e2e ./e2e/... -v -timeout 60m
```

### Funding on testnet

Tests need BSV to pay for transactions. Two methods (tried in order):

1. **Faucet** (preferred): Set `BITFS_E2E_FAUCET_URL`. The test framework sends HTTP POST requests to get test coins.
2. **Pre-funded WIF**: Set `BITFS_E2E_FUND_WIF` to a WIF private key that holds testnet BSV. The framework imports the key and sends coins from it.

### Important notes

- First sync takes hours — the testnet blockchain is several GB
- Confirmation wait time defaults to **30 minutes** (vs 30s on regtest)
- Override with `BITFS_E2E_CONFIRM_TIMEOUT=45m` if needed
- Tests that call `MineBlocks()` will error on testnet (can't mine) — these tests skip gracefully

---

## 3. Mainnet

Mainnet tests run against the live BSV blockchain with **real money**. Use with extreme caution.

### Step-by-step

```bash
cd bitfs

BITFS_E2E_NETWORK=mainnet \
BITFS_E2E_RPC_URL=http://<your-mainnet-node>:<port> \
BITFS_E2E_RPC_USER=<user> \
BITFS_E2E_RPC_PASS=<pass> \
BITFS_E2E_FUND_WIF=<WIF-private-key-with-BSV> \
  go test -tags e2e ./e2e/... -v -timeout 120m
```

### Requirements

- **BSV mainnet node** with RPC access (no Docker compose provided — you must supply your own)
- **Funded wallet**: A WIF private key (`BITFS_E2E_FUND_WIF`) holding enough BSV to cover test transactions. Each test uses small amounts (~0.01 BSV per `Fund()` call) but 25 tests add up.
- **Patience**: Confirmation timeout defaults to **60 minutes**. Real blocks take ~10min on average.

### Cost estimation

Each e2e test funds a wallet with ~0.01 BSV and creates 1-5 transactions. Rough estimate for all 25 tests: **~0.5 BSV** total (transactions are small, mostly OP_RETURN data + miner fees).

### Safety

- **Do NOT run mainnet tests in CI/CD** — they spend real money
- Fund the WIF wallet with only what you're willing to spend
- Consider running a subset of tests: `go test -tags e2e ./e2e/... -run TestWalletFund -v`
- Monitor the wallet balance before and after

---

## Environment Variables Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `BITFS_E2E_NETWORK` | `regtest` | Target network: `regtest`, `testnet`, `mainnet` |
| `BITFS_E2E_RPC_URL` | Per network* | BSV node JSON-RPC endpoint |
| `BITFS_E2E_RPC_USER` | `bitfs` | RPC username |
| `BITFS_E2E_RPC_PASS` | `bitfs` | RPC password |
| `BITFS_E2E_FAUCET_URL` | — | Testnet faucet HTTP API URL |
| `BITFS_E2E_FUND_WIF` | — | WIF private key for funding (testnet/mainnet) |
| `BITFS_E2E_CONFIRM_TIMEOUT` | Per network** | Max wait time for tx confirmation |

\* RPC URL defaults: regtest=`http://localhost:18332`, testnet=`http://localhost:18333`, mainnet=none (must provide)

\** Timeout defaults: regtest=30s, testnet=30m, mainnet=60m

---

## Test Suite Overview

25 e2e test files covering the full BitFS stack:

| # | Test File | What it Tests | Needs Node |
|---|-----------|---------------|------------|
| 01 | `wallet_fund` | HD wallet creation, key derivation, funding | Yes |
| 02 | `metanet_root` | Root transaction creation on chain | Yes |
| 03 | `mkdir_upload` | Directory + file creation via Metanet DAG | Yes |
| 04 | `spv_verify` | SPV proof generation and verification | Yes |
| 05 | `free_content` | FREE access mode content retrieval | Yes |
| 06 | `paid_purchase` | HTLC-based paid file purchase | Yes |
| 07 | `full_lifecycle` | Complete create→upload→buy→verify flow | Yes |
| 08 | `move_rename` | File move/rename via SelfUpdate | Yes |
| 09 | `copy` | File copy operation | Yes |
| 10 | `remove` | File/directory removal | Yes |
| 11 | `link` | Soft and hard links | Yes |
| 12 | `encrypt_transition` | PRIVATE→FREE access mode transition | Yes |
| 13 | `sell_pricing` | Sell pricing and configuration | Yes |
| 14 | `vault_crud` | Vault create/read/update/delete | Yes |
| 15 | `multi_vault` | Multi-vault isolation | Yes |
| 16 | `fund_external` | External UTXO funding | Yes |
| 17 | `daemon_handshake` | LFCP + Method 42 ECDH handshake | — |
| 18 | `daemon_buy_api` | Daemon purchase API | — |
| 19 | `daemon_content_neg` | HTTP content negotiation | — |
| 20 | `daemon_paymail` | Paymail endpoint resolution | — |
| 21 | `daemon_spv_endpoint` | SPV proof endpoint | — |
| 22 | `client_roundtrip` | Full client→daemon roundtrip | — |
| 23 | `error_paths` | Error handling and edge cases | — |
| 24 | `large_file` | 1MB file upload stress test | Yes |
| 25 | `self_update_chain` | Version chain via self-updates | Yes |

Tests marked "—" under Needs Node test the daemon/client layer offline.

### Running a single test

```bash
go test -tags e2e ./e2e/... -run TestFullLifecycle -v -timeout 120s
```

### Running a subset

```bash
# Only daemon tests (no blockchain needed)
go test -tags e2e ./e2e/... -run "TestDaemon" -v

# Only DAG mutation tests
go test -tags e2e ./e2e/... -run "TestMove|TestCopy|TestRemove|TestLink" -v
```

---

## Build Tags

| Tag | Directory | External Deps | Run Command |
|-----|-----------|---------------|-------------|
| (none) | `./...` | None | `go test ./...` |
| `integration` | `./integration/...` | None | `go test -tags integration ./integration/... -v -count=1` |
| `e2e` | `./e2e/...` | BSV node | `go test -tags e2e ./e2e/... -v -timeout 120s` |

Always use `-count=1` to disable test caching when results depend on external state.

---

## Architecture Notes

Understanding these helps debug test failures:

- **TestNode interface** (`e2e/testutil/node.go`): Abstracts network differences. `RegtestNode` mines blocks; `LiveNode` (testnet/mainnet) polls for confirmations.
- **Funding** (`e2e/testutil/funder.go`): Regtest mines coinbase; testnet tries faucet then WIF; mainnet requires WIF.
- **RPC client** (`e2e/testutil/rpc.go`): Thin JSON-RPC 1.0 wrapper (go-sdk has no built-in RPC client).
- **Vault setup** (`e2e/testutil/engine_helpers.go`): `SetupTestEngine(t)` creates a complete Vault with HD wallet, ready for transaction building.
- **Transaction building**: All tests use `MutationBatch` (the sole tx build path). Old `BuildCreateRoot`/`BuildCreateChild` APIs are deleted.
- **TxID endianness**: RPC returns display-order (big-endian hex). Internal go-sdk uses `*chainhash.Hash`. Use `.String()` for display format.

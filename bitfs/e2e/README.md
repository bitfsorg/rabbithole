# BitFS End-to-End Tests

End-to-end tests that exercise the full BitFS stack against a real BSV node
running in regtest mode via Docker.

## Prerequisites

- **Docker Desktop** installed and running
- **Go 1.25.6+**

## Quick Start

```bash
# Start the BSV regtest node
cd e2e && docker compose up -d

# Wait for node to be ready (check health)
docker compose exec bsv-node bitcoin-cli -regtest -rpcuser=bitfs -rpcpassword=bitfs getblockchaininfo

# Run all e2e tests
cd .. && go test -tags e2e ./e2e/... -v -timeout 300s

# Run specific test
go test -tags e2e ./e2e/ -run TestWalletFund -v

# Stop the node
cd e2e && docker compose down

# Stop and clean data
cd e2e && docker compose down -v
```

## Test Suite

### Core Tests (01-07)

| File | Description | Requires |
|------|-------------|----------|
| `01_wallet_fund_test.go` | HD wallet key derivation + regtest funding | Regtest |
| `02_metanet_root_test.go` | Metanet root directory tx broadcast | Regtest |
| `03_mkdir_upload_test.go` | Directory + encrypted file upload chain | Regtest |
| `04_spv_verify_test.go` | SPV Merkle proof verification | Regtest |
| `05_free_content_test.go` | Free content via daemon HTTP | - |
| `06_paid_purchase_test.go` | x402 paid purchase with HTLC | Regtest |
| `07_full_lifecycle_test.go` | Complete lifecycle smoke test | Regtest |

### DAG Mutation Tests (08-11)

| File | Description | Requires |
|------|-------------|----------|
| `08_move_rename_test.go` | Move file between dirs + rename via SelfUpdate | Regtest |
| `09_copy_test.go` | Copy file (new P_node, same content) + independence | Regtest |
| `10_remove_test.go` | Remove file/dir from parent via SelfUpdate | Regtest |
| `11_link_test.go` | Hard links (shared P_node) + soft links (target ref) | Regtest |

### Access Control Tests (12-13)

| File | Description | Requires |
|------|-------------|----------|
| `12_encrypt_transition_test.go` | Free→Private transitions, access mode isolation | - |
| `13_sell_pricing_test.go` | Engine.Sell pricing + state updates | Regtest |

### Vault & Wallet Tests (14-16)

| File | Description | Requires |
|------|-------------|----------|
| `14_vault_crud_test.go` | Vault create/list/rename/delete | - |
| `15_multi_vault_test.go` | Vault key isolation + encryption isolation | - |
| `16_fund_external_test.go` | External UTXO registration + use | Regtest |

### Daemon HTTP API Tests (17-21)

| File | Description | Requires |
|------|-------------|----------|
| `17_daemon_handshake_test.go` | Method 42 ECDH handshake endpoint | - |
| `18_daemon_buy_api_test.go` | Buy API (GET info, POST HTLC, x402 headers) | - |
| `19_daemon_content_neg_test.go` | Content negotiation edge cases (deep path, 402, empty dir) | - |
| `20_daemon_paymail_test.go` | BSV Alias capabilities + PKI lookup | - |
| `21_daemon_spv_endpoint_test.go` | SPV proof endpoint (mock SPVService) | - |

### Client & Integration Tests (22-25)

| File | Description | Requires |
|------|-------------|----------|
| `22_client_roundtrip_test.go` | Client library → daemon → response chain | - |
| `23_error_paths_test.go` | Double-spend, malformed tx, insufficient funds | Regtest |
| `24_large_file_test.go` | 1MB encrypt/decrypt + daemon roundtrip | - |
| `25_self_update_chain_test.go` | Multiple sequential SelfUpdates, version chain | Regtest |

## Troubleshooting

**Node won't start**
- Check that Docker Desktop is running.
- Verify ports 18332 (RPC) and 18444 (P2P) are not in use by another process.

**Coinbase maturity**
- Regtest requires 100 confirmations before coinbase outputs are spendable.
  The test helpers mine 101 blocks to satisfy this requirement.

**Tests skip instead of running**
- If the regtest node is unreachable, tests gracefully skip with a descriptive
  message rather than failing. Start the node with `docker compose up -d` and
  re-run.

**RPC authentication errors**
- The default credentials are `bitfs`/`bitfs`, matching `bitcoin.conf` and
  `docker-compose.yml`. Do not change one without updating the other.

## Architecture

```
e2e/
├── docker-compose.yml       BSV SV Node 1.0.11 in regtest mode
├── bitcoin.conf             Node configuration (RPC credentials, regtest flags)
├── testutil/
│   ├── rpc.go               JSON-RPC 1.0 client (stdlib net/http, basic auth)
│   ├── node.go              RegtestNode helper (mine, fund, broadcast, import)
│   └── engine_helpers.go    Engine + wallet setup helpers for E2E tests
└── *_test.go                25 test files (57 test functions) gated by `e2e` tag
```

- **Docker** runs Bitcoin SV Node in regtest mode with `txindex=1` enabled.
- **JSON-RPC client** (`testutil/rpc.go`) provides a thin, typed wrapper over
  the node's JSON-RPC 1.0 interface using stdlib `net/http` with basic auth.
- **RegtestNode helper** (`testutil/node.go`) exposes high-level operations:
  address generation, block mining, UTXO listing, raw tx broadcast, Merkle
  proof retrieval, and convenience funding.
- **Build tag `e2e`** gates all test files so they are excluded from regular
  `go test ./...` runs. Pass `-tags e2e` explicitly to include them.
- Tests are **independent** but ordered by complexity -- each file builds on
  concepts from earlier files without requiring them to run first.

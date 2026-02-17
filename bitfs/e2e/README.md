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
cd .. && go test -tags e2e ./e2e/... -v -timeout 120s

# Run specific test
go test -tags e2e ./e2e/ -run TestWalletFund -v

# Stop the node
cd e2e && docker compose down

# Stop and clean data
cd e2e && docker compose down -v
```

## Test Suite

| File | Description |
|------|-------------|
| `01_wallet_fund_test.go` | HD wallet key derivation + regtest funding |
| `02_metanet_root_test.go` | Metanet root directory tx broadcast |
| `03_mkdir_upload_test.go` | Directory + encrypted file upload chain |
| `04_spv_verify_test.go` | SPV Merkle proof verification |
| `05_free_content_test.go` | Free content via daemon HTTP |
| `06_paid_purchase_test.go` | x402 paid purchase with HTLC |
| `07_full_lifecycle_test.go` | Complete lifecycle smoke test |

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
│   └── node.go              RegtestNode helper (mine, fund, broadcast, import)
└── *_test.go                Test files gated by build tag `e2e`
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

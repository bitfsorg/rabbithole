# BitFS E2E Regtest Verification Design

## Goal

Replace mock-only integration tests with end-to-end verification against a real BSV regtest node. Full lifecycle: wallet creation, funding, Metanet DAG construction, encrypted file upload, x402 paid purchase, SPV verification. Testnet layer follows after regtest is stable.

## Current State

- 1,249 tests across bitfs (355) and libbitfs (894), all passing
- All blockchain interaction is mocked: fake UTXOs, hardcoded headers, no signing/broadcast
- Clean interfaces (`HeaderStore`, `ContentStore`, `MetanetService`) ready for real implementations
- go-sdk v1.2.18 used only for EC primitives; RPC capabilities unexploited

## Architecture

### Infrastructure Layer

**Docker regtest node**: `bitfs/e2e/docker-compose.yml`
- BSV SV Node in regtest mode
- RPC port 18332, credentials: `bitfs`/`bitfs`
- Data volume for persistence across test runs (optional)

**RPC helper**: `bitfs/e2e/testutil/node.go`
- Uses go-sdk RPC client to connect to regtest node
- Functions: `MineBlocks(n)`, `FundWallet(addr, amount)`, `GetUTXOs(addr)`, `BroadcastTx(rawTx)`, `GetRawTx(txid)`, `GetBlockHeader(hash)`
- Connection check with graceful skip when node unavailable

**Build tag**: `//go:build e2e` on all e2e files. Run with `go test -tags e2e ./e2e/...`.

### E2E Test Suite (7 stages)

```
e2e/
├── docker-compose.yml
├── testutil/
│   └── node.go
├── 01_wallet_fund_test.go      # Create wallet -> mine blocks -> verify balance
├── 02_metanet_root_test.go     # Build root dir tx -> broadcast -> confirm -> parse
├── 03_mkdir_upload_test.go     # mkdir + put encrypted file -> broadcast -> DAG verify
├── 04_spv_verify_test.go       # Get Merkle proof from chain -> full SPV verification
├── 05_free_content_test.go     # Upload free file -> HTTP GET -> auto-decrypt
├── 06_paid_purchase_test.go    # x402 flow -> HTLC -> capsule reveal -> decrypt
├── 07_full_lifecycle_test.go   # Create -> upload -> purchase -> delete -> verify
└── README.md
```

**Test design**:
- `TestMain` checks regtest node reachability; skips if unavailable
- Shared funded wallet via `testutil` to avoid redundant mining
- Test 06 uses two wallets (seller + buyer) for realistic purchase flow

### New Production Code in libbitfs

#### `libbitfs/tx/sign.go` — Transaction Signing
```go
func SignInput(tx *transaction.Transaction, idx int, key *ec.PrivateKey, utxo UTXO) error
func SignAllInputs(tx *transaction.Transaction, keys []*ec.PrivateKey, utxos []UTXO) error
```

#### `libbitfs/tx/broadcast.go` — Broadcasting Interface
```go
type Broadcaster interface {
    Broadcast(ctx context.Context, rawTx []byte) (txID []byte, err error)
}
```
Regtest implementation in `e2e/testutil/`.

#### `libbitfs/spv/sync.go` — Header Sync Interface
```go
type HeaderSyncer interface {
    SyncHeaders(ctx context.Context, fromHeight uint32) ([]*BlockHeader, error)
    GetMerkleProof(ctx context.Context, txID []byte) (*MerkleProof, error)
}
```

#### `libbitfs/wallet/fund.go` — UTXO Provider Interface
```go
type UTXOProvider interface {
    ListUnspent(ctx context.Context, address string) ([]UTXO, error)
}
```

**Principle**: Every new capability is an interface. Regtest implementations live in `e2e/testutil/`. Production implementations added independently later.

### Testnet Layer (Phase 2, not in scope now)

- WhatsOnChain API as `Broadcaster` + `UTXOProvider` for testnet
- Faucet integration for test coin funding
- Same test suite, different `testutil` wiring

## Dependencies

- Existing: go-sdk v1.2.18 (RPC + signing), testify
- New: Docker (BSV SV Node image), no new Go dependencies expected

## Success Criteria

1. `docker compose up -d` starts regtest node
2. `go test -tags e2e ./e2e/...` runs full lifecycle against live node
3. Real transactions signed, broadcast, confirmed, and SPV-verified
4. Paid purchase flow completes with real HTLC on-chain
5. All 7 test stages pass with zero mocks for blockchain interaction

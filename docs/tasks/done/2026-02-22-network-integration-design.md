# Network Integration Design — libbitfs/network

**Date**: 2026-02-22
**Scope**: Blockchain network layer for UTXO queries, transaction broadcasting, and SPV verification
**Location**: `libbitfs/network/` (shared by BitFS and Metanet)

## Goals

1. Let BitFS operations (put/mkdir/rm/mv/cp) broadcast real transactions
2. UTXO discovery and tracking from live blockchain
3. SPV verification of transactions via block headers and Merkle proofs
4. Support regtest and testnet via JSON-RPC; mainnet adds WhatsOnChain later

## Non-Goals (This Phase)

- Mempool monitoring / webhook notifications
- Automatic UTXO consolidation
- P2P peer discovery (header sync uses RPC, not P2P)
- WhatsOnChain implementation (interface only)

## Architecture

```
libbitfs/
├── method42/    ← encryption
├── tx/          ← transaction building
├── spv/         ← Merkle/header verification (existing)
├── wallet/      ← HD wallet
└── network/     ← NEW: blockchain interaction layer
    ├── service.go       ← BlockchainService interface + data types
    ├── config.go        ← RPCConfig + presets + ResolveConfig
    ├── rpc.go           ← RPCClient (JSON-RPC 1.0 over HTTP)
    ├── rpc_test.go      ← unit tests (httptest mock)
    ├── spvclient.go     ← SPVClient (bridges network ↔ spv)
    ├── spvclient_test.go
    └── woc.go           ← WoCClient placeholder interface

Consumers:
  bitfs/internal/engine/   ← import libbitfs/network
  metanet/                 ← import libbitfs/network (future)
```

## Core Interface

```go
// BlockchainService is the primary interface for blockchain interaction.
type BlockchainService interface {
    // UTXO queries
    ListUnspent(ctx context.Context, address string) ([]*UTXO, error)
    GetUTXO(ctx context.Context, txid []byte, vout uint32) (*UTXO, error)

    // Transaction lifecycle
    BroadcastTx(ctx context.Context, rawTxHex string) (txid string, err error)
    GetRawTx(ctx context.Context, txid string) ([]byte, error)
    GetTxStatus(ctx context.Context, txid string) (*TxStatus, error)

    // SPV support
    GetBlockHeader(ctx context.Context, blockHash string) ([]byte, error)
    GetMerkleProof(ctx context.Context, txid string) (*MerkleProof, error)

    // Network info
    GetBestBlockHeight(ctx context.Context) (uint64, error)
}
```

### Data Types

```go
type UTXO struct {
    TxID         string
    Vout         uint32
    Amount       uint64   // satoshis
    ScriptPubKey []byte
    Address      string
    Confirmations int64
}

type TxStatus struct {
    Confirmed   bool
    BlockHash   string
    BlockHeight uint64
    TxIndex     int      // position in block (TTOR ordering)
}

type MerkleProof struct {
    TxID      string
    BlockHash string
    Branches  [][]byte  // sibling hashes
    Index     int       // leaf position
}
```

## RPC Backend

Promoted from `e2e/testutil/rpc.go` with enhancements:

- JSON-RPC 1.0 protocol (bitcoind standard)
- HTTP keep-alive connection pooling
- Exponential backoff retry (3 attempts, 1s/2s/4s)
- Context timeout propagation
- Structured error types (connection, auth, RPC error codes)

### RPC Methods Used

| Method | Purpose |
|--------|---------|
| `listunspent` | UTXO queries by address |
| `gettxout` | Single UTXO lookup |
| `sendrawtransaction` | Broadcast signed tx |
| `getrawtransaction` | Fetch raw tx bytes |
| `getblockheader` | 80-byte block header |
| `gettxoutproof` | Merkle proof for tx |
| `getblockcount` | Current chain height |

## SPV Client

Bridges the network layer with existing `libbitfs/spv/` verification:

```go
type SPVClient struct {
    chain   BlockchainService
    store   *spv.HeaderStore  // existing libbitfs/spv
}

func (c *SPVClient) VerifyTx(ctx context.Context, txid string) (*spv.VerifyResult, error)
func (c *SPVClient) SyncHeaders(ctx context.Context) error
```

`VerifyTx` performs the full 4-step SPV verification chain:
1. Fetch raw tx → verify transaction integrity
2. Fetch Merkle proof → verify inclusion in block
3. Fetch block header → verify header chain
4. Verify header is on longest chain

## Configuration

### Priority Chain (highest to lowest)

1. **CLI flags**: `--rpc-url`, `--rpc-user`, `--rpc-pass`
2. **Environment variables**: `BITFS_RPC_URL`, `BITFS_RPC_USER`, `BITFS_RPC_PASS`
3. **Config file**: `~/.bitfs/config` fields `rpc_url`, `rpc_user`, `rpc_pass`
4. **Network presets**: default values per network

### Network Presets

| Network | Default URL | Default Auth | Notes |
|---------|-------------|--------------|-------|
| regtest | `http://localhost:18332` | `bitfs:bitfs` | Matches e2e Docker setup |
| testnet | `http://localhost:18332` | `bitfs:bitfs` | Local testnet node |
| mainnet | (none — must configure) | (none) | Requires explicit config |

```go
func ResolveConfig(flags *RPCConfig, envPrefix string, configFile string, network string) (*RPCConfig, error)
```

## Engine Integration

```go
// bitfs/internal/engine/engine.go
type Engine struct {
    // ... existing fields ...
    Chain network.BlockchainService  // NEW
    SPV   *network.SPVClient         // NEW
}
```

### Operation Flow Change

**Before** (local only):
```
put → encrypt → build tx → sign → save to local state
```

**After** (real network):
```
put → encrypt → build tx → sign → Chain.BroadcastTx() → save to state
                                        ↓ (on error)
                                   rollback local state
```

### UTXO Refresh

Engine's `AllocateFeeUTXO` gains a network fallback:
1. Check local UTXO cache first
2. If empty/stale → `Chain.ListUnspent(feeAddress)` to refresh
3. Select UTXO with sufficient funds
4. Mark as spent locally (optimistic)

## Testing Strategy

| Layer | Method | Build Tag |
|-------|--------|-----------|
| Interface mock | `MockBlockchainService` for engine unit tests | (none) |
| RPC unit tests | `httptest.Server` simulating JSON-RPC responses | (none) |
| Regtest integration | Docker bitcoind, real RPC calls | `//go:build e2e` |
| Testnet smoke | Real testnet node (manual/CI optional) | `//go:build testnet` |

**TDD approach**: Write tests first for each component.

## Error Handling

```go
var (
    ErrConnectionFailed = errors.New("network: connection failed")
    ErrAuthFailed       = errors.New("network: authentication failed")
    ErrTxNotFound       = errors.New("network: transaction not found")
    ErrInsufficientFunds = errors.New("network: insufficient funds")
    ErrBroadcastFailed  = errors.New("network: broadcast rejected")
)
```

All errors wrap underlying cause with `fmt.Errorf("context: %w", err)`.

## Dependencies

- `net/http` (stdlib) — JSON-RPC transport
- `libbitfs/spv` — existing Merkle/header verification
- No new external dependencies

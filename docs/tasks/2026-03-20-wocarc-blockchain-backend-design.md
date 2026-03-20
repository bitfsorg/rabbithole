# WoC + ARC BlockchainService Backend Design

**Date**: 2026-03-20
**Status**: Approved

## Goal

Add a `WoCARCClient` implementation of `BlockchainService` in `libbitfs-go/network/` so that mainnet/testnet users can use BitFS without running their own BSV node. WoC handles queries, ARC handles broadcast.

## Backend Selection Logic

```
Has --rpc-url or BITFS_RPC_URL?
  → Yes: Use RPCClient (any network — mainnet, testnet, regtest)
  → No:
    → mainnet/testnet: Use WoCARCClient (new default)
    → regtest: offline mode (unchanged)
```

RPC is always highest priority. WoC+ARC is the fallback default for mainnet/testnet.

## Method → Backend Mapping

| BlockchainService Method | Backend | Endpoint |
|--------------------------|---------|----------|
| `ListUnspent` | WoC | `GET /address/{addr}/unspent/all` |
| `GetTxStatus` | WoC | `GET /tx/{txid}` |
| `GetRawTx` | WoC | `GET /tx/{txid}/hex` |
| `GetBlockHeader` | WoC | `GET /block/{hash}/header` |
| `GetBlockHash` | WoC | `GET /block/height/{height}` |
| `GetMerkleProof` | WoC | `GET /tx/{txid}/proof` |
| `BroadcastTx` | ARC | `POST /v1/tx` |

## New Files (libbitfs-go/network/)

| File | Purpose |
|------|---------|
| `woc.go` | WoC REST client (all query methods) |
| `arc.go` | ARC REST client (BroadcastTx only) |
| `wocarc.go` | `WoCARCClient` struct combining WoC+ARC, implements `BlockchainService` |
| `woc_test.go` | Unit tests with mock HTTP server |
| `arc_test.go` | Unit tests with mock HTTP server |
| `wocarc_test.go` | Integration-style tests for the composite client |

## Modified Files

| File | Change |
|------|--------|
| `bitfs/cmd/bitfs/rpc.go` | Rename to `chain.go`. Add WoCARCClient fallback when no RPC configured and network is mainnet/testnet. |

## Configuration

### Environment Variables (all optional)

| Variable | Purpose | Default |
|----------|---------|---------|
| `BITFS_WOC_API_KEY` | WoC API key (higher rate limits) | none (free tier) |
| `BITFS_ARC_API_KEY` | ARC API key | none |
| `BITFS_ARC_URL` | Custom ARC endpoint | `https://arc.taal.com/v1` |

### CLI Flags (optional)

| Flag | Purpose |
|------|---------|
| `--arc-url` | Override ARC endpoint |

### Default Endpoints

| Network | WoC Base URL | ARC Base URL |
|---------|-------------|-------------|
| mainnet | `https://api.whatsonchain.com/v1/bsv/main` | `https://arc.taal.com/v1` |
| testnet | `https://api.whatsonchain.com/v1/bsv/test` | `https://arc.taal.com/v1` |

## Error Handling

- HTTP 429 (rate limit): exponential backoff retry, max 3 attempts
- ARC broadcast failure: return error, no fallback to WoC
- WoC query failure: return error, no fallback to RPC

## Out of Scope

- Do NOT remove `RPCClient` (regtest still needs it, and users can choose RPC on any network)
- Do NOT change `BlockchainService` interface
- No multi-ARC endpoint failover (future work)
- No caching layer

## Reference

- Existing e2e WoC client: `bitfs/e2e/testutil/woc_client.go`
- Existing e2e ARC client: `bitfs/e2e/testutil/arc_client.go`
- BlockchainService interface: `libbitfs-go/network/service.go`
- Current RPC implementation: `libbitfs-go/network/rpc.go`

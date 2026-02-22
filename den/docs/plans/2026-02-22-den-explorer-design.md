# Den — BitFS Blockchain Explorer Design

Date: 2026-02-22

## Overview

**Den** (巢穴) is a web-based blockchain explorer for BSV regtest and testnet, with built-in BitFS/Metanet protocol verification capabilities. Primarily a development and debugging tool.

| Item | Value |
|------|-------|
| Name | Den |
| Location | `RabbitHole/den/` (standalone Go module) |
| Form | Web UI (Go backend + htmx) |
| Data source | Direct bitcoind JSON-RPC |
| Purpose | Development debugging (regtest + testnet) |
| Dependencies | `libbitfs` (tx, metanet, network, method42, spv) |

## Architecture

```
Browser <--htmx--> Go HTTP Server <--JSON-RPC--> bitcoind
                        |
                  libbitfs/tx        (tx parsing, OP_RETURN decode)
                  libbitfs/metanet   (DAG parsing, TLV decode)
                  libbitfs/network   (RPCClient)
                  libbitfs/method42  (ECDH encryption verification)
                  libbitfs/spv       (Merkle proof verification)
```

Single Go binary with embedded HTML templates (`embed` package). Start and use immediately.

## Features

### Layer 1: Standard Blockchain Explorer

- **Home page**: Latest blocks list + chain info (height, best block hash)
- **Block detail**: Header fields (version, prevHash, merkleRoot, timestamp, bits, nonce) + transaction list
- **Transaction detail**: Inputs/outputs with script disassembly, confirmation count, raw hex toggle
- **Address query**: Balance + UTXO list
- **Search**: Supports txid / block height / block hash / address

### Layer 2: BitFS Protocol Verification

#### OP_RETURN Decoding
- Detect MetaFlag (`0x6d657461`) in transactions
- Decode and display: P_node (compressed pubkey), ParentTxID, TLV payload fields
- Human-readable field names with hex/decoded dual view
- TLV tag reference (0x01-0x1A) with field descriptions

#### Metanet DAG Visualization
- Starting from a root node, recursively display parent-child tree
- Annotate each node: type (FILE/DIR/LINK), access level (Private/Free/Paid), file size, MIME type
- ChildEntry listing with index, name, pubkey, hardened flag
- Navigate between nodes by clicking

#### Method 42 Encryption Verification
- Given a Metanet transaction, extract P_node and verify ECDH key derivation chain
- Check KeyHash (SHA256(SHA256(plaintext))) presence and format
- Display encryption parameters: access level, key derivation inputs
- Note: cannot verify actual decryption without private key, but can verify structure

#### SPV Proof Verification
- Fetch Merkle proof for any confirmed transaction
- Visual display of proof path (leaf → root)
- Verify proof against block header's merkleRoot
- Directory MerkleRoot (TLV tag 0x1A): verify Merkle tree over ChildEntry list, display membership proof

## Technology Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | Go | Consistent with project ecosystem |
| HTTP | `net/http` stdlib | No framework needed for this scope |
| Templates | `html/template` + `embed` | Server-rendered, zero build step |
| Dynamic UI | htmx | AJAX via HTML attributes, no JS framework |
| Styling | Minimal CSS (inline or single file) | Dev tool, function over form |
| RPC client | `libbitfs/network.RPCClient` | Already implemented and tested |
| TX parsing | `libbitfs/tx` | Metanet transaction templates |
| DAG parsing | `libbitfs/metanet` | TLV decode, Node/ChildEntry types |

## Configuration

Command-line flags for the single binary:

```
den [flags]
  -rpc-url     string   bitcoind RPC URL (default "http://localhost:18332")
  -rpc-user    string   RPC username (default "bitfs")
  -rpc-pass    string   RPC password (default "bitfs")
  -addr        string   HTTP listen address (default ":8080")
  -network     string   Network: regtest|testnet (default "regtest")
```

## Page Routes

| Route | Description |
|-------|-------------|
| `GET /` | Home: latest blocks, chain info, search |
| `GET /block/:hash` | Block detail |
| `GET /block/height/:n` | Block by height (redirects to hash) |
| `GET /tx/:txid` | Transaction detail + BitFS decode |
| `GET /address/:addr` | Address UTXO list |
| `GET /metanet/:txid` | Metanet node detail + DAG tree |
| `GET /spv/:txid` | SPV proof visualization |
| `GET /method42/:txid` | Method 42 encryption analysis |

htmx partial endpoints (return HTML fragments):
| Route | Description |
|-------|-------------|
| `GET /api/search?q=...` | Search dispatch (returns redirect or inline result) |
| `GET /api/blocks?page=...` | Paginated block list fragment |
| `GET /api/dag/:txid/children` | Lazy-load DAG children for tree expansion |

## Non-Goals (YAGNI)

- No database / indexing — all queries go direct to RPC
- No mainnet support (dev tool only)
- No user authentication
- No transaction broadcasting / wallet features
- No real-time WebSocket updates
- No mobile-responsive design

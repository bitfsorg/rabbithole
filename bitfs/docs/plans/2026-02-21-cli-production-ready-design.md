# BitFS CLI Production-Ready Design

Date: 2026-02-21

## Problem

The BitFS CLI write path (put/mkdir/rm/mv/link/sell/encrypt) is fully functional, but the read path (b-tools), payment flow (x402), and several commands remain stubs or incomplete. This document catalogs all gaps and defines a phased plan to reach production readiness.

## Current State

### What Works End-to-End
- **Write path**: CLI → Engine → libbitfs tx building → go-sdk signing → hex output
- **Wallet/Vault**: init, show, create, list, rename, delete
- **Daemon**: health check, Method 42 handshake, raw ciphertext retrieval (`/_bitfs/data/{hash}`)
- **All libbitfs packages**: method42, wallet, tx, metanet, spv, storage, paymail, x402, config — all implemented with passing tests

### What's Broken or Missing

#### Critical Gaps
1. **b-tools are pure stubs** — bls, bcat, bget, bstat, btree all print "Would ..." messages. No `internal/client/` library exists.
2. **`handleMeta` is a stub** — echoes parameters, doesn't query Metanet DAG.
3. **x402 payment flow not wired** — x402 package is implemented in libbitfs but daemon doesn't use it. No invoice generation, no HTLC submission endpoints, no capsule reveal.
4. **`ripemd160Hash` incorrect** — `engine/helpers.go` uses SHA256[:20] instead of RIPEMD160. Affects change address derivation.
5. **Cross-directory `mv` unsupported** — returns explicit error.

#### Missing Commands
6. **`cp` command** — mentioned in spec (10-cmd-bitfs.md line 22) but not implemented.
7. **Shell `lcd`** — spec requires local directory navigation (10-cmd-bitfs.md line 188).
8. **`unpublish`** — spec'd (10-cmd-bitfs.md line 32) but not implemented.

#### Incomplete Daemon Endpoints
9. **`POST /_bitfs/pay/{invoice_id}`** — not implemented.
10. **`GET/POST /_bitfs/buy/{txid}`** — not implemented.
11. **`/.well-known/bsvalias`** — returns placeholder templates, not connected to real PKI.
12. **`/api/v1/pki/{alias}@{domain}`** — not implemented.
13. **Content negotiation** (`GET /`, `GET /{path}`) — not rendering real content.

#### Minor Issues
14. **`pathToIndices()` dead code** in cmd_wallet.go.
15. **`Publish()` informational only** — prints DNS instructions, no verification.

## Design

### Phase 0: Correctness Fixes

**Scope**: Fix bugs that affect correctness of existing working features.

- **Fix `ripemd160Hash`**: Use `crypto/sha256` + go-sdk's RIPEMD160 (or `golang.org/x/crypto/ripemd160`) to implement real HASH160. The function is used by `pubKeyHash()` → `DeriveChangeAddr()`, so change outputs in all transactions currently have wrong script hashes.
- **Remove `pathToIndices`**: Dead code in cmd_wallet.go, never called.

### Phase 1: Client Library + Daemon Completion

**Scope**: Build the infrastructure layer that b-tools and payment flow depend on.

#### `internal/client/` — New Package

HTTP client for daemon API, shared by all b-tools:

```go
type Client struct {
    BaseURL    string
    HTTPClient *http.Client
    CacheDir   string // ~/.bitfs/cache/meta/ (optional)
    Timeout    time.Duration
}

// Metadata
func (c *Client) GetMeta(pnode, path string) (*MetaResponse, error)
func (c *Client) ListDir(pnode, path string) ([]DirEntry, error)

// Data
func (c *Client) GetData(hash string) (io.ReadCloser, error)
func (c *Client) GetDataFree(hash string, nodePrivKey *ec.PrivateKey) ([]byte, error)

// Payment
func (c *Client) Handshake(buyerPubKey *ec.PublicKey) (*Session, error)
func (c *Client) GetBuyInfo(txid string) (*BuyInfo, error)
func (c *Client) SubmitHTLC(txid string, htlcRawTx []byte) (*CapsuleResponse, error)
```

Shared CLI flags extracted as helpers:
- `--host` (daemon address, default from config)
- `--json` (JSON output)
- `--no-cache` (skip local cache)
- `--timeout` (request timeout)
- `--offline` (local cache only)

#### Daemon Completion

- **`handleMeta`**: Query `MetanetAdapter.GetNodeByPath()` → return full node metadata (type, hash, size, owner P_node, txid, block height, access mode, price, children list for directories).
- **Content negotiation** (`GET /{path}`): Resolve path via Metanet DAG. For directories: list children. For files: stream content (free) or 402 (paid). Negotiate HTML/Markdown/JSON via Accept header.
- **`/.well-known/bsvalias`**: Return real capability URLs with daemon's actual host.
- **`/api/v1/pki/{alias}@{domain}`**: Look up vault by alias, return public key.

### Phase 2: B-Tools Read Path

All five tools follow the same pattern: parse URI → resolve via client → format output.

| Tool | Core Logic |
|------|-----------|
| `bls` | `client.ListDir()` → tabular output (`-l`: size/date/access) or `--json` |
| `bcat` | `client.GetDataFree()` for free content → stdout. Paid: show price, suggest `--buy` |
| `bget` | Same as bcat + write to file (`-o`). Support `--version N` for historical versions |
| `bstat` | `client.GetMeta()` → display type/hash/size/owner/txid/time/access. `--versions` lists all |
| `btree` | Recursive `client.ListDir()` → tree visualization. `-d N` limits depth |

Exit codes per spec: 0=success, 1=general error, 2=not found, 3=permission denied, 4=network error, 5=payment required, 6=invalid argument, 7=timeout.

### Phase 3: x402 Payment Flow

Wire the end-to-end paid content sequence:

1. **Daemon: invoice generation** — When paid content is requested, daemon creates `x402.Invoice` with capsule_hash derived from the node's encryption key, sets price from node metadata, returns HTTP 402 with standard headers.

2. **Daemon: `POST /_bitfs/pay/{invoice_id}`** — Accept raw BSV payment tx, verify via `x402.VerifyPayment()`, mark invoice as paid.

3. **Daemon: `GET /_bitfs/buy/{txid}`** — Return `{capsule_hash, price, payment_addr}` for a content node.

4. **Daemon: `POST /_bitfs/buy/{txid}`** — Accept HTLC raw tx, verify it's on-chain (or in mempool with monitoring), reveal capsule (preimage). Seller claims HTLC using the IF branch.

5. **Client: `--buy` flag** — In bcat/bget, when `--buy` is set:
   - Load wallet (prompt for password)
   - Call `client.GetBuyInfo(txid)`
   - Build HTLC via `x402.BuildHTLC()`
   - Sign and broadcast HTLC tx
   - Call `client.SubmitHTLC()` to get capsule
   - Decrypt via `method42.DecryptWithCapsule()`

### Phase 4: Missing Commands

- **`bitfs cp <src> <dst>`**: Read source node metadata + content → create new independent Metanet node at destination (new key pair, re-encrypt content, new CreateChild tx). Not a hard link — full deep copy.

- **Cross-directory `mv`**: Two-phase update:
  1. SelfUpdate on source parent: RemoveChild
  2. SelfUpdate on dest parent: AddChild (same P_node, new name)
  Both updates must reference correct parent UTXOs. Atomic only at application level (two separate txs).

- **Shell `lcd <path>`**: Track local working directory separately from remote cwd. Used by `put` (upload from local) and `bget` (download to local) within shell.

- **`bitfs unpublish <domain>`**: Remove local binding record. Print instructions to remove DNS TXT record.

### Phase 5: Paymail/Publish Real Binding

- **`bitfs publish <domain> [path]`**: Derive vault root pubkey → output DNS TXT record instructions → verify bidirectional match (DNS TXT → P_node AND node domain field → DNS domain) → store binding in local config.
- **`bitfs publish`** (no args): List all active bindings with verification status.
- **DNSLink resolution**: `_bitfs_pubkey.{domain}` TXT record + `_bitfs._tcp.{domain}` SRV record for endpoint discovery.
- **Paymail server in daemon**: Real `.well-known/bsvalias` capabilities + `/api/v1/pki/` endpoint backed by wallet data.

### Phase 6: Security Hardening

- Input validation audit on all external boundaries (URIs, HTTP parameters, transaction data, DNS responses)
- Error message sanitization (no internal state leakage)
- TLS support for daemon (cert/key config)
- HTLC security verification (timeout bounds, amount validation, on-chain confirmation before capsule reveal per DD#86)
- Rate limiting configuration (configurable RPM/burst per spec)
- Wallet password handling (no echo, memory zeroing)

### Phase 7: Documentation

- Daemon API reference (Markdown, one section per endpoint)
- b-tools usage guide with examples
- End-to-end tutorial: wallet init → put → bcat → sell → buy
- Configuration reference (all TOML fields documented)

## Dependency Graph

```
Phase 0 ──────────────────────────────────────────→ (independent)
Phase 1 → Phase 2 → Phase 3
Phase 4 ──────────────────────────────────────────→ (independent, parallel with 2/3)
Phase 5 ← Phase 1 daemon completion
Phase 6 ← Phase 1-5 all complete
Phase 7 ← Phase 6
```

## Estimated Scope

| Phase | New/Modified Files | Rough Size |
|-------|--------------------|------------|
| 0 | 2 files | ~20 lines changed |
| 1 | ~8 files (new client pkg + daemon fixes) | ~800 lines |
| 2 | 5 files (b-tool rewrites) | ~600 lines |
| 3 | ~6 files (daemon endpoints + client buy) | ~700 lines |
| 4 | 4 files (new commands + engine methods) | ~400 lines |
| 5 | ~4 files (publish + paymail daemon) | ~300 lines |
| 6 | Audit across all packages | ~200 lines changed |
| 7 | ~5 doc files | ~2000 words |

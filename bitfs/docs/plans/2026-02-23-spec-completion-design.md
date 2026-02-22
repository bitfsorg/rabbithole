# Spec Completion Design

## Problem

A comprehensive audit of all 12 spec files against the codebase revealed 40 gaps:
- 14 completely missing features/commands/functions
- 6 interface/naming mismatches
- 19 missing CLI flags
- 1 empty module (revshare)

Plus `lock/unlock` session management defined in design doc T23 (24 test cases) but absent from both spec and code.

## Scope

Implement ALL gaps to bring code into full spec compliance, organized in 6 dependency-ordered tiers.

## Tier 0: Library Foundations

Things other features depend on.

### 0.1 tx interface alignment

Rename 3 functions in `libbitfs/tx/` to match spec signatures:
- `BuildOPReturnData(...)` → `BuildOPReturn(pNode, parentTxID, payload) (*script.Script, error)`
- `ParseOPReturnData(...)` → `ParseOPReturn(s *script.Script) (*ec.PublicKey, []byte, []byte, error)`
- `BuildP2PKHScript(...)` → `P2PKHScript(pubKey) (*script.Script, error)`

Update all callers in libbitfs and bitfs.

### 0.2 OnChainRef type

Add to `libbitfs/storage/`:

```go
type OnChainRef struct {
    KeyHash     []byte
    ContentTxIDs [][]byte
    TotalChunks uint32
}
```

### 0.3 DeriveKeyCacheKey

Add to `libbitfs/wallet/`:

```go
func (w *Wallet) DeriveKeyCacheKey() (*KeyPair, error)
```

Derives a deterministic key for encrypting cached AES keys in `~/.bitfs/cache/keys/`. Uses a fixed BIP44 path like `m/44'/236'/0'/2/0` (cache key chain).

### 0.4 revshare skeleton

Create minimal types in `libbitfs/revshare/`:

```go
type Split struct {
    Address string
    Share   uint32 // basis points (1/10000)
}

type Plan struct {
    Splits []Split
}

func Validate(p *Plan) error
func CalculateOutputs(p *Plan, totalSats uint64) []Output
```

## Tier 1: Lock/Unlock Session Management

Based on design doc T23 (24 test cases).

### Session file format

```json
{
    "created_at": 1708700000,
    "expires_at": 1708701800,
    "encryption_key": "<hex-encoded derived key>",
    "vault": "default"
}
```

Stored at `~/.bitfs/session.json`, permissions 0600.

### Secure delete

3-pass zero-fill + fsync + delete. Idempotent (no error if file absent).

### Commands

- `bitfs unlock [--duration 30m]` — decrypt wallet with password, write session file
- `bitfs lock` — secure-delete session file, notify daemon if running

### Key resolution priority

All commands needing private keys follow: daemon RPC > session file > password prompt.

## Tier 2: Missing CLI Commands

### 2.1 `bitfs init`
Top-level init = `wallet init` + `vault create "default"`. Convenience wrapper.

### 2.2 `bitfs vault use <name>`
Write vault name to `~/.bitfs/active_vault`. Read by all commands as default vault.

### 2.3 `bitfs vault info [name]`
Display: name, account index, root txid, published status, child count, key derivation path.

### 2.4 `bitfs decrypt <path>`
Reverse of `encrypt`: re-encrypt content with AccessFree, update node metadata. Needs SelfUpdate tx.

### 2.5 `bitfs rmdir <path>`
Like `rm` but error if directory has children. No `--force` flag.

### 2.6 `bitfs sales [path]`
Query sales records from engine state. Show: buyer, amount, timestamp, file path.

### 2.7 `bitfs wallet restore`
Accept mnemonic words via stdin (secure), reconstruct wallet.enc with new password.

### 2.8 `bitfs wallet info` (rename)
Rename `show` → `info`. Keep `show` as silent alias for backwards compat.

### 2.9 `bitfs daemon status`
Check PID file existence, ping health endpoint, report uptime.

### 2.10 `bitfs daemon config`
Read and print `~/.bitfs/config.toml`.

### 2.11 `bitfs sell --recursive`
Walk directory tree, apply pricing to all file nodes. Skip directories.

## Tier 3: Global CLI Flags

### 3.1 `--json`
All commands output JSON when this flag is set. Each command's Result struct gets a `ToJSON()` method.

### 3.2 `--vault <name>`
Override active vault for single command invocation.

### 3.3 `--home <path>`
Override `BITFS_HOME` (default `~/.bitfs`). Replaces per-command `--datadir`.

### 3.4 `--no-cache`, `--offline`, `--timeout`
- `--no-cache`: bypass local content cache
- `--offline`: error on any network call
- `--timeout N`: request timeout in seconds (default 30)

## Tier 4: B-tools Flags + Paymail Resolution

### Per-tool flags
- bls: `--keyword <kw>` (filter), `--no-cache`, `--offline`
- bcat: `--json`, `--no-cache`
- bget: real `--version N` (query version history), `--json`, `--no-cache`
- bstat: real `--versions` (list all versions), `--no-cache`
- btree: `--no-cache`

### Paymail/DNSLink resolution
Wire `paymail.ResolveURI()` into all b-tools URI parsing. Replace "not yet supported" messages with actual resolution.

## Tier 5: Daemon Endpoints

### 5.1 `POST /_bitfs/pay/{invoice_id}`
Accept x402 payment submission. Verify BSV tx, release content capsule.

### 5.2 `POST /_bitfs/git/push`
Git remote helper push. Parse packfile, create Metanet transactions for each git object.

### 5.3 `GET /_bitfs/git/refs/{path}`
Serve git references from Metanet DAG. Maps git refs to Metanet node paths.

## Dependency Graph

```
Tier 0 (foundations)
  ├── 0.3 DeriveKeyCacheKey ──→ Tier 1 (lock/unlock)
  └── 0.1-0.4 ──→ Tier 2-5 (independent)

Tier 1 (lock/unlock) ──→ Tier 2 (CLI commands use key resolution chain)

Tier 2 (CLI commands) ──→ Tier 3 (global flags wrap all commands)

Tier 3 (global flags) ──→ Tier 4 (b-tools inherit flag patterns)

Tier 5 (daemon) ── independent, can parallelize
```

## Estimated Scope

~30 tasks, ~3000-5000 LOC across libbitfs + bitfs. Largest items: lock/unlock (~500 LOC), git endpoints (~800 LOC), paymail resolution wiring (~300 LOC).

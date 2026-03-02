# B* Tools + Shell Full Spec Compliance — Design

Date: 2026-02-27
Branch: `feat/btools-shell-compliance` (bitfs repo)
Scope: bitfs CLI, shell, daemon, client — full spec compliance for b* tools and shell

## Background

Audit of b* tools (bls, bcat, bget, bstat, btree, bmget) and bitfs shell found:
- All tools and shell commands are implemented and functional
- ~70% spec compliance — missing caching, version history, and several shell features
- 1 critical bug (bmget unbounded read)

## Three Phases

### Phase 1: Critical Fixes + Shell Gaps (7 tasks)

Smallest scope, highest immediate value. No architectural changes.

#### 1.1 bmget unbounded read fix
- **File**: `cmd/bmget/main.go`
- **Change**: Add `io.LimitReader(reader, maxContentSize)` matching bcat's existing pattern
- **Risk**: P0 — potential OOM on malicious/huge files

#### 1.2 Engine: Decrypt method
- **File**: `internal/engine/decrypt.go` (new)
- **Operation**: Inverse of Encrypt — converts PRIVATE → FREE
  - Read current ciphertext from storage
  - Decrypt with owner's private key (PRIVATE mode)
  - Re-encrypt with scalar-1 key (FREE mode)
  - Create SelfUpdate tx via MutationBatch with Access=FREE
- **Opts/Result**: `DecryptOpts{Path string}`, reuse `Result`

#### 1.3 Shell: decrypt command
- **File**: `cmd/bitfs/cmd_shell.go`
- **Change**: Add `case "decrypt"` → `eng.Decrypt(ctx, DecryptOpts{Path: path})`
- **Help**: Update shellHelp with `decrypt <path>  Decrypt (PRIVATE -> FREE)`
- **Completer**: Add "decrypt" to shellCommands list

#### 1.4 Shell: put with access mode
- **File**: `cmd/bitfs/cmd_shell.go`
- **Change**: Parse optional 3rd argument: `put <local> <remote> [free|private]`
- **Default**: "free" (unchanged behavior)
- **If "private"**: Set `PutOpts.Access = "private"` (engine handles encryption)

#### 1.5 Shell: rm -r (recursive)
- **File**: `cmd/bitfs/cmd_shell.go`
- **Change**: Check for `-r` or `--recursive` flag in args
- **Pass**: `RemoveOpts{Recursive: true}` to engine
- **Guard**: Confirm before recursive delete on non-empty directories

#### 1.6 Shell: sell --recursive
- **File**: `cmd/bitfs/cmd_shell.go`
- **Change**: Check for `--recursive`/`-r` flag
- **Behavior**: Apply sell price to target + all descendants
- **Implementation**: Walk directory tree, call eng.Sell for each file

#### 1.7 Shell: link flag fix
- **File**: `cmd/bitfs/cmd_shell.go`
- **Change**: Scan all args for `-s` or `--soft`, remove from positional args
- **Result**: `link <target> <path> [-s|--soft]` works regardless of flag position

### Phase 2: Metadata Cache + Tool Flags (7 tasks)

Infrastructure work — new cache layer, then wire into all tools.

#### 2.1 MetaCache layer
- **File**: `internal/client/cache.go` (new)
- **Structure**:
  ```
  ~/.bitfs/cache/meta/{hex_prefix}/{hex_hash}.json
  ```
  - Key: SHA256(`{pnode}/{path}`) → hex hash
  - Value: JSON-serialized MetaResponse + `cached_at` timestamp
  - TTL: 5 minutes default
- **API**: `MetaCache.Get(pnode, path) (*MetaResponse, error)`, `MetaCache.Put(pnode, path, resp)`, `MetaCache.Invalidate(pnode, path)`

#### 2.2 CachedClient wrapper
- **File**: `internal/client/cached.go` (new)
- **Wraps**: Existing `Client` with cache-aware `GetMeta()`
- **Options**: `NoCache bool`, `Offline bool`
- **Logic**:
  - Normal: check cache → if miss/expired, fetch → populate cache
  - NoCache: skip cache read, fetch, populate cache
  - Offline: check cache → if miss, return ErrOfflineCacheMiss

#### 2.3 --no-cache flag (all 6 tools)
- **Files**: `cmd/{bls,bcat,bget,bstat,btree,bmget}/main.go`
- **Flag**: `--no-cache` boolean
- **Behavior**: Pass `NoCache: true` to CachedClient

#### 2.4 --offline flag (all 6 tools)
- **Files**: `cmd/{bls,bcat,bget,bstat,btree,bmget}/main.go`
- **Flag**: `--offline` boolean
- **Behavior**: Pass `Offline: true` to CachedClient
- **Exit code**: 4 (network error) on cache miss in offline mode

#### 2.5 bls --keyword filter
- **File**: `cmd/bls/main.go`
- **Flag**: `--keyword <string>`
- **Behavior**: Filter `meta.Children` by case-insensitive substring match on name

#### 2.6 Shared error helpers
- **File**: `internal/buyer/errors.go` (new or extend existing)
- **Extract**: `ErrorToExitCode()`, `ErrorMessage()` from all 6 tools
- **Dedup**: Single implementation, all tools import from buyer package

#### 2.7 bstat timestamp
- **Files**: `internal/daemon/handler.go` (MetaResponse), `internal/client/client.go`, `cmd/bstat/main.go`
- **Change**: Add `CreatedAt time.Time` to MetaResponse
- **Source**: Node's block timestamp if available from SPV/blockchain, otherwise omit
- **Display**: `Time: 2026-02-14 10:30:00 UTC` in human output

### Phase 3: Daemon Enhancements (7 tasks)

New daemon endpoints for version history and sales.

#### 3.1 Version history endpoint
- **File**: `internal/daemon/routes.go`, `internal/daemon/handler.go`
- **Route**: `GET /_bitfs/versions/{pnode}/{path...}`
- **Response**: JSON array of `VersionEntry`:
  ```json
  [
    {"version": 1, "txid": "abc...", "block_height": 12345, "timestamp": "...", "size": 4096, "access": "free"},
    {"version": 2, "txid": "def...", "block_height": 12300, "timestamp": "...", "size": 4096, "access": "private"}
  ]
  ```
- **Backend**: `NodeStore.GetNodeVersions()` ordered newest-first
- **Numbering**: version=1 is latest, version=2 is previous, etc.

#### 3.2 Client: GetVersions
- **File**: `internal/client/client.go`
- **Method**: `GetVersions(pnode, path string) ([]VersionEntry, error)`
- **Type**: `VersionEntry{Version int, TxID string, BlockHeight uint32, Timestamp time.Time, Size uint64, Access string}`

#### 3.3 bstat --versions
- **File**: `cmd/bstat/main.go`
- **Change**: Replace stub with `client.GetVersions()` call
- **Output**: Table of versions with txid, height, timestamp, access
- **JSON**: Array of VersionEntry objects

#### 3.4 bget --version N
- **File**: `cmd/bget/main.go`
- **Change**: Replace stub. Call `GetVersions()`, find version N, fetch that version's content by txid
- **Default**: N=1 (latest, same as current behavior)

#### 3.5 Sales endpoint
- **File**: `internal/daemon/routes.go`, `internal/daemon/handler.go`
- **Route**: `GET /_bitfs/sales`
- **Query params**: `?status=paid|all`, `?limit=50`, `?offset=0`
- **Response**: JSON array of `SaleRecord`:
  ```json
  [
    {"invoice_id": "...", "price": 5000, "key_hash": "...", "timestamp": "...", "paid": true}
  ]
  ```
- **Backend**: Daemon's `invoices` map, filtered by status

#### 3.6 Client: GetSales
- **File**: `internal/client/client.go`
- **Method**: `GetSales(status string, limit int) ([]SaleRecord, error)`

#### 3.7 Shell: sales command
- **File**: `cmd/bitfs/cmd_shell.go`
- **Syntax**: `sales [path]` — if path given, filter by key_hash prefix
- **Connection**: Uses `client.New("http://localhost:{daemon_port}")` to query running daemon
- **Error**: If daemon not running: `"daemon not running — start with 'bitfs daemon'"`
- **Help**: Add to shellHelp

## Test Strategy

Each phase includes unit tests for new code:
- Phase 1: Engine decrypt test, shell command parsing tests
- Phase 2: MetaCache get/put/invalidate/expiry tests, CachedClient tests, keyword filter test
- Phase 3: Version endpoint test, sales endpoint test, integration with client

All existing tests must continue to pass. Run: `go test ./... -count=1 -race`

## Estimated Scope

| Phase | New Files | Modified Files | Lines (est.) |
|-------|-----------|----------------|-------------|
| 1 | 1 (decrypt.go) | 2 (cmd_shell.go, bmget) | ~200 |
| 2 | 3 (cache.go, cached.go, errors.go) | 7 (6 tools + bstat) | ~600 |
| 3 | 0 | 4 (routes, handler, client, cmd_shell) | ~400 |
| **Total** | **4** | **13** | **~1,200** |

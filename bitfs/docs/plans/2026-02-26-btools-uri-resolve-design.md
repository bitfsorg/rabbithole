# b* Tools URI Endpoint Resolution Design

**Date**: 2026-02-26
**Status**: Approved
**Priority**: P2
**Scope**: 5 b* tools (bls/bcat/bget/bstat/btree) URI → remote daemon endpoint resolution.

## Problem

b* tools are visitor/viewer tools for accessing **remote** filesystems. Currently they:
- Only support bare pubkey URIs (`bitfs://02abc.../path`)
- Hardcode `--host` default to `http://localhost:8080`
- Print "paymail/dnslink resolution not yet supported" for `alice@domain` or `domain` URIs

The spec (11-cmd-btools.md line 116) already requires `paymail.ResolveURI()` for endpoint discovery — the code just doesn't implement it.

## Decision

Wire up the existing `paymail.ResolveURI()` in b* tools via a shared resolve helper in `bitfs/internal/client/`.

## Architecture

### Shared Resolve Function

New file: `bitfs/internal/client/resolve.go`

```go
// ResolveResult holds the resolved connection parameters from a bitfs:// URI.
type ResolveResult struct {
    Client *Client  // HTTP client connected to the resolved endpoint
    PNode  string   // Hex-encoded 33-byte compressed pubkey
    Path   string   // Path component from URI (with leading /)
}

// ResolveURI resolves a bitfs:// URI to a connected client, pnode, and path.
// If hostOverride is non-empty, it is used as the daemon URL (skipping endpoint resolution).
// For AddressPubKey URIs without hostOverride, returns an error (no domain to resolve from).
func ResolveURI(uri, hostOverride string) (*ResolveResult, error)
```

### Resolution Flow

```
URI → paymail.ResolveURIWith() → (pubkey, endpoints, err)
                                        │
                     ┌──────────────────┴──────────────────┐
                     │                                      │
              endpoints != nil                       endpoints == nil
              (Paymail/DNSLink)                      (bare PubKey)
                     │                                      │
           --host provided?                        --host provided?
           ┌───┴───┐                               ┌───┴───┐
          yes      no                              yes      no
           │        │                               │        │
        use --host  use https://endpoints[0]     use --host  ERROR:
                                                            "bare pubkey URI
                                                             requires --host"
```

### --host Flag Changes

| Before | After |
|--------|-------|
| Default: `http://localhost:8080` | Default: `""` (empty) |
| Always used as daemon URL | Only used if explicitly provided |
| Required for all URIs | Required only for bare pubkey URIs |

### Per-Tool Changes

Each b* tool's `switch parsed.Type` block (lines ~58-68 in bls) replaced with:

```go
resolved, err := client.ResolveURI(uri, *host)
if err != nil {
    fmt.Fprintf(stderr, "bls: %v\n", err)
    return 6
}
c := resolved.Client
pnode := resolved.PNode
path := resolved.Path
```

The rest of each tool's logic (GetMeta, GetData, output formatting) remains unchanged.

### Timeout Integration

`ResolveURI` returns a `*Client` without timeout. Tools apply timeout after:

```go
resolved, err := client.ResolveURI(uri, *host)
// ...
if *timeout != "" {
    d, _ := time.ParseDuration(*timeout)
    resolved.Client = resolved.Client.WithTimeout(d)
}
```

## Endpoint URL Construction

From `paymail.ResolveURI()` endpoints:
- Paymail SRV: `_paymail._tcp.domain` → `host:port`
- DNSLink SRV: `_bitfs._tcp.domain` → `host:port`
- Fallback: `domain:443`

URL construction in `resolve.go`:
```go
endpoint := endpoints[0]  // "example.com:443" or "cdn.example.com:8080"
baseURL := "https://" + endpoint
```

Always HTTPS. The `--host` override allows `http://` for local development.

## Scope

**In scope:**
- `bitfs/internal/client/resolve.go` — new shared resolve function
- `bitfs/internal/client/resolve_test.go` — unit tests with mocked DNS/HTTP
- `bitfs/cmd/bls/main.go` — wire up ResolveURI
- `bitfs/cmd/bcat/main.go` — wire up ResolveURI
- `bitfs/cmd/bget/main.go` — wire up ResolveURI
- `bitfs/cmd/bstat/main.go` — wire up ResolveURI
- `bitfs/cmd/btree/main.go` — wire up ResolveURI
- `bitfs/docs/spec/11-cmd-btools.md` — update spec to reflect new --host behavior

**Out of scope:**
- Caching resolved endpoints (future)
- `--no-cache` / `--offline` flags (not yet implemented, separate P2)
- Local engine fallback (not needed — b* tools are for remote access)

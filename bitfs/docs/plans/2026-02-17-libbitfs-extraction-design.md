# libbitfs Shared Core Library Extraction

**Date**: 2026-02-17
**Status**: Approved

## Goal

Extract 8 reusable packages from `bitfs/internal/` into `libbitfs/` as public Go packages. Both `bitfs` and `metanet` (and future products) can import them.

## Decisions

| Decision | Choice |
|----------|--------|
| API visibility | Public packages (top-level, not internal/) |
| Module path | `github.com/tongxiaofeng/libbitfs` |
| Packages to extract | All 8 standalone packages |
| Structure | Flat public packages (Option A) |
| Migration strategy | Copy first, verify, then delete originals |

## Target Structure

```
libbitfs/
├── go.mod                 github.com/tongxiaofeng/libbitfs
├── method42/              ECDH encryption engine
├── wallet/                HD wallet (BIP32/BIP39 + Argon2id)
├── spv/                   SPV light client (Merkle proof)
├── storage/               Content-addressed file store
├── config/                Configuration management
├── paymail/               Paymail URI + DNS resolution
├── x402/                  x402 payment protocol (HTLC)
└── tx/                    BSV transaction builder
```

bitfs retains:
```
bitfs/internal/
├── metanet/               Metanet DAG (imports libbitfs/tx)
├── daemon/                LFCP HTTP server
└── revshare/              Placeholder (empty)
```

## Import Path Change

```go
// Before
import "github.com/tongxiaofeng/bitfs/internal/method42"
// After
import "github.com/tongxiaofeng/libbitfs/method42"
```

## Dependencies

libbitfs go.mod:
- `github.com/bsv-blockchain/go-sdk v1.2.18`
- `golang.org/x/crypto v0.47.0`
- `github.com/stretchr/testify v1.11.1` (test)

bitfs go.mod:
- `require github.com/tongxiaofeng/libbitfs v0.0.0`
- `replace github.com/tongxiaofeng/libbitfs => ../libbitfs` (local dev)

## Migration Steps

1. Initialize libbitfs `go.mod`, copy 8 packages from `internal/` to top-level
2. Fix package paths, verify `go build ./...` and `go test ./...` in libbitfs
3. Add require + replace in bitfs `go.mod`, update all imports to libbitfs paths
4. Update `metanet` package to import `libbitfs/tx`, check `daemon` test imports
5. Verify bitfs `go test ./...` passes
6. Delete migrated packages from bitfs `internal/`
7. Final verification: both repos `go test ./...` pass

## Notes

- Package names stay the same (method42, wallet, etc.)
- wallet's compat alias unchanged: `compat "github.com/bsv-blockchain/go-sdk/compat/bip32"`
- Test files (`*_test.go` + `coverage_supplement_test.go`) migrate with their packages
- metanet is the only package with an internal dep (→ tx), changes to `libbitfs/tx`
- daemon uses interfaces, no direct imports of migrated packages (verify in tests)

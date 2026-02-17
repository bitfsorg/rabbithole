# Implementation Plan: BitFS Core Filesystem

**Branch**: `001-bitfs-core` | **Date**: 2026-02-17 | **Spec**: [spec.md](spec.md)
**Input**: Design documents from `design/bitfs/1-4*.zh.md` + existing spec files in `bitfs/spec/`

## Summary

Implement the complete BitFS decentralized encrypted filesystem on BSV
blockchain. The system provides Unix-style filesystem semantics over a
Metanet DAG, Method 42 ECDH encryption for all content, SPV verification
without blockchain queries, HTLC atomic swap monetization, and an HTTP
daemon for content serving. The implementation is organized as 11 Go
packages (12 internal + 6 CLI binaries) following the four-phase design
document structure.

**Current Status**: ~70% test coverage (659/938 planned tests passing).
All 12 internal packages and 6 CLI binaries have initial implementations.
Remaining work focuses on completing missing test cases, integration
tests (~333 cases), and production hardening.

## Technical Context

**Language/Version**: Go 1.25.6
**Primary Dependencies**: `github.com/bsv-blockchain/go-sdk` v1.2.18 (only BSV dependency), `github.com/stretchr/testify` v1.11.1, `golang.org/x/crypto` v0.47.0
**Storage**: Content-addressed file store with hash-sharded directories (~/.bitfs/storage/)
**Testing**: `go test ./...` with table-driven tests, `testify/assert` + `testify/require`
**Target Platform**: Linux/macOS/Windows CLI, HTTP daemon on Linux servers
**Project Type**: Single Go module with multiple binaries
**Performance Goals**: SPV verification <10ms (cached headers), daemon handles 100 concurrent requests
**Constraints**: SPV-only (no blockchain queries), single BSV dependency, all content encrypted by default
**Scale/Scope**: 12 internal packages, 6 CLI binaries, ~938 test cases, ~13K LOC

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Plan First | PASS | This plan document exists; design docs (4 levels) precede implementation |
| II. Documentation First | PASS | 5 design documents + 11 module specs + TASKS.md all precede code |
| III. Test-Driven Development | PASS | 659 tests exist and pass; remaining ~279 tests to be written before new implementation |

**Gate Result**: All three principles satisfied. Proceed to Phase 0.

**Post-Design Re-check**:
- Plan First: Design documents cover all modules exhaustively
- Documentation First: spec/ directory has 11 module specs written before code
- TDD: Test-first workflow MUST be enforced for remaining implementation
  (write tests → verify red → user approval → implement)

## Project Structure

### Documentation (this feature)

```text
specs/001-bitfs-core/
├── plan.md              # This file
├── research.md          # Phase 0: technology decisions
├── data-model.md        # Phase 1: entity definitions
├── quickstart.md        # Phase 1: getting started guide
└── tasks.md             # Phase 2: implementation tasks (via /speckit.tasks)
```

### Source Code (repository root)

```text
bitfs/                              # Go module root
├── cmd/                            # CLI binaries
│   ├── bitfs/                      # Main CLI (put/get/ls/cat/rm/mv/cp/sell/wallet/daemon)
│   ├── bls/                        # Read-only: list directory
│   ├── bcat/                       # Read-only: output file contents
│   ├── bget/                       # Read-only: download file
│   ├── bstat/                      # Read-only: file metadata
│   └── btree/                      # Read-only: recursive tree
├── internal/                       # Core libraries
│   ├── method42/                   # ECDH encryption engine (45 tests)
│   ├── wallet/                     # HD wallet BIP32/BIP39 (55 tests)
│   ├── tx/                         # BSV transaction builder (40 tests)
│   ├── metanet/                    # Metanet DAG parser + Unix FS (65 tests)
│   ├── spv/                        # SPV light client (35 tests)
│   ├── storage/                    # Content-addressed storage (25 tests)
│   ├── paymail/                    # Paymail + bitfs:// URI (35 tests)
│   ├── x402/                       # x402 payment protocol (30 tests)
│   ├── daemon/                     # HTTP server LFCP (80 tests)
│   ├── config/                     # Configuration management
│   └── revshare/                   # Revenue sharing (placeholder)
├── integration/                    # Cross-package integration tests (~333 tests)
│   ├── filesystem_test.go
│   ├── payment_flow_test.go
│   ├── tx_build_test.go
│   ├── uri_resolve_test.go
│   └── wallet_crypto_test.go
└── spec/                           # Module specifications (Chinese)
    ├── 01-method42.md ... 11-cmd-btools.md
    └── TASKS.md
```

**Structure Decision**: Standard Go project layout with `cmd/` for binaries
and `internal/` for private packages. This matches the existing codebase
and Go conventions. No structural changes needed.

## Complexity Tracking

> No constitution violations requiring justification.

| Aspect | Decision | Rationale |
|--------|----------|-----------|
| 12 internal packages | Required | Each maps to a distinct design document chapter |
| 6 CLI binaries | Required | Unix philosophy: one tool per task, composable |
| Single BSV dependency | Constraint | Design decision #24: go-sdk is the only BSV library |

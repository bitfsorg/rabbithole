# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**BitFS** is a Unix-style decentralized encrypted file system on BSV blockchain. Agent-first design with Unix CLI tools.

Core concepts:
- **Metanet DAG**: Blockchain-based DAG implementing Unix filesystem (inode=P_node, dirent=ChildEntry, soft/hard links)
- **Method 42**: Deterministic per-file ECDH encryption (all data encrypted by default), key formula: `aes_key = HKDF-SHA256(ECDH(D_node, P_node).x, key_hash)`
- **SPV mode**: Local tx + Merkle proof, never queries blockchain
- **HTLC atomic swap**: Trustless buy/sell via hash time-locked contracts
- **x402 payment**: HTTP 402-based content payment protocol
- **Metanet Chain**: Decentralized CDN network — separate product (see `../metanet/`)

## Build & Test

```bash
go test ./...                    # Run all unit tests (~512 cases)
go test ./internal/method42/     # Run single package tests
go test ./... -v -count=1        # Verbose, no cache
go build ./cmd/bitfs             # Build main CLI binary
go build ./cmd/...               # Build all binaries (bitfs, bls, bcat, bget, bstat, btree)
```

Module: `github.com/tongxiaofeng/bitfs`, Go 1.25.6

## Project Structure

```
bitfs/                                  ← THIS DIRECTORY (code implementation)
├── CLAUDE.md
├── go.mod / go.sum
├── cmd/                               ← CLI binaries
│   ├── bitfs/                           Main CLI (put/get/ls/cat/rm/mv/cp/sell/wallet/daemon)
│   ├── bls/                             Read-only: list directory
│   ├── bcat/                            Read-only: output file contents to stdout
│   ├── bget/                            Read-only: download file to local filesystem
│   ├── bstat/                           Read-only: file metadata (size, hash, owner, access)
│   └── btree/                           Read-only: recursive directory tree
├── internal/                          ← Application layer
│   ├── buyer/                           Purchase state machine
│   ├── client/                          b-tools HTTP client
│   ├── daemon/                          Daemon HTTP server (LFCP, WebMCP, content negotiation)
│   └── engine/                          Unified business logic layer
├── docs/                              ← Documentation
│   ├── spec/                            Module specifications (01-method42 to 11-cmd-btools, TASKS.md)
│   └── plans/                           Design & implementation plans
├── integration/                       ← Integration test suites (276 cases)
├── e2e/                               ← Docker regtest end-to-end tests
└── dashboard/                         ← React SPA (embedded in daemon)
```

## Design Documents

Design docs are in the parent directory. For this project, read:

| File | Purpose |
|------|---------|
| `../design/0-OverallDesign.zh.md` | Two-product ecosystem overview, three-layer architecture |
| `../design/bitfs/1-ConceptDesign.zh.md` | Vision, core concepts, 86 design decisions |
| `../design/bitfs/2-SystemDesign.zh.md` | 23 sections: modules, interfaces, data flow |
| `../design/bitfs/3-DetailedDesign.zh.md` | Algorithms, data structures, protocols |
| `../design/bitfs/4-TestDesign.zh.md` | ~980 test cases across 27 categories |

Design docs are in Chinese. Code, specs, and comments are in English.

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/bsv-blockchain/go-sdk` v1.2.18 | Only BSV dependency (ec, bip32, bip39, transaction, script) |
| `github.com/stretchr/testify` v1.11.1 | Test assertions |
| `golang.org/x/crypto` v0.47.0 | HKDF, Argon2id |

## Coding Conventions

- Idiomatic Go with table-driven tests
- go-sdk `compat/bip32` package name is `compat`, needs alias import: `compat "github.com/bsv-blockchain/go-sdk/compat/bip32"`
- Error wrapping: `fmt.Errorf("context: %w", err)`
- Long-running ops accept `context.Context`
- BSV has no dust limit; P2PKH outputs can hold any amount (546 sat is a legacy constant, not a requirement)
- MetaFlag constant: `0x6d657461` ("meta" in ASCII)
- Three access modes: Private (0), Free (1), Paid (2)
- License: OpenBSV License Version 5

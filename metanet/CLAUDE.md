# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Metanet** is a decentralized CDN network built on BSV blockchain. It incentivizes data retrieval (not storage), forming a self-organizing content delivery layer for BitFS and other applications.

Core concepts:
- **Metanet Chain**: BSV-homomorphic sidechain for CDN economics (identical tx format, Bitcoin Script)
- **MNT Token**: Native token for Metanet Node marketplace (21M supply, 50 MNT initial reward, 210K halving)
- **Metanet Node**: CDN node operators who cache and serve content for profit
- **x402 Protocol**: HTTP-native micropayment for content retrieval
- **Dual Payment Channels**: BSV channels for end-user payments, MNT channels for node economics
- **Merged Mining**: SHA256 AuxPoW shared with BTC/BSV miners

## Current Status

**Implementation in progress.** Foundation packages (chain, mining, contract) are implemented with tests. Remaining: proof, payment, overlay, CLI.

- Design docs: Complete (4-layer design at `../design/metanet/`)
- Module specs: Complete at `docs/spec/` (7 modules)
- Task breakdown: `docs/spec/TASKS.md` (8 tasks, 4 phases, ~128 tests)
- Implementation: Phase 1 (chain, mining) and Phase 2 partial (contract) complete

## Project Structure

```
metanet/
├── CLAUDE.md
├── LICENSE                    ← Open BSV License v5
├── go.mod                     ← module github.com/tongxiaofeng/metanet
├── docs/                      ← Documentation
│   └── spec/                  ← Module specifications
│       ├── TASKS.md           ← Implementation task breakdown
│       ├── chain.md           ← internal/chain spec
│       ├── mining.md          ← internal/mining spec
│       ├── contract.md        ← internal/contract spec
│       ├── proof.md           ← internal/proof spec
│       ├── payment.md         ← internal/payment spec
│       ├── overlay.md         ← internal/overlay spec
│       └── cmd-metanet.md     ← cmd/metanet spec
├── internal/
│   ├── chain/                 ← Metanet Chain core (params, block, token, genesis)
│   ├── mining/                ← Merged mining (AuxPoW, difficulty, BSV anchoring)
│   ├── contract/              ← Storage contracts (deal, challenge, script)
│   ├── proof/                 ← Storage proofs (ECDH encryption, Merkle tree)
│   ├── payment/               ← Payment channels (BSV + MNT)
│   ├── overlay/               ← BRC Overlay Network
│   └── config/                ← Configuration management
└── cmd/
    └── metanet/               ← Metanet Node CLI binary
```

## Key Design Documents

| File | Purpose |
|------|---------|
| `../design/0-OverallDesign.zh.md` | Two-product ecosystem, three-layer architecture |
| `../design/metanet/1-ConceptDesign.zh.md` | CDN model, economic design, design principles |
| `../design/metanet/2-SystemDesign.zh.md` | Node architecture, contracts, payment channels |
| `../design/metanet/3-DetailedDesign.zh.md` | Consensus, mining, settlement protocol details |
| `../design/metanet/4-TestDesign.zh.md` | Test case design (20 test cases, 5 categories) |

## Dependencies

- **BSV SDK**: `github.com/bsv-blockchain/go-sdk` (the ONLY allowed BSV dependency)
- **BitFS shared library**: `github.com/tongxiaofeng/bitfs` (method42, metanet DAG, spv, tx)
- Standard library only for current packages (no external deps yet)

## Coding Conventions

- Idiomatic Go with table-driven tests using `testing` package
- Error types defined in `errors.go` per package
- Double-SHA256 (SHA256d) for all block/tx hashing (Bitcoin convention)
- Bitcoin compact target format (Bits field) for difficulty
- Little-endian byte order for all serialization (Bitcoin convention)
- All design docs are in Chinese (zh); code and specs are in English

## Relationship to BitFS

- **BitFS** (bitfs.org) = Decentralized encrypted file system protocol
- **Metanet** (metanet.org) = Decentralized CDN network
- Analogy: BitFS:IPFS :: Metanet:Filecoin
- `bitfs` CLI = user filesystem tool, `metanet` CLI = CDN node operator tool
- Separate binaries, shared Go library

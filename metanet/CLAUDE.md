# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Metanet** is a decentralized CDN network built on BSV blockchain. It incentivizes data retrieval (not storage), forming a self-organizing content delivery layer for BitFS and other applications.

Core concepts:
- **Metanet Chain**: BSV-homomorphic sidechain for CDN economics (identical tx format, Bitcoin Script)
- **MNT Token**: Native token for Metanet Node marketplace (staking, retrieval fees, revenue sharing)
- **Metanet Node**: CDN node operators who cache and serve content for profit
- **x402 Protocol**: HTTP-native micropayment for content retrieval
- **Dual Payment Channels**: BSV channels for end-user payments, MNT channels for node economics

## Current Status

**Design phase. Metanet CDN design is specified within the shared design documents and the Metanet whitepaper.**

- Whitepaper: Complete (English)
- Design docs: Shared at workspace root (will be split during implementation phase)
- Implementation: Not started

## Project Structure

```
RabbitHole/                             ← Workspace root
├── doc/                                ← Shared design documents
├── whitepaper/                         ← All whitepapers (BitFS + Metanet)
├── references/                         ← Research papers
├── slides/                             ← Presentation slides
├── bitfs/                              ← Sister repo — BitFS file system
└── metanet/                            ← THIS REPO — Metanet CDN implementation
    ├── CLAUDE.md
    ├── doc/                            ← Reserved for implementation specs
    └── website/                        ← metanet.org landing page
```

## Key Documents

| File | Purpose |
|------|---------|
| `../whitepaper/Metanet-Whitepaper.en.md` | Metanet Network whitepaper (English) |
| `../doc/2-SystemDesign.zh.md` | System design section 十九: Metanet Chain CDN |
| `../doc/3-DetailedDesign.zh.md` | Detailed design: Metanet Chain protocols |
| `../references/` | Research papers (Metanet Technical Summary, etc.) |

## Relationship to BitFS

- **BitFS** (bitfs.org) = Decentralized encrypted file system protocol
- **Metanet** (metanet.org) = Decentralized CDN network
- Analogy: BitFS:IPFS :: Metanet:Filecoin
- `bitfs` CLI = user filesystem tool, `metanet` CLI = CDN node operator tool
- Separate binaries, shared Go library

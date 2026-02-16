# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**BitFS** is a Unix-style decentralized encrypted file system on BSV blockchain. Agent-first design with Unix CLI tools.

Core concepts:
- **Metanet DAG**: Blockchain-based DAG implementing Unix filesystem (inode=P_node, dirent=ChildEntry, soft/hard links)
- **Method 42**: Deterministic per-file ECDH encryption (all data encrypted by default)
- **SPV mode**: Local tx + Merkle proof, never queries blockchain
- **HTLC atomic swap**: Trustless buy/sell via hash time-locked contracts
- **Metanet Chain**: Decentralized CDN (BSV-homomorphic chain, incentivizes retrieval not storage) — separate product at metanet.org

## Current Status

**Design phase. No implementation code — all previous code and OpenSpec specs have been removed pending design finalization.**

The four-layer design document set is actively being refined:
- Concept Design (vision, architecture, design decisions)
- System Design (23 sections covering all modules)
- Detailed Design (algorithms, protocols, data structures with B-sections)
- Test Design (~938 test cases specified)

## Project Structure

```
RabbitHole/                             ← Workspace root
├── doc/                                ← Shared design documents
│   ├── 1-ConceptDesign.zh.md           ← Vision, core concepts, architecture, design decisions (#1-#86)
│   ├── 2-SystemDesign.zh.md            ← Modules, interfaces, data flow (23 sections)
│   ├── 3-DetailedDesign.zh.md          ← Algorithms, data structures, protocols (B-sections)
│   └── 4-TestDesign.zh.md              ← Test case design (~938 test cases)
├── whitepaper/                         ← All whitepapers (BitFS + Metanet)
├── references/                         ← Research papers (6 PDFs)
├── slides/                             ← Presentation slides
├── bitfs/                              ← THIS REPO — BitFS implementation
│   ├── CLAUDE.md
│   ├── doc/                            ← Reserved for OpenSpec implementation specs
│   ├── website/                        ← bitfs.org landing page
│   └── backup/
└── metanet/                            ← Sister repo — Metanet CDN implementation
    ├── CLAUDE.md
    ├── doc/                            ← Reserved for implementation specs
    └── website/                        ← metanet.org landing page
```

## Key Documents

| File | Purpose |
|------|---------|
| `../doc/1-ConceptDesign.zh.md` | Vision, core concepts, architecture, 86 design decisions |
| `../doc/2-SystemDesign.zh.md` | 23 sections: HD wallet, Metanet, encryption, daemon, CLI, Metanet Chain CDN, etc. |
| `../doc/3-DetailedDesign.zh.md` | B-sections: HD wallet derivation, Metanet tx, CLI commands, protocols, Metanet Chain |
| `../doc/4-TestDesign.zh.md` | ~938 test cases across 22 categories |
| `../whitepaper/` | BitFS + Metanet whitepapers (EN/ZH) |
| `../references/` | Research papers (Method 42, Metanet, etc.) |

## Design Document Conventions

- All design docs are in Chinese (zh)
- Documents cross-reference each other (概念设计 → 系统设计 → 详细设计 → 测试设计)
- Design decisions are numbered sequentially (#1-#86) in ConceptDesign
- System sections are numbered 二 through 二十三
- Detailed B-sections correspond to system design sections (e.g., 二-B expands section 二)
- Test categories map to design sections and future code packages

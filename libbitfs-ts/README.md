# libbitfs-ts

TypeScript client library for BitFS — mirrors the functionality of [`libbitfs-go`](../libbitfs-go).

## Purpose

Provides the same core BitFS primitives as `libbitfs-go`, targeting browser and Node.js environments:

- **method42** — ECDH-based per-file encryption (Method 42)
- **wallet** — HD wallet (BIP32/BIP39)
- **storage** — Content-addressed file store
- **metanet** — Metanet DAG parser and Unix filesystem operations
- **spv** — SPV light client with Merkle proof verification
- **tx** — BSV transaction builder (Metanet tx templates)
- **x402** — x402 payment protocol (HTTP 402, HTLC)
- **paymail** — Paymail identity and `bitfs://` URI resolution
- **network** — Blockchain service interface (RPC / SPV)
- **config** — Configuration management

## Status

Not yet implemented. Planned for a future release.

## Design Principles

- Feature parity with `libbitfs-go` (same interfaces, same wire formats)
- Zero Node.js-only dependencies where possible — target browser + Node.js
- ESM-first, with CommonJS compatibility shim
- BSV dependency: `@bsv/sdk` (official TypeScript BSV SDK)

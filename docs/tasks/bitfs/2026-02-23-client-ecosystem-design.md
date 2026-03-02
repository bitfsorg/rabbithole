# Client Ecosystem Design

Date: 2026-02-23

## Overview

BitFS daemon has a complete HTTP API but no visual clients. This design covers scaffolding for five modules that form the client ecosystem:

1. **Web Dashboard** — React SPA embedded in daemon via `go:embed`
2. **Flutter App** — cross-platform mobile + desktop client with Go FFI
3. **Chrome Extension** — MetaMask-model browser extension with pure TS crypto
4. **Daemon Web Serving** — static site hosting + file browsing enhancements
5. **Project configuration** — gitignore, Makefile, README updates

Scope: design decisions + project scaffolding only. No business logic.

## Architecture

```
                   libbitfs (Go)
                  /     |      \
           daemon    gomobile    (shared core)
           / | \       |
    Dashboard | WebServe  Flutter App (5 platforms)
    (React)   |           dart:ffi <-> Go .so/.dylib/.dll
              |
         Chrome Extension
         (TS, @noble/*, MetaMask model)
```

All clients consume the same BitFS daemon HTTP API. The Flutter app additionally uses Go FFI for direct library access. The Chrome extension implements its own crypto stack in pure TypeScript using @noble/* libraries.

## Module 1: Web Dashboard

**Location**: `bitfs/dashboard/`

**Tech stack**: React 19 + TypeScript + Vite 6 + TailwindCSS 4

**Embedding strategy**: Go `embed.FS` in `dashboard/embed.go` serves the built `dist/` directory. Daemon routes `/_dashboard/*` to the embedded SPA with client-side routing fallback.

**Base path**: `/_dashboard` — underscore prefix avoids collision with Metanet content paths (which are user-defined names).

**Pages**:
| Route | Page | Purpose |
|-------|------|---------|
| `/` | DashboardHome | Overview stats, quick actions |
| `/storage` | Storage | Content-addressed store stats, file list |
| `/network` | Network | Peer connections, SPV header chain status |
| `/wallet` | Wallet | HD wallet info, UTXO balance, transaction history |
| `/logs` | Logs | Daemon log viewer, streaming |

**API client**: `src/lib/api.ts` wraps daemon HTTP endpoints. All requests go to the same origin (daemon serves both API and dashboard).

**Key dependencies**:
- react 19, react-dom 19, react-router-dom 7
- tailwind-merge, lucide-react, clsx, class-variance-authority
- @vitejs/plugin-react, @tailwindcss/vite, vite 6, typescript 5.7

## Module 2: Flutter App

**Location**: `/Users/alex/Codes/RabbitHole/bitfs-app/` (sibling repo)

**Platforms**: iOS, Android, macOS, Windows, Linux

**Architecture**:
- State management: Riverpod 2 (compile-time safe, modern alternative to Provider)
- Routing: GoRouter (declarative, deep link support)
- Go FFI: cgo exports with C ABI, loaded via `dart:ffi`

**Go FFI bridge design**:
```
native/bridge.go (cgo exports)
    -> C ABI: BitFS_Init, BitFS_GetStatus, BitFS_Free
    -> Dart FFI loads .so/.dylib/.dll
    -> lib/core/ffi/bridge.dart wraps C calls
```

cgo exports (C ABI) chosen over gomobile bind because:
- Simpler build pipeline (just `go build -buildmode=c-shared`)
- Works on all 5 platforms with the same pattern
- No gomobile toolchain dependency
- Dart FFI directly calls C functions

**Platform build matrix**:
| Platform | Output | Build Mode |
|----------|--------|------------|
| Android | libbitfs.so (per arch) | c-shared |
| iOS | libbitfs.a → xcframework | c-archive |
| macOS | libbitfs.dylib | c-shared |
| Linux | libbitfs.so | c-shared |
| Windows | libbitfs.dll | c-shared |

**Key dependencies**:
- flutter_riverpod 2.6, go_router 14, ffi 2.1, http
- freezed_annotation + json_annotation (code generation)

## Module 3: Chrome Extension

**Location**: `/Users/alex/Codes/RabbitHole/bitfs-extension/` (sibling repo)

**Model**: MetaMask-style — all crypto runs locally in the extension. Data comes from BSV public APIs and any BitFS daemon node. Extension never exposes private keys.

**Architecture**:
- **Popup** (React SPA): wallet UI, settings, transaction approval
- **Service Worker**: key management, transaction signing, message routing
- **Content Script**: bitfs:// link detection, HTTP 402 interception, dapp provider injection

**Crypto stack** (pure TypeScript, no WASM):
| Library | Purpose | Bundle Size |
|---------|---------|-------------|
| @noble/secp256k1 2.2 | ECDH, signing | ~30KB |
| @noble/hashes 1.7 | HKDF-SHA256, SHA-256 | ~40KB |
| @scure/bip32 1.6 | HD key derivation | ~15KB |
| @scure/bip39 1.5 | Mnemonic generation | ~100KB |
| Web Crypto API | AES-256-GCM | built-in |

Total crypto bundle: ~200KB (acceptable for extension).

**Vite build**: multi-entry configuration builds popup, background, and content-script as separate entry points in one build command.

**Core modules** (`src/bitfs-core/`):
| Module | Go Counterpart | Purpose |
|--------|---------------|---------|
| method42.ts | libbitfs/method42/ | ECDH encryption, HKDF key derivation |
| wallet.ts | libbitfs/wallet/ | HD wallet, BIP44 path m/44'/236' |
| metanet.ts | libbitfs/metanet/ | DAG parsing, child entries |
| x402.ts | libbitfs/x402/ | HTTP 402 payment protocol |
| types.ts | libbitfs/*/types.go | Shared constants and types |

**MV3 permissions**: `storage` (wallet data) + `activeTab` (current tab interaction). Minimal permissions for security.

## Module 4: Daemon Web Serving

**File**: `bitfs/internal/daemon/webserve.go`

**Purpose**: Enhance daemon HTTP server with:
1. `/_dashboard/*` route serving embedded React SPA
2. SPA client-side routing fallback (serve index.html for non-file paths)
3. Static site hosting capability (future: serve Metanet content as websites)
4. File browser enhancement (future: HTML directory listings)

**Implementation**: Stub file with TODO comments. Actual implementation deferred to when dashboard is built.

## Module 5: Go Embed Integration

**File**: `bitfs/dashboard/embed.go`

```go
package dashboard

import "embed"

// FS contains the built dashboard files.
// Uncomment the go:embed directive after running `npm run build` in this directory.
//
// //go:embed dist/*
var FS embed.FS
```

The `go:embed` directive is commented out because `dist/` doesn't exist until the dashboard is built. This allows `go build ./...` to succeed immediately.

## Key Decisions

| Decision | Choice | Alternatives Considered | Rationale |
|----------|--------|------------------------|-----------|
| Dashboard base path | `/_dashboard` | `/admin`, `/dashboard` | Underscore prefix avoids collision with user content paths |
| Flutter state mgmt | Riverpod 2 | Provider, Bloc, GetX | Compile-time safe, modern, good testing story |
| Flutter router | GoRouter | auto_route, Navigator 2 | Declarative, deep link support, official recommendation |
| Go bridge approach | cgo exports (C ABI) | gomobile bind | Simpler pipeline, works on all platforms, no extra toolchain |
| Chrome crypto | @noble/* + Web Crypto | noble-secp256k1 + sjcl, libsodium-wrappers | Pure JS, audited, small bundle, maintained by paulmillr |
| Extension model | MetaMask-style | embedded daemon, proxy | Crypto local = secure, no daemon dependency for basic ops |
| Chrome build | Vite multi-entry | webpack, parcel, rollup | Consistent with dashboard tooling, fast HMR, good plugin ecosystem |
| Content script scope | `<all_urls>` | specific domains | bitfs:// links can appear on any page |

## Priority Order

1. **Dashboard** (highest) — immediately useful for daemon operators
2. **Chrome Extension** — enables end-user access without CLI
3. **Flutter App** — full-featured client, longer development timeline
4. **Daemon Web Serving** — depends on dashboard being built first

## File Summary

| Component | Location | Files | Status |
|-----------|----------|-------|--------|
| Design doc | docs/plans/ | 1 | This file |
| Dashboard | dashboard/ | ~18 | Scaffold |
| Flutter App | ../bitfs-app/ | ~25 | Scaffold (separate repo) |
| Chrome Extension | ../bitfs-extension/ | ~22 | Scaffold (separate repo) |
| Go stubs | dashboard/embed.go, internal/daemon/webserve.go | 2 | Stub |
| Config | .gitignore, Makefile, README | 3 | Update |

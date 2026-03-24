# hexToBytes Consolidation Design

**Status:** Approved
**Date:** 2026-03-24

## Problem

`hexToBytes` is duplicated 8 times across 4 repos with inconsistent error handling:

| Location | Behavior on odd-length | Validates hex chars |
|----------|----------------------|---------------------|
| `libbitfs-ts/src/util.ts` (canonical) | Throws | No |
| `libbitfs-ts/src/network/rpc.ts` | Throws `InvalidResponseError` | Yes (NaN check) |
| `bitfs-extension/src/background/file-reader.ts` | Returns empty | No |
| `bitfs-extension/src/lib/provider.ts` | Returns empty | No |
| `bitfs-app/src/adapters/WoCProvider.ts` | Returns empty | No |
| `bitfs-app/src/adapters/FileSystemStore.ts` | Returns empty | No |
| `bitfs-app/src/services/TxService.ts` | Returns empty | No |
| `bitfs-explorer/src/decoder.ts` | Returns empty | No |

Additionally, `bitfs-app/src/services/PaymentService.ts` has a `parseHexBytes` variant with regex validation and optional length checking.

## Design

### Prerequisite: Export hexToBytes from libbitfs-ts

`libbitfs-ts/src/util.ts` exports `hexToBytes`, but `src/index.ts` does NOT re-export it. Consumers cannot `import { hexToBytes } from '@bitfs/libbitfs'` today. **This must be fixed first** by adding `export { hexToBytes } from './util.js'` to `src/index.ts`.

### Error handling: throw on invalid input

All consumer call sites process data from APIs (WoC, RPC) or blockchain storage. Invalid hex is always an error in these contexts — returning empty bytes silently produces incorrect downstream behavior (broken decryption, wrong UTXO parsing). The throwing behavior from `libbitfs-ts/util.ts` is correct.

### What changes

**Step 0: Add export to libbitfs-ts/src/index.ts**

Add `export { hexToBytes } from './util.js'` so consumers can import from `@bitfs/libbitfs`.

**Delete local `hexToBytes` from 6 files:**

1. `bitfs-extension/src/background/file-reader.ts` — delete local function, import from `@bitfs/libbitfs`
2. `bitfs-extension/src/lib/provider.ts` — delete local function, import from `@bitfs/libbitfs`
3. `bitfs-app/src/adapters/WoCProvider.ts` — delete local function, import from `@bitfs/libbitfs`
4. `bitfs-app/src/adapters/FileSystemStore.ts` — delete local function, import from `@bitfs/libbitfs`
5. `bitfs-app/src/services/TxService.ts` — delete local function, import from `@bitfs/libbitfs`
6. `bitfs-explorer/src/decoder.ts` — delete local function, import from `@bitfs/libbitfs`. Also wrap the `hexToBytes` call inside `parseScriptPushes` in a try/catch (return `[]` on error) since the old version returned empty on invalid input and callers like `content.ts` expect `null`/empty, not an uncaught throw.

**Keep as-is:**

- `libbitfs-ts/src/util.ts` `hexToBytes` — the canonical implementation, unchanged
- `libbitfs-ts/src/network/rpc.ts` `hexToBytes` — has unique NaN-per-character validation + throws `InvalidResponseError` (RPC-specific error type). This validation is appropriate for untrusted RPC responses and should stay in the RPC layer.
- `bitfs-app/src/services/PaymentService.ts` `parseHexBytes` — has unique regex + length validation, not a simple duplicate

### Import paths

| Repo | Import statement |
|------|-----------------|
| bitfs-extension | `import { hexToBytes } from '@bitfs/libbitfs'` |
| bitfs-app | `import { hexToBytes } from '@bitfs/libbitfs'` |
| bitfs-explorer | `import { hexToBytes } from '@bitfs/libbitfs'` |

All three consumer repos already have `@bitfs/libbitfs` as a `file:` dependency in their `package.json`.

### Verification

- `libbitfs-ts`: `bun test` — 962 tests pass
- `bitfs-extension`: `bun run build` — tsc + vite succeed
- `bitfs-app`: type check passes
- `bitfs-explorer`: `bun run build` — vite succeeds

## Out of scope

- Adding hex character validation (NaN check) to the canonical `hexToBytes` — the `rpc.ts` version has this for RPC-specific error handling. Not needed at the util level.
- Consolidating `bytesToHex` (fewer duplicates, already well-distributed).
- Changing `parseHexBytes` to use the canonical `hexToBytes` internally.

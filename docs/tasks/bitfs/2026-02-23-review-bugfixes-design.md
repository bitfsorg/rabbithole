# Review Bugfixes Design

Date: 2026-02-23
Source: antigravity code review

## Overview

Five issues found during external review. Fixes ordered by severity.

## Fix 1: Remove doesn't update parent directory [Critical]

**File**: `internal/engine/remove.go`

**Problem**: `Engine.Remove()` sends `OpDelete` to the node itself but never updates the parent directory's child list. Deleted files remain visible in directory listings.

**Solution**: After building the node's delete TX, build a second SelfUpdate TX for the parent directory with the child entry removed.

**Changes**:
- `remove.go`: After node delete TX, find parent via `nodeState.Path` → `path.Dir()` → `resolveParentDir()`. Remove child entry from parent's `Children` slice. Call `buildParentSelfUpdate()` (already exists in move.go — extract to shared helper). Return both TXs concatenated with `\n` separator (same pattern as `crossDirectoryMove`).
- Update local state: remove node from `State.Nodes` map, remove child from parent's `Children`.
- Extract `buildParentSelfUpdate()` from `move.go` into a shared file (e.g. `helpers.go`) so both `remove.go` and `move.go` can use it.

**Result format**: `Result.TxHex` = `"<node_delete_tx>\n<parent_update_tx>"`, `Result.TxID` = parent update txid.

## Fix 2: Free content outputs ciphertext [Critical]

**Files**: `cmd/bcat/main.go`, `cmd/bget/main.go`

**Problem**: `outputContent()` and `downloadContent()` fetch encrypted data for free-access files but output raw ciphertext without decrypting.

**Solution**: After fetching data, decrypt using `method42.Decrypt(ciphertext, nil, pubKey, keyHash, AccessFree)`. The `nil` private key triggers `FreePrivateKey()` (scalar 1). `pubKey` comes from `meta.PNode` (already in `MetaResponse`).

**Changes in bcat/main.go `outputContent()`**:
```go
// Read all bytes
ciphertext, err := io.ReadAll(reader)
// Decode pubkey and keyHash from hex
pubKeyBytes, _ := hex.DecodeString(meta.PNode)
pubKey, _ := ec.PublicKeyFromBytes(pubKeyBytes)
keyHashBytes, _ := hex.DecodeString(meta.KeyHash)
// Decrypt
result, err := method42.Decrypt(ciphertext, nil, pubKey, keyHashBytes, method42.AccessFree)
// Write plaintext
stdout.Write(result.Plaintext)
```

**Changes in bget/main.go `downloadContent()`**: Same pattern — read all, decrypt, write plaintext to file.

## Fix 3: Extended metadata lost in Copy/Sell/Encrypt [High]

**Files**: `internal/engine/copy.go`, `sell.go`, `encrypt.go`, `state.go`

**Problem**: When building `metanet.Node` payloads, only `MimeType`, `FileSize`, `KeyHash` are preserved. Fields `Keywords`, `Description`, `Domain`, `OnChain`, `Compression`, `ContentTxIDs` are dropped.

**Solution**: Two parts:

### Part A: Extend NodeState

Add fields to `NodeState` in `state.go`:
```go
Keywords    string   `json:"keywords,omitempty"`
Description string   `json:"description,omitempty"`
Domain      string   `json:"domain,omitempty"`
OnChain     bool     `json:"on_chain,omitempty"`
Compression int32    `json:"compression,omitempty"`
```

`ContentTxIDs` is not tracked in local state because it's derivable from the content storage layer.

### Part B: Preserve metadata in operations

**copy.go**: Copy all extended fields from `srcNode` to the new `metanet.Node`:
```go
node.Keywords    = srcNode.Keywords
node.Description = srcNode.Description
node.Domain      = srcNode.Domain
node.OnChain     = srcNode.OnChain
node.Compression = srcNode.Compression
```
Also copy to `childState` (local state).

**sell.go**: Carry forward extended fields from `nodeState`:
```go
if nodeState.Keywords != "" { node.Keywords = nodeState.Keywords }
if nodeState.Description != "" { node.Description = nodeState.Description }
// etc.
```

**encrypt.go**: Same pattern as sell.go — carry forward all metadata.

**put.go** (if exists): Ensure extended metadata is populated when creating files initially.

## Fix 4: Move validate-before-broadcast [Medium]

**File**: `internal/engine/move.go`

**Problem**: `crossDirectoryMove` mutates local state (parent TxIDs, child lists) between building TX1 and TX2. If TX2 build fails, state is inconsistent.

**Solution**: Restructure to build-then-apply:

1. **Phase 1 — Build**: Clone the child lists, build both TXs without modifying any state. If either fails, return error with no side effects.
2. **Phase 2 — Apply**: Both TXs built successfully. Now apply all local state changes atomically.

```go
// Phase 1: Build both TXs (no state mutation)
srcChildrenCopy := cloneChildren(srcParent.Children)
// remove child from copy
srcTxHex, srcTxID, err := e.buildParentSelfUpdateFromChildren(srcParent, srcChildrenCopy)
if err != nil { return nil, err }

dstChildrenCopy := cloneChildren(dstParent.Children)
// add child to copy
dstTxHex, dstTxID, err := e.buildParentSelfUpdateFromChildren(dstParent, dstChildrenCopy)
if err != nil { return nil, err }

// Phase 2: Apply state (both succeeded)
srcParent.Children = srcChildrenCopy
srcParent.TxID = srcTxID
dstParent.Children = dstChildrenCopy
dstParent.TxID = dstTxID
nodeState.Path = opts.DstPath
```

## Fix 5: Spec doc path references [Low]

**Files**: `spec/01-method42.md` through `spec/11-cmd-btools.md`, `spec/TASKS.md`

**Problem**: 45 references to `internal/method42`, `internal/wallet`, etc. — packages now in `libbitfs/`.

**Solution**: Batch find-and-replace:
- `internal/method42` → `libbitfs/method42`
- `internal/wallet` → `libbitfs/wallet`
- `internal/tx` → `libbitfs/tx`
- `internal/metanet` → `libbitfs/metanet`
- `internal/spv` → `libbitfs/spv`
- `internal/storage` → `libbitfs/storage`
- `internal/paymail` → `libbitfs/paymail`
- `internal/x402` → `libbitfs/x402`
- `internal/config` → `libbitfs/config`

Spec titles like "模块规范：internal/method42" become "模块规范：libbitfs/method42".

## Test Strategy

- **Fix 1 (Remove)**: Update `remove_test.go` to verify parent's child list is updated. Add test for "remove last child" and "remove from root".
- **Fix 2 (Free decrypt)**: Update `bcat` and `bget` tests to verify decrypted output matches plaintext.
- **Fix 3 (Metadata)**: Add test cases that create files with Keywords/Description, then Copy/Sell/Encrypt and verify fields survive.
- **Fix 4 (Move)**: Add test that simulates second TX build failure and verifies state is unchanged.
- **Fix 5 (Specs)**: Manual verification — no automated tests needed.

## Files Changed

| Fix | Files | Type |
|-----|-------|------|
| 1 | `engine/remove.go`, `engine/helpers.go` (new), `engine/move.go`, `engine/remove_test.go` | Bug fix |
| 2 | `cmd/bcat/main.go`, `cmd/bget/main.go`, `cmd/bcat/main_test.go`, `cmd/bget/main_test.go` | Bug fix |
| 3 | `engine/state.go`, `engine/copy.go`, `engine/sell.go`, `engine/encrypt.go`, `engine/copy_test.go`, `engine/sell_test.go`, `engine/encrypt_test.go` | Bug fix |
| 4 | `engine/move.go`, `engine/move_test.go` | Hardening |
| 5 | `spec/01-method42.md` ... `spec/TASKS.md` (12 files) | Docs |

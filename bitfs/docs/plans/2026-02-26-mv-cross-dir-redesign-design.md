# Cross-Directory mv Redesign: DELETE + CreateChild

**Date**: 2026-02-26
**Status**: Approved
**Priority**: P2
**Scope**: Cross-directory mv only. Same-directory rename unchanged (SelfUpdate of parent ChildEntry.Name).

## Problem

Current cross-directory mv preserves node identity (PubKey, HD path, encryption key). This causes:
- HD tree diverges from directory tree (node at `/b/file` still derived from `/a/file` path)
- Ghost node accumulation in DAG
- Inconsistent security model — moved file retains old access control keys

## Decision

Cross-directory `mv src dst` = DELETE old node + CreateChild new node at destination.

Reference: `tasks/2026-02-25-design-review.md` section 1.3 [DECIDED].

## Transaction Flow (4 txs)

1. **Tx1** `dstParent.SelfUpdate` — AddChild to destination directory, allocate new HD index, derive new P_node
2. **Tx2** `CreateChild` — Create new node at destination with new P_node, re-encrypted content
3. **Tx3** `srcNode.SelfUpdate(op=DELETE)` — Mark old node deleted; embed `moved_to: new_P_node` via tagLinkTarget (0x09)
4. **Tx4** `srcParent.SelfUpdate` — RemoveChild from source directory

Build-then-apply: all 4 transactions built before any state mutation. If any build fails, state unchanged.

## Re-encryption

For file nodes:
1. Read content from storage (hash-sharded `~/.bitfs/storage/`)
2. Decrypt with old node's Method 42 key: `old_key = HKDF-SHA256(ECDH(D_old, P_old).x, key_hash)`
3. Re-encrypt with new node's key: `new_key = HKDF-SHA256(ECDH(D_new, P_new).x, key_hash)`
4. Store new encrypted content

For directory nodes with children: only the directory node itself is moved. Children remain attached to their original parent via Metanet DAG parent pointers. Recursive mv of subtrees is out of scope — the user must mv individual files.

Note: For FREE access files (unencrypted), skip decrypt/re-encrypt; just create new node with same content reference.

## `moved_to` Metadata

Reuse `tagLinkTarget (0x09)` in the DELETE OP_RETURN to store the new P_node (33 bytes). Semantics: when Op=DELETE and LinkTarget is set, it means "this node was moved to LinkTarget". Clients can follow this pointer to find the new location.

## Capsule Invalidation Warning

New P_node → new encryption key → existing purchased capsules become invalid.

Shell interactive prompt before cross-directory mv of PAID files:
```
WARNING: Moving this file will invalidate N existing capsule(s).
Buyers will need to re-purchase access at the new location.
Continue? [y/N]
```

For non-interactive (CLI/agent) mode: `--force` flag to skip prompt.

## Scope Boundaries

**In scope:**
- `bitfs/internal/engine/move.go` — rewrite crossDirectoryMove()
- `bitfs/cmd/bitfs/cmd_shell.go` — add capsule invalidation warning
- `bitfs/internal/engine/move_test.go` — rewrite cross-directory tests
- `design/bitfs/2-SystemDesign.zh.md` line 215 — update mv description
- `design/bitfs/3-DetailedDesign.zh.md` lines 801-839 — update cross-dir mv flow
- `bitfs/spec/10-cmd-bitfs.md` — update mv behavior spec

**Out of scope:**
- Same-directory rename (unchanged)
- Recursive subtree mv (user moves files individually)
- Capsule migration/refund mechanism (future feature)

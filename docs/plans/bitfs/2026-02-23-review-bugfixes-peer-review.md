# Peer Review Results: fix/review-bugfixes Branch

**Date**: 2026-02-23
**Reviewers**: OpenAI Codex (gpt-5.3-codex), Google Gemini CLI (0.28.2)
**Branch**: fix/review-bugfixes (10 commits, 5 bugfixes)

## Critical Findings (Both reviewers agree)

### [P1] Remove mutates parent state before tx is built

**File**: `internal/engine/remove.go:102-104`

**Problem**: `Remove()` modifies `parent.Children` in memory *before* calling `buildParentSelfUpdate`. If the parent tx build fails (e.g. UTXO exhaustion, signing error), local state diverges from what was actually broadcast. The parent's child list shows the entry removed, but no parent update transaction was produced.

**Root Cause**: Same class of bug that Fix 4 (Move validate-before-broadcast) already solved — state mutation happens before transaction build confirmation.

**Recommendation**: Refactor `Engine.Remove()` to use the same "build-then-apply" two-phase pattern from Move:
1. Build the node deletion TX
2. Prepare the new children slice (without mutating the original)
3. Build the parent update TX using the prepared slice
4. Only if both TXs succeed, apply the state mutations

**Codex quote**:
> "The remove flow now mutates parent directory state before confirming the parent update transaction can be built, and does not roll back on failure. This can leave in-memory filesystem state inconsistent with the transactions returned to callers."

## High Findings

### [P2] No failure/rollback test for Move validate-before-broadcast

**File**: `internal/engine/move.go` tests

**Problem**: The design doc specifies the need for a test that "simulates second TX build failure and verifies state is unchanged." However, the implementation plan only runs existing tests (success path). Without a failure/rollback test, the fix cannot be verified as effective.

**Recommendation**: Write a test for `crossDirectoryMove` that:
1. Induces a failure during the second TX build (e.g. no fee UTXO available)
2. Asserts that engine state (parent child lists, paths) remains unchanged from pre-call state

**Gemini suggestion** — use `defer` for robustness:
```go
origSrcChildren := srcParent.Children
defer func() { srcParent.Children = origSrcChildren }()
srcParent.Children = srcChildrenAfter
srcTxHex, srcTxID, err := e.buildParentSelfUpdate(srcParent)
```

### [P3] `put.go` not checked for metadata population on creation

**File**: `internal/engine/put.go`

**Problem**: Metadata fields (Keywords, Description, Domain, OnChain, Compression) are now preserved during Copy/Sell/Encrypt operations. However, no one has verified that `put.go` (initial file creation) actually populates these fields. If they're never set on creation, preserving them on mutation is meaningless.

**Recommendation**:
1. Investigate `put.go` file creation logic
2. Ensure options exist to specify extended metadata on creation
3. Update logic to save metadata to `NodeState`
4. Add test verifying metadata is saved during initial `put`

## Medium Findings

### [P4] Missing directory removal edge case tests

**File**: `internal/engine/remove.go` tests

**Problem**: Tests cover file removal but not directory removal edge cases.

**Recommendation**: Add tests for:
- Attempting to remove a non-empty directory (should fail)
- Removing an empty directory (should succeed)

## Low Findings

### [P5] `io.ReadAll` in bcat/bget for free content decryption

**Files**: `cmd/bcat/main.go`, `cmd/bget/main.go`

**Problem**: Free content decryption reads entire file into memory via `io.ReadAll`. Works but doesn't scale for large files.

**Recommendation**: Acknowledge as known limitation. Consider streaming `io.Reader`/`io.Writer` interface for `method42` in a future enhancement. Current approach is correct for the bug fix.

### [P6] Consider `defer`/cloning for Move's crossDirectoryMove

**File**: `internal/engine/move.go`

**Problem**: Temporary variable swap to manage children slices is functional but fragile.

**Recommendation**: Use `defer` or full slice cloning for guaranteed rollback even on unexpected panics.

## Action Items (Ordered by Priority)

1. **Fix Remove build-then-apply** [P1] — Apply the same pattern from Move to Remove
2. **Add Move failure/rollback test** [P2] — New test case with induced TX build failure
3. **Verify put.go metadata population** [P3] — Check if extended metadata is set on creation
4. **Add directory removal tests** [P4] — Edge cases for empty/non-empty directories
5. *(Deferred)* Streaming decryption [P5]
6. *(Deferred)* Defer-based rollback in Move [P6]

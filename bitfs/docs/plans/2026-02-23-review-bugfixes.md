# Review Bugfixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 5 issues found during external code review: Remove parent cleanup, free content decryption, metadata preservation, Move atomicity, and spec doc paths.

**Architecture:** Each fix is self-contained. Tasks ordered by severity (critical first). Engine fixes share test infrastructure via `initTestEngine()` + `addFeeUTXO()`. CLI fixes (bcat/bget) use mock HTTP servers with Method 42 encryption.

**Tech Stack:** Go 1.25.6, `libbitfs/method42`, `libbitfs/metanet`, `testify`

---

### Task 1: Extract `buildParentSelfUpdate` to shared helper

This helper is currently in `move.go` and will be needed by `remove.go` too.

**Files:**
- Modify: `internal/engine/move.go` (remove `buildParentSelfUpdate` and `resolveParentDir`)
- Create: `internal/engine/dir_update.go` (new home for shared functions)

**Step 1: Create `dir_update.go` with extracted functions**

Move `buildParentSelfUpdate()` (lines 183-257) and `resolveParentDir()` (lines 159-177) from `move.go` into a new file `dir_update.go`. No logic changes — just relocation.

```go
// internal/engine/dir_update.go
package engine

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tongxiaofeng/libbitfs/metanet"
)

// resolveParentDir finds the parent directory node for a given directory path.
// Handles the root directory case (path "/" or ".").
func (e *Engine) resolveParentDir(dirPath string, vaultIdx uint32) (*NodeState, error) {
	parent := e.State.FindNodeByPath(dirPath)
	if parent != nil {
		return parent, nil
	}

	if dirPath == "/" || dirPath == "." {
		rootPubHex, err := e.getRootPubHex(vaultIdx)
		if err != nil {
			return nil, err
		}
		parent = e.State.GetNode(rootPubHex)
		if parent != nil {
			return parent, nil
		}
	}

	return nil, fmt.Errorf("directory %q not found", dirPath)
}

// buildParentSelfUpdate builds and signs a SelfUpdate transaction for a parent
// directory node, reflecting its current children list. It allocates a fee UTXO,
// derives a change address, and tracks the resulting UTXOs.
// Returns the signed tx hex and tx ID hex.
func (e *Engine) buildParentSelfUpdate(parent *NodeState) (txHex string, txIDHex string, err error) {
	// (exact same code as currently in move.go lines 183-257)
	// ...
}
```

**Step 2: Remove the two functions from `move.go`**

Delete `resolveParentDir` (lines 157-177) and `buildParentSelfUpdate` (lines 179-257) from `move.go`. The `move.go` file should only contain `Move()`, `crossDirectoryMove()`.

**Step 3: Run tests to verify no regressions**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -run TestMove -count=1`
Expected: All 8 TestMove_* tests pass.

**Step 4: Commit**

```bash
git add internal/engine/dir_update.go internal/engine/move.go
git commit -m "refactor: extract buildParentSelfUpdate to dir_update.go"
```

---

### Task 2: Fix Remove to update parent directory

**Files:**
- Modify: `internal/engine/remove.go`
- Modify: `internal/engine/engine_test.go` (existing remove tests)

**Step 1: Write failing test**

Add to `engine_test.go` after `TestRemove_NodeNotFound`:

```go
func TestRemove_UpdatesParentChildList(t *testing.T) {
	eng, _ := setupCopyTestEngine(t) // gives us root + /test.txt

	// Verify the file exists in parent's children.
	root := eng.State.FindNodeByPath("/")
	if root == nil {
		// root might be stored by pubkey only
		rootPubHex, _ := eng.getRootPubHex(0)
		root = eng.State.GetNode(rootPubHex)
	}
	require.NotNil(t, root)

	found := false
	for _, c := range root.Children {
		if c.Name == "test.txt" {
			found = true
		}
	}
	require.True(t, found, "test.txt should be in root children before remove")

	// Add fee UTXOs for both txs (node delete + parent update).
	addFeeUTXO(t, eng, 100000)
	addFeeUTXO(t, eng, 100000)

	result, err := eng.Remove(&RemoveOpts{VaultIndex: 0, Path: "/test.txt"})
	require.NoError(t, err)
	assert.NotEmpty(t, result.TxHex)
	assert.Contains(t, result.Message, "Removed")

	// Result should contain 2 txs (newline-separated).
	txParts := strings.Split(result.TxHex, "\n")
	assert.Len(t, txParts, 2, "Remove should produce 2 txs: node delete + parent update")

	// Parent's children list should no longer contain test.txt.
	rootPubHex, _ := eng.getRootPubHex(0)
	rootAfter := eng.State.GetNode(rootPubHex)
	for _, c := range rootAfter.Children {
		assert.NotEqual(t, "test.txt", c.Name, "test.txt should be removed from parent children")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -run TestRemove_UpdatesParentChildList -count=1`
Expected: FAIL — currently Remove doesn't produce 2 txs and doesn't update parent.

**Step 3: Implement the fix in `remove.go`**

Update `Engine.Remove()` to also update the parent directory:

```go
func (e *Engine) Remove(opts *RemoveOpts) (*Result, error) {
	// Find the node.
	nodeState := e.State.FindNodeByPath(opts.Path)
	if nodeState == nil {
		return nil, fmt.Errorf("engine: node %q not found", opts.Path)
	}

	// Derive key pair.
	kp, err := e.Wallet.DeriveNodeKey(nodeState.VaultIndex, nodeState.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive key: %w", err)
	}

	// Build delete payload.
	node := &metanet.Node{
		Version:   1,
		Type:      metanet.NodeType(nodeTypeInt(nodeState.Type)),
		Op:        metanet.OpDelete,
		Timestamp: uint64(time.Now().Unix()),
	}

	payload, err := metanet.SerializePayload(node)
	if err != nil {
		return nil, fmt.Errorf("engine: serialize payload: %w", err)
	}

	// Get parent TxID.
	var parentTxID []byte
	if nodeState.ParentTxID != "" {
		parentTxID, err = TxIDBytes(nodeState.ParentTxID)
		if err != nil {
			return nil, err
		}
	}

	// Get node UTXO.
	nodeUTXO, err := e.getNodeUTXO(nodeState.PubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("engine: node UTXO: %w", err)
	}

	changeAddr, changePriv, err := e.DeriveChangeAddr()
	if err != nil {
		return nil, err
	}
	changePubHex := hex.EncodeToString(changePriv.PubKey().Compressed())

	feeUTXO, err := e.AllocateFeeUTXO(2000)
	if err != nil {
		return nil, err
	}

	mtx, err := buildUnsignedSelfUpdateTx(kp, parentTxID, payload, nodeUTXO, feeUTXO, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("engine: build self-update tx: %w", err)
	}

	txHex, err := signSelfUpdateTx(mtx, nodeUTXO, feeUTXO)
	if err != nil {
		return nil, fmt.Errorf("engine: sign self-update tx: %w", err)
	}

	txIDHex := hex.EncodeToString(mtx.TxID)

	// Update local state for node.
	nodeState.TxID = txIDHex
	e.TrackNewUTXOs(mtx, nodeState.PubKeyHex, changePubHex)

	// --- NEW: Update parent directory to remove child entry ---
	parentDir := path.Dir(opts.Path)
	parent, err := e.resolveParentDir(parentDir, opts.VaultIndex)
	if err != nil {
		// If parent not found, return node-only result (best effort).
		return &Result{
			TxHex:   txHex,
			TxID:    txIDHex,
			Message: fmt.Sprintf("Removed %s (parent update skipped: %v)", opts.Path, err),
			NodePub: nodeState.PubKeyHex,
		}, nil
	}

	// Remove child from parent's children list.
	childName := path.Base(opts.Path)
	for i, c := range parent.Children {
		if c.Name == childName {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			break
		}
	}

	// Build parent SelfUpdate tx.
	parentTxHex, parentTxIDHex, err := e.buildParentSelfUpdate(parent)
	if err != nil {
		// Return node-only result if parent update fails.
		return &Result{
			TxHex:   txHex,
			TxID:    txIDHex,
			Message: fmt.Sprintf("Removed %s (parent update failed: %v)", opts.Path, err),
			NodePub: nodeState.PubKeyHex,
		}, nil
	}
	parent.TxID = parentTxIDHex

	return &Result{
		TxHex:   txHex + "\n" + parentTxHex,
		TxID:    parentTxIDHex,
		Message: fmt.Sprintf("Removed %s", opts.Path),
		NodePub: nodeState.PubKeyHex,
	}, nil
}
```

Add `"path"` to the imports in `remove.go`.

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -run TestRemove -count=1`
Expected: All remove tests pass.

**Step 5: Run all engine tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Expected: All pass.

**Step 6: Commit**

```bash
git add internal/engine/remove.go internal/engine/engine_test.go
git commit -m "fix: Remove now updates parent directory child list"
```

---

### Task 3: Fix bcat free content decryption

**Files:**
- Modify: `cmd/bcat/main.go`
- Modify: `cmd/bcat/main_test.go`

**Step 1: Update existing free-content tests to use encrypted data**

The mock daemon currently serves raw plaintext for free content. After the fix, `outputContent` will decrypt it, so the mock must serve properly encrypted data.

Update `TestFreeContent_OutputToStdout` in `main_test.go`:

```go
func TestFreeContent_OutputToStdout(t *testing.T) {
	plaintext := []byte("Hello, BitFS world!\nSecond line.\n")

	// Encrypt content with Method 42 AccessFree using testPubKey.
	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/hello.txt",
				MimeType: "text/plain",
				FileSize: uint64(len(plaintext)),
				KeyHash:  keyHashHex,
				Access:   "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	assert.Equal(t, plaintext, stdout.Bytes(), "stdout should contain decrypted plaintext")
}
```

Apply the same pattern to: `TestFreeContent_BinaryData`, `TestFreeContent_LargeFile`, `TestFreeContent_DataEndpointCalled`. For `TestFreeContent_EmptyFile`, handle as a special case (empty ciphertext → empty output, or skip decryption if ciphertext is empty).

For `TestFreeContent_WriteError` and `TestDataEndpoint_ServerError` and `TestDataEndpoint_NotFound`, no changes needed (they test error paths before decryption runs).

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./cmd/bcat/ -v -run TestFreeContent_OutputToStdout -count=1`
Expected: FAIL — `outputContent` still outputs raw ciphertext.

**Step 3: Implement free decryption in `outputContent()`**

Replace `outputContent` in `cmd/bcat/main.go`:

```go
// outputContent fetches encrypted data by key_hash, decrypts it using Method 42
// (free mode: D_node = scalar 1), and writes plaintext to stdout.
func outputContent(c *client.Client, meta *client.MetaResponse, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		fmt.Fprintf(stderr, "bcat: no content hash available\n")
		return 1
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleError(err, stderr)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(reader)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: read error: %v\n", err)
		return 4
	}

	// Empty content — nothing to decrypt.
	if len(ciphertext) == 0 {
		return 0
	}

	// Decode the node's public key for Method 42 free-mode decryption.
	pubKeyBytes, err := hex.DecodeString(meta.PNode)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid pnode hex: %v\n", err)
		return 1
	}
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid pnode key: %v\n", err)
		return 1
	}

	keyHashBytes, err := hex.DecodeString(meta.KeyHash)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid key hash hex: %v\n", err)
		return 1
	}

	// Decrypt: nil private key triggers FreePrivateKey() (scalar 1).
	result, err := method42.Decrypt(ciphertext, nil, pubKey, keyHashBytes, method42.AccessFree)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: decrypt: %v\n", err)
		return 5
	}

	if _, err := stdout.Write(result.Plaintext); err != nil {
		fmt.Fprintf(stderr, "bcat: write error: %v\n", err)
		return 1
	}
	return 0
}
```

Add `ec "github.com/bsv-blockchain/go-sdk/primitives/ec"` to imports (already imported).

**Step 4: Run bcat tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./cmd/bcat/ -v -count=1`
Expected: All pass. Update any other tests that serve raw content to serve encrypted content.

**Step 5: Commit**

```bash
git add cmd/bcat/main.go cmd/bcat/main_test.go
git commit -m "fix: bcat decrypts free content using Method 42"
```

---

### Task 4: Fix bget free content decryption

**Files:**
- Modify: `cmd/bget/main.go`
- Modify: `cmd/bget/main_test.go`

Same pattern as Task 3 but for bget.

**Step 1: Update `downloadContent()` in bget**

```go
func downloadContent(c *client.Client, meta *client.MetaResponse, outputName string, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		fmt.Fprintf(stderr, "bget: no content hash available\n")
		return 1
	}

	filename := outputName
	if filename == "" {
		filename = deriveFilename(meta.Path)
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleError(err, stderr)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(reader)
	if err != nil {
		fmt.Fprintf(stderr, "bget: read error: %v\n", err)
		return 4
	}

	// Decrypt using Method 42 free mode.
	var plaintext []byte
	if len(ciphertext) > 0 {
		pubKeyBytes, err := hex.DecodeString(meta.PNode)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid pnode hex: %v\n", err)
			return 1
		}
		pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid pnode key: %v\n", err)
			return 1
		}

		keyHashBytes, err := hex.DecodeString(meta.KeyHash)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid key hash hex: %v\n", err)
			return 1
		}

		result, err := method42.Decrypt(ciphertext, nil, pubKey, keyHashBytes, method42.AccessFree)
		if err != nil {
			fmt.Fprintf(stderr, "bget: decrypt: %v\n", err)
			return 5
		}
		plaintext = result.Plaintext
	}

	file, err := os.Create(filename)
	if err != nil {
		fmt.Fprintf(stderr, "bget: cannot create file %q: %v\n", filename, err)
		return 1
	}

	n, err := file.Write(plaintext)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(filename)
		fmt.Fprintf(stderr, "bget: write error: %v\n", err)
		return 1
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(filename)
		fmt.Fprintf(stderr, "bget: close error: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Downloaded %d bytes to %s\n", n, filename)
	return 0
}
```

Add `ec "github.com/bsv-blockchain/go-sdk/primitives/ec"` to imports if not present.

**Step 2: Update bget free-content tests to serve encrypted data**

Same pattern as Task 3: encrypt with `method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)` and serve ciphertext. Update `TestFreeContent_DefaultFilename`, `TestFreeContent_CustomOutputFilename`, `TestFreeContent_BinaryData`, `TestFreeContent_LargeFile`, `TestFreeContent_ByteCountInMessage`.

**Step 3: Run bget tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./cmd/bget/ -v -count=1`
Expected: All pass.

**Step 4: Commit**

```bash
git add cmd/bget/main.go cmd/bget/main_test.go
git commit -m "fix: bget decrypts free content using Method 42"
```

---

### Task 5: Extend NodeState with metadata fields

**Files:**
- Modify: `internal/engine/state.go`

**Step 1: Add metadata fields to NodeState**

Add after `Metadata` field (line 44):

```go
Keywords    string `json:"keywords,omitempty"`
Description string `json:"description,omitempty"`
Domain      string `json:"domain,omitempty"`
OnChain     bool   `json:"on_chain,omitempty"`
Compression int32  `json:"compression,omitempty"`
```

**Step 2: Run all tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -count=1`
Expected: All pass (additive change only).

**Step 3: Commit**

```bash
git add internal/engine/state.go
git commit -m "feat: add extended metadata fields to NodeState"
```

---

### Task 6: Preserve metadata in Copy, Sell, Encrypt

**Files:**
- Modify: `internal/engine/copy.go`
- Modify: `internal/engine/sell.go`
- Modify: `internal/engine/encrypt.go`

**Step 1: Write failing test for Copy metadata preservation**

Add to `copy_test.go`:

```go
func TestCopy_PreservesExtendedMetadata(t *testing.T) {
	eng, _ := setupCopyTestEngine(t)

	// Manually set extended metadata on the source node.
	srcNode := eng.State.FindNodeByPath("/test.txt")
	require.NotNil(t, srcNode)
	srcNode.Keywords = "test,example"
	srcNode.Description = "A test file"
	srcNode.Domain = "example.com"

	addFeeUTXO(t, eng, 100000)

	_, err := eng.Copy(&CopyOpts{
		VaultIndex: 0,
		SrcPath:    "/test.txt",
		DstPath:    "/copy.txt",
	})
	require.NoError(t, err)

	dstNode := eng.State.FindNodeByPath("/copy.txt")
	require.NotNil(t, dstNode)
	assert.Equal(t, "test,example", dstNode.Keywords)
	assert.Equal(t, "A test file", dstNode.Description)
	assert.Equal(t, "example.com", dstNode.Domain)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -run TestCopy_PreservesExtendedMetadata -count=1`
Expected: FAIL.

**Step 3: Fix Copy to preserve metadata**

In `copy.go`, after line 122 (closing `}` of the node struct), add:

```go
	// Preserve extended metadata from source.
	if srcNode.Keywords != "" {
		node.Keywords = srcNode.Keywords
	}
	if srcNode.Description != "" {
		node.Description = srcNode.Description
	}
	if srcNode.Domain != "" {
		node.Domain = srcNode.Domain
	}
	if srcNode.OnChain {
		node.OnChain = srcNode.OnChain
	}
	if srcNode.Compression != 0 {
		node.Compression = srcNode.Compression
	}
```

In `copy.go`, in the childState construction (after line 176), add:

```go
	if srcNode.Keywords != "" {
		childState.Keywords = srcNode.Keywords
	}
	if srcNode.Description != "" {
		childState.Description = srcNode.Description
	}
	if srcNode.Domain != "" {
		childState.Domain = srcNode.Domain
	}
	childState.OnChain = srcNode.OnChain
	childState.Compression = srcNode.Compression
```

**Step 4: Fix Sell to preserve metadata**

In `sell.go`, after the existing `if nodeState.FileSize > 0` block (line 47), add:

```go
	if nodeState.Keywords != "" {
		node.Keywords = nodeState.Keywords
	}
	if nodeState.Description != "" {
		node.Description = nodeState.Description
	}
	if nodeState.Domain != "" {
		node.Domain = nodeState.Domain
	}
	if nodeState.OnChain {
		node.OnChain = nodeState.OnChain
	}
	if nodeState.Compression != 0 {
		node.Compression = nodeState.Compression
	}
```

**Step 5: Fix Encrypt to preserve metadata**

In `encrypt.go`, after the existing `if nodeState.FileSize > 0` block (line 72), add the same block as Sell.

**Step 6: Run all tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Expected: All pass.

**Step 7: Commit**

```bash
git add internal/engine/copy.go internal/engine/sell.go internal/engine/encrypt.go internal/engine/copy_test.go
git commit -m "fix: preserve extended metadata in Copy, Sell, Encrypt operations"
```

---

### Task 7: Fix Move validate-before-broadcast

**Files:**
- Modify: `internal/engine/move.go`
- Modify: `internal/engine/move_test.go`

**Step 1: Restructure `crossDirectoryMove` to build-then-apply**

Replace the current `crossDirectoryMove` in `move.go`:

```go
func (e *Engine) crossDirectoryMove(opts *MoveOpts, nodeState *NodeState) (*Result, error) {
	srcDir := path.Dir(opts.SrcPath)
	dstDir := path.Dir(opts.DstPath)
	srcName := path.Base(opts.SrcPath)
	dstName := path.Base(opts.DstPath)

	// 1. Find source parent directory.
	srcParent, err := e.resolveParentDir(srcDir, opts.VaultIndex)
	if err != nil {
		return nil, fmt.Errorf("engine: source directory %q: %w", srcDir, err)
	}

	// 2. Find destination parent directory.
	dstParent, err := e.resolveParentDir(dstDir, opts.VaultIndex)
	if err != nil {
		return nil, fmt.Errorf("engine: destination directory %q: %w", dstDir, err)
	}

	// 3. Check destination doesn't already have this name.
	for _, c := range dstParent.Children {
		if c.Name == dstName {
			return nil, fmt.Errorf("engine: %q already exists in %q", dstName, dstDir)
		}
	}

	// 4. Find the child entry in source parent (don't remove yet).
	var movedChild *ChildState
	var srcChildIdx int
	for i, c := range srcParent.Children {
		if c.Name == srcName {
			movedChild = c
			srcChildIdx = i
			break
		}
	}
	if movedChild == nil {
		return nil, fmt.Errorf("engine: %q not found in source directory", srcName)
	}

	// --- Phase 1: Build both TXs without mutating state ---

	// Build source parent children list (without the moved child).
	srcChildrenAfter := make([]*ChildState, 0, len(srcParent.Children)-1)
	srcChildrenAfter = append(srcChildrenAfter, srcParent.Children[:srcChildIdx]...)
	srcChildrenAfter = append(srcChildrenAfter, srcParent.Children[srcChildIdx+1:]...)

	// Temporarily swap children for build.
	origSrcChildren := srcParent.Children
	srcParent.Children = srcChildrenAfter
	srcTxHex, srcTxID, err := e.buildParentSelfUpdate(srcParent)
	srcParent.Children = origSrcChildren // restore
	if err != nil {
		return nil, fmt.Errorf("engine: update source parent: %w", err)
	}

	// Build destination parent children list (with the moved child).
	newChild := &ChildState{
		Name:     dstName,
		Type:     movedChild.Type,
		PubKey:   movedChild.PubKey,
		Index:    movedChild.Index,
		Hardened: movedChild.Hardened,
	}
	dstChildrenAfter := make([]*ChildState, len(dstParent.Children)+1)
	copy(dstChildrenAfter, dstParent.Children)
	dstChildrenAfter[len(dstParent.Children)] = newChild

	origDstChildren := dstParent.Children
	dstParent.Children = dstChildrenAfter
	dstTxHex, dstTxID, err := e.buildParentSelfUpdate(dstParent)
	dstParent.Children = origDstChildren // restore
	if err != nil {
		return nil, fmt.Errorf("engine: update destination parent: %w", err)
	}

	// --- Phase 2: Both builds succeeded — apply state ---
	srcParent.Children = srcChildrenAfter
	srcParent.TxID = srcTxID
	dstParent.Children = dstChildrenAfter
	dstParent.TxID = dstTxID
	nodeState.Path = opts.DstPath

	return &Result{
		TxHex:   srcTxHex + "\n" + dstTxHex,
		TxID:    dstTxID,
		Message: fmt.Sprintf("Moved %s -> %s (2 txs: src=%s, dst=%s)", opts.SrcPath, opts.DstPath, srcTxID[:8], dstTxID[:8]),
		NodePub: nodeState.PubKeyHex,
	}, nil
}
```

Note: `buildParentSelfUpdate` allocates fee UTXOs internally. The temporary children swap ensures it serializes the right payload. After restore, if the second build fails, state is unchanged.

**Step 2: Run all move tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -run TestMove -count=1`
Expected: All pass.

**Step 3: Commit**

```bash
git add internal/engine/move.go
git commit -m "fix: Move validates both TXs before mutating local state"
```

---

### Task 8: Update spec doc path references

**Files:**
- Modify: `spec/01-method42.md` through `spec/11-cmd-btools.md` (12 files)
- Modify: `spec/TASKS.md`

**Step 1: Batch replace across all spec files**

For each mapping, do a find-and-replace across all files in `spec/`:
- `internal/method42` → `libbitfs/method42`
- `internal/wallet` → `libbitfs/wallet`
- `internal/tx` → `libbitfs/tx`
- `internal/metanet` → `libbitfs/metanet`
- `internal/spv` → `libbitfs/spv`
- `internal/storage` → `libbitfs/storage`
- `internal/paymail` → `libbitfs/paymail`
- `internal/x402` → `libbitfs/x402`
- `internal/config` → `libbitfs/config`

Use `sed` or editor find-replace. Verify no over-matching (e.g., `internal/daemon` should NOT be changed — it's still in `bitfs/internal/`).

**Step 2: Verify replacements**

Run: `grep -r "internal/method42\|internal/wallet\|internal/tx\|internal/metanet\|internal/spv\|internal/storage\|internal/paymail\|internal/x402\|internal/config" spec/`
Expected: No matches (all replaced).

Run: `grep -r "internal/daemon\|internal/client\|internal/engine" spec/`
Expected: These should remain unchanged (they are still in bitfs/internal/).

**Step 3: Commit**

```bash
git add spec/
git commit -m "docs: update spec module paths from internal/ to libbitfs/"
```

---

### Task 9: Run full test suite and verify

**Step 1: Run all tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1`
Expected: All pass.

**Step 2: Run linter**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && golangci-lint run ./...`
Expected: No new issues.

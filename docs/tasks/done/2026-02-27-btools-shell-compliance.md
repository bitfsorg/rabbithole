# B* Tools + Shell Full Spec Compliance — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bring b* tools and bitfs shell to full spec compliance across 3 phases: critical fixes, caching infrastructure, daemon enhancements.

**Architecture:** Phase 1 fixes shell gaps using existing engine patterns. Phase 2 adds a metadata cache layer in `internal/client/` and wires `--no-cache`/`--offline`/`--keyword` flags into all tools. Phase 3 adds daemon endpoints for version history and sales, replacing tool stubs.

**Tech Stack:** Go 1.25.6, `github.com/tongxiaofeng/libbitfs-go` (method42, metanet, tx, storage), `github.com/stretchr/testify`

**Design doc:** `docs/plans/2026-02-27-btools-shell-compliance-design.md`

---

## Phase 1: Critical Fixes + Shell Gaps

### Task 1: Fix bmget unbounded read

**Files:**
- Modify: `cmd/bmget/main.go:308,378`

**Step 1: Add io.LimitReader to downloadFreeFile**

In `downloadFreeFile`, line 308, change:
```go
ciphertext, err := io.ReadAll(reader)
```
to:
```go
ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
```

Add `maxContentSize` constant at file top (check if it exists already — bcat uses `const maxContentSize = 1 << 30` for 1GB):
```go
const maxContentSize = 1 << 30 // 1 GB
```

**Step 2: Add io.LimitReader to downloadPaidFile**

In `downloadPaidFile`, line 378, same change:
```go
ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
```

**Step 3: Run tests**

Run: `go test ./cmd/bmget/ -v -count=1`
Expected: All existing tests pass

**Step 4: Commit**

```bash
git add cmd/bmget/main.go
git commit -m "fix(bmget): add io.LimitReader to prevent unbounded reads"
```

---

### Task 2: Engine Decrypt method

**Files:**
- Create: `internal/engine/decrypt.go`
- Create: `internal/engine/decrypt_test.go`

**Step 1: Write failing test**

Create `internal/engine/decrypt_test.go`:
```go
package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecryptNode(t *testing.T) {
	eng := newTestEngine(t)

	// Create a file and encrypt it first.
	_, err := eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  createTempFile(t, "hello decrypt"),
		RemotePath: "/test-decrypt.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.EncryptNode(&EncryptOpts{VaultIndex: 0, Path: "/test-decrypt.txt"})
	require.NoError(t, err)

	// Verify it's now private.
	ns := eng.State.FindNodeByPath("/test-decrypt.txt")
	require.NotNil(t, ns)
	assert.Equal(t, "private", ns.Access)

	// Decrypt it.
	result, err := eng.DecryptNode(&DecryptOpts{Path: "/test-decrypt.txt"})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "Decrypted")

	// Verify it's now free.
	ns = eng.State.FindNodeByPath("/test-decrypt.txt")
	assert.Equal(t, "free", ns.Access)
}

func TestDecryptNode_AlreadyFree(t *testing.T) {
	eng := newTestEngine(t)

	_, err := eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  createTempFile(t, "already free"),
		RemotePath: "/free-file.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.DecryptNode(&DecryptOpts{Path: "/free-file.txt"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already free")
}

func TestDecryptNode_NotFound(t *testing.T) {
	eng := newTestEngine(t)
	_, err := eng.DecryptNode(&DecryptOpts{Path: "/nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDecryptNode_Directory(t *testing.T) {
	eng := newTestEngine(t)

	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/somedir"})
	require.NoError(t, err)

	_, err = eng.DecryptNode(&DecryptOpts{Path: "/somedir"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a file")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestDecryptNode -v -count=1`
Expected: FAIL — `DecryptNode` not defined

**Step 3: Write implementation**

Create `internal/engine/decrypt.go` — mirror of `encrypt.go` with reversed access:
```go
package engine

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tongxiaofeng/libbitfs-go/metanet"
	"github.com/tongxiaofeng/libbitfs-go/method42"
	"github.com/tongxiaofeng/libbitfs-go/tx"
)

// DecryptOpts holds options for the Decrypt operation.
type DecryptOpts struct {
	Path string // remote path
}

// DecryptNode re-encrypts content from PRIVATE to FREE access.
func (e *Engine) DecryptNode(opts *DecryptOpts) (*Result, error) {
	nodeState := e.State.FindNodeByPath(opts.Path)
	if nodeState == nil {
		return nil, fmt.Errorf("engine: node %q not found", opts.Path)
	}

	if nodeState.Type != "file" {
		return nil, fmt.Errorf("engine: %q is not a file", opts.Path)
	}
	if nodeState.Access != "private" {
		return nil, fmt.Errorf("engine: %q is already %s", opts.Path, nodeState.Access)
	}

	kp, err := e.Wallet.DeriveNodeKey(nodeState.VaultIndex, nodeState.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive key: %w", err)
	}

	// Read current ciphertext from store.
	keyHash := mustDecodeHex(nodeState.KeyHash)
	ciphertext, err := e.Store.Get(keyHash)
	if err != nil {
		return nil, fmt.Errorf("engine: read content: %w", err)
	}

	// Re-encrypt PRIVATE -> FREE.
	reEncResult, err := method42.ReEncrypt(ciphertext, kp.PrivateKey, kp.PublicKey, keyHash, method42.AccessPrivate, method42.AccessFree)
	if err != nil {
		return nil, fmt.Errorf("engine: re-encrypt: %w", err)
	}

	// Store new ciphertext.
	if err := e.Store.Put(reEncResult.KeyHash, reEncResult.Ciphertext); err != nil {
		return nil, fmt.Errorf("engine: store re-encrypted: %w", err)
	}

	// Delete old ciphertext.
	_ = e.Store.Delete(keyHash)

	// Build SelfUpdate payload.
	node := &metanet.Node{
		Version:   1,
		Type:      metanet.NodeTypeFile,
		Op:        metanet.OpUpdate,
		Access:    metanet.AccessFree,
		KeyHash:   reEncResult.KeyHash,
		Timestamp: uint64(time.Now().Unix()),
	}
	if nodeState.MimeType != "" {
		node.MimeType = nodeState.MimeType
	}
	if nodeState.FileSize > 0 {
		node.FileSize = nodeState.FileSize
	}

	// Preserve extended metadata.
	node.Keywords = nodeState.Keywords
	node.Description = nodeState.Description
	node.Domain = nodeState.Domain
	node.OnChain = nodeState.OnChain
	node.Compression = nodeState.Compression

	payload, err := metanet.SerializePayload(node)
	if err != nil {
		return nil, fmt.Errorf("engine: serialize payload: %w", err)
	}

	var parentTxID []byte
	if nodeState.ParentTxID != "" {
		parentTxID, err = TxIDBytes(nodeState.ParentTxID)
		if err != nil {
			return nil, err
		}
	}

	nodeUTXO, nodeUS, err := e.getNodeUTXOWithState(nodeState.PubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("engine: node UTXO: %w", err)
	}

	changeAddr, changePriv, err := e.DeriveChangeAddr()
	if err != nil {
		nodeUS.Spent = false
		return nil, err
	}
	changePubHex := hex.EncodeToString(changePriv.PubKey().Compressed())

	feeUTXO, feeUS, err := e.AllocateFeeUTXOWithState(2000)
	if err != nil {
		nodeUS.Spent = false
		return nil, err
	}

	success := false
	defer func() {
		if !success {
			nodeUS.Spent = false
			feeUS.Spent = false
		}
	}()

	batch := tx.NewMutationBatch()
	batch.AddSelfUpdate(kp.PublicKey, parentTxID, payload, nodeUTXO, kp.PrivateKey)
	batch.AddFeeInput(feeUTXO)
	batch.SetChange(changeAddr)

	txHex, result, err := buildAndSignBatch(batch)
	if err != nil {
		return nil, fmt.Errorf("engine: batch decrypt tx: %w", err)
	}

	success = true
	txIDHex := hex.EncodeToString(result.TxID)

	// Update local state.
	nodeState.TxID = txIDHex
	nodeState.Access = "free"
	nodeState.KeyHash = hex.EncodeToString(reEncResult.KeyHash)
	e.TrackBatchUTXOs(result, []string{nodeState.PubKeyHex}, changePubHex)

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Decrypted %s (PRIVATE -> FREE)", opts.Path),
		NodePub: nodeState.PubKeyHex,
	}, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/engine/ -run TestDecryptNode -v -count=1`
Expected: All 4 tests pass

**Step 5: Run full engine tests**

Run: `go test ./internal/engine/ -v -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add internal/engine/decrypt.go internal/engine/decrypt_test.go
git commit -m "feat(engine): add DecryptNode — PRIVATE to FREE re-encryption"
```

---

### Task 3: Shell — decrypt, put access, rm -r, sell --recursive, link fix

**Files:**
- Modify: `cmd/bitfs/cmd_shell.go`

All shell changes in one task since they're small, co-located edits.

**Step 1: Add decrypt command**

After the `"encrypt"` case (line 482), add:
```go
	case "decrypt":
		if len(cmdArgs) < 1 {
			fmt.Println("Usage: decrypt <path>")
			continue
		}
		result, decErr := eng.DecryptNode(&engine.DecryptOpts{
			Path: resolvePath(cwd, cmdArgs[0]),
		})
		if decErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", decErr)
		} else {
			fmt.Println(result.Message)
		}
```

**Step 2: Add "decrypt" to shellCommands**

Line 45, add `"decrypt"` to the list:
```go
var shellCommands = []string{
	"ls", "cd", "lcd", "pwd", "cat", "get", "mget", "mput", "mkdir", "put", "rm", "mv", "cp",
	"link", "sell", "encrypt", "decrypt", "publish", "unpublish", "help", "quit", "exit",
}
```

**Step 3: Update put to accept access mode**

Replace the `"put"` case (lines 200-220) with:
```go
	case "put":
		if len(cmdArgs) < 2 {
			fmt.Println("Usage: put <local-file> <remote-path> [free|private]")
			continue
		}
		localFile := cmdArgs[0]
		if !filepath.IsAbs(localFile) {
			localFile = filepath.Join(localCwd, localFile)
		}
		remotePath := resolvePath(cwd, cmdArgs[1])
		access := "free"
		if len(cmdArgs) > 2 && cmdArgs[2] == "private" {
			access = "private"
		}
		result, putErr := eng.PutFile(&engine.PutOpts{
			VaultIndex: vaultIdx,
			LocalFile:  localFile,
			RemotePath: remotePath,
			Access:     access,
		})
		if putErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", putErr)
		} else {
			fmt.Println(result.Message)
		}
```

**Step 4: Add -r flag to rm**

Replace the `"rm"` case (lines 221-232) with:
```go
	case "rm":
		if len(cmdArgs) < 1 {
			fmt.Println("Usage: rm [-r] <path>")
			continue
		}
		recursive := false
		pathArgs := cmdArgs
		for i, a := range cmdArgs {
			if a == "-r" || a == "--recursive" {
				recursive = true
				pathArgs = append(cmdArgs[:i], cmdArgs[i+1:]...)
				break
			}
		}
		if len(pathArgs) < 1 {
			fmt.Println("Usage: rm [-r] <path>")
			continue
		}
		rmPath := resolvePath(cwd, pathArgs[0])
		if recursive {
			if err := shellRemoveRecursive(eng, vaultIdx, rmPath); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		} else {
			result, rmErr := eng.Remove(&engine.RemoveOpts{VaultIndex: vaultIdx, Path: rmPath})
			if rmErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", rmErr)
			} else {
				fmt.Println(result.Message)
			}
		}
```

Add the recursive helper function at end of file:
```go
// shellRemoveRecursive removes a path and all its children bottom-up.
func shellRemoveRecursive(eng *engine.Engine, vaultIdx uint32, path string) error {
	ns := eng.State.FindNodeByPath(path)
	if ns == nil {
		return fmt.Errorf("engine: node %q not found", path)
	}
	// Remove children first (depth-first).
	if ns.Type == "dir" {
		for _, child := range ns.Children {
			childPath := path + "/" + child.Name
			if err := shellRemoveRecursive(eng, vaultIdx, childPath); err != nil {
				return err
			}
		}
	}
	result, err := eng.Remove(&engine.RemoveOpts{VaultIndex: vaultIdx, Path: path})
	if err != nil {
		return err
	}
	fmt.Println(result.Message)
	return nil
}
```

**Step 5: Add --recursive to sell**

Replace the `"sell"` case (lines 300-320) with:
```go
	case "sell":
		if len(cmdArgs) < 2 {
			fmt.Println("Usage: sell <path> <price-sats-per-kb> [--recursive]")
			continue
		}
		recursive := false
		cleanArgs := make([]string, 0, len(cmdArgs))
		for _, a := range cmdArgs {
			if a == "-r" || a == "--recursive" {
				recursive = true
			} else {
				cleanArgs = append(cleanArgs, a)
			}
		}
		if len(cleanArgs) < 2 {
			fmt.Println("Usage: sell <path> <price-sats-per-kb> [--recursive]")
			continue
		}
		var price uint64
		if _, err := fmt.Sscanf(cleanArgs[1], "%d", &price); err != nil || price == 0 {
			fmt.Println("Error: price must be a positive integer")
			continue
		}
		sellPath := resolvePath(cwd, cleanArgs[0])
		if recursive {
			if err := shellSellRecursive(eng, vaultIdx, sellPath, price); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
		} else {
			result, sellErr := eng.Sell(&engine.SellOpts{
				VaultIndex: vaultIdx,
				Path:       sellPath,
				PricePerKB: price,
			})
			if sellErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", sellErr)
			} else {
				fmt.Println(result.Message)
			}
		}
```

Add recursive sell helper:
```go
// shellSellRecursive applies a price to a path and all file descendants.
func shellSellRecursive(eng *engine.Engine, vaultIdx uint32, path string, price uint64) error {
	ns := eng.State.FindNodeByPath(path)
	if ns == nil {
		return fmt.Errorf("engine: node %q not found", path)
	}
	if ns.Type == "dir" {
		for _, child := range ns.Children {
			childPath := path + "/" + child.Name
			if err := shellSellRecursive(eng, vaultIdx, childPath, price); err != nil {
				return err
			}
		}
		return nil // don't sell directories themselves
	}
	result, err := eng.Sell(&engine.SellOpts{
		VaultIndex: vaultIdx,
		Path:       path,
		PricePerKB: price,
	})
	if err != nil {
		return err
	}
	fmt.Println(result.Message)
	return nil
}
```

**Step 6: Fix link flag parsing**

Replace the `"link"` case (lines 283-299) with:
```go
	case "link":
		if len(cmdArgs) < 2 {
			fmt.Println("Usage: link <target> <link-path> [-s|--soft]")
			continue
		}
		soft := false
		posArgs := make([]string, 0, len(cmdArgs))
		for _, a := range cmdArgs {
			if a == "-s" || a == "--soft" {
				soft = true
			} else {
				posArgs = append(posArgs, a)
			}
		}
		if len(posArgs) < 2 {
			fmt.Println("Usage: link <target> <link-path> [-s|--soft]")
			continue
		}
		result, lnErr := eng.Link(&engine.LinkOpts{
			VaultIndex: vaultIdx,
			TargetPath: resolvePath(cwd, posArgs[0]),
			LinkPath:   resolvePath(cwd, posArgs[1]),
			Soft:       soft,
		})
		if lnErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", lnErr)
		} else {
			fmt.Println(result.Message)
		}
```

**Step 7: Update shellHelp**

Update the help text to reflect all changes:
```go
func shellHelp() {
	fmt.Println(`Available commands:
  ls [path]                     List directory contents
  cd [path]                     Change remote directory
  lcd [path]                    Change local directory (or print current)
  pwd                           Print remote working directory
  cat <path>                    View file contents (--force for binary)
  get <remote> [local]          Download file to local disk
  mget <dir> [local-dir]        Download directory recursively
  mput <dir> [remote-dir]       Upload directory recursively
  mkdir <path>                  Create directory
  put <local> <remote> [access] Upload file (access: free|private, default free)
  rm [-r] <path>                Remove file/directory (-r for recursive)
  mv <src> <dst>                Move/rename
  cp <src> <dst>                Copy file
  link <target> <path> [-s]     Create link (-s for soft/symlink)
  sell <path> <price> [-r]      Set price sats/KB (-r for recursive)
  encrypt <path>                Encrypt (FREE -> PRIVATE)
  decrypt <path>                Decrypt (PRIVATE -> FREE)
  publish [domain]              List or bind domain via DNSLink
  unpublish <domain>            Remove domain binding
  help                          Show this help
  quit                          Exit shell`)
}
```

**Step 8: Run tests**

Run: `go test ./cmd/bitfs/ -v -count=1`
Expected: All pass

**Step 9: Commit**

```bash
git add cmd/bitfs/cmd_shell.go
git commit -m "feat(shell): add decrypt, put access, rm -r, sell --recursive, fix link flags"
```

---

### Task 4: Phase 1 integration verification

**Step 1: Run full test suite**

Run: `go test ./... -count=1 -race`
Expected: All packages pass

**Step 2: Run integration tests**

Run: `go test -tags=integration ./integration/ -count=1 -race`
Expected: All 276 tests pass

**Step 3: Commit phase marker**

No commit needed — all code committed in Tasks 1-3.

---

## Phase 2: Metadata Cache + Tool Flags

### Task 5: Shared error helpers

**Files:**
- Create: `internal/buyer/exit.go`
- Modify: `cmd/bcat/main.go`, `cmd/bget/main.go`, `cmd/bls/main.go`, `cmd/bstat/main.go`, `cmd/btree/main.go`, `cmd/bmget/main.go`

**Step 1: Create shared error helpers**

Create `internal/buyer/exit.go`:
```go
package buyer

import (
	"errors"
	"fmt"
	"io"

	"github.com/tongxiaofeng/bitfs/internal/client"
)

// ExitCodeFromError maps a client error to a CLI exit code.
// 0=success, 1=general, 2=not found, 4=network/timeout, 5=payment required, 6=usage.
func ExitCodeFromError(err error) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		return 2
	case errors.Is(err, client.ErrTimeout), errors.Is(err, client.ErrNetwork), errors.Is(err, client.ErrServer):
		return 4
	case errors.Is(err, client.ErrPaymentRequired):
		return 5
	default:
		return 1
	}
}

// ErrorMessage returns a short human-readable message for a client error.
func ErrorMessage(err error) string {
	switch {
	case errors.Is(err, client.ErrNotFound):
		return "not found"
	case errors.Is(err, client.ErrTimeout):
		return "request timeout"
	case errors.Is(err, client.ErrNetwork):
		return "network error"
	case errors.Is(err, client.ErrServer):
		return "server error"
	default:
		return err.Error()
	}
}

// HandleError prints a formatted error to stderr and returns the exit code.
// The toolName is prefixed to the message (e.g. "bcat", "bget").
func HandleError(err error, toolName string, stderr io.Writer) int {
	code := ExitCodeFromError(err)
	msg := ErrorMessage(err)
	if code == 4 && !errors.Is(err, client.ErrTimeout) {
		// For network/server errors, include the underlying message.
		fmt.Fprintf(stderr, "%s: %s: %v\n", toolName, msg, err)
	} else {
		fmt.Fprintf(stderr, "%s: %s\n", toolName, msg)
	}
	return code
}
```

**Step 2: Run test to ensure buyer package still compiles**

Run: `go test ./internal/buyer/ -v -count=1`
Expected: Pass

**Step 3: Replace error helpers in each tool**

In each of the 6 tools, delete the local `errorToCode`, `errorMessage`, `handleError` functions and replace calls with `buyer.ExitCodeFromError`, `buyer.ErrorMessage`, `buyer.HandleError`.

Example for bcat — replace `handleError(err, stderr)` calls with `buyer.HandleError(err, "bcat", stderr)`, and `handleErrorJSON(err, stdout)` with:
```go
func handleErrorJSON(err error, stdout io.Writer) int {
	code := buyer.ExitCodeFromError(err)
	resp := &buyer.ErrorResponse{Error: buyer.ErrorMessage(err), Code: code}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(stdout, string(data))
	return code
}
```

Keep `handleErrorJSON` local to each tool since it uses `json.Marshal` + tool-specific output patterns.

**Step 4: Run all tool tests**

Run: `go test ./cmd/... -count=1`
Expected: All pass

**Step 5: Commit**

```bash
git add internal/buyer/exit.go cmd/bcat/main.go cmd/bget/main.go cmd/bls/main.go cmd/bstat/main.go cmd/btree/main.go cmd/bmget/main.go
git commit -m "refactor: extract shared error helpers to buyer.ExitCodeFromError/HandleError"
```

---

### Task 6: MetaCache layer

**Files:**
- Create: `internal/client/cache.go`
- Create: `internal/client/cache_test.go`

**Step 1: Write failing tests**

Create `internal/client/cache_test.go`:
```go
package client

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetaCache_PutGet(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	resp := &MetaResponse{
		PNode:  "02abc123",
		Type:   "file",
		Path:   "/hello.txt",
		Access: "free",
	}

	cache.Put("02abc123", "/hello.txt", resp)

	got, err := cache.Get("02abc123", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "02abc123", got.PNode)
	assert.Equal(t, "/hello.txt", got.Path)
}

func TestMetaCache_Miss(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	got, err := cache.Get("nonexistent", "/path")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestMetaCache_Expired(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 1*time.Millisecond)

	resp := &MetaResponse{PNode: "02abc", Type: "file", Path: "/x"}
	cache.Put("02abc", "/x", resp)

	time.Sleep(5 * time.Millisecond)

	got, err := cache.Get("02abc", "/x")
	assert.NoError(t, err)
	assert.Nil(t, got) // expired
}

func TestMetaCache_Invalidate(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	resp := &MetaResponse{PNode: "02abc", Type: "file", Path: "/x"}
	cache.Put("02abc", "/x", resp)

	cache.Invalidate("02abc", "/x")

	got, err := cache.Get("02abc", "/x")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestMetaCache_CreatesSubdirs(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	resp := &MetaResponse{PNode: "02abc", Type: "file", Path: "/x"}
	cache.Put("02abc", "/x", resp)

	// Verify subdirectory was created (2-char hex prefix).
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
	assert.Len(t, entries[0].Name(), 2) // hex prefix
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/client/ -run TestMetaCache -v -count=1`
Expected: FAIL — NewMetaCache not defined

**Step 3: Implement MetaCache**

Create `internal/client/cache.go`:
```go
package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// cacheEntry wraps a MetaResponse with a timestamp for TTL.
type cacheEntry struct {
	CachedAt time.Time     `json:"cached_at"`
	Response *MetaResponse `json:"response"`
}

// MetaCache provides file-based caching of MetaResponse objects.
type MetaCache struct {
	dir string
	ttl time.Duration
}

// NewMetaCache creates a MetaCache backed by the given directory.
func NewMetaCache(dir string, ttl time.Duration) *MetaCache {
	return &MetaCache{dir: dir, ttl: ttl}
}

// cacheKey returns a hex-encoded SHA256 of "pnode/path".
func cacheKey(pnode, path string) string {
	h := sha256.Sum256([]byte(pnode + "/" + path))
	return hex.EncodeToString(h[:])
}

// cachePath returns the file path for a cache key: dir/ab/abcdef...json
func (c *MetaCache) cachePath(key string) string {
	return filepath.Join(c.dir, key[:2], key+".json")
}

// Get retrieves a cached MetaResponse. Returns nil if not found or expired.
func (c *MetaCache) Get(pnode, path string) (*MetaResponse, error) {
	key := cacheKey(pnode, path)
	data, err := os.ReadFile(c.cachePath(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, nil // corrupt cache, treat as miss
	}

	if time.Since(entry.CachedAt) > c.ttl {
		return nil, nil // expired
	}

	return entry.Response, nil
}

// Put stores a MetaResponse in the cache.
func (c *MetaCache) Put(pnode, path string, resp *MetaResponse) {
	key := cacheKey(pnode, path)
	fp := c.cachePath(key)

	_ = os.MkdirAll(filepath.Dir(fp), 0700)

	entry := cacheEntry{CachedAt: time.Now(), Response: resp}
	data, err := json.Marshal(entry)
	if err != nil {
		return // best-effort
	}
	_ = os.WriteFile(fp, data, 0600)
}

// Invalidate removes a cached entry.
func (c *MetaCache) Invalidate(pnode, path string) {
	key := cacheKey(pnode, path)
	_ = os.Remove(c.cachePath(key))
}
```

**Step 4: Run tests**

Run: `go test ./internal/client/ -run TestMetaCache -v -count=1`
Expected: All pass

**Step 5: Commit**

```bash
git add internal/client/cache.go internal/client/cache_test.go
git commit -m "feat(client): add MetaCache — file-based metadata cache with TTL"
```

---

### Task 7: CachedClient wrapper

**Files:**
- Create: `internal/client/cached.go`
- Create: `internal/client/cached_test.go`

**Step 1: Write failing tests**

Create `internal/client/cached_test.go`:
```go
package client

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCachedClient_PopulatesCache(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	inner := &mockClient{
		meta: &MetaResponse{PNode: "02abc", Type: "file", Path: "/hello.txt"},
	}
	cc := NewCachedClient(inner, cache)

	got, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "/hello.txt", got.Path)
	assert.Equal(t, 1, inner.calls) // called inner

	// Second call should hit cache.
	got2, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "/hello.txt", got2.Path)
	assert.Equal(t, 1, inner.calls) // NOT called again
}

func TestCachedClient_NoCache(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	inner := &mockClient{
		meta: &MetaResponse{PNode: "02abc", Type: "file", Path: "/hello.txt"},
	}
	cc := NewCachedClient(inner, cache)
	cc.NoCache = true

	_, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, 1, inner.calls)

	// With NoCache, should call inner again.
	_, err = cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, 2, inner.calls)
}

func TestCachedClient_Offline_Hit(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	cache.Put("02abc", "/hello.txt", &MetaResponse{PNode: "02abc", Type: "file", Path: "/hello.txt"})

	inner := &mockClient{}
	cc := NewCachedClient(inner, cache)
	cc.Offline = true

	got, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "/hello.txt", got.Path)
	assert.Equal(t, 0, inner.calls) // never called inner
}

func TestCachedClient_Offline_Miss(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	inner := &mockClient{}
	cc := NewCachedClient(inner, cache)
	cc.Offline = true

	_, err := cc.GetMeta("02abc", "/missing")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrOfflineCacheMiss))
}

// mockClient is a test double for MetaGetter.
type mockClient struct {
	meta  *MetaResponse
	err   error
	calls int
}

func (m *mockClient) GetMeta(pnode, path string) (*MetaResponse, error) {
	m.calls++
	return m.meta, m.err
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/client/ -run TestCachedClient -v -count=1`
Expected: FAIL — NewCachedClient not defined

**Step 3: Implement CachedClient**

Create `internal/client/cached.go`:
```go
package client

import "errors"

// ErrOfflineCacheMiss is returned when offline mode has no cached data.
var ErrOfflineCacheMiss = errors.New("client: offline mode, no cached data")

// MetaGetter abstracts the GetMeta call for testability.
type MetaGetter interface {
	GetMeta(pnode, path string) (*MetaResponse, error)
}

// CachedClient wraps a MetaGetter with cache-aware GetMeta.
type CachedClient struct {
	inner   MetaGetter
	cache   *MetaCache
	NoCache bool // skip cache read, still populate
	Offline bool // cache-only, fail on miss
}

// NewCachedClient wraps a MetaGetter with a MetaCache.
func NewCachedClient(inner MetaGetter, cache *MetaCache) *CachedClient {
	return &CachedClient{inner: inner, cache: cache}
}

// GetMeta returns metadata, using cache according to NoCache/Offline settings.
func (cc *CachedClient) GetMeta(pnode, path string) (*MetaResponse, error) {
	// Check cache first (unless NoCache).
	if !cc.NoCache {
		cached, err := cc.cache.Get(pnode, path)
		if err == nil && cached != nil {
			return cached, nil
		}
	}

	// In offline mode, can't fetch from network.
	if cc.Offline {
		return nil, ErrOfflineCacheMiss
	}

	// Fetch from inner client.
	resp, err := cc.inner.GetMeta(pnode, path)
	if err != nil {
		return nil, err
	}

	// Populate cache.
	cc.cache.Put(pnode, path, resp)
	return resp, nil
}
```

**Step 4: Ensure Client implements MetaGetter**

Verify `Client.GetMeta` signature matches `MetaGetter` — it does: `func (c *Client) GetMeta(pnode, path string) (*MetaResponse, error)`.

**Step 5: Run tests**

Run: `go test ./internal/client/ -run TestCachedClient -v -count=1`
Expected: All pass

Run: `go test ./internal/client/ -v -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add internal/client/cached.go internal/client/cached_test.go
git commit -m "feat(client): add CachedClient wrapper with --no-cache/--offline support"
```

---

### Task 8: Wire --no-cache/--offline into all 6 tools

**Files:**
- Modify: `cmd/bls/main.go`, `cmd/bcat/main.go`, `cmd/bget/main.go`, `cmd/bstat/main.go`, `cmd/btree/main.go`, `cmd/bmget/main.go`

**Pattern for each tool:**

1. Add flags to the `flag.NewFlagSet` block:
```go
noCache := fs.Bool("no-cache", false, "skip metadata cache")
offline := fs.Bool("offline", false, "cache-only mode")
```

2. After creating the `client.Client`, wrap it with CachedClient:
```go
cacheDir := filepath.Join(homeDir(), ".bitfs", "cache", "meta")
cache := client.NewMetaCache(cacheDir, 5*time.Minute)
cc := client.NewCachedClient(cl, cache)
cc.NoCache = *noCache
cc.Offline = *offline
```

3. Replace `cl.GetMeta(...)` calls with `cc.GetMeta(...)`.

4. Add `"path/filepath"` and `"time"` imports as needed.

5. Add `homeDir()` helper (or use `os.UserHomeDir()`).

**Important**: For tools that call `GetData` (bcat, bget, bmget), those calls still go through the raw `Client` — only `GetMeta` is cached.

**Step 1: Apply pattern to all 6 tools**

Each tool follows the same pattern. Apply mechanically.

**Step 2: Run all tool tests**

Run: `go test ./cmd/... -count=1`
Expected: All pass

**Step 3: Commit**

```bash
git add cmd/bls/main.go cmd/bcat/main.go cmd/bget/main.go cmd/bstat/main.go cmd/btree/main.go cmd/bmget/main.go
git commit -m "feat(tools): add --no-cache and --offline flags to all b* tools"
```

---

### Task 9: bls --keyword filter

**Files:**
- Modify: `cmd/bls/main.go`

**Step 1: Add --keyword flag**

In the flag parsing section (line 25-32), add:
```go
keyword := fs.String("keyword", "", "filter children by name substring")
```

**Step 2: Add filtering to outputDefault and outputLong**

After `meta.Children` is accessed, filter if keyword is set. Add a helper:
```go
func filterChildren(children []client.ChildEntry, keyword string) []client.ChildEntry {
	if keyword == "" {
		return children
	}
	kw := strings.ToLower(keyword)
	filtered := make([]client.ChildEntry, 0, len(children))
	for _, c := range children {
		if strings.Contains(strings.ToLower(c.Name), kw) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}
```

Apply filter in `run()` before passing to output functions. Pass keyword through as needed, or filter `meta.Children` in-place before output:
```go
if *keyword != "" {
	meta.Children = filterChildren(meta.Children, *keyword)
}
```

**Step 3: Run tests**

Run: `go test ./cmd/bls/ -v -count=1`
Expected: All pass

**Step 4: Commit**

```bash
git add cmd/bls/main.go
git commit -m "feat(bls): add --keyword flag for filtering directory children"
```

---

### Task 10: bstat timestamp

**Files:**
- Modify: `internal/client/client.go` (MetaResponse)
- Modify: `internal/daemon/handler.go` (handleMeta)
- Modify: `cmd/bstat/main.go` (outputHuman, outputJSON)

**Step 1: Add Timestamp to MetaResponse**

In `internal/client/client.go`, add to MetaResponse struct:
```go
Timestamp  int64  `json:"timestamp,omitempty"` // Unix timestamp (seconds), 0 if unavailable
```

**Step 2: Populate Timestamp in daemon handleMeta**

In `internal/daemon/handler.go`, when building MetaResponse from the node, add:
```go
resp.Timestamp = int64(node.Timestamp)
```

(The `metanet.Node.Timestamp` field already exists as `uint64`.)

**Step 3: Display in bstat outputHuman**

In `cmd/bstat/main.go` `outputHuman`, after the TxID line, add:
```go
if meta.Timestamp > 0 {
	t := time.Unix(meta.Timestamp, 0).UTC()
	fmt.Fprintf(w, "    Time: %s\n", t.Format("2006-01-02 15:04:05 UTC"))
}
```

**Step 4: Run tests**

Run: `go test ./internal/client/ ./internal/daemon/ ./cmd/bstat/ -v -count=1`
Expected: All pass

**Step 5: Commit**

```bash
git add internal/client/client.go internal/daemon/handler.go cmd/bstat/main.go
git commit -m "feat(bstat): add timestamp display from node metadata"
```

---

### Task 11: Phase 2 integration verification

**Step 1: Run full test suite**

Run: `go test ./... -count=1 -race`
Expected: All pass

**Step 2: Run integration tests**

Run: `go test -tags=integration ./integration/ -count=1 -race`
Expected: All pass

**Step 3: Commit if any fixups needed**

---

## Phase 3: Daemon Enhancements

### Task 12: Version history endpoint + client method

**Files:**
- Modify: `internal/daemon/routes.go`
- Modify: `internal/daemon/handler.go`
- Create: `internal/daemon/handler_versions_test.go`
- Modify: `internal/client/client.go`

**Step 1: Define VersionEntry type in client**

In `internal/client/client.go`, add:
```go
// VersionEntry represents a single version of a node.
type VersionEntry struct {
	Version     int    `json:"version"`      // 1=latest, 2=previous, etc.
	TxID        string `json:"txid"`
	BlockHeight uint32 `json:"block_height"`
	Timestamp   int64  `json:"timestamp"`
	FileSize    uint64 `json:"file_size"`
	Access      string `json:"access"`
}
```

**Step 2: Add GetVersions to Client**

```go
// GetVersions retrieves the version history for a node.
func (c *Client) GetVersions(pnode, path string) ([]VersionEntry, error) {
	url := fmt.Sprintf("%s/_bitfs/versions/%s/%s", c.BaseURL, pnode, strings.TrimPrefix(path, "/"))
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, classifyError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, statusToError(resp.StatusCode)
	}

	var versions []VersionEntry
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, fmt.Errorf("client: decode versions: %w", err)
	}
	return versions, nil
}
```

**Step 3: Add daemon handler**

In `internal/daemon/handler.go`, add:
```go
func (d *Daemon) handleVersions(w http.ResponseWriter, r *http.Request) {
	pnode := r.PathValue("pnode")
	path := "/" + r.PathValue("path")

	// Look up the node.
	ns := d.State.FindNodeByPath(path)
	if ns == nil || ns.PubKeyHex != pnode {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Get version history from node store.
	pnodeBytes, err := hex.DecodeString(pnode)
	if err != nil {
		http.Error(w, "invalid pnode", http.StatusBadRequest)
		return
	}

	nodes, err := d.NodeStore.GetNodeVersions(pnodeBytes)
	if err != nil {
		http.Error(w, "version lookup failed", http.StatusInternalServerError)
		return
	}

	versions := make([]client.VersionEntry, len(nodes))
	for i, n := range nodes {
		versions[i] = client.VersionEntry{
			Version:     i + 1,
			TxID:        hex.EncodeToString(n.TxID),
			BlockHeight: n.BlockHeight,
			Timestamp:   int64(n.Timestamp),
			FileSize:    n.FileSize,
			Access:      accessString(n.Access),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions)
}
```

Note: This implementation depends on `d.NodeStore` existing on the Daemon struct. Check if it does; if not, add it or use whatever interface the daemon uses for node data. Adapt based on actual daemon fields — the handler may need to query the node store differently depending on the daemon's architecture.

**Step 4: Register route**

In `internal/daemon/routes.go`, add:
```go
mux.HandleFunc("GET /_bitfs/versions/{pnode}/{path...}", wrap(d.handleVersions))
```

**Step 5: Run daemon tests**

Run: `go test ./internal/daemon/ -v -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add internal/daemon/routes.go internal/daemon/handler.go internal/client/client.go
git commit -m "feat(daemon): add version history endpoint GET /_bitfs/versions/{pnode}/{path}"
```

---

### Task 13: bstat --versions (replace stub)

**Files:**
- Modify: `cmd/bstat/main.go`

**Step 1: Replace stub**

Replace the versions stub (lines 37-41):
```go
	if *versions {
		fmt.Fprintf(stderr, "version listing not yet supported\n")
		return 0
	}
```

With:
```go
	if *versions {
		vers, versErr := cl.GetVersions(pnode, path)
		if versErr != nil {
			return buyer.HandleError(versErr, "bstat", stderr)
		}
		if *jsonOut {
			data, _ := json.Marshal(vers)
			fmt.Fprintln(stdout, string(data))
			return 0
		}
		fmt.Fprintf(w, "Versions for %s (%d total):\n\n", path, len(vers))
		for _, v := range vers {
			t := time.Unix(v.Timestamp, 0).UTC().Format("2006-01-02 15:04:05")
			fmt.Fprintf(w, "  v%-4d  %s  height=%-8d  %s  [%s]\n",
				v.Version, v.TxID[:16]+"...", v.BlockHeight, t, v.Access)
		}
		return 0
	}
```

Note: `cl` is the `*client.Client` — use the raw client (not cached) for version queries since they're always fresh. Adapt variable names to match the tool's existing code.

**Step 2: Run tests**

Run: `go test ./cmd/bstat/ -v -count=1`
Expected: All pass

**Step 3: Commit**

```bash
git add cmd/bstat/main.go
git commit -m "feat(bstat): implement --versions with real version history"
```

---

### Task 14: bget --version N (replace stub)

**Files:**
- Modify: `cmd/bget/main.go`

**Step 1: Replace stub**

Find the `--version` stub (currently prints "not yet supported"). Replace with logic that:
1. Calls `cl.GetVersions(pnode, path)`
2. Selects version N from the list (1-indexed)
3. Uses that version's TxID to resolve metadata and content

```go
if *version > 0 {
	vers, versErr := cl.GetVersions(pnode, path)
	if versErr != nil {
		return buyer.HandleError(versErr, "bget", stderr)
	}
	if *version > len(vers) {
		fmt.Fprintf(stderr, "bget: version %d not found (only %d versions)\n", *version, len(vers))
		return 2
	}
	// Use the versioned TxID to fetch content.
	v := vers[*version-1]
	// Override metadata with version's data for download.
	meta.TxID = v.TxID
	meta.FileSize = v.FileSize
	meta.Access = v.Access
}
```

Adapt to fit the existing download flow — the version's metadata overrides the latest metadata before the download proceeds.

**Step 2: Run tests**

Run: `go test ./cmd/bget/ -v -count=1`
Expected: All pass

**Step 3: Commit**

```bash
git add cmd/bget/main.go
git commit -m "feat(bget): implement --version N for downloading specific file versions"
```

---

### Task 15: Sales endpoint + client method

**Files:**
- Modify: `internal/daemon/routes.go`
- Modify: `internal/daemon/handler.go`
- Modify: `internal/client/client.go`

**Step 1: Define SaleRecord type in client**

```go
// SaleRecord represents a completed or pending sale.
type SaleRecord struct {
	InvoiceID string `json:"invoice_id"`
	Price     uint64 `json:"price"`
	KeyHash   string `json:"key_hash"`
	Timestamp int64  `json:"timestamp"`
	Paid      bool   `json:"paid"`
}
```

**Step 2: Add GetSales to Client**

```go
// GetSales retrieves sales records from the daemon.
func (c *Client) GetSales(status string, limit int) ([]SaleRecord, error) {
	url := fmt.Sprintf("%s/_bitfs/sales?status=%s&limit=%d", c.BaseURL, status, limit)
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, classifyError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, statusToError(resp.StatusCode)
	}

	var records []SaleRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("client: decode sales: %w", err)
	}
	return records, nil
}
```

**Step 3: Add daemon handler**

In `internal/daemon/handler.go`:
```go
func (d *Daemon) handleSales(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "all"
	}
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	d.invoiceMu.RLock()
	defer d.invoiceMu.RUnlock()

	records := make([]client.SaleRecord, 0)
	for _, inv := range d.invoices {
		if status == "paid" && !inv.Paid {
			continue
		}
		if status == "pending" && inv.Paid {
			continue
		}
		records = append(records, client.SaleRecord{
			InvoiceID: inv.ID,
			Price:     inv.TotalPrice,
			KeyHash:   hex.EncodeToString(inv.KeyHash),
			Timestamp: inv.Expiry.Unix(),
			Paid:      inv.Paid,
		})
		if len(records) >= limit {
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}
```

Note: Adapt to match the daemon's actual invoice storage and mutex patterns. Check if `d.invoiceMu` exists or if a different synchronization pattern is used.

**Step 4: Register route**

In `internal/daemon/routes.go`:
```go
mux.HandleFunc("GET /_bitfs/sales", wrap(d.handleSales))
```

**Step 5: Run daemon tests**

Run: `go test ./internal/daemon/ ./internal/client/ -v -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add internal/daemon/routes.go internal/daemon/handler.go internal/client/client.go
git commit -m "feat(daemon): add sales endpoint GET /_bitfs/sales"
```

---

### Task 16: Shell sales command

**Files:**
- Modify: `cmd/bitfs/cmd_shell.go`

**Step 1: Add sales command**

After the `"decrypt"` case, add:
```go
	case "sales":
		daemonURL := "http://localhost:2042" // default daemon port
		cl := client.New(daemonURL)
		records, salesErr := cl.GetSales("all", 50)
		if salesErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v (is daemon running?)\n", salesErr)
			continue
		}
		if len(records) == 0 {
			fmt.Println("No sales records.")
			continue
		}
		fmt.Printf("%-36s  %10s  %5s  %s\n", "INVOICE", "PRICE(sat)", "PAID", "KEY_HASH")
		for _, r := range records {
			paid := "no"
			if r.Paid {
				paid = "yes"
			}
			kh := r.KeyHash
			if len(kh) > 16 {
				kh = kh[:16] + "..."
			}
			fmt.Printf("%-36s  %10d  %5s  %s\n", r.InvoiceID, r.Price, paid, kh)
		}
```

Note: The daemon port should ideally come from config. Check if there's a config loading mechanism in the shell setup. If so, use it. Otherwise, 2042 is the default from the spec.

**Step 2: Add "sales" to shellCommands and shellHelp**

```go
var shellCommands = []string{
	"ls", "cd", "lcd", "pwd", "cat", "get", "mget", "mput", "mkdir", "put", "rm", "mv", "cp",
	"link", "sell", "encrypt", "decrypt", "sales", "publish", "unpublish", "help", "quit", "exit",
}
```

Help text — add:
```
  sales                         View sales records (requires daemon)
```

**Step 3: Run tests**

Run: `go test ./cmd/bitfs/ -v -count=1`
Expected: All pass

**Step 4: Commit**

```bash
git add cmd/bitfs/cmd_shell.go
git commit -m "feat(shell): add sales command to view daemon sales records"
```

---

### Task 17: Phase 3 integration verification

**Step 1: Run full test suite**

Run: `go test ./... -count=1 -race`
Expected: All pass

**Step 2: Run integration tests**

Run: `go test -tags=integration ./integration/ -count=1 -race`
Expected: All pass

**Step 3: Final commit if needed**

---

## Summary

| Phase | Tasks | Scope |
|-------|-------|-------|
| 1 | Tasks 1-4 | bmget fix, engine decrypt, shell gaps (7 changes) |
| 2 | Tasks 5-11 | shared errors, cache layer, tool flags, keyword, timestamp |
| 3 | Tasks 12-17 | version endpoint, sales endpoint, tool/shell integration |

Total: 17 tasks, ~1,200 new lines, 4 new files, ~15 modified files.

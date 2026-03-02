# Shell Commands Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add 6 shell commands (cat, get, mget, mput, publish, unpublish) plus their Engine methods, building on the existing ContentResolver.

**Architecture:** Engine methods fetch ciphertext via ContentResolver, decrypt with Method 42 using owner's BIP32-derived key, and return plaintext. Shell wiring follows the existing switch-dispatch pattern in `cmd_shell.go`. Mget/mput recurse over the directory tree calling Get/PutFile per file.

**Tech Stack:** Go, method42.Decrypt, storage.ContentResolver, engine pattern (Opts → Result)

**Design Doc:** `bitfs/docs/plans/2026-02-26-shell-commands-design.md`

---

## Task 1: Add ContentResolver to Engine

Wire the existing `storage.ContentResolver` into the Engine struct so Cat/Get can use it.

**Files:**
- Modify: `bitfs/internal/engine/engine.go` (add Resolver field + init in New())
- Test: `bitfs/internal/engine/engine_test.go` (verify Resolver is initialized)

**Step 1: Add Resolver field to Engine struct**

In `engine.go`, add to Engine struct after `Store`:

```go
Resolver *storage.ContentResolver // content resolver (local store + remote endpoints)
```

**Step 2: Initialize Resolver in New()**

In `engine.go` `New()` function, after `store` is created (line ~83), add:

```go
resolver := storage.NewContentResolver(store)
```

Then add `Resolver: resolver` to the returned Engine struct.

**Step 3: Write test**

In `engine_test.go`, add:

```go
func TestNew_ResolverInitialized(t *testing.T) {
	eng := initTestEngine(t)
	assert.NotNil(t, eng.Resolver, "Resolver should be initialized")
	assert.Equal(t, eng.Store, eng.Resolver.Store, "Resolver should use engine's store")
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestNew_Resolver -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/engine/engine.go internal/engine/engine_test.go
git commit -m "feat(engine): add ContentResolver field"
```

---

## Task 2: Engine Cat() method

**Files:**
- Create: `bitfs/internal/engine/cat.go`
- Test: `bitfs/internal/engine/cat_test.go`

**Step 1: Write failing tests**

Create `cat_test.go`:

```go
package engine

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCat_Success_FreeFile(t *testing.T) {
	eng := initTestEngine(t)

	// Upload a file first.
	localFile := createTempFile(t, "hello world")
	_, err := eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/test.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Cat should return the plaintext.
	result, err := eng.Cat(&CatOpts{VaultIndex: 0, Path: "/test.txt"})
	require.NoError(t, err)
	assert.Equal(t, []byte("hello world"), result.Data)
	assert.Equal(t, "text/plain; charset=utf-8", result.MimeType)
	assert.Equal(t, uint64(11), result.FileSize)
}

func TestCat_Success_PrivateFile(t *testing.T) {
	eng := initTestEngine(t)

	localFile := createTempFile(t, "secret data")
	_, err := eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/secret.txt",
		Access:     "private",
	})
	require.NoError(t, err)

	result, err := eng.Cat(&CatOpts{VaultIndex: 0, Path: "/secret.txt"})
	require.NoError(t, err)
	assert.Equal(t, []byte("secret data"), result.Data)
}

func TestCat_NotFound(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Cat(&CatOpts{VaultIndex: 0, Path: "/nonexistent.txt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCat_IsDirectory(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/mydir"})
	require.NoError(t, err)

	_, err = eng.Cat(&CatOpts{VaultIndex: 0, Path: "/mydir"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory")
}

func TestCat_ContentNotInStore(t *testing.T) {
	eng := initTestEngine(t)

	// Upload file, then delete content from store to simulate missing data.
	localFile := createTempFile(t, "disappearing content")
	_, err := eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/vanish.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Remove content from store.
	node := eng.State.FindNodeByPath("/vanish.txt")
	require.NotNil(t, node)
	keyHash, _ := hex.DecodeString(node.KeyHash)
	_ = eng.Store.Delete(keyHash)

	_, err = eng.Cat(&CatOpts{VaultIndex: 0, Path: "/vanish.txt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// createTempFile is a test helper that creates a temporary file with content.
func createTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "bitfs-test-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return f.Name()
}
```

Note: Check if `createTempFile` or similar helper already exists in test files. If it does, reuse it instead of creating a new one.

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestCat -v`
Expected: FAIL — `Cat` not defined

**Step 3: Implement Cat()**

Create `cat.go`:

```go
package engine

import (
	"encoding/hex"
	"fmt"

	"github.com/tongxiaofeng/libbitfs-go/method42"
)

// CatOpts holds options for the Cat (view file) operation.
type CatOpts struct {
	VaultIndex uint32
	Path       string
}

// CatResult holds the output of a Cat operation.
type CatResult struct {
	Data     []byte
	MimeType string
	FileSize uint64
}

// Cat retrieves and decrypts a file from the vault, returning its plaintext.
func (e *Engine) Cat(opts *CatOpts) (*CatResult, error) {
	// Resolve node by path.
	node := e.State.FindNodeByPath(opts.Path)
	if node == nil {
		return nil, fmt.Errorf("engine: not found: %s", opts.Path)
	}
	if node.Type == "dir" {
		return nil, fmt.Errorf("engine: %s is a directory", opts.Path)
	}

	// Fetch ciphertext via resolver.
	keyHash, err := hex.DecodeString(node.KeyHash)
	if err != nil {
		return nil, fmt.Errorf("engine: invalid key hash: %w", err)
	}

	ciphertext, err := e.Resolver.Fetch(keyHash)
	if err != nil {
		return nil, fmt.Errorf("engine: fetch content: %w", err)
	}

	// Derive owner's key for decryption.
	kp, err := e.Wallet.DeriveNodeKey(node.VaultIndex, node.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive node key: %w", err)
	}

	// Map access string to method42 access mode.
	accessMode := accessStringToMode(node.Access)

	// Decrypt.
	decResult, err := method42.Decrypt(ciphertext, kp.PrivateKey, kp.PublicKey, keyHash, accessMode)
	if err != nil {
		return nil, fmt.Errorf("engine: decrypt: %w", err)
	}

	return &CatResult{
		Data:     decResult.Plaintext,
		MimeType: node.MimeType,
		FileSize: node.FileSize,
	}, nil
}

// accessStringToMode converts a string access level to method42.Access.
func accessStringToMode(access string) method42.Access {
	switch access {
	case "private", "paid":
		return method42.AccessPrivate
	default:
		return method42.AccessFree
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestCat -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/engine/cat.go internal/engine/cat_test.go
git commit -m "feat(engine): add Cat() method for file content retrieval"
```

---

## Task 3: Engine Get() method

**Files:**
- Create: `bitfs/internal/engine/get.go`
- Test: `bitfs/internal/engine/get_test.go`

**Step 1: Write failing tests**

Create `get_test.go`:

```go
package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet_Success(t *testing.T) {
	eng := initTestEngine(t)
	outDir := t.TempDir()

	localFile := createTempFile(t, "download me")
	_, err := eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/dl.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	outPath := filepath.Join(outDir, "dl.txt")
	result, err := eng.Get(&GetOpts{
		VaultIndex: 0,
		RemotePath: "/dl.txt",
		LocalPath:  outPath,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "dl.txt")

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, "download me", string(data))
}

func TestGet_DefaultLocalPath(t *testing.T) {
	eng := initTestEngine(t)
	outDir := t.TempDir()

	localFile := createTempFile(t, "default path test")
	_, err := eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/readme.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// When LocalPath is empty, use LocalDir + filename from remote path.
	result, err := eng.Get(&GetOpts{
		VaultIndex: 0,
		RemotePath: "/readme.txt",
		LocalDir:   outDir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "readme.txt")

	data, err := os.ReadFile(filepath.Join(outDir, "readme.txt"))
	require.NoError(t, err)
	assert.Equal(t, "default path test", string(data))
}

func TestGet_NotFound(t *testing.T) {
	eng := initTestEngine(t)
	outDir := t.TempDir()

	_, err := eng.Get(&GetOpts{
		VaultIndex: 0,
		RemotePath: "/nope.txt",
		LocalPath:  filepath.Join(outDir, "nope.txt"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestGet -v`
Expected: FAIL

**Step 3: Implement Get()**

Create `get.go`:

```go
package engine

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
)

// GetOpts holds options for the Get (download file) operation.
type GetOpts struct {
	VaultIndex uint32
	RemotePath string
	LocalPath  string // explicit output path; takes priority over LocalDir
	LocalDir   string // output directory; filename derived from RemotePath
}

// Get downloads and decrypts a file from the vault to the local filesystem.
func (e *Engine) Get(opts *GetOpts) (*Result, error) {
	// Determine output path.
	outPath := opts.LocalPath
	if outPath == "" {
		dir := opts.LocalDir
		if dir == "" {
			dir = "."
		}
		outPath = filepath.Join(dir, path.Base(opts.RemotePath))
	}

	// Use Cat to fetch + decrypt.
	catResult, err := e.Cat(&CatOpts{
		VaultIndex: opts.VaultIndex,
		Path:       opts.RemotePath,
	})
	if err != nil {
		return nil, err
	}

	// Write to local file.
	if err := os.WriteFile(outPath, catResult.Data, 0644); err != nil {
		return nil, fmt.Errorf("engine: write file: %w", err)
	}

	return &Result{
		Message: fmt.Sprintf("Downloaded %s -> %s (%d bytes)", opts.RemotePath, outPath, len(catResult.Data)),
	}, nil
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestGet -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/engine/get.go internal/engine/get_test.go
git commit -m "feat(engine): add Get() method for file download"
```

---

## Task 4: Engine Mget() method

**Files:**
- Create: `bitfs/internal/engine/mget.go`
- Test: `bitfs/internal/engine/mget_test.go`

**Step 1: Write failing tests**

Create `mget_test.go`:

```go
package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMget_Success(t *testing.T) {
	eng := initTestEngine(t)
	outDir := t.TempDir()

	// Create directory structure: /docs/a.txt, /docs/sub/b.txt
	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/docs"})
	require.NoError(t, err)
	_, err = eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/docs/sub"})
	require.NoError(t, err)

	f1 := createTempFile(t, "file a")
	_, err = eng.PutFile(&PutOpts{VaultIndex: 0, LocalFile: f1, RemotePath: "/docs/a.txt", Access: "free"})
	require.NoError(t, err)

	f2 := createTempFile(t, "file b")
	_, err = eng.PutFile(&PutOpts{VaultIndex: 0, LocalFile: f2, RemotePath: "/docs/sub/b.txt", Access: "free"})
	require.NoError(t, err)

	result, err := eng.Mget(&MgetOpts{VaultIndex: 0, RemotePath: "/docs", LocalDir: outDir})
	require.NoError(t, err)
	assert.Equal(t, 2, result.FilesDownloaded)
	assert.Equal(t, 1, result.DirsCreated) // "sub" is created; "docs" root maps to outDir

	data1, err := os.ReadFile(filepath.Join(outDir, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "file a", string(data1))

	data2, err := os.ReadFile(filepath.Join(outDir, "sub", "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "file b", string(data2))
}

func TestMget_EmptyDir(t *testing.T) {
	eng := initTestEngine(t)
	outDir := t.TempDir()

	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/empty"})
	require.NoError(t, err)

	result, err := eng.Mget(&MgetOpts{VaultIndex: 0, RemotePath: "/empty", LocalDir: outDir})
	require.NoError(t, err)
	assert.Equal(t, 0, result.FilesDownloaded)
}

func TestMget_NotADirectory(t *testing.T) {
	eng := initTestEngine(t)
	outDir := t.TempDir()

	f := createTempFile(t, "single file")
	_, err := eng.PutFile(&PutOpts{VaultIndex: 0, LocalFile: f, RemotePath: "/single.txt", Access: "free"})
	require.NoError(t, err)

	_, err = eng.Mget(&MgetOpts{VaultIndex: 0, RemotePath: "/single.txt", LocalDir: outDir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestMget_NotFound(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Mget(&MgetOpts{VaultIndex: 0, RemotePath: "/nope", LocalDir: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestMget -v`
Expected: FAIL

**Step 3: Implement Mget()**

Create `mget.go`:

```go
package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

// MgetOpts holds options for the Mget (recursive download) operation.
type MgetOpts struct {
	VaultIndex uint32
	RemotePath string
	LocalDir   string // output directory; defaults to "."
}

// MgetResult holds the output of an Mget operation.
type MgetResult struct {
	FilesDownloaded int
	DirsCreated     int
	Errors          []string
}

// Mget recursively downloads a directory from the vault to the local filesystem.
func (e *Engine) Mget(opts *MgetOpts) (*MgetResult, error) {
	node := e.State.FindNodeByPath(opts.RemotePath)
	if node == nil {
		return nil, fmt.Errorf("engine: not found: %s", opts.RemotePath)
	}
	if node.Type != "dir" {
		return nil, fmt.Errorf("engine: %s is not a directory", opts.RemotePath)
	}

	localDir := opts.LocalDir
	if localDir == "" {
		localDir = "."
	}

	result := &MgetResult{}
	e.mgetRecursive(opts.VaultIndex, node, localDir, result)
	return result, nil
}

func (e *Engine) mgetRecursive(vaultIdx uint32, dirNode *NodeState, localDir string, result *MgetResult) {
	for _, child := range dirNode.Children {
		childNode := e.State.GetNode(child.PubKey)
		if childNode == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("missing node for %s", child.Name))
			continue
		}

		if child.Type == "dir" {
			subDir := filepath.Join(localDir, child.Name)
			if err := os.MkdirAll(subDir, 0755); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("mkdir %s: %v", subDir, err))
				continue
			}
			result.DirsCreated++
			e.mgetRecursive(vaultIdx, childNode, subDir, result)
		} else {
			outPath := filepath.Join(localDir, child.Name)
			_, err := e.Get(&GetOpts{
				VaultIndex: vaultIdx,
				RemotePath: childNode.Path,
				LocalPath:  outPath,
			})
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("get %s: %v", child.Name, err))
				continue
			}
			result.FilesDownloaded++
		}
	}
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestMget -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/engine/mget.go internal/engine/mget_test.go
git commit -m "feat(engine): add Mget() method for recursive directory download"
```

---

## Task 5: Engine Mput() method

**Files:**
- Create: `bitfs/internal/engine/mput.go`
- Test: `bitfs/internal/engine/mput_test.go`

**Step 1: Write failing tests**

Create `mput_test.go`:

```go
package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMput_Success(t *testing.T) {
	eng := initTestEngine(t)
	// Seed fee UTXOs for multiple transactions.
	seedFeeUTXOs(t, eng, 10)

	// Create local directory: src/x.txt, src/inner/y.txt
	srcDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "inner"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "x.txt"), []byte("x content"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "inner", "y.txt"), []byte("y content"), 0644))

	result, err := eng.Mput(&MputOpts{
		VaultIndex: 0,
		LocalDir:   srcDir,
		RemoteDir:  "/upload",
		Access:     "free",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.FilesUploaded)
	assert.True(t, result.DirsCreated >= 2) // /upload + /upload/inner

	// Verify files exist in vault.
	assert.NotNil(t, eng.State.FindNodeByPath("/upload/x.txt"))
	assert.NotNil(t, eng.State.FindNodeByPath("/upload/inner/y.txt"))
}

func TestMput_EmptyDir(t *testing.T) {
	eng := initTestEngine(t)
	seedFeeUTXOs(t, eng, 5)

	srcDir := t.TempDir() // empty

	result, err := eng.Mput(&MputOpts{
		VaultIndex: 0,
		LocalDir:   srcDir,
		RemoteDir:  "/empty-upload",
		Access:     "free",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.FilesUploaded)
}

func TestMput_LocalDirNotFound(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Mput(&MputOpts{
		VaultIndex: 0,
		LocalDir:   "/nonexistent/path",
		RemoteDir:  "/dest",
		Access:     "free",
	})
	require.Error(t, err)
}
```

Note: `seedFeeUTXOs` helper may already exist in test files. If not, create it — it adds synthetic fee UTXOs to `eng.State` so multi-tx operations don't fail with "no fee UTXO" errors.

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestMput -v`
Expected: FAIL

**Step 3: Implement Mput()**

Create `mput.go`:

```go
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MputOpts holds options for the Mput (recursive upload) operation.
type MputOpts struct {
	VaultIndex uint32
	LocalDir   string
	RemoteDir  string // destination directory in vault; defaults to "/"
	Access     string // "free" or "private", applied to all files
}

// MputResult holds the output of an Mput operation.
type MputResult struct {
	FilesUploaded int
	DirsCreated   int
	Errors        []string
}

// Mput recursively uploads a local directory to the vault.
func (e *Engine) Mput(opts *MputOpts) (*MputResult, error) {
	info, err := os.Stat(opts.LocalDir)
	if err != nil {
		return nil, fmt.Errorf("engine: local dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("engine: %s is not a directory", opts.LocalDir)
	}

	remoteDir := opts.RemoteDir
	if remoteDir == "" {
		remoteDir = "/"
	}
	access := opts.Access
	if access == "" {
		access = "free"
	}

	result := &MputResult{}

	err = filepath.WalkDir(opts.LocalDir, func(localPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("walk %s: %v", localPath, walkErr))
			return nil // continue walking
		}

		// Compute relative path from source root.
		rel, err := filepath.Rel(opts.LocalDir, localPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("rel %s: %v", localPath, err))
			return nil
		}
		if rel == "." {
			// Create the root destination directory.
			if _, mkErr := e.Mkdir(&MkdirOpts{VaultIndex: opts.VaultIndex, Path: remoteDir}); mkErr != nil {
				// Directory may already exist — ignore "already exists" errors.
				if !strings.Contains(mkErr.Error(), "already exists") {
					result.Errors = append(result.Errors, fmt.Sprintf("mkdir %s: %v", remoteDir, mkErr))
				}
			} else {
				result.DirsCreated++
			}
			return nil
		}

		// Convert OS path separators to forward slashes for remote path.
		remotePath := remoteDir + "/" + filepath.ToSlash(rel)

		if d.IsDir() {
			if _, mkErr := e.Mkdir(&MkdirOpts{VaultIndex: opts.VaultIndex, Path: remotePath}); mkErr != nil {
				if !strings.Contains(mkErr.Error(), "already exists") {
					result.Errors = append(result.Errors, fmt.Sprintf("mkdir %s: %v", remotePath, mkErr))
				}
			} else {
				result.DirsCreated++
			}
			return nil
		}

		// Upload file.
		if _, putErr := e.PutFile(&PutOpts{
			VaultIndex: opts.VaultIndex,
			LocalFile:  localPath,
			RemotePath: remotePath,
			Access:     access,
		}); putErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("put %s: %v", remotePath, putErr))
		} else {
			result.FilesUploaded++
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("engine: walk dir: %w", err)
	}

	return result, nil
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestMput -v`
Expected: ALL PASS (may need `seedFeeUTXOs` helper — see note below)

**Note on `seedFeeUTXOs`:** If this helper doesn't exist, create it in a shared test file:

```go
func seedFeeUTXOs(t *testing.T, eng *Engine, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		eng.State.AddUTXO(&UTXOState{
			TxID:         fmt.Sprintf("%064x", i+1000),
			Vout:         0,
			Amount:       100000,
			ScriptPubKey: deriveFeeScriptPubKey(t, eng, i),
			PubKeyHex:    deriveFeePubKeyHex(t, eng, i),
			Type:         "fee",
		})
	}
}
```

Check how existing tests that need fee UTXOs handle this (e.g., `put_test.go`, `move_test.go`) and follow the same pattern.

**Step 5: Commit**

```bash
git add internal/engine/mput.go internal/engine/mput_test.go
git commit -m "feat(engine): add Mput() method for recursive directory upload"
```

---

## Task 6: Engine Unpublish() method

**Files:**
- Modify: `bitfs/internal/engine/publish.go` (add Unpublish method)
- Modify: `bitfs/internal/engine/publish_test.go` (add tests)

**Step 1: Write failing tests**

Add to `publish_test.go`:

```go
func TestUnpublish_Success(t *testing.T) {
	eng := initTestEngine(t)
	eng.DNS = &mockDNSResolver{err: fmt.Errorf("no such host")}

	// First publish.
	_, err := eng.Publish(&PublishOpts{VaultIndex: 0, Domain: "test.com"})
	require.NoError(t, err)

	// Verify binding exists.
	bindings := eng.State.ListPublishBindings()
	require.Len(t, bindings, 1)

	// Unpublish.
	result, err := eng.Unpublish(&UnpublishOpts{Domain: "test.com"})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "test.com")

	// Verify binding removed.
	bindings = eng.State.ListPublishBindings()
	assert.Empty(t, bindings)
}

func TestUnpublish_NotFound(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Unpublish(&UnpublishOpts{Domain: "unknown.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUnpublish_EmptyDomain(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Unpublish(&UnpublishOpts{Domain: ""})
	require.Error(t, err)
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestUnpublish -v`
Expected: FAIL

**Step 3: Implement Unpublish()**

Add to `publish.go`:

```go
// UnpublishOpts holds options for the Unpublish operation.
type UnpublishOpts struct {
	Domain string
}

// Unpublish removes a domain binding from local state and prints DNS cleanup instructions.
func (e *Engine) Unpublish(opts *UnpublishOpts) (*Result, error) {
	if opts.Domain == "" {
		return nil, fmt.Errorf("engine: domain is required")
	}

	if !e.State.RemovePublishBinding(opts.Domain) {
		return nil, fmt.Errorf("engine: publish binding not found for %s", opts.Domain)
	}

	msg := fmt.Sprintf("Removed publish binding for %s.\n\nTo complete unpublishing, remove the DNS TXT record:\n  _bitfs.%s  TXT  (delete this record)",
		opts.Domain, opts.Domain)

	return &Result{Message: msg}, nil
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestUnpublish -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/engine/publish.go internal/engine/publish_test.go
git commit -m "feat(engine): add Unpublish() method"
```

---

## Task 7: Wire all commands into Shell

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_shell.go` (add 6 commands to switch + update help + update command list)
- Modify: `bitfs/cmd/bitfs/completer.go` (add new commands to completion)

**Step 1: Update shellCommands list**

In `cmd_shell.go`, update the `shellCommands` slice:

```go
var shellCommands = []string{
	"ls", "cd", "lcd", "pwd", "mkdir", "put", "rm", "mv", "cp",
	"link", "sell", "encrypt", "cat", "get", "mget", "mput",
	"publish", "unpublish", "help", "quit", "exit",
}
```

**Step 2: Add shell cases for all 6 commands**

Add these cases to the switch statement in `cmd_shell.go`, before the `default:` case:

```go
case "cat":
	if len(cmdArgs) < 1 {
		fmt.Println("Usage: cat <path>")
		continue
	}
	remotePath := resolvePath(cwd, cmdArgs[0])
	catResult, catErr := eng.Cat(&engine.CatOpts{VaultIndex: vaultIdx, Path: remotePath})
	if catErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", catErr)
	} else {
		// Check for binary content (unless --force).
		force := len(cmdArgs) > 1 && cmdArgs[1] == "--force"
		if !force && isBinaryContent(catResult.Data) {
			fmt.Println("Binary file detected. Use 'get' to download, or 'cat <path> --force' to display.")
		} else {
			_, _ = os.Stdout.Write(catResult.Data)
			// Ensure trailing newline for terminal readability.
			if len(catResult.Data) > 0 && catResult.Data[len(catResult.Data)-1] != '\n' {
				fmt.Println()
			}
		}
	}
case "get":
	if len(cmdArgs) < 1 {
		fmt.Println("Usage: get <remote> [local]")
		continue
	}
	remotePath := resolvePath(cwd, cmdArgs[0])
	getOpts := &engine.GetOpts{
		VaultIndex: vaultIdx,
		RemotePath: remotePath,
		LocalDir:   localCwd,
	}
	if len(cmdArgs) >= 2 {
		lp := cmdArgs[1]
		if !filepath.IsAbs(lp) {
			lp = filepath.Join(localCwd, lp)
		}
		getOpts.LocalPath = lp
	}
	result, getErr := eng.Get(getOpts)
	if getErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", getErr)
	} else {
		fmt.Println(result.Message)
	}
case "mget":
	if len(cmdArgs) < 1 {
		fmt.Println("Usage: mget <remotedir> [localdir]")
		continue
	}
	remotePath := resolvePath(cwd, cmdArgs[0])
	outDir := localCwd
	if len(cmdArgs) >= 2 {
		outDir = cmdArgs[1]
		if !filepath.IsAbs(outDir) {
			outDir = filepath.Join(localCwd, outDir)
		}
	}
	mgetResult, mgetErr := eng.Mget(&engine.MgetOpts{
		VaultIndex: vaultIdx,
		RemotePath: remotePath,
		LocalDir:   outDir,
	})
	if mgetErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", mgetErr)
	} else {
		fmt.Printf("Downloaded %d files, created %d directories\n",
			mgetResult.FilesDownloaded, mgetResult.DirsCreated)
		for _, e := range mgetResult.Errors {
			fmt.Fprintf(os.Stderr, "  Warning: %s\n", e)
		}
	}
case "mput":
	if len(cmdArgs) < 1 {
		fmt.Println("Usage: mput <localdir> [remotedir]")
		continue
	}
	srcDir := cmdArgs[0]
	if !filepath.IsAbs(srcDir) {
		srcDir = filepath.Join(localCwd, srcDir)
	}
	remoteDir := cwd
	if len(cmdArgs) >= 2 {
		remoteDir = resolvePath(cwd, cmdArgs[1])
	}
	mputResult, mputErr := eng.Mput(&engine.MputOpts{
		VaultIndex: vaultIdx,
		LocalDir:   srcDir,
		RemoteDir:  remoteDir,
		Access:     "free",
	})
	if mputErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", mputErr)
	} else {
		fmt.Printf("Uploaded %d files, created %d directories\n",
			mputResult.FilesUploaded, mputResult.DirsCreated)
		for _, e := range mputResult.Errors {
			fmt.Fprintf(os.Stderr, "  Warning: %s\n", e)
		}
	}
case "publish":
	domain := ""
	if len(cmdArgs) > 0 {
		domain = cmdArgs[0]
	}
	result, pubErr := eng.Publish(&engine.PublishOpts{
		VaultIndex: vaultIdx,
		Domain:     domain,
	})
	if pubErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", pubErr)
	} else {
		fmt.Println(result.Message)
	}
case "unpublish":
	if len(cmdArgs) < 1 {
		fmt.Println("Usage: unpublish <domain>")
		continue
	}
	result, unpubErr := eng.Unpublish(&engine.UnpublishOpts{
		Domain: cmdArgs[0],
	})
	if unpubErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", unpubErr)
	} else {
		fmt.Println(result.Message)
	}
```

**Step 3: Add isBinaryContent helper**

Add to `cmd_shell.go`:

```go
// isBinaryContent checks if data contains binary (non-text) content
// by looking for NUL bytes in the first 8KB.
func isBinaryContent(data []byte) bool {
	check := data
	if len(check) > 8192 {
		check = check[:8192]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}
```

**Step 4: Update shellHelp()**

Replace the help text:

```go
func shellHelp() {
	fmt.Println(`Available commands:
  ls [path]                List directory contents
  cd [path]                Change remote directory
  lcd [path]               Change local directory (or print current)
  pwd                      Print remote working directory
  cat <path>               View file contents (--force for binary)
  get <remote> [local]     Download file to local disk
  mget <remotedir> [local] Recursively download directory
  mkdir <path>             Create directory
  put <local> <remote>     Upload file
  mput <localdir> [remote] Recursively upload directory
  rm <path>                Remove file/directory
  mv <src> <dst>           Move/rename
  cp <src> <dst>           Copy file
  link <target> <path>     Create hard link (--soft for symlink)
  sell <path> <price>      Set price (sats/KB)
  encrypt <path>           Encrypt (FREE -> PRIVATE)
  publish [domain]         List or add domain binding
  unpublish <domain>       Remove domain binding
  help                     Show this help
  quit                     Exit shell`)
}
```

**Step 5: Update completer for new commands**

In `completer.go`, update the `Do()` switch to add completion for new commands:

```go
case "cat", "get":
	// Remote path completion.
	candidates := sc.completeRemotePath(currentPrefix)
	return formatCandidates(candidates, currentPrefix)

case "mget":
	// Remote directory path completion.
	candidates := sc.completeRemotePath(currentPrefix)
	return formatCandidates(candidates, currentPrefix)

case "mput":
	if argIndex <= 1 {
		// First arg: local directory.
		candidates := sc.completeLocalPath(currentPrefix)
		return formatCandidates(candidates, currentPrefix)
	}
	// Second arg: remote path.
	candidates := sc.completeRemotePath(currentPrefix)
	return formatCandidates(candidates, currentPrefix)
```

For `publish`/`unpublish`: no special completion needed (domains are not in local state for tab completion).

**Step 6: Build to verify compilation**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go build ./cmd/bitfs`
Expected: SUCCESS

**Step 7: Run all tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1`
Expected: ALL PASS

**Step 8: Commit**

```bash
git add cmd/bitfs/cmd_shell.go cmd/bitfs/completer.go
git commit -m "feat(shell): wire cat/get/mget/mput/publish/unpublish commands"
```

---

## Task 8: Final validation

**Step 1: Run full test suite**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1 -race`
Expected: ALL PASS, no data races

**Step 2: Run linter**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && golangci-lint run ./...`
Expected: No new issues

**Step 3: Build all binaries**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go build ./cmd/...`
Expected: SUCCESS

**Step 4: Update design review tracker**

Mark item 2.2 as complete in `tasks/2026-02-25-design-review.md`:

```
- [x] 2.2 Shell 补全 cat/publish 命令
```

**Step 5: Commit**

```bash
git add tasks/2026-02-25-design-review.md
git commit -m "docs: mark shell commands (2.2) as complete"
```

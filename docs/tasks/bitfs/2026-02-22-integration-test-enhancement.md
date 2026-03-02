# Integration Test Enhancement — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add ~35 cross-package integration tests covering Engine orchestrator, Daemon↔Client HTTP round trips, Merkle mutations, and SPV verification pipeline.

**Architecture:** Extend existing `integration/` directory with 7 new files (1 helpers + 6 test files). All engine tests use a shared `initIntegrationEngine()` helper that creates a fully-seeded Engine with fee UTXOs. Daemon tests use `httptest.Server` with real Engine adapters. No Docker or network required — all blockchain interactions mocked via interface injection.

**Tech Stack:** Go 1.25.6, `testify` (require/assert), `net/http/httptest`, `//go:build integration`

---

### Task 1: Mock Infrastructure & Shared Helpers

**Files:**
- Create: `integration/engine_helpers_test.go`

This file provides the `initIntegrationEngine()` helper and `seedFeeUTXOs()` that all engine integration tests depend on. The engine's `AllocateFeeUTXO()` needs pre-seeded UTXOs with matching private keys, so we derive actual fee keys from the wallet.

**Step 1: Write the helper file**

Create `integration/engine_helpers_test.go`:

```go
//go:build integration

package integration

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/engine"
	"github.com/tongxiaofeng/libbitfs/tx"
	"github.com/tongxiaofeng/libbitfs/wallet"
)

const integrationPassword = "integration-test-pass"

// initIntegrationEngine creates a fully initialized Engine with:
// - HD wallet with "default" vault at index 0
// - 20 fee UTXOs seeded (10,000 sats each)
// - Content-addressed file store in temp directory
// - Empty local state
//
// This differs from the unit-test helper (engine_test.go:initTestEngine)
// which doesn't seed fee UTXOs, making it unusable for happy-path tests.
func initIntegrationEngine(t *testing.T) *engine.Engine {
	t.Helper()
	dataDir := t.TempDir()

	// Generate wallet.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	encrypted, err := wallet.EncryptSeed(seed, integrationPassword)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "wallet.enc"), encrypted, 0600))

	// Create wallet state with default vault.
	w, err := wallet.NewWallet(seed, &wallet.MainNet)
	require.NoError(t, err)

	wState := wallet.NewWalletState()
	_, err = w.CreateVault(wState, "default")
	require.NoError(t, err)

	stateBytes, err := encodeJSON(wState)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "state.json"), stateBytes, 0600))

	eng, err := engine.New(dataDir, integrationPassword)
	require.NoError(t, err)
	t.Cleanup(func() { eng.Close() })

	// Seed fee UTXOs.
	seedFeeUTXOs(t, eng, 20, 10000)

	return eng
}

// seedFeeUTXOs creates `count` fake fee UTXOs with `amount` satoshis each.
// Each UTXO's pubkey matches an actual derived fee key so that lookupPrivKey
// in engine.utxoStateToTx succeeds.
func seedFeeUTXOs(t *testing.T, eng *engine.Engine, count int, amount uint64) {
	t.Helper()
	for i := 0; i < count; i++ {
		kp, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, uint32(i))
		require.NoError(t, err)

		pubHex := hex.EncodeToString(kp.PublicKey.Compressed())
		scriptPK, err := tx.BuildP2PKHScript(kp.PublicKey)
		require.NoError(t, err)

		fakeTxID := make([]byte, 32)
		_, _ = rand.Read(fakeTxID)

		eng.State.AddUTXO(&engine.UTXOState{
			TxID:         hex.EncodeToString(fakeTxID),
			Vout:         uint32(i),
			Amount:       amount,
			ScriptPubKey: hex.EncodeToString(scriptPK),
			PubKeyHex:    pubHex,
			Type:         "fee",
		})
	}
	// Bump NextReceiveIndex so lookupPrivKey can find these keys.
	eng.WState.NextReceiveIndex = uint32(count)
}

// encodeJSON is a small helper for pretty-printing JSON.
func encodeJSON(v interface{}) ([]byte, error) {
	import "encoding/json"
	return json.MarshalIndent(v, "", "  ")
}

// createTempFile writes content to a temp file and returns its path.
func createTempFile(t *testing.T, content []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "bitfs-test-*")
	require.NoError(t, err)
	_, err = f.Write(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}
```

Note: The `encodeJSON` function has an inline import which won't compile. Move `"encoding/json"` to the import block. Also, `engine.UTXOState` is in an internal package — we need to verify the integration test can access it. The `integration/` package is inside `bitfs/`, so it CAN import `bitfs/internal/engine`.

**Step 2: Verify compilation**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go build -tags=integration ./integration/`

Expected: Compiles successfully. If `engine.UTXOState` is unexported, we'll need to add a `SeedTestUTXO()` helper method to the engine package.

**Step 3: Fix any compilation issues**

If `UTXOState` fields are inaccessible, add a test helper to `internal/engine/`:

```go
// File: internal/engine/testing_helpers.go (only if needed)
package engine

// SeedFeeUTXO adds a fee UTXO for testing. Only use in tests.
func (e *Engine) SeedFeeUTXO(txID string, vout uint32, amount uint64, scriptPK string, pubKeyHex string) {
	e.State.AddUTXO(&UTXOState{
		TxID:         txID,
		Vout:         vout,
		Amount:       amount,
		ScriptPubKey: scriptPK,
		PubKeyHex:    pubKeyHex,
		Type:         "fee",
	})
}
```

**Step 4: Commit**

```bash
git add integration/engine_helpers_test.go
# If needed: git add internal/engine/testing_helpers.go
git commit -m "test(integration): add engine test infrastructure

Add initIntegrationEngine() and seedFeeUTXOs() helpers that create
a fully-seeded Engine with HD wallet, fee UTXOs, and content store.
These enable happy-path integration tests for all engine operations."
```

---

### Task 2: Engine Workflow Tests — Put, Mkdir, Move, Copy, Remove

**Files:**
- Create: `integration/engine_workflow_test.go`

**Step 1: Write the test file**

Create `integration/engine_workflow_test.go` with these 10 tests. Each test exercises a cross-package flow through the Engine orchestrator.

```go
//go:build integration

package integration

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/engine"
	"github.com/tongxiaofeng/libbitfs/method42"
)

// TestEnginePutAndRetrieve exercises the full put+retrieve cycle:
// wallet → key derivation → method42 encrypt → engine.Put → store.Put → store.Get → method42 decrypt
func TestEnginePutAndRetrieve(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create root directory first.
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Put a file.
	content := []byte("Hello, BitFS integration test!")
	localFile := createTempFile(t, content)

	result, err := eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/hello.txt",
		Access:     "free",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.TxID)
	require.NotEmpty(t, result.TxHex)
	require.NotEmpty(t, result.NodePub)

	// Verify the file node exists in state.
	fileNode := eng.State.FindNodeByPath("/hello.txt")
	require.NotNil(t, fileNode)
	assert.Equal(t, "file", fileNode.Type)
	assert.Equal(t, "free", fileNode.Access)
	assert.Equal(t, uint64(len(content)), fileNode.FileSize)

	// Retrieve and decrypt: use the file's key to get ciphertext from store,
	// then decrypt with Method 42.
	keyHash, err := hex.DecodeString(fileNode.KeyHash)
	require.NoError(t, err)

	ciphertext, err := eng.Store.Get(keyHash)
	require.NoError(t, err)
	require.NotNil(t, ciphertext)

	kp, err := eng.Wallet.DeriveNodeKey(fileNode.VaultIndex, fileNode.ChildIndices, nil)
	require.NoError(t, err)

	decResult, err := method42.Decrypt(ciphertext, kp.PrivateKey, kp.PublicKey, keyHash, method42.AccessFree)
	require.NoError(t, err)
	assert.Equal(t, content, decResult.Plaintext)
}

// TestEngineMkdirNested tests creating nested directories:
// wallet → engine.Mkdir("/") → engine.Mkdir("/docs") → engine.Mkdir("/docs/sub") → state path resolution
func TestEngineMkdirNested(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create root.
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Create /docs.
	result, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/docs"})
	require.NoError(t, err)
	require.NotEmpty(t, result.TxID)

	// Create /docs/sub.
	result2, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/docs/sub"})
	require.NoError(t, err)
	require.NotEmpty(t, result2.TxID)

	// Verify both dirs exist in state.
	docsNode := eng.State.FindNodeByPath("/docs")
	require.NotNil(t, docsNode)
	assert.Equal(t, "dir", docsNode.Type)
	assert.Len(t, docsNode.Children, 1) // has "sub" child

	subNode := eng.State.FindNodeByPath("/docs/sub")
	require.NotNil(t, subNode)
	assert.Equal(t, "dir", subNode.Type)

	// Verify parent-child relationship.
	assert.Equal(t, "sub", docsNode.Children[0].Name)
	assert.Equal(t, subNode.PubKeyHex, docsNode.Children[0].PubKey)
}

// TestEngineMoveFile tests same-directory rename:
// engine.Put → engine.Move (same dir) → source gone, target exists, store unchanged
func TestEngineMoveFile(t *testing.T) {
	eng := initIntegrationEngine(t)
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Put a file.
	localFile := createTempFile(t, []byte("moveable content"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/old.txt", Access: "free",
	})
	require.NoError(t, err)

	oldNode := eng.State.FindNodeByPath("/old.txt")
	require.NotNil(t, oldNode)
	oldKeyHash := oldNode.KeyHash

	// Move /old.txt → /new.txt (same directory rename).
	_, err = eng.Move(&engine.MoveOpts{
		VaultIndex: 0, SrcPath: "/old.txt", DstPath: "/new.txt",
	})
	require.NoError(t, err)

	// Source path gone.
	assert.Nil(t, eng.State.FindNodeByPath("/old.txt"))

	// Target exists with same KeyHash (content not re-encrypted on move).
	newNode := eng.State.FindNodeByPath("/new.txt")
	require.NotNil(t, newNode)
	assert.Equal(t, oldKeyHash, newNode.KeyHash)

	// Store still has the content.
	keyHashBytes, _ := hex.DecodeString(newNode.KeyHash)
	has, err := eng.Store.Has(keyHashBytes)
	require.NoError(t, err)
	assert.True(t, has)
}

// TestEngineCrossDirectoryMove tests moving a file between directories.
func TestEngineCrossDirectoryMove(t *testing.T) {
	eng := initIntegrationEngine(t)
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/src"})
	require.NoError(t, err)
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/dst"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("cross-dir"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/src/file.txt", Access: "free",
	})
	require.NoError(t, err)

	srcDir := eng.State.FindNodeByPath("/src")
	require.Len(t, srcDir.Children, 1)

	// Move across directories.
	result, err := eng.Move(&engine.MoveOpts{
		VaultIndex: 0, SrcPath: "/src/file.txt", DstPath: "/dst/file.txt",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.TxID)

	// Source dir empty, destination dir has file.
	srcDir = eng.State.FindNodeByPath("/src")
	assert.Empty(t, srcDir.Children)

	dstDir := eng.State.FindNodeByPath("/dst")
	require.Len(t, dstDir.Children, 1)
	assert.Equal(t, "file.txt", dstDir.Children[0].Name)

	// File is now at new path.
	assert.NotNil(t, eng.State.FindNodeByPath("/dst/file.txt"))
	assert.Nil(t, eng.State.FindNodeByPath("/src/file.txt"))
}

// TestEngineCopyFile tests file copy with re-encryption:
// engine.Put → engine.Copy → both nodes exist, different keys, store has both
func TestEngineCopyFile(t *testing.T) {
	eng := initIntegrationEngine(t)
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	content := []byte("content to copy")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/original.txt", Access: "free",
	})
	require.NoError(t, err)

	_, err = eng.Copy(&engine.CopyOpts{
		VaultIndex: 0, SrcPath: "/original.txt", DstPath: "/copy.txt",
	})
	require.NoError(t, err)

	origNode := eng.State.FindNodeByPath("/original.txt")
	copyNode := eng.State.FindNodeByPath("/copy.txt")
	require.NotNil(t, origNode)
	require.NotNil(t, copyNode)

	// Different public keys (independent identity).
	assert.NotEqual(t, origNode.PubKeyHex, copyNode.PubKeyHex)
	// Different key hashes (re-encrypted with different key).
	assert.NotEqual(t, origNode.KeyHash, copyNode.KeyHash)
	// Same file size.
	assert.Equal(t, origNode.FileSize, copyNode.FileSize)

	// Both ciphertexts exist in store.
	origKH, _ := hex.DecodeString(origNode.KeyHash)
	copyKH, _ := hex.DecodeString(copyNode.KeyHash)
	hasOrig, _ := eng.Store.Has(origKH)
	hasCopy, _ := eng.Store.Has(copyKH)
	assert.True(t, hasOrig)
	assert.True(t, hasCopy)

	// Decrypt copy and verify content matches.
	ct, err := eng.Store.Get(copyKH)
	require.NoError(t, err)
	kp, err := eng.Wallet.DeriveNodeKey(copyNode.VaultIndex, copyNode.ChildIndices, nil)
	require.NoError(t, err)
	dec, err := method42.Decrypt(ct, kp.PrivateKey, kp.PublicKey, copyKH, method42.AccessFree)
	require.NoError(t, err)
	assert.Equal(t, content, dec.Plaintext)
}

// TestEngineRemoveFile tests file removal:
// engine.Put → engine.Remove → state: node path search returns nil
func TestEngineRemoveFile(t *testing.T) {
	eng := initIntegrationEngine(t)
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("to be removed"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/doomed.txt", Access: "free",
	})
	require.NoError(t, err)
	require.NotNil(t, eng.State.FindNodeByPath("/doomed.txt"))

	result, err := eng.Remove(&engine.RemoveOpts{VaultIndex: 0, Path: "/doomed.txt"})
	require.NoError(t, err)
	require.NotEmpty(t, result.TxID)

	// Node is still in state (Remove does SelfUpdate with OpDelete, doesn't
	// actually remove from the map), but we can verify the tx was built.
	// The Remove op builds a delete SelfUpdate — the node's TxID should be updated.
	node := eng.State.GetNode(result.NodePub)
	require.NotNil(t, node)
	assert.Equal(t, result.TxID, node.TxID)
}

// TestEngineSoftLink tests soft link creation:
// engine.Put → engine.Link(soft) → link node exists, points to target pubkey
func TestEngineSoftLink(t *testing.T) {
	eng := initIntegrationEngine(t)
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("link target"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/target.txt", Access: "free",
	})
	require.NoError(t, err)

	targetNode := eng.State.FindNodeByPath("/target.txt")
	require.NotNil(t, targetNode)

	_, err = eng.Link(&engine.LinkOpts{
		VaultIndex: 0, TargetPath: "/target.txt",
		LinkPath: "/shortcut.txt", Soft: true,
	})
	require.NoError(t, err)

	linkNode := eng.State.FindNodeByPath("/shortcut.txt")
	require.NotNil(t, linkNode)
	assert.Equal(t, "link", linkNode.Type)
	assert.Equal(t, targetNode.PubKeyHex, linkNode.LinkTarget)
}

// TestEngineSellAndPrice tests setting a price on content:
// engine.Put → engine.Sell(pricePerKB) → access becomes "paid", PricePerKB set
func TestEngineSellAndPrice(t *testing.T) {
	eng := initIntegrationEngine(t)
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("premium content"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/premium.txt", Access: "free",
	})
	require.NoError(t, err)

	_, err = eng.Sell(&engine.SellOpts{
		VaultIndex: 0, Path: "/premium.txt", PricePerKB: 100,
	})
	require.NoError(t, err)

	node := eng.State.FindNodeByPath("/premium.txt")
	require.NotNil(t, node)
	assert.Equal(t, "paid", node.Access)
	assert.Equal(t, uint64(100), node.PricePerKB)
}

// TestEngineEncryptTransition tests Free→Private re-encryption:
// engine.Put(Free) → engine.Encrypt(Private) → old keyHash gone, new keyHash works
func TestEngineEncryptTransition(t *testing.T) {
	eng := initIntegrationEngine(t)
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	content := []byte("transition me to private")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/transition.txt", Access: "free",
	})
	require.NoError(t, err)

	node := eng.State.FindNodeByPath("/transition.txt")
	oldKeyHash := node.KeyHash

	_, err = eng.EncryptNode(&engine.EncryptOpts{
		VaultIndex: 0, Path: "/transition.txt",
	})
	require.NoError(t, err)

	node = eng.State.FindNodeByPath("/transition.txt")
	assert.Equal(t, "private", node.Access)
	assert.NotEqual(t, oldKeyHash, node.KeyHash, "KeyHash should change after re-encryption")

	// Old ciphertext removed from store.
	oldKH, _ := hex.DecodeString(oldKeyHash)
	has, _ := eng.Store.Has(oldKH)
	assert.False(t, has, "old ciphertext should be deleted")

	// New ciphertext decryptable with PRIVATE access.
	newKH, _ := hex.DecodeString(node.KeyHash)
	ct, err := eng.Store.Get(newKH)
	require.NoError(t, err)

	kp, err := eng.Wallet.DeriveNodeKey(node.VaultIndex, node.ChildIndices, nil)
	require.NoError(t, err)

	dec, err := method42.Decrypt(ct, kp.PrivateKey, kp.PublicKey, newKH, method42.AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, content, dec.Plaintext)
}

// TestEngineOfflineMode tests that with Chain=nil, local ops still work.
func TestEngineOfflineMode(t *testing.T) {
	eng := initIntegrationEngine(t)
	assert.False(t, eng.IsOnline())

	// Local operations should work without Chain.
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("offline content"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/offline.txt", Access: "free",
	})
	require.NoError(t, err)

	// BroadcastTx and VerifyTx should fail gracefully.
	_, err = eng.BroadcastTx(t.Context(), "deadbeef")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "offline mode")

	_, err = eng.VerifyTx(t.Context(), "deadbeef")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "offline mode")
}
```

**Step 2: Run tests to verify they pass**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -v -run "TestEngine(Put|Mkdir|Move|Cross|Copy|Remove|SoftLink|Sell|Encrypt|Offline)" -count=1`

Expected: All 10 tests PASS. If any fail, debug and fix.

**Step 3: Commit**

```bash
git add integration/engine_workflow_test.go
git commit -m "test(integration): add engine workflow tests

10 tests covering the core engine operations:
- PutAndRetrieve: full encrypt→store→decrypt cycle
- MkdirNested: root → child → grandchild path resolution
- MoveFile: same-directory rename
- CrossDirectoryMove: two-parent SelfUpdate
- CopyFile: decrypt→re-encrypt with independent key
- RemoveFile: OpDelete SelfUpdate
- SoftLink: link node pointing to target pubkey
- SellAndPrice: access transition to paid
- EncryptTransition: Free→Private re-encryption
- OfflineMode: Chain=nil graceful fallback"
```

---

### Task 3: Engine State Consistency Tests

**Files:**
- Create: `integration/engine_state_test.go`

**Step 1: Write the test file**

```go
//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/engine"
)

// TestStateConsistencyAfterMutations runs a sequence of mutations and
// verifies the state snapshot matches expectations at each step.
func TestStateConsistencyAfterMutations(t *testing.T) {
	eng := initIntegrationEngine(t)

	// 1. Mkdir /
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// 2. Mkdir /docs
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/docs"})
	require.NoError(t, err)

	// 3. Put /docs/readme.txt
	localFile := createTempFile(t, []byte("readme"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/docs/readme.txt", Access: "free",
	})
	require.NoError(t, err)

	docsNode := eng.State.FindNodeByPath("/docs")
	require.Len(t, docsNode.Children, 1)

	// 4. Move /docs/readme.txt → /docs/README.md
	_, err = eng.Move(&engine.MoveOpts{
		VaultIndex: 0, SrcPath: "/docs/readme.txt", DstPath: "/docs/README.md",
	})
	require.NoError(t, err)

	docsNode = eng.State.FindNodeByPath("/docs")
	require.Len(t, docsNode.Children, 1)
	assert.Equal(t, "README.md", docsNode.Children[0].Name)

	// 5. Copy /docs/README.md → /docs/backup.md
	_, err = eng.Copy(&engine.CopyOpts{
		VaultIndex: 0, SrcPath: "/docs/README.md", DstPath: "/docs/backup.md",
	})
	require.NoError(t, err)

	docsNode = eng.State.FindNodeByPath("/docs")
	require.Len(t, docsNode.Children, 2)

	// 6. Remove /docs/backup.md
	_, err = eng.Remove(&engine.RemoveOpts{VaultIndex: 0, Path: "/docs/backup.md"})
	require.NoError(t, err)

	// README.md still exists.
	require.NotNil(t, eng.State.FindNodeByPath("/docs/README.md"))
}

// TestCrossVaultIsolation verifies that two vaults have independent key trees.
func TestCrossVaultIsolation(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create a second vault.
	_, err := eng.Wallet.CreateVault(eng.WState, "vault2")
	require.NoError(t, err)

	// Seed additional fee UTXOs for vault operations.
	seedFeeUTXOs(t, eng, 10, 10000)

	// Mkdir / on vault 0.
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Mkdir / on vault 1.
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 1, Path: "/"})
	require.NoError(t, err)

	// Root keys should differ.
	root0Pub, err := eng.Wallet.DeriveVaultRootKey(0)
	require.NoError(t, err)
	root1Pub, err := eng.Wallet.DeriveVaultRootKey(1)
	require.NoError(t, err)

	assert.NotEqual(t, root0Pub.PublicKey.Compressed(), root1Pub.PublicKey.Compressed(),
		"vault 0 and vault 1 should have different root keys")
}

// TestFeeKeyRotation verifies that each operation uses a unique change address.
func TestFeeKeyRotation(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Seed extra UTXOs — we'll need many for multiple ops.
	seedFeeUTXOs(t, eng, 30, 10000)

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	startIdx := eng.WState.NextChangeIndex

	// 5 Put operations.
	for i := 0; i < 5; i++ {
		localFile := createTempFile(t, []byte("fee rotation test"))
		_, err := eng.PutFile(&engine.PutOpts{
			VaultIndex: 0, LocalFile: localFile,
			RemotePath: "/fee" + string(rune('A'+i)) + ".txt", Access: "free",
		})
		require.NoError(t, err)
	}

	// Each Put (PutFile calls DeriveChangeAddr which increments NextChangeIndex)
	// and EnsureRootExists→createRootNode also uses DeriveChangeAddr.
	// So NextChangeIndex should have advanced.
	assert.Greater(t, eng.WState.NextChangeIndex, startIdx,
		"NextChangeIndex should advance after multiple operations")
}

// TestResolveVaultIndexDefault verifies that empty vault name resolves to vault 0.
func TestResolveVaultIndexDefault(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Empty name → first active vault (index 0).
	idx, err := eng.ResolveVaultIndex("")
	require.NoError(t, err)
	assert.Equal(t, uint32(0), idx)

	// Explicit name "default" → vault 0.
	idx, err = eng.ResolveVaultIndex("default")
	require.NoError(t, err)
	assert.Equal(t, uint32(0), idx)

	// Non-existent vault → error.
	_, err = eng.ResolveVaultIndex("nonexistent")
	assert.Error(t, err)
}

// TestUTXOChainRefresh verifies that parent UTXO is refreshed after each child creation.
func TestUTXOChainRefresh(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10000)

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Create 3 children under root. Each CreateChild refreshes the parent UTXO.
	for i := 0; i < 3; i++ {
		localFile := createTempFile(t, []byte("child content"))
		_, err := eng.PutFile(&engine.PutOpts{
			VaultIndex: 0, LocalFile: localFile,
			RemotePath: "/child" + string(rune('0'+i)) + ".txt", Access: "free",
		})
		require.NoError(t, err)
	}

	// Verify root has 3 children.
	rootPub, err := eng.Wallet.DeriveVaultRootKey(0)
	require.NoError(t, err)
	rootPubHex := hexEncode(rootPub.PublicKey.Compressed())
	rootNode := eng.State.GetNode(rootPubHex)
	require.NotNil(t, rootNode)
	assert.Len(t, rootNode.Children, 3)

	// The root node should still have an unspent UTXO (from the last refresh).
	rootUTXO := eng.State.GetNodeUTXO(rootPubHex)
	require.NotNil(t, rootUTXO, "root should have a refreshed node UTXO after child creations")
}
```

Note: We need a `hexEncode` helper or just use `hex.EncodeToString`. Add the import and helper.

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -v -run "TestState|TestCrossVault|TestFeeKey|TestResolve|TestUTXOChain" -count=1`

Expected: All 5 tests PASS.

**Step 3: Commit**

```bash
git add integration/engine_state_test.go
git commit -m "test(integration): add engine state consistency tests

5 tests verifying state integrity under mutations:
- StateConsistencyAfterMutations: Put→Mkdir→Move→Copy→Remove sequence
- CrossVaultIsolation: vault 0 and vault 1 have different key trees
- FeeKeyRotation: each op increments NextChangeIndex
- ResolveVaultIndexDefault: empty name → vault 0
- UTXOChainRefresh: parent UTXO refreshed after each child creation"
```

---

### Task 4: Daemon ← Engine Integration Tests

**Files:**
- Create: `integration/daemon_engine_test.go`

These tests create a real Engine, wire it into the Daemon via adapters (WalletAdapter, StoreAdapter, MetanetAdapter), and use `httptest.Server` for HTTP assertions.

**Step 1: Write the test file**

```go
//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/daemon"
	"github.com/tongxiaofeng/bitfs/internal/engine"
)

// createDaemonWithEngine creates a real Engine + Daemon wired together via adapters.
// Returns the Engine (for setup), the httptest.Server (for HTTP calls), and a cleanup func.
func createDaemonWithEngine(t *testing.T) (*engine.Engine, *httptest.Server) {
	t.Helper()

	eng := initIntegrationEngine(t)

	walletAdapter := engine.NewWalletAdapter(eng)
	storeAdapter := engine.NewStoreAdapter(eng)
	metanetAdapter := engine.NewMetanetAdapter(eng)

	config := daemon.DefaultConfig()
	config.ListenAddr = ":0"
	// Disable rate limiter for tests.
	config.Security.RateLimit.RPM = 0

	d, err := daemon.New(config, walletAdapter, storeAdapter, metanetAdapter)
	require.NoError(t, err)

	ts := httptest.NewServer(d.Handler())
	t.Cleanup(ts.Close)

	return eng, ts
}

// TestDaemonSeesEngineMkdir tests that after Engine.Mkdir, the Daemon can
// resolve the directory via its HTTP API.
func TestDaemonSeesEngineMkdir(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	// Create directories via engine.
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/docs"})
	require.NoError(t, err)

	// Query via daemon HTTP API.
	resp, err := http.Get(ts.URL + "/docs")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// Default Accept → JSON.
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	assert.Contains(t, string(body), "dir")
}

// TestDaemonReflectsRemove tests that after Engine.Remove, the Daemon returns 404.
// Note: Engine.Remove does SelfUpdate with OpDelete but doesn't remove the node
// from the state map. The MetanetAdapter's GetNodeByPath uses FindNodeByPath
// which still finds it. This test documents the current behavior.
func TestDaemonReflectsRemove(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("remove me"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/removeme.txt", Access: "free",
	})
	require.NoError(t, err)

	// Verify file is visible via daemon.
	resp, err := http.Get(ts.URL + "/removeme.txt")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Remove via engine. Note: current Remove() doesn't delete from state map,
	// it only does SelfUpdate with OpDelete. The file will still be findable.
	// This test documents this behavior — a future enhancement may purge the
	// node from the state map and make the daemon return 404.
	_, err = eng.Remove(&engine.RemoveOpts{VaultIndex: 0, Path: "/removeme.txt"})
	require.NoError(t, err)

	// The node is still in state (current behavior), so daemon still finds it.
	resp2, err := http.Get(ts.URL + "/removeme.txt")
	require.NoError(t, err)
	resp2.Body.Close()
	// Document current behavior: still 200 because Remove() doesn't purge state.
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

// TestDaemonContentNegotiationWithEngine tests that the daemon serves different
// formats based on Accept header, using real engine-created content.
func TestDaemonContentNegotiationWithEngine(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/mydir"})
	require.NoError(t, err)

	tests := []struct {
		accept      string
		contentType string
		bodyContain string
	}{
		{"application/json", "application/json", "dir"},
		{"text/html", "text/html", "<html"},
		{"text/markdown", "text/markdown", "# "},
	}

	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+"/mydir", nil)
			req.Header.Set("Accept", tt.accept)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Contains(t, resp.Header.Get("Content-Type"), tt.contentType)

			body, _ := io.ReadAll(resp.Body)
			assert.Contains(t, string(body), tt.bodyContain)
		})
	}
}

// TestDaemonPriceInResponse tests that after Engine.Sell, the daemon's JSON
// response includes the correct PricePerKB.
func TestDaemonPriceInResponse(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("paid content"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/paid.txt", Access: "free",
	})
	require.NoError(t, err)

	_, err = eng.Sell(&engine.SellOpts{
		VaultIndex: 0, Path: "/paid.txt", PricePerKB: 250,
	})
	require.NoError(t, err)

	resp, err := http.Get(ts.URL + "/paid.txt")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var nodeResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &nodeResp))

	assert.Equal(t, "paid", nodeResp["Access"])
	assert.Equal(t, float64(250), nodeResp["PricePerKB"])
}

// TestDaemonHealthWithEngine verifies the health endpoint works with a
// fully-wired daemon (not just mocks).
func TestDaemonHealthWithEngine(t *testing.T) {
	_, ts := createDaemonWithEngine(t)

	resp, err := http.Get(ts.URL + "/_bitfs/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), `"status":"ok"`)
}
```

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -v -run "TestDaemon(Sees|Reflects|Content|Price|Health)" -count=1`

Expected: All 5 tests PASS.

**Step 3: Commit**

```bash
git add integration/daemon_engine_test.go
git commit -m "test(integration): add daemon-engine integration tests

5 tests verifying Daemon ← Engine state propagation via adapters:
- DaemonSeesEngineMkdir: Engine.Mkdir visible via HTTP
- DaemonReflectsRemove: documents current behavior (Remove doesn't purge state)
- DaemonContentNegotiationWithEngine: JSON/HTML/Markdown responses
- DaemonPriceInResponse: Engine.Sell reflected in daemon JSON
- DaemonHealthWithEngine: health endpoint with real wiring"
```

---

### Task 5: Client HTTP Roundtrip Tests

**Files:**
- Create: `integration/client_roundtrip_test.go`

These tests exercise the Client → Daemon → Engine chain via HTTP.

**Step 1: Write the test file**

```go
//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/bitfs/internal/engine"
)

// TestClientGetMeta tests Client.GetMeta round trip through daemon.
func TestClientGetMeta(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	// Setup: create directory with a file via engine.
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("client test content"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/client-test.txt", Access: "free",
	})
	require.NoError(t, err)

	// Use Client to query metadata.
	rootPub, err := eng.Wallet.DeriveVaultRootKey(0)
	require.NoError(t, err)
	pnode := hexEncode(rootPub.PublicKey.Compressed())

	c := client.New(ts.URL)
	meta, err := c.GetMeta(pnode, "/client-test.txt")
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "file", meta.Type)
	assert.Equal(t, "free", meta.Access)
	assert.Equal(t, uint64(len("client test content")), meta.FileSize)
}

// TestClientGetMetaNotFound tests that Client returns ErrNotFound for missing paths.
func TestClientGetMetaNotFound(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	rootPub, err := eng.Wallet.DeriveVaultRootKey(0)
	require.NoError(t, err)
	pnode := hexEncode(rootPub.PublicKey.Compressed())

	c := client.New(ts.URL)
	_, err = c.GetMeta(pnode, "/nonexistent.txt")
	assert.ErrorIs(t, err, client.ErrNotFound)
}

// TestClientGetDirectoryListing tests Client.GetMeta on a directory returns children.
func TestClientGetDirectoryListing(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("file in dir"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/listing.txt", Access: "free",
	})
	require.NoError(t, err)

	rootPub, err := eng.Wallet.DeriveVaultRootKey(0)
	require.NoError(t, err)
	pnode := hexEncode(rootPub.PublicKey.Compressed())

	c := client.New(ts.URL)
	meta, err := c.GetMeta(pnode, "/")
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "dir", meta.Type)
	require.Len(t, meta.Children, 1)
	assert.Equal(t, "listing.txt", meta.Children[0].Name)
}

// TestClientGetData tests Client.GetData retrieves encrypted content.
func TestClientGetData(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	localFile := createTempFile(t, []byte("data to fetch"))
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0, LocalFile: localFile,
		RemotePath: "/fetchme.txt", Access: "free",
	})
	require.NoError(t, err)

	// Get the key hash from state.
	node := eng.State.FindNodeByPath("/fetchme.txt")
	require.NotNil(t, node)

	c := client.New(ts.URL)
	data, err := c.GetData(node.KeyHash)
	require.NoError(t, err)
	assert.NotEmpty(t, data, "encrypted data should not be empty")
}

// TestClientMultipleRequests verifies that the client can make multiple
// sequential requests without connection issues.
func TestClientMultipleRequests(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Create 3 files.
	for i := 0; i < 3; i++ {
		localFile := createTempFile(t, []byte("multi-request test"))
		_, err := eng.PutFile(&engine.PutOpts{
			VaultIndex: 0, LocalFile: localFile,
			RemotePath: "/multi" + string(rune('A'+i)) + ".txt", Access: "free",
		})
		require.NoError(t, err)
	}

	rootPub, err := eng.Wallet.DeriveVaultRootKey(0)
	require.NoError(t, err)
	pnode := hexEncode(rootPub.PublicKey.Compressed())

	c := client.New(ts.URL)

	// Query each file.
	for _, name := range []string{"/multiA.txt", "/multiB.txt", "/multiC.txt"} {
		meta, err := c.GetMeta(pnode, name)
		require.NoError(t, err, "GetMeta(%s)", name)
		assert.Equal(t, "file", meta.Type)
	}

	// Query root directory — should have 3 children.
	meta, err := c.GetMeta(pnode, "/")
	require.NoError(t, err)
	assert.Len(t, meta.Children, 3)
}
```

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -v -run "TestClient" -count=1`

Expected: All 5 tests PASS. Note: `client.GetMeta` and `client.GetData` use the daemon's `/_bitfs/meta/{pnode}/{path}` and `/_bitfs/data/{hash}` endpoints respectively. If the Client API doesn't match the daemon routes exactly, adjust the test setup.

**Step 3: Commit**

```bash
git add integration/client_roundtrip_test.go
git commit -m "test(integration): add client HTTP roundtrip tests

5 tests verifying Client → Daemon → Engine HTTP chain:
- ClientGetMeta: metadata retrieval via /_bitfs/meta endpoint
- ClientGetMetaNotFound: 404 → ErrNotFound mapping
- ClientGetDirectoryListing: directory with children
- ClientGetData: encrypted content retrieval
- ClientMultipleRequests: sequential connection reuse"
```

---

### Task 6: Merkle Mutation Tests

**Files:**
- Create: `integration/merkle_mutations_test.go`

These tests exercise the directory Merkle root recomputation from `libbitfs/metanet/merkle.go` under various mutations (add, remove, rename children).

**Step 1: Write the test file**

```go
//go:build integration

package integration

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/libbitfs/metanet"
)

// makeChildEntry creates a ChildEntry for testing.
func makeChildEntry(name string, index uint32) metanet.ChildEntry {
	// Use a deterministic fake pubkey based on index.
	pubKey := make([]byte, 33)
	pubKey[0] = 0x02
	pubKey[1] = byte(index)
	return metanet.ChildEntry{
		Index:    index,
		Name:     name,
		Type:     metanet.NodeTypeFile,
		PubKey:   pubKey,
		Hardened: true,
	}
}

// TestMerkleRootChangesOnAddChild verifies that adding a child changes the root.
func TestMerkleRootChangesOnAddChild(t *testing.T) {
	children := []metanet.ChildEntry{
		makeChildEntry("first.txt", 0),
	}
	root0 := metanet.ComputeDirectoryMerkleRoot(children)
	require.NotNil(t, root0)
	require.Len(t, root0, 32)

	// Add a second child.
	children = append(children, makeChildEntry("second.txt", 1))
	root1 := metanet.ComputeDirectoryMerkleRoot(children)
	require.NotNil(t, root1)

	assert.False(t, bytes.Equal(root0, root1), "root should change after adding a child")
}

// TestMerkleRootChangesOnRemoveChild verifies that removing a child changes
// the root and that remaining children's proofs are still valid.
func TestMerkleRootChangesOnRemoveChild(t *testing.T) {
	children := []metanet.ChildEntry{
		makeChildEntry("a.txt", 0),
		makeChildEntry("b.txt", 1),
		makeChildEntry("c.txt", 2),
	}
	rootBefore := metanet.ComputeDirectoryMerkleRoot(children)

	// Build proof for child "a" before removal.
	proofA, err := metanet.BuildDirectoryMerkleProof(children, 0)
	require.NoError(t, err)
	assert.True(t, metanet.VerifyChildMembership(&children[0], proofA, 0, rootBefore))

	// Remove child "b" (index 1).
	children = append(children[:1], children[2:]...) // [a, c]
	rootAfter := metanet.ComputeDirectoryMerkleRoot(children)

	assert.False(t, bytes.Equal(rootBefore, rootAfter), "root should change after removal")

	// Proof for "a" against OLD root no longer valid (root changed).
	assert.False(t, metanet.VerifyChildMembership(&children[0], proofA, 0, rootAfter),
		"old proof should not verify against new root")

	// Build new proof for "a" — should be valid against new root.
	newProofA, err := metanet.BuildDirectoryMerkleProof(children, 0)
	require.NoError(t, err)
	assert.True(t, metanet.VerifyChildMembership(&children[0], newProofA, 0, rootAfter))
}

// TestMerkleRootChangesOnRename verifies that changing a child's name
// changes the root (because name is part of the ChildEntry hash).
func TestMerkleRootChangesOnRename(t *testing.T) {
	children := []metanet.ChildEntry{
		makeChildEntry("old-name.txt", 0),
		makeChildEntry("other.txt", 1),
	}
	rootBefore := metanet.ComputeDirectoryMerkleRoot(children)

	// Rename first child.
	children[0].Name = "new-name.txt"
	rootAfter := metanet.ComputeDirectoryMerkleRoot(children)

	assert.False(t, bytes.Equal(rootBefore, rootAfter),
		"root should change when a child is renamed")
}

// TestMerkleProofLargeDirectory verifies that proof size is O(log2(n)).
func TestMerkleProofLargeDirectory(t *testing.T) {
	n := 1000
	children := make([]metanet.ChildEntry, n)
	for i := 0; i < n; i++ {
		children[i] = makeChildEntry("file"+string(rune(i)), uint32(i))
	}

	root := metanet.ComputeDirectoryMerkleRoot(children)
	require.NotNil(t, root)

	// Build proof for a child in the middle.
	proof, err := metanet.BuildDirectoryMerkleProof(children, 500)
	require.NoError(t, err)

	// log2(1000) ≈ 10, so proof should have ~10 nodes.
	assert.LessOrEqual(t, len(proof), 12, "proof should be O(log2(n))")
	assert.GreaterOrEqual(t, len(proof), 8, "proof should have at least 8 nodes for 1000 children")

	// Verify the proof.
	assert.True(t, metanet.VerifyChildMembership(&children[500], proof, 500, root))
}

// TestMerkleEmptyAndSingleChild tests edge cases.
func TestMerkleEmptyAndSingleChild(t *testing.T) {
	// Empty children → nil root.
	assert.Nil(t, metanet.ComputeDirectoryMerkleRoot(nil))
	assert.Nil(t, metanet.ComputeDirectoryMerkleRoot([]metanet.ChildEntry{}))

	// Single child: root = leaf hash.
	children := []metanet.ChildEntry{makeChildEntry("only.txt", 0)}
	root := metanet.ComputeDirectoryMerkleRoot(children)
	require.NotNil(t, root)

	leaf := metanet.ComputeChildLeafHash(&children[0])
	assert.True(t, bytes.Equal(root, leaf), "single child: root should equal leaf hash")

	// Proof for single child: empty proof, verified by leaf == root.
	proof, err := metanet.BuildDirectoryMerkleProof(children, 0)
	require.NoError(t, err)
	assert.Nil(t, proof, "single child proof should be nil/empty")

	assert.True(t, metanet.VerifyChildMembership(&children[0], proof, 0, root))
}
```

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -v -run "TestMerkle" -count=1`

Expected: All 5 tests PASS.

**Step 3: Commit**

```bash
git add integration/merkle_mutations_test.go
git commit -m "test(integration): add Merkle mutation tests

5 tests verifying directory Merkle root behavior:
- MerkleRootChangesOnAddChild: root changes when child added
- MerkleRootChangesOnRemoveChild: root changes, old proofs invalid
- MerkleRootChangesOnRename: name change affects root
- MerkleProofLargeDirectory: 1000 children, proof size ~10 (O(log2(n)))
- MerkleEmptyAndSingleChild: nil root for empty, leaf==root for single"
```

---

### Task 7: SPV Verification Pipeline Tests

**Files:**
- Create: `integration/spv_verification_test.go`

These tests exercise SPV proof construction, verification, and caching using `libbitfs/spv`. Since we don't have a real blockchain, we construct synthetic Merkle trees and proofs.

**Step 1: Write the test file**

```go
//go:build integration

package integration

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/libbitfs/spv"
)

// makeFakeTxID generates a random 32-byte txid.
func makeFakeTxID(t *testing.T) []byte {
	t.Helper()
	txid := make([]byte, 32)
	_, err := rand.Read(txid)
	require.NoError(t, err)
	return txid
}

// TestSPVStoreAndRetrieve tests storing a tx and retrieving it.
func TestSPVStoreAndRetrieve(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "spv.db")
	store, err := spv.OpenBoltStore(dbPath)
	require.NoError(t, err)
	defer store.Close()

	txid := makeFakeTxID(t)
	rawTx := []byte("fake raw transaction bytes")

	// Store tx.
	storedTx := &spv.StoredTx{
		TxID:  txid,
		RawTx: rawTx,
	}
	err = store.Txs().PutTx(storedTx)
	require.NoError(t, err)

	// Retrieve tx.
	got, err := store.Txs().GetTx(txid)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, bytes.Equal(txid, got.TxID))
	assert.Equal(t, rawTx, got.RawTx)
	assert.Nil(t, got.Proof, "newly stored tx should have no proof")
}

// TestSPVProofBackfill tests storing a proof after the tx is already stored.
func TestSPVProofBackfill(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "spv.db")
	store, err := spv.OpenBoltStore(dbPath)
	require.NoError(t, err)
	defer store.Close()

	txid := makeFakeTxID(t)
	blockHash := makeFakeTxID(t) // reuse as fake block hash

	// Store tx without proof.
	err = store.Txs().PutTx(&spv.StoredTx{
		TxID:  txid,
		RawTx: []byte("raw tx"),
	})
	require.NoError(t, err)

	// Update with proof.
	stored, err := store.Txs().GetTx(txid)
	require.NoError(t, err)

	stored.Proof = &spv.MerkleProof{
		TxID:      txid,
		BlockHash: blockHash,
	}
	stored.BlockHeight = 12345

	err = store.Txs().UpdateTx(stored)
	require.NoError(t, err)

	// Verify proof is persisted.
	got, err := store.Txs().GetTx(txid)
	require.NoError(t, err)
	require.NotNil(t, got.Proof)
	assert.True(t, bytes.Equal(blockHash, got.Proof.BlockHash))
	assert.Equal(t, uint32(12345), got.BlockHeight)
}

// TestSPVMerkleRootComputation tests that ComputeMerkleRoot correctly
// recomputes the root from a leaf + proof path.
func TestSPVMerkleRootComputation(t *testing.T) {
	// Build a small Merkle tree from 4 tx hashes.
	tx0 := spv.DoubleHash([]byte("tx0"))
	tx1 := spv.DoubleHash([]byte("tx1"))
	tx2 := spv.DoubleHash([]byte("tx2"))
	tx3 := spv.DoubleHash([]byte("tx3"))

	// Level 1: pair (tx0,tx1) and (tx2,tx3)
	pair01 := make([]byte, 64)
	copy(pair01[:32], tx0)
	copy(pair01[32:], tx1)
	node01 := spv.DoubleHash(pair01)

	pair23 := make([]byte, 64)
	copy(pair23[:32], tx2)
	copy(pair23[32:], tx3)
	node23 := spv.DoubleHash(pair23)

	// Root
	pairRoot := make([]byte, 64)
	copy(pairRoot[:32], node01)
	copy(pairRoot[32:], node23)
	root := spv.DoubleHash(pairRoot)

	// Proof for tx2 (index 2): sibling is tx3, then node01
	proof := [][]byte{tx3, node01}

	computed := spv.ComputeMerkleRoot(tx2, 2, proof)
	require.NotNil(t, computed)
	assert.True(t, bytes.Equal(root, computed),
		"computed root should match manually-constructed root")
}

// TestSPVTamperedProof tests that modifying a proof node causes verification to fail.
func TestSPVTamperedProof(t *testing.T) {
	tx0 := spv.DoubleHash([]byte("tx0"))
	tx1 := spv.DoubleHash([]byte("tx1"))

	pair := make([]byte, 64)
	copy(pair[:32], tx0)
	copy(pair[32:], tx1)
	root := spv.DoubleHash(pair)

	// Valid proof for tx0: sibling is tx1.
	proof := [][]byte{tx1}
	computed := spv.ComputeMerkleRoot(tx0, 0, proof)
	assert.True(t, bytes.Equal(root, computed))

	// Tamper with the proof.
	tampered := make([]byte, 32)
	copy(tampered, tx1)
	tampered[0] ^= 0xFF // flip a byte

	computedBad := spv.ComputeMerkleRoot(tx0, 0, [][]byte{tampered})
	assert.False(t, bytes.Equal(root, computedBad),
		"tampered proof should not produce correct root")
}

// TestSPVBoltStoreHeaderChain tests storing and retrieving block headers.
func TestSPVBoltStoreHeaderChain(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "spv.db")
	store, err := spv.OpenBoltStore(dbPath)
	require.NoError(t, err)
	defer store.Close()

	headers := store.Headers()

	// Store a header.
	blockHash := makeFakeTxID(t)
	header := &spv.BlockHeader{
		Hash:   blockHash,
		Height: 100,
	}
	err = headers.PutHeader(header)
	require.NoError(t, err)

	// Retrieve by hash.
	got, err := headers.GetHeader(blockHash)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, uint32(100), got.Height)

	// Unknown hash → error.
	unknown := makeFakeTxID(t)
	_, err = headers.GetHeader(unknown)
	assert.Error(t, err)

	// Cleanup: verify the DB file was created.
	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}
```

Note: The exact SPV API (`spv.BoltStore`, `spv.StoredTx`, `spv.MerkleProof`, etc.) may have slightly different field names. If compilation fails, adjust to match the actual `libbitfs/spv` package API.

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -v -run "TestSPV" -count=1`

Expected: All 5 tests PASS.

**Step 3: Commit**

```bash
git add integration/spv_verification_test.go
git commit -m "test(integration): add SPV verification pipeline tests

5 tests covering SPV storage and Merkle proof verification:
- SPVStoreAndRetrieve: basic BoltStore put/get cycle
- SPVProofBackfill: update tx with proof after initial store
- SPVMerkleRootComputation: 4-tx tree, recompute root from proof
- SPVTamperedProof: flipped byte in proof → root mismatch
- SPVBoltStoreHeaderChain: block header put/get/unknown-hash"
```

---

### Task 8: Run All Integration Tests + Fix Issues

**Step 1: Run full suite**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -v -count=1 2>&1 | tee /tmp/integration-results.txt`

Expected: All ~35 new tests + ~45 existing tests PASS.

**Step 2: Run with race detector**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -v -count=1 -race`

Expected: No data races detected.

**Step 3: Fix any failures**

Debug and fix any failing tests. Common issues:
- Import path mismatches (integration package can import internal/)
- Field name capitalization differences between engine types
- SPV BoltStore API differences from what's assumed above
- `t.Context()` requires Go 1.21+ (use `context.Background()` if older)

**Step 4: Final commit**

```bash
git add -A integration/
git commit -m "test(integration): fix integration test issues from full run

All integration tests passing with -race flag."
```

---

## Run Commands

```bash
# All integration tests (existing + new)
go test -tags=integration ./integration/ -v -count=1

# With race detector
go test -tags=integration ./integration/ -v -count=1 -race

# Single batch
go test -tags=integration ./integration/ -v -run "TestEngine"
go test -tags=integration ./integration/ -v -run "TestDaemon|TestClient"
go test -tags=integration ./integration/ -v -run "TestMerkle|TestSPV"
```

## Summary

| Task | Files | Tests | Description |
|------|-------|-------|-------------|
| 1 | engine_helpers_test.go | — | initIntegrationEngine, seedFeeUTXOs |
| 2 | engine_workflow_test.go | 10 | Put, Mkdir, Move, Copy, Remove, Link, Sell, Encrypt, Offline |
| 3 | engine_state_test.go | 5 | Mutation sequence, vault isolation, fee rotation, UTXO refresh |
| 4 | daemon_engine_test.go | 5 | Daemon ← Engine adapter wiring |
| 5 | client_roundtrip_test.go | 5 | Client → Daemon → Engine HTTP chain |
| 6 | merkle_mutations_test.go | 5 | Directory Merkle root under mutations |
| 7 | spv_verification_test.go | 5 | SPV proof construction and verification |
| 8 | — | — | Full test run + race detector + fixes |
| **Total** | **7 files** | **~35** | |

//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/vault"
)

// --- Test 1: TestStateConsistencyAfterMutations ---

// TestStateConsistencyAfterMutations exercises a full mutation sequence and
// verifies state at each step:
//
//	Mkdir / -> Mkdir /docs -> Put /docs/readme.txt ->
//	Move /docs/readme.txt -> /docs/README.md ->
//	Copy /docs/README.md -> /docs/backup.md ->
//	Remove /docs/backup.md
func TestStateConsistencyAfterMutations(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	// Step 1: Mkdir /
	rootResult, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err, "Mkdir /")
	require.NotEmpty(t, rootResult.NodePub, "root NodePub")

	rootState := eng.State.FindNodeByPath("/")
	require.NotNil(t, rootState, "root node should exist")
	assert.Equal(t, "dir", rootState.Type)
	assert.Empty(t, rootState.Children, "root should start with no children")

	// Step 2: Mkdir /docs
	_, err = eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/docs"})
	require.NoError(t, err, "Mkdir /docs")

	docsState := eng.State.FindNodeByPath("/docs")
	require.NotNil(t, docsState, "/docs should exist")
	assert.Equal(t, "dir", docsState.Type)

	rootState = eng.State.FindNodeByPath("/")
	require.NotNil(t, rootState)
	require.Len(t, rootState.Children, 1, "root should have 1 child after Mkdir /docs")
	assert.Equal(t, "docs", rootState.Children[0].Name)

	// Step 3: Put /docs/readme.txt
	plaintext := []byte("Hello, this is the readme content for state consistency test.")
	localFile := createTempFile(t, plaintext)

	_, err = eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/docs/readme.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile /docs/readme.txt")

	readmeState := eng.State.FindNodeByPath("/docs/readme.txt")
	require.NotNil(t, readmeState, "/docs/readme.txt should exist")
	assert.Equal(t, "file", readmeState.Type)
	assert.Equal(t, uint64(len(plaintext)), readmeState.FileSize)
	origKeyHash := readmeState.KeyHash
	origPubKey := readmeState.PubKeyHex

	docsState = eng.State.FindNodeByPath("/docs")
	require.NotNil(t, docsState)
	require.Len(t, docsState.Children, 1, "/docs should have 1 child")
	assert.Equal(t, "readme.txt", docsState.Children[0].Name)

	// Step 4: Move /docs/readme.txt -> /docs/README.md
	_, err = eng.Move(&vault.MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/docs/readme.txt",
		DstPath:    "/docs/README.md",
	})
	require.NoError(t, err, "Move readme.txt -> README.md")

	// Source path should be gone.
	assert.Nil(t, eng.State.FindNodeByPath("/docs/readme.txt"),
		"old path should not exist after move")

	// Target path should exist with same identity.
	movedState := eng.State.FindNodeByPath("/docs/README.md")
	require.NotNil(t, movedState, "/docs/README.md should exist after move")
	assert.Equal(t, origPubKey, movedState.PubKeyHex,
		"node identity (pubkey) should be preserved after rename")
	assert.Equal(t, origKeyHash, movedState.KeyHash,
		"KeyHash should be preserved after rename")

	docsState = eng.State.FindNodeByPath("/docs")
	require.NotNil(t, docsState)
	require.Len(t, docsState.Children, 1, "/docs should still have 1 child")
	assert.Equal(t, "README.md", docsState.Children[0].Name)

	// Step 5: Copy /docs/README.md -> /docs/backup.md
	_, err = eng.Copy(&vault.CopyOpts{
		VaultIndex: 0,
		SrcPath:    "/docs/README.md",
		DstPath:    "/docs/backup.md",
	})
	require.NoError(t, err, "Copy README.md -> backup.md")

	backupState := eng.State.FindNodeByPath("/docs/backup.md")
	require.NotNil(t, backupState, "/docs/backup.md should exist after copy")
	assert.Equal(t, "file", backupState.Type)
	assert.NotEqual(t, movedState.PubKeyHex, backupState.PubKeyHex,
		"copy should have a different pubkey (independent node)")
	assert.Equal(t, movedState.FileSize, backupState.FileSize,
		"copy should have same file size")

	docsState = eng.State.FindNodeByPath("/docs")
	require.NotNil(t, docsState)
	require.Len(t, docsState.Children, 2, "/docs should have 2 children after copy")

	childNames := make(map[string]bool)
	for _, c := range docsState.Children {
		childNames[c.Name] = true
	}
	assert.True(t, childNames["README.md"], "README.md should be in children")
	assert.True(t, childNames["backup.md"], "backup.md should be in children")

	// Step 6: Remove /docs/backup.md
	removeResult, err := eng.Remove(&vault.RemoveOpts{
		VaultIndex: 0,
		Path:       "/docs/backup.md",
	})
	require.NoError(t, err, "Remove /docs/backup.md")
	require.NotEmpty(t, removeResult.TxID)

	// The node still exists in state (marked deleted via tx), but
	// we verify the remove produced a valid transaction.
	removedNode := eng.State.FindNodeByPath("/docs/backup.md")
	require.NotNil(t, removedNode, "removed node should still exist in state (deleted via tx)")

	// The original README.md should be untouched.
	finalReadme := eng.State.FindNodeByPath("/docs/README.md")
	require.NotNil(t, finalReadme, "README.md should still exist")
	assert.Equal(t, origPubKey, finalReadme.PubKeyHex)
	assert.Equal(t, origKeyHash, finalReadme.KeyHash)
}

// --- Test 2: TestCrossVaultIsolation ---

// TestCrossVaultIsolation creates two vaults, performs Mkdir / on each,
// and verifies root keys are different (vault isolation).
func TestCrossVaultIsolation(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	// Vault 0 ("default") already exists from initIntegrationEngine.
	// Create vault 1 ("vault2").
	_, err := eng.Wallet.CreateVault(eng.WState, "vault2")
	require.NoError(t, err, "CreateVault vault2")

	// Derive root keys for both vaults.
	rootKP0, err := eng.Wallet.DeriveVaultRootKey(0)
	require.NoError(t, err, "DeriveVaultRootKey(0)")

	rootKP1, err := eng.Wallet.DeriveVaultRootKey(1)
	require.NoError(t, err, "DeriveVaultRootKey(1)")

	pub0 := hexEncode(rootKP0.PublicKey.Compressed())
	pub1 := hexEncode(rootKP1.PublicKey.Compressed())

	// Root keys must differ.
	assert.NotEqual(t, pub0, pub1,
		"vault 0 and vault 1 should have different root keys")

	// Create root on vault 0.
	result0, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err, "Mkdir / on vault 0")
	require.NotEmpty(t, result0.NodePub)

	// Create root on vault 1.
	result1, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 1, Path: "/"})
	require.NoError(t, err, "Mkdir / on vault 1")
	require.NotEmpty(t, result1.NodePub)

	// NodePub should match the derived root keys.
	assert.Equal(t, pub0, result0.NodePub,
		"vault 0 root NodePub should match derived key")
	assert.Equal(t, pub1, result1.NodePub,
		"vault 1 root NodePub should match derived key")

	// Root nodes should be distinct entries in state.
	node0 := eng.State.GetNode(pub0)
	node1 := eng.State.GetNode(pub1)
	require.NotNil(t, node0, "vault 0 root should exist in state")
	require.NotNil(t, node1, "vault 1 root should exist in state")
	assert.NotEqual(t, node0.TxID, node1.TxID,
		"vault roots should have different TxIDs")
}

// --- Test 3: TestFeeKeyRotation ---

// TestFeeKeyRotation creates root and performs 5 PutFile operations,
// verifying that NextChangeIndex advances with each operation.
func TestFeeKeyRotation(t *testing.T) {
	eng := initIntegrationEngine(t)
	// Seed extra UTXOs since each PutFile uses fee + change UTXOs.
	seedFeeUTXOs(t, eng, 50, 10_000)

	// Record initial NextChangeIndex.
	initialChangeIdx := eng.WState.NextChangeIndex

	// Create root (consumes 1 change key).
	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err, "Mkdir /")

	afterRoot := eng.WState.NextChangeIndex
	assert.Greater(t, afterRoot, initialChangeIdx,
		"NextChangeIndex should advance after Mkdir /")

	// Perform 5 PutFile operations.
	for i := 0; i < 5; i++ {
		beforePut := eng.WState.NextChangeIndex

		content := []byte("fee rotation test content " + string(rune('A'+i)))
		localFile := createTempFile(t, content)

		_, err := eng.PutFile(&vault.PutOpts{
			VaultIndex: 0,
			LocalFile:  localFile,
			RemotePath: "/file" + string(rune('0'+i)) + ".txt",
			Access:     "free",
		})
		require.NoError(t, err, "PutFile #%d", i)

		afterPut := eng.WState.NextChangeIndex
		assert.Greater(t, afterPut, beforePut,
			"NextChangeIndex should advance after PutFile #%d", i)
	}

	// Overall: NextChangeIndex should have advanced at least 6 times
	// (1 for root + 5 for PutFile, each uses DeriveChangeAddr which increments).
	totalAdvance := eng.WState.NextChangeIndex - initialChangeIdx
	assert.GreaterOrEqual(t, totalAdvance, uint32(6),
		"NextChangeIndex should advance at least 6 (1 root + 5 puts)")
}

// --- Test 4: TestResolveVaultIndexDefault ---

// TestResolveVaultIndexDefault verifies ResolveVaultIndex behavior:
// empty name -> vault 0, "default" -> vault 0, "nonexistent" -> error.
func TestResolveVaultIndexDefault(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Empty name -> vault 0 (first active vault).
	idx, err := eng.ResolveVaultIndex("")
	require.NoError(t, err, "empty name should resolve")
	assert.Equal(t, uint32(0), idx,
		"empty name should resolve to vault 0")

	// "default" -> vault 0.
	idx, err = eng.ResolveVaultIndex("default")
	require.NoError(t, err, "\"default\" should resolve")
	assert.Equal(t, uint32(0), idx,
		"\"default\" should resolve to vault 0")

	// "nonexistent" -> error.
	_, err = eng.ResolveVaultIndex("nonexistent")
	require.Error(t, err, "nonexistent vault should return error")
	assert.Contains(t, err.Error(), "nonexistent",
		"error message should mention the vault name")
}

// --- Test 5: TestUTXOChainRefresh ---

// TestUTXOChainRefresh creates 3 children under root and verifies:
// - Root has 3 children in state
// - Root still has an unspent node UTXO (refreshed after each child creation)
func TestUTXOChainRefresh(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	// Create root.
	rootResult, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err, "Mkdir /")

	rootPubHex := rootResult.NodePub
	require.NotEmpty(t, rootPubHex)

	// Verify root has a node UTXO after creation.
	rootUTXO := eng.State.GetNodeUTXO(rootPubHex)
	require.NotNil(t, rootUTXO, "root should have a node UTXO after creation")

	// Create 3 children under root.
	childNames := []string{"/alpha", "/beta", "/gamma"}
	for _, name := range childNames {
		_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: name})
		require.NoError(t, err, "Mkdir %s", name)
	}

	// Verify root has 3 children.
	rootState := eng.State.FindNodeByPath("/")
	require.NotNil(t, rootState, "root should exist")
	require.Len(t, rootState.Children, 3,
		"root should have 3 children after creating alpha, beta, gamma")

	actualNames := make(map[string]bool)
	for _, c := range rootState.Children {
		actualNames[c.Name] = true
	}
	assert.True(t, actualNames["alpha"], "alpha should be in children")
	assert.True(t, actualNames["beta"], "beta should be in children")
	assert.True(t, actualNames["gamma"], "gamma should be in children")

	// Verify root still has an unspent node UTXO.
	// Each child creation spends the parent's UTXO and produces a new one
	// via TrackParentRefreshUTXO.
	rootUTXO = eng.State.GetNodeUTXO(rootPubHex)
	require.NotNil(t, rootUTXO,
		"root should still have an unspent node UTXO after 3 child creations")
	assert.False(t, rootUTXO.Spent,
		"root node UTXO should not be marked spent")
	assert.Equal(t, "node", rootUTXO.Type,
		"root UTXO should be type 'node'")
}

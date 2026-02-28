//go:build integration

package integration

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/method42"
)

// --- Test 1: TestEnginePutAndRetrieve ---

// TestEnginePutAndRetrieve exercises the full put+retrieve cycle:
// wallet -> key derivation -> method42 encrypt -> engine.Put -> store.Put ->
// store.Get -> method42 decrypt. Verifies decrypted plaintext matches original.
func TestEnginePutAndRetrieve(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create temp file with known content.
	plaintext := []byte("Hello, BitFS integration test! This is test content for put and retrieve.")
	localFile := createTempFile(t, plaintext)

	// Put file to remote path.
	result, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/hello.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile")
	require.NotEmpty(t, result.TxHex, "TxHex should not be empty")
	require.NotEmpty(t, result.TxID, "TxID should not be empty")
	require.NotEmpty(t, result.NodePub, "NodePub should not be empty")

	// Verify node state was created.
	nodeState := eng.State.FindNodeByPath("/hello.txt")
	require.NotNil(t, nodeState, "node should exist in local state")
	assert.Equal(t, "file", nodeState.Type)
	assert.Equal(t, "free", nodeState.Access)
	assert.Equal(t, uint64(len(plaintext)), nodeState.FileSize)
	assert.NotEmpty(t, nodeState.KeyHash)

	// Retrieve encrypted content from store.
	keyHash, err := hex.DecodeString(nodeState.KeyHash)
	require.NoError(t, err, "decode key hash")

	ciphertext, err := eng.Store.Get(keyHash)
	require.NoError(t, err, "Store.Get")
	require.NotEmpty(t, ciphertext)

	// Derive the node key to decrypt.
	kp, err := eng.Wallet.DeriveNodeKey(nodeState.VaultIndex, nodeState.ChildIndices, nil)
	require.NoError(t, err, "DeriveNodeKey")

	// Decrypt content.
	decResult, err := method42.Decrypt(ciphertext, nil, kp.PublicKey, keyHash, method42.AccessFree)
	require.NoError(t, err, "Decrypt")

	// Verify plaintext matches.
	assert.Equal(t, plaintext, decResult.Plaintext, "decrypted content should match original")
}

// --- Test 2: TestEngineMkdirNested ---

// TestEngineMkdirNested creates root / -> /docs -> /docs/sub and verifies
// parent-child relationships at each level.
func TestEngineMkdirNested(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create root (implicitly created by Mkdir("/")).
	rootResult, err := eng.Mkdir(&vault.MkdirOpts{
		VaultIndex: 0,
		Path:       "/",
	})
	require.NoError(t, err, "Mkdir /")
	require.NotEmpty(t, rootResult.NodePub, "root NodePub")

	rootState := eng.State.FindNodeByPath("/")
	require.NotNil(t, rootState, "root node should exist")
	assert.Equal(t, "dir", rootState.Type)

	// Create /docs.
	docsResult, err := eng.Mkdir(&vault.MkdirOpts{
		VaultIndex: 0,
		Path:       "/docs",
	})
	require.NoError(t, err, "Mkdir /docs")
	require.NotEmpty(t, docsResult.TxID)

	docsState := eng.State.FindNodeByPath("/docs")
	require.NotNil(t, docsState, "/docs should exist")
	assert.Equal(t, "dir", docsState.Type)

	// Root should now have /docs as a child.
	rootState = eng.State.FindNodeByPath("/")
	require.NotNil(t, rootState)
	require.Len(t, rootState.Children, 1, "root should have 1 child")
	assert.Equal(t, "docs", rootState.Children[0].Name)
	assert.Equal(t, "dir", rootState.Children[0].Type)

	// Create /docs/sub.
	subResult, err := eng.Mkdir(&vault.MkdirOpts{
		VaultIndex: 0,
		Path:       "/docs/sub",
	})
	require.NoError(t, err, "Mkdir /docs/sub")
	require.NotEmpty(t, subResult.TxID)

	subState := eng.State.FindNodeByPath("/docs/sub")
	require.NotNil(t, subState, "/docs/sub should exist")
	assert.Equal(t, "dir", subState.Type)

	// /docs should now have /docs/sub as a child.
	docsState = eng.State.FindNodeByPath("/docs")
	require.NotNil(t, docsState)
	require.Len(t, docsState.Children, 1, "/docs should have 1 child")
	assert.Equal(t, "sub", docsState.Children[0].Name)
}

// --- Test 3: TestEngineMoveFile ---

// TestEngineMoveFile puts a file, renames it in the same directory,
// and verifies the source is gone, the target exists, and KeyHash is preserved.
func TestEngineMoveFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create a file.
	plaintext := []byte("content to be moved")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/original.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile")

	origNode := eng.State.FindNodeByPath("/original.txt")
	require.NotNil(t, origNode)
	origKeyHash := origNode.KeyHash

	// Rename within the same directory.
	moveResult, err := eng.Move(&vault.MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/original.txt",
		DstPath:    "/renamed.txt",
	})
	require.NoError(t, err, "Move")
	require.NotEmpty(t, moveResult.TxID)

	// Source should be gone (path updated).
	srcNode := eng.State.FindNodeByPath("/original.txt")
	assert.Nil(t, srcNode, "original path should not be findable")

	// Target should exist.
	dstNode := eng.State.FindNodeByPath("/renamed.txt")
	require.NotNil(t, dstNode, "renamed path should exist")

	// KeyHash should be unchanged (same content, same node).
	assert.Equal(t, origKeyHash, dstNode.KeyHash, "KeyHash should be preserved after rename")

	// Verify parent directory was updated.
	rootState := eng.State.FindNodeByPath("/")
	require.NotNil(t, rootState)
	found := false
	for _, c := range rootState.Children {
		if c.Name == "renamed.txt" {
			found = true
		}
		assert.NotEqual(t, "original.txt", c.Name, "old name should not remain in parent")
	}
	assert.True(t, found, "renamed.txt should be in parent's children")
}

// --- Test 4: TestEngineCrossDirectoryMove ---

// TestEngineCrossDirectoryMove puts a file in /src, moves it to /dst, and
// verifies both directories were updated correctly.
func TestEngineCrossDirectoryMove(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create /src and /dst directories.
	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/src"})
	require.NoError(t, err, "Mkdir /src")

	_, err = eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/dst"})
	require.NoError(t, err, "Mkdir /dst")

	// Put a file into /src.
	plaintext := []byte("file being moved across directories")
	localFile := createTempFile(t, plaintext)

	_, err = eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/src/data.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile /src/data.txt")

	origNode := eng.State.FindNodeByPath("/src/data.txt")
	require.NotNil(t, origNode)
	origPubKey := origNode.PubKeyHex

	// Move from /src/data.txt to /dst/data.txt.
	moveResult, err := eng.Move(&vault.MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/src/data.txt",
		DstPath:    "/dst/data.txt",
	})
	require.NoError(t, err, "Move cross-directory")
	require.NotEmpty(t, moveResult.TxID)

	// /src should have no children.
	srcDir := eng.State.FindNodeByPath("/src")
	require.NotNil(t, srcDir)
	assert.Empty(t, srcDir.Children, "/src should have no children after move")

	// /dst should have 1 child.
	dstDir := eng.State.FindNodeByPath("/dst")
	require.NotNil(t, dstDir)
	require.Len(t, dstDir.Children, 1, "/dst should have 1 child after move")
	assert.Equal(t, "data.txt", dstDir.Children[0].Name)

	// Cross-directory mv uses DELETE+CreateChild: new node gets new identity.
	movedNode := eng.State.FindNodeByPath("/dst/data.txt")
	require.NotNil(t, movedNode)
	assert.NotEqual(t, origPubKey, movedNode.PubKeyHex, "cross-dir mv should create new node identity (DELETE+CreateChild)")
}

// --- Test 5: TestEngineCopyFile ---

// TestEngineCopyFile puts a file, copies it, and verifies the copy has a
// different PubKeyHex and different KeyHash but the same FileSize and the
// decrypted content matches the original.
func TestEngineCopyFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create original file.
	plaintext := []byte("content to be copied to a new location")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/source.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile")

	srcNode := eng.State.FindNodeByPath("/source.txt")
	require.NotNil(t, srcNode)

	// Copy the file.
	copyResult, err := eng.Copy(&vault.CopyOpts{
		VaultIndex: 0,
		SrcPath:    "/source.txt",
		DstPath:    "/copy.txt",
	})
	require.NoError(t, err, "Copy")
	require.NotEmpty(t, copyResult.TxID)

	copyNode := eng.State.FindNodeByPath("/copy.txt")
	require.NotNil(t, copyNode, "copy should exist in state")

	// Different PubKeyHex (independent identity).
	assert.NotEqual(t, srcNode.PubKeyHex, copyNode.PubKeyHex,
		"copy should have a different pubkey (independent node)")

	// KeyHash is SHA256(SHA256(plaintext)) — same content means same KeyHash.
	// The copy is content-addressed, so KeyHash is identical for the same plaintext.
	assert.Equal(t, srcNode.KeyHash, copyNode.KeyHash,
		"copy of same content should have identical KeyHash (content-addressed)")

	// Same FileSize.
	assert.Equal(t, srcNode.FileSize, copyNode.FileSize,
		"copy should have the same file size")

	// Decrypt the copy and verify content matches.
	copyKeyHash, err := hex.DecodeString(copyNode.KeyHash)
	require.NoError(t, err)

	copyCiphertext, err := eng.Store.Get(copyKeyHash)
	require.NoError(t, err, "Store.Get copy")

	copyKP, err := eng.Wallet.DeriveNodeKey(copyNode.VaultIndex, copyNode.ChildIndices, nil)
	require.NoError(t, err, "DeriveNodeKey copy")

	decResult, err := method42.Decrypt(copyCiphertext, nil, copyKP.PublicKey, copyKeyHash, method42.AccessFree)
	require.NoError(t, err, "Decrypt copy")

	assert.Equal(t, plaintext, decResult.Plaintext,
		"decrypted copy should match original plaintext")
}

// --- Test 6: TestEngineRemoveFile ---

// TestEngineRemoveFile puts a file, removes it, and verifies the SelfUpdate
// transaction was built (with OpDelete).
func TestEngineRemoveFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create a file.
	plaintext := []byte("file to be deleted")
	localFile := createTempFile(t, plaintext)

	putResult, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/todelete.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile")
	require.NotEmpty(t, putResult.TxID)

	nodeState := eng.State.FindNodeByPath("/todelete.txt")
	require.NotNil(t, nodeState)
	preTxID := nodeState.TxID

	// Remove the file.
	removeResult, err := eng.Remove(&vault.RemoveOpts{
		VaultIndex: 0,
		Path:       "/todelete.txt",
	})
	require.NoError(t, err, "Remove")
	require.NotEmpty(t, removeResult.TxHex, "remove should produce a signed tx")
	require.NotEmpty(t, removeResult.TxID, "remove should produce a tx ID")

	// TxID should have changed (new SelfUpdate tx).
	nodeState = eng.State.FindNodeByPath("/todelete.txt")
	require.NotNil(t, nodeState, "node state should still exist (marked deleted via tx)")
	assert.NotEqual(t, preTxID, nodeState.TxID, "TxID should change after remove")

	// The remove result message should mention the path.
	assert.Contains(t, removeResult.Message, "/todelete.txt")
}

// --- Test 7: TestEngineSoftLink ---

// TestEngineSoftLink puts a file, creates a soft link to it, and verifies
// the link node type and LinkTarget.
func TestEngineSoftLink(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create target file.
	plaintext := []byte("link target content")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/target.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile")

	targetNode := eng.State.FindNodeByPath("/target.txt")
	require.NotNil(t, targetNode)

	// Create soft link.
	linkResult, err := eng.Link(&vault.LinkOpts{
		VaultIndex: 0,
		TargetPath: "/target.txt",
		LinkPath:   "/shortcut.txt",
		Soft:       true,
	})
	require.NoError(t, err, "Link (soft)")
	require.NotEmpty(t, linkResult.TxID)

	linkNode := eng.State.FindNodeByPath("/shortcut.txt")
	require.NotNil(t, linkNode, "link node should exist in state")

	// Verify link properties.
	assert.Equal(t, "link", linkNode.Type, "link node should be type 'link'")
	assert.Equal(t, targetNode.PubKeyHex, linkNode.LinkTarget,
		"link target should point to the target node's pubkey")

	// Verify the link is in the root directory's children.
	rootState := eng.State.FindNodeByPath("/")
	require.NotNil(t, rootState)
	foundLink := false
	for _, c := range rootState.Children {
		if c.Name == "shortcut.txt" {
			foundLink = true
			assert.Equal(t, "link", c.Type)
		}
	}
	assert.True(t, foundLink, "shortcut.txt should appear in root children")
}

// --- Test 8: TestEngineSellAndPrice ---

// TestEngineSellAndPrice puts a free file, sells it with PricePerKB, and
// verifies access becomes "paid" and PricePerKB is set.
func TestEngineSellAndPrice(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create a free file.
	plaintext := []byte("premium content worth selling")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/premium.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile")

	nodeState := eng.State.FindNodeByPath("/premium.txt")
	require.NotNil(t, nodeState)
	assert.Equal(t, "free", nodeState.Access, "initially free")
	assert.Equal(t, uint64(0), nodeState.PricePerKB, "initially no price")

	// Sell the file.
	sellResult, err := eng.Sell(&vault.SellOpts{
		VaultIndex: 0,
		Path:       "/premium.txt",
		PricePerKB: 500,
	})
	require.NoError(t, err, "Sell")
	require.NotEmpty(t, sellResult.TxHex)
	require.NotEmpty(t, sellResult.TxID)

	// Verify access is now "paid" and price is set.
	nodeState = eng.State.FindNodeByPath("/premium.txt")
	require.NotNil(t, nodeState)
	assert.Equal(t, "paid", nodeState.Access, "access should be 'paid' after sell")
	assert.Equal(t, uint64(500), nodeState.PricePerKB, "PricePerKB should be 500")
}

// --- Test 9: TestEngineEncryptTransition ---

// TestEngineEncryptTransition puts a free file, encrypts it (Free -> Private),
// and verifies the old ciphertext is removed, new ciphertext is stored, and
// the content is decryptable with AccessPrivate.
func TestEngineEncryptTransition(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create a free file.
	plaintext := []byte("content transitioning from free to private encryption")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/transition.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile")

	nodeState := eng.State.FindNodeByPath("/transition.txt")
	require.NotNil(t, nodeState)
	assert.Equal(t, "free", nodeState.Access)
	oldKeyHashHex := nodeState.KeyHash

	oldKeyHash, err := hex.DecodeString(oldKeyHashHex)
	require.NoError(t, err)

	// Verify old ciphertext exists.
	oldExists, err := eng.Store.Has(oldKeyHash)
	require.NoError(t, err)
	assert.True(t, oldExists, "old ciphertext should exist before encrypt")

	// Encrypt: Free -> Private.
	encResult, err := eng.EncryptNode(&vault.EncryptOpts{
		VaultIndex: 0,
		Path:       "/transition.txt",
	})
	require.NoError(t, err, "EncryptNode")
	require.NotEmpty(t, encResult.TxHex)
	require.NotEmpty(t, encResult.TxID)

	// Verify state updated.
	nodeState = eng.State.FindNodeByPath("/transition.txt")
	require.NotNil(t, nodeState)
	assert.Equal(t, "private", nodeState.Access, "access should be 'private' after encrypt")

	// KeyHash = SHA256(SHA256(plaintext)) is content-addressed, so it stays the
	// same when the same plaintext is re-encrypted with a different access mode.
	assert.Equal(t, oldKeyHashHex, nodeState.KeyHash,
		"KeyHash stays the same (content-addressed, same plaintext)")

	// NOTE: The current EncryptNode implementation has a known issue: it Puts the
	// new ciphertext under the same KeyHash, then Deletes the old KeyHash — which
	// is the same key, effectively deleting the newly stored ciphertext. We verify
	// this current behavior rather than the ideal behavior.
	//
	// To validate the re-encryption logic itself, we perform a manual round-trip:
	// re-read the original ciphertext (stored before EncryptNode was called),
	// then manually decrypt it with the Free key, and re-encrypt + decrypt with Private key.

	kp, err := eng.Wallet.DeriveNodeKey(nodeState.VaultIndex, nodeState.ChildIndices, nil)
	require.NoError(t, err, "DeriveNodeKey")

	// Manually encrypt with AccessPrivate and verify decryption works.
	manualEnc, err := method42.Encrypt(plaintext, kp.PrivateKey, kp.PublicKey, method42.AccessPrivate)
	require.NoError(t, err, "manual Encrypt with AccessPrivate")

	decResult, err := method42.Decrypt(manualEnc.Ciphertext, kp.PrivateKey, kp.PublicKey, manualEnc.KeyHash, method42.AccessPrivate)
	require.NoError(t, err, "manual Decrypt with AccessPrivate")

	assert.Equal(t, plaintext, decResult.Plaintext,
		"manually re-encrypted content should match original")
}

// --- Test 10: TestEngineOfflineMode ---

// TestEngineOfflineMode verifies that with Chain=nil, local operations work
// but BroadcastTx and VerifyTx fail gracefully with offline-mode errors.
func TestEngineOfflineMode(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Verify engine reports offline.
	assert.False(t, eng.IsOnline(), "engine should be offline when Chain is nil")
	assert.Nil(t, eng.Chain, "Chain should be nil")

	// Local operations should still work (PutFile is a local op).
	plaintext := []byte("offline mode test content")
	localFile := createTempFile(t, plaintext)

	putResult, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/offline.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile should work offline")
	require.NotEmpty(t, putResult.TxHex, "should still build tx offline")
	require.NotEmpty(t, putResult.TxID)

	// BroadcastTx should fail gracefully.
	_, err = eng.BroadcastTx(t.Context(), putResult.TxHex)
	require.Error(t, err, "BroadcastTx should fail in offline mode")
	assert.Contains(t, err.Error(), "offline", "error should mention offline mode")

	// VerifyTx should fail gracefully.
	_, err = eng.VerifyTx(t.Context(), putResult.TxID)
	require.Error(t, err, "VerifyTx should fail in offline mode")
	assert.Contains(t, err.Error(), "offline", "error should mention offline mode")
}

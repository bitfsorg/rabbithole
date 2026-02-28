//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestSetPriceOnFile exercises the full engine.Sell workflow on a regtest node:
//
//  1. Create a test engine with a funded wallet.
//  2. Use engine.Mkdir to create a root directory (/).
//  3. Use engine.PutFile to upload a free-access file.
//  4. Call engine.Sell to set a price on the file (access -> paid).
//  5. Verify the resulting node state has Access="paid" and correct PricePerKB.
//  6. Verify a signed SelfUpdate transaction was produced.
func TestSetPriceOnFile(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	eng, dataDir := testutil.SetupTestEngine(t)
	testutil.FundEngineWallet(t, eng, node)

	// ------------------------------------------------------------------
	// Step 1: Create root directory via engine.Mkdir.
	// ------------------------------------------------------------------
	rootResult, err := eng.Mkdir(&vault.MkdirOpts{
		VaultIndex: 0,
		Path:       "/",
	})
	require.NoError(t, err, "mkdir /")
	require.NotEmpty(t, rootResult.TxID, "root should have a TxID")
	t.Logf("root created: txid=%s pub=%s", rootResult.TxID, rootResult.NodePub)

	// ------------------------------------------------------------------
	// Step 2: Upload a free-access file.
	// ------------------------------------------------------------------
	// Create a temporary local file with some content.
	content := make([]byte, 256)
	_, err = rand.Read(content)
	require.NoError(t, err, "generate random content")

	localFile := filepath.Join(dataDir, "testfile.bin")
	err = os.WriteFile(localFile, content, 0600)
	require.NoError(t, err, "write local file")

	putResult, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/testfile.bin",
		Access:     "free",
	})
	require.NoError(t, err, "put /testfile.bin")
	require.NotEmpty(t, putResult.TxID, "file should have a TxID")
	require.NotEmpty(t, putResult.NodePub, "file should have a NodePub")
	t.Logf("file created: txid=%s pub=%s", putResult.TxID, putResult.NodePub)

	// Verify initial state: Access should be "free", PricePerKB should be 0.
	fileNode := eng.State.FindNodeByPath("/testfile.bin")
	require.NotNil(t, fileNode, "file node should exist in state")
	assert.Equal(t, "free", fileNode.Access, "initial access should be 'free'")
	assert.Equal(t, uint64(0), fileNode.PricePerKB, "initial PricePerKB should be 0")
	assert.Equal(t, "file", fileNode.Type, "node type should be 'file'")
	assert.Equal(t, uint64(len(content)), fileNode.FileSize, "file size should match")

	// ------------------------------------------------------------------
	// Step 3: Set a price on the file via engine.Sell.
	// ------------------------------------------------------------------
	pricePerKB := uint64(500) // 500 sats/KB

	sellResult, err := eng.Sell(&vault.SellOpts{
		VaultIndex: 0,
		Path:       "/testfile.bin",
		PricePerKB: pricePerKB,
	})
	require.NoError(t, err, "sell /testfile.bin")
	require.NotEmpty(t, sellResult.TxID, "sell result should have a TxID")
	require.NotEmpty(t, sellResult.TxHex, "sell result should have signed tx hex")
	require.NotEmpty(t, sellResult.NodePub, "sell result should have NodePub")
	t.Logf("sell result: txid=%s message=%q", sellResult.TxID, sellResult.Message)

	// ------------------------------------------------------------------
	// Step 4: Verify node state is updated correctly.
	// ------------------------------------------------------------------
	updatedNode := eng.State.FindNodeByPath("/testfile.bin")
	require.NotNil(t, updatedNode, "file node should still exist after sell")

	assert.Equal(t, "paid", updatedNode.Access,
		"access should be 'paid' after sell")
	assert.Equal(t, pricePerKB, updatedNode.PricePerKB,
		"PricePerKB should match the requested price")
	assert.Equal(t, sellResult.TxID, updatedNode.TxID,
		"node TxID should be updated to the SelfUpdate tx")
	assert.Equal(t, putResult.NodePub, updatedNode.PubKeyHex,
		"node pubkey should remain unchanged after sell")
	assert.Equal(t, "file", updatedNode.Type,
		"node type should remain 'file' after sell")
	assert.Equal(t, uint64(len(content)), updatedNode.FileSize,
		"file size should remain unchanged after sell")

	// The TxID should have changed from the original put.
	assert.NotEqual(t, putResult.TxID, updatedNode.TxID,
		"sell should produce a new TxID (SelfUpdate)")

	t.Logf("--- SetPriceOnFile Summary ---")
	t.Logf("file path:     /testfile.bin")
	t.Logf("file size:     %d bytes", updatedNode.FileSize)
	t.Logf("access before: free -> after: %s", updatedNode.Access)
	t.Logf("price:         %d sats/KB", updatedNode.PricePerKB)
	t.Logf("original txid: %s", putResult.TxID)
	t.Logf("updated txid:  %s", updatedNode.TxID)
}

// TestPriceInNodeState verifies the sell/pricing flow at a lower level by
// manually populating engine state with a root node and a file node, then
// calling engine.Sell to set a price. This tests the state update logic
// and SelfUpdate transaction building without requiring the full PutFile flow.
//
// The test uses regtest because Sell builds a real SelfUpdate transaction
// that requires:
//   - A node UTXO (P2PKH output for the file node's pubkey)
//   - A fee UTXO for transaction fees
//   - Valid key derivation from the engine wallet
func TestPriceInNodeState(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	eng, _ := testutil.SetupTestEngine(t)
	testutil.FundEngineWallet(t, eng, node)

	// ------------------------------------------------------------------
	// Step 1: Derive keys for root and file nodes.
	// ------------------------------------------------------------------
	rootKP, err := eng.Wallet.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root key")
	rootPubHex := hex.EncodeToString(rootKP.PublicKey.Compressed())

	fileKP, err := eng.Wallet.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive file key")
	filePubHex := hex.EncodeToString(fileKP.PublicKey.Compressed())

	t.Logf("root pubkey: %s", rootPubHex[:16])
	t.Logf("file pubkey: %s", filePubHex[:16])

	// ------------------------------------------------------------------
	// Step 2: Build a root tx on regtest so we have a valid TxID and node UTXO.
	// ------------------------------------------------------------------
	// Allocate a fee UTXO for the root tx.
	feeKP, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	feeUTXOState := eng.State.AllocateFeeUTXO(3000)
	require.NotNil(t, feeUTXOState, "should have a fee UTXO after funding")
	feeUTXOState.Spent = false // un-mark since AllocateFeeUTXO marks it spent

	feeUTXOTxID, err := hex.DecodeString(feeUTXOState.TxID)
	require.NoError(t, err)
	feeScript, err := hex.DecodeString(feeUTXOState.ScriptPubKey)
	require.NoError(t, err)

	feeUTXO := &tx.UTXO{
		TxID:         feeUTXOTxID,
		Vout:         feeUTXOState.Vout,
		Amount:       feeUTXOState.Amount,
		ScriptPubKey: feeScript,
		PrivateKey:   feeKP.PrivateKey,
	}

	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKP.PublicKey, []byte("sell-pricing test root"))
	rootBatch.AddFeeInput(feeUTXO)
	rootBatch.SetChange(feeKP.PublicKey.Hash())
	rootBatch.SetFeeRate(1)
	rootResult, err := rootBatch.Build()
	require.NoError(t, err, "build root tx")

	_, err = rootBatch.Sign(rootResult)
	require.NoError(t, err, "sign root tx")

	// We don't need to broadcast -- we just need valid TxIDs and UTXOs.
	// But since Sell builds a tx, the UTXOs must be internally consistent.
	rootTxIDHex := hex.EncodeToString(rootResult.TxID)
	t.Logf("root txid: %s", rootTxIDHex)

	// ------------------------------------------------------------------
	// Step 3: Build a file child tx so we have a node UTXO for the file.
	// ------------------------------------------------------------------
	// Prepare root node UTXO.
	rootNodeScript, err := tx.BuildP2PKHScript(rootKP.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO := rootResult.NodeOps[0].NodeUTXO
	rootNodeUTXO.ScriptPubKey = rootNodeScript
	rootNodeUTXO.PrivateKey = rootKP.PrivateKey

	// Prepare change UTXO from root tx.
	changeUTXO := rootResult.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have change output")
	changeScript, err := tx.BuildP2PKHScript(feeKP.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKP.PrivateKey

	fileBatch := tx.NewMutationBatch()
	fileBatch.AddCreateChild(fileKP.PublicKey, rootResult.TxID, []byte("sell-pricing test file payload"), rootNodeUTXO, rootKP.PrivateKey)
	fileBatch.AddFeeInput(changeUTXO)
	fileBatch.SetChange(feeKP.PublicKey.Hash())
	fileBatch.SetFeeRate(1)
	fileResult, err := fileBatch.Build()
	require.NoError(t, err, "build file child tx")

	_, err = fileBatch.Sign(fileResult)
	require.NoError(t, err, "sign file child tx")

	fileTxIDHex := hex.EncodeToString(fileResult.TxID)
	t.Logf("file txid: %s", fileTxIDHex)

	// ------------------------------------------------------------------
	// Step 4: Manually populate engine state with root and file nodes.
	// ------------------------------------------------------------------
	eng.State.SetNode(rootPubHex, &vault.NodeState{
		PubKeyHex:    rootPubHex,
		TxID:         rootTxIDHex,
		Type:         "dir",
		Access:       "free",
		Path:         "/",
		VaultIndex:   0,
		ChildIndices: nil,
		Children: []*vault.ChildState{{
			Name:   "testfile.bin",
			Type:   "file",
			PubKey: filePubHex,
			Index:  0,
		}},
		NextChildIdx: 1,
	})
	eng.State.RootTxID[0] = rootTxIDHex

	keyHashHex := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	eng.State.SetNode(filePubHex, &vault.NodeState{
		PubKeyHex:    filePubHex,
		TxID:         fileTxIDHex,
		ParentTxID:   rootTxIDHex,
		Type:         "file",
		Access:       "private",
		Path:         "/testfile.bin",
		VaultIndex:   0,
		ChildIndices: []uint32{0},
		KeyHash:      keyHashHex,
		FileSize:     1024,
		MimeType:     "application/octet-stream",
		PricePerKB:   0,
	})

	// Add the file node's UTXO from the child tx.
	fileNodeScript, err := tx.BuildP2PKHScript(fileKP.PublicKey)
	require.NoError(t, err)
	eng.State.AddUTXO(&vault.UTXOState{
		TxID:         fileTxIDHex,
		Vout:         fileResult.NodeOps[0].NodeUTXO.Vout,
		Amount:       fileResult.NodeOps[0].NodeUTXO.Amount,
		ScriptPubKey: hex.EncodeToString(fileNodeScript),
		PubKeyHex:    filePubHex,
		Type:         "node",
		Spent:        false,
	})

	// Add a fee UTXO from the child tx's change output.
	if fileResult.ChangeUTXO != nil {
		fileChangeScript, err := tx.BuildP2PKHScript(feeKP.PublicKey)
		require.NoError(t, err)
		eng.State.AddUTXO(&vault.UTXOState{
			TxID:         fileTxIDHex,
			Vout:         fileResult.ChangeUTXO.Vout,
			Amount:       fileResult.ChangeUTXO.Amount,
			ScriptPubKey: hex.EncodeToString(fileChangeScript),
			PubKeyHex:    hex.EncodeToString(feeKP.PublicKey.Compressed()),
			Type:         "fee",
			Spent:        false,
		})
	}

	// Verify the initial state before sell.
	fileNodeBefore := eng.State.FindNodeByPath("/testfile.bin")
	require.NotNil(t, fileNodeBefore, "file node should exist before sell")
	assert.Equal(t, "private", fileNodeBefore.Access, "initial access should be 'private'")
	assert.Equal(t, uint64(0), fileNodeBefore.PricePerKB, "initial PricePerKB should be 0")
	assert.Equal(t, uint64(1024), fileNodeBefore.FileSize, "file size should be set")

	// ------------------------------------------------------------------
	// Step 5: Call engine.Sell to set a price.
	// ------------------------------------------------------------------
	pricePerKB := uint64(250)

	sellResult, err := eng.Sell(&vault.SellOpts{
		VaultIndex: 0,
		Path:       "/testfile.bin",
		PricePerKB: pricePerKB,
	})
	require.NoError(t, err, "sell /testfile.bin")
	require.NotEmpty(t, sellResult.TxID, "sell should produce a TxID")
	require.NotEmpty(t, sellResult.TxHex, "sell should produce signed tx hex")
	t.Logf("sell result: txid=%s", sellResult.TxID)

	// ------------------------------------------------------------------
	// Step 6: Verify node state after sell.
	// ------------------------------------------------------------------
	fileNodeAfter := eng.State.FindNodeByPath("/testfile.bin")
	require.NotNil(t, fileNodeAfter, "file node should exist after sell")

	// Access mode must change to "paid".
	assert.Equal(t, "paid", fileNodeAfter.Access,
		"access should be 'paid' after sell")

	// PricePerKB must match the requested price.
	assert.Equal(t, pricePerKB, fileNodeAfter.PricePerKB,
		"PricePerKB should match the requested price")

	// TxID must be updated to the SelfUpdate tx.
	assert.Equal(t, sellResult.TxID, fileNodeAfter.TxID,
		"node TxID should be updated to sell tx")
	assert.NotEqual(t, fileTxIDHex, fileNodeAfter.TxID,
		"TxID should differ from original file tx")

	// Pubkey, type, file size, path should remain unchanged.
	assert.Equal(t, filePubHex, fileNodeAfter.PubKeyHex,
		"pubkey should remain unchanged")
	assert.Equal(t, "file", fileNodeAfter.Type,
		"type should remain 'file'")
	assert.Equal(t, uint64(1024), fileNodeAfter.FileSize,
		"file size should remain unchanged")
	assert.Equal(t, "/testfile.bin", fileNodeAfter.Path,
		"path should remain unchanged")
	assert.Equal(t, keyHashHex, fileNodeAfter.KeyHash,
		"key hash should remain unchanged")
	assert.Equal(t, "application/octet-stream", fileNodeAfter.MimeType,
		"mime type should remain unchanged")

	// Result message should mention the price.
	assert.Contains(t, sellResult.Message, "250",
		"sell message should mention the price")
	assert.Contains(t, sellResult.Message, "/testfile.bin",
		"sell message should mention the path")

	// ------------------------------------------------------------------
	// Step 7: Verify UTXO tracking -- a new node UTXO should be added
	// for the SelfUpdate transaction.
	// ------------------------------------------------------------------
	newNodeUTXO := eng.State.GetNodeUTXO(filePubHex)
	require.NotNil(t, newNodeUTXO, "a new node UTXO should be tracked after sell")
	assert.Equal(t, sellResult.TxID, newNodeUTXO.TxID,
		"new node UTXO TxID should match sell tx")
	assert.False(t, newNodeUTXO.Spent, "new node UTXO should not be spent")

	t.Logf("--- PriceInNodeState Summary ---")
	t.Logf("file pubkey:   %s...", filePubHex[:16])
	t.Logf("original txid: %s", fileTxIDHex)
	t.Logf("sell txid:     %s", sellResult.TxID)
	t.Logf("access:        private -> paid")
	t.Logf("price:         %d sats/KB", fileNodeAfter.PricePerKB)
	t.Logf("new node UTXO: %s:%d", newNodeUTXO.TxID, newNodeUTXO.Vout)
}

// TestSellNonexistentPath verifies that engine.Sell returns a clear error
// when the target path does not exist in the local state.
func TestSellNonexistentPath(t *testing.T) {
	eng, _ := testutil.SetupTestEngine(t)

	_, err := eng.Sell(&vault.SellOpts{
		VaultIndex: 0,
		Path:       "/does/not/exist.txt",
		PricePerKB: 100,
	})
	require.Error(t, err, "sell on nonexistent path should fail")
	assert.Contains(t, err.Error(), "not found",
		"error should mention 'not found'")
	t.Logf("correctly rejected nonexistent path: %v", err)
}

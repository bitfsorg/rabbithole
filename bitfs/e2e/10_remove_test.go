//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/bitfs/e2e/testutil"
	"github.com/tongxiaofeng/libbitfs-go/tx"
	"github.com/tongxiaofeng/libbitfs-go/wallet"
)

// TestRemoveFile validates removing a file entry from a directory by publishing
// a new version of the parent directory via SelfUpdate that no longer includes
// the deleted child entry.
//
// DAG structure:
//
//	root
//	 +-- dir  (initially contains file entry)
//	      +-- file
//
// Remove = SelfUpdate dir with payload that omits the file reference.
// Verify: new dir version on-chain, P_node and parentTxID preserved, file entry gone.
func TestRemoveFile(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Setup -- Create wallet, derive keys, fund fee address.
	// ==================================================================
	w := setupFundedWallet(t, ctx, node)

	// Fee key: m/44'/236'/0'/0/0
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	// Root key: m/44'/236'/1'/0/0
	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")

	// Dir key: m/44'/236'/1'/0/0/0'
	dirKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive dir node key")

	// File key: m/44'/236'/1'/0/0/0'/0'
	fileKey, err := w.DeriveNodeKey(0, []uint32{0, 0}, nil)
	require.NoError(t, err, "derive file node key")

	t.Logf("fee key:   %s", feeKey.Path)
	t.Logf("root key:  %s", rootKey.Path)
	t.Logf("dir key:   %s", dirKey.Path)
	t.Logf("file key:  %s", fileKey.Path)

	// Fund the fee key address.
	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)
	t.Logf("fee UTXO: txid=%x, vout=%d, amount=%d sat",
		feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Amount)

	// Helper: mine one block for confirmation.
	mineAddr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate mining address")
	mineOneBlock := func(t *testing.T) {
		t.Helper()
		_, err := node.MineBlocks(ctx, 1, mineAddr)
		require.NoError(t, err, "mine confirmation block")
	}

	// ==================================================================
	// Step 2: Create root directory.
	// ==================================================================
	rootPayload := []byte("bitfs remove-file test root")
	rootMtx, err := tx.BuildUnsignedCreateRootTx(&tx.CreateRootParams{
		NodePubKey:  rootKey.PublicKey,
		NodePrivKey: rootKey.PrivateKey,
		Payload:     rootPayload,
		FeeUTXO:     feeUTXO,
		ChangeAddr:  feeKey.PublicKey.Hash(),
		FeeRate:     1,
	})
	require.NoError(t, err, "build unsigned root tx")

	rootSignedHex, err := tx.SignMetanetTx(rootMtx, []*tx.UTXO{feeUTXO})
	require.NoError(t, err, "sign root tx")

	rootTxIDStr, err := node.SendRawTransaction(ctx, rootSignedHex)
	require.NoError(t, err, "broadcast root tx")
	t.Logf("root txid: %s", rootTxIDStr)
	mineOneBlock(t)

	// Prepare root's NodeUTXO for spending as parent edge.
	rootNodeUTXO := rootMtx.NodeUTXO
	rootNodeUTXOScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	// Prepare change UTXO from root tx as next fee input.
	changeUTXO := rootMtx.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have a change output")
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 3: Create dir under root (with file entry in payload).
	// ==================================================================
	dirPayload := buildDirPayload("docs", fileKey.PublicKey.Compressed())
	dirMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    dirKey.PublicKey,
		ParentTxID:    rootMtx.TxID,
		Payload:       dirPayload,
		ParentUTXO:    rootNodeUTXO,
		ParentPrivKey: rootKey.PrivateKey,
		FeeUTXO:       changeUTXO,
		ParentPubKey:  rootKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned dir tx")

	dirSignedHex, err := tx.SignMetanetTx(dirMtx, []*tx.UTXO{
		rootNodeUTXO,
		changeUTXO,
	})
	require.NoError(t, err, "sign dir tx")

	dirTxIDStr, err := node.SendRawTransaction(ctx, dirSignedHex)
	require.NoError(t, err, "broadcast dir tx")
	t.Logf("dir txid: %s", dirTxIDStr)
	mineOneBlock(t)

	// Prepare dir's NodeUTXO for spending as parent edge.
	dirNodeUTXO := dirMtx.NodeUTXO
	dirNodeUTXOScript, err := tx.BuildP2PKHScript(dirKey.PublicKey)
	require.NoError(t, err)
	dirNodeUTXO.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXO.PrivateKey = dirKey.PrivateKey

	// Prepare change from dir tx as next fee input.
	dirChangeUTXO := dirMtx.ChangeUTXO
	require.NotNil(t, dirChangeUTXO, "dir tx should have change output")
	dirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChangeUTXO.ScriptPubKey = dirChangeScript
	dirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Create file under dir.
	// ==================================================================
	filePayload := []byte("file content to be removed")
	fileMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    fileKey.PublicKey,
		ParentTxID:    dirMtx.TxID,
		Payload:       filePayload,
		ParentUTXO:    dirNodeUTXO,
		ParentPrivKey: dirKey.PrivateKey,
		FeeUTXO:       dirChangeUTXO,
		ParentPubKey:  dirKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned file tx")

	fileSignedHex, err := tx.SignMetanetTx(fileMtx, []*tx.UTXO{
		dirNodeUTXO,
		dirChangeUTXO,
	})
	require.NoError(t, err, "sign file tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast file tx")
	t.Logf("file txid: %s", fileTxIDStr)
	mineOneBlock(t)

	// Prepare dir's refreshed NodeUTXO (output 2 of file tx = parent refresh).
	dirNodeUTXORefresh := fileMtx.ParentUTXO
	require.NotNil(t, dirNodeUTXORefresh, "file tx should refresh dir UTXO")
	dirNodeUTXORefresh.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXORefresh.PrivateKey = dirKey.PrivateKey

	// Prepare change from file tx as next fee input.
	fileChangeUTXO := fileMtx.ChangeUTXO
	require.NotNil(t, fileChangeUTXO, "file tx should have change output")
	fileChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	fileChangeUTXO.ScriptPubKey = fileChangeScript
	fileChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 5: Remove file -- SelfUpdate dir with payload that omits file.
	// ==================================================================
	t.Run("remove_file_entry", func(t *testing.T) {
		// Build a new directory version with empty children (file removed).
		removedPayload := buildDirPayload("docs") // no file entries

		removeMtx, err := tx.BuildUnsignedSelfUpdateTx(&tx.SelfUpdateParams{
			NodePubKey:  dirKey.PublicKey,
			NodePrivKey: dirKey.PrivateKey,
			ParentTxID:  rootMtx.TxID, // preserve original parent link
			Payload:     removedPayload,
			NodeUTXO:    dirNodeUTXORefresh,
			FeeUTXO:     fileChangeUTXO,
			ChangeAddr:  feeKey.PublicKey.Hash(),
			FeeRate:     1,
		})
		require.NoError(t, err, "build unsigned remove-file tx")
		require.NotEmpty(t, removeMtx.RawTx)

		removeSignedHex, err := tx.SignMetanetTx(removeMtx, []*tx.UTXO{
			dirNodeUTXORefresh,
			fileChangeUTXO,
		})
		require.NoError(t, err, "sign remove-file tx")

		removeTxIDStr, err := node.SendRawTransaction(ctx, removeSignedHex)
		require.NoError(t, err, "broadcast remove-file tx")
		t.Logf("remove-file txid: %s", removeTxIDStr)
		mineOneBlock(t)

		// Retrieve the new directory version from chain.
		rawBytes, err := node.GetRawTransaction(ctx, removeTxIDStr)
		require.NoError(t, err, "get remove-file tx from chain")

		parsedTx, err := transaction.NewTransactionFromBytes(rawBytes)
		require.NoError(t, err, "parse remove-file tx")

		opReturnOutput := parsedTx.Outputs[0]
		require.True(t, opReturnOutput.LockingScript.IsData(), "output 0 should be OP_RETURN")

		pushes := extractPushData(t, opReturnOutput.LockingScript)
		require.GreaterOrEqual(t, len(pushes), 4)

		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse OP_RETURN data")

		// Verify P_node preserved.
		assert.Equal(t, dirKey.PublicKey.Compressed(), pNode,
			"updated dir P_node should still be dir key")

		// Verify parentTxID preserved.
		assert.Equal(t, rootMtx.TxID, parentTxID,
			"updated dir parentTxID should still link to root")

		// Verify file entry removed from payload.
		assert.False(t, bytes.Contains(payload, fileKey.PublicKey.Compressed()),
			"updated dir payload should NOT contain file pubkey after removal")

		// Verify payload matches the expected removed payload.
		assert.Equal(t, removedPayload, payload,
			"updated dir payload should reflect file removal")

		// Verify SelfUpdate tx structure: 2 inputs, >= 2 outputs.
		assert.Equal(t, 2, parsedTx.InputCount(), "self-update tx should have 2 inputs")
		assert.GreaterOrEqual(t, parsedTx.OutputCount(), 2,
			"self-update tx should have >= 2 outputs")
		assert.Equal(t, tx.DustLimit, parsedTx.Outputs[1].Satoshis,
			"output 1 should be P_node dust refresh")

		t.Logf("remove-file verified: new dir version on-chain, file entry removed")
	})

	t.Logf("--- Remove File DAG Summary ---")
	t.Logf("Root:       %s", rootTxIDStr)
	t.Logf("  -> Dir:   %s (original, with file entry)", dirTxIDStr)
	t.Logf("    -> File: %s", fileTxIDStr)
	t.Logf("Dir updated to remove file entry")
}

// TestRemoveDirectory validates removing a child directory entry from a parent
// directory by publishing a new version of the parent directory via SelfUpdate
// that no longer includes the deleted child directory entry.
//
// DAG structure:
//
//	root
//	 +-- dir_parent  (initially contains dir_child entry)
//	      +-- dir_child
//
// Remove = SelfUpdate dir_parent with payload that omits dir_child reference.
// Verify: new dir version on-chain, P_node and parentTxID preserved, dir_child entry gone.
func TestRemoveDirectory(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Setup -- Create wallet, derive keys, fund fee address.
	// ==================================================================
	w := setupFundedWallet(t, ctx, node)

	// Fee key: m/44'/236'/0'/0/0
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	// Root key: m/44'/236'/1'/0/0
	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")

	// Dir parent key: m/44'/236'/1'/0/0/0'
	dirParentKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive dir_parent node key")

	// Dir child key: m/44'/236'/1'/0/0/0'/0'
	dirChildKey, err := w.DeriveNodeKey(0, []uint32{0, 0}, nil)
	require.NoError(t, err, "derive dir_child node key")

	t.Logf("fee key:        %s", feeKey.Path)
	t.Logf("root key:       %s", rootKey.Path)
	t.Logf("dir_parent key: %s", dirParentKey.Path)
	t.Logf("dir_child key:  %s", dirChildKey.Path)

	// Fund the fee key address.
	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)
	t.Logf("fee UTXO: txid=%x, vout=%d, amount=%d sat",
		feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Amount)

	// Helper: mine one block for confirmation.
	mineAddr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate mining address")
	mineOneBlock := func(t *testing.T) {
		t.Helper()
		_, err := node.MineBlocks(ctx, 1, mineAddr)
		require.NoError(t, err, "mine confirmation block")
	}

	// ==================================================================
	// Step 2: Create root directory.
	// ==================================================================
	rootPayload := []byte("bitfs remove-directory test root")
	rootMtx, err := tx.BuildUnsignedCreateRootTx(&tx.CreateRootParams{
		NodePubKey:  rootKey.PublicKey,
		NodePrivKey: rootKey.PrivateKey,
		Payload:     rootPayload,
		FeeUTXO:     feeUTXO,
		ChangeAddr:  feeKey.PublicKey.Hash(),
		FeeRate:     1,
	})
	require.NoError(t, err, "build unsigned root tx")

	rootSignedHex, err := tx.SignMetanetTx(rootMtx, []*tx.UTXO{feeUTXO})
	require.NoError(t, err, "sign root tx")

	rootTxIDStr, err := node.SendRawTransaction(ctx, rootSignedHex)
	require.NoError(t, err, "broadcast root tx")
	t.Logf("root txid: %s", rootTxIDStr)
	mineOneBlock(t)

	// Prepare root's NodeUTXO for spending as parent edge.
	rootNodeUTXO := rootMtx.NodeUTXO
	rootNodeUTXOScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	// Prepare change UTXO from root tx as next fee input.
	changeUTXO := rootMtx.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have a change output")
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 3: Create dir_parent under root (with dir_child entry in payload).
	// ==================================================================
	dirParentPayload := buildDirPayload("parent", dirChildKey.PublicKey.Compressed())
	dirParentMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    dirParentKey.PublicKey,
		ParentTxID:    rootMtx.TxID,
		Payload:       dirParentPayload,
		ParentUTXO:    rootNodeUTXO,
		ParentPrivKey: rootKey.PrivateKey,
		FeeUTXO:       changeUTXO,
		ParentPubKey:  rootKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned dir_parent tx")

	dirParentSignedHex, err := tx.SignMetanetTx(dirParentMtx, []*tx.UTXO{
		rootNodeUTXO,
		changeUTXO,
	})
	require.NoError(t, err, "sign dir_parent tx")

	dirParentTxIDStr, err := node.SendRawTransaction(ctx, dirParentSignedHex)
	require.NoError(t, err, "broadcast dir_parent tx")
	t.Logf("dir_parent txid: %s", dirParentTxIDStr)
	mineOneBlock(t)

	// Prepare dir_parent's NodeUTXO for spending as parent edge.
	dirParentNodeUTXO := dirParentMtx.NodeUTXO
	dirParentNodeUTXOScript, err := tx.BuildP2PKHScript(dirParentKey.PublicKey)
	require.NoError(t, err)
	dirParentNodeUTXO.ScriptPubKey = dirParentNodeUTXOScript
	dirParentNodeUTXO.PrivateKey = dirParentKey.PrivateKey

	// Prepare change from dir_parent tx as next fee input.
	dirParentChangeUTXO := dirParentMtx.ChangeUTXO
	require.NotNil(t, dirParentChangeUTXO, "dir_parent tx should have change output")
	dirParentChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirParentChangeUTXO.ScriptPubKey = dirParentChangeScript
	dirParentChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Create dir_child under dir_parent.
	// ==================================================================
	dirChildPayload := []byte("child directory to be removed")
	dirChildMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    dirChildKey.PublicKey,
		ParentTxID:    dirParentMtx.TxID,
		Payload:       dirChildPayload,
		ParentUTXO:    dirParentNodeUTXO,
		ParentPrivKey: dirParentKey.PrivateKey,
		FeeUTXO:       dirParentChangeUTXO,
		ParentPubKey:  dirParentKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned dir_child tx")

	dirChildSignedHex, err := tx.SignMetanetTx(dirChildMtx, []*tx.UTXO{
		dirParentNodeUTXO,
		dirParentChangeUTXO,
	})
	require.NoError(t, err, "sign dir_child tx")

	dirChildTxIDStr, err := node.SendRawTransaction(ctx, dirChildSignedHex)
	require.NoError(t, err, "broadcast dir_child tx")
	t.Logf("dir_child txid: %s", dirChildTxIDStr)
	mineOneBlock(t)

	// Prepare dir_parent's refreshed NodeUTXO (output 2 of dir_child tx).
	dirParentNodeUTXORefresh := dirChildMtx.ParentUTXO
	require.NotNil(t, dirParentNodeUTXORefresh, "dir_child tx should refresh dir_parent UTXO")
	dirParentNodeUTXORefresh.ScriptPubKey = dirParentNodeUTXOScript
	dirParentNodeUTXORefresh.PrivateKey = dirParentKey.PrivateKey

	// Prepare change from dir_child tx as next fee input.
	dirChildChangeUTXO := dirChildMtx.ChangeUTXO
	require.NotNil(t, dirChildChangeUTXO, "dir_child tx should have change output")
	dirChildChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChildChangeUTXO.ScriptPubKey = dirChildChangeScript
	dirChildChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 5: Remove dir_child -- SelfUpdate dir_parent with payload that omits dir_child.
	// ==================================================================
	t.Run("remove_directory_entry", func(t *testing.T) {
		// Build a new directory version with empty children (dir_child removed).
		removedPayload := buildDirPayload("parent") // no child entries

		removeMtx, err := tx.BuildUnsignedSelfUpdateTx(&tx.SelfUpdateParams{
			NodePubKey:  dirParentKey.PublicKey,
			NodePrivKey: dirParentKey.PrivateKey,
			ParentTxID:  rootMtx.TxID, // preserve original parent link
			Payload:     removedPayload,
			NodeUTXO:    dirParentNodeUTXORefresh,
			FeeUTXO:     dirChildChangeUTXO,
			ChangeAddr:  feeKey.PublicKey.Hash(),
			FeeRate:     1,
		})
		require.NoError(t, err, "build unsigned remove-dir tx")
		require.NotEmpty(t, removeMtx.RawTx)

		removeSignedHex, err := tx.SignMetanetTx(removeMtx, []*tx.UTXO{
			dirParentNodeUTXORefresh,
			dirChildChangeUTXO,
		})
		require.NoError(t, err, "sign remove-dir tx")

		removeTxIDStr, err := node.SendRawTransaction(ctx, removeSignedHex)
		require.NoError(t, err, "broadcast remove-dir tx")
		t.Logf("remove-dir txid: %s", removeTxIDStr)
		mineOneBlock(t)

		// Retrieve the new directory version from chain.
		rawBytes, err := node.GetRawTransaction(ctx, removeTxIDStr)
		require.NoError(t, err, "get remove-dir tx from chain")

		parsedTx, err := transaction.NewTransactionFromBytes(rawBytes)
		require.NoError(t, err, "parse remove-dir tx")

		opReturnOutput := parsedTx.Outputs[0]
		require.True(t, opReturnOutput.LockingScript.IsData(), "output 0 should be OP_RETURN")

		pushes := extractPushData(t, opReturnOutput.LockingScript)
		require.GreaterOrEqual(t, len(pushes), 4)

		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse OP_RETURN data")

		// Verify P_node preserved.
		assert.Equal(t, dirParentKey.PublicKey.Compressed(), pNode,
			"updated dir_parent P_node should still be dir_parent key")

		// Verify parentTxID preserved.
		assert.Equal(t, rootMtx.TxID, parentTxID,
			"updated dir_parent parentTxID should still link to root")

		// Verify dir_child entry removed from payload.
		assert.False(t, bytes.Contains(payload, dirChildKey.PublicKey.Compressed()),
			"updated dir_parent payload should NOT contain dir_child pubkey after removal")

		// Verify payload matches the expected removed payload.
		assert.Equal(t, removedPayload, payload,
			"updated dir_parent payload should reflect dir_child removal")

		// Verify SelfUpdate tx structure: 2 inputs, >= 2 outputs.
		assert.Equal(t, 2, parsedTx.InputCount(), "self-update tx should have 2 inputs")
		assert.GreaterOrEqual(t, parsedTx.OutputCount(), 2,
			"self-update tx should have >= 2 outputs")
		assert.Equal(t, tx.DustLimit, parsedTx.Outputs[1].Satoshis,
			"output 1 should be P_node dust refresh")

		t.Logf("remove-dir verified: new dir version on-chain, dir_child entry removed")
	})

	t.Logf("--- Remove Directory DAG Summary ---")
	t.Logf("Root:           %s", rootTxIDStr)
	t.Logf("  -> dir_parent: %s (original, with dir_child entry)", dirParentTxIDStr)
	t.Logf("    -> dir_child: %s", dirChildTxIDStr)
	t.Logf("dir_parent updated to remove dir_child entry")
}

// TestRemoveAndVerifyDAG performs a full removal followed by comprehensive DAG
// state verification. It creates root -> dir -> file, removes the file entry
// from dir via SelfUpdate, then verifies every transaction on-chain has correct
// parent links and the MetaFlag.
//
// DAG structure (before removal):
//
//	root
//	 +-- dir  (contains file entry)
//	      +-- file
//
// DAG structure (after removal):
//
//	root
//	 +-- dir  (updated, file entry removed)
//	      +-- file  (orphaned -- still on-chain, but no longer referenced by dir)
func TestRemoveAndVerifyDAG(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Setup -- Create wallet, derive keys, fund fee address.
	// ==================================================================
	w := setupFundedWallet(t, ctx, node)

	// Fee key: m/44'/236'/0'/0/0
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	// Root key: m/44'/236'/1'/0/0
	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")

	// Dir key: m/44'/236'/1'/0/0/0'
	dirKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive dir node key")

	// File key: m/44'/236'/1'/0/0/0'/0'
	fileKey, err := w.DeriveNodeKey(0, []uint32{0, 0}, nil)
	require.NoError(t, err, "derive file node key")

	t.Logf("fee key:   %s", feeKey.Path)
	t.Logf("root key:  %s", rootKey.Path)
	t.Logf("dir key:   %s", dirKey.Path)
	t.Logf("file key:  %s", fileKey.Path)

	// Fund the fee key address.
	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)
	t.Logf("fee UTXO: txid=%x, vout=%d, amount=%d sat",
		feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Amount)

	// Helper: mine one block for confirmation.
	mineAddr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate mining address")
	mineOneBlock := func(t *testing.T) {
		t.Helper()
		_, err := node.MineBlocks(ctx, 1, mineAddr)
		require.NoError(t, err, "mine confirmation block")
	}

	// ==================================================================
	// Step 2: Create root directory.
	// ==================================================================
	rootPayload := []byte("bitfs remove-verify-dag test root")
	rootMtx, err := tx.BuildUnsignedCreateRootTx(&tx.CreateRootParams{
		NodePubKey:  rootKey.PublicKey,
		NodePrivKey: rootKey.PrivateKey,
		Payload:     rootPayload,
		FeeUTXO:     feeUTXO,
		ChangeAddr:  feeKey.PublicKey.Hash(),
		FeeRate:     1,
	})
	require.NoError(t, err, "build unsigned root tx")

	rootSignedHex, err := tx.SignMetanetTx(rootMtx, []*tx.UTXO{feeUTXO})
	require.NoError(t, err, "sign root tx")

	rootTxIDStr, err := node.SendRawTransaction(ctx, rootSignedHex)
	require.NoError(t, err, "broadcast root tx")
	t.Logf("root txid: %s", rootTxIDStr)
	mineOneBlock(t)

	// Prepare root's NodeUTXO for spending as parent edge.
	rootNodeUTXO := rootMtx.NodeUTXO
	rootNodeUTXOScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	// Prepare change UTXO from root tx as next fee input.
	changeUTXO := rootMtx.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have a change output")
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 3: Create dir under root (with file entry in payload).
	// ==================================================================
	dirPayload := buildDirPayload("docs", fileKey.PublicKey.Compressed())
	dirMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    dirKey.PublicKey,
		ParentTxID:    rootMtx.TxID,
		Payload:       dirPayload,
		ParentUTXO:    rootNodeUTXO,
		ParentPrivKey: rootKey.PrivateKey,
		FeeUTXO:       changeUTXO,
		ParentPubKey:  rootKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned dir tx")

	dirSignedHex, err := tx.SignMetanetTx(dirMtx, []*tx.UTXO{
		rootNodeUTXO,
		changeUTXO,
	})
	require.NoError(t, err, "sign dir tx")

	dirTxIDStr, err := node.SendRawTransaction(ctx, dirSignedHex)
	require.NoError(t, err, "broadcast dir tx")
	t.Logf("dir txid: %s", dirTxIDStr)
	mineOneBlock(t)

	// Prepare dir's NodeUTXO for spending as parent edge.
	dirNodeUTXO := dirMtx.NodeUTXO
	dirNodeUTXOScript, err := tx.BuildP2PKHScript(dirKey.PublicKey)
	require.NoError(t, err)
	dirNodeUTXO.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXO.PrivateKey = dirKey.PrivateKey

	// Prepare change from dir tx as next fee input.
	dirChangeUTXO := dirMtx.ChangeUTXO
	require.NotNil(t, dirChangeUTXO, "dir tx should have change output")
	dirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChangeUTXO.ScriptPubKey = dirChangeScript
	dirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Create file under dir.
	// ==================================================================
	filePayload := []byte("file content for DAG verification test")
	fileMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    fileKey.PublicKey,
		ParentTxID:    dirMtx.TxID,
		Payload:       filePayload,
		ParentUTXO:    dirNodeUTXO,
		ParentPrivKey: dirKey.PrivateKey,
		FeeUTXO:       dirChangeUTXO,
		ParentPubKey:  dirKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned file tx")

	fileSignedHex, err := tx.SignMetanetTx(fileMtx, []*tx.UTXO{
		dirNodeUTXO,
		dirChangeUTXO,
	})
	require.NoError(t, err, "sign file tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast file tx")
	t.Logf("file txid: %s", fileTxIDStr)
	mineOneBlock(t)

	// Prepare dir's refreshed NodeUTXO (output 2 of file tx = parent refresh).
	dirNodeUTXORefresh := fileMtx.ParentUTXO
	require.NotNil(t, dirNodeUTXORefresh, "file tx should refresh dir UTXO")
	dirNodeUTXORefresh.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXORefresh.PrivateKey = dirKey.PrivateKey

	// Prepare change from file tx as next fee input.
	fileChangeUTXO := fileMtx.ChangeUTXO
	require.NotNil(t, fileChangeUTXO, "file tx should have change output")
	fileChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	fileChangeUTXO.ScriptPubKey = fileChangeScript
	fileChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 5: Remove file -- SelfUpdate dir with payload that omits file.
	// ==================================================================
	removedPayload := buildDirPayload("docs") // no file entries

	removeMtx, err := tx.BuildUnsignedSelfUpdateTx(&tx.SelfUpdateParams{
		NodePubKey:  dirKey.PublicKey,
		NodePrivKey: dirKey.PrivateKey,
		ParentTxID:  rootMtx.TxID, // preserve original parent link
		Payload:     removedPayload,
		NodeUTXO:    dirNodeUTXORefresh,
		FeeUTXO:     fileChangeUTXO,
		ChangeAddr:  feeKey.PublicKey.Hash(),
		FeeRate:     1,
	})
	require.NoError(t, err, "build unsigned remove tx")

	removeSignedHex, err := tx.SignMetanetTx(removeMtx, []*tx.UTXO{
		dirNodeUTXORefresh,
		fileChangeUTXO,
	})
	require.NoError(t, err, "sign remove tx")

	removeTxIDStr, err := node.SendRawTransaction(ctx, removeSignedHex)
	require.NoError(t, err, "broadcast remove tx")
	t.Logf("remove txid: %s", removeTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 6: Verify all txids are on-chain and DAG state is correct.
	// ==================================================================
	t.Run("verify_all_txs_on_chain", func(t *testing.T) {
		// Verify root tx is on-chain with MetaFlag.
		rootRaw, err := node.GetRawTransaction(ctx, rootTxIDStr)
		require.NoError(t, err, "get root tx from chain")
		rootParsed, err := transaction.NewTransactionFromBytes(rootRaw)
		require.NoError(t, err, "parse root tx")
		require.True(t, rootParsed.Outputs[0].LockingScript.IsData())
		rootScriptBytes := []byte(*rootParsed.Outputs[0].LockingScript)
		assert.True(t, bytes.Contains(rootScriptBytes, tx.MetaFlagBytes),
			"root OP_RETURN should contain MetaFlag")

		// Verify dir tx is on-chain with correct parent link.
		dirRaw, err := node.GetRawTransaction(ctx, dirTxIDStr)
		require.NoError(t, err, "get dir tx from chain")
		dirParsed, err := transaction.NewTransactionFromBytes(dirRaw)
		require.NoError(t, err, "parse dir tx")
		dirPushes := extractPushData(t, dirParsed.Outputs[0].LockingScript)
		dirPNode, dirParentTxID, _, err := tx.ParseOPReturnData(dirPushes)
		require.NoError(t, err, "parse dir OP_RETURN")

		assert.Equal(t, dirKey.PublicKey.Compressed(), dirPNode,
			"dir P_node should match dir key")

		rootTxIDBytes, err := hex.DecodeString(rootTxIDStr)
		require.NoError(t, err)
		reverseBytes(rootTxIDBytes)
		assert.Equal(t, rootTxIDBytes, dirParentTxID,
			"dir parentTxID should match root txid")

		// Verify file tx is on-chain with correct parent link.
		fileRaw, err := node.GetRawTransaction(ctx, fileTxIDStr)
		require.NoError(t, err, "get file tx from chain")
		fileParsed, err := transaction.NewTransactionFromBytes(fileRaw)
		require.NoError(t, err, "parse file tx")
		filePushes := extractPushData(t, fileParsed.Outputs[0].LockingScript)
		filePNode, fileParentTxID, _, err := tx.ParseOPReturnData(filePushes)
		require.NoError(t, err, "parse file OP_RETURN")

		assert.Equal(t, fileKey.PublicKey.Compressed(), filePNode,
			"file P_node should match file key")

		dirTxIDBytes, err := hex.DecodeString(dirTxIDStr)
		require.NoError(t, err)
		reverseBytes(dirTxIDBytes)
		assert.Equal(t, dirTxIDBytes, fileParentTxID,
			"file parentTxID should match dir txid")

		t.Logf("all original txs verified on-chain with correct parent links")
	})

	t.Run("verify_remove_tx_dag_state", func(t *testing.T) {
		// Retrieve the remove (dir update) tx from chain.
		removeRaw, err := node.GetRawTransaction(ctx, removeTxIDStr)
		require.NoError(t, err, "get remove tx from chain")
		removeParsed, err := transaction.NewTransactionFromBytes(removeRaw)
		require.NoError(t, err, "parse remove tx")

		opReturnOutput := removeParsed.Outputs[0]
		require.True(t, opReturnOutput.LockingScript.IsData(), "output 0 should be OP_RETURN")

		// Verify MetaFlag is present.
		removeScriptBytes := []byte(*opReturnOutput.LockingScript)
		assert.True(t, bytes.Contains(removeScriptBytes, tx.MetaFlagBytes),
			"remove tx OP_RETURN should contain MetaFlag")

		pushes := extractPushData(t, opReturnOutput.LockingScript)
		require.GreaterOrEqual(t, len(pushes), 4)

		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse remove tx OP_RETURN data")

		// P_node should still be the dir key (same node, new version).
		assert.Equal(t, dirKey.PublicKey.Compressed(), pNode,
			"remove tx P_node should still be dir key")

		// parentTxID should still link to root (preserved by SelfUpdate).
		assert.Equal(t, rootMtx.TxID, parentTxID,
			"remove tx parentTxID should still link to root")

		// Payload should NOT contain the file pubkey anymore.
		assert.False(t, bytes.Contains(payload, fileKey.PublicKey.Compressed()),
			"remove tx payload should NOT contain file pubkey")

		// Payload should match the expected empty directory payload.
		assert.Equal(t, removedPayload, payload,
			"remove tx payload should match expected empty dir payload")

		// Verify SelfUpdate tx structure.
		assert.Equal(t, 2, removeParsed.InputCount(),
			"remove tx should have 2 inputs (nodeUTXO + feeUTXO)")
		assert.GreaterOrEqual(t, removeParsed.OutputCount(), 2,
			"remove tx should have >= 2 outputs (OP_RETURN + P_node dust)")
		assert.Equal(t, tx.DustLimit, removeParsed.Outputs[1].Satoshis,
			"output 1 should be P_node dust refresh")

		t.Logf("remove tx DAG state verified: P_node preserved, parent link correct, file entry gone")
	})

	t.Run("verify_file_still_on_chain", func(t *testing.T) {
		// The file tx is still on-chain (orphaned from directory, but immutable).
		fileRaw, err := node.GetRawTransaction(ctx, fileTxIDStr)
		require.NoError(t, err, "file tx should still be retrievable from chain")
		require.NotEmpty(t, fileRaw, "file tx bytes should not be empty")

		fileParsed, err := transaction.NewTransactionFromBytes(fileRaw)
		require.NoError(t, err, "parse file tx")

		filePushes := extractPushData(t, fileParsed.Outputs[0].LockingScript)
		filePNode, fileParentTxID, filePayloadOnChain, err := tx.ParseOPReturnData(filePushes)
		require.NoError(t, err, "parse file OP_RETURN")

		// File's P_node and parent link are immutable on-chain.
		assert.Equal(t, fileKey.PublicKey.Compressed(), filePNode,
			"orphaned file P_node should still be file key")
		assert.Equal(t, dirMtx.TxID, fileParentTxID,
			"orphaned file parentTxID should still link to dir")
		assert.Equal(t, filePayload, filePayloadOnChain,
			"orphaned file payload should still be intact")

		t.Logf("orphaned file still on-chain with original data intact")
	})

	t.Logf("--- Remove and Verify DAG Summary ---")
	t.Logf("Root:         %s", rootTxIDStr)
	t.Logf("  -> Dir:     %s (original, with file entry)", dirTxIDStr)
	t.Logf("    -> File:  %s (orphaned after removal)", fileTxIDStr)
	t.Logf("  -> Dir v2:  %s (updated, file entry removed)", removeTxIDStr)
	t.Logf("DAG integrity verified: all txids on-chain, parent links correct, removal clean")
}

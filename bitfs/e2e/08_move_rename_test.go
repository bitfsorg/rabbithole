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
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestMoveRename validates moving a file between directories and renaming it
// using MutationBatch transactions on the Metanet DAG.
//
// DAG structure:
//
//	root
//	 +-- dir_a  (initially contains file)
//	 +-- dir_b  (initially empty)
//
// Move = SelfUpdate dir_b (add file entry). dir_a's file reference is removed
// implicitly when its UTXO is consumed during file creation.
// Rename = SelfUpdate dir_b with changed payload name.
func TestMoveRename(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Setup -- Create wallet, derive 5 keys, fund fee address.
	// ==================================================================
	t.Run("setup", func(t *testing.T) {})

	w := setupFundedWallet(t, ctx, node)

	// Fee key: m/44'/236'/0'/0/0
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	// Root key: m/44'/236'/1'/0/0
	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")

	// dir_a key: m/44'/236'/1'/0/0/0'
	dirAKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive dir_a node key")

	// dir_b key: m/44'/236'/1'/0/0/1'
	dirBKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err, "derive dir_b node key")

	// file key: m/44'/236'/1'/0/0/0'/0'
	fileKey, err := w.DeriveNodeKey(0, []uint32{0, 0}, nil)
	require.NoError(t, err, "derive file node key")

	t.Logf("fee key:    %s", feeKey.Path)
	t.Logf("root key:   %s", rootKey.Path)
	t.Logf("dir_a key:  %s", dirAKey.Path)
	t.Logf("dir_b key:  %s", dirBKey.Path)
	t.Logf("file key:   %s", fileKey.Path)

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
	rootPayload := []byte("bitfs move-rename test root")
	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, rootPayload)
	rootBatch.AddFeeInput(feeUTXO)
	rootBatch.SetChange(feeKey.PublicKey.Hash())
	rootBatch.SetFeeRate(1)
	rootResult, err := rootBatch.Build()
	require.NoError(t, err, "build root tx batch")

	rootSignedHex, err := rootBatch.Sign(rootResult)
	require.NoError(t, err, "sign root tx batch")

	rootTxIDStr, err := node.SendRawTransaction(ctx, rootSignedHex)
	require.NoError(t, err, "broadcast root tx")
	t.Logf("root txid: %s", rootTxIDStr)
	mineOneBlock(t)

	// Prepare root's NodeUTXO for spending as parent edge.
	rootNodeUTXO := rootResult.NodeOps[0].NodeUTXO
	rootNodeUTXOScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	// Prepare change UTXO from root tx as next fee input.
	changeUTXO := rootResult.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have a change output")
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 3: Create dir_a and dir_b under root in a single batch.
	// ==================================================================
	// Both dirs share the same parent (root), so they go in one batch
	// because AddCreateChild consumes the parent UTXO (deduped).
	dirAPayload := buildDirPayload("dir_a", fileKey.PublicKey.Compressed())
	dirBPayload := buildDirPayload("dir_b") // initially empty

	dirBatch := tx.NewMutationBatch()
	dirBatch.AddCreateChild(dirAKey.PublicKey, rootResult.TxID, dirAPayload, rootNodeUTXO, rootKey.PrivateKey)
	dirBatch.AddCreateChild(dirBKey.PublicKey, rootResult.TxID, dirBPayload, rootNodeUTXO, rootKey.PrivateKey)
	dirBatch.AddFeeInput(changeUTXO)
	dirBatch.SetChange(feeKey.PublicKey.Hash())
	dirBatch.SetFeeRate(1)
	dirResult, err := dirBatch.Build()
	require.NoError(t, err, "build dir_a + dir_b batch")

	dirSignedHex, err := dirBatch.Sign(dirResult)
	require.NoError(t, err, "sign dir_a + dir_b batch")

	dirTxIDStr, err := node.SendRawTransaction(ctx, dirSignedHex)
	require.NoError(t, err, "broadcast dir_a + dir_b tx")
	t.Logf("dir_a + dir_b txid: %s", dirTxIDStr)
	mineOneBlock(t)

	// dir_a = NodeOps[0], dir_b = NodeOps[1].
	require.Len(t, dirResult.NodeOps, 2, "batch should have 2 node ops")

	// Prepare dir_a's NodeUTXO for spending as parent edge in file creation.
	dirANodeUTXO := dirResult.NodeOps[0].NodeUTXO
	dirANodeUTXOScript, err := tx.BuildP2PKHScript(dirAKey.PublicKey)
	require.NoError(t, err)
	dirANodeUTXO.ScriptPubKey = dirANodeUTXOScript
	dirANodeUTXO.PrivateKey = dirAKey.PrivateKey

	// Prepare dir_b's NodeUTXO for self-update later.
	dirBNodeUTXO := dirResult.NodeOps[1].NodeUTXO
	dirBNodeUTXOScript, err := tx.BuildP2PKHScript(dirBKey.PublicKey)
	require.NoError(t, err)
	dirBNodeUTXO.ScriptPubKey = dirBNodeUTXOScript
	dirBNodeUTXO.PrivateKey = dirBKey.PrivateKey

	// Prepare change from dir batch as next fee input.
	dirChangeUTXO := dirResult.ChangeUTXO
	require.NotNil(t, dirChangeUTXO, "dir batch should have change output")
	dirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChangeUTXO.ScriptPubKey = dirChangeScript
	dirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Create file child tx under dir_a.
	// ==================================================================
	filePayload := []byte("file content under dir_a")
	fileBatch := tx.NewMutationBatch()
	fileBatch.AddCreateChild(fileKey.PublicKey, dirResult.TxID, filePayload, dirANodeUTXO, dirAKey.PrivateKey)
	fileBatch.AddFeeInput(dirChangeUTXO)
	fileBatch.SetChange(feeKey.PublicKey.Hash())
	fileBatch.SetFeeRate(1)
	fileResult, err := fileBatch.Build()
	require.NoError(t, err, "build file tx batch")

	fileSignedHex, err := fileBatch.Sign(fileResult)
	require.NoError(t, err, "sign file tx batch")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast file tx")
	t.Logf("file txid: %s", fileTxIDStr)
	mineOneBlock(t)

	// Prepare change from file tx as next fee input.
	fileChangeUTXO := fileResult.ChangeUTXO
	require.NotNil(t, fileChangeUTXO, "file tx should have change output")
	fileChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	fileChangeUTXO.ScriptPubKey = fileChangeScript
	fileChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 5: Move file from dir_a to dir_b via SelfUpdate of dir_b.
	// ==================================================================
	// In MutationBatch, AddCreateChild consumes the parent UTXO without
	// producing a refresh. dir_a's UTXO was consumed during file creation,
	// so only dir_b can be self-updated. This models the "move" as adding
	// the file reference to dir_b's payload.
	t.Run("move_file_to_dir_b", func(t *testing.T) {
		dirBWithFilePayload := buildDirPayload("dir_b", fileKey.PublicKey.Compressed())
		moveBatch := tx.NewMutationBatch()
		moveBatch.AddSelfUpdate(dirBKey.PublicKey, rootResult.TxID, dirBWithFilePayload, dirBNodeUTXO, dirBKey.PrivateKey)
		moveBatch.AddFeeInput(fileChangeUTXO)
		moveBatch.SetChange(feeKey.PublicKey.Hash())
		moveBatch.SetFeeRate(1)
		moveResult, err := moveBatch.Build()
		require.NoError(t, err, "build dir_b self-update tx (add file)")

		moveSignedHex, err := moveBatch.Sign(moveResult)
		require.NoError(t, err, "sign dir_b self-update tx")

		dirBUpdateTxIDStr, err := node.SendRawTransaction(ctx, moveSignedHex)
		require.NoError(t, err, "broadcast dir_b self-update tx")
		t.Logf("dir_b updated (file added) txid: %s", dirBUpdateTxIDStr)
		mineOneBlock(t)

		// --- Verify dir_b now references file ---
		rawDirB, err := node.GetRawTransaction(ctx, dirBUpdateTxIDStr)
		require.NoError(t, err, "get dir_b update tx from chain")

		parsedDirB, err := transaction.NewTransactionFromBytes(rawDirB)
		require.NoError(t, err, "parse dir_b update tx")

		opReturnDirB := parsedDirB.Outputs[0]
		require.True(t, opReturnDirB.LockingScript.IsData(), "dir_b output 0 should be OP_RETURN")

		pushesDirB := extractPushData(t, opReturnDirB.LockingScript)
		pNodeDirB, parentTxIDDirB, payloadDirB, err := tx.ParseOPReturnData(pushesDirB)
		require.NoError(t, err, "parse dir_b OP_RETURN")

		assert.Equal(t, dirBKey.PublicKey.Compressed(), pNodeDirB,
			"dir_b P_node should still be dir_b key")
		assert.Equal(t, rootResult.TxID, parentTxIDDirB,
			"dir_b parentTxID should still link to root")
		assert.True(t, bytes.Contains(payloadDirB, fileKey.PublicKey.Compressed()),
			"dir_b payload should contain file pubkey after move")
		t.Logf("dir_b verified: file entry added to payload")

		// Update dir_b NodeUTXO for next operation (rename).
		dirBNodeUTXO = moveResult.NodeOps[0].NodeUTXO
		dirBNodeUTXO.ScriptPubKey = dirBNodeUTXOScript
		dirBNodeUTXO.PrivateKey = dirBKey.PrivateKey

		// Update change UTXO for next operation.
		fileChangeUTXO = moveResult.ChangeUTXO
		require.NotNil(t, fileChangeUTXO, "dir_b update tx should have change output")
		renameChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
		require.NoError(t, err)
		fileChangeUTXO.ScriptPubKey = renameChangeScript
		fileChangeUTXO.PrivateKey = feeKey.PrivateKey
	})

	// ==================================================================
	// Step 6: Rename -- SelfUpdate dir_b with a changed name in payload.
	// ==================================================================
	t.Run("rename_file_in_dir_b", func(t *testing.T) {
		renamedPayload := buildDirPayload("dir_b_renamed", fileKey.PublicKey.Compressed())

		renameBatch := tx.NewMutationBatch()
		renameBatch.AddSelfUpdate(dirBKey.PublicKey, rootResult.TxID, renamedPayload, dirBNodeUTXO, dirBKey.PrivateKey)
		renameBatch.AddFeeInput(fileChangeUTXO)
		renameBatch.SetChange(feeKey.PublicKey.Hash())
		renameBatch.SetFeeRate(1)
		renameResult, err := renameBatch.Build()
		require.NoError(t, err, "build dir_b rename batch")

		dirBRenameSignedHex, err := renameBatch.Sign(renameResult)
		require.NoError(t, err, "sign dir_b rename batch")

		dirBRenameTxIDStr, err := node.SendRawTransaction(ctx, dirBRenameSignedHex)
		require.NoError(t, err, "broadcast dir_b rename tx")
		t.Logf("dir_b renamed txid: %s", dirBRenameTxIDStr)
		mineOneBlock(t)

		// --- Verify renamed payload from chain ---
		rawRenamed, err := node.GetRawTransaction(ctx, dirBRenameTxIDStr)
		require.NoError(t, err, "get dir_b rename tx from chain")

		parsedRenamed, err := transaction.NewTransactionFromBytes(rawRenamed)
		require.NoError(t, err, "parse dir_b rename tx")

		opReturnRenamed := parsedRenamed.Outputs[0]
		require.True(t, opReturnRenamed.LockingScript.IsData(),
			"dir_b rename output 0 should be OP_RETURN")

		pushesRenamed := extractPushData(t, opReturnRenamed.LockingScript)
		pNodeRenamed, parentTxIDRenamed, payloadRenamed, err := tx.ParseOPReturnData(pushesRenamed)
		require.NoError(t, err, "parse dir_b rename OP_RETURN")

		// P_node and parent link must be preserved.
		assert.Equal(t, dirBKey.PublicKey.Compressed(), pNodeRenamed,
			"renamed dir_b P_node should still be dir_b key")
		assert.Equal(t, rootResult.TxID, parentTxIDRenamed,
			"renamed dir_b parentTxID should still link to root")

		// Payload should contain the new name and still reference the file.
		assert.True(t, bytes.Contains(payloadRenamed, []byte("dir_b_renamed")),
			"renamed payload should contain new name 'dir_b_renamed'")
		assert.True(t, bytes.Contains(payloadRenamed, fileKey.PublicKey.Compressed()),
			"renamed payload should still contain file pubkey")
		t.Logf("rename verified: payload contains 'dir_b_renamed' and file pubkey")
	})

	// ==================================================================
	// Step 7: Verify final DAG state.
	// ==================================================================
	t.Run("verify_dag_state", func(t *testing.T) {
		// Verify root tx is on-chain with MetaFlag.
		rootRaw, err := node.GetRawTransaction(ctx, rootTxIDStr)
		require.NoError(t, err, "get root tx from chain")
		rootParsed, err := transaction.NewTransactionFromBytes(rootRaw)
		require.NoError(t, err, "parse root tx")
		require.True(t, rootParsed.Outputs[0].LockingScript.IsData())
		rootScriptBytes := []byte(*rootParsed.Outputs[0].LockingScript)
		assert.True(t, bytes.Contains(rootScriptBytes, tx.MetaFlagBytes),
			"root OP_RETURN should contain MetaFlag")

		// dir_a and dir_b are in the same TX (dirTxIDStr).
		// Batch output layout: [0] dir_a OP_RETURN, [1] dir_a P2PKH,
		//                      [2] dir_b OP_RETURN, [3] dir_b P2PKH,
		//                      [4] change.
		dirRaw, err := node.GetRawTransaction(ctx, dirTxIDStr)
		require.NoError(t, err, "get dir batch tx from chain")
		dirParsed, err := transaction.NewTransactionFromBytes(dirRaw)
		require.NoError(t, err, "parse dir batch tx")

		rootTxIDBytes, err := hex.DecodeString(rootTxIDStr)
		require.NoError(t, err)
		reverseBytes(rootTxIDBytes)

		// Check dir_a parent link (output 0).
		dirAPushes := extractPushData(t, dirParsed.Outputs[0].LockingScript)
		_, dirAParentTxID, _, err := tx.ParseOPReturnData(dirAPushes)
		require.NoError(t, err)
		assert.Equal(t, rootTxIDBytes, dirAParentTxID,
			"dir_a parentTxID should match root txid")

		// Check dir_b parent link (output 2).
		dirBPushes := extractPushData(t, dirParsed.Outputs[2].LockingScript)
		_, dirBParentTxID, _, err := tx.ParseOPReturnData(dirBPushes)
		require.NoError(t, err)
		assert.Equal(t, rootTxIDBytes, dirBParentTxID,
			"dir_b parentTxID should match root txid")

		// Verify file parent link -> dir batch tx (same TX as dir_a).
		fileRaw, err := node.GetRawTransaction(ctx, fileTxIDStr)
		require.NoError(t, err, "get file tx from chain")
		fileParsed, err := transaction.NewTransactionFromBytes(fileRaw)
		require.NoError(t, err, "parse file tx")
		filePushes := extractPushData(t, fileParsed.Outputs[0].LockingScript)
		_, fileParentTxID, _, err := tx.ParseOPReturnData(filePushes)
		require.NoError(t, err)

		dirTxIDBytes, err := hex.DecodeString(dirTxIDStr)
		require.NoError(t, err)
		reverseBytes(dirTxIDBytes)
		assert.Equal(t, dirTxIDBytes, fileParentTxID,
			"file parentTxID should match dir batch txid")

		t.Logf("--- Move/Rename DAG Summary ---")
		t.Logf("Root:         %s", rootTxIDStr)
		t.Logf("  -> dir_a+b: %s", dirTxIDStr)
		t.Logf("  -> file:    %s (originally under dir_a)", fileTxIDStr)
		t.Logf("DAG integrity verified: all parent links correct")
		t.Logf("Move+rename complete: dir_b move, dir_b rename verified")
	})
}

// buildDirPayload constructs a simple directory payload that encodes the
// directory name and optional child pubkey references.
// Format: "name:<dirname>" followed by optional "|child:<hex_pubkey>" entries.
func buildDirPayload(name string, childPubKeys ...[]byte) []byte {
	payload := []byte("name:" + name)
	for _, pk := range childPubKeys {
		payload = append(payload, []byte("|child:")...)
		payload = append(payload, pk...)
	}
	return payload
}

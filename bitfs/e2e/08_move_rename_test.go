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

// TestMoveRename validates moving a file between directories and renaming it
// using SelfUpdate transactions on the Metanet DAG.
//
// DAG structure:
//
//	root
//	 +-- dir_a  (initially contains file)
//	 +-- dir_b  (initially empty)
//
// Move = SelfUpdate dir_a (remove file entry) + SelfUpdate dir_b (add file entry).
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
	// Step 3: Create dir_a child tx under root.
	// ==================================================================
	// dir_a payload references the file's pubkey (file is a child of dir_a).
	dirAPayload := buildDirPayload("dir_a", fileKey.PublicKey.Compressed())
	dirAMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    dirAKey.PublicKey,
		ParentTxID:    rootMtx.TxID,
		Payload:       dirAPayload,
		ParentUTXO:    rootNodeUTXO,
		ParentPrivKey: rootKey.PrivateKey,
		FeeUTXO:       changeUTXO,
		ParentPubKey:  rootKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned dir_a tx")

	dirASignedHex, err := tx.SignMetanetTx(dirAMtx, []*tx.UTXO{
		rootNodeUTXO,
		changeUTXO,
	})
	require.NoError(t, err, "sign dir_a tx")

	dirATxIDStr, err := node.SendRawTransaction(ctx, dirASignedHex)
	require.NoError(t, err, "broadcast dir_a tx")
	t.Logf("dir_a txid: %s", dirATxIDStr)
	mineOneBlock(t)

	// Prepare dir_a's NodeUTXO for spending as parent edge in file creation.
	dirANodeUTXO := dirAMtx.NodeUTXO
	dirANodeUTXOScript, err := tx.BuildP2PKHScript(dirAKey.PublicKey)
	require.NoError(t, err)
	dirANodeUTXO.ScriptPubKey = dirANodeUTXOScript
	dirANodeUTXO.PrivateKey = dirAKey.PrivateKey

	// Prepare change from dir_a tx as next fee input.
	dirAChangeUTXO := dirAMtx.ChangeUTXO
	require.NotNil(t, dirAChangeUTXO, "dir_a tx should have change output")
	dirAChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirAChangeUTXO.ScriptPubKey = dirAChangeScript
	dirAChangeUTXO.PrivateKey = feeKey.PrivateKey

	// Capture the refreshed root NodeUTXO (output 2 of dir_a tx).
	rootNodeUTXORefresh1 := dirAMtx.ParentUTXO
	require.NotNil(t, rootNodeUTXORefresh1, "dir_a tx should refresh root UTXO")
	rootNodeUTXORefresh1.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXORefresh1.PrivateKey = rootKey.PrivateKey

	// ==================================================================
	// Step 4: Create dir_b child tx under root (using refreshed root UTXO).
	// ==================================================================
	// dir_b initially has no file entries (empty directory).
	dirBPayload := buildDirPayload("dir_b")
	dirBMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    dirBKey.PublicKey,
		ParentTxID:    rootMtx.TxID,
		Payload:       dirBPayload,
		ParentUTXO:    rootNodeUTXORefresh1, // refreshed from dir_a creation
		ParentPrivKey: rootKey.PrivateKey,
		FeeUTXO:       dirAChangeUTXO, // chain fee from dir_a
		ParentPubKey:  rootKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned dir_b tx")

	dirBSignedHex, err := tx.SignMetanetTx(dirBMtx, []*tx.UTXO{
		rootNodeUTXORefresh1,
		dirAChangeUTXO,
	})
	require.NoError(t, err, "sign dir_b tx")

	dirBTxIDStr, err := node.SendRawTransaction(ctx, dirBSignedHex)
	require.NoError(t, err, "broadcast dir_b tx")
	t.Logf("dir_b txid: %s", dirBTxIDStr)
	mineOneBlock(t)

	// Prepare dir_b's NodeUTXO for self-update later.
	dirBNodeUTXO := dirBMtx.NodeUTXO
	dirBNodeUTXOScript, err := tx.BuildP2PKHScript(dirBKey.PublicKey)
	require.NoError(t, err)
	dirBNodeUTXO.ScriptPubKey = dirBNodeUTXOScript
	dirBNodeUTXO.PrivateKey = dirBKey.PrivateKey

	// Prepare change from dir_b tx as next fee input.
	dirBChangeUTXO := dirBMtx.ChangeUTXO
	require.NotNil(t, dirBChangeUTXO, "dir_b tx should have change output")
	dirBChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirBChangeUTXO.ScriptPubKey = dirBChangeScript
	dirBChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 5: Create file child tx under dir_a.
	// ==================================================================
	filePayload := []byte("file content under dir_a")
	fileMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    fileKey.PublicKey,
		ParentTxID:    dirAMtx.TxID,
		Payload:       filePayload,
		ParentUTXO:    dirANodeUTXO,
		ParentPrivKey: dirAKey.PrivateKey,
		FeeUTXO:       dirBChangeUTXO, // chain fee from dir_b
		ParentPubKey:  dirAKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned file tx")

	fileSignedHex, err := tx.SignMetanetTx(fileMtx, []*tx.UTXO{
		dirANodeUTXO,
		dirBChangeUTXO,
	})
	require.NoError(t, err, "sign file tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast file tx")
	t.Logf("file txid: %s", fileTxIDStr)
	mineOneBlock(t)

	// Prepare change from file tx as next fee input.
	fileChangeUTXO := fileMtx.ChangeUTXO
	require.NotNil(t, fileChangeUTXO, "file tx should have change output")
	fileChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	fileChangeUTXO.ScriptPubKey = fileChangeScript
	fileChangeUTXO.PrivateKey = feeKey.PrivateKey

	// Capture the refreshed dir_a NodeUTXO (output 2 of file tx).
	dirANodeUTXORefresh := fileMtx.ParentUTXO
	require.NotNil(t, dirANodeUTXORefresh, "file tx should refresh dir_a UTXO")
	dirANodeUTXORefresh.ScriptPubKey = dirANodeUTXOScript
	dirANodeUTXORefresh.PrivateKey = dirAKey.PrivateKey

	// ==================================================================
	// Step 6: Move file from dir_a to dir_b.
	// ==================================================================
	t.Run("move_file_to_dir_b", func(t *testing.T) {
		// --- 6a: SelfUpdate dir_a with empty payload (file removed) ---
		dirAEmptyPayload := buildDirPayload("dir_a") // no file entries
		dirAUpdateMtx, err := tx.BuildUnsignedSelfUpdateTx(&tx.SelfUpdateParams{
			NodePubKey:  dirAKey.PublicKey,
			NodePrivKey: dirAKey.PrivateKey,
			ParentTxID:  rootMtx.TxID, // preserve original parent link
			Payload:     dirAEmptyPayload,
			NodeUTXO:    dirANodeUTXORefresh,
			FeeUTXO:     fileChangeUTXO,
			ChangeAddr:  feeKey.PublicKey.Hash(),
			FeeRate:     1,
		})
		require.NoError(t, err, "build unsigned dir_a self-update tx (remove file)")

		dirAUpdateSignedHex, err := tx.SignMetanetTx(dirAUpdateMtx, []*tx.UTXO{
			dirANodeUTXORefresh,
			fileChangeUTXO,
		})
		require.NoError(t, err, "sign dir_a self-update tx")

		dirAUpdateTxIDStr, err := node.SendRawTransaction(ctx, dirAUpdateSignedHex)
		require.NoError(t, err, "broadcast dir_a self-update tx")
		t.Logf("dir_a updated (file removed) txid: %s", dirAUpdateTxIDStr)
		mineOneBlock(t)

		// Prepare change from dir_a update for dir_b update fee.
		dirAUpdateChangeUTXO := dirAUpdateMtx.ChangeUTXO
		require.NotNil(t, dirAUpdateChangeUTXO, "dir_a update tx should have change output")
		dirAUpdateChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
		require.NoError(t, err)
		dirAUpdateChangeUTXO.ScriptPubKey = dirAUpdateChangeScript
		dirAUpdateChangeUTXO.PrivateKey = feeKey.PrivateKey

		// --- 6b: SelfUpdate dir_b with payload referencing file's pubkey ---
		dirBWithFilePayload := buildDirPayload("dir_b", fileKey.PublicKey.Compressed())
		dirBUpdateMtx, err := tx.BuildUnsignedSelfUpdateTx(&tx.SelfUpdateParams{
			NodePubKey:  dirBKey.PublicKey,
			NodePrivKey: dirBKey.PrivateKey,
			ParentTxID:  rootMtx.TxID, // preserve original parent link
			Payload:     dirBWithFilePayload,
			NodeUTXO:    dirBNodeUTXO,
			FeeUTXO:     dirAUpdateChangeUTXO,
			ChangeAddr:  feeKey.PublicKey.Hash(),
			FeeRate:     1,
		})
		require.NoError(t, err, "build unsigned dir_b self-update tx (add file)")

		dirBUpdateSignedHex, err := tx.SignMetanetTx(dirBUpdateMtx, []*tx.UTXO{
			dirBNodeUTXO,
			dirAUpdateChangeUTXO,
		})
		require.NoError(t, err, "sign dir_b self-update tx")

		dirBUpdateTxIDStr, err := node.SendRawTransaction(ctx, dirBUpdateSignedHex)
		require.NoError(t, err, "broadcast dir_b self-update tx")
		t.Logf("dir_b updated (file added) txid: %s", dirBUpdateTxIDStr)
		mineOneBlock(t)

		// --- Verify dir_a no longer references file ---
		rawDirA, err := node.GetRawTransaction(ctx, dirAUpdateTxIDStr)
		require.NoError(t, err, "get dir_a update tx from chain")

		parsedDirA, err := transaction.NewTransactionFromBytes(rawDirA)
		require.NoError(t, err, "parse dir_a update tx")

		opReturnDirA := parsedDirA.Outputs[0]
		require.True(t, opReturnDirA.LockingScript.IsData(), "dir_a output 0 should be OP_RETURN")

		pushesDirA := extractPushData(t, opReturnDirA.LockingScript)
		pNodeDirA, parentTxIDDirA, payloadDirA, err := tx.ParseOPReturnData(pushesDirA)
		require.NoError(t, err, "parse dir_a OP_RETURN")

		assert.Equal(t, dirAKey.PublicKey.Compressed(), pNodeDirA,
			"dir_a P_node should still be dir_a key")
		assert.Equal(t, rootMtx.TxID, parentTxIDDirA,
			"dir_a parentTxID should still link to root")
		assert.False(t, bytes.Contains(payloadDirA, fileKey.PublicKey.Compressed()),
			"dir_a payload should NOT contain file pubkey after move")
		t.Logf("dir_a verified: file entry removed from payload")

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
		assert.Equal(t, rootMtx.TxID, parentTxIDDirB,
			"dir_b parentTxID should still link to root")
		assert.True(t, bytes.Contains(payloadDirB, fileKey.PublicKey.Compressed()),
			"dir_b payload should contain file pubkey after move")
		t.Logf("dir_b verified: file entry added to payload")

		// Update dir_b NodeUTXO for next operation (rename).
		dirBNodeUTXO = dirBUpdateMtx.NodeUTXO
		dirBNodeUTXO.ScriptPubKey = dirBNodeUTXOScript
		dirBNodeUTXO.PrivateKey = dirBKey.PrivateKey

		// Update change UTXO for next operation.
		fileChangeUTXO = dirBUpdateMtx.ChangeUTXO
		require.NotNil(t, fileChangeUTXO, "dir_b update tx should have change output")
		renameChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
		require.NoError(t, err)
		fileChangeUTXO.ScriptPubKey = renameChangeScript
		fileChangeUTXO.PrivateKey = feeKey.PrivateKey
	})

	// ==================================================================
	// Step 7: Rename -- SelfUpdate dir_b with a changed name in payload.
	// ==================================================================
	t.Run("rename_file_in_dir_b", func(t *testing.T) {
		// Rename = SelfUpdate dir_b with a modified payload that has a new name
		// but still references the same file pubkey.
		renamedPayload := buildDirPayload("dir_b_renamed", fileKey.PublicKey.Compressed())

		dirBRenameMtx, err := tx.BuildUnsignedSelfUpdateTx(&tx.SelfUpdateParams{
			NodePubKey:  dirBKey.PublicKey,
			NodePrivKey: dirBKey.PrivateKey,
			ParentTxID:  rootMtx.TxID,
			Payload:     renamedPayload,
			NodeUTXO:    dirBNodeUTXO,
			FeeUTXO:     fileChangeUTXO,
			ChangeAddr:  feeKey.PublicKey.Hash(),
			FeeRate:     1,
		})
		require.NoError(t, err, "build unsigned dir_b rename tx")

		dirBRenameSignedHex, err := tx.SignMetanetTx(dirBRenameMtx, []*tx.UTXO{
			dirBNodeUTXO,
			fileChangeUTXO,
		})
		require.NoError(t, err, "sign dir_b rename tx")

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
		assert.Equal(t, rootMtx.TxID, parentTxIDRenamed,
			"renamed dir_b parentTxID should still link to root")

		// Payload should contain the new name and still reference the file.
		assert.True(t, bytes.Contains(payloadRenamed, []byte("dir_b_renamed")),
			"renamed payload should contain new name 'dir_b_renamed'")
		assert.True(t, bytes.Contains(payloadRenamed, fileKey.PublicKey.Compressed()),
			"renamed payload should still contain file pubkey")
		t.Logf("rename verified: payload contains 'dir_b_renamed' and file pubkey")
	})

	// ==================================================================
	// Step 8: Verify final DAG state.
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

		// Verify dir_a parent link -> root.
		dirARaw, err := node.GetRawTransaction(ctx, dirATxIDStr)
		require.NoError(t, err, "get dir_a tx from chain")
		dirAParsed, err := transaction.NewTransactionFromBytes(dirARaw)
		require.NoError(t, err, "parse dir_a tx")
		dirAPushes := extractPushData(t, dirAParsed.Outputs[0].LockingScript)
		_, dirAParentTxID, _, err := tx.ParseOPReturnData(dirAPushes)
		require.NoError(t, err)

		rootTxIDBytes, err := hex.DecodeString(rootTxIDStr)
		require.NoError(t, err)
		reverseBytes(rootTxIDBytes)
		assert.Equal(t, rootTxIDBytes, dirAParentTxID,
			"dir_a parentTxID should match root txid")

		// Verify dir_b parent link -> root.
		dirBRaw, err := node.GetRawTransaction(ctx, dirBTxIDStr)
		require.NoError(t, err, "get dir_b tx from chain")
		dirBParsed, err := transaction.NewTransactionFromBytes(dirBRaw)
		require.NoError(t, err, "parse dir_b tx")
		dirBPushes := extractPushData(t, dirBParsed.Outputs[0].LockingScript)
		_, dirBParentTxID, _, err := tx.ParseOPReturnData(dirBPushes)
		require.NoError(t, err)
		assert.Equal(t, rootTxIDBytes, dirBParentTxID,
			"dir_b parentTxID should match root txid")

		// Verify file parent link -> dir_a.
		fileRaw, err := node.GetRawTransaction(ctx, fileTxIDStr)
		require.NoError(t, err, "get file tx from chain")
		fileParsed, err := transaction.NewTransactionFromBytes(fileRaw)
		require.NoError(t, err, "parse file tx")
		filePushes := extractPushData(t, fileParsed.Outputs[0].LockingScript)
		_, fileParentTxID, _, err := tx.ParseOPReturnData(filePushes)
		require.NoError(t, err)

		dirATxIDBytes, err := hex.DecodeString(dirATxIDStr)
		require.NoError(t, err)
		reverseBytes(dirATxIDBytes)
		assert.Equal(t, dirATxIDBytes, fileParentTxID,
			"file parentTxID should match dir_a txid")

		t.Logf("--- Move/Rename DAG Summary ---")
		t.Logf("Root:         %s", rootTxIDStr)
		t.Logf("  -> dir_a:   %s", dirATxIDStr)
		t.Logf("  -> dir_b:   %s", dirBTxIDStr)
		t.Logf("  -> file:    %s (originally under dir_a)", fileTxIDStr)
		t.Logf("DAG integrity verified: all parent links correct")
		t.Logf("Move+rename complete: dir_a->dir_b move, dir_b rename verified")
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

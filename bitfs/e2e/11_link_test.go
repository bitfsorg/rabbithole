//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/bitfs/e2e/testutil"
	"github.com/tongxiaofeng/libbitfs/tx"
	"github.com/tongxiaofeng/libbitfs/wallet"
)

// TestHardLink validates hard links in the Metanet DAG on regtest.
//
// A hard link means multiple parent directories reference the same child P_node.
// The file itself is unchanged — both directories include the file's pubkey in
// their OP_RETURN payloads.
//
// DAG structure:
//
//	root
//	 +-- dir_a  (contains file entry)
//	 +-- dir_b  (initially empty, then self-updated to also contain file entry)
//
//	file (child of dir_a, but dir_b's payload also references file's pubkey)
//
// Steps:
//  1. Create root, dir_a (with file ref), dir_b (empty), file under dir_a
//  2. SelfUpdate dir_b to add file's pubkey to its payload (hard link)
//  3. Verify: file's compressed pubkey appears in both dir_a and dir_b payloads
func TestHardLink(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Setup -- Create wallet, derive keys, fund fee address.
	// ==================================================================
	w := setupFundedWallet(t, ctx, node)

	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")

	dirAKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive dir_a node key")

	dirBKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err, "derive dir_b node key")

	fileKey, err := w.DeriveNodeKey(0, []uint32{0, 0}, nil)
	require.NoError(t, err, "derive file node key")

	t.Logf("fee key:    %s", feeKey.Path)
	t.Logf("root key:   %s", rootKey.Path)
	t.Logf("dir_a key:  %s", dirAKey.Path)
	t.Logf("dir_b key:  %s", dirBKey.Path)
	t.Logf("file key:   %s", fileKey.Path)

	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)
	t.Logf("fee UTXO: txid=%x, vout=%d, amount=%d sat",
		feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Amount)

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
	rootPayload := []byte("bitfs hard-link test root")
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

	// Prepare root's NodeUTXO.
	rootNodeUTXO := rootMtx.NodeUTXO
	rootNodeUTXOScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	// Prepare change UTXO from root tx.
	changeUTXO := rootMtx.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have a change output")
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 3: Create dir_a (with file's pubkey reference) under root.
	// ==================================================================
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

	// Prepare dir_a's NodeUTXO for file creation.
	dirANodeUTXO := dirAMtx.NodeUTXO
	dirANodeUTXOScript, err := tx.BuildP2PKHScript(dirAKey.PublicKey)
	require.NoError(t, err)
	dirANodeUTXO.ScriptPubKey = dirANodeUTXOScript
	dirANodeUTXO.PrivateKey = dirAKey.PrivateKey

	// Prepare change from dir_a tx.
	dirAChangeUTXO := dirAMtx.ChangeUTXO
	require.NotNil(t, dirAChangeUTXO, "dir_a tx should have change output")
	dirAChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirAChangeUTXO.ScriptPubKey = dirAChangeScript
	dirAChangeUTXO.PrivateKey = feeKey.PrivateKey

	// Capture refreshed root NodeUTXO (output 2 of dir_a tx).
	rootNodeUTXORefresh := dirAMtx.ParentUTXO
	require.NotNil(t, rootNodeUTXORefresh, "dir_a tx should refresh root UTXO")
	rootNodeUTXORefresh.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXORefresh.PrivateKey = rootKey.PrivateKey

	// ==================================================================
	// Step 4: Create dir_b (empty) under root.
	// ==================================================================
	dirBPayload := buildDirPayload("dir_b")
	dirBMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    dirBKey.PublicKey,
		ParentTxID:    rootMtx.TxID,
		Payload:       dirBPayload,
		ParentUTXO:    rootNodeUTXORefresh,
		ParentPrivKey: rootKey.PrivateKey,
		FeeUTXO:       dirAChangeUTXO,
		ParentPubKey:  rootKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned dir_b tx")

	dirBSignedHex, err := tx.SignMetanetTx(dirBMtx, []*tx.UTXO{
		rootNodeUTXORefresh,
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

	// Prepare change from dir_b tx.
	dirBChangeUTXO := dirBMtx.ChangeUTXO
	require.NotNil(t, dirBChangeUTXO, "dir_b tx should have change output")
	dirBChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirBChangeUTXO.ScriptPubKey = dirBChangeScript
	dirBChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 5: Create file under dir_a.
	// ==================================================================
	filePayload := []byte("hard-link test file content")
	fileMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    fileKey.PublicKey,
		ParentTxID:    dirAMtx.TxID,
		Payload:       filePayload,
		ParentUTXO:    dirANodeUTXO,
		ParentPrivKey: dirAKey.PrivateKey,
		FeeUTXO:       dirBChangeUTXO,
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

	// Prepare change from file tx for the hard-link self-update.
	fileChangeUTXO := fileMtx.ChangeUTXO
	require.NotNil(t, fileChangeUTXO, "file tx should have change output")
	fileChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	fileChangeUTXO.ScriptPubKey = fileChangeScript
	fileChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 6: SelfUpdate dir_b to add file's pubkey (hard link).
	// ==================================================================
	t.Run("hard_link_dir_b", func(t *testing.T) {
		dirBHardLinkPayload := buildDirPayload("dir_b", fileKey.PublicKey.Compressed())
		dirBUpdateMtx, err := tx.BuildUnsignedSelfUpdateTx(&tx.SelfUpdateParams{
			NodePubKey:  dirBKey.PublicKey,
			NodePrivKey: dirBKey.PrivateKey,
			ParentTxID:  rootMtx.TxID,
			Payload:     dirBHardLinkPayload,
			NodeUTXO:    dirBNodeUTXO,
			FeeUTXO:     fileChangeUTXO,
			ChangeAddr:  feeKey.PublicKey.Hash(),
			FeeRate:     1,
		})
		require.NoError(t, err, "build unsigned dir_b self-update tx (hard link)")

		dirBUpdateSignedHex, err := tx.SignMetanetTx(dirBUpdateMtx, []*tx.UTXO{
			dirBNodeUTXO,
			fileChangeUTXO,
		})
		require.NoError(t, err, "sign dir_b self-update tx")

		dirBUpdateTxIDStr, err := node.SendRawTransaction(ctx, dirBUpdateSignedHex)
		require.NoError(t, err, "broadcast dir_b self-update tx")
		t.Logf("dir_b hard-link update txid: %s", dirBUpdateTxIDStr)
		mineOneBlock(t)

		// --- Verify dir_a payload still contains file's pubkey ---
		rawDirA, err := node.GetRawTransaction(ctx, dirATxIDStr)
		require.NoError(t, err, "get dir_a tx from chain")

		parsedDirA, err := transaction.NewTransactionFromBytes(rawDirA)
		require.NoError(t, err, "parse dir_a tx")

		opReturnDirA := parsedDirA.Outputs[0]
		require.True(t, opReturnDirA.LockingScript.IsData(), "dir_a output 0 should be OP_RETURN")

		pushesDirA := extractPushData(t, opReturnDirA.LockingScript)
		_, _, payloadDirA, err := tx.ParseOPReturnData(pushesDirA)
		require.NoError(t, err, "parse dir_a OP_RETURN")

		assert.True(t, bytes.Contains(payloadDirA, fileKey.PublicKey.Compressed()),
			"dir_a payload should contain file pubkey (original reference)")
		t.Logf("dir_a verified: contains file pubkey %x", fileKey.PublicKey.Compressed()[:8])

		// --- Verify dir_b (updated) payload now also contains file's pubkey ---
		rawDirB, err := node.GetRawTransaction(ctx, dirBUpdateTxIDStr)
		require.NoError(t, err, "get dir_b update tx from chain")

		parsedDirB, err := transaction.NewTransactionFromBytes(rawDirB)
		require.NoError(t, err, "parse dir_b update tx")

		opReturnDirB := parsedDirB.Outputs[0]
		require.True(t, opReturnDirB.LockingScript.IsData(), "dir_b output 0 should be OP_RETURN")

		pushesDirB := extractPushData(t, opReturnDirB.LockingScript)
		pNodeDirB, _, payloadDirB, err := tx.ParseOPReturnData(pushesDirB)
		require.NoError(t, err, "parse dir_b OP_RETURN")

		assert.Equal(t, dirBKey.PublicKey.Compressed(), pNodeDirB,
			"dir_b P_node should be dir_b's own key")
		assert.True(t, bytes.Contains(payloadDirB, fileKey.PublicKey.Compressed()),
			"dir_b payload should contain file pubkey (hard link)")
		t.Logf("dir_b verified: contains file pubkey %x (hard link)", fileKey.PublicKey.Compressed()[:8])

		// --- Verify the same file pubkey bytes appear in both payloads ---
		filePub := fileKey.PublicKey.Compressed()
		assert.True(t,
			bytes.Contains(payloadDirA, filePub) && bytes.Contains(payloadDirB, filePub),
			"hard link: same file P_node (%x) referenced by both dir_a and dir_b", filePub[:8])

		t.Logf("--- Hard Link Summary ---")
		t.Logf("Root:      %s", rootTxIDStr)
		t.Logf("  dir_a:   %s (file ref in original payload)", dirATxIDStr)
		t.Logf("  dir_b:   %s (file ref added via SelfUpdate)", dirBUpdateTxIDStr)
		t.Logf("  file:    %s (P_node=%x)", fileTxIDStr, filePub[:8])
		t.Logf("Hard link verified: file pubkey in both dir_a and dir_b payloads")
	})
}

// TestSoftLink validates soft links in the Metanet DAG on regtest.
//
// A soft link creates a separate node (with its own P_node) whose payload
// contains the target file's compressed pubkey as a reference.
//
// DAG structure:
//
//	root
//	 +-- dir
//	      +-- file  (actual file)
//	      +-- link  (soft link, payload contains file's pubkey as target)
//
// Steps:
//  1. Create root -> dir -> file
//  2. Create link as a new child of dir, with payload containing file's pubkey
//  3. Verify: link has its own P_node (different from file), link payload
//     contains file's compressed pubkey as the target reference.
func TestSoftLink(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Setup -- Create wallet, derive keys, fund fee address.
	// ==================================================================
	w := setupFundedWallet(t, ctx, node)

	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")

	dirKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive dir node key")

	fileKey, err := w.DeriveNodeKey(0, []uint32{0, 0}, nil)
	require.NoError(t, err, "derive file node key")

	linkKey, err := w.DeriveNodeKey(0, []uint32{0, 1}, nil)
	require.NoError(t, err, "derive link node key")

	t.Logf("fee key:    %s", feeKey.Path)
	t.Logf("root key:   %s", rootKey.Path)
	t.Logf("dir key:    %s", dirKey.Path)
	t.Logf("file key:   %s", fileKey.Path)
	t.Logf("link key:   %s", linkKey.Path)

	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)
	t.Logf("fee UTXO: txid=%x, vout=%d, amount=%d sat",
		feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Amount)

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
	rootPayload := []byte("bitfs soft-link test root")
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

	// Prepare root's NodeUTXO.
	rootNodeUTXO := rootMtx.NodeUTXO
	rootNodeUTXOScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	// Prepare change UTXO from root tx.
	changeUTXO := rootMtx.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have a change output")
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 3: Create dir under root.
	// ==================================================================
	// dir payload references both file and link as children.
	dirPayload := buildDirPayload("dir",
		fileKey.PublicKey.Compressed(),
		linkKey.PublicKey.Compressed(),
	)
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

	// Prepare dir's NodeUTXO for file creation.
	dirNodeUTXO := dirMtx.NodeUTXO
	dirNodeUTXOScript, err := tx.BuildP2PKHScript(dirKey.PublicKey)
	require.NoError(t, err)
	dirNodeUTXO.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXO.PrivateKey = dirKey.PrivateKey

	// Prepare change from dir tx.
	dirChangeUTXO := dirMtx.ChangeUTXO
	require.NotNil(t, dirChangeUTXO, "dir tx should have change output")
	dirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChangeUTXO.ScriptPubKey = dirChangeScript
	dirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Create file under dir.
	// ==================================================================
	filePayload := []byte("soft-link test file content")
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

	// Prepare change from file tx.
	fileChangeUTXO := fileMtx.ChangeUTXO
	require.NotNil(t, fileChangeUTXO, "file tx should have change output")
	fileChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	fileChangeUTXO.ScriptPubKey = fileChangeScript
	fileChangeUTXO.PrivateKey = feeKey.PrivateKey

	// Capture refreshed dir NodeUTXO (output 2 of file tx).
	dirNodeUTXORefresh := fileMtx.ParentUTXO
	require.NotNil(t, dirNodeUTXORefresh, "file tx should refresh dir UTXO")
	dirNodeUTXORefresh.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXORefresh.PrivateKey = dirKey.PrivateKey

	// ==================================================================
	// Step 5: Create soft link node under dir.
	// ==================================================================
	// The soft link payload contains "symlink:" prefix followed by the target
	// file's compressed pubkey. This allows traversal to resolve the link.
	linkPayload := append([]byte("symlink:"), fileKey.PublicKey.Compressed()...)

	linkMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    linkKey.PublicKey,
		ParentTxID:    dirMtx.TxID,
		Payload:       linkPayload,
		ParentUTXO:    dirNodeUTXORefresh,
		ParentPrivKey: dirKey.PrivateKey,
		FeeUTXO:       fileChangeUTXO,
		ParentPubKey:  dirKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned link tx")

	linkSignedHex, err := tx.SignMetanetTx(linkMtx, []*tx.UTXO{
		dirNodeUTXORefresh,
		fileChangeUTXO,
	})
	require.NoError(t, err, "sign link tx")

	linkTxIDStr, err := node.SendRawTransaction(ctx, linkSignedHex)
	require.NoError(t, err, "broadcast link tx")
	t.Logf("link txid: %s", linkTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 6: Verify soft link from chain.
	// ==================================================================
	t.Run("verify_soft_link", func(t *testing.T) {
		// --- Verify the link node from chain ---
		rawLink, err := node.GetRawTransaction(ctx, linkTxIDStr)
		require.NoError(t, err, "get link tx from chain")

		parsedLink, err := transaction.NewTransactionFromBytes(rawLink)
		require.NoError(t, err, "parse link tx")

		opReturnLink := parsedLink.Outputs[0]
		require.True(t, opReturnLink.LockingScript.IsData(), "link output 0 should be OP_RETURN")

		pushesLink := extractPushData(t, opReturnLink.LockingScript)
		pNodeLink, _, payloadLink, err := tx.ParseOPReturnData(pushesLink)
		require.NoError(t, err, "parse link OP_RETURN")

		// Soft link's P_node should be the link key, NOT the file key.
		assert.Equal(t, linkKey.PublicKey.Compressed(), pNodeLink,
			"soft link P_node should be link's own key")
		assert.NotEqual(t, fileKey.PublicKey.Compressed(), pNodeLink,
			"soft link P_node should be different from file's key")

		// Soft link's payload should contain the target file's compressed pubkey.
		assert.True(t, bytes.Contains(payloadLink, fileKey.PublicKey.Compressed()),
			"soft link payload should contain target file's pubkey")

		// Also verify it starts with the "symlink:" prefix.
		assert.True(t, bytes.HasPrefix(payloadLink, []byte("symlink:")),
			"soft link payload should start with 'symlink:' prefix")

		t.Logf("link P_node:   %x", pNodeLink[:8])
		t.Logf("file P_node:   %x", fileKey.PublicKey.Compressed()[:8])
		t.Logf("link payload target: %x", payloadLink[len("symlink:"):len("symlink:")+8])

		// --- Verify the file node from chain for comparison ---
		rawFile, err := node.GetRawTransaction(ctx, fileTxIDStr)
		require.NoError(t, err, "get file tx from chain")

		parsedFile, err := transaction.NewTransactionFromBytes(rawFile)
		require.NoError(t, err, "parse file tx")

		opReturnFile := parsedFile.Outputs[0]
		require.True(t, opReturnFile.LockingScript.IsData(), "file output 0 should be OP_RETURN")

		pushesFile := extractPushData(t, opReturnFile.LockingScript)
		pNodeFile, _, _, err := tx.ParseOPReturnData(pushesFile)
		require.NoError(t, err, "parse file OP_RETURN")

		// Confirm file P_node matches fileKey.
		assert.Equal(t, fileKey.PublicKey.Compressed(), pNodeFile,
			"file P_node should match file key")

		// Confirm link and file have different P_nodes.
		assert.NotEqual(t, pNodeLink, pNodeFile,
			"soft link and file should have different P_nodes")

		t.Logf("--- Soft Link Summary ---")
		t.Logf("Root:     %s", rootTxIDStr)
		t.Logf("  dir:    %s", dirTxIDStr)
		t.Logf("    file: %s (P_node=%x)", fileTxIDStr, pNodeFile[:8])
		t.Logf("    link: %s (P_node=%x, target=%x)", linkTxIDStr, pNodeLink[:8], fileKey.PublicKey.Compressed()[:8])
		t.Logf("Soft link verified: different P_node, payload contains target pubkey")
	})
}

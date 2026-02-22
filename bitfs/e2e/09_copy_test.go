//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/bitfs/e2e/testutil"
	"github.com/tongxiaofeng/libbitfs/method42"
	"github.com/tongxiaofeng/libbitfs/tx"
	"github.com/tongxiaofeng/libbitfs/wallet"
)

// TestCopyFile validates copying a file in the Metanet DAG. A "copy" creates
// an independent node with a new P_node but identical decrypted content.
//
// DAG structure:
//
//	root
//	 +-- dir
//	      +-- original (file, Free encrypted)
//	      +-- copy     (file, Free encrypted, same plaintext, different key)
//
// Steps:
//  1. Create root -> dir -> original file (encrypted Free)
//  2. Decrypt original content
//  3. Generate new key for copy, re-encrypt same plaintext
//  4. Build CreateChild tx for copy under same dir
//  5. Verify: different P_node, different TxID, same decrypted plaintext
func TestCopyFile(t *testing.T) {
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
	require.NoError(t, err, "derive original file node key")

	copyKey, err := w.DeriveNodeKey(0, []uint32{0, 1}, nil)
	require.NoError(t, err, "derive copy file node key")

	t.Logf("fee key:      %s", feeKey.Path)
	t.Logf("root key:     %s", rootKey.Path)
	t.Logf("dir key:      %s", dirKey.Path)
	t.Logf("file key:     %s", fileKey.Path)
	t.Logf("copy key:     %s", copyKey.Path)

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
	rootPayload := []byte("bitfs copy test root")
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
	// Step 3: Create dir under root.
	// ==================================================================
	dirPayload := []byte("bitfs directory: docs")
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
	// Step 4: Create original file under dir (Free encrypted).
	// ==================================================================
	originalContent := []byte("Hello BitFS! Original file content for copy test.")

	encResult, err := method42.Encrypt(originalContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt original (free)")
	require.NotEmpty(t, encResult.Ciphertext)
	require.NotEmpty(t, encResult.KeyHash)
	t.Logf("encrypted original: %d bytes -> %d bytes ciphertext", len(originalContent), len(encResult.Ciphertext))

	// Payload = keyHash(32B) + ciphertext.
	filePayload := make([]byte, 0, 32+len(encResult.Ciphertext))
	filePayload = append(filePayload, encResult.KeyHash...)
	filePayload = append(filePayload, encResult.Ciphertext...)

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
	require.NoError(t, err, "build unsigned original file tx")

	fileSignedHex, err := tx.SignMetanetTx(fileMtx, []*tx.UTXO{
		dirNodeUTXO,
		dirChangeUTXO,
	})
	require.NoError(t, err, "sign original file tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast original file tx")
	t.Logf("original file txid: %s", fileTxIDStr)
	mineOneBlock(t)

	// Prepare dir's refreshed NodeUTXO from the file tx (output 2 = parent refresh).
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
	// Step 5: Copy -- re-encrypt same plaintext with new key, create new child.
	// ==================================================================
	copyEncResult, err := method42.Encrypt(originalContent, copyKey.PrivateKey, copyKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt copy (free)")
	require.NotEmpty(t, copyEncResult.Ciphertext)
	require.NotEmpty(t, copyEncResult.KeyHash)
	t.Logf("encrypted copy: %d bytes -> %d bytes ciphertext", len(originalContent), len(copyEncResult.Ciphertext))

	// Copy payload = keyHash(32B) + ciphertext.
	copyPayload := make([]byte, 0, 32+len(copyEncResult.Ciphertext))
	copyPayload = append(copyPayload, copyEncResult.KeyHash...)
	copyPayload = append(copyPayload, copyEncResult.Ciphertext...)

	// Build CreateChild tx for the copy under the same dir.
	// Uses dir's refreshed NodeUTXO from the original file's CreateChild tx.
	copyMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    copyKey.PublicKey,
		ParentTxID:    dirMtx.TxID,
		Payload:       copyPayload,
		ParentUTXO:    dirNodeUTXORefresh,
		ParentPrivKey: dirKey.PrivateKey,
		FeeUTXO:       fileChangeUTXO,
		ParentPubKey:  dirKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned copy file tx")

	copySignedHex, err := tx.SignMetanetTx(copyMtx, []*tx.UTXO{
		dirNodeUTXORefresh,
		fileChangeUTXO,
	})
	require.NoError(t, err, "sign copy file tx")

	copyTxIDStr, err := node.SendRawTransaction(ctx, copySignedHex)
	require.NoError(t, err, "broadcast copy file tx")
	t.Logf("copy file txid: %s", copyTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 6: Verify -- different P_node, different TxID, same plaintext.
	// ==================================================================
	t.Run("verify_copy_different_pnode_same_content", func(t *testing.T) {
		// Different P_node (public keys).
		assert.NotEqual(t, fileKey.PublicKey.Compressed(), copyKey.PublicKey.Compressed(),
			"original and copy should have different P_node keys")

		// Different TxID.
		assert.NotEqual(t, fileTxIDStr, copyTxIDStr,
			"original and copy should have different TxIDs")

		// Read back the copy from chain and decrypt.
		rawCopy, err := node.GetRawTransaction(ctx, copyTxIDStr)
		require.NoError(t, err, "get copy tx from chain")

		parsedCopy, err := transaction.NewTransactionFromBytes(rawCopy)
		require.NoError(t, err, "parse copy tx")

		opReturnCopy := parsedCopy.Outputs[0]
		require.True(t, opReturnCopy.LockingScript.IsData(), "copy output 0 should be OP_RETURN")

		pushes := extractPushData(t, opReturnCopy.LockingScript)
		require.GreaterOrEqual(t, len(pushes), 4, "copy OP_RETURN should have >= 4 pushes")

		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse copy OP_RETURN data")

		// Verify copy's P_node is the copy key.
		assert.Equal(t, copyKey.PublicKey.Compressed(), pNode,
			"copy P_node should match copy key")

		// Verify copy's parent link points to the same dir.
		assert.Equal(t, dirMtx.TxID, parentTxID,
			"copy parentTxID should link to same dir")

		// Decrypt the copy and verify content matches original.
		require.True(t, len(payload) > 32, "payload should contain keyHash + ciphertext")
		extractedKeyHash := payload[:32]
		extractedCiphertext := payload[32:]

		decResult, err := method42.Decrypt(
			extractedCiphertext,
			copyKey.PrivateKey,
			copyKey.PublicKey,
			extractedKeyHash,
			method42.AccessFree,
		)
		require.NoError(t, err, "decrypt copy file content")
		assert.Equal(t, originalContent, decResult.Plaintext,
			"copy decrypted content should match original plaintext")

		t.Logf("copy verified: different P_node, different TxID, same plaintext (%d bytes)", len(decResult.Plaintext))
	})

	// Also verify original is still readable.
	t.Run("verify_original_still_readable", func(t *testing.T) {
		rawOriginal, err := node.GetRawTransaction(ctx, fileTxIDStr)
		require.NoError(t, err, "get original tx from chain")

		parsedOriginal, err := transaction.NewTransactionFromBytes(rawOriginal)
		require.NoError(t, err, "parse original tx")

		opReturnOriginal := parsedOriginal.Outputs[0]
		require.True(t, opReturnOriginal.LockingScript.IsData())

		pushes := extractPushData(t, opReturnOriginal.LockingScript)
		pNode, _, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err)

		assert.Equal(t, fileKey.PublicKey.Compressed(), pNode,
			"original P_node should match file key")

		extractedKeyHash := payload[:32]
		extractedCiphertext := payload[32:]

		decResult, err := method42.Decrypt(
			extractedCiphertext,
			fileKey.PrivateKey,
			fileKey.PublicKey,
			extractedKeyHash,
			method42.AccessFree,
		)
		require.NoError(t, err, "decrypt original file content")
		assert.Equal(t, originalContent, decResult.Plaintext,
			"original decrypted content should still match")

		t.Logf("original still readable: %d bytes decrypted correctly", len(decResult.Plaintext))
	})

	t.Logf("--- Copy File DAG Summary ---")
	t.Logf("Root:             %s", rootTxIDStr)
	t.Logf("  -> Dir:         %s", dirTxIDStr)
	t.Logf("    -> Original:  %s", fileTxIDStr)
	t.Logf("    -> Copy:      %s", copyTxIDStr)
}

// TestCopyIndependence verifies that after copying a file, updating the
// original does not affect the copy. The copy retains its original content
// even after the source file is modified via SelfUpdate.
//
// DAG structure:
//
//	root
//	 +-- dir
//	      +-- original (file, Free encrypted, then self-updated)
//	      +-- copy     (file, Free encrypted, should retain original content)
func TestCopyIndependence(t *testing.T) {
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
	require.NoError(t, err, "derive original file node key")

	copyKey, err := w.DeriveNodeKey(0, []uint32{0, 1}, nil)
	require.NoError(t, err, "derive copy file node key")

	t.Logf("fee key:      %s", feeKey.Path)
	t.Logf("root key:     %s", rootKey.Path)
	t.Logf("dir key:      %s", dirKey.Path)
	t.Logf("file key:     %s", fileKey.Path)
	t.Logf("copy key:     %s", copyKey.Path)

	// Fund the fee key address.
	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)

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
	rootPayload := []byte("bitfs copy-independence test root")
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

	rootNodeUTXO := rootMtx.NodeUTXO
	rootNodeUTXOScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	changeUTXO := rootMtx.ChangeUTXO
	require.NotNil(t, changeUTXO)
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 3: Create dir under root.
	// ==================================================================
	dirPayload := []byte("bitfs directory: docs")
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

	dirNodeUTXO := dirMtx.NodeUTXO
	dirNodeUTXOScript, err := tx.BuildP2PKHScript(dirKey.PublicKey)
	require.NoError(t, err)
	dirNodeUTXO.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXO.PrivateKey = dirKey.PrivateKey

	dirChangeUTXO := dirMtx.ChangeUTXO
	require.NotNil(t, dirChangeUTXO)
	dirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChangeUTXO.ScriptPubKey = dirChangeScript
	dirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Create original file under dir (Free encrypted).
	// ==================================================================
	originalContent := []byte("Original content before any updates.")

	encResult, err := method42.Encrypt(originalContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt original (free)")

	filePayload := make([]byte, 0, 32+len(encResult.Ciphertext))
	filePayload = append(filePayload, encResult.KeyHash...)
	filePayload = append(filePayload, encResult.Ciphertext...)

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
	require.NoError(t, err, "build unsigned original file tx")

	fileSignedHex, err := tx.SignMetanetTx(fileMtx, []*tx.UTXO{
		dirNodeUTXO,
		dirChangeUTXO,
	})
	require.NoError(t, err, "sign original file tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast original file tx")
	t.Logf("original file txid: %s", fileTxIDStr)
	mineOneBlock(t)

	// Prepare dir's refreshed NodeUTXO (output 2 of file tx = parent refresh).
	dirNodeUTXORefresh := fileMtx.ParentUTXO
	require.NotNil(t, dirNodeUTXORefresh, "file tx should refresh dir UTXO")
	dirNodeUTXORefresh.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXORefresh.PrivateKey = dirKey.PrivateKey

	// Prepare file's NodeUTXO for SelfUpdate later.
	fileNodeUTXO := fileMtx.NodeUTXO
	fileNodeUTXOScript, err := tx.BuildP2PKHScript(fileKey.PublicKey)
	require.NoError(t, err)
	fileNodeUTXO.ScriptPubKey = fileNodeUTXOScript
	fileNodeUTXO.PrivateKey = fileKey.PrivateKey

	// Prepare change from file tx.
	fileChangeUTXO := fileMtx.ChangeUTXO
	require.NotNil(t, fileChangeUTXO)
	fileChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	fileChangeUTXO.ScriptPubKey = fileChangeScript
	fileChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 5: Copy original file under same dir.
	// ==================================================================
	copyEncResult, err := method42.Encrypt(originalContent, copyKey.PrivateKey, copyKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt copy (free)")

	copyPayload := make([]byte, 0, 32+len(copyEncResult.Ciphertext))
	copyPayload = append(copyPayload, copyEncResult.KeyHash...)
	copyPayload = append(copyPayload, copyEncResult.Ciphertext...)

	copyMtx, err := tx.BuildUnsignedCreateChildTx(&tx.CreateChildParams{
		NodePubKey:    copyKey.PublicKey,
		ParentTxID:    dirMtx.TxID,
		Payload:       copyPayload,
		ParentUTXO:    dirNodeUTXORefresh,
		ParentPrivKey: dirKey.PrivateKey,
		FeeUTXO:       fileChangeUTXO,
		ParentPubKey:  dirKey.PublicKey,
		ChangeAddr:    feeKey.PublicKey.Hash(),
		FeeRate:       1,
	})
	require.NoError(t, err, "build unsigned copy file tx")

	copySignedHex, err := tx.SignMetanetTx(copyMtx, []*tx.UTXO{
		dirNodeUTXORefresh,
		fileChangeUTXO,
	})
	require.NoError(t, err, "sign copy file tx")

	copyTxIDStr, err := node.SendRawTransaction(ctx, copySignedHex)
	require.NoError(t, err, "broadcast copy file tx")
	t.Logf("copy file txid: %s", copyTxIDStr)
	mineOneBlock(t)

	// Prepare change from copy tx for the SelfUpdate fee.
	copyChangeUTXO := copyMtx.ChangeUTXO
	require.NotNil(t, copyChangeUTXO, "copy tx should have change output")
	copyChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	copyChangeUTXO.ScriptPubKey = copyChangeScript
	copyChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 6: SelfUpdate original file with new content.
	// ==================================================================
	updatedContent := []byte("Updated content! The original file has been modified.")

	updatedEncResult, err := method42.Encrypt(updatedContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt updated content")

	updatedPayload := make([]byte, 0, 32+len(updatedEncResult.Ciphertext))
	updatedPayload = append(updatedPayload, updatedEncResult.KeyHash...)
	updatedPayload = append(updatedPayload, updatedEncResult.Ciphertext...)

	updateMtx, err := tx.BuildUnsignedSelfUpdateTx(&tx.SelfUpdateParams{
		NodePubKey:  fileKey.PublicKey,
		NodePrivKey: fileKey.PrivateKey,
		ParentTxID:  dirMtx.TxID, // preserve original parent link
		Payload:     updatedPayload,
		NodeUTXO:    fileNodeUTXO,
		FeeUTXO:     copyChangeUTXO,
		ChangeAddr:  feeKey.PublicKey.Hash(),
		FeeRate:     1,
	})
	require.NoError(t, err, "build unsigned self-update tx")

	updateSignedHex, err := tx.SignMetanetTx(updateMtx, []*tx.UTXO{
		fileNodeUTXO,
		copyChangeUTXO,
	})
	require.NoError(t, err, "sign self-update tx")

	updateTxIDStr, err := node.SendRawTransaction(ctx, updateSignedHex)
	require.NoError(t, err, "broadcast self-update tx")
	t.Logf("self-update txid: %s", updateTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 7: Verify copy still has original content (independence).
	// ==================================================================
	t.Run("copy_retains_original_content", func(t *testing.T) {
		rawCopy, err := node.GetRawTransaction(ctx, copyTxIDStr)
		require.NoError(t, err, "get copy tx from chain")

		parsedCopy, err := transaction.NewTransactionFromBytes(rawCopy)
		require.NoError(t, err, "parse copy tx")

		opReturnCopy := parsedCopy.Outputs[0]
		require.True(t, opReturnCopy.LockingScript.IsData())

		pushes := extractPushData(t, opReturnCopy.LockingScript)
		_, _, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse copy OP_RETURN")

		require.True(t, len(payload) > 32)
		extractedKeyHash := payload[:32]
		extractedCiphertext := payload[32:]

		decResult, err := method42.Decrypt(
			extractedCiphertext,
			copyKey.PrivateKey,
			copyKey.PublicKey,
			extractedKeyHash,
			method42.AccessFree,
		)
		require.NoError(t, err, "decrypt copy file content")
		assert.Equal(t, originalContent, decResult.Plaintext,
			"copy should still have original content after original was updated")
		assert.NotEqual(t, updatedContent, decResult.Plaintext,
			"copy should NOT have the updated content")

		t.Logf("copy independence verified: copy has original content (%d bytes)", len(decResult.Plaintext))
	})

	// ==================================================================
	// Step 8: Verify original now has updated content.
	// ==================================================================
	t.Run("original_has_updated_content", func(t *testing.T) {
		rawUpdate, err := node.GetRawTransaction(ctx, updateTxIDStr)
		require.NoError(t, err, "get update tx from chain")

		parsedUpdate, err := transaction.NewTransactionFromBytes(rawUpdate)
		require.NoError(t, err, "parse update tx")

		opReturnUpdate := parsedUpdate.Outputs[0]
		require.True(t, opReturnUpdate.LockingScript.IsData())

		pushes := extractPushData(t, opReturnUpdate.LockingScript)
		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse update OP_RETURN")

		// SelfUpdate preserves P_node and parentTxID.
		assert.Equal(t, fileKey.PublicKey.Compressed(), pNode,
			"updated P_node should still be file key")
		assert.Equal(t, dirMtx.TxID, parentTxID,
			"updated parentTxID should still link to dir")

		require.True(t, len(payload) > 32)
		extractedKeyHash := payload[:32]
		extractedCiphertext := payload[32:]

		decResult, err := method42.Decrypt(
			extractedCiphertext,
			fileKey.PrivateKey,
			fileKey.PublicKey,
			extractedKeyHash,
			method42.AccessFree,
		)
		require.NoError(t, err, "decrypt updated file content")
		assert.Equal(t, updatedContent, decResult.Plaintext,
			"original should now have updated content")
		assert.NotEqual(t, originalContent, decResult.Plaintext,
			"original should NOT have old content after update")

		t.Logf("original updated verified: now has updated content (%d bytes)", len(decResult.Plaintext))
	})

	t.Logf("--- Copy Independence DAG Summary ---")
	t.Logf("Root:              %s", rootTxIDStr)
	t.Logf("  -> Dir:          %s", dirTxIDStr)
	t.Logf("    -> Original:   %s (created)", fileTxIDStr)
	t.Logf("    -> Copy:       %s (independent copy)", copyTxIDStr)
	t.Logf("    -> Update:     %s (original self-updated)", updateTxIDStr)
	t.Logf("Copy independence verified: updating original does not affect copy")
}

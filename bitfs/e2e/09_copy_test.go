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
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
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
//  1. Create root -> dir -> original file + copy (in same batch, both Free encrypted)
//  2. Verify: different P_node, different vout, same decrypted plaintext
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
	// Step 3: Create dir under root.
	// ==================================================================
	dirPayload := []byte("bitfs directory: docs")
	dirBatch := tx.NewMutationBatch()
	dirBatch.AddCreateChild(dirKey.PublicKey, rootResult.TxID, dirPayload, rootNodeUTXO, rootKey.PrivateKey)
	dirBatch.AddFeeInput(changeUTXO)
	dirBatch.SetChange(feeKey.PublicKey.Hash())
	dirBatch.SetFeeRate(1)
	dirResult, err := dirBatch.Build()
	require.NoError(t, err, "build dir tx batch")

	dirSignedHex, err := dirBatch.Sign(dirResult)
	require.NoError(t, err, "sign dir tx batch")

	dirTxIDStr, err := node.SendRawTransaction(ctx, dirSignedHex)
	require.NoError(t, err, "broadcast dir tx")
	t.Logf("dir txid: %s", dirTxIDStr)
	mineOneBlock(t)

	// Prepare dir's NodeUTXO for spending as parent edge.
	dirNodeUTXO := dirResult.NodeOps[0].NodeUTXO
	dirNodeUTXOScript, err := tx.BuildP2PKHScript(dirKey.PublicKey)
	require.NoError(t, err)
	dirNodeUTXO.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXO.PrivateKey = dirKey.PrivateKey

	// Prepare change from dir tx as next fee input.
	dirChangeUTXO := dirResult.ChangeUTXO
	require.NotNil(t, dirChangeUTXO, "dir tx should have change output")
	dirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChangeUTXO.ScriptPubKey = dirChangeScript
	dirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Create original file and copy under dir in a single batch.
	// ==================================================================
	// Both files share the same parent (dir), so they go in one batch
	// because AddCreateChild consumes the parent UTXO (deduped).
	originalContent := []byte("Hello BitFS! Original file content for copy test.")

	// Encrypt original.
	encResult, err := method42.Encrypt(originalContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt original (free)")
	require.NotEmpty(t, encResult.Ciphertext)
	require.NotEmpty(t, encResult.KeyHash)
	t.Logf("encrypted original: %d bytes -> %d bytes ciphertext", len(originalContent), len(encResult.Ciphertext))

	filePayload := make([]byte, 0, 32+len(encResult.Ciphertext))
	filePayload = append(filePayload, encResult.KeyHash...)
	filePayload = append(filePayload, encResult.Ciphertext...)

	// Encrypt copy (same plaintext, different key).
	copyEncResult, err := method42.Encrypt(originalContent, copyKey.PrivateKey, copyKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt copy (free)")
	require.NotEmpty(t, copyEncResult.Ciphertext)
	require.NotEmpty(t, copyEncResult.KeyHash)
	t.Logf("encrypted copy: %d bytes -> %d bytes ciphertext", len(originalContent), len(copyEncResult.Ciphertext))

	copyPayload := make([]byte, 0, 32+len(copyEncResult.Ciphertext))
	copyPayload = append(copyPayload, copyEncResult.KeyHash...)
	copyPayload = append(copyPayload, copyEncResult.Ciphertext...)

	// Build batch with both file creations.
	filesBatch := tx.NewMutationBatch()
	filesBatch.AddCreateChild(fileKey.PublicKey, dirResult.TxID, filePayload, dirNodeUTXO, dirKey.PrivateKey)
	filesBatch.AddCreateChild(copyKey.PublicKey, dirResult.TxID, copyPayload, dirNodeUTXO, dirKey.PrivateKey)
	filesBatch.AddFeeInput(dirChangeUTXO)
	filesBatch.SetChange(feeKey.PublicKey.Hash())
	filesBatch.SetFeeRate(1)
	filesResult, err := filesBatch.Build()
	require.NoError(t, err, "build original + copy batch")

	filesSignedHex, err := filesBatch.Sign(filesResult)
	require.NoError(t, err, "sign original + copy batch")

	filesTxIDStr, err := node.SendRawTransaction(ctx, filesSignedHex)
	require.NoError(t, err, "broadcast original + copy tx")
	t.Logf("original + copy txid: %s", filesTxIDStr)
	mineOneBlock(t)

	// original = NodeOps[0], copy = NodeOps[1].
	require.Len(t, filesResult.NodeOps, 2, "batch should have 2 node ops")

	// ==================================================================
	// Step 5: Verify -- different P_node, same TX, same decrypted plaintext.
	// ==================================================================
	t.Run("verify_copy_different_pnode_same_content", func(t *testing.T) {
		// Different P_node (public keys).
		assert.NotEqual(t, fileKey.PublicKey.Compressed(), copyKey.PublicKey.Compressed(),
			"original and copy should have different P_node keys")

		// Both are in the same TX but at different output indices.
		// original OP_RETURN = output 0, copy OP_RETURN = output 2.

		// Read the TX from chain.
		rawFiles, err := node.GetRawTransaction(ctx, filesTxIDStr)
		require.NoError(t, err, "get files tx from chain")

		parsedFiles, err := transaction.NewTransactionFromBytes(rawFiles)
		require.NoError(t, err, "parse files tx")

		// Verify copy's OP_RETURN (output 2).
		opReturnCopy := parsedFiles.Outputs[2]
		require.True(t, opReturnCopy.LockingScript.IsData(), "copy output 2 should be OP_RETURN")

		pushes := extractPushData(t, opReturnCopy.LockingScript)
		require.GreaterOrEqual(t, len(pushes), 4, "copy OP_RETURN should have >= 4 pushes")

		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse copy OP_RETURN data")

		// Verify copy's P_node is the copy key.
		assert.Equal(t, copyKey.PublicKey.Compressed(), pNode,
			"copy P_node should match copy key")

		// Verify copy's parent link points to the dir.
		assert.Equal(t, dirResult.TxID, parentTxID,
			"copy parentTxID should link to dir")

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

		t.Logf("copy verified: different P_node, same TX, same plaintext (%d bytes)", len(decResult.Plaintext))
	})

	// Also verify original is still readable.
	t.Run("verify_original_still_readable", func(t *testing.T) {
		rawFiles, err := node.GetRawTransaction(ctx, filesTxIDStr)
		require.NoError(t, err, "get files tx from chain")

		parsedFiles, err := transaction.NewTransactionFromBytes(rawFiles)
		require.NoError(t, err, "parse files tx")

		// Original OP_RETURN = output 0.
		opReturnOriginal := parsedFiles.Outputs[0]
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
	t.Logf("  -> Orig+Copy:   %s", filesTxIDStr)
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

	rootNodeUTXO := rootResult.NodeOps[0].NodeUTXO
	rootNodeUTXOScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	changeUTXO := rootResult.ChangeUTXO
	require.NotNil(t, changeUTXO)
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 3: Create dir under root.
	// ==================================================================
	dirPayload := []byte("bitfs directory: docs")
	dirBatch := tx.NewMutationBatch()
	dirBatch.AddCreateChild(dirKey.PublicKey, rootResult.TxID, dirPayload, rootNodeUTXO, rootKey.PrivateKey)
	dirBatch.AddFeeInput(changeUTXO)
	dirBatch.SetChange(feeKey.PublicKey.Hash())
	dirBatch.SetFeeRate(1)
	dirResult, err := dirBatch.Build()
	require.NoError(t, err, "build dir tx batch")

	dirSignedHex, err := dirBatch.Sign(dirResult)
	require.NoError(t, err, "sign dir tx batch")

	dirTxIDStr, err := node.SendRawTransaction(ctx, dirSignedHex)
	require.NoError(t, err, "broadcast dir tx")
	t.Logf("dir txid: %s", dirTxIDStr)
	mineOneBlock(t)

	dirNodeUTXO := dirResult.NodeOps[0].NodeUTXO
	dirNodeUTXOScript, err := tx.BuildP2PKHScript(dirKey.PublicKey)
	require.NoError(t, err)
	dirNodeUTXO.ScriptPubKey = dirNodeUTXOScript
	dirNodeUTXO.PrivateKey = dirKey.PrivateKey

	dirChangeUTXO := dirResult.ChangeUTXO
	require.NotNil(t, dirChangeUTXO)
	dirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChangeUTXO.ScriptPubKey = dirChangeScript
	dirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Create original file and copy under dir in a single batch.
	// ==================================================================
	originalContent := []byte("Original content before any updates.")

	encResult, err := method42.Encrypt(originalContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt original (free)")

	filePayload := make([]byte, 0, 32+len(encResult.Ciphertext))
	filePayload = append(filePayload, encResult.KeyHash...)
	filePayload = append(filePayload, encResult.Ciphertext...)

	copyEncResult, err := method42.Encrypt(originalContent, copyKey.PrivateKey, copyKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt copy (free)")

	copyPayload := make([]byte, 0, 32+len(copyEncResult.Ciphertext))
	copyPayload = append(copyPayload, copyEncResult.KeyHash...)
	copyPayload = append(copyPayload, copyEncResult.Ciphertext...)

	// Both files share the same parent (dir), so they go in one batch.
	filesBatch := tx.NewMutationBatch()
	filesBatch.AddCreateChild(fileKey.PublicKey, dirResult.TxID, filePayload, dirNodeUTXO, dirKey.PrivateKey)
	filesBatch.AddCreateChild(copyKey.PublicKey, dirResult.TxID, copyPayload, dirNodeUTXO, dirKey.PrivateKey)
	filesBatch.AddFeeInput(dirChangeUTXO)
	filesBatch.SetChange(feeKey.PublicKey.Hash())
	filesBatch.SetFeeRate(1)
	filesResult, err := filesBatch.Build()
	require.NoError(t, err, "build original + copy batch")

	filesSignedHex, err := filesBatch.Sign(filesResult)
	require.NoError(t, err, "sign original + copy batch")

	filesTxIDStr, err := node.SendRawTransaction(ctx, filesSignedHex)
	require.NoError(t, err, "broadcast original + copy tx")
	t.Logf("original + copy txid: %s", filesTxIDStr)
	mineOneBlock(t)

	// original = NodeOps[0], copy = NodeOps[1].
	require.Len(t, filesResult.NodeOps, 2, "batch should have 2 node ops")

	// Prepare original file's NodeUTXO for SelfUpdate later.
	fileNodeUTXO := filesResult.NodeOps[0].NodeUTXO
	fileNodeUTXOScript, err := tx.BuildP2PKHScript(fileKey.PublicKey)
	require.NoError(t, err)
	fileNodeUTXO.ScriptPubKey = fileNodeUTXOScript
	fileNodeUTXO.PrivateKey = fileKey.PrivateKey

	// Prepare change from files batch.
	filesChangeUTXO := filesResult.ChangeUTXO
	require.NotNil(t, filesChangeUTXO, "files batch should have change output")
	filesChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	filesChangeUTXO.ScriptPubKey = filesChangeScript
	filesChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 5: SelfUpdate original file with new content.
	// ==================================================================
	updatedContent := []byte("Updated content! The original file has been modified.")

	updatedEncResult, err := method42.Encrypt(updatedContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt updated content")

	updatedPayload := make([]byte, 0, 32+len(updatedEncResult.Ciphertext))
	updatedPayload = append(updatedPayload, updatedEncResult.KeyHash...)
	updatedPayload = append(updatedPayload, updatedEncResult.Ciphertext...)

	updateBatch := tx.NewMutationBatch()
	updateBatch.AddSelfUpdate(fileKey.PublicKey, dirResult.TxID, updatedPayload, fileNodeUTXO, fileKey.PrivateKey)
	updateBatch.AddFeeInput(filesChangeUTXO)
	updateBatch.SetChange(feeKey.PublicKey.Hash())
	updateBatch.SetFeeRate(1)
	updateResult, err := updateBatch.Build()
	require.NoError(t, err, "build self-update batch")

	updateSignedHex, err := updateBatch.Sign(updateResult)
	require.NoError(t, err, "sign self-update batch")

	updateTxIDStr, err := node.SendRawTransaction(ctx, updateSignedHex)
	require.NoError(t, err, "broadcast self-update tx")
	t.Logf("self-update txid: %s", updateTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 6: Verify copy still has original content (independence).
	// ==================================================================
	t.Run("copy_retains_original_content", func(t *testing.T) {
		// Copy is in the files batch TX at output index 2 (OP_RETURN).
		rawFiles, err := node.GetRawTransaction(ctx, filesTxIDStr)
		require.NoError(t, err, "get files tx from chain")

		parsedFiles, err := transaction.NewTransactionFromBytes(rawFiles)
		require.NoError(t, err, "parse files tx")

		opReturnCopy := parsedFiles.Outputs[2]
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
	// Step 7: Verify original now has updated content.
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
		assert.Equal(t, dirResult.TxID, parentTxID,
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
	t.Logf("  -> Orig+Copy:    %s (batch)", filesTxIDStr)
	t.Logf("  -> Update:       %s (original self-updated)", updateTxIDStr)
	t.Logf("Copy independence verified: updating original does not affect copy")
}

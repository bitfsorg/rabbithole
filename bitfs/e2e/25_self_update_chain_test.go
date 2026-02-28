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

// TestMultipleUpdates creates a root -> dir -> file chain, then performs 3
// sequential SelfUpdate transactions on the file node with different content
// ("v1", "v2", "v3"). It retrieves all 4 versions from the chain (original +
// 3 updates), decrypts each, and verifies the correct content per version.
//
// Version chain: file_tx (v0) -> update1_tx (v1) -> update2_tx (v2) -> update3_tx (v3)
func TestMultipleUpdates(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Setup -- create wallet, derive keys, fund fee address.
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

	t.Logf("fee key:  %s", feeKey.Path)
	t.Logf("root key: %s", rootKey.Path)
	t.Logf("dir key:  %s", dirKey.Path)
	t.Logf("file key: %s", fileKey.Path)

	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)

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
	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, []byte("self-update chain test root"))
	rootBatch.AddFeeInput(feeUTXO)
	rootBatch.SetChange(feeKey.PublicKey.Hash())
	rootBatch.SetFeeRate(1)
	rootResult, err := rootBatch.Build()
	require.NoError(t, err, "build root tx")

	rootSignedHex, err := rootBatch.Sign(rootResult)
	require.NoError(t, err, "sign root tx")

	rootTxIDStr, err := node.SendRawTransaction(ctx, rootSignedHex)
	require.NoError(t, err, "broadcast root tx")
	t.Logf("root txid: %s", rootTxIDStr)
	mineOneBlock(t)

	rootNodeUTXO := rootResult.NodeOps[0].NodeUTXO
	rootNodeScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	changeUTXO := rootResult.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have change output")
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 3: Create child directory.
	// ==================================================================
	dirBatch := tx.NewMutationBatch()
	dirBatch.AddCreateChild(dirKey.PublicKey, rootResult.TxID, []byte("self-update chain test dir"), rootNodeUTXO, rootKey.PrivateKey)
	dirBatch.AddFeeInput(changeUTXO)
	dirBatch.SetChange(feeKey.PublicKey.Hash())
	dirBatch.SetFeeRate(1)
	dirResult, err := dirBatch.Build()
	require.NoError(t, err, "build dir tx")

	dirSignedHex, err := dirBatch.Sign(dirResult)
	require.NoError(t, err, "sign dir tx")

	dirTxIDStr, err := node.SendRawTransaction(ctx, dirSignedHex)
	require.NoError(t, err, "broadcast dir tx")
	t.Logf("dir txid: %s", dirTxIDStr)
	mineOneBlock(t)

	dirNodeUTXO := dirResult.NodeOps[0].NodeUTXO
	dirNodeScript, err := tx.BuildP2PKHScript(dirKey.PublicKey)
	require.NoError(t, err)
	dirNodeUTXO.ScriptPubKey = dirNodeScript
	dirNodeUTXO.PrivateKey = dirKey.PrivateKey

	dirChangeUTXO := dirResult.ChangeUTXO
	require.NotNil(t, dirChangeUTXO, "dir tx should have change output")
	dirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChangeUTXO.ScriptPubKey = dirChangeScript
	dirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Create file (v0) with encrypted content.
	// ==================================================================
	v0Content := []byte("version 0: original file content")

	v0Enc, err := method42.Encrypt(v0Content, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "encrypt v0")

	v0Payload := make([]byte, 0, 32+len(v0Enc.Ciphertext))
	v0Payload = append(v0Payload, v0Enc.KeyHash...)
	v0Payload = append(v0Payload, v0Enc.Ciphertext...)

	fileBatch := tx.NewMutationBatch()
	fileBatch.AddCreateChild(fileKey.PublicKey, dirResult.TxID, v0Payload, dirNodeUTXO, dirKey.PrivateKey)
	fileBatch.AddFeeInput(dirChangeUTXO)
	fileBatch.SetChange(feeKey.PublicKey.Hash())
	fileBatch.SetFeeRate(1)
	fileResult, err := fileBatch.Build()
	require.NoError(t, err, "build file tx")

	fileSignedHex, err := fileBatch.Sign(fileResult)
	require.NoError(t, err, "sign file tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast file tx")
	t.Logf("file txid (v0): %s", fileTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 5: Perform 3 sequential SelfUpdates (v1, v2, v3).
	// ==================================================================
	versions := []struct {
		label   string
		content []byte
	}{
		{"v1", []byte("version 1: first update")},
		{"v2", []byte("version 2: second update")},
		{"v3", []byte("version 3: third and final update")},
	}

	// Track all txids for later verification. Start with v0.
	allTxIDs := []string{fileTxIDStr}
	allContents := [][]byte{v0Content}

	// Current node and fee UTXOs start from the file creation tx.
	curNodeUTXO := fileResult.NodeOps[0].NodeUTXO
	fileNodeScript, err := tx.BuildP2PKHScript(fileKey.PublicKey)
	require.NoError(t, err)
	curNodeUTXO.ScriptPubKey = fileNodeScript
	curNodeUTXO.PrivateKey = fileKey.PrivateKey

	curFeeUTXO := fileResult.ChangeUTXO
	require.NotNil(t, curFeeUTXO, "file tx should have change output")
	feeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	curFeeUTXO.ScriptPubKey = feeScript
	curFeeUTXO.PrivateKey = feeKey.PrivateKey

	for _, ver := range versions {
		t.Run("self_update_"+ver.label, func(t *testing.T) {
			// Encrypt the new version content.
			enc, err := method42.Encrypt(ver.content, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
			require.NoError(t, err, "encrypt %s", ver.label)

			payload := make([]byte, 0, 32+len(enc.Ciphertext))
			payload = append(payload, enc.KeyHash...)
			payload = append(payload, enc.Ciphertext...)

			// Build self-update tx.
			updateBatch := tx.NewMutationBatch()
			updateBatch.AddSelfUpdate(fileKey.PublicKey, dirResult.TxID, payload, curNodeUTXO, fileKey.PrivateKey)
			updateBatch.AddFeeInput(curFeeUTXO)
			updateBatch.SetChange(feeKey.PublicKey.Hash())
			updateBatch.SetFeeRate(1)
			updateResult, err := updateBatch.Build()
			require.NoError(t, err, "build self-update tx %s", ver.label)

			signedHex, err := updateBatch.Sign(updateResult)
			require.NoError(t, err, "sign self-update tx %s", ver.label)

			txIDStr, err := node.SendRawTransaction(ctx, signedHex)
			require.NoError(t, err, "broadcast self-update tx %s", ver.label)
			t.Logf("%s txid: %s", ver.label, txIDStr)
			mineOneBlock(t)

			allTxIDs = append(allTxIDs, txIDStr)
			allContents = append(allContents, ver.content)

			// Advance the UTXO chain for the next iteration.
			curNodeUTXO = updateResult.NodeOps[0].NodeUTXO
			curNodeUTXO.ScriptPubKey = fileNodeScript
			curNodeUTXO.PrivateKey = fileKey.PrivateKey

			curFeeUTXO = updateResult.ChangeUTXO
			require.NotNil(t, curFeeUTXO, "%s tx should have change output", ver.label)
			curFeeUTXO.ScriptPubKey = feeScript
			curFeeUTXO.PrivateKey = feeKey.PrivateKey
		})
	}

	// ==================================================================
	// Step 6: Verify all 4 versions from the chain.
	// ==================================================================
	freeKey := method42.FreePrivateKey()
	versionLabels := []string{"v0", "v1", "v2", "v3"}

	for i, txIDStr := range allTxIDs {
		label := versionLabels[i]
		expectedContent := allContents[i]

		t.Run("verify_"+label, func(t *testing.T) {
			rawBytes, err := node.GetRawTransaction(ctx, txIDStr)
			require.NoError(t, err, "get %s tx from chain", label)

			parsedTx, err := transaction.NewTransactionFromBytes(rawBytes)
			require.NoError(t, err, "parse %s tx", label)

			opReturnOutput := parsedTx.Outputs[0]
			require.True(t, opReturnOutput.LockingScript.IsData(),
				"%s output 0 should be OP_RETURN", label)

			pushes := extractPushData(t, opReturnOutput.LockingScript)
			require.GreaterOrEqual(t, len(pushes), 4,
				"%s OP_RETURN should have >= 4 pushes", label)

			pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
			require.NoError(t, err, "parse %s OP_RETURN data", label)

			// All versions should have the same P_node (file key).
			assert.Equal(t, fileKey.PublicKey.Compressed(), pNode,
				"%s P_node should match file key", label)

			// All versions should link to the same parent (dir tx).
			assert.Equal(t, dirResult.TxID, parentTxID,
				"%s parentTxID should link to dir tx", label)

			// Decrypt and verify content.
			require.True(t, len(payload) > 32,
				"%s payload should contain keyHash + ciphertext", label)
			keyHash := payload[:32]
			ciphertext := payload[32:]

			dec, err := method42.Decrypt(
				ciphertext,
				freeKey,
				fileKey.PublicKey,
				keyHash,
				method42.AccessFree,
			)
			require.NoError(t, err, "decrypt %s content", label)
			assert.Equal(t, expectedContent, dec.Plaintext,
				"%s decrypted content should match expected", label)

			t.Logf("%s verified: txid=%s, content=%q", label, txIDStr, string(dec.Plaintext))
		})
	}

	// ==================================================================
	// Summary.
	// ==================================================================
	t.Logf("--- Self-Update Version Chain Summary ---")
	for i, txIDStr := range allTxIDs {
		t.Logf("  %s: %s", versionLabels[i], txIDStr)
	}
	t.Logf("All 4 versions created, retrieved, decrypted, and verified successfully")
}

// TestUpdatePreservesIdentity verifies that a SelfUpdate transaction preserves
// the node's identity: P_node and parentTxID remain unchanged across updates.
// Only the TxID and payload change.
func TestUpdatePreservesIdentity(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Setup -- create wallet, derive keys, fund fee address.
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

	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)

	mineAddr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate mining address")
	mineOneBlock := func(t *testing.T) {
		t.Helper()
		_, err := node.MineBlocks(ctx, 1, mineAddr)
		require.NoError(t, err, "mine confirmation block")
	}

	// ==================================================================
	// Step 2: Create root -> dir -> file chain.
	// ==================================================================

	// Root.
	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, []byte("identity test root"))
	rootBatch.AddFeeInput(feeUTXO)
	rootBatch.SetChange(feeKey.PublicKey.Hash())
	rootBatch.SetFeeRate(1)
	rootResult, err := rootBatch.Build()
	require.NoError(t, err, "build root tx")

	rootSignedHex, err := rootBatch.Sign(rootResult)
	require.NoError(t, err, "sign root tx")

	_, err = node.SendRawTransaction(ctx, rootSignedHex)
	require.NoError(t, err, "broadcast root tx")
	mineOneBlock(t)

	rootNodeUTXO := rootResult.NodeOps[0].NodeUTXO
	rootNodeScript, err := tx.BuildP2PKHScript(rootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeScript
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	rootChangeUTXO := rootResult.ChangeUTXO
	require.NotNil(t, rootChangeUTXO)
	rootChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	rootChangeUTXO.ScriptPubKey = rootChangeScript
	rootChangeUTXO.PrivateKey = feeKey.PrivateKey

	// Dir.
	dirBatch := tx.NewMutationBatch()
	dirBatch.AddCreateChild(dirKey.PublicKey, rootResult.TxID, []byte("identity test dir"), rootNodeUTXO, rootKey.PrivateKey)
	dirBatch.AddFeeInput(rootChangeUTXO)
	dirBatch.SetChange(feeKey.PublicKey.Hash())
	dirBatch.SetFeeRate(1)
	dirResult, err := dirBatch.Build()
	require.NoError(t, err, "build dir tx")

	dirSignedHex, err := dirBatch.Sign(dirResult)
	require.NoError(t, err, "sign dir tx")

	_, err = node.SendRawTransaction(ctx, dirSignedHex)
	require.NoError(t, err, "broadcast dir tx")
	mineOneBlock(t)

	dirNodeUTXO := dirResult.NodeOps[0].NodeUTXO
	dirNodeScript, err := tx.BuildP2PKHScript(dirKey.PublicKey)
	require.NoError(t, err)
	dirNodeUTXO.ScriptPubKey = dirNodeScript
	dirNodeUTXO.PrivateKey = dirKey.PrivateKey

	dirChangeUTXO := dirResult.ChangeUTXO
	require.NotNil(t, dirChangeUTXO)
	dirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	dirChangeUTXO.ScriptPubKey = dirChangeScript
	dirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// File (original version).
	originalContent := []byte("original content before self-update")
	origEnc, err := method42.Encrypt(originalContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "encrypt original content")

	origPayload := make([]byte, 0, 32+len(origEnc.Ciphertext))
	origPayload = append(origPayload, origEnc.KeyHash...)
	origPayload = append(origPayload, origEnc.Ciphertext...)

	fileBatch := tx.NewMutationBatch()
	fileBatch.AddCreateChild(fileKey.PublicKey, dirResult.TxID, origPayload, dirNodeUTXO, dirKey.PrivateKey)
	fileBatch.AddFeeInput(dirChangeUTXO)
	fileBatch.SetChange(feeKey.PublicKey.Hash())
	fileBatch.SetFeeRate(1)
	fileResult, err := fileBatch.Build()
	require.NoError(t, err, "build file tx")

	fileSignedHex, err := fileBatch.Sign(fileResult)
	require.NoError(t, err, "sign file tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast file tx")
	t.Logf("original file txid: %s", fileTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 3: Parse original version from chain to capture identity.
	// ==================================================================
	origRawBytes, err := node.GetRawTransaction(ctx, fileTxIDStr)
	require.NoError(t, err, "get original file tx from chain")

	origParsedTx, err := transaction.NewTransactionFromBytes(origRawBytes)
	require.NoError(t, err, "parse original file tx")

	origPushes := extractPushData(t, origParsedTx.Outputs[0].LockingScript)
	origPNode, origParentTxID, origPayloadFromChain, err := tx.ParseOPReturnData(origPushes)
	require.NoError(t, err, "parse original OP_RETURN data")

	t.Logf("original P_node:      %x", origPNode)
	t.Logf("original parentTxID:  %x", origParentTxID)

	// ==================================================================
	// Step 4: Perform SelfUpdate with new content.
	// ==================================================================
	updatedContent := []byte("updated content after self-update")
	updEnc, err := method42.Encrypt(updatedContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "encrypt updated content")

	updPayload := make([]byte, 0, 32+len(updEnc.Ciphertext))
	updPayload = append(updPayload, updEnc.KeyHash...)
	updPayload = append(updPayload, updEnc.Ciphertext...)

	fileNodeUTXO := fileResult.NodeOps[0].NodeUTXO
	fileNodeScript, err := tx.BuildP2PKHScript(fileKey.PublicKey)
	require.NoError(t, err)
	fileNodeUTXO.ScriptPubKey = fileNodeScript
	fileNodeUTXO.PrivateKey = fileKey.PrivateKey

	fileChangeUTXO := fileResult.ChangeUTXO
	require.NotNil(t, fileChangeUTXO, "file tx should have change output")
	fileFeeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	fileChangeUTXO.ScriptPubKey = fileFeeScript
	fileChangeUTXO.PrivateKey = feeKey.PrivateKey

	updateBatch := tx.NewMutationBatch()
	updateBatch.AddSelfUpdate(fileKey.PublicKey, dirResult.TxID, updPayload, fileNodeUTXO, fileKey.PrivateKey)
	updateBatch.AddFeeInput(fileChangeUTXO)
	updateBatch.SetChange(feeKey.PublicKey.Hash())
	updateBatch.SetFeeRate(1)
	updateResult, err := updateBatch.Build()
	require.NoError(t, err, "build self-update tx")

	updateSignedHex, err := updateBatch.Sign(updateResult)
	require.NoError(t, err, "sign self-update tx")

	updateTxIDStr, err := node.SendRawTransaction(ctx, updateSignedHex)
	require.NoError(t, err, "broadcast self-update tx")
	t.Logf("self-update txid: %s", updateTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 5: Parse updated version from chain and verify identity.
	// ==================================================================
	updRawBytes, err := node.GetRawTransaction(ctx, updateTxIDStr)
	require.NoError(t, err, "get self-update tx from chain")

	updParsedTx, err := transaction.NewTransactionFromBytes(updRawBytes)
	require.NoError(t, err, "parse self-update tx")

	updPushes := extractPushData(t, updParsedTx.Outputs[0].LockingScript)
	updPNode, updParentTxID, updPayloadFromChain, err := tx.ParseOPReturnData(updPushes)
	require.NoError(t, err, "parse updated OP_RETURN data")

	t.Logf("updated P_node:      %x", updPNode)
	t.Logf("updated parentTxID:  %x", updParentTxID)

	// ------------------------------------------------------------------
	// Verify: P_node is unchanged.
	// ------------------------------------------------------------------
	assert.Equal(t, origPNode, updPNode,
		"P_node should be unchanged after SelfUpdate")

	// ------------------------------------------------------------------
	// Verify: parentTxID is unchanged.
	// ------------------------------------------------------------------
	assert.Equal(t, origParentTxID, updParentTxID,
		"parentTxID should be unchanged after SelfUpdate")

	// ------------------------------------------------------------------
	// Verify: TxID has changed (the update is a new transaction).
	// ------------------------------------------------------------------
	assert.NotEqual(t, fileTxIDStr, updateTxIDStr,
		"TxID should change after SelfUpdate")

	// ------------------------------------------------------------------
	// Verify: Payload has changed (new encrypted content).
	// ------------------------------------------------------------------
	assert.NotEqual(t, origPayloadFromChain, updPayloadFromChain,
		"payload should change after SelfUpdate")

	// ------------------------------------------------------------------
	// Verify: Both payloads decrypt to the correct content.
	// ------------------------------------------------------------------
	freeKey := method42.FreePrivateKey()

	origKeyHash := origPayloadFromChain[:32]
	origCiphertext := origPayloadFromChain[32:]
	origDec, err := method42.Decrypt(origCiphertext, freeKey, fileKey.PublicKey, origKeyHash, method42.AccessFree)
	require.NoError(t, err, "decrypt original content")
	assert.Equal(t, originalContent, origDec.Plaintext,
		"original version should decrypt to original content")

	updKeyHash := updPayloadFromChain[:32]
	updCiphertext := updPayloadFromChain[32:]
	updDec, err := method42.Decrypt(updCiphertext, freeKey, fileKey.PublicKey, updKeyHash, method42.AccessFree)
	require.NoError(t, err, "decrypt updated content")
	assert.Equal(t, updatedContent, updDec.Plaintext,
		"updated version should decrypt to updated content")

	// ------------------------------------------------------------------
	// Verify: SelfUpdate tx structure is correct.
	// ------------------------------------------------------------------
	assert.Equal(t, 2, updParsedTx.InputCount(),
		"self-update tx should have 2 inputs (nodeUTXO + feeUTXO)")
	assert.GreaterOrEqual(t, updParsedTx.OutputCount(), 2,
		"self-update tx should have >= 2 outputs (OP_RETURN + P_node refresh)")
	assert.Equal(t, tx.DustLimit, updParsedTx.Outputs[1].Satoshis,
		"output 1 should be P_node dust refresh (%d sat)", tx.DustLimit)

	t.Logf("--- Update Preserves Identity Summary ---")
	t.Logf("P_node unchanged:      %x", updPNode[:8])
	t.Logf("parentTxID unchanged:  %x", updParentTxID[:8])
	t.Logf("TxID changed:          %s -> %s", fileTxIDStr[:16], updateTxIDStr[:16])
	t.Logf("Original content:      %q", string(origDec.Plaintext))
	t.Logf("Updated content:       %q", string(updDec.Plaintext))
	t.Logf("Identity preserved across SelfUpdate: verified")
}

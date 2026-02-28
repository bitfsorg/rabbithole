//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestFullLifecycle is a comprehensive smoke test that validates the full BitFS
// workflow: create wallet -> create root dir -> mkdir "docs" -> put file (Free) ->
// read back -> self-update -> read updated -> change to Paid -> purchase flow ->
// delete (new dir version) -> verify final DAG state.
func TestFullLifecycle(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Setup -- Create wallet, derive keys, fund fee address.
	// ==================================================================
	t.Run("setup", func(t *testing.T) {})

	w := setupFundedWallet(t, ctx, node)

	// Fee key: m/44'/236'/0'/0/0
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	// Root key: m/44'/236'/1'/0/0
	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")

	// Child dir key: m/44'/236'/1'/0/0/0'
	childDirKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive child dir node key")

	// File key: m/44'/236'/1'/0/0/0'/0'
	fileKey, err := w.DeriveNodeKey(0, []uint32{0, 0}, nil)
	require.NoError(t, err, "derive file node key")

	t.Logf("fee key:       %s", feeKey.Path)
	t.Logf("root key:      %s", rootKey.Path)
	t.Logf("child dir key: %s", childDirKey.Path)
	t.Logf("file key:      %s", fileKey.Path)

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
	rootPayload := []byte("bitfs lifecycle root directory")
	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, rootPayload)
	rootBatch.AddFeeInput(feeUTXO)
	rootBatch.SetChange(feeKey.PublicKey.Hash())
	rootBatch.SetFeeRate(1)
	rootResult, err := rootBatch.Build()
	require.NoError(t, err, "build unsigned root tx")

	rootSignedHex, err := rootBatch.Sign(rootResult)
	require.NoError(t, err, "sign root tx")

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
	// Step 3: Mkdir "docs" -- child directory under root.
	// ==================================================================
	childDirPayload := []byte("bitfs directory: docs")
	childDirBatch := tx.NewMutationBatch()
	childDirBatch.AddCreateChild(childDirKey.PublicKey, rootResult.TxID, childDirPayload, rootNodeUTXO, rootKey.PrivateKey)
	childDirBatch.AddFeeInput(changeUTXO)
	childDirBatch.SetChange(feeKey.PublicKey.Hash())
	childDirBatch.SetFeeRate(1)
	childDirResult, err := childDirBatch.Build()
	require.NoError(t, err, "build unsigned child dir tx")

	childDirSignedHex, err := childDirBatch.Sign(childDirResult)
	require.NoError(t, err, "sign child dir tx")

	childDirTxIDStr, err := node.SendRawTransaction(ctx, childDirSignedHex)
	require.NoError(t, err, "broadcast child dir tx")
	t.Logf("child dir txid: %s", childDirTxIDStr)
	mineOneBlock(t)

	// Prepare child dir's NodeUTXO for spending as parent edge in step 4.
	childDirNodeUTXO := childDirResult.NodeOps[0].NodeUTXO
	childDirNodeUTXOScript, err := tx.BuildP2PKHScript(childDirKey.PublicKey)
	require.NoError(t, err)
	childDirNodeUTXO.ScriptPubKey = childDirNodeUTXOScript
	childDirNodeUTXO.PrivateKey = childDirKey.PrivateKey

	// Prepare change from child dir tx as next fee input.
	childDirChangeUTXO := childDirResult.ChangeUTXO
	require.NotNil(t, childDirChangeUTXO, "child dir tx should have change output")
	childDirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	childDirChangeUTXO.ScriptPubKey = childDirChangeScript
	childDirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 4: Put "docs/hello.txt" (Free) -- encrypt and build file node tx.
	// ==================================================================
	originalContent := []byte("Hello BitFS! This is an encrypted document stored on-chain.")

	encResult, err := method42.Encrypt(originalContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt (free)")
	require.NotEmpty(t, encResult.Ciphertext)
	require.NotEmpty(t, encResult.KeyHash)
	t.Logf("encrypted %d bytes -> %d bytes ciphertext, keyHash=%x",
		len(originalContent), len(encResult.Ciphertext), encResult.KeyHash[:8])

	// Build file payload: keyHash(32B) + ciphertext.
	filePayload := make([]byte, 0, 32+len(encResult.Ciphertext))
	filePayload = append(filePayload, encResult.KeyHash...)
	filePayload = append(filePayload, encResult.Ciphertext...)

	fileBatch := tx.NewMutationBatch()
	fileBatch.AddCreateChild(fileKey.PublicKey, childDirResult.TxID, filePayload, childDirNodeUTXO, childDirKey.PrivateKey)
	// Also refresh the docs dir UTXO (parent) so step 10 can update it.
	// The old API produced a parent refresh automatically; the batch API
	// requires an explicit SelfUpdate op sharing the same parent input (deduped).
	fileBatch.AddSelfUpdate(childDirKey.PublicKey, rootResult.TxID, childDirPayload, childDirNodeUTXO, childDirKey.PrivateKey)
	fileBatch.AddFeeInput(childDirChangeUTXO)
	fileBatch.SetChange(feeKey.PublicKey.Hash())
	fileBatch.SetFeeRate(1)
	fileResult, err := fileBatch.Build()
	require.NoError(t, err, "build unsigned file tx")

	fileSignedHex, err := fileBatch.Sign(fileResult)
	require.NoError(t, err, "sign file tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast file tx")
	t.Logf("file txid: %s", fileTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 5: Read back -- retrieve from chain, parse OP_RETURN, decrypt.
	// ==================================================================
	t.Run("read_back_free_content", func(t *testing.T) {
		rawBytes, err := node.GetRawTransaction(ctx, fileTxIDStr)
		require.NoError(t, err, "get file tx from chain")

		parsedTx, err := transaction.NewTransactionFromBytes(rawBytes)
		require.NoError(t, err, "parse file tx")

		opReturnOutput := parsedTx.Outputs[0]
		require.True(t, opReturnOutput.LockingScript.IsData(), "output 0 should be OP_RETURN")

		pushes := extractPushData(t, opReturnOutput.LockingScript)
		require.GreaterOrEqual(t, len(pushes), 4, "OP_RETURN should have >= 4 pushes")

		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse OP_RETURN data")

		// Verify node pubkey.
		assert.Equal(t, fileKey.PublicKey.Compressed(), pNode, "P_node should match file key")
		// Verify parent link.
		assert.Equal(t, childDirResult.TxID, parentTxID, "parentTxID should link to docs dir")

		// Decrypt.
		require.True(t, len(payload) > 32, "payload should contain keyHash + ciphertext")
		extractedKeyHash := payload[:32]
		extractedCiphertext := payload[32:]

		decResult, err := method42.Decrypt(
			extractedCiphertext,
			fileKey.PrivateKey,
			fileKey.PublicKey,
			extractedKeyHash,
			method42.AccessFree,
		)
		require.NoError(t, err, "decrypt file content")
		assert.Equal(t, originalContent, decResult.Plaintext,
			"decrypted content should match original plaintext")
		t.Logf("read-back verified: %d bytes decrypted correctly", len(decResult.Plaintext))
	})

	// ==================================================================
	// Step 6: SelfUpdate -- update file content with new payload.
	// ==================================================================
	updatedContent := []byte("Hello BitFS v2! This file has been updated via SelfUpdate tx.")

	updatedEncResult, err := method42.Encrypt(updatedContent, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt updated content")

	updatedPayload := make([]byte, 0, 32+len(updatedEncResult.Ciphertext))
	updatedPayload = append(updatedPayload, updatedEncResult.KeyHash...)
	updatedPayload = append(updatedPayload, updatedEncResult.Ciphertext...)

	// Prepare file node UTXO for self-update (NodeOps[0] from file tx).
	fileNodeUTXO := fileResult.NodeOps[0].NodeUTXO
	fileNodeUTXOScript, err := tx.BuildP2PKHScript(fileKey.PublicKey)
	require.NoError(t, err)
	fileNodeUTXO.ScriptPubKey = fileNodeUTXOScript
	fileNodeUTXO.PrivateKey = fileKey.PrivateKey

	// Prepare change from file tx as fee input.
	fileChangeUTXO := fileResult.ChangeUTXO
	require.NotNil(t, fileChangeUTXO, "file tx should have change output")
	fileChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err)
	fileChangeUTXO.ScriptPubKey = fileChangeScript
	fileChangeUTXO.PrivateKey = feeKey.PrivateKey

	// Build self-update tx: spends file's NodeUTXO, preserves parentTxID.
	updateBatch := tx.NewMutationBatch()
	updateBatch.AddSelfUpdate(fileKey.PublicKey, childDirResult.TxID, updatedPayload, fileNodeUTXO, fileKey.PrivateKey)
	updateBatch.AddFeeInput(fileChangeUTXO)
	updateBatch.SetChange(feeKey.PublicKey.Hash())
	updateBatch.SetFeeRate(1)
	updateResult, err := updateBatch.Build()
	require.NoError(t, err, "build unsigned self-update tx")
	require.NotEmpty(t, updateResult.RawTx)

	updateSignedHex, err := updateBatch.Sign(updateResult)
	require.NoError(t, err, "sign self-update tx")

	updateTxIDStr, err := node.SendRawTransaction(ctx, updateSignedHex)
	require.NoError(t, err, "broadcast self-update tx")
	t.Logf("self-update txid: %s", updateTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 7: Read updated version -- verify new content.
	// ==================================================================
	t.Run("read_updated_version", func(t *testing.T) {
		rawBytes, err := node.GetRawTransaction(ctx, updateTxIDStr)
		require.NoError(t, err, "get update tx from chain")

		parsedTx, err := transaction.NewTransactionFromBytes(rawBytes)
		require.NoError(t, err, "parse update tx")

		opReturnOutput := parsedTx.Outputs[0]
		require.True(t, opReturnOutput.LockingScript.IsData(), "output 0 should be OP_RETURN")

		pushes := extractPushData(t, opReturnOutput.LockingScript)
		require.GreaterOrEqual(t, len(pushes), 4)

		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse OP_RETURN data")

		// SelfUpdate preserves P_node and parentTxID.
		assert.Equal(t, fileKey.PublicKey.Compressed(), pNode,
			"updated P_node should still be file key")
		assert.Equal(t, childDirResult.TxID, parentTxID,
			"updated parentTxID should still link to docs dir")

		// Verify the update tx has correct structure:
		// 2 inputs, at least 2 outputs (OP_RETURN + P_node refresh).
		assert.Equal(t, 2, parsedTx.InputCount(), "self-update tx should have 2 inputs")
		assert.GreaterOrEqual(t, parsedTx.OutputCount(), 2,
			"self-update tx should have >= 2 outputs")
		assert.Equal(t, tx.DustLimit, parsedTx.Outputs[1].Satoshis,
			"output 1 should be P_node dust refresh")

		// Decrypt updated content.
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
			"decrypted updated content should match")
		t.Logf("updated content verified: %d bytes decrypted correctly", len(decResult.Plaintext))
	})

	// ==================================================================
	// Step 8: Change to Paid -- re-encrypt with AccessPaid mode.
	// ==================================================================
	t.Run("change_to_paid", func(t *testing.T) {
		paidEncResult, err := method42.Encrypt(
			updatedContent,
			fileKey.PrivateKey,
			fileKey.PublicKey,
			method42.AccessPaid,
		)
		require.NoError(t, err, "re-encrypt with AccessPaid")
		require.NotEmpty(t, paidEncResult.Ciphertext)
		require.NotEmpty(t, paidEncResult.KeyHash)

		// Verify the owner can still decrypt.
		ownerDec, err := method42.Decrypt(
			paidEncResult.Ciphertext,
			fileKey.PrivateKey,
			fileKey.PublicKey,
			paidEncResult.KeyHash,
			method42.AccessPaid,
		)
		require.NoError(t, err, "owner decrypt AccessPaid")
		assert.Equal(t, updatedContent, ownerDec.Plaintext)

		// Verify that AccessFree decryption fails on paid-encrypted content.
		// Free mode uses scalar-1 key, which will produce a different ECDH result
		// than the real private key used in Paid mode.
		freeKey := method42.FreePrivateKey()
		_, err = method42.Decrypt(
			paidEncResult.Ciphertext,
			freeKey,
			fileKey.PublicKey,
			paidEncResult.KeyHash,
			method42.AccessFree,
		)
		assert.Error(t, err, "free-mode decrypt of paid content should fail")
		t.Logf("paid mode encryption verified: owner decrypts OK, free-mode correctly rejected")
	})

	// ==================================================================
	// Step 9: Purchase flow -- buyer computes capsule, decrypts.
	// ==================================================================
	t.Run("purchase_flow", func(t *testing.T) {
		// Create a buyer key pair.
		buyerPrivKey, err := ec.NewPrivateKey()
		require.NoError(t, err, "generate buyer private key")
		buyerPubKey := buyerPrivKey.PubKey()

		// Seller encrypts content with their own key pair (AccessPaid uses ECDH(D_file, P_file)).
		buyerEncResult, err := method42.Encrypt(
			updatedContent,
			fileKey.PrivateKey,
			fileKey.PublicKey,
			method42.AccessPaid,
		)
		require.NoError(t, err, "encrypt for buyer")

		// Seller computes buyer-specific XOR capsule:
		//   capsule = AES_key XOR BuyerMask
		sellerCapsule, err := method42.ComputeCapsule(fileKey.PrivateKey, fileKey.PublicKey, buyerPubKey, buyerEncResult.KeyHash)
		require.NoError(t, err, "seller compute capsule")
		require.Len(t, sellerCapsule, 32)

		fileTxID := bytes.Repeat([]byte{0xf0}, 32) // mock file txid for e2e test
		capsuleHash := method42.ComputeCapsuleHash(fileTxID, sellerCapsule)
		expectedHasher := sha256.New()
		expectedHasher.Write(fileTxID)
		expectedHasher.Write(sellerCapsule)
		assert.Equal(t, expectedHasher.Sum(nil), capsuleHash, "capsule hash = SHA256(fileTxID || capsule)")

		// Buyer receives the capsule (via HTLC reveal) and decrypts.
		// DecryptWithCapsule uses ECDH(D_buyer, P_file) to derive the buyer mask,
		// then recovers AES_key = capsule XOR buyerMask.
		decResult, err := method42.DecryptWithCapsule(
			buyerEncResult.Ciphertext,
			sellerCapsule,
			buyerEncResult.KeyHash,
			buyerPrivKey,
			fileKey.PublicKey,
		)
		require.NoError(t, err, "buyer decrypt with capsule")
		assert.Equal(t, updatedContent, decResult.Plaintext,
			"buyer decrypted content should match")
		t.Logf("purchase flow verified: XOR capsule decrypt OK, buyer decrypted %d bytes",
			len(decResult.Plaintext))
	})

	// ==================================================================
	// Step 10: Delete -- build a new directory version without the file child.
	// ==================================================================
	t.Run("delete_file_entry", func(t *testing.T) {
		// A "delete" in Metanet DAG is modeled by publishing a new version of
		// the parent directory that no longer includes the deleted child entry.
		// We simulate this by building a SelfUpdate on the docs directory with
		// a payload that omits the file reference.

		// Prepare docs dir's refreshed NodeUTXO (NodeOps[1] from file tx,
		// produced by the SelfUpdate op we added for parent refresh).
		docsRefreshUTXO := fileResult.NodeOps[1].NodeUTXO
		require.NotNil(t, docsRefreshUTXO, "file tx should have refreshed docs dir UTXO")
		docsRefreshUTXO.ScriptPubKey = childDirNodeUTXOScript
		docsRefreshUTXO.PrivateKey = childDirKey.PrivateKey

		// Prepare change from update tx as fee input.
		updateChangeUTXO := updateResult.ChangeUTXO
		require.NotNil(t, updateChangeUTXO, "update tx should have change output")
		updateChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
		require.NoError(t, err)
		updateChangeUTXO.ScriptPubKey = updateChangeScript
		updateChangeUTXO.PrivateKey = feeKey.PrivateKey

		// Build a new directory version with empty children (file removed).
		deletedPayload := []byte("bitfs directory: docs (empty, file deleted)")

		deleteBatch := tx.NewMutationBatch()
		deleteBatch.AddSelfUpdate(childDirKey.PublicKey, rootResult.TxID, deletedPayload, docsRefreshUTXO, childDirKey.PrivateKey)
		deleteBatch.AddFeeInput(updateChangeUTXO)
		deleteBatch.SetChange(feeKey.PublicKey.Hash())
		deleteBatch.SetFeeRate(1)
		deleteResult, err := deleteBatch.Build()
		require.NoError(t, err, "build unsigned delete-dir tx")
		require.NotEmpty(t, deleteResult.RawTx)

		deleteSignedHex, err := deleteBatch.Sign(deleteResult)
		require.NoError(t, err, "sign delete-dir tx")

		deleteTxIDStr, err := node.SendRawTransaction(ctx, deleteSignedHex)
		require.NoError(t, err, "broadcast delete-dir tx")
		t.Logf("delete-dir txid: %s", deleteTxIDStr)
		mineOneBlock(t)

		// Retrieve the new directory version from chain.
		rawBytes, err := node.GetRawTransaction(ctx, deleteTxIDStr)
		require.NoError(t, err, "get delete-dir tx from chain")

		parsedTx, err := transaction.NewTransactionFromBytes(rawBytes)
		require.NoError(t, err, "parse delete-dir tx")

		opReturnOutput := parsedTx.Outputs[0]
		require.True(t, opReturnOutput.LockingScript.IsData(), "output 0 should be OP_RETURN")

		pushes := extractPushData(t, opReturnOutput.LockingScript)
		require.GreaterOrEqual(t, len(pushes), 4)

		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err)

		// Verify the new dir version still has the same P_node and parent link.
		assert.Equal(t, childDirKey.PublicKey.Compressed(), pNode,
			"updated dir P_node should still be docs dir key")
		assert.Equal(t, rootResult.TxID, parentTxID,
			"updated dir parentTxID should still link to root")
		assert.Equal(t, deletedPayload, payload,
			"updated dir payload should reflect deletion")

		t.Logf("delete verified: new dir version on-chain, file entry removed")
	})

	// ==================================================================
	// Step 11: Verify final DAG state.
	// ==================================================================
	t.Run("verify_dag_state", func(t *testing.T) {
		// Verify the root tx is on-chain and well-formed.
		rootRaw, err := node.GetRawTransaction(ctx, rootTxIDStr)
		require.NoError(t, err, "get root tx from chain")
		rootParsed, err := transaction.NewTransactionFromBytes(rootRaw)
		require.NoError(t, err, "parse root tx")
		require.True(t, rootParsed.Outputs[0].LockingScript.IsData())
		rootScriptBytes := []byte(*rootParsed.Outputs[0].LockingScript)
		assert.True(t, bytes.Contains(rootScriptBytes, tx.MetaFlagBytes),
			"root OP_RETURN should contain MetaFlag")

		// Verify the child dir tx links to root.
		childDirRaw, err := node.GetRawTransaction(ctx, childDirTxIDStr)
		require.NoError(t, err, "get child dir tx from chain")
		childDirParsed, err := transaction.NewTransactionFromBytes(childDirRaw)
		require.NoError(t, err, "parse child dir tx")
		childDirPushes := extractPushData(t, childDirParsed.Outputs[0].LockingScript)
		_, childParentTxID, _, err := tx.ParseOPReturnData(childDirPushes)
		require.NoError(t, err)

		rootTxIDBytes, err := hex.DecodeString(rootTxIDStr)
		require.NoError(t, err)
		reverseBytes(rootTxIDBytes)
		assert.Equal(t, rootTxIDBytes, childParentTxID,
			"child dir parentTxID should match root txid")

		// Verify the file tx links to docs dir.
		fileRaw, err := node.GetRawTransaction(ctx, fileTxIDStr)
		require.NoError(t, err, "get file tx from chain")
		fileParsed, err := transaction.NewTransactionFromBytes(fileRaw)
		require.NoError(t, err, "parse file tx")
		filePushes := extractPushData(t, fileParsed.Outputs[0].LockingScript)
		_, fileParentTxID, _, err := tx.ParseOPReturnData(filePushes)
		require.NoError(t, err)

		childDirTxIDBytes, err := hex.DecodeString(childDirTxIDStr)
		require.NoError(t, err)
		reverseBytes(childDirTxIDBytes)
		assert.Equal(t, childDirTxIDBytes, fileParentTxID,
			"file parentTxID should match child dir txid")

		// Verify the self-update tx links back to the same parent.
		updateRaw, err := node.GetRawTransaction(ctx, updateTxIDStr)
		require.NoError(t, err, "get update tx from chain")
		updateParsed, err := transaction.NewTransactionFromBytes(updateRaw)
		require.NoError(t, err, "parse update tx")
		updatePushes := extractPushData(t, updateParsed.Outputs[0].LockingScript)
		updatePNode, updateParentTxID, _, err := tx.ParseOPReturnData(updatePushes)
		require.NoError(t, err)

		assert.Equal(t, fileKey.PublicKey.Compressed(), updatePNode,
			"update P_node should still be file key")
		assert.Equal(t, childDirTxIDBytes, updateParentTxID,
			"update parentTxID should still link to docs dir")

		t.Logf("--- Full Lifecycle DAG Summary ---")
		t.Logf("Root:          %s", rootTxIDStr)
		t.Logf("  -> Docs:     %s", childDirTxIDStr)
		t.Logf("    -> File:   %s (original)", fileTxIDStr)
		t.Logf("    -> Update: %s (self-update)", updateTxIDStr)
		t.Logf("DAG integrity verified: all parent links correct")
		t.Logf("Lifecycle complete: create -> upload -> read -> update -> paid -> purchase -> delete -> verify")
	})
}

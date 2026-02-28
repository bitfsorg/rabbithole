//go:build e2e

package e2e

import (
	"bytes"
	"context"
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

// TestMkdirUpload builds a chain of three Metanet transactions:
//
//  1. Root directory (CreateRoot)
//  2. Child directory "docs" (CreateChild, linking to root)
//  3. File node with encrypted content (CreateChild, linking to "docs" dir)
//
// Each transaction is broadcast and confirmed on regtest. After all three are
// on-chain, the test retrieves each, parses the OP_RETURN, and verifies the
// ParentTxID links form a valid DAG: root <- docs <- file.
func TestMkdirUpload(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Create wallet and derive keys.
	// ==================================================================
	w := setupFundedWallet(t, ctx, node)

	// Fee key: m/44'/236'/0'/0/0
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	// Root key: m/44'/236'/1'/0/0
	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")

	// Child dir key: m/44'/236'/1'/0/0/0' (first child of root)
	childDirKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive child dir node key")

	// File key: m/44'/236'/1'/0/0/0'/0' (first child of docs dir)
	fileKey, err := w.DeriveNodeKey(0, []uint32{0, 0}, nil)
	require.NoError(t, err, "derive file node key")

	t.Logf("fee key:       %s", feeKey.Path)
	t.Logf("root key:      %s", rootKey.Path)
	t.Logf("child dir key: %s", childDirKey.Path)
	t.Logf("file key:      %s", fileKey.Path)

	// ==================================================================
	// Step 2: Fund the fee key address.
	// ==================================================================
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
	// Step 3: Build, sign, broadcast ROOT directory tx.
	// ==================================================================
	rootPayload := []byte("bitfs root directory")

	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, rootPayload)
	rootBatch.AddFeeInput(feeUTXO)
	rootBatch.SetChange(feeKey.PublicKey.Hash())
	rootBatch.SetFeeRate(1)
	rootResult, err := rootBatch.Build()
	require.NoError(t, err, "build root tx batch")
	require.NotEmpty(t, rootResult.RawTx)

	rootSignedHex, err := rootBatch.Sign(rootResult)
	require.NoError(t, err, "sign root tx")

	rootTxIDStr, err := node.SendRawTransaction(ctx, rootSignedHex)
	require.NoError(t, err, "broadcast root tx")
	t.Logf("root txid: %s", rootTxIDStr)

	mineOneBlock(t)

	// Prepare the root's NodeUTXO for spending as the parent edge in step 4.
	rootNodeUTXO := rootResult.NodeOps[0].NodeUTXO
	rootNodeUTXO.PrivateKey = rootKey.PrivateKey

	// Prepare the change UTXO from root tx as next fee input.
	changeUTXO := rootResult.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have a change output")
	changeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err, "build change P2PKH script")
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = feeKey.PrivateKey

	t.Logf("root NodeUTXO: txid=%x vout=%d amount=%d",
		rootNodeUTXO.TxID, rootNodeUTXO.Vout, rootNodeUTXO.Amount)
	t.Logf("root ChangeUTXO: txid=%x vout=%d amount=%d",
		changeUTXO.TxID, changeUTXO.Vout, changeUTXO.Amount)

	// ==================================================================
	// Step 4: Build, sign, broadcast CHILD DIRECTORY tx ("docs").
	// ==================================================================
	childDirPayload := []byte("bitfs directory: docs")

	childDirBatch := tx.NewMutationBatch()
	childDirBatch.AddCreateChild(childDirKey.PublicKey, rootResult.TxID, childDirPayload, rootNodeUTXO, rootKey.PrivateKey)
	childDirBatch.AddFeeInput(changeUTXO)
	childDirBatch.SetChange(feeKey.PublicKey.Hash())
	childDirBatch.SetFeeRate(1)
	childDirResult, err := childDirBatch.Build()
	require.NoError(t, err, "build child dir tx batch")
	require.NotEmpty(t, childDirResult.RawTx)

	childDirSignedHex, err := childDirBatch.Sign(childDirResult)
	require.NoError(t, err, "sign child dir tx")

	childDirTxIDStr, err := node.SendRawTransaction(ctx, childDirSignedHex)
	require.NoError(t, err, "broadcast child dir tx")
	t.Logf("child dir txid: %s", childDirTxIDStr)

	mineOneBlock(t)

	// Prepare child dir's NodeUTXO for spending as parent edge in step 5.
	childDirNodeUTXO := childDirResult.NodeOps[0].NodeUTXO
	childDirNodeUTXO.PrivateKey = childDirKey.PrivateKey

	// Prepare change from child dir tx as next fee input.
	childDirChangeUTXO := childDirResult.ChangeUTXO
	require.NotNil(t, childDirChangeUTXO, "child dir tx should have a change output")
	childDirChangeScript, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err, "build child dir change script")
	childDirChangeUTXO.ScriptPubKey = childDirChangeScript
	childDirChangeUTXO.PrivateKey = feeKey.PrivateKey

	// ==================================================================
	// Step 5: Encrypt file content using Method 42 (AccessFree).
	// ==================================================================
	plaintext := []byte("Hello BitFS! This is an encrypted document stored on-chain.")

	encResult, err := method42.Encrypt(plaintext, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "method42 encrypt")
	require.NotEmpty(t, encResult.Ciphertext)
	require.NotEmpty(t, encResult.KeyHash)
	t.Logf("encrypted %d bytes -> %d bytes ciphertext, keyHash=%x",
		len(plaintext), len(encResult.Ciphertext), encResult.KeyHash[:8])

	// Build the file payload: concatenate keyHash (32B) + ciphertext for the OP_RETURN.
	// In a real system, this would be a TLV BitFSPayload; for the e2e test
	// we use a simple format that can be verified.
	filePayload := make([]byte, 0, 32+len(encResult.Ciphertext))
	filePayload = append(filePayload, encResult.KeyHash...)
	filePayload = append(filePayload, encResult.Ciphertext...)

	// ==================================================================
	// Step 6: Build, sign, broadcast FILE NODE tx.
	// ==================================================================
	fileBatch := tx.NewMutationBatch()
	fileBatch.AddCreateChild(fileKey.PublicKey, childDirResult.TxID, filePayload, childDirNodeUTXO, childDirKey.PrivateKey)
	fileBatch.AddFeeInput(childDirChangeUTXO)
	fileBatch.SetChange(feeKey.PublicKey.Hash())
	fileBatch.SetFeeRate(1)
	fileResult, err := fileBatch.Build()
	require.NoError(t, err, "build file tx batch")
	require.NotEmpty(t, fileResult.RawTx)

	fileSignedHex, err := fileBatch.Sign(fileResult)
	require.NoError(t, err, "sign file tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast file tx")
	t.Logf("file txid: %s", fileTxIDStr)

	mineOneBlock(t)

	// ==================================================================
	// Step 7: Retrieve all three txs from chain and parse OP_RETURN.
	// ==================================================================
	type parsedNode struct {
		txIDStr    string
		pNode      []byte // compressed pubkey from OP_RETURN
		parentTxID []byte // 0 or 32 bytes
		payload    []byte
	}

	parseTxFromChain := func(t *testing.T, txIDStr string) parsedNode {
		t.Helper()
		rawBytes, err := node.GetRawTransaction(ctx, txIDStr)
		require.NoError(t, err, "get raw tx %s", txIDStr)

		parsedTx, err := transaction.NewTransactionFromBytes(rawBytes)
		require.NoError(t, err, "parse tx %s", txIDStr)

		// Output 0 must be OP_RETURN.
		opReturnOutput := parsedTx.Outputs[0]
		require.True(t, opReturnOutput.LockingScript.IsData(),
			"output 0 of %s should be OP_RETURN", txIDStr)

		// Extract push data from the OP_RETURN script.
		pushes := extractPushData(t, opReturnOutput.LockingScript)
		require.GreaterOrEqual(t, len(pushes), 4,
			"OP_RETURN should have >= 4 data pushes in tx %s", txIDStr)

		pNode, parentTxID, payload, err := tx.ParseOPReturnData(pushes)
		require.NoError(t, err, "parse OP_RETURN data for tx %s", txIDStr)

		return parsedNode{
			txIDStr:    txIDStr,
			pNode:      pNode,
			parentTxID: parentTxID,
			payload:    payload,
		}
	}

	rootNode := parseTxFromChain(t, rootTxIDStr)
	childDirNode := parseTxFromChain(t, childDirTxIDStr)
	fileNode := parseTxFromChain(t, fileTxIDStr)

	// ==================================================================
	// Step 8: Verify DAG structure.
	// ==================================================================

	// Root: no parent TxID (empty), P_node = root pubkey.
	assert.Empty(t, rootNode.parentTxID, "root should have empty parent TxID")
	assert.Equal(t, rootKey.PublicKey.Compressed(), rootNode.pNode,
		"root P_node should match root key")
	assert.Equal(t, rootPayload, rootNode.payload, "root payload should match")

	// Child dir: parent TxID should be root's TxID, P_node = childDir pubkey.
	assert.Equal(t, childDirKey.PublicKey.Compressed(), childDirNode.pNode,
		"child dir P_node should match childDir key")
	assert.Equal(t, childDirPayload, childDirNode.payload, "child dir payload should match")

	// The parentTxID in the child dir's OP_RETURN should match the root's TxID.
	// rootResult.TxID is in internal (little-endian) byte order.
	assert.Equal(t, rootResult.TxID, childDirNode.parentTxID,
		"child dir's parentTxID should link to root tx")

	// File: parent TxID should be child dir's TxID, P_node = file pubkey.
	assert.Equal(t, fileKey.PublicKey.Compressed(), fileNode.pNode,
		"file P_node should match file key")

	// The parentTxID in the file's OP_RETURN should match the child dir's TxID.
	assert.Equal(t, childDirResult.TxID, fileNode.parentTxID,
		"file's parentTxID should link to child dir tx")

	// Verify the encrypted payload can be decrypted.
	// File payload format: keyHash(32B) + ciphertext.
	require.True(t, len(fileNode.payload) > 32, "file payload should be > 32 bytes")
	extractedKeyHash := fileNode.payload[:32]
	extractedCiphertext := fileNode.payload[32:]

	decResult, err := method42.Decrypt(
		extractedCiphertext,
		fileKey.PrivateKey,
		fileKey.PublicKey,
		extractedKeyHash,
		method42.AccessFree,
	)
	require.NoError(t, err, "decrypt file content")
	assert.Equal(t, plaintext, decResult.Plaintext,
		"decrypted content should match original plaintext")

	// ==================================================================
	// Step 9: Verify the DAG chain is connected: root <- docs <- file.
	// ==================================================================
	// Convert root txid string to internal bytes for comparison.
	rootTxIDBytes, err := hex.DecodeString(rootTxIDStr)
	require.NoError(t, err)
	reverseBytes(rootTxIDBytes)
	assert.Equal(t, rootTxIDBytes, childDirNode.parentTxID,
		"child dir parentTxID should match root txid from chain")

	childDirTxIDBytes, err := hex.DecodeString(childDirTxIDStr)
	require.NoError(t, err)
	reverseBytes(childDirTxIDBytes)
	assert.Equal(t, childDirTxIDBytes, fileNode.parentTxID,
		"file parentTxID should match child dir txid from chain")

	// ==================================================================
	// Step 10: Log summary.
	// ==================================================================
	t.Logf("--- Metanet DAG Summary ---")
	t.Logf("Root:      %s (P_node=%x, parent=<none>)", rootTxIDStr, rootNode.pNode[:8])
	t.Logf("  -> Docs: %s (P_node=%x, parent=%x...)", childDirTxIDStr, childDirNode.pNode[:8], childDirNode.parentTxID[:8])
	t.Logf("    -> File: %s (P_node=%x, parent=%x...)", fileTxIDStr, fileNode.pNode[:8], fileNode.parentTxID[:8])
	t.Logf("DAG structure verified: root <- docs <- file")
	t.Logf("File content encrypted (%d bytes) and decrypted successfully", len(plaintext))
}

// extractPushData extracts all data pushes from an OP_RETURN script.
// The script is expected to start with OP_FALSE (0x00) OP_RETURN (0x6a),
// followed by push data elements.
func extractPushData(t *testing.T, s *script.Script) [][]byte {
	t.Helper()

	raw := []byte(*s)
	if len(raw) < 2 {
		t.Fatalf("script too short: %d bytes", len(raw))
	}

	// Skip OP_FALSE (0x00) and OP_RETURN (0x6a).
	require.Equal(t, byte(script.Op0), raw[0], "first byte should be OP_FALSE")
	require.Equal(t, byte(script.OpRETURN), raw[1], "second byte should be OP_RETURN")

	pos := 2
	var pushes [][]byte

	for pos < len(raw) {
		opcode := raw[pos]
		pos++

		var dataLen int
		switch {
		case opcode == 0x00:
			// OP_0 pushes empty bytes.
			pushes = append(pushes, []byte{})
			continue
		case opcode >= 0x01 && opcode <= 0x4b:
			// Direct push: opcode is the length.
			dataLen = int(opcode)
		case opcode == 0x4c:
			// OP_PUSHDATA1: next 1 byte is length.
			if pos >= len(raw) {
				t.Fatalf("unexpected end of script at OP_PUSHDATA1")
			}
			dataLen = int(raw[pos])
			pos++
		case opcode == 0x4d:
			// OP_PUSHDATA2: next 2 bytes (LE) is length.
			if pos+2 > len(raw) {
				t.Fatalf("unexpected end of script at OP_PUSHDATA2")
			}
			dataLen = int(raw[pos]) | int(raw[pos+1])<<8
			pos += 2
		case opcode == 0x4e:
			// OP_PUSHDATA4: next 4 bytes (LE) is length.
			if pos+4 > len(raw) {
				t.Fatalf("unexpected end of script at OP_PUSHDATA4")
			}
			dataLen = int(raw[pos]) | int(raw[pos+1])<<8 | int(raw[pos+2])<<16 | int(raw[pos+3])<<24
			pos += 4
		default:
			// Non-push opcode; stop parsing.
			break
		}

		if pos+dataLen > len(raw) {
			t.Fatalf("push data exceeds script length at offset %d (need %d, have %d)", pos, dataLen, len(raw)-pos)
		}

		data := make([]byte, dataLen)
		copy(data, raw[pos:pos+dataLen])
		pushes = append(pushes, data)
		pos += dataLen
	}

	return pushes
}

// TestMkdirUpload_ChildTxStructure is a focused unit-level check that verifies
// the CreateChild transaction has the correct input/output structure without
// needing a regtest node.
func TestMkdirUpload_ChildTxStructure(t *testing.T) {
	// Create a wallet for key derivation.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)
	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)
	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err)

	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err)
	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err)
	childKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err)

	// Simulate a parent TxID (32 bytes).
	parentTxID := bytes.Repeat([]byte{0xAB}, 32)

	// Simulate UTXOs.
	parentUTXO := &tx.UTXO{
		TxID:         bytes.Repeat([]byte{0x01}, 32),
		Vout:         1,
		Amount:       tx.DustLimit,
		ScriptPubKey: mustBuildP2PKH(t, rootKey.PublicKey),
		PrivateKey:   rootKey.PrivateKey,
	}
	feeUTXO := &tx.UTXO{
		TxID:         bytes.Repeat([]byte{0x02}, 32),
		Vout:         0,
		Amount:       100000,
		ScriptPubKey: mustBuildP2PKH(t, feeKey.PublicKey),
		PrivateKey:   feeKey.PrivateKey,
	}

	batch := tx.NewMutationBatch()
	batch.AddCreateChild(childKey.PublicKey, parentTxID, []byte("test child node"), parentUTXO, rootKey.PrivateKey)
	batch.AddFeeInput(feeUTXO)
	batch.SetChange(feeKey.PublicKey.Hash())
	batch.SetFeeRate(1)
	batchResult, err := batch.Build()
	require.NoError(t, err, "build child tx batch")
	require.NotEmpty(t, batchResult.RawTx, "should have raw tx bytes")

	// Parse the unsigned tx.
	parsedTx, err := transaction.NewTransactionFromBytes(batchResult.RawTx)
	require.NoError(t, err, "parse unsigned child tx")

	// Verify structure: 2 inputs, 3 outputs (OP_RETURN, P_child, change).
	assert.Equal(t, 2, parsedTx.InputCount(), "child tx should have 2 inputs")
	assert.GreaterOrEqual(t, parsedTx.OutputCount(), 3,
		"child tx should have at least 3 outputs")

	// Output 0: OP_RETURN.
	assert.True(t, parsedTx.Outputs[0].LockingScript.IsData(),
		"output 0 should be OP_RETURN")
	assert.Equal(t, uint64(0), parsedTx.Outputs[0].Satoshis)

	// Output 1: P_child dust.
	assert.Equal(t, tx.DustLimit, parsedTx.Outputs[1].Satoshis,
		"output 1 should be P_child dust")

	// OP_RETURN should contain MetaFlag.
	scriptBytes := []byte(*parsedTx.Outputs[0].LockingScript)
	assert.True(t, bytes.Contains(scriptBytes, tx.MetaFlagBytes),
		"OP_RETURN should contain MetaFlag")

	// OP_RETURN should contain ParentTxID.
	assert.True(t, bytes.Contains(scriptBytes, parentTxID),
		"OP_RETURN should contain ParentTxID")

	// Sign the tx and verify it produces valid signed hex.
	signedHex, err := batch.Sign(batchResult)
	require.NoError(t, err, "sign child tx")
	require.NotEmpty(t, signedHex)

	// Verify TxID is set after signing.
	assert.Len(t, batchResult.TxID, 32, "TxID should be 32 bytes after signing")

	// Verify NodeUTXO and ChangeUTXO have TxID set.
	assert.NotNil(t, batchResult.NodeOps[0].NodeUTXO, "NodeUTXO should be set")
	assert.Equal(t, batchResult.TxID, batchResult.NodeOps[0].NodeUTXO.TxID, "NodeUTXO.TxID should match")
	assert.NotNil(t, batchResult.ChangeUTXO, "ChangeUTXO should be set")
	assert.Equal(t, batchResult.TxID, batchResult.ChangeUTXO.TxID, "ChangeUTXO.TxID should match")

	t.Logf("child tx structure verified: 2 inputs, %d outputs, signed hex=%d chars",
		parsedTx.OutputCount(), len(signedHex))
}

func mustBuildP2PKH(t *testing.T, pub *ec.PublicKey) []byte {
	t.Helper()
	s, err := tx.BuildP2PKHScript(pub)
	require.NoError(t, err)
	return s
}

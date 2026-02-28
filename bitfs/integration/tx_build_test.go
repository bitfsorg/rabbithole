//go:build integration

package integration

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/metanet"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/spv"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// --- TestBuildFullMetanetTree ---

func TestBuildFullMetanetTree(t *testing.T) {
	// 1. Create wallet
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	state := wallet.NewWalletState()
	_, err := w.CreateVault(state, "test-tree")
	require.NoError(t, err)

	// Derive root key
	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	require.NotNil(t, rootKey.PrivateKey)
	require.NotNil(t, rootKey.PublicKey)

	// Derive fee key
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err)

	// Build a root directory node payload
	rootNode := &metanet.Node{
		Version: 1,
		Type:    metanet.NodeTypeDir,
		Op:      metanet.OpCreate,
	}
	payload, err := metanet.SerializePayload(rootNode)
	require.NoError(t, err)
	require.NotEmpty(t, payload)

	// 2. Build root directory transaction via MutationBatch
	feeUTXO := &tx.UTXO{
		TxID:       bytes.Repeat([]byte{0x01}, 32),
		Vout:       0,
		Amount:     100000,
		PrivateKey: feeKey.PrivateKey,
	}

	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, payload)
	rootBatch.AddFeeInput(feeUTXO)
	rootBatch.SetFeeRate(1)

	rootResult, err := rootBatch.Build()
	require.NoError(t, err)
	require.NotNil(t, rootResult)
	require.Len(t, rootResult.NodeOps, 1)
	assert.NotNil(t, rootResult.NodeOps[0].NodeUTXO)
	assert.Equal(t, uint32(1), rootResult.NodeOps[0].NodeVout)
	assert.Equal(t, tx.DustLimit, rootResult.NodeOps[0].NodeUTXO.Amount)

	// Verify OP_RETURN format
	opReturnPushes, err := tx.BuildOPReturnData(rootKey.PublicKey, nil, payload)
	require.NoError(t, err)
	assert.Len(t, opReturnPushes, 4)

	// Verify MetaFlag (0x6d657461) is present
	assert.Equal(t, tx.MetaFlagBytes, opReturnPushes[0])
	assert.Equal(t, []byte{0x6d, 0x65, 0x74, 0x61}, opReturnPushes[0])

	// Verify P_node matches derived public key
	assert.Equal(t, rootKey.PublicKey.Compressed(), opReturnPushes[1])

	// Verify root has empty parent TxID
	assert.Empty(t, opReturnPushes[2])

	// Parse it back
	pNode, parentTxID, parsedPayload, err := tx.ParseOPReturnData(opReturnPushes)
	require.NoError(t, err)
	assert.Equal(t, rootKey.PublicKey.Compressed(), pNode)
	assert.Empty(t, parentTxID)
	assert.Equal(t, payload, parsedPayload)

	// 3. Build child file transaction via MutationBatch
	childKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	childNode := &metanet.Node{
		Version:  1,
		Type:     metanet.NodeTypeFile,
		Op:       metanet.OpCreate,
		MimeType: "text/plain",
		FileSize: 1024,
	}
	childPayload, err := metanet.SerializePayload(childNode)
	require.NoError(t, err)

	fakeTxID := bytes.Repeat([]byte{0xaa}, 32)
	parentUTXO := &tx.UTXO{
		TxID:       fakeTxID,
		Vout:       1,
		Amount:     tx.DustLimit,
		PrivateKey: rootKey.PrivateKey,
	}

	childBatch := tx.NewMutationBatch()
	childBatch.AddCreateChild(childKey.PublicKey, fakeTxID, childPayload, parentUTXO, rootKey.PrivateKey)
	childBatch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x02}, 32), Vout: 0, Amount: 100000})
	childBatch.SetFeeRate(1)

	childResult, err := childBatch.Build()
	require.NoError(t, err)
	require.NotNil(t, childResult)
	require.Len(t, childResult.NodeOps, 1)
	assert.NotNil(t, childResult.NodeOps[0].NodeUTXO)

	// Verify parent-child linkage via OP_RETURN
	childPushes, err := tx.BuildOPReturnData(childKey.PublicKey, fakeTxID, childPayload)
	require.NoError(t, err)
	assert.Equal(t, fakeTxID, childPushes[2], "ParentTxID should be in OP_RETURN")
	assert.Equal(t, childKey.PublicKey.Compressed(), childPushes[1])

	// 4. Build update transaction via MutationBatch
	updatedNode := &metanet.Node{
		Version:  2,
		Type:     metanet.NodeTypeFile,
		Op:       metanet.OpUpdate,
		MimeType: "text/markdown",
		FileSize: 2048,
	}
	updatePayload, err := metanet.SerializePayload(updatedNode)
	require.NoError(t, err)

	updateBatch := tx.NewMutationBatch()
	updateBatch.AddSelfUpdate(childKey.PublicKey, fakeTxID, updatePayload,
		&tx.UTXO{TxID: bytes.Repeat([]byte{0xbb}, 32), Vout: 1, Amount: tx.DustLimit, PrivateKey: childKey.PrivateKey},
		childKey.PrivateKey)
	updateBatch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x03}, 32), Vout: 0, Amount: 100000})
	updateBatch.SetFeeRate(1)

	updateResult, err := updateBatch.Build()
	require.NoError(t, err)
	require.NotNil(t, updateResult)
	require.Len(t, updateResult.NodeOps, 1)
	assert.NotNil(t, updateResult.NodeOps[0].NodeUTXO)
}

// --- TestTransactionSigning ---

func TestTransactionSigning(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	// 1. Derive two different key pairs
	key1, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	key2, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	// Verify keys are different
	assert.NotEqual(t, key1.PublicKey.Compressed(), key2.PublicKey.Compressed(),
		"different paths should produce different keys")
	// Different public keys imply different private keys (secp256k1 is injective)

	// 2. Both should be valid secp256k1 keys
	assert.Len(t, key1.PublicKey.Compressed(), 33)
	assert.Len(t, key2.PublicKey.Compressed(), 33)

	// Verify first byte is 0x02 or 0x03 (compressed pubkey prefix)
	prefix1 := key1.PublicKey.Compressed()[0]
	prefix2 := key2.PublicKey.Compressed()[0]
	assert.True(t, prefix1 == 0x02 || prefix1 == 0x03)
	assert.True(t, prefix2 == 0x02 || prefix2 == 0x03)
}

// --- TestOPReturnPayloadRoundTrip ---

func TestOPReturnPayloadRoundTrip(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	// Build a complex directory node with children
	childPubKey, err := w.DeriveNodePubKey(0, []uint32{1, 1}, nil)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		Version:        1,
		Type:           metanet.NodeTypeDir,
		Op:             metanet.OpCreate,
		Access:         metanet.AccessFree,
		PricePerKB:     100,
		Domain:         "test.bitfs.org",
		Description:    "Test directory node",
		NextChildIndex: 1,
		Children: []metanet.ChildEntry{
			{
				Index:    0,
				Name:     "readme.txt",
				Type:     metanet.NodeTypeFile,
				PubKey:   childPubKey.Compressed(),
				Hardened: true,
			},
		},
	}

	payload, err := metanet.SerializePayload(dirNode)
	require.NoError(t, err)
	require.NotEmpty(t, payload)

	// Build OP_RETURN data
	parentTxID := bytes.Repeat([]byte{0xcc}, 32)
	pushes, err := tx.BuildOPReturnData(nodeKey.PublicKey, parentTxID, payload)
	require.NoError(t, err)

	// Parse the OP_RETURN data back
	pNode, parsedParentTxID, parsedPayload, err := tx.ParseOPReturnData(pushes)
	require.NoError(t, err)
	assert.Equal(t, nodeKey.PublicKey.Compressed(), pNode)
	assert.Equal(t, parentTxID, parsedParentTxID)
	assert.Equal(t, payload, parsedPayload)

	// Parse the payload back into a node
	fullPushes := [][]byte{
		tx.MetaFlagBytes,
		nodeKey.PublicKey.Compressed(),
		parentTxID,
		parsedPayload,
	}
	parsedNode, err := metanet.ParseNode(fullPushes)
	require.NoError(t, err)
	assert.Equal(t, metanet.NodeTypeDir, parsedNode.Type)
	assert.Equal(t, metanet.OpCreate, parsedNode.Op)
	assert.Equal(t, uint64(100), parsedNode.PricePerKB)
	assert.Equal(t, "test.bitfs.org", parsedNode.Domain)
	assert.Equal(t, "Test directory node", parsedNode.Description)
	assert.Len(t, parsedNode.Children, 1)
	assert.Equal(t, "readme.txt", parsedNode.Children[0].Name)
	assert.Equal(t, metanet.NodeTypeFile, parsedNode.Children[0].Type)
	assert.Equal(t, childPubKey.Compressed(), parsedNode.Children[0].PubKey)
}

// --- TestDataTransactionWithEncryptedContent ---

func TestDataTransactionWithEncryptedContent(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	// Encrypt content
	plaintext := []byte("This is the file content to be stored on-chain")
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// Build data transaction with encrypted content
	dataTx, err := tx.BuildDataTransaction(&tx.DataTxParams{
		NodePubKey: nodeKey.PublicKey,
		Content:    encResult.Ciphertext,
		SourceUTXO: &tx.UTXO{
			TxID:   bytes.Repeat([]byte{0x01}, 32),
			Vout:   0,
			Amount: 100000,
		},
		FeeRate: 1,
	})
	require.NoError(t, err)
	assert.NotNil(t, dataTx)
	assert.NotNil(t, dataTx.NodeUTXO)
}

// --- TestTransactionFeeEstimation ---

func TestTransactionFeeEstimation(t *testing.T) {
	// Verify fee estimation produces sane values for Metanet transactions
	// Root tx: 1 input, 3 outputs
	rootSize := tx.EstimateTxSize(1, 3, 100)
	rootFee := tx.EstimateFee(rootSize, 1)
	assert.Greater(t, rootSize, 0)
	assert.Greater(t, rootFee, uint64(0))

	// Child tx: 2 inputs, 4 outputs (larger)
	childSize := tx.EstimateTxSize(2, 4, 100)
	childFee := tx.EstimateFee(childSize, 1)
	assert.Greater(t, childSize, rootSize, "child tx should be larger than root tx")
	assert.GreaterOrEqual(t, childFee, rootFee)

	// Self-update tx: 2 inputs, 3 outputs
	updateSize := tx.EstimateTxSize(2, 3, 200)
	assert.Greater(t, updateSize, 0)

	// Larger payload = larger size
	largePayloadSize := tx.EstimateTxSize(1, 3, 10000)
	assert.Greater(t, largePayloadSize, rootSize, "larger payload should increase tx size")
}

// --- TestBatchCreateRootInsufficientFundsWithWalletKeys ---

func TestBatchCreateRootInsufficientFundsWithWalletKeys(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	payload, err := metanet.SerializePayload(&metanet.Node{
		Version: 1,
		Type:    metanet.NodeTypeDir,
		Op:      metanet.OpCreate,
	})
	require.NoError(t, err)

	batch := tx.NewMutationBatch()
	batch.AddCreateRoot(rootKey.PublicKey, payload)
	batch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x01}, 32), Amount: 1}) // too little
	batch.SetFeeRate(1)

	_, err = batch.Build()
	assert.ErrorIs(t, err, tx.ErrInsufficientFunds)
}

// --- TestUTXOChainContinuity (T031) ---

func TestUTXOChainContinuity(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	state := wallet.NewWalletState()
	_, err := w.CreateVault(state, "chain-test")
	require.NoError(t, err)

	// Derive root key and fee key
	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err)

	// Build root directory payload
	rootPayload, err := metanet.SerializePayload(&metanet.Node{
		Version: 1,
		Type:    metanet.NodeTypeDir,
		Op:      metanet.OpCreate,
	})
	require.NoError(t, err)

	// 1. Build root tx via MutationBatch
	feeUTXO := &tx.UTXO{
		TxID:       bytes.Repeat([]byte{0x01}, 32),
		Vout:       0,
		Amount:     500000,
		PrivateKey: feeKey.PrivateKey,
	}

	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, rootPayload)
	rootBatch.AddFeeInput(feeUTXO)
	rootBatch.SetFeeRate(1)

	rootResult, err := rootBatch.Build()
	require.NoError(t, err)
	require.Len(t, rootResult.NodeOps, 1)
	require.NotNil(t, rootResult.NodeOps[0].NodeUTXO)
	assert.Equal(t, tx.DustLimit, rootResult.NodeOps[0].NodeUTXO.Amount, "root NodeUTXO should be dust")
	assert.Equal(t, uint32(1), rootResult.NodeOps[0].NodeVout, "root P_node at output 1")

	// 2. Build child 1 via MutationBatch
	child1Key, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	child1Payload, err := metanet.SerializePayload(&metanet.Node{
		Version:  1,
		Type:     metanet.NodeTypeFile,
		Op:       metanet.OpCreate,
		MimeType: "text/plain",
		FileSize: 100,
	})
	require.NoError(t, err)

	// Simulate rootTx.NodeUTXO with a fake TxID
	parentUTXO1 := &tx.UTXO{
		TxID:       bytes.Repeat([]byte{0xaa}, 32),
		Vout:       rootResult.NodeOps[0].NodeVout,
		Amount:     rootResult.NodeOps[0].NodeUTXO.Amount,
		PrivateKey: rootKey.PrivateKey,
	}

	child1Batch := tx.NewMutationBatch()
	child1Batch.AddCreateChild(child1Key.PublicKey, bytes.Repeat([]byte{0xaa}, 32), child1Payload, parentUTXO1, rootKey.PrivateKey)
	child1Batch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x02}, 32), Vout: 0, Amount: 500000})
	child1Batch.SetFeeRate(1)

	child1Result, err := child1Batch.Build()
	require.NoError(t, err)
	require.Len(t, child1Result.NodeOps, 1)
	require.NotNil(t, child1Result.NodeOps[0].NodeUTXO)

	// Verify child1's outputs
	assert.Equal(t, tx.DustLimit, child1Result.NodeOps[0].NodeUTXO.Amount, "child1 NodeUTXO should be dust")

	// 3. Build child 2 via MutationBatch
	child2Key, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	child2Payload, err := metanet.SerializePayload(&metanet.Node{
		Version:  1,
		Type:     metanet.NodeTypeFile,
		Op:       metanet.OpCreate,
		MimeType: "application/pdf",
		FileSize: 2048,
	})
	require.NoError(t, err)

	// Simulate refreshed parent UTXO for child2
	parentUTXO2 := &tx.UTXO{
		TxID:       bytes.Repeat([]byte{0xbb}, 32),
		Vout:       1,
		Amount:     tx.DustLimit,
		PrivateKey: rootKey.PrivateKey,
	}

	child2Batch := tx.NewMutationBatch()
	child2Batch.AddCreateChild(child2Key.PublicKey, bytes.Repeat([]byte{0xbb}, 32), child2Payload, parentUTXO2, rootKey.PrivateKey)
	child2Batch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x03}, 32), Vout: 0, Amount: 500000})
	child2Batch.SetFeeRate(1)

	child2Result, err := child2Batch.Build()
	require.NoError(t, err)
	require.Len(t, child2Result.NodeOps, 1)
	require.NotNil(t, child2Result.NodeOps[0].NodeUTXO)

	// 4. Verify chain continuity: all amounts should be DustLimit
	assert.Equal(t, tx.DustLimit, child2Result.NodeOps[0].NodeUTXO.Amount)
}

// --- TestTransactionSizeEstimationAccuracy (T032) ---

func TestTransactionSizeEstimationAccuracy(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	// Test root tx size estimation
	rootPayload, err := metanet.SerializePayload(&metanet.Node{
		Version: 1,
		Type:    metanet.NodeTypeDir,
		Op:      metanet.OpCreate,
	})
	require.NoError(t, err)

	// Root tx: 1 input, 3 outputs
	rootEstSize := tx.EstimateTxSize(1, 3, len(rootPayload))
	assert.Greater(t, rootEstSize, 0, "estimated size should be positive")

	// Verify estimate is in a reasonable range (200-500 bytes for a simple root tx)
	assert.Greater(t, rootEstSize, 100, "root tx estimate too small")
	assert.Less(t, rootEstSize, 1000, "root tx estimate too large")

	// Test child tx size estimation (2 inputs, 4 outputs)
	childPayload, err := metanet.SerializePayload(&metanet.Node{
		Version:  1,
		Type:     metanet.NodeTypeFile,
		Op:       metanet.OpCreate,
		MimeType: "text/plain",
		FileSize: 1024,
	})
	require.NoError(t, err)

	childEstSize := tx.EstimateTxSize(2, 4, len(childPayload))
	assert.Greater(t, childEstSize, rootEstSize, "child tx should be larger than root tx")

	// Test self-update tx size estimation (2 inputs, 3 outputs)
	updatePayload, err := metanet.SerializePayload(&metanet.Node{
		Version:  2,
		Type:     metanet.NodeTypeFile,
		Op:       metanet.OpUpdate,
		MimeType: "text/markdown",
		FileSize: 2048,
	})
	require.NoError(t, err)

	updateEstSize := tx.EstimateTxSize(2, 3, len(updatePayload))
	assert.Greater(t, updateEstSize, 0)

	// Verify fee estimation is consistent: higher fee rate = higher or equal fee
	// Use a large enough size to ensure rounding doesn't flatten differences
	largeSize := 10000
	fee1 := tx.EstimateFee(largeSize, 1)
	fee2 := tx.EstimateFee(largeSize, 2)
	assert.Greater(t, fee2, fee1, "higher fee rate should produce higher fee for large tx")

	// Verify large payload increases estimate proportionally
	largePayloadSize := tx.EstimateTxSize(1, 3, 10000)
	assert.Greater(t, largePayloadSize, rootEstSize, "larger payload should increase size")

	// The payload size difference should be roughly reflected in estimate difference
	sizeDiff := largePayloadSize - rootEstSize
	payloadDiff := 10000 - len(rootPayload)
	// Size difference should be at least the payload difference (may include overhead)
	assert.GreaterOrEqual(t, sizeDiff, payloadDiff,
		"estimate should grow at least by payload size difference")

	// Test that MutationBatch uses fee estimation properly
	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, rootPayload)
	rootBatch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x01}, 32), Vout: 0, Amount: 100000})
	rootBatch.SetFeeRate(1)

	rootResult, err := rootBatch.Build()
	require.NoError(t, err)

	// If change exists, verify: fee UTXO amount = dust + fee + change
	if rootResult.ChangeUTXO != nil {
		totalSpent := tx.DustLimit + tx.EstimateFee(rootEstSize, 1) + rootResult.ChangeUTXO.Amount
		assert.Equal(t, uint64(100000), totalSpent,
			"total outputs + fee should equal input amount")
	}
}

// --- TestMerkleProofFullChain (T039) ---

func TestMerkleProofFullChain(t *testing.T) {
	// Create 4 mock transaction hashes (32 bytes each)
	txHashes := make([][]byte, 4)
	for i := range txHashes {
		txHashes[i] = make([]byte, 32)
		for j := range txHashes[i] {
			txHashes[i][j] = byte(i*32 + j)
		}
	}

	// 1. Build Merkle tree from the 4 tx hashes
	tree := spv.BuildMerkleTree(txHashes)
	require.NotNil(t, tree, "tree should not be nil")
	require.Len(t, tree, 1, "root level should have exactly 1 element")

	// 2. Compute merkle root using ComputeMerkleRootFromTxList
	merkleRoot := spv.ComputeMerkleRootFromTxList(txHashes)
	require.NotNil(t, merkleRoot)
	require.Len(t, merkleRoot, 32)

	// Both methods should produce the same root
	assert.Equal(t, tree[0], merkleRoot, "tree root and computed root should match")

	// 3. Create a MerkleProof for tx[1] (index=1)
	// For a 4-tx tree: proof needs sibling at level 0, and sibling at level 1
	// Level 0 pairs: (tx0, tx1), (tx2, tx3)
	// Level 1 pairs: (H01, H23)
	// Proof for tx[1] at index=1: [tx[0], H23]

	// Compute proof nodes manually
	// H01 parent's sibling: hash of tx[0] (the sibling at leaf level)
	proofNode0 := make([]byte, 32)
	copy(proofNode0, txHashes[0]) // sibling of tx[1] at level 0

	// H23 = DoubleHash(tx[2] || tx[3])
	combined23 := make([]byte, 64)
	copy(combined23[:32], txHashes[2])
	copy(combined23[32:], txHashes[3])
	proofNode1 := spv.DoubleHash(combined23) // sibling at level 1

	proof := &spv.MerkleProof{
		TxID:  txHashes[1],
		Index: 1,
		Nodes: [][]byte{proofNode0, proofNode1},
	}

	// 4. VerifyMerkleProof should succeed
	valid, err := spv.VerifyMerkleProof(proof, merkleRoot)
	require.NoError(t, err)
	assert.True(t, valid, "proof for tx[1] should verify against merkle root")

	// 5. Tamper with the proof -> should fail
	tamperedProof := &spv.MerkleProof{
		TxID:  txHashes[1],
		Index: 1,
		Nodes: [][]byte{proofNode0, bytes.Repeat([]byte{0xff}, 32)}, // wrong sibling
	}
	valid2, err := spv.VerifyMerkleProof(tamperedProof, merkleRoot)
	if err == nil {
		assert.False(t, valid2, "tampered proof should not verify")
	} else {
		// VerifyMerkleProof returns ErrMerkleProofInvalid for mismatch
		assert.ErrorIs(t, err, spv.ErrMerkleProofInvalid)
	}

	// 6. Tamper with TxID -> should also fail
	tamperedTxID := make([]byte, 32)
	copy(tamperedTxID, txHashes[1])
	tamperedTxID[0] ^= 0xff
	tamperedProof2 := &spv.MerkleProof{
		TxID:  tamperedTxID,
		Index: 1,
		Nodes: [][]byte{proofNode0, proofNode1},
	}
	valid3, err := spv.VerifyMerkleProof(tamperedProof2, merkleRoot)
	if err == nil {
		assert.False(t, valid3, "proof with wrong TxID should not verify")
	} else {
		assert.ErrorIs(t, err, spv.ErrMerkleProofInvalid)
	}

	// 7. Verify edge case: single-tx tree
	singleTxRoot := spv.ComputeMerkleRootFromTxList(txHashes[:1])
	require.NotNil(t, singleTxRoot)
	assert.Equal(t, txHashes[0], singleTxRoot, "single-tx merkle root should be the tx itself")

	// 8. Verify edge case: nil/empty input
	assert.Nil(t, spv.BuildMerkleTree(nil))
	assert.Nil(t, spv.BuildMerkleTree([][]byte{}))
	assert.Nil(t, spv.ComputeMerkleRootFromTxList(nil))
}

// --- TestSPVVerifyMetanetTransaction (T040) ---

func TestSPVVerifyMetanetTransaction(t *testing.T) {
	// 1. Create 4 mock tx hashes and build merkle tree
	txHashes := make([][]byte, 4)
	for i := range txHashes {
		txHashes[i] = make([]byte, 32)
		for j := range txHashes[i] {
			txHashes[i][j] = byte(i*16 + j + 1)
		}
	}

	merkleRoot := spv.ComputeMerkleRootFromTxList(txHashes)
	require.NotNil(t, merkleRoot)

	// 2. Create a mock block header with the known merkle root
	// Bits=0x2100ffff sets a very easy PoW target (target[0:2]=0xFFFF) so
	// any synthetic header hash passes VerifyPoW.
	header := &spv.BlockHeader{
		Version:    1,
		PrevBlock:  bytes.Repeat([]byte{0x00}, 32),
		MerkleRoot: merkleRoot,
		Timestamp:  1700000000,
		Bits:       0x2100ffff,
		Nonce:      12345,
		Height:     100,
	}

	// Compute and set the header hash
	header.Hash = spv.ComputeHeaderHash(header)
	require.NotNil(t, header.Hash)
	require.Len(t, header.Hash, 32)

	// 3. Store header in MemHeaderStore
	headerStore := spv.NewMemHeaderStore()
	err := headerStore.PutHeader(header)
	require.NoError(t, err)

	// Verify header was stored correctly
	retrievedHeader, err := headerStore.GetHeader(header.Hash)
	require.NoError(t, err)
	assert.Equal(t, merkleRoot, retrievedHeader.MerkleRoot)

	// 4. Create merkle proof for tx[0] (index=0)
	// Proof nodes for tx[0] at index=0: [tx[1], H23]
	proofNode0 := make([]byte, 32)
	copy(proofNode0, txHashes[1]) // sibling at leaf level

	combined23 := make([]byte, 64)
	copy(combined23[:32], txHashes[2])
	copy(combined23[32:], txHashes[3])
	proofNode1 := spv.DoubleHash(combined23)

	merkleProof := &spv.MerkleProof{
		TxID:      txHashes[0],
		Index:     0,
		Nodes:     [][]byte{proofNode0, proofNode1},
		BlockHash: header.Hash,
	}

	// 5. Create a StoredTx with valid merkle proof
	storedTx := &spv.StoredTx{
		TxID:        txHashes[0],
		RawTx:       nil, // nil to skip RawTx hash check; synthetic TxID won't match arbitrary bytes
		Proof:       merkleProof,
		BlockHeight: 100,
	}

	// 6. VerifyTransaction should pass
	err = spv.VerifyTransaction(storedTx, headerStore)
	require.NoError(t, err, "SPV verification should pass for valid proof")

	// 7. Create StoredTx with wrong merkle root (header not in store)
	badBlockHash := bytes.Repeat([]byte{0xff}, 32)
	badProof := &spv.MerkleProof{
		TxID:      txHashes[0],
		Index:     0,
		Nodes:     [][]byte{proofNode0, proofNode1},
		BlockHash: badBlockHash,
	}
	badStoredTx := &spv.StoredTx{
		TxID:        txHashes[0],
		Proof:       badProof,
		BlockHeight: 100,
	}
	err = spv.VerifyTransaction(badStoredTx, headerStore)
	assert.Error(t, err, "should fail when block hash not in header store")
	assert.ErrorIs(t, err, spv.ErrHeaderNotFound)

	// 8. Test with nil proof (unconfirmed tx)
	unconfirmedTx := &spv.StoredTx{
		TxID:  txHashes[0],
		Proof: nil,
	}
	err = spv.VerifyTransaction(unconfirmedTx, headerStore)
	assert.ErrorIs(t, err, spv.ErrUnconfirmed)

	// 9. Test with tampered proof (wrong TxID in proof)
	tamperedProof := &spv.MerkleProof{
		TxID:      bytes.Repeat([]byte{0xde}, 32),
		Index:     0,
		Nodes:     [][]byte{proofNode0, proofNode1},
		BlockHash: header.Hash,
	}
	tamperedStoredTx := &spv.StoredTx{
		TxID:  txHashes[0],
		Proof: tamperedProof,
	}
	err = spv.VerifyTransaction(tamperedStoredTx, headerStore)
	assert.Error(t, err, "should fail when proof TxID doesn't match stored TxID")

	// 10. Verify header chain works with MemHeaderStore
	header2 := &spv.BlockHeader{
		Version:    1,
		PrevBlock:  header.Hash, // chain to previous
		MerkleRoot: bytes.Repeat([]byte{0xab}, 32),
		Timestamp:  1700000600,
		Bits:       0x2100ffff, // easy PoW target for synthetic headers
		Nonce:      67890,
		Height:     101,
	}
	header2.Hash = spv.ComputeHeaderHash(header2)
	err = headerStore.PutHeader(header2)
	require.NoError(t, err)

	// Verify chain
	err = spv.VerifyHeaderChain([]*spv.BlockHeader{header, header2})
	require.NoError(t, err, "header chain should be valid")

	// GetTip should return the latest header
	tip, err := headerStore.GetTip()
	require.NoError(t, err)
	assert.Equal(t, uint32(101), tip.Height)

	count, err := headerStore.GetHeaderCount()
	require.NoError(t, err)
	assert.Equal(t, uint64(2), count)
}

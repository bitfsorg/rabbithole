//go:build integration

package integration

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/metanet"
	"github.com/tongxiaofeng/bitfs/internal/method42"
	"github.com/tongxiaofeng/bitfs/internal/tx"
	"github.com/tongxiaofeng/bitfs/internal/wallet"
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

	// 2. Build root directory transaction (BuildCreateRoot)
	feeUTXO := &tx.UTXO{
		TxID:       bytes.Repeat([]byte{0x01}, 32),
		Vout:       0,
		Amount:     100000,
		PrivateKey: feeKey.PrivateKey,
	}

	rootTx, err := tx.BuildCreateRoot(&tx.CreateRootParams{
		NodePubKey: rootKey.PublicKey,
		Payload:    payload,
		FeeUTXO:    feeUTXO,
		FeeRate:    1,
	})
	require.NoError(t, err)
	assert.NotNil(t, rootTx)
	assert.NotNil(t, rootTx.NodeUTXO)
	assert.Equal(t, uint32(1), rootTx.NodeUTXO.Vout)
	assert.Equal(t, tx.DustLimit, rootTx.NodeUTXO.Amount)
	assert.Nil(t, rootTx.ParentUTXO, "root has no parent refresh UTXO")

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

	// 3. Build child file transaction (BuildCreateChild)
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

	childTx, err := tx.BuildCreateChild(&tx.CreateChildParams{
		NodePubKey:    childKey.PublicKey,
		ParentTxID:    fakeTxID,
		Payload:       childPayload,
		ParentUTXO:    parentUTXO,
		ParentPrivKey: rootKey.PrivateKey,
		FeeUTXO:       &tx.UTXO{TxID: bytes.Repeat([]byte{0x02}, 32), Vout: 0, Amount: 100000},
		ParentPubKey:  rootKey.PublicKey,
		FeeRate:       1,
	})
	require.NoError(t, err)
	assert.NotNil(t, childTx)
	assert.NotNil(t, childTx.NodeUTXO)
	assert.NotNil(t, childTx.ParentUTXO, "child should have parent refresh")

	// Verify parent-child linkage via OP_RETURN
	childPushes, err := tx.BuildOPReturnData(childKey.PublicKey, fakeTxID, childPayload)
	require.NoError(t, err)
	assert.Equal(t, fakeTxID, childPushes[2], "ParentTxID should be in OP_RETURN")
	assert.Equal(t, childKey.PublicKey.Compressed(), childPushes[1])

	// 4. Build update transaction (BuildSelfUpdate)
	updatedNode := &metanet.Node{
		Version:  2,
		Type:     metanet.NodeTypeFile,
		Op:       metanet.OpUpdate,
		MimeType: "text/markdown",
		FileSize: 2048,
	}
	updatePayload, err := metanet.SerializePayload(updatedNode)
	require.NoError(t, err)

	updateTx, err := tx.BuildSelfUpdate(&tx.SelfUpdateParams{
		NodePubKey:  childKey.PublicKey,
		NodePrivKey: childKey.PrivateKey,
		ParentTxID:  fakeTxID, // preserved from original creation
		Payload:     updatePayload,
		NodeUTXO:    &tx.UTXO{TxID: bytes.Repeat([]byte{0xbb}, 32), Vout: 1, Amount: tx.DustLimit, PrivateKey: childKey.PrivateKey},
		FeeUTXO:     &tx.UTXO{TxID: bytes.Repeat([]byte{0x03}, 32), Vout: 0, Amount: 100000},
		FeeRate:     1,
	})
	require.NoError(t, err)
	assert.NotNil(t, updateTx)
	assert.NotNil(t, updateTx.NodeUTXO)
	assert.Nil(t, updateTx.ParentUTXO, "self-update has no parent refresh")
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

// --- TestBuildCreateRootInsufficientFundsWithWalletKeys ---

func TestBuildCreateRootInsufficientFundsWithWalletKeys(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	payload, err := metanet.SerializePayload(&metanet.Node{
		Version: 1,
		Type:    metanet.NodeTypeDir,
		Op:      metanet.OpCreate,
	})
	require.NoError(t, err)

	_, err = tx.BuildCreateRoot(&tx.CreateRootParams{
		NodePubKey: rootKey.PublicKey,
		Payload:    payload,
		FeeUTXO:    &tx.UTXO{TxID: bytes.Repeat([]byte{0x01}, 32), Amount: 100}, // too little
		FeeRate:    1,
	})
	assert.ErrorIs(t, err, tx.ErrInsufficientFunds)
}

//go:build integration

package integration

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/metanet"
	"github.com/bitfsorg/libbitfs-go/spv"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeMinimalPayload creates a minimal valid Metanet payload for testing.
func makeMinimalPayload(t *testing.T) []byte {
	t.Helper()
	payload, err := metanet.SerializePayload(&metanet.Node{
		Version: 1,
		Type:    metanet.NodeTypeDir,
		Op:      metanet.OpCreate,
	})
	require.NoError(t, err)
	return payload
}

// deriveTestRootKey creates a wallet and derives a vault root key.
func deriveTestRootKey(t *testing.T) (*wallet.Wallet, *wallet.KeyPair) {
	t.Helper()
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	state := wallet.NewWalletState()
	_, err := w.CreateVault(state, "extra-tests")
	require.NoError(t, err)
	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	return w, rootKey
}

// makeFeeUTXO builds a fake fee UTXO with the given amount.
func makeFeeUTXO(amount uint64) *tx.UTXO {
	return &tx.UTXO{
		TxID:   bytes.Repeat([]byte{0x01}, 32),
		Vout:   0,
		Amount: amount,
	}
}

// buildMerkleProofForIndex constructs a manual Merkle proof for the given tx at
// index in a set of hashes. The caller must provide the exact set of hashes
// used to build the tree.
func buildMerkleProofForIndex(txHashes [][]byte, idx uint32) *spv.MerkleProof {
	// Build all levels of the tree so we can extract sibling nodes.
	levels := [][][]byte{copyHashes(txHashes)}
	for len(levels[len(levels)-1]) > 1 {
		prev := levels[len(levels)-1]
		if len(prev)%2 != 0 {
			dup := make([]byte, 32)
			copy(dup, prev[len(prev)-1])
			prev = append(prev, dup)
		}
		next := make([][]byte, len(prev)/2)
		for i := 0; i < len(prev); i += 2 {
			combined := make([]byte, 64)
			copy(combined[:32], prev[i])
			copy(combined[32:], prev[i+1])
			next[i/2] = spv.DoubleHash(combined)
		}
		levels = append(levels, next)
	}

	var proofNodes [][]byte
	pos := idx
	for l := 0; l < len(levels)-1; l++ {
		level := levels[l]
		// Pad if needed
		if len(level)%2 != 0 {
			dup := make([]byte, 32)
			copy(dup, level[len(level)-1])
			level = append(level, dup)
		}
		var siblingIdx uint32
		if pos%2 == 0 {
			siblingIdx = pos + 1
		} else {
			siblingIdx = pos - 1
		}
		node := make([]byte, 32)
		copy(node, level[siblingIdx])
		proofNodes = append(proofNodes, node)
		pos /= 2
	}

	return &spv.MerkleProof{
		TxID:  txHashes[idx],
		Index: idx,
		Nodes: proofNodes,
	}
}

// copyHashes deep-copies a slice of 32-byte hashes.
func copyHashes(src [][]byte) [][]byte {
	dst := make([][]byte, len(src))
	for i, h := range src {
		dst[i] = make([]byte, 32)
		copy(dst[i], h)
	}
	return dst
}

// makeTxHashes generates n deterministic 32-byte hashes.
func makeTxHashes(n int) [][]byte {
	hashes := make([][]byte, n)
	for i := range hashes {
		hashes[i] = make([]byte, 32)
		for j := range hashes[i] {
			hashes[i][j] = byte(i*32 + j)
		}
	}
	return hashes
}

// makeBlockHeader creates a BlockHeader with fields set and computes its hash.
func makeBlockHeader(version int32, prevBlock, merkleRoot []byte, timestamp, bits, nonce, height uint32) *spv.BlockHeader {
	h := &spv.BlockHeader{
		Version:    version,
		PrevBlock:  prevBlock,
		MerkleRoot: merkleRoot,
		Timestamp:  timestamp,
		Bits:       bits,
		Nonce:      nonce,
		Height:     height,
	}
	h.Hash = spv.ComputeHeaderHash(h)
	return h
}

// ---------------------------------------------------------------------------
// 1. TestBatchCreateRootMinimalFee
// ---------------------------------------------------------------------------

func TestBatchCreateRootMinimalFee(t *testing.T) {
	_, rootKey := deriveTestRootKey(t)
	payload := makeMinimalPayload(t)

	// Compute the minimum amount needed: 1 OP_RETURN + 1 P2PKH + 1 potential change = 3 outputs.
	estSize := tx.EstimateTxSize(1, 3, len(payload))
	estFee := tx.EstimateFee(estSize, 1)
	minAmount := tx.DustLimit + estFee

	batch := tx.NewMutationBatch()
	batch.AddCreateRoot(rootKey.PublicKey, payload)
	batch.AddFeeInput(makeFeeUTXO(minAmount))
	batch.SetFeeRate(1)

	result, err := batch.Build()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.NodeOps, 1)
	assert.NotNil(t, result.NodeOps[0].NodeUTXO)
	assert.Equal(t, tx.DustLimit, result.NodeOps[0].NodeUTXO.Amount)

	// Change should be nil or very small (below dust, so nil).
	assert.Nil(t, result.ChangeUTXO, "with minimal fee, change should be nil or below dust")
}

// ---------------------------------------------------------------------------
// 2. TestBatchCreateRootWithChange
// ---------------------------------------------------------------------------

func TestBatchCreateRootWithChange(t *testing.T) {
	_, rootKey := deriveTestRootKey(t)
	payload := makeMinimalPayload(t)

	batch := tx.NewMutationBatch()
	batch.AddCreateRoot(rootKey.PublicKey, payload)
	batch.AddFeeInput(makeFeeUTXO(500000))
	batch.SetFeeRate(1)

	result, err := batch.Build()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.ChangeUTXO, "with 500000 sats there should be change")

	estSize := tx.EstimateTxSize(1, 3, len(payload))
	estFee := tx.EstimateFee(estSize, 1)
	expectedChange := uint64(500000) - tx.DustLimit - estFee
	assert.Equal(t, expectedChange, result.ChangeUTXO.Amount,
		"change should equal input - dust - fee")
}

// ---------------------------------------------------------------------------
// 3. TestBatchCreateChildAllOutputs
// ---------------------------------------------------------------------------

func TestBatchCreateChildAllOutputs(t *testing.T) {
	w, rootKey := deriveTestRootKey(t)

	childKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	childPayload, err := metanet.SerializePayload(&metanet.Node{
		Version:  1,
		Type:     metanet.NodeTypeFile,
		Op:       metanet.OpCreate,
		MimeType: "text/plain",
		FileSize: 100,
	})
	require.NoError(t, err)

	fakeTxID := bytes.Repeat([]byte{0xaa}, 32)
	parentUTXO := &tx.UTXO{TxID: fakeTxID, Vout: 1, Amount: tx.DustLimit, PrivateKey: rootKey.PrivateKey}

	batch := tx.NewMutationBatch()
	batch.AddCreateChild(childKey.PublicKey, fakeTxID, childPayload, parentUTXO, rootKey.PrivateKey)
	batch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x02}, 32), Vout: 0, Amount: 500000})
	batch.SetFeeRate(1)

	result, err := batch.Build()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.NodeOps, 1)

	// NodeUTXO (P_node dust)
	require.NotNil(t, result.NodeOps[0].NodeUTXO, "child must have NodeUTXO")
	assert.Equal(t, tx.DustLimit, result.NodeOps[0].NodeUTXO.Amount)

	// Change should exist with large fee input
	require.NotNil(t, result.ChangeUTXO, "should have change with 500000 sats input")
	assert.Greater(t, result.ChangeUTXO.Amount, tx.DustLimit)
}

// ---------------------------------------------------------------------------
// 4. TestBatchSelfUpdatePreservesParentTxID
// ---------------------------------------------------------------------------

func TestBatchSelfUpdatePreservesParentTxID(t *testing.T) {
	w, rootKey := deriveTestRootKey(t)
	childKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	parentTxID := bytes.Repeat([]byte{0xdd}, 32)
	updatePayload, err := metanet.SerializePayload(&metanet.Node{
		Version:  2,
		Type:     metanet.NodeTypeFile,
		Op:       metanet.OpUpdate,
		MimeType: "text/markdown",
		FileSize: 2048,
	})
	require.NoError(t, err)

	batch := tx.NewMutationBatch()
	batch.AddSelfUpdate(childKey.PublicKey, parentTxID, updatePayload,
		&tx.UTXO{TxID: bytes.Repeat([]byte{0xbb}, 32), Vout: 1, Amount: tx.DustLimit, PrivateKey: childKey.PrivateKey},
		childKey.PrivateKey)
	batch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x03}, 32), Vout: 0, Amount: 100000})
	batch.SetFeeRate(1)

	result, err := batch.Build()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.NodeOps, 1)

	// Build the OP_RETURN data with the same params and verify parentTxID is preserved.
	pushes, err := tx.BuildOPReturnData(childKey.PublicKey, parentTxID, updatePayload)
	require.NoError(t, err)
	_, parsedParentTxID, _, err := tx.ParseOPReturnData(pushes)
	require.NoError(t, err)

	assert.Equal(t, parentTxID, parsedParentTxID,
		"self-update must preserve the original parent TxID in OP_RETURN")
	_ = rootKey
}

// ---------------------------------------------------------------------------
// 5. TestBatchSelfUpdateSingleOp
// ---------------------------------------------------------------------------

func TestBatchSelfUpdateSingleOp(t *testing.T) {
	w, _ := deriveTestRootKey(t)
	childKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	payload := makeMinimalPayload(t)

	batch := tx.NewMutationBatch()
	batch.AddSelfUpdate(childKey.PublicKey, bytes.Repeat([]byte{0xee}, 32), payload,
		&tx.UTXO{TxID: bytes.Repeat([]byte{0xcc}, 32), Vout: 1, Amount: tx.DustLimit, PrivateKey: childKey.PrivateKey},
		childKey.PrivateKey)
	batch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x04}, 32), Vout: 0, Amount: 100000})
	batch.SetFeeRate(1)

	result, err := batch.Build()
	require.NoError(t, err)

	// Self-update produces exactly 1 node op with a refreshed NodeUTXO.
	require.Len(t, result.NodeOps, 1)
	assert.NotNil(t, result.NodeOps[0].NodeUTXO, "self-update must refresh its own UTXO")
	assert.Equal(t, tx.DustLimit, result.NodeOps[0].NodeUTXO.Amount)
}

// ---------------------------------------------------------------------------
// 6. TestDataTransactionVariousSizes
// ---------------------------------------------------------------------------

func TestDataTransactionVariousSizes(t *testing.T) {
	_, rootKey := deriveTestRootKey(t)

	tests := []struct {
		name string
		size int
	}{
		{"100 bytes", 100},
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content := bytes.Repeat([]byte{0xAB}, tc.size)
			dataTx, err := tx.BuildDataTransaction(&tx.DataTxParams{
				NodePubKey: rootKey.PublicKey,
				Content:    content,
				SourceUTXO: &tx.UTXO{TxID: bytes.Repeat([]byte{0x01}, 32), Vout: 0, Amount: 1000000},
				FeeRate:    1,
			})
			require.NoError(t, err, "size=%d should succeed", tc.size)
			assert.NotNil(t, dataTx.NodeUTXO, "NodeUTXO must exist for size=%d", tc.size)
		})
	}
}

// ---------------------------------------------------------------------------
// 7. TestOPReturnDataRootCase
// ---------------------------------------------------------------------------

func TestOPReturnDataRootCase(t *testing.T) {
	_, rootKey := deriveTestRootKey(t)
	payload := makeMinimalPayload(t)

	pushes, err := tx.BuildOPReturnData(rootKey.PublicKey, nil, payload)
	require.NoError(t, err)
	require.Len(t, pushes, 4)

	// Third push (parentTxID) should be nil for root.
	assert.Nil(t, pushes[2], "root node's parentTxID push should be nil")

	// Parse it back.
	_, parsedParentTxID, _, err := tx.ParseOPReturnData(pushes)
	require.NoError(t, err)
	assert.Empty(t, parsedParentTxID, "parsed parentTxID for root should be empty")
}

// ---------------------------------------------------------------------------
// 8. TestOPReturnDataMetaFlag
// ---------------------------------------------------------------------------

func TestOPReturnDataMetaFlag(t *testing.T) {
	_, rootKey := deriveTestRootKey(t)

	payloads := [][]byte{
		makeMinimalPayload(t),
		bytes.Repeat([]byte{0xff}, 100),
		[]byte("hello world payload"),
	}

	for i, payload := range payloads {
		pushes, err := tx.BuildOPReturnData(rootKey.PublicKey, nil, payload)
		require.NoError(t, err, "payload %d", i)
		assert.Equal(t, tx.MetaFlagBytes, pushes[0],
			"first push must always be MetaFlagBytes for payload %d", i)
		assert.Equal(t, []byte{0x6d, 0x65, 0x74, 0x61}, pushes[0])
	}
}

// ---------------------------------------------------------------------------
// 9. TestOPReturnDataPubKeyPreserved
// ---------------------------------------------------------------------------

func TestOPReturnDataPubKeyPreserved(t *testing.T) {
	_, rootKey := deriveTestRootKey(t)
	payload := makeMinimalPayload(t)
	parentTxID := bytes.Repeat([]byte{0xaa}, 32)

	pushes, err := tx.BuildOPReturnData(rootKey.PublicKey, parentTxID, payload)
	require.NoError(t, err)

	pNode, _, _, err := tx.ParseOPReturnData(pushes)
	require.NoError(t, err)
	assert.Len(t, pNode, 33, "compressed pubkey must be 33 bytes")
	assert.Equal(t, rootKey.PublicKey.Compressed(), pNode,
		"P_node pubkey must round-trip exactly")
}

// ---------------------------------------------------------------------------
// 10. TestParseOPReturnDataMalformed
// ---------------------------------------------------------------------------

func TestParseOPReturnDataMalformed(t *testing.T) {
	tests := []struct {
		name   string
		pushes [][]byte
	}{
		{"nil pushes", nil},
		{"empty pushes", [][]byte{}},
		{"1 push", [][]byte{{0x01}}},
		{"2 pushes", [][]byte{{0x01}, {0x02}}},
		{"3 pushes", [][]byte{tx.MetaFlagBytes, bytes.Repeat([]byte{0x02}, 33), bytes.Repeat([]byte{0x03}, 32)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := tx.ParseOPReturnData(tc.pushes)
			assert.Error(t, err, "malformed pushes should error")
		})
	}
}

// ---------------------------------------------------------------------------
// 11. TestParseOPReturnDataWrongMetaFlag
// ---------------------------------------------------------------------------

func TestParseOPReturnDataWrongMetaFlag(t *testing.T) {
	pushes := [][]byte{
		[]byte("FAKE"),                      // wrong meta flag
		bytes.Repeat([]byte{0x02}, 33),      // fake pubkey
		bytes.Repeat([]byte{0x03}, 32),      // fake parentTxID
		[]byte("payload"),                   // payload
	}
	_, _, _, err := tx.ParseOPReturnData(pushes)
	assert.Error(t, err, "wrong MetaFlag should produce an error")
}

// ---------------------------------------------------------------------------
// 12. TestBatchCreateChildInsufficientFunds
// ---------------------------------------------------------------------------

func TestBatchCreateChildInsufficientFunds(t *testing.T) {
	w, rootKey := deriveTestRootKey(t)
	childKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	payload := makeMinimalPayload(t)
	fakeTxID := bytes.Repeat([]byte{0xaa}, 32)
	parentUTXO := &tx.UTXO{TxID: fakeTxID, Vout: 1, Amount: tx.DustLimit, PrivateKey: rootKey.PrivateKey}

	batch := tx.NewMutationBatch()
	batch.AddCreateChild(childKey.PublicKey, fakeTxID, payload, parentUTXO, rootKey.PrivateKey)
	batch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{0x02}, 32), Vout: 0, Amount: 0}) // zero fee = insufficient
	batch.SetFeeRate(1)

	_, err = batch.Build()
	assert.ErrorIs(t, err, tx.ErrInsufficientFunds)
}

// ---------------------------------------------------------------------------
// 13. TestBatchNoOpsError
// ---------------------------------------------------------------------------

func TestBatchNoOpsError(t *testing.T) {
	batch := tx.NewMutationBatch()
	batch.AddFeeInput(makeFeeUTXO(100000))
	_, err := batch.Build()
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// 14. TestBatchNilPubKeyError
// ---------------------------------------------------------------------------

func TestBatchNilPubKeyError(t *testing.T) {
	batch := tx.NewMutationBatch()
	batch.AddCreateRoot(nil, makeMinimalPayload(t))
	batch.AddFeeInput(makeFeeUTXO(100000))
	_, err := batch.Build()
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// 15. TestFeeEstimationLinearScaling
// ---------------------------------------------------------------------------

func TestFeeEstimationLinearScaling(t *testing.T) {
	payloadSizes := []int{100, 1000, 5000, 10000, 50000}
	var prevSize int
	for i, pl := range payloadSizes {
		size := tx.EstimateTxSize(1, 3, pl)
		assert.Greater(t, size, 0)
		if i > 0 {
			assert.Greater(t, size, prevSize,
				"larger payload (%d > %d) must yield larger tx size", pl, payloadSizes[i-1])
		}
		prevSize = size
	}

	// Check approximate linearity: (size2-size1) ~ (payload2-payload1).
	s1 := tx.EstimateTxSize(1, 3, 1000)
	s2 := tx.EstimateTxSize(1, 3, 2000)
	s3 := tx.EstimateTxSize(1, 3, 3000)
	diff12 := s2 - s1
	diff23 := s3 - s2
	assert.Equal(t, diff12, diff23,
		"size increase should be constant for constant payload increments")
}

// ---------------------------------------------------------------------------
// 16. TestFeeEstimationFeeRateScaling
// ---------------------------------------------------------------------------

func TestFeeEstimationFeeRateScaling(t *testing.T) {
	size := 1000 // 1000 bytes to avoid rounding noise
	feeRates := []uint64{1, 2, 5, 10}
	fees := make([]uint64, len(feeRates))
	for i, rate := range feeRates {
		fees[i] = tx.EstimateFee(size, rate)
	}

	// Fee should scale proportionally with fee rate.
	for i := 1; i < len(fees); i++ {
		assert.Greater(t, fees[i], fees[i-1],
			"higher fee rate (%d) must yield higher fee", feeRates[i])
	}

	// For size=1000 bytes, fee = ceil(1000*rate/1000) = rate exactly.
	for i, rate := range feeRates {
		assert.Equal(t, rate, fees[i],
			"for 1000 byte tx, fee should equal fee rate")
	}
}

// ---------------------------------------------------------------------------
// 17. TestDustLimitConstant
// ---------------------------------------------------------------------------

func TestDustLimitConstant(t *testing.T) {
	assert.Equal(t, uint64(1), tx.DustLimit)
}

// ---------------------------------------------------------------------------
// 18. TestMetaFlagConstant
// ---------------------------------------------------------------------------

func TestMetaFlagConstant(t *testing.T) {
	assert.Equal(t, []byte("meta"), tx.MetaFlagBytes)
	assert.Equal(t, []byte{0x6d, 0x65, 0x74, 0x61}, tx.MetaFlagBytes)
}

// ---------------------------------------------------------------------------
// 19. TestBatchMultiChildSequential
// ---------------------------------------------------------------------------

func TestBatchMultiChildSequential(t *testing.T) {
	w, rootKey := deriveTestRootKey(t)
	payload := makeMinimalPayload(t)

	// Build root tx via batch.
	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, payload)
	rootBatch.AddFeeInput(makeFeeUTXO(500000))
	rootBatch.SetFeeRate(1)

	rootResult, err := rootBatch.Build()
	require.NoError(t, err)
	require.Len(t, rootResult.NodeOps, 1)
	require.NotNil(t, rootResult.NodeOps[0].NodeUTXO)

	// Simulate the TxID output for the root.
	currentParentTxID := bytes.Repeat([]byte{0xa0}, 32)
	currentParentUTXO := &tx.UTXO{
		TxID:       currentParentTxID,
		Vout:       rootResult.NodeOps[0].NodeVout,
		Amount:     rootResult.NodeOps[0].NodeUTXO.Amount,
		PrivateKey: rootKey.PrivateKey,
	}

	// Build child1 -> child2 -> child3 sequentially, each as separate batch.
	for i := uint32(1); i <= 3; i++ {
		childKey, err := w.DeriveNodeKey(0, []uint32{i}, nil)
		require.NoError(t, err)

		childPayload, err := metanet.SerializePayload(&metanet.Node{
			Version:  1,
			Type:     metanet.NodeTypeFile,
			Op:       metanet.OpCreate,
			MimeType: "text/plain",
			FileSize: uint64(i * 100),
		})
		require.NoError(t, err)

		childBatch := tx.NewMutationBatch()
		childBatch.AddCreateChild(childKey.PublicKey, currentParentTxID, childPayload, currentParentUTXO, rootKey.PrivateKey)
		childBatch.AddFeeInput(&tx.UTXO{TxID: bytes.Repeat([]byte{byte(i)}, 32), Vout: 0, Amount: 500000})
		childBatch.SetFeeRate(1)

		childResult, err := childBatch.Build()
		require.NoError(t, err, "child %d build should succeed", i)
		require.Len(t, childResult.NodeOps, 1, "child %d must have 1 node op", i)
		require.NotNil(t, childResult.NodeOps[0].NodeUTXO, "child %d must have NodeUTXO", i)
		assert.Equal(t, tx.DustLimit, childResult.NodeOps[0].NodeUTXO.Amount)

		// Simulate refreshed parent UTXO for next child.
		currentParentTxID = bytes.Repeat([]byte{byte(0xa0 + i)}, 32)
		currentParentUTXO = &tx.UTXO{
			TxID:       currentParentTxID,
			Vout:       1, // simulated
			Amount:     tx.DustLimit,
			PrivateKey: rootKey.PrivateKey,
		}
	}
}

// ---------------------------------------------------------------------------
// 20. TestMerkleProofOddTxCount
// ---------------------------------------------------------------------------

func TestMerkleProofOddTxCount(t *testing.T) {
	txHashes := makeTxHashes(3)
	merkleRoot := spv.ComputeMerkleRootFromTxList(txHashes)
	require.NotNil(t, merkleRoot)

	for idx := uint32(0); idx < 3; idx++ {
		proof := buildMerkleProofForIndex(txHashes, idx)
		valid, err := spv.VerifyMerkleProof(proof, merkleRoot)
		require.NoError(t, err, "proof for tx[%d] should not error", idx)
		assert.True(t, valid, "proof for tx[%d] should verify", idx)
	}
}

// ---------------------------------------------------------------------------
// 21. TestMerkleProofLargeTree
// ---------------------------------------------------------------------------

func TestMerkleProofLargeTree(t *testing.T) {
	txHashes := makeTxHashes(16)
	merkleRoot := spv.ComputeMerkleRootFromTxList(txHashes)
	require.NotNil(t, merkleRoot)

	for _, idx := range []uint32{0, 7, 15} {
		proof := buildMerkleProofForIndex(txHashes, idx)
		valid, err := spv.VerifyMerkleProof(proof, merkleRoot)
		require.NoError(t, err, "proof for tx[%d]", idx)
		assert.True(t, valid, "proof for tx[%d] should verify", idx)
	}
}

// ---------------------------------------------------------------------------
// 22. TestMerkleProofPowerOfTwo
// ---------------------------------------------------------------------------

func TestMerkleProofPowerOfTwo(t *testing.T) {
	txHashes := makeTxHashes(8)
	merkleRoot := spv.ComputeMerkleRootFromTxList(txHashes)
	require.NotNil(t, merkleRoot)

	for idx := uint32(0); idx < 8; idx++ {
		proof := buildMerkleProofForIndex(txHashes, idx)
		valid, err := spv.VerifyMerkleProof(proof, merkleRoot)
		require.NoError(t, err, "proof for tx[%d]", idx)
		assert.True(t, valid, "proof for tx[%d] should verify", idx)
	}
}

// ---------------------------------------------------------------------------
// 23. TestMerkleRootDeterminism
// ---------------------------------------------------------------------------

func TestMerkleRootDeterminism(t *testing.T) {
	txHashes := makeTxHashes(4)
	root1 := spv.ComputeMerkleRootFromTxList(txHashes)
	root2 := spv.ComputeMerkleRootFromTxList(txHashes)
	assert.Equal(t, root1, root2, "same input must yield same merkle root")
}

// ---------------------------------------------------------------------------
// 24. TestMerkleRootOrderMatters
// ---------------------------------------------------------------------------

func TestMerkleRootOrderMatters(t *testing.T) {
	txHashes := makeTxHashes(4)

	root1 := spv.ComputeMerkleRootFromTxList(txHashes)

	// Swap first two elements.
	swapped := copyHashes(txHashes)
	swapped[0], swapped[1] = swapped[1], swapped[0]
	root2 := spv.ComputeMerkleRootFromTxList(swapped)

	assert.NotEqual(t, root1, root2, "different tx order must produce different root")
}

// ---------------------------------------------------------------------------
// 25. TestHeaderSerializationRoundTrip
// ---------------------------------------------------------------------------

func TestHeaderSerializationRoundTrip(t *testing.T) {
	original := &spv.BlockHeader{
		Version:    2,
		PrevBlock:  bytes.Repeat([]byte{0x11}, 32),
		MerkleRoot: bytes.Repeat([]byte{0x22}, 32),
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      42,
		Height:     500,
	}
	original.Hash = spv.ComputeHeaderHash(original)

	data := spv.SerializeHeader(original)
	require.Len(t, data, 80, "serialized header must be 80 bytes")

	restored, err := spv.DeserializeHeader(data)
	require.NoError(t, err)

	assert.Equal(t, original.Version, restored.Version)
	assert.Equal(t, original.PrevBlock, restored.PrevBlock)
	assert.Equal(t, original.MerkleRoot, restored.MerkleRoot)
	assert.Equal(t, original.Timestamp, restored.Timestamp)
	assert.Equal(t, original.Bits, restored.Bits)
	assert.Equal(t, original.Nonce, restored.Nonce)

	// DeserializeHeader computes Hash from the raw data.
	assert.Equal(t, original.Hash, restored.Hash)
}

// ---------------------------------------------------------------------------
// 26. TestHeaderHashDeterminism
// ---------------------------------------------------------------------------

func TestHeaderHashDeterminism(t *testing.T) {
	h := &spv.BlockHeader{
		Version:    1,
		PrevBlock:  bytes.Repeat([]byte{0x00}, 32),
		MerkleRoot: bytes.Repeat([]byte{0xab}, 32),
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      99999,
	}

	hash1 := spv.ComputeHeaderHash(h)
	hash2 := spv.ComputeHeaderHash(h)
	assert.Equal(t, hash1, hash2, "same header must yield same hash")
}

// ---------------------------------------------------------------------------
// 27. TestHeaderHashChangesWithFields
// ---------------------------------------------------------------------------

func TestHeaderHashChangesWithFields(t *testing.T) {
	base := &spv.BlockHeader{
		Version:    1,
		PrevBlock:  bytes.Repeat([]byte{0x00}, 32),
		MerkleRoot: bytes.Repeat([]byte{0xab}, 32),
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      99999,
	}
	baseHash := spv.ComputeHeaderHash(base)

	// Modify Version.
	modified := *base
	modified.Version = 2
	assert.NotEqual(t, baseHash, spv.ComputeHeaderHash(&modified), "different Version -> different hash")

	// Modify Timestamp.
	modified = *base
	modified.Timestamp = 1700000001
	assert.NotEqual(t, baseHash, spv.ComputeHeaderHash(&modified), "different Timestamp -> different hash")

	// Modify Nonce.
	modified = *base
	modified.Nonce = 100000
	assert.NotEqual(t, baseHash, spv.ComputeHeaderHash(&modified), "different Nonce -> different hash")
}

// ---------------------------------------------------------------------------
// 28. TestHeaderChainValid
// ---------------------------------------------------------------------------

func TestHeaderChainValid(t *testing.T) {
	headers := make([]*spv.BlockHeader, 5)

	prevBlock := bytes.Repeat([]byte{0x00}, 32)
	for i := 0; i < 5; i++ {
		headers[i] = makeBlockHeader(
			1,                                  // version
			prevBlock,                          // prevBlock
			bytes.Repeat([]byte{byte(i)}, 32),  // merkleRoot
			uint32(1700000000+i*600),           // timestamp
			0x2100ffff,                         // bits — easy PoW target for synthetic headers
			uint32(i*1000),                     // nonce
			uint32(100+i),                      // height
		)
		prevBlock = headers[i].Hash
	}

	err := spv.VerifyHeaderChain(headers)
	assert.NoError(t, err, "valid chain of 5 headers should pass")
}

// ---------------------------------------------------------------------------
// 29. TestHeaderChainBrokenLink
// ---------------------------------------------------------------------------

func TestHeaderChainBrokenLink(t *testing.T) {
	headers := make([]*spv.BlockHeader, 3)

	prevBlock := bytes.Repeat([]byte{0x00}, 32)
	for i := 0; i < 3; i++ {
		headers[i] = makeBlockHeader(
			1,
			prevBlock,
			bytes.Repeat([]byte{byte(0x10 + i)}, 32),
			uint32(1700000000+i*600),
			0x2100ffff, // easy PoW target for synthetic headers
			uint32(i*1000),
			uint32(100+i),
		)
		prevBlock = headers[i].Hash
	}

	// Corrupt the middle header's PrevBlock.
	headers[1].PrevBlock = bytes.Repeat([]byte{0xff}, 32)
	// Recompute hash after mutation so Hash itself is consistent with fields,
	// but the chain link is broken.
	headers[1].Hash = spv.ComputeHeaderHash(headers[1])

	err := spv.VerifyHeaderChain(headers)
	assert.ErrorIs(t, err, spv.ErrChainBroken)
}

// ---------------------------------------------------------------------------
// 30. TestHeaderChainSingleHeader
// ---------------------------------------------------------------------------

func TestHeaderChainSingleHeader(t *testing.T) {
	h := makeBlockHeader(1, bytes.Repeat([]byte{0x00}, 32), bytes.Repeat([]byte{0xab}, 32), 1700000000, 0x2100ffff, 0, 100)
	err := spv.VerifyHeaderChain([]*spv.BlockHeader{h})
	assert.NoError(t, err, "single header should pass verification")
}

// ---------------------------------------------------------------------------
// 31. TestMemHeaderStoreGetByHeight
// ---------------------------------------------------------------------------

func TestMemHeaderStoreGetByHeight(t *testing.T) {
	store := spv.NewMemHeaderStore()

	headers := make([]*spv.BlockHeader, 3)
	for i := 0; i < 3; i++ {
		headers[i] = makeBlockHeader(
			1,
			bytes.Repeat([]byte{byte(i)}, 32),
			bytes.Repeat([]byte{byte(0x10 + i)}, 32),
			uint32(1700000000+i*600),
			0x1d00ffff,
			uint32(i),
			uint32(100+i),
		)
		err := store.PutHeader(headers[i])
		require.NoError(t, err)
	}

	got, err := store.GetHeaderByHeight(101)
	require.NoError(t, err)
	assert.Equal(t, headers[1].Hash, got.Hash)
	assert.Equal(t, uint32(101), got.Height)
}

// ---------------------------------------------------------------------------
// 32. TestMemHeaderStoreGetTipIsHighest
// ---------------------------------------------------------------------------

func TestMemHeaderStoreGetTipIsHighest(t *testing.T) {
	store := spv.NewMemHeaderStore()

	// Insert out of order: 100, 102, 101.
	heights := []uint32{100, 102, 101}
	for i, h := range heights {
		hdr := makeBlockHeader(
			1,
			bytes.Repeat([]byte{byte(i)}, 32),
			bytes.Repeat([]byte{byte(0x20 + i)}, 32),
			uint32(1700000000+i*600),
			0x1d00ffff,
			uint32(i),
			h,
		)
		err := store.PutHeader(hdr)
		require.NoError(t, err)
	}

	tip, err := store.GetTip()
	require.NoError(t, err)
	assert.Equal(t, uint32(102), tip.Height, "tip should be the highest height")
}

// ---------------------------------------------------------------------------
// 33. TestMemHeaderStoreDuplicateHeader
// ---------------------------------------------------------------------------

func TestMemHeaderStoreDuplicateHeader(t *testing.T) {
	store := spv.NewMemHeaderStore()
	h := makeBlockHeader(1, bytes.Repeat([]byte{0x00}, 32), bytes.Repeat([]byte{0xab}, 32), 1700000000, 0x1d00ffff, 0, 100)

	err := store.PutHeader(h)
	require.NoError(t, err)

	err = store.PutHeader(h)
	assert.ErrorIs(t, err, spv.ErrDuplicateHeader, "putting same header twice should return ErrDuplicateHeader")
}

// ---------------------------------------------------------------------------
// 34. TestDoubleHashCorrectness
// ---------------------------------------------------------------------------

func TestDoubleHashCorrectness(t *testing.T) {
	data := []byte("The quick brown fox jumps over the lazy dog")

	result := spv.DoubleHash(data)

	// Manual computation.
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])

	assert.Equal(t, second[:], result, "DoubleHash must equal SHA256(SHA256(data))")
}

// ---------------------------------------------------------------------------
// 35. TestVerifyTransactionTxIDMismatch
// ---------------------------------------------------------------------------

func TestVerifyTransactionTxIDMismatch(t *testing.T) {
	txHashes := makeTxHashes(4)
	merkleRoot := spv.ComputeMerkleRootFromTxList(txHashes)

	header := makeBlockHeader(1, bytes.Repeat([]byte{0x00}, 32), merkleRoot, 1700000000, 0x1d00ffff, 12345, 100)

	store := spv.NewMemHeaderStore()
	err := store.PutHeader(header)
	require.NoError(t, err)

	proof := buildMerkleProofForIndex(txHashes, 0)
	proof.BlockHash = header.Hash

	// StoredTx.TxID differs from Proof.TxID
	mismatchedTxID := bytes.Repeat([]byte{0xde}, 32)
	storedTx := &spv.StoredTx{
		TxID:        mismatchedTxID,
		RawTx:       []byte("mock-raw-tx"),
		Proof:       proof,
		BlockHeight: 100,
	}

	err = spv.VerifyTransaction(storedTx, store)
	assert.Error(t, err, "TxID mismatch between StoredTx and Proof should fail")
}

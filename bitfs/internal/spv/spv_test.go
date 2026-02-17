package spv

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper functions ---

func makeHash(seed byte) []byte {
	h := make([]byte, 32)
	for i := range h {
		h[i] = seed
	}
	return h
}

func makeTxHash(seed byte) []byte {
	data := []byte{seed}
	return DoubleHash(data)
}

// buildTestProof builds a valid Merkle proof for a single-tx block.
func buildTestProof(txHash []byte) (*MerkleProof, []byte) {
	// For a block with 2 txs, proof for tx at index 0 needs the other tx as the only node
	otherTx := makeTxHash(0x99)
	combined := make([]byte, 64)
	copy(combined[:32], txHash)
	copy(combined[32:], otherTx)
	merkleRoot := DoubleHash(combined)

	proof := &MerkleProof{
		TxID:      txHash,
		Index:     0,
		Nodes:     [][]byte{otherTx},
		BlockHash: makeHash(0xBB),
	}

	return proof, merkleRoot
}

func buildTestHeader(height uint32, prevBlock, merkleRoot []byte) *BlockHeader {
	h := &BlockHeader{
		Version:    1,
		PrevBlock:  prevBlock,
		MerkleRoot: merkleRoot,
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      12345,
		Height:     height,
	}
	h.Hash = ComputeHeaderHash(h)
	return h
}

// --- DoubleHash tests ---

func TestDoubleHash(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty data", []byte{}},
		{"single byte", []byte{0x42}},
		{"32 bytes", bytes.Repeat([]byte{0xAA}, 32)},
		{"large data", bytes.Repeat([]byte{0xFF}, 1000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DoubleHash(tt.data)
			assert.Len(t, result, 32)

			// Verify manually: SHA256(SHA256(data))
			first := sha256.Sum256(tt.data)
			second := sha256.Sum256(first[:])
			assert.Equal(t, second[:], result)
		})
	}
}

func TestDoubleHash_Deterministic(t *testing.T) {
	data := []byte("bitcoin transaction data")
	h1 := DoubleHash(data)
	h2 := DoubleHash(data)
	assert.Equal(t, h1, h2, "DoubleHash should be deterministic")
}

func TestDoubleHash_DifferentInputs(t *testing.T) {
	h1 := DoubleHash([]byte("data1"))
	h2 := DoubleHash([]byte("data2"))
	assert.NotEqual(t, h1, h2, "different inputs should produce different hashes")
}

// --- SerializeHeader / DeserializeHeader tests ---

func TestSerializeHeader(t *testing.T) {
	h := &BlockHeader{
		Version:    2,
		PrevBlock:  makeHash(0xAA),
		MerkleRoot: makeHash(0xBB),
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      42,
	}

	data := SerializeHeader(h)
	assert.Len(t, data, BlockHeaderSize)

	// Verify version
	version := int32(binary.LittleEndian.Uint32(data[0:4]))
	assert.Equal(t, int32(2), version)

	// Verify prevBlock
	assert.Equal(t, makeHash(0xAA), data[4:36])

	// Verify merkleRoot
	assert.Equal(t, makeHash(0xBB), data[36:68])

	// Verify timestamp
	ts := binary.LittleEndian.Uint32(data[68:72])
	assert.Equal(t, uint32(1700000000), ts)

	// Verify bits
	bits := binary.LittleEndian.Uint32(data[72:76])
	assert.Equal(t, uint32(0x1d00ffff), bits)

	// Verify nonce
	nonce := binary.LittleEndian.Uint32(data[76:80])
	assert.Equal(t, uint32(42), nonce)
}

func TestSerializeHeader_Nil(t *testing.T) {
	result := SerializeHeader(nil)
	assert.Nil(t, result)
}

func TestDeserializeHeader(t *testing.T) {
	original := &BlockHeader{
		Version:    536870912,
		PrevBlock:  makeHash(0x11),
		MerkleRoot: makeHash(0x22),
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      987654321,
	}

	data := SerializeHeader(original)
	decoded, err := DeserializeHeader(data)
	require.NoError(t, err)

	assert.Equal(t, original.Version, decoded.Version)
	assert.Equal(t, original.PrevBlock, decoded.PrevBlock)
	assert.Equal(t, original.MerkleRoot, decoded.MerkleRoot)
	assert.Equal(t, original.Timestamp, decoded.Timestamp)
	assert.Equal(t, original.Bits, decoded.Bits)
	assert.Equal(t, original.Nonce, decoded.Nonce)
	assert.Len(t, decoded.Hash, 32, "Hash should be computed")
}

func TestDeserializeHeader_InvalidLength(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"too short", make([]byte, 79)},
		{"too long", make([]byte, 81)},
		{"half", make([]byte, 40)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DeserializeHeader(tt.data)
			assert.ErrorIs(t, err, ErrInvalidHeader)
		})
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	h := &BlockHeader{
		Version:    1,
		PrevBlock:  makeTxHash(0x01),
		MerkleRoot: makeTxHash(0x02),
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      0xdeadbeef,
	}

	data := SerializeHeader(h)
	decoded, err := DeserializeHeader(data)
	require.NoError(t, err)

	assert.Equal(t, h.Version, decoded.Version)
	assert.Equal(t, h.PrevBlock, decoded.PrevBlock)
	assert.Equal(t, h.MerkleRoot, decoded.MerkleRoot)
	assert.Equal(t, h.Timestamp, decoded.Timestamp)
	assert.Equal(t, h.Bits, decoded.Bits)
	assert.Equal(t, h.Nonce, decoded.Nonce)
}

func TestComputeHeaderHash(t *testing.T) {
	h := &BlockHeader{
		Version:    1,
		PrevBlock:  makeHash(0x00),
		MerkleRoot: makeHash(0x11),
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      42,
	}

	hash := ComputeHeaderHash(h)
	assert.Len(t, hash, 32)

	// Same header should produce same hash
	hash2 := ComputeHeaderHash(h)
	assert.Equal(t, hash, hash2)
}

func TestComputeHeaderHash_Nil(t *testing.T) {
	hash := ComputeHeaderHash(nil)
	assert.Nil(t, hash)
}

// --- ComputeMerkleRoot tests ---

func TestComputeMerkleRoot_SingleProofNode(t *testing.T) {
	txHash := makeTxHash(0x01)
	sibling := makeTxHash(0x02)

	// Index 0: txHash is on the left
	root := ComputeMerkleRoot(txHash, 0, [][]byte{sibling})
	assert.Len(t, root, 32)

	// Manual computation
	combined := make([]byte, 64)
	copy(combined[:32], txHash)
	copy(combined[32:], sibling)
	expected := DoubleHash(combined)
	assert.Equal(t, expected, root)
}

func TestComputeMerkleRoot_IndexOnRight(t *testing.T) {
	txHash := makeTxHash(0x01)
	sibling := makeTxHash(0x02)

	// Index 1: txHash is on the right
	root := ComputeMerkleRoot(txHash, 1, [][]byte{sibling})
	assert.Len(t, root, 32)

	combined := make([]byte, 64)
	copy(combined[:32], sibling)
	copy(combined[32:], txHash)
	expected := DoubleHash(combined)
	assert.Equal(t, expected, root)
}

func TestComputeMerkleRoot_TwoLevels(t *testing.T) {
	// Build a 4-tx tree and verify proof for tx at index 2
	tx0 := makeTxHash(0x10)
	tx1 := makeTxHash(0x11)
	tx2 := makeTxHash(0x12) // target
	tx3 := makeTxHash(0x13)

	// Level 0 pairs: (tx0, tx1) and (tx2, tx3)
	pair01 := make([]byte, 64)
	copy(pair01[:32], tx0)
	copy(pair01[32:], tx1)
	h01 := DoubleHash(pair01)

	pair23 := make([]byte, 64)
	copy(pair23[:32], tx2)
	copy(pair23[32:], tx3)
	h23 := DoubleHash(pair23)

	// Root = hash(h01, h23)
	rootPair := make([]byte, 64)
	copy(rootPair[:32], h01)
	copy(rootPair[32:], h23)
	expectedRoot := DoubleHash(rootPair)

	// Proof for tx2 (index 2 = binary 10):
	// Level 0: tx2 is left in its pair (bit 0 of index is 0), sibling = tx3
	// Level 1: h23 is right in root pair (bit 1 of index is 1), sibling = h01
	proofNodes := [][]byte{tx3, h01}
	computedRoot := ComputeMerkleRoot(tx2, 2, proofNodes)

	assert.Equal(t, expectedRoot, computedRoot)
}

func TestComputeMerkleRoot_InvalidTxHash(t *testing.T) {
	result := ComputeMerkleRoot([]byte{0x01}, 0, [][]byte{makeHash(0xAA)})
	assert.Nil(t, result, "should return nil for invalid txHash length")
}

func TestComputeMerkleRoot_InvalidProofNode(t *testing.T) {
	txHash := makeHash(0x01)
	result := ComputeMerkleRoot(txHash, 0, [][]byte{{0x01, 0x02}})
	assert.Nil(t, result, "should return nil for invalid proof node length")
}

func TestComputeMerkleRoot_EmptyProofNodes(t *testing.T) {
	txHash := makeHash(0x01)
	result := ComputeMerkleRoot(txHash, 0, nil)
	// With no proof nodes, the root is just the txHash itself
	assert.Equal(t, txHash, result)
}

// --- BuildMerkleTree tests ---

func TestBuildMerkleTree_SingleTx(t *testing.T) {
	txHash := makeTxHash(0x01)
	tree := BuildMerkleTree([][]byte{txHash})
	require.NotNil(t, tree)
	// Single tx: tree is just the tx itself (no hashing needed since it's odd, it duplicates)
	// Actually: single element, odd pad -> [txHash, txHash] -> hash(txHash || txHash) = root
	// Wait: BuildMerkleTree returns final level. Let me check.
	// For single tx: len(level)=1 which is >1 false, so it returns [txHash]
	assert.Len(t, tree, 1)
	assert.Equal(t, txHash, tree[0])
}

func TestBuildMerkleTree_TwoTx(t *testing.T) {
	tx0 := makeTxHash(0x01)
	tx1 := makeTxHash(0x02)

	tree := BuildMerkleTree([][]byte{tx0, tx1})
	require.NotNil(t, tree)
	assert.Len(t, tree, 1) // Single root

	// Manual: root = hash(tx0 || tx1)
	combined := make([]byte, 64)
	copy(combined[:32], tx0)
	copy(combined[32:], tx1)
	expected := DoubleHash(combined)
	assert.Equal(t, expected, tree[0])
}

func TestBuildMerkleTree_FourTx(t *testing.T) {
	txs := make([][]byte, 4)
	for i := range txs {
		txs[i] = makeTxHash(byte(i))
	}

	tree := BuildMerkleTree(txs)
	require.NotNil(t, tree)
	assert.Len(t, tree, 1)

	// Compute expected root manually
	p01 := make([]byte, 64)
	copy(p01[:32], txs[0])
	copy(p01[32:], txs[1])
	h01 := DoubleHash(p01)

	p23 := make([]byte, 64)
	copy(p23[:32], txs[2])
	copy(p23[32:], txs[3])
	h23 := DoubleHash(p23)

	rootPair := make([]byte, 64)
	copy(rootPair[:32], h01)
	copy(rootPair[32:], h23)
	expected := DoubleHash(rootPair)

	assert.Equal(t, expected, tree[0])
}

func TestBuildMerkleTree_ThreeTx_OddPadding(t *testing.T) {
	txs := make([][]byte, 3)
	for i := range txs {
		txs[i] = makeTxHash(byte(i + 10))
	}

	tree := BuildMerkleTree(txs)
	require.NotNil(t, tree)
	assert.Len(t, tree, 1)

	// With 3 txs, the 4th is a duplicate of the 3rd
	p01 := make([]byte, 64)
	copy(p01[:32], txs[0])
	copy(p01[32:], txs[1])
	h01 := DoubleHash(p01)

	p23 := make([]byte, 64)
	copy(p23[:32], txs[2])
	copy(p23[32:], txs[2]) // duplicated
	h23 := DoubleHash(p23)

	rootPair := make([]byte, 64)
	copy(rootPair[:32], h01)
	copy(rootPair[32:], h23)
	expected := DoubleHash(rootPair)

	assert.Equal(t, expected, tree[0])
}

func TestBuildMerkleTree_Empty(t *testing.T) {
	tree := BuildMerkleTree(nil)
	assert.Nil(t, tree)
}

func TestComputeMerkleRootFromTxList(t *testing.T) {
	txs := make([][]byte, 4)
	for i := range txs {
		txs[i] = makeTxHash(byte(i))
	}

	root := ComputeMerkleRootFromTxList(txs)
	assert.Len(t, root, 32)

	// Should match BuildMerkleTree result
	tree := BuildMerkleTree(txs)
	assert.Equal(t, tree[0], root)
}

func TestComputeMerkleRootFromTxList_Empty(t *testing.T) {
	root := ComputeMerkleRootFromTxList(nil)
	assert.Nil(t, root)
}

// --- VerifyMerkleProof tests ---

func TestVerifyMerkleProof_Valid(t *testing.T) {
	txHash := makeTxHash(0x42)
	proof, merkleRoot := buildTestProof(txHash)

	valid, err := VerifyMerkleProof(proof, merkleRoot)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestVerifyMerkleProof_InvalidRoot(t *testing.T) {
	txHash := makeTxHash(0x42)
	proof, _ := buildTestProof(txHash)

	wrongRoot := makeHash(0xFF)
	valid, err := VerifyMerkleProof(proof, wrongRoot)
	assert.ErrorIs(t, err, ErrMerkleProofInvalid)
	assert.False(t, valid)
}

func TestVerifyMerkleProof_NilProof(t *testing.T) {
	_, err := VerifyMerkleProof(nil, makeHash(0x00))
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestVerifyMerkleProof_InvalidTxIDLength(t *testing.T) {
	proof := &MerkleProof{
		TxID:  []byte{0x01, 0x02}, // too short
		Index: 0,
		Nodes: [][]byte{makeHash(0xAA)},
	}
	_, err := VerifyMerkleProof(proof, makeHash(0x00))
	assert.ErrorIs(t, err, ErrInvalidTxID)
}

func TestVerifyMerkleProof_EmptyNodes(t *testing.T) {
	proof := &MerkleProof{
		TxID:  makeHash(0x01),
		Index: 0,
		Nodes: nil,
	}
	_, err := VerifyMerkleProof(proof, makeHash(0x00))
	assert.ErrorIs(t, err, ErrEmptyProofNodes)
}

func TestVerifyMerkleProof_InvalidExpectedRootLength(t *testing.T) {
	proof := &MerkleProof{
		TxID:  makeHash(0x01),
		Index: 0,
		Nodes: [][]byte{makeHash(0xAA)},
	}
	_, err := VerifyMerkleProof(proof, []byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidHeader)
}

// --- VerifyHeaderChain tests ---

func TestVerifyHeaderChain_Valid(t *testing.T) {
	h1 := buildTestHeader(1, makeHash(0x00), makeHash(0x11))
	h2 := buildTestHeader(2, h1.Hash, makeHash(0x22))
	h3 := buildTestHeader(3, h2.Hash, makeHash(0x33))

	err := VerifyHeaderChain([]*BlockHeader{h1, h2, h3})
	assert.NoError(t, err)
}

func TestVerifyHeaderChain_Broken(t *testing.T) {
	h1 := buildTestHeader(1, makeHash(0x00), makeHash(0x11))
	h2 := buildTestHeader(2, makeHash(0xFF), makeHash(0x22)) // wrong prevBlock

	err := VerifyHeaderChain([]*BlockHeader{h1, h2})
	assert.ErrorIs(t, err, ErrChainBroken)
}

func TestVerifyHeaderChain_Empty(t *testing.T) {
	err := VerifyHeaderChain(nil)
	assert.NoError(t, err)
}

func TestVerifyHeaderChain_Single(t *testing.T) {
	h := buildTestHeader(1, makeHash(0x00), makeHash(0x11))
	err := VerifyHeaderChain([]*BlockHeader{h})
	assert.NoError(t, err)
}

func TestVerifyHeaderChain_NilHeader(t *testing.T) {
	h1 := buildTestHeader(1, makeHash(0x00), makeHash(0x11))
	err := VerifyHeaderChain([]*BlockHeader{h1, nil})
	assert.ErrorIs(t, err, ErrNilParam)
}

// --- VerifyTransaction tests ---

func TestVerifyTransaction_Valid(t *testing.T) {
	txHash := makeTxHash(0x42)
	proof, merkleRoot := buildTestProof(txHash)

	header := buildTestHeader(100, makeHash(0x00), merkleRoot)

	// Update proof block hash to match header hash
	proof.BlockHash = header.Hash

	headerStore := NewMemHeaderStore()
	err := headerStore.PutHeader(header)
	require.NoError(t, err)

	storedTx := &StoredTx{
		TxID:        txHash,
		RawTx:       []byte("fake raw tx"),
		Proof:       proof,
		BlockHeight: 100,
	}

	err = VerifyTransaction(storedTx, headerStore)
	assert.NoError(t, err)
}

func TestVerifyTransaction_NilTx(t *testing.T) {
	err := VerifyTransaction(nil, NewMemHeaderStore())
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestVerifyTransaction_NilHeaderStore(t *testing.T) {
	tx := &StoredTx{TxID: makeHash(0x01)}
	err := VerifyTransaction(tx, nil)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestVerifyTransaction_InvalidTxID(t *testing.T) {
	tx := &StoredTx{TxID: []byte{0x01}}
	err := VerifyTransaction(tx, NewMemHeaderStore())
	assert.ErrorIs(t, err, ErrInvalidTxID)
}

func TestVerifyTransaction_Unconfirmed(t *testing.T) {
	tx := &StoredTx{
		TxID:  makeHash(0x01),
		RawTx: []byte("tx data"),
		Proof: nil, // unconfirmed
	}
	err := VerifyTransaction(tx, NewMemHeaderStore())
	assert.ErrorIs(t, err, ErrUnconfirmed)
}

func TestVerifyTransaction_MismatchTxID(t *testing.T) {
	tx := &StoredTx{
		TxID:  makeHash(0x01),
		RawTx: []byte("tx data"),
		Proof: &MerkleProof{
			TxID:      makeHash(0x02), // different from StoredTx.TxID
			Index:     0,
			Nodes:     [][]byte{makeHash(0xAA)},
			BlockHash: makeHash(0xBB),
		},
	}
	err := VerifyTransaction(tx, NewMemHeaderStore())
	assert.ErrorIs(t, err, ErrMerkleProofInvalid)
}

func TestVerifyTransaction_HeaderNotFound(t *testing.T) {
	txHash := makeTxHash(0x42)

	tx := &StoredTx{
		TxID:  txHash,
		RawTx: []byte("tx data"),
		Proof: &MerkleProof{
			TxID:      txHash,
			Index:     0,
			Nodes:     [][]byte{makeHash(0xAA)},
			BlockHash: makeHash(0xBB),
		},
	}
	err := VerifyTransaction(tx, NewMemHeaderStore())
	assert.ErrorIs(t, err, ErrHeaderNotFound)
}

// --- MemHeaderStore tests ---

func TestMemHeaderStore_PutAndGet(t *testing.T) {
	store := NewMemHeaderStore()
	h := buildTestHeader(100, makeHash(0x00), makeHash(0x11))

	err := store.PutHeader(h)
	require.NoError(t, err)

	got, err := store.GetHeader(h.Hash)
	require.NoError(t, err)
	assert.Equal(t, h, got)
}

func TestMemHeaderStore_GetByHeight(t *testing.T) {
	store := NewMemHeaderStore()
	h := buildTestHeader(42, makeHash(0x00), makeHash(0x11))

	err := store.PutHeader(h)
	require.NoError(t, err)

	got, err := store.GetHeaderByHeight(42)
	require.NoError(t, err)
	assert.Equal(t, h, got)
}

func TestMemHeaderStore_GetByHeight_NotFound(t *testing.T) {
	store := NewMemHeaderStore()
	_, err := store.GetHeaderByHeight(999)
	assert.ErrorIs(t, err, ErrHeaderNotFound)
}

func TestMemHeaderStore_GetTip(t *testing.T) {
	store := NewMemHeaderStore()

	h1 := buildTestHeader(10, makeHash(0x01), makeHash(0x11))
	h2 := buildTestHeader(20, makeHash(0x02), makeHash(0x22))
	h3 := buildTestHeader(15, makeHash(0x03), makeHash(0x33))

	require.NoError(t, store.PutHeader(h1))
	require.NoError(t, store.PutHeader(h2))
	require.NoError(t, store.PutHeader(h3))

	tip, err := store.GetTip()
	require.NoError(t, err)
	assert.Equal(t, uint32(20), tip.Height)
}

func TestMemHeaderStore_GetTip_Empty(t *testing.T) {
	store := NewMemHeaderStore()
	_, err := store.GetTip()
	assert.ErrorIs(t, err, ErrHeaderNotFound)
}

func TestMemHeaderStore_GetHeaderCount(t *testing.T) {
	store := NewMemHeaderStore()
	count, err := store.GetHeaderCount()
	require.NoError(t, err)
	assert.Equal(t, uint64(0), count)

	require.NoError(t, store.PutHeader(buildTestHeader(1, makeHash(0x01), makeHash(0x11))))
	require.NoError(t, store.PutHeader(buildTestHeader(2, makeHash(0x02), makeHash(0x22))))

	count, err = store.GetHeaderCount()
	require.NoError(t, err)
	assert.Equal(t, uint64(2), count)
}

func TestMemHeaderStore_PutNil(t *testing.T) {
	store := NewMemHeaderStore()
	err := store.PutHeader(nil)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestMemHeaderStore_Duplicate(t *testing.T) {
	store := NewMemHeaderStore()
	h := buildTestHeader(1, makeHash(0x01), makeHash(0x11))

	require.NoError(t, store.PutHeader(h))
	err := store.PutHeader(h)
	assert.ErrorIs(t, err, ErrDuplicateHeader)
}

func TestMemHeaderStore_GetInvalidHashLength(t *testing.T) {
	store := NewMemHeaderStore()
	_, err := store.GetHeader([]byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidHeader)
}

// --- MemTxStore tests ---

func TestMemTxStore_PutAndGet(t *testing.T) {
	store := NewMemTxStore()
	tx := &StoredTx{
		TxID:  makeHash(0x01),
		RawTx: []byte("raw transaction data"),
	}

	err := store.PutTx(tx)
	require.NoError(t, err)

	got, err := store.GetTx(tx.TxID)
	require.NoError(t, err)
	assert.Equal(t, tx, got)
}

func TestMemTxStore_GetNotFound(t *testing.T) {
	store := NewMemTxStore()
	_, err := store.GetTx(makeHash(0xFF))
	assert.ErrorIs(t, err, ErrTxNotFound)
}

func TestMemTxStore_PutNilTx(t *testing.T) {
	store := NewMemTxStore()
	err := store.PutTx(nil)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestMemTxStore_PutInvalidTxID(t *testing.T) {
	store := NewMemTxStore()
	tx := &StoredTx{TxID: []byte{0x01}}
	err := store.PutTx(tx)
	assert.ErrorIs(t, err, ErrInvalidTxID)
}

func TestMemTxStore_Duplicate(t *testing.T) {
	store := NewMemTxStore()
	tx := &StoredTx{
		TxID:  makeHash(0x01),
		RawTx: []byte("data"),
	}

	require.NoError(t, store.PutTx(tx))
	err := store.PutTx(tx)
	assert.ErrorIs(t, err, ErrDuplicateTx)
}

func TestMemTxStore_Delete(t *testing.T) {
	store := NewMemTxStore()
	tx := &StoredTx{
		TxID:  makeHash(0x01),
		RawTx: []byte("data"),
	}

	require.NoError(t, store.PutTx(tx))
	err := store.DeleteTx(tx.TxID)
	require.NoError(t, err)

	_, err = store.GetTx(tx.TxID)
	assert.ErrorIs(t, err, ErrTxNotFound)
}

func TestMemTxStore_DeleteNotFound(t *testing.T) {
	store := NewMemTxStore()
	err := store.DeleteTx(makeHash(0xFF))
	assert.ErrorIs(t, err, ErrTxNotFound)
}

func TestMemTxStore_DeleteInvalidTxID(t *testing.T) {
	store := NewMemTxStore()
	err := store.DeleteTx([]byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidTxID)
}

func TestMemTxStore_ListTxs(t *testing.T) {
	store := NewMemTxStore()

	tx1 := &StoredTx{TxID: makeHash(0x01), RawTx: []byte("tx1")}
	tx2 := &StoredTx{TxID: makeHash(0x02), RawTx: []byte("tx2")}

	require.NoError(t, store.PutTx(tx1))
	require.NoError(t, store.PutTx(tx2))

	txs, err := store.ListTxs()
	require.NoError(t, err)
	assert.Len(t, txs, 2)
}

func TestMemTxStore_ListTxs_Empty(t *testing.T) {
	store := NewMemTxStore()
	txs, err := store.ListTxs()
	require.NoError(t, err)
	assert.Empty(t, txs)
}

func TestMemTxStore_PutWithPubKey(t *testing.T) {
	store := NewMemTxStore()
	pNode := makeHash(0xAA)

	tx1 := &StoredTx{TxID: makeHash(0x01), RawTx: []byte("tx1")}
	tx2 := &StoredTx{TxID: makeHash(0x02), RawTx: []byte("tx2")}

	require.NoError(t, store.PutTxWithPubKey(tx1, pNode))
	require.NoError(t, store.PutTxWithPubKey(tx2, pNode))

	txs, err := store.GetTxsByPubKey(pNode)
	require.NoError(t, err)
	assert.Len(t, txs, 2)
}

func TestMemTxStore_GetTxsByPubKey_NotFound(t *testing.T) {
	store := NewMemTxStore()
	txs, err := store.GetTxsByPubKey(makeHash(0xFF))
	require.NoError(t, err)
	assert.Nil(t, txs)
}

func TestMemTxStore_GetTxsByPubKey_EmptyPNode(t *testing.T) {
	store := NewMemTxStore()
	_, err := store.GetTxsByPubKey(nil)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestMemTxStore_GetInvalidTxIDLength(t *testing.T) {
	store := NewMemTxStore()
	_, err := store.GetTx([]byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidTxID)
}

func TestMemTxStore_DeleteWithPubKeyIndex(t *testing.T) {
	store := NewMemTxStore()
	pNode := makeHash(0xAA)

	tx := &StoredTx{TxID: makeHash(0x01), RawTx: []byte("tx")}
	require.NoError(t, store.PutTxWithPubKey(tx, pNode))

	require.NoError(t, store.DeleteTx(tx.TxID))

	txs, err := store.GetTxsByPubKey(pNode)
	require.NoError(t, err)
	assert.Empty(t, txs)
}

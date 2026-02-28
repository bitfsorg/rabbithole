//go:build integration

// Package integration provides cross-package integration tests for BitFS.
// These tests exercise the SPV verification pipeline: BoltStore storage,
// Merkle proof construction/verification, and header chain operations.
// Run with: go test -tags=integration ./integration/ -run "TestSPV" -count=1 -v
package integration

import (
	"bytes"
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/spv"
)

// randomHash generates a cryptographically random 32-byte hash.
func randomHash(t *testing.T) []byte {
	t.Helper()
	h := make([]byte, 32)
	_, err := rand.Read(h)
	require.NoError(t, err, "crypto/rand.Read failed")
	return h
}

// openTestBoltStore creates a BoltStore in a temp directory with automatic cleanup.
func openTestBoltStore(t *testing.T) *spv.BoltStore {
	t.Helper()
	dir := t.TempDir()
	store, err := spv.OpenBoltStore(filepath.Join(dir, "spv_test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

// buildTestBlockHeader constructs a BlockHeader with the given fields and mines a valid PoW.
func buildTestBlockHeader(height uint32, prevBlock, merkleRoot []byte) *spv.BlockHeader {
	h := &spv.BlockHeader{
		Version:    1,
		PrevBlock:  prevBlock,
		MerkleRoot: merkleRoot,
		Timestamp:  1700000000 + height,
		Bits:       0x207fffff, // Regtest target: easy PoW
		Nonce:      0,
		Height:     height,
	}
	// Mine a valid nonce for PoW validation.
	for nonce := uint32(0); ; nonce++ {
		h.Nonce = nonce
		h.Hash = spv.ComputeHeaderHash(h)
		if spv.VerifyPoW(h) == nil {
			break
		}
	}
	return h
}

// TestSPVStoreAndRetrieve opens a BoltStore, stores a transaction, retrieves it,
// and verifies all fields match.
func TestSPVStoreAndRetrieve(t *testing.T) {
	store := openTestBoltStore(t)

	txID := randomHash(t)
	rawTx := []byte("fake-serialized-transaction-bytes")
	blockHash := randomHash(t)

	proof := &spv.MerkleProof{
		TxID:      txID,
		Index:     5,
		Nodes:     [][]byte{randomHash(t), randomHash(t)},
		BlockHash: blockHash,
	}

	original := &spv.StoredTx{
		TxID:        txID,
		RawTx:       rawTx,
		Proof:       proof,
		BlockHeight: 12345,
		Timestamp:   1700000000,
	}

	// Store
	err := store.Txs().PutTx(original)
	require.NoError(t, err, "PutTx should succeed")

	// Retrieve
	got, err := store.Txs().GetTx(txID)
	require.NoError(t, err, "GetTx should succeed")
	require.NotNil(t, got)

	// Verify all fields
	assert.Equal(t, original.TxID, got.TxID, "TxID mismatch")
	assert.Equal(t, original.RawTx, got.RawTx, "RawTx mismatch")
	assert.Equal(t, original.BlockHeight, got.BlockHeight, "BlockHeight mismatch")
	assert.Equal(t, original.Timestamp, got.Timestamp, "Timestamp mismatch")

	require.NotNil(t, got.Proof, "Proof should not be nil")
	assert.Equal(t, proof.TxID, got.Proof.TxID, "Proof.TxID mismatch")
	assert.Equal(t, proof.Index, got.Proof.Index, "Proof.Index mismatch")
	assert.Equal(t, proof.BlockHash, got.Proof.BlockHash, "Proof.BlockHash mismatch")
	require.Len(t, got.Proof.Nodes, 2, "Proof.Nodes length mismatch")
	assert.Equal(t, proof.Nodes[0], got.Proof.Nodes[0], "Proof.Nodes[0] mismatch")
	assert.Equal(t, proof.Nodes[1], got.Proof.Nodes[1], "Proof.Nodes[1] mismatch")

	// Verify duplicate detection
	err = store.Txs().PutTx(original)
	assert.ErrorIs(t, err, spv.ErrDuplicateTx, "second PutTx should fail with duplicate error")
}

// TestSPVProofBackfill stores a transaction without a proof, then updates it
// with a Merkle proof and block height, and verifies the update persisted.
func TestSPVProofBackfill(t *testing.T) {
	store := openTestBoltStore(t)

	txID := randomHash(t)

	// Step 1: Store tx without proof (unconfirmed)
	unconfirmed := &spv.StoredTx{
		TxID:        txID,
		RawTx:       []byte("unconfirmed-raw-tx"),
		Proof:       nil,
		BlockHeight: 0,
		Timestamp:   1700000001,
	}

	err := store.Txs().PutTx(unconfirmed)
	require.NoError(t, err)

	// Verify it's stored without proof
	got, err := store.Txs().GetTx(txID)
	require.NoError(t, err)
	assert.Nil(t, got.Proof, "initial tx should have nil Proof")
	assert.Equal(t, uint32(0), got.BlockHeight, "initial tx should have zero BlockHeight")

	// Step 2: Backfill with proof
	blockHash := randomHash(t)
	proof := &spv.MerkleProof{
		TxID:      txID,
		Index:     2,
		Nodes:     [][]byte{randomHash(t), randomHash(t), randomHash(t)},
		BlockHash: blockHash,
	}

	confirmed := &spv.StoredTx{
		TxID:        txID,
		RawTx:       []byte("unconfirmed-raw-tx"),
		Proof:       proof,
		BlockHeight: 54321,
		Timestamp:   1700000001,
	}

	err = store.Txs().UpdateTx(confirmed)
	require.NoError(t, err, "UpdateTx should succeed for backfill")

	// Step 3: Verify update persisted
	got, err = store.Txs().GetTx(txID)
	require.NoError(t, err)
	require.NotNil(t, got.Proof, "backfilled tx should have non-nil Proof")
	assert.Equal(t, uint32(54321), got.BlockHeight, "BlockHeight should be updated")
	assert.Equal(t, proof.Index, got.Proof.Index, "Proof.Index should match")
	assert.Equal(t, proof.BlockHash, got.Proof.BlockHash, "Proof.BlockHash should match")
	assert.Len(t, got.Proof.Nodes, 3, "Proof.Nodes should have 3 entries")

	// Step 4: Verify UpdateTx fails for non-existent tx
	nonExistent := &spv.StoredTx{
		TxID:  randomHash(t),
		RawTx: []byte("does-not-exist"),
	}
	err = store.Txs().UpdateTx(nonExistent)
	assert.ErrorIs(t, err, spv.ErrTxNotFound, "UpdateTx on non-existent tx should fail")
}

// TestSPVMerkleRootComputation builds a 4-tx Merkle tree manually, constructs
// a proof for one of the transactions, and verifies ComputeMerkleRoot reproduces
// the expected root.
func TestSPVMerkleRootComputation(t *testing.T) {
	// Generate 4 distinct transaction hashes using DoubleHash for realistic values
	tx0 := spv.DoubleHash([]byte("transaction-0"))
	tx1 := spv.DoubleHash([]byte("transaction-1"))
	tx2 := spv.DoubleHash([]byte("transaction-2"))
	tx3 := spv.DoubleHash([]byte("transaction-3"))

	// combine is a helper for DoubleHash(left || right)
	combine := func(left, right []byte) []byte {
		buf := make([]byte, 64)
		copy(buf[:32], left)
		copy(buf[32:], right)
		return spv.DoubleHash(buf)
	}

	// Build tree manually:
	// Level 0 (leaves): tx0, tx1, tx2, tx3
	// Level 1: node01 = H(tx0||tx1), node23 = H(tx2||tx3)
	// Level 2 (root): H(node01||node23)
	node01 := combine(tx0, tx1)
	node23 := combine(tx2, tx3)
	expectedRoot := combine(node01, node23)

	// Also verify via ComputeMerkleRootFromTxList
	rootFromList := spv.ComputeMerkleRootFromTxList([][]byte{tx0, tx1, tx2, tx3})
	require.NotNil(t, rootFromList)
	assert.Equal(t, expectedRoot, rootFromList, "ComputeMerkleRootFromTxList should match manual root")

	// Test proof for each transaction index
	tests := []struct {
		name       string
		txHash     []byte
		index      uint32
		proofNodes [][]byte
	}{
		{
			name:       "tx0 (index 0, binary 00)",
			txHash:     tx0,
			index:      0,
			proofNodes: [][]byte{tx1, node23},
		},
		{
			name:       "tx1 (index 1, binary 01)",
			txHash:     tx1,
			index:      1,
			proofNodes: [][]byte{tx0, node23},
		},
		{
			name:       "tx2 (index 2, binary 10)",
			txHash:     tx2,
			index:      2,
			proofNodes: [][]byte{tx3, node01},
		},
		{
			name:       "tx3 (index 3, binary 11)",
			txHash:     tx3,
			index:      3,
			proofNodes: [][]byte{tx2, node01},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			computedRoot := spv.ComputeMerkleRoot(tt.txHash, tt.index, tt.proofNodes)
			require.NotNil(t, computedRoot, "ComputeMerkleRoot should not return nil")
			assert.Equal(t, expectedRoot, computedRoot,
				"computed root should match expected root for %s", tt.name)
		})
	}

	// Verify VerifyMerkleProof works end-to-end with a constructed proof
	proof := &spv.MerkleProof{
		TxID:      tx2,
		Index:     2,
		Nodes:     [][]byte{tx3, node01},
		BlockHash: randomHash(t),
	}
	valid, err := spv.VerifyMerkleProof(proof, expectedRoot)
	require.NoError(t, err)
	assert.True(t, valid, "valid proof should verify successfully")
}

// TestSPVTamperedProof verifies that a valid Merkle proof passes verification,
// and then a tampered proof (single flipped byte) does not.
func TestSPVTamperedProof(t *testing.T) {
	// Build a 4-tx tree
	tx0 := spv.DoubleHash([]byte("tamper-test-tx-0"))
	tx1 := spv.DoubleHash([]byte("tamper-test-tx-1"))
	tx2 := spv.DoubleHash([]byte("tamper-test-tx-2"))
	tx3 := spv.DoubleHash([]byte("tamper-test-tx-3"))

	combine := func(left, right []byte) []byte {
		buf := make([]byte, 64)
		copy(buf[:32], left)
		copy(buf[32:], right)
		return spv.DoubleHash(buf)
	}

	node01 := combine(tx0, tx1)
	node23 := combine(tx2, tx3)
	merkleRoot := combine(node01, node23)

	// Valid proof for tx0 at index 0: siblings are [tx1, node23]
	validProof := &spv.MerkleProof{
		TxID:      tx0,
		Index:     0,
		Nodes:     [][]byte{tx1, node23},
		BlockHash: randomHash(t),
	}

	// Step 1: Valid proof should verify
	valid, err := spv.VerifyMerkleProof(validProof, merkleRoot)
	require.NoError(t, err, "valid proof verification should not error")
	assert.True(t, valid, "valid proof should verify")

	// Step 2: Tamper with the first proof node by flipping one byte
	tamperedNode := make([]byte, 32)
	copy(tamperedNode, tx1)
	tamperedNode[0] ^= 0xFF // flip all bits of first byte

	tamperedProof := &spv.MerkleProof{
		TxID:      tx0,
		Index:     0,
		Nodes:     [][]byte{tamperedNode, node23},
		BlockHash: randomHash(t),
	}

	valid, err = spv.VerifyMerkleProof(tamperedProof, merkleRoot)
	assert.ErrorIs(t, err, spv.ErrMerkleProofInvalid, "tampered proof should produce ErrMerkleProofInvalid")
	assert.False(t, valid, "tampered proof should not verify")

	// Step 3: Tamper with the second proof node
	tamperedNode2 := make([]byte, 32)
	copy(tamperedNode2, node23)
	tamperedNode2[15] ^= 0x01 // flip one bit in the middle

	tamperedProof2 := &spv.MerkleProof{
		TxID:      tx0,
		Index:     0,
		Nodes:     [][]byte{tx1, tamperedNode2},
		BlockHash: randomHash(t),
	}

	valid, err = spv.VerifyMerkleProof(tamperedProof2, merkleRoot)
	assert.ErrorIs(t, err, spv.ErrMerkleProofInvalid, "tampered proof node 2 should fail")
	assert.False(t, valid, "tampered proof node 2 should not verify")

	// Step 4: Verify that ComputeMerkleRoot with tampered data produces a different root
	tamperedRoot := spv.ComputeMerkleRoot(tx0, 0, [][]byte{tamperedNode, node23})
	require.NotNil(t, tamperedRoot)
	assert.False(t, bytes.Equal(merkleRoot, tamperedRoot),
		"tampered proof should compute a different root")

	// Step 5: Full VerifyTransaction pipeline with tampered proof
	header := buildTestBlockHeader(100, randomHash(t), merkleRoot)

	headerStore := spv.NewMemHeaderStore()
	require.NoError(t, headerStore.PutHeader(header))

	storedTx := &spv.StoredTx{
		TxID:  tx0,
		RawTx: nil, // nil to skip RawTx hash check; this test targets Merkle proof tampering
		Proof: &spv.MerkleProof{
			TxID:      tx0,
			Index:     0,
			Nodes:     [][]byte{tamperedNode, node23}, // tampered
			BlockHash: header.Hash,
		},
		BlockHeight: 100,
	}

	err = spv.VerifyTransaction(storedTx, headerStore)
	assert.ErrorIs(t, err, spv.ErrMerkleProofInvalid,
		"VerifyTransaction should detect tampered proof")
}

// TestSPVBoltStoreHeaderChain stores a block header in the BoltStore, retrieves
// it by hash, and verifies that looking up an unknown hash returns an error.
func TestSPVBoltStoreHeaderChain(t *testing.T) {
	store := openTestBoltStore(t)

	prevBlock := randomHash(t)
	merkleRoot := randomHash(t)

	header := buildTestBlockHeader(500, prevBlock, merkleRoot)
	require.NotNil(t, header.Hash, "header hash should be computed")
	require.Len(t, header.Hash, 32, "header hash should be 32 bytes")

	// Step 1: Store the header
	err := store.Headers().PutHeader(header)
	require.NoError(t, err, "PutHeader should succeed")

	// Step 2: Retrieve by hash and verify all fields
	got, err := store.Headers().GetHeader(header.Hash)
	require.NoError(t, err, "GetHeader should succeed")
	require.NotNil(t, got)

	assert.Equal(t, header.Hash, got.Hash, "Hash mismatch")
	assert.Equal(t, header.Height, got.Height, "Height mismatch")
	assert.Equal(t, header.Version, got.Version, "Version mismatch")
	assert.Equal(t, header.PrevBlock, got.PrevBlock, "PrevBlock mismatch")
	assert.Equal(t, header.MerkleRoot, got.MerkleRoot, "MerkleRoot mismatch")
	assert.Equal(t, header.Timestamp, got.Timestamp, "Timestamp mismatch")
	assert.Equal(t, header.Bits, got.Bits, "Bits mismatch")
	assert.Equal(t, header.Nonce, got.Nonce, "Nonce mismatch")

	// Step 3: Retrieve by height
	gotByHeight, err := store.Headers().GetHeaderByHeight(500)
	require.NoError(t, err, "GetHeaderByHeight should succeed")
	assert.Equal(t, header.Hash, gotByHeight.Hash, "GetHeaderByHeight Hash mismatch")

	// Step 4: Unknown hash should return ErrHeaderNotFound
	unknownHash := randomHash(t)
	_, err = store.Headers().GetHeader(unknownHash)
	assert.ErrorIs(t, err, spv.ErrHeaderNotFound, "unknown hash should return ErrHeaderNotFound")

	// Step 5: Duplicate header should return ErrDuplicateHeader
	err = store.Headers().PutHeader(header)
	assert.ErrorIs(t, err, spv.ErrDuplicateHeader, "duplicate header should fail")

	// Step 6: Store a second header and verify GetTip returns the highest
	header2 := buildTestBlockHeader(600, header.Hash, randomHash(t))
	require.NoError(t, store.Headers().PutHeader(header2))

	tip, err := store.Headers().GetTip()
	require.NoError(t, err)
	assert.Equal(t, uint32(600), tip.Height, "tip should be highest header")

	// Step 7: Verify header count
	count, err := store.Headers().GetHeaderCount()
	require.NoError(t, err)
	assert.Equal(t, uint64(2), count, "should have 2 stored headers")
}

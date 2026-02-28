//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/network"
	"github.com/bitfsorg/libbitfs-go/spv"
)

// TestSPVVerify_RegtestMerkleProof performs a full SPV verification round-trip
// against a live BSV regtest node:
//
//  1. Fund a wallet address (mine 101 blocks, send coins, mine 1 more)
//  2. Get the funding transaction's raw bytes and txid
//  3. Retrieve a BIP37 MerkleBlock proof via gettxoutproof
//  4. Parse the MerkleBlock to extract block header + Merkle branch
//  5. Store the block header in MemHeaderStore
//  6. Construct a MerkleProof and StoredTx
//  7. Call spv.VerifyTransaction — should pass
//  8. Tamper with the proof (flip a byte) — should fail
func TestSPVVerify_RegtestMerkleProof(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// ---------------------------------------------------------------
	// Step 1: Fund an address
	// ---------------------------------------------------------------
	addr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate new address")

	// Mine 101 blocks so coinbase is spendable, then send coins.
	_, err = node.MineBlocks(ctx, 101, addr)
	require.NoError(t, err, "mine 101 blocks")

	// Send 1.0 BSV to a second address to create a non-coinbase tx.
	addr2, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate second address")

	txid, err := node.SendToAddress(ctx, addr2, 1.0)
	require.NoError(t, err, "send to address")
	t.Logf("funding txid: %s", txid)

	// Mine 1 more block to confirm the tx.
	_, err = node.MineBlocks(ctx, 1, addr)
	require.NoError(t, err, "mine confirmation block")

	// ---------------------------------------------------------------
	// Step 2: Get the raw transaction
	// ---------------------------------------------------------------
	rawTx, err := node.GetRawTransaction(ctx, txid)
	require.NoError(t, err, "get raw transaction")
	require.NotEmpty(t, rawTx)

	// Compute the txid from raw tx (double-SHA256) and verify it matches.
	computedTxID := spv.DoubleHash(rawTx)
	// Bitcoin txid is displayed in reversed byte order; the RPC txid is
	// big-endian hex, but internally it is stored in little-endian.
	txidBytes, err := hex.DecodeString(txid)
	require.NoError(t, err)
	// Reverse to get internal (little-endian) byte order.
	reverseBytes(txidBytes)
	require.Equal(t, txidBytes, computedTxID, "computed txid should match RPC txid (internal byte order)")

	// ---------------------------------------------------------------
	// Step 3: Get the BIP37 MerkleBlock proof
	// ---------------------------------------------------------------
	merkleBlockBytes, err := node.GetTxOutProof(ctx, txid)
	require.NoError(t, err, "get txout proof")
	require.True(t, len(merkleBlockBytes) > 80, "merkle block must be larger than header")

	// ---------------------------------------------------------------
	// Step 4: Parse the BIP37 MerkleBlock
	// ---------------------------------------------------------------
	mbHeader, txIndex, branch, totalTxs, err := network.ParseBIP37MerkleBlock(merkleBlockBytes, computedTxID)
	require.NoError(t, err, "parse BIP37 merkle block")
	t.Logf("totalTxs=%d, txIndex=%d, branch length=%d", totalTxs, txIndex, len(branch))

	// ---------------------------------------------------------------
	// Step 5: Deserialize block header and store it
	// ---------------------------------------------------------------
	blockHeader, err := spv.DeserializeHeader(mbHeader)
	require.NoError(t, err, "deserialize block header")

	// Get block height from the node via verbose getblockheader.
	blockHashHex := hex.EncodeToString(reverseBytesCopy(blockHeader.Hash))
	verbose, err := node.GetBlockHeaderVerbose(ctx, blockHashHex)
	require.NoError(t, err, "get verbose block header")

	heightFloat, ok := verbose["height"].(float64)
	require.True(t, ok, "height should be a number")
	blockHeader.Height = uint32(heightFloat)
	t.Logf("block hash: %s, height: %d", blockHashHex, blockHeader.Height)

	headerStore := spv.NewMemHeaderStore()
	err = headerStore.PutHeader(blockHeader)
	require.NoError(t, err, "store block header")

	// ---------------------------------------------------------------
	// Step 6: Construct MerkleProof and StoredTx
	// ---------------------------------------------------------------
	proof := &spv.MerkleProof{
		TxID:      computedTxID,
		Index:     txIndex,
		Nodes:     branch,
		BlockHash: blockHeader.Hash,
	}

	// Sanity check: compute the Merkle root from the proof and compare.
	computedRoot := spv.ComputeMerkleRoot(proof.TxID, proof.Index, proof.Nodes)
	require.NotNil(t, computedRoot, "computed Merkle root should not be nil")
	require.Equal(t, blockHeader.MerkleRoot, computedRoot,
		"Merkle root from proof should match block header's Merkle root")

	storedTx := &spv.StoredTx{
		TxID:        computedTxID,
		RawTx:       rawTx,
		Proof:       proof,
		BlockHeight: blockHeader.Height,
		Timestamp:   uint64(blockHeader.Timestamp),
	}

	// ---------------------------------------------------------------
	// Step 7: Verify — should pass
	// ---------------------------------------------------------------
	err = spv.VerifyTransaction(storedTx, headerStore)
	assert.NoError(t, err, "VerifyTransaction should succeed with valid proof")

	// ---------------------------------------------------------------
	// Step 8: Tamper with the proof — should fail
	// ---------------------------------------------------------------
	// Clone the proof and flip a byte in the first branch node.
	tamperedNodes := make([][]byte, len(branch))
	for i, n := range branch {
		c := make([]byte, len(n))
		copy(c, n)
		tamperedNodes[i] = c
	}
	tamperedNodes[0][0] ^= 0xFF // flip the first byte

	tamperedProof := &spv.MerkleProof{
		TxID:      computedTxID,
		Index:     txIndex,
		Nodes:     tamperedNodes,
		BlockHash: blockHeader.Hash,
	}

	tamperedTx := &spv.StoredTx{
		TxID:        computedTxID,
		RawTx:       rawTx,
		Proof:       tamperedProof,
		BlockHeight: blockHeader.Height,
		Timestamp:   uint64(blockHeader.Timestamp),
	}

	err = spv.VerifyTransaction(tamperedTx, headerStore)
	assert.Error(t, err, "VerifyTransaction should fail with tampered proof")
	assert.ErrorIs(t, err, spv.ErrMerkleProofInvalid, "error should be ErrMerkleProofInvalid")
}

// TestSPVVerify_ManualMerkleBranch uses the getblock RPC to retrieve all
// transaction IDs in a block, manually builds the Merkle tree and proof,
// and verifies it through the SPV pipeline. This serves as a cross-check
// for the BIP37 parsing approach.
func TestSPVVerify_ManualMerkleBranch(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Fund and create a transaction.
	addr, err := node.NewAddress(ctx)
	require.NoError(t, err)

	_, err = node.MineBlocks(ctx, 101, addr)
	require.NoError(t, err)

	addr2, err := node.NewAddress(ctx)
	require.NoError(t, err)

	txid, err := node.SendToAddress(ctx, addr2, 0.5)
	require.NoError(t, err)

	blockHashes, err := node.MineBlocks(ctx, 1, addr)
	require.NoError(t, err)
	require.NotEmpty(t, blockHashes)

	blockHash := blockHashes[0]

	// Get the raw transaction.
	rawTx, err := node.GetRawTransaction(ctx, txid)
	require.NoError(t, err)
	computedTxID := spv.DoubleHash(rawTx)

	// Get block info to find all transaction IDs.
	var blockInfo map[string]interface{}
	err = callRPC(node, ctx, "getblock", []interface{}{blockHash, 1}, &blockInfo)
	require.NoError(t, err)

	txListRaw, ok := blockInfo["tx"].([]interface{})
	require.True(t, ok, "block should have tx list")
	require.True(t, len(txListRaw) >= 2, "block should have at least 2 txs (coinbase + our tx)")

	// Convert tx IDs to internal byte order (little-endian).
	txHashes := make([][]byte, len(txListRaw))
	targetIndex := -1
	for i, txIDRaw := range txListRaw {
		txIDStr, ok := txIDRaw.(string)
		require.True(t, ok)
		txIDBytes, err := hex.DecodeString(txIDStr)
		require.NoError(t, err)
		reverseBytes(txIDBytes) // convert from display (big-endian) to internal (little-endian)
		txHashes[i] = txIDBytes
		if bytes.Equal(txIDBytes, computedTxID) {
			targetIndex = i
		}
	}
	require.NotEqual(t, -1, targetIndex, "our tx should be in the block")
	t.Logf("target tx at index %d of %d txs", targetIndex, len(txHashes))

	// Build Merkle branch manually.
	branchNodes := buildMerkleBranch(txHashes, uint32(targetIndex))
	require.NotEmpty(t, branchNodes, "branch should have at least one node for multi-tx block")

	// Verify the branch produces the correct Merkle root.
	computedRoot := spv.ComputeMerkleRoot(computedTxID, uint32(targetIndex), branchNodes)
	require.NotNil(t, computedRoot)

	// Get the block header to compare Merkle roots.
	headerBytes, err := node.GetBlockHeader(ctx, blockHash)
	require.NoError(t, err)

	blockHeader, err := spv.DeserializeHeader(headerBytes)
	require.NoError(t, err)

	require.Equal(t, blockHeader.MerkleRoot, computedRoot,
		"manually built Merkle branch should produce correct root")

	// Get height for the header.
	verbose, err := node.GetBlockHeaderVerbose(ctx, blockHash)
	require.NoError(t, err)
	blockHeader.Height = uint32(verbose["height"].(float64))

	// Run full SPV verification.
	headerStore := spv.NewMemHeaderStore()
	err = headerStore.PutHeader(blockHeader)
	require.NoError(t, err)

	proof := &spv.MerkleProof{
		TxID:      computedTxID,
		Index:     uint32(targetIndex),
		Nodes:     branchNodes,
		BlockHash: blockHeader.Hash,
	}

	storedTx := &spv.StoredTx{
		TxID:        computedTxID,
		RawTx:       rawTx,
		Proof:       proof,
		BlockHeight: blockHeader.Height,
		Timestamp:   uint64(blockHeader.Timestamp),
	}

	err = spv.VerifyTransaction(storedTx, headerStore)
	assert.NoError(t, err, "VerifyTransaction should succeed with manually built proof")
}

// =============================================================================
// Helpers
// =============================================================================

// reverseBytes reverses a byte slice in place.
func reverseBytes(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}

// reverseBytesCopy returns a reversed copy of a byte slice.
func reverseBytesCopy(b []byte) []byte {
	c := make([]byte, len(b))
	for i, v := range b {
		c[len(b)-1-i] = v
	}
	return c
}

// callRPC is a helper that uses the unexported rpc field on RegtestNode.
// Since we cannot access the private rpc field, we create a separate RPCClient.
func callRPC(node *testutil.RegtestNode, ctx context.Context, method string, params []interface{}, result interface{}) error {
	// We recreate a client with the same default credentials.
	client := testutil.NewRPCClient("http://localhost:18332", "bitfs", "bitfs")
	return client.Call(ctx, method, params, result)
}

// buildMerkleBranch manually constructs a Merkle branch (proof nodes) for the
// transaction at targetIndex within the given list of transaction hashes.
// This is the "option b" approach: given all txids in a block, build the proof
// without parsing BIP37.
func buildMerkleBranch(txHashes [][]byte, targetIndex uint32) [][]byte {
	if len(txHashes) <= 1 {
		return nil
	}

	// Work with a copy of the leaves.
	level := make([][]byte, len(txHashes))
	for i, h := range txHashes {
		level[i] = make([]byte, 32)
		copy(level[i], h)
	}

	var branch [][]byte
	idx := targetIndex

	for len(level) > 1 {
		// Pad if odd.
		if len(level)%2 != 0 {
			dup := make([]byte, 32)
			copy(dup, level[len(level)-1])
			level = append(level, dup)
		}

		// Determine sibling index.
		var siblingIdx uint32
		if idx%2 == 0 {
			siblingIdx = idx + 1
		} else {
			siblingIdx = idx - 1
		}

		sibling := make([]byte, 32)
		copy(sibling, level[siblingIdx])
		branch = append(branch, sibling)

		// Build next level.
		nextLevel := make([][]byte, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			combined := make([]byte, 64)
			copy(combined[:32], level[i])
			copy(combined[32:], level[i+1])
			nextLevel[i/2] = spv.DoubleHash(combined)
		}
		level = nextLevel
		idx = idx / 2
	}

	return branch
}

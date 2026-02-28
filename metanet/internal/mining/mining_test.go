// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package mining

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/bitfsorg/metanet/internal/chain"
)

// ---------------------------------------------------------------------------
// Coinbase commitment tests
// ---------------------------------------------------------------------------

func TestBuildCoinbaseCommitment(t *testing.T) {
	blockHash := [32]byte{0xaa, 0xbb, 0xcc, 0xdd}
	commitment := BuildCoinbaseCommitment(blockHash)

	// Should start with OP_RETURN (0x6a).
	if commitment[0] != 0x6a {
		t.Errorf("commitment[0] = 0x%02x, want 0x6a (OP_RETURN)", commitment[0])
	}

	// Should contain MNMP magic.
	if !bytes.Equal(commitment[2:6], AuxPowMagic[:]) {
		t.Errorf("commitment magic = %x, want %x", commitment[2:6], AuxPowMagic[:])
	}

	// Should contain the block hash.
	if !bytes.Equal(commitment[6:38], blockHash[:]) {
		t.Errorf("commitment hash mismatch")
	}

	// Total length: OP_RETURN(1) + push(1) + MNMP(4) + hash(32) = 38.
	if len(commitment) != 38 {
		t.Errorf("commitment length = %d, want 38", len(commitment))
	}
}

func TestFindAuxPoWCommitment(t *testing.T) {
	blockHash := [32]byte{0x01, 0x02, 0x03, 0x04, 0x05}

	// Build a fake coinbase with the commitment embedded.
	coinbase := []byte{0x00, 0x00, 0x00} // Padding.
	coinbase = append(coinbase, AuxPowMagic[:]...)
	coinbase = append(coinbase, blockHash[:]...)
	coinbase = append(coinbase, 0x00, 0x00) // Trailing bytes.

	found := FindAuxPoWCommitment(coinbase)
	if found == nil {
		t.Fatal("FindAuxPoWCommitment returned nil, expected hash")
	}
	if !bytes.Equal(found, blockHash[:]) {
		t.Errorf("found hash = %x, want %x", found, blockHash[:])
	}
}

func TestFindAuxPoWCommitmentNotFound(t *testing.T) {
	// Coinbase without MNMP marker.
	coinbase := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	found := FindAuxPoWCommitment(coinbase)
	if found != nil {
		t.Errorf("expected nil, got %x", found)
	}
}

func TestFindAuxPoWCommitmentTooShort(t *testing.T) {
	// Coinbase with MNMP but not enough bytes for a hash.
	coinbase := AuxPowMagic[:]
	coinbase = append(coinbase, 0x01, 0x02) // Only 2 bytes, need 32.
	found := FindAuxPoWCommitment(coinbase)
	if found != nil {
		t.Errorf("expected nil for truncated commitment, got %x", found)
	}
}

// ---------------------------------------------------------------------------
// Coinbase branch verification tests
// ---------------------------------------------------------------------------

func TestVerifyCoinbaseBranchSingleTx(t *testing.T) {
	// With a single transaction (coinbase only), the Merkle root IS the
	// coinbase TxID and the branch is empty.
	txHash := sha256d([]byte("test coinbase"))
	root := txHash // Single tx: root == txHash.

	if !VerifyCoinbaseBranch(txHash, nil, 0, root) {
		t.Error("single-tx branch verification failed")
	}
}

func TestVerifyCoinbaseBranchTwoTxs(t *testing.T) {
	// Build a Merkle tree with two transactions.
	tx0 := sha256d([]byte("coinbase"))
	tx1 := sha256d([]byte("tx1"))

	// Root = SHA256d(tx0 || tx1)
	var combined [64]byte
	copy(combined[0:32], tx0[:])
	copy(combined[32:64], tx1[:])
	first := sha256.Sum256(combined[:])
	root := sha256.Sum256(first[:])

	// Branch for tx0 (index 0): [tx1]
	branch := [][32]byte{tx1}
	if !VerifyCoinbaseBranch(tx0, branch, 0, root) {
		t.Error("two-tx branch verification failed for index 0")
	}

	// Branch for tx1 (index 1): [tx0]
	branch1 := [][32]byte{tx0}
	if !VerifyCoinbaseBranch(tx1, branch1, 1, root) {
		t.Error("two-tx branch verification failed for index 1")
	}
}

func TestVerifyCoinbaseBranchInvalid(t *testing.T) {
	txHash := sha256d([]byte("coinbase"))
	wrongRoot := [32]byte{0xff}
	if VerifyCoinbaseBranch(txHash, nil, 0, wrongRoot) {
		t.Error("should not verify with wrong root")
	}
}

// ---------------------------------------------------------------------------
// Difficulty adjustment tests
// ---------------------------------------------------------------------------

func TestCalcNextDifficultyExactTimespan(t *testing.T) {
	// If actual timespan exactly matches expected, difficulty should not change.
	oldBits := uint32(0x1d00ffff)
	newBits := CalcNextDifficulty(oldBits, chain.ExpectedRetargetTimespan)

	// The result should be the same (or very close due to rounding).
	oldTarget := chain.CompactToBig(oldBits)
	newTarget := chain.CompactToBig(newBits)

	if oldTarget.Cmp(newTarget) != 0 {
		t.Errorf("difficulty changed with exact timespan: old=%s, new=%s",
			oldTarget.String(), newTarget.String())
	}
}

func TestCalcNextDifficultyDoubleTimespan(t *testing.T) {
	// If blocks took twice as long, target should double (difficulty halves).
	oldBits := uint32(0x1b0404cb)
	doubled := chain.ExpectedRetargetTimespan * 2
	newBits := CalcNextDifficulty(oldBits, doubled)

	oldTarget := chain.CompactToBig(oldBits)
	newTarget := chain.CompactToBig(newBits)

	// New target should be approximately 2x old target.
	expectedTarget := new(big.Int).Mul(oldTarget, big.NewInt(2))
	// Allow small rounding difference.
	diff := new(big.Int).Sub(expectedTarget, newTarget)
	diff.Abs(diff)
	tolerance := new(big.Int).Div(expectedTarget, big.NewInt(100)) // 1%
	if diff.Cmp(tolerance) > 0 {
		t.Errorf("expected ~2x target, got old=%s, new=%s", oldTarget.String(), newTarget.String())
	}
}

func TestCalcNextDifficultyHalfTimespan(t *testing.T) {
	// If blocks took half as long, target should halve (difficulty doubles).
	oldBits := uint32(0x1b0404cb)
	halved := chain.ExpectedRetargetTimespan / 2
	newBits := CalcNextDifficulty(oldBits, halved)

	oldTarget := chain.CompactToBig(oldBits)
	newTarget := chain.CompactToBig(newBits)

	// New target should be approximately 0.5x old target.
	expectedTarget := new(big.Int).Div(oldTarget, big.NewInt(2))
	diff := new(big.Int).Sub(expectedTarget, newTarget)
	diff.Abs(diff)
	tolerance := new(big.Int).Div(expectedTarget, big.NewInt(100))
	if diff.Cmp(tolerance) > 0 {
		t.Errorf("expected ~0.5x target, got old=%s, new=%s", oldTarget.String(), newTarget.String())
	}
}

func TestCalcNextDifficultyClampMin(t *testing.T) {
	// Timespan much shorter than expected/4 should be clamped.
	oldBits := uint32(0x1b0404cb)
	veryFast := int64(1) // 1 second for 2016 blocks.
	newBits := CalcNextDifficulty(oldBits, veryFast)

	// Should be clamped to expected/4 (4x difficulty increase, target /= 4).
	clampedTimespan := chain.ExpectedRetargetTimespan / chain.MaxRetargetFactor
	expectedBits := CalcNextDifficulty(oldBits, clampedTimespan)

	if newBits != expectedBits {
		t.Errorf("min clamp: got 0x%08x, want 0x%08x", newBits, expectedBits)
	}
}

func TestCalcNextDifficultyClampMax(t *testing.T) {
	// Timespan much longer than expected*4 should be clamped.
	oldBits := uint32(0x1b0404cb)
	verySlow := int64(999_999_999)
	newBits := CalcNextDifficulty(oldBits, verySlow)

	// Should be clamped to expected*4 (4x difficulty decrease, target *= 4).
	clampedTimespan := chain.ExpectedRetargetTimespan * chain.MaxRetargetFactor
	expectedBits := CalcNextDifficulty(oldBits, clampedTimespan)

	if newBits != expectedBits {
		t.Errorf("max clamp: got 0x%08x, want 0x%08x", newBits, expectedBits)
	}
}

func TestCalcNextDifficultyNeverExceedsMax(t *testing.T) {
	// Even with extremely slow blocks, target should not exceed max.
	maxBits := uint32(0x1d00ffff)
	verySlow := chain.ExpectedRetargetTimespan * chain.MaxRetargetFactor
	newBits := CalcNextDifficulty(maxBits, verySlow)

	newTarget := chain.CompactToBig(newBits)
	maxTarget := chain.CompactToBig(0x1d00ffff)

	if newTarget.Cmp(maxTarget) > 0 {
		t.Error("new target exceeds maximum")
	}
}

// ---------------------------------------------------------------------------
// Anchor serialization tests
// ---------------------------------------------------------------------------

func TestSerializeDeserializeAnchorData(t *testing.T) {
	original := &AnchorTx{
		Version:      AnchorVersion,
		Flag:         AnchorMagic,
		StartHeight:  1000,
		EndHeight:    1099,
		MerkleRoot:   [32]byte{0xaa, 0xbb, 0xcc},
		BlockCount:   100,
		PrevAnchorTx: [32]byte{0x11, 0x22, 0x33},
	}

	data := SerializeAnchorData(original)
	if len(data) != AnchorDataMinSize {
		t.Errorf("serialized size = %d, want %d", len(data), AnchorDataMinSize)
	}

	deserialized, err := DeserializeAnchorData(data)
	if err != nil {
		t.Fatalf("DeserializeAnchorData error: %v", err)
	}

	if deserialized.Version != original.Version {
		t.Errorf("Version: got %d, want %d", deserialized.Version, original.Version)
	}
	if deserialized.Flag != original.Flag {
		t.Errorf("Flag: got %v, want %v", deserialized.Flag, original.Flag)
	}
	if deserialized.StartHeight != original.StartHeight {
		t.Errorf("StartHeight: got %d, want %d", deserialized.StartHeight, original.StartHeight)
	}
	if deserialized.EndHeight != original.EndHeight {
		t.Errorf("EndHeight: got %d, want %d", deserialized.EndHeight, original.EndHeight)
	}
	if deserialized.MerkleRoot != original.MerkleRoot {
		t.Errorf("MerkleRoot mismatch")
	}
	if deserialized.BlockCount != original.BlockCount {
		t.Errorf("BlockCount: got %d, want %d", deserialized.BlockCount, original.BlockCount)
	}
	if deserialized.PrevAnchorTx != original.PrevAnchorTx {
		t.Errorf("PrevAnchorTx mismatch")
	}
}

func TestDeserializeAnchorDataTooShort(t *testing.T) {
	_, err := DeserializeAnchorData([]byte{0x01, 0x02})
	if err != ErrAnchorDataTooShort {
		t.Errorf("expected ErrAnchorDataTooShort, got %v", err)
	}
}

func TestDeserializeAnchorDataBadMagic(t *testing.T) {
	data := make([]byte, AnchorDataMinSize)
	data[0] = AnchorVersion
	copy(data[1:5], []byte("XXXX")) // Wrong magic.
	_, err := DeserializeAnchorData(data)
	if err != ErrInvalidAnchorMagic {
		t.Errorf("expected ErrInvalidAnchorMagic, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// BuildAnchorTx tests
// ---------------------------------------------------------------------------

func TestBuildAnchorTx(t *testing.T) {
	headers := make([]chain.BlockHeader, 100)
	for i := range headers {
		headers[i] = chain.BlockHeader{
			Version:   1,
			Timestamp: uint32(1700000000 + i*600),
			Nonce:     uint32(i),
		}
	}

	prevTxID := [32]byte{0xff}
	anchor, err := BuildAnchorTx(0, 99, headers, prevTxID)
	if err != nil {
		t.Fatalf("BuildAnchorTx error: %v", err)
	}

	if anchor.StartHeight != 0 {
		t.Errorf("StartHeight = %d, want 0", anchor.StartHeight)
	}
	if anchor.EndHeight != 99 {
		t.Errorf("EndHeight = %d, want 99", anchor.EndHeight)
	}
	if anchor.BlockCount != 100 {
		t.Errorf("BlockCount = %d, want 100", anchor.BlockCount)
	}
	if anchor.MerkleRoot == ([32]byte{}) {
		t.Error("MerkleRoot should not be zero")
	}
	if anchor.PrevAnchorTx != prevTxID {
		t.Error("PrevAnchorTx mismatch")
	}
}

func TestBuildAnchorTxEmpty(t *testing.T) {
	_, err := BuildAnchorTx(0, 0, nil, [32]byte{})
	if err != ErrNoBlockHeaders {
		t.Errorf("expected ErrNoBlockHeaders, got %v", err)
	}
}

func TestBuildAnchorTxMismatchCount(t *testing.T) {
	headers := make([]chain.BlockHeader, 5)
	_, err := BuildAnchorTx(0, 99, headers, [32]byte{})
	if err != ErrAnchorHeightMismatch {
		t.Errorf("expected ErrAnchorHeightMismatch, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Anchor chain validation tests
// ---------------------------------------------------------------------------

func TestValidateAnchorChainValid(t *testing.T) {
	// Build a chain of 3 anchors.
	anchor0 := &AnchorTx{
		Version:     AnchorVersion,
		Flag:        AnchorMagic,
		StartHeight: 0,
		EndHeight:   99,
		BlockCount:  100,
	}

	anchor1 := &AnchorTx{
		Version:      AnchorVersion,
		Flag:         AnchorMagic,
		StartHeight:  100,
		EndHeight:    199,
		BlockCount:   100,
		PrevAnchorTx: ComputeAnchorTxID(anchor0),
	}

	anchor2 := &AnchorTx{
		Version:      AnchorVersion,
		Flag:         AnchorMagic,
		StartHeight:  200,
		EndHeight:    299,
		BlockCount:   100,
		PrevAnchorTx: ComputeAnchorTxID(anchor1),
	}

	err := ValidateAnchorChain([]*AnchorTx{anchor0, anchor1, anchor2})
	if err != nil {
		t.Errorf("ValidateAnchorChain error: %v", err)
	}
}

func TestValidateAnchorChainBroken(t *testing.T) {
	anchor0 := &AnchorTx{
		Version:     AnchorVersion,
		Flag:        AnchorMagic,
		StartHeight: 0,
		EndHeight:   99,
	}

	anchor1 := &AnchorTx{
		Version:      AnchorVersion,
		Flag:         AnchorMagic,
		StartHeight:  100,
		EndHeight:    199,
		PrevAnchorTx: [32]byte{0xff}, // Wrong reference.
	}

	err := ValidateAnchorChain([]*AnchorTx{anchor0, anchor1})
	if err != ErrAnchorChainBroken {
		t.Errorf("expected ErrAnchorChainBroken, got %v", err)
	}
}

func TestValidateAnchorChainHeightGap(t *testing.T) {
	anchor0 := &AnchorTx{
		Version:     AnchorVersion,
		Flag:        AnchorMagic,
		StartHeight: 0,
		EndHeight:   99,
	}

	anchor1 := &AnchorTx{
		Version:      AnchorVersion,
		Flag:         AnchorMagic,
		StartHeight:  200, // Gap: should be 100.
		EndHeight:    299,
		PrevAnchorTx: ComputeAnchorTxID(anchor0),
	}

	err := ValidateAnchorChain([]*AnchorTx{anchor0, anchor1})
	if err != ErrAnchorHeightMismatch {
		t.Errorf("expected ErrAnchorHeightMismatch, got %v", err)
	}
}

func TestValidateAnchorChainSingle(t *testing.T) {
	// A single anchor should validate fine (nothing to check).
	err := ValidateAnchorChain([]*AnchorTx{{Version: AnchorVersion}})
	if err != nil {
		t.Errorf("single anchor should validate, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// BuildBlockRangeMerkleRoot tests
// ---------------------------------------------------------------------------

func TestBuildBlockRangeMerkleRootEmpty(t *testing.T) {
	root := BuildBlockRangeMerkleRoot(nil)
	if root != ([32]byte{}) {
		t.Error("empty headers should produce zero root")
	}
}

func TestBuildBlockRangeMerkleRootSingle(t *testing.T) {
	h := chain.BlockHeader{Version: 1, Nonce: 42}
	root := BuildBlockRangeMerkleRoot([]chain.BlockHeader{h})
	expected := chain.HashBlockHeader(&h)
	if root != expected {
		t.Error("single header root should equal header hash")
	}
}

func TestBuildBlockRangeMerkleRootDeterministic(t *testing.T) {
	headers := []chain.BlockHeader{
		{Version: 1, Nonce: 1},
		{Version: 1, Nonce: 2},
		{Version: 1, Nonce: 3},
	}
	root1 := BuildBlockRangeMerkleRoot(headers)
	root2 := BuildBlockRangeMerkleRoot(headers)
	if root1 != root2 {
		t.Error("BuildBlockRangeMerkleRoot is not deterministic")
	}
}

// ---------------------------------------------------------------------------
// AuxPoW validation integration test
// ---------------------------------------------------------------------------

func TestValidateAuxPoWValid(t *testing.T) {
	// Construct a valid AuxPoW scenario.
	// 1. Create a Metanet Chain block header.
	// Use a trivially easy target (0x2f00ffff ≈ 2^255) so any hash meets it.
	testBits := uint32(0x2f00ffff)

	metanetHeader := chain.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      testBits,
		Nonce:     0,
	}
	metanetBlockHash := chain.HashBlockHeader(&metanetHeader)

	// 2. Create a coinbase transaction with MNMP commitment.
	coinbase := []byte{0x01, 0x00, 0x00, 0x00} // tx version
	coinbase = append(coinbase, AuxPowMagic[:]...)
	coinbase = append(coinbase, metanetBlockHash[:]...)
	coinbase = append(coinbase, 0x00, 0x00) // padding

	coinbaseTxID := sha256d(coinbase)

	// 3. Create a parent block with just the coinbase (single tx = merkle root is txid).
	parentMerkleRoot := coinbaseTxID

	// 4. Create a parent header. With testBits any hash trivially meets the target.
	parentHeader := chain.BlockHeader{
		Version:    1,
		MerkleRoot: parentMerkleRoot,
		Timestamp:  1700000000,
		Bits:       testBits,
	}

	serializedParent := chain.SerializeBlockHeader(&parentHeader)

	// 5. Build AuxPoW header.
	auxpow := &AuxPoWHeader{
		Header:         metanetHeader,
		ParentHeader:   serializedParent,
		CoinbaseTx:     coinbase,
		CoinbaseBranch: nil, // Single tx, no branch needed.
		CoinbaseIndex:  0,
	}

	params := chain.MainNetParams()
	err := ValidateAuxPoW(auxpow, params)
	if err != nil {
		t.Errorf("ValidateAuxPoW failed: %v", err)
	}
}

func TestValidateAuxPoWWrongHash(t *testing.T) {
	testBits := uint32(0x2f00ffff)
	metanetHeader := chain.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      testBits,
	}

	// Coinbase with WRONG block hash.
	wrongHash := [32]byte{0xff}
	coinbase := []byte{0x01, 0x00, 0x00, 0x00}
	coinbase = append(coinbase, AuxPowMagic[:]...)
	coinbase = append(coinbase, wrongHash[:]...)

	coinbaseTxID := sha256d(coinbase)
	parentHeader := chain.BlockHeader{
		Version:    1,
		MerkleRoot: coinbaseTxID,
		Timestamp:  1700000000,
		Bits:       testBits,
	}
	serializedParent := chain.SerializeBlockHeader(&parentHeader)

	auxpow := &AuxPoWHeader{
		Header:       metanetHeader,
		ParentHeader: serializedParent,
		CoinbaseTx:   coinbase,
	}

	err := ValidateAuxPoW(auxpow, chain.MainNetParams())
	if err != ErrAuxPoWHashMismatch {
		t.Errorf("expected ErrAuxPoWHashMismatch, got %v", err)
	}
}

func TestValidateAuxPoWNoCommitment(t *testing.T) {
	testBits := uint32(0x2f00ffff)
	metanetHeader := chain.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      testBits,
	}

	// Coinbase WITHOUT MNMP marker.
	coinbase := []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00}
	coinbaseTxID := sha256d(coinbase)
	parentHeader := chain.BlockHeader{
		Version:    1,
		MerkleRoot: coinbaseTxID,
		Timestamp:  1700000000,
		Bits:       testBits,
	}
	serializedParent := chain.SerializeBlockHeader(&parentHeader)

	auxpow := &AuxPoWHeader{
		Header:       metanetHeader,
		ParentHeader: serializedParent,
		CoinbaseTx:   coinbase,
	}

	err := ValidateAuxPoW(auxpow, chain.MainNetParams())
	if err != ErrNoAuxPoWCommitment {
		t.Errorf("expected ErrNoAuxPoWCommitment, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sha256d(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}


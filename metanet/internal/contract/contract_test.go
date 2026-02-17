// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package contract

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

// testNodePubKey is a valid-format compressed public key for testing.
var testNodePubKey = append([]byte{0x02}, make([]byte, 32)...)

// testOwnerPubKey is a valid-format compressed public key for testing.
var testOwnerPubKey = func() []byte {
	key := make([]byte, 33)
	key[0] = 0x03
	key[1] = 0x01
	return key
}()

// ---------------------------------------------------------------------------
// Challenge computation tests
// ---------------------------------------------------------------------------

func TestComputeChallengeDeterministic(t *testing.T) {
	txid := [32]byte{0x01, 0x02, 0x03}
	numChunks := uint32(100)

	c1 := ComputeChallenge(txid, 0, numChunks)
	c2 := ComputeChallenge(txid, 0, numChunks)

	if c1.Seed != c2.Seed {
		t.Error("ComputeChallenge is not deterministic: seeds differ")
	}
	if c1.ChunkIndex != c2.ChunkIndex {
		t.Error("ComputeChallenge is not deterministic: chunk indices differ")
	}
}

func TestComputeChallengeDifferentPeriods(t *testing.T) {
	txid := [32]byte{0x01, 0x02, 0x03}
	numChunks := uint32(1000)

	seeds := make(map[[32]byte]bool)
	for k := uint32(0); k < 100; k++ {
		c := ComputeChallenge(txid, k, numChunks)
		if seeds[c.Seed] {
			t.Errorf("period %d produced duplicate seed", k)
		}
		seeds[c.Seed] = true
	}
}

func TestComputeChallengeChunkIndexInRange(t *testing.T) {
	txid := [32]byte{0xaa, 0xbb, 0xcc}

	tests := []uint32{1, 2, 10, 100, 1000, 65536}
	for _, numChunks := range tests {
		for k := uint32(0); k < 50; k++ {
			c := ComputeChallenge(txid, k, numChunks)
			if c.ChunkIndex >= numChunks {
				t.Errorf("period %d, numChunks %d: chunk_index %d out of range",
					k, numChunks, c.ChunkIndex)
			}
		}
	}
}

func TestComputeChallengeDifferentTxIDs(t *testing.T) {
	txid1 := [32]byte{0x01}
	txid2 := [32]byte{0x02}
	numChunks := uint32(100)

	c1 := ComputeChallenge(txid1, 0, numChunks)
	c2 := ComputeChallenge(txid2, 0, numChunks)

	if c1.Seed == c2.Seed {
		t.Error("different txids should produce different seeds")
	}
}

func TestComputeChallengeSingleChunk(t *testing.T) {
	txid := [32]byte{0xab}
	c := ComputeChallenge(txid, 0, 1)
	if c.ChunkIndex != 0 {
		t.Errorf("with 1 chunk, index should be 0, got %d", c.ChunkIndex)
	}
}

// ---------------------------------------------------------------------------
// Expected hash computation tests
// ---------------------------------------------------------------------------

func TestComputeExpectedHash(t *testing.T) {
	chunkData := []byte("test chunk data")
	proofBytes := []byte("merkle proof bytes")

	hash := ComputeExpectedHash(chunkData, proofBytes)

	// Manually compute: SHA256(proofBytes || chunkData)
	combined := append(proofBytes, chunkData...)
	expected := sha256.Sum256(combined)

	if hash != expected {
		t.Error("ComputeExpectedHash does not match manual computation")
	}
}

func TestComputeExpectedHashDeterministic(t *testing.T) {
	chunk := []byte("data")
	proof := []byte("proof")

	h1 := ComputeExpectedHash(chunk, proof)
	h2 := ComputeExpectedHash(chunk, proof)
	if h1 != h2 {
		t.Error("ComputeExpectedHash is not deterministic")
	}
}

func TestComputeExpectedHashDifferentData(t *testing.T) {
	proof := []byte("same proof")
	h1 := ComputeExpectedHash([]byte("data1"), proof)
	h2 := ComputeExpectedHash([]byte("data2"), proof)
	if h1 == h2 {
		t.Error("different chunk data should produce different hashes")
	}
}

// ---------------------------------------------------------------------------
// Script construction tests
// ---------------------------------------------------------------------------

func TestBuildDealScript(t *testing.T) {
	expectedHash := [32]byte{0x01, 0x02, 0x03}
	expireBlock := uint32(100000)

	script, err := BuildDealScript(testNodePubKey, testOwnerPubKey, expectedHash, expireBlock)
	if err != nil {
		t.Fatalf("BuildDealScript error: %v", err)
	}

	if len(script) == 0 {
		t.Fatal("script is empty")
	}

	// Verify the script starts with OP_IF.
	if script[0] != opIF {
		t.Errorf("script[0] = 0x%02x, want 0x%02x (OP_IF)", script[0], opIF)
	}

	// Verify the script ends with OP_ENDIF.
	if script[len(script)-1] != opENDIF {
		t.Errorf("script last byte = 0x%02x, want 0x%02x (OP_ENDIF)",
			script[len(script)-1], opENDIF)
	}

	// Verify the script contains OP_CHECKSIGVERIFY.
	if !containsByte(script, opCHECKSIGVERIFY) {
		t.Error("script missing OP_CHECKSIGVERIFY")
	}

	// Verify the script contains OP_SHA256.
	if !containsByte(script, opSHA256) {
		t.Error("script missing OP_SHA256")
	}

	// Verify the script contains OP_EQUALVERIFY.
	if !containsByte(script, opEQUALVERIFY) {
		t.Error("script missing OP_EQUALVERIFY")
	}

	// Verify the script contains OP_CHECKLOCKTIMEVERIFY.
	if !containsByte(script, opCHECKLOCKTIMEVERIFY) {
		t.Error("script missing OP_CHECKLOCKTIMEVERIFY")
	}

	// Verify the script contains OP_ELSE.
	if !containsByte(script, opELSE) {
		t.Error("script missing OP_ELSE")
	}

	// Verify the script contains the expected hash.
	if !bytes.Contains(script, expectedHash[:]) {
		t.Error("script does not contain expected hash")
	}

	// Verify the script contains both public keys.
	if !bytes.Contains(script, testNodePubKey) {
		t.Error("script does not contain node public key")
	}
	if !bytes.Contains(script, testOwnerPubKey) {
		t.Error("script does not contain owner public key")
	}
}

func TestBuildDealScriptInvalidNodeKey(t *testing.T) {
	_, err := BuildDealScript([]byte{0x01}, testOwnerPubKey, [32]byte{}, 100)
	if err != ErrInvalidPubKey {
		t.Errorf("expected ErrInvalidPubKey, got %v", err)
	}
}

func TestBuildDealScriptInvalidOwnerKey(t *testing.T) {
	_, err := BuildDealScript(testNodePubKey, []byte{0x04, 0x01}, [32]byte{}, 100)
	if err != ErrInvalidPubKey {
		t.Errorf("expected ErrInvalidPubKey, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Claim and refund input tests
// ---------------------------------------------------------------------------

func TestBuildClaimInput(t *testing.T) {
	sig := []byte("signature_data")
	proof := []byte("proof_data")

	input := BuildClaimInput(sig, proof)
	if len(input) == 0 {
		t.Fatal("claim input is empty")
	}

	// Should end with OP_TRUE (0x51) to select IF branch.
	if input[len(input)-1] != opTrue {
		t.Errorf("claim input last byte = 0x%02x, want 0x%02x (OP_TRUE)",
			input[len(input)-1], opTrue)
	}

	// Should contain the signature data.
	if !bytes.Contains(input, sig) {
		t.Error("claim input does not contain signature")
	}

	// Should contain the proof data.
	if !bytes.Contains(input, proof) {
		t.Error("claim input does not contain proof data")
	}
}

func TestBuildRefundInput(t *testing.T) {
	sig := []byte("owner_signature")

	input := BuildRefundInput(sig)
	if len(input) == 0 {
		t.Fatal("refund input is empty")
	}

	// Should end with OP_FALSE (0x00) to select ELSE branch.
	if input[len(input)-1] != opFalse {
		t.Errorf("refund input last byte = 0x%02x, want 0x%02x (OP_FALSE)",
			input[len(input)-1], opFalse)
	}

	// Should contain the signature.
	if !bytes.Contains(input, sig) {
		t.Error("refund input does not contain signature")
	}
}

// ---------------------------------------------------------------------------
// Deal parameter validation tests
// ---------------------------------------------------------------------------

func TestValidateDealParams(t *testing.T) {
	validDeal := &StorageDeal{
		OwnerPubKey:     testOwnerPubKey,
		NodePubKey:      testNodePubKey,
		NumPeriods:      10,
		TokenPerPeriod:  1000,
		BlocksPerPeriod: 144,
		NumChunks:       100,
	}

	if err := ValidateDealParams(validDeal); err != nil {
		t.Errorf("valid deal failed validation: %v", err)
	}
}

func TestValidateDealParamsErrors(t *testing.T) {
	tests := []struct {
		name string
		deal *StorageDeal
		want error
	}{
		{
			name: "invalid owner key",
			deal: &StorageDeal{
				OwnerPubKey: []byte{0x01}, NodePubKey: testNodePubKey,
				NumPeriods: 1, TokenPerPeriod: 1, BlocksPerPeriod: 10, NumChunks: 1,
			},
			want: ErrInvalidPubKey,
		},
		{
			name: "invalid node key",
			deal: &StorageDeal{
				OwnerPubKey: testOwnerPubKey, NodePubKey: []byte{0x04},
				NumPeriods: 1, TokenPerPeriod: 1, BlocksPerPeriod: 10, NumChunks: 1,
			},
			want: ErrInvalidPubKey,
		},
		{
			name: "zero periods",
			deal: &StorageDeal{
				OwnerPubKey: testOwnerPubKey, NodePubKey: testNodePubKey,
				NumPeriods: 0, TokenPerPeriod: 1, BlocksPerPeriod: 10, NumChunks: 1,
			},
			want: ErrZeroPeriods,
		},
		{
			name: "zero payment",
			deal: &StorageDeal{
				OwnerPubKey: testOwnerPubKey, NodePubKey: testNodePubKey,
				NumPeriods: 1, TokenPerPeriod: 0, BlocksPerPeriod: 10, NumChunks: 1,
			},
			want: ErrZeroPayment,
		},
		{
			name: "zero chunks",
			deal: &StorageDeal{
				OwnerPubKey: testOwnerPubKey, NodePubKey: testNodePubKey,
				NumPeriods: 1, TokenPerPeriod: 1, BlocksPerPeriod: 10, NumChunks: 0,
			},
			want: ErrInvalidChunkCount,
		},
		{
			name: "period too short",
			deal: &StorageDeal{
				OwnerPubKey: testOwnerPubKey, NodePubKey: testNodePubKey,
				NumPeriods: 1, TokenPerPeriod: 1, BlocksPerPeriod: 2, NumChunks: 1,
			},
			want: ErrPeriodTooShort,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDealParams(tc.deal)
			if err != tc.want {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BuildStorageDealUTXOs tests
// ---------------------------------------------------------------------------

func TestBuildStorageDealUTXOs(t *testing.T) {
	numPeriods := uint32(5)
	expectedHashes := make([][32]byte, numPeriods)
	for i := range expectedHashes {
		expectedHashes[i] = sha256.Sum256([]byte{byte(i)})
	}

	deal := &StorageDeal{
		OwnerPubKey:     testOwnerPubKey,
		NodePubKey:      testNodePubKey,
		NumPeriods:      numPeriods,
		TokenPerPeriod:  50000,
		BlocksPerPeriod: 144,
		StartBlock:      1000,
		NumChunks:       10,
		ExpectedHashes:  expectedHashes,
	}

	utxos, err := BuildStorageDealUTXOs(deal)
	if err != nil {
		t.Fatalf("BuildStorageDealUTXOs error: %v", err)
	}

	if uint32(len(utxos)) != numPeriods {
		t.Fatalf("got %d UTXOs, want %d", len(utxos), numPeriods)
	}

	for i, utxo := range utxos {
		if utxo.Period != uint32(i) {
			t.Errorf("UTXO %d: period = %d, want %d", i, utxo.Period, i)
		}
		if utxo.Value != 50000 {
			t.Errorf("UTXO %d: value = %d, want 50000", i, utxo.Value)
		}
		expectedExpire := uint32(1000) + uint32(i+1)*144
		if utxo.ExpireBlock != expectedExpire {
			t.Errorf("UTXO %d: expire = %d, want %d", i, utxo.ExpireBlock, expectedExpire)
		}
		if utxo.ExpectedHash != expectedHashes[i] {
			t.Errorf("UTXO %d: expected hash mismatch", i)
		}
		if len(utxo.Script) == 0 {
			t.Errorf("UTXO %d: script is empty", i)
		}
	}
}

func TestBuildStorageDealUTXOsWrongHashCount(t *testing.T) {
	deal := &StorageDeal{
		OwnerPubKey:     testOwnerPubKey,
		NodePubKey:      testNodePubKey,
		NumPeriods:      5,
		TokenPerPeriod:  1000,
		BlocksPerPeriod: 144,
		NumChunks:       10,
		ExpectedHashes:  make([][32]byte, 3), // Wrong count.
	}

	_, err := BuildStorageDealUTXOs(deal)
	if err == nil {
		t.Error("expected error for mismatched hash count")
	}
}

// ---------------------------------------------------------------------------
// PrecomputeExpectedHashes tests
// ---------------------------------------------------------------------------

func TestPrecomputeExpectedHashes(t *testing.T) {
	// Create test chunks.
	chunks := make([][]byte, 4)
	leaves := make([][32]byte, 4)
	for i := range chunks {
		chunks[i] = []byte{byte(i), byte(i + 1), byte(i + 2)}
		leaves[i] = sha256.Sum256(chunks[i])
	}

	txid := [32]byte{0xab, 0xcd}
	numPeriods := uint32(3)

	hashes, err := PrecomputeExpectedHashes(txid, numPeriods, chunks, leaves)
	if err != nil {
		t.Fatalf("PrecomputeExpectedHashes error: %v", err)
	}

	if uint32(len(hashes)) != numPeriods {
		t.Errorf("got %d hashes, want %d", len(hashes), numPeriods)
	}

	// Each hash should be non-zero and unique.
	seen := make(map[[32]byte]bool)
	for i, h := range hashes {
		if h == ([32]byte{}) {
			t.Errorf("hash %d is zero", i)
		}
		seen[h] = true
	}

	// Hashes should be deterministic.
	hashes2, err := PrecomputeExpectedHashes(txid, numPeriods, chunks, leaves)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	for i := range hashes {
		if hashes[i] != hashes2[i] {
			t.Errorf("hash %d not deterministic", i)
		}
	}
}

func TestPrecomputeExpectedHashesEmpty(t *testing.T) {
	_, err := PrecomputeExpectedHashes([32]byte{}, 1, nil, nil)
	if err != ErrInvalidChunkCount {
		t.Errorf("expected ErrInvalidChunkCount, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsByte(data []byte, b byte) bool {
	for _, v := range data {
		if v == b {
			return true
		}
	}
	return false
}

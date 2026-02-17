// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package chain

import (
	"crypto/sha256"
	"math/big"
	"testing"
	"time"
)

// sha256Sum256 is a test helper for computing SHA256.
func sha256Sum256(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// ---------------------------------------------------------------------------
// Params tests
// ---------------------------------------------------------------------------

func TestMainNetParams(t *testing.T) {
	p := MainNetParams()

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"Name", p.Name, "mainnet"},
		{"MaxBlockSize", p.MaxBlockSize, uint32(32 * 1024 * 1024)},
		{"TargetBlockTime", p.TargetBlockTime, 10 * time.Minute},
		{"DifficultyAdjustmentInterval", p.DifficultyAdjustmentInterval, uint32(2016)},
		{"InitialReward", p.InitialReward, uint64(5_000_000_000)},
		{"HalvingInterval", p.HalvingInterval, uint32(210_000)},
		{"MaxTotalSupply", p.MaxTotalSupply, uint64(2_100_000_000_000_000)},
		{"SatoshiPerToken", p.SatoshiPerToken, uint64(100_000_000)},
		{"MinFeePerByte", p.MinFeePerByte, uint64(1)},
		{"AnchorInterval", p.AnchorInterval, uint32(100)},
		{"AnchorMagic", string(p.AnchorMagic[:]), "MNTA"},
		{"AuxPowMagic", string(p.AuxPowMagic[:]), "MNMP"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestTestNetParams(t *testing.T) {
	p := TestNetParams()
	if p.Name != "testnet" {
		t.Errorf("TestNetParams.Name = %q, want %q", p.Name, "testnet")
	}
	// Testnet should have the same economic parameters.
	if p.InitialReward != InitialRewardSat {
		t.Errorf("TestNetParams.InitialReward = %d, want %d", p.InitialReward, InitialRewardSat)
	}
}

func TestMagicBytes(t *testing.T) {
	anchor := AnchorMagicBytes()
	if string(anchor[:]) != "MNTA" {
		t.Errorf("AnchorMagicBytes = %q, want %q", string(anchor[:]), "MNTA")
	}
	auxpow := AuxPowMagicBytes()
	if string(auxpow[:]) != "MNMP" {
		t.Errorf("AuxPowMagicBytes = %q, want %q", string(auxpow[:]), "MNMP")
	}
}

// ---------------------------------------------------------------------------
// Token economics tests
// ---------------------------------------------------------------------------

func TestBlockReward(t *testing.T) {
	tests := []struct {
		name   string
		height uint32
		want   uint64
	}{
		{"genesis", 0, 5_000_000_000},
		{"era0_mid", 100_000, 5_000_000_000},
		{"era0_last", 209_999, 5_000_000_000},
		{"era1_first", 210_000, 2_500_000_000},
		{"era1_last", 419_999, 2_500_000_000},
		{"era2_first", 420_000, 1_250_000_000},
		{"era3_first", 630_000, 625_000_000},
		{"era4_first", 840_000, 312_500_000},
		{"era10_first", 2_100_000, 5_000_000_000 >> 10},
		{"era20_first", 4_200_000, 5_000_000_000 >> 20},
		{"era32_first", 6_720_000, 5_000_000_000 >> 32}, // 1 satoshi
		{"era33_first", 6_930_000, 0},                     // Reward exhausted.
		{"era34_first", 7_140_000, 0},
		{"very_high", 100_000_000, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BlockReward(tc.height)
			if got != tc.want {
				t.Errorf("BlockReward(%d) = %d, want %d", tc.height, got, tc.want)
			}
		})
	}
}

func TestHalvingEra(t *testing.T) {
	tests := []struct {
		height uint32
		want   uint32
	}{
		{0, 0},
		{1, 0},
		{209_999, 0},
		{210_000, 1},
		{419_999, 1},
		{420_000, 2},
		{6_930_000, 33},
	}

	for _, tc := range tests {
		got := HalvingEra(tc.height)
		if got != tc.want {
			t.Errorf("HalvingEra(%d) = %d, want %d", tc.height, got, tc.want)
		}
	}
}

func TestTotalSupplyAtHeight(t *testing.T) {
	tests := []struct {
		name   string
		height uint32
		want   uint64
	}{
		{"block_0", 0, 5_000_000_000},
		{"block_1", 1, 10_000_000_000},
		{"era0_end", 209_999, 210_000 * 5_000_000_000},
		{"era1_first", 210_000, 210_000*5_000_000_000 + 2_500_000_000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TotalSupplyAtHeight(tc.height)
			if got != tc.want {
				t.Errorf("TotalSupplyAtHeight(%d) = %d, want %d", tc.height, got, tc.want)
			}
		})
	}
}

func TestMaxSupplySatoshis(t *testing.T) {
	max := MaxSupplySatoshis()
	// The theoretical maximum for Bitcoin-like emission with 50*10^8 initial
	// reward and 210,000 block halving is very close to 21,000,000 * 10^8.
	// Due to integer truncation during halving, it is slightly less.
	if max > MaxMNTSupplySat {
		t.Errorf("MaxSupplySatoshis() = %d, exceeds MaxMNTSupplySat %d", max, MaxMNTSupplySat)
	}
	// It should be very close (within 1 MNT / 10^8 sat of 21M MNT).
	diff := MaxMNTSupplySat - max
	if diff > SatoshiPerMNT {
		t.Errorf("MaxSupplySatoshis() = %d, too far from 21M MNT (%d), diff = %d",
			max, MaxMNTSupplySat, diff)
	}
}

func TestBlockRewardReachesZero(t *testing.T) {
	// After MaxHalvingEras, reward should be zero.
	height := MaxHalvingEras * HalvingIntervalBlocks
	if BlockReward(height) != 0 {
		t.Errorf("BlockReward at era %d should be 0, got %d", MaxHalvingEras, BlockReward(height))
	}
}

// ---------------------------------------------------------------------------
// Block header tests
// ---------------------------------------------------------------------------

func TestSerializeDeserializeBlockHeader(t *testing.T) {
	original := &BlockHeader{
		Version:    1,
		PrevHash:   [32]byte{0x01, 0x02, 0x03},
		MerkleRoot: [32]byte{0xaa, 0xbb, 0xcc},
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      42,
	}

	serialized := SerializeBlockHeader(original)
	deserialized := DeserializeBlockHeader(serialized)

	if deserialized.Version != original.Version {
		t.Errorf("Version: got %d, want %d", deserialized.Version, original.Version)
	}
	if deserialized.PrevHash != original.PrevHash {
		t.Errorf("PrevHash mismatch")
	}
	if deserialized.MerkleRoot != original.MerkleRoot {
		t.Errorf("MerkleRoot mismatch")
	}
	if deserialized.Timestamp != original.Timestamp {
		t.Errorf("Timestamp: got %d, want %d", deserialized.Timestamp, original.Timestamp)
	}
	if deserialized.Bits != original.Bits {
		t.Errorf("Bits: got %d, want %d", deserialized.Bits, original.Bits)
	}
	if deserialized.Nonce != original.Nonce {
		t.Errorf("Nonce: got %d, want %d", deserialized.Nonce, original.Nonce)
	}
}

func TestHashBlockHeaderDeterministic(t *testing.T) {
	h := &BlockHeader{
		Version:    1,
		Timestamp:  1700000000,
		Bits:       0x1d00ffff,
		Nonce:      12345,
	}
	hash1 := HashBlockHeader(h)
	hash2 := HashBlockHeader(h)
	if hash1 != hash2 {
		t.Error("HashBlockHeader is not deterministic")
	}
}

func TestHashBlockHeaderDoublesha256(t *testing.T) {
	// Verify HashBlockHeader produces a double-SHA256.
	h := &BlockHeader{Version: 1}
	serialized := SerializeBlockHeader(h)
	hash := HashBlockHeader(h)

	// Manual double-SHA256 computation using crypto/sha256 (imported at package level).
	first := sha256Sum256(serialized[:])
	manual := sha256Sum256(first[:])

	if hash != manual {
		t.Errorf("HashBlockHeader does not match manual double-SHA256")
	}

	// The hash should not be all zeros.
	zero := [32]byte{}
	if hash == zero {
		t.Error("HashBlockHeader returned all zeros for non-trivial header")
	}
}

func TestHashBlockHeaderIsDoubleSHA256(t *testing.T) {
	h := &BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      0x1d00ffff,
		Nonce:     42,
	}
	hash := HashBlockHeader(h)

	// Not all zeros.
	zero := [32]byte{}
	if hash == zero {
		t.Error("HashBlockHeader returned all-zero hash")
	}

	// Different headers produce different hashes.
	h2 := *h
	h2.Nonce = 43
	hash2 := HashBlockHeader(&h2)
	if hash == hash2 {
		t.Error("Different headers produced same hash")
	}
}

// ---------------------------------------------------------------------------
// CompactToBig / BigToCompact tests
// ---------------------------------------------------------------------------

func TestCompactToBigRoundtrip(t *testing.T) {
	tests := []uint32{
		0x1d00ffff, // Bitcoin genesis difficulty.
		0x1b0404cb, // Typical Bitcoin difficulty.
		0x1a44b9f2, // Higher difficulty.
		0x18009645, // Very high difficulty.
		0x03000001, // Minimum non-zero.
	}

	for _, compact := range tests {
		target := CompactToBig(compact)
		back := BigToCompact(target)
		// Roundtrip may not be bit-exact due to normalization,
		// but the resulting target should be the same.
		target2 := CompactToBig(back)
		if target.Cmp(target2) != 0 {
			t.Errorf("CompactToBig roundtrip: 0x%08x -> %s -> 0x%08x -> %s",
				compact, target.String(), back, target2.String())
		}
	}
}

func TestCompactToBigZero(t *testing.T) {
	target := CompactToBig(0)
	if target.Sign() != 0 {
		t.Errorf("CompactToBig(0) = %s, want 0", target.String())
	}
}

func TestBigToCompactZero(t *testing.T) {
	compact := BigToCompact(big.NewInt(0))
	if compact != 0 {
		t.Errorf("BigToCompact(0) = 0x%08x, want 0", compact)
	}
}

// ---------------------------------------------------------------------------
// HashMeetsTarget tests
// ---------------------------------------------------------------------------

func TestHashMeetsTarget(t *testing.T) {
	// With the maximum target (0x1d00ffff), almost any hash should meet it.
	maxBits := uint32(0x1d00ffff)
	hash := [32]byte{0x00, 0x00, 0x00, 0x01}
	if !HashMeetsTarget(hash, maxBits) {
		t.Error("Hash with leading zeros should meet max target")
	}

	// With a very low target (0x03000001 = 1), only hash=0 should meet it.
	lowBits := uint32(0x03000001)
	allZeros := [32]byte{}
	if !HashMeetsTarget(allZeros, lowBits) {
		t.Error("All-zero hash should meet any positive target")
	}

	// A hash with high value should not meet a low target.
	highHash := [32]byte{0xff, 0xff, 0xff, 0xff}
	if HashMeetsTarget(highHash, lowBits) {
		t.Error("High hash should not meet low target")
	}
}

func TestHashMeetsTargetZeroBits(t *testing.T) {
	hash := [32]byte{}
	if HashMeetsTarget(hash, 0) {
		t.Error("Zero bits (zero target) should reject all hashes")
	}
}

// ---------------------------------------------------------------------------
// ComputeMerkleRoot tests
// ---------------------------------------------------------------------------

func TestComputeMerkleRootEmpty(t *testing.T) {
	root := ComputeMerkleRoot(nil)
	if root != ([32]byte{}) {
		t.Error("ComputeMerkleRoot(nil) should return zero hash")
	}
}

func TestComputeMerkleRootSingleTx(t *testing.T) {
	txHash := [32]byte{0x01, 0x02, 0x03}
	root := ComputeMerkleRoot([][32]byte{txHash})
	if root != txHash {
		t.Error("ComputeMerkleRoot with single tx should return that tx hash")
	}
}

func TestComputeMerkleRootTwoTxs(t *testing.T) {
	tx1 := [32]byte{0x01}
	tx2 := [32]byte{0x02}
	root := ComputeMerkleRoot([][32]byte{tx1, tx2})

	// Root should not be zero.
	if root == ([32]byte{}) {
		t.Error("ComputeMerkleRoot with two txs should not return zero")
	}

	// Root should differ from either input.
	if root == tx1 || root == tx2 {
		t.Error("Root should not equal either input hash")
	}
}

func TestComputeMerkleRootOddTxs(t *testing.T) {
	tx1 := [32]byte{0x01}
	tx2 := [32]byte{0x02}
	tx3 := [32]byte{0x03}
	root := ComputeMerkleRoot([][32]byte{tx1, tx2, tx3})
	if root == ([32]byte{}) {
		t.Error("ComputeMerkleRoot with odd txs should not return zero")
	}
}

func TestComputeMerkleRootDeterministic(t *testing.T) {
	txs := [][32]byte{{0x01}, {0x02}, {0x03}, {0x04}}
	root1 := ComputeMerkleRoot(txs)
	root2 := ComputeMerkleRoot(txs)
	if root1 != root2 {
		t.Error("ComputeMerkleRoot is not deterministic")
	}
}

func TestComputeMerkleRootOrderMatters(t *testing.T) {
	tx1 := [32]byte{0x01}
	tx2 := [32]byte{0x02}
	root1 := ComputeMerkleRoot([][32]byte{tx1, tx2})
	root2 := ComputeMerkleRoot([][32]byte{tx2, tx1})
	if root1 == root2 {
		t.Error("ComputeMerkleRoot should produce different roots for different orders")
	}
}

// ---------------------------------------------------------------------------
// Genesis block tests
// ---------------------------------------------------------------------------

func TestGenesisBlockNotNil(t *testing.T) {
	gb := GenesisBlock()
	if gb == nil {
		t.Fatal("GenesisBlock() returned nil")
	}
	if len(gb.Txs) == 0 {
		t.Error("Genesis block has no transactions")
	}
}

func TestGenesisBlockHeader(t *testing.T) {
	h := GenesisBlockHeader()
	if h == nil {
		t.Fatal("GenesisBlockHeader() returned nil")
	}
	if h.Version != 1 {
		t.Errorf("Genesis version = %d, want 1", h.Version)
	}
	if h.PrevHash != ([32]byte{}) {
		t.Error("Genesis PrevHash should be all zeros")
	}
}

func TestGenesisBlockHashDeterministic(t *testing.T) {
	hash1 := GenesisBlockHash()
	hash2 := GenesisBlockHash()
	if hash1 != hash2 {
		t.Error("GenesisBlockHash is not deterministic")
	}
	if hash1 == ([32]byte{}) {
		t.Error("GenesisBlockHash should not be all zeros")
	}
}

func TestGenesisBlockHashHex(t *testing.T) {
	hexStr := GenesisBlockHashHex()
	if len(hexStr) != 64 {
		t.Errorf("GenesisBlockHashHex length = %d, want 64", len(hexStr))
	}
}

func TestGenesisCoinbaseMessage(t *testing.T) {
	msg := GenesisCoinbaseMessage()
	expected := "Metanet: Decentralized CDN on Bitcoin"
	if string(msg) != expected {
		t.Errorf("GenesisCoinbaseMessage = %q, want %q", string(msg), expected)
	}
}

func TestGenesisCoinbaseContainsMessage(t *testing.T) {
	gb := GenesisBlock()
	if len(gb.Txs) == 0 {
		t.Fatal("Genesis block has no transactions")
	}
	coinbase := gb.Txs[0]
	msg := []byte(GenesisMessage)

	// Check that the coinbase bytes contain the genesis message.
	found := false
	for i := 0; i <= len(coinbase)-len(msg); i++ {
		match := true
		for j := 0; j < len(msg); j++ {
			if coinbase[i+j] != msg[j] {
				match = false
				break
			}
		}
		if match {
			found = true
			break
		}
	}
	if !found {
		t.Error("Genesis coinbase does not contain the genesis message")
	}
}

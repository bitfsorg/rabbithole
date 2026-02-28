// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

//go:build integration

package integration

import (
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/bitfsorg/metanet/internal/chain"
	"github.com/bitfsorg/metanet/internal/mining"
)

// ---------------------------------------------------------------------------
// TestGenesisToBlock10 — build 10 sequential blocks from genesis and validate
// the chain links and rewards.
// ---------------------------------------------------------------------------

func TestGenesisToBlock10(t *testing.T) {
	genesis := chain.GenesisBlockHeader()
	prevHash := chain.HashBlockHeader(genesis)

	headers := make([]*chain.BlockHeader, 11) // genesis + 10
	headers[0] = genesis

	for i := 1; i <= 10; i++ {
		h := &chain.BlockHeader{
			Version:   1,
			PrevHash:  prevHash,
			Timestamp: genesis.Timestamp + uint32(i*600),
			Bits:      genesis.Bits,
			Nonce:     uint32(i * 7),
		}
		// Compute a Merkle root for a single dummy coinbase tx.
		coinbaseTxHash := doubleSHA256([]byte{byte(i)})
		h.MerkleRoot = coinbaseTxHash
		headers[i] = h
		prevHash = chain.HashBlockHeader(h)
	}

	t.Run("chain_links_correctly", func(t *testing.T) {
		for i := 1; i <= 10; i++ {
			expectedPrevHash := chain.HashBlockHeader(headers[i-1])
			if headers[i].PrevHash != expectedPrevHash {
				t.Errorf("block %d: PrevHash does not reference block %d", i, i-1)
			}
		}
	})

	t.Run("all_hashes_unique", func(t *testing.T) {
		seen := make(map[[32]byte]bool)
		for i := 0; i <= 10; i++ {
			h := chain.HashBlockHeader(headers[i])
			if seen[h] {
				t.Errorf("block %d: duplicate hash", i)
			}
			seen[h] = true
		}
	})

	t.Run("block_rewards_50_MNT", func(t *testing.T) {
		for i := uint32(0); i <= 10; i++ {
			reward := chain.BlockReward(i)
			if reward != chain.InitialRewardSat {
				t.Errorf("block %d: reward = %d, want %d (50 MNT)", i, reward, chain.InitialRewardSat)
			}
		}
	})

	t.Run("genesis_has_zero_prevhash", func(t *testing.T) {
		if headers[0].PrevHash != ([32]byte{}) {
			t.Error("genesis PrevHash should be all zeros")
		}
	})
}

// ---------------------------------------------------------------------------
// TestMergedMiningFullCycle — construct a complete AuxPoW and validate it,
// then tamper and verify failure.
// ---------------------------------------------------------------------------

func TestMergedMiningFullCycle(t *testing.T) {
	// Step 1: Create Metanet Chain block header with easy difficulty.
	testBits := uint32(0x2f00ffff)
	metanetHeader := chain.BlockHeader{
		Version:   1,
		Timestamp: 1700000000,
		Bits:      testBits,
		Nonce:     42,
	}

	// Step 2: Compute block hash.
	metanetBlockHash := chain.HashBlockHeader(&metanetHeader)

	// Step 3: Build coinbase commitment (MNMP + block hash).
	commitment := mining.BuildCoinbaseCommitment(metanetBlockHash)
	if len(commitment) != 38 {
		t.Fatalf("commitment length = %d, want 38", len(commitment))
	}

	// Step 4: Create coinbase tx containing the commitment.
	coinbaseTx := []byte{0x01, 0x00, 0x00, 0x00} // tx version
	coinbaseTx = append(coinbaseTx, commitment[2:]...)   // skip OP_RETURN + push byte, embed MNMP+hash
	coinbaseTx = append(coinbaseTx, 0x00, 0x00, 0x00)    // padding

	// Step 5: Compute coinbase TxID.
	coinbaseTxID := doubleSHA256(coinbaseTx)

	// Step 6: Create parent (BTC) block header.
	// Single tx = Merkle root is the coinbase TxID.
	parentHeader := chain.BlockHeader{
		Version:    1,
		MerkleRoot: coinbaseTxID,
		Timestamp:  1700000000,
		Bits:       testBits,
	}
	serializedParent := chain.SerializeBlockHeader(&parentHeader)

	// Step 7: Construct AuxPoWHeader.
	auxpow := &mining.AuxPoWHeader{
		Header:         metanetHeader,
		ParentHeader:   serializedParent,
		CoinbaseTx:     coinbaseTx,
		CoinbaseBranch: nil, // Single tx = empty branch.
		CoinbaseIndex:  0,
	}

	params := chain.MainNetParams()

	t.Run("valid_auxpow_passes", func(t *testing.T) {
		err := mining.ValidateAuxPoW(auxpow, params)
		if err != nil {
			t.Errorf("ValidateAuxPoW failed: %v", err)
		}
	})

	t.Run("tampered_hash_fails", func(t *testing.T) {
		// Modify the Metanet block header so its hash no longer matches commitment.
		tamperedHeader := metanetHeader
		tamperedHeader.Nonce = 999999

		tamperedAuxPow := &mining.AuxPoWHeader{
			Header:         tamperedHeader,
			ParentHeader:   serializedParent,
			CoinbaseTx:     coinbaseTx,
			CoinbaseBranch: nil,
			CoinbaseIndex:  0,
		}

		err := mining.ValidateAuxPoW(tamperedAuxPow, params)
		if err == nil {
			t.Error("ValidateAuxPoW should fail with tampered block hash")
		}
	})

	t.Run("commitment_found_in_coinbase", func(t *testing.T) {
		found := mining.FindAuxPoWCommitment(coinbaseTx)
		if found == nil {
			t.Fatal("FindAuxPoWCommitment returned nil")
		}
		for i := 0; i < 32; i++ {
			if found[i] != metanetBlockHash[i] {
				t.Errorf("committed hash byte %d mismatch", i)
				break
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestDifficultyAdjustmentAcross2016Blocks — simulate difficulty retargets.
// ---------------------------------------------------------------------------

func TestDifficultyAdjustmentAcross2016Blocks(t *testing.T) {
	initialBits := uint32(0x1d00ffff)

	t.Run("exact_timespan_no_change", func(t *testing.T) {
		newBits := mining.CalcNextDifficulty(initialBits, chain.ExpectedRetargetTimespan)
		oldTarget := chain.CompactToBig(initialBits)
		newTarget := chain.CompactToBig(newBits)
		if oldTarget.Cmp(newTarget) != 0 {
			t.Error("exact timespan should not change difficulty")
		}
	})

	t.Run("fast_blocks_increase_difficulty", func(t *testing.T) {
		// Half the expected time -> target halves -> difficulty doubles.
		fastTimespan := chain.ExpectedRetargetTimespan / 2
		newBits := mining.CalcNextDifficulty(initialBits, fastTimespan)
		oldTarget := chain.CompactToBig(initialBits)
		newTarget := chain.CompactToBig(newBits)

		if newTarget.Cmp(oldTarget) >= 0 {
			t.Error("fast blocks should decrease target (increase difficulty)")
		}

		// Target should be approximately half.
		expectedTarget := new(big.Int).Div(oldTarget, big.NewInt(2))
		diff := new(big.Int).Sub(expectedTarget, newTarget)
		diff.Abs(diff)
		tolerance := new(big.Int).Div(expectedTarget, big.NewInt(100))
		if diff.Cmp(tolerance) > 0 {
			t.Errorf("fast blocks: target not ~half (diff=%s, tolerance=%s)",
				diff.String(), tolerance.String())
		}
	})

	t.Run("slow_blocks_decrease_difficulty", func(t *testing.T) {
		// Use a harder difficulty (lower target) so there's room to increase.
		harderBits := uint32(0x1b0404cb)
		slowTimespan := chain.ExpectedRetargetTimespan * 2
		newBits := mining.CalcNextDifficulty(harderBits, slowTimespan)
		oldTarget := chain.CompactToBig(harderBits)
		newTarget := chain.CompactToBig(newBits)

		if newTarget.Cmp(oldTarget) <= 0 {
			t.Error("slow blocks should increase target (decrease difficulty)")
		}

		// Target should be approximately doubled.
		expectedTarget := new(big.Int).Mul(oldTarget, big.NewInt(2))
		diff := new(big.Int).Sub(expectedTarget, newTarget)
		diff.Abs(diff)
		tolerance := new(big.Int).Div(expectedTarget, big.NewInt(100))
		if diff.Cmp(tolerance) > 0 {
			t.Errorf("slow blocks: target not ~doubled")
		}
	})

	t.Run("clamp_at_4x_bounds", func(t *testing.T) {
		// Extremely fast: should clamp to expected/4.
		veryFast := mining.CalcNextDifficulty(initialBits, 1)
		clampedFast := mining.CalcNextDifficulty(initialBits, chain.ExpectedRetargetTimespan/4)
		if veryFast != clampedFast {
			t.Error("extremely fast blocks should clamp to 4x max increase")
		}

		// Extremely slow: should clamp to expected*4.
		verySlow := mining.CalcNextDifficulty(initialBits, 999_999_999)
		clampedSlow := mining.CalcNextDifficulty(initialBits, chain.ExpectedRetargetTimespan*4)
		if verySlow != clampedSlow {
			t.Error("extremely slow blocks should clamp to 4x max decrease")
		}
	})

	t.Run("never_exceeds_max_target", func(t *testing.T) {
		maxBits := uint32(0x1d00ffff)
		maxTarget := chain.CompactToBig(maxBits)
		slowTimespan := chain.ExpectedRetargetTimespan * chain.MaxRetargetFactor
		newBits := mining.CalcNextDifficulty(maxBits, slowTimespan)
		newTarget := chain.CompactToBig(newBits)
		if newTarget.Cmp(maxTarget) > 0 {
			t.Error("new target should not exceed maximum")
		}
	})
}

// ---------------------------------------------------------------------------
// TestBSVAnchorChain — create sequence of blocks, anchor every 5, validate
// anchor chain linkage and Merkle roots.
// ---------------------------------------------------------------------------

func TestBSVAnchorChain(t *testing.T) {
	// Create 10 block headers.
	headers := make([]chain.BlockHeader, 10)
	for i := range headers {
		headers[i] = chain.BlockHeader{
			Version:   1,
			Timestamp: uint32(1700000000 + i*600),
			Nonce:     uint32(i * 13),
		}
		if i > 0 {
			headers[i].PrevHash = chain.HashBlockHeader(&headers[i-1])
		}
	}

	t.Run("build_two_anchors", func(t *testing.T) {
		// Anchor 0: blocks 0-4.
		prevTxID := [32]byte{} // Genesis anchor has zero prev.
		anchor0, err := mining.BuildAnchorTx(0, 4, headers[0:5], prevTxID)
		if err != nil {
			t.Fatalf("BuildAnchorTx(0-4): %v", err)
		}
		if anchor0.BlockCount != 5 {
			t.Errorf("anchor0 BlockCount = %d, want 5", anchor0.BlockCount)
		}
		if anchor0.MerkleRoot == ([32]byte{}) {
			t.Error("anchor0 MerkleRoot should not be zero")
		}

		// Anchor 1: blocks 5-9, referencing anchor 0.
		anchor0TxID := mining.ComputeAnchorTxID(anchor0)
		anchor1, err := mining.BuildAnchorTx(5, 9, headers[5:10], anchor0TxID)
		if err != nil {
			t.Fatalf("BuildAnchorTx(5-9): %v", err)
		}
		if anchor1.PrevAnchorTx != anchor0TxID {
			t.Error("anchor1 PrevAnchorTx should reference anchor0")
		}

		// Validate chain linkage.
		err = mining.ValidateAnchorChain([]*mining.AnchorTx{anchor0, anchor1})
		if err != nil {
			t.Errorf("ValidateAnchorChain: %v", err)
		}
	})

	t.Run("verify_merkle_roots_over_ranges", func(t *testing.T) {
		// Manually compute Merkle root for blocks 0-4.
		root := mining.BuildBlockRangeMerkleRoot(headers[0:5])
		if root == ([32]byte{}) {
			t.Error("Merkle root for range 0-4 should not be zero")
		}

		// Verify it's deterministic.
		root2 := mining.BuildBlockRangeMerkleRoot(headers[0:5])
		if root != root2 {
			t.Error("Merkle root should be deterministic")
		}

		// Different range produces different root.
		root3 := mining.BuildBlockRangeMerkleRoot(headers[5:10])
		if root == root3 {
			t.Error("different block ranges should produce different Merkle roots")
		}
	})

	t.Run("anchor_serialization_roundtrip", func(t *testing.T) {
		anchor, _ := mining.BuildAnchorTx(0, 4, headers[0:5], [32]byte{0xaa})
		data := mining.SerializeAnchorData(anchor)
		decoded, err := mining.DeserializeAnchorData(data)
		if err != nil {
			t.Fatalf("DeserializeAnchorData: %v", err)
		}
		if decoded.StartHeight != anchor.StartHeight {
			t.Error("StartHeight mismatch after roundtrip")
		}
		if decoded.EndHeight != anchor.EndHeight {
			t.Error("EndHeight mismatch after roundtrip")
		}
		if decoded.MerkleRoot != anchor.MerkleRoot {
			t.Error("MerkleRoot mismatch after roundtrip")
		}
	})

	t.Run("broken_anchor_chain_detected", func(t *testing.T) {
		anchor0, _ := mining.BuildAnchorTx(0, 4, headers[0:5], [32]byte{})
		// Create anchor1 with wrong prev reference.
		wrongAnchor1 := &mining.AnchorTx{
			Version:      mining.AnchorVersion,
			Flag:         [4]byte{'M', 'N', 'T', 'A'},
			StartHeight:  5,
			EndHeight:    9,
			BlockCount:   5,
			PrevAnchorTx: [32]byte{0xff}, // Wrong.
		}
		err := mining.ValidateAnchorChain([]*mining.AnchorTx{anchor0, wrongAnchor1})
		if err == nil {
			t.Error("should detect broken anchor chain")
		}
	})
}

// ---------------------------------------------------------------------------
// TestTokenEconomicsFullSchedule — walk through all 33 halving eras and
// verify the full emission schedule.
// ---------------------------------------------------------------------------

func TestTokenEconomicsFullSchedule(t *testing.T) {
	t.Run("reward_at_height_0", func(t *testing.T) {
		reward := chain.BlockReward(0)
		if reward != 5_000_000_000 {
			t.Errorf("reward at height 0 = %d, want 5000000000 (50 MNT)", reward)
		}
	})

	t.Run("reward_at_height_209999", func(t *testing.T) {
		reward := chain.BlockReward(209_999)
		if reward != 5_000_000_000 {
			t.Errorf("reward at height 209999 = %d, want 5000000000 (50 MNT)", reward)
		}
	})

	t.Run("first_halving_at_210000", func(t *testing.T) {
		reward := chain.BlockReward(210_000)
		if reward != 2_500_000_000 {
			t.Errorf("reward at height 210000 = %d, want 2500000000 (25 MNT)", reward)
		}
	})

	t.Run("all_33_halving_eras", func(t *testing.T) {
		for era := uint32(0); era < chain.MaxHalvingEras; era++ {
			height := era * chain.HalvingIntervalBlocks
			reward := chain.BlockReward(height)
			expected := chain.InitialRewardSat >> era
			if reward != expected {
				t.Errorf("era %d (height %d): reward = %d, want %d", era, height, reward, expected)
			}
		}
	})

	t.Run("reward_zero_after_era_33", func(t *testing.T) {
		height := chain.MaxHalvingEras * chain.HalvingIntervalBlocks
		reward := chain.BlockReward(height)
		if reward != 0 {
			t.Errorf("reward at era 33+ = %d, want 0", reward)
		}
	})

	t.Run("total_supply_converges_to_21M", func(t *testing.T) {
		maxSupply := chain.MaxSupplySatoshis()
		// Should be very close to 21M MNT but slightly less due to integer truncation.
		if maxSupply > chain.MaxMNTSupplySat {
			t.Errorf("max supply %d exceeds 21M MNT (%d)", maxSupply, chain.MaxMNTSupplySat)
		}
		// Should be within 1 MNT of 21M.
		diff := chain.MaxMNTSupplySat - maxSupply
		if diff > chain.SatoshiPerMNT {
			t.Errorf("max supply too far from 21M: diff = %d satoshis (> 1 MNT)", diff)
		}
	})

	t.Run("supply_monotonically_increases", func(t *testing.T) {
		prevSupply := uint64(0)
		for era := uint32(0); era < chain.MaxHalvingEras; era++ {
			height := era*chain.HalvingIntervalBlocks + 100 // Mid-era.
			supply := chain.TotalSupplyAtHeight(height)
			if supply <= prevSupply {
				t.Errorf("supply at era %d (%d) should exceed era %d (%d)",
					era, supply, era-1, prevSupply)
			}
			prevSupply = supply
		}
	})
}

// doubleSHA256 computes SHA256(SHA256(data)).
func doubleSHA256(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

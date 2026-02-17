// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package chain

// BlockReward computes the block reward in satoshis for a given block height,
// accounting for the halving schedule.
//
// The reward halves every HalvingIntervalBlocks (210,000) blocks:
//   Era 0 (blocks 0-209,999):       50.00000000 MNT = 5,000,000,000 sat
//   Era 1 (blocks 210,000-419,999): 25.00000000 MNT = 2,500,000,000 sat
//   Era 2 (blocks 420,000-629,999): 12.50000000 MNT = 1,250,000,000 sat
//   ...
//   Era 33+ (blocks 6,930,000+):     0 MNT (reward exhausted)
//
// This matches the Bitcoin emission curve exactly.
func BlockReward(height uint32) uint64 {
	era := HalvingEra(height)
	if era >= MaxHalvingEras {
		return 0
	}
	// Right-shift the initial reward by the era number.
	// 5,000,000,000 >> 0 = 5,000,000,000 (50 MNT)
	// 5,000,000,000 >> 1 = 2,500,000,000 (25 MNT)
	// ...
	// 5,000,000,000 >> 32 = 1 (0.00000001 MNT)
	// 5,000,000,000 >> 33 = 0
	return InitialRewardSat >> era
}

// HalvingEra returns the halving era (0-indexed) for a given block height.
// Era 0 is blocks [0, HalvingInterval), Era 1 is [HalvingInterval, 2*HalvingInterval), etc.
func HalvingEra(height uint32) uint32 {
	return height / HalvingIntervalBlocks
}

// TotalSupplyAtHeight computes the cumulative MNT supply (in satoshis)
// mined from genesis through the given block height (inclusive).
//
// This sums the rewards for all blocks from 0 to height.
func TotalSupplyAtHeight(height uint32) uint64 {
	var total uint64
	currentEra := HalvingEra(height)

	for era := uint32(0); era <= currentEra; era++ {
		reward := BlockReward(era * HalvingIntervalBlocks)
		if reward == 0 {
			break
		}

		eraStart := era * HalvingIntervalBlocks
		eraEnd := (era+1)*HalvingIntervalBlocks - 1

		if eraEnd > height {
			eraEnd = height
		}

		blocksInEra := uint64(eraEnd-eraStart) + 1
		total += blocksInEra * reward
	}

	return total
}

// MaxSupplySatoshis returns the theoretical maximum supply, which is
// the sum of all block rewards across all halving eras.
// This should equal (or be very close to) 2,100,000,000,000,000 satoshis
// (21,000,000 MNT).
func MaxSupplySatoshis() uint64 {
	var total uint64
	for era := uint32(0); era < MaxHalvingEras; era++ {
		reward := InitialRewardSat >> era
		if reward == 0 {
			break
		}
		total += uint64(HalvingIntervalBlocks) * reward
	}
	return total
}

// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package mining

import (
	"math/big"

	"github.com/bitfsorg/metanet/internal/chain"
)

// CalcNextDifficulty computes the new compact difficulty target for the next
// retarget period using the Bitcoin difficulty adjustment algorithm.
//
// The algorithm:
//   1. Compute the actual timespan for the last 2016 blocks.
//   2. Clamp the timespan to [expected/4, expected*4] to prevent wild swings.
//   3. new_target = old_target * clamped_timespan / expected_timespan
//
// Parameters:
//   - oldBits: the compact target from the previous retarget period.
//   - actualTimespan: seconds elapsed during the last 2016-block period.
//
// Returns the new compact target.
func CalcNextDifficulty(oldBits uint32, actualTimespan int64) uint32 {
	expected := chain.ExpectedRetargetTimespan

	// Clamp the actual timespan to prevent extreme adjustments.
	// Minimum: expected / 4 (difficulty can at most quadruple).
	// Maximum: expected * 4 (difficulty can at most quarter).
	minTimespan := expected / chain.MaxRetargetFactor
	maxTimespan := expected * chain.MaxRetargetFactor

	clamped := actualTimespan
	if clamped < minTimespan {
		clamped = minTimespan
	}
	if clamped > maxTimespan {
		clamped = maxTimespan
	}

	// Convert old target to big.Int.
	oldTarget := chain.CompactToBig(oldBits)

	// new_target = old_target * clamped_timespan / expected_timespan
	newTarget := new(big.Int).Mul(oldTarget, big.NewInt(clamped))
	newTarget.Div(newTarget, big.NewInt(expected))

	// Ensure the target does not exceed the maximum (easiest) difficulty.
	// The maximum target corresponds to bits = 0x1d00ffff.
	powLimit := chain.CompactToBig(0x1d00ffff)
	if newTarget.Cmp(powLimit) > 0 {
		newTarget = powLimit
	}

	// Ensure the target is positive.
	if newTarget.Sign() <= 0 {
		newTarget = big.NewInt(1)
	}

	return chain.BigToCompact(newTarget)
}

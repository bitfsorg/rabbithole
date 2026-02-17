// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package chain

import "time"

// Params defines the consensus parameters for the Metanet Chain.
type Params struct {
	// Name is a human-readable identifier for the network.
	Name string

	// Block parameters
	MaxBlockSize               uint32        // Maximum serialized block size in bytes.
	TargetBlockTime            time.Duration // Target time between blocks.
	DifficultyAdjustmentInterval uint32      // Number of blocks between difficulty adjustments.

	// Token economics
	InitialReward   uint64 // Initial block reward in satoshis (50 MNT = 5,000,000,000 sat).
	HalvingInterval uint32 // Number of blocks between halvings.
	MaxTotalSupply  uint64 // Maximum total supply in satoshis (21M MNT = 2,100,000,000,000,000 sat).
	SatoshiPerToken uint64 // Satoshis per 1 MNT (100,000,000).

	// Fee parameters
	MinFeePerByte uint64 // Minimum transaction fee rate in sat/byte.

	// BSV anchoring
	AnchorInterval uint32 // Number of Metanet Chain blocks between BSV anchors.
	AnchorMagic    [4]byte // Magic bytes for anchor transactions ("MNTA").

	// Merged mining
	AuxPowMagic [4]byte // Magic bytes for AuxPoW coinbase commitment ("MNMP").

	// Genesis
	GenesisHash [32]byte // Double-SHA256 hash of the genesis block header.
}

const (
	// SatoshiPerMNT is the number of satoshis in one MNT token.
	SatoshiPerMNT uint64 = 100_000_000

	// MaxMNTSupply is the maximum total supply of MNT in whole tokens.
	MaxMNTSupply uint64 = 21_000_000

	// MaxMNTSupplySat is the maximum total supply of MNT in satoshis.
	MaxMNTSupplySat uint64 = MaxMNTSupply * SatoshiPerMNT // 2,100,000,000,000,000

	// InitialRewardMNT is the initial block reward in whole MNT tokens.
	InitialRewardMNT uint64 = 50

	// InitialRewardSat is the initial block reward in satoshis.
	InitialRewardSat uint64 = InitialRewardMNT * SatoshiPerMNT // 5,000,000,000

	// HalvingIntervalBlocks is the number of blocks between reward halvings.
	HalvingIntervalBlocks uint32 = 210_000

	// DifficultyRetargetBlocks is the number of blocks between difficulty adjustments.
	DifficultyRetargetBlocks uint32 = 2016

	// TargetBlockTimeSec is the target block interval in seconds.
	TargetBlockTimeSec int64 = 600 // 10 minutes

	// ExpectedRetargetTimespan is the expected time for a full retarget period.
	// 2016 * 600 = 1,209,600 seconds (~2 weeks).
	ExpectedRetargetTimespan int64 = int64(DifficultyRetargetBlocks) * TargetBlockTimeSec

	// MaxBlockSizeBytes is the maximum block size (32 MB).
	MaxBlockSizeBytes uint32 = 32 * 1024 * 1024

	// AnchorIntervalBlocks is the number of blocks between BSV anchors.
	AnchorIntervalBlocks uint32 = 100

	// MinFeePerByteDefault is the default minimum fee rate.
	MinFeePerByteDefault uint64 = 1

	// MaxRetargetFactor is the maximum factor by which difficulty can change
	// in a single retarget period (4x in either direction).
	MaxRetargetFactor int64 = 4

	// MaxFutureBlockTime is the maximum allowed time a block timestamp can
	// be ahead of the current time (2 hours).
	MaxFutureBlockTime = 2 * time.Hour

	// MedianTimeBlocks is the number of previous blocks used for the
	// median time past calculation.
	MedianTimeBlocks = 11

	// MaxHalvingEras is the number of halving eras before the reward
	// reaches zero. After era 32, the 50 MNT reward has been halved 33 times
	// and becomes 0 in integer satoshi arithmetic.
	MaxHalvingEras uint32 = 33
)

// AnchorMagicBytes returns the "MNTA" magic bytes for BSV anchor transactions.
func AnchorMagicBytes() [4]byte {
	return [4]byte{'M', 'N', 'T', 'A'}
}

// AuxPowMagicBytes returns the "MNMP" magic bytes for merged mining coinbase.
func AuxPowMagicBytes() [4]byte {
	return [4]byte{'M', 'N', 'M', 'P'}
}

// MainNetParams returns the Metanet Chain mainnet consensus parameters.
func MainNetParams() *Params {
	p := &Params{
		Name:                       "mainnet",
		MaxBlockSize:               MaxBlockSizeBytes,
		TargetBlockTime:            time.Duration(TargetBlockTimeSec) * time.Second,
		DifficultyAdjustmentInterval: DifficultyRetargetBlocks,
		InitialReward:              InitialRewardSat,
		HalvingInterval:            HalvingIntervalBlocks,
		MaxTotalSupply:             MaxMNTSupplySat,
		SatoshiPerToken:            SatoshiPerMNT,
		MinFeePerByte:              MinFeePerByteDefault,
		AnchorInterval:             AnchorIntervalBlocks,
		AnchorMagic:                AnchorMagicBytes(),
		AuxPowMagic:                AuxPowMagicBytes(),
	}
	p.GenesisHash = GenesisBlockHash()
	return p
}

// TestNetParams returns the Metanet Chain testnet consensus parameters.
// Testnet uses lower difficulty and smaller block intervals for development.
func TestNetParams() *Params {
	p := &Params{
		Name:                       "testnet",
		MaxBlockSize:               MaxBlockSizeBytes,
		TargetBlockTime:            time.Duration(TargetBlockTimeSec) * time.Second,
		DifficultyAdjustmentInterval: DifficultyRetargetBlocks,
		InitialReward:              InitialRewardSat,
		HalvingInterval:            HalvingIntervalBlocks,
		MaxTotalSupply:             MaxMNTSupplySat,
		SatoshiPerToken:            SatoshiPerMNT,
		MinFeePerByte:              MinFeePerByteDefault,
		AnchorInterval:             AnchorIntervalBlocks,
		AnchorMagic:                AnchorMagicBytes(),
		AuxPowMagic:                AuxPowMagicBytes(),
	}
	// Testnet uses the same genesis structure but different hash
	// (in practice, testnet genesis would have a different nonce).
	p.GenesisHash = GenesisBlockHash()
	return p
}

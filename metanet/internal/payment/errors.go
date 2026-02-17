// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package payment

import "errors"

var (
	// ErrInsufficientCapacity indicates the payment amount exceeds the
	// remaining initiator balance.
	ErrInsufficientCapacity = errors.New("payment: insufficient channel capacity")

	// ErrChannelClosed indicates an attempt to update a closed channel.
	ErrChannelClosed = errors.New("payment: channel is closed")

	// ErrInvalidSequence indicates the commitment sequence number is not
	// greater than the current state.
	ErrInvalidSequence = errors.New("payment: invalid sequence number")

	// ErrInvalidSignature indicates signature verification failed.
	ErrInvalidSignature = errors.New("payment: invalid signature")

	// ErrInvalidVoucher indicates the voucher decoding or verification failed.
	ErrInvalidVoucher = errors.New("payment: invalid voucher")

	// ErrDisputeWindowActive indicates an action was attempted during an
	// active dispute period.
	ErrDisputeWindowActive = errors.New("payment: dispute window is active")

	// ErrInvalidRevocationKey indicates the revocation key does not match
	// the expected value.
	ErrInvalidRevocationKey = errors.New("payment: invalid revocation key")

	// ErrBelowMinDeposit indicates the funding amount is below MinDeposit.
	ErrBelowMinDeposit = errors.New("payment: funding amount below minimum deposit")

	// ErrChannelExpired indicates the channel has exceeded MaxDuration.
	ErrChannelExpired = errors.New("payment: channel has expired")

	// ErrInvalidChannelID indicates the channel ID format is invalid.
	ErrInvalidChannelID = errors.New("payment: invalid channel ID format")

	// ErrInvalidPubKey indicates a public key is not a valid 33-byte
	// compressed key.
	ErrInvalidPubKey = errors.New("payment: invalid compressed public key")

	// ErrInvalidPrivKey indicates a private key is not a valid 32-byte scalar.
	ErrInvalidPrivKey = errors.New("payment: invalid private key")

	// ErrZeroCapacity indicates channel capacity is zero.
	ErrZeroCapacity = errors.New("payment: channel capacity must be positive")

	// ErrZeroAmount indicates a zero payment amount.
	ErrZeroAmount = errors.New("payment: payment amount must be positive")
)

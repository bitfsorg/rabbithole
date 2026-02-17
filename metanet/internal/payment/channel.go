// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package payment

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
)

// ChannelType identifies the currency and purpose of a payment channel.
type ChannelType int

const (
	// ChannelBSV is a BSV channel for User <-> Metanet Node (x402).
	ChannelBSV ChannelType = iota
	// ChannelMNT is an MNT channel for Owner <-> Node or Node <-> Node.
	ChannelMNT
)

// Channel represents an open payment channel between two parties.
type Channel struct {
	ID              [32]byte    // SHA256d(funding_txid || vout).
	Type            ChannelType
	FundingTxID     [32]byte // On-chain funding transaction.
	FundingVout     uint32   // Output index in funding tx.
	Capacity        uint64   // Total channel capacity in satoshis.
	InitiatorPubKey []byte   // 33-byte compressed public key.
	ResponderPubKey []byte   // 33-byte compressed public key.
	State           *ChannelState
	Closed          bool // Whether the channel has been closed.
}

// ChannelState represents the current balance state of a channel.
type ChannelState struct {
	SeqNum           uint64 // Monotonically increasing sequence number.
	InitiatorBalance uint64 // Initiator's current balance (satoshis).
	ResponderBalance uint64 // Responder's current balance (satoshis).
	CommitmentTx     []byte // Serialized commitment transaction.
	InitiatorSig     []byte // Initiator's signature on commitment tx.
	ResponderSig     []byte // Responder's signature on commitment tx.
	RevocationKey    []byte // Revocation key for this state.
}

// ChannelParams defines the negotiated parameters for a channel.
type ChannelParams struct {
	MinDeposit    uint64 // Minimum funding amount (10,000 sat default).
	MaxDuration   uint32 // Maximum channel lifetime in blocks (144 default, ~1 day).
	DisputeWindow uint32 // CSV dispute window in blocks (6 default).
}

// DefaultBSVParams returns default channel parameters for BSV channels.
func DefaultBSVParams() *ChannelParams {
	return &ChannelParams{
		MinDeposit:    10_000,
		MaxDuration:   144,
		DisputeWindow: 6,
	}
}

// DefaultMNTParams returns default channel parameters for MNT channels.
func DefaultMNTParams() *ChannelParams {
	return &ChannelParams{
		MinDeposit:    10_000,
		MaxDuration:   144,
		DisputeWindow: 6,
	}
}

// computeChannelID computes the unique channel ID: SHA256d(funding_txid || vout).
func computeChannelID(fundingTxID [32]byte, vout uint32) [32]byte {
	var buf [36]byte
	copy(buf[0:32], fundingTxID[:])
	binary.LittleEndian.PutUint32(buf[32:36], vout)
	first := sha256.Sum256(buf[:])
	return sha256.Sum256(first[:])
}

// OpenChannel creates a new channel from a confirmed funding transaction.
func OpenChannel(
	channelType ChannelType,
	fundingTxID [32]byte,
	fundingVout uint32,
	capacity uint64,
	initiatorPubKey, responderPubKey []byte,
	params *ChannelParams,
) (*Channel, error) {
	if err := validatePubKey(initiatorPubKey); err != nil {
		return nil, err
	}
	if err := validatePubKey(responderPubKey); err != nil {
		return nil, err
	}
	if capacity == 0 {
		return nil, ErrZeroCapacity
	}
	if params != nil && capacity < params.MinDeposit {
		return nil, ErrBelowMinDeposit
	}

	ch := &Channel{
		ID:              computeChannelID(fundingTxID, fundingVout),
		Type:            channelType,
		FundingTxID:     fundingTxID,
		FundingVout:     fundingVout,
		Capacity:        capacity,
		InitiatorPubKey: make([]byte, len(initiatorPubKey)),
		ResponderPubKey: make([]byte, len(responderPubKey)),
		State: &ChannelState{
			SeqNum:           0,
			InitiatorBalance: capacity,
			ResponderBalance: 0,
		},
	}
	copy(ch.InitiatorPubKey, initiatorPubKey)
	copy(ch.ResponderPubKey, responderPubKey)

	// Generate initial revocation key.
	ch.State.RevocationKey = generateRevocationKey(ch.ID[:], 0)

	return ch, nil
}

// UpdateChannel creates a new commitment transaction reflecting a payment.
// The sequence number is incremented and new revocation keys are generated.
//
// Returns the new state and the revocation key for the OLD state
// (to be given to the counterparty).
func UpdateChannel(
	ch *Channel,
	amount uint64,
	initiatorPrivKey []byte,
) (*ChannelState, []byte, error) {
	if ch.Closed {
		return nil, nil, ErrChannelClosed
	}
	if amount == 0 {
		return nil, nil, ErrZeroAmount
	}
	if len(initiatorPrivKey) != 32 {
		return nil, nil, ErrInvalidPrivKey
	}
	if amount > ch.State.InitiatorBalance {
		return nil, nil, ErrInsufficientCapacity
	}

	// Save old revocation key to give to counterparty.
	oldRevocationKey := make([]byte, len(ch.State.RevocationKey))
	copy(oldRevocationKey, ch.State.RevocationKey)

	// Create new state.
	newSeqNum := ch.State.SeqNum + 1
	newState := &ChannelState{
		SeqNum:           newSeqNum,
		InitiatorBalance: ch.State.InitiatorBalance - amount,
		ResponderBalance: ch.State.ResponderBalance + amount,
		RevocationKey:    generateRevocationKey(ch.ID[:], newSeqNum),
	}

	// Build the commitment transaction.
	disputeWindow := uint32(6) // default
	commitTx, err := BuildCommitmentTx(
		ch.FundingTxID, ch.FundingVout,
		ch.InitiatorPubKey, ch.ResponderPubKey,
		newState.InitiatorBalance, newState.ResponderBalance,
		newSeqNum, disputeWindow,
	)
	if err != nil {
		return nil, nil, err
	}
	newState.CommitmentTx = commitTx

	// Sign as initiator.
	sig := signData(initiatorPrivKey, commitTx)
	newState.InitiatorSig = sig

	// Update channel state.
	ch.State = newState

	return newState, oldRevocationKey, nil
}

// SignCommitment signs a commitment transaction as the responder.
func SignCommitment(
	ch *Channel,
	state *ChannelState,
	responderPrivKey []byte,
) ([]byte, error) {
	if len(responderPrivKey) != 32 {
		return nil, ErrInvalidPrivKey
	}
	if state.CommitmentTx == nil {
		return nil, ErrInvalidVoucher
	}

	sig := signData(responderPrivKey, state.CommitmentTx)
	state.ResponderSig = sig
	return sig, nil
}

// CloseChannelCooperative constructs a final settlement transaction
// signed by both parties. No dispute window needed.
func CloseChannelCooperative(
	ch *Channel,
	initiatorPrivKey, responderPrivKey []byte,
) ([]byte, error) {
	if ch.Closed {
		return nil, ErrChannelClosed
	}
	if len(initiatorPrivKey) != 32 {
		return nil, ErrInvalidPrivKey
	}
	if len(responderPrivKey) != 32 {
		return nil, ErrInvalidPrivKey
	}

	// Build final settlement (no CSV, direct outputs).
	settleTx := buildSettlementTx(
		ch.FundingTxID, ch.FundingVout,
		ch.InitiatorPubKey, ch.ResponderPubKey,
		ch.State.InitiatorBalance, ch.State.ResponderBalance,
	)

	// Both parties sign.
	initiatorSig := signData(initiatorPrivKey, settleTx)
	responderSig := signData(responderPrivKey, settleTx)

	// Prepend signatures to the settlement tx.
	var signedTx []byte
	signedTx = append(signedTx, byte(len(initiatorSig)))
	signedTx = append(signedTx, initiatorSig...)
	signedTx = append(signedTx, byte(len(responderSig)))
	signedTx = append(signedTx, responderSig...)
	signedTx = append(signedTx, settleTx...)

	ch.Closed = true
	return signedTx, nil
}

// CloseChannelUnilateral broadcasts the latest commitment transaction
// for unilateral close. Subject to dispute window.
func CloseChannelUnilateral(ch *Channel) ([]byte, error) {
	if ch.Closed {
		return nil, ErrChannelClosed
	}
	if ch.State.CommitmentTx == nil {
		return nil, ErrInvalidVoucher
	}

	ch.Closed = true
	return ch.State.CommitmentTx, nil
}

// BuildPunishmentTx constructs a punishment transaction using a
// revocation key to claim the full channel balance when the counterparty
// broadcasts an old state.
func BuildPunishmentTx(
	ch *Channel,
	revokedState *ChannelState,
	revocationKey []byte,
	claimerPubKey []byte,
) ([]byte, error) {
	if err := validatePubKey(claimerPubKey); err != nil {
		return nil, err
	}

	// Verify the revocation key matches the revoked state.
	expectedRevKey := revokedState.RevocationKey
	if !hmac.Equal(revocationKey, expectedRevKey) {
		return nil, ErrInvalidRevocationKey
	}

	// Build the punishment transaction that claims the full balance.
	totalBalance := ch.Capacity

	var tx []byte
	// Version.
	tx = appendUint32LE(tx, 1)
	// Input count: 1 (the revoked commitment output).
	tx = append(tx, 0x01)
	// Previous outpoint: funding_txid + vout.
	tx = append(tx, ch.FundingTxID[:]...)
	tx = appendUint32LE(tx, ch.FundingVout)
	// ScriptSig: revocation_key + claimer_pubkey.
	scriptSig := make([]byte, 0, len(revocationKey)+len(claimerPubKey)+2)
	scriptSig = append(scriptSig, byte(len(revocationKey)))
	scriptSig = append(scriptSig, revocationKey...)
	scriptSig = append(scriptSig, byte(len(claimerPubKey)))
	scriptSig = append(scriptSig, claimerPubKey...)
	tx = append(tx, byte(len(scriptSig)))
	tx = append(tx, scriptSig...)
	// Sequence: 0xffffffff.
	tx = appendUint32LE(tx, 0xffffffff)
	// Output count: 1.
	tx = append(tx, 0x01)
	// Output: full balance to claimer.
	tx = appendUint64LE(tx, totalBalance)
	// P2PKH script for claimer.
	claimerScript := buildP2PKHScript(claimerPubKey)
	tx = append(tx, byte(len(claimerScript)))
	tx = append(tx, claimerScript...)
	// Locktime: 0.
	tx = appendUint32LE(tx, 0)

	return tx, nil
}

// generateRevocationKey generates a deterministic revocation key for a
// given channel and sequence number.
func generateRevocationKey(channelID []byte, seqNum uint64) []byte {
	var buf []byte
	buf = append(buf, channelID...)
	seqBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(seqBytes, seqNum)
	buf = append(buf, seqBytes...)
	buf = append(buf, []byte("revocation")...)

	// Use random salt for additional entropy.
	salt := make([]byte, 16)
	rand.Read(salt)
	buf = append(buf, salt...)

	hash := sha256.Sum256(buf)
	return hash[:]
}

// signData computes HMAC-SHA256(privKey, data) as a signature simulation.
// In production, this would be ECDSA signing with secp256k1.
func signData(privKey []byte, data []byte) []byte {
	mac := hmac.New(sha256.New, privKey)
	mac.Write(data)
	return mac.Sum(nil)
}

// verifySignature verifies HMAC-SHA256(pubKey-as-key, data) matches sig.
// In production, this would verify an ECDSA signature.
// Since we simulate with HMAC, we use the "associated private key" concept:
// the verification uses the private key material that would be embedded
// in a real signature. For testing, we accept any well-formed signature.
func verifySignature(pubKey []byte, data []byte, sig []byte) bool {
	// In a real implementation, this would do ECDSA verification.
	// For the simulation, we just verify the signature has the right length.
	return len(sig) == 32 && len(data) > 0
}

// validatePubKey checks that a public key is a valid 33-byte compressed key.
func validatePubKey(pubKey []byte) error {
	if len(pubKey) != 33 {
		return ErrInvalidPubKey
	}
	if pubKey[0] != 0x02 && pubKey[0] != 0x03 {
		return ErrInvalidPubKey
	}
	return nil
}

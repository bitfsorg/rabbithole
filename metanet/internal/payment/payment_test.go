// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package payment

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// testInitiatorPubKey is a valid-format 33-byte compressed public key.
var testInitiatorPubKey = func() []byte {
	key := make([]byte, 33)
	key[0] = 0x02
	h := sha256.Sum256([]byte("initiator-pubkey-seed"))
	copy(key[1:], h[:])
	return key
}()

// testResponderPubKey is a valid-format 33-byte compressed public key.
var testResponderPubKey = func() []byte {
	key := make([]byte, 33)
	key[0] = 0x03
	h := sha256.Sum256([]byte("responder-pubkey-seed"))
	copy(key[1:], h[:])
	return key
}()

// testInitiatorPrivKey is a 32-byte private key for testing.
var testInitiatorPrivKey = func() []byte {
	h := sha256.Sum256([]byte("initiator-privkey-seed"))
	return h[:]
}()

// testResponderPrivKey is a 32-byte private key for testing.
var testResponderPrivKey = func() []byte {
	h := sha256.Sum256([]byte("responder-privkey-seed"))
	return h[:]
}()

// testFundingTxID is a test funding transaction ID.
var testFundingTxID = func() [32]byte {
	return sha256.Sum256([]byte("test-funding-txid"))
}()

// ---------------------------------------------------------------------------
// DefaultParams tests
// ---------------------------------------------------------------------------

func TestDefaultBSVParams(t *testing.T) {
	p := DefaultBSVParams()
	if p.MinDeposit != 10_000 {
		t.Errorf("MinDeposit = %d, want 10000", p.MinDeposit)
	}
	if p.MaxDuration != 144 {
		t.Errorf("MaxDuration = %d, want 144", p.MaxDuration)
	}
	if p.DisputeWindow != 6 {
		t.Errorf("DisputeWindow = %d, want 6", p.DisputeWindow)
	}
}

func TestDefaultMNTParams(t *testing.T) {
	p := DefaultMNTParams()
	if p.MinDeposit != 10_000 {
		t.Errorf("MinDeposit = %d, want 10000", p.MinDeposit)
	}
}

// ---------------------------------------------------------------------------
// BuildFundingTx tests
// ---------------------------------------------------------------------------

func TestBuildFundingTx(t *testing.T) {
	script, err := BuildFundingTx(testInitiatorPubKey, testResponderPubKey, 100_000)
	if err != nil {
		t.Fatalf("BuildFundingTx: %v", err)
	}
	if len(script) == 0 {
		t.Fatal("script is empty")
	}

	// Should start with OP_2.
	if script[0] != op2 {
		t.Errorf("script[0] = 0x%02x, want 0x%02x (OP_2)", script[0], op2)
	}
	// Should end with OP_CHECKMULTISIG.
	if script[len(script)-1] != opCHECKMULTISIG {
		t.Errorf("last byte = 0x%02x, want 0x%02x", script[len(script)-1], opCHECKMULTISIG)
	}
	// Should contain both pubkeys.
	if !bytes.Contains(script, testInitiatorPubKey) {
		t.Error("script missing initiator pubkey")
	}
	if !bytes.Contains(script, testResponderPubKey) {
		t.Error("script missing responder pubkey")
	}
}

func TestBuildFundingTxInvalidKeys(t *testing.T) {
	_, err := BuildFundingTx([]byte{0x01}, testResponderPubKey, 100_000)
	if err != ErrInvalidPubKey {
		t.Errorf("bad initiator: got %v, want ErrInvalidPubKey", err)
	}
	_, err = BuildFundingTx(testInitiatorPubKey, []byte{0x04}, 100_000)
	if err != ErrInvalidPubKey {
		t.Errorf("bad responder: got %v, want ErrInvalidPubKey", err)
	}
}

func TestBuildFundingTxZeroCapacity(t *testing.T) {
	_, err := BuildFundingTx(testInitiatorPubKey, testResponderPubKey, 0)
	if err != ErrZeroCapacity {
		t.Errorf("got %v, want ErrZeroCapacity", err)
	}
}

// ---------------------------------------------------------------------------
// OpenChannel tests
// ---------------------------------------------------------------------------

func TestOpenChannel(t *testing.T) {
	ch, err := OpenChannel(
		ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey,
		DefaultBSVParams(),
	)
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}

	if ch.Type != ChannelBSV {
		t.Errorf("Type = %d, want ChannelBSV", ch.Type)
	}
	if ch.Capacity != 100_000 {
		t.Errorf("Capacity = %d, want 100000", ch.Capacity)
	}
	if ch.State.InitiatorBalance != 100_000 {
		t.Errorf("InitiatorBalance = %d, want 100000", ch.State.InitiatorBalance)
	}
	if ch.State.ResponderBalance != 0 {
		t.Errorf("ResponderBalance = %d, want 0", ch.State.ResponderBalance)
	}
	if ch.State.SeqNum != 0 {
		t.Errorf("SeqNum = %d, want 0", ch.State.SeqNum)
	}
	if ch.ID == ([32]byte{}) {
		t.Error("channel ID should not be zero")
	}
	if ch.Closed {
		t.Error("new channel should not be closed")
	}
}

func TestOpenChannelBelowMinDeposit(t *testing.T) {
	_, err := OpenChannel(
		ChannelBSV, testFundingTxID, 0, 1000, // below 10000 min
		testInitiatorPubKey, testResponderPubKey,
		DefaultBSVParams(),
	)
	if err != ErrBelowMinDeposit {
		t.Errorf("got %v, want ErrBelowMinDeposit", err)
	}
}

func TestOpenChannelInvalidKeys(t *testing.T) {
	_, err := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		[]byte{0x01}, testResponderPubKey, nil)
	if err != ErrInvalidPubKey {
		t.Errorf("bad initiator: got %v, want ErrInvalidPubKey", err)
	}
}

func TestOpenChannelZeroCapacity(t *testing.T) {
	_, err := OpenChannel(ChannelBSV, testFundingTxID, 0, 0,
		testInitiatorPubKey, testResponderPubKey, nil)
	if err != ErrZeroCapacity {
		t.Errorf("got %v, want ErrZeroCapacity", err)
	}
}

// ---------------------------------------------------------------------------
// UpdateChannel tests
// ---------------------------------------------------------------------------

func TestUpdateChannel(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)

	newState, oldRevKey, err := UpdateChannel(ch, 5000, testInitiatorPrivKey)
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	if newState.SeqNum != 1 {
		t.Errorf("SeqNum = %d, want 1", newState.SeqNum)
	}
	if newState.InitiatorBalance != 95_000 {
		t.Errorf("InitiatorBalance = %d, want 95000", newState.InitiatorBalance)
	}
	if newState.ResponderBalance != 5000 {
		t.Errorf("ResponderBalance = %d, want 5000", newState.ResponderBalance)
	}
	if len(newState.CommitmentTx) == 0 {
		t.Error("CommitmentTx should not be empty")
	}
	if len(newState.InitiatorSig) == 0 {
		t.Error("InitiatorSig should not be empty")
	}
	if len(oldRevKey) == 0 {
		t.Error("old revocation key should not be empty")
	}
}

func TestUpdateChannelMultiple(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)

	// Make 5 sequential updates.
	for i := uint64(1); i <= 5; i++ {
		state, _, err := UpdateChannel(ch, 1000, testInitiatorPrivKey)
		if err != nil {
			t.Fatalf("update %d: %v", i, err)
		}
		if state.SeqNum != i {
			t.Errorf("update %d: SeqNum = %d, want %d", i, state.SeqNum, i)
		}
	}

	if ch.State.InitiatorBalance != 95_000 {
		t.Errorf("after 5 updates: InitiatorBalance = %d, want 95000", ch.State.InitiatorBalance)
	}
	if ch.State.ResponderBalance != 5000 {
		t.Errorf("after 5 updates: ResponderBalance = %d, want 5000", ch.State.ResponderBalance)
	}
}

func TestUpdateChannelInsufficientCapacity(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)

	_, _, err := UpdateChannel(ch, 200_000, testInitiatorPrivKey)
	if err != ErrInsufficientCapacity {
		t.Errorf("got %v, want ErrInsufficientCapacity", err)
	}
}

func TestUpdateChannelClosed(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)
	ch.Closed = true

	_, _, err := UpdateChannel(ch, 1000, testInitiatorPrivKey)
	if err != ErrChannelClosed {
		t.Errorf("got %v, want ErrChannelClosed", err)
	}
}

func TestUpdateChannelZeroAmount(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)

	_, _, err := UpdateChannel(ch, 0, testInitiatorPrivKey)
	if err != ErrZeroAmount {
		t.Errorf("got %v, want ErrZeroAmount", err)
	}
}

// ---------------------------------------------------------------------------
// SignCommitment tests
// ---------------------------------------------------------------------------

func TestSignCommitment(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)

	newState, _, err := UpdateChannel(ch, 5000, testInitiatorPrivKey)
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	sig, err := SignCommitment(ch, newState, testResponderPrivKey)
	if err != nil {
		t.Fatalf("SignCommitment: %v", err)
	}
	if len(sig) == 0 {
		t.Error("signature should not be empty")
	}
	if len(newState.ResponderSig) == 0 {
		t.Error("state ResponderSig should be set")
	}
}

func TestSignCommitmentInvalidKey(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)
	state, _, _ := UpdateChannel(ch, 1000, testInitiatorPrivKey)

	_, err := SignCommitment(ch, state, []byte{0x01})
	if err != ErrInvalidPrivKey {
		t.Errorf("got %v, want ErrInvalidPrivKey", err)
	}
}

// ---------------------------------------------------------------------------
// CloseChannel tests
// ---------------------------------------------------------------------------

func TestCloseChannelCooperative(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)
	UpdateChannel(ch, 30_000, testInitiatorPrivKey)

	signedTx, err := CloseChannelCooperative(ch, testInitiatorPrivKey, testResponderPrivKey)
	if err != nil {
		t.Fatalf("CloseChannelCooperative: %v", err)
	}
	if len(signedTx) == 0 {
		t.Error("settlement tx should not be empty")
	}
	if !ch.Closed {
		t.Error("channel should be closed after cooperative close")
	}
}

func TestCloseChannelCooperativeAlreadyClosed(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)
	ch.Closed = true

	_, err := CloseChannelCooperative(ch, testInitiatorPrivKey, testResponderPrivKey)
	if err != ErrChannelClosed {
		t.Errorf("got %v, want ErrChannelClosed", err)
	}
}

func TestCloseChannelUnilateral(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)
	UpdateChannel(ch, 10_000, testInitiatorPrivKey)

	tx, err := CloseChannelUnilateral(ch)
	if err != nil {
		t.Fatalf("CloseChannelUnilateral: %v", err)
	}
	if len(tx) == 0 {
		t.Error("commitment tx should not be empty")
	}
	if !ch.Closed {
		t.Error("channel should be closed after unilateral close")
	}
}

// ---------------------------------------------------------------------------
// BuildPunishmentTx tests
// ---------------------------------------------------------------------------

func TestBuildPunishmentTx(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)

	// Update to state 1, get revocation key for state 0.
	_, oldRevKey, _ := UpdateChannel(ch, 5000, testInitiatorPrivKey)

	// Build punishment using the old state's revocation key.
	revokedState := &ChannelState{
		SeqNum:           0,
		InitiatorBalance: 100_000,
		ResponderBalance: 0,
		RevocationKey:    oldRevKey,
	}

	tx, err := BuildPunishmentTx(ch, revokedState, oldRevKey, testResponderPubKey)
	if err != nil {
		t.Fatalf("BuildPunishmentTx: %v", err)
	}
	if len(tx) == 0 {
		t.Error("punishment tx should not be empty")
	}
}

func TestBuildPunishmentTxInvalidRevocationKey(t *testing.T) {
	ch, _ := OpenChannel(ChannelBSV, testFundingTxID, 0, 100_000,
		testInitiatorPubKey, testResponderPubKey, nil)

	revokedState := &ChannelState{
		RevocationKey: []byte("correct-key"),
	}

	_, err := BuildPunishmentTx(ch, revokedState, []byte("wrong-key"), testResponderPubKey)
	if err != ErrInvalidRevocationKey {
		t.Errorf("got %v, want ErrInvalidRevocationKey", err)
	}
}

// ---------------------------------------------------------------------------
// Voucher tests
// ---------------------------------------------------------------------------

func TestEncodeDecodeVoucher(t *testing.T) {
	original := &ChannelState{
		SeqNum:           42,
		InitiatorBalance: 95_000,
		ResponderBalance: 5_000,
		InitiatorSig:     []byte("test-initiator-sig-32-bytes-pad!"),
		ResponderSig:     []byte("test-responder-sig-32-bytes-pad!"),
	}

	encoded, err := EncodeVoucher(original)
	if err != nil {
		t.Fatalf("EncodeVoucher: %v", err)
	}
	if encoded == "" {
		t.Fatal("encoded voucher is empty")
	}

	decoded, err := DecodeVoucher(encoded)
	if err != nil {
		t.Fatalf("DecodeVoucher: %v", err)
	}

	if decoded.SeqNum != original.SeqNum {
		t.Errorf("SeqNum: got %d, want %d", decoded.SeqNum, original.SeqNum)
	}
	if decoded.InitiatorBalance != original.InitiatorBalance {
		t.Errorf("InitiatorBalance: got %d, want %d", decoded.InitiatorBalance, original.InitiatorBalance)
	}
	if decoded.ResponderBalance != original.ResponderBalance {
		t.Errorf("ResponderBalance: got %d, want %d", decoded.ResponderBalance, original.ResponderBalance)
	}
	if !bytes.Equal(decoded.InitiatorSig, original.InitiatorSig) {
		t.Error("InitiatorSig mismatch")
	}
	if !bytes.Equal(decoded.ResponderSig, original.ResponderSig) {
		t.Error("ResponderSig mismatch")
	}
}

func TestDecodeVoucherInvalid(t *testing.T) {
	_, err := DecodeVoucher("")
	if err != ErrInvalidVoucher {
		t.Errorf("empty: got %v, want ErrInvalidVoucher", err)
	}
	_, err = DecodeVoucher("not-valid-base64!!!")
	if err != ErrInvalidVoucher {
		t.Errorf("bad base64: got %v, want ErrInvalidVoucher", err)
	}
}

func TestEncodeVoucherNil(t *testing.T) {
	_, err := EncodeVoucher(nil)
	if err != ErrInvalidVoucher {
		t.Errorf("got %v, want ErrInvalidVoucher", err)
	}
}

// ---------------------------------------------------------------------------
// FormatChannelID / ParseChannelID tests
// ---------------------------------------------------------------------------

func TestFormatParseChannelID(t *testing.T) {
	txid := testFundingTxID
	vout := uint32(42)

	formatted := FormatChannelID(txid, vout)
	if formatted == "" {
		t.Fatal("formatted ID is empty")
	}

	parsedTxID, parsedVout, err := ParseChannelID(formatted)
	if err != nil {
		t.Fatalf("ParseChannelID: %v", err)
	}
	if parsedTxID != txid {
		t.Error("parsed txid mismatch")
	}
	if parsedVout != vout {
		t.Errorf("parsed vout = %d, want %d", parsedVout, vout)
	}
}

func TestParseChannelIDInvalid(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"no colon", "abc123"},
		{"empty txid", ":0"},
		{"bad hex", "xyz:0"},
		{"short hex", "aabb:0"},
		{"bad vout", hex.EncodeToString(testFundingTxID[:]) + ":abc"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseChannelID(tc.id)
			if err != ErrInvalidChannelID {
				t.Errorf("got %v, want ErrInvalidChannelID", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BuildCommitmentTx tests
// ---------------------------------------------------------------------------

func TestBuildCommitmentTx(t *testing.T) {
	tx, err := BuildCommitmentTx(
		testFundingTxID, 0,
		testInitiatorPubKey, testResponderPubKey,
		70_000, 30_000,
		1, 6,
	)
	if err != nil {
		t.Fatalf("BuildCommitmentTx: %v", err)
	}
	if len(tx) == 0 {
		t.Fatal("commitment tx is empty")
	}
	// Verify tx starts with version 1.
	if tx[0] != 1 || tx[1] != 0 || tx[2] != 0 || tx[3] != 0 {
		t.Error("commitment tx should have version 1")
	}
}

func TestBuildCommitmentTxZeroResponder(t *testing.T) {
	tx, err := BuildCommitmentTx(
		testFundingTxID, 0,
		testInitiatorPubKey, testResponderPubKey,
		100_000, 0,
		0, 6,
	)
	if err != nil {
		t.Fatalf("BuildCommitmentTx: %v", err)
	}
	if len(tx) == 0 {
		t.Fatal("commitment tx is empty")
	}
}

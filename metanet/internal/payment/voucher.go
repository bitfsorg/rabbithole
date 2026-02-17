// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package payment

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// x402ChannelHeaders contains the HTTP header fields for x402 channel payments.
type x402ChannelHeaders struct {
	// Request headers.
	AcceptChannel  bool   // X-Accept-Channel.
	ChannelID      string // X-Channel-ID: <funding_txid>:<vout>.
	PaymentVoucher []byte // X-Payment-Voucher: base64(signed_commitment).

	// Response headers.
	ChannelPrice  uint64 // X-Channel-Price: sats_per_kb.
	MinDeposit    uint64 // X-Channel-Min-Deposit: sats.
	Balance       uint64 // X-Channel-Balance: remaining_sats.
	Expiry        uint32 // X-Channel-Expiry: block_height.
	TopUpRequired bool   // X-Channel-TopUp-Required.
	Expired       bool   // X-Channel-Expired.
}

// VerifyVoucher verifies an x402 payment voucher (signed commitment update).
func VerifyVoucher(
	ch *Channel,
	voucher []byte,
	expectedAmount uint64,
) error {
	if len(voucher) == 0 {
		return ErrInvalidVoucher
	}

	// Decode the voucher to get the state.
	state, err := decodeVoucherBytes(voucher)
	if err != nil {
		return ErrInvalidVoucher
	}

	// Verify sequence number is greater than current.
	if state.SeqNum <= ch.State.SeqNum {
		return ErrInvalidSequence
	}

	// Verify balances are consistent with the expected amount.
	expectedResponderBalance := ch.State.ResponderBalance + expectedAmount
	if state.ResponderBalance != expectedResponderBalance {
		return ErrInvalidVoucher
	}

	// Verify total balance is preserved.
	if state.InitiatorBalance+state.ResponderBalance != ch.Capacity {
		return ErrInvalidVoucher
	}

	return nil
}

// EncodeVoucher encodes a channel state update as a base64 payment voucher
// for use in the X-Payment-Voucher HTTP header.
func EncodeVoucher(state *ChannelState) (string, error) {
	if state == nil {
		return "", ErrInvalidVoucher
	}

	data := encodeVoucherBytes(state)
	return base64.StdEncoding.EncodeToString(data), nil
}

// DecodeVoucher decodes a base64 payment voucher from an HTTP header.
func DecodeVoucher(encoded string) (*ChannelState, error) {
	if encoded == "" {
		return nil, ErrInvalidVoucher
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrInvalidVoucher
	}

	return decodeVoucherBytes(data)
}

// FormatChannelID formats a channel ID for use in HTTP headers.
// Format: "<funding_txid_hex>:<vout>"
func FormatChannelID(fundingTxID [32]byte, vout uint32) string {
	return fmt.Sprintf("%s:%d", hex.EncodeToString(fundingTxID[:]), vout)
}

// ParseChannelID parses a channel ID from HTTP header format.
func ParseChannelID(id string) (fundingTxID [32]byte, vout uint32, err error) {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 {
		return fundingTxID, 0, ErrInvalidChannelID
	}

	txidBytes, err := hex.DecodeString(parts[0])
	if err != nil || len(txidBytes) != 32 {
		return fundingTxID, 0, ErrInvalidChannelID
	}
	copy(fundingTxID[:], txidBytes)

	voutVal, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return fundingTxID, 0, ErrInvalidChannelID
	}

	return fundingTxID, uint32(voutVal), nil
}

// encodeVoucherBytes serializes a ChannelState for voucher transmission.
//
// Format:
//
//	seq_num          (8 bytes LE)
//	initiator_bal    (8 bytes LE)
//	responder_bal    (8 bytes LE)
//	initiator_sig_len (2 bytes LE)
//	initiator_sig    (variable)
//	responder_sig_len (2 bytes LE)
//	responder_sig    (variable)
func encodeVoucherBytes(state *ChannelState) []byte {
	size := 8 + 8 + 8 + 2 + len(state.InitiatorSig) + 2 + len(state.ResponderSig)
	buf := make([]byte, size)
	offset := 0

	binary.LittleEndian.PutUint64(buf[offset:offset+8], state.SeqNum)
	offset += 8
	binary.LittleEndian.PutUint64(buf[offset:offset+8], state.InitiatorBalance)
	offset += 8
	binary.LittleEndian.PutUint64(buf[offset:offset+8], state.ResponderBalance)
	offset += 8

	binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(len(state.InitiatorSig)))
	offset += 2
	copy(buf[offset:], state.InitiatorSig)
	offset += len(state.InitiatorSig)

	binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(len(state.ResponderSig)))
	offset += 2
	copy(buf[offset:], state.ResponderSig)

	return buf
}

// decodeVoucherBytes deserializes a ChannelState from voucher bytes.
func decodeVoucherBytes(data []byte) (*ChannelState, error) {
	// Minimum: 8 + 8 + 8 + 2 + 2 = 28 bytes.
	if len(data) < 28 {
		return nil, ErrInvalidVoucher
	}

	offset := 0
	state := &ChannelState{}

	state.SeqNum = binary.LittleEndian.Uint64(data[offset : offset+8])
	offset += 8
	state.InitiatorBalance = binary.LittleEndian.Uint64(data[offset : offset+8])
	offset += 8
	state.ResponderBalance = binary.LittleEndian.Uint64(data[offset : offset+8])
	offset += 8

	if offset+2 > len(data) {
		return nil, ErrInvalidVoucher
	}
	initSigLen := binary.LittleEndian.Uint16(data[offset : offset+2])
	offset += 2
	if offset+int(initSigLen) > len(data) {
		return nil, ErrInvalidVoucher
	}
	state.InitiatorSig = make([]byte, initSigLen)
	copy(state.InitiatorSig, data[offset:offset+int(initSigLen)])
	offset += int(initSigLen)

	if offset+2 > len(data) {
		return nil, ErrInvalidVoucher
	}
	respSigLen := binary.LittleEndian.Uint16(data[offset : offset+2])
	offset += 2
	if offset+int(respSigLen) > len(data) {
		return nil, ErrInvalidVoucher
	}
	state.ResponderSig = make([]byte, respSigLen)
	copy(state.ResponderSig, data[offset:offset+int(respSigLen)])

	return state, nil
}

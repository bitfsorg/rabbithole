// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package contract

import "encoding/binary"

// Bitcoin Script opcodes used in storage contracts.
const (
	opFalse              = 0x00
	opTrue               = 0x51 // OP_1
	opIF                 = 0x63
	opELSE               = 0x67
	opENDIF              = 0x68
	opDROP               = 0x75
	opSHA256             = 0xa8
	opEQUALVERIFY        = 0x88
	opCHECKSIG           = 0xac
	opCHECKSIGVERIFY     = 0xad
	opCHECKLOCKTIMEVERIFY = 0xb1
)

// BuildDealScript constructs the Bitcoin Script locking script for a single
// period's StorageDeal UTXO.
//
// The script has two spending paths:
//
// IF path (Metanet Node claim): <node_sig> <proof_data> OP_TRUE
//
//	<node_pubkey> OP_CHECKSIGVERIFY
//	OP_SHA256 <expected_hash> OP_EQUALVERIFY
//	OP_TRUE
//
// ELSE path (Owner refund after expiry): <owner_sig> OP_FALSE
//
//	<expire_block> OP_CHECKLOCKTIMEVERIFY OP_DROP
//	<owner_pubkey> OP_CHECKSIG
//
// ENDIF
func BuildDealScript(nodePubKey []byte, ownerPubKey []byte, expectedHash [32]byte, expireBlock uint32) ([]byte, error) {
	if err := validatePubKey(nodePubKey); err != nil {
		return nil, err
	}
	if err := validatePubKey(ownerPubKey); err != nil {
		return nil, err
	}

	var script []byte

	// OP_IF
	script = append(script, opIF)

	// --- Node claim path ---
	// Push node_pubkey (33 bytes).
	script = append(script, pushData(nodePubKey)...)
	// OP_CHECKSIGVERIFY
	script = append(script, opCHECKSIGVERIFY)
	// OP_SHA256
	script = append(script, opSHA256)
	// Push expected_hash (32 bytes).
	script = append(script, pushData(expectedHash[:])...)
	// OP_EQUALVERIFY
	script = append(script, opEQUALVERIFY)
	// OP_TRUE (so the script succeeds after verification).
	script = append(script, opTrue)

	// OP_ELSE
	script = append(script, opELSE)

	// --- Owner refund path ---
	// Push expire_block as a minimal-encoded integer.
	expireBytes := encodeScriptNum(int64(expireBlock))
	script = append(script, pushData(expireBytes)...)
	// OP_CHECKLOCKTIMEVERIFY
	script = append(script, opCHECKLOCKTIMEVERIFY)
	// OP_DROP (remove the locktime value from the stack).
	script = append(script, opDROP)
	// Push owner_pubkey (33 bytes).
	script = append(script, pushData(ownerPubKey)...)
	// OP_CHECKSIG
	script = append(script, opCHECKSIG)

	// OP_ENDIF
	script = append(script, opENDIF)

	return script, nil
}

// BuildClaimInput constructs the unlocking script (scriptSig) for a Metanet
// Node to claim a period's payment.
//
// Format: <node_sig> <proof_data> OP_TRUE
func BuildClaimInput(nodeSig []byte, proofData []byte) []byte {
	var script []byte
	script = append(script, pushData(nodeSig)...)
	script = append(script, pushData(proofData)...)
	script = append(script, opTrue) // Selects the IF branch.
	return script
}

// BuildRefundInput constructs the unlocking script (scriptSig) for an Owner
// to reclaim tokens after expiry.
//
// Format: <owner_sig> OP_FALSE
func BuildRefundInput(ownerSig []byte) []byte {
	var script []byte
	script = append(script, pushData(ownerSig)...)
	script = append(script, opFalse) // Selects the ELSE branch.
	return script
}

// pushData creates the appropriate push opcode + data for Bitcoin Script.
func pushData(data []byte) []byte {
	n := len(data)
	switch {
	case n == 0:
		return []byte{opFalse}
	case n <= 75:
		// Direct push: OP_PUSH<n> <data>
		result := make([]byte, 1+n)
		result[0] = byte(n)
		copy(result[1:], data)
		return result
	case n <= 255:
		// OP_PUSHDATA1 <length_1byte> <data>
		result := make([]byte, 2+n)
		result[0] = 0x4c // OP_PUSHDATA1
		result[1] = byte(n)
		copy(result[2:], data)
		return result
	case n <= 65535:
		// OP_PUSHDATA2 <length_2bytes_LE> <data>
		result := make([]byte, 3+n)
		result[0] = 0x4d // OP_PUSHDATA2
		binary.LittleEndian.PutUint16(result[1:3], uint16(n))
		copy(result[3:], data)
		return result
	default:
		// OP_PUSHDATA4 <length_4bytes_LE> <data>
		result := make([]byte, 5+n)
		result[0] = 0x4e // OP_PUSHDATA4
		binary.LittleEndian.PutUint32(result[1:5], uint32(n))
		copy(result[5:], data)
		return result
	}
}

// encodeScriptNum encodes an integer as a Bitcoin Script number (minimal encoding).
func encodeScriptNum(n int64) []byte {
	if n == 0 {
		return nil
	}

	negative := n < 0
	if negative {
		n = -n
	}

	// Encode as little-endian.
	var result []byte
	for n > 0 {
		result = append(result, byte(n&0xff))
		n >>= 8
	}

	// If the high bit of the last byte is set, add an extra byte for the sign.
	if result[len(result)-1]&0x80 != 0 {
		if negative {
			result = append(result, 0x80)
		} else {
			result = append(result, 0x00)
		}
	} else if negative {
		result[len(result)-1] |= 0x80
	}

	return result
}

// validatePubKey checks that a public key is a valid 33-byte compressed key.
func validatePubKey(pubKey []byte) error {
	if len(pubKey) != 33 {
		return ErrInvalidPubKey
	}
	// Compressed public keys start with 0x02 or 0x03.
	if pubKey[0] != 0x02 && pubKey[0] != 0x03 {
		return ErrInvalidPubKey
	}
	return nil
}

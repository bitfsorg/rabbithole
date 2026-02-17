// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package payment

import (
	sha256pkg "crypto/sha256"
	"encoding/binary"
)

// Bitcoin Script opcodes used in payment channel scripts.
const (
	op0                   = 0x00 // OP_0
	op2                   = 0x52 // OP_2
	opCHECKMULTISIG      = 0xae
	opCHECKSEQUENCEVERIFY = 0xb2
	opDROP                = 0x75
	opCHECKSIG            = 0xac
	opDUP                 = 0x76
	opHASH160             = 0xa9
	opEQUALVERIFY         = 0x88
	opRETURN              = 0x6a
)

// BuildFundingTx constructs a 2-of-2 multisig funding script.
//
// Output script: OP_2 <pubkey_a> <pubkey_b> OP_2 OP_CHECKMULTISIG
func BuildFundingTx(
	initiatorPubKey, responderPubKey []byte,
	capacity uint64,
) ([]byte, error) {
	if err := validatePubKey(initiatorPubKey); err != nil {
		return nil, err
	}
	if err := validatePubKey(responderPubKey); err != nil {
		return nil, err
	}
	if capacity == 0 {
		return nil, ErrZeroCapacity
	}

	var script []byte
	// OP_2
	script = append(script, op2)
	// Push initiator_pubkey (33 bytes).
	script = append(script, pushData(initiatorPubKey)...)
	// Push responder_pubkey (33 bytes).
	script = append(script, pushData(responderPubKey)...)
	// OP_2
	script = append(script, op2)
	// OP_CHECKMULTISIG
	script = append(script, opCHECKMULTISIG)

	return script, nil
}

// BuildCommitmentTx constructs the commitment transaction for the current state.
//
// Output 0: Responder balance (with CSV time lock for dispute).
// Output 1: Initiator balance (P2PKH).
func BuildCommitmentTx(
	fundingTxID [32]byte,
	fundingVout uint32,
	initiatorPubKey, responderPubKey []byte,
	initiatorBalance, responderBalance uint64,
	seqNum uint64,
	disputeWindow uint32,
) ([]byte, error) {
	var tx []byte

	// Version.
	tx = appendUint32LE(tx, 1)

	// Input count: 1.
	tx = append(tx, 0x01)

	// Previous outpoint.
	tx = append(tx, fundingTxID[:]...)
	tx = appendUint32LE(tx, fundingVout)

	// ScriptSig placeholder (multisig will be filled by signatures).
	tx = append(tx, 0x00) // empty scriptSig
	// Sequence (encodes CSV).
	tx = appendUint32LE(tx, uint32(seqNum&0xffffffff))

	// Output count: count non-zero outputs.
	outputCount := byte(0)
	if responderBalance > 0 {
		outputCount++
	}
	if initiatorBalance > 0 {
		outputCount++
	}
	if outputCount == 0 {
		outputCount = 1 // At least one output.
	}
	tx = append(tx, outputCount)

	// Output 0: Responder with CSV.
	if responderBalance > 0 {
		tx = appendUint64LE(tx, responderBalance)
		respScript := buildCSVScript(responderPubKey, disputeWindow)
		tx = append(tx, byte(len(respScript)))
		tx = append(tx, respScript...)
	}

	// Output 1: Initiator (P2PKH).
	if initiatorBalance > 0 {
		tx = appendUint64LE(tx, initiatorBalance)
		initScript := buildP2PKHScript(initiatorPubKey)
		tx = append(tx, byte(len(initScript)))
		tx = append(tx, initScript...)
	}

	// If both are zero, create a minimal OP_RETURN output.
	if responderBalance == 0 && initiatorBalance == 0 {
		tx = appendUint64LE(tx, 0)
		tx = append(tx, 0x01, opRETURN)
	}

	// Locktime: 0.
	tx = appendUint32LE(tx, 0)

	return tx, nil
}

// buildCSVScript creates a script with CSV time lock:
//
//	<dispute_window> OP_CHECKSEQUENCEVERIFY OP_DROP <pubkey> OP_CHECKSIG
func buildCSVScript(pubKey []byte, disputeWindow uint32) []byte {
	var script []byte
	// Push dispute window as script number.
	csvBytes := encodeScriptNum(int64(disputeWindow))
	script = append(script, pushData(csvBytes)...)
	script = append(script, opCHECKSEQUENCEVERIFY)
	script = append(script, opDROP)
	script = append(script, pushData(pubKey)...)
	script = append(script, opCHECKSIG)
	return script
}

// buildP2PKHScript creates a standard P2PKH script:
//
//	OP_DUP OP_HASH160 <pubkey_hash> OP_EQUALVERIFY OP_CHECKSIG
func buildP2PKHScript(pubKey []byte) []byte {
	// Compute pubkey hash (simple SHA256 for simulation; real Bitcoin uses RIPEMD160(SHA256)).
	pubKeyHash := sha256Hash160(pubKey)

	var script []byte
	script = append(script, opDUP)
	script = append(script, opHASH160)
	script = append(script, pushData(pubKeyHash)...)
	script = append(script, opEQUALVERIFY)
	script = append(script, opCHECKSIG)
	return script
}

// buildSettlementTx builds a cooperative settlement transaction.
// No CSV lock -- outputs go directly to both parties.
func buildSettlementTx(
	fundingTxID [32]byte,
	fundingVout uint32,
	initiatorPubKey, responderPubKey []byte,
	initiatorBalance, responderBalance uint64,
) []byte {
	var tx []byte

	// Version.
	tx = appendUint32LE(tx, 2)

	// Input count: 1.
	tx = append(tx, 0x01)
	tx = append(tx, fundingTxID[:]...)
	tx = appendUint32LE(tx, fundingVout)
	tx = append(tx, 0x00)       // empty scriptSig
	tx = appendUint32LE(tx, 0xffffffff) // sequence

	// Output count.
	outputCount := byte(0)
	if initiatorBalance > 0 {
		outputCount++
	}
	if responderBalance > 0 {
		outputCount++
	}
	if outputCount == 0 {
		outputCount = 1
	}
	tx = append(tx, outputCount)

	// Initiator output (P2PKH, no CSV).
	if initiatorBalance > 0 {
		tx = appendUint64LE(tx, initiatorBalance)
		script := buildP2PKHScript(initiatorPubKey)
		tx = append(tx, byte(len(script)))
		tx = append(tx, script...)
	}

	// Responder output (P2PKH, no CSV).
	if responderBalance > 0 {
		tx = appendUint64LE(tx, responderBalance)
		script := buildP2PKHScript(responderPubKey)
		tx = append(tx, byte(len(script)))
		tx = append(tx, script...)
	}

	if initiatorBalance == 0 && responderBalance == 0 {
		tx = appendUint64LE(tx, 0)
		tx = append(tx, 0x01, opRETURN)
	}

	// Locktime: 0.
	tx = appendUint32LE(tx, 0)

	return tx
}

// sha256Hash160 computes a simple hash for pubkey-to-address.
// Real Bitcoin uses RIPEMD160(SHA256(pubkey)); we use SHA256 and take 20 bytes.
func sha256Hash160(data []byte) []byte {
	h := sha256pkg.Sum256(data)
	return h[:20]
}

// pushData creates the appropriate push opcode + data for Bitcoin Script.
func pushData(data []byte) []byte {
	n := len(data)
	switch {
	case n == 0:
		return []byte{op0}
	case n <= 75:
		result := make([]byte, 1+n)
		result[0] = byte(n)
		copy(result[1:], data)
		return result
	case n <= 255:
		result := make([]byte, 2+n)
		result[0] = 0x4c // OP_PUSHDATA1
		result[1] = byte(n)
		copy(result[2:], data)
		return result
	default:
		result := make([]byte, 3+n)
		result[0] = 0x4d // OP_PUSHDATA2
		binary.LittleEndian.PutUint16(result[1:3], uint16(n))
		copy(result[3:], data)
		return result
	}
}

// encodeScriptNum encodes an integer as a Bitcoin Script number.
func encodeScriptNum(n int64) []byte {
	if n == 0 {
		return nil
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var result []byte
	for n > 0 {
		result = append(result, byte(n&0xff))
		n >>= 8
	}
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

// appendUint32LE appends a uint32 in little-endian to a byte slice.
func appendUint32LE(buf []byte, v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return append(buf, b...)
}

// appendUint64LE appends a uint64 in little-endian to a byte slice.
func appendUint64LE(buf []byte, v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return append(buf, b...)
}

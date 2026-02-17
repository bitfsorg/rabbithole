// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package chain

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"
)

// BlockHeaderSize is the size of a serialized block header in bytes.
const BlockHeaderSize = 80

// BlockHeader is the standard 80-byte block header, identical to BSV.
type BlockHeader struct {
	Version    uint32
	PrevHash   [32]byte
	MerkleRoot [32]byte
	Timestamp  uint32
	Bits       uint32
	Nonce      uint32
}

// Block represents a Metanet Chain block.
type Block struct {
	Header BlockHeader
	Txs    [][]byte // Raw serialized transactions (BSV-format).
}

// SerializeBlockHeader serializes a block header to its canonical 80-byte form.
// Field order: Version(4) | PrevHash(32) | MerkleRoot(32) | Timestamp(4) | Bits(4) | Nonce(4)
func SerializeBlockHeader(h *BlockHeader) [BlockHeaderSize]byte {
	var buf [BlockHeaderSize]byte
	binary.LittleEndian.PutUint32(buf[0:4], h.Version)
	copy(buf[4:36], h.PrevHash[:])
	copy(buf[36:68], h.MerkleRoot[:])
	binary.LittleEndian.PutUint32(buf[68:72], h.Timestamp)
	binary.LittleEndian.PutUint32(buf[72:76], h.Bits)
	binary.LittleEndian.PutUint32(buf[76:80], h.Nonce)
	return buf
}

// DeserializeBlockHeader deserializes an 80-byte block header.
func DeserializeBlockHeader(data [BlockHeaderSize]byte) *BlockHeader {
	h := &BlockHeader{}
	h.Version = binary.LittleEndian.Uint32(data[0:4])
	copy(h.PrevHash[:], data[4:36])
	copy(h.MerkleRoot[:], data[36:68])
	h.Timestamp = binary.LittleEndian.Uint32(data[68:72])
	h.Bits = binary.LittleEndian.Uint32(data[72:76])
	h.Nonce = binary.LittleEndian.Uint32(data[76:80])
	return h
}

// HashBlockHeader computes the double-SHA256 hash of a block header.
// This is the standard Bitcoin block hash: SHA256(SHA256(header_bytes)).
func HashBlockHeader(h *BlockHeader) [32]byte {
	serialized := SerializeBlockHeader(h)
	first := sha256.Sum256(serialized[:])
	return sha256.Sum256(first[:])
}

// CompactToBig converts a Bitcoin compact target representation (Bits field)
// to a big.Int target value.
//
// The compact format encodes as: mantissa * 256^(exponent-3)
// where the first byte is the exponent and the remaining 3 bytes are the mantissa.
func CompactToBig(compact uint32) *big.Int {
	// Extract the mantissa and exponent.
	exponent := uint(compact >> 24)
	mantissa := new(big.Int).SetInt64(int64(compact & 0x007fffff))

	// Apply sign bit.
	if compact&0x00800000 != 0 {
		mantissa.Neg(mantissa)
	}

	// Shift mantissa by exponent.
	if exponent <= 3 {
		mantissa.Rsh(mantissa, 8*(3-exponent))
	} else {
		mantissa.Lsh(mantissa, 8*(exponent-3))
	}

	return mantissa
}

// BigToCompact converts a big.Int target to the Bitcoin compact target
// representation (Bits field).
func BigToCompact(target *big.Int) uint32 {
	if target.Sign() == 0 {
		return 0
	}

	// Determine the byte length.
	targetBytes := target.Bytes()
	exponent := uint32(len(targetBytes))

	// Ensure we have at least 3 bytes of mantissa.
	var mantissa uint32
	if exponent <= 3 {
		// Shift left to fill 3 bytes.
		shifted := new(big.Int).Lsh(target, 8*(3-uint(exponent)))
		mantissa = uint32(shifted.Int64()) & 0x00ffffff
	} else {
		// Take the top 3 bytes.
		shifted := new(big.Int).Rsh(target, 8*(uint(exponent)-3))
		mantissa = uint32(shifted.Int64()) & 0x00ffffff
	}

	// If the sign bit of the mantissa is set, increase exponent.
	if mantissa&0x00800000 != 0 {
		mantissa >>= 8
		exponent++
	}

	return (exponent << 24) | mantissa
}

// HashMeetsTarget checks whether a block hash (as double-SHA256 in
// little-endian, standard Bitcoin order) meets the given difficulty target.
//
// The hash is interpreted as a 256-bit little-endian integer and must be
// less than or equal to the target.
func HashMeetsTarget(hash [32]byte, bits uint32) bool {
	target := CompactToBig(bits)
	if target.Sign() <= 0 {
		return false
	}

	// Convert hash to big.Int. Block hashes are stored in internal byte
	// order (result of double-SHA256), which is big-endian for big.Int.
	// We need to reverse the bytes for big.Int interpretation because
	// Bitcoin treats the hash as a little-endian number.
	var reversed [32]byte
	for i := 0; i < 32; i++ {
		reversed[i] = hash[31-i]
	}
	hashInt := new(big.Int).SetBytes(reversed[:])

	return hashInt.Cmp(target) <= 0
}

// ComputeMerkleRoot computes the Merkle root from a list of transaction
// hashes (double-SHA256). This uses the standard Bitcoin Merkle tree
// algorithm: if the list has an odd number of elements, the last element
// is duplicated.
func ComputeMerkleRoot(txHashes [][32]byte) [32]byte {
	if len(txHashes) == 0 {
		return [32]byte{}
	}
	if len(txHashes) == 1 {
		return txHashes[0]
	}

	// Work with a copy to avoid modifying the input.
	level := make([][32]byte, len(txHashes))
	copy(level, txHashes)

	for len(level) > 1 {
		// If odd number, duplicate the last element.
		if len(level)%2 != 0 {
			level = append(level, level[len(level)-1])
		}

		nextLevel := make([][32]byte, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			// Concatenate pair and double-SHA256.
			var combined [64]byte
			copy(combined[0:32], level[i][:])
			copy(combined[32:64], level[i+1][:])
			first := sha256.Sum256(combined[:])
			nextLevel[i/2] = sha256.Sum256(first[:])
		}
		level = nextLevel
	}

	return level[0]
}

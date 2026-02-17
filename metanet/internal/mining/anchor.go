// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package mining

import (
	"bytes"
	"encoding/binary"

	"github.com/tongxiaofeng/metanet/internal/chain"
)

// AnchorMagic is the 4-byte marker for BSV anchor transactions.
// "MNTA" = Metanet Anchor
var AnchorMagic = [4]byte{'M', 'N', 'T', 'A'}

// AnchorVersion is the current anchor protocol version.
const AnchorVersion = 0x01

// AnchorDataMinSize is the minimum serialized size of anchor data:
// version(1) + magic(4) + start_height(4) + end_height(4) + merkle_root(32) + block_count(2) + prev_anchor_txid(32) = 79
const AnchorDataMinSize = 1 + 4 + 4 + 4 + 32 + 2 + 32

// AnchorTx represents a BSV anchor transaction for a range of Metanet Chain blocks.
type AnchorTx struct {
	Version      uint8    // Protocol version (0x01).
	Flag         [4]byte  // "MNTA".
	StartHeight  uint32   // First Metanet Chain block in range.
	EndHeight    uint32   // Last Metanet Chain block in range.
	MerkleRoot   [32]byte // Merkle root of the block hashes in the range.
	BlockCount   uint16   // Number of blocks (typically 100).
	PrevAnchorTx [32]byte // Previous BSV anchor TxID (chain linkage).
}

// SerializeAnchorData serializes an AnchorTx into the OP_RETURN payload bytes.
//
// Format:
//   version(1) | flag(4) | start_height(4 LE) | end_height(4 LE) |
//   merkle_root(32) | block_count(2 LE) | prev_anchor_txid(32)
func SerializeAnchorData(anchor *AnchorTx) []byte {
	buf := make([]byte, 0, AnchorDataMinSize)

	buf = append(buf, anchor.Version)
	buf = append(buf, anchor.Flag[:]...)

	var heightBuf [4]byte
	binary.LittleEndian.PutUint32(heightBuf[:], anchor.StartHeight)
	buf = append(buf, heightBuf[:]...)

	binary.LittleEndian.PutUint32(heightBuf[:], anchor.EndHeight)
	buf = append(buf, heightBuf[:]...)

	buf = append(buf, anchor.MerkleRoot[:]...)

	var countBuf [2]byte
	binary.LittleEndian.PutUint16(countBuf[:], anchor.BlockCount)
	buf = append(buf, countBuf[:]...)

	buf = append(buf, anchor.PrevAnchorTx[:]...)

	return buf
}

// DeserializeAnchorData deserializes an OP_RETURN payload into an AnchorTx.
func DeserializeAnchorData(data []byte) (*AnchorTx, error) {
	if len(data) < AnchorDataMinSize {
		return nil, ErrAnchorDataTooShort
	}

	anchor := &AnchorTx{}
	offset := 0

	anchor.Version = data[offset]
	offset++

	copy(anchor.Flag[:], data[offset:offset+4])
	offset += 4

	if anchor.Flag != AnchorMagic {
		return nil, ErrInvalidAnchorMagic
	}

	anchor.StartHeight = binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	anchor.EndHeight = binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	copy(anchor.MerkleRoot[:], data[offset:offset+32])
	offset += 32

	anchor.BlockCount = binary.LittleEndian.Uint16(data[offset : offset+2])
	offset += 2

	copy(anchor.PrevAnchorTx[:], data[offset:offset+32])

	return anchor, nil
}

// BuildBlockRangeMerkleRoot computes the Merkle root for a range of
// Metanet Chain block headers by hashing each header (double-SHA256)
// and building a Merkle tree.
func BuildBlockRangeMerkleRoot(headers []chain.BlockHeader) [32]byte {
	if len(headers) == 0 {
		return [32]byte{}
	}

	// Compute the double-SHA256 hash of each block header.
	hashes := make([][32]byte, len(headers))
	for i := range headers {
		hashes[i] = chain.HashBlockHeader(&headers[i])
	}

	return chain.ComputeMerkleRoot(hashes)
}

// BuildAnchorTx constructs a BSV anchor transaction for a range of
// Metanet Chain blocks.
func BuildAnchorTx(startHeight, endHeight uint32, blockHeaders []chain.BlockHeader, prevAnchorTxID [32]byte) (*AnchorTx, error) {
	if len(blockHeaders) == 0 {
		return nil, ErrNoBlockHeaders
	}

	expectedCount := endHeight - startHeight + 1
	if uint32(len(blockHeaders)) != expectedCount {
		return nil, ErrAnchorHeightMismatch
	}

	merkleRoot := BuildBlockRangeMerkleRoot(blockHeaders)

	return &AnchorTx{
		Version:      AnchorVersion,
		Flag:         AnchorMagic,
		StartHeight:  startHeight,
		EndHeight:    endHeight,
		MerkleRoot:   merkleRoot,
		BlockCount:   uint16(len(blockHeaders)),
		PrevAnchorTx: prevAnchorTxID,
	}, nil
}

// ValidateAnchorChain validates that a sequence of anchor transactions
// forms a valid chain where each anchor references the previous one.
//
// The "TxID" of an anchor is computed as SHA256d(SerializeAnchorData(anchor)).
// The first anchor's PrevAnchorTx is not checked (it bootstraps the chain).
func ValidateAnchorChain(anchors []*AnchorTx) error {
	if len(anchors) < 2 {
		return nil // Nothing to validate with 0 or 1 anchor.
	}

	for i := 1; i < len(anchors); i++ {
		// Compute the "TxID" of the previous anchor.
		prevData := SerializeAnchorData(anchors[i-1])
		prevID := doubleSHA256(prevData)

		if !bytes.Equal(anchors[i].PrevAnchorTx[:], prevID[:]) {
			return ErrAnchorChainBroken
		}

		// Verify height continuity.
		if anchors[i].StartHeight != anchors[i-1].EndHeight+1 {
			return ErrAnchorHeightMismatch
		}
	}

	return nil
}

// ComputeAnchorTxID computes the SHA256d "TxID" of an anchor for chain linkage.
func ComputeAnchorTxID(anchor *AnchorTx) [32]byte {
	data := SerializeAnchorData(anchor)
	return doubleSHA256(data)
}

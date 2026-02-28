// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package mining

import (
	"bytes"
	"crypto/sha256"

	"github.com/bitfsorg/metanet/internal/chain"
)

// AuxPowMagic is the 4-byte marker embedded in parent chain coinbase
// transactions to commit to a Metanet Chain block hash.
// "MNMP" = Metanet Merged PoW
var AuxPowMagic = [4]byte{'M', 'N', 'M', 'P'}

// AuxPoWHeader extends a standard block header with merged mining proof data.
type AuxPoWHeader struct {
	// Standard Metanet Chain block header.
	Header chain.BlockHeader

	// Parent chain (BTC/BSV) block header (80 bytes).
	ParentHeader [80]byte

	// Coinbase transaction from the parent chain containing the MNMP marker.
	CoinbaseTx []byte

	// Merkle branch proving the coinbase is in the parent block.
	CoinbaseBranch [][32]byte

	// Index of coinbase in the parent Merkle tree.
	CoinbaseIndex uint32
}

// BuildCoinbaseCommitment constructs the OP_RETURN output data for
// embedding in a parent chain coinbase transaction.
//
// Format: OP_RETURN OP_PUSH(36) "MNMP" <32-byte block_hash>
func BuildCoinbaseCommitment(blockHash [32]byte) []byte {
	// OP_RETURN (0x6a) + push 36 bytes (0x24) + "MNMP" + block_hash
	data := make([]byte, 0, 2+4+32) // 38 bytes total after OP_RETURN + push
	data = append(data, 0x6a)        // OP_RETURN
	data = append(data, 0x24)        // Push 36 bytes
	data = append(data, AuxPowMagic[:]...)
	data = append(data, blockHash[:]...)
	return data
}

// FindAuxPoWCommitment scans a coinbase transaction's raw bytes for the MNMP
// marker and returns the committed Metanet Chain block hash.
//
// Returns the 32-byte block hash if found, or nil if not found.
func FindAuxPoWCommitment(coinbaseTx []byte) []byte {
	magic := AuxPowMagic[:]
	// Search for the MNMP magic bytes followed by 32 bytes of block hash.
	for i := 0; i <= len(coinbaseTx)-4-32; i++ {
		if bytes.Equal(coinbaseTx[i:i+4], magic) {
			hash := make([]byte, 32)
			copy(hash, coinbaseTx[i+4:i+4+32])
			return hash
		}
	}
	return nil
}

// VerifyCoinbaseBranch verifies a Merkle branch proving a coinbase
// transaction is included in a block with the given Merkle root.
//
// The coinbase TxID is typically at index 0 in the Merkle tree, but
// this function supports arbitrary indices.
func VerifyCoinbaseBranch(coinbaseTxHash [32]byte, branch [][32]byte, index uint32, merkleRoot [32]byte) bool {
	current := coinbaseTxHash
	idx := index

	for _, sibling := range branch {
		var combined [64]byte
		if idx&1 == 0 {
			// Current is on the left.
			copy(combined[0:32], current[:])
			copy(combined[32:64], sibling[:])
		} else {
			// Current is on the right.
			copy(combined[0:32], sibling[:])
			copy(combined[32:64], current[:])
		}
		// Double-SHA256.
		first := sha256.Sum256(combined[:])
		current = sha256.Sum256(first[:])
		idx >>= 1
	}

	return current == merkleRoot
}

// ValidateAuxPoW validates the auxiliary proof-of-work for a Metanet Chain block.
//
// Verification steps:
//  1. Parse the parent header and verify its hash meets the Metanet Chain difficulty.
//  2. Compute the coinbase TxID and verify it is in the parent Merkle tree.
//  3. Find the MNMP marker in the coinbase and extract the committed block hash.
//  4. Verify the committed hash matches SHA256d(Metanet Chain block header).
func ValidateAuxPoW(auxpow *AuxPoWHeader, params *chain.Params) error {
	// Step 1: Verify the parent header hash meets Metanet Chain difficulty.
	parentHeaderObj := chain.DeserializeBlockHeader(auxpow.ParentHeader)
	parentHash := chain.HashBlockHeader(parentHeaderObj)
	if !chain.HashMeetsTarget(parentHash, auxpow.Header.Bits) {
		return ErrInsufficientAuxPoW
	}

	// Step 2: Compute coinbase TxID and verify Merkle inclusion.
	coinbaseTxID := doubleSHA256(auxpow.CoinbaseTx)
	parentMerkleRoot := parentHeaderObj.MerkleRoot
	if !VerifyCoinbaseBranch(coinbaseTxID, auxpow.CoinbaseBranch, auxpow.CoinbaseIndex, parentMerkleRoot) {
		return ErrInvalidCoinbaseBranch
	}

	// Step 3: Find MNMP commitment in coinbase.
	committedHash := FindAuxPoWCommitment(auxpow.CoinbaseTx)
	if committedHash == nil {
		return ErrNoAuxPoWCommitment
	}

	// Step 4: Verify committed hash matches the Metanet Chain block header hash.
	expectedHash := chain.HashBlockHeader(&auxpow.Header)
	if !bytes.Equal(committedHash, expectedHash[:]) {
		return ErrAuxPoWHashMismatch
	}

	return nil
}

// doubleSHA256 computes SHA256(SHA256(data)).
func doubleSHA256(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

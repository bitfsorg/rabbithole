// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package chain

import (
	"crypto/sha256"
	"encoding/hex"
)

// GenesisMessage is the coinbase message embedded in the genesis block.
const GenesisMessage = "Metanet: Decentralized CDN on Bitcoin"

// genesisHeader is the Metanet Chain genesis block header.
// The timestamp and nonce are experimental values for development;
// they will be replaced at mainnet launch.
var genesisHeader = BlockHeader{
	Version:    1,
	PrevHash:   [32]byte{}, // All zeros — no previous block.
	MerkleRoot: genesisComputedMerkleRoot(),
	Timestamp:  1700000000, // Experimental: 2023-11-14T22:13:20Z
	Bits:       0x1d00ffff, // Initial difficulty (same as Bitcoin genesis).
	Nonce:      0,          // Experimental: will be mined at launch.
}

// genesisComputedMerkleRoot computes the Merkle root for the genesis block,
// which contains exactly one transaction: the coinbase.
func genesisComputedMerkleRoot() [32]byte {
	// The genesis coinbase transaction hash is the double-SHA256 of the
	// serialized coinbase. For the genesis block, the Merkle root IS the
	// coinbase TxID since there is only one transaction.
	coinbaseTxBytes := buildGenesisCoinbase()
	first := sha256.Sum256(coinbaseTxBytes)
	return sha256.Sum256(first[:])
}

// buildGenesisCoinbase constructs a minimal coinbase transaction for the
// genesis block containing the genesis message.
//
// This is a simplified coinbase with:
//   - 1 input: coinbase (prev_txid=0, prev_vout=0xffffffff)
//   - 1 output: InitialRewardSat to an unspendable OP_RETURN + genesis message
//
// The output is intentionally unspendable (OP_RETURN) because the genesis
// block's coinbase is traditionally non-spendable in Bitcoin-derived chains.
func buildGenesisCoinbase() []byte {
	msg := []byte(GenesisMessage)

	// Minimal Bitcoin transaction format:
	//   version(4) + vin_count(1) + vin + vout_count(1) + vout + locktime(4)

	// Transaction version.
	tx := []byte{0x01, 0x00, 0x00, 0x00}

	// Input count: 1
	tx = append(tx, 0x01)

	// Coinbase input:
	//   prev_txid: 32 zero bytes
	//   prev_vout: 0xffffffff
	//   script_len: varint(len(msg) + 1)
	//   script: OP_PUSHDATA(len) + msg
	//   sequence: 0xffffffff
	tx = append(tx, make([]byte, 32)...) // prev_txid = 0x00...00
	tx = append(tx, 0xff, 0xff, 0xff, 0xff) // prev_vout = -1

	// Coinbase script: push the genesis message.
	scriptLen := len(msg) + 1 // 1 byte for push opcode
	tx = append(tx, byte(scriptLen))
	tx = append(tx, byte(len(msg))) // OP_PUSH<n>
	tx = append(tx, msg...)

	// Sequence.
	tx = append(tx, 0xff, 0xff, 0xff, 0xff)

	// Output count: 1
	tx = append(tx, 0x01)

	// Output: OP_RETURN <message> with value = InitialRewardSat
	// Value (8 bytes LE).
	value := InitialRewardSat
	tx = append(tx,
		byte(value), byte(value>>8), byte(value>>16), byte(value>>24),
		byte(value>>32), byte(value>>40), byte(value>>48), byte(value>>56),
	)

	// Output script: OP_RETURN <message>
	outScript := []byte{0x6a} // OP_RETURN
	outScript = append(outScript, byte(len(msg)))
	outScript = append(outScript, msg...)
	tx = append(tx, byte(len(outScript)))
	tx = append(tx, outScript...)

	// Locktime: 0
	tx = append(tx, 0x00, 0x00, 0x00, 0x00)

	return tx
}

// GenesisBlock returns the Metanet Chain genesis block.
func GenesisBlock() *Block {
	return &Block{
		Header: genesisHeader,
		Txs:    [][]byte{buildGenesisCoinbase()},
	}
}

// GenesisBlockHeader returns the genesis block header.
func GenesisBlockHeader() *BlockHeader {
	h := genesisHeader // Return a copy.
	return &h
}

// GenesisBlockHash returns the double-SHA256 hash of the genesis block header.
func GenesisBlockHash() [32]byte {
	return HashBlockHeader(&genesisHeader)
}

// GenesisBlockHashHex returns the genesis block hash as a hex string
// in standard (reversed) display order.
func GenesisBlockHashHex() string {
	hash := GenesisBlockHash()
	// Bitcoin convention: display hash in reversed byte order.
	var reversed [32]byte
	for i := 0; i < 32; i++ {
		reversed[i] = hash[31-i]
	}
	return hex.EncodeToString(reversed[:])
}

// GenesisCoinbaseMessage returns the genesis message bytes for verification.
func GenesisCoinbaseMessage() []byte {
	return []byte(GenesisMessage)
}

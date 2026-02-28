package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/bitfsorg/libbitfs-go/network"
)

// ChainInfo holds blockchain summary from getblockchaininfo.
type ChainInfo struct {
	Chain         string  `json:"chain"`
	Blocks        int64   `json:"blocks"`
	BestBlockHash string  `json:"bestblockhash"`
	Difficulty    float64 `json:"difficulty"`
	MedianTime    int64   `json:"mediantime"`
}

// BlockInfo holds block details from getblock (verbosity=1).
type BlockInfo struct {
	Hash          string   `json:"hash"`
	Height        int64    `json:"height"`
	Version       int32    `json:"version"`
	PreviousHash  string   `json:"previousblockhash"`
	MerkleRoot    string   `json:"merkleroot"`
	Time          int64    `json:"time"`
	Bits          string   `json:"bits"`
	Nonce         uint64   `json:"nonce"`
	Size          int64    `json:"size"`
	TxCount       int      `json:"-"` // computed from len(Tx)
	Tx            []string `json:"tx"`
	Confirmations int64    `json:"confirmations"`
	NextBlockHash string   `json:"nextblockhash"`
}

// VerboseTx holds verbose transaction details from getrawtransaction(txid, true).
type VerboseTx struct {
	TxID          string     `json:"txid"`
	Hash          string     `json:"hash"`
	Version       int32      `json:"version"`
	Size          int64      `json:"size"`
	LockTime      uint32     `json:"locktime"`
	Vin           []TxInput  `json:"vin"`
	Vout          []TxOutput `json:"vout"`
	BlockHash     string     `json:"blockhash"`
	Confirmations int64      `json:"confirmations"`
	Time          int64      `json:"time"`
	BlockTime     int64      `json:"blocktime"`
}

// TxInput holds a transaction input.
type TxInput struct {
	TxID      string    `json:"txid"`
	Vout      uint32    `json:"vout"`
	ScriptSig ScriptSig `json:"scriptSig"`
	Sequence  uint32    `json:"sequence"`
	Coinbase  string    `json:"coinbase,omitempty"`
}

// ScriptSig holds the unlocking script details.
type ScriptSig struct {
	ASM string `json:"asm"`
	Hex string `json:"hex"`
}

// TxOutput holds a transaction output.
type TxOutput struct {
	Value        float64      `json:"value"`
	N            uint32       `json:"n"`
	ScriptPubKey ScriptPubKey `json:"scriptPubKey"`
}

// ScriptPubKey holds the locking script details.
type ScriptPubKey struct {
	ASM       string   `json:"asm"`
	Hex       string   `json:"hex"`
	ReqSigs   int      `json:"reqSigs"`
	Type      string   `json:"type"`
	Addresses []string `json:"addresses"`
}

// Explorer wraps RPCClient with explorer-specific query methods.
type Explorer struct {
	rpc *network.RPCClient
}

// NewExplorer creates a new Explorer with the given RPC client.
func NewExplorer(rpc *network.RPCClient) *Explorer {
	return &Explorer{rpc: rpc}
}

// GetChainInfo calls getblockchaininfo.
func (e *Explorer) GetChainInfo(ctx context.Context) (*ChainInfo, error) {
	var info ChainInfo
	if err := e.rpc.Call(ctx, "getblockchaininfo", nil, &info); err != nil {
		return nil, fmt.Errorf("getblockchaininfo: %w", err)
	}
	return &info, nil
}

// GetBlockHash calls getblockhash for a given height.
func (e *Explorer) GetBlockHash(ctx context.Context, height int64) (string, error) {
	var hash string
	if err := e.rpc.Call(ctx, "getblockhash", []interface{}{height}, &hash); err != nil {
		return "", fmt.Errorf("getblockhash(%d): %w", height, err)
	}
	return hash, nil
}

// GetBlock calls getblock with verbosity=1 (JSON with txid list).
func (e *Explorer) GetBlock(ctx context.Context, hash string) (*BlockInfo, error) {
	var block BlockInfo
	if err := e.rpc.Call(ctx, "getblock", []interface{}{hash, 1}, &block); err != nil {
		return nil, fmt.Errorf("getblock(%s): %w", hash, err)
	}
	block.TxCount = len(block.Tx)
	return &block, nil
}

// GetVerboseTx calls getrawtransaction with verbose=true.
func (e *Explorer) GetVerboseTx(ctx context.Context, txid string) (*VerboseTx, error) {
	var tx VerboseTx
	if err := e.rpc.Call(ctx, "getrawtransaction", []interface{}{txid, true}, &tx); err != nil {
		return nil, fmt.Errorf("getrawtransaction(%s): %w", txid, err)
	}
	return &tx, nil
}

// GetRawTxHex calls getrawtransaction with verbose=false, returns hex string.
func (e *Explorer) GetRawTxHex(ctx context.Context, txid string) (string, error) {
	var hex string
	if err := e.rpc.Call(ctx, "getrawtransaction", []interface{}{txid, false}, &hex); err != nil {
		return "", fmt.Errorf("getrawtransaction(%s): %w", txid, err)
	}
	return hex, nil
}

// GetRecentBlocks returns the N most recent blocks (from tip backwards).
func (e *Explorer) GetRecentBlocks(ctx context.Context, count int) ([]*BlockInfo, error) {
	info, err := e.GetChainInfo(ctx)
	if err != nil {
		return nil, err
	}

	blocks := make([]*BlockInfo, 0, count)
	hash := info.BestBlockHash
	for i := 0; i < count && hash != ""; i++ {
		block, err := e.GetBlock(ctx, hash)
		if err != nil {
			break
		}
		blocks = append(blocks, block)
		hash = block.PreviousHash
	}
	return blocks, nil
}

// SearchQuery determines the type of a search query and returns a redirect path.
func (e *Explorer) SearchQuery(ctx context.Context, q string) (string, error) {
	// Try as block height (strict numeric parse).
	if height, err := strconv.ParseInt(q, 10, 64); err == nil && height >= 0 {
		hash, err := e.GetBlockHash(ctx, height)
		if err == nil {
			return "/block/" + hash, nil
		}
	}

	// Try as txid or block hash (64-char hex)
	if len(q) == 64 {
		// Try block first
		if _, err := e.GetBlock(ctx, q); err == nil {
			return "/block/" + q, nil
		}
		// Try tx
		if _, err := e.GetVerboseTx(ctx, q); err == nil {
			return "/tx/" + q, nil
		}
	}

	// Try as address — check if there are UTXOs
	utxos, err := e.rpc.ListUnspent(ctx, q)
	if err == nil && len(utxos) > 0 {
		return "/address/" + q, nil
	}

	return "", fmt.Errorf("not found: %s", q)
}

// formatSat formats satoshis as a display string.
func formatSat(sat int64) string {
	btc := float64(sat) / 1e8
	return fmt.Sprintf("%.8f BSV", btc)
}

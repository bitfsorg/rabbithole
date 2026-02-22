//go:build e2e

package testutil

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

const (
	// Default regtest node connection parameters (matches docker-compose.yml / bitcoin.conf).
	defaultRPCURL  = "http://localhost:18332"
	defaultRPCUser = "bitfs"
	defaultRPCPass = "bitfs"
)

// RegtestUTXO represents an unspent transaction output from the regtest node.
type RegtestUTXO struct {
	TxID          string  `json:"txid"`
	Vout          uint32  `json:"vout"`
	Address       string  `json:"address"`
	ScriptPubKey  string  `json:"scriptPubKey"`
	Amount        float64 `json:"amount"`
	Confirmations int     `json:"confirmations"`
}

// RegtestNode provides high-level helpers around a BSV regtest JSON-RPC node.
type RegtestNode struct {
	rpc *RPCClient
}

// NewRegtestNode creates a RegtestNode that connects to the local regtest
// node at localhost:18332 with the default bitfs/bitfs credentials.
func NewRegtestNode() *RegtestNode {
	return &RegtestNode{
		rpc: NewRPCClient(defaultRPCURL, defaultRPCUser, defaultRPCPass),
	}
}

// IsAvailable returns true if the regtest node is reachable and responding.
func (n *RegtestNode) IsAvailable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var hash string
	err := n.rpc.Call(ctx, "getbestblockhash", nil, &hash)
	return err == nil && hash != ""
}

// SkipIfUnavailable skips the current test if the regtest node is not reachable.
func SkipIfUnavailable(t *testing.T, node *RegtestNode) {
	t.Helper()
	if !node.IsAvailable(context.Background()) {
		t.Skip("BSV regtest node not available (start with: docker compose -f e2e/docker-compose.yml up -d)")
	}
}

// NewAddress generates a new address from the node's wallet.
func (n *RegtestNode) NewAddress(ctx context.Context) (string, error) {
	var addr string
	if err := n.rpc.Call(ctx, "getnewaddress", nil, &addr); err != nil {
		return "", fmt.Errorf("getnewaddress: %w", err)
	}
	return addr, nil
}

// MineBlocks mines the given number of blocks, sending coinbase rewards to addr.
// Returns the hashes of the generated blocks.
func (n *RegtestNode) MineBlocks(ctx context.Context, count int, addr string) ([]string, error) {
	var hashes []string
	params := []interface{}{count, addr}
	if err := n.rpc.Call(ctx, "generatetoaddress", params, &hashes); err != nil {
		return nil, fmt.Errorf("generatetoaddress(%d, %s): %w", count, addr, err)
	}
	return hashes, nil
}

// ListUnspent returns the unspent outputs for the given address.
// The address must be known to the node's wallet (imported or generated).
func (n *RegtestNode) ListUnspent(ctx context.Context, addr string) ([]RegtestUTXO, error) {
	var utxos []RegtestUTXO
	// listunspent minconf=1 maxconf=9999999 [addresses]
	params := []interface{}{1, 9999999, []string{addr}}
	if err := n.rpc.Call(ctx, "listunspent", params, &utxos); err != nil {
		return nil, fmt.Errorf("listunspent(%s): %w", addr, err)
	}
	return utxos, nil
}

// SendRawTransaction broadcasts a signed raw transaction hex to the network.
// Returns the transaction ID.
func (n *RegtestNode) SendRawTransaction(ctx context.Context, rawTxHex string) (string, error) {
	var txid string
	params := []interface{}{rawTxHex}
	if err := n.rpc.Call(ctx, "sendrawtransaction", params, &txid); err != nil {
		return "", fmt.Errorf("sendrawtransaction: %w", err)
	}
	return txid, nil
}

// SendToAddress sends the given BTC amount to addr from the node's wallet.
// Returns the transaction ID.
func (n *RegtestNode) SendToAddress(ctx context.Context, addr string, amount float64) (string, error) {
	var txid string
	params := []interface{}{addr, amount}
	if err := n.rpc.Call(ctx, "sendtoaddress", params, &txid); err != nil {
		return "", fmt.Errorf("sendtoaddress(%s, %f): %w", addr, amount, err)
	}
	return txid, nil
}

// GetRawTransaction returns the raw transaction bytes for the given txid.
func (n *RegtestNode) GetRawTransaction(ctx context.Context, txid string) ([]byte, error) {
	var rawHex string
	// getrawtransaction txid verbose=false
	params := []interface{}{txid, false}
	if err := n.rpc.Call(ctx, "getrawtransaction", params, &rawHex); err != nil {
		return nil, fmt.Errorf("getrawtransaction(%s): %w", txid, err)
	}
	raw, err := hex.DecodeString(rawHex)
	if err != nil {
		return nil, fmt.Errorf("decode raw tx hex: %w", err)
	}
	return raw, nil
}

// GetBlockHeader returns the raw 80-byte block header for the given block hash.
func (n *RegtestNode) GetBlockHeader(ctx context.Context, blockHash string) ([]byte, error) {
	var headerHex string
	// getblockheader hash verbose=false → returns hex string of 80-byte header
	params := []interface{}{blockHash, false}
	if err := n.rpc.Call(ctx, "getblockheader", params, &headerHex); err != nil {
		return nil, fmt.Errorf("getblockheader(%s): %w", blockHash, err)
	}
	header, err := hex.DecodeString(headerHex)
	if err != nil {
		return nil, fmt.Errorf("decode block header hex: %w", err)
	}
	return header, nil
}

// GetBlockHeaderVerbose returns the verbose block header info as a map.
func (n *RegtestNode) GetBlockHeaderVerbose(ctx context.Context, blockHash string) (map[string]interface{}, error) {
	var result map[string]interface{}
	// getblockheader hash verbose=true
	params := []interface{}{blockHash, true}
	if err := n.rpc.Call(ctx, "getblockheader", params, &result); err != nil {
		return nil, fmt.Errorf("getblockheader verbose(%s): %w", blockHash, err)
	}
	return result, nil
}

// GetTxOutProof returns the BIP37-style Merkle proof for the given transaction.
// The transaction must be in the UTXO set or in the mempool.
func (n *RegtestNode) GetTxOutProof(ctx context.Context, txid string) ([]byte, error) {
	var proofHex string
	params := []interface{}{[]string{txid}}
	if err := n.rpc.Call(ctx, "gettxoutproof", params, &proofHex); err != nil {
		return nil, fmt.Errorf("gettxoutproof(%s): %w", txid, err)
	}
	proof, err := hex.DecodeString(proofHex)
	if err != nil {
		return nil, fmt.Errorf("decode txout proof hex: %w", err)
	}
	return proof, nil
}

// GetBestBlockHash returns the hash of the best (tip) block.
func (n *RegtestNode) GetBestBlockHash(ctx context.Context) (string, error) {
	var hash string
	if err := n.rpc.Call(ctx, "getbestblockhash", nil, &hash); err != nil {
		return "", fmt.Errorf("getbestblockhash: %w", err)
	}
	return hash, nil
}

// GetBlockHash returns the block hash at the given height.
func (n *RegtestNode) GetBlockHash(ctx context.Context, height int) (string, error) {
	var hash string
	params := []interface{}{height}
	if err := n.rpc.Call(ctx, "getblockhash", params, &hash); err != nil {
		return "", fmt.Errorf("getblockhash(%d): %w", height, err)
	}
	return hash, nil
}

// GetBlockCount returns the current block height.
func (n *RegtestNode) GetBlockCount(ctx context.Context) (int64, error) {
	var count int64
	if err := n.rpc.Call(ctx, "getblockcount", nil, &count); err != nil {
		return 0, fmt.Errorf("getblockcount: %w", err)
	}
	return count, nil
}

// ImportAddress imports an address (or script) for watch-only tracking.
// This allows ListUnspent to find UTXOs sent to non-wallet addresses.
func (n *RegtestNode) ImportAddress(ctx context.Context, addr string) error {
	// importaddress address label="" rescan=false
	params := []interface{}{addr, "", false}
	if err := n.rpc.Call(ctx, "importaddress", params, nil); err != nil {
		return fmt.Errorf("importaddress(%s): %w", addr, err)
	}
	return nil
}

// FundAddress is a convenience method that generates a fresh wallet address,
// mines 101 blocks to it (making the coinbase spendable), and then returns
// the first available UTXO. This is the typical setup for e2e tests that need
// funded outputs.
func (n *RegtestNode) FundAddress(ctx context.Context, addr string) (*RegtestUTXO, error) {
	// Mine 101 blocks to make coinbase maturable (100 confirmations required).
	_, err := n.MineBlocks(ctx, 101, addr)
	if err != nil {
		return nil, fmt.Errorf("mine 101 blocks to %s: %w", addr, err)
	}

	utxos, err := n.ListUnspent(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("list unspent for %s: %w", addr, err)
	}
	if len(utxos) == 0 {
		return nil, fmt.Errorf("no UTXOs found for %s after mining 101 blocks", addr)
	}

	return &utxos[0], nil
}

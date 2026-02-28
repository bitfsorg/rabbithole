# Den — BitFS Blockchain Explorer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a web-based BSV blockchain explorer with BitFS/Metanet protocol verification for regtest and testnet development.

**Architecture:** Go HTTP server with htmx for dynamic UI, directly querying bitcoind via JSON-RPC. Reuses libbitfs packages (tx, metanet, network, method42, spv) for protocol parsing and verification. Single binary with embedded templates.

**Tech Stack:** Go 1.25, net/http, html/template, embed, htmx (CDN), libbitfs

---

## Prerequisites

- Docker Desktop running with `bitfs/e2e/docker-compose.yml` for regtest node
- libbitfs at `../libbitfs` (go.mod `replace` directive)

---

### Task 1: Initialize Go Module & Project Skeleton

**Files:**
- Create: `den/go.mod`
- Create: `den/go.sum`
- Create: `den/main.go`

**Step 1: Create go.mod**

```
cd den && go mod init github.com/tongxiaofeng/den
```

Edit `go.mod` to add replace directive:
```
module github.com/tongxiaofeng/den

go 1.25

require github.com/tongxiaofeng/libbitfs v0.0.0

replace github.com/tongxiaofeng/libbitfs => ../libbitfs
```

**Step 2: Write minimal main.go**

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/tongxiaofeng/libbitfs/network"
)

func main() {
	rpcURL := flag.String("rpc-url", "", "bitcoind RPC URL")
	rpcUser := flag.String("rpc-user", "", "RPC username")
	rpcPass := flag.String("rpc-pass", "", "RPC password")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	net := flag.String("network", "regtest", "Network: regtest|testnet")
	flag.Parse()

	cfg, err := network.ResolveConfig(
		&network.RPCConfig{URL: *rpcURL, User: *rpcUser, Password: *rpcPass},
		nil, *net,
	)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	rpc := network.NewRPCClient(*cfg)
	_ = rpc

	fmt.Printf("Den starting on %s (network=%s, rpc=%s)\n", *addr, *net, cfg.URL)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
```

**Step 3: Tidy and verify it compiles**

```bash
cd den && go mod tidy && go build .
```

**Step 4: Commit**

```bash
git add den/go.mod den/go.sum den/main.go
git commit -m "feat(den): initialize Go module with RPC config"
```

---

### Task 2: RPC Service Layer — Explorer-Specific Queries

The existing `BlockchainService` interface covers UTXO/tx/header operations but lacks block-level queries needed for an explorer (getblock, getblockhash, getblockchaininfo). Build a thin wrapper that uses `RPCClient.Call()` directly.

**Files:**
- Create: `den/rpc.go`
- Create: `den/rpc_test.go`

**Step 1: Write types and explorer RPC wrapper**

`den/rpc.go`:
```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tongxiaofeng/libbitfs/network"
)

// ChainInfo holds blockchain summary from getblockchaininfo.
type ChainInfo struct {
	Chain         string `json:"chain"`
	Blocks        int64  `json:"blocks"`
	BestBlockHash string `json:"bestblockhash"`
	Difficulty    float64 `json:"difficulty"`
	MedianTime    int64  `json:"mediantime"`
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
	Confirmations int64   `json:"confirmations"`
	NextBlockHash string   `json:"nextblockhash"`
}

// VerboseTx holds verbose transaction details from getrawtransaction(txid, true).
type VerboseTx struct {
	TxID          string      `json:"txid"`
	Hash          string      `json:"hash"`
	Version       int32       `json:"version"`
	Size          int64       `json:"size"`
	LockTime      uint32      `json:"locktime"`
	Vin           []TxInput   `json:"vin"`
	Vout          []TxOutput  `json:"vout"`
	BlockHash     string      `json:"blockhash"`
	Confirmations int64       `json:"confirmations"`
	Time          int64       `json:"time"`
	BlockTime     int64       `json:"blocktime"`
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
	// Try as block height (numeric)
	var height int64
	if _, err := fmt.Sscanf(q, "%d", &height); err == nil && height >= 0 {
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

// btcToSat converts BTC float to satoshis.
func btcToSat(v float64) int64 {
	return int64(v * 1e8)
}

// formatSat formats satoshis as a display string.
func formatSat(sat int64) string {
	btc := float64(sat) / 1e8
	return fmt.Sprintf("%.8f BSV", btc)
}
```

**Step 2: Write a basic test (uses real RPC if available, otherwise skip)**

`den/rpc_test.go`:
```go
package main

import (
	"context"
	"os"
	"testing"

	"github.com/tongxiaofeng/libbitfs/network"
)

func newTestExplorer(t *testing.T) *Explorer {
	t.Helper()
	url := os.Getenv("DEN_RPC_URL")
	if url == "" {
		url = "http://localhost:18332"
	}
	rpc := network.NewRPCClient(network.RPCConfig{
		URL: url, User: "bitfs", Password: "bitfs",
	})
	// Quick health check
	ctx := context.Background()
	_, err := rpc.GetBestBlockHeight(ctx)
	if err != nil {
		t.Skipf("RPC not available: %v", err)
	}
	return NewExplorer(rpc)
}

func TestGetChainInfo(t *testing.T) {
	e := newTestExplorer(t)
	info, err := e.GetChainInfo(context.Background())
	if err != nil {
		t.Fatalf("GetChainInfo: %v", err)
	}
	if info.Chain != "regtest" {
		t.Errorf("expected regtest, got %s", info.Chain)
	}
	if info.Blocks < 0 {
		t.Errorf("invalid block count: %d", info.Blocks)
	}
}

func TestGetRecentBlocks(t *testing.T) {
	e := newTestExplorer(t)
	blocks, err := e.GetRecentBlocks(context.Background(), 5)
	if err != nil {
		t.Fatalf("GetRecentBlocks: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("no blocks returned")
	}
	// Genesis or first block should exist
	if blocks[len(blocks)-1].Height < 0 {
		t.Errorf("invalid block height")
	}
}
```

**Step 3: Run tests (requires docker regtest running)**

```bash
cd bitfs/e2e && docker compose up -d
cd ../../den && go test -v -run TestGet -count=1
```

**Step 4: Commit**

```bash
git add den/rpc.go den/rpc_test.go
git commit -m "feat(den): add explorer RPC service layer"
```

---

### Task 3: Metanet Transaction Decoder

Parse raw transactions and detect/decode Metanet OP_RETURN data using libbitfs/tx and libbitfs/metanet.

**Files:**
- Create: `den/decode.go`
- Create: `den/decode_test.go`

**Step 1: Write the Metanet decoder**

`den/decode.go`:
```go
package main

import (
	"encoding/hex"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/tongxiaofeng/libbitfs/metanet"
	libtx "github.com/tongxiaofeng/libbitfs/tx"
)

// DecodedMetanet holds parsed Metanet data from a transaction.
type DecodedMetanet struct {
	IsMetanet   bool
	PNode       string // hex-encoded compressed pubkey
	ParentTxID  string // hex-encoded (empty for root)
	IsRoot      bool
	Node        *metanet.Node
	RawPayload  string // hex-encoded TLV payload
	Error       string // parsing error if any
}

// TLVField represents a single decoded TLV field for display.
type TLVField struct {
	Tag     byte
	TagName string
	Length  int
	ValueHex string
	ValueStr string // human-readable interpretation
}

// tlvTagNames maps TLV tags to human-readable names.
var tlvTagNames = map[byte]string{
	0x01: "Version",
	0x02: "Type",
	0x03: "Operation",
	0x04: "MimeType",
	0x05: "FileSize",
	0x06: "KeyHash",
	0x07: "Access",
	0x08: "PricePerKB",
	0x09: "LinkTarget",
	0x0A: "LinkType",
	0x0B: "Timestamp",
	0x0C: "Parent",
	0x0D: "Index",
	0x0E: "ChildEntry",
	0x0F: "NextChildIndex",
	0x10: "Domain",
	0x11: "Keywords",
	0x12: "Description",
	0x13: "Encrypted",
	0x14: "OnChain",
	0x15: "ContentTxID",
	0x16: "Compression",
	0x17: "CltvHeight",
	0x18: "RevenueShare",
	0x19: "NetworkName",
	0x1A: "MerkleRoot",
}

// DecodeMetanetTx attempts to parse a raw transaction and extract Metanet data.
// rawTxBytes is the serialized transaction.
func DecodeMetanetTx(rawTxBytes []byte) *DecodedMetanet {
	result := &DecodedMetanet{}

	sdkTx, err := transaction.NewTransactionFromBytes(rawTxBytes)
	if err != nil {
		result.Error = fmt.Sprintf("parse tx: %v", err)
		return result
	}

	// Find OP_RETURN output (typically output 0)
	pushes := extractOPReturnPushes(sdkTx)
	if pushes == nil {
		return result // not a Metanet tx
	}

	// Check MetaFlag
	if len(pushes) < 4 {
		return result
	}
	if string(pushes[0]) != libtx.MetaFlag {
		return result
	}

	result.IsMetanet = true
	result.PNode = hex.EncodeToString(pushes[1])
	result.ParentTxID = hex.EncodeToString(pushes[2])
	result.IsRoot = len(pushes[2]) == 0
	result.RawPayload = hex.EncodeToString(pushes[3])

	// Parse into full Node
	node, err := metanet.ParseNode(pushes)
	if err != nil {
		result.Error = fmt.Sprintf("parse metanet: %v", err)
		return result
	}
	result.Node = node
	return result
}

// DecodeTLVFields parses raw TLV payload bytes into individual fields for display.
func DecodeTLVFields(payloadHex string) []TLVField {
	data, err := hex.DecodeString(payloadHex)
	if err != nil {
		return nil
	}

	var fields []TLVField
	pos := 0
	for pos+3 <= len(data) {
		tag := data[pos]
		if pos+3 > len(data) {
			break
		}
		length := int(data[pos+1]) | int(data[pos+2])<<8
		pos += 3

		if pos+length > len(data) {
			break
		}
		value := data[pos : pos+length]
		pos += length

		name := tlvTagNames[tag]
		if name == "" {
			name = fmt.Sprintf("Unknown(0x%02X)", tag)
		}

		field := TLVField{
			Tag:      tag,
			TagName:  name,
			Length:   length,
			ValueHex: hex.EncodeToString(value),
			ValueStr: interpretTLVValue(tag, value),
		}
		fields = append(fields, field)
	}
	return fields
}

// interpretTLVValue returns a human-readable string for common TLV fields.
func interpretTLVValue(tag byte, value []byte) string {
	switch tag {
	case 0x01: // Version
		if len(value) == 4 {
			return fmt.Sprintf("%d", uint32(value[0])|uint32(value[1])<<8|uint32(value[2])<<16|uint32(value[3])<<24)
		}
	case 0x02: // Type
		if len(value) == 4 {
			v := int32(value[0]) | int32(value[1])<<8 | int32(value[2])<<16 | int32(value[3])<<24
			return metanet.NodeType(v).String()
		}
	case 0x03: // Op
		if len(value) == 4 {
			v := int32(value[0]) | int32(value[1])<<8 | int32(value[2])<<16 | int32(value[3])<<24
			return metanet.OpType(v).String()
		}
	case 0x04: // MimeType
		return string(value)
	case 0x05: // FileSize
		if len(value) == 8 {
			v := uint64(0)
			for i := 0; i < 8; i++ {
				v |= uint64(value[i]) << (i * 8)
			}
			return fmt.Sprintf("%d bytes", v)
		}
	case 0x07: // Access
		if len(value) == 4 {
			v := int32(value[0]) | int32(value[1])<<8 | int32(value[2])<<16 | int32(value[3])<<24
			switch metanet.AccessLevel(v) {
			case metanet.AccessPrivate:
				return "PRIVATE"
			case metanet.AccessFree:
				return "FREE"
			case metanet.AccessPaid:
				return "PAID"
			}
		}
	case 0x10: // Domain
		return string(value)
	case 0x11: // Keywords
		return string(value)
	case 0x12: // Description
		return string(value)
	case 0x19: // NetworkName
		return string(value)
	}
	return ""
}

// extractOPReturnPushes finds and extracts OP_RETURN data pushes from a transaction.
func extractOPReturnPushes(tx *transaction.Transaction) [][]byte {
	for _, out := range tx.Outputs {
		script := out.LockingScript
		// OP_FALSE (0x00) OP_RETURN (0x6a) or OP_RETURN (0x6a)
		chunks, err := script.Chunks()
		if err != nil || len(chunks) < 3 {
			continue
		}

		isOPReturn := false
		startIdx := 0
		// Check for OP_FALSE OP_RETURN pattern
		if chunks[0].Op == 0x00 && chunks[1].Op == 0x6a {
			isOPReturn = true
			startIdx = 2
		} else if chunks[0].Op == 0x6a {
			isOPReturn = true
			startIdx = 1
		}

		if !isOPReturn {
			continue
		}

		var pushes [][]byte
		for _, chunk := range chunks[startIdx:] {
			pushes = append(pushes, chunk.Data)
		}
		return pushes
	}
	return nil
}
```

**Step 2: Write tests with known Metanet data**

`den/decode_test.go`:
```go
package main

import (
	"encoding/hex"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/tongxiaofeng/libbitfs/metanet"
	libtx "github.com/tongxiaofeng/libbitfs/tx"
)

func TestDecodeMetanetTx_NonMetanet(t *testing.T) {
	// A minimal coinbase-like raw tx (won't have OP_RETURN)
	result := DecodeMetanetTx([]byte{0x01, 0x00})
	if result.IsMetanet {
		t.Error("should not be detected as Metanet")
	}
}

func TestDecodeTLVFields(t *testing.T) {
	// Manually construct a TLV: Version=1 (tag=0x01, len=4, value=LE uint32(1))
	payload := []byte{
		0x01, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, // Version=1
		0x02, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, // Type=DIR
	}
	fields := DecodeTLVFields(hex.EncodeToString(payload))
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields[0].TagName != "Version" {
		t.Errorf("expected Version, got %s", fields[0].TagName)
	}
	if fields[0].ValueStr != "1" {
		t.Errorf("expected '1', got '%s'", fields[0].ValueStr)
	}
	if fields[1].TagName != "Type" {
		t.Errorf("expected Type, got %s", fields[1].TagName)
	}
	if fields[1].ValueStr != "DIR" {
		t.Errorf("expected DIR, got '%s'", fields[1].ValueStr)
	}
}

func TestDecodeMetanetTx_RoundTrip(t *testing.T) {
	// Build a real Metanet root tx using libbitfs/tx
	privKey, _ := ec.NewPrivateKey()
	pubKey := privKey.PubKey()

	// Create a minimal Node and serialize
	node := &metanet.Node{
		Version: 1,
		Type:    metanet.NodeTypeDir,
		Op:      metanet.OpCreate,
		Access:  metanet.AccessFree,
	}
	payload, err := metanet.SerializePayload(node)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	pushes, err := libtx.BuildOPReturnData(pubKey, nil, payload)
	if err != nil {
		t.Fatalf("build OP_RETURN: %v", err)
	}

	// Verify ParseOPReturnData round-trips
	pNode, parentTxID, payloadOut, err := libtx.ParseOPReturnData(pushes)
	if err != nil {
		t.Fatalf("parse OP_RETURN: %v", err)
	}
	if len(pNode) != 33 {
		t.Errorf("pNode length: %d", len(pNode))
	}
	if len(parentTxID) != 0 {
		t.Error("root should have empty parentTxID")
	}
	if len(payloadOut) == 0 {
		t.Error("payload should not be empty")
	}
}
```

**Step 3: Run tests**

```bash
cd den && go mod tidy && go test -v -run TestDecode -count=1
```

**Step 4: Commit**

```bash
git add den/decode.go den/decode_test.go den/go.mod den/go.sum
git commit -m "feat(den): add Metanet transaction decoder with TLV field parsing"
```

---

### Task 4: HTML Templates — Base Layout + Home Page

Embed HTML templates with htmx for dynamic UI. Start with the base layout and home page showing recent blocks.

**Files:**
- Create: `den/templates/base.html`
- Create: `den/templates/home.html`
- Create: `den/templates/partials/blocks.html`
- Create: `den/templates.go`

**Step 1: Create base layout template**

`den/templates/base.html`:
```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Den — {{.Title}}</title>
  <script src="https://unpkg.com/htmx.org@2.0.4"></script>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: 'SF Mono', 'Menlo', 'Monaco', monospace; font-size: 14px; line-height: 1.6; color: #e0e0e0; background: #1a1a2e; }
    a { color: #64b5f6; text-decoration: none; }
    a:hover { text-decoration: underline; }
    .container { max-width: 1200px; margin: 0 auto; padding: 16px; }
    header { background: #16213e; border-bottom: 1px solid #0f3460; padding: 12px 0; margin-bottom: 24px; }
    header .container { display: flex; align-items: center; justify-content: space-between; }
    header h1 { font-size: 20px; color: #e94560; }
    header h1 span { color: #666; font-weight: normal; font-size: 14px; }
    .search-form { display: flex; gap: 8px; }
    .search-form input { background: #1a1a2e; border: 1px solid #0f3460; color: #e0e0e0; padding: 6px 12px; border-radius: 4px; width: 400px; font-family: inherit; font-size: 13px; }
    .search-form button { background: #0f3460; color: #e0e0e0; border: none; padding: 6px 16px; border-radius: 4px; cursor: pointer; font-family: inherit; }
    .card { background: #16213e; border: 1px solid #0f3460; border-radius: 6px; padding: 16px; margin-bottom: 16px; }
    .card h2 { font-size: 16px; color: #e94560; margin-bottom: 12px; }
    table { width: 100%; border-collapse: collapse; }
    th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid #0f3460; }
    th { color: #999; font-weight: normal; font-size: 12px; text-transform: uppercase; }
    td { font-size: 13px; }
    .mono { font-family: inherit; }
    .hash { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; }
    .tag { display: inline-block; padding: 2px 8px; border-radius: 3px; font-size: 11px; font-weight: bold; }
    .tag-file { background: #1b5e20; color: #a5d6a7; }
    .tag-dir { background: #0d47a1; color: #90caf9; }
    .tag-link { background: #4a148c; color: #ce93d8; }
    .tag-root { background: #e65100; color: #ffcc80; }
    .tag-private { background: #880e4f; color: #f48fb1; }
    .tag-free { background: #1b5e20; color: #a5d6a7; }
    .tag-paid { background: #f57f17; color: #fff9c4; }
    .tag-metanet { background: #e94560; color: #fff; }
    .chain-info { display: flex; gap: 24px; margin-bottom: 16px; }
    .chain-info .stat { }
    .chain-info .stat .label { color: #999; font-size: 12px; }
    .chain-info .stat .value { font-size: 18px; color: #e0e0e0; }
    .field-row { display: flex; gap: 8px; padding: 6px 0; border-bottom: 1px solid #0f3460; }
    .field-row .label { color: #999; min-width: 140px; font-size: 12px; }
    .field-row .value { word-break: break-all; }
    .htmx-indicator { display: none; }
    .htmx-request .htmx-indicator { display: inline; }
    .error { color: #e94560; padding: 12px; border: 1px solid #e94560; border-radius: 4px; }
    .breadcrumb { margin-bottom: 16px; color: #999; font-size: 13px; }
    .breadcrumb a { color: #64b5f6; }
  </style>
</head>
<body>
  <header>
    <div class="container">
      <h1>Den <span>BitFS Explorer</span></h1>
      <form class="search-form" action="/search" method="get">
        <input type="text" name="q" placeholder="Search txid / block hash / height / address" autocomplete="off">
        <button type="submit">Search</button>
      </form>
    </div>
  </header>
  <div class="container">
    {{template "content" .}}
  </div>
</body>
</html>
```

`den/templates/home.html`:
```html
{{define "content"}}
<div class="chain-info">
  <div class="stat">
    <div class="label">Network</div>
    <div class="value">{{.Chain.Chain}}</div>
  </div>
  <div class="stat">
    <div class="label">Height</div>
    <div class="value">{{.Chain.Blocks}}</div>
  </div>
  <div class="stat">
    <div class="label">Best Block</div>
    <div class="value"><a href="/block/{{.Chain.BestBlockHash}}" class="hash" style="max-width:160px">{{.Chain.BestBlockHash}}</a></div>
  </div>
</div>

<div class="card">
  <h2>Recent Blocks</h2>
  <div id="block-list">
    {{template "block-table" .Blocks}}
  </div>
</div>
{{end}}

{{define "block-table"}}
<table>
  <tr><th>Height</th><th>Hash</th><th>Txs</th><th>Size</th><th>Time</th></tr>
  {{range .}}
  <tr>
    <td>{{.Height}}</td>
    <td><a href="/block/{{.Hash}}" class="hash">{{.Hash}}</a></td>
    <td>{{.TxCount}}</td>
    <td>{{.Size}} B</td>
    <td>{{formatTime .Time}}</td>
  </tr>
  {{end}}
</table>
{{end}}
```

**Step 2: Create template loader with embed**

`den/templates.go`:
```go
package main

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"time"
)

//go:embed templates
var templateFS embed.FS

// tmplFuncs are custom template functions.
var tmplFuncs = template.FuncMap{
	"formatTime": func(unix int64) string {
		if unix == 0 {
			return "-"
		}
		return time.Unix(unix, 0).Format("2006-01-02 15:04:05")
	},
	"formatSatoshis": func(sat int64) string {
		return formatSat(sat)
	},
	"truncHash": func(h string) string {
		if len(h) > 16 {
			return h[:8] + "..." + h[len(h)-8:]
		}
		return h
	},
}

// Templates holds all parsed templates.
type Templates struct {
	base *template.Template
}

// LoadTemplates parses all embedded templates.
func LoadTemplates() (*Templates, error) {
	base, err := template.New("base").Funcs(tmplFuncs).ParseFS(templateFS,
		"templates/base.html",
		"templates/home.html",
		"templates/partials/*.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Templates{base: base}, nil
}

// Render renders a named template into the writer.
func (t *Templates) Render(w io.Writer, name string, data interface{}) error {
	return t.base.ExecuteTemplate(w, name, data)
}
```

**Step 3: Verify templates parse**

```bash
cd den && go build .
```

**Step 4: Commit**

```bash
git add den/templates/ den/templates.go
git commit -m "feat(den): add base layout, home page, and template system"
```

---

### Task 5: HTTP Handlers — Home, Block, Tx, Search

Wire up the HTTP routes and handlers.

**Files:**
- Create: `den/handlers.go`
- Modify: `den/main.go` (wire routes)

**Step 1: Write handlers**

`den/handlers.go`:
```go
package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Server holds shared state for HTTP handlers.
type Server struct {
	explorer  *Explorer
	templates *Templates
}

// NewServer creates a new HTTP server.
func NewServer(explorer *Explorer, templates *Templates) *Server {
	return &Server{explorer: explorer, templates: templates}
}

// Routes registers all HTTP routes.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleHome)
	mux.HandleFunc("GET /block/{hash}", s.handleBlock)
	mux.HandleFunc("GET /block/height/{n}", s.handleBlockByHeight)
	mux.HandleFunc("GET /tx/{txid}", s.handleTx)
	mux.HandleFunc("GET /address/{addr}", s.handleAddress)
	mux.HandleFunc("GET /search", s.handleSearch)
	mux.HandleFunc("GET /metanet/{txid}", s.handleMetanet)
	mux.HandleFunc("GET /spv/{txid}", s.handleSPV)
	return mux
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	chain, err := s.explorer.GetChainInfo(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	blocks, err := s.explorer.GetRecentBlocks(ctx, 20)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	data := map[string]interface{}{
		"Title":  "Home",
		"Chain":  chain,
		"Blocks": blocks,
	}
	s.render(w, "base.html", data)
}

func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	ctx := r.Context()

	block, err := s.explorer.GetBlock(ctx, hash)
	if err != nil {
		http.Error(w, fmt.Sprintf("block not found: %v", err), 404)
		return
	}

	data := map[string]interface{}{
		"Title": fmt.Sprintf("Block %d", block.Height),
		"Block": block,
	}
	s.render(w, "base.html", data)
}

func (s *Server) handleBlockByHeight(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.ParseInt(r.PathValue("n"), 10, 64)
	if err != nil {
		http.Error(w, "invalid height", 400)
		return
	}

	hash, err := s.explorer.GetBlockHash(r.Context(), n)
	if err != nil {
		http.Error(w, fmt.Sprintf("block not found: %v", err), 404)
		return
	}
	http.Redirect(w, r, "/block/"+hash, http.StatusFound)
}

func (s *Server) handleTx(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	vtx, err := s.explorer.GetVerboseTx(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("tx not found: %v", err), 404)
		return
	}

	// Try to decode as Metanet tx
	rawBytes, _ := s.explorer.rpc.GetRawTx(ctx, txid)
	var decoded *DecodedMetanet
	if rawBytes != nil {
		decoded = DecodeMetanetTx(rawBytes)
	}

	data := map[string]interface{}{
		"Title":   fmt.Sprintf("Tx %s", truncHash(txid)),
		"Tx":      vtx,
		"Metanet": decoded,
	}
	s.render(w, "base.html", data)
}

func (s *Server) handleAddress(w http.ResponseWriter, r *http.Request) {
	addr := r.PathValue("addr")
	ctx := r.Context()

	utxos, err := s.explorer.rpc.ListUnspent(ctx, addr)
	if err != nil {
		http.Error(w, fmt.Sprintf("address lookup failed: %v", err), 500)
		return
	}

	data := map[string]interface{}{
		"Title":   fmt.Sprintf("Address %s", truncHash(addr)),
		"Address": addr,
		"UTXOs":   utxos,
	}
	s.render(w, "base.html", data)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	path, err := s.explorer.SearchQuery(r.Context(), q)
	if err != nil {
		data := map[string]interface{}{
			"Title": "Search",
			"Query": q,
			"Error": err.Error(),
		}
		s.render(w, "base.html", data)
		return
	}
	http.Redirect(w, r, path, http.StatusFound)
}

func (s *Server) handleMetanet(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	rawBytes, err := s.explorer.rpc.GetRawTx(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("tx not found: %v", err), 404)
		return
	}

	decoded := DecodeMetanetTx(rawBytes)
	if !decoded.IsMetanet {
		http.Error(w, "not a Metanet transaction", 400)
		return
	}

	var tlvFields []TLVField
	if decoded.RawPayload != "" {
		tlvFields = DecodeTLVFields(decoded.RawPayload)
	}

	data := map[string]interface{}{
		"Title":     fmt.Sprintf("Metanet %s", truncHash(txid)),
		"TxID":      txid,
		"Metanet":   decoded,
		"TLVFields": tlvFields,
	}
	s.render(w, "base.html", data)
}

func (s *Server) handleSPV(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	proof, err := s.explorer.rpc.GetMerkleProof(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("proof not available: %v", err), 404)
		return
	}

	// Get block header for verification
	headerBytes, _ := s.explorer.rpc.GetBlockHeader(ctx, proof.BlockHash)

	data := map[string]interface{}{
		"Title":       fmt.Sprintf("SPV Proof %s", truncHash(txid)),
		"TxID":        txid,
		"Proof":       proof,
		"HeaderBytes": headerBytes,
	}
	s.render(w, "base.html", data)
}

func (s *Server) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.Render(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func truncHash(h string) string {
	if len(h) > 16 {
		return h[:8] + "..." + h[len(h)-8:]
	}
	return h
}
```

**Step 2: Update main.go to wire routes**

Replace `den/main.go`:
```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/tongxiaofeng/libbitfs/network"
)

func main() {
	rpcURL := flag.String("rpc-url", "", "bitcoind RPC URL")
	rpcUser := flag.String("rpc-user", "", "RPC username")
	rpcPass := flag.String("rpc-pass", "", "RPC password")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	net := flag.String("network", "regtest", "Network: regtest|testnet")
	flag.Parse()

	cfg, err := network.ResolveConfig(
		&network.RPCConfig{URL: *rpcURL, User: *rpcUser, Password: *rpcPass},
		nil, *net,
	)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	rpc := network.NewRPCClient(*cfg)
	explorer := NewExplorer(rpc)

	templates, err := LoadTemplates()
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	srv := NewServer(explorer, templates)

	fmt.Printf("Den starting on %s (network=%s, rpc=%s)\n", *addr, *net, cfg.URL)
	log.Fatal(http.ListenAndServe(*addr, srv.Routes()))
}
```

**Step 3: Verify it compiles**

```bash
cd den && go build .
```

**Step 4: Commit**

```bash
git add den/handlers.go den/main.go
git commit -m "feat(den): add HTTP handlers for home, block, tx, address, search"
```

---

### Task 6: HTML Templates — Block Detail, Tx Detail, Address Pages

**Files:**
- Create: `den/templates/block.html`
- Create: `den/templates/tx.html`
- Create: `den/templates/address.html`
- Create: `den/templates/search.html`
- Modify: `den/templates.go` (add block/tx templates)

**Step 1: Block detail template**

`den/templates/block.html`:
```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / Block {{.Block.Height}}</div>
<div class="card">
  <h2>Block #{{.Block.Height}}</h2>
  <div class="field-row"><span class="label">Hash</span><span class="value mono">{{.Block.Hash}}</span></div>
  <div class="field-row"><span class="label">Previous</span><span class="value"><a href="/block/{{.Block.PreviousHash}}" class="mono">{{.Block.PreviousHash}}</a></span></div>
  {{if .Block.NextBlockHash}}
  <div class="field-row"><span class="label">Next</span><span class="value"><a href="/block/{{.Block.NextBlockHash}}" class="mono">{{.Block.NextBlockHash}}</a></span></div>
  {{end}}
  <div class="field-row"><span class="label">Merkle Root</span><span class="value mono">{{.Block.MerkleRoot}}</span></div>
  <div class="field-row"><span class="label">Time</span><span class="value">{{formatTime .Block.Time}}</span></div>
  <div class="field-row"><span class="label">Version</span><span class="value">{{.Block.Version}}</span></div>
  <div class="field-row"><span class="label">Bits</span><span class="value mono">{{.Block.Bits}}</span></div>
  <div class="field-row"><span class="label">Nonce</span><span class="value">{{.Block.Nonce}}</span></div>
  <div class="field-row"><span class="label">Size</span><span class="value">{{.Block.Size}} bytes</span></div>
  <div class="field-row"><span class="label">Confirmations</span><span class="value">{{.Block.Confirmations}}</span></div>
</div>

<div class="card">
  <h2>Transactions ({{.Block.TxCount}})</h2>
  <table>
    <tr><th>#</th><th>TxID</th></tr>
    {{range $i, $txid := .Block.Tx}}
    <tr>
      <td>{{$i}}</td>
      <td><a href="/tx/{{$txid}}" class="mono">{{$txid}}</a></td>
    </tr>
    {{end}}
  </table>
</div>
{{end}}
```

**Step 2: Transaction detail template**

`den/templates/tx.html`:
```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a>{{if .Tx.BlockHash}} / <a href="/block/{{.Tx.BlockHash}}">Block</a>{{end}} / Tx</div>
<div class="card">
  <h2>Transaction {{if .Metanet}}{{if .Metanet.IsMetanet}}<span class="tag tag-metanet">METANET</span>{{end}}{{end}}</h2>
  <div class="field-row"><span class="label">TxID</span><span class="value mono">{{.Tx.TxID}}</span></div>
  <div class="field-row"><span class="label">Size</span><span class="value">{{.Tx.Size}} bytes</span></div>
  <div class="field-row"><span class="label">Version</span><span class="value">{{.Tx.Version}}</span></div>
  <div class="field-row"><span class="label">LockTime</span><span class="value">{{.Tx.LockTime}}</span></div>
  {{if .Tx.BlockHash}}
  <div class="field-row"><span class="label">Block</span><span class="value"><a href="/block/{{.Tx.BlockHash}}" class="mono">{{truncHash .Tx.BlockHash}}</a></span></div>
  <div class="field-row"><span class="label">Confirmations</span><span class="value">{{.Tx.Confirmations}}</span></div>
  {{else}}
  <div class="field-row"><span class="label">Status</span><span class="value" style="color:#e94560">Unconfirmed</span></div>
  {{end}}
  {{if .Metanet}}{{if .Metanet.IsMetanet}}
  <div class="field-row"><span class="label">Metanet</span><span class="value"><a href="/metanet/{{.Tx.TxID}}">View Metanet Details</a> | <a href="/spv/{{.Tx.TxID}}">SPV Proof</a></span></div>
  {{end}}{{end}}
</div>

<div class="card">
  <h2>Inputs ({{len .Tx.Vin}})</h2>
  <table>
    <tr><th>#</th><th>Source</th><th>Script</th></tr>
    {{range $i, $in := .Tx.Vin}}
    <tr>
      <td>{{$i}}</td>
      <td>{{if $in.Coinbase}}Coinbase{{else}}<a href="/tx/{{$in.TxID}}" class="mono hash">{{$in.TxID}}</a>:{{$in.Vout}}{{end}}</td>
      <td class="mono" style="font-size:11px;max-width:400px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{if $in.Coinbase}}{{$in.Coinbase}}{{else}}{{$in.ScriptSig.ASM}}{{end}}</td>
    </tr>
    {{end}}
  </table>
</div>

<div class="card">
  <h2>Outputs ({{len .Tx.Vout}})</h2>
  <table>
    <tr><th>#</th><th>Value (BSV)</th><th>Type</th><th>Address</th><th>Script</th></tr>
    {{range .Tx.Vout}}
    <tr>
      <td>{{.N}}</td>
      <td>{{printf "%.8f" .Value}}</td>
      <td>{{.ScriptPubKey.Type}}</td>
      <td>{{range .ScriptPubKey.Addresses}}<a href="/address/{{.}}">{{.}}</a> {{end}}</td>
      <td class="mono" style="font-size:11px;max-width:300px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{.ScriptPubKey.ASM}}</td>
    </tr>
    {{end}}
  </table>
</div>
{{end}}
```

**Step 3: Address template**

`den/templates/address.html`:
```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / Address</div>
<div class="card">
  <h2>Address</h2>
  <div class="field-row"><span class="label">Address</span><span class="value mono">{{.Address}}</span></div>
  <div class="field-row"><span class="label">UTXOs</span><span class="value">{{len .UTXOs}}</span></div>
</div>

<div class="card">
  <h2>Unspent Outputs</h2>
  {{if .UTXOs}}
  <table>
    <tr><th>TxID</th><th>Vout</th><th>Amount (sat)</th><th>Confirmations</th></tr>
    {{range .UTXOs}}
    <tr>
      <td><a href="/tx/{{.TxID}}" class="mono hash">{{.TxID}}</a></td>
      <td>{{.Vout}}</td>
      <td>{{.Amount}}</td>
      <td>{{.Confirmations}}</td>
    </tr>
    {{end}}
  </table>
  {{else}}
  <p style="color:#999;padding:12px">No unspent outputs found.</p>
  {{end}}
</div>
{{end}}
```

**Step 4: Search results template**

`den/templates/search.html`:
```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / Search</div>
<div class="card">
  <h2>Search Results</h2>
  {{if .Error}}
  <div class="error">Nothing found for "{{.Query}}": {{.Error}}</div>
  {{end}}
</div>
{{end}}
```

**Step 5: Update templates.go to load all page templates**

Update the `LoadTemplates` function to handle multiple page templates properly. Each page template needs to be parsed together with base.html. Use a pattern where we create a clone of the base template for each page.

Update `den/templates.go` — replace the `LoadTemplates` function and `Render` method:
```go
// LoadTemplates parses all embedded templates.
// Each page template is combined with the base layout.
func LoadTemplates() (*Templates, error) {
	// Parse base first
	base, err := template.New("base.html").Funcs(tmplFuncs).ParseFS(templateFS, "templates/base.html")
	if err != nil {
		return nil, fmt.Errorf("parse base: %w", err)
	}

	pages := []string{"home.html", "block.html", "tx.html", "address.html", "search.html"}
	t := &Templates{pages: make(map[string]*template.Template)}

	for _, page := range pages {
		clone, err := base.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone base for %s: %w", page, err)
		}
		_, err = clone.ParseFS(templateFS, "templates/"+page)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", page, err)
		}
		t.pages[page] = clone
	}
	return t, nil
}

// Templates holds all parsed templates.
type Templates struct {
	pages map[string]*template.Template
}

// Render renders a page template into the writer.
func (t *Templates) Render(w io.Writer, page string, data interface{}) error {
	tmpl, ok := t.pages[page]
	if !ok {
		return fmt.Errorf("template not found: %s", page)
	}
	return tmpl.ExecuteTemplate(w, "base.html", data)
}
```

**Step 6: Update handlers to use correct page names**

Update `den/handlers.go` render calls:
- `s.render(w, "home.html", data)` in handleHome
- `s.render(w, "block.html", data)` in handleBlock
- `s.render(w, "tx.html", data)` in handleTx
- `s.render(w, "address.html", data)` in handleAddress
- `s.render(w, "search.html", data)` in handleSearch

**Step 7: Verify compilation**

```bash
cd den && go build .
```

**Step 8: Commit**

```bash
git add den/templates/ den/templates.go den/handlers.go
git commit -m "feat(den): add block, tx, address, search page templates"
```

---

### Task 7: Metanet Detail Page + DAG Tree View

**Files:**
- Create: `den/templates/metanet.html`
- Modify: `den/handlers.go` (add DAG children endpoint)

**Step 1: Create metanet detail template**

`den/templates/metanet.html`:
```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / <a href="/tx/{{.TxID}}">Tx</a> / Metanet</div>

<div class="card">
  <h2>Metanet Node
    {{if .Metanet.IsRoot}}<span class="tag tag-root">ROOT</span>{{end}}
    {{if .Metanet.Node}}
      {{if .Metanet.Node.IsDir}}<span class="tag tag-dir">DIR</span>{{end}}
      {{if .Metanet.Node.IsFile}}<span class="tag tag-file">FILE</span>{{end}}
      {{if .Metanet.Node.IsLink}}<span class="tag tag-link">LINK</span>{{end}}
    {{end}}
  </h2>
  <div class="field-row"><span class="label">TxID</span><span class="value mono">{{.TxID}}</span></div>
  <div class="field-row"><span class="label">P_node</span><span class="value mono">{{.Metanet.PNode}}</span></div>
  {{if not .Metanet.IsRoot}}
  <div class="field-row"><span class="label">Parent TxID</span><span class="value mono"><a href="/metanet/{{.Metanet.ParentTxID}}">{{.Metanet.ParentTxID}}</a></span></div>
  {{end}}
  {{if .Metanet.Error}}
  <div class="field-row"><span class="label">Parse Error</span><span class="value error">{{.Metanet.Error}}</span></div>
  {{end}}
</div>

{{if .Metanet.Node}}
<div class="card">
  <h2>Node Properties</h2>
  {{with .Metanet.Node}}
  <div class="field-row"><span class="label">Version</span><span class="value">{{.Version}}</span></div>
  <div class="field-row"><span class="label">Type</span><span class="value">{{.Type}}</span></div>
  <div class="field-row"><span class="label">Operation</span><span class="value">{{.Op}}</span></div>
  <div class="field-row"><span class="label">Access</span><span class="value">
    {{if eq .Access.String "PRIVATE"}}<span class="tag tag-private">PRIVATE</span>
    {{else if eq .Access.String "FREE"}}<span class="tag tag-free">FREE</span>
    {{else}}<span class="tag tag-paid">PAID</span>{{end}}
  </span></div>
  {{if .MimeType}}<div class="field-row"><span class="label">MIME Type</span><span class="value">{{.MimeType}}</span></div>{{end}}
  {{if .FileSize}}<div class="field-row"><span class="label">File Size</span><span class="value">{{.FileSize}} bytes</span></div>{{end}}
  {{if .PricePerKB}}<div class="field-row"><span class="label">Price/KB</span><span class="value">{{.PricePerKB}} sat</span></div>{{end}}
  {{if .Domain}}<div class="field-row"><span class="label">Domain</span><span class="value">{{.Domain}}</span></div>{{end}}
  {{if .Encrypted}}<div class="field-row"><span class="label">Encrypted</span><span class="value">Yes</span></div>{{end}}
  {{end}}
</div>

{{if .Metanet.Node.Children}}
<div class="card">
  <h2>Children ({{len .Metanet.Node.Children}})</h2>
  <table>
    <tr><th>Index</th><th>Name</th><th>Type</th><th>P_node (pubkey)</th><th>Hardened</th></tr>
    {{range .Metanet.Node.Children}}
    <tr>
      <td>{{.Index}}</td>
      <td>{{.Name}}</td>
      <td>
        {{if eq .Type.String "DIR"}}<span class="tag tag-dir">DIR</span>
        {{else if eq .Type.String "FILE"}}<span class="tag tag-file">FILE</span>
        {{else}}<span class="tag tag-link">LINK</span>{{end}}
      </td>
      <td class="mono" style="font-size:11px">{{printf "%x" .PubKey}}</td>
      <td>{{if .Hardened}}Yes{{else}}No{{end}}</td>
    </tr>
    {{end}}
  </table>
</div>
{{end}}
{{end}}

{{if .TLVFields}}
<div class="card">
  <h2>TLV Payload Fields</h2>
  <table>
    <tr><th>Tag</th><th>Name</th><th>Length</th><th>Value (hex)</th><th>Decoded</th></tr>
    {{range .TLVFields}}
    <tr>
      <td class="mono">0x{{printf "%02X" .Tag}}</td>
      <td>{{.TagName}}</td>
      <td>{{.Length}}</td>
      <td class="mono" style="font-size:11px;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{.ValueHex}}</td>
      <td>{{.ValueStr}}</td>
    </tr>
    {{end}}
  </table>
</div>
{{end}}
{{end}}
```

**Step 2: Add metanet page to templates.go pages list**

Add `"metanet.html"` to the `pages` slice in `LoadTemplates`.

**Step 3: Update handleMetanet to render metanet.html**

Ensure `s.render(w, "metanet.html", data)` is used.

**Step 4: Verify and commit**

```bash
cd den && go build .
git add den/templates/metanet.html den/templates.go den/handlers.go
git commit -m "feat(den): add Metanet detail page with DAG tree and TLV viewer"
```

---

### Task 8: SPV Proof Verification Page

**Files:**
- Create: `den/templates/spv.html`
- Create: `den/verify.go`

**Step 1: Write SPV verification logic**

`den/verify.go`:
```go
package main

import (
	"encoding/hex"
	"fmt"

	"github.com/tongxiaofeng/libbitfs/metanet"
	"github.com/tongxiaofeng/libbitfs/network"
	"github.com/tongxiaofeng/libbitfs/spv"
)

// SPVVerification holds the result of SPV proof verification.
type SPVVerification struct {
	TxID           string
	BlockHash      string
	Index          int
	Branches       []SPVBranch
	ComputedRoot   string
	ExpectedRoot   string
	Valid          bool
	Error          string
}

// SPVBranch holds a single branch in the Merkle proof path for display.
type SPVBranch struct {
	Level    int
	Hash     string
	Side     string // "left" or "right"
}

// VerifySPVProof verifies a Merkle inclusion proof and returns display data.
func VerifySPVProof(proof *network.MerkleProof, headerBytes []byte) *SPVVerification {
	result := &SPVVerification{
		TxID:      proof.TxID,
		BlockHash: proof.BlockHash,
		Index:     proof.Index,
	}

	// Build branch display
	index := uint32(proof.Index)
	for i, branch := range proof.Branches {
		side := "right"
		if index%2 == 1 {
			side = "left"
		}
		result.Branches = append(result.Branches, SPVBranch{
			Level: i,
			Hash:  hex.EncodeToString(branch),
			Side:  side,
		})
		index /= 2
	}

	// Compute Merkle root from proof
	txidBytes, err := hex.DecodeString(proof.TxID)
	if err != nil {
		result.Error = fmt.Sprintf("invalid txid: %v", err)
		return result
	}
	// Reverse for internal byte order
	txidInternal := make([]byte, 32)
	for i, b := range txidBytes {
		txidInternal[31-i] = b
	}

	computedRoot := spv.ComputeMerkleRoot(txidInternal, uint32(proof.Index), proof.Branches)
	result.ComputedRoot = hex.EncodeToString(computedRoot)

	// Extract expected root from header
	if len(headerBytes) >= 80 {
		header, err := spv.DeserializeHeader(headerBytes)
		if err == nil {
			result.ExpectedRoot = hex.EncodeToString(header.MerkleRoot)
			// Compare in internal byte order
			if result.ComputedRoot == result.ExpectedRoot {
				result.Valid = true
			}
		}
	}

	return result
}

// DirMerkleVerification holds directory Merkle root verification results.
type DirMerkleVerification struct {
	Children       []metanet.ChildEntry
	ComputedRoot   string
	StoredRoot     string
	Valid          bool
}

// VerifyDirMerkleRoot verifies a directory's MerkleRoot against its children.
func VerifyDirMerkleRoot(node *metanet.Node) *DirMerkleVerification {
	if node == nil || !node.IsDir() {
		return nil
	}

	result := &DirMerkleVerification{
		Children: node.Children,
	}

	if len(node.MerkleRoot) > 0 {
		result.StoredRoot = hex.EncodeToString(node.MerkleRoot)
	}

	computed := metanet.ComputeDirectoryMerkleRoot(node.Children)
	if computed != nil {
		result.ComputedRoot = hex.EncodeToString(computed)
	}

	if result.StoredRoot != "" && result.ComputedRoot != "" {
		result.Valid = result.StoredRoot == result.ComputedRoot
	}

	return result
}
```

**Step 2: Create SPV template**

`den/templates/spv.html`:
```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / <a href="/tx/{{.TxID}}">Tx</a> / SPV Proof</div>

<div class="card">
  <h2>SPV Merkle Proof</h2>
  <div class="field-row"><span class="label">TxID</span><span class="value mono">{{.TxID}}</span></div>
  {{if .Verification}}
  <div class="field-row"><span class="label">Block</span><span class="value"><a href="/block/{{.Verification.BlockHash}}" class="mono">{{truncHash .Verification.BlockHash}}</a></span></div>
  <div class="field-row"><span class="label">Tx Index</span><span class="value">{{.Verification.Index}}</span></div>
  <div class="field-row"><span class="label">Computed Root</span><span class="value mono" style="font-size:11px">{{.Verification.ComputedRoot}}</span></div>
  <div class="field-row"><span class="label">Expected Root</span><span class="value mono" style="font-size:11px">{{.Verification.ExpectedRoot}}</span></div>
  <div class="field-row"><span class="label">Verified</span><span class="value">
    {{if .Verification.Valid}}<span style="color:#4caf50;font-weight:bold">VALID</span>
    {{else}}<span style="color:#e94560;font-weight:bold">INVALID</span>{{end}}
  </span></div>
  {{if .Verification.Error}}
  <div class="field-row"><span class="label">Error</span><span class="value error">{{.Verification.Error}}</span></div>
  {{end}}
  {{end}}
</div>

{{if .Verification.Branches}}
<div class="card">
  <h2>Merkle Path ({{len .Verification.Branches}} levels)</h2>
  <table>
    <tr><th>Level</th><th>Side</th><th>Hash</th></tr>
    {{range .Verification.Branches}}
    <tr>
      <td>{{.Level}}</td>
      <td>{{.Side}}</td>
      <td class="mono" style="font-size:11px">{{.Hash}}</td>
    </tr>
    {{end}}
  </table>
</div>
{{end}}
{{end}}
```

**Step 3: Update handleSPV to use verification**

Update `den/handlers.go` handleSPV:
```go
func (s *Server) handleSPV(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	proof, err := s.explorer.rpc.GetMerkleProof(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("proof not available: %v", err), 404)
		return
	}

	headerBytes, _ := s.explorer.rpc.GetBlockHeader(ctx, proof.BlockHash)
	verification := VerifySPVProof(proof, headerBytes)

	data := map[string]interface{}{
		"Title":        fmt.Sprintf("SPV Proof %s", truncHash(txid)),
		"TxID":         txid,
		"Verification": verification,
	}
	s.render(w, "spv.html", data)
}
```

**Step 4: Add spv.html to pages list, verify and commit**

```bash
cd den && go build .
git add den/verify.go den/templates/spv.html den/handlers.go den/templates.go
git commit -m "feat(den): add SPV proof verification page with Merkle path display"
```

---

### Task 9: Method 42 Encryption Analysis Page

**Files:**
- Create: `den/templates/method42.html`
- Create: `den/method42.go`
- Modify: `den/handlers.go` (add method42 handler)

**Step 1: Write Method 42 analysis logic**

`den/method42.go`:
```go
package main

import (
	"encoding/hex"
	"fmt"

	"github.com/tongxiaofeng/libbitfs/metanet"
)

// Method42Analysis holds the encryption analysis for a Metanet node.
type Method42Analysis struct {
	TxID        string
	PNode       string
	Access      string
	Encrypted   bool
	KeyHash     string // hex
	KeyHashLen  int
	AccessMode  string // description of how ECDH works for this mode
	CanDecrypt  string // explanation of what's needed to decrypt
}

// AnalyzeMethod42 examines a Metanet node's encryption properties.
func AnalyzeMethod42(txid string, node *metanet.Node, pNodeHex string) *Method42Analysis {
	a := &Method42Analysis{
		TxID:  txid,
		PNode: pNodeHex,
	}

	if node == nil {
		return a
	}

	a.Encrypted = node.Encrypted
	a.Access = node.Access.String()

	if len(node.KeyHash) > 0 {
		a.KeyHash = hex.EncodeToString(node.KeyHash)
		a.KeyHashLen = len(node.KeyHash)
	}

	// Describe ECDH derivation based on access level
	switch node.Access {
	case metanet.AccessPrivate:
		a.AccessMode = "PRIVATE: aes_key = HKDF-SHA256(ECDH(D_node, P_node).x, key_hash)"
		a.CanDecrypt = fmt.Sprintf("Requires D_node (private key for P_node=%s). Only the node owner can decrypt.", truncHash(pNodeHex))
	case metanet.AccessFree:
		a.AccessMode = "FREE: aes_key = HKDF-SHA256(ECDH(1, P_node).x, key_hash) — D_node=1 (trivial key)"
		a.CanDecrypt = "Anyone can decrypt — the private key is the scalar 1 (trivial key trick)."
	case metanet.AccessPaid:
		a.AccessMode = "PAID: aes_key derived from HTLC capsule after atomic swap payment"
		a.CanDecrypt = "Requires completing HTLC atomic swap payment to obtain the ECDH capsule."
	}

	return a
}
```

**Step 2: Create template**

`den/templates/method42.html`:
```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / <a href="/tx/{{.TxID}}">Tx</a> / <a href="/metanet/{{.TxID}}">Metanet</a> / Method 42</div>

<div class="card">
  <h2>Method 42 Encryption Analysis</h2>
  <div class="field-row"><span class="label">TxID</span><span class="value mono">{{.Analysis.TxID}}</span></div>
  <div class="field-row"><span class="label">P_node</span><span class="value mono">{{.Analysis.PNode}}</span></div>
  <div class="field-row"><span class="label">Encrypted</span><span class="value">{{if .Analysis.Encrypted}}Yes{{else}}No{{end}}</span></div>
  <div class="field-row"><span class="label">Access Level</span><span class="value">
    {{if eq .Analysis.Access "PRIVATE"}}<span class="tag tag-private">PRIVATE</span>
    {{else if eq .Analysis.Access "FREE"}}<span class="tag tag-free">FREE</span>
    {{else if eq .Analysis.Access "PAID"}}<span class="tag tag-paid">PAID</span>
    {{else}}{{.Analysis.Access}}{{end}}
  </span></div>
</div>

<div class="card">
  <h2>ECDH Key Derivation</h2>
  <div class="field-row"><span class="label">Mode</span><span class="value" style="font-size:12px">{{.Analysis.AccessMode}}</span></div>
  <div class="field-row"><span class="label">Decryption</span><span class="value" style="font-size:12px">{{.Analysis.CanDecrypt}}</span></div>
  {{if .Analysis.KeyHash}}
  <div class="field-row"><span class="label">KeyHash</span><span class="value mono" style="font-size:11px">{{.Analysis.KeyHash}}</span></div>
  <div class="field-row"><span class="label">KeyHash Length</span><span class="value">{{.Analysis.KeyHashLen}} bytes</span></div>
  <div class="field-row"><span class="label">Formula</span><span class="value" style="font-size:12px">KeyHash = SHA256(SHA256(plaintext))</span></div>
  {{else}}
  <div class="field-row"><span class="label">KeyHash</span><span class="value" style="color:#999">Not present (content may not be encrypted)</span></div>
  {{end}}
</div>

<div class="card">
  <h2>Key Derivation Chain</h2>
  <pre style="color:#a5d6a7;font-size:12px;line-height:1.8;padding:8px 0">
1. P_node        = {{.Analysis.PNode}}
{{if eq .Analysis.Access "FREE"}}2. D_node        = 1 (trivial private key — public knowledge)
3. shared_secret = ECDH(1, P_node).x
{{else if eq .Analysis.Access "PRIVATE"}}2. D_node        = ??? (owner's private key — secret)
3. shared_secret = ECDH(D_node, P_node).x
{{else}}2. capsule       = ECDH(D_node, P_buyer) — revealed via HTLC
3. shared_secret = capsule
{{end}}4. key_hash      = {{if .Analysis.KeyHash}}{{.Analysis.KeyHash}}{{else}}(not available){{end}}
5. aes_key       = HKDF-SHA256(shared_secret, key_hash, "bitfs-file-encryption")
6. plaintext     = AES-256-GCM-Decrypt(ciphertext, aes_key)
  </pre>
</div>
{{end}}
```

**Step 3: Add handler**

In `den/handlers.go`, the `handleMethod42` function (add to Routes as well):
```go
// In Routes():
mux.HandleFunc("GET /method42/{txid}", s.handleMethod42)

func (s *Server) handleMethod42(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	rawBytes, err := s.explorer.rpc.GetRawTx(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("tx not found: %v", err), 404)
		return
	}

	decoded := DecodeMetanetTx(rawBytes)
	if !decoded.IsMetanet {
		http.Error(w, "not a Metanet transaction", 400)
		return
	}

	analysis := AnalyzeMethod42(txid, decoded.Node, decoded.PNode)

	data := map[string]interface{}{
		"Title":    fmt.Sprintf("Method 42 %s", truncHash(txid)),
		"TxID":     txid,
		"Analysis": analysis,
	}
	s.render(w, "method42.html", data)
}
```

**Step 4: Add method42.html to pages, verify and commit**

```bash
cd den && go build .
git add den/method42.go den/templates/method42.html den/handlers.go den/templates.go
git commit -m "feat(den): add Method 42 encryption analysis page"
```

---

### Task 10: Integration Test — Full Smoke Test with Docker Regtest

Create an integration test that starts the server, connects to the docker regtest node, and exercises all pages.

**Files:**
- Create: `den/integration_test.go`

**Step 1: Write integration test**

`den/integration_test.go`:
```go
package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/tongxiaofeng/libbitfs/network"
)

func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	url := os.Getenv("DEN_RPC_URL")
	if url == "" {
		url = "http://localhost:18332"
	}
	rpc := network.NewRPCClient(network.RPCConfig{
		URL: url, User: "bitfs", Password: "bitfs",
	})

	// Health check
	ctx := context.Background()
	if _, err := rpc.GetBestBlockHeight(ctx); err != nil {
		t.Skipf("RPC not available: %v", err)
	}

	explorer := NewExplorer(rpc)
	templates, err := LoadTemplates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}

	srv := NewServer(explorer, templates)
	return httptest.NewServer(srv.Routes())
}

func TestHomePage(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "Den") {
		t.Error("missing title")
	}
	if !strings.Contains(html, "regtest") {
		t.Error("missing network info")
	}
}

func TestBlockPage(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Get genesis block via height redirect
	resp, err := http.Get(ts.URL + "/block/height/0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Block #0") {
		t.Error("missing block height")
	}
}

func TestSearch_Height(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Search for block 0
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse // don't follow redirects
	}}

	resp, err := client.Get(ts.URL + "/search?q=0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 302 {
		t.Fatalf("expected redirect, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/block/") {
		t.Errorf("expected /block/ redirect, got %s", loc)
	}
}

func TestSearch_NotFound(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/search?q=nonexistent_garbage_query_12345")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") && !strings.Contains(string(body), "Nothing found") {
		t.Error("expected not found message")
	}
}
```

**Step 2: Run with docker regtest**

```bash
cd bitfs/e2e && docker compose up -d
cd ../../den && go test -v -count=1 -timeout 30s
```

**Step 3: Commit**

```bash
git add den/integration_test.go
git commit -m "test(den): add integration smoke tests for all pages"
```

---

### Task 11: Polish & README

**Files:**
- Create: `den/README.md`
- Modify: `den/main.go` (add startup banner)

**Step 1: Add startup banner with useful info**

Update main.go to print:
```
Den v0.1.0 — BitFS Blockchain Explorer
Network:  regtest
RPC:      http://localhost:18332
Listen:   http://localhost:8080
```

**Step 2: Create README**

`den/README.md`:
```markdown
# Den — BitFS Blockchain Explorer

A web-based BSV blockchain explorer with BitFS/Metanet protocol verification.
Built for development and debugging on regtest and testnet.

## Quick Start

```bash
# Start regtest node (requires Docker)
cd ../bitfs/e2e && docker compose up -d

# Build and run
cd ../den && go build . && ./den

# Open http://localhost:8080
```

## Features

**Standard Explorer:**
- Block and transaction browsing
- Address UTXO lookup
- Search by txid, block hash, height, or address

**BitFS Protocol Verification:**
- Metanet OP_RETURN decoding (MetaFlag, P_node, TLV payload)
- Metanet DAG tree visualization (node types, access levels, children)
- Method 42 encryption analysis (ECDH key derivation chain)
- SPV Merkle proof verification (transaction inclusion + directory MerkleRoot)

## Configuration

```
den [flags]
  -rpc-url     bitcoind RPC URL      (default from network preset)
  -rpc-user    RPC username           (default "bitfs")
  -rpc-pass    RPC password           (default "bitfs")
  -addr        HTTP listen address    (default ":8080")
  -network     regtest|testnet        (default "regtest")
```

Environment variables: `BITFS_RPC_URL`, `BITFS_RPC_USER`, `BITFS_RPC_PASS`

## Tech Stack

Go + htmx + libbitfs. Single binary, no build tools, no JavaScript framework.
```

**Step 3: Commit**

```bash
git add den/README.md den/main.go
git commit -m "docs(den): add README and startup banner"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Go module & skeleton | go.mod, main.go |
| 2 | RPC service layer | rpc.go, rpc_test.go |
| 3 | Metanet decoder | decode.go, decode_test.go |
| 4 | Templates: base + home | templates/, templates.go |
| 5 | HTTP handlers | handlers.go, main.go |
| 6 | Templates: block, tx, addr | 4 template files |
| 7 | Metanet detail + DAG | metanet.html |
| 8 | SPV proof verification | verify.go, spv.html |
| 9 | Method 42 analysis | method42.go, method42.html |
| 10 | Integration tests | integration_test.go |
| 11 | README + polish | README.md |

**Total: 11 tasks, ~15 files, estimated ~1200 LOC**

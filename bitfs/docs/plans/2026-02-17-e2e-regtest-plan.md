# BitFS E2E Regtest Verification — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace mock-only blockchain tests with end-to-end verification against a real BSV regtest node, covering wallet funding through paid purchase with HTLC.

**Architecture:** Docker runs BSV SV Node in regtest mode. A thin JSON-RPC client (no go-sdk RPC — it doesn't have one) connects to the node. Transaction signing uses go-sdk's `p2pkh.Unlock` + `tx.Sign()`. All e2e tests gated behind `//go:build e2e` build tag.

**Tech Stack:** Go 1.25.6, go-sdk v1.2.18 (signing/serialization), Docker (bitcoinsv/bitcoin-sv:1.0.11), JSON-RPC over HTTP (stdlib net/http)

---

## Task 1: Docker Infrastructure

**Files:**
- Create: `e2e/docker-compose.yml`
- Create: `e2e/bitcoin.conf`

**Step 1: Create bitcoin.conf**

```ini
regtest=1
server=1
rest=1
listen=1
printtoconsole=1
dnsseed=0
upnp=0
listenonion=0

rpcuser=bitfs
rpcpassword=bitfs
rpcbind=0.0.0.0
rpcport=18332
rpcallowip=0.0.0.0/0

port=18444
txindex=1
genesisactivationheight=1

excessiveblocksize=2000000000
maxstackmemoryusageconsensus=100000000

maxmempool=512
minminingtxfee=0.00000050
```

**Step 2: Create docker-compose.yml**

```yaml
services:
  bsv-node:
    image: bitcoinsv/bitcoin-sv:1.0.11
    container_name: bitfs-regtest
    ports:
      - "18332:18332"
      - "18444:18444"
    volumes:
      - ./bitcoin.conf:/data/bitcoin.conf:ro
      - bsv-data:/data
    healthcheck:
      test: ["CMD", "/entrypoint.sh", "bitcoin-cli", "-regtest", "getinfo"]
      interval: 5s
      timeout: 3s
      retries: 10

volumes:
  bsv-data:
```

**Step 3: Verify node starts**

Run:
```bash
cd e2e && docker compose up -d && docker compose exec bsv-node bitcoin-cli -regtest -rpcuser=bitfs -rpcpassword=bitfs getblockchaininfo
```
Expected: JSON response with `"chain": "regtest"`, `"blocks": 0`

**Step 4: Commit**

```bash
git add e2e/docker-compose.yml e2e/bitcoin.conf
git commit -m "feat(e2e): add Docker regtest BSV node infrastructure"
```

---

## Task 2: JSON-RPC Client

go-sdk has no RPC client. Write a thin JSON-RPC wrapper using stdlib.

**Files:**
- Create: `e2e/testutil/rpc.go`
- Create: `e2e/testutil/rpc_test.go`

**Step 1: Write the RPC client test**

```go
//go:build e2e

package testutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRPCClient_GetBlockchainInfo(t *testing.T) {
	client := NewRPCClient("http://localhost:18332", "bitfs", "bitfs")
	ctx := context.Background()

	var result map[string]interface{}
	err := client.Call(ctx, "getblockchaininfo", nil, &result)
	require.NoError(t, err)
	require.Equal(t, "regtest", result["chain"])
}
```

**Step 2: Run test to verify it fails**

Run: `go test -tags e2e ./e2e/testutil/ -run TestRPCClient -v`
Expected: FAIL — `NewRPCClient` undefined

**Step 3: Implement RPC client**

```go
//go:build e2e

package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

type RPCClient struct {
	url      string
	user     string
	password string
	idSeq    atomic.Int64
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewRPCClient(url, user, password string) *RPCClient {
	return &RPCClient{url: url, user: user, password: password}
}

func (c *RPCClient) Call(ctx context.Context, method string, params []interface{}, result interface{}) error {
	if params == nil {
		params = []interface{}{}
	}
	reqBody, err := json.Marshal(rpcRequest{
		JSONRPC: "1.0",
		ID:      c.idSeq.Add(1),
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.user, c.password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("rpc call %s: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if result != nil {
		return json.Unmarshal(rpcResp.Result, result)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -tags e2e ./e2e/testutil/ -run TestRPCClient -v`
Expected: PASS (requires regtest node running)

**Step 5: Commit**

```bash
git add e2e/testutil/rpc.go e2e/testutil/rpc_test.go
git commit -m "feat(e2e): add thin JSON-RPC client for regtest node"
```

---

## Task 3: Regtest Node Helper

High-level helpers wrapping RPC for mining, funding, UTXO queries, tx broadcast, and Merkle proofs.

**Files:**
- Create: `e2e/testutil/node.go`
- Create: `e2e/testutil/node_test.go`

**Step 1: Write test for MineBlocks + GetBalance**

```go
//go:build e2e

package testutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNode_MineAndFund(t *testing.T) {
	node := NewRegtestNode()
	ctx := context.Background()

	// Mine 101 blocks (coinbase maturity)
	addr, err := node.NewAddress(ctx)
	require.NoError(t, err)

	hashes, err := node.MineBlocks(ctx, 101, addr)
	require.NoError(t, err)
	require.Len(t, hashes, 101)

	// Should have spendable UTXOs
	utxos, err := node.ListUnspent(ctx, addr)
	require.NoError(t, err)
	require.NotEmpty(t, utxos)
}

func TestNode_BroadcastAndConfirm(t *testing.T) {
	node := NewRegtestNode()
	ctx := context.Background()

	// Fund a wallet address
	addr, err := node.NewAddress(ctx)
	require.NoError(t, err)
	_, err = node.MineBlocks(ctx, 101, addr)
	require.NoError(t, err)

	// Send coins to another address
	addr2, err := node.NewAddress(ctx)
	require.NoError(t, err)

	txid, err := node.SendToAddress(ctx, addr2, 1.0)
	require.NoError(t, err)
	require.NotEmpty(t, txid)

	// Mine a block to confirm
	_, err = node.MineBlocks(ctx, 1, addr)
	require.NoError(t, err)

	// Get raw tx
	rawTx, err := node.GetRawTransaction(ctx, txid)
	require.NoError(t, err)
	require.NotEmpty(t, rawTx)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -tags e2e ./e2e/testutil/ -run TestNode -v`
Expected: FAIL — `NewRegtestNode` undefined

**Step 3: Implement node helper**

```go
//go:build e2e

package testutil

import (
	"context"
	"encoding/hex"
	"fmt"
)

// RegtestUTXO represents an unspent output from the regtest node.
type RegtestUTXO struct {
	TxID          string  `json:"txid"`
	Vout          uint32  `json:"vout"`
	Address       string  `json:"address"`
	ScriptPubKey  string  `json:"scriptPubKey"`
	Amount        float64 `json:"amount"`
	Confirmations int     `json:"confirmations"`
}

// RegtestNode wraps RPC calls to a local BSV regtest node.
type RegtestNode struct {
	rpc *RPCClient
}

func NewRegtestNode() *RegtestNode {
	return &RegtestNode{
		rpc: NewRPCClient("http://localhost:18332", "bitfs", "bitfs"),
	}
}

// IsAvailable checks if the regtest node is reachable.
func (n *RegtestNode) IsAvailable(ctx context.Context) bool {
	var result map[string]interface{}
	err := n.rpc.Call(ctx, "getblockchaininfo", nil, &result)
	return err == nil
}

// NewAddress generates a new address from the node's built-in wallet.
func (n *RegtestNode) NewAddress(ctx context.Context) (string, error) {
	var addr string
	err := n.rpc.Call(ctx, "getnewaddress", nil, &addr)
	return addr, err
}

// MineBlocks mines n blocks, sending coinbase reward to addr.
func (n *RegtestNode) MineBlocks(ctx context.Context, count int, addr string) ([]string, error) {
	var hashes []string
	err := n.rpc.Call(ctx, "generatetoaddress", []interface{}{count, addr}, &hashes)
	return hashes, err
}

// ListUnspent returns UTXOs for the given address.
func (n *RegtestNode) ListUnspent(ctx context.Context, addr string) ([]RegtestUTXO, error) {
	var utxos []RegtestUTXO
	err := n.rpc.Call(ctx, "listunspent", []interface{}{1, 9999999, []string{addr}}, &utxos)
	return utxos, err
}

// SendRawTransaction broadcasts a signed raw transaction hex.
func (n *RegtestNode) SendRawTransaction(ctx context.Context, rawTxHex string) (string, error) {
	var txid string
	err := n.rpc.Call(ctx, "sendrawtransaction", []interface{}{rawTxHex}, &txid)
	return txid, err
}

// SendToAddress sends BSV from the node wallet to an address (for test setup).
func (n *RegtestNode) SendToAddress(ctx context.Context, addr string, amount float64) (string, error) {
	var txid string
	err := n.rpc.Call(ctx, "sendtoaddress", []interface{}{addr, amount}, &txid)
	return txid, err
}

// GetRawTransaction returns the hex-encoded raw transaction.
func (n *RegtestNode) GetRawTransaction(ctx context.Context, txid string) ([]byte, error) {
	var hexStr string
	err := n.rpc.Call(ctx, "getrawtransaction", []interface{}{txid, 0}, &hexStr)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(hexStr)
}

// GetBlockHeader returns raw block header bytes (80 bytes).
func (n *RegtestNode) GetBlockHeader(ctx context.Context, blockHash string) ([]byte, error) {
	var hexStr string
	err := n.rpc.Call(ctx, "getblockheader", []interface{}{blockHash, false}, &hexStr)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(hexStr)
}

// GetBlockHeaderVerbose returns block header as JSON.
func (n *RegtestNode) GetBlockHeaderVerbose(ctx context.Context, blockHash string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := n.rpc.Call(ctx, "getblockheader", []interface{}{blockHash, true}, &result)
	return result, err
}

// GetMerkleProof returns raw Merkle proof for a confirmed transaction.
func (n *RegtestNode) GetTxOutProof(ctx context.Context, txid string) ([]byte, error) {
	var hexStr string
	err := n.rpc.Call(ctx, "gettxoutproof", []interface{}{[]string{txid}}, &hexStr)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(hexStr)
}

// GetBestBlockHash returns the hash of the current tip.
func (n *RegtestNode) GetBestBlockHash(ctx context.Context) (string, error) {
	var hash string
	err := n.rpc.Call(ctx, "getbestblockhash", nil, &hash)
	return hash, err
}

// GetBlockHash returns block hash at a given height.
func (n *RegtestNode) GetBlockHash(ctx context.Context, height int) (string, error) {
	var hash string
	err := n.rpc.Call(ctx, "getblockhash", []interface{}{height}, &hash)
	return hash, err
}

// ImportAddress adds an address to the node's watch list (for listunspent).
func (n *RegtestNode) ImportAddress(ctx context.Context, addr string) error {
	return n.rpc.Call(ctx, "importaddress", []interface{}{addr, "", false}, nil)
}

// FundAddress mines 101 blocks to the given address, returning the first spendable UTXO.
func (n *RegtestNode) FundAddress(ctx context.Context, addr string) (*RegtestUTXO, error) {
	_, err := n.MineBlocks(ctx, 101, addr)
	if err != nil {
		return nil, fmt.Errorf("mine blocks: %w", err)
	}
	utxos, err := n.ListUnspent(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("list unspent: %w", err)
	}
	if len(utxos) == 0 {
		return nil, fmt.Errorf("no UTXOs after mining")
	}
	return &utxos[0], nil
}

// SkipIfUnavailable skips the test if regtest node is not reachable.
func SkipIfUnavailable(t interface{ Skip(...interface{}) }, node *RegtestNode) {
	if !node.IsAvailable(context.Background()) {
		t.(interface{ Skip(...interface{}) }).Skip("regtest node not available — run: cd e2e && docker compose up -d")
	}
}
```

**Step 4: Run tests**

Run: `go test -tags e2e ./e2e/testutil/ -run TestNode -v`
Expected: PASS

**Step 5: Commit**

```bash
git add e2e/testutil/node.go e2e/testutil/node_test.go
git commit -m "feat(e2e): add regtest node helper with mine/fund/broadcast/query"
```

---

## Task 4: Transaction Signing in libbitfs/tx

The current `BuildCreateRoot` etc. build transaction structure but don't sign. Add signing using go-sdk's `p2pkh.Unlock` + `tx.Sign()`.

**Files:**
- Create: `../libbitfs/tx/sign.go`
- Create: `../libbitfs/tx/sign_test.go`

**Step 1: Write the failing test**

```go
package tx

import (
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/stretchr/testify/require"
)

func TestSignMetanetTx(t *testing.T) {
	// Generate a key pair
	priv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	addr, err := script.NewAddressFromPublicKey(priv.PubKey(), false)
	require.NoError(t, err)

	// Create a fake UTXO with a real P2PKH locking script
	lockScript, err := buildP2PKHScript(addr.PublicKeyHash)
	require.NoError(t, err)

	utxo := &UTXO{
		TxID:         make([]byte, 32),
		Vout:         0,
		Amount:       50000,
		ScriptPubKey: lockScript,
		PrivateKey:   priv,
	}

	// Build a root transaction
	result, err := BuildCreateRoot(&CreateRootParams{
		NodePubKey:  priv.PubKey(),
		NodePrivKey: priv,
		Payload:     []byte("test payload"),
		FeeUTXO:     utxo,
		ChangeAddr:  addr.PublicKeyHash,
		FeeRate:     500,
	})
	require.NoError(t, err)

	// Sign the transaction
	signedHex, err := SignMetanetTx(result, []*UTXO{utxo})
	require.NoError(t, err)
	require.NotEmpty(t, signedHex)

	// Verify it's valid hex and has non-empty unlocking scripts
	require.True(t, len(signedHex) > len(result.RawTx)*2, "signed tx should be larger than unsigned")
}
```

**Step 2: Run test to verify it fails**

Run: `cd ../libbitfs && go test ./tx/ -run TestSignMetanetTx -v`
Expected: FAIL — `SignMetanetTx` undefined

**Step 3: Implement signing**

```go
package tx

import (
	"encoding/hex"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"
)

// SignMetanetTx takes a MetanetTx (built by Build* functions) and signs all
// inputs using the private keys from the provided UTXOs. Returns the signed
// transaction as a hex string ready for broadcast.
func SignMetanetTx(mtx *MetanetTx, utxos []*UTXO) (string, error) {
	tx, err := transaction.NewTransactionFromBytes(mtx.RawTx)
	if err != nil {
		return "", fmt.Errorf("parse raw tx: %w", err)
	}

	if len(tx.Inputs) != len(utxos) {
		return "", fmt.Errorf("input count mismatch: tx has %d inputs, got %d UTXOs", len(tx.Inputs), len(utxos))
	}

	for i, utxo := range utxos {
		if utxo.PrivateKey == nil {
			return "", fmt.Errorf("input %d: missing private key", i)
		}

		unlocker, err := p2pkh.Unlock(utxo.PrivateKey, nil)
		if err != nil {
			return "", fmt.Errorf("input %d: create unlocker: %w", i, err)
		}

		tx.Inputs[i].UnlockingScriptTemplate = unlocker
		tx.Inputs[i].SourceTxScript = func() []byte {
			s, _ := hex.DecodeString(hex.EncodeToString(utxo.ScriptPubKey))
			return s
		}()
		tx.Inputs[i].SourceTxSatoshis = utxo.Amount
	}

	if err := tx.Sign(); err != nil {
		return "", fmt.Errorf("sign transaction: %w", err)
	}

	return tx.Hex(), nil
}

// buildP2PKHScript creates a standard P2PKH locking script from a 20-byte pubkey hash.
func buildP2PKHScript(pubKeyHash []byte) ([]byte, error) {
	if len(pubKeyHash) != 20 {
		return nil, fmt.Errorf("pubkey hash must be 20 bytes, got %d", len(pubKeyHash))
	}
	// OP_DUP OP_HASH160 <20 bytes> OP_EQUALVERIFY OP_CHECKSIG
	s := make([]byte, 0, 25)
	s = append(s, 0x76, 0xa9, 0x14)
	s = append(s, pubKeyHash...)
	s = append(s, 0x88, 0xac)
	return s, nil
}
```

Note: The `buildP2PKHScript` helper may already exist in the codebase. Check and reuse if so. If the go-sdk's `transaction` package doesn't expose `SourceTxScript` and `SourceTxSatoshis` directly on inputs, adapt the approach — inspect the actual go-sdk `TransactionInput` struct fields. The key pattern is: parse the unsigned raw tx, attach unlockers, then call `tx.Sign()`.

**Step 4: Run test**

Run: `cd ../libbitfs && go test ./tx/ -run TestSignMetanetTx -v`
Expected: PASS

**Step 5: Run all existing tx tests to confirm no regression**

Run: `cd ../libbitfs && go test ./tx/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
cd ../libbitfs && git add tx/sign.go tx/sign_test.go
git commit -m "feat(tx): add SignMetanetTx for P2PKH signing via go-sdk"
```

---

## Task 5: E2E Test — 01_wallet_fund

Verify HD wallet key derivation produces real addresses that can receive and spend regtest coins.

**Files:**
- Create: `e2e/01_wallet_fund_test.go`

**Step 1: Write the test**

```go
//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/bitfs/e2e/testutil"
	"github.com/tongxiaofeng/libbitfs/wallet"
)

func TestWalletFund(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)
	ctx := context.Background()

	// Create a real HD wallet
	mnemonic, err := wallet.GenerateMnemonic(12)
	require.NoError(t, err)

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err)

	// Derive a fee key
	feeKey, err := w.DeriveFeeKey(0, 0)
	require.NoError(t, err)

	// Convert to BSV address
	addr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err)
	t.Logf("Fee address: %s", addr.AddressString)

	// Import address into node wallet (so listunspent works)
	err = node.ImportAddress(ctx, addr.AddressString)
	require.NoError(t, err)

	// Mine blocks to the node's own address, then send coins to our address
	nodeAddr, err := node.NewAddress(ctx)
	require.NoError(t, err)
	_, err = node.MineBlocks(ctx, 101, nodeAddr)
	require.NoError(t, err)

	txid, err := node.SendToAddress(ctx, addr.AddressString, 0.01)
	require.NoError(t, err)
	t.Logf("Funding txid: %s", txid)

	// Mine to confirm
	_, err = node.MineBlocks(ctx, 1, nodeAddr)
	require.NoError(t, err)

	// Verify UTXO exists
	utxos, err := node.ListUnspent(ctx, addr.AddressString)
	require.NoError(t, err)
	require.NotEmpty(t, utxos, "should have at least one UTXO")
	require.InDelta(t, 0.01, utxos[0].Amount, 0.0001)

	t.Logf("UTXO: %s:%d = %.8f BSV", utxos[0].TxID, utxos[0].Vout, utxos[0].Amount)
}
```

**Step 2: Run test**

Run: `go test -tags e2e ./e2e/ -run TestWalletFund -v`
Expected: PASS

**Step 3: Commit**

```bash
git add e2e/01_wallet_fund_test.go
git commit -m "test(e2e): add wallet funding verification against regtest"
```

---

## Task 6: E2E Test — 02_metanet_root

Build a Metanet root directory transaction, sign it, broadcast to regtest, confirm, and parse back.

**Files:**
- Create: `e2e/02_metanet_root_test.go`

**Step 1: Write the test**

```go
//go:build e2e

package e2e

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/bitfs/e2e/testutil"
	"github.com/tongxiaofeng/libbitfs/tx"
	"github.com/tongxiaofeng/libbitfs/wallet"
)

func TestMetanetRootBroadcast(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)
	ctx := context.Background()

	// Setup: create wallet, fund it
	w := setupFundedWallet(t, ctx, node)

	// Derive node key for root directory
	nodeKey, err := w.DeriveNodeKey(1, []uint32{0}, []bool{true})
	require.NoError(t, err)

	// Derive fee key
	feeKey, err := w.DeriveFeeKey(0, 0)
	require.NoError(t, err)

	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err)

	// Get a funded UTXO for the fee key
	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)

	// Build root transaction
	payload := []byte{0x0a, 0x04, 0x72, 0x6f, 0x6f, 0x74} // minimal protobuf: name="root"
	result, err := tx.BuildCreateRoot(&tx.CreateRootParams{
		NodePubKey:  nodeKey.PublicKey,
		NodePrivKey: nodeKey.PrivateKey,
		Payload:     payload,
		FeeUTXO:     feeUTXO,
		ChangeAddr:  feeAddr.PublicKeyHash,
		FeeRate:     500,
	})
	require.NoError(t, err)

	// Sign
	signedHex, err := tx.SignMetanetTx(result, []*tx.UTXO{feeUTXO})
	require.NoError(t, err)

	// Broadcast
	txid, err := node.SendRawTransaction(ctx, signedHex)
	require.NoError(t, err)
	t.Logf("Root tx broadcast: %s", txid)

	// Mine to confirm
	nodeAddr, _ := node.NewAddress(ctx)
	_, err = node.MineBlocks(ctx, 1, nodeAddr)
	require.NoError(t, err)

	// Retrieve from chain
	rawTx, err := node.GetRawTransaction(ctx, txid)
	require.NoError(t, err)

	// Parse back and verify OP_RETURN
	parsedTx, err := transaction.NewTransactionFromBytes(rawTx)
	require.NoError(t, err)
	require.True(t, len(parsedTx.Outputs) >= 2, "should have at least 2 outputs")

	// Verify OP_RETURN output contains MetaFlag
	opReturnScript := parsedTx.Outputs[0].LockingScript
	require.Contains(t, hex.EncodeToString(*opReturnScript), "6d657461", "should contain MetaFlag")

	t.Logf("Root tx confirmed with %d outputs", len(parsedTx.Outputs))
}

// setupFundedWallet creates an HD wallet and funds it via regtest mining.
func setupFundedWallet(t *testing.T, ctx context.Context, node *testutil.RegtestNode) *wallet.Wallet {
	t.Helper()
	mnemonic, err := wallet.GenerateMnemonic(12)
	require.NoError(t, err)
	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)
	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err)
	return w
}

// getFundedUTXO funds an address and returns a UTXO ready for transaction building.
func getFundedUTXO(t *testing.T, ctx context.Context, node *testutil.RegtestNode, addr string, key *wallet.KeyPair) *tx.UTXO {
	t.Helper()

	err := node.ImportAddress(ctx, addr)
	require.NoError(t, err)

	nodeAddr, _ := node.NewAddress(ctx)
	_, err = node.MineBlocks(ctx, 101, nodeAddr)
	require.NoError(t, err)

	txid, err := node.SendToAddress(ctx, addr, 0.01)
	require.NoError(t, err)

	_, err = node.MineBlocks(ctx, 1, nodeAddr)
	require.NoError(t, err)

	utxos, err := node.ListUnspent(ctx, addr)
	require.NoError(t, err)
	require.NotEmpty(t, utxos)

	u := utxos[0]
	txidBytes, _ := hex.DecodeString(u.TxID)
	scriptBytes, _ := hex.DecodeString(u.ScriptPubKey)

	return &tx.UTXO{
		TxID:         txidBytes,
		Vout:         u.Vout,
		Amount:       uint64(u.Amount * 1e8),
		ScriptPubKey: scriptBytes,
		PrivateKey:   key.PrivateKey,
	}
}
```

**Step 2: Run test**

Run: `go test -tags e2e ./e2e/ -run TestMetanetRoot -v -timeout 60s`
Expected: PASS

**Step 3: Commit**

```bash
git add e2e/02_metanet_root_test.go
git commit -m "test(e2e): add Metanet root tx broadcast and confirmation against regtest"
```

---

## Task 7: E2E Test — 03_mkdir_upload

Create a directory, upload an encrypted file, broadcast both, verify DAG structure.

**Files:**
- Create: `e2e/03_mkdir_upload_test.go`

**Description:** This test builds on Task 6's helpers. It creates a root dir, then a child dir (mkdir), then a child file node with encrypted content. All three transactions are broadcast and confirmed. After confirmation, the test retrieves all three from the chain and verifies the Metanet DAG parent→child links via OP_RETURN parsing.

**Key steps:**
1. Create wallet, fund fee key
2. `BuildCreateRoot` → sign → broadcast → confirm
3. `BuildCreateChild` (directory) → sign → broadcast → confirm (uses root's NodeUTXO)
4. `BuildCreateChild` (file) + `BuildDataTransaction` (encrypted content) → sign → broadcast → confirm
5. Retrieve all txs from chain, parse OP_RETURN, verify ParentTxID links

The test code will follow the same patterns as Task 6 but with chained transactions. Implementation details depend on how the MetanetTx.NodeUTXO and ParentUTXO chain — the implementer should trace the UTXO flow through consecutive Build calls.

**Step 1: Write test, Step 2: Run, Step 3: Commit** — same pattern as Task 6.

---

## Task 8: E2E Test — 04_spv_verify

Get a Merkle proof from the regtest node for a confirmed transaction and run full SPV verification.

**Files:**
- Create: `e2e/04_spv_verify_test.go`

**Key steps:**
1. Broadcast any transaction (reuse wallet funding tx)
2. Mine to confirm
3. `node.GetTxOutProof(txid)` → parse BIP37 MerkleBlock
4. `node.GetBlockHeader(blockHash)` → parse into `spv.BlockHeader`
5. Construct `spv.MerkleProof` from the BIP37 data
6. Store header in `spv.MemHeaderStore`
7. Call `spv.VerifyTransaction()` — should pass
8. Tamper with proof → should fail

**Note:** BIP37 MerkleBlock parsing is needed. This may require a small parser in `e2e/testutil/` that extracts the Merkle branch from the `gettxoutproof` response format. The BIP37 MerkleBlock format is: 80-byte header + varint tx count + bit flags + hashes.

---

## Task 9: E2E Test — 05_free_content

Upload a free-access file and retrieve it through the daemon HTTP endpoint.

**Files:**
- Create: `e2e/05_free_content_test.go`

**Key steps:**
1. Create wallet, build Metanet root + child file node with AccessMode=Free
2. Encrypt content with Method 42 Free mode (scalar 1)
3. Store ciphertext in local FileStore
4. Start daemon `httptest.Server` with real WalletService + ContentStore
5. HTTP GET the file path → should return 200 with decrypted content
6. Verify Content-Type negotiation (JSON, HTML, Markdown)

**Note:** This test combines on-chain tx building (for the Metanet DAG entry) with local daemon serving. The daemon doesn't need to query the blockchain — it only needs the MetanetService mock pointed at the real tx data.

---

## Task 10: E2E Test — 06_paid_purchase

Full x402 paid purchase flow with HTLC between two wallets.

**Files:**
- Create: `e2e/06_paid_purchase_test.go`

**Key steps:**
1. Create seller wallet + buyer wallet, fund both
2. Seller: build Metanet file node with AccessMode=Paid, pricePerKB set
3. Seller: encrypt content with Method 42 Paid mode
4. Buyer: request content → gets HTTP 402 with x402 headers
5. Buyer: parse invoice, build HTLC transaction, sign, broadcast
6. Seller: see HTLC on-chain, reveal capsule (preimage)
7. Buyer: extract capsule from seller's spend tx, decrypt content
8. Verify decrypted content matches original

**Note:** This is the most complex test. It exercises x402 invoice creation, HTLC script building, capsule computation, and the buy/reveal handshake. The HTLC claim by the seller requires a separate transaction that spends the HTLC output using the capsule as preimage. This may need a new helper function in libbitfs/x402 to build the claim transaction.

---

## Task 11: E2E Test — 07_full_lifecycle

End-to-end lifecycle: create → upload → read → update → purchase → delete → verify.

**Files:**
- Create: `e2e/07_full_lifecycle_test.go`

**Key steps:**
1. Create wallet, fund
2. Create root dir → broadcast
3. mkdir "docs" → broadcast
4. put "docs/hello.txt" (Free) → broadcast + store encrypted content
5. Read back via daemon → verify content
6. SelfUpdate "docs/hello.txt" with new content → broadcast
7. Read updated version → verify new content
8. Change access to Paid → ReEncrypt
9. Buyer purchases → HTLC flow
10. Delete file (remove from directory) → broadcast
11. Verify file no longer accessible

**Note:** This test is a comprehensive smoke test. It doesn't need to be as detailed as individual tests — it validates the full workflow hangs together. Share helpers from earlier tests.

---

## Task 12: README and CI Integration

**Files:**
- Create: `e2e/README.md`
- Modify: `.gitignore` (add `e2e/bsv-data/` if volume is local)

**Step 1: Write README**

Document:
- Prerequisites: Docker
- How to start: `cd e2e && docker compose up -d`
- How to run: `go test -tags e2e ./e2e/... -v -timeout 120s`
- How to stop: `cd e2e && docker compose down -v`
- Troubleshooting: node startup, port conflicts, coinbase maturity

**Step 2: Commit everything**

```bash
git add e2e/README.md .gitignore
git commit -m "docs(e2e): add README and gitignore for regtest infrastructure"
```

---

## Summary

| Task | What | New Files | Depends On |
|------|------|-----------|------------|
| 1 | Docker infrastructure | docker-compose.yml, bitcoin.conf | — |
| 2 | JSON-RPC client | testutil/rpc.go | Task 1 |
| 3 | Regtest node helper | testutil/node.go | Task 2 |
| 4 | Transaction signing | libbitfs/tx/sign.go | — |
| 5 | 01_wallet_fund | e2e test | Tasks 3, 4 |
| 6 | 02_metanet_root | e2e test | Task 5 |
| 7 | 03_mkdir_upload | e2e test | Task 6 |
| 8 | 04_spv_verify | e2e test | Task 3 |
| 9 | 05_free_content | e2e test | Task 7 |
| 10 | 06_paid_purchase | e2e test | Task 7 |
| 11 | 07_full_lifecycle | e2e test | Tasks 9, 10 |
| 12 | README + CI | docs | Task 11 |

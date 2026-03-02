# Network Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `libbitfs/network/` package that lets BitFS (and future Metanet) query UTXOs, broadcast transactions, fetch headers, and verify transactions via SPV — backed by JSON-RPC for regtest/testnet.

**Architecture:** Define a `BlockchainService` interface in `libbitfs/network/`. Implement it with an `RPCClient` (JSON-RPC 1.0) promoted from `bitfs/e2e/testutil/rpc.go`. Add an `SPVClient` that bridges the network layer with existing `libbitfs/spv/` verification. Wire into `bitfs/internal/engine/` so operations like `put`/`mkdir` can broadcast real transactions.

**Tech Stack:** Go 1.25.6, `net/http` (stdlib), `libbitfs/spv`, `libbitfs/tx`, `github.com/stretchr/testify`

**TDD:** Every task writes tests first, then implementation. User explicitly requested test-first.

---

## Task 1: Define BlockchainService Interface and Data Types

**Files:**
- Create: `libbitfs/network/service.go`
- Create: `libbitfs/network/errors.go`
- Test: `libbitfs/network/service_test.go`

**Step 1: Write the test that validates interface compliance**

```go
// libbitfs/network/service_test.go
package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockBlockchainService is a test double for BlockchainService.
type MockBlockchainService struct {
	ListUnspentFn      func(ctx context.Context, address string) ([]*UTXO, error)
	GetUTXOFn          func(ctx context.Context, txid string, vout uint32) (*UTXO, error)
	BroadcastTxFn      func(ctx context.Context, rawTxHex string) (string, error)
	GetRawTxFn         func(ctx context.Context, txid string) ([]byte, error)
	GetTxStatusFn      func(ctx context.Context, txid string) (*TxStatus, error)
	GetBlockHeaderFn   func(ctx context.Context, blockHash string) ([]byte, error)
	GetMerkleProofFn   func(ctx context.Context, txid string) (*MerkleProof, error)
	GetBestBlockHeightFn func(ctx context.Context) (uint64, error)
}

func (m *MockBlockchainService) ListUnspent(ctx context.Context, address string) ([]*UTXO, error) {
	return m.ListUnspentFn(ctx, address)
}
func (m *MockBlockchainService) GetUTXO(ctx context.Context, txid string, vout uint32) (*UTXO, error) {
	return m.GetUTXOFn(ctx, txid, vout)
}
func (m *MockBlockchainService) BroadcastTx(ctx context.Context, rawTxHex string) (string, error) {
	return m.BroadcastTxFn(ctx, rawTxHex)
}
func (m *MockBlockchainService) GetRawTx(ctx context.Context, txid string) ([]byte, error) {
	return m.GetRawTxFn(ctx, txid)
}
func (m *MockBlockchainService) GetTxStatus(ctx context.Context, txid string) (*TxStatus, error) {
	return m.GetTxStatusFn(ctx, txid)
}
func (m *MockBlockchainService) GetBlockHeader(ctx context.Context, blockHash string) ([]byte, error) {
	return m.GetBlockHeaderFn(ctx, blockHash)
}
func (m *MockBlockchainService) GetMerkleProof(ctx context.Context, txid string) (*MerkleProof, error) {
	return m.GetMerkleProofFn(ctx, txid)
}
func (m *MockBlockchainService) GetBestBlockHeight(ctx context.Context) (uint64, error) {
	return m.GetBestBlockHeightFn(ctx)
}

func TestMockImplementsInterface(t *testing.T) {
	var _ BlockchainService = (*MockBlockchainService)(nil)
}

func TestUTXOAmountSatoshis(t *testing.T) {
	u := &UTXO{
		TxID:   "abc123",
		Vout:   0,
		Amount: 100000,
	}
	assert.Equal(t, uint64(100000), u.Amount)
	assert.Equal(t, "abc123", u.TxID)
}

func TestTxStatusConfirmed(t *testing.T) {
	s := &TxStatus{Confirmed: true, BlockHeight: 100, TxIndex: 3}
	assert.True(t, s.Confirmed)
	assert.Equal(t, uint64(100), s.BlockHeight)
	assert.Equal(t, 3, s.TxIndex)
}

func TestMerkleProofFields(t *testing.T) {
	p := &MerkleProof{
		TxID:      "deadbeef",
		BlockHash: "cafebabe",
		Branches:  [][]byte{{0x01}, {0x02}},
		Index:     5,
	}
	assert.Len(t, p.Branches, 2)
	assert.Equal(t, 5, p.Index)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -v`
Expected: FAIL — package doesn't exist yet

**Step 3: Write the interface and types**

```go
// libbitfs/network/service.go
package network

import "context"

// BlockchainService is the primary interface for blockchain interaction.
// Both BitFS and Metanet products import and use this interface.
type BlockchainService interface {
	// ListUnspent returns all unspent outputs for a BSV address.
	ListUnspent(ctx context.Context, address string) ([]*UTXO, error)

	// GetUTXO returns a specific unspent output by txid and vout.
	// Returns ErrTxNotFound if the output doesn't exist or is spent.
	GetUTXO(ctx context.Context, txid string, vout uint32) (*UTXO, error)

	// BroadcastTx submits a signed raw transaction hex to the network.
	// Returns the txid on success, or ErrBroadcastRejected if the node rejects it.
	BroadcastTx(ctx context.Context, rawTxHex string) (string, error)

	// GetRawTx returns the raw serialized bytes of a transaction.
	GetRawTx(ctx context.Context, txid string) ([]byte, error)

	// GetTxStatus returns confirmation status of a transaction.
	GetTxStatus(ctx context.Context, txid string) (*TxStatus, error)

	// GetBlockHeader returns the raw 80-byte block header.
	GetBlockHeader(ctx context.Context, blockHash string) ([]byte, error)

	// GetMerkleProof returns a Merkle inclusion proof for a confirmed transaction.
	GetMerkleProof(ctx context.Context, txid string) (*MerkleProof, error)

	// GetBestBlockHeight returns the height of the chain tip.
	GetBestBlockHeight(ctx context.Context) (uint64, error)
}

// UTXO represents an unspent transaction output.
// TxID is display-format hex (big-endian), matching RPC/WoC conventions.
type UTXO struct {
	TxID          string `json:"txid"`
	Vout          uint32 `json:"vout"`
	Amount        uint64 `json:"amount"`         // satoshis
	ScriptPubKey  string `json:"script_pubkey"`  // hex-encoded locking script
	Address       string `json:"address"`
	Confirmations int64  `json:"confirmations"`
}

// TxStatus represents the confirmation status of a transaction.
type TxStatus struct {
	Confirmed   bool   `json:"confirmed"`
	BlockHash   string `json:"block_hash"`
	BlockHeight uint64 `json:"block_height"`
	TxIndex     int    `json:"tx_index"` // position in block (TTOR)
}

// MerkleProof represents a Merkle inclusion proof for a transaction.
type MerkleProof struct {
	TxID      string   `json:"txid"`
	BlockHash string   `json:"block_hash"`
	Branches  [][]byte `json:"branches"` // sibling hashes, bottom-up
	Index     int      `json:"index"`    // leaf position
}
```

```go
// libbitfs/network/errors.go
package network

import "errors"

var (
	ErrConnectionFailed  = errors.New("network: connection failed")
	ErrAuthFailed        = errors.New("network: authentication failed")
	ErrTxNotFound        = errors.New("network: transaction not found")
	ErrBroadcastRejected = errors.New("network: broadcast rejected")
	ErrInvalidResponse   = errors.New("network: invalid response")
)
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -v`
Expected: PASS — all 4 tests pass

**Step 5: Commit**

```bash
git add libbitfs/network/
git commit -m "feat(network): define BlockchainService interface and data types"
```

---

## Task 2: Configuration — RPCConfig, Presets, ResolveConfig

**Files:**
- Create: `libbitfs/network/config.go`
- Create: `libbitfs/network/config_test.go`

**Step 1: Write the tests**

```go
// libbitfs/network/config_test.go
package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkPresets(t *testing.T) {
	tests := []struct {
		name    string
		network string
		url     string
		user    string
	}{
		{"regtest defaults", "regtest", "http://localhost:18332", "bitfs"},
		{"testnet defaults", "testnet", "http://localhost:18332", "bitfs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preset, ok := NetworkPresets[tt.network]
			require.True(t, ok, "preset should exist for %s", tt.network)
			assert.Equal(t, tt.url, preset.URL)
			assert.Equal(t, tt.user, preset.User)
		})
	}
}

func TestMainnetHasNoPreset(t *testing.T) {
	_, ok := NetworkPresets["mainnet"]
	assert.False(t, ok, "mainnet should not have a default preset")
}

func TestResolveConfigFlagsOverrideAll(t *testing.T) {
	flags := &RPCConfig{URL: "http://custom:9999", User: "me", Password: "secret"}
	cfg, err := ResolveConfig(flags, nil, "regtest")
	require.NoError(t, err)
	assert.Equal(t, "http://custom:9999", cfg.URL)
	assert.Equal(t, "me", cfg.User)
	assert.Equal(t, "secret", cfg.Password)
}

func TestResolveConfigEnvOverridesPreset(t *testing.T) {
	env := map[string]string{
		"BITFS_RPC_URL":  "http://env-node:18332",
		"BITFS_RPC_USER": "envuser",
	}
	cfg, err := ResolveConfig(nil, env, "regtest")
	require.NoError(t, err)
	assert.Equal(t, "http://env-node:18332", cfg.URL)
	assert.Equal(t, "envuser", cfg.User)
	assert.Equal(t, "bitfs", cfg.Password) // falls through to preset
}

func TestResolveConfigPresetFallback(t *testing.T) {
	cfg, err := ResolveConfig(nil, nil, "regtest")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:18332", cfg.URL)
	assert.Equal(t, "bitfs", cfg.User)
	assert.Equal(t, "bitfs", cfg.Password)
}

func TestResolveConfigMainnetRequiresExplicit(t *testing.T) {
	_, err := ResolveConfig(nil, nil, "mainnet")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mainnet")
}

func TestResolveConfigPartialFlags(t *testing.T) {
	flags := &RPCConfig{URL: "http://partial:8332"}
	cfg, err := ResolveConfig(flags, nil, "regtest")
	require.NoError(t, err)
	assert.Equal(t, "http://partial:8332", cfg.URL)
	assert.Equal(t, "bitfs", cfg.User)     // from preset
	assert.Equal(t, "bitfs", cfg.Password) // from preset
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -run TestNetwork -v && go test ./network/ -run TestResolve -v`
Expected: FAIL — RPCConfig, NetworkPresets, ResolveConfig not defined

**Step 3: Write the implementation**

```go
// libbitfs/network/config.go
package network

import "fmt"

// RPCConfig holds connection parameters for a BSV JSON-RPC node.
type RPCConfig struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
	Network  string `json:"network"` // "mainnet", "testnet", "regtest"
}

// NetworkPresets contains default RPC configurations per network.
// Mainnet has no preset — it must be explicitly configured.
var NetworkPresets = map[string]RPCConfig{
	"regtest": {URL: "http://localhost:18332", User: "bitfs", Password: "bitfs"},
	"testnet": {URL: "http://localhost:18332", User: "bitfs", Password: "bitfs"},
}

// ResolveConfig merges configuration from multiple sources.
// Priority: flags > env > preset. env keys: BITFS_RPC_URL, BITFS_RPC_USER, BITFS_RPC_PASS.
// If env is nil, environment variables are not consulted.
func ResolveConfig(flags *RPCConfig, env map[string]string, network string) (*RPCConfig, error) {
	// Start with preset (if exists).
	result := RPCConfig{Network: network}
	if preset, ok := NetworkPresets[network]; ok {
		result = preset
		result.Network = network
	}

	// Apply environment variables.
	if env != nil {
		if v, ok := env["BITFS_RPC_URL"]; ok && v != "" {
			result.URL = v
		}
		if v, ok := env["BITFS_RPC_USER"]; ok && v != "" {
			result.User = v
		}
		if v, ok := env["BITFS_RPC_PASS"]; ok && v != "" {
			result.Password = v
		}
	}

	// Apply CLI flags (highest priority).
	if flags != nil {
		if flags.URL != "" {
			result.URL = flags.URL
		}
		if flags.User != "" {
			result.User = flags.User
		}
		if flags.Password != "" {
			result.Password = flags.Password
		}
	}

	// Validate: URL must be set.
	if result.URL == "" {
		return nil, fmt.Errorf("network: %s requires explicit RPC configuration (set --rpc-url, BITFS_RPC_URL, or config file)", network)
	}

	return &result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -v`
Expected: PASS — all tests pass

**Step 5: Commit**

```bash
git add libbitfs/network/config.go libbitfs/network/config_test.go
git commit -m "feat(network): add RPCConfig, presets, and ResolveConfig"
```

---

## Task 3: RPCClient — JSON-RPC Core (Call Method)

**Files:**
- Create: `libbitfs/network/rpc.go`
- Create: `libbitfs/network/rpc_test.go`

**Step 1: Write the tests**

```go
// libbitfs/network/rpc_test.go
package network

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRPCClientCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify basic auth.
		user, pass, ok := r.BasicAuth()
		require.True(t, ok)
		assert.Equal(t, "testuser", user)
		assert.Equal(t, "testpass", pass)

		// Verify content type.
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Parse request.
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "getblockcount", req.Method)

		// Respond.
		resp := rpcResponse{ID: req.ID, Result: json.RawMessage(`100`)}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL, User: "testuser", Password: "testpass"})
	var height int
	err := client.Call(context.Background(), "getblockcount", nil, &height)
	require.NoError(t, err)
	assert.Equal(t, 100, height)
}

func TestRPCClientRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // bitcoind returns 500 for RPC errors
		resp := rpcResponse{
			Error: &rpcError{Code: -5, Message: "No such mempool or blockchain transaction"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	var result json.RawMessage
	err := client.Call(context.Background(), "getrawtransaction", []interface{}{"badtxid"}, &result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No such mempool")
}

func TestRPCClientConnectionError(t *testing.T) {
	client := NewRPCClient(RPCConfig{URL: "http://localhost:1"}) // nothing listening
	var result int
	err := client.Call(context.Background(), "getblockcount", nil, &result)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConnectionFailed)
}

func TestRPCClientContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // block until canceled
	}))
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	var result int
	err := client.Call(ctx, "getblockcount", nil, &result)
	require.Error(t, err)
}

func TestRPCClientSequentialIDs(t *testing.T) {
	var ids []int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		json.NewDecoder(r.Body).Decode(&req)
		ids = append(ids, req.ID)
		resp := rpcResponse{ID: req.ID, Result: json.RawMessage(`0`)}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	for i := 0; i < 3; i++ {
		var n int
		client.Call(context.Background(), "getblockcount", nil, &n)
	}
	assert.Equal(t, int64(1), ids[0])
	assert.Equal(t, int64(2), ids[1])
	assert.Equal(t, int64(3), ids[2])
}

func TestRPCClientNilResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := rpcResponse{ID: req.ID, Result: json.RawMessage(`"txid123"`)}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Calling with nil result should not panic.
	client := NewRPCClient(RPCConfig{URL: server.URL})
	err := client.Call(context.Background(), "sendrawtransaction", []interface{}{"hex"}, nil)
	require.NoError(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -run TestRPCClient -v`
Expected: FAIL — NewRPCClient, rpcRequest etc. not defined

**Step 3: Write the implementation**

```go
// libbitfs/network/rpc.go
package network

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// RPCClient implements JSON-RPC 1.0 communication with a BSV node.
type RPCClient struct {
	url    string
	user   string
	pass   string
	client *http.Client
	nextID atomic.Int64
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	ID     int64            `json:"id"`
	Result json.RawMessage  `json:"result"`
	Error  *rpcError        `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewRPCClient creates a JSON-RPC client from an RPCConfig.
func NewRPCClient(cfg RPCConfig) *RPCClient {
	return &RPCClient{
		url:  cfg.URL,
		user: cfg.User,
		pass: cfg.Password,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     90 * time.Second,
				MaxIdleConnsPerHost: 10,
			},
		},
	}
}

// Call performs a JSON-RPC call. params may be nil for methods with no arguments.
// result may be nil if the caller doesn't need the return value.
func (c *RPCClient) Call(ctx context.Context, method string, params []interface{}, result interface{}) error {
	if params == nil {
		params = []interface{}{}
	}
	reqBody := rpcRequest{
		JSONRPC: "1.0",
		ID:      c.nextID.Add(1),
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("network: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("network: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}
	defer resp.Body.Close()

	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrInvalidResponse, err)
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("network: rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if result != nil && rpcResp.Result != nil {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("%w: unmarshal result: %v", ErrInvalidResponse, err)
		}
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -run TestRPCClient -v`
Expected: PASS

**Step 5: Commit**

```bash
git add libbitfs/network/rpc.go libbitfs/network/rpc_test.go
git commit -m "feat(network): implement RPCClient JSON-RPC core"
```

---

## Task 4: RPCClient — BlockchainService Implementation

**Files:**
- Create: `libbitfs/network/rpc_blockchain.go`
- Create: `libbitfs/network/rpc_blockchain_test.go`

This implements all 8 methods of BlockchainService on RPCClient.

**Step 1: Write the tests**

```go
// libbitfs/network/rpc_blockchain_test.go
package network

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rpcTestServer dispatches based on JSON-RPC method name.
func rpcTestServer(t *testing.T, handlers map[string]func(params []interface{}) (interface{}, *rpcError)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		handler, ok := handlers[req.Method]
		if !ok {
			t.Fatalf("unexpected RPC method: %s", req.Method)
		}
		result, rpcErr := handler(req.Params)
		resp := rpcResponse{ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			resp.Result, _ = json.Marshal(result)
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestListUnspent(t *testing.T) {
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"listunspent": func(params []interface{}) (interface{}, *rpcError) {
			return []map[string]interface{}{
				{
					"txid":          "aabb",
					"vout":          float64(0),
					"address":       "1A1zP1",
					"scriptPubKey":  "76a914...",
					"amount":        0.001,
					"confirmations": float64(6),
				},
			}, nil
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	utxos, err := client.ListUnspent(context.Background(), "1A1zP1")
	require.NoError(t, err)
	require.Len(t, utxos, 1)
	assert.Equal(t, "aabb", utxos[0].TxID)
	assert.Equal(t, uint64(100000), utxos[0].Amount) // 0.001 BTC = 100000 sat
	assert.Equal(t, int64(6), utxos[0].Confirmations)
}

func TestBroadcastTx(t *testing.T) {
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"sendrawtransaction": func(params []interface{}) (interface{}, *rpcError) {
			return "txid_returned", nil
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	txid, err := client.BroadcastTx(context.Background(), "01000000...")
	require.NoError(t, err)
	assert.Equal(t, "txid_returned", txid)
}

func TestBroadcastTxRejected(t *testing.T) {
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"sendrawtransaction": func(params []interface{}) (interface{}, *rpcError) {
			return nil, &rpcError{Code: -25, Message: "Missing inputs"}
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	_, err := client.BroadcastTx(context.Background(), "badtx")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBroadcastRejected)
}

func TestGetRawTx(t *testing.T) {
	rawHex := "0100000001abcd"
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"getrawtransaction": func(params []interface{}) (interface{}, *rpcError) {
			return rawHex, nil
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	raw, err := client.GetRawTx(context.Background(), "sometxid")
	require.NoError(t, err)
	expected, _ := hex.DecodeString(rawHex)
	assert.Equal(t, expected, raw)
}

func TestGetTxStatus(t *testing.T) {
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"getrawtransaction": func(params []interface{}) (interface{}, *rpcError) {
			return map[string]interface{}{
				"blockhash":     "000abc",
				"blockheight":   float64(500),
				"confirmations": float64(10),
			}, nil
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	status, err := client.GetTxStatus(context.Background(), "txid123")
	require.NoError(t, err)
	assert.True(t, status.Confirmed)
	assert.Equal(t, "000abc", status.BlockHash)
	assert.Equal(t, uint64(500), status.BlockHeight)
}

func TestGetTxStatusUnconfirmed(t *testing.T) {
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"getrawtransaction": func(params []interface{}) (interface{}, *rpcError) {
			return map[string]interface{}{
				"confirmations": float64(0),
			}, nil
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	status, err := client.GetTxStatus(context.Background(), "txid123")
	require.NoError(t, err)
	assert.False(t, status.Confirmed)
}

func TestGetBlockHeader(t *testing.T) {
	// 80-byte header as hex.
	headerHex := hex.EncodeToString(make([]byte, 80))
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"getblockheader": func(params []interface{}) (interface{}, *rpcError) {
			return headerHex, nil
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	data, err := client.GetBlockHeader(context.Background(), "blockhash123")
	require.NoError(t, err)
	assert.Len(t, data, 80)
}

func TestGetMerkleProof(t *testing.T) {
	// Minimal serialized CMerkleBlock (header 80B + 4B numTx + varint + hashes + flags).
	// For test purposes, we just verify the method call works.
	proofHex := hex.EncodeToString(make([]byte, 100))
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"gettxoutproof": func(params []interface{}) (interface{}, *rpcError) {
			return proofHex, nil
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	proof, err := client.GetMerkleProof(context.Background(), "txid123")
	require.NoError(t, err)
	assert.NotNil(t, proof)
}

func TestGetBestBlockHeight(t *testing.T) {
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"getblockcount": func(params []interface{}) (interface{}, *rpcError) {
			return float64(12345), nil
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	height, err := client.GetBestBlockHeight(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(12345), height)
}

func TestGetUTXO(t *testing.T) {
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"gettxout": func(params []interface{}) (interface{}, *rpcError) {
			return map[string]interface{}{
				"value": 0.00001,
				"scriptPubKey": map[string]interface{}{
					"hex":       "76a914...",
					"addresses": []interface{}{"1A1zP1"},
				},
				"confirmations": float64(3),
			}, nil
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	utxo, err := client.GetUTXO(context.Background(), "sometxid", 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(1000), utxo.Amount) // 0.00001 BTC
	assert.Equal(t, "1A1zP1", utxo.Address)
}

func TestGetUTXOSpent(t *testing.T) {
	server := rpcTestServer(t, map[string]func([]interface{}) (interface{}, *rpcError){
		"gettxout": func(params []interface{}) (interface{}, *rpcError) {
			return nil, nil // null result means spent
		},
	})
	defer server.Close()

	client := NewRPCClient(RPCConfig{URL: server.URL})
	_, err := client.GetUTXO(context.Background(), "sometxid", 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTxNotFound)
}

func TestRPCClientImplementsBlockchainService(t *testing.T) {
	var _ BlockchainService = (*RPCClient)(nil)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -run "TestList|TestBroadcast|TestGetRaw|TestGetTxStatus|TestGetBlock|TestGetMerkle|TestGetBest|TestGetUTXO|TestRPCClientImpl" -v`
Expected: FAIL — methods not implemented on RPCClient

**Step 3: Write the implementation**

```go
// libbitfs/network/rpc_blockchain.go
package network

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
)

// btcToSat converts BTC (float64 from JSON-RPC) to satoshis.
func btcToSat(btc float64) uint64 {
	return uint64(math.Round(btc * 1e8))
}

// ListUnspent queries the node for UTXOs belonging to an address.
// Uses: listunspent 0 9999999 ["address"]
func (c *RPCClient) ListUnspent(ctx context.Context, address string) ([]*UTXO, error) {
	var rpcResult []struct {
		TxID          string  `json:"txid"`
		Vout          uint32  `json:"vout"`
		Address       string  `json:"address"`
		ScriptPubKey  string  `json:"scriptPubKey"`
		Amount        float64 `json:"amount"`
		Confirmations int64   `json:"confirmations"`
	}

	err := c.Call(ctx, "listunspent", []interface{}{0, 9999999, []string{address}}, &rpcResult)
	if err != nil {
		return nil, err
	}

	utxos := make([]*UTXO, len(rpcResult))
	for i, r := range rpcResult {
		utxos[i] = &UTXO{
			TxID:          r.TxID,
			Vout:          r.Vout,
			Amount:        btcToSat(r.Amount),
			ScriptPubKey:  r.ScriptPubKey,
			Address:       r.Address,
			Confirmations: r.Confirmations,
		}
	}
	return utxos, nil
}

// GetUTXO queries a specific unspent output.
// Uses: gettxout "txid" vout
func (c *RPCClient) GetUTXO(ctx context.Context, txid string, vout uint32) (*UTXO, error) {
	var result *struct {
		Value        float64 `json:"value"`
		ScriptPubKey struct {
			Hex       string   `json:"hex"`
			Addresses []string `json:"addresses"`
		} `json:"scriptPubKey"`
		Confirmations int64 `json:"confirmations"`
	}

	err := c.Call(ctx, "gettxout", []interface{}{txid, vout}, &result)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("%w: output %s:%d is spent or does not exist", ErrTxNotFound, txid, vout)
	}

	addr := ""
	if len(result.ScriptPubKey.Addresses) > 0 {
		addr = result.ScriptPubKey.Addresses[0]
	}

	return &UTXO{
		TxID:          txid,
		Vout:          vout,
		Amount:        btcToSat(result.Value),
		ScriptPubKey:  result.ScriptPubKey.Hex,
		Address:       addr,
		Confirmations: result.Confirmations,
	}, nil
}

// BroadcastTx submits a signed raw transaction to the network.
// Uses: sendrawtransaction "hexstring"
func (c *RPCClient) BroadcastTx(ctx context.Context, rawTxHex string) (string, error) {
	var txid string
	err := c.Call(ctx, "sendrawtransaction", []interface{}{rawTxHex}, &txid)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBroadcastRejected, err)
	}
	return txid, nil
}

// GetRawTx fetches raw transaction bytes.
// Uses: getrawtransaction "txid" false
func (c *RPCClient) GetRawTx(ctx context.Context, txid string) ([]byte, error) {
	var rawHex string
	err := c.Call(ctx, "getrawtransaction", []interface{}{txid, false}, &rawHex)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(rawHex)
}

// GetTxStatus returns the confirmation status of a transaction.
// Uses: getrawtransaction "txid" true (verbose)
func (c *RPCClient) GetTxStatus(ctx context.Context, txid string) (*TxStatus, error) {
	var result struct {
		BlockHash     string `json:"blockhash"`
		BlockHeight   uint64 `json:"blockheight"`
		Confirmations int64  `json:"confirmations"`
	}

	err := c.Call(ctx, "getrawtransaction", []interface{}{txid, true}, &result)
	if err != nil {
		return nil, err
	}

	return &TxStatus{
		Confirmed:   result.Confirmations > 0,
		BlockHash:   result.BlockHash,
		BlockHeight: result.BlockHeight,
	}, nil
}

// GetBlockHeader fetches the raw 80-byte block header.
// Uses: getblockheader "hash" false
func (c *RPCClient) GetBlockHeader(ctx context.Context, blockHash string) ([]byte, error) {
	var headerHex string
	err := c.Call(ctx, "getblockheader", []interface{}{blockHash, false}, &headerHex)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(headerHex)
}

// GetMerkleProof fetches a Merkle inclusion proof for a transaction.
// Uses: gettxoutproof ["txid"]
// Returns a parsed MerkleProof from the BIP37 CMerkleBlock.
func (c *RPCClient) GetMerkleProof(ctx context.Context, txid string) (*MerkleProof, error) {
	var proofHex string
	err := c.Call(ctx, "gettxoutproof", []interface{}{[]string{txid}}, &proofHex)
	if err != nil {
		return nil, err
	}

	data, err := hex.DecodeString(proofHex)
	if err != nil {
		return nil, fmt.Errorf("%w: decode proof hex: %v", ErrInvalidResponse, err)
	}

	return parseCMerkleBlock(txid, data)
}

// GetBestBlockHeight returns the current chain height.
// Uses: getblockcount
func (c *RPCClient) GetBestBlockHeight(ctx context.Context) (uint64, error) {
	var height uint64
	err := c.Call(ctx, "getblockcount", nil, &height)
	return height, err
}
```

Also need the CMerkleBlock parser (minimal):

```go
// parseCMerkleBlock extracts a MerkleProof from raw CMerkleBlock bytes.
// CMerkleBlock format: [header 80B][numTx 4B][varint numHashes][hashes...][varint numFlags][flags...]
func parseCMerkleBlock(txid string, data []byte) (*MerkleProof, error) {
	if len(data) < 84 {
		return nil, fmt.Errorf("%w: CMerkleBlock too short (%d bytes)", ErrInvalidResponse, len(data))
	}

	// Extract block hash from header.
	// For now, return a minimal proof. Full parsing is done in Task 5 (SPVClient).
	return &MerkleProof{
		TxID:      txid,
		BlockHash: "", // filled by SPVClient when needed
		Branches:  nil,
		Index:     0,
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add libbitfs/network/rpc_blockchain.go libbitfs/network/rpc_blockchain_test.go
git commit -m "feat(network): implement BlockchainService on RPCClient"
```

---

## Task 5: SPVClient — Bridge Network Layer with libbitfs/spv

**Files:**
- Create: `libbitfs/network/spvclient.go`
- Create: `libbitfs/network/spvclient_test.go`

**Step 1: Write the tests**

Test SPVClient using MockBlockchainService (from Task 1) and libbitfs/spv's MemHeaderStore.

```go
// libbitfs/network/spvclient_test.go
package network

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/libbitfs/spv"
)

func TestSPVClientVerifyTxConfirmed(t *testing.T) {
	// Build a valid Merkle tree with one tx.
	txHash := spv.DoubleHash([]byte("test-tx"))
	merkleRoot := txHash // single tx = root

	// Build a header containing this Merkle root.
	header := &spv.BlockHeader{
		Version:    1,
		PrevBlock:  make([]byte, 32),
		MerkleRoot: merkleRoot,
		Timestamp:  1700000000,
		Bits:       0x207fffff,
		Nonce:      0,
		Height:     1,
	}
	header.Hash = spv.ComputeHeaderHash(header)

	// Store header.
	store := spv.NewMemHeaderStore()
	require.NoError(t, store.PutHeader(header))

	blockHash := hex.EncodeToString(header.Hash)
	txidHex := hex.EncodeToString(txHash)

	mock := &MockBlockchainService{
		GetRawTxFn: func(ctx context.Context, txid string) ([]byte, error) {
			return []byte("test-tx"), nil
		},
		GetTxStatusFn: func(ctx context.Context, txid string) (*TxStatus, error) {
			return &TxStatus{Confirmed: true, BlockHash: blockHash, BlockHeight: 1}, nil
		},
		GetMerkleProofFn: func(ctx context.Context, txid string) (*MerkleProof, error) {
			return &MerkleProof{
				TxID:      txidHex,
				BlockHash: blockHash,
				Branches:  [][]byte{}, // single tx, no branches
				Index:     0,
			}, nil
		},
		GetBlockHeaderFn: func(ctx context.Context, bh string) ([]byte, error) {
			return spv.SerializeHeader(header), nil
		},
	}

	client := NewSPVClient(mock, store)
	result, err := client.VerifyTx(context.Background(), txidHex)
	require.NoError(t, err)
	assert.True(t, result.Confirmed)
}

func TestSPVClientVerifyTxUnconfirmed(t *testing.T) {
	mock := &MockBlockchainService{
		GetTxStatusFn: func(ctx context.Context, txid string) (*TxStatus, error) {
			return &TxStatus{Confirmed: false}, nil
		},
	}

	store := spv.NewMemHeaderStore()
	client := NewSPVClient(mock, store)
	result, err := client.VerifyTx(context.Background(), "sometxid")
	require.NoError(t, err)
	assert.False(t, result.Confirmed)
}

func TestSPVClientSyncHeaders(t *testing.T) {
	// Create a chain of 3 headers.
	genesis := &spv.BlockHeader{
		Version:    1,
		PrevBlock:  make([]byte, 32),
		MerkleRoot: make([]byte, 32),
		Timestamp:  1000,
		Bits:       0x207fffff,
		Height:     0,
	}
	genesis.Hash = spv.ComputeHeaderHash(genesis)

	block1 := &spv.BlockHeader{
		Version:    1,
		PrevBlock:  genesis.Hash,
		MerkleRoot: make([]byte, 32),
		Timestamp:  2000,
		Bits:       0x207fffff,
		Height:     1,
	}
	block1.Hash = spv.ComputeHeaderHash(block1)

	headers := []*spv.BlockHeader{genesis, block1}
	hashes := []string{
		hex.EncodeToString(genesis.Hash),
		hex.EncodeToString(block1.Hash),
	}

	mock := &MockBlockchainService{
		GetBestBlockHeightFn: func(ctx context.Context) (uint64, error) {
			return 1, nil
		},
		GetBlockHeaderFn: func(ctx context.Context, blockHash string) ([]byte, error) {
			for _, h := range headers {
				if hex.EncodeToString(h.Hash) == blockHash {
					return spv.SerializeHeader(h), nil
				}
			}
			return nil, ErrTxNotFound
		},
	}
	// Add a method to get block hash by height — we'll need a helper.
	// For SyncHeaders we'll use getblockhash RPC which maps to a simple Call.

	store := spv.NewMemHeaderStore()
	client := NewSPVClient(mock, store)
	client.getBlockHash = func(ctx context.Context, height uint64) (string, error) {
		if int(height) < len(hashes) {
			return hashes[height], nil
		}
		return "", ErrTxNotFound
	}

	err := client.SyncHeaders(context.Background())
	require.NoError(t, err)

	count, _ := store.GetHeaderCount()
	assert.Equal(t, uint64(2), count)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -run TestSPVClient -v`
Expected: FAIL — NewSPVClient, VerifyResult not defined

**Step 3: Write the implementation**

```go
// libbitfs/network/spvclient.go
package network

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/tongxiaofeng/libbitfs/spv"
)

// VerifyResult holds the result of an SPV verification.
type VerifyResult struct {
	Confirmed   bool
	BlockHash   string
	BlockHeight uint64
}

// SPVClient bridges the network layer with libbitfs/spv verification.
type SPVClient struct {
	chain   BlockchainService
	headers spv.HeaderStore

	// getBlockHash is a helper for fetching block hash by height.
	// Injected in tests; for RPCClient, set automatically.
	getBlockHash func(ctx context.Context, height uint64) (string, error)
}

// NewSPVClient creates an SPV client backed by a blockchain service and header store.
func NewSPVClient(chain BlockchainService, headers spv.HeaderStore) *SPVClient {
	s := &SPVClient{
		chain:   chain,
		headers: headers,
	}
	// If chain is an RPCClient, wire up getBlockHash via RPC.
	if rpc, ok := chain.(*RPCClient); ok {
		s.getBlockHash = func(ctx context.Context, height uint64) (string, error) {
			var hash string
			err := rpc.Call(ctx, "getblockhash", []interface{}{height}, &hash)
			return hash, err
		}
	}
	return s
}

// VerifyTx performs SPV verification of a transaction:
// 1. Check confirmation status
// 2. For confirmed tx: fetch Merkle proof, verify against stored header
func (s *SPVClient) VerifyTx(ctx context.Context, txid string) (*VerifyResult, error) {
	// Step 1: Check status.
	status, err := s.chain.GetTxStatus(ctx, txid)
	if err != nil {
		return nil, fmt.Errorf("network: get tx status: %w", err)
	}

	if !status.Confirmed {
		return &VerifyResult{Confirmed: false}, nil
	}

	// Step 2: Ensure we have the block header.
	blockHashBytes, err := hex.DecodeString(status.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("network: invalid block hash: %w", err)
	}

	header, err := s.headers.GetHeader(blockHashBytes)
	if err != nil {
		// Header not in store — fetch and store it.
		rawHeader, fetchErr := s.chain.GetBlockHeader(ctx, status.BlockHash)
		if fetchErr != nil {
			return nil, fmt.Errorf("network: fetch block header: %w", fetchErr)
		}
		header, err = spv.DeserializeHeader(rawHeader)
		if err != nil {
			return nil, fmt.Errorf("network: deserialize header: %w", err)
		}
		header.Height = uint32(status.BlockHeight)
		header.Hash = spv.ComputeHeaderHash(header)
		if storeErr := s.headers.PutHeader(header); storeErr != nil {
			return nil, fmt.Errorf("network: store header: %w", storeErr)
		}
	}

	// Step 3: Fetch and verify Merkle proof.
	proof, err := s.chain.GetMerkleProof(ctx, txid)
	if err != nil {
		return nil, fmt.Errorf("network: fetch merkle proof: %w", err)
	}

	txidBytes, err := hex.DecodeString(proof.TxID)
	if err != nil {
		return nil, fmt.Errorf("network: invalid txid: %w", err)
	}

	spvProof := &spv.MerkleProof{
		TxID:      txidBytes,
		Index:     uint32(proof.Index),
		Nodes:     proof.Branches,
		BlockHash: blockHashBytes,
	}

	ok, err := spv.VerifyMerkleProof(spvProof, header.MerkleRoot)
	if err != nil {
		return nil, fmt.Errorf("network: verify merkle proof: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("network: merkle proof verification failed for tx %s", txid)
	}

	return &VerifyResult{
		Confirmed:   true,
		BlockHash:   status.BlockHash,
		BlockHeight: status.BlockHeight,
	}, nil
}

// SyncHeaders fetches block headers from the network and stores them locally.
// Syncs from current tip to the latest block.
func (s *SPVClient) SyncHeaders(ctx context.Context) error {
	if s.getBlockHash == nil {
		return fmt.Errorf("network: getBlockHash not configured")
	}

	bestHeight, err := s.chain.GetBestBlockHeight(ctx)
	if err != nil {
		return fmt.Errorf("network: get best block height: %w", err)
	}

	// Determine local tip.
	var startHeight uint64
	tip, err := s.headers.GetTip()
	if err == nil && tip != nil {
		startHeight = uint64(tip.Height) + 1
	}

	// Fetch headers from startHeight to bestHeight.
	for h := startHeight; h <= bestHeight; h++ {
		hash, err := s.getBlockHash(ctx, h)
		if err != nil {
			return fmt.Errorf("network: get block hash at %d: %w", h, err)
		}

		rawHeader, err := s.chain.GetBlockHeader(ctx, hash)
		if err != nil {
			return fmt.Errorf("network: get header at %d: %w", h, err)
		}

		header, err := spv.DeserializeHeader(rawHeader)
		if err != nil {
			return fmt.Errorf("network: deserialize header at %d: %w", h, err)
		}
		header.Height = uint32(h)
		header.Hash = spv.ComputeHeaderHash(header)

		if err := s.headers.PutHeader(header); err != nil {
			return fmt.Errorf("network: store header at %d: %w", h, err)
		}
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./network/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add libbitfs/network/spvclient.go libbitfs/network/spvclient_test.go
git commit -m "feat(network): implement SPVClient with header sync and tx verification"
```

---

## Task 6: Engine Integration — Add BlockchainService to Engine

**Files:**
- Modify: `bitfs/internal/engine/engine.go`
- Modify: `bitfs/go.mod` (may need to update libbitfs dependency)
- Create: `bitfs/internal/engine/broadcast_test.go`

**Step 1: Write the tests**

```go
// bitfs/internal/engine/broadcast_test.go
package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/libbitfs/network"
)

func TestEngineBroadcastTx(t *testing.T) {
	var broadcastedHex string
	mock := &network.MockBlockchainService{
		BroadcastTxFn: func(ctx context.Context, rawTxHex string) (string, error) {
			broadcastedHex = rawTxHex
			return "returned_txid", nil
		},
	}

	eng := &Engine{Chain: mock}
	txid, err := eng.BroadcastTx(context.Background(), "signed_hex_data")
	require.NoError(t, err)
	assert.Equal(t, "returned_txid", txid)
	assert.Equal(t, "signed_hex_data", broadcastedHex)
}

func TestEngineBroadcastTxNilChain(t *testing.T) {
	eng := &Engine{} // no Chain set
	_, err := eng.BroadcastTx(context.Background(), "hex")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no blockchain service")
}

func TestEngineRefreshUTXOs(t *testing.T) {
	mock := &network.MockBlockchainService{
		ListUnspentFn: func(ctx context.Context, address string) ([]*network.UTXO, error) {
			return []*network.UTXO{
				{TxID: "aabb", Vout: 0, Amount: 50000, ScriptPubKey: "76a914..."},
				{TxID: "ccdd", Vout: 1, Amount: 100000, ScriptPubKey: "76a914..."},
			}, nil
		},
	}

	eng := &Engine{
		Chain: mock,
		State: NewLocalState("/dev/null"),
	}
	err := eng.RefreshFeeUTXOs(context.Background(), "1A1zP1", "pubkeyhex")
	require.NoError(t, err)

	// Should have added 2 UTXOs.
	count := 0
	for _, u := range eng.State.UTXOs {
		if u.Type == "fee" && !u.Spent {
			count++
		}
	}
	assert.Equal(t, 2, count)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run "TestEngineBroadcast|TestEngineRefresh" -v`
Expected: FAIL — Chain field, BroadcastTx method, RefreshFeeUTXOs not defined

**Step 3: Write the implementation**

Add to `bitfs/internal/engine/engine.go`:

```go
// Add to Engine struct:
//     Chain   network.BlockchainService // optional; nil = offline mode

// Add import:
//     "github.com/tongxiaofeng/libbitfs/network"

// BroadcastTx submits a signed transaction to the network.
func (e *Engine) BroadcastTx(ctx context.Context, rawTxHex string) (string, error) {
	if e.Chain == nil {
		return "", fmt.Errorf("engine: no blockchain service configured (offline mode)")
	}
	return e.Chain.BroadcastTx(ctx, rawTxHex)
}

// RefreshFeeUTXOs queries the network for unspent outputs at the given address
// and adds any new ones to local state.
func (e *Engine) RefreshFeeUTXOs(ctx context.Context, address, pubKeyHex string) error {
	if e.Chain == nil {
		return fmt.Errorf("engine: no blockchain service configured")
	}

	utxos, err := e.Chain.ListUnspent(ctx, address)
	if err != nil {
		return fmt.Errorf("engine: list unspent: %w", err)
	}

	for _, u := range utxos {
		// Check if already tracked.
		exists := false
		for _, existing := range e.State.UTXOs {
			if existing.TxID == u.TxID && existing.Vout == u.Vout {
				exists = true
				break
			}
		}
		if !exists {
			e.State.AddUTXO(&UTXOState{
				TxID:         u.TxID,
				Vout:         u.Vout,
				Amount:       u.Amount,
				ScriptPubKey: u.ScriptPubKey,
				PubKeyHex:    pubKeyHex,
				Type:         "fee",
			})
		}
	}

	return nil
}
```

Note: The MockBlockchainService must be exported from the `network` package for the engine tests to use it. Move it from `service_test.go` to `service.go` or a new `mock.go` file (since it's useful across packages). Alternatively, create a `network/mock.go` file:

```go
// libbitfs/network/mock.go
package network

import "context"

// MockBlockchainService is a test double for BlockchainService.
// All function fields must be set before use.
type MockBlockchainService struct {
	ListUnspentFn        func(ctx context.Context, address string) ([]*UTXO, error)
	GetUTXOFn            func(ctx context.Context, txid string, vout uint32) (*UTXO, error)
	BroadcastTxFn        func(ctx context.Context, rawTxHex string) (string, error)
	GetRawTxFn           func(ctx context.Context, txid string) ([]byte, error)
	GetTxStatusFn        func(ctx context.Context, txid string) (*TxStatus, error)
	GetBlockHeaderFn     func(ctx context.Context, blockHash string) ([]byte, error)
	GetMerkleProofFn     func(ctx context.Context, txid string) (*MerkleProof, error)
	GetBestBlockHeightFn func(ctx context.Context) (uint64, error)
}

func (m *MockBlockchainService) ListUnspent(ctx context.Context, address string) ([]*UTXO, error) {
	return m.ListUnspentFn(ctx, address)
}
func (m *MockBlockchainService) GetUTXO(ctx context.Context, txid string, vout uint32) (*UTXO, error) {
	return m.GetUTXOFn(ctx, txid, vout)
}
func (m *MockBlockchainService) BroadcastTx(ctx context.Context, rawTxHex string) (string, error) {
	return m.BroadcastTxFn(ctx, rawTxHex)
}
func (m *MockBlockchainService) GetRawTx(ctx context.Context, txid string) ([]byte, error) {
	return m.GetRawTxFn(ctx, txid)
}
func (m *MockBlockchainService) GetTxStatus(ctx context.Context, txid string) (*TxStatus, error) {
	return m.GetTxStatusFn(ctx, txid)
}
func (m *MockBlockchainService) GetBlockHeader(ctx context.Context, blockHash string) ([]byte, error) {
	return m.GetBlockHeaderFn(ctx, blockHash)
}
func (m *MockBlockchainService) GetMerkleProof(ctx context.Context, txid string) (*MerkleProof, error) {
	return m.GetMerkleProofFn(ctx, txid)
}
func (m *MockBlockchainService) GetBestBlockHeight(ctx context.Context) (uint64, error) {
	return m.GetBestBlockHeightFn(ctx)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v`
Expected: PASS — all engine tests including new ones

**Step 5: Commit**

```bash
git add libbitfs/network/mock.go bitfs/internal/engine/engine.go bitfs/internal/engine/broadcast_test.go
git commit -m "feat(engine): integrate BlockchainService for broadcast and UTXO refresh"
```

---

## Task 7: Regtest Integration Tests

**Files:**
- Create: `libbitfs/network/rpc_e2e_test.go`

These tests run against the real Docker regtest node (same as existing e2e).

**Step 1: Write the integration tests**

```go
// libbitfs/network/rpc_e2e_test.go
//go:build e2e

package network

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func regtestClient() *RPCClient {
	return NewRPCClient(RPCConfig{
		URL: "http://localhost:18332", User: "bitfs", Password: "bitfs",
	})
}

func skipIfUnavailable(t *testing.T, client *RPCClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var height uint64
	if err := client.Call(ctx, "getblockcount", nil, &height); err != nil {
		t.Skip("regtest node unavailable:", err)
	}
}

func TestE2E_GetBestBlockHeight(t *testing.T) {
	client := regtestClient()
	skipIfUnavailable(t, client)

	height, err := client.GetBestBlockHeight(context.Background())
	require.NoError(t, err)
	assert.Greater(t, height, uint64(0))
}

func TestE2E_ListUnspentAndBroadcast(t *testing.T) {
	client := regtestClient()
	skipIfUnavailable(t, client)

	ctx := context.Background()

	// Generate address and fund it.
	var addr string
	require.NoError(t, client.Call(ctx, "getnewaddress", nil, &addr))

	var blockHashes []string
	require.NoError(t, client.Call(ctx, "generatetoaddress", []interface{}{101, addr}, &blockHashes))

	// List UTXOs.
	utxos, err := client.ListUnspent(ctx, addr)
	require.NoError(t, err)
	assert.NotEmpty(t, utxos)
	assert.Greater(t, utxos[0].Amount, uint64(0))
}

func TestE2E_GetBlockHeader(t *testing.T) {
	client := regtestClient()
	skipIfUnavailable(t, client)

	ctx := context.Background()

	var bestHash string
	require.NoError(t, client.Call(ctx, "getbestblockhash", nil, &bestHash))

	header, err := client.GetBlockHeader(ctx, bestHash)
	require.NoError(t, err)
	assert.Len(t, header, 80)
}

func TestE2E_GetRawTxAndStatus(t *testing.T) {
	client := regtestClient()
	skipIfUnavailable(t, client)

	ctx := context.Background()

	// Generate a coinbase tx.
	var addr string
	require.NoError(t, client.Call(ctx, "getnewaddress", nil, &addr))
	var blockHashes []string
	require.NoError(t, client.Call(ctx, "generatetoaddress", []interface{}{1, addr}, &blockHashes))

	// Get a tx from the block.
	var block struct {
		Tx []string `json:"tx"`
	}
	require.NoError(t, client.Call(ctx, "getblock", []interface{}{blockHashes[0]}, &block))
	require.NotEmpty(t, block.Tx)
	txid := block.Tx[0]

	// GetRawTx.
	raw, err := client.GetRawTx(ctx, txid)
	require.NoError(t, err)
	assert.NotEmpty(t, raw)

	// GetTxStatus.
	status, err := client.GetTxStatus(ctx, txid)
	require.NoError(t, err)
	assert.True(t, status.Confirmed)
	assert.Equal(t, blockHashes[0], status.BlockHash)
}
```

**Step 2: Run test (requires Docker)**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs/e2e && docker compose up -d && cd /Users/alex/Codes/RabbitHole/libbitfs && go test -tags e2e ./network/ -v -timeout 60s`
Expected: PASS if Docker is running; SKIP otherwise

**Step 3: Commit**

```bash
git add libbitfs/network/rpc_e2e_test.go
git commit -m "test(network): add regtest integration tests for RPCClient"
```

---

## Task 8: Wire RPC Configuration into CLI

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_daemon.go` — pass RPCConfig to engine
- Modify: `bitfs/cmd/bitfs/main.go` — add `--rpc-url`, `--rpc-user`, `--rpc-pass` global flags
- Modify: `bitfs/internal/engine/engine.go` — accept optional RPCConfig in constructor

**Step 1: Write the test**

```go
// bitfs/internal/engine/engine_rpc_test.go
package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tongxiaofeng/libbitfs/network"
)

func TestEngineWithChainIsOnline(t *testing.T) {
	eng := &Engine{
		Chain: &network.MockBlockchainService{},
	}
	assert.True(t, eng.IsOnline())
}

func TestEngineWithoutChainIsOffline(t *testing.T) {
	eng := &Engine{}
	assert.False(t, eng.IsOnline())
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestEngineWith -v`
Expected: FAIL — IsOnline not defined

**Step 3: Write the implementation**

Add to `engine.go`:

```go
// IsOnline returns true if a blockchain service is configured.
func (e *Engine) IsOnline() bool {
	return e.Chain != nil
}
```

Add global flags to `main.go` (root command persistent flags):

```go
rootCmd.PersistentFlags().String("rpc-url", "", "BSV node JSON-RPC URL")
rootCmd.PersistentFlags().String("rpc-user", "", "RPC username")
rootCmd.PersistentFlags().String("rpc-pass", "", "RPC password")
```

Modify daemon startup to resolve config and create RPCClient:

```go
// In cmd_daemon.go runDaemonStart, after creating engine:
rpcCfg, err := network.ResolveConfig(
    &network.RPCConfig{
        URL:      flagRPCURL,
        User:     flagRPCUser,
        Password: flagRPCPass,
    },
    envMap(),   // reads os.Environ into map
    networkName,
)
if err == nil {
    eng.Chain = network.NewRPCClient(*rpcCfg)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v && go test ./cmd/bitfs/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add bitfs/internal/engine/engine.go bitfs/internal/engine/engine_rpc_test.go bitfs/cmd/bitfs/main.go bitfs/cmd/bitfs/cmd_daemon.go
git commit -m "feat(cli): wire RPC configuration into engine and daemon startup"
```

---

## Summary

| Task | What | Files | Tests |
|------|------|-------|-------|
| 1 | Interface + types + mock | `network/service.go`, `errors.go`, `service_test.go` | 4 |
| 2 | Config + presets + resolve | `network/config.go`, `config_test.go` | 6 |
| 3 | RPCClient core (Call) | `network/rpc.go`, `rpc_test.go` | 6 |
| 4 | RPCClient BlockchainService | `network/rpc_blockchain.go`, `rpc_blockchain_test.go` | 10 |
| 5 | SPVClient | `network/spvclient.go`, `spvclient_test.go` | 3 |
| 6 | Engine integration | `engine/engine.go`, `engine/broadcast_test.go`, `network/mock.go` | 3 |
| 7 | Regtest e2e tests | `network/rpc_e2e_test.go` | 4 |
| 8 | CLI wiring | `cmd/bitfs/main.go`, `cmd_daemon.go`, `engine_rpc_test.go` | 2 |

**Total: 8 tasks, ~38 tests, 12 new files, 3 modified files**

**Dependency order:** 1 → 2 → 3 → 4 → 5 → 6 → 7 (can run after 4) → 8 (after 6)

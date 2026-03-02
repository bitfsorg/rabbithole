# E2E Multi-Network Testing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Adapt all 25 e2e tests to run on regtest, testnet, and mainnet via a unified TestNode abstraction, controlled by environment variables.

**Architecture:** Extract `RegtestNode` into a `TestNode` interface with two implementations: `regtestNode` (existing logic) and `liveNode` (testnet/mainnet). Test files replace `NewRegtestNode()` with `NewTestNode(t)` and `MineBlocks` with `WaitForConfirmation`. Funding strategy differs by network: regtest mines, testnet tries faucet then WIF wallet, mainnet requires WIF wallet.

**Tech Stack:** Go 1.25.6, go-sdk v1.2.18, stdlib `net/http` for faucet API

**Design doc:** `docs/plans/bitfs/2026-03-02-e2e-multinetwork-design.md`

---

### Task 1: Create `testutil/config.go` — environment variable configuration

**Files:**
- Create: `bitfs/e2e/testutil/config.go`

**Step 1: Write the test**

```go
// bitfs/e2e/testutil/config_test.go
//go:build e2e

package testutil

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear all env vars to test defaults.
	os.Unsetenv("BITFS_E2E_NETWORK")
	os.Unsetenv("BITFS_E2E_RPC_URL")
	os.Unsetenv("BITFS_E2E_RPC_USER")
	os.Unsetenv("BITFS_E2E_RPC_PASS")
	os.Unsetenv("BITFS_E2E_FAUCET_URL")
	os.Unsetenv("BITFS_E2E_FUND_WIF")
	os.Unsetenv("BITFS_E2E_CONFIRM_TIMEOUT")

	cfg := LoadConfig()
	assert.Equal(t, "regtest", cfg.Network)
	assert.Equal(t, "http://localhost:18332", cfg.RPCURL)
	assert.Equal(t, "bitfs", cfg.RPCUser)
	assert.Equal(t, "bitfs", cfg.RPCPass)
	assert.Equal(t, "", cfg.FaucetURL)
	assert.Equal(t, "", cfg.FundWIF)
	assert.Equal(t, 30*time.Second, cfg.ConfirmTimeout)
}

func TestLoadConfig_Testnet(t *testing.T) {
	t.Setenv("BITFS_E2E_NETWORK", "testnet")

	cfg := LoadConfig()
	assert.Equal(t, "testnet", cfg.Network)
	assert.Equal(t, "http://localhost:18333", cfg.RPCURL)
	assert.Equal(t, 30*time.Minute, cfg.ConfirmTimeout)
}

func TestLoadConfig_Mainnet(t *testing.T) {
	t.Setenv("BITFS_E2E_NETWORK", "mainnet")

	cfg := LoadConfig()
	assert.Equal(t, "mainnet", cfg.Network)
	assert.Equal(t, "http://localhost:8332", cfg.RPCURL)
	assert.Equal(t, 60*time.Minute, cfg.ConfirmTimeout)
}

func TestLoadConfig_CustomOverrides(t *testing.T) {
	t.Setenv("BITFS_E2E_NETWORK", "testnet")
	t.Setenv("BITFS_E2E_RPC_URL", "http://remote:9999")
	t.Setenv("BITFS_E2E_CONFIRM_TIMEOUT", "5m")
	t.Setenv("BITFS_E2E_FUND_WIF", "L1abc...")

	cfg := LoadConfig()
	assert.Equal(t, "http://remote:9999", cfg.RPCURL)
	assert.Equal(t, 5*time.Minute, cfg.ConfirmTimeout)
	assert.Equal(t, "L1abc...", cfg.FundWIF)
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test -tags e2e ./e2e/testutil/ -run TestLoadConfig -v`
Expected: FAIL — `LoadConfig` undefined

**Step 3: Write minimal implementation**

```go
// bitfs/e2e/testutil/config.go
//go:build e2e

package testutil

import (
	"os"
	"time"
)

// Config holds e2e test configuration from environment variables.
type Config struct {
	Network        string        // "regtest", "testnet", "mainnet"
	RPCURL         string        // RPC endpoint URL
	RPCUser        string        // RPC username
	RPCPass        string        // RPC password
	FaucetURL      string        // Faucet API URL (testnet only)
	FundWIF        string        // Pre-funded wallet WIF private key
	ConfirmTimeout time.Duration // Timeout waiting for confirmations
}

// networkDefaults maps network names to their default RPC URLs and confirm timeouts.
var networkDefaults = map[string]struct {
	rpcURL         string
	confirmTimeout time.Duration
}{
	"regtest": {"http://localhost:18332", 30 * time.Second},
	"testnet": {"http://localhost:18333", 30 * time.Minute},
	"mainnet": {"http://localhost:8332", 60 * time.Minute},
}

// LoadConfig reads e2e configuration from environment variables with sensible defaults.
func LoadConfig() *Config {
	network := envOr("BITFS_E2E_NETWORK", "regtest")

	defaults, ok := networkDefaults[network]
	if !ok {
		defaults = networkDefaults["regtest"]
	}

	rpcURL := envOr("BITFS_E2E_RPC_URL", defaults.rpcURL)
	confirmTimeout := defaults.confirmTimeout

	if v := os.Getenv("BITFS_E2E_CONFIRM_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			confirmTimeout = d
		}
	}

	return &Config{
		Network:        network,
		RPCURL:         rpcURL,
		RPCUser:        envOr("BITFS_E2E_RPC_USER", "bitfs"),
		RPCPass:        envOr("BITFS_E2E_RPC_PASS", "bitfs"),
		FaucetURL:      os.Getenv("BITFS_E2E_FAUCET_URL"),
		FundWIF:        os.Getenv("BITFS_E2E_FUND_WIF"),
		ConfirmTimeout: confirmTimeout,
	}
}

// IsMainnet returns true if the configured network is mainnet.
func (c *Config) IsMainnet() bool { return c.Network == "mainnet" }

// IsRegtest returns true if the configured network is regtest.
func (c *Config) IsRegtest() bool { return c.Network == "regtest" }

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

**Step 4: Run test to verify it passes**

Run: `cd bitfs && go test -tags e2e ./e2e/testutil/ -run TestLoadConfig -v`
Expected: PASS (4 tests)

**Step 5: Commit**

```bash
git add e2e/testutil/config.go e2e/testutil/config_test.go
git commit -m "feat(e2e): add config.go for multi-network environment configuration"
```

---

### Task 2: Define `TestNode` interface and refactor `regtestNode`

**Files:**
- Modify: `bitfs/e2e/testutil/node.go` — extract interface, rename struct, add `NewTestNode()`
- Create: `bitfs/e2e/testutil/types.go` — shared types (UTXO renamed from RegtestUTXO)

**Step 1: Create `types.go` with shared types**

```go
// bitfs/e2e/testutil/types.go
//go:build e2e

package testutil

// UTXO represents an unspent transaction output from a BSV node.
type UTXO struct {
	TxID          string  `json:"txid"`
	Vout          uint32  `json:"vout"`
	Address       string  `json:"address"`
	ScriptPubKey  string  `json:"scriptPubKey"`
	Amount        float64 `json:"amount"`
	Confirmations int     `json:"confirmations"`
}
```

**Step 2: Refactor `node.go` — extract `TestNode` interface, wrap `regtestNode`**

Key changes to `node.go`:
- Rename `RegtestUTXO` → use `UTXO` from types.go (keep `RegtestUTXO` as alias for backward compat)
- Add `TestNode` interface with all methods
- Rename `RegtestNode` struct to `regtestNode` (unexported)
- Keep `RegtestNode` as exported type alias for backward compat during migration
- Add `WaitForConfirmation()` to regtestNode (mines N blocks)
- Add `Network()` method
- Add `Fund()` method (wraps existing FundAddress logic)
- Add `NewTestNode(t *testing.T)` factory that reads config and returns appropriate implementation

```go
// TestNode is the common interface for all network node implementations.
type TestNode interface {
	Network() string
	IsAvailable(ctx context.Context) bool
	Fund(ctx context.Context, addr string, amount float64) (*UTXO, error)
	WaitForConfirmation(ctx context.Context, txid string, minConf int) error
	SendRawTransaction(ctx context.Context, hex string) (string, error)
	GetRawTransaction(ctx context.Context, txid string) ([]byte, error)
	GetTxOutProof(ctx context.Context, txid string) ([]byte, error)
	GetBlockHeader(ctx context.Context, hash string) ([]byte, error)
	GetBlockHeaderVerbose(ctx context.Context, hash string) (map[string]interface{}, error)
	GetBestBlockHash(ctx context.Context) (string, error)
	GetBlockHash(ctx context.Context, height int) (string, error)
	GetBlockCount(ctx context.Context) (int64, error)
	ImportAddress(ctx context.Context, addr string) error
	ListUnspent(ctx context.Context, addr string) ([]UTXO, error)
	SendToAddress(ctx context.Context, addr string, amount float64) (string, error)
	NewAddress(ctx context.Context) (string, error)
	RPC() *RPCClient
}
```

`NewTestNode(t)` logic:
```go
func NewTestNode(t *testing.T) TestNode {
	t.Helper()
	cfg := LoadConfig()
	rpc := NewRPCClient(cfg.RPCURL, cfg.RPCUser, cfg.RPCPass)

	switch cfg.Network {
	case "regtest":
		node := &regtestNode{rpc: rpc}
		if !node.IsAvailable(context.Background()) {
			t.Skip("BSV regtest node not available")
		}
		return node
	case "testnet", "mainnet":
		node := newLiveNode(rpc, cfg)
		if !node.IsAvailable(context.Background()) {
			t.Skipf("BSV %s node not available at %s", cfg.Network, cfg.RPCURL)
		}
		return node
	default:
		t.Fatalf("unknown network: %s", cfg.Network)
		return nil
	}
}
```

Keep `NewRegtestNode()` and `RegtestNode` as deprecated aliases for backward compat during migration:
```go
type RegtestNode = regtestNode

func NewRegtestNode() *RegtestNode {
	return &regtestNode{rpc: NewRPCClient(defaultRPCURL, defaultRPCUser, defaultRPCPass)}
}
```

**Step 3: Add `WaitForConfirmation` and `Fund` to regtestNode**

```go
func (n *regtestNode) Network() string { return "regtest" }

func (n *regtestNode) RPC() *RPCClient { return n.rpc }

func (n *regtestNode) WaitForConfirmation(ctx context.Context, txid string, minConf int) error {
	addr, err := n.NewAddress(ctx)
	if err != nil {
		return fmt.Errorf("generate mining address: %w", err)
	}
	_, err = n.MineBlocks(ctx, minConf, addr)
	return err
}

func (n *regtestNode) Fund(ctx context.Context, addr string, amount float64) (*UTXO, error) {
	// Mine 101 blocks to make coinbase spendable.
	miningAddr, err := n.NewAddress(ctx)
	if err != nil {
		return nil, fmt.Errorf("generate mining address: %w", err)
	}
	if _, err := n.MineBlocks(ctx, 101, miningAddr); err != nil {
		return nil, fmt.Errorf("mine 101 blocks: %w", err)
	}

	// Send to target address.
	txid, err := n.SendToAddress(ctx, addr, amount)
	if err != nil {
		return nil, fmt.Errorf("send to address: %w", err)
	}

	// Mine 1 confirmation block.
	if _, err := n.MineBlocks(ctx, 1, miningAddr); err != nil {
		return nil, fmt.Errorf("mine confirmation: %w", err)
	}

	// List UTXOs.
	utxos, err := n.ListUnspent(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("list unspent: %w", err)
	}
	if len(utxos) == 0 {
		return nil, fmt.Errorf("no UTXOs found for %s after funding", addr)
	}
	return &utxos[0], nil
}
```

**Step 4: Verify existing tests still pass**

Run: `cd bitfs && go test -tags e2e ./e2e/testutil/ -v`
Expected: PASS (all existing tests plus config tests)

**Step 5: Commit**

```bash
git add e2e/testutil/types.go e2e/testutil/node.go
git commit -m "feat(e2e): define TestNode interface, refactor regtestNode"
```

---

### Task 3: Implement `liveNode` for testnet/mainnet

**Files:**
- Create: `bitfs/e2e/testutil/live_node.go`

**Step 1: Write the test**

```go
// bitfs/e2e/testutil/live_node_test.go
//go:build e2e

package testutil

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLiveNode_Network(t *testing.T) {
	cfg := &Config{Network: "testnet", RPCURL: "http://localhost:18333", RPCUser: "bitfs", RPCPass: "bitfs", ConfirmTimeout: 30 * time.Minute}
	rpc := NewRPCClient(cfg.RPCURL, cfg.RPCUser, cfg.RPCPass)
	node := newLiveNode(rpc, cfg)
	assert.Equal(t, "testnet", node.Network())
}

func TestLiveNode_MainnetNetwork(t *testing.T) {
	cfg := &Config{Network: "mainnet", RPCURL: "http://localhost:8332", RPCUser: "bitfs", RPCPass: "bitfs", ConfirmTimeout: 60 * time.Minute}
	rpc := NewRPCClient(cfg.RPCURL, cfg.RPCUser, cfg.RPCPass)
	node := newLiveNode(rpc, cfg)
	assert.Equal(t, "mainnet", node.Network())
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test -tags e2e ./e2e/testutil/ -run TestLiveNode -v`
Expected: FAIL — `newLiveNode` undefined

**Step 3: Write implementation**

```go
// bitfs/e2e/testutil/live_node.go
//go:build e2e

package testutil

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"
)

// liveNode provides TestNode for testnet and mainnet.
// Unlike regtestNode, it cannot mine blocks, so funding and confirmation
// use external strategies (faucet, WIF wallet, polling).
type liveNode struct {
	rpc     *RPCClient
	config  *Config
}

func newLiveNode(rpc *RPCClient, cfg *Config) *liveNode {
	return &liveNode{rpc: rpc, config: cfg}
}

func (n *liveNode) Network() string { return n.config.Network }
func (n *liveNode) RPC() *RPCClient { return n.rpc }

func (n *liveNode) IsAvailable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var hash string
	err := n.rpc.Call(ctx, "getbestblockhash", nil, &hash)
	return err == nil && hash != ""
}

// Fund sends coins to addr. Tries faucet first, then WIF wallet.
// On mainnet, WIF is required.
func (n *liveNode) Fund(ctx context.Context, addr string, amount float64) (*UTXO, error) {
	// Try faucet first (testnet only, if configured).
	if n.config.FaucetURL != "" && !n.config.IsMainnet() {
		if err := faucetFund(ctx, n.config.FaucetURL, addr, amount); err == nil {
			// Wait for the faucet tx to appear in UTXOs.
			if utxo, err := n.waitForUTXO(ctx, addr); err == nil {
				return utxo, nil
			}
		}
		// Faucet failed, fall through to WIF.
	}

	// Use WIF wallet.
	if n.config.FundWIF == "" {
		return nil, fmt.Errorf("no funding source: set BITFS_E2E_FAUCET_URL or BITFS_E2E_FUND_WIF for %s", n.config.Network)
	}

	txid, err := fundFromWIF(ctx, n.rpc, n.config.FundWIF, addr, amount)
	if err != nil {
		return nil, fmt.Errorf("fund from WIF: %w", err)
	}

	// Wait for confirmation.
	if err := n.WaitForConfirmation(ctx, txid, 1); err != nil {
		return nil, fmt.Errorf("wait for funding confirmation: %w", err)
	}

	return n.waitForUTXO(ctx, addr)
}

// WaitForConfirmation polls gettransaction until the tx has >= minConf confirmations.
func (n *liveNode) WaitForConfirmation(ctx context.Context, txid string, minConf int) error {
	deadline := time.After(n.config.ConfirmTimeout)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for %d confirmations on %s (network=%s)", minConf, txid, n.config.Network)
		case <-ticker.C:
			conf, err := n.getConfirmations(ctx, txid)
			if err != nil {
				continue // tx might not be indexed yet
			}
			if conf >= minConf {
				return nil
			}
		}
	}
}

// getConfirmations returns the number of confirmations for a txid.
func (n *liveNode) getConfirmations(ctx context.Context, txid string) (int, error) {
	var result map[string]interface{}
	// getrawtransaction txid verbose=true
	params := []interface{}{txid, true}
	if err := n.rpc.Call(ctx, "getrawtransaction", params, &result); err != nil {
		return 0, err
	}
	conf, ok := result["confirmations"].(float64)
	if !ok {
		return 0, nil // unconfirmed
	}
	return int(conf), nil
}

// waitForUTXO polls ListUnspent until at least one UTXO appears for addr.
func (n *liveNode) waitForUTXO(ctx context.Context, addr string) (*UTXO, error) {
	deadline := time.After(n.config.ConfirmTimeout)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for UTXO at %s", addr)
		case <-ticker.C:
			utxos, err := n.ListUnspent(ctx, addr)
			if err != nil {
				continue
			}
			if len(utxos) > 0 {
				return &utxos[0], nil
			}
		}
	}
}

// --- Delegated RPC methods (same as regtestNode) ---

func (n *liveNode) NewAddress(ctx context.Context) (string, error) {
	var addr string
	if err := n.rpc.Call(ctx, "getnewaddress", nil, &addr); err != nil {
		return "", fmt.Errorf("getnewaddress: %w", err)
	}
	return addr, nil
}

func (n *liveNode) SendRawTransaction(ctx context.Context, rawTxHex string) (string, error) {
	var txid string
	if err := n.rpc.Call(ctx, "sendrawtransaction", []interface{}{rawTxHex}, &txid); err != nil {
		return "", fmt.Errorf("sendrawtransaction: %w", err)
	}
	return txid, nil
}

func (n *liveNode) GetRawTransaction(ctx context.Context, txid string) ([]byte, error) {
	var rawHex string
	if err := n.rpc.Call(ctx, "getrawtransaction", []interface{}{txid, false}, &rawHex); err != nil {
		return nil, fmt.Errorf("getrawtransaction(%s): %w", txid, err)
	}
	return hex.DecodeString(rawHex)
}

func (n *liveNode) GetBlockHeader(ctx context.Context, blockHash string) ([]byte, error) {
	var headerHex string
	if err := n.rpc.Call(ctx, "getblockheader", []interface{}{blockHash, false}, &headerHex); err != nil {
		return nil, fmt.Errorf("getblockheader(%s): %w", blockHash, err)
	}
	return hex.DecodeString(headerHex)
}

func (n *liveNode) GetBlockHeaderVerbose(ctx context.Context, blockHash string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := n.rpc.Call(ctx, "getblockheader", []interface{}{blockHash, true}, &result); err != nil {
		return nil, fmt.Errorf("getblockheader verbose(%s): %w", blockHash, err)
	}
	return result, nil
}

func (n *liveNode) GetTxOutProof(ctx context.Context, txid string) ([]byte, error) {
	var proofHex string
	if err := n.rpc.Call(ctx, "gettxoutproof", []interface{}{[]string{txid}}, &proofHex); err != nil {
		return nil, fmt.Errorf("gettxoutproof(%s): %w", txid, err)
	}
	return hex.DecodeString(proofHex)
}

func (n *liveNode) GetBestBlockHash(ctx context.Context) (string, error) {
	var hash string
	if err := n.rpc.Call(ctx, "getbestblockhash", nil, &hash); err != nil {
		return "", fmt.Errorf("getbestblockhash: %w", err)
	}
	return hash, nil
}

func (n *liveNode) GetBlockHash(ctx context.Context, height int) (string, error) {
	var hash string
	if err := n.rpc.Call(ctx, "getblockhash", []interface{}{height}, &hash); err != nil {
		return "", fmt.Errorf("getblockhash(%d): %w", height, err)
	}
	return hash, nil
}

func (n *liveNode) GetBlockCount(ctx context.Context) (int64, error) {
	var count int64
	if err := n.rpc.Call(ctx, "getblockcount", nil, &count); err != nil {
		return 0, fmt.Errorf("getblockcount: %w", err)
	}
	return count, nil
}

func (n *liveNode) ImportAddress(ctx context.Context, addr string) error {
	return n.rpc.Call(ctx, "importaddress", []interface{}{addr, "", false}, nil)
}

func (n *liveNode) ListUnspent(ctx context.Context, addr string) ([]UTXO, error) {
	var utxos []UTXO
	if err := n.rpc.Call(ctx, "listunspent", []interface{}{1, 9999999, []string{addr}}, &utxos); err != nil {
		return nil, fmt.Errorf("listunspent(%s): %w", addr, err)
	}
	return utxos, nil
}

func (n *liveNode) SendToAddress(ctx context.Context, addr string, amount float64) (string, error) {
	var txid string
	if err := n.rpc.Call(ctx, "sendtoaddress", []interface{}{addr, amount}, &txid); err != nil {
		return "", fmt.Errorf("sendtoaddress: %w", err)
	}
	return txid, nil
}
```

**Step 4: Run tests**

Run: `cd bitfs && go test -tags e2e ./e2e/testutil/ -run TestLiveNode -v`
Expected: PASS

**Step 5: Commit**

```bash
git add e2e/testutil/live_node.go e2e/testutil/live_node_test.go
git commit -m "feat(e2e): implement liveNode for testnet/mainnet"
```

---

### Task 4: Implement `faucet.go` and `funder.go`

**Files:**
- Create: `bitfs/e2e/testutil/faucet.go`
- Create: `bitfs/e2e/testutil/funder.go`

**Step 1: Write `faucet.go`**

```go
// bitfs/e2e/testutil/faucet.go
//go:build e2e

package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// faucetFund requests testnet coins from a faucet API.
// The faucet is expected to accept POST with JSON body {"address": addr, "amount": amount}.
// Returns nil on success, error on failure.
func faucetFund(ctx context.Context, faucetURL, addr string, amount float64) error {
	body, err := json.Marshal(map[string]interface{}{
		"address": addr,
		"amount":  amount,
	})
	if err != nil {
		return fmt.Errorf("marshal faucet request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, faucetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create faucet request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("faucet request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("faucet returned status %d", resp.StatusCode)
	}
	return nil
}
```

**Step 2: Write `funder.go`**

```go
// bitfs/e2e/testutil/funder.go
//go:build e2e

package testutil

import (
	"context"
	"fmt"
)

// fundFromWIF sends amount BSV from the WIF-imported wallet to addr.
// It imports the WIF into the node wallet and uses sendtoaddress.
func fundFromWIF(ctx context.Context, rpc *RPCClient, wif, addr string, amount float64) (string, error) {
	// Import the WIF private key into the node's wallet (rescan=false).
	if err := rpc.Call(ctx, "importprivkey", []interface{}{wif, "", false}, nil); err != nil {
		return "", fmt.Errorf("importprivkey: %w", err)
	}

	// Send from the imported wallet to target address.
	var txid string
	if err := rpc.Call(ctx, "sendtoaddress", []interface{}{addr, amount}, &txid); err != nil {
		return "", fmt.Errorf("sendtoaddress from WIF: %w", err)
	}

	return txid, nil
}
```

**Step 3: Run all testutil tests**

Run: `cd bitfs && go test -tags e2e ./e2e/testutil/ -v`
Expected: PASS (compiles)

**Step 4: Commit**

```bash
git add e2e/testutil/faucet.go e2e/testutil/funder.go
git commit -m "feat(e2e): add faucet and WIF funding strategies"
```

---

### Task 5: Adapt `engine_helpers.go` to use `TestNode` interface

**Files:**
- Modify: `bitfs/e2e/testutil/engine_helpers.go`

**Step 1: Change function signatures**

Replace `*RegtestNode` parameter with `TestNode` in both `SetupTestEngine` and `FundEngineWallet`.

Key changes:
- `FundEngineWallet(t, eng, node *RegtestNode)` → `FundEngineWallet(t, eng, node TestNode)`
- Use `node.Network()` to select `wallet.NetworkConfig` (regtest/testnet/mainnet)
- Use `node.Fund()` instead of manual MineBlocks+SendToAddress
- Use `NewAddressFromPublicKey(pubkey, node.Network() == "mainnet")` for correct address version

**Step 2: Update `SetupTestEngine` to accept network config**

```go
func SetupTestEngine(t *testing.T, node TestNode) (*vault.Vault, string) {
	// ...
	netCfg := networkConfigForNode(node)
	w, err := wallet.NewWallet(seed, netCfg)
	// ...
}

func networkConfigForNode(node TestNode) *wallet.NetworkConfig {
	switch node.Network() {
	case "mainnet":
		return &wallet.MainNet
	case "testnet":
		return &wallet.TestNet
	default:
		return &wallet.RegTest
	}
}
```

**Step 3: Verify compilation**

Run: `cd bitfs && go build -tags e2e ./e2e/testutil/`
Expected: compiles (test files may have errors until Task 6)

**Step 4: Commit**

```bash
git add e2e/testutil/engine_helpers.go
git commit -m "feat(e2e): adapt engine_helpers to use TestNode interface"
```

---

### Task 6: Adapt shared test helpers in `02_metanet_root_test.go`

**Files:**
- Modify: `bitfs/e2e/02_metanet_root_test.go` — update `setupFundedWallet` and `getFundedUTXO`

**Step 1: Update helper signatures**

```go
// Change from:
func setupFundedWallet(t *testing.T, ctx context.Context, node *testutil.RegtestNode) *wallet.Wallet
func getFundedUTXO(t *testing.T, ctx context.Context, node *testutil.RegtestNode, addr string, kp *wallet.KeyPair) *tx.UTXO

// To:
func setupFundedWallet(t *testing.T, ctx context.Context, node testutil.TestNode) *wallet.Wallet
func getFundedUTXO(t *testing.T, ctx context.Context, node testutil.TestNode, addr string, kp *wallet.KeyPair) *tx.UTXO
```

Key changes in `setupFundedWallet`:
- `wallet.NewWallet(seed, &wallet.RegTest)` → select network config based on `node.Network()`

Key changes in `getFundedUTXO`:
- Replace the manual MineBlocks+SendToAddress+MineBlocks+ListUnspent sequence with `node.Fund(ctx, addr, 0.01)` and then `node.ListUnspent()`
- Or simpler: keep using `node.ImportAddress` + `node.Fund()` (which handles everything)

Key changes in `TestMetanetRootTx`:
- `node := testutil.NewRegtestNode()` → `node := testutil.NewTestNode(t)`
- `testutil.SkipIfUnavailable(t, node)` → removed (NewTestNode already skips)
- `node.MineBlocks(ctx, 1, nodeAddr)` → `node.WaitForConfirmation(ctx, broadcastTxID, 1)`

**Step 2: Update `NewAddressFromPublicKey` calls**

```go
// Change from:
addr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)

// To:
addr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, node.Network() == "mainnet")
```

**Step 3: Run test**

Run: `cd bitfs && go test -tags e2e ./e2e/ -run TestMetanetRootTx -v`
Expected: PASS (on regtest)

**Step 4: Commit**

```bash
git add e2e/02_metanet_root_test.go
git commit -m "feat(e2e): adapt metanet root test and shared helpers for TestNode"
```

---

### Task 7: Adapt `01_wallet_fund_test.go`

**Files:**
- Modify: `bitfs/e2e/01_wallet_fund_test.go`

**Step 1: Apply the standard migration pattern**

1. `testutil.NewRegtestNode()` → `testutil.NewTestNode(t)`
2. Remove `testutil.SkipIfUnavailable(t, node)`
3. `wallet.NewWallet(seed, &wallet.RegTest)` → select by `node.Network()`
4. `script.NewAddressFromPublicKey(feeKey.PublicKey, false)` → `..., node.Network() == "mainnet")`
5. Replace manual `MineBlocks(101)+SendToAddress+MineBlocks(1)+ListUnspent` with `node.ImportAddress(ctx, addr) + node.Fund(ctx, addr, 0.01) + node.ListUnspent(...)`

**Step 2: Run test**

Run: `cd bitfs && go test -tags e2e ./e2e/ -run TestWalletFund -v`
Expected: PASS

**Step 3: Commit**

```bash
git add e2e/01_wallet_fund_test.go
git commit -m "feat(e2e): adapt wallet fund test for multi-network"
```

---

### Task 8: Adapt tests 03-07 (core tests)

**Files:**
- Modify: `bitfs/e2e/03_mkdir_upload_test.go`
- Modify: `bitfs/e2e/04_spv_verify_test.go`
- Modify: `bitfs/e2e/05_free_content_test.go`
- Modify: `bitfs/e2e/06_paid_purchase_test.go`
- Modify: `bitfs/e2e/07_full_lifecycle_test.go`

**Apply the standard migration pattern to each file:**

1. `testutil.NewRegtestNode()` → `testutil.NewTestNode(t)`
2. Remove `testutil.SkipIfUnavailable`
3. All `wallet.RegTest` → network-aware selection
4. All `NewAddressFromPublicKey(..., false)` → `..., node.Network() == "mainnet")`
5. All `node.MineBlocks(ctx, N, addr)` used for confirmation → `node.WaitForConfirmation(ctx, txid, N)`
6. All `mineOneBlock(t)` helper closures → replace with `node.WaitForConfirmation(ctx, lastTxID, 1)`
7. All `node.MineBlocks(ctx, 101, addr)` used for funding → use `node.Fund()` via `getFundedUTXO`
8. `callRPC(node, ...)` in 04_spv_verify → use `node.RPC().Call(...)` instead

**Special cases:**
- `04_spv_verify_test.go` uses `callRPC(node, ...)` with hardcoded localhost:18332 → replace with `node.RPC().Call(...)`
- `05_free_content_test.go` and `06_paid_purchase_test.go` may not use regtest node directly for some sub-tests → only adapt where `node` is used

**Run all:**

Run: `cd bitfs && go test -tags e2e ./e2e/ -run "TestMkdirUpload|TestSPVVerify|TestFreeContent|TestPaidPurchase|TestFullLifecycle" -v -timeout 300s`
Expected: PASS (on regtest)

**Commit:**

```bash
git add e2e/03_mkdir_upload_test.go e2e/04_spv_verify_test.go e2e/05_free_content_test.go e2e/06_paid_purchase_test.go e2e/07_full_lifecycle_test.go
git commit -m "feat(e2e): adapt core tests 03-07 for multi-network"
```

---

### Task 9: Adapt tests 08-13 (DAG mutation + access control)

**Files:**
- Modify: `bitfs/e2e/08_move_rename_test.go`
- Modify: `bitfs/e2e/09_copy_test.go`
- Modify: `bitfs/e2e/10_remove_test.go`
- Modify: `bitfs/e2e/11_link_test.go`
- Modify: `bitfs/e2e/12_encrypt_transition_test.go`
- Modify: `bitfs/e2e/13_sell_pricing_test.go`

**Same migration pattern as Task 8.** These tests follow the same structure: create wallet, fund, build txs, mine blocks.

**Run:**

Run: `cd bitfs && go test -tags e2e ./e2e/ -run "TestMove|TestCopy|TestRemove|TestLink|TestEncrypt|TestSell" -v -timeout 300s`
Expected: PASS

**Commit:**

```bash
git add e2e/08_move_rename_test.go e2e/09_copy_test.go e2e/10_remove_test.go e2e/11_link_test.go e2e/12_encrypt_transition_test.go e2e/13_sell_pricing_test.go
git commit -m "feat(e2e): adapt DAG mutation tests 08-13 for multi-network"
```

---

### Task 10: Adapt tests 14-16 (vault + wallet)

**Files:**
- Modify: `bitfs/e2e/14_vault_crud_test.go`
- Modify: `bitfs/e2e/15_multi_vault_test.go`
- Modify: `bitfs/e2e/16_fund_external_test.go`

**Same pattern.** Tests 14-15 don't use regtest node directly (no `Requires: Regtest` in README), but they use `wallet.RegTest` which needs to be network-aware. Test 16 uses regtest for funding.

**Run:**

Run: `cd bitfs && go test -tags e2e ./e2e/ -run "TestVault|TestMultiVault|TestFundExternal" -v -timeout 300s`
Expected: PASS

**Commit:**

```bash
git add e2e/14_vault_crud_test.go e2e/15_multi_vault_test.go e2e/16_fund_external_test.go
git commit -m "feat(e2e): adapt vault/wallet tests 14-16 for multi-network"
```

---

### Task 11: Adapt tests 17-21 (daemon HTTP API)

**Files:**
- Modify: `bitfs/e2e/17_daemon_handshake_test.go`
- Modify: `bitfs/e2e/18_daemon_buy_api_test.go`
- Modify: `bitfs/e2e/19_daemon_content_neg_test.go`
- Modify: `bitfs/e2e/20_daemon_paymail_test.go`
- Modify: `bitfs/e2e/21_daemon_spv_endpoint_test.go`

These tests don't require a regtest node (no `Requires: Regtest` in README) but use `wallet.RegTest`. Only need to replace wallet network config references.

**Run:**

Run: `cd bitfs && go test -tags e2e ./e2e/ -run "TestDaemon" -v -timeout 300s`
Expected: PASS

**Commit:**

```bash
git add e2e/17_daemon_handshake_test.go e2e/18_daemon_buy_api_test.go e2e/19_daemon_content_neg_test.go e2e/20_daemon_paymail_test.go e2e/21_daemon_spv_endpoint_test.go
git commit -m "feat(e2e): adapt daemon API tests 17-21 for multi-network"
```

---

### Task 12: Adapt tests 22-25 (client + integration)

**Files:**
- Modify: `bitfs/e2e/22_client_roundtrip_test.go`
- Modify: `bitfs/e2e/23_error_paths_test.go`
- Modify: `bitfs/e2e/24_large_file_test.go`
- Modify: `bitfs/e2e/25_self_update_chain_test.go`

**Same pattern.** Test 23 (error paths) and 25 (self-update chain) use regtest heavily. Test 22 and 24 use `wallet.RegTest`.

**Run:**

Run: `cd bitfs && go test -tags e2e ./e2e/ -run "TestClient|TestError|TestLargeFile|TestSelfUpdate" -v -timeout 300s`
Expected: PASS

**Commit:**

```bash
git add e2e/22_client_roundtrip_test.go e2e/23_error_paths_test.go e2e/24_large_file_test.go e2e/25_self_update_chain_test.go
git commit -m "feat(e2e): adapt client/integration tests 22-25 for multi-network"
```

---

### Task 13: Remove deprecated backward compat and clean up

**Files:**
- Modify: `bitfs/e2e/testutil/node.go` — remove `NewRegtestNode`, `RegtestNode` alias, `SkipIfUnavailable`, `FundAddress`, `RegtestUTXO`
- Delete: `bitfs/e2e/docker-compose.stn.yml`
- Delete: `bitfs/e2e/bitcoin-stn.conf`

**Step 1: Search for any remaining references to deprecated functions**

Run: `cd bitfs && grep -rn "NewRegtestNode\|SkipIfUnavailable\|RegtestNode\|RegtestUTXO\|FundAddress" e2e/`
Expected: Only in `testutil/node.go` (the definitions)

**Step 2: Remove deprecated code from node.go**

Remove:
- `type RegtestUTXO` (replaced by `UTXO` in types.go)
- `func NewRegtestNode()` (replaced by `NewTestNode`)
- `func SkipIfUnavailable()` (NewTestNode handles this)
- `func FundAddress()` (replaced by `Fund()` on the interface)
- `type RegtestNode = regtestNode` alias

**Step 3: Delete STN files**

```bash
rm e2e/docker-compose.stn.yml e2e/bitcoin-stn.conf
```

**Step 4: Run all tests**

Run: `cd bitfs && go test -tags e2e ./e2e/... -v -timeout 300s`
Expected: PASS (all 25 test files compile and pass on regtest)

**Step 5: Commit**

```bash
git add -A e2e/
git commit -m "feat(e2e): remove deprecated regtest-only code, delete STN config"
```

---

### Task 14: Update README and documentation

**Files:**
- Modify: `bitfs/e2e/README.md`

**Step 1: Update README with multi-network instructions**

Add sections for:
- Environment variables table
- Running on testnet (`BITFS_E2E_NETWORK=testnet`)
- Running on mainnet (`BITFS_E2E_NETWORK=mainnet BITFS_E2E_FUND_WIF=...`)
- Faucet configuration
- Timeout expectations per network

**Step 2: Commit**

```bash
git add e2e/README.md
git commit -m "docs(e2e): update README with multi-network testing instructions"
```

---

### Task 15: Full regression test on regtest

**Step 1: Run the complete test suite**

```bash
cd bitfs/e2e && docker compose up -d
cd .. && go test -tags e2e ./e2e/... -v -timeout 300s -count=1
```

Expected: All 25 test files pass with no regressions.

**Step 2: Verify clean compilation without e2e tag**

```bash
cd bitfs && go build ./...
go test ./... -count=1
```

Expected: No compilation errors, regular tests pass.

**Step 3: Final commit if any fixes needed**

```bash
git commit -m "fix(e2e): address regression issues from multi-network migration"
```

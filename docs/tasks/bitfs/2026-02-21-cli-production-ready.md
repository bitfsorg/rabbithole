# BitFS CLI Production-Ready Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Take the BitFS CLI from write-path-only to production-ready with complete read path, payment flow, missing commands, security hardening, and documentation.

**Architecture:** 8 phases, dependency-ordered. Phase 0 fixes correctness bugs. Phase 1 builds `internal/client/` HTTP client + completes daemon endpoints. Phase 2 replaces all b-tool stubs with real implementations. Phase 3 wires the x402 payment flow end-to-end. Phase 4 adds missing commands (cp, cross-dir mv, lcd, unpublish). Phase 5 implements real Paymail/DNSLink binding. Phase 6 is security audit. Phase 7 is documentation.

**Tech Stack:** Go 1.25.6, `github.com/bsv-blockchain/go-sdk` v1.2.18, `golang.org/x/crypto` v0.47.0

---

## Phase 0: Correctness Fixes

### Task 1: Fix ripemd160Hash to use real HASH160

**Files:**
- Modify: `internal/engine/helpers.go:38-57`
- Test: `internal/engine/helpers_test.go` (create)

**Step 1: Write the failing test**

Create `internal/engine/helpers_test.go`:

```go
package engine

import (
	"encoding/hex"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/primitives/hash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPubKeyHash_MatchesGoSDK(t *testing.T) {
	// Generate a known key and verify our pubKeyHash matches go-sdk's Hash160.
	privKey, err := ec.NewPrivateKey()
	require.NoError(t, err)

	pub := privKey.PubKey()
	got := pubKeyHash(pub)

	// go-sdk's canonical Hash160 = RIPEMD160(SHA256(data))
	want := hash.Hash160(pub.Compressed())

	assert.Equal(t, want, got, "pubKeyHash must match go-sdk Hash160")
	assert.Len(t, got, 20)
}

func TestRipemd160Hash_KnownVector(t *testing.T) {
	// SHA256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	// RIPEMD160(SHA256("")) = b472a266d0bd89c13706a4132ccfb16f7c3b9fcb
	input, _ := hex.DecodeString("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	got := hex.EncodeToString(ripemd160Hash(input))
	assert.Equal(t, "b472a266d0bd89c13706a4132ccfb16f7c3b9fcb", got)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestPubKeyHash_MatchesGoSDK -v -count=1`
Expected: FAIL — current impl uses SHA256[:20] which won't match Hash160.

**Step 3: Fix the implementation**

In `internal/engine/helpers.go`, replace the `ripemd160Hash` function:

```go
import (
	"github.com/bsv-blockchain/go-sdk/primitives/hash"
)

// ripemd160Hash computes RIPEMD160 of the input using go-sdk's implementation.
func ripemd160Hash(data []byte) []byte {
	// hash.Ripemd160 computes RIPEMD160. For HASH160 the caller passes SHA256 output.
	return hash.Ripemd160(data)
}
```

Note: Check if `hash.Ripemd160` exists. If not, use `hash.Hash160` directly in `pubKeyHash` and skip the intermediate function:

```go
func pubKeyHash(pub *ec.PublicKey) []byte {
	return hash.Hash160(pub.Compressed())
}
```

Remove `ripemd160Hash` entirely if `pubKeyHash` is its only caller.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run "TestPubKeyHash|TestRipemd160" -v -count=1`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./... -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/engine/helpers.go internal/engine/helpers_test.go
git commit -m "fix(engine): use real HASH160 for change address derivation

Replace SHA256-truncated placeholder with go-sdk's hash.Hash160.
Fixes incorrect change output script hashes in all transactions."
```

---

### Task 2: Remove dead code pathToIndices

**Files:**
- Modify: `cmd/bitfs/cmd_wallet.go:318-330`

**Step 1: Verify it's unused**

Run: `grep -rn "pathToIndices" bitfs/`
Expected: Only the definition in cmd_wallet.go, no callers.

**Step 2: Delete the function**

Remove lines 318-330 from `cmd/bitfs/cmd_wallet.go` (the `pathToIndices` function and its comment).

**Step 3: Verify compilation**

Run: `go build ./cmd/bitfs`
Expected: Success

**Step 4: Commit**

```bash
git add cmd/bitfs/cmd_wallet.go
git commit -m "chore: remove unused pathToIndices stub"
```

---

## Phase 1: Client Library + Daemon Completion

### Task 3: Create internal/client package with core types and GetMeta

**Files:**
- Create: `internal/client/client.go`
- Create: `internal/client/client_test.go`

**Step 1: Write the failing test**

Create `internal/client/client_test.go`:

```go
package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMeta_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/_bitfs/meta/")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MetaResponse{
			PNode:    "02abc123",
			Type:     "file",
			Path:     "/hello.txt",
			MimeType: "text/plain",
			FileSize: 42,
			Access:   "free",
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	meta, err := c.GetMeta("02abc123", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "file", meta.Type)
	assert.Equal(t, uint64(42), meta.FileSize)
	assert.Equal(t, "free", meta.Access)
}

func TestGetMeta_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": "NOT_FOUND", "message": "not found"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetMeta("02abc123", "/missing.txt")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestNew_DefaultTimeout(t *testing.T) {
	c := New("http://localhost:8080")
	assert.Equal(t, "http://localhost:8080", c.BaseURL)
	assert.NotNil(t, c.HTTPClient)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -v -count=1`
Expected: FAIL — package doesn't exist.

**Step 3: Implement**

Create `internal/client/client.go`:

```go
// Package client provides an HTTP client for the BitFS daemon API.
// Used by b-tools (bls, bcat, bget, bstat, btree) to read content.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var (
	ErrNotFound        = errors.New("client: not found")
	ErrPaymentRequired = errors.New("client: payment required")
	ErrTimeout         = errors.New("client: request timeout")
	ErrNetwork         = errors.New("client: network error")
	ErrServer          = errors.New("client: server error")
)

// MetaResponse holds node metadata returned by the daemon.
type MetaResponse struct {
	PNode      string          `json:"pnode"`
	Type       string          `json:"type"`
	Path       string          `json:"path"`
	MimeType   string          `json:"mime_type,omitempty"`
	FileSize   uint64          `json:"file_size,omitempty"`
	KeyHash    string          `json:"key_hash,omitempty"`
	Access     string          `json:"access"`
	PricePerKB uint64          `json:"price_per_kb,omitempty"`
	TxID       string          `json:"txid,omitempty"`
	Children   []ChildEntry    `json:"children,omitempty"`
}

// ChildEntry is a directory child in metadata responses.
type ChildEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// PaymentInfo holds 402 payment details returned by the daemon.
type PaymentInfo struct {
	PricePerKB uint64 `json:"price_per_kb"`
	FileSize   uint64 `json:"file_size"`
	InvoiceID  string `json:"invoice_id,omitempty"`
}

// Client is an HTTP client for the BitFS daemon.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// New creates a Client with default settings.
func New(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithTimeout returns a copy with the given timeout.
func (c *Client) WithTimeout(d time.Duration) *Client {
	return &Client{
		BaseURL: c.BaseURL,
		HTTPClient: &http.Client{
			Timeout: d,
		},
	}
}

// GetMeta fetches node metadata from /_bitfs/meta/{pnode}/{path}.
func (c *Client) GetMeta(pnode, path string) (*MetaResponse, error) {
	u := fmt.Sprintf("%s/_bitfs/meta/%s/%s", c.BaseURL, pnode, url.PathEscape(path))
	resp, err := c.HTTPClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var meta MetaResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("client: decode meta: %w", err)
	}
	return &meta, nil
}

// GetData fetches raw encrypted data from /_bitfs/data/{hash}.
// Caller is responsible for closing the returned ReadCloser.
func (c *Client) GetData(hash string) (io.ReadCloser, error) {
	u := fmt.Sprintf("%s/_bitfs/data/%s", c.BaseURL, hash)
	resp, err := c.HTTPClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}

	if err := checkStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}

	return resp.Body, nil
}

// checkStatus maps HTTP status codes to sentinel errors.
func checkStatus(resp *http.Response) error {
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusPaymentRequired:
		return ErrPaymentRequired
	case resp.StatusCode == http.StatusRequestTimeout:
		return ErrTimeout
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: HTTP %d", ErrServer, resp.StatusCode)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("client: HTTP %d: %s", resp.StatusCode, body)
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/client/ -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/client/
git commit -m "feat(client): add HTTP client library for daemon API

Provides GetMeta and GetData with error mapping.
Foundation for b-tools read path."
```

---

### Task 4: Complete daemon handleMeta with real Metanet lookup

**Files:**
- Modify: `internal/daemon/content.go:61-79`
- Modify: `internal/daemon/routes.go:27` (add path-less meta route)
- Test: `internal/daemon/content_test.go` (create or extend)

**Step 1: Write the failing test**

Create `internal/daemon/meta_test.go`:

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockMetanet struct {
	nodes map[string]*NodeInfo
}

func (m *mockMetanet) GetNodeByPath(path string) (*NodeInfo, error) {
	n, ok := m.nodes[path]
	if !ok {
		return nil, ErrContentNotFound
	}
	return n, nil
}

func TestHandleMeta_ReturnsFullNodeInfo(t *testing.T) {
	node := &NodeInfo{
		PNode:    []byte{0x02, 0xab},
		Type:     "file",
		MimeType: "text/plain",
		FileSize: 100,
		Access:   "free",
	}
	meta := &mockMetanet{nodes: map[string]*NodeInfo{"/hello.txt": node}}
	cfg := DefaultConfig()
	d, err := New(cfg, &mockWallet{}, &mockStore{data: map[string][]byte{}}, meta)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/_bitfs/meta/02ab/hello.txt", nil)
	w := httptest.NewRecorder()
	d.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "file", resp["type"])
	assert.Equal(t, "free", resp["access"])
	assert.Equal(t, float64(100), resp["file_size"])
}
```

Note: You may need to create mock types for `WalletService` and `ContentStore` if they don't exist in test files yet. Check existing test files first.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestHandleMeta_ReturnsFullNodeInfo -v -count=1`
Expected: FAIL — current handleMeta returns stub JSON.

**Step 3: Fix handleMeta**

Replace the `handleMeta` function in `internal/daemon/content.go`:

```go
// handleMeta handles GET /_bitfs/meta/{pnode}/{path...} for metadata queries.
func (d *Daemon) handleMeta(w http.ResponseWriter, r *http.Request) {
	pnode := r.PathValue("pnode")
	if pnode == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_PNODE", "P_node parameter is required")
		return
	}

	pnodeBytes, err := hex.DecodeString(pnode)
	if err != nil || len(pnodeBytes) != 33 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PNODE", "P_node must be 66 hex characters (33 bytes)")
		return
	}

	path := r.PathValue("path")
	if path == "" {
		path = "/"
	}
	if path[0] != '/' {
		path = "/" + path
	}

	if d.metanet == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "NO_METANET", "Metanet service not available")
		return
	}

	node, err := d.metanet.GetNodeByPath(path)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Node not found")
		return
	}

	resp := map[string]interface{}{
		"pnode":     pnode,
		"path":      path,
		"type":      node.Type,
		"access":    node.Access,
		"mime_type": node.MimeType,
		"file_size": node.FileSize,
		"key_hash":  hex.EncodeToString(node.KeyHash),
	}

	if node.PricePerKB > 0 {
		resp["price_per_kb"] = node.PricePerKB
	}

	if node.Type == "dir" {
		children := make([]map[string]string, 0, len(node.Children))
		for _, c := range node.Children {
			children = append(children, map[string]string{
				"name": c.Name,
				"type": c.Type,
			})
		}
		resp["children"] = children
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
```

**Step 4: Run tests**

Run: `go test ./internal/daemon/ -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/content.go internal/daemon/meta_test.go
git commit -m "feat(daemon): implement real handleMeta with Metanet lookup

Replace stub that echoed parameters with actual node metadata query."
```

---

### Task 5: Add daemon invoice and payment endpoints

**Files:**
- Create: `internal/daemon/payment.go`
- Modify: `internal/daemon/routes.go` (register new routes)
- Modify: `internal/daemon/daemon.go` (add invoice store to Daemon struct)
- Create: `internal/daemon/payment_test.go`

**Step 1: Write the failing test**

Create `internal/daemon/payment_test.go`:

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServePaidContent_Returns402WithInvoice(t *testing.T) {
	node := &NodeInfo{
		Type:       "file",
		FileSize:   10240,
		PricePerKB: 50,
		Access:     "paid",
		KeyHash:    make([]byte, 32),
	}

	cfg := DefaultConfig()
	cfg.X402.Enabled = true
	d, err := New(cfg, &mockWallet{}, &mockStore{data: map[string][]byte{}}, nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	d.servePaidContent(w, node)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-Price-Per-KB"))
	assert.NotEmpty(t, w.Header().Get("X-File-Size"))

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp, "invoice_id")
}
```

**Step 2: Implement payment.go**

Create `internal/daemon/payment.go` with:
- In-memory `invoiceStore` map on Daemon (ID → invoice details)
- Enhanced `servePaidContent` that creates an invoice, stores it, returns invoice_id in response
- `handleGetBuyInfo`: `GET /_bitfs/buy/{txid}` — returns capsule_hash, price, payment_addr
- `handleSubmitHTLC`: `POST /_bitfs/buy/{txid}` — accepts HTLC tx, verifies, returns capsule

Key structures:
```go
type InvoiceRecord struct {
	ID          string
	NodePNode   []byte
	KeyHash     []byte
	PricePerKB  uint64
	FileSize    uint64
	PaymentAddr []byte
	CapsuleHash []byte
	Expiry      time.Time
	Paid        bool
}
```

**Step 3: Register routes in routes.go**

Add to `RegisterRoutes`:
```go
mux.HandleFunc("GET /_bitfs/buy/{txid}", wrap(d.handleGetBuyInfo))
mux.HandleFunc("POST /_bitfs/buy/{txid}", wrap(d.handleSubmitHTLC))
```

**Step 4: Run tests**

Run: `go test ./internal/daemon/ -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/payment.go internal/daemon/payment_test.go internal/daemon/routes.go internal/daemon/daemon.go
git commit -m "feat(daemon): add x402 invoice generation and HTLC buy endpoints

Add GET/POST /_bitfs/buy/{txid} for capsule purchase flow.
Enhanced servePaidContent to generate and track invoices."
```

---

### Task 6: Add client payment methods (Handshake, GetBuyInfo, SubmitHTLC)

**Files:**
- Modify: `internal/client/client.go`
- Modify: `internal/client/client_test.go`

**Step 1: Write failing tests**

Add to `internal/client/client_test.go`:

```go
func TestGetBuyInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		json.NewEncoder(w).Encode(BuyInfo{
			CapsuleHash: "abcd1234",
			Price:       500,
			PaymentAddr: "1BitcoinAddr",
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	info, err := c.GetBuyInfo("sometxid")
	require.NoError(t, err)
	assert.Equal(t, uint64(500), info.Price)
}
```

**Step 2: Implement**

Add to `client.go`:

```go
type BuyInfo struct {
	CapsuleHash string `json:"capsule_hash"`
	Price       uint64 `json:"price"`
	PaymentAddr string `json:"payment_addr"`
}

type CapsuleResponse struct {
	Capsule string `json:"capsule"`
}

func (c *Client) GetBuyInfo(txid string) (*BuyInfo, error) {
	u := fmt.Sprintf("%s/_bitfs/buy/%s", c.BaseURL, txid)
	resp, err := c.HTTPClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return nil, err
	}
	var info BuyInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("client: decode buy info: %w", err)
	}
	return &info, nil
}

func (c *Client) SubmitHTLC(txid string, htlcRawTx []byte) (*CapsuleResponse, error) {
	u := fmt.Sprintf("%s/_bitfs/buy/%s", c.BaseURL, txid)
	resp, err := c.HTTPClient.Post(u, "application/octet-stream", bytes.NewReader(htlcRawTx))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return nil, err
	}
	var capsule CapsuleResponse
	if err := json.NewDecoder(resp.Body).Decode(&capsule); err != nil {
		return nil, fmt.Errorf("client: decode capsule: %w", err)
	}
	return &capsule, nil
}
```

**Step 3: Run tests**

Run: `go test ./internal/client/ -v -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/client/
git commit -m "feat(client): add GetBuyInfo and SubmitHTLC payment methods"
```

---

## Phase 2: B-Tools Read Path

### Task 7: Implement bls (directory listing)

**Files:**
- Rewrite: `cmd/bls/main.go`
- Test: `cmd/bls/main_test.go` (create)

**Step 1: Write the failing test**

Create `cmd/bls/main_test.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serveMeta(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type":   "dir",
			"access": "free",
			"children": []map[string]string{
				{"name": "hello.txt", "type": "file"},
				{"name": "docs", "type": "dir"},
			},
		})
	}))
}

func TestBls_TextOutput(t *testing.T) {
	srv := serveMeta(t)
	defer srv.Close()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := run([]string{"--host", srv.URL, "bitfs://02abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab/"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.Equal(t, 0, code)
	assert.Contains(t, buf.String(), "hello.txt")
	assert.Contains(t, buf.String(), "docs")
}

func TestBls_JsonOutput(t *testing.T) {
	srv := serveMeta(t)
	defer srv.Close()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	code := run([]string{"--json", "--host", srv.URL, "bitfs://02abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab/"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.Equal(t, 0, code)

	var resp map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp, "children")
}
```

**Step 2: Rewrite bls**

Rewrite `cmd/bls/main.go` to:
1. Parse flags: `--json`, `--long`/`-l`, `--host`, `--no-cache`, `--timeout`
2. Parse URI via `paymail.ParseURI`
3. Resolve pnode from URI (for bare pubkey: use directly; for paymail/dnslink: resolve first)
4. Call `client.GetMeta(pnode, path)`
5. Format output:
   - Default: one name per line
   - `-l`: `type  access  size  name`
   - `--json`: full MetaResponse as JSON

Exit codes: 0=success, 2=not found, 4=network error, 6=invalid argument.

**Step 3: Run tests**

Run: `go test ./cmd/bls/ -v -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/bls/
git commit -m "feat(bls): implement real directory listing via daemon API

Replace stub with client.GetMeta + formatted output."
```

---

### Task 8: Implement bstat (file metadata)

**Files:**
- Rewrite: `cmd/bstat/main.go`
- Test: `cmd/bstat/main_test.go` (create)

Same pattern as Task 7 but for metadata display:
- Calls `client.GetMeta`
- Formats: Type, Hash, Size, Owner (PNode), TxID, Access, MimeType
- `--versions` flag: list all versions (requires daemon ListVersions endpoint — can defer, output "not yet supported" for now)
- `--json` flag: full JSON output

**Commit:**
```bash
git commit -m "feat(bstat): implement real metadata display via daemon API"
```

---

### Task 9: Implement bcat (content output to stdout)

**Files:**
- Rewrite: `cmd/bcat/main.go`
- Test: `cmd/bcat/main_test.go` (create)

Pattern:
1. Parse URI, resolve pnode
2. `client.GetMeta` to check access mode and get key_hash
3. If free: `client.GetData(key_hash)` → decrypt with trivial key (D_node=1) → write to stdout
4. If paid and no `--buy`: print price info, suggest `--buy`, exit 5
5. If paid and `--buy`: payment flow (Task 13)

For now, implement free path only. Add `// TODO: --buy flag for paid content` comment for Phase 3.

**Commit:**
```bash
git commit -m "feat(bcat): implement free content output to stdout

Paid content purchase (--buy) deferred to Phase 3."
```

---

### Task 10: Implement bget (download to file)

**Files:**
- Rewrite: `cmd/bget/main.go`
- Test: `cmd/bget/main_test.go` (create)

Same as bcat but writes to file instead of stdout:
- `-o <file>` flag for output filename (default: basename from URI path)
- `--version N` flag (defer to "not yet supported")
- `--buy` flag (defer to Phase 3)

**Commit:**
```bash
git commit -m "feat(bget): implement file download via daemon API"
```

---

### Task 11: Implement btree (recursive tree)

**Files:**
- Rewrite: `cmd/btree/main.go`
- Test: `cmd/btree/main_test.go` (create)

Pattern:
1. Parse URI, resolve pnode
2. Recursive `client.GetMeta` for each directory
3. Tree-style output with indentation: `├── file.txt (free, 1.2K)` / `└── dir/`
4. `-d N` depth limit
5. `--json` nested JSON tree

**Commit:**
```bash
git commit -m "feat(btree): implement recursive tree visualization"
```

---

## Phase 3: x402 Payment Flow

### Task 12: Wire daemon invoice generation into servePaidContent

**Files:**
- Modify: `internal/daemon/payment.go`
- Modify: `internal/daemon/routes.go` (if needed)
- Test: `internal/daemon/payment_test.go` (extend)

Ensure `servePaidContent` creates a real `x402.Invoice` using libbitfs's `x402.CalculatePrice` and `x402.NewInvoice`, stores it in Daemon's invoice map, and returns proper 402 headers including `X-Invoice-Id`.

**Commit:**
```bash
git commit -m "feat(daemon): wire x402 invoice generation into paid content flow"
```

---

### Task 13: Wire --buy flag in bcat and bget

**Files:**
- Modify: `cmd/bcat/main.go`
- Modify: `cmd/bget/main.go`

When `--buy` is set:
1. Load wallet from `--datadir` (prompt for password)
2. Call `client.GetBuyInfo(txid)` to get capsule_hash
3. Build HTLC via `x402.BuildHTLC()` from libbitfs
4. Sign HTLC tx
5. Call `client.SubmitHTLC()` to get capsule
6. Decrypt via `method42.DecryptWithCapsule(capsule, ...)`
7. Output/save decrypted content

This requires adding `--datadir` and `--password` flags to bcat/bget.

**Commit:**
```bash
git commit -m "feat(bcat,bget): add --buy flag for paid content HTLC purchase"
```

---

## Phase 4: Missing Commands

### Task 14: Add bitfs cp command

**Files:**
- Create: `internal/engine/copy.go`
- Create: `internal/engine/copy_test.go`
- Create: `cmd/bitfs/cmd_cp.go`
- Modify: `cmd/bitfs/main.go:43-81` (add "cp" case)

**Step 1: Write failing engine test**

```go
func TestCopy_CreatesIndependentNode(t *testing.T) {
	eng := setupTestEngine(t)

	// Create root and source file first
	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)
	_, err = eng.PutFile(&PutOpts{VaultIndex: 0, LocalFile: "testdata/hello.txt", RemotePath: "/hello.txt", Access: "free"})
	require.NoError(t, err)

	// Copy
	result, err := eng.Copy(&CopyOpts{VaultIndex: 0, SrcPath: "/hello.txt", DstPath: "/hello-copy.txt"})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "Copied")

	// Verify independent node
	src := eng.State.FindNodeByPath("/hello.txt")
	dst := eng.State.FindNodeByPath("/hello-copy.txt")
	require.NotNil(t, dst)
	assert.NotEqual(t, src.PubKeyHex, dst.PubKeyHex, "copy must have independent key")
}
```

**Step 2: Implement engine.Copy**

`internal/engine/copy.go`:
```go
type CopyOpts struct {
	VaultIndex uint32
	SrcPath    string
	DstPath    string
}

func (e *Engine) Copy(opts *CopyOpts) (*Result, error) {
	// 1. Read source node state
	// 2. Read source content from store
	// 3. Derive new child key for destination
	// 4. Re-encrypt content with new key
	// 5. Store new ciphertext
	// 6. Build CreateChild tx for destination
	// 7. Update local state
}
```

**Step 3: Add CLI wiring**

`cmd/bitfs/cmd_cp.go`: parse args, call `eng.Copy()`
`cmd/bitfs/main.go`: add `case "cp": return runCp(cmdArgs)`

**Step 4: Run tests, commit**

```bash
git commit -m "feat: add bitfs cp command for independent file copy"
```

---

### Task 15: Implement cross-directory mv

**Files:**
- Modify: `internal/engine/move.go:21-27`
- Extend: `internal/engine/move_test.go`

**Step 1: Write failing test**

```go
func TestMove_CrossDirectory(t *testing.T) {
	eng := setupTestEngine(t)
	// Create /src/file.txt and /dst/
	// Move /src/file.txt to /dst/file.txt
	result, err := eng.Move(&MoveOpts{VaultIndex: 0, SrcPath: "/src/file.txt", DstPath: "/dst/file.txt"})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "Moved")
}
```

**Step 2: Implement**

Replace the early `srcDir != dstDir` error in `move.go` with:
1. Find source parent, remove child entry
2. Find dest parent, add child entry (same PubKey, new name)
3. Build SelfUpdate tx for source parent (RemoveChild)
4. Build SelfUpdate tx for dest parent (AddChild)
5. Update local state (node path, parent children)

Note: Two separate transactions. Document this in a comment as application-level atomicity only.

**Step 3: Run tests, commit**

```bash
git commit -m "feat(engine): implement cross-directory mv with two-phase update"
```

---

### Task 16: Add shell lcd command

**Files:**
- Modify: `cmd/bitfs/cmd_shell.go:63-198`

**Step 1: Add lcd to shell switch**

In `cmd_shell.go`, add case for `lcd`:

```go
case "lcd":
	if len(cmdArgs) == 0 {
		fmt.Println(localCwd)
	} else {
		target := cmdArgs[0]
		if !filepath.IsAbs(target) {
			target = filepath.Join(localCwd, target)
		}
		target = filepath.Clean(target)
		info, err := os.Stat(target)
		if err != nil || !info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", target)
			continue
		}
		localCwd = target
		fmt.Printf("Local directory: %s\n", localCwd)
	}
```

Add `localCwd` variable initialization before the loop:
```go
localCwd, _ := os.Getwd()
```

Update `shellHelp()` to include `lcd`.

**Step 2: Run tests, commit**

```bash
git commit -m "feat(shell): add lcd command for local directory navigation"
```

---

### Task 17: Add bitfs unpublish command

**Files:**
- Create: `cmd/bitfs/cmd_unpublish.go`
- Modify: `cmd/bitfs/main.go` (add case)

Simple implementation:
- Read local publish bindings from config/state
- Remove the binding for the given domain
- Print instructions to remove DNS TXT record

```bash
git commit -m "feat: add bitfs unpublish command"
```

---

## Phase 5: Paymail/Publish Real Binding

### Task 18: Implement real publish with DNSLink verification

**Files:**
- Modify: `internal/engine/publish.go` (or create if separate from existing)
- Modify: `cmd/bitfs/cmd_publish.go`

Enhance `Publish()` to:
1. Derive vault root pubkey
2. Output DNS TXT record instructions
3. Attempt DNS verification: query `_bitfs_pubkey.{domain}` TXT record
4. Check bidirectional match: DNS → P_node AND node domain → DNS domain
5. Store binding in local state if verified
6. `bitfs publish` (no args): list all bindings with verification status

```bash
git commit -m "feat: implement real DNSLink publish with bidirectional verification"
```

---

### Task 19: Implement Paymail PKI endpoint in daemon

**Files:**
- Modify: `internal/daemon/routes.go` (add PKI route)
- Create: `internal/daemon/paymail.go`

Add route: `GET /api/v1/pki/{alias}@{domain}`

Implementation:
1. Parse alias from path
2. Look up vault by alias name (via WalletService)
3. Return `{"bsvalias": "1.0", "handle": "alias@domain", "pubkey": "02..."}`

Fix `handleBSVAlias` to use real host and correct URL templates.

```bash
git commit -m "feat(daemon): implement Paymail PKI endpoint and fix bsvalias capabilities"
```

---

## Phase 6: Security Hardening

### Task 20: Input validation audit

**Files:** All `internal/daemon/*.go`, `internal/client/*.go`, `cmd/*/main.go`

Audit checklist:
- [ ] All hex inputs validated for length and charset
- [ ] All paths sanitized (no path traversal)
- [ ] HTTP request body size limited (`MaxBytesReader`)
- [ ] DNS responses validated (secp256k1 pubkey point validation)
- [ ] No internal state leaked in error messages
- [ ] HTLC timeout bounds checked (min 6 blocks, max 288)
- [ ] Payment amounts > 0 and within reasonable bounds
- [ ] TLS cert/key paths validated on daemon start

Fix any issues found during audit.

```bash
git commit -m "security: input validation audit across daemon and client"
```

---

### Task 21: Wallet password hardening

**Files:**
- Modify: `cmd/bitfs/cmd_wallet.go` (password prompting)
- Modify: `cmd/bitfs/main.go` or create `cmd/bitfs/password.go`

Replace hardcoded `"bitfs"` default password with:
1. Read from terminal without echo using `golang.org/x/term`
2. Confirm password on wallet init (enter twice)
3. Memory zeroing after use (`for i := range pass { pass[i] = 0 }`)

```bash
git commit -m "security: add proper terminal password prompting with no-echo"
```

---

## Phase 7: Documentation

### Task 22: Daemon API reference

**Files:**
- Create: `docs/api-reference.md`

Document all endpoints:
- `GET /_bitfs/health`
- `POST /_bitfs/handshake`
- `GET /_bitfs/data/{hash}`
- `GET /_bitfs/meta/{pnode}/{path...}`
- `GET/POST /_bitfs/buy/{txid}`
- `GET /.well-known/bsvalias`
- `GET /api/v1/pki/{alias}@{domain}`
- `GET /{path}` (content negotiation)

For each: method, URL, parameters, request body, response body, status codes, example.

```bash
git commit -m "docs: add daemon API reference"
```

---

### Task 23: User guide

**Files:**
- Create: `docs/user-guide.md`

Sections:
1. Installation
2. Wallet setup (`bitfs wallet init`)
3. Creating directories and uploading files
4. Reading content with b-tools
5. Selling content (`bitfs sell`)
6. Buying content (`bcat --buy`)
7. Publishing with DNSLink
8. Running the daemon
9. Shell REPL usage

```bash
git commit -m "docs: add end-to-end user guide"
```

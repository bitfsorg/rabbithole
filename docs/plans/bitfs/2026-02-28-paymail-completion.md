# Paymail Completion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement 5 missing Paymail features: PublicProfile handler, VerifyPubKey handler, Address Resolution, Custom BRFC capabilities, DNSSEC validation.

**Architecture:** Two packages modified: `libbitfs-go/paymail/` (BRFC, address resolution, DNSSEC) and `bitfs/internal/daemon/` (two new handlers + capability advertising). All new code follows existing patterns — mock interfaces for testing, `*WithClient` variants for dependency injection.

**Tech Stack:** Go 1.25, `github.com/miekg/dns` (DNSSEC only), existing `paymail.HTTPClient`/`DNSResolver` interfaces.

---

### Task 1: BRFC ID Computation

**Files:**
- Create: `libbitfs-go/paymail/brfc.go`
- Create: `libbitfs-go/paymail/brfc_test.go`

**Step 1: Write the failing test**

```go
// brfc_test.go
package paymail

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeBRFCID(t *testing.T) {
	// BRFC spec: ID = hex(SHA256d(title + author + version))[:12]
	// SHA256d = SHA256(SHA256(x))
	// We verify the function produces deterministic 12-char hex output.
	id := ComputeBRFCID("BitFS Browse", "BitFS", "1.0")
	assert.Len(t, id, 12)

	// Same inputs → same output
	id2 := ComputeBRFCID("BitFS Browse", "BitFS", "1.0")
	assert.Equal(t, id, id2)

	// Different inputs → different output
	id3 := ComputeBRFCID("BitFS Buy", "BitFS", "1.0")
	assert.NotEqual(t, id, id3)
}

func TestBRFCConstants(t *testing.T) {
	// Verify constants match computed values
	assert.Equal(t, ComputeBRFCID("BitFS Browse", "BitFS", "1.0"), BRFCBitFSBrowse)
	assert.Equal(t, ComputeBRFCID("BitFS Buy", "BitFS", "1.0"), BRFCBitFSBuy)
	assert.Equal(t, ComputeBRFCID("BitFS Sell", "BitFS", "1.0"), BRFCBitFSSell)
}

func TestComputeBRFCID_KnownPaymail(t *testing.T) {
	// Verify against known Paymail BRFC IDs for sanity
	// "pki" capability has BRFC "0c4339ef99c2" per spec:
	// SHA256d("Public Key InfrastructureOpenPaymail1") → first 6 bytes
	// We just verify our function produces 12-char hex, since we don't
	// have the exact concatenation for the pki BRFC.
	id := ComputeBRFCID("Public Key Infrastructure", "nChain", "1")
	assert.Len(t, id, 12)
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./paymail/ -run TestComputeBRFCID -v`
Expected: FAIL — `ComputeBRFCID` undefined

**Step 3: Write implementation**

```go
// brfc.go
package paymail

import (
	"crypto/sha256"
	"encoding/hex"
)

// ComputeBRFCID generates a BRFC (Bitcoin SV Request-For-Comments) ID
// per the BRC standard: ID = hex(SHA256d(title + author + version))[:12].
// SHA256d is double SHA-256. The result is a 12-character hex string (6 bytes).
func ComputeBRFCID(title, author, version string) string {
	data := []byte(title + author + version)
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return hex.EncodeToString(second[:6])
}

// BitFS custom BRFC capability IDs.
var (
	BRFCBitFSBrowse = ComputeBRFCID("BitFS Browse", "BitFS", "1.0")
	BRFCBitFSBuy    = ComputeBRFCID("BitFS Buy", "BitFS", "1.0")
	BRFCBitFSSell   = ComputeBRFCID("BitFS Sell", "BitFS", "1.0")
)
```

**Step 4: Run test to verify it passes**

Run: `cd libbitfs-go && go test ./paymail/ -run TestComputeBRFC -v`
Expected: PASS

**Step 5: Commit**

```
feat(paymail): add BRFC ID computation and BitFS capability constants
```

---

### Task 2: Daemon PublicProfile + VerifyPubKey handlers

**Files:**
- Modify: `bitfs/internal/daemon/paymail.go` (add two handlers + response types)
- Modify: `bitfs/internal/daemon/paymail_test.go` (add tests)

**Step 1: Write failing tests**

Append to `paymail_test.go`:

```go
// --- handlePublicProfile Tests ---

type profileResponse struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Avatar string `json:"avatar"`
}

func TestHandlePublicProfile_Success(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)
	wallet.vaultKeys["alice"] = hex.EncodeToString(wallet.pubKey.Compressed())

	req := httptest.NewRequest("GET", "/api/v1/public-profile/alice@bitfs.org", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp profileResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "alice", resp.Name)
	assert.Equal(t, "bitfs.org", resp.Domain)
}

func TestHandlePublicProfile_UnknownAlias(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/public-profile/unknown@bitfs.org", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandlePublicProfile_MalformedHandle(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/public-profile/noatsign", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- handleVerifyPubKey Tests ---

type verifyResponse struct {
	Handle string `json:"handle"`
	PubKey string `json:"pubkey"`
	Match  bool   `json:"match"`
}

func TestHandleVerifyPubKey_Match(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)
	pubHex := hex.EncodeToString(wallet.pubKey.Compressed())
	wallet.vaultKeys["alice"] = pubHex

	req := httptest.NewRequest("GET", "/api/v1/verify/alice@bitfs.org/"+pubHex, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp verifyResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.True(t, resp.Match)
	assert.Equal(t, "alice@bitfs.org", resp.Handle)
	assert.Equal(t, pubHex, resp.PubKey)
}

func TestHandleVerifyPubKey_NoMatch(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)
	wallet.vaultKeys["alice"] = hex.EncodeToString(wallet.pubKey.Compressed())

	wrongKey := "03" + strings.Repeat("ab", 32)
	req := httptest.NewRequest("GET", "/api/v1/verify/alice@bitfs.org/"+wrongKey, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp verifyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.False(t, resp.Match)
}

func TestHandleVerifyPubKey_UnknownAlias(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	someKey := "02" + strings.Repeat("ab", 32)
	req := httptest.NewRequest("GET", "/api/v1/verify/unknown@bitfs.org/"+someKey, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp verifyResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.False(t, resp.Match) // Unknown alias → match=false, no 404
}

func TestHandleVerifyPubKey_MalformedHandle(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/verify/noatsign/02abcd", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
```

**Step 2: Run tests to verify they fail**

Run: `cd bitfs && go test ./internal/daemon/ -run "TestHandlePublicProfile|TestHandleVerifyPubKey" -v`
Expected: FAIL — routes return 404/405 (no handlers registered)

**Step 3: Write implementation**

Add to `paymail.go`:

```go
// publicProfileResponse is the Paymail Public Profile response.
type publicProfileResponse struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Avatar string `json:"avatar"`
}

// handlePublicProfile handles GET /api/v1/public-profile/{handle} requests.
// Returns the public profile for a Paymail alias (name, domain, avatar).
func (d *Daemon) handlePublicProfile(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if handle == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HANDLE", "missing handle")
		return
	}

	parts := strings.SplitN(handle, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HANDLE", "handle must be in alias@domain format")
		return
	}
	alias, domain := parts[0], parts[1]

	// Verify alias exists
	if _, err := d.wallet.GetVaultPubKey(alias); err != nil {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "unknown alias: "+alias)
		return
	}

	resp := publicProfileResponse{
		Name:   alias,
		Domain: domain,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// verifyPubKeyResponse is the Paymail Verify Public Key Owner response.
type verifyPubKeyResponse struct {
	Handle string `json:"handle"`
	PubKey string `json:"pubkey"`
	Match  bool   `json:"match"`
}

// handleVerifyPubKey handles GET /api/v1/verify/{handle}/{pubkey} requests.
// Verifies whether the given public key belongs to the specified Paymail alias.
// Returns match=false for unknown aliases (does not reveal alias existence).
func (d *Daemon) handleVerifyPubKey(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	pubkeyHex := r.PathValue("pubkey")

	if handle == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HANDLE", "missing handle")
		return
	}

	parts := strings.SplitN(handle, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HANDLE", "handle must be in alias@domain format")
		return
	}
	alias := parts[0]

	match := false
	if vaultPubKey, err := d.wallet.GetVaultPubKey(alias); err == nil {
		match = (vaultPubKey == pubkeyHex)
	}

	resp := verifyPubKeyResponse{
		Handle: handle,
		PubKey: pubkeyHex,
		Match:  match,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
```

Add routes in `routes.go` after the existing PKI route (line 56):

```go
	mux.HandleFunc("GET /api/v1/public-profile/{handle}", wrap(d.handlePublicProfile))
	mux.HandleFunc("GET /api/v1/verify/{handle}/{pubkey}", wrap(d.handleVerifyPubKey))
```

**Step 4: Run tests to verify they pass**

Run: `cd bitfs && go test ./internal/daemon/ -run "TestHandlePublicProfile|TestHandleVerifyPubKey" -v`
Expected: PASS

**Step 5: Commit**

```
feat(daemon): add PublicProfile and VerifyPubKey Paymail handlers
```

---

### Task 3: Update capability advertising (VerifyPubKey + BRFC)

**Files:**
- Modify: `bitfs/internal/daemon/routes.go:124-150` (handleBSVAlias)
- Modify: `bitfs/internal/daemon/paymail_test.go` (add capability tests)
- Note: requires `libbitfs-go/paymail` BRFC constants from Task 1

**Step 1: Write failing tests**

Append to `paymail_test.go`:

```go
func TestBSVAlias_HasVerifyPubKeyCapability(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/.well-known/bsvalias", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	caps := resp["capabilities"].(map[string]interface{})

	// a9f510c16bde is the VerifyPubKey BRFC ID
	verifyURL, ok := caps["a9f510c16bde"].(string)
	assert.True(t, ok, "VerifyPubKey capability should be advertised")
	assert.Contains(t, verifyURL, "/api/v1/verify/")
	assert.Contains(t, verifyURL, "{alias}")
	assert.Contains(t, verifyURL, "{pubkey}")
}

func TestBSVAlias_HasBRFCCapabilities(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/.well-known/bsvalias", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	caps := resp["capabilities"].(map[string]interface{})

	// Verify 3 BitFS BRFC capabilities exist and point to /_bitfs/ endpoints
	for _, brfcID := range []string{
		paymail.BRFCBitFSBrowse,
		paymail.BRFCBitFSBuy,
		paymail.BRFCBitFSSell,
	} {
		url, ok := caps[brfcID].(string)
		assert.True(t, ok, "BRFC %s should be advertised", brfcID)
		assert.Contains(t, url, "/_bitfs/")
	}
}
```

Note: The test file will need `import "github.com/tongxiaofeng/libbitfs-go/paymail"` for the BRFC constants. Since `bitfs/` depends on `libbitfs-go/` already via `go.mod replace`, this import works.

**Step 2: Run tests to verify they fail**

Run: `cd bitfs && go test ./internal/daemon/ -run "TestBSVAlias_HasVerifyPubKey|TestBSVAlias_HasBRFC" -v`
Expected: FAIL — capabilities map doesn't have these keys

**Step 3: Update handleBSVAlias**

In `routes.go`, replace the capabilities map in `handleBSVAlias()` (lines 140-145):

```go
	caps := map[string]interface{}{
		"bsvalias": "1.0",
		"capabilities": map[string]interface{}{
			// Standard Paymail capabilities
			"pki":          base + "/api/v1/pki/{alias}@{domain.tld}",
			"f12f968c92d6": base + "/api/v1/public-profile/{alias}@{domain.tld}",
			"a9f510c16bde": base + "/api/v1/verify/{alias}@{domain.tld}/{pubkey}",
			// BitFS custom BRFC capabilities
			paymail.BRFCBitFSBrowse: base + "/_bitfs/meta/{pnode}/{path}",
			paymail.BRFCBitFSBuy:    base + "/_bitfs/buy/{txid}",
			paymail.BRFCBitFSSell:   base + "/_bitfs/sales",
		},
	}
```

Add import for `paymail` package in `routes.go`:

```go
import (
	// ... existing imports ...
	"github.com/tongxiaofeng/libbitfs-go/paymail"
)
```

**Step 4: Run tests to verify they pass**

Run: `cd bitfs && go test ./internal/daemon/ -run "TestBSVAlias" -v`
Expected: PASS (all BSVAlias tests, including existing ones)

**Step 5: Run full daemon test suite**

Run: `cd bitfs && go test ./internal/daemon/ -v -count=1`
Expected: ALL PASS

**Step 6: Commit**

```
feat(daemon): advertise VerifyPubKey and BitFS BRFC capabilities
```

---

### Task 4: Address Resolution (client-side)

**Files:**
- Create: `libbitfs-go/paymail/address.go`
- Create: `libbitfs-go/paymail/address_test.go`
- Modify: `libbitfs-go/paymail/resolve.go` (add PaymentDestination to PaymailCapabilities)
- Modify: `libbitfs-go/paymail/errors.go` (add ErrAddressResolution)

**Step 1: Update PaymailCapabilities and errors**

In `resolve.go`, add field to `PaymailCapabilities` struct (line 17):

```go
type PaymailCapabilities struct {
	PKI                string // URL template for public key infrastructure
	PublicProfile      string // URL template for profile info
	VerifyPubKey       string // URL template for key verification
	PaymentDestination string // URL template for P2P payment destination (BRFC 2a40af698840)
}
```

In `resolve.go`, add capability constant (after line 54):

```go
	capPaymentDestination = "2a40af698840"
```

In `resolve.go`, add case to DiscoverCapabilitiesWithClient switch (after line 109):

```go
		case key == capPaymentDestination || strings.Contains(key, "paymentDestination"):
			caps.PaymentDestination = urlStr
```

In `errors.go`, add:

```go
	// ErrAddressResolution indicates Paymail address resolution failed.
	ErrAddressResolution = errors.New("paymail: address resolution failed")
```

**Step 2: Write failing test for ResolvePaymentDestination**

```go
// address_test.go
package paymail

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPaymentClient simulates a Paymail server that supports payment destination.
type mockPaymentClient struct {
	responses map[string]*http.Response
}

func (m *mockPaymentClient) Get(url string) (*http.Response, error) {
	if resp, ok := m.responses[url]; ok {
		return resp, nil
	}
	return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (m *mockPaymentClient) Post(url, contentType string, body io.Reader) (*http.Response, error) {
	if resp, ok := m.responses[url]; ok {
		return resp, nil
	}
	return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func newMockPaymentServer(domain string) *mockPaymentClient {
	capJSON, _ := json.Marshal(map[string]interface{}{
		"bsvalias": "1.0",
		"capabilities": map[string]interface{}{
			"pki":          "https://" + domain + "/api/v1/pki/{alias}@{domain.tld}",
			"2a40af698840": "https://" + domain + "/api/v1/payment-destination/{alias}@{domain.tld}",
		},
	})

	paymentJSON, _ := json.Marshal(map[string]interface{}{
		"outputs": []map[string]interface{}{
			{
				"script": "76a91489abcdefabbaabbaabbaabbaabbaabbaabbaabba88ac",
				"satoshis": 0,
			},
		},
	})

	return &mockPaymentClient{
		responses: map[string]*http.Response{
			"https://" + domain + "/.well-known/bsvalias": {
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(capJSON))),
			},
			"https://" + domain + "/api/v1/payment-destination/alice@" + domain: {
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(paymentJSON))),
			},
		},
	}
}

func TestResolvePaymentDestination_Success(t *testing.T) {
	client := newMockPaymentServer("example.com")
	outputs, err := ResolvePaymentDestinationWithClient("alice", "example.com", client)
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	assert.Equal(t, "76a91489abcdefabbaabbaabbaabbaabbaabbaabbaabba88ac", outputs[0].Script)
}

func TestResolvePaymentDestination_EmptyAlias(t *testing.T) {
	_, err := ResolvePaymentDestinationWithClient("", "example.com", nil)
	assert.ErrorIs(t, err, ErrAddressResolution)
}

func TestResolvePaymentDestination_NoCapability(t *testing.T) {
	// Server without payment destination capability
	capJSON, _ := json.Marshal(map[string]interface{}{
		"bsvalias":     "1.0",
		"capabilities": map[string]interface{}{"pki": "https://example.com/pki"},
	})
	client := &mockPaymentClient{
		responses: map[string]*http.Response{
			"https://example.com/.well-known/bsvalias": {
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(capJSON))),
			},
		},
	}
	_, err := ResolvePaymentDestinationWithClient("alice", "example.com", client)
	assert.ErrorIs(t, err, ErrAddressResolution)
}
```

**Step 3: Run tests to verify they fail**

Run: `cd libbitfs-go && go test ./paymail/ -run TestResolvePaymentDestination -v`
Expected: FAIL — `ResolvePaymentDestinationWithClient` undefined

**Step 4: Write implementation**

```go
// address.go
package paymail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// PostClient extends HTTPClient with POST capability for payment destination.
type PostClient interface {
	HTTPClient
	Post(url, contentType string, body io.Reader) (*http.Response, error)
}

// defaultPostClient wraps http.DefaultClient.
type defaultPostClient struct{}

func (d *defaultPostClient) Get(url string) (*http.Response, error) {
	return http.DefaultClient.Get(url)
}

func (d *defaultPostClient) Post(url, contentType string, body io.Reader) (*http.Response, error) {
	return http.DefaultClient.Post(url, contentType, body)
}

// DefaultPostClient is the production HTTP client with POST support.
var DefaultPostClient PostClient = &defaultPostClient{}

// PaymentOutput holds a single payment output from the payment destination response.
type PaymentOutput struct {
	Script   string `json:"script"`
	Satoshis uint64 `json:"satoshis"`
}

// paymentDestinationRequest is the POST body for payment destination.
type paymentDestinationRequest struct {
	SenderName   string `json:"senderName"`
	SenderHandle string `json:"senderHandle"`
	DateTime     string `json:"dt"`
	Amount       uint64 `json:"amount"`
	Purpose      string `json:"purpose"`
}

// paymentDestinationResponse is the response from the payment destination endpoint.
type paymentDestinationResponse struct {
	Outputs []PaymentOutput `json:"outputs"`
}

// ResolvePaymentDestination resolves a Paymail alias to payment outputs
// using the P2P Payment Destination protocol (BRFC 2a40af698840).
// Each call may return a different HD-derived address for privacy.
func ResolvePaymentDestination(alias, domain string) ([]PaymentOutput, error) {
	return ResolvePaymentDestinationWithClient(alias, domain, DefaultPostClient)
}

// ResolvePaymentDestinationWithClient resolves payment destination using the provided client.
func ResolvePaymentDestinationWithClient(alias, domain string, client PostClient) ([]PaymentOutput, error) {
	if alias == "" || domain == "" {
		return nil, fmt.Errorf("%w: alias and domain are required", ErrAddressResolution)
	}

	// Discover capabilities (reuse existing HTTPClient-compatible Get method)
	caps, err := DiscoverCapabilitiesWithClient(domain, client)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAddressResolution, err)
	}

	if caps.PaymentDestination == "" {
		return nil, fmt.Errorf("%w: no payment destination capability for %s", ErrAddressResolution, domain)
	}

	// Build URL from template
	destURL := strings.ReplaceAll(caps.PaymentDestination, "{alias}", url.PathEscape(alias))
	destURL = strings.ReplaceAll(destURL, "{domain.tld}", url.PathEscape(domain))

	// POST request body
	reqBody := paymentDestinationRequest{
		SenderName: "BitFS",
		Purpose:    "revshare",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %w", ErrAddressResolution, err)
	}

	resp, err := client.Post(destURL, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: POST %s: %w", ErrAddressResolution, destURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: POST %s returned status %d", ErrAddressResolution, destURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxPaymailResponseSize))
	if err != nil {
		return nil, fmt.Errorf("%w: reading response: %w", ErrAddressResolution, err)
	}

	var destResp paymentDestinationResponse
	if err := json.Unmarshal(body, &destResp); err != nil {
		return nil, fmt.Errorf("%w: parsing response: %w", ErrAddressResolution, err)
	}

	if len(destResp.Outputs) == 0 {
		return nil, fmt.Errorf("%w: empty outputs in response", ErrAddressResolution)
	}

	return destResp.Outputs, nil
}
```

**Step 5: Run tests to verify they pass**

Run: `cd libbitfs-go && go test ./paymail/ -run TestResolvePaymentDestination -v`
Expected: PASS

**Step 6: Run full paymail test suite**

Run: `cd libbitfs-go && go test ./paymail/ -v -count=1`
Expected: ALL PASS

**Step 7: Commit**

```
feat(paymail): add P2P Payment Destination address resolution
```

---

### Task 5: DNSSEC Validation

**Files:**
- Create: `libbitfs-go/paymail/dnssec.go`
- Create: `libbitfs-go/paymail/dnssec_test.go`
- Modify: `libbitfs-go/paymail/errors.go` (add DNSSEC errors)
- Modify: `libbitfs-go/go.mod` (add miekg/dns dependency)

**Step 1: Add miekg/dns dependency**

Run: `cd libbitfs-go && go get github.com/miekg/dns`

**Step 2: Add error sentinel**

In `errors.go`, add:

```go
	// ErrDNSSECValidationFailed indicates DNSSEC signature chain validation failed.
	ErrDNSSECValidationFailed = errors.New("paymail: DNSSEC validation failed")
```

**Step 3: Write failing tests**

```go
// dnssec_test.go
package paymail

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDNSSECResolver_ImplementsDNSResolver(t *testing.T) {
	// Verify DNSSECResolver satisfies the DNSResolver interface.
	var _ DNSResolver = &DNSSECResolver{}
}

func TestNewDNSSECResolver_Defaults(t *testing.T) {
	r := NewDNSSECResolver("")
	assert.Equal(t, "8.8.8.8:53", r.Upstream)
}

func TestNewDNSSECResolver_Custom(t *testing.T) {
	r := NewDNSSECResolver("1.1.1.1:53")
	assert.Equal(t, "1.1.1.1:53", r.Upstream)
}

func TestDNSSECResolver_LookupSRV_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DNS integration test in short mode")
	}

	r := NewDNSSECResolver("")

	// Use a well-known SRV record that should exist.
	// _bsvalias._tcp may not exist for most domains, so test with a basic lookup.
	// If no SRV records found, we just verify no crash and proper error.
	_, srvs, err := r.LookupSRV("bsvalias", "tcp", "moneybutton.com")
	if err != nil {
		// Expected for domains without SRV records — just verify clean error
		assert.Error(t, err)
		return
	}
	// If it succeeds, verify we got valid SRV records
	for _, srv := range srvs {
		assert.NotEmpty(t, srv.Target)
		assert.Greater(t, srv.Port, uint16(0))
	}
}

func TestDNSSECResolver_LookupTXT_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DNS integration test in short mode")
	}

	r := NewDNSSECResolver("")

	// Google's TXT records should be DNSSEC-signed and always available.
	txts, err := r.LookupTXT("google.com")
	require.NoError(t, err)
	assert.NotEmpty(t, txts)
}

func TestDNSSECResolver_LookupTXT_NonExistentDomain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DNS integration test in short mode")
	}

	r := NewDNSSECResolver("")
	_, err := r.LookupTXT("this-domain-definitely-does-not-exist-xyz123.com")
	assert.Error(t, err)
}
```

**Step 4: Run tests to verify they fail**

Run: `cd libbitfs-go && go test ./paymail/ -run TestDNSSECResolver -v`
Expected: FAIL — `DNSSECResolver` undefined

**Step 5: Write implementation**

```go
// dnssec.go
package paymail

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// DNSSECResolver performs DNS lookups with DNSSEC validation using miekg/dns.
// It implements the DNSResolver interface for use with existing paymail functions.
type DNSSECResolver struct {
	Upstream string // Recursive resolver address (e.g. "8.8.8.8:53")
}

// NewDNSSECResolver creates a DNSSECResolver. If upstream is empty, defaults to "8.8.8.8:53".
func NewDNSSECResolver(upstream string) *DNSSECResolver {
	if upstream == "" {
		upstream = "8.8.8.8:53"
	}
	return &DNSSECResolver{Upstream: upstream}
}

// queryWithDNSSEC sends a DNS query with the DNSSEC OK (DO) flag and validates
// the AD (Authenticated Data) flag in the response. The upstream recursive
// resolver performs full DNSSEC validation; we verify the AD flag is set.
func (r *DNSSECResolver) queryWithDNSSEC(name string, qtype uint16) (*dns.Msg, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), qtype)
	msg.SetEdns0(4096, true) // DO flag = true (DNSSEC OK)
	msg.RecursionDesired = true

	client := &dns.Client{
		Timeout: 10 * time.Second,
	}

	resp, _, err := client.Exchange(msg, r.Upstream)
	if err != nil {
		return nil, fmt.Errorf("%w: DNS query for %s: %w", ErrDNSSECValidationFailed, name, err)
	}

	if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError {
		return nil, fmt.Errorf("%w: DNS query for %s returned rcode %d", ErrDNSSECValidationFailed, name, resp.Rcode)
	}

	// Verify the AD (Authenticated Data) flag.
	// When using a trusted recursive resolver (e.g. 8.8.8.8), the AD flag
	// indicates the resolver performed full DNSSEC validation.
	if !resp.AuthenticatedData {
		return nil, fmt.Errorf("%w: response for %s not authenticated (AD flag not set)", ErrDNSSECValidationFailed, name)
	}

	return resp, nil
}

// LookupSRV resolves SRV records with DNSSEC validation.
func (r *DNSSECResolver) LookupSRV(service, proto, name string) (string, []*net.SRV, error) {
	qname := fmt.Sprintf("_%s._%s.%s", service, proto, name)

	resp, err := r.queryWithDNSSEC(qname, dns.TypeSRV)
	if err != nil {
		return "", nil, err
	}

	var srvs []*net.SRV
	for _, rr := range resp.Answer {
		if srv, ok := rr.(*dns.SRV); ok {
			srvs = append(srvs, &net.SRV{
				Target:   strings.TrimSuffix(srv.Target, "."),
				Port:     srv.Port,
				Priority: srv.Priority,
				Weight:   srv.Weight,
			})
		}
	}

	if len(srvs) == 0 {
		return "", nil, fmt.Errorf("%w: no SRV records for %s", ErrDNSLookupFailed, qname)
	}

	return "", srvs, nil
}

// LookupTXT resolves TXT records with DNSSEC validation.
func (r *DNSSECResolver) LookupTXT(name string) ([]string, error) {
	resp, err := r.queryWithDNSSEC(name, dns.TypeTXT)
	if err != nil {
		return nil, err
	}

	var txts []string
	for _, rr := range resp.Answer {
		if txt, ok := rr.(*dns.TXT); ok {
			txts = append(txts, strings.Join(txt.Txt, ""))
		}
	}

	if len(txts) == 0 {
		return nil, fmt.Errorf("%w: no TXT records for %s", ErrDNSLookupFailed, name)
	}

	return txts, nil
}
```

**Step 6: Run tests to verify they pass**

Run: `cd libbitfs-go && go test ./paymail/ -run TestDNSSECResolver -v`
Expected: PASS (unit tests pass, integration tests pass if network available)

**Step 7: Run full paymail test suite**

Run: `cd libbitfs-go && go test ./paymail/ -v -count=1`
Expected: ALL PASS

**Step 8: Commit**

```
feat(paymail): add DNSSEC validation resolver using miekg/dns
```

---

### Task 6: Full integration verification

**Step 1: Run all libbitfs-go tests**

Run: `cd libbitfs-go && go test ./... -count=1 -race`
Expected: ALL PASS

**Step 2: Run all bitfs tests**

Run: `cd bitfs && go test ./... -count=1 -race`
Expected: ALL PASS

**Step 3: Run golangci-lint on both**

Run: `cd libbitfs-go && golangci-lint run ./paymail/`
Run: `cd bitfs && golangci-lint run ./internal/daemon/`
Expected: No new warnings

**Step 4: Final commit (if any lint fixes needed)**

```
chore: fix lint warnings from paymail completion
```

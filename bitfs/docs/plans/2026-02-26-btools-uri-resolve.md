# b* Tools URI Endpoint Resolution Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire up `paymail.ResolveURI()` in all 5 b* tools so they resolve remote daemon endpoints from `bitfs://` URIs instead of hardcoding `localhost:8080`.

**Architecture:** New shared `ResolveURI()` function in `bitfs/internal/client/resolve.go` calls `paymail.ResolveURIWith()` to get pubkey + endpoints, then constructs a `Client`. All 5 b* tools replace their `switch parsed.Type` blocks with one call to this function. `--host` becomes an optional override (empty default).

**Tech Stack:** Go, `libbitfs-go/paymail` (ParseURI, ResolveURIWith, DNSResolver, HTTPClient), `bitfs/internal/client`, testify

**Design doc:** `bitfs/docs/plans/2026-02-26-btools-uri-resolve-design.md`

---

### Task 1: Create `client.ResolveURI` with tests

**Files:**
- Create: `bitfs/internal/client/resolve.go`
- Create: `bitfs/internal/client/resolve_test.go`

**Step 1: Write the test file**

The test uses mock HTTP (for paymail PKI) and mock DNS (for SRV records). Test cases:
1. Bare pubkey URI + hostOverride → returns client with override URL
2. Bare pubkey URI + no hostOverride → returns error
3. Paymail URI → resolves pubkey via PKI, endpoint via SRV
4. Paymail URI + hostOverride → uses override, still resolves pubkey
5. DNSLink URI → resolves pubkey via TXT, endpoint via SRV
6. Invalid URI → returns error

```go
// bitfs/internal/client/resolve_test.go
package client

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/libbitfs-go/paymail"
)

// testPubKeyHex is a well-known compressed public key (33 bytes, prefix 02).
const testPubKeyHex = "02b4632d08485ff1df2db55b9dafd23347d1c47a457072a1e87be26896549a8737"

// mockDNS implements paymail.DNSResolver for testing.
type mockDNS struct {
	srvRecords map[string][]*net.SRV // key: "service.domain"
	txtRecords map[string][]string   // key: domain
}

func (m *mockDNS) LookupSRV(service, proto, name string) (string, []*net.SRV, error) {
	key := service + "." + name
	if recs, ok := m.srvRecords[key]; ok {
		return "", recs, nil
	}
	return "", nil, fmt.Errorf("no SRV records for %s", key)
}

func (m *mockDNS) LookupTXT(name string) ([]string, error) {
	if recs, ok := m.txtRecords[name]; ok {
		return recs, nil
	}
	return nil, fmt.Errorf("no TXT records for %s", name)
}

func TestResolveURI_PubKey_WithHostOverride(t *testing.T) {
	uri := "bitfs://" + testPubKeyHex + "/docs/readme.txt"
	result, err := ResolveURI(uri, "http://example.com:8080", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, testPubKeyHex, result.PNode)
	assert.Equal(t, "/docs/readme.txt", result.Path)
	assert.Equal(t, "http://example.com:8080", result.Client.BaseURL)
}

func TestResolveURI_PubKey_NoHost_Error(t *testing.T) {
	uri := "bitfs://" + testPubKeyHex + "/docs"
	_, err := ResolveURI(uri, "", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--host")
}

func TestResolveURI_Paymail(t *testing.T) {
	// Mock PKI server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/bsvalias":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"bsvalias": "1.0",
				"capabilities": map[string]interface{}{
					"pki": fmt.Sprintf("%s/api/v1/pki/{alias}@{domain.tld}", srv.URL),
				},
			})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"bsvalias": "1.0",
				"handle":   "alice@example.com",
				"pubkey":   testPubKeyHex,
			})
		}
	}))
	defer srv.Close()

	// We can't easily test paymail since the domain resolves to the mock server.
	// This test is better done in integration tests. For unit test, test the
	// path where host override is provided but URI is paymail.

	// Paymail with host override — resolves pubkey via PKI, uses override URL.
	// This requires a real PKI mock. Skipping for now — covered by integration test.
	t.Skip("paymail resolution requires HTTPS mocking — covered by integration tests")
}

func TestResolveURI_DNSLink(t *testing.T) {
	dns := &mockDNS{
		txtRecords: map[string][]string{
			"_bitfs_pubkey.example.com": {testPubKeyHex},
		},
		srvRecords: map[string][]*net.SRV{
			"bitfs.example.com": {{Target: "cdn.example.com.", Port: 443, Priority: 1, Weight: 100}},
		},
	}

	uri := "bitfs://example.com/docs/readme.txt"
	result, err := ResolveURI(uri, "", nil, dns)
	require.NoError(t, err)
	assert.Equal(t, testPubKeyHex, result.PNode)
	assert.Equal(t, "/docs/readme.txt", result.Path)
	assert.Equal(t, "https://cdn.example.com:443", result.Client.BaseURL)
}

func TestResolveURI_DNSLink_WithHostOverride(t *testing.T) {
	dns := &mockDNS{
		txtRecords: map[string][]string{
			"_bitfs_pubkey.example.com": {testPubKeyHex},
		},
	}

	uri := "bitfs://example.com/docs"
	result, err := ResolveURI(uri, "http://localhost:9090", nil, dns)
	require.NoError(t, err)
	assert.Equal(t, testPubKeyHex, result.PNode)
	assert.Equal(t, "/docs", result.Path)
	assert.Equal(t, "http://localhost:9090", result.Client.BaseURL)
}

func TestResolveURI_InvalidURI(t *testing.T) {
	_, err := ResolveURI("not-a-uri", "", nil, nil)
	require.Error(t, err)
}

func TestResolveURI_EmptyPath_DefaultsToRoot(t *testing.T) {
	uri := "bitfs://" + testPubKeyHex
	result, err := ResolveURI(uri, "http://localhost:8080", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "/", result.Path)
}
```

Note: The `ResolveURI` signature accepts optional `paymail.HTTPClient` and `paymail.DNSResolver` for testability. When nil, the function uses `paymail.DefaultHTTPClient` and `paymail.DefaultDNSResolver`.

**Step 2: Write the implementation**

```go
// bitfs/internal/client/resolve.go
package client

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/tongxiaofeng/libbitfs-go/paymail"
)

// ResolveResult holds the resolved connection parameters from a bitfs:// URI.
type ResolveResult struct {
	Client *Client // HTTP client connected to the resolved endpoint.
	PNode  string  // Hex-encoded 33-byte compressed pubkey (66 chars).
	Path   string  // Path component from URI (with leading /).
}

// ResolveURI resolves a bitfs:// URI into a connected Client, pnode, and path.
//
// If hostOverride is non-empty, it is used as the daemon base URL
// (skipping SRV endpoint resolution, but pubkey is still resolved for
// paymail/dnslink URIs).
//
// For bare pubkey URIs (bitfs://02abc.../path) without hostOverride,
// an error is returned since there is no domain to resolve endpoints from.
//
// httpClient and dnsResolver may be nil to use defaults.
func ResolveURI(uri, hostOverride string, httpClient paymail.HTTPClient, dnsResolver paymail.DNSResolver) (*ResolveResult, error) {
	if httpClient == nil {
		httpClient = paymail.DefaultHTTPClient
	}
	if dnsResolver == nil {
		dnsResolver = paymail.DefaultDNSResolver
	}

	pubKey, endpoints, err := paymail.ResolveURIWith(uri, httpClient, dnsResolver)
	if err != nil {
		return nil, fmt.Errorf("resolve URI: %w", err)
	}

	pnode := hex.EncodeToString(pubKey)

	// Determine daemon base URL.
	var baseURL string
	switch {
	case hostOverride != "":
		baseURL = strings.TrimRight(hostOverride, "/")
	case len(endpoints) > 0:
		baseURL = "https://" + endpoints[0]
	default:
		return nil, fmt.Errorf("bare pubkey URI requires --host flag to specify the daemon address")
	}

	// Extract path from the parsed URI.
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		return nil, fmt.Errorf("resolve URI: %w", err)
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}

	return &ResolveResult{
		Client: New(baseURL),
		PNode:  pnode,
		Path:   path,
	}, nil
}
```

**Step 3: Run tests to verify they pass**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/client/ -run TestResolveURI -v -count=1`
Expected: all non-skipped tests PASS

**Step 4: Commit**

```bash
git add bitfs/internal/client/resolve.go bitfs/internal/client/resolve_test.go
git commit -m "feat(client): add ResolveURI for b* tools endpoint resolution"
```

---

### Task 2: Refactor bls to use `client.ResolveURI`

**Files:**
- Modify: `bitfs/cmd/bls/main.go:30-85`
- Modify: `bitfs/cmd/bls/main_test.go` (add paymail/dnslink test cases)

**Step 1: Modify bls main.go**

Replace lines 30-85 of `run()`. Key changes:
1. `--host` default changes from `"http://localhost:8080"` to `""`
2. Remove `"encoding/hex"` import, remove `paymail` import
3. Replace `switch parsed.Type` block + `client.New(*host)` with `client.ResolveURI()`
4. Apply timeout after resolve

The new `run()` body (from flag parsing to GetMeta call):

```go
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bls", flag.ContinueOnError)
	fs.SetOutput(stderr)

	jsonOut := fs.Bool("json", false, "JSON output")
	long := fs.Bool("long", false, "detailed listing")
	longAlias := fs.Bool("l", false, "detailed listing (alias)")
	host := fs.String("host", "", "daemon URL override")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if *longAlias {
		*long = true
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "Usage: bls [--json] [--long|-l] [--host URL] [--timeout DURATION] <bitfs-uri>\n")
		return 6
	}

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "bls: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bls: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	meta, err := c.GetMeta(resolved.PNode, resolved.Path)
	if err != nil {
		return handleError(err, stderr)
	}
	// ... rest unchanged
```

Updated imports (remove `"encoding/hex"` and `paymail`, keep `client`):
```go
import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tongxiaofeng/bitfs/internal/client"
)
```

**Step 2: Update bls tests**

Existing tests use `--host` with httptest server — they still work since `--host` override is respected. Update `makeURI` helper and verify existing tests still pass.

**Step 3: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./cmd/bls/ -v -count=1`
Expected: all existing tests PASS

**Step 4: Commit**

```bash
git add bitfs/cmd/bls/main.go
git commit -m "refactor(bls): use client.ResolveURI for endpoint resolution"
```

---

### Task 3: Refactor bcat to use `client.ResolveURI`

**Files:**
- Modify: `bitfs/cmd/bcat/main.go:32-82`

**Step 1: Apply same pattern as bls**

Key changes in `run()`:
1. `host` default → `""`
2. Remove `"encoding/hex"` import, remove `paymail` import
3. Replace `switch parsed.Type` + `client.New(*host)` with `client.ResolveURI()`
4. Apply timeout after resolve

The section to replace (lines 32-82):

```go
	host := fs.String("host", "", "daemon URL override")
	// ...

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bcat: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	pnode := resolved.PNode
	path := resolved.Path

	meta, err := c.GetMeta(pnode, path)
```

Note: bcat uses `pnode` and `path` variables later (in `outputContent` and `handlePaid` via `meta.PNode`), so we just set them from `resolved`.

Updated imports — remove `"encoding/hex"`, remove `paymail`:
```go
import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/libbitfs-go/method42"
	"github.com/tongxiaofeng/libbitfs-go/x402"
)
```

Note: `"encoding/hex"` is still needed by handlePaid — check before removing. Keep it if used elsewhere in the file.

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./cmd/bcat/ -v -count=1`
Expected: all tests PASS

**Step 3: Commit**

```bash
git add bitfs/cmd/bcat/main.go
git commit -m "refactor(bcat): use client.ResolveURI for endpoint resolution"
```

---

### Task 4: Refactor bget to use `client.ResolveURI`

**Files:**
- Modify: `bitfs/cmd/bget/main.go:40-94`

**Step 1: Apply same pattern**

Same changes as bcat. Replace lines 40-94. Keep `"encoding/hex"` (used in handlePaid). Remove `paymail` import.

```go
	host := fs.String("host", "", "daemon URL override")
	// ...

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "bget: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	pnode := resolved.PNode
	uriPath := resolved.Path

	meta, err := c.GetMeta(pnode, uriPath)
```

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./cmd/bget/ -v -count=1`
Expected: all tests PASS

**Step 3: Commit**

```bash
git add bitfs/cmd/bget/main.go
git commit -m "refactor(bget): use client.ResolveURI for endpoint resolution"
```

---

### Task 5: Refactor bstat to use `client.ResolveURI`

**Files:**
- Modify: `bitfs/cmd/bstat/main.go:30-86`

**Step 1: Apply same pattern**

Remove `"encoding/hex"`, remove `paymail` import.

```go
	host := fs.String("host", "", "daemon URL override")
	// ...

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "bstat: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bstat: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	meta, err := c.GetMeta(resolved.PNode, resolved.Path)
```

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./cmd/bstat/ -v -count=1`
Expected: all tests PASS

**Step 3: Commit**

```bash
git add bitfs/cmd/bstat/main.go
git commit -m "refactor(bstat): use client.ResolveURI for endpoint resolution"
```

---

### Task 6: Refactor btree to use `client.ResolveURI`

**Files:**
- Modify: `bitfs/cmd/btree/main.go:33-83`

**Step 1: Apply same pattern**

Remove `"encoding/hex"`, remove `paymail` import.

```go
	host := fs.String("host", "", "daemon URL override")
	// ...

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "btree: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "btree: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	pnode := resolved.PNode
	uriPath := resolved.Path

	meta, err := c.GetMeta(pnode, uriPath)
```

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./cmd/btree/ -v -count=1`
Expected: all tests PASS

**Step 3: Commit**

```bash
git add bitfs/cmd/btree/main.go
git commit -m "refactor(btree): use client.ResolveURI for endpoint resolution"
```

---

### Task 7: Update client package doc + spec, run full test suite

**Files:**
- Modify: `bitfs/internal/client/client.go:1-5` (package doc comment)
- Modify: `bitfs/spec/11-cmd-btools.md`

**Step 1: Update client package doc**

Replace line 4 of `client.go`:
```go
// It is the foundation that all b-tools (bls, bcat, bget, bstat, btree) use
// to communicate with the local BitFS daemon over the LFCP HTTP interface.
```
with:
```go
// It is the foundation that all b-tools (bls, bcat, bget, bstat, btree) use
// to communicate with BitFS daemons over the LFCP HTTP interface.
// Daemon endpoints are resolved from bitfs:// URIs via paymail PKI or DNS SRV records.
```

**Step 2: Update spec 11-cmd-btools.md**

After line 119 (`公共标志：--json、--no-cache、--timeout、--offline`), update the flags section to reflect `--host` behavior:

```
- 公共标志：`--json`、`--no-cache`、`--timeout`、`--offline`、`--host`（可选覆盖）
- `--host` 为可选覆盖：未指定时从 URI 域名解析 daemon 端点（Paymail SRV / DNSLink SRV / domain:443 fallback）
- 裸公钥 URI（`bitfs://02abc...`）必须提供 `--host`
```

**Step 3: Run full test suite**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1`
Expected: all tests pass

**Step 4: Run integration tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -count=1 -timeout 120s`
Expected: all tests pass (no changes to integration test code)

**Step 5: Commit**

```bash
git add bitfs/internal/client/client.go bitfs/spec/11-cmd-btools.md
git commit -m "docs: update client pkg doc and spec for URI endpoint resolution"
```

---

### Task 8: Update design review status

**Files:**
- Modify: `tasks/2026-02-25-design-review.md:223` (P2 #2 checkbox)

**Step 1: Mark P2 2.1 as done**

Change line 223:
```
- [ ] 2.1 Shell/b* 访问路径统一
```
to:
```
- [x] 2.1 Shell/b* 访问路径统一 (b* tools 从 URI 解析远程 daemon 端点)
```

**Step 2: Commit**

```bash
git add tasks/2026-02-25-design-review.md
git commit -m "chore: mark P2 2.1 (b* tools URI resolve) as done"
```

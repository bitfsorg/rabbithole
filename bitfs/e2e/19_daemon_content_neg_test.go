//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// setupContentNegDaemon creates a daemon with mock MetanetService for content
// negotiation tests. Returns the httptest.Server and cleanup function.
func setupContentNegDaemon(t *testing.T, nodes map[string]*daemon.NodeInfo, x402Enabled bool) *httptest.Server {
	t.Helper()

	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err)

	fileStore, err := storage.NewFileStore(t.TempDir())
	require.NoError(t, err)

	walletSvc := &testWalletService{w: w}
	metanetSvc := &testMetanetService{nodes: nodes}

	config := daemon.DefaultConfig()
	config.Security.RateLimit.RPM = 0 // disable rate limiting for tests
	config.X402.Enabled = x402Enabled

	d, err := daemon.New(config, walletSvc, fileStore, metanetSvc)
	require.NoError(t, err)

	server := httptest.NewServer(d.Handler())
	t.Cleanup(server.Close)
	return server
}

// TestContentNegDeepPath verifies content negotiation for a directory with
// 3 levels of nesting. Each level should be independently accessible and
// return correct HTML directory listings showing their children.
func TestContentNegDeepPath(t *testing.T) {
	nodes := map[string]*daemon.NodeInfo{
		"/": {
			Type:   "dir",
			Access: "free",
			Children: []daemon.ChildInfo{
				{Name: "projects", Type: "dir"},
			},
		},
		"/projects": {
			Type:   "dir",
			Access: "free",
			Children: []daemon.ChildInfo{
				{Name: "bitfs", Type: "dir"},
				{Name: "metanet", Type: "dir"},
			},
		},
		"/projects/bitfs": {
			Type:   "dir",
			Access: "free",
			Children: []daemon.ChildInfo{
				{Name: "src", Type: "dir"},
				{Name: "README.md", Type: "file"},
			},
		},
		"/projects/bitfs/src": {
			Type:   "dir",
			Access: "free",
			Children: []daemon.ChildInfo{
				{Name: "main.go", Type: "file"},
				{Name: "handler.go", Type: "file"},
			},
		},
		"/projects/bitfs/README.md": {
			Type:     "file",
			MimeType: "text/markdown",
			FileSize: 2048,
			Access:   "free",
		},
		"/projects/bitfs/src/main.go": {
			Type:     "file",
			MimeType: "text/x-go",
			FileSize: 4096,
			Access:   "free",
		},
	}

	server := setupContentNegDaemon(t, nodes, false)

	// Sub-test: root level HTML lists "projects" child.
	t.Run("root_html_lists_projects", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/html")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		html := string(body)

		assert.Contains(t, html, "<!DOCTYPE html>")
		assert.Contains(t, html, "projects")
		assert.Contains(t, html, "(dir)")
		t.Logf("root HTML contains 'projects' child entry")
	})

	// Sub-test: level-2 directory lists its two children.
	t.Run("level2_html_lists_children", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/projects", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/html")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		html := string(body)

		assert.Contains(t, html, "bitfs")
		assert.Contains(t, html, "metanet")
		t.Logf("level-2 (/projects) lists both children: bitfs, metanet")
	})

	// Sub-test: level-3 directory lists files and subdirectories.
	t.Run("level3_html_lists_mixed_children", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/projects/bitfs", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/html")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		html := string(body)

		assert.Contains(t, html, "src")
		assert.Contains(t, html, "(dir)")
		assert.Contains(t, html, "README.md")
		assert.Contains(t, html, "(file)")
		t.Logf("level-3 (/projects/bitfs) lists src (dir) + README.md (file)")
	})

	// Sub-test: deepest directory via JSON returns correct children.
	t.Run("level4_json_deep_directory", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/projects/bitfs/src", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

		var nodeInfo daemon.NodeInfo
		err = json.NewDecoder(resp.Body).Decode(&nodeInfo)
		require.NoError(t, err)

		assert.Equal(t, "dir", nodeInfo.Type)
		require.Len(t, nodeInfo.Children, 2)
		assert.Equal(t, "main.go", nodeInfo.Children[0].Name)
		assert.Equal(t, "handler.go", nodeInfo.Children[1].Name)
		t.Logf("deep JSON dir has 2 children: %v", nodeInfo.Children)
	})

	// Sub-test: file at depth 3 returns correct metadata.
	t.Run("deep_file_metadata", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/projects/bitfs/README.md", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var nodeInfo daemon.NodeInfo
		err = json.NewDecoder(resp.Body).Decode(&nodeInfo)
		require.NoError(t, err)

		assert.Equal(t, "file", nodeInfo.Type)
		assert.Equal(t, "text/markdown", nodeInfo.MimeType)
		assert.Equal(t, uint64(2048), nodeInfo.FileSize)
		t.Logf("deep file metadata: type=%s mime=%s size=%d", nodeInfo.Type, nodeInfo.MimeType, nodeInfo.FileSize)
	})
}

// TestContentNegPaidFile402 verifies that accessing a paid file with x402
// enabled returns 402 Payment Required with appropriate x402 headers.
func TestContentNegPaidFile402(t *testing.T) {
	// Generate a valid public key for the paid file node.
	privKey, err := ec.NewPrivateKey()
	require.NoError(t, err)
	pubKey := privKey.PubKey()

	nodes := map[string]*daemon.NodeInfo{
		"/": {
			Type:   "dir",
			Access: "free",
			Children: []daemon.ChildInfo{
				{Name: "secret.pdf", Type: "file"},
			},
		},
		"/secret.pdf": {
			PNode:      pubKey.Compressed(),
			Type:       "file",
			MimeType:   "application/pdf",
			FileSize:   102400, // 100 KB
			Access:     "paid",
			PricePerKB: 10,
			KeyHash:    make([]byte, 32), // dummy hash
		},
	}

	server := setupContentNegDaemon(t, nodes, true)

	// Sub-test: GET with Accept: text/html still returns 402 for paid files.
	t.Run("html_request_returns_402", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/secret.pdf", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/html")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusPaymentRequired, resp.StatusCode,
			"paid file should return 402 regardless of Accept header")

		// Verify x402 headers are present.
		assert.NotEmpty(t, resp.Header.Get("X-Price"), "X-Price header should be set")
		assert.NotEmpty(t, resp.Header.Get("X-Price-Per-KB"), "X-Price-Per-KB header should be set")
		assert.NotEmpty(t, resp.Header.Get("X-File-Size"), "X-File-Size header should be set")
		assert.NotEmpty(t, resp.Header.Get("X-Invoice-Id"), "X-Invoice-Id header should be set")
		assert.NotEmpty(t, resp.Header.Get("X-Expiry"), "X-Expiry header should be set")
		t.Logf("402 headers: Price=%s PricePerKB=%s FileSize=%s InvoiceID=%s",
			resp.Header.Get("X-Price"),
			resp.Header.Get("X-Price-Per-KB"),
			resp.Header.Get("X-File-Size"),
			resp.Header.Get("X-Invoice-Id"))
	})

	// Sub-test: GET with Accept: application/json also returns 402.
	t.Run("json_request_returns_402", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/secret.pdf", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusPaymentRequired, resp.StatusCode)

		// Verify response body contains invoice details.
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		bodyStr := string(body)

		assert.Contains(t, bodyStr, "payment required")
		assert.Contains(t, bodyStr, "invoice_id")
		assert.Contains(t, bodyStr, "total_price")
		t.Logf("402 JSON body: %s", bodyStr[:min(len(bodyStr), 200)])
	})

	// Sub-test: default Accept (no header) also returns 402.
	t.Run("default_request_returns_402", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/secret.pdf", nil)
		require.NoError(t, err)
		// No Accept header set.

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusPaymentRequired, resp.StatusCode,
			"paid file should return 402 even without Accept header")
	})

	// Sub-test: x402 disabled should NOT return 402 (falls through to content negotiation).
	t.Run("x402_disabled_serves_normally", func(t *testing.T) {
		serverNoX402 := setupContentNegDaemon(t, nodes, false)

		req, err := http.NewRequest("GET", serverNoX402.URL+"/secret.pdf", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// When x402 is disabled, paid files are served normally via content negotiation.
		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"with x402 disabled, paid file should serve normally")
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

		var nodeInfo daemon.NodeInfo
		err = json.NewDecoder(resp.Body).Decode(&nodeInfo)
		require.NoError(t, err)
		assert.Equal(t, "file", nodeInfo.Type)
		assert.Equal(t, "paid", nodeInfo.Access)
		t.Logf("x402 disabled: served normally with access=%s", nodeInfo.Access)
	})
}

// TestContentNegEmptyDirectory verifies that an empty directory (no children)
// returns valid HTML with an empty list, valid JSON with an empty children
// array, and valid Markdown with no entries.
func TestContentNegEmptyDirectory(t *testing.T) {
	nodes := map[string]*daemon.NodeInfo{
		"/": {
			Type:     "dir",
			Access:   "free",
			Children: []daemon.ChildInfo{}, // explicitly empty
		},
	}

	server := setupContentNegDaemon(t, nodes, false)

	// Sub-test: HTML shows empty directory structure.
	t.Run("empty_dir_html", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/html")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		html := string(body)

		assert.Contains(t, html, "<!DOCTYPE html>")
		assert.Contains(t, html, "Directory")
		assert.Contains(t, html, "<ul>")
		assert.Contains(t, html, "</ul>")
		// Empty directory should not contain any <li> entries.
		assert.NotContains(t, html, "<li>")
		t.Logf("empty dir HTML: %d bytes, no <li> entries", len(body))
	})

	// Sub-test: JSON returns dir with empty or nil children.
	t.Run("empty_dir_json", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var nodeInfo daemon.NodeInfo
		err = json.NewDecoder(resp.Body).Decode(&nodeInfo)
		require.NoError(t, err)

		assert.Equal(t, "dir", nodeInfo.Type)
		assert.Empty(t, nodeInfo.Children, "empty dir should have no children in JSON")
		t.Logf("empty dir JSON: type=%s, children=%d", nodeInfo.Type, len(nodeInfo.Children))
	})

	// Sub-test: Markdown returns directory heading with no entries.
	t.Run("empty_dir_markdown", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/markdown")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		md := string(body)

		assert.Contains(t, md, "# Directory Listing")
		// Should not contain any bullet point entries.
		assert.NotContains(t, md, "- ")
		t.Logf("empty dir markdown: %q", md)
	})
}

// TestContentNegMultipleFiles verifies that a directory with many children
// (5+ entries) lists all of them correctly across all content types.
func TestContentNegMultipleFiles(t *testing.T) {
	children := []daemon.ChildInfo{
		{Name: "alpha.txt", Type: "file"},
		{Name: "beta.go", Type: "file"},
		{Name: "gamma", Type: "dir"},
		{Name: "delta.rs", Type: "file"},
		{Name: "epsilon.py", Type: "file"},
		{Name: "zeta.md", Type: "file"},
		{Name: "eta", Type: "dir"},
	}

	nodes := map[string]*daemon.NodeInfo{
		"/": {
			Type:   "dir",
			Access: "free",
			Children: []daemon.ChildInfo{
				{Name: "workspace", Type: "dir"},
			},
		},
		"/workspace": {
			Type:     "dir",
			Access:   "free",
			Children: children,
		},
	}

	server := setupContentNegDaemon(t, nodes, false)

	// Sub-test: HTML listing contains all 7 children.
	t.Run("html_lists_all_children", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/workspace", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/html")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		html := string(body)

		for _, child := range children {
			assert.Contains(t, html, child.Name,
				"HTML should contain child: %s", child.Name)
			assert.Contains(t, html, "("+child.Type+")",
				"HTML should contain type annotation for: %s", child.Name)
		}
		t.Logf("HTML listing contains all %d children", len(children))
	})

	// Sub-test: JSON listing contains all 7 children with correct types.
	t.Run("json_lists_all_children", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/workspace", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var nodeInfo daemon.NodeInfo
		err = json.NewDecoder(resp.Body).Decode(&nodeInfo)
		require.NoError(t, err)

		assert.Equal(t, "dir", nodeInfo.Type)
		require.Len(t, nodeInfo.Children, len(children),
			"JSON should contain exactly %d children", len(children))

		// Build a name->type map from the response for verification.
		childMap := make(map[string]string, len(nodeInfo.Children))
		for _, c := range nodeInfo.Children {
			childMap[c.Name] = c.Type
		}

		for _, expected := range children {
			gotType, ok := childMap[expected.Name]
			assert.True(t, ok, "child %q should exist in JSON response", expected.Name)
			assert.Equal(t, expected.Type, gotType,
				"child %q type mismatch", expected.Name)
		}
		t.Logf("JSON listing has %d children, all types correct", len(nodeInfo.Children))
	})

	// Sub-test: Markdown listing contains all 7 children.
	t.Run("markdown_lists_all_children", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/workspace", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/markdown")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		md := string(body)

		assert.Contains(t, md, "# Directory Listing")

		for _, child := range children {
			assert.Contains(t, md, child.Name,
				"Markdown should contain child: %s", child.Name)
			assert.Contains(t, md, "("+child.Type+")",
				"Markdown should contain type for: %s", child.Name)
		}
		t.Logf("Markdown listing contains all %d children", len(children))
	})

	// Sub-test: verify file vs dir type annotations are distinct in HTML.
	t.Run("html_distinguishes_file_and_dir", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/workspace", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/html")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		html := string(body)

		// Count file and dir annotations.
		fileCount := countOccurrences(html, "(file)")
		dirCount := countOccurrences(html, "(dir)")

		assert.Equal(t, 5, fileCount, "should have 5 file entries")
		assert.Equal(t, 2, dirCount, "should have 2 dir entries")
		t.Logf("type annotation counts: file=%d dir=%d", fileCount, dirCount)
	})
}

// countOccurrences counts how many times substr appears in s.
func countOccurrences(s, substr string) int {
	count := 0
	idx := 0
	for {
		i := indexOf(s[idx:], substr)
		if i < 0 {
			break
		}
		count++
		idx += i + len(substr)
	}
	return count
}

// indexOf returns the index of substr in s, or -1 if not found.
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/client"
)

// testPubKey is a well-known compressed public key hex (33 bytes, prefix 02).
const testPubKey = "02b4632d08485ff1df2db55b9dafd23347d1c47a457072a1e87be26896549a8737"

func makeURI(path string) string {
	if path == "" || path == "/" {
		return "bitfs://" + testPubKey
	}
	return "bitfs://" + testPubKey + path
}

// newMockDaemon creates an httptest.Server that routes /_bitfs/meta/ requests
// based on the URL path, using a map from path to MetaResponse.
func newMockDaemon(t *testing.T, responses map[string]client.MetaResponse) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		// Extract the path after /_bitfs/meta/<pnode>
		prefix := fmt.Sprintf("/_bitfs/meta/%s", testPubKey)
		reqPath := strings.TrimPrefix(r.URL.Path, prefix)
		if reqPath == "" {
			reqPath = "/"
		}

		resp, ok := responses[reqPath]
		if !ok {
			http.Error(w, "no such path", http.StatusNotFound)
			return
		}
		serveJSON(w, resp)
	})
	return httptest.NewServer(mux)
}

// newMockDaemonHandler creates an httptest.Server with a custom handler.
func newMockDaemonHandler(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", handler)
	return httptest.NewServer(mux)
}

// serveJSON is a helper that writes a JSON response.
func serveJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// Simple tree with one level of children
// ---------------------------------------------------------------------------

func TestSimpleTree_OneLevel(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "hello.txt", Type: "file"},
				{Name: "world.txt", Type: "file"},
				{Name: "docs", Type: "dir"},
			},
		},
		"/docs": {
			PNode:    testPubKey,
			Type:     "dir",
			Path:     "/docs",
			Access:   "free",
			Children: []client.ChildEntry{},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	out := stdout.String()
	// Root name
	assert.True(t, strings.HasPrefix(out, "/\n"), "should start with root /")
	// Tree connectors
	assert.Contains(t, out, "\u251c\u2500\u2500 hello.txt")
	assert.Contains(t, out, "\u251c\u2500\u2500 world.txt")
	assert.Contains(t, out, "\u2514\u2500\u2500 docs/")
	// Summary line
	assert.Contains(t, out, "1 directory, 2 files")
}

// ---------------------------------------------------------------------------
// Nested tree (2+ levels deep)
// ---------------------------------------------------------------------------

func TestNestedTree_MultipleDepths(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "hello.txt", Type: "file"},
				{Name: "docs", Type: "dir"},
				{Name: "images", Type: "dir"},
			},
		},
		"/docs": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/docs",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "readme.md", Type: "file"},
				{Name: "notes.txt", Type: "file"},
			},
		},
		"/images": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/images",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "logo.png", Type: "file"},
			},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())

	out := stdout.String()
	// Check tree structure
	assert.Contains(t, out, "\u251c\u2500\u2500 hello.txt")
	assert.Contains(t, out, "\u251c\u2500\u2500 docs/")
	assert.Contains(t, out, "\u2502   \u251c\u2500\u2500 readme.md")
	assert.Contains(t, out, "\u2502   \u2514\u2500\u2500 notes.txt")
	assert.Contains(t, out, "\u2514\u2500\u2500 images/")
	assert.Contains(t, out, "    \u2514\u2500\u2500 logo.png")
	// Summary
	assert.Contains(t, out, "2 directories, 4 files")
}

// ---------------------------------------------------------------------------
// Depth-limited tree
// ---------------------------------------------------------------------------

func TestDepthLimited_OnlyDirectChildren(t *testing.T) {
	// With -d 1, only direct children should be listed, no recursion.
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "a.txt", Type: "file"},
				{Name: "sub", Type: "dir"},
			},
		},
		// This should NOT be fetched when depth=1.
		"/sub": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/sub",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "deep.txt", Type: "file"},
			},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-d", "1", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "\u251c\u2500\u2500 a.txt")
	assert.Contains(t, out, "\u2514\u2500\u2500 sub/")
	// Should NOT contain deep.txt since depth is 1.
	assert.NotContains(t, out, "deep.txt")
	// Summary still counts the directory even if not recursed.
	assert.Contains(t, out, "1 directory, 1 file")
}

func TestDepthLimited_DepthTwo(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "level1", Type: "dir"},
			},
		},
		"/level1": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/level1",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "level2", Type: "dir"},
				{Name: "file1.txt", Type: "file"},
			},
		},
		"/level1/level2": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/level1/level2",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "file2.txt", Type: "file"},
			},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--depth", "2", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code)

	out := stdout.String()
	// Level 1 contents should be shown.
	assert.Contains(t, out, "file1.txt")
	assert.Contains(t, out, "level2/")
	// Level 2 contents should NOT be shown (depth 2 = levels 1 and 2,
	// but level2 directory children are at depth 3).
	assert.NotContains(t, out, "file2.txt")
}

// ---------------------------------------------------------------------------
// JSON output
// ---------------------------------------------------------------------------

func TestJSONOutput_TreeStructure(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "hello.txt", Type: "file"},
				{Name: "docs", Type: "dir"},
			},
		},
		"/docs": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/docs",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "readme.md", Type: "file"},
			},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	// Verify valid JSON.
	assert.True(t, json.Valid(stdout.Bytes()), "output should be valid JSON")

	// Parse and verify structure.
	var root treeNode
	err := json.Unmarshal(stdout.Bytes(), &root)
	require.NoError(t, err)

	assert.Equal(t, "/", root.Name)
	assert.Equal(t, "dir", root.Type)
	require.Len(t, root.Children, 2)

	assert.Equal(t, "hello.txt", root.Children[0].Name)
	assert.Equal(t, "file", root.Children[0].Type)

	assert.Equal(t, "docs", root.Children[1].Name)
	assert.Equal(t, "dir", root.Children[1].Type)
	require.Len(t, root.Children[1].Children, 1)
	assert.Equal(t, "readme.md", root.Children[1].Children[0].Name)
}

func TestJSONOutput_IsValidJSON(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:    testPubKey,
			Type:     "dir",
			Path:     "/",
			Access:   "free",
			Children: []client.ChildEntry{},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	run([]string{"--json", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.True(t, json.Valid(stdout.Bytes()), "output should be valid JSON")
}

// ---------------------------------------------------------------------------
// Single file (not directory) at root
// ---------------------------------------------------------------------------

func TestSingleFile_NotDirectory(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/hello.txt": {
			PNode:    testPubKey,
			Type:     "file",
			Path:     "/hello.txt",
			MimeType: "text/plain",
			FileSize: 1234,
			Access:   "free",
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "file target should exit 0; stderr: %s", stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "/hello.txt")
	assert.Contains(t, out, "free")
	assert.Contains(t, out, "1.2K")
	assert.Contains(t, out, "0 directories, 1 file")
}

func TestSingleFile_JSONOutput(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/data.bin": {
			PNode:      testPubKey,
			Type:       "file",
			Path:       "/data.bin",
			FileSize:   2048,
			Access:     "paid",
			PricePerKB: 100,
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/data.bin")}, &stdout, &stderr)

	assert.Equal(t, 0, code)

	var node treeNode
	err := json.Unmarshal(stdout.Bytes(), &node)
	require.NoError(t, err)
	assert.Equal(t, "data.bin", node.Name)
	assert.Equal(t, "file", node.Type)
	assert.Equal(t, "paid", node.Access)
	assert.Equal(t, uint64(2048), node.Size)
	assert.Equal(t, uint64(100), node.PricePerKB)
}

// ---------------------------------------------------------------------------
// Empty directory
// ---------------------------------------------------------------------------

func TestEmptyDirectory(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:    testPubKey,
			Type:     "dir",
			Path:     "/",
			Access:   "free",
			Children: []client.ChildEntry{},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code)

	out := stdout.String()
	assert.Contains(t, out, "/\n")
	assert.Contains(t, out, "0 directories, 0 files")
}

// ---------------------------------------------------------------------------
// Not found -> exit 2
// ---------------------------------------------------------------------------

func TestNotFound_Exit2(t *testing.T) {
	srv := newMockDaemonHandler(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such path", http.StatusNotFound)
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/nonexistent")}, &stdout, &stderr)

	assert.Equal(t, 2, code, "not found should exit 2")
	assert.Contains(t, stderr.String(), "not found")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Network error -> exit 4
// ---------------------------------------------------------------------------

func TestNetworkError_Exit4(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", "http://127.0.0.1:1", "--timeout", "1s", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "network error should exit 4")
	assert.Contains(t, stderr.String(), "network error")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Server error -> exit 4
// ---------------------------------------------------------------------------

func TestServerError_Exit4(t *testing.T) {
	srv := newMockDaemonHandler(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal failure", http.StatusInternalServerError)
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "server error should exit 4")
	assert.Contains(t, stderr.String(), "server error")
}

// ---------------------------------------------------------------------------
// Missing URI -> exit 6
// ---------------------------------------------------------------------------

func TestMissingURI_Exit6(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)

	assert.Equal(t, 6, code, "missing URI should exit 6")
	assert.Contains(t, stderr.String(), "Usage:")
	assert.Empty(t, stdout.String())
}

func TestMissingURI_NilArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)

	assert.Equal(t, 6, code, "nil args should exit 6")
	assert.Contains(t, stderr.String(), "Usage:")
}

// ---------------------------------------------------------------------------
// Invalid URI -> exit 6
// ---------------------------------------------------------------------------

func TestInvalidURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"http://not-a-bitfs-uri"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "invalid URI should exit 6")
	assert.Contains(t, stderr.String(), "btree:")
	assert.Empty(t, stdout.String())
}

func TestEmptyURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{""}, &stdout, &stderr)

	assert.Equal(t, 6, code, "empty URI should exit 6")
}

// ---------------------------------------------------------------------------
// Summary line present with correct counts
// ---------------------------------------------------------------------------

func TestSummaryLine_CorrectCounts(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "a.txt", Type: "file"},
				{Name: "b.txt", Type: "file"},
				{Name: "c.txt", Type: "file"},
				{Name: "sub1", Type: "dir"},
				{Name: "sub2", Type: "dir"},
			},
		},
		"/sub1": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/sub1",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "d.txt", Type: "file"},
				{Name: "sub3", Type: "dir"},
			},
		},
		"/sub1/sub3": {
			PNode:    testPubKey,
			Type:     "dir",
			Path:     "/sub1/sub3",
			Access:   "free",
			Children: []client.ChildEntry{},
		},
		"/sub2": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/sub2",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "e.txt", Type: "file"},
			},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())

	out := stdout.String()
	// 3 directories (sub1, sub2, sub3), 5 files (a, b, c, d, e)
	assert.Contains(t, out, "3 directories, 5 files")
}

// ---------------------------------------------------------------------------
// Paymail / DNSLink resolution errors
// ---------------------------------------------------------------------------

func TestPaymailResolveFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://alice@example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "paymail resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "btree:")
	assert.Empty(t, stdout.String())
}

func TestDNSLinkResolveFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "dnslink resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "btree:")
}

func TestPubKeyNoHost_RequiresHostFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{makeURI("/docs")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "--host")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Default path resolution
// ---------------------------------------------------------------------------

func TestDefaultPathIsRoot(t *testing.T) {
	var requestedPath string
	srv := newMockDaemonHandler(t, func(w http.ResponseWriter, r *http.Request) {
		prefix := fmt.Sprintf("/_bitfs/meta/%s", testPubKey)
		requestedPath = strings.TrimPrefix(r.URL.Path, prefix)
		if requestedPath == "" {
			requestedPath = "/"
		}
		serveJSON(w, client.MetaResponse{
			PNode:    testPubKey,
			Type:     "dir",
			Path:     "/",
			Access:   "free",
			Children: []client.ChildEntry{},
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, "bitfs://" + testPubKey}, &stdout, &stderr)

	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Equal(t, "/", requestedPath)
}

// ---------------------------------------------------------------------------
// Flag edge cases
// ---------------------------------------------------------------------------

func TestUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--unknown", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
}

func TestInvalidTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", "http://localhost:8080", "--timeout", "notaduration", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "invalid timeout")
}

func TestDepthFlag_ShortForm(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "sub", Type: "dir"},
			},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-d", "1", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "sub/")
}

func TestDepthFlag_LongForm(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "sub", Type: "dir"},
			},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--depth", "1", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "sub/")
}

// ---------------------------------------------------------------------------
// Tree indentation correctness
// ---------------------------------------------------------------------------

func TestTreeIndentation_DeepNesting(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "a", Type: "dir"},
			},
		},
		"/a": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/a",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "b", Type: "dir"},
			},
		},
		"/a/b": {
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/a/b",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "c.txt", Type: "file"},
			},
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.True(t, len(lines) >= 4, "expected at least 4 lines, got %d: %s", len(lines), stdout.String())

	// Line 0: /
	assert.Equal(t, "/", lines[0])
	// Line 1: └── a/
	assert.Contains(t, lines[1], "\u2514\u2500\u2500 a/")
	// Line 2: (indented) └── b/
	assert.Contains(t, lines[2], "\u2514\u2500\u2500 b/")
	assert.True(t, strings.HasPrefix(lines[2], "    "), "should be indented with spaces")
	// Line 3: (double indented) └── c.txt
	assert.Contains(t, lines[3], "\u2514\u2500\u2500 c.txt")
}

// ---------------------------------------------------------------------------
// File annotation formatting
// ---------------------------------------------------------------------------

func TestSingleFile_PaidAnnotation(t *testing.T) {
	responses := map[string]client.MetaResponse{
		"/premium.pdf": {
			PNode:      testPubKey,
			Type:       "file",
			Path:       "/premium.pdf",
			FileSize:   5242880,
			Access:     "paid",
			PricePerKB: 100,
		},
	}

	srv := newMockDaemon(t, responses)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	out := stdout.String()
	assert.Contains(t, out, "paid, 100 sat/KB")
}

// ---------------------------------------------------------------------------
// formatSize unit tests
// ---------------------------------------------------------------------------

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{0, "0"},
		{512, "512"},
		{1023, "1023"},
		{1024, "1.0K"},
		{1234, "1.2K"},
		{1536, "1.5K"},
		{1048576, "1.0M"},
		{1073741824, "1.0G"},
		{1099511627776, "1.0T"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.bytes), func(t *testing.T) {
			assert.Equal(t, tt.expected, formatSize(tt.bytes))
		})
	}
}

// ---------------------------------------------------------------------------
// formatFileAnnotation unit tests
// ---------------------------------------------------------------------------

func TestFormatFileAnnotation(t *testing.T) {
	tests := []struct {
		name       string
		access     string
		size       uint64
		pricePerKB uint64
		expected   string
	}{
		{"free with size", "free", 1234, 0, "free, 1.2K"},
		{"paid with price", "paid", 5000, 100, "paid, 100 sat/KB"},
		{"free zero size", "free", 0, 0, "free"},
		{"private", "private", 0, 0, "private"},
		{"free large file", "free", 1048576, 0, "free, 1.0M"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatFileAnnotation(tt.access, tt.size, tt.pricePerKB))
		})
	}
}

// ---------------------------------------------------------------------------
// Malformed paymail URI
// ---------------------------------------------------------------------------

func TestMalformedPaymailURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://@example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "missing alias should fail")
}

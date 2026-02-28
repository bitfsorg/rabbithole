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

// newMockDaemon creates an httptest.Server that serves /_bitfs/meta/ requests.
func newMockDaemon(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
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
// Default human-readable output — file node
// ---------------------------------------------------------------------------

func TestDefaultOutput_FileNode(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:    testPubKey,
			Type:     "file",
			Path:     "/hello.txt",
			MimeType: "text/plain",
			FileSize: 1234,
			KeyHash:  "abcdef1234567890",
			Access:   "free",
			TxID:     "deadbeef",
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "Path: /hello.txt")
	assert.Contains(t, out, "Type: file")
	assert.Contains(t, out, "Owner: "+testPubKey)
	assert.Contains(t, out, "Access: free")
	assert.Contains(t, out, "MIME: text/plain")
	assert.Contains(t, out, "Size: 1.2 KB")
	assert.Contains(t, out, "Hash: abcdef1234567890")
	assert.Contains(t, out, "TxID: deadbeef")
	// Files should NOT show Children count.
	assert.NotContains(t, out, "Children:")
}

func TestDefaultOutput_FileNode_MinimalFields(t *testing.T) {
	// When optional fields are empty/zero, they should be omitted.
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:  testPubKey,
			Type:   "file",
			Path:   "/empty.bin",
			Access: "private",
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/empty.bin")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	out := stdout.String()
	assert.Contains(t, out, "Path: /empty.bin")
	assert.Contains(t, out, "Type: file")
	assert.Contains(t, out, "Access: private")
	// Optional fields should be absent.
	assert.NotContains(t, out, "MIME:")
	assert.NotContains(t, out, "Size:")
	assert.NotContains(t, out, "Hash:")
	assert.NotContains(t, out, "TxID:")
	assert.NotContains(t, out, "PriceKB:")
}

// ---------------------------------------------------------------------------
// Default human-readable output — directory node
// ---------------------------------------------------------------------------

func TestDefaultOutput_DirectoryNode(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/docs",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "readme.md", Type: "file"},
				{Name: "images", Type: "dir"},
				{Name: "notes.txt", Type: "file"},
			},
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/docs")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "Path: /docs")
	assert.Contains(t, out, "Type: dir")
	assert.Contains(t, out, "Children: 3")
	// Directories should NOT show Size or Hash.
	assert.NotContains(t, out, "Size:")
	assert.NotContains(t, out, "Hash:")
}

func TestDefaultOutput_EmptyDirectory(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:    testPubKey,
			Type:     "dir",
			Path:     "/empty",
			Access:   "free",
			Children: []client.ChildEntry{},
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/empty")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), "Children: 0")
}

// ---------------------------------------------------------------------------
// Default output — paid file with price
// ---------------------------------------------------------------------------

func TestDefaultOutput_PaidFile(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:      testPubKey,
			Type:       "file",
			Path:       "/premium.pdf",
			MimeType:   "application/pdf",
			FileSize:   5242880,
			Access:     "paid",
			PricePerKB: 100,
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	out := stdout.String()
	assert.Contains(t, out, "Access: paid")
	assert.Contains(t, out, "PriceKB: 100 sat")
	assert.Contains(t, out, "Size: 5.0 MB")
}

// ---------------------------------------------------------------------------
// JSON output
// ---------------------------------------------------------------------------

func TestJSONOutput_FileNode(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:    testPubKey,
			Type:     "file",
			Path:     "/image.png",
			MimeType: "image/png",
			FileSize: 51200,
			KeyHash:  "abc123",
			Access:   "free",
			TxID:     "tx999",
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/image.png")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	var got client.MetaResponse
	err := json.Unmarshal(stdout.Bytes(), &got)
	require.NoError(t, err, "output should be valid JSON")
	assert.Equal(t, "file", got.Type)
	assert.Equal(t, "/image.png", got.Path)
	assert.Equal(t, uint64(51200), got.FileSize)
	assert.Equal(t, "image/png", got.MimeType)
	assert.Equal(t, "abc123", got.KeyHash)
	assert.Equal(t, "tx999", got.TxID)
}

func TestJSONOutput_DirectoryNode(t *testing.T) {
	meta := client.MetaResponse{
		PNode:  testPubKey,
		Type:   "dir",
		Path:   "/projects",
		Access: "free",
		Children: []client.ChildEntry{
			{Name: "alpha", Type: "dir"},
			{Name: "notes.md", Type: "file"},
		},
	}

	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, meta)
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/projects")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	assert.Empty(t, stderr.String())

	var got client.MetaResponse
	err := json.Unmarshal(stdout.Bytes(), &got)
	require.NoError(t, err)
	assert.Equal(t, "dir", got.Type)
	assert.Equal(t, "/projects", got.Path)
	assert.Len(t, got.Children, 2)
	assert.Equal(t, "alpha", got.Children[0].Name)
}

func TestJSONOutput_IsValidJSON(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	run([]string{"--json", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.True(t, json.Valid(stdout.Bytes()), "output should be valid JSON")
}

// ---------------------------------------------------------------------------
// --versions flag
// ---------------------------------------------------------------------------

// newMockDaemonMulti creates an httptest.Server that dispatches on URL prefix.
func newMockDaemonMulti(t *testing.T, handlers map[string]func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, handler := range handlers {
		mux.HandleFunc(pattern, handler)
	}
	return httptest.NewServer(mux)
}

func TestVersionsFlag_HumanOutput(t *testing.T) {
	versions := []client.VersionEntry{
		{Version: 1, TxID: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", BlockHeight: 800100, Timestamp: 1700000000, FileSize: 2048, Access: "free"},
		{Version: 2, TxID: "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff", BlockHeight: 800050, Timestamp: 1699000000, FileSize: 1024, Access: "paid"},
	}

	srv := newMockDaemonMulti(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/_bitfs/versions/": func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, versions)
		},
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--versions", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "Versions for /hello.txt (2 total):")
	assert.Contains(t, out, "v1")
	assert.Contains(t, out, "v2")
	assert.Contains(t, out, "abcdef1234567890...")
	assert.Contains(t, out, "[free]")
	assert.Contains(t, out, "[paid]")
	assert.Contains(t, out, "height=800100")
	assert.Contains(t, out, "height=800050")
}

func TestVersionsFlag_JSONOutput(t *testing.T) {
	versions := []client.VersionEntry{
		{Version: 1, TxID: "aabb", BlockHeight: 100, Timestamp: 1700000000, FileSize: 512, Access: "private"},
	}

	srv := newMockDaemonMulti(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/_bitfs/versions/": func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, versions)
		},
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--versions", "--json", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	var got []client.VersionEntry
	err := json.Unmarshal(stdout.Bytes(), &got)
	require.NoError(t, err, "output should be valid JSON")
	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].Version)
	assert.Equal(t, "aabb", got[0].TxID)
	assert.Equal(t, "private", got[0].Access)
}

func TestVersionsFlag_EmptyTxID(t *testing.T) {
	// Edge case: daemon returns empty TxID (stub implementation).
	versions := []client.VersionEntry{
		{Version: 1, TxID: "", BlockHeight: 0, Timestamp: 1700000000, Access: "free"},
	}

	srv := newMockDaemonMulti(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/_bitfs/versions/": func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, versions)
		},
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--versions", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	// Should not panic on short/empty TxID.
	assert.NotContains(t, stdout.String(), "...")
}

func TestVersionsFlag_ShortTxID(t *testing.T) {
	// Edge case: TxID is shorter than 16 chars — should display as-is.
	versions := []client.VersionEntry{
		{Version: 1, TxID: "shortid", BlockHeight: 42, Timestamp: 1700000000, Access: "free"},
	}

	srv := newMockDaemonMulti(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/_bitfs/versions/": func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, versions)
		},
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--versions", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	out := stdout.String()
	assert.Contains(t, out, "shortid")
	assert.NotContains(t, out, "shortid...")
}

func TestVersionsFlag_NoURIRequired(t *testing.T) {
	// --versions without URI should show usage (URI is needed for version query).
	var stdout, stderr bytes.Buffer
	code := run([]string{"--versions"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "missing URI should exit 6")
	assert.Contains(t, stderr.String(), "Usage:")
}

func TestVersionsFlag_ServerError(t *testing.T) {
	srv := newMockDaemonMulti(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/_bitfs/versions/": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		},
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--versions", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code, "server error should produce non-zero exit")
	assert.Contains(t, stderr.String(), "server error")
}

// ---------------------------------------------------------------------------
// Missing/invalid arguments
// ---------------------------------------------------------------------------

func TestMissingURIArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)

	assert.Equal(t, 6, code, "missing URI should exit 6")
	assert.Contains(t, stderr.String(), "Usage:")
	assert.Empty(t, stdout.String())
}

func TestMissingURIArgument_NilArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)

	assert.Equal(t, 6, code, "nil args should exit 6")
	assert.Contains(t, stderr.String(), "Usage:")
}

func TestInvalidURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"http://not-a-bitfs-uri"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "invalid URI should exit 6")
	assert.Contains(t, stderr.String(), "bstat:")
	assert.Empty(t, stdout.String())
}

func TestEmptyURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{""}, &stdout, &stderr)

	assert.Equal(t, 6, code, "empty URI should exit 6")
}

// ---------------------------------------------------------------------------
// Paymail / DNSLink resolution errors
// ---------------------------------------------------------------------------

func TestPaymailResolveFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://alice@example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "paymail resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "bstat:")
	assert.Empty(t, stdout.String())
}

func TestDNSLinkResolveFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "dnslink resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "bstat:")
}

func TestPubKeyNoHost_RequiresHostFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{makeURI("/docs")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "--host")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Error handling / exit codes
// ---------------------------------------------------------------------------

func TestNotFoundError(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such path", http.StatusNotFound)
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/nonexistent")}, &stdout, &stderr)

	assert.Equal(t, 2, code, "not found should exit 2")
	assert.Contains(t, stderr.String(), "not found")
	assert.Empty(t, stdout.String())
}

func TestNetworkError(t *testing.T) {
	// Use a host that is guaranteed to refuse connections.
	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", "http://127.0.0.1:1", "--timeout", "1s", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "network error should exit 4")
	assert.Contains(t, stderr.String(), "network error")
	assert.Empty(t, stdout.String())
}

func TestServerError(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal failure", http.StatusInternalServerError)
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "server error should exit 4")
	assert.Contains(t, stderr.String(), "server error")
}

// ---------------------------------------------------------------------------
// Default path resolution
// ---------------------------------------------------------------------------

func TestDefaultPathIsRoot(t *testing.T) {
	var requestedPath string
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		prefix := fmt.Sprintf("/_bitfs/meta/%s/", testPubKey)
		requestedPath = r.URL.Path[len(prefix)-1:] // keep leading /
		serveJSON(w, client.MetaResponse{
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
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

// ---------------------------------------------------------------------------
// formatSize unit tests
// ---------------------------------------------------------------------------

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{0, "0"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1234, "1.2 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{5242880, "5.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.bytes), func(t *testing.T) {
			assert.Equal(t, tt.expected, formatSize(tt.bytes))
		})
	}
}

// ---------------------------------------------------------------------------
// Output label alignment
// ---------------------------------------------------------------------------

func TestOutputLabelsAreRightAligned(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:    testPubKey,
			Type:     "file",
			Path:     "/test.txt",
			MimeType: "text/plain",
			FileSize: 1024,
			Access:   "free",
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/test.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code)

	// Split output into lines, filtering out empty trailing lines.
	allLines := strings.Split(stdout.String(), "\n")
	var lines []string
	for _, line := range allLines {
		if line != "" {
			lines = append(lines, line)
		}
	}
	require.NotEmpty(t, lines)
	// All colon positions should be at the same column.
	for _, line := range lines {
		colonIdx := strings.Index(line, ":")
		require.NotEqual(t, -1, colonIdx, "line should contain colon: %q", line)
		// The colon should be at position 8 (0-indexed) for all labels.
		assert.Equal(t, 8, colonIdx, "colon should be at column 8 in line: %q", line)
	}
}

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
// Default text output
// ---------------------------------------------------------------------------

func TestDefaultOutput_Directory(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "docs", Type: "dir"},
				{Name: "readme.txt", Type: "file"},
				{Name: "music", Type: "dir"},
			},
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "docs/", lines[0])
	assert.Equal(t, "readme.txt", lines[1])
	assert.Equal(t, "music/", lines[2])
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
	assert.Empty(t, stdout.String())
}

func TestDefaultOutput_FileNode(t *testing.T) {
	// When bls is pointed at a file (not a directory), it should
	// print the file path.
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:    testPubKey,
			Type:     "file",
			Path:     "/readme.txt",
			MimeType: "text/plain",
			FileSize: 1234,
			Access:   "free",
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/readme.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "file listing should succeed; stderr: %s", stderr.String())
	assert.Equal(t, "/readme.txt\n", stdout.String())
}

// ---------------------------------------------------------------------------
// JSON output
// ---------------------------------------------------------------------------

func TestJSONOutput_Directory(t *testing.T) {
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

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	var got client.MetaResponse
	err := json.Unmarshal(stdout.Bytes(), &got)
	require.NoError(t, err, "output should be valid JSON")
	assert.Equal(t, "dir", got.Type)
	assert.Equal(t, "/projects", got.Path)
	assert.Len(t, got.Children, 2)
	assert.Equal(t, "alpha", got.Children[0].Name)
	assert.Equal(t, "notes.md", got.Children[1].Name)
}

func TestJSONOutput_FileNode(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:    testPubKey,
			Type:     "file",
			Path:     "/image.png",
			MimeType: "image/png",
			FileSize: 51200,
			Access:   "free",
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/image.png")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())

	var got client.MetaResponse
	err := json.Unmarshal(stdout.Bytes(), &got)
	require.NoError(t, err)
	assert.Equal(t, "file", got.Type)
	assert.Equal(t, "/image.png", got.Path)
	assert.Equal(t, uint64(51200), got.FileSize)
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
// Long format output
// ---------------------------------------------------------------------------

func TestLongOutput_Directory(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "photos", Type: "dir"},
				{Name: "hello.txt", Type: "file"},
			},
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--long", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.Len(t, lines, 2)

	// First line: directory child
	assert.Contains(t, lines[0], "dir")
	assert.Contains(t, lines[0], "photos/")

	// Second line: file child
	assert.Contains(t, lines[1], "file")
	assert.Contains(t, lines[1], "hello.txt")
}

func TestLongOutput_FileNode(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:    testPubKey,
			Type:     "file",
			Path:     "/data.bin",
			FileSize: 2048,
			Access:   "paid",
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--long", "--host", srv.URL, makeURI("/data.bin")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "file")
	assert.Contains(t, out, "paid")
	assert.Contains(t, out, "2.0K")
	assert.Contains(t, out, "/data.bin")
}

func TestLongAlias_L(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "a.txt", Type: "file"},
			},
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-l", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	// -l should produce long format output (tab-separated fields)
	assert.Contains(t, stdout.String(), "file\t")
	assert.Contains(t, stdout.String(), "a.txt")
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
	assert.Contains(t, stderr.String(), "bls:")
	assert.Empty(t, stdout.String())
}

func TestEmptyURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{""}, &stdout, &stderr)

	assert.Equal(t, 6, code, "empty URI should exit 6")
}

func TestMalformedPaymailURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://@example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "missing alias should fail")
}

// ---------------------------------------------------------------------------
// Paymail / DNSLink resolution errors
// ---------------------------------------------------------------------------

func TestPaymailResolveFails(t *testing.T) {
	// Paymail URI without --host will try PKI resolution, which fails
	// because there is no reachable Paymail server at example.com.
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://alice@example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "paymail resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "bls:")
	assert.Empty(t, stdout.String())
}

func TestDNSLinkResolveFails(t *testing.T) {
	// DNSLink URI without --host will try DNS TXT + SRV resolution,
	// which fails because there are no BitFS DNS records at example.com.
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "dnslink resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "bls:")
	assert.Empty(t, stdout.String())
}

func TestPubKeyNoHost_RequiresHostFlag(t *testing.T) {
	// Bare pubkey URI without --host should fail with a helpful message.
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
	// When no path is given in the URI (just bitfs://<pubkey>), the
	// command should query "/" by default.
	var requestedPath string
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		// Extract path after /_bitfs/meta/<pnode>/
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
	// URI without path: bitfs://<pubkey>
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

func TestJSONTakesPrecedenceOverLong(t *testing.T) {
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:  testPubKey,
			Type:   "dir",
			Path:   "/",
			Access: "free",
			Children: []client.ChildEntry{
				{Name: "x.txt", Type: "file"},
			},
		})
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--long", "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	// When both --json and --long are given, --json takes precedence
	assert.True(t, json.Valid(stdout.Bytes()), "output should be valid JSON")
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

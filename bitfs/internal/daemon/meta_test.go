package daemon

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// metaResponse is a helper struct for unmarshalling handleMeta JSON responses.
type metaResponse struct {
	PNode      string      `json:"pnode"`
	Path       string      `json:"path"`
	Type       string      `json:"type"`
	Access     string      `json:"access"`
	MimeType   string      `json:"mime_type,omitempty"`
	FileSize   uint64      `json:"file_size,omitempty"`
	KeyHash    string      `json:"key_hash,omitempty"`
	PricePerKB uint64      `json:"price_per_kb,omitempty"`
	Children   []childResp `json:"children,omitempty"`
}

type childResp struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// validPnode returns a valid 66-hex-char compressed public key for testing.
func validPnode() string {
	return "02" + strings.Repeat("ab", 32)
}

func validPnodeBytes() []byte {
	b, _ := hex.DecodeString(validPnode())
	return b
}

// TestHandleMeta_FileNode verifies that handleMeta returns full node info for a file.
func TestHandleMeta_FileNode(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)

	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i)
	}

	meta.nodes["/hello.txt"] = &NodeInfo{
		PNode:      validPnodeBytes(),
		Type:       "file",
		MimeType:   "text/plain",
		FileSize:   100,
		KeyHash:    keyHash,
		Access:     "free",
		PricePerKB: 0,
	}

	req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/hello.txt", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp metaResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, validPnode(), resp.PNode)
	assert.Equal(t, "/hello.txt", resp.Path)
	assert.Equal(t, "file", resp.Type)
	assert.Equal(t, "free", resp.Access)
	assert.Equal(t, "text/plain", resp.MimeType)
	assert.Equal(t, uint64(100), resp.FileSize)
	assert.Equal(t, hex.EncodeToString(keyHash), resp.KeyHash)
	assert.Equal(t, uint64(0), resp.PricePerKB) // omitted when 0
	assert.Nil(t, resp.Children)                // no children for file
}

// TestHandleMeta_DirectoryNode verifies that handleMeta returns children for a directory.
func TestHandleMeta_DirectoryNode(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)

	meta.nodes["/docs"] = &NodeInfo{
		PNode:  validPnodeBytes(),
		Type:   "dir",
		Access: "free",
		Children: []ChildInfo{
			{Name: "foo.txt", Type: "file"},
			{Name: "bar", Type: "dir"},
		},
	}

	req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/docs", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp metaResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "dir", resp.Type)
	assert.Equal(t, "/docs", resp.Path)
	require.Len(t, resp.Children, 2)
	assert.Equal(t, "foo.txt", resp.Children[0].Name)
	assert.Equal(t, "file", resp.Children[0].Type)
	assert.Equal(t, "bar", resp.Children[1].Name)
	assert.Equal(t, "dir", resp.Children[1].Type)
}

// TestHandleMeta_UnknownPath verifies that handleMeta returns 404 for an unknown path.
func TestHandleMeta_UnknownPath(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/nonexistent", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NOT_FOUND")
}

// TestHandleMeta_InvalidPnode verifies that handleMeta returns 400 for an invalid pnode.
func TestHandleMeta_InvalidPnode(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	tests := []struct {
		name  string
		pnode string
	}{
		{"not hex", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
		{"too short", strings.Repeat("ab", 16)},
		{"too long", strings.Repeat("ab", 34)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/_bitfs/meta/"+tt.pnode+"/path", nil)
			w := httptest.NewRecorder()
			d.Handler().ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "INVALID_PNODE")
		})
	}
}

// TestHandleMeta_NilMetanetService verifies that handleMeta returns 503
// when the metanet service is nil.
func TestHandleMeta_NilMetanetService(t *testing.T) {
	wallet := newMockWallet(t)
	store := newMockStore()
	config := DefaultConfig()
	config.Security.RateLimit.RPM = 0
	d, err := New(config, wallet, store, nil) // nil metanet
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/somepath", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "SERVICE_UNAVAILABLE")
}

// TestHandleMeta_PathPrepended verifies that a "/" is prepended to the path if missing.
func TestHandleMeta_PathPrepended(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)

	meta.nodes["/some/deep/path"] = &NodeInfo{
		PNode:  validPnodeBytes(),
		Type:   "file",
		Access: "free",
	}

	// URL path value will be "some/deep/path" (no leading slash)
	req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/some/deep/path", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp metaResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "/some/deep/path", resp.Path)
}

// TestHandleMeta_PaidFile verifies that price_per_kb is included when > 0.
func TestHandleMeta_PaidFile(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)

	meta.nodes["/premium.dat"] = &NodeInfo{
		PNode:      validPnodeBytes(),
		Type:       "file",
		MimeType:   "application/octet-stream",
		FileSize:   2048,
		Access:     "paid",
		PricePerKB: 50,
	}

	req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/premium.dat", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp metaResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "paid", resp.Access)
	assert.Equal(t, uint64(50), resp.PricePerKB)
}

// TestHandleMeta_EmptyPath tests the edge case of an empty path segment.
func TestHandleMeta_EmptyPath(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)

	meta.nodes["/"] = &NodeInfo{
		PNode:  validPnodeBytes(),
		Type:   "dir",
		Access: "free",
		Children: []ChildInfo{
			{Name: "root.txt", Type: "file"},
		},
	}

	// The route pattern requires {path...}, so an empty path may still arrive as ""
	req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	// With the {path...} wildcard and trailing slash, the path value should be empty
	// which gets prepended to "/"
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleMeta_PnodeMismatch verifies that handleMeta returns 404 when the
// resolved node's PNode does not match the pnode in the URL.
func TestHandleMeta_PnodeMismatch(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)

	// Register a node with a different pnode than what we'll request.
	differentPnode := make([]byte, 33)
	differentPnode[0] = 0x03
	for i := 1; i < 33; i++ {
		differentPnode[i] = 0xcc
	}

	meta.nodes["/mismatch.txt"] = &NodeInfo{
		PNode:  differentPnode,
		Type:   "file",
		Access: "free",
	}

	// Request with validPnode(), but the node has a different pnode.
	req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/mismatch.txt", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NOT_FOUND")
}

// TestHandleMeta_InternalError verifies that handleMeta returns 500 for
// non-"not found" errors from the Metanet service.
func TestHandleMeta_InternalError(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)

	// Set a generic error that is not a "not found" error.
	meta.err = fmt.Errorf("database connection failed")

	req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/anypath", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "INTERNAL_ERROR")
}

// --- Input validation tests (Task 20: security audit) ---

// TestHandleMeta_PathTraversal verifies that ".." segments in paths are rejected.
// Note: we call handleMeta directly because Go's HTTP mux normalizes ".." in URLs
// before the handler sees them. In production, a reverse proxy or raw TCP client
// could send un-normalized paths, so the handler-level check is still valuable.
func TestHandleMeta_PathTraversal(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	tests := []struct {
		name string
		path string
	}{
		{"simple traversal", "../etc/passwd"},
		{"mid-path traversal", "docs/../../../etc/shadow"},
		{"double dot only", ".."},
		{"nested traversal", "a/b/../../c/../../../etc/hosts"},
		{"trailing traversal", "docs/.."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build request with SetPathValue to simulate router-extracted path values,
			// bypassing HTTP mux's URL normalization.
			req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/placeholder", nil)
			req.SetPathValue("pnode", validPnode())
			req.SetPathValue("path", tt.path)
			w := httptest.NewRecorder()
			d.handleMeta(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, "path %q should be rejected", tt.path)
			assert.Contains(t, w.Body.String(), "INVALID_PATH")
		})
	}
}

// TestHandleMeta_PathTraversal_Allowed verifies that legitimate paths with dots are NOT rejected.
func TestHandleMeta_PathTraversal_Allowed(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)

	// Register nodes for the valid paths (after "/" prepend).
	paths := []string{
		"file..name",
		"dir.with.dots/file.txt",
		".hidden",
		"...triple",
	}
	for _, p := range paths {
		meta.nodes["/"+p] = &NodeInfo{
			PNode:  validPnodeBytes(),
			Type:   "file",
			Access: "free",
		}
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/_bitfs/meta/"+validPnode()+"/placeholder", nil)
			req.SetPathValue("pnode", validPnode())
			req.SetPathValue("path", p)
			w := httptest.NewRecorder()
			d.handleMeta(w, req)

			// Should NOT be 400 — these are legitimate paths.
			assert.NotEqual(t, http.StatusBadRequest, w.Code, "path %q should be allowed", p)
		})
	}
}

// TestContainsPathTraversal is a unit test for the containsPathTraversal helper.
func TestContainsPathTraversal(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/normal/path", false},
		{"/.hidden/file", false},
		{"/file..name", false},
		{"/...triple", false},
		{"/..", true},
		{"/a/../b", true},
		{"/../etc/passwd", true},
		{"/a/b/../../c", true},
		{"..", true},
		{"a/../../b", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := containsPathTraversal(tt.path)
			assert.Equal(t, tt.expected, got, "containsPathTraversal(%q)", tt.path)
		})
	}
}

// TestHtmlEscape verifies the htmlEscape helper escapes dangerous characters.
func TestHtmlEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"<script>alert(1)</script>", "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{`"quoted"`, "&#34;quoted&#34;"},
		{"a&b", "a&amp;b"},
		{"safe/path.txt", "safe/path.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := htmlEscape(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

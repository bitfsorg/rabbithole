package daemon

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/template"
)

// randRead is a variable to allow test injection.
var randRead = func(b []byte) (int, error) {
	return io.ReadFull(rand.Reader, b)
}

// handleData handles GET /_bitfs/data/{hash} for encrypted data retrieval.
//
// Access control note: This endpoint intentionally serves encrypted ciphertext
// without authentication. The ciphertext is AES-256-GCM encrypted and cannot
// be decrypted without completing the Method 42 key exchange or HTLC purchase.
// This is analogous to how IPFS serves encrypted blocks by CID.
func (d *Daemon) handleData(w http.ResponseWriter, r *http.Request) {
	hashStr := r.PathValue("hash")
	if hashStr == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_HASH", "Hash parameter is required")
		return
	}

	keyHash, err := hex.DecodeString(hashStr)
	if err != nil || len(keyHash) != 32 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HASH", "Hash must be 64 hex characters (32 bytes)")
		return
	}

	// Check if content exists
	exists, err := d.store.Has(keyHash)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "STORAGE_ERROR", "Failed to check content")
		return
	}
	if !exists {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Content not found")
		return
	}

	// Get content size
	size, err := d.store.Size(keyHash)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "STORAGE_ERROR", "Failed to get content size")
		return
	}

	// Retrieve content
	data, err := d.store.Get(keyHash)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "STORAGE_ERROR", "Failed to retrieve content")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	w.Header().Set("X-Key-Hash", hashStr)
	_, _ = w.Write(data)
}

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

	// Prepend "/" to path if missing.
	if path == "" || path[0] != '/' {
		path = "/" + path
	}

	// Reject path traversal attempts.
	if containsPathTraversal(path) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PATH", "Path must not contain '..' segments")
		return
	}

	// Check that the Metanet service is available.
	if d.metanet == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Metanet service is not available")
		return
	}

	// Resolve the path via the Metanet service.
	node, err := d.metanet.GetNodeByPath(path)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Path not found")
		} else {
			writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to resolve path")
		}
		return
	}

	// Validate that the resolved node's PNode matches the URL pnode.
	if !bytes.Equal(node.PNode, pnodeBytes) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Path not found for this pnode")
		return
	}

	// Build the JSON response.
	resp := metaNodeResponse{
		PNode:    hex.EncodeToString(node.PNode),
		Path:     path,
		Type:     node.Type,
		Access:   node.Access,
		MimeType: node.MimeType,
		FileSize: node.FileSize,
	}
	if len(node.KeyHash) > 0 {
		resp.KeyHash = hex.EncodeToString(node.KeyHash)
	}
	if node.PricePerKB > 0 {
		resp.PricePerKB = node.PricePerKB
	}
	if node.Timestamp > 0 {
		resp.Timestamp = int64(node.Timestamp)
	}
	if node.Type == "dir" && len(node.Children) > 0 {
		resp.Children = make([]metaChildResponse, len(node.Children))
		for i, c := range node.Children {
			resp.Children[i] = metaChildResponse(c)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// metaNodeResponse is the JSON response structure for handleMeta.
type metaNodeResponse struct {
	PNode      string              `json:"pnode"`
	Path       string              `json:"path"`
	Type       string              `json:"type"`
	Access     string              `json:"access"`
	MimeType   string              `json:"mime_type,omitempty"`
	FileSize   uint64              `json:"file_size,omitempty"`
	KeyHash    string              `json:"key_hash,omitempty"`
	PricePerKB uint64              `json:"price_per_kb,omitempty"`
	Timestamp  int64               `json:"timestamp,omitempty"`
	Children   []metaChildResponse `json:"children,omitempty"`
}

// metaChildResponse is a child entry in the metaNodeResponse.
type metaChildResponse struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// versionEntryResponse is the JSON response structure for a single version entry.
type versionEntryResponse struct {
	Version     int    `json:"version"`
	TxID        string `json:"txid"`
	BlockHeight uint32 `json:"block_height"`
	Timestamp   int64  `json:"timestamp"`
	FileSize    uint64 `json:"file_size"`
	Access      string `json:"access"`
}

// handleVersions handles GET /_bitfs/versions/{pnode}/{path...} for version history.
// Currently returns a single-entry array with the current node's data,
// since full version history tracking is not yet implemented.
func (d *Daemon) handleVersions(w http.ResponseWriter, r *http.Request) {
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

	// Prepend "/" to path if missing.
	if path == "" || path[0] != '/' {
		path = "/" + path
	}

	// Reject path traversal attempts.
	if containsPathTraversal(path) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PATH", "Path must not contain '..' segments")
		return
	}

	// Check that the Metanet service is available.
	if d.metanet == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Metanet service is not available")
		return
	}

	// Resolve the path via the Metanet service.
	node, err := d.metanet.GetNodeByPath(path)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Path not found")
		} else {
			writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to resolve path")
		}
		return
	}

	// Validate that the resolved node's PNode matches the URL pnode.
	if !bytes.Equal(node.PNode, pnodeBytes) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Path not found for this pnode")
		return
	}

	// Build a single-entry version history from the current node state.
	// Full version history will be populated once version tracking is implemented.
	entry := versionEntryResponse{
		Version:  1,
		FileSize: node.FileSize,
		Access:   node.Access,
	}
	if node.Timestamp > 0 {
		entry.Timestamp = int64(node.Timestamp)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode([]versionEntryResponse{entry})
}

// containsPathTraversal returns true if the path contains ".." segments
// that could allow directory traversal attacks. It iteratively URL-decodes
// (up to 3 rounds) to catch percent-encoded and double-encoded sequences.
func containsPathTraversal(path string) bool {
	for i := 0; i < 3; i++ {
		for _, segment := range strings.Split(path, "/") {
			if segment == ".." {
				return true
			}
		}
		decoded, err := url.PathUnescape(path)
		if err != nil || decoded == path {
			break
		}
		path = decoded
	}
	return false
}

// htmlEscape escapes a string for safe inclusion in HTML output.
func htmlEscape(s string) string {
	return template.HTMLEscapeString(s)
}

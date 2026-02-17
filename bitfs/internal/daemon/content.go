package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
)

// randRead is a variable to allow test injection.
var randRead = func(b []byte) (int, error) {
	return io.ReadFull(rand.Reader, b)
}

// handleData handles GET /_bitfs/data/{hash} for encrypted data retrieval.
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
	w.Write(data)
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

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"pnode":%q,"path":%q,"status":"ok"}`, pnode, path)
}

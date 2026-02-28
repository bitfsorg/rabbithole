package daemon

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardStatus(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/_bitfs/dashboard/status", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, dashboardVersion, resp["version"])
	assert.Contains(t, resp, "uptime_seconds")
	assert.Contains(t, resp, "listen_addr")
	assert.Contains(t, resp, "started_at")
	assert.Contains(t, resp, "mainnet")

	// Verify vault_pnode is present and matches the wallet's public key.
	expectedPnode := hex.EncodeToString(wallet.pubKey.Compressed())
	assert.Equal(t, expectedPnode, resp["vault_pnode"])
}

func TestDashboardStorage(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Create a temp directory with 2 files (5 + 6 = 11 bytes).
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.dat"), []byte("hello"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.dat"), []byte("world!"), 0600))

	d.StorageDir = tmpDir

	req := httptest.NewRequest("GET", "/_bitfs/dashboard/storage", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, float64(2), resp["file_count"])
	assert.Equal(t, float64(11), resp["total_size_bytes"])
	assert.Equal(t, tmpDir, resp["storage_path"])
}

func TestDashboardWallet(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/_bitfs/dashboard/wallet", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Contains(t, resp, "available")
	assert.Equal(t, true, resp["available"])
	assert.Contains(t, resp, "pubkey")
}

func TestDashboardNetwork(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/_bitfs/dashboard/network", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Contains(t, resp, "mainnet")
	assert.Contains(t, resp, "spv_enabled")
}

func TestDashboardLogs(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	d.logBuf.Add("info", "msg1")
	d.logBuf.Add("info", "msg2")
	d.logBuf.Add("info", "msg3")

	req := httptest.NewRequest("GET", "/_bitfs/dashboard/logs?limit=2", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Entries []LogEntry `json:"entries"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	require.Len(t, resp.Entries, 2)
	assert.Equal(t, "msg2", resp.Entries[0].Message)
	assert.Equal(t, "msg3", resp.Entries[1].Message)
}

func TestDashboardLogs_FilterLevel(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	d.logBuf.Add("info", "info message")
	d.logBuf.Add("error", "error message")

	req := httptest.NewRequest("GET", "/_bitfs/dashboard/logs?level=error", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Entries []LogEntry `json:"entries"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	require.Len(t, resp.Entries, 1)
	assert.Equal(t, "error", resp.Entries[0].Level)
	assert.Equal(t, "error message", resp.Entries[0].Message)
}

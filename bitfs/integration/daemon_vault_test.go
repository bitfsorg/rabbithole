//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// createDaemonWithEngine creates a real Engine wired into a Daemon via adapters
// and returns the engine plus an httptest.Server ready for HTTP assertions.
// The server and engine are cleaned up automatically when the test finishes.
func createDaemonWithEngine(t *testing.T) (*vault.Vault, *httptest.Server) {
	t.Helper()

	eng := initIntegrationEngine(t)

	// Create root directory so that Metanet path resolution works.
	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err, "Mkdir /")

	// Wire vault adapters.
	cfg := daemon.DefaultConfig()
	cfg.Security.RateLimit.RPM = 0
	cfg.Security.RateLimit.Burst = 0

	d, err := daemon.New(cfg, &testWalletAdapter{v: eng}, &testStoreAdapter{v: eng}, &testMetanetAdapter{v: eng})
	require.NoError(t, err, "daemon.New")

	ts := httptest.NewServer(d.Handler())
	t.Cleanup(ts.Close)

	return eng, ts
}

// --- Test 1: TestDaemonSeesEngineMkdir ---

// TestDaemonSeesEngineMkdir creates a directory via Engine.Mkdir and verifies
// that the daemon HTTP API returns it as JSON with "dir" type.
func TestDaemonSeesEngineMkdir(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	// Create /docs directory.
	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/docs"})
	require.NoError(t, err, "Mkdir /docs")

	// Query the daemon for /docs via content negotiation (default=JSON).
	resp, err := http.Get(ts.URL + "/docs")
	require.NoError(t, err, "GET /docs")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "status should be 200")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read body")

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "unmarshal JSON body: %s", string(body))

	assert.Equal(t, "dir", result["type"], "type should be 'dir'")
}

// --- Test 2: TestDaemonReflectsRemove ---

// TestDaemonReflectsRemove verifies the current behavior after Engine.Remove:
// Remove doesn't purge node from local state, so the daemon still finds it.
// This test documents this design choice.
func TestDaemonReflectsRemove(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	// Create a file to remove.
	plaintext := []byte("content to be removed")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/removeme.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile /removeme.txt")

	// Verify the file is accessible via daemon.
	resp, err := http.Get(ts.URL + "/removeme.txt")
	require.NoError(t, err, "GET /removeme.txt before remove")
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "file should be accessible before remove")

	// Remove the file via engine.
	_, err = eng.Remove(&vault.RemoveOpts{VaultIndex: 0, Path: "/removeme.txt"})
	require.NoError(t, err, "Remove /removeme.txt")

	// Document current behavior: Remove does NOT purge state, so daemon still finds the node.
	resp, err = http.Get(ts.URL + "/removeme.txt")
	require.NoError(t, err, "GET /removeme.txt after remove")
	defer resp.Body.Close()

	// Current behavior: node still exists in state, daemon returns 200.
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"after Remove, daemon still finds node (Remove doesn't purge local state)")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read body")

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "unmarshal JSON: %s", string(body))

	assert.Equal(t, "file", result["type"],
		"removed node still reports as file (deletion only recorded on-chain)")
}

// --- Test 3: TestDaemonContentNegotiationWithEngine ---

// TestDaemonContentNegotiationWithEngine is a table-driven test that verifies
// the daemon responds with correct Content-Type based on Accept header,
// using real engine state.
func TestDaemonContentNegotiationWithEngine(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	// Create a subdirectory so we have something to negotiate over.
	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/mydir"})
	require.NoError(t, err, "Mkdir /mydir")

	tests := []struct {
		name           string
		accept         string
		wantStatus     int
		wantCTContains string // substring of Content-Type header
		wantContains   string // substring expected in body
	}{
		{
			name:           "JSON",
			accept:         "application/json",
			wantStatus:     http.StatusOK,
			wantCTContains: "application/json",
			wantContains:   `"type":"dir"`,
		},
		{
			name:           "HTML",
			accept:         "text/html",
			wantStatus:     http.StatusOK,
			wantCTContains: "text/html",
			wantContains:   "Directory",
		},
		{
			name:           "Markdown",
			accept:         "text/markdown",
			wantStatus:     http.StatusOK,
			wantCTContains: "text/markdown",
			wantContains:   "# Directory Listing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", ts.URL+"/mydir", nil)
			require.NoError(t, err)
			req.Header.Set("Accept", tc.accept)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.wantStatus, resp.StatusCode, "status code")

			ct := resp.Header.Get("Content-Type")
			assert.Contains(t, ct, tc.wantCTContains,
				"Content-Type should contain %q, got %q", tc.wantCTContains, ct)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(body), tc.wantContains,
				"body should contain %q", tc.wantContains)
		})
	}
}

// --- Test 4: TestDaemonPriceInResponse ---

// TestDaemonPriceInResponse puts a free file, sells it via Engine.Sell,
// and verifies the daemon JSON response includes the correct PricePerKB.
func TestDaemonPriceInResponse(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	// Seed extra UTXOs for Sell transaction.
	seedFeeUTXOs(t, eng, 30, 10_000)

	// Create a free file.
	plaintext := []byte("premium content for daemon price test")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/premium.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile /premium.txt")

	// Sell the file at 1000 sats/KB.
	_, err = eng.Sell(&vault.SellOpts{
		VaultIndex: 0,
		Path:       "/premium.txt",
		PricePerKB: 1000,
	})
	require.NoError(t, err, "Sell /premium.txt")

	// Query daemon for the file. The daemon JSON serializes NodeInfo directly.
	resp, err := http.Get(ts.URL + "/premium.txt")
	require.NoError(t, err, "GET /premium.txt")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "status should be 200")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read body")

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "unmarshal JSON: %s", string(body))

	assert.Equal(t, "paid", result["access"],
		"Access should be 'paid' after Sell")

	// price_per_kb is uint64 in Go, JSON encodes it as a number.
	priceVal, ok := result["price_per_kb"]
	require.True(t, ok, "price_per_kb should be present in JSON response")
	assert.Equal(t, float64(1000), priceVal,
		"price_per_kb should be 1000 (JSON numbers decode as float64)")
}

// --- Test 5: TestDaemonHealthWithEngine ---

// TestDaemonHealthWithEngine verifies the health endpoint returns 200
// with {"status":"ok"} when wired with a real engine.
func TestDaemonHealthWithEngine(t *testing.T) {
	_, ts := createDaemonWithEngine(t)

	resp, err := http.Get(ts.URL + "/_bitfs/health")
	require.NoError(t, err, "GET /_bitfs/health")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "health should return 200")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read body")

	var result map[string]string
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "unmarshal health JSON: %s", string(body))

	assert.Equal(t, "ok", result["status"],
		"health status should be 'ok'")
}

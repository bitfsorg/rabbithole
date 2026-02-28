//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// --- Mock SPV service for E2E testing ---

// testSPVService implements daemon.SPVService with a map of pre-configured results.
type testSPVService struct {
	results map[string]*daemon.SPVResult
}

func (s *testSPVService) VerifyTx(_ context.Context, txid string) (*daemon.SPVResult, error) {
	r, ok := s.results[txid]
	if !ok {
		return nil, fmt.Errorf("tx not found: %s", txid)
	}
	return r, nil
}

// setupSPVServer creates a daemon httptest.Server with an optional SPVService.
// Pass nil for spvSvc to test the "SPV not configured" path.
func setupSPVServer(t *testing.T, spvSvc daemon.SPVService) *httptest.Server {
	t.Helper()

	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	walletSvc := &testWalletService{w: w}
	metanetSvc := &testMetanetService{nodes: map[string]*daemon.NodeInfo{}}

	fileStore, err := storage.NewFileStore(t.TempDir())
	require.NoError(t, err, "create file store")

	config := daemon.DefaultConfig()
	config.Security.RateLimit.RPM = 0 // disable rate limiting for tests

	d, err := daemon.New(config, walletSvc, fileStore, metanetSvc)
	require.NoError(t, err, "create daemon")

	if spvSvc != nil {
		d.SetSPV(spvSvc)
	}

	server := httptest.NewServer(d.Handler())
	t.Cleanup(server.Close)

	return server
}

// TestSPVProofEndpointSuccess verifies that the SPV proof endpoint returns
// a confirmed proof when the mock SPVService has a matching txid:
//
//   - GET /_bitfs/spv/proof/{txid} returns 200
//   - Response JSON contains confirmed: true
//   - Response JSON contains block_hash and block_height
//   - Response JSON echoes back the txid
func TestSPVProofEndpointSuccess(t *testing.T) {
	const testTxID = "aabbccdd11223344556677889900aabbccdd11223344556677889900aabbccdd"
	const testBlockHash = "000000000000000003fa5ec0e8f3e6e7d7f1c8b4a2e0d1c3b5a7f9e1d3c5b7a9"
	const testBlockHeight = uint64(800123)

	spvSvc := &testSPVService{
		results: map[string]*daemon.SPVResult{
			testTxID: {
				Confirmed:   true,
				BlockHash:   testBlockHash,
				BlockHeight: testBlockHeight,
			},
		},
	}

	server := setupSPVServer(t, spvSvc)

	resp, err := http.Get(server.URL + "/_bitfs/spv/proof/" + testTxID)
	require.NoError(t, err, "GET /_bitfs/spv/proof/{txid}")
	defer resp.Body.Close()

	// Verify 200 OK.
	require.Equal(t, http.StatusOK, resp.StatusCode, "should return 200 for confirmed proof")
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json",
		"Content-Type should be JSON")

	// Parse the JSON response.
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read response body")

	var proof struct {
		TxID        string `json:"txid"`
		Confirmed   bool   `json:"confirmed"`
		BlockHash   string `json:"block_hash"`
		BlockHeight uint64 `json:"block_height"`
	}
	err = json.Unmarshal(body, &proof)
	require.NoError(t, err, "unmarshal SPV proof response")

	// Verify all fields.
	assert.Equal(t, testTxID, proof.TxID, "txid should be echoed back")
	assert.True(t, proof.Confirmed, "confirmed should be true")
	assert.Equal(t, testBlockHash, proof.BlockHash, "block_hash should match")
	assert.Equal(t, testBlockHeight, proof.BlockHeight, "block_height should match")

	t.Logf("SPV proof success: txid=%s, confirmed=%t, block_hash=%s, block_height=%d",
		proof.TxID, proof.Confirmed, proof.BlockHash, proof.BlockHeight)
}

// TestSPVProofEndpointUnconfirmed verifies that an unconfirmed transaction
// returns confirmed: false with no block_hash or block_height fields:
//
//   - GET /_bitfs/spv/proof/{txid} returns 200
//   - Response JSON contains confirmed: false
//   - block_hash and block_height are empty/zero (omitted)
func TestSPVProofEndpointUnconfirmed(t *testing.T) {
	const testTxID = "1111111122222222333333334444444455555555666666667777777788888888"

	spvSvc := &testSPVService{
		results: map[string]*daemon.SPVResult{
			testTxID: {
				Confirmed: false,
			},
		},
	}

	server := setupSPVServer(t, spvSvc)

	resp, err := http.Get(server.URL + "/_bitfs/spv/proof/" + testTxID)
	require.NoError(t, err, "GET /_bitfs/spv/proof/{txid}")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "should return 200 for unconfirmed tx")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read response body")

	var proof map[string]interface{}
	err = json.Unmarshal(body, &proof)
	require.NoError(t, err, "unmarshal SPV proof response")

	// Verify confirmed is false.
	confirmed, ok := proof["confirmed"].(bool)
	require.True(t, ok, "confirmed should be a boolean")
	assert.False(t, confirmed, "confirmed should be false for unconfirmed tx")

	// block_hash and block_height should be absent (omitempty in JSON).
	_, hasBlockHash := proof["block_hash"]
	assert.False(t, hasBlockHash, "block_hash should be omitted for unconfirmed tx")

	_, hasBlockHeight := proof["block_height"]
	assert.False(t, hasBlockHeight, "block_height should be omitted for unconfirmed tx")

	t.Logf("SPV proof unconfirmed: txid=%s, confirmed=%t", testTxID, confirmed)
}

// TestSPVProofEndpointNotFound verifies that when the mock SPVService returns
// an error (tx not found), the endpoint returns 502 Bad Gateway:
//
//   - GET /_bitfs/spv/proof/{unknown-txid} returns 502
//   - Response contains SPV_ERROR error code
//   - Response message indicates the error from SPVService
func TestSPVProofEndpointNotFound(t *testing.T) {
	const unknownTxID = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	// SPV service with no matching txids -> VerifyTx returns error.
	spvSvc := &testSPVService{
		results: map[string]*daemon.SPVResult{},
	}

	server := setupSPVServer(t, spvSvc)

	resp, err := http.Get(server.URL + "/_bitfs/spv/proof/" + unknownTxID)
	require.NoError(t, err, "GET /_bitfs/spv/proof/{unknown-txid}")
	defer resp.Body.Close()

	// The handler returns 502 Bad Gateway when SPVService.VerifyTx returns an error.
	require.Equal(t, http.StatusBadGateway, resp.StatusCode,
		"should return 502 when SPV service returns error")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read response body")
	bodyStr := string(body)

	assert.Contains(t, bodyStr, "SPV_ERROR",
		"error response should contain SPV_ERROR code")
	assert.Contains(t, bodyStr, "tx not found",
		"error message should indicate tx not found")

	t.Logf("SPV proof not found: HTTP %d, body=%s", resp.StatusCode, bodyStr)
}

// TestSPVProofEndpointNoSPV verifies that when no SPV service is configured
// (nil), the endpoint returns 503 Service Unavailable:
//
//   - GET /_bitfs/spv/proof/{txid} returns 503
//   - Response contains SPV_UNAVAILABLE error code
//   - Response message indicates SPV is not configured (offline mode)
func TestSPVProofEndpointNoSPV(t *testing.T) {
	const testTxID = "cafebabecafebabecafebabecafebabecafebabecafebabecafebabecafebabe"

	// Pass nil SPV service -- daemon should not have SetSPV called.
	server := setupSPVServer(t, nil)

	resp, err := http.Get(server.URL + "/_bitfs/spv/proof/" + testTxID)
	require.NoError(t, err, "GET /_bitfs/spv/proof/{txid} (no SPV)")
	defer resp.Body.Close()

	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
		"should return 503 when SPV service is nil")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read response body")
	bodyStr := string(body)

	assert.Contains(t, bodyStr, "SPV_UNAVAILABLE",
		"error response should contain SPV_UNAVAILABLE code")
	assert.Contains(t, bodyStr, "offline mode",
		"error message should mention offline mode")

	t.Logf("SPV not configured: HTTP %d, body=%s", resp.StatusCode, bodyStr)
}

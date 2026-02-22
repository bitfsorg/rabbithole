package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSPV implements SPVService for testing.
type mockSPV struct {
	result *SPVResult
	err    error
}

func (m *mockSPV) VerifyTx(_ context.Context, _ string) (*SPVResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func testDaemonWithSPV(t *testing.T, spv SPVService) *Daemon {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Security.RateLimit.RPM = 0 // disable rate limiting for tests
	w := newMockWallet(t)
	s := &mockStore{data: make(map[string][]byte)}

	d, err := New(cfg, w, s, nil)
	require.NoError(t, err)
	d.SetSPV(spv)
	return d
}

func TestHandleSPVProof_Confirmed(t *testing.T) {
	spv := &mockSPV{
		result: &SPVResult{
			Confirmed:   true,
			BlockHash:   "000000abc123",
			BlockHeight: 12345,
		},
	}
	d := testDaemonWithSPV(t, spv)

	req := httptest.NewRequest("GET", "/_bitfs/spv/proof/deadbeef1234", nil)
	w := httptest.NewRecorder()
	d.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp spvProofResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "deadbeef1234", resp.TxID)
	assert.True(t, resp.Confirmed)
	assert.Equal(t, "000000abc123", resp.BlockHash)
	assert.Equal(t, uint64(12345), resp.BlockHeight)
}

func TestHandleSPVProof_Unconfirmed(t *testing.T) {
	spv := &mockSPV{
		result: &SPVResult{Confirmed: false},
	}
	d := testDaemonWithSPV(t, spv)

	req := httptest.NewRequest("GET", "/_bitfs/spv/proof/sometxid", nil)
	w := httptest.NewRecorder()
	d.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp spvProofResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.False(t, resp.Confirmed)
	assert.Empty(t, resp.BlockHash)
}

func TestHandleSPVProof_NoSPVService(t *testing.T) {
	d := testDaemonWithSPV(t, nil) // no SPV service

	req := httptest.NewRequest("GET", "/_bitfs/spv/proof/sometxid", nil)
	w := httptest.NewRecorder()
	d.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "SPV_UNAVAILABLE")
}

func TestHandleSPVProof_Error(t *testing.T) {
	spv := &mockSPV{
		err: fmt.Errorf("network: connection refused"),
	}
	d := testDaemonWithSPV(t, spv)

	req := httptest.NewRequest("GET", "/_bitfs/spv/proof/sometxid", nil)
	w := httptest.NewRecorder()
	d.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "SPV_ERROR")
}

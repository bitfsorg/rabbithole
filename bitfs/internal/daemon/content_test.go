package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainsPathTraversal_URLEncoded(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		expect bool
	}{
		{"literal dot-dot", "/foo/../bar", true},
		{"leading dot-dot", "/../etc/passwd", true},
		{"percent-encoded dot-dot lowercase", "/foo/%2e%2e/bar", true},
		{"percent-encoded dot-dot uppercase", "/foo/%2E%2E/bar", true},
		{"percent-encoded slash", "/foo%2F..%2Fbar", true},
		{"double percent-encoded", "/foo/%252e%252e/bar", true},
		{"clean path", "/foo/bar/baz", false},
		{"single dot", "/foo/./bar", false},
		{"dot in name", "/foo/bar..baz", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, containsPathTraversal(tt.path))
		})
	}
}

func TestHandleBSVAlias_IgnoresMaliciousHost(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	// Default config has ListenAddr=":8080", TLS.Enabled=false

	req := httptest.NewRequest(http.MethodGet, "/.well-known/bsvalias", nil)
	req.Host = "evil.example.com"
	w := httptest.NewRecorder()

	d.handleBSVAlias(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	caps, ok := resp["capabilities"].(map[string]interface{})
	require.True(t, ok, "capabilities should be a JSON object")

	pki, ok := caps["pki"].(string)
	require.True(t, ok)

	// Must NOT contain the malicious host header value.
	assert.NotContains(t, pki, "evil.example.com")
	// Must use the configured address.
	assert.Contains(t, pki, "localhost:8080")
}

func TestHandleBSVAlias_CustomListenAddr(t *testing.T) {
	config := DefaultConfig()
	config.ListenAddr = "192.168.1.10:9090"
	config.Security.RateLimit.RPM = 0
	w := newMockWallet(t)
	s := newMockStore()
	d, err := New(config, w, s, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/bsvalias", nil)
	req.Host = "attacker.com"
	rec := httptest.NewRecorder()

	d.handleBSVAlias(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	caps := resp["capabilities"].(map[string]interface{})
	pki := caps["pki"].(string)
	assert.Contains(t, pki, "192.168.1.10:9090")
	assert.NotContains(t, pki, "attacker.com")
}

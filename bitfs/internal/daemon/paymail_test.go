package daemon

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/libbitfs-go/paymail"
)

// --- handlePKI Tests ---

func TestHandlePKI_Success(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)

	// Register a vault alias with a known pubkey.
	expectedPubKey := hex.EncodeToString(wallet.pubKey.Compressed())
	wallet.vaultKeys["alice"] = expectedPubKey

	req := httptest.NewRequest("GET", "/api/v1/pki/alice@bitfs.org", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp pkiResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "1.0", resp.BSVAlias)
	assert.Equal(t, "alice@bitfs.org", resp.Handle)
	assert.Equal(t, expectedPubKey, resp.PubKey)
}

func TestHandlePKI_UnknownAlias(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/pki/unknown@bitfs.org", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NOT_FOUND")
	assert.Contains(t, w.Body.String(), "unknown")
}

func TestHandlePKI_MalformedHandle_NoAt(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/pki/nodomain", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "INVALID_HANDLE")
}

func TestHandlePKI_MalformedHandle_EmptyAlias(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/pki/@bitfs.org", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "INVALID_HANDLE")
}

func TestHandlePKI_MalformedHandle_EmptyDomain(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/pki/alice@", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "INVALID_HANDLE")
}

func TestHandlePKI_MultipleAliases(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)

	pub1 := hex.EncodeToString(wallet.pubKey.Compressed())
	wallet.vaultKeys["alice"] = pub1
	wallet.vaultKeys["bob"] = "03" + strings.Repeat("cd", 32)

	// Request alice
	req1 := httptest.NewRequest("GET", "/api/v1/pki/alice@example.com", nil)
	w1 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w1, req1)

	assert.Equal(t, http.StatusOK, w1.Code)
	var resp1 pkiResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	assert.Equal(t, pub1, resp1.PubKey)

	// Request bob
	req2 := httptest.NewRequest("GET", "/api/v1/pki/bob@example.com", nil)
	w2 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	var resp2 pkiResponse
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	assert.Equal(t, "03"+strings.Repeat("cd", 32), resp2.PubKey)
}

func TestHandlePKI_DomainIgnored(t *testing.T) {
	// The domain part of the handle is included in the response but does not
	// affect vault lookup; only the alias (before @) is used.
	d, wallet, _, _ := newTestDaemon(t)

	expectedPubKey := hex.EncodeToString(wallet.pubKey.Compressed())
	wallet.vaultKeys["alice"] = expectedPubKey

	// Different domains, same alias
	req1 := httptest.NewRequest("GET", "/api/v1/pki/alice@bitfs.org", nil)
	w1 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w1, req1)

	req2 := httptest.NewRequest("GET", "/api/v1/pki/alice@other.com", nil)
	w2 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, http.StatusOK, w2.Code)

	var resp1, resp2 pkiResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	assert.Equal(t, expectedPubKey, resp1.PubKey)
	assert.Equal(t, expectedPubKey, resp2.PubKey)
	assert.Equal(t, "alice@bitfs.org", resp1.Handle)
	assert.Equal(t, "alice@other.com", resp2.Handle)
}

// --- BSVAlias Capabilities Include PKI URL Tests ---

func TestBSVAliasEndpoint_PKIURLTemplate(t *testing.T) {
	config := DefaultConfig()
	config.ListenAddr = "bitfs.example.com:8080"
	config.Security.RateLimit.RPM = 0
	d, err := New(config, newMockWallet(t), newMockStore(), nil)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/.well-known/bsvalias", nil)
	req.Host = "should-be-ignored.com"
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	caps, ok := resp["capabilities"].(map[string]interface{})
	require.True(t, ok)

	pki, ok := caps["pki"].(string)
	require.True(t, ok)

	// The PKI URL template should contain the configured addr and the path template.
	assert.Contains(t, pki, "bitfs.example.com:8080")
	assert.Contains(t, pki, "/api/v1/pki/")
	assert.Contains(t, pki, "{alias}")
	assert.Contains(t, pki, "{domain.tld}")
}

// --- Integration: PKI via httptest.Server ---

func TestHTTPTestServer_PKI(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)
	expectedPubKey := hex.EncodeToString(wallet.pubKey.Compressed())
	wallet.vaultKeys["testuser"] = expectedPubKey

	server := httptest.NewServer(d.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/pki/testuser@bitfs.org")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var pkiResp pkiResponse
	err = json.NewDecoder(resp.Body).Decode(&pkiResp)
	require.NoError(t, err)

	assert.Equal(t, "1.0", pkiResp.BSVAlias)
	assert.Equal(t, "testuser@bitfs.org", pkiResp.Handle)
	assert.Equal(t, expectedPubKey, pkiResp.PubKey)
}

func TestHTTPTestServer_PKI_NotFound(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	server := httptest.NewServer(d.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/pki/nonexistent@bitfs.org")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- handlePublicProfile Tests ---

func TestHandlePublicProfile_Success(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)

	expectedPubKey := hex.EncodeToString(wallet.pubKey.Compressed())
	wallet.vaultKeys["alice"] = expectedPubKey

	req := httptest.NewRequest("GET", "/api/v1/public-profile/alice@bitfs.org", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp publicProfileResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "alice", resp.Name)
	assert.Equal(t, "bitfs.org", resp.Domain)
	assert.Equal(t, "", resp.Avatar)
}

func TestHandlePublicProfile_UnknownAlias(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/public-profile/unknown@bitfs.org", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NOT_FOUND")
}

func TestHandlePublicProfile_MalformedHandle(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/public-profile/nodomain", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "INVALID_HANDLE")
}

// --- handleVerifyPubKey Tests ---

func TestHandleVerifyPubKey_Match(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)

	expectedPubKey := hex.EncodeToString(wallet.pubKey.Compressed())
	wallet.vaultKeys["alice"] = expectedPubKey

	req := httptest.NewRequest("GET", "/api/v1/verify/alice@bitfs.org/"+expectedPubKey, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp verifyPubKeyResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "alice@bitfs.org", resp.Handle)
	assert.Equal(t, expectedPubKey, resp.PubKey)
	assert.True(t, resp.Match)
}

func TestHandleVerifyPubKey_NoMatch(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)

	expectedPubKey := hex.EncodeToString(wallet.pubKey.Compressed())
	wallet.vaultKeys["alice"] = expectedPubKey

	wrongPubKey := "03" + strings.Repeat("ab", 32)

	req := httptest.NewRequest("GET", "/api/v1/verify/alice@bitfs.org/"+wrongPubKey, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp verifyPubKeyResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "alice@bitfs.org", resp.Handle)
	assert.Equal(t, wrongPubKey, resp.PubKey)
	assert.False(t, resp.Match)
}

func TestHandleVerifyPubKey_UnknownAlias(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	somePubKey := "03" + strings.Repeat("ff", 32)

	req := httptest.NewRequest("GET", "/api/v1/verify/unknown@bitfs.org/"+somePubKey, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	// Unknown alias returns 200 with match=false (don't reveal alias existence).
	assert.Equal(t, http.StatusOK, w.Code)

	var resp verifyPubKeyResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "unknown@bitfs.org", resp.Handle)
	assert.Equal(t, somePubKey, resp.PubKey)
	assert.False(t, resp.Match)
}

func TestHandleVerifyPubKey_MalformedHandle(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/api/v1/verify/nodomain/somepubkey", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "INVALID_HANDLE")
}

// --- BSVAlias Capability Advertising Tests ---

func TestBSVAlias_HasVerifyPubKeyCapability(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/.well-known/bsvalias", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	caps, ok := resp["capabilities"].(map[string]interface{})
	require.True(t, ok, "capabilities must be a map")

	verifyURL, ok := caps["a9f510c16bde"].(string)
	require.True(t, ok, "a9f510c16bde (VerifyPubKey) capability must be present")

	assert.Contains(t, verifyURL, "/api/v1/verify/")
	assert.Contains(t, verifyURL, "{pubkey}")
}

func TestBSVAlias_HasBRFCCapabilities(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/.well-known/bsvalias", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	caps, ok := resp["capabilities"].(map[string]interface{})
	require.True(t, ok, "capabilities must be a map")

	brfcTests := []struct {
		brfcID   string
		name     string
		contains string
	}{
		{paymail.BRFCBitFSBrowse, "BRFCBitFSBrowse", "/_bitfs/meta/"},
		{paymail.BRFCBitFSBuy, "BRFCBitFSBuy", "/_bitfs/buy/"},
		{paymail.BRFCBitFSSell, "BRFCBitFSSell", "/_bitfs/sales"},
	}

	for _, tc := range brfcTests {
		t.Run(tc.name, func(t *testing.T) {
			url, ok := caps[tc.brfcID].(string)
			require.True(t, ok, "BRFC %s (%s) must be present in capabilities", tc.name, tc.brfcID)
			assert.Contains(t, url, tc.contains, "BRFC %s URL must point to %s", tc.name, tc.contains)
		})
	}
}

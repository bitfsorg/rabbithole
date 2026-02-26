package daemon

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Handshake Error Paths ---

// TestHandshake_NonceGenerationFailure injects an error into cryptoRandRead
// so that seller nonce generation fails and the handler returns 500.
func TestHandshake_NonceGenerationFailure(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Save original and restore after test
	orig := cryptoRandRead
	t.Cleanup(func() { cryptoRandRead = orig })
	cryptoRandRead = func(b []byte) (int, error) {
		return 0, fmt.Errorf("simulated entropy failure")
	}

	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(buyerPriv.PubKey().Compressed()),
		NonceB:    hex.EncodeToString(make([]byte, 32)),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "NONCE_ERROR")
}

// TestHandshake_InvalidCurvePoint sends a 33-byte key that starts with a valid
// prefix (0x02) but whose x-coordinate does not lie on secp256k1, so
// ec.PublicKeyFromBytes will reject it.
func TestHandshake_InvalidCurvePoint(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// 33 bytes: valid compressed prefix 0x02 + x=5.
	// x^3 + 7 = 132 is a quadratic non-residue mod the secp256k1 prime,
	// so decompressPoint returns "invalid square root".
	invalidKey := make([]byte, 33)
	invalidKey[0] = 0x02
	invalidKey[32] = 0x05

	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(invalidKey),
		NonceB:    hex.EncodeToString(make([]byte, 32)),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "INVALID_PUBKEY")
}

// TestHandshake_EmptyNonceValue sends an empty string for the nonce_b field.
// The handler should reject it because nonce_b is required to be non-empty.
func TestHandshake_EmptyNonceValue(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(buyerPriv.PubKey().Compressed()),
		NonceB:    "", // empty nonce
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "MISSING_FIELD")
}

// --- Data Endpoint ---

// TestDataEndpoint_EmptyHash verifies that requesting /_bitfs/data/ (with a
// trailing slash but no hash segment) does not panic or expose content.
// The Go 1.22+ ServeMux pattern "GET /_bitfs/data/{hash}" requires a non-empty
// path segment, so the request falls through to the root catch-all handler.
func TestDataEndpoint_EmptyHash(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/_bitfs/data/", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	// Should not match the data handler; falls to the root handler.
	// Either a 200 from the root handler or a 404 is acceptable;
	// it must NOT be a 500 or expose data.
	assert.NotEqual(t, http.StatusInternalServerError, w.Code)
	// Confirm it did NOT return content as octet-stream (data handler's type).
	assert.NotEqual(t, "application/octet-stream", w.Header().Get("Content-Type"))
}

// --- Meta Endpoint ---

// TestMetaEndpoint_ShortPnode sends valid hex that decodes to 32 bytes (not the
// required 33 bytes for a compressed public key).
func TestMetaEndpoint_ShortPnode(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// 64 hex chars = 32 bytes, but pnode must be 33 bytes (66 hex chars)
	shortPnode := strings.Repeat("ab", 32)
	req := httptest.NewRequest("GET", "/_bitfs/meta/"+shortPnode+"/somepath", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "INVALID_PNODE")
}

// --- BSVAlias ---

// TestBSVAliasEndpoint_TLSScheme verifies that when TLS is enabled the
// capability URLs use the https:// scheme and the configured ListenAddr.
func TestBSVAliasEndpoint_TLSScheme(t *testing.T) {
	wallet := newMockWallet(t)
	store := newMockStore()
	config := DefaultConfig()
	config.Security.RateLimit.RPM = 0
	config.TLS.Enabled = true
	d, err := New(config, wallet, store, nil)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/.well-known/bsvalias", nil)
	req.Host = "bitfs.example.com" // should be ignored
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
	assert.True(t, strings.HasPrefix(pki, "https://localhost:8080/"),
		"pki URL should use https scheme with configured addr, got: %s", pki)
}

// TestBSVAliasEndpoint_CustomHost verifies that the configured ListenAddr is
// used for capability URLs, and the Host header is ignored (L-NEW-11).
func TestBSVAliasEndpoint_CustomHost(t *testing.T) {
	config := DefaultConfig()
	config.ListenAddr = "my-custom-host.io:9090"
	config.Security.RateLimit.RPM = 0
	d, err := New(config, newMockWallet(t), newMockStore(), nil)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/.well-known/bsvalias", nil)
	req.Host = "evil.attacker.com"
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
	assert.Contains(t, pki, "my-custom-host.io:9090",
		"pki URL should contain the configured ListenAddr")
	assert.NotContains(t, pki, "evil.attacker.com",
		"pki URL must not contain the request Host header")
}

// --- Content Negotiation: Link Node ---

// TestContentNegotiation_LinkNode verifies that a node with type "link"
// (symlink) is served through the content negotiation path without error.
// Because "link" is neither "dir" nor "file", it falls into the non-dir branch
// of serveNodeContent.
func TestContentNegotiation_LinkNode(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	meta.nodes["/symlink"] = &NodeInfo{
		Type:     "link",
		MimeType: "application/x-symlink",
		FileSize: 0,
		Access:   "free",
	}

	// Test JSON response for a link node
	req := httptest.NewRequest("GET", "/symlink", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var node NodeInfo
	err := json.Unmarshal(w.Body.Bytes(), &node)
	require.NoError(t, err)
	assert.Equal(t, "link", node.Type)

	// Test HTML response for a link node (goes through the else branch of serveHTML)
	req2 := httptest.NewRequest("GET", "/symlink", nil)
	req2.Header.Set("Accept", "text/html")
	w2 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "link") // type shown in HTML

	// Test Markdown response for a link node (goes through the else branch of serveMarkdown)
	req3 := httptest.NewRequest("GET", "/symlink", nil)
	req3.Header.Set("Accept", "text/markdown")
	w3 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w3, req3)

	assert.Equal(t, http.StatusOK, w3.Code)
	assert.Contains(t, w3.Header().Get("Content-Type"), "text/markdown")
}

// --- WriteJSONError Full Structure ---

// TestWriteJSONError_FullStructure unmarshals the JSON error response and
// verifies that all fields (code, message, retry, cached) are present and
// correctly typed.
func TestWriteJSONError_FullStructure(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusForbidden, "ACCESS_DENIED", "You do not have permission")

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Unmarshal into a generic structure to verify all fields
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Retry   bool   `json:"retry"`
			Cached  bool   `json:"cached"`
		} `json:"error"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &envelope)
	require.NoError(t, err, "JSON error response should be valid JSON")

	assert.Equal(t, "ACCESS_DENIED", envelope.Error.Code)
	assert.Equal(t, "You do not have permission", envelope.Error.Message)
	assert.False(t, envelope.Error.Retry, "403 should not be retryable")
	assert.False(t, envelope.Error.Cached, "cached should always be false")
}

// --- Session Edge Cases ---

// TestCreateSession_ExpiredImmediately creates a session with zero TTL, which
// means ExpiresAt == CreatedAt. Since time.Now() will be at or after ExpiresAt,
// the session should be immediately expired when retrieved.
func TestCreateSession_ExpiredImmediately(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	buyer := make([]byte, 33)
	buyer[0] = 0x02
	seller := make([]byte, 33)
	seller[0] = 0x03
	sharedX := make([]byte, 32)
	nonceB := []byte{0xAA}
	nonceS := []byte{0xBB}

	session := d.CreateSession(buyer, seller, sharedX, nonceB, nonceS, 0)

	// The session was created, so it has an ID and key
	assert.NotEmpty(t, session.ID)
	assert.NotEmpty(t, session.SessionKey)
	assert.Equal(t, session.CreatedAt, session.ExpiresAt)

	// Retrieving it should return ErrSessionExpired because
	// time.Now() is at or after ExpiresAt.
	_, err := d.GetSession(session.ID)
	assert.ErrorIs(t, err, ErrSessionExpired)

	// Confirm the session was cleaned up from the map
	d.sessionsMu.RLock()
	_, exists := d.sessions[session.ID]
	d.sessionsMu.RUnlock()
	assert.False(t, exists, "expired session should be removed from map on access")
}

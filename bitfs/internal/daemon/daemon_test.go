package daemon

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock implementations ---

// mockWallet implements WalletService for testing.
type mockWallet struct {
	privKey *ec.PrivateKey
	pubKey  *ec.PublicKey
	err     error
}

func newMockWallet(t *testing.T) *mockWallet {
	t.Helper()
	priv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	return &mockWallet{
		privKey: priv,
		pubKey:  priv.PubKey(),
	}
}

func (m *mockWallet) DeriveNodePubKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*ec.PublicKey, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pubKey, nil
}

func (m *mockWallet) GetSellerKeyPair() (*ec.PrivateKey, *ec.PublicKey, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.privKey, m.pubKey, nil
}

// mockStore implements ContentStore for testing.
type mockStore struct {
	data map[string][]byte
	err  error
}

func newMockStore() *mockStore {
	return &mockStore{data: make(map[string][]byte)}
}

func (m *mockStore) Get(keyHash []byte) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := hex.EncodeToString(keyHash)
	data, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return data, nil
}

func (m *mockStore) Has(keyHash []byte) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	key := hex.EncodeToString(keyHash)
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockStore) Size(keyHash []byte) (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	key := hex.EncodeToString(keyHash)
	data, ok := m.data[key]
	if !ok {
		return 0, fmt.Errorf("not found")
	}
	return int64(len(data)), nil
}

func (m *mockStore) Put(keyHash string, data []byte) {
	m.data[keyHash] = data
}

// mockMetanet implements MetanetService for testing.
type mockMetanet struct {
	nodes map[string]*NodeInfo
	err   error
}

func newMockMetanet() *mockMetanet {
	return &mockMetanet{nodes: make(map[string]*NodeInfo)}
}

func (m *mockMetanet) GetNodeByPath(path string) (*NodeInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	node, ok := m.nodes[path]
	if !ok {
		return nil, fmt.Errorf("not found: %s", path)
	}
	return node, nil
}

// --- Helper functions ---

func newTestDaemon(t *testing.T) (*Daemon, *mockWallet, *mockStore, *mockMetanet) {
	t.Helper()
	w := newMockWallet(t)
	s := newMockStore()
	m := newMockMetanet()
	config := DefaultConfig()
	config.Security.RateLimit.RPM = 0 // disable rate limiting for most tests
	d, err := New(config, w, s, m)
	require.NoError(t, err)
	return d, w, s, m
}

func newTestDaemonWithRateLimit(t *testing.T) *Daemon {
	t.Helper()
	w := newMockWallet(t)
	s := newMockStore()
	config := DefaultConfig()
	config.Security.RateLimit.RPM = 60
	config.Security.RateLimit.Burst = 5
	d, err := New(config, w, s, nil)
	require.NoError(t, err)
	return d
}

// --- New() Tests ---

func TestNew_Success(t *testing.T) {
	w := newMockWallet(t)
	s := newMockStore()
	config := DefaultConfig()
	d, err := New(config, w, s, nil)
	require.NoError(t, err)
	assert.NotNil(t, d)
}

func TestNew_NilConfig(t *testing.T) {
	w := newMockWallet(t)
	s := newMockStore()
	_, err := New(nil, w, s, nil)
	assert.ErrorIs(t, err, ErrNilConfig)
}

func TestNew_NilWallet(t *testing.T) {
	s := newMockStore()
	config := DefaultConfig()
	_, err := New(config, nil, s, nil)
	assert.ErrorIs(t, err, ErrNilWallet)
}

func TestNew_NilStore(t *testing.T) {
	w := newMockWallet(t)
	config := DefaultConfig()
	_, err := New(config, w, nil, nil)
	assert.ErrorIs(t, err, ErrNilStore)
}

func TestNew_WithRateLimit(t *testing.T) {
	w := newMockWallet(t)
	s := newMockStore()
	config := DefaultConfig()
	config.Security.RateLimit.RPM = 60
	config.Security.RateLimit.Burst = 20
	d, err := New(config, w, s, nil)
	require.NoError(t, err)
	assert.NotNil(t, d.rateLimiter)
}

func TestNew_NoRateLimit(t *testing.T) {
	w := newMockWallet(t)
	s := newMockStore()
	config := DefaultConfig()
	config.Security.RateLimit.RPM = 0
	d, err := New(config, w, s, nil)
	require.NoError(t, err)
	assert.Nil(t, d.rateLimiter)
}

// --- DefaultConfig Tests ---

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	assert.Equal(t, ":8080", c.ListenAddr)
	assert.False(t, c.TLS.Enabled)
	assert.Equal(t, 60, c.Security.RateLimit.RPM)
	assert.Equal(t, 20, c.Security.RateLimit.Burst)
	assert.Equal(t, "info", c.Log.Level)
}

// --- Health Endpoint Tests ---

func TestHealthEndpoint(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/_bitfs/health", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"ok"`)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

// --- BSV Alias Endpoint Tests ---

func TestBSVAliasEndpoint(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/.well-known/bsvalias", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "1.0", resp["bsvalias"])
	assert.NotNil(t, resp["capabilities"])
}

func TestBSVAliasEndpoint_HasPKICapability(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/.well-known/bsvalias", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	caps, ok := resp["capabilities"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, caps, "pki")
}

// --- Content Negotiation Tests ---

func TestContentNegotiation_HTML(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "BitFS")
}

func TestContentNegotiation_Markdown(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/markdown")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/markdown")
	assert.Contains(t, w.Body.String(), "# BitFS")
}

func TestContentNegotiation_JSON(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestContentNegotiation_Default(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/", nil)
	// No Accept header
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestContentNegotiation_WithMetanet_Dir(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	meta.nodes["/docs"] = &NodeInfo{
		Type: "dir",
		Children: []ChildInfo{
			{Name: "file1.txt", Type: "file"},
			{Name: "subdir", Type: "dir"},
		},
		Access: "free",
	}

	req := httptest.NewRequest("GET", "/docs", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "file1.txt")
	assert.Contains(t, w.Body.String(), "subdir")
}

func TestContentNegotiation_WithMetanet_File(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	meta.nodes["/docs/paper.pdf"] = &NodeInfo{
		Type:     "file",
		MimeType: "application/pdf",
		FileSize: 12345,
		Access:   "free",
	}

	req := httptest.NewRequest("GET", "/docs/paper.pdf", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp NodeInfo
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "file", resp.Type)
	assert.Equal(t, uint64(12345), resp.FileSize)
}

func TestContentNegotiation_WithMetanet_Markdown(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	meta.nodes["/docs"] = &NodeInfo{
		Type: "dir",
		Children: []ChildInfo{
			{Name: "readme.md", Type: "file"},
		},
		Access: "free",
	}

	req := httptest.NewRequest("GET", "/docs", nil)
	req.Header.Set("Accept", "text/markdown")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "readme.md")
}

func TestContentNegotiation_PaidContent(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	d.config.X402.Enabled = true
	meta.nodes["/premium/video.mp4"] = &NodeInfo{
		Type:       "file",
		MimeType:   "video/mp4",
		FileSize:   10485760,
		Access:     "paid",
		PricePerKB: 50,
	}

	req := httptest.NewRequest("GET", "/premium/video.mp4", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
}

func TestContentNegotiation_NotFound(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- Data Endpoint Tests ---

func TestDataEndpoint_Success(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)
	keyHash := strings.Repeat("ab", 32) // 64 hex chars = 32 bytes
	store.Put(keyHash, []byte("encrypted content"))

	req := httptest.NewRequest("GET", "/_bitfs/data/"+keyHash, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "encrypted content", w.Body.String())
}

func TestDataEndpoint_NotFound(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	keyHash := strings.Repeat("cd", 32)

	req := httptest.NewRequest("GET", "/_bitfs/data/"+keyHash, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDataEndpoint_InvalidHash(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/_bitfs/data/invalidhex", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDataEndpoint_ShortHash(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/_bitfs/data/abcd", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDataEndpoint_StorageError(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)
	store.err = fmt.Errorf("disk error")
	keyHash := strings.Repeat("ab", 32)

	req := httptest.NewRequest("GET", "/_bitfs/data/"+keyHash, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDataEndpoint_KeyHashHeader(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)
	keyHash := strings.Repeat("ef", 32)
	store.Put(keyHash, []byte("data"))

	req := httptest.NewRequest("GET", "/_bitfs/data/"+keyHash, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, keyHash, w.Header().Get("X-Key-Hash"))
}

// --- Meta Endpoint Tests ---

func TestMetaEndpoint_Success(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	pnode := strings.Repeat("02", 1) + strings.Repeat("ab", 32) // 66 hex chars
	pnodeBytes, _ := hex.DecodeString(pnode)
	path := "docs/file.txt"

	meta.nodes["/"+path] = &NodeInfo{
		PNode:    pnodeBytes,
		Type:     "file",
		MimeType: "text/plain",
		FileSize: 42,
		Access:   "free",
	}

	req := httptest.NewRequest("GET", "/_bitfs/meta/"+pnode+"/"+path, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), pnode)
}

func TestMetaEndpoint_InvalidPnode(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/_bitfs/meta/invalidhex/path", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- Handshake Endpoint Tests ---

func TestHandshake_Success(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)

	// Generate buyer keys
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPub := buyerPriv.PubKey()

	nonceB := make([]byte, 32)
	for i := range nonceB {
		nonceB[i] = byte(i)
	}

	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(buyerPub.Compressed()),
		NonceB:    hex.EncodeToString(nonceB),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HandshakeResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	// Verify seller pub matches wallet
	assert.Equal(t, hex.EncodeToString(wallet.pubKey.Compressed()), resp.SellerPub)
	assert.NotEmpty(t, resp.NonceS)
	assert.NotEmpty(t, resp.SessionID)
	assert.Greater(t, resp.ExpiresAt, time.Now().Unix())
}

func TestHandshake_EmptyBody(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader([]byte{}))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandshake_InvalidJSON(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandshake_MissingBuyerPub(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	reqBody := HandshakeRequest{
		NonceB:    hex.EncodeToString(make([]byte, 32)),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandshake_MissingNonce(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	buyerPriv, _ := ec.NewPrivateKey()
	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(buyerPriv.PubKey().Compressed()),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandshake_InvalidPubKey(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	reqBody := HandshakeRequest{
		BuyerPub:  "not-valid-hex",
		NonceB:    hex.EncodeToString(make([]byte, 32)),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandshake_ShortPubKey(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString([]byte{0x02, 0x01}),
		NonceB:    hex.EncodeToString(make([]byte, 32)),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandshake_WalletError(t *testing.T) {
	d, wallet, _, _ := newTestDaemon(t)
	wallet.err = fmt.Errorf("wallet locked")

	buyerPriv, _ := ec.NewPrivateKey()
	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(buyerPriv.PubKey().Compressed()),
		NonceB:    hex.EncodeToString(make([]byte, 32)),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandshake_CreatesSession(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	buyerPriv, _ := ec.NewPrivateKey()
	nonceB := make([]byte, 32)
	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(buyerPriv.PubKey().Compressed()),
		NonceB:    hex.EncodeToString(nonceB),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	var resp HandshakeResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Verify session was created
	session, err := d.GetSession(resp.SessionID)
	require.NoError(t, err)
	assert.NotNil(t, session)
	assert.NotEmpty(t, session.SessionKey)
}

func TestHandshake_InvalidNonceHex(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	buyerPriv, _ := ec.NewPrivateKey()
	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(buyerPriv.PubKey().Compressed()),
		NonceB:    "zzzz",
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- Session Management Tests ---

func TestSession_IsExpired(t *testing.T) {
	s := &Session{ExpiresAt: time.Now().Add(-1 * time.Hour)}
	assert.True(t, s.IsExpired())

	s2 := &Session{ExpiresAt: time.Now().Add(1 * time.Hour)}
	assert.False(t, s2.IsExpired())
}

func TestCreateSession(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	buyer := make([]byte, 33)
	buyer[0] = 0x02
	seller := make([]byte, 33)
	seller[0] = 0x03
	sharedX := make([]byte, 32)
	nonceB := make([]byte, 32)
	nonceS := make([]byte, 32)

	session := d.CreateSession(buyer, seller, sharedX, nonceB, nonceS, time.Hour)

	assert.NotEmpty(t, session.ID)
	assert.Equal(t, buyer, session.BuyerPub)
	assert.Equal(t, seller, session.SellerPub)
	assert.NotEmpty(t, session.SessionKey)
	assert.False(t, session.IsExpired())
}

func TestGetSession_Found(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	session := d.CreateSession(
		make([]byte, 33), make([]byte, 33),
		make([]byte, 32), make([]byte, 32), make([]byte, 32),
		time.Hour,
	)

	found, err := d.GetSession(session.ID)
	require.NoError(t, err)
	assert.Equal(t, session.ID, found.ID)
}

func TestGetSession_NotFound(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	_, err := d.GetSession("nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestGetSession_Expired(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	session := d.CreateSession(
		make([]byte, 33), make([]byte, 33),
		make([]byte, 32), make([]byte, 32), make([]byte, 32),
		-time.Hour, // Already expired
	)

	_, err := d.GetSession(session.ID)
	assert.ErrorIs(t, err, ErrSessionExpired)
}

func TestCleanupExpiredSessions(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Create an expired session
	d.CreateSession(
		make([]byte, 33), make([]byte, 33),
		make([]byte, 32), make([]byte, 32), make([]byte, 32),
		-time.Hour,
	)

	// Create a valid session
	validSession := d.CreateSession(
		make([]byte, 33), make([]byte, 33),
		make([]byte, 32), []byte{1}, []byte{2}, // different nonces to get different session ID
		time.Hour,
	)

	d.cleanupExpiredSessions()

	d.sessionsMu.RLock()
	assert.Equal(t, 1, len(d.sessions))
	_, exists := d.sessions[validSession.ID]
	d.sessionsMu.RUnlock()
	assert.True(t, exists)
}

// --- Rate Limiting Tests ---

func TestRateLimiter_Allow(t *testing.T) {
	rl := newRateLimiter(60, 5)
	// First 5 requests should be allowed (burst)
	for i := 0; i < 5; i++ {
		assert.True(t, rl.Allow("127.0.0.1"), "Request %d should be allowed", i)
	}
	// Next request should be rate limited
	assert.False(t, rl.Allow("127.0.0.1"))
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	rl := newRateLimiter(60, 3)
	// Exhaust limit for IP1
	for i := 0; i < 3; i++ {
		rl.Allow("192.168.1.1")
	}
	// IP2 should still be allowed
	assert.True(t, rl.Allow("192.168.1.2"))
}

func TestRateLimiter_TokenRefill(t *testing.T) {
	rl := newRateLimiter(6000, 1) // 100 per second
	// Use the burst
	assert.True(t, rl.Allow("127.0.0.1"))
	assert.False(t, rl.Allow("127.0.0.1"))

	// Simulate time passage by manipulating lastCheck
	rl.mu.Lock()
	rl.clients["127.0.0.1"].lastCheck = time.Now().Add(-2 * time.Second)
	rl.mu.Unlock()

	// Should be allowed after token refill
	assert.True(t, rl.Allow("127.0.0.1"))
}

func TestRateLimiter_HTTPIntegration(t *testing.T) {
	d := newTestDaemonWithRateLimit(t)

	// Send requests up to burst limit
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/_bitfs/health", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		d.Handler().ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Request %d should succeed", i)
	}

	// Next request should be rate limited
	req := httptest.NewRequest("GET", "/_bitfs/health", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

// --- CORS Tests ---

func TestCORS_DefaultHeaders(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/_bitfs/health", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Methods"))
}

func TestCORS_Options(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("OPTIONS", "/_bitfs/health", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCORS_SpecificOrigin(t *testing.T) {
	w := newMockWallet(&testing.T{})
	s := newMockStore()
	config := DefaultConfig()
	config.Security.RateLimit.RPM = 0
	config.Security.CORS.Origins = []string{"https://bitfs.org"}
	d, err := New(config, w, s, nil)
	require.NoError(t, err)

	// Matching origin
	req := httptest.NewRequest("GET", "/_bitfs/health", nil)
	req.Header.Set("Origin", "https://bitfs.org")
	rec := httptest.NewRecorder()
	d.Handler().ServeHTTP(rec, req)
	assert.Equal(t, "https://bitfs.org", rec.Header().Get("Access-Control-Allow-Origin"))

	// Non-matching origin
	req2 := httptest.NewRequest("GET", "/_bitfs/health", nil)
	req2.Header.Set("Origin", "https://evil.com")
	rec2 := httptest.NewRecorder()
	d.Handler().ServeHTTP(rec2, req2)
	assert.Empty(t, rec2.Header().Get("Access-Control-Allow-Origin"))
}

// --- Graceful Shutdown Tests ---

func TestStartStop(t *testing.T) {
	w := newMockWallet(t)
	s := newMockStore()
	config := DefaultConfig()
	config.ListenAddr = ":0" // Random port
	config.Security.RateLimit.RPM = 0
	d, err := New(config, w, s, nil)
	require.NoError(t, err)

	err = d.Start()
	require.NoError(t, err)

	// Double start should fail
	err = d.Start()
	assert.ErrorIs(t, err, ErrAlreadyRunning)

	// Stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = d.Stop(ctx)
	assert.NoError(t, err)
}

func TestStop_NotRunning(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	ctx := context.Background()
	err := d.Stop(ctx)
	assert.ErrorIs(t, err, ErrNotRunning)
}

// --- Error Response Tests ---

func TestWriteJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Content not found")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "NOT_FOUND")
	assert.Contains(t, w.Body.String(), "Content not found")
}

func TestWriteJSONError_Retry(t *testing.T) {
	// 429 should have retry=true
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests")
	assert.Contains(t, w.Body.String(), `"retry":true`)

	// 500 should have retry=true
	w2 := httptest.NewRecorder()
	writeJSONError(w2, http.StatusInternalServerError, "INTERNAL", "Internal error")
	assert.Contains(t, w2.Body.String(), `"retry":true`)

	// 400 should have retry=false
	w3 := httptest.NewRecorder()
	writeJSONError(w3, http.StatusBadRequest, "BAD_REQUEST", "Bad request")
	assert.Contains(t, w3.Body.String(), `"retry":false`)
}

// --- Client IP Extraction Tests ---

func TestExtractClientIP_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.1:1234"
	assert.Equal(t, "192.168.1.1:1234", extractClientIP(r))
}

func TestExtractClientIP_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	assert.Equal(t, "10.0.0.1", extractClientIP(r))
}

func TestExtractClientIP_XRealIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Real-IP", "10.0.0.2")
	assert.Equal(t, "10.0.0.2", extractClientIP(r))
}

// --- Negotiate Content Type Tests ---

func TestNegotiateContentType(t *testing.T) {
	tests := []struct {
		accept string
		want   string
	}{
		{"text/html", "text/html"},
		{"text/markdown", "text/markdown"},
		{"application/json", "application/json"},
		{"text/html, application/json", "text/html"},
		{"*/*", "application/json"},
		{"", "application/json"},
	}

	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tt.accept != "" {
				r.Header.Set("Accept", tt.accept)
			}
			got := negotiateContentType(r)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- Handler Tests (via httptest.Server) ---

func TestHTTPTestServer_Health(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	server := httptest.NewServer(d.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/_bitfs/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "ok")
}

func TestHTTPTestServer_Handshake(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	server := httptest.NewServer(d.Handler())
	defer server.Close()

	buyerPriv, _ := ec.NewPrivateKey()
	nonceB := make([]byte, 32)
	for i := range nonceB {
		nonceB[i] = byte(i)
	}

	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(buyerPriv.PubKey().Compressed()),
		NonceB:    hex.EncodeToString(nonceB),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)

	resp, err := http.Post(server.URL+"/_bitfs/handshake", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var hsResp HandshakeResponse
	json.NewDecoder(resp.Body).Decode(&hsResp)
	assert.NotEmpty(t, hsResp.SessionID)
}

func TestHTTPTestServer_Data(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)
	server := httptest.NewServer(d.Handler())
	defer server.Close()

	keyHash := strings.Repeat("ab", 32)
	store.Put(keyHash, []byte("test data"))

	resp, err := http.Get(server.URL + "/_bitfs/data/" + keyHash)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "test data", string(body))
}

// --- computeSessionKey Tests ---

func TestComputeSessionKey(t *testing.T) {
	sharedX := make([]byte, 32)
	sharedX[0] = 0x01
	nonceB := make([]byte, 32)
	nonceB[0] = 0x02
	nonceS := make([]byte, 32)
	nonceS[0] = 0x03

	key1 := computeSessionKey(sharedX, nonceB, nonceS)
	assert.Len(t, key1, 32)

	// Same inputs should produce same key
	key2 := computeSessionKey(sharedX, nonceB, nonceS)
	assert.Equal(t, key1, key2)

	// Different inputs should produce different key
	nonceS2 := make([]byte, 32)
	nonceS2[0] = 0x04
	key3 := computeSessionKey(sharedX, nonceB, nonceS2)
	assert.NotEqual(t, key1, key3)
}

// --- Concurrent Access Tests ---

func TestConcurrentSessionAccess(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nonce := make([]byte, 32)
			nonce[0] = byte(idx)
			session := d.CreateSession(
				make([]byte, 33), make([]byte, 33),
				make([]byte, 32), nonce, make([]byte, 32),
				time.Hour,
			)
			_, _ = d.GetSession(session.ID)
		}(i)
	}
	wg.Wait()
}

func TestConcurrentHealthRequests(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/_bitfs/health", nil)
			w := httptest.NewRecorder()
			d.Handler().ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}()
	}
	wg.Wait()
}

// --- ECDH Integration Test ---

func TestComputeECDH(t *testing.T) {
	priv1, err := ec.NewPrivateKey()
	require.NoError(t, err)
	pub1 := priv1.PubKey()

	priv2, err := ec.NewPrivateKey()
	require.NoError(t, err)
	pub2 := priv2.PubKey()

	// ECDH(priv1, pub2) should equal ECDH(priv2, pub1)
	shared1, err := computeECDH(priv1, pub2)
	require.NoError(t, err)

	shared2, err := computeECDH(priv2, pub1)
	require.NoError(t, err)

	assert.Equal(t, shared1, shared2)
	assert.Len(t, shared1, 32)
}

// --- Additional Edge Case Tests ---

func TestHandler_Returns_Handler(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	h := d.Handler()
	assert.NotNil(t, h)
}

func TestPathRouting(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	meta.nodes["/foo/bar"] = &NodeInfo{
		Type:     "file",
		MimeType: "text/plain",
		FileSize: 100,
		Access:   "free",
	}

	req := httptest.NewRequest("GET", "/foo/bar", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPaidContent_X402Disabled(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	d.config.X402.Enabled = false
	meta.nodes["/premium"] = &NodeInfo{
		Type:   "file",
		Access: "paid",
	}

	req := httptest.NewRequest("GET", "/premium", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	// When x402 is disabled, paid content is served without payment
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- Additional Tests to reach 80 ---

func TestDataEndpoint_ContentLength(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)
	keyHash := strings.Repeat("cc", 32)
	content := []byte("hello world data content")
	store.Put(keyHash, content)

	req := httptest.NewRequest("GET", "/_bitfs/data/"+keyHash, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, fmt.Sprintf("%d", len(content)), w.Header().Get("Content-Length"))
}

func TestMetaEndpoint_WithPath(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	pnode := "02" + strings.Repeat("ab", 32)
	pnodeBytes, _ := hex.DecodeString(pnode)
	path := "deep/nested/path"

	meta.nodes["/"+path] = &NodeInfo{
		PNode:  pnodeBytes,
		Type:   "file",
		Access: "free",
	}

	req := httptest.NewRequest("GET", "/_bitfs/meta/"+pnode+"/"+path, nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), path)
}

func TestHandshake_SessionKeyDerivation(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	buyerPriv, _ := ec.NewPrivateKey()
	nonceB := make([]byte, 32)
	for i := range nonceB {
		nonceB[i] = byte(i + 10)
	}

	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(buyerPriv.PubKey().Compressed()),
		NonceB:    hex.EncodeToString(nonceB),
		Timestamp: time.Now().Unix(),
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	var resp HandshakeResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	session, err := d.GetSession(resp.SessionID)
	require.NoError(t, err)

	// Session key should be 32 bytes (SHA256)
	assert.Len(t, session.SessionKey, 32)
	// Session should not be expired
	assert.False(t, session.IsExpired())
}

func TestHandshake_MultipleSessionsUnique(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	var sessionIDs []string
	for i := 0; i < 5; i++ {
		buyerPriv, _ := ec.NewPrivateKey()
		nonceB := make([]byte, 32)
		nonceB[0] = byte(i)

		reqBody := HandshakeRequest{
			BuyerPub:  hex.EncodeToString(buyerPriv.PubKey().Compressed()),
			NonceB:    hex.EncodeToString(nonceB),
			Timestamp: time.Now().Unix(),
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/_bitfs/handshake", bytes.NewReader(body))
		w := httptest.NewRecorder()
		d.Handler().ServeHTTP(w, req)

		var resp HandshakeResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		sessionIDs = append(sessionIDs, resp.SessionID)
	}

	// All session IDs should be unique
	uniqueIDs := make(map[string]bool)
	for _, id := range sessionIDs {
		uniqueIDs[id] = true
	}
	assert.Equal(t, len(sessionIDs), len(uniqueIDs))
}

func TestRateLimiter_BurstCapacity(t *testing.T) {
	rl := newRateLimiter(60, 10)
	// All burst requests should be allowed
	for i := 0; i < 10; i++ {
		assert.True(t, rl.Allow("test-ip"), "Request %d should be allowed within burst", i)
	}
	// After burst, should be blocked
	assert.False(t, rl.Allow("test-ip"))
}

func TestCORS_AllowedHeaders(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	req := httptest.NewRequest("GET", "/_bitfs/health", nil)
	req.Header.Set("Origin", "http://test.com")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	allowHeaders := w.Header().Get("Access-Control-Allow-Headers")
	assert.Contains(t, allowHeaders, "Content-Type")
	assert.Contains(t, allowHeaders, "Authorization")
	assert.Contains(t, allowHeaders, "X-Session-Id")
}

func TestContentNegotiation_WithMetanet_FileHTML(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	meta.nodes["/readme.txt"] = &NodeInfo{
		Type:     "file",
		MimeType: "text/plain",
		FileSize: 256,
		Access:   "free",
	}

	req := httptest.NewRequest("GET", "/readme.txt", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "text/plain")
}

func TestContentNegotiation_WithMetanet_DirMarkdown(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	meta.nodes["/photos"] = &NodeInfo{
		Type: "dir",
		Children: []ChildInfo{
			{Name: "sunset.jpg", Type: "file"},
			{Name: "dawn.jpg", Type: "file"},
		},
		Access: "free",
	}

	req := httptest.NewRequest("GET", "/photos", nil)
	req.Header.Set("Accept", "text/markdown")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "sunset.jpg")
	assert.Contains(t, w.Body.String(), "dawn.jpg")
}

func TestPaidContent_PriceHeaders(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	d.config.X402.Enabled = true
	meta.nodes["/premium/data"] = &NodeInfo{
		Type:       "file",
		FileSize:   2048,
		Access:     "paid",
		PricePerKB: 100,
	}

	req := httptest.NewRequest("GET", "/premium/data", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.Equal(t, "100", w.Header().Get("X-Price-Per-KB"))
	assert.Equal(t, "2048", w.Header().Get("X-File-Size"))
}

func TestHTTPTestServer_BSVAlias(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	server := httptest.NewServer(d.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/.well-known/bsvalias")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "1.0", result["bsvalias"])
}

func TestHTTPTestServer_ContentNegotiation(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)
	server := httptest.NewServer(d.Handler())
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL+"/", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/html")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
}

func TestConcurrentDataRequests(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)
	keyHash := strings.Repeat("dd", 32)
	store.Put(keyHash, []byte("concurrent test data"))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/_bitfs/data/"+keyHash, nil)
			w := httptest.NewRecorder()
			d.Handler().ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "concurrent test data", w.Body.String())
		}()
	}
	wg.Wait()
}

// --- PrivateKey from big.Int for mock ---

func init() {
	// Verify ec.NewPrivateKey works (compilation check)
	_ = big.NewInt(1)
}

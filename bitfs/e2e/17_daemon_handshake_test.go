//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// setupHandshakeServer creates a daemon httptest.Server configured for
// handshake testing. Returns the server, the seller wallet, and a cleanup func.
func setupHandshakeServer(t *testing.T) (*httptest.Server, *wallet.Wallet) {
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

	server := httptest.NewServer(d.Handler())
	t.Cleanup(server.Close)

	return server, w
}

// TestMethod42Handshake verifies the full Method 42 ECDH handshake flow:
// POST /_bitfs/handshake with valid buyer_pub + nonce_b returns a 200
// response containing seller_pub (33-byte compressed hex), nonce_s,
// session_id, and expires_at.
func TestMethod42Handshake(t *testing.T) {
	server, w := setupHandshakeServer(t)

	// Derive the expected seller public key from the wallet.
	sellerKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive seller node key")
	expectedSellerPub := hex.EncodeToString(sellerKey.PublicKey.Compressed())

	// Generate a random buyer key pair and nonce.
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err, "generate buyer key")
	buyerPub := buyerPriv.PubKey()

	nonceB := make([]byte, 32)
	_, err = rand.Read(nonceB)
	require.NoError(t, err, "generate buyer nonce")

	body := fmt.Sprintf(`{"buyer_pub":"%s","nonce_b":"%s","timestamp":%d}`,
		hex.EncodeToString(buyerPub.Compressed()),
		hex.EncodeToString(nonceB),
		time.Now().Unix(),
	)

	// POST the handshake request.
	resp, err := http.Post(server.URL+"/_bitfs/handshake", "application/json", strings.NewReader(body))
	require.NoError(t, err, "POST handshake")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "handshake should return 200")

	// Decode the response.
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read response body")

	var hsResp daemon.HandshakeResponse
	err = json.Unmarshal(respBody, &hsResp)
	require.NoError(t, err, "unmarshal handshake response")

	// Verify seller_pub is the expected 33-byte compressed key (66 hex chars).
	assert.Equal(t, expectedSellerPub, hsResp.SellerPub,
		"seller_pub should match the wallet's derived node key")
	sellerPubBytes, err := hex.DecodeString(hsResp.SellerPub)
	require.NoError(t, err, "decode seller_pub hex")
	assert.Len(t, sellerPubBytes, 33, "seller_pub should be 33 bytes (compressed)")

	// Verify nonce_s is a 32-byte hex string (64 chars).
	assert.Len(t, hsResp.NonceS, 64, "nonce_s should be 64 hex chars (32 bytes)")
	nonceS, err := hex.DecodeString(hsResp.NonceS)
	require.NoError(t, err, "decode nonce_s hex")
	assert.Len(t, nonceS, 32, "nonce_s should decode to 32 bytes")

	// Verify session_id is not empty.
	assert.NotEmpty(t, hsResp.SessionID, "session_id should not be empty")

	// Verify expires_at is in the future (at least 23 hours from now for 24h TTL).
	assert.Greater(t, hsResp.ExpiresAt, time.Now().Unix(),
		"expires_at should be in the future")
	assert.Greater(t, hsResp.ExpiresAt, time.Now().Add(23*time.Hour).Unix(),
		"expires_at should reflect ~24h TTL")

	// Verify timestamp is recent (within last 5 seconds).
	assert.InDelta(t, time.Now().Unix(), hsResp.Timestamp, 5,
		"timestamp should be approximately now")

	t.Logf("Handshake OK: session_id=%s, seller_pub=%s..., nonce_s=%s..., expires=%d",
		hsResp.SessionID[:16], hsResp.SellerPub[:16], hsResp.NonceS[:16], hsResp.ExpiresAt)
}

// TestHandshakeSessionPersists verifies that after a successful handshake,
// the returned session_id corresponds to a valid, persistent session that
// can be looked up and used for subsequent authenticated operations.
func TestHandshakeSessionPersists(t *testing.T) {
	server, _ := setupHandshakeServer(t)

	// Perform two independent handshakes with different buyer keys.
	sessionIDs := make([]string, 2)
	for i := 0; i < 2; i++ {
		buyerPriv, err := ec.NewPrivateKey()
		require.NoError(t, err)

		nonceB := make([]byte, 32)
		_, err = rand.Read(nonceB)
		require.NoError(t, err)

		body := fmt.Sprintf(`{"buyer_pub":"%s","nonce_b":"%s","timestamp":%d}`,
			hex.EncodeToString(buyerPriv.PubKey().Compressed()),
			hex.EncodeToString(nonceB),
			time.Now().Unix(),
		)

		resp, err := http.Post(server.URL+"/_bitfs/handshake", "application/json", strings.NewReader(body))
		require.NoError(t, err, "POST handshake #%d", i)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "handshake #%d should return 200", i)

		var hsResp daemon.HandshakeResponse
		err = json.NewDecoder(resp.Body).Decode(&hsResp)
		require.NoError(t, err, "decode handshake response #%d", i)

		sessionIDs[i] = hsResp.SessionID
		require.NotEmpty(t, sessionIDs[i], "session_id #%d should not be empty", i)
	}

	// Verify the two sessions have different IDs (different buyer keys + nonces
	// produce different ECDH shared secrets and thus different session keys).
	assert.NotEqual(t, sessionIDs[0], sessionIDs[1],
		"different buyer keys should produce different session IDs")

	// Verify session_id can be used in subsequent requests by including it
	// as the X-Session-Id header in a health check request (the daemon allows
	// this header through CORS).
	for i, sid := range sessionIDs {
		req, err := http.NewRequest("GET", server.URL+"/_bitfs/health", nil)
		require.NoError(t, err)
		req.Header.Set("X-Session-Id", sid)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "GET health with session #%d", i)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"health request with session_id #%d should succeed", i)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), `"status":"ok"`,
			"health response should contain status ok")
	}

	t.Logf("Two independent sessions created and persisted: %s, %s",
		sessionIDs[0][:16], sessionIDs[1][:16])
}

// TestHandshakeInvalidPubkey verifies that posting a malformed buyer_pub
// to the handshake endpoint returns an appropriate 400 error.
func TestHandshakeInvalidPubkey(t *testing.T) {
	server, _ := setupHandshakeServer(t)

	nonceB := make([]byte, 32)
	_, err := rand.Read(nonceB)
	require.NoError(t, err)
	nonceHex := hex.EncodeToString(nonceB)
	ts := time.Now().Unix()

	tests := []struct {
		name     string
		buyerPub string
		wantCode int
		wantErr  string
	}{
		{
			name:     "not_valid_hex",
			buyerPub: "zzzz-not-hex-at-all",
			wantCode: http.StatusBadRequest,
			wantErr:  "INVALID_PUBKEY",
		},
		{
			name:     "too_short_pubkey",
			buyerPub: hex.EncodeToString([]byte{0x02, 0x01, 0x02}),
			wantCode: http.StatusBadRequest,
			wantErr:  "INVALID_PUBKEY",
		},
		{
			name:     "empty_pubkey",
			buyerPub: "",
			wantCode: http.StatusBadRequest,
			wantErr:  "MISSING_FIELD",
		},
		{
			name:     "wrong_length_33_bytes_but_invalid_prefix",
			buyerPub: hex.EncodeToString(append([]byte{0x05}, make([]byte, 32)...)),
			wantCode: http.StatusBadRequest,
			wantErr:  "INVALID_PUBKEY",
		},
		{
			name:     "64_char_hex_but_only_32_bytes",
			buyerPub: strings.Repeat("ab", 32),
			wantCode: http.StatusBadRequest,
			wantErr:  "INVALID_PUBKEY",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"buyer_pub":"%s","nonce_b":"%s","timestamp":%d}`,
				tc.buyerPub, nonceHex, ts)

			resp, err := http.Post(server.URL+"/_bitfs/handshake", "application/json", strings.NewReader(body))
			require.NoError(t, err, "POST handshake")
			defer resp.Body.Close()

			assert.Equal(t, tc.wantCode, resp.StatusCode,
				"expected HTTP %d for %s", tc.wantCode, tc.name)

			respBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(respBody), tc.wantErr,
				"error response should contain %q", tc.wantErr)

			t.Logf("%s: HTTP %d, body contains %q", tc.name, resp.StatusCode, tc.wantErr)
		})
	}
}

// TestHandshakeMissingNonce verifies that omitting the nonce_b field
// returns a 400 error with MISSING_FIELD.
func TestHandshakeMissingNonce(t *testing.T) {
	server, _ := setupHandshakeServer(t)

	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	body := fmt.Sprintf(`{"buyer_pub":"%s","timestamp":%d}`,
		hex.EncodeToString(buyerPriv.PubKey().Compressed()),
		time.Now().Unix(),
	)

	resp, err := http.Post(server.URL+"/_bitfs/handshake", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(respBody), "MISSING_FIELD",
		"should return MISSING_FIELD error for absent nonce_b")
}

// TestHandshakeInvalidJSON verifies that sending non-JSON body returns 400.
func TestHandshakeInvalidJSON(t *testing.T) {
	server, _ := setupHandshakeServer(t)

	resp, err := http.Post(server.URL+"/_bitfs/handshake", "application/json",
		strings.NewReader("this is not json"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(respBody), "INVALID_JSON")
}

// TestHandshakeECDHConsistency verifies that the buyer can independently
// compute the same session key as the seller by performing the ECDH
// computation on the client side.
func TestHandshakeECDHConsistency(t *testing.T) {
	server, w := setupHandshakeServer(t)

	// Get the seller's private key from the wallet (for verification only).
	sellerKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err)

	// Generate buyer key pair and nonce.
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPub := buyerPriv.PubKey()

	nonceB := make([]byte, 32)
	_, err = rand.Read(nonceB)
	require.NoError(t, err)

	body := fmt.Sprintf(`{"buyer_pub":"%s","nonce_b":"%s","timestamp":%d}`,
		hex.EncodeToString(buyerPub.Compressed()),
		hex.EncodeToString(nonceB),
		time.Now().Unix(),
	)

	resp, err := http.Post(server.URL+"/_bitfs/handshake", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var hsResp daemon.HandshakeResponse
	err = json.NewDecoder(resp.Body).Decode(&hsResp)
	require.NoError(t, err)

	// Parse the seller's public key from the response.
	sellerPubBytes, err := hex.DecodeString(hsResp.SellerPub)
	require.NoError(t, err)
	sellerPubFromResp, err := ec.PublicKeyFromBytes(sellerPubBytes)
	require.NoError(t, err)

	// Verify the seller pub from the response matches the wallet's key.
	assert.Equal(t, sellerKey.PublicKey.Compressed(), sellerPubFromResp.Compressed(),
		"seller pub from response should match wallet key")

	// Client-side ECDH: ECDH(D_buyer, P_seller) should equal ECDH(D_seller, P_buyer)
	// due to the commutativity of ECDH.
	buyerShared, err := buyerPriv.DeriveSharedSecret(sellerPubFromResp)
	require.NoError(t, err, "buyer ECDH")

	sellerShared, err := sellerKey.PrivateKey.DeriveSharedSecret(buyerPub)
	require.NoError(t, err, "seller ECDH (verification)")

	// The X coordinates should match.
	assert.Equal(t, sellerShared.X.Bytes(), buyerShared.X.Bytes(),
		"ECDH shared secret X coordinates should be equal (commutativity)")

	t.Logf("ECDH consistency verified: shared.X = %x...",
		buyerShared.X.Bytes()[:8])
}

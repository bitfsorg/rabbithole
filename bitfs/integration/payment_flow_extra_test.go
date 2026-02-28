//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/wallet"
	"github.com/bitfsorg/libbitfs-go/x402"
)

// --- Invoice Expiry & Fields Tests ---

// TestInvoiceExpiryZeroTTL verifies that an invoice created with ttlSeconds=0
// has an expiry at or before the current time, so it becomes expired very quickly.
func TestInvoiceExpiryZeroTTL(t *testing.T) {
	capsuleHash := bytes.Repeat([]byte{0xaa}, 32)
	invoice := x402.NewInvoice(100, 1024, "1TestAddr", capsuleHash, 0)
	require.NotNil(t, invoice)

	// With ttl=0, Expiry = time.Now().Unix() + 0.
	// IsExpired checks time.Now().Unix() > Expiry. After even a 1-second sleep,
	// this should be true. We also verify the expiry is not in the future.
	assert.LessOrEqual(t, invoice.Expiry, time.Now().Unix(),
		"invoice with zero TTL should have expiry at or before current time")

	// Sleep briefly to ensure wall clock advances past the expiry.
	time.Sleep(1100 * time.Millisecond)
	assert.True(t, invoice.IsExpired(), "invoice with zero TTL should be expired after brief delay")
}

// TestInvoiceExpiryFuture verifies that an invoice with ttlSeconds=3600
// is not expired immediately after creation.
func TestInvoiceExpiryFuture(t *testing.T) {
	capsuleHash := bytes.Repeat([]byte{0xbb}, 32)
	invoice := x402.NewInvoice(50, 2048, "1FutureAddr", capsuleHash, 3600)
	require.NotNil(t, invoice)

	assert.False(t, invoice.IsExpired(), "invoice with 1-hour TTL should not be expired immediately")
	assert.Greater(t, invoice.Expiry, time.Now().Unix(),
		"invoice expiry should be in the future")
}

// TestInvoiceZeroPrice verifies that an invoice with pricePerKB=0 has price 0.
func TestInvoiceZeroPrice(t *testing.T) {
	capsuleHash := bytes.Repeat([]byte{0xcc}, 32)
	invoice := x402.NewInvoice(0, 1024, "1ZeroPriceAddr", capsuleHash, 3600)
	require.NotNil(t, invoice)

	assert.Equal(t, uint64(0), invoice.Price, "price should be 0 when pricePerKB is 0")
	assert.Equal(t, uint64(0), invoice.PricePerKB)
}

// TestInvoiceLargeFile verifies price calculation for a 1GB file at 10 sat/KB.
// Expected: ceil(10 * 1073741824 / 1024) = 10 * 1048576 = 10485760.
func TestInvoiceLargeFile(t *testing.T) {
	capsuleHash := bytes.Repeat([]byte{0xdd}, 32)
	var fileSize uint64 = 1073741824 // 1 GB
	var pricePerKB uint64 = 10

	invoice := x402.NewInvoice(pricePerKB, fileSize, "1LargeFileAddr", capsuleHash, 3600)
	require.NotNil(t, invoice)

	// 1GB = 1048576 KB exactly, so price = 10 * 1048576 = 10485760 (no rounding needed).
	assert.Equal(t, uint64(10485760), invoice.Price,
		"1GB file at 10 sat/KB should cost 10485760 satoshis")

	// Also verify via CalculatePrice directly.
	expected := x402.CalculatePrice(pricePerKB, fileSize)
	assert.Equal(t, expected, invoice.Price)
}

// TestInvoiceFieldsPreserved verifies all fields are correctly populated.
func TestInvoiceFieldsPreserved(t *testing.T) {
	capsuleHash := bytes.Repeat([]byte{0xee}, 32)
	paymentAddr := "1PreservedFieldsAddr"
	var pricePerKB uint64 = 200
	var fileSize uint64 = 5120

	invoice := x402.NewInvoice(pricePerKB, fileSize, paymentAddr, capsuleHash, 7200)
	require.NotNil(t, invoice)

	assert.NotEmpty(t, invoice.ID, "invoice ID should not be empty")
	assert.Equal(t, paymentAddr, invoice.PaymentAddr, "payment address should match")
	assert.Equal(t, capsuleHash, invoice.CapsuleHash, "capsule hash should match")
	assert.Equal(t, pricePerKB, invoice.PricePerKB, "pricePerKB should match")
	assert.Equal(t, fileSize, invoice.FileSize, "fileSize should match")
	assert.Equal(t, x402.CalculatePrice(pricePerKB, fileSize), invoice.Price,
		"price should match CalculatePrice result")
}

// --- HTLC Parameter Validation Tests ---

// TestHTLCParamsNilCapsuleHash verifies BuildHTLC fails with nil CapsuleHash.
func TestHTLCParamsNilCapsuleHash(t *testing.T) {
	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerAddr := bytes.Repeat([]byte{0x11}, 20)

	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	_, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPub,
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  nil,
		Amount:       1000,
		Timeout:      144,
	})
	assert.ErrorIs(t, err, x402.ErrHTLCBuildFailed,
		"nil capsule hash should fail with ErrHTLCBuildFailed")
}

// TestHTLCParamsCapsuleHashWrongLength verifies BuildHTLC fails with 16-byte CapsuleHash.
func TestHTLCParamsCapsuleHashWrongLength(t *testing.T) {
	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerAddr := bytes.Repeat([]byte{0x11}, 20)
	shortHash := bytes.Repeat([]byte{0xab}, 16) // 16 bytes instead of 32

	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	_, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPub,
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  shortHash,
		Amount:       1000,
		Timeout:      144,
	})
	assert.ErrorIs(t, err, x402.ErrHTLCBuildFailed,
		"16-byte capsule hash should fail with ErrHTLCBuildFailed")
}

// TestHTLCParamsZeroTimeout verifies BuildHTLC with Timeout=0 fails.
// The implementation requires Timeout > 0.
func TestHTLCParamsZeroTimeout(t *testing.T) {
	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerAddr := bytes.Repeat([]byte{0x11}, 20)
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)

	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	_, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPub,
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       1000,
		Timeout:      0,
	})
	assert.ErrorIs(t, err, x402.ErrHTLCBuildFailed,
		"zero timeout should fail with ErrHTLCBuildFailed")
}

// TestHTLCScriptContainsBuyerPubKey verifies the HTLC script embeds the buyer's pubkey.
func TestHTLCScriptContainsBuyerPubKey(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	buyerKey, err := w.DeriveNodeKey(0, []uint32{10}, nil)
	require.NoError(t, err)

	sellerAddr := bytes.Repeat([]byte{0x22}, 20)
	capsuleHash := bytes.Repeat([]byte{0x33}, 32)
	buyerPub := buyerKey.PublicKey.Compressed()

	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPub,
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       5000,
		Timeout:      x402.DefaultHTLCTimeout,
	})
	require.NoError(t, err)
	assert.True(t, bytes.Contains(htlcScript, buyerPub),
		"HTLC script should contain buyer's compressed pubkey")
}

// TestHTLCScriptContainsSellerAddr verifies the HTLC script embeds the seller address hash.
func TestHTLCScriptContainsSellerAddr(t *testing.T) {
	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerAddr := bytes.Repeat([]byte{0x44}, 20)
	capsuleHash := bytes.Repeat([]byte{0x55}, 32)

	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPub,
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       2000,
		Timeout:      x402.DefaultHTLCTimeout,
	})
	require.NoError(t, err)
	assert.True(t, bytes.Contains(htlcScript, sellerAddr),
		"HTLC script should contain seller address")
}

// TestHTLCScriptContainsCapsuleHash verifies the HTLC script embeds the capsule hash.
func TestHTLCScriptContainsCapsuleHash(t *testing.T) {
	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerAddr := bytes.Repeat([]byte{0x66}, 20)
	capsuleHash := bytes.Repeat([]byte{0x77}, 32)

	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPub,
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       3000,
		Timeout:      x402.DefaultHTLCTimeout,
	})
	require.NoError(t, err)
	assert.True(t, bytes.Contains(htlcScript, capsuleHash),
		"HTLC script should contain capsule hash")
}

// TestCapsuleHashDeterminism verifies ComputeCapsuleHash returns the same result for the same input.
func TestCapsuleHashDeterminism(t *testing.T) {
	fileTxID := bytes.Repeat([]byte{0xf0}, 32) // mock file txid
	capsule := bytes.Repeat([]byte{0x42}, 32)

	hash1 := method42.ComputeCapsuleHash(fileTxID, capsule)
	hash2 := method42.ComputeCapsuleHash(fileTxID, capsule)

	assert.Equal(t, hash1, hash2,
		"ComputeCapsuleHash should be deterministic for the same capsule input")
	assert.Len(t, hash1, 32, "capsule hash should be 32 bytes")
}

// --- Daemon Session Tests ---

// TestDaemonSessionCreation verifies session creation through the daemon.
func TestDaemonSessionCreation(t *testing.T) {
	d, _ := createTestDaemon(t)

	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	sharedX := bytes.Repeat([]byte{0xaa}, 32)
	nonceB := bytes.Repeat([]byte{0xbb}, 32)
	nonceS := bytes.Repeat([]byte{0xcc}, 32)

	session := d.CreateSession(buyerPub, sellerPub, sharedX, nonceB, nonceS, 24*time.Hour)
	require.NotNil(t, session)
	assert.NotEmpty(t, session.ID, "session ID should not be empty")
	assert.False(t, session.IsExpired(), "newly created session should not be expired")
	assert.Equal(t, buyerPub, session.BuyerPub)
	assert.Equal(t, sellerPub, session.SellerPub)
}

// TestDaemonSessionRetrieval verifies that a created session can be retrieved by ID.
func TestDaemonSessionRetrieval(t *testing.T) {
	d, _ := createTestDaemon(t)

	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	sharedX := bytes.Repeat([]byte{0x11}, 32)
	nonceB := bytes.Repeat([]byte{0x22}, 32)
	nonceS := bytes.Repeat([]byte{0x33}, 32)

	created := d.CreateSession(buyerPub, sellerPub, sharedX, nonceB, nonceS, 24*time.Hour)
	require.NotNil(t, created)

	retrieved, err := d.GetSession(created.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, created.BuyerPub, retrieved.BuyerPub)
	assert.Equal(t, created.SellerPub, retrieved.SellerPub)
	assert.Equal(t, created.SessionKey, retrieved.SessionKey)
}

// TestDaemonSessionNotFound verifies GetSession with an unknown ID returns ErrSessionNotFound.
func TestDaemonSessionNotFound(t *testing.T) {
	d, _ := createTestDaemon(t)

	_, err := d.GetSession("nonexistent-session-id-12345")
	assert.ErrorIs(t, err, daemon.ErrSessionNotFound)
}

// TestDaemonSessionExpired verifies that a session with ExpiresAt in the past is considered expired.
func TestDaemonSessionExpired(t *testing.T) {
	d, _ := createTestDaemon(t)

	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	sharedX := bytes.Repeat([]byte{0xff}, 32)
	nonceB := bytes.Repeat([]byte{0xee}, 32)
	nonceS := bytes.Repeat([]byte{0xdd}, 32)

	// Create with a very short TTL so it expires almost immediately.
	session := d.CreateSession(buyerPub, sellerPub, sharedX, nonceB, nonceS, 1*time.Millisecond)
	require.NotNil(t, session)

	// Wait for expiry.
	time.Sleep(50 * time.Millisecond)

	assert.True(t, session.IsExpired(), "session should be expired after TTL")

	// GetSession should also detect expiry and return ErrSessionExpired.
	_, err := d.GetSession(session.ID)
	assert.ErrorIs(t, err, daemon.ErrSessionExpired)
}

// TestDaemonHandshakeNilBuyerPub verifies that handshake POST with empty buyer_pub returns an error.
func TestDaemonHandshakeNilBuyerPub(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Send handshake request with empty buyer_pub.
	reqBody := `{"buyer_pub":"","nonce_b":"aabbccdd","timestamp":1234567890}`
	resp, err := http.Post(ts.URL+"/_bitfs/handshake", "application/json", bytes.NewBufferString(reqBody))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return a 400 Bad Request.
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"empty buyer_pub should produce a 400 error")
}

// TestDaemonHandshakeShortBuyerPub verifies that a short buyer pubkey (not 33 bytes) fails.
func TestDaemonHandshakeShortBuyerPub(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 10 bytes hex-encoded = 20 hex chars.
	shortPubHex := "02030405060708091011"
	nonceHex := "aabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd"
	reqBody := fmt.Sprintf(`{"buyer_pub":"%s","nonce_b":"%s","timestamp":1234567890}`, shortPubHex, nonceHex)

	resp, err := http.Post(ts.URL+"/_bitfs/handshake", "application/json", bytes.NewBufferString(reqBody))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"short buyer pubkey should produce a 400 error")
}

// --- Daemon HTTP Tests ---

// TestDaemon404ForMissingContent verifies 404 for nonexistent paths.
func TestDaemon404ForMissingContent(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent/path/to/file")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode,
		"request to nonexistent path should return 404")
}

// TestDaemonMethodNotAllowed verifies that POST to a GET-only endpoint fails.
func TestDaemonMethodNotAllowed(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/_bitfs/health", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Go 1.22+ ServeMux returns 405 for method mismatches on explicit method patterns.
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode,
		"POST to GET-only /_bitfs/health should return 405")
}

// TestDaemonMultipleHealthChecks verifies that repeated health checks all succeed.
func TestDaemonMultipleHealthChecks(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	for i := 0; i < 10; i++ {
		resp, err := http.Get(ts.URL + "/_bitfs/health")
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, resp.StatusCode, "health check %d should return 200", i)
		assert.Contains(t, string(body), `"status":"ok"`, "health check %d should contain ok status", i)
	}
}

// TestDaemonConfigDefaults verifies DefaultConfig has sensible non-empty defaults.
func TestDaemonConfigDefaults(t *testing.T) {
	config := daemon.DefaultConfig()
	require.NotNil(t, config)

	assert.NotEmpty(t, config.ListenAddr, "default ListenAddr should not be empty")
	assert.Equal(t, ":8080", config.ListenAddr)
	assert.False(t, config.TLS.Enabled, "TLS should be disabled by default")
	assert.Greater(t, config.Security.RateLimit.RPM, 0, "rate limit RPM should be positive")
	assert.Greater(t, config.Security.RateLimit.Burst, 0, "rate limit burst should be positive")
	assert.NotEmpty(t, config.Storage.DataDir, "storage data dir should not be empty")
	assert.NotEmpty(t, config.Log.Level, "log level should not be empty")
}

// TestDaemonNewNilConfig verifies daemon.New with nil config returns ErrNilConfig.
func TestDaemonNewNilConfig(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	walletSvc := &mockWalletService{
		privKey: nodeKey.PrivateKey,
		pubKey:  nodeKey.PublicKey,
	}
	store := newMockContentStore()
	metanetSvc := newMockMetanetService()

	_, err = daemon.New(nil, walletSvc, store, metanetSvc)
	assert.ErrorIs(t, err, daemon.ErrNilConfig)
}

// --- x402 Price Calculation Boundary Tests ---

// TestX402PriceCalculationBoundary tests edge cases in price calculation.
func TestX402PriceCalculationBoundary(t *testing.T) {
	tests := []struct {
		name       string
		pricePerKB uint64
		fileSize   uint64
		expected   uint64
	}{
		{"1 sat/KB, 1 byte", 1, 1, 1},
		{"1 sat/KB, 1023 bytes", 1, 1023, 1},
		{"1 sat/KB, 1024 bytes (exact 1KB)", 1, 1024, 1},
		{"1 sat/KB, 1025 bytes (just over 1KB)", 1, 1025, 2},
		{"both zero", 0, 0, 0},
		{"price zero, big file", 0, 1048576, 0},
		{"big price, zero file", 999999, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			price := x402.CalculatePrice(tc.pricePerKB, tc.fileSize)
			assert.Equal(t, tc.expected, price)
		})
	}

	// Overflow edge case: MaxUint64/1024 * 1024 should not overflow.
	t.Run("near-max values", func(t *testing.T) {
		// Use pricePerKB that won't overflow when multiplied by fileSize.
		var pricePerKB uint64 = math.MaxUint64 / 1024
		var fileSize uint64 = 1024

		// Should not panic.
		price := x402.CalculatePrice(pricePerKB, fileSize)
		assert.Greater(t, price, uint64(0), "price should be positive for non-zero inputs")
	})
}

// --- x402 Payment Headers Round-Trip ---

// TestX402PaymentHeadersRoundTrip verifies full header set/parse cycle.
func TestX402PaymentHeadersRoundTrip(t *testing.T) {
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)
	invoice := x402.NewInvoice(250, 8192, "1RoundTripAddr", capsuleHash, 3600)
	require.NotNil(t, invoice)

	// Set headers on response.
	recorder := httptest.NewRecorder()
	headers := x402.PaymentHeadersFromInvoice(invoice)
	x402.SetPaymentHeaders(recorder, headers)

	// Parse headers from response.
	resp := recorder.Result()
	assert.Equal(t, http.StatusPaymentRequired, resp.StatusCode)

	parsed, err := x402.ParsePaymentHeaders(resp)
	require.NoError(t, err)

	assert.Equal(t, invoice.Price, parsed.Price)
	assert.Equal(t, invoice.PricePerKB, parsed.PricePerKB)
	assert.Equal(t, invoice.FileSize, parsed.FileSize)
	assert.Equal(t, invoice.ID, parsed.InvoiceID)
	assert.Equal(t, invoice.Expiry, parsed.Expiry)
}

// TestX402ParsePaymentHeadersMissing verifies ParsePaymentHeaders fails when headers are absent.
func TestX402ParsePaymentHeadersMissing(t *testing.T) {
	// Create a response with no x402 headers.
	recorder := httptest.NewRecorder()
	recorder.WriteHeader(http.StatusOK)
	resp := recorder.Result()

	_, err := x402.ParsePaymentHeaders(resp)
	assert.ErrorIs(t, err, x402.ErrMissingHeaders,
		"missing payment headers should return ErrMissingHeaders")
}

// --- End-to-End Flow Tests ---

// TestEndToEndFreeContentFlow verifies that free content can be encrypted and
// decrypted by anyone without payment.
func TestEndToEndFreeContentFlow(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := []byte("This is free content available to all users")

	// Encrypt with AccessFree (nil private key uses scalar 1).
	encResult, err := method42.Encrypt(plaintext, nil, nodeKey.PublicKey, method42.AccessFree)
	require.NoError(t, err)

	// Create invoice with price=0 for free content.
	capsuleHash := bytes.Repeat([]byte{0x00}, 32)
	invoice := x402.NewInvoice(0, uint64(len(plaintext)), "1FreeAddr", capsuleHash, 3600)
	assert.Equal(t, uint64(0), invoice.Price, "free content should have zero price")

	// Any party can decrypt AccessFree content using only the public key.
	decResult, err := method42.Decrypt(encResult.Ciphertext, nil, nodeKey.PublicKey, encResult.KeyHash, method42.AccessFree)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext, "free content should be decryptable by anyone")
}

// TestEndToEndEncryptPayDecrypt simulates the full payment cycle:
// encrypt (Private) -> create invoice -> build HTLC -> reveal capsule -> decrypt.
func TestEndToEndEncryptPayDecrypt(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	// Seller's node key.
	sellerKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	// Step 1: Encrypt content with AccessPrivate.
	plaintext := []byte("Premium paid content requiring HTLC atomic swap")
	encResult, err := method42.Encrypt(plaintext, sellerKey.PrivateKey, sellerKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// Step 2: Buyer keypair.
	buyerKey, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	// Step 3: Compute capsule and capsule_hash (seller side).
	capsule, err := method42.ComputeCapsule(sellerKey.PrivateKey, sellerKey.PublicKey, buyerKey.PublicKey, encResult.KeyHash)
	require.NoError(t, err)
	fileTxID := bytes.Repeat([]byte{0xf0}, 32) // mock file txid
	capsuleHash := method42.ComputeCapsuleHash(fileTxID, capsule)

	// Step 4: Create invoice.
	invoice := x402.NewInvoice(50, uint64(len(plaintext)), "1SellerPaidAddr", capsuleHash, 3600)
	assert.Greater(t, invoice.Price, uint64(0), "paid content should have non-zero price")
	assert.False(t, invoice.IsExpired())

	// Step 5: Buyer builds HTLC.
	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerKey.PublicKey.Compressed(),
		SellerPubKey: sellerPub,
		SellerAddr:   bytes.Repeat([]byte{0x11}, 20),
		CapsuleHash:  capsuleHash,
		Amount:       invoice.Price,
		Timeout:      x402.DefaultHTLCTimeout,
	})
	require.NoError(t, err)
	require.NotEmpty(t, htlcScript)

	// Step 6: Seller reveals capsule (simulated). Buyer decrypts with capsule.
	decResult, err := method42.DecryptWithCapsule(encResult.Ciphertext, capsule, encResult.KeyHash, buyerKey.PrivateKey, sellerKey.PublicKey)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext,
		"decrypted content should match original plaintext")
}

// --- HTLC Amount Variations ---

// TestHTLCDifferentAmounts verifies HTLC builds succeed for various valid amounts.
func TestHTLCDifferentAmounts(t *testing.T) {
	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerAddr := bytes.Repeat([]byte{0x11}, 20)
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)

	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	amounts := []uint64{546, 1000, 100000}
	for _, amount := range amounts {
		t.Run(
			fmt.Sprintf("amount_%d", amount),
			func(t *testing.T) {
				htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
					BuyerPubKey:  buyerPub,
					SellerPubKey: sellerPub,
					SellerAddr:   sellerAddr,
					CapsuleHash:  capsuleHash,
					Amount:       amount,
					Timeout:      x402.DefaultHTLCTimeout,
				})
				require.NoError(t, err, "BuildHTLC should succeed for amount %d", amount)
				assert.NotEmpty(t, htlcScript)
			},
		)
	}
}

// TestHTLCAmountBelowDust verifies HTLC builds succeed with small amounts.
// HTLC script construction doesn't enforce the dust limit; that's a transaction-layer concern.
func TestHTLCAmountBelowDust(t *testing.T) {
	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerAddr := bytes.Repeat([]byte{0x11}, 20)
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)

	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	// Amount=1 is below the dust limit (546) but HTLC construction should succeed.
	htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPub,
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       1,
		Timeout:      x402.DefaultHTLCTimeout,
	})
	require.NoError(t, err, "HTLC with below-dust amount should still build successfully")
	assert.NotEmpty(t, htlcScript)
}

// --- Daemon BSVAlias and CORS Tests ---

// TestDaemonBSVAliasCapabilities verifies the .well-known/bsvalias endpoint.
func TestDaemonBSVAliasCapabilities(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/bsvalias")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var caps map[string]interface{}
	err = json.Unmarshal(body, &caps)
	require.NoError(t, err)
	assert.Equal(t, "1.0", caps["bsvalias"],
		"bsvalias version should be 1.0")
	assert.NotNil(t, caps["capabilities"],
		"capabilities should not be nil")
}

// TestDaemonCORSAllowsAllOrigins verifies CORS headers on preflight requests.
func TestDaemonCORSAllowsAllOrigins(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequest("OPTIONS", ts.URL+"/_bitfs/health", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://any-origin.example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Access-Control-Allow-Origin"),
		"CORS Allow-Origin header should be set")
}

// --- Content Negotiation Tests ---

// TestDaemonContentNegotiationAllFormats tests Accept headers for all supported formats.
func TestDaemonContentNegotiationAllFormats(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	tests := []struct {
		accept              string
		expectedContentType string
		bodyContains        string
	}{
		{"text/html", "text/html", "<!DOCTYPE html>"},
		{"application/json", "application/json", "BitFS"},
		{"text/markdown", "text/markdown", "# BitFS"},
		{"*/*", "application/json", "BitFS"}, // default is JSON
	}

	for _, tc := range tests {
		t.Run("accept_"+tc.accept, func(t *testing.T) {
			req, err := http.NewRequest("GET", ts.URL+"/", nil)
			require.NoError(t, err)
			req.Header.Set("Accept", tc.accept)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Contains(t, resp.Header.Get("Content-Type"), tc.expectedContentType,
				"Content-Type should match for Accept: %s", tc.accept)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(body), tc.bodyContains,
				"body should contain expected string for Accept: %s", tc.accept)
		})
	}
}

// --- Invoice Uniqueness Tests ---

// TestMultipleConcurrentInvoices verifies that invoices created concurrently have unique IDs.
func TestMultipleConcurrentInvoices(t *testing.T) {
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)
	var invoices [5]*x402.Invoice

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			invoices[idx] = x402.NewInvoice(
				uint64(100*(idx+1)),
				uint64(1024*(idx+1)),
				"1ConcurrentAddr",
				capsuleHash,
				3600,
			)
		}(i)
	}
	wg.Wait()

	// Collect IDs and verify uniqueness.
	ids := make(map[string]bool)
	for i, inv := range invoices {
		require.NotNil(t, inv, "invoice %d should not be nil", i)
		assert.NotEmpty(t, inv.ID, "invoice %d should have a non-empty ID", i)
		assert.False(t, ids[inv.ID], "invoice %d has duplicate ID: %s", i, inv.ID)
		ids[inv.ID] = true
	}
}

// TestInvoiceIDUniqueness creates 100 invoices and verifies all IDs are unique.
func TestInvoiceIDUniqueness(t *testing.T) {
	capsuleHash := bytes.Repeat([]byte{0xcd}, 32)
	ids := make(map[string]struct{}, 100)

	for i := 0; i < 100; i++ {
		invoice := x402.NewInvoice(100, 1024, "1UniqueAddr", capsuleHash, 3600)
		require.NotNil(t, invoice)
		require.NotEmpty(t, invoice.ID)

		_, exists := ids[invoice.ID]
		assert.False(t, exists, "invoice ID collision at iteration %d: %s", i, invoice.ID)
		ids[invoice.ID] = struct{}{}
	}

	assert.Len(t, ids, 100, "all 100 invoice IDs should be unique")
}

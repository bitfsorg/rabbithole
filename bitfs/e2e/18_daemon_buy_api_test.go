//go:build e2e

package e2e

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
	"github.com/bitfsorg/libbitfs-go/x402"
)

// setupBuyServer creates a daemon httptest.Server configured for buy-API testing.
//
// It registers a paid file node ("/docs/secret.txt") in a mock MetanetService,
// encrypts test content via Method 42, stores ciphertext in a real FileStore,
// and enables x402 payment so that accessing the paid path returns HTTP 402
// with an invoice. Returns the test server, seller wallet, and the node's
// Method 42 encryption result (for capsule/key verification).
func setupBuyServer(t *testing.T) (*httptest.Server, *wallet.Wallet, *method42.EncryptResult) {
	t.Helper()

	// Create HD wallet.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	// Derive a file node key for the paid file.
	nodeKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive node key")

	// Encrypt some content so we have a valid KeyHash and ciphertext.
	plaintext := []byte("This is paid content that requires an HTLC purchase.")
	encResult, err := method42.Encrypt(
		plaintext,
		nodeKey.PrivateKey,
		nodeKey.PublicKey,
		method42.AccessPaid,
	)
	require.NoError(t, err, "method42 encrypt (paid mode)")

	// Store in a real FileStore.
	fileStore, err := storage.NewFileStore(t.TempDir())
	require.NoError(t, err, "create file store")

	err = fileStore.Put(encResult.KeyHash, encResult.Ciphertext)
	require.NoError(t, err, "store ciphertext")

	// Build mock MetanetService with a paid node.
	metanetSvc := &testMetanetService{
		nodes: map[string]*daemon.NodeInfo{
			"/docs/secret.txt": {
				PNode:      nodeKey.PublicKey.Compressed(),
				Type:       "file",
				Access:     "paid",
				PricePerKB: 100,
				FileSize:   1024,
				KeyHash:    encResult.KeyHash,
				MimeType:   "text/plain",
			},
		},
	}

	walletSvc := &testWalletService{w: w}

	config := daemon.DefaultConfig()
	config.Security.RateLimit.RPM = 0 // disable rate limiting for tests
	config.X402.Enabled = true         // enable x402 so paid paths return 402

	d, err := daemon.New(config, walletSvc, fileStore, metanetSvc)
	require.NoError(t, err, "create daemon")

	server := httptest.NewServer(d.Handler())
	t.Cleanup(server.Close)

	return server, w, encResult
}

// createInvoice triggers the paid-content path to produce an x402 invoice.
// It issues GET /docs/secret.txt, expects HTTP 402, extracts the X-Invoice-Id
// header, and returns the invoice ID along with parsed x402 payment headers.
func createInvoice(t *testing.T, serverURL string) (string, *x402.PaymentHeaders) {
	t.Helper()

	req, err := http.NewRequest("GET", serverURL+"/docs/secret.txt", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET paid path to create invoice")
	defer resp.Body.Close()

	require.Equal(t, http.StatusPaymentRequired, resp.StatusCode,
		"paid path should return 402 Payment Required")

	headers, err := x402.ParsePaymentHeaders(resp)
	require.NoError(t, err, "parse x402 headers from 402 response")
	require.NotEmpty(t, headers.InvoiceID, "invoice ID should not be empty")

	return headers.InvoiceID, headers
}

// TestGetBuyInfo verifies the full buy-info retrieval flow:
//
//  1. Access a paid path to trigger invoice creation (HTTP 402).
//  2. GET /_bitfs/buy/{invoiceID} returns 200 with capsule_hash, price, payment_addr.
//  3. Verify all fields match the original 402 response headers.
//  4. GET /_bitfs/buy/{unknown} returns 404 for non-existent invoice.
func TestGetBuyInfo(t *testing.T) {
	server, _, _ := setupBuyServer(t)

	// Step 1: Trigger invoice creation by accessing the paid path.
	invoiceID, payHeaders := createInvoice(t, server.URL)
	t.Logf("invoice created: id=%s, price=%d, pricePerKB=%d, fileSize=%d",
		invoiceID, payHeaders.Price, payHeaders.PricePerKB, payHeaders.FileSize)

	// Step 2: GET /_bitfs/buy/{invoiceID} to retrieve buy info.
	t.Run("get_buy_info_success", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/_bitfs/buy/" + invoiceID)
		require.NoError(t, err, "GET buy info")
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "should return 200")
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var buyInfo map[string]interface{}
		err = json.Unmarshal(body, &buyInfo)
		require.NoError(t, err, "unmarshal buy info response")

		// Verify invoice_id matches.
		assert.Equal(t, invoiceID, buyInfo["invoice_id"],
			"invoice_id should match the one from 402 response")

		// Verify total_price matches the x402 header price.
		totalPrice, ok := buyInfo["total_price"].(float64)
		require.True(t, ok, "total_price should be a number")
		assert.Equal(t, float64(payHeaders.Price), totalPrice,
			"total_price should match x402 header price")

		// Verify price_per_kb.
		pricePerKB, ok := buyInfo["price_per_kb"].(float64)
		require.True(t, ok, "price_per_kb should be a number")
		assert.Equal(t, float64(payHeaders.PricePerKB), pricePerKB,
			"price_per_kb should match")

		// Verify file_size.
		fileSize, ok := buyInfo["file_size"].(float64)
		require.True(t, ok, "file_size should be a number")
		assert.Equal(t, float64(payHeaders.FileSize), fileSize,
			"file_size should match")

		// Verify capsule_hash is a non-empty hex string.
		capsuleHash, ok := buyInfo["capsule_hash"].(string)
		require.True(t, ok, "capsule_hash should be a string")
		assert.NotEmpty(t, capsuleHash, "capsule_hash should not be empty")
		capsuleHashBytes, err := hex.DecodeString(capsuleHash)
		require.NoError(t, err, "capsule_hash should be valid hex")
		assert.Len(t, capsuleHashBytes, 32, "capsule_hash should be 32 bytes (SHA-256)")

		// Verify payment_addr is present.
		paymentAddr, ok := buyInfo["payment_addr"].(string)
		require.True(t, ok, "payment_addr should be a string")
		assert.NotEmpty(t, paymentAddr, "payment_addr should not be empty")

		// Verify paid is false (not yet purchased).
		paid, ok := buyInfo["paid"].(bool)
		require.True(t, ok, "paid should be a boolean")
		assert.False(t, paid, "paid should be false before purchase")

		t.Logf("buy info: capsule_hash=%s..., price=%v, addr=%s, paid=%v",
			capsuleHash[:16], totalPrice, paymentAddr, paid)
	})

	// Step 3: GET /_bitfs/buy/{unknown} for non-existent invoice returns 404.
	t.Run("get_buy_info_not_found", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/_bitfs/buy/nonexistent-invoice-id")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"non-existent invoice should return 404")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "NOT_FOUND",
			"error response should contain NOT_FOUND code")
	})
}

// TestSubmitInvalidHTLC verifies that POST /_bitfs/buy/{invoiceID} rejects
// invalid HTLC transaction submissions with appropriate error codes:
//
//  1. Empty body returns 400 EMPTY_TX.
//  2. Garbage (non-transaction) bytes return 400 PAYMENT_INVALID.
//  3. Non-existent invoice returns 404 NOT_FOUND.
func TestSubmitInvalidHTLC(t *testing.T) {
	server, _, _ := setupBuyServer(t)

	// Create an invoice first.
	invoiceID, _ := createInvoice(t, server.URL)
	t.Logf("invoice created for HTLC tests: id=%s", invoiceID)

	// Sub-test 1: Empty body.
	t.Run("empty_body", func(t *testing.T) {
		resp, err := http.Post(
			server.URL+"/_bitfs/buy/"+invoiceID,
			"application/octet-stream",
			strings.NewReader(""),
		)
		require.NoError(t, err, "POST with empty body")
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"empty HTLC body should return 400")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "EMPTY_TX",
			"error should indicate empty transaction body")
		t.Logf("empty body: HTTP %d, error=%s", resp.StatusCode, string(body))
	})

	// Sub-test 2: Garbage bytes (not a valid BSV transaction).
	t.Run("garbage_body", func(t *testing.T) {
		garbage := strings.Repeat("deadbeef", 16) // 128 hex chars = 64 bytes of garbage
		garbageBytes, err := hex.DecodeString(garbage)
		require.NoError(t, err)

		resp, err := http.Post(
			server.URL+"/_bitfs/buy/"+invoiceID,
			"application/octet-stream",
			strings.NewReader(string(garbageBytes)),
		)
		require.NoError(t, err, "POST with garbage body")
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"garbage HTLC body should return 400")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "PAYMENT_INVALID",
			"error should indicate payment verification failure")
		t.Logf("garbage body: HTTP %d, error=%s", resp.StatusCode, string(body))
	})

	// Sub-test 3: Non-existent invoice ID.
	t.Run("nonexistent_invoice", func(t *testing.T) {
		resp, err := http.Post(
			server.URL+"/_bitfs/buy/fake-invoice-12345",
			"application/octet-stream",
			strings.NewReader("some-tx-bytes"),
		)
		require.NoError(t, err, "POST to non-existent invoice")
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"non-existent invoice should return 404")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "NOT_FOUND",
			"error should indicate invoice not found")
		t.Logf("nonexistent invoice: HTTP %d, error=%s", resp.StatusCode, string(body))
	})
}

// TestBuyFlowX402Headers verifies that the 402 Payment Required response
// from accessing a paid path includes all required x402 HTTP headers and
// that the JSON body contains the expected invoice fields.
func TestBuyFlowX402Headers(t *testing.T) {
	server, _, _ := setupBuyServer(t)

	req, err := http.NewRequest("GET", server.URL+"/docs/secret.txt", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET paid path")
	defer resp.Body.Close()

	// Verify HTTP 402 status.
	require.Equal(t, http.StatusPaymentRequired, resp.StatusCode,
		"paid content should return 402")

	// Verify all x402 headers are present.
	assert.NotEmpty(t, resp.Header.Get("X-Price"), "X-Price header should be set")
	assert.NotEmpty(t, resp.Header.Get("X-Price-Per-KB"), "X-Price-Per-KB header should be set")
	assert.NotEmpty(t, resp.Header.Get("X-File-Size"), "X-File-Size header should be set")
	assert.NotEmpty(t, resp.Header.Get("X-Invoice-Id"), "X-Invoice-Id header should be set")
	assert.NotEmpty(t, resp.Header.Get("X-Expiry"), "X-Expiry header should be set")

	// Verify JSON body has invoice fields.
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var invoiceResp map[string]interface{}
	err = json.Unmarshal(body, &invoiceResp)
	require.NoError(t, err, "unmarshal 402 JSON body")

	assert.Equal(t, "payment required", invoiceResp["error"],
		"error field should be 'payment required'")
	assert.NotEmpty(t, invoiceResp["invoice_id"], "invoice_id should be present")
	assert.NotNil(t, invoiceResp["total_price"], "total_price should be present")
	assert.NotNil(t, invoiceResp["price_per_kb"], "price_per_kb should be present")
	assert.NotNil(t, invoiceResp["file_size"], "file_size should be present")
	assert.NotEmpty(t, invoiceResp["payment_addr"], "payment_addr should be present")

	t.Logf("402 response: invoice_id=%s, price=%v, addr=%s",
		invoiceResp["invoice_id"], invoiceResp["total_price"], invoiceResp["payment_addr"])
}

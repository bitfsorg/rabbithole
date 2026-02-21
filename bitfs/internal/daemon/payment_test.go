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

	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/libbitfs/x402"
)

// testPaymentAddr is a well-known Bitcoin address used in tests.
// This is the genesis block coinbase address.
const testPaymentAddr = "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"

// buildTestPaymentTx creates a serialized BSV transaction paying to the given
// address with the specified amount.
func buildTestPaymentTx(t *testing.T, addr string, satoshis uint64) []byte {
	t.Helper()
	tx := transaction.NewTransaction()
	err := tx.PayToAddress(addr, satoshis)
	require.NoError(t, err)
	return tx.Bytes()
}

// --- servePaidContent Tests ---

func TestServePaidContent_Returns402WithInvoice(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	d.config.X402.Enabled = true

	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i)
	}
	pnode := validPnodeBytes()

	meta.nodes["/premium/video.mp4"] = &NodeInfo{
		Type:       "file",
		MimeType:   "video/mp4",
		FileSize:   10485760,
		Access:     "paid",
		PricePerKB: 50,
		PNode:      pnode,
		KeyHash:    keyHash,
	}

	req := httptest.NewRequest("GET", "/premium/video.mp4", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Verify x402 headers set by libbitfs/x402.SetPaymentHeaders.
	assert.Equal(t, "50", w.Header().Get("X-Price-Per-KB"))
	assert.Equal(t, "10485760", w.Header().Get("X-File-Size"))
	assert.NotEmpty(t, w.Header().Get("X-Price"), "X-Price header should be set by x402")
	assert.NotEmpty(t, w.Header().Get("X-Invoice-Id"), "X-Invoice-Id header should be set by x402")
	assert.NotEmpty(t, w.Header().Get("X-Expiry"), "X-Expiry header should be set by x402")

	// Verify total price: ceil(50 * 10485760 / 1024) = 50 * 10240 = 512000
	expectedTotal := x402.CalculatePrice(50, 10485760)
	assert.Equal(t, fmt.Sprintf("%d", expectedTotal), w.Header().Get("X-Price"))

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "payment required", resp["error"])
	assert.NotEmpty(t, resp["invoice_id"])
	assert.Equal(t, float64(50), resp["price_per_kb"])
	assert.Equal(t, float64(10485760), resp["file_size"])
	assert.NotEmpty(t, resp["payment_addr"])
	assert.Equal(t, float64(expectedTotal), resp["total_price"])

	// Verify invoice was stored with TotalPrice.
	invoiceID := resp["invoice_id"].(string)
	d.invoicesMu.RLock()
	invoice, exists := d.invoices[invoiceID]
	d.invoicesMu.RUnlock()
	assert.True(t, exists)
	assert.Equal(t, invoiceID, invoice.ID)
	assert.Equal(t, uint64(50), invoice.PricePerKB)
	assert.Equal(t, uint64(10485760), invoice.FileSize)
	assert.Equal(t, expectedTotal, invoice.TotalPrice)
	assert.False(t, invoice.Paid)
}

func TestServePaidContent_InvoiceExpiry(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	d.config.X402.Enabled = true
	d.config.X402.InvoiceExpiry = 7200 // 2 hours

	meta.nodes["/premium/expiry.dat"] = &NodeInfo{
		Type:       "file",
		FileSize:   512,
		Access:     "paid",
		PricePerKB: 25,
		PNode:      validPnodeBytes(),
		KeyHash:    make([]byte, 32),
	}

	req := httptest.NewRequest("GET", "/premium/expiry.dat", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	invoiceID := resp["invoice_id"].(string)

	d.invoicesMu.RLock()
	invoice := d.invoices[invoiceID]
	d.invoicesMu.RUnlock()

	// Invoice should expire in approximately 2 hours.
	expectedExpiry := time.Now().Add(2 * time.Hour)
	assert.WithinDuration(t, expectedExpiry, invoice.Expiry, 5*time.Second)
}

func TestServePaidContent_TotalPriceCalculation(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	d.config.X402.Enabled = true

	meta.nodes["/premium/priced.dat"] = &NodeInfo{
		Type:       "file",
		FileSize:   2048,
		Access:     "paid",
		PricePerKB: 100,
		PNode:      validPnodeBytes(),
		KeyHash:    make([]byte, 32),
	}

	req := httptest.NewRequest("GET", "/premium/priced.dat", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)

	// Verify total price: ceil(100 * 2048 / 1024) = 200
	expectedTotal := x402.CalculatePrice(100, 2048)
	assert.Equal(t, uint64(200), expectedTotal)
	assert.Equal(t, fmt.Sprintf("%d", expectedTotal), w.Header().Get("X-Price"))

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, float64(200), resp["total_price"])
}

func TestServePaidContent_X402HeadersComplete(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	d.config.X402.Enabled = true

	meta.nodes["/premium/headers.dat"] = &NodeInfo{
		Type:       "file",
		FileSize:   4096,
		Access:     "paid",
		PricePerKB: 50,
		PNode:      validPnodeBytes(),
		KeyHash:    make([]byte, 32),
	}

	req := httptest.NewRequest("GET", "/premium/headers.dat", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)

	// All five x402 headers should be present.
	assert.NotEmpty(t, w.Header().Get("X-Price"), "X-Price should be set")
	assert.NotEmpty(t, w.Header().Get("X-Price-Per-KB"), "X-Price-Per-KB should be set")
	assert.NotEmpty(t, w.Header().Get("X-File-Size"), "X-File-Size should be set")
	assert.NotEmpty(t, w.Header().Get("X-Invoice-Id"), "X-Invoice-Id should be set")
	assert.NotEmpty(t, w.Header().Get("X-Expiry"), "X-Expiry should be set")

	// Verify the invoice ID in the header matches the body.
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, w.Header().Get("X-Invoice-Id"), resp["invoice_id"])
}

// --- handleGetBuyInfo Tests ---

func TestHandleGetBuyInfo_Success(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Create an invoice directly.
	totalPrice := x402.CalculatePrice(100, 4096)
	invoice := &InvoiceRecord{
		ID:          "test-invoice-001",
		TotalPrice:  totalPrice,
		PricePerKB:  100,
		FileSize:    4096,
		PaymentAddr: "1BitFStest",
		CapsuleHash: strings.Repeat("ab", 32),
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["test-invoice-001"] = invoice
	d.invoicesMu.Unlock()

	req := httptest.NewRequest("GET", "/_bitfs/buy/test-invoice-001", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "test-invoice-001", resp["invoice_id"])
	assert.Equal(t, float64(totalPrice), resp["total_price"])
	assert.Equal(t, strings.Repeat("ab", 32), resp["capsule_hash"])
	assert.Equal(t, float64(100), resp["price_per_kb"])
	assert.Equal(t, float64(4096), resp["file_size"])
	assert.Equal(t, "1BitFStest", resp["payment_addr"])
	assert.Equal(t, false, resp["paid"])
}

func TestHandleGetBuyInfo_NotFound(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("GET", "/_bitfs/buy/nonexistent-invoice", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NOT_FOUND")
}

func TestHandleGetBuyInfo_Expired(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Create an expired invoice.
	invoice := &InvoiceRecord{
		ID:          "expired-invoice",
		TotalPrice:  x402.CalculatePrice(50, 1024),
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: "1BitFSexpired",
		CapsuleHash: strings.Repeat("cc", 32),
		Expiry:      time.Now().Add(-time.Hour), // expired 1 hour ago
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["expired-invoice"] = invoice
	d.invoicesMu.Unlock()

	req := httptest.NewRequest("GET", "/_bitfs/buy/expired-invoice", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "EXPIRED")

	// Verify the expired invoice was cleaned up.
	d.invoicesMu.RLock()
	_, exists := d.invoices["expired-invoice"]
	d.invoicesMu.RUnlock()
	assert.False(t, exists)
}

// --- handleSubmitHTLC Tests ---

func TestHandleSubmitHTLC_Success(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)

	// Put some encrypted content in the store.
	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i + 0x10)
	}
	keyHashHex := hex.EncodeToString(keyHash)
	encryptedContent := []byte("encrypted-capsule-data-here")
	store.Put(keyHashHex, encryptedContent)

	totalPrice := x402.CalculatePrice(75, 2048)

	// Create an invoice with a real BSV address for x402 verification.
	invoice := &InvoiceRecord{
		ID:          "htlc-invoice-001",
		TotalPrice:  totalPrice,
		KeyHash:     keyHash,
		PricePerKB:  75,
		FileSize:    2048,
		PaymentAddr: testPaymentAddr,
		CapsuleHash: strings.Repeat("dd", 32),
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["htlc-invoice-001"] = invoice
	d.invoicesMu.Unlock()

	// Build a valid BSV transaction paying to the invoice address.
	htlcTx := buildTestPaymentTx(t, testPaymentAddr, totalPrice)
	req := httptest.NewRequest("POST", "/_bitfs/buy/htlc-invoice-001", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "htlc-invoice-001", resp["invoice_id"])
	assert.Equal(t, hex.EncodeToString(encryptedContent), resp["capsule"])
	assert.Equal(t, true, resp["paid"])

	// Verify the invoice is now marked as paid.
	d.invoicesMu.RLock()
	assert.True(t, d.invoices["htlc-invoice-001"].Paid)
	d.invoicesMu.RUnlock()
}

func TestHandleSubmitHTLC_NotFound(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	htlcTx := []byte("raw-htlc-transaction-bytes")
	req := httptest.NewRequest("POST", "/_bitfs/buy/nonexistent-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NOT_FOUND")
}

func TestHandleSubmitHTLC_AlreadyPaid(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)

	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i + 0x20)
	}
	store.Put(hex.EncodeToString(keyHash), []byte("data"))

	// Create an invoice that is already paid.
	invoice := &InvoiceRecord{
		ID:          "paid-invoice",
		TotalPrice:  x402.CalculatePrice(50, 1024),
		KeyHash:     keyHash,
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: testPaymentAddr,
		CapsuleHash: strings.Repeat("ee", 32),
		Expiry:      time.Now().Add(time.Hour),
		Paid:        true, // already paid
	}
	d.invoicesMu.Lock()
	d.invoices["paid-invoice"] = invoice
	d.invoicesMu.Unlock()

	htlcTx := buildTestPaymentTx(t, testPaymentAddr, 1000)
	req := httptest.NewRequest("POST", "/_bitfs/buy/paid-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "ALREADY_PAID")
}

func TestHandleSubmitHTLC_Expired(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Create an expired invoice.
	invoice := &InvoiceRecord{
		ID:          "expired-htlc",
		TotalPrice:  x402.CalculatePrice(50, 1024),
		KeyHash:     make([]byte, 32),
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: testPaymentAddr,
		Expiry:      time.Now().Add(-time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["expired-htlc"] = invoice
	d.invoicesMu.Unlock()

	htlcTx := buildTestPaymentTx(t, testPaymentAddr, 1000)
	req := httptest.NewRequest("POST", "/_bitfs/buy/expired-htlc", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "EXPIRED")
}

func TestHandleSubmitHTLC_EmptyBody(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	invoice := &InvoiceRecord{
		ID:          "empty-body-invoice",
		TotalPrice:  x402.CalculatePrice(50, 1024),
		KeyHash:     make([]byte, 32),
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: testPaymentAddr,
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["empty-body-invoice"] = invoice
	d.invoicesMu.Unlock()

	req := httptest.NewRequest("POST", "/_bitfs/buy/empty-body-invoice", bytes.NewReader([]byte{}))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "EMPTY_TX")
}

func TestHandleSubmitHTLC_ContentNotInStore(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Create invoice with a key hash that doesn't exist in the store.
	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i + 0x30)
	}

	totalPrice := x402.CalculatePrice(50, 1024)
	invoice := &InvoiceRecord{
		ID:          "no-content-invoice",
		TotalPrice:  totalPrice,
		KeyHash:     keyHash,
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: testPaymentAddr,
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["no-content-invoice"] = invoice
	d.invoicesMu.Unlock()

	htlcTx := buildTestPaymentTx(t, testPaymentAddr, totalPrice)
	req := httptest.NewRequest("POST", "/_bitfs/buy/no-content-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "CONTENT_NOT_FOUND")
}

func TestHandleSubmitHTLC_NoKeyHash(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	totalPrice := x402.CalculatePrice(50, 1024)
	// Create invoice with no key hash.
	invoice := &InvoiceRecord{
		ID:          "no-keyhash-invoice",
		TotalPrice:  totalPrice,
		KeyHash:     nil, // empty key hash
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: testPaymentAddr,
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["no-keyhash-invoice"] = invoice
	d.invoicesMu.Unlock()

	htlcTx := buildTestPaymentTx(t, testPaymentAddr, totalPrice)
	req := httptest.NewRequest("POST", "/_bitfs/buy/no-keyhash-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NO_CONTENT")
}

func TestHandleSubmitHTLC_StorageError(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)

	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i + 0x40)
	}

	totalPrice := x402.CalculatePrice(50, 1024)
	invoice := &InvoiceRecord{
		ID:          "storage-err-invoice",
		TotalPrice:  totalPrice,
		KeyHash:     keyHash,
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: testPaymentAddr,
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["storage-err-invoice"] = invoice
	d.invoicesMu.Unlock()

	// Inject storage error.
	store.err = fmt.Errorf("disk failure")

	htlcTx := buildTestPaymentTx(t, testPaymentAddr, totalPrice)
	req := httptest.NewRequest("POST", "/_bitfs/buy/storage-err-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "STORAGE_ERROR")
}

// --- x402 VerifyPayment Integration Tests ---

func TestHandleSubmitHTLC_InvalidTxBytes(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	totalPrice := x402.CalculatePrice(50, 1024)
	invoice := &InvoiceRecord{
		ID:          "invalid-tx-invoice",
		TotalPrice:  totalPrice,
		KeyHash:     make([]byte, 32),
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: testPaymentAddr,
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["invalid-tx-invoice"] = invoice
	d.invoicesMu.Unlock()

	// Send garbage bytes that cannot be deserialized as a BSV transaction.
	req := httptest.NewRequest("POST", "/_bitfs/buy/invalid-tx-invoice", bytes.NewReader([]byte("not-a-valid-bsv-transaction")))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "PAYMENT_INVALID")
}

func TestHandleSubmitHTLC_InsufficientPayment(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)

	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i + 0x60)
	}
	store.Put(hex.EncodeToString(keyHash), []byte("some-data"))

	totalPrice := x402.CalculatePrice(100, 4096) // = 400

	invoice := &InvoiceRecord{
		ID:          "insufficient-invoice",
		TotalPrice:  totalPrice,
		KeyHash:     keyHash,
		PricePerKB:  100,
		FileSize:    4096,
		PaymentAddr: testPaymentAddr,
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["insufficient-invoice"] = invoice
	d.invoicesMu.Unlock()

	// Build a transaction that pays less than required.
	htlcTx := buildTestPaymentTx(t, testPaymentAddr, totalPrice-1)
	req := httptest.NewRequest("POST", "/_bitfs/buy/insufficient-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "PAYMENT_INVALID")
}

func TestHandleSubmitHTLC_WrongAddress(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)

	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i + 0x70)
	}
	store.Put(hex.EncodeToString(keyHash), []byte("data"))

	totalPrice := x402.CalculatePrice(50, 1024)

	invoice := &InvoiceRecord{
		ID:          "wrong-addr-invoice",
		TotalPrice:  totalPrice,
		KeyHash:     keyHash,
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: testPaymentAddr,
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["wrong-addr-invoice"] = invoice
	d.invoicesMu.Unlock()

	// Build a transaction paying to a DIFFERENT address.
	htlcTx := buildTestPaymentTx(t, "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2", totalPrice)
	req := httptest.NewRequest("POST", "/_bitfs/buy/wrong-addr-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "PAYMENT_INVALID")
}

// --- Integration: Full Purchase Flow ---

func TestFullPurchaseFlow(t *testing.T) {
	d, _, store, meta := newTestDaemon(t)
	d.config.X402.Enabled = true

	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i + 0x50)
	}
	keyHashHex := hex.EncodeToString(keyHash)
	encryptedContent := []byte("super-secret-encrypted-file-content")
	store.Put(keyHashHex, encryptedContent)

	meta.nodes["/premium/secret.dat"] = &NodeInfo{
		Type:       "file",
		MimeType:   "application/octet-stream",
		FileSize:   uint64(len(encryptedContent)),
		Access:     "paid",
		PricePerKB: 200,
		PNode:      validPnodeBytes(),
		KeyHash:    keyHash,
	}

	// Step 1: Request the paid content, get a 402 with invoice.
	req1 := httptest.NewRequest("GET", "/premium/secret.dat", nil)
	w1 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w1, req1)

	assert.Equal(t, http.StatusPaymentRequired, w1.Code)
	// Verify x402 headers are set.
	assert.NotEmpty(t, w1.Header().Get("X-Price"))
	assert.NotEmpty(t, w1.Header().Get("X-Invoice-Id"))

	var invoiceResp map[string]interface{}
	err := json.Unmarshal(w1.Body.Bytes(), &invoiceResp)
	require.NoError(t, err)

	invoiceID := invoiceResp["invoice_id"].(string)
	assert.NotEmpty(t, invoiceID)
	assert.NotZero(t, invoiceResp["total_price"])

	// Step 2: GET buy info for the invoice.
	req2 := httptest.NewRequest("GET", "/_bitfs/buy/"+invoiceID, nil)
	w2 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)

	var buyInfo map[string]interface{}
	err = json.Unmarshal(w2.Body.Bytes(), &buyInfo)
	require.NoError(t, err)
	assert.Equal(t, invoiceID, buyInfo["invoice_id"])
	assert.Equal(t, float64(200), buyInfo["price_per_kb"])
	assert.NotZero(t, buyInfo["total_price"])
	assert.Equal(t, false, buyInfo["paid"])

	// Read the stored invoice to get total price and payment address for the tx.
	d.invoicesMu.RLock()
	storedInvoice := d.invoices[invoiceID]
	d.invoicesMu.RUnlock()
	require.NotNil(t, storedInvoice)

	// The placeholder payment address starts with "1BitFS" which is not a valid
	// Base58Check address. For the full flow test, we override it with a real
	// address so that x402.VerifyPayment can parse it.
	d.invoicesMu.Lock()
	storedInvoice.PaymentAddr = testPaymentAddr
	d.invoicesMu.Unlock()

	// Step 3: Submit HTLC payment with a valid BSV transaction.
	htlcTx := buildTestPaymentTx(t, testPaymentAddr, storedInvoice.TotalPrice)
	req3 := httptest.NewRequest("POST", "/_bitfs/buy/"+invoiceID, bytes.NewReader(htlcTx))
	w3 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w3, req3)

	assert.Equal(t, http.StatusOK, w3.Code)

	var capsuleResp map[string]interface{}
	err = json.Unmarshal(w3.Body.Bytes(), &capsuleResp)
	require.NoError(t, err)
	assert.Equal(t, invoiceID, capsuleResp["invoice_id"])
	assert.Equal(t, hex.EncodeToString(encryptedContent), capsuleResp["capsule"])
	assert.Equal(t, true, capsuleResp["paid"])

	// Step 4: Try to pay again, should fail with ALREADY_PAID.
	req4 := httptest.NewRequest("POST", "/_bitfs/buy/"+invoiceID, bytes.NewReader(htlcTx))
	w4 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w4, req4)

	assert.Equal(t, http.StatusConflict, w4.Code)
	assert.Contains(t, w4.Body.String(), "ALREADY_PAID")
}

// --- CORS Preflight for Buy Endpoint ---

func TestBuyEndpoint_OptionsPreflight(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	req := httptest.NewRequest("OPTIONS", "/_bitfs/buy/some-invoice", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

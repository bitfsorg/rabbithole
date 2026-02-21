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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.Equal(t, "50", w.Header().Get("X-Price-Per-KB"))
	assert.Equal(t, "10485760", w.Header().Get("X-File-Size"))

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "payment required", resp["error"])
	assert.NotEmpty(t, resp["invoice_id"])
	assert.Equal(t, float64(50), resp["price_per_kb"])
	assert.Equal(t, float64(10485760), resp["file_size"])
	assert.NotEmpty(t, resp["payment_addr"])

	// Verify invoice was stored
	invoiceID := resp["invoice_id"].(string)
	d.invoicesMu.RLock()
	invoice, exists := d.invoices[invoiceID]
	d.invoicesMu.RUnlock()
	assert.True(t, exists)
	assert.Equal(t, invoiceID, invoice.ID)
	assert.Equal(t, uint64(50), invoice.PricePerKB)
	assert.Equal(t, uint64(10485760), invoice.FileSize)
	assert.False(t, invoice.Paid)
}

func TestServePaidContent_RandReadFailure(t *testing.T) {
	d, _, _, meta := newTestDaemon(t)
	d.config.X402.Enabled = true

	meta.nodes["/premium/fail.mp4"] = &NodeInfo{
		Type:       "file",
		FileSize:   1024,
		Access:     "paid",
		PricePerKB: 10,
	}

	// Inject randRead failure
	orig := randRead
	t.Cleanup(func() { randRead = orig })
	randRead = func(b []byte) (int, error) {
		return 0, fmt.Errorf("simulated entropy failure")
	}

	req := httptest.NewRequest("GET", "/premium/fail.mp4", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "INTERNAL_ERROR")
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

	// Invoice should expire in approximately 2 hours
	expectedExpiry := time.Now().Add(2 * time.Hour)
	assert.WithinDuration(t, expectedExpiry, invoice.Expiry, 5*time.Second)
}

// --- handleGetBuyInfo Tests ---

func TestHandleGetBuyInfo_Success(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Create an invoice directly
	invoice := &InvoiceRecord{
		ID:          "test-invoice-001",
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

	// Create an expired invoice
	invoice := &InvoiceRecord{
		ID:          "expired-invoice",
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

	// Verify the expired invoice was cleaned up
	d.invoicesMu.RLock()
	_, exists := d.invoices["expired-invoice"]
	d.invoicesMu.RUnlock()
	assert.False(t, exists)
}

// --- handleSubmitHTLC Tests ---

func TestHandleSubmitHTLC_Success(t *testing.T) {
	d, _, store, _ := newTestDaemon(t)

	// Put some encrypted content in the store
	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i + 0x10)
	}
	keyHashHex := hex.EncodeToString(keyHash)
	encryptedContent := []byte("encrypted-capsule-data-here")
	store.Put(keyHashHex, encryptedContent)

	// Create an invoice
	invoice := &InvoiceRecord{
		ID:          "htlc-invoice-001",
		KeyHash:     keyHash,
		PricePerKB:  75,
		FileSize:    2048,
		PaymentAddr: "1BitFShtlc",
		CapsuleHash: strings.Repeat("dd", 32),
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["htlc-invoice-001"] = invoice
	d.invoicesMu.Unlock()

	// Submit an HTLC transaction
	htlcTx := []byte("raw-htlc-transaction-bytes")
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

	// Verify the invoice is now marked as paid
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

	// Create an invoice that is already paid
	invoice := &InvoiceRecord{
		ID:          "paid-invoice",
		KeyHash:     keyHash,
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: "1BitFSpaid",
		CapsuleHash: strings.Repeat("ee", 32),
		Expiry:      time.Now().Add(time.Hour),
		Paid:        true, // already paid
	}
	d.invoicesMu.Lock()
	d.invoices["paid-invoice"] = invoice
	d.invoicesMu.Unlock()

	htlcTx := []byte("second-payment-attempt")
	req := httptest.NewRequest("POST", "/_bitfs/buy/paid-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "ALREADY_PAID")
}

func TestHandleSubmitHTLC_Expired(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Create an expired invoice
	invoice := &InvoiceRecord{
		ID:          "expired-htlc",
		KeyHash:     make([]byte, 32),
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: "1BitFSexpired",
		Expiry:      time.Now().Add(-time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["expired-htlc"] = invoice
	d.invoicesMu.Unlock()

	htlcTx := []byte("htlc-tx-for-expired")
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
		KeyHash:     make([]byte, 32),
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: "1BitFSempty",
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

	// Create invoice with a key hash that doesn't exist in the store
	keyHash := make([]byte, 32)
	for i := range keyHash {
		keyHash[i] = byte(i + 0x30)
	}

	invoice := &InvoiceRecord{
		ID:          "no-content-invoice",
		KeyHash:     keyHash,
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: "1BitFSnodata",
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["no-content-invoice"] = invoice
	d.invoicesMu.Unlock()

	htlcTx := []byte("htlc-tx-bytes")
	req := httptest.NewRequest("POST", "/_bitfs/buy/no-content-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "CONTENT_NOT_FOUND")
}

func TestHandleSubmitHTLC_NoKeyHash(t *testing.T) {
	d, _, _, _ := newTestDaemon(t)

	// Create invoice with no key hash
	invoice := &InvoiceRecord{
		ID:          "no-keyhash-invoice",
		KeyHash:     nil, // empty key hash
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: "1BitFSnohash",
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["no-keyhash-invoice"] = invoice
	d.invoicesMu.Unlock()

	htlcTx := []byte("htlc-tx-bytes")
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

	invoice := &InvoiceRecord{
		ID:          "storage-err-invoice",
		KeyHash:     keyHash,
		PricePerKB:  50,
		FileSize:    1024,
		PaymentAddr: "1BitFSerr",
		Expiry:      time.Now().Add(time.Hour),
		Paid:        false,
	}
	d.invoicesMu.Lock()
	d.invoices["storage-err-invoice"] = invoice
	d.invoicesMu.Unlock()

	// Inject storage error
	store.err = fmt.Errorf("disk failure")

	htlcTx := []byte("htlc-tx-bytes")
	req := httptest.NewRequest("POST", "/_bitfs/buy/storage-err-invoice", bytes.NewReader(htlcTx))
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "STORAGE_ERROR")
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

	// Step 1: Request the paid content, get a 402 with invoice
	req1 := httptest.NewRequest("GET", "/premium/secret.dat", nil)
	w1 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w1, req1)

	assert.Equal(t, http.StatusPaymentRequired, w1.Code)

	var invoiceResp map[string]interface{}
	err := json.Unmarshal(w1.Body.Bytes(), &invoiceResp)
	require.NoError(t, err)

	invoiceID := invoiceResp["invoice_id"].(string)
	assert.NotEmpty(t, invoiceID)

	// Step 2: GET buy info for the invoice
	req2 := httptest.NewRequest("GET", "/_bitfs/buy/"+invoiceID, nil)
	w2 := httptest.NewRecorder()
	d.Handler().ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)

	var buyInfo map[string]interface{}
	err = json.Unmarshal(w2.Body.Bytes(), &buyInfo)
	require.NoError(t, err)
	assert.Equal(t, invoiceID, buyInfo["invoice_id"])
	assert.Equal(t, float64(200), buyInfo["price_per_kb"])
	assert.Equal(t, false, buyInfo["paid"])

	// Step 3: Submit HTLC payment
	htlcTx := []byte("valid-htlc-transaction-raw-bytes")
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

	// Step 4: Try to pay again, should fail with ALREADY_PAID
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

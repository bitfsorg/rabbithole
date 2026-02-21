package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// InvoiceRecord tracks a pending or completed content purchase.
type InvoiceRecord struct {
	ID          string    `json:"invoice_id"`
	NodePNode   []byte    `json:"-"`
	KeyHash     []byte    `json:"-"`
	PricePerKB  uint64    `json:"price_per_kb"`
	FileSize    uint64    `json:"file_size"`
	PaymentAddr string    `json:"payment_addr"`
	CapsuleHash string    `json:"capsule_hash"`
	Expiry      time.Time `json:"-"`
	Paid        bool      `json:"-"`
}

// DefaultInvoiceExpiry is the default invoice time-to-live.
const DefaultInvoiceExpiry = 1 * time.Hour

// maxHTLCBodySize is the maximum size of an HTLC transaction body (1 MB).
const maxHTLCBodySize = 1 << 20

// servePaidContent returns 402 Payment Required for paid content,
// generating and storing an invoice for the purchase flow.
func (d *Daemon) servePaidContent(w http.ResponseWriter, node *NodeInfo) {
	// Generate a random invoice ID (16 bytes = 32 hex chars).
	idBytes := make([]byte, 16)
	if _, err := randRead(idBytes); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate invoice ID")
		return
	}
	invoiceID := hex.EncodeToString(idBytes)

	// Compute capsule hash as SHA256 of the key hash.
	capsuleHash := ""
	if len(node.KeyHash) > 0 {
		h := sha256.Sum256(node.KeyHash)
		capsuleHash = hex.EncodeToString(h[:])
	}

	// Derive a payment address from the node's public key.
	paymentAddr := ""
	if len(node.PNode) > 0 {
		paymentAddr = fmt.Sprintf("1BitFS%s", hex.EncodeToString(node.PNode[:8]))
	}

	// Determine invoice expiry.
	expiry := DefaultInvoiceExpiry
	if d.config.X402.InvoiceExpiry > 0 {
		expiry = time.Duration(d.config.X402.InvoiceExpiry) * time.Second
	}

	invoice := &InvoiceRecord{
		ID:          invoiceID,
		NodePNode:   node.PNode,
		KeyHash:     node.KeyHash,
		PricePerKB:  node.PricePerKB,
		FileSize:    node.FileSize,
		PaymentAddr: paymentAddr,
		CapsuleHash: capsuleHash,
		Expiry:      time.Now().Add(expiry),
		Paid:        false,
	}

	// Store the invoice.
	d.invoicesMu.Lock()
	d.invoices[invoiceID] = invoice
	d.invoicesMu.Unlock()

	// Return 402 with invoice details.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Price-Per-KB", fmt.Sprintf("%d", node.PricePerKB))
	w.Header().Set("X-File-Size", fmt.Sprintf("%d", node.FileSize))
	w.WriteHeader(http.StatusPaymentRequired)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":        "payment required",
		"invoice_id":   invoiceID,
		"price_per_kb": node.PricePerKB,
		"file_size":    node.FileSize,
		"payment_addr": paymentAddr,
	})
}

// handleGetBuyInfo handles GET /_bitfs/buy/{txid} and returns buy information
// for a previously generated invoice.
func (d *Daemon) handleGetBuyInfo(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	if txid == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_TXID", "Invoice ID is required")
		return
	}

	d.invoicesMu.RLock()
	invoice, ok := d.invoices[txid]
	d.invoicesMu.RUnlock()

	if !ok {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Invoice not found")
		return
	}

	// Check if the invoice has expired.
	if time.Now().After(invoice.Expiry) {
		// Clean up expired invoice.
		d.invoicesMu.Lock()
		delete(d.invoices, txid)
		d.invoicesMu.Unlock()
		writeJSONError(w, http.StatusNotFound, "EXPIRED", "Invoice has expired")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"invoice_id":   invoice.ID,
		"capsule_hash": invoice.CapsuleHash,
		"price_per_kb": invoice.PricePerKB,
		"file_size":    invoice.FileSize,
		"payment_addr": invoice.PaymentAddr,
		"paid":         invoice.Paid,
	})
}

// handleSubmitHTLC handles POST /_bitfs/buy/{txid} and accepts an HTLC
// transaction in exchange for the encrypted content capsule.
func (d *Daemon) handleSubmitHTLC(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	if txid == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_TXID", "Invoice ID is required")
		return
	}

	d.invoicesMu.RLock()
	invoice, ok := d.invoices[txid]
	d.invoicesMu.RUnlock()

	if !ok {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Invoice not found")
		return
	}

	// Check if the invoice has expired.
	if time.Now().After(invoice.Expiry) {
		d.invoicesMu.Lock()
		delete(d.invoices, txid)
		d.invoicesMu.Unlock()
		writeJSONError(w, http.StatusNotFound, "EXPIRED", "Invoice has expired")
		return
	}

	// Check if already paid.
	if invoice.Paid {
		writeJSONError(w, http.StatusConflict, "ALREADY_PAID", "Invoice has already been paid")
		return
	}

	// Read the HTLC transaction from the request body.
	htlcBody, err := io.ReadAll(io.LimitReader(r.Body, maxHTLCBodySize))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Require a non-empty transaction body.
	if len(htlcBody) == 0 {
		writeJSONError(w, http.StatusBadRequest, "EMPTY_TX", "HTLC transaction body is required")
		return
	}

	// TODO: Real HTLC transaction verification will be added in a future task.
	// For now, accept any non-empty body as a valid payment.

	// Mark the invoice as paid.
	d.invoicesMu.Lock()
	invoice.Paid = true
	d.invoicesMu.Unlock()

	// Retrieve the encrypted content from the store.
	if len(invoice.KeyHash) == 0 {
		writeJSONError(w, http.StatusNotFound, "NO_CONTENT", "No content key hash associated with this invoice")
		return
	}

	exists, err := d.store.Has(invoice.KeyHash)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "STORAGE_ERROR", "Failed to check content availability")
		return
	}
	if !exists {
		writeJSONError(w, http.StatusNotFound, "CONTENT_NOT_FOUND", "Encrypted content not found in store")
		return
	}

	data, err := d.store.Get(invoice.KeyHash)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "STORAGE_ERROR", "Failed to retrieve encrypted content")
		return
	}

	// Return the capsule (hex-encoded encrypted content).
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"invoice_id": invoice.ID,
		"capsule":    hex.EncodeToString(data),
		"paid":       true,
	})

	// Suppress unused variable warning for htlcBody.
	_ = htlcBody
}

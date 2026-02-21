package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tongxiaofeng/libbitfs/x402"
)

// InvoiceRecord tracks a pending or completed content purchase.
type InvoiceRecord struct {
	ID          string    `json:"invoice_id"`
	TotalPrice  uint64    `json:"total_price"`
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
// Uses libbitfs/x402 for invoice creation, price calculation, and HTTP headers.
func (d *Daemon) servePaidContent(w http.ResponseWriter, node *NodeInfo) {
	// Compute capsule hash as SHA256 of the key hash.
	var capsuleHashBytes []byte
	capsuleHashHex := ""
	if len(node.KeyHash) > 0 {
		h := sha256.Sum256(node.KeyHash)
		capsuleHashBytes = h[:]
		capsuleHashHex = hex.EncodeToString(capsuleHashBytes)
	}

	// TODO(payment): Replace with real BSV P2PKH address derived from node's public key.
	// This is a placeholder format for development; real addresses use Base58Check encoding.
	paymentAddr := ""
	if len(node.PNode) > 0 {
		paymentAddr = fmt.Sprintf("1BitFS%s", hex.EncodeToString(node.PNode[:8]))
	}

	// Determine invoice TTL in seconds.
	ttlSeconds := int64(DefaultInvoiceExpiry / time.Second)
	if d.config.X402.InvoiceExpiry > 0 {
		ttlSeconds = d.config.X402.InvoiceExpiry
	}

	// Create invoice via libbitfs/x402.
	inv := x402.NewInvoice(node.PricePerKB, node.FileSize, paymentAddr, capsuleHashBytes, ttlSeconds)

	// Convert x402.Invoice to daemon's InvoiceRecord for internal state management.
	record := &InvoiceRecord{
		ID:          inv.ID,
		TotalPrice:  inv.Price,
		NodePNode:   node.PNode,
		KeyHash:     node.KeyHash,
		PricePerKB:  inv.PricePerKB,
		FileSize:    inv.FileSize,
		PaymentAddr: inv.PaymentAddr,
		CapsuleHash: capsuleHashHex,
		Expiry:      time.Unix(inv.Expiry, 0),
		Paid:        false,
	}

	// Store the invoice.
	d.invoicesMu.Lock()
	d.invoices[inv.ID] = record
	d.invoicesMu.Unlock()

	// Set x402 HTTP headers and return 402 status via libbitfs/x402.
	w.Header().Set("Content-Type", "application/json")
	x402.SetPaymentHeaders(w, x402.PaymentHeadersFromInvoice(inv))

	// Return JSON body with invoice details.
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":        "payment required",
		"invoice_id":   inv.ID,
		"total_price":  inv.Price,
		"price_per_kb": inv.PricePerKB,
		"file_size":    inv.FileSize,
		"payment_addr": inv.PaymentAddr,
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
		"total_price":  invoice.TotalPrice,
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
	defer func() { _ = r.Body.Close() }()
	htlcBody, err := io.ReadAll(io.LimitReader(r.Body, maxHTLCBodySize))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
		return
	}

	// Require a non-empty transaction body.
	if len(htlcBody) == 0 {
		writeJSONError(w, http.StatusBadRequest, "EMPTY_TX", "HTLC transaction body is required")
		return
	}

	// Verify the payment transaction using libbitfs/x402.
	proof := &x402.PaymentProof{RawTx: htlcBody}
	inv := &x402.Invoice{
		ID:          invoice.ID,
		Price:       invoice.TotalPrice,
		PricePerKB:  invoice.PricePerKB,
		FileSize:    invoice.FileSize,
		PaymentAddr: invoice.PaymentAddr,
		Expiry:      invoice.Expiry.Unix(),
	}
	if err := x402.VerifyPayment(proof, inv); err != nil {
		writeJSONError(w, http.StatusBadRequest, "PAYMENT_INVALID", fmt.Sprintf("Payment verification failed: %v", err))
		return
	}

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

	// Mark the invoice as paid only after content is successfully retrieved.
	d.invoicesMu.Lock()
	invoice.Paid = true
	d.invoicesMu.Unlock()

	// Return the capsule (hex-encoded encrypted content).
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"invoice_id": invoice.ID,
		"capsule":    hex.EncodeToString(data),
		"paid":       true,
	})
}

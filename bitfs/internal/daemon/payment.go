package daemon

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/tongxiaofeng/libbitfs-go/method42"
	"github.com/tongxiaofeng/libbitfs-go/x402"
)

// InvoiceRecord tracks a pending or completed content purchase.
type InvoiceRecord struct {
	ID           string    `json:"invoice_id"`
	TotalPrice   uint64    `json:"total_price"`
	NodePNode    []byte    `json:"-"`
	KeyHash      []byte    `json:"-"`
	PricePerKB   uint64    `json:"price_per_kb"`
	FileSize     uint64    `json:"file_size"`
	PaymentAddr  string    `json:"payment_addr"`
	SellerPubKey string    `json:"seller_pubkey"`        // Hex-encoded compressed seller pubkey (for HTLC 2-of-2 multisig)
	CapsuleHash  string    `json:"capsule_hash"`
	HTLCScript   []byte    `json:"-"`                    // Precomputed HTLC script for verification
	Capsule      []byte    `json:"capsule,omitempty"`    // ECDH capsule for buyer (persisted for crash recovery)
	Expiry       time.Time `json:"expiry"`
	Paid         bool      `json:"paid"`
}

// DefaultInvoiceExpiry is the default invoice time-to-live.
const DefaultInvoiceExpiry = 1 * time.Hour

// maxHTLCBodySize is the maximum size of an HTLC transaction body (1 MB).
const maxHTLCBodySize = 1 << 20

// servePaidContent returns 402 Payment Required for paid content,
// generating and storing an invoice for the purchase flow.
// Uses libbitfs/x402 for invoice creation, price calculation, and HTTP headers.
//
// Capsule computation is deferred to handleGetBuyInfo, where the buyer
// provides their public key. This is required because the capsule is
// XOR-masked with a buyer-specific mask derived from ECDH(D_node, P_buyer).
func (d *Daemon) servePaidContent(w http.ResponseWriter, node *NodeInfo) {
	sellerPriv, _, err := d.wallet.GetSellerKeyPair()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "WALLET_ERROR", "Failed to get seller key pair")
		return
	}

	// Derive payment address from seller's public key (proper P2PKH address).
	sellerAddr, err := script.NewAddressFromPublicKey(sellerPriv.PubKey(), d.config.Mainnet)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "ADDR_ERROR", "Failed to derive payment address")
		return
	}
	paymentAddr := sellerAddr.AddressString

	// Determine invoice TTL in seconds.
	ttlSeconds := int64(DefaultInvoiceExpiry / time.Second)
	if d.config.X402.InvoiceExpiry > 0 {
		ttlSeconds = d.config.X402.InvoiceExpiry
	}

	// Create invoice without capsule hash (deferred until buyer identifies themselves).
	inv := x402.NewInvoice(node.PricePerKB, node.FileSize, paymentAddr, nil, ttlSeconds)

	// Convert x402.Invoice to daemon's InvoiceRecord for internal state management.
	sellerPubKeyHex := hex.EncodeToString(sellerPriv.PubKey().Compressed())
	record := &InvoiceRecord{
		ID:           inv.ID,
		TotalPrice:   inv.Price,
		NodePNode:    node.PNode,
		KeyHash:      node.KeyHash,
		PricePerKB:   inv.PricePerKB,
		FileSize:     inv.FileSize,
		PaymentAddr:  inv.PaymentAddr,
		SellerPubKey: sellerPubKeyHex,
		Expiry:       time.Unix(inv.Expiry, 0),
		Paid:         false,
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
//
// The buyer must provide their public key via the "buyer_pubkey" query parameter
// (hex-encoded compressed 33-byte key). On first call with a valid buyer_pubkey,
// the server computes the XOR-masked capsule using:
//
//	capsule = aes_key XOR HKDF(ECDH(D_node, P_buyer).x, key_hash, "bitfs-buyer-mask")
//
// and stores the capsule + capsule_hash in the invoice for the HTLC flow.
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
		d.invoicesMu.Lock()
		delete(d.invoices, txid)
		d.invoicesMu.Unlock()
		writeJSONError(w, http.StatusNotFound, "EXPIRED", "Invoice has expired")
		return
	}

	// Compute capsule on demand when buyer provides their pubkey.
	// Use write lock for the entire check-compute-set to prevent TOCTOU race.
	buyerPubHex := r.URL.Query().Get("buyer_pubkey")
	if buyerPubHex != "" && len(invoice.NodePNode) > 0 {
		d.invoicesMu.Lock()
		if len(invoice.Capsule) == 0 {
			buyerPubBytes, err := hex.DecodeString(buyerPubHex)
			if err == nil && len(buyerPubBytes) == 33 {
				buyerPub, err := ec.PublicKeyFromBytes(buyerPubBytes)
				if err == nil {
					nodePriv, nodePub, err := d.wallet.DeriveNodeKeyPair(invoice.NodePNode)
					if err == nil {
						capsule, err := method42.ComputeCapsule(nodePriv, nodePub, buyerPub, invoice.KeyHash)
						if err == nil {
							capsuleHash := method42.ComputeCapsuleHash(capsule)
							// Build HTLC script for payment verification.
							sellerPriv2, _, kpErr := d.wallet.GetSellerKeyPair()
							var htlcScript []byte
							if kpErr == nil {
								sellerPKH := sellerPriv2.PubKey().Hash()
								htlcScript, _ = x402.BuildHTLC(&x402.HTLCParams{
									BuyerPubKey:  buyerPubBytes,
									SellerPubKey: sellerPriv2.PubKey().Compressed(),
									SellerAddr:   sellerPKH,
									CapsuleHash:  capsuleHash,
									Amount:       invoice.TotalPrice,
									Timeout:      x402.DefaultHTLCTimeout,
								})
							}
							invoice.Capsule = capsule
							invoice.CapsuleHash = hex.EncodeToString(capsuleHash)
							if len(htlcScript) > 0 {
								invoice.HTLCScript = htlcScript
							}
						}
					}
				}
			}
		}
		d.invoicesMu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"invoice_id":    invoice.ID,
		"total_price":   invoice.TotalPrice,
		"capsule_hash":  invoice.CapsuleHash,
		"price_per_kb":  invoice.PricePerKB,
		"file_size":     invoice.FileSize,
		"payment_addr":  invoice.PaymentAddr,
		"seller_pubkey": invoice.SellerPubKey,
		"paid":          invoice.Paid,
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

	// Read body BEFORE acquiring lock (I/O should not hold locks).
	defer func() { _ = r.Body.Close() }()
	htlcBody, err := io.ReadAll(io.LimitReader(r.Body, maxHTLCBodySize))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
		return
	}
	if len(htlcBody) == 0 {
		writeJSONError(w, http.StatusBadRequest, "EMPTY_TX", "HTLC transaction body is required")
		return
	}

	// Single write lock for the entire check-verify-set sequence to prevent TOCTOU.
	d.invoicesMu.Lock()
	invoice, ok := d.invoices[txid]
	if !ok {
		d.invoicesMu.Unlock()
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Invoice not found")
		return
	}
	if time.Now().After(invoice.Expiry) {
		delete(d.invoices, txid)
		d.invoicesMu.Unlock()
		writeJSONError(w, http.StatusNotFound, "EXPIRED", "Invoice has expired")
		return
	}
	if invoice.Paid {
		d.invoicesMu.Unlock()
		writeJSONError(w, http.StatusConflict, "ALREADY_PAID", "Invoice has already been paid")
		return
	}
	// Mark paid immediately to prevent concurrent claims (optimistic lock).
	invoice.Paid = true
	d.invoicesMu.Unlock()

	// Verify the payment transaction (outside lock — crypto/parsing is CPU-bound, not lock-worthy).
	// On any failure below, rollback the Paid flag.
	rollbackPaid := func() {
		d.invoicesMu.Lock()
		invoice.Paid = false
		d.invoicesMu.Unlock()
	}

	if len(invoice.HTLCScript) > 0 {
		// HTLC path: verify the funding tx has a matching HTLC output.
		_, err := x402.VerifyHTLCFunding(htlcBody, invoice.HTLCScript, invoice.TotalPrice)
		if err != nil {
			rollbackPaid()
			writeJSONError(w, http.StatusBadRequest, "PAYMENT_INVALID",
				fmt.Sprintf("HTLC verification failed: %v", err))
			return
		}
	} else {
		// Fallback: verify as P2PKH payment (backwards compatibility).
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
			rollbackPaid()
			writeJSONError(w, http.StatusBadRequest, "PAYMENT_INVALID", "Payment verification failed")
			return
		}
	}

	// Replay protection: ensure the same transaction is not used for multiple invoices.
	submittedTx, parseErr := transaction.NewTransactionFromBytes(htlcBody)
	if parseErr != nil {
		rollbackPaid()
		writeJSONError(w, http.StatusBadRequest, "PAYMENT_INVALID", "Cannot parse transaction")
		return
	}
	submittedTxID := submittedTx.TxID().String()

	d.usedTxIDsMu.Lock()
	if existingInvoice, used := d.usedTxIDs[submittedTxID]; used {
		d.usedTxIDsMu.Unlock()
		rollbackPaid()
		writeJSONError(w, http.StatusConflict, "TX_REUSED",
			fmt.Sprintf("Transaction already used for invoice %s", existingInvoice))
		return
	}
	d.usedTxIDs[submittedTxID] = invoice.ID
	d.usedTxIDsMu.Unlock()

	// Broadcast the payment transaction to the blockchain before revealing capsule.
	if d.chain != nil {
		txHex := hex.EncodeToString(htlcBody)
		_, broadcastErr := d.chain.BroadcastTx(r.Context(), txHex)
		if broadcastErr != nil {
			// Rollback replay tracking and paid flag on broadcast failure.
			d.usedTxIDsMu.Lock()
			delete(d.usedTxIDs, submittedTxID)
			d.usedTxIDsMu.Unlock()
			rollbackPaid()
			writeJSONError(w, http.StatusBadRequest, "BROADCAST_FAILED",
				fmt.Sprintf("Payment tx not accepted: %v", broadcastErr))
			return
		}
	}

	// Return the capsule (ECDH shared secret).
	if len(invoice.Capsule) == 0 {
		writeJSONError(w, http.StatusInternalServerError, "NO_CAPSULE", "No capsule computed for this invoice")
		return
	}

	// Persist paid invoice before sending response (crash recovery).
	_ = d.persistInvoice(invoice)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"invoice_id": invoice.ID,
		"capsule":    hex.EncodeToString(invoice.Capsule),
		"paid":       true,
	})
}

// Package buyer provides shared types and logic for b* buyer tools
// (bcat, bget, bmget). It encapsulates JSON output formats, buyer
// wallet configuration, UTXO selection, and the unified buy flow.
package buy

import "github.com/bitfsorg/bitfs/internal/client"

// CatResponse is the JSON output for bcat --json.
type CatResponse struct {
	Meta            *client.MetaResponse `json:"meta,omitempty"`
	Content         *string              `json:"content,omitempty"`
	ContentBase64   *string              `json:"content_base64,omitempty"`
	PaymentRequired bool                 `json:"payment_required,omitempty"`
	PaymentInfo     *PaymentInfo         `json:"payment_info,omitempty"`
	Payment         *PaymentResult       `json:"payment,omitempty"`
}

// GetResponse is the JSON output for bget --json.
type GetResponse struct {
	Meta            *client.MetaResponse `json:"meta,omitempty"`
	OutputPath      string               `json:"output_path,omitempty"`
	BytesWritten    int64                `json:"bytes_written,omitempty"`
	PaymentRequired bool                 `json:"payment_required,omitempty"`
	PaymentInfo     *PaymentInfo         `json:"payment_info,omitempty"`
	Payment         *PaymentResult       `json:"payment,omitempty"`
}

// PaymentInfo describes a pending payment for paid content.
type PaymentInfo struct {
	Price        uint64 `json:"price"`
	PricePerKB   uint64 `json:"price_per_kb"`
	SellerPubKey string `json:"seller_pubkey"`
	PaymentAddr  string `json:"payment_addr"`
}

// PaymentResult describes a completed payment.
type PaymentResult struct {
	CostSatoshis uint64 `json:"cost_satoshis"`
	HTLCTxID     string `json:"htlc_txid"`
}

// ErrorResponse is the unified JSON error output for all b* tools.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

// BatchGetResponse is the JSON output for bmget --json.
type BatchGetResponse struct {
	Total     int              `json:"total"`
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	Files     []BatchFileEntry `json:"files"`
}

// BatchFileEntry is a single file result in a batch operation.
type BatchFileEntry struct {
	Path         string         `json:"path"`
	OutputPath   string         `json:"output_path,omitempty"`
	BytesWritten int64          `json:"bytes_written,omitempty"`
	Payment      *PaymentResult `json:"payment,omitempty"`
	Error        string         `json:"error,omitempty"`
	Code         int            `json:"code,omitempty"`
}

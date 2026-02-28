// Package client provides an HTTP client for the BitFS daemon API.
//
// It is the foundation that all b-tools (bls, bcat, bget, bstat, btree) use
// to communicate with BitFS daemons over the LFCP HTTP interface.
// Daemon endpoints are resolved from bitfs:// URIs via paymail PKI or DNS SRV records.
package client

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Sentinel errors for exit code mapping in CLI tools.
var (
	// ErrNotFound indicates the requested resource was not found (HTTP 404).
	ErrNotFound = errors.New("client: not found")

	// ErrPaymentRequired indicates the content requires payment (HTTP 402).
	ErrPaymentRequired = errors.New("client: payment required")

	// ErrTimeout indicates the request timed out.
	ErrTimeout = errors.New("client: request timeout")

	// ErrNetwork indicates a network-level failure (connection refused, DNS, etc.).
	ErrNetwork = errors.New("client: network error")

	// ErrServer indicates a server-side error (HTTP 5xx or 429).
	ErrServer = errors.New("client: server error")
)

// MetaResponse holds node metadata returned by the daemon.
type MetaResponse struct {
	PNode      string       `json:"pnode"`
	Type       string       `json:"type"` // "file", "dir", "link"
	Path       string       `json:"path"`
	MimeType   string       `json:"mime_type,omitempty"`
	FileSize   uint64       `json:"file_size,omitempty"`
	KeyHash    string       `json:"key_hash,omitempty"`
	Access     string       `json:"access"` // "free", "paid", "private"
	PricePerKB uint64       `json:"price_per_kb,omitempty"`
	TxID       string       `json:"txid,omitempty"`
	Timestamp  int64        `json:"timestamp,omitempty"` // Unix timestamp (seconds), 0 if unavailable
	Children   []ChildEntry `json:"children,omitempty"`
}

// ChildEntry represents a child node in a directory listing.
type ChildEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// BuyInfo holds purchase information for a paid file.
type BuyInfo struct {
	CapsuleHash  string `json:"capsule_hash"`
	Price        uint64 `json:"total_price"`
	PaymentAddr  string `json:"payment_addr"`
	SellerPubKey string `json:"seller_pubkey"`           // Hex-encoded compressed seller pubkey
	CapsuleNonce string `json:"capsule_nonce,omitempty"` // Hex-encoded per-invoice nonce for capsule unlinkability
}

// CapsuleResponse holds the re-encryption capsule returned after HTLC payment.
type CapsuleResponse struct {
	Capsule      string `json:"capsule"`
	CapsuleNonce string `json:"capsule_nonce,omitempty"` // Hex-encoded per-invoice nonce for capsule unlinkability
}

// SaleRecord represents a completed or pending sale.
type SaleRecord struct {
	InvoiceID string `json:"invoice_id"`
	Price     uint64 `json:"price"`
	KeyHash   string `json:"key_hash"`
	Timestamp int64  `json:"timestamp"`
	Paid      bool   `json:"paid"`
}

// Client is an HTTP client for the BitFS daemon API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// New creates a new Client with a 30-second default timeout.
// The trailing slash is stripped from baseURL if present.
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithTimeout returns a copy of the client with the given timeout.
// The original client is not modified.
func (c *Client) WithTimeout(d time.Duration) *Client {
	return &Client{
		BaseURL: c.BaseURL,
		HTTPClient: &http.Client{
			Timeout:   d,
			Transport: c.HTTPClient.Transport,
		},
	}
}

// GetMeta retrieves node metadata from the daemon.
// Endpoint: GET /_bitfs/meta/{pnode}/{path}
func (c *Client) GetMeta(pnode, path string) (*MetaResponse, error) {
	// Validate pnode is a 66-char hex string (33-byte compressed pubkey).
	if err := validateHex(pnode, 33, "pnode"); err != nil {
		return nil, err
	}

	// URL-encode each path segment to handle spaces, #, ?, etc.
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	escapedPath := strings.Join(segments, "/")
	reqURL := fmt.Sprintf("%s/_bitfs/meta/%s/%s", c.BaseURL, pnode, escapedPath)

	resp, err := c.HTTPClient.Get(reqURL)
	if err != nil {
		return nil, wrapNetworkError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var meta MetaResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("client: decode meta response: %w", err)
	}
	return &meta, nil
}

// GetData retrieves encrypted content by hash from the daemon.
// The caller is responsible for closing the returned ReadCloser.
// Endpoint: GET /_bitfs/data/{hash}
func (c *Client) GetData(hash string) (io.ReadCloser, error) {
	// Validate hash is a 64-char hex string (32-byte SHA256).
	if err := validateHex(hash, 32, "hash"); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/_bitfs/data/%s", c.BaseURL, hash)

	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, wrapNetworkError(err)
	}

	if err := checkStatus(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}

	return resp.Body, nil
}

// GetBuyInfo retrieves purchase information for a paid file.
// If buyerPubKeyHex is non-empty, it is sent as the "buyer_pubkey" query
// parameter so the server can compute the buyer-specific capsule.
// Endpoint: GET /_bitfs/buy/{txid}[?buyer_pubkey=...]
func (c *Client) GetBuyInfo(txid string, buyerPubKeyHex ...string) (*BuyInfo, error) {
	reqURL := fmt.Sprintf("%s/_bitfs/buy/%s", c.BaseURL, txid)
	if len(buyerPubKeyHex) > 0 && buyerPubKeyHex[0] != "" {
		q := url.Values{}
		q.Set("buyer_pubkey", buyerPubKeyHex[0])
		reqURL += "?" + q.Encode()
	}

	resp, err := c.HTTPClient.Get(reqURL)
	if err != nil {
		return nil, wrapNetworkError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var info BuyInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("client: decode buy info: %w", err)
	}
	return &info, nil
}

// SubmitHTLC submits a signed HTLC transaction to purchase content.
// The raw transaction bytes are sent as application/octet-stream.
// Endpoint: POST /_bitfs/buy/{txid}
func (c *Client) SubmitHTLC(txid string, htlcRawTx []byte) (*CapsuleResponse, error) {
	url := fmt.Sprintf("%s/_bitfs/buy/%s", c.BaseURL, txid)

	req, err := http.NewRequest("POST", url, bytes.NewReader(htlcRawTx))
	if err != nil {
		return nil, fmt.Errorf("client: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapNetworkError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var capsule CapsuleResponse
	if err := json.NewDecoder(resp.Body).Decode(&capsule); err != nil {
		return nil, fmt.Errorf("client: decode capsule response: %w", err)
	}
	return &capsule, nil
}

// VersionEntry represents a single version of a node.
type VersionEntry struct {
	Version     int    `json:"version"`      // 1=latest, 2=previous, etc.
	TxID        string `json:"txid"`         // Transaction ID for this version
	BlockHeight uint32 `json:"block_height"` // Block height (0 if unconfirmed)
	Timestamp   int64  `json:"timestamp"`    // Unix timestamp (seconds)
	FileSize    uint64 `json:"file_size"`    // File size in bytes
	Access      string `json:"access"`       // "free", "paid", "private"
}

// GetVersions retrieves the version history for a node.
// Endpoint: GET /_bitfs/versions/{pnode}/{path}
func (c *Client) GetVersions(pnode, path string) ([]VersionEntry, error) {
	// Validate pnode is a 66-char hex string (33-byte compressed pubkey).
	if err := validateHex(pnode, 33, "pnode"); err != nil {
		return nil, err
	}

	// URL-encode each path segment to handle spaces, #, ?, etc.
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	escapedPath := strings.Join(segments, "/")
	reqURL := fmt.Sprintf("%s/_bitfs/versions/%s/%s", c.BaseURL, pnode, escapedPath)

	resp, err := c.HTTPClient.Get(reqURL)
	if err != nil {
		return nil, wrapNetworkError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var versions []VersionEntry
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, fmt.Errorf("client: decode versions: %w", err)
	}
	return versions, nil
}

// SPVProofResponse holds the SPV verification result returned by the daemon.
type SPVProofResponse struct {
	TxID        string `json:"txid"`
	Confirmed   bool   `json:"confirmed"`
	BlockHash   string `json:"block_hash,omitempty"`
	BlockHeight uint64 `json:"block_height,omitempty"`
}

// VerifySPV requests SPV verification of a transaction from the daemon.
// Endpoint: GET /_bitfs/spv/proof/{txid}
func (c *Client) VerifySPV(txid string) (*SPVProofResponse, error) {
	url := fmt.Sprintf("%s/_bitfs/spv/proof/%s", c.BaseURL, txid)

	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, wrapNetworkError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var result SPVProofResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("client: decode SPV proof response: %w", err)
	}
	return &result, nil
}

// GetSales retrieves sales records from the daemon.
// Endpoint: GET /_bitfs/sales[?status=...&limit=...]
func (c *Client) GetSales(status string, limit int) ([]SaleRecord, error) {
	reqURL := fmt.Sprintf("%s/_bitfs/sales?status=%s&limit=%d", c.BaseURL, url.QueryEscape(status), limit)

	resp, err := c.HTTPClient.Get(reqURL)
	if err != nil {
		return nil, wrapNetworkError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var records []SaleRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("client: decode sales: %w", err)
	}
	return records, nil
}

// checkStatus maps HTTP status codes to sentinel errors.
// Returns nil for 2xx responses.
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// Drain body for error context (limit to 1KB)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	msg := strings.TrimSpace(string(body))

	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	case http.StatusPaymentRequired:
		return fmt.Errorf("%w: %s", ErrPaymentRequired, msg)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return fmt.Errorf("%w: %s", ErrTimeout, msg)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: rate limited", ErrServer)
	default:
		if resp.StatusCode >= 500 {
			return fmt.Errorf("%w: HTTP %d: %s", ErrServer, resp.StatusCode, msg)
		}
		return fmt.Errorf("client: unexpected HTTP %d: %s", resp.StatusCode, msg)
	}
}

// wrapNetworkError wraps a network-level error with ErrNetwork.
func wrapNetworkError(err error) error {
	return fmt.Errorf("%w: %w", ErrNetwork, err)
}

// validateHex validates that s is a valid hex string decoding to exactly expectedBytes bytes.
func validateHex(s string, expectedBytes int, name string) error {
	if len(s) != expectedBytes*2 {
		return fmt.Errorf("client: %s must be %d hex characters, got %d", name, expectedBytes*2, len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("client: invalid %s hex: %w", name, err)
	}
	return nil
}

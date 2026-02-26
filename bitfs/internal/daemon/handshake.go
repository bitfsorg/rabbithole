package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
)

// HandshakeRequest represents an incoming Method 42 handshake request.
type HandshakeRequest struct {
	BuyerPub  string `json:"buyer_pub"` // Hex-encoded compressed public key
	NonceB    string `json:"nonce_b"`   // Hex-encoded nonce
	Timestamp int64  `json:"timestamp"` // Unix timestamp
}

// HandshakeResponse represents the seller's handshake response.
type HandshakeResponse struct {
	SellerPub string `json:"seller_pub"` // Hex-encoded compressed public key
	NonceS    string `json:"nonce_s"`    // Hex-encoded nonce
	Timestamp int64  `json:"timestamp"`  // Unix timestamp
	SessionID string `json:"session_id"` // Session identifier
	ExpiresAt int64  `json:"expires_at"` // Session expiry time
}

// DefaultSessionTTL is the default session time-to-live.
const DefaultSessionTTL = 24 * time.Hour

// MaxHandshakeClockSkew is the maximum allowed time difference between
// buyer and seller clocks for handshake requests.
const MaxHandshakeClockSkew = 5 * time.Minute

// handleHandshake handles POST /_bitfs/handshake.
// Implements the Method 42 ECDH handshake:
//  1. Buyer sends {P_buyer, nonce_b, timestamp}
//  2. Seller responds {P_seller, nonce_s, timestamp}
//  3. Both compute session_key = SHA256(ECDH.x || nonce_b || nonce_s)
func (d *Daemon) handleHandshake(w http.ResponseWriter, r *http.Request) {
	// Read and parse the request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req HandshakeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON in request body")
		return
	}

	// Validate request fields
	if req.BuyerPub == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELD", "buyer_pub is required")
		return
	}
	if req.NonceB == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELD", "nonce_b is required")
		return
	}

	// Validate timestamp is within acceptable window.
	if req.Timestamp == 0 {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELD", "timestamp is required")
		return
	}
	reqTime := time.Unix(req.Timestamp, 0)
	skew := time.Since(reqTime)
	if skew < 0 {
		skew = -skew
	}
	if skew > MaxHandshakeClockSkew {
		writeJSONError(w, http.StatusBadRequest, "INVALID_TIMESTAMP",
			"timestamp too far from server time (max skew 5 minutes)")
		return
	}

	// Decode buyer's public key
	buyerPubBytes, err := hex.DecodeString(req.BuyerPub)
	if err != nil || len(buyerPubBytes) != 33 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PUBKEY", "Invalid buyer public key")
		return
	}

	// Decode buyer's nonce
	nonceB, err := hex.DecodeString(req.NonceB)
	if err != nil || len(nonceB) == 0 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_NONCE", "Invalid buyer nonce")
		return
	}

	// Get seller's key pair
	sellerPriv, sellerPub, err := d.wallet.GetSellerKeyPair()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "KEY_ERROR", "Failed to get seller key pair")
		return
	}

	// Generate seller's nonce
	nonceS := make([]byte, 32)
	if _, err := cryptoRandRead(nonceS); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "NONCE_ERROR", "Failed to generate nonce")
		return
	}

	// Parse buyer's public key
	buyerPubKey, err := ec.PublicKeyFromBytes(buyerPubBytes)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PUBKEY", "Failed to parse buyer public key")
		return
	}

	// Compute ECDH shared secret: ECDH(D_seller, P_buyer)
	sharedX, err := computeECDH(sellerPriv, buyerPubKey)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "ECDH_ERROR", "ECDH computation failed")
		return
	}

	// Serialize seller's public key
	sellerPubBytes := sellerPub.Compressed()

	// Create session
	session := d.CreateSession(buyerPubBytes, sellerPubBytes, sharedX, nonceB, nonceS, DefaultSessionTTL)

	// Build response
	resp := HandshakeResponse{
		SellerPub: hex.EncodeToString(sellerPubBytes),
		NonceS:    hex.EncodeToString(nonceS),
		Timestamp: time.Now().Unix(),
		SessionID: session.ID,
		ExpiresAt: session.ExpiresAt.Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// computeECDH performs the ECDH shared secret computation.
// Returns the x-coordinate of the shared point (32 bytes).
func computeECDH(privKey *ec.PrivateKey, pubKey *ec.PublicKey) ([]byte, error) {
	sharedPoint, err := privKey.DeriveSharedSecret(pubKey)
	if err != nil {
		return nil, err
	}

	xBytes := sharedPoint.X.Bytes()
	if len(xBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(xBytes):], xBytes)
		return padded, nil
	}
	return xBytes[:32], nil
}

// computeSessionKey derives the session key from ECDH shared secret and nonces.
// session_key = SHA256(shared.x || nonce_b || nonce_s)
func computeSessionKey(sharedX, nonceB, nonceS []byte) []byte {
	h := sha256.New()
	h.Write(sharedX)
	h.Write(nonceB)
	h.Write(nonceS)
	return h.Sum(nil)
}

// cryptoRandRead is a variable for testing injection.
var cryptoRandRead = cryptoRandReadDefault

func cryptoRandReadDefault(b []byte) (int, error) {
	return randRead(b)
}

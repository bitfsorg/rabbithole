// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/bitfs/internal/buyer"
	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/libbitfs-go/method42"
)

// testPubKey is a well-known compressed public key hex (33 bytes, prefix 02).
const testPubKey = "02b4632d08485ff1df2db55b9dafd23347d1c47a457072a1e87be26896549a8737"

// testKeyHash returns a valid 64-hex-char hash for testing (32 bytes).
func testKeyHash(suffix string) string {
	base := strings.Repeat("ab", 32) // 64 hex chars
	// Replace the last len(suffix) chars with suffix for uniqueness.
	if len(suffix) <= len(base) {
		return base[:len(base)-len(suffix)] + suffix
	}
	return base
}

func makeURI(path string) string {
	if path == "" || path == "/" {
		return "bitfs://" + testPubKey
	}
	return "bitfs://" + testPubKey + path
}

// newMockDaemon creates an httptest.Server that serves both /_bitfs/meta/ and
// /_bitfs/data/ requests.
func newMockDaemon(t *testing.T, metaHandler, dataHandler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", metaHandler)
	if dataHandler != nil {
		mux.HandleFunc("/_bitfs/data/", dataHandler)
	}
	return httptest.NewServer(mux)
}

// serveJSON is a helper that writes a JSON response.
func serveJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// newFullMockDaemon creates an httptest.Server that serves /_bitfs/meta/,
// /_bitfs/data/, and /_bitfs/buy/ requests for testing the purchase flow.
func newFullMockDaemon(t *testing.T,
	metaHandler func(w http.ResponseWriter, r *http.Request),
	dataHandler func(w http.ResponseWriter, r *http.Request),
	buyHandler func(w http.ResponseWriter, r *http.Request),
) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", metaHandler)
	if dataHandler != nil {
		mux.HandleFunc("/_bitfs/data/", dataHandler)
	}
	if buyHandler != nil {
		mux.HandleFunc("/_bitfs/buy/", buyHandler)
	}
	return httptest.NewServer(mux)
}

// ---------------------------------------------------------------------------
// Free content output
// ---------------------------------------------------------------------------

func TestFreeContent_OutputToStdout(t *testing.T) {
	plaintext := []byte("Hello, BitFS world!\nSecond line.\n")

	// Encrypt content with Method 42 AccessFree using testPubKey.
	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/hello.txt",
				MimeType: "text/plain",
				FileSize: uint64(len(plaintext)),
				KeyHash:  keyHashHex,
				Access:   "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	assert.Equal(t, plaintext, stdout.Bytes(), "stdout should contain decrypted plaintext")
}

func TestFreeContent_BinaryData(t *testing.T) {
	// Binary content with null bytes, high bytes, etc.
	plaintext := make([]byte, 256)
	for i := range plaintext {
		plaintext[i] = byte(i)
	}

	// Encrypt with Method 42 AccessFree.
	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/binary.dat",
				MimeType: "application/octet-stream",
				FileSize: uint64(len(plaintext)),
				KeyHash:  keyHashHex,
				Access:   "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/binary.dat")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Equal(t, plaintext, stdout.Bytes(), "binary content should be preserved exactly")
}

func TestFreeContent_EmptyFile(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/empty.txt",
				KeyHash: testKeyHash("cc"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			// Write nothing — empty body.
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/empty.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	assert.Empty(t, stdout.Bytes())
	assert.Empty(t, stderr.String())
}

// ---------------------------------------------------------------------------
// Directory node
// ---------------------------------------------------------------------------

func TestDirectory_ReturnsExit6(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:  testPubKey,
				Type:   "dir",
				Path:   "/docs",
				Access: "free",
				Children: []client.ChildEntry{
					{Name: "readme.md", Type: "file"},
				},
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/docs")}, &stdout, &stderr)

	assert.Equal(t, 6, code, "directory should exit 6")
	assert.Contains(t, stderr.String(), "is a directory")
	assert.Empty(t, stdout.String(), "nothing should be written to stdout for directories")
}

// ---------------------------------------------------------------------------
// Paid content
// ---------------------------------------------------------------------------

func TestPaid_WithoutBuy_ReturnsExit5(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      testPubKey,
				Type:       "file",
				Path:       "/premium.pdf",
				FileSize:   5242880,
				Access:     "paid",
				PricePerKB: 100,
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.Equal(t, 5, code, "paid without --buy should exit 5")
	assert.Contains(t, stderr.String(), "content requires payment")
	assert.Contains(t, stderr.String(), "100 sat/KB")
	assert.Contains(t, stderr.String(), "5242880 bytes")
	assert.Contains(t, stderr.String(), "--buy")
	assert.Empty(t, stdout.String())
}

func TestPaid_WithBuy_MissingWalletKey(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      testPubKey,
				Type:       "file",
				Path:       "/premium.pdf",
				FileSize:   5242880,
				Access:     "paid",
				PricePerKB: 100,
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.Equal(t, 6, code, "--buy without --wallet-key should exit 6")
	assert.Contains(t, stderr.String(), "no wallet key configured")
	assert.Empty(t, stdout.String())
}

func TestPaid_WithBuy_InvalidWalletKey(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      testPubKey,
				Type:       "file",
				Path:       "/premium.pdf",
				FileSize:   5242880,
				Access:     "paid",
				PricePerKB: 100,
			})
		},
		nil,
	)
	defer srv.Close()

	tests := []struct {
		name      string
		walletKey string
		wantMsg   string
	}{
		{"not hex", "zzzz", "invalid hex"},
		{"wrong length", "aabbcc", "expected 32 bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{"--buy", "--wallet-key", tt.walletKey, "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

			assert.Equal(t, 6, code, "invalid wallet key should exit 6")
			assert.Contains(t, stderr.String(), tt.wantMsg)
			assert.Empty(t, stdout.String())
		})
	}
}

func TestPaid_WithBuy_MissingTxID(t *testing.T) {
	// Generate a valid buyer key.
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      testPubKey,
				Type:       "file",
				Path:       "/premium.pdf",
				FileSize:   5242880,
				Access:     "paid",
				PricePerKB: 100,
				TxID:       "", // No TxID
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.Equal(t, 5, code, "missing txid should exit 5")
	assert.Contains(t, stderr.String(), "transaction ID is required")
	assert.Empty(t, stdout.String())
}

func TestPaid_WithBuy_SubmitHTLCFails(t *testing.T) {
	// Generate key pairs.
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	// Encrypt test content using node's own pubkey (correct Method 42 encryption).
	plaintext := []byte("paid premium content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	// Compute XOR-masked capsule for buyer.
	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := hex.EncodeToString(nodePriv.PubKey().Hash())
	sellerPubKeyHex := nodePubHex

	// Build a mock UTXO for the buyer (txid:vout:amount).
	utxoTxID := strings.Repeat("ff", 32) // 64 hex chars
	utxoFlag := utxoTxID + ":0:100000"

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      nodePubHex,
				Type:       "file",
				Path:       "/premium.pdf",
				FileSize:   uint64(len(plaintext)),
				Access:     "paid",
				PricePerKB: 100,
				TxID:       "abc123txid",
				KeyHash:    keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash:  capsuleHashHex,
					Price:        1000,
					PaymentAddr:  sellerAddr,
					SellerPubKey: sellerPubKeyHex,
				})
				return
			}
			// POST: Submit HTLC fails with server error.
			http.Error(w, "payment processing failed", http.StatusInternalServerError)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", utxoFlag, "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.Equal(t, 5, code, "submit HTLC failure should exit 5 (purchase failed)")
	assert.Contains(t, stderr.String(), "purchase failed")
	assert.Empty(t, stdout.String())
}

func TestPaid_WithBuy_Success(t *testing.T) {
	// Generate key pairs.
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	// Encrypt test content using node's own pubkey (correct Method 42 encryption).
	plaintext := []byte("Hello, this is paid premium content!")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	// Compute XOR-masked capsule for buyer.
	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := hex.EncodeToString(nodePriv.PubKey().Hash())
	sellerPubKeyHex := nodePubHex

	// Build a mock UTXO for the buyer (txid:vout:amount).
	utxoTxID := strings.Repeat("ff", 32) // 64 hex chars
	utxoFlag := utxoTxID + ":0:100000"

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      nodePubHex,
				Type:       "file",
				Path:       "/premium.txt",
				MimeType:   "text/plain",
				FileSize:   uint64(len(plaintext)),
				Access:     "paid",
				PricePerKB: 50,
				TxID:       "invoice123",
				KeyHash:    keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			// Return the encrypted ciphertext.
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				// Return buy info.
				serveJSON(w, client.BuyInfo{
					CapsuleHash:  capsuleHashHex,
					Price:        1000,
					PaymentAddr:  sellerAddr,
					SellerPubKey: sellerPubKeyHex,
				})
				return
			}
			// POST: Return the capsule.
			serveJSON(w, client.CapsuleResponse{
				Capsule: capsuleHex,
			})
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", utxoFlag, "--host", srv.URL, makeURI("/premium.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "successful purchase should exit 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	assert.Equal(t, plaintext, stdout.Bytes(), "decrypted content should match original plaintext")
}

// ---------------------------------------------------------------------------
// Private content
// ---------------------------------------------------------------------------

func TestPrivate_ReturnsExit6(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:  testPubKey,
				Type:   "file",
				Path:   "/secret.key",
				Access: "private",
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/secret.key")}, &stdout, &stderr)

	assert.Equal(t, 6, code, "private content should exit 6")
	assert.Contains(t, stderr.String(), "private content cannot be accessed remotely")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Missing key_hash
// ---------------------------------------------------------------------------

func TestMissingKeyHash(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/broken.txt",
				Access:  "free",
				KeyHash: "", // Missing key_hash
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/broken.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code, "missing key_hash should be an error")
	assert.Contains(t, stderr.String(), "no content hash")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Error handling / exit codes
// ---------------------------------------------------------------------------

func TestNotFoundError(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no such path", http.StatusNotFound)
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/nonexistent")}, &stdout, &stderr)

	assert.Equal(t, 2, code, "not found should exit 2")
	assert.Contains(t, stderr.String(), "not found")
	assert.Empty(t, stdout.String())
}

func TestNetworkError(t *testing.T) {
	// Use a host that is guaranteed to refuse connections.
	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", "http://127.0.0.1:1", "--timeout", "1s", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "network error should exit 4")
	assert.Contains(t, stderr.String(), "network error")
	assert.Empty(t, stdout.String())
}

func TestServerError(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal failure", http.StatusInternalServerError)
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "server error should exit 4")
	assert.Contains(t, stderr.String(), "server error")
}

func TestDataEndpoint_ServerError(t *testing.T) {
	// Meta succeeds but data endpoint returns 500.
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/fail.txt",
				KeyHash: testKeyHash("dd"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "storage failure", http.StatusInternalServerError)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/fail.txt")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "data server error should exit 4")
	assert.Contains(t, stderr.String(), "server error")
}

func TestDataEndpoint_NotFound(t *testing.T) {
	// Meta succeeds but data hash not found.
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/missing-data.txt",
				KeyHash: testKeyHash("ee"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/missing-data.txt")}, &stdout, &stderr)

	assert.Equal(t, 2, code, "data not found should exit 2")
	assert.Contains(t, stderr.String(), "not found")
}

// ---------------------------------------------------------------------------
// Missing/invalid arguments
// ---------------------------------------------------------------------------

func TestMissingURIArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)

	assert.Equal(t, 6, code, "missing URI should exit 6")
	assert.Contains(t, stderr.String(), "Usage:")
	assert.Empty(t, stdout.String())
}

func TestMissingURIArgument_NilArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)

	assert.Equal(t, 6, code, "nil args should exit 6")
	assert.Contains(t, stderr.String(), "Usage:")
}

func TestInvalidURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"http://not-a-bitfs-uri"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "invalid URI should exit 6")
	assert.Contains(t, stderr.String(), "bcat:")
	assert.Empty(t, stdout.String())
}

func TestEmptyURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{""}, &stdout, &stderr)

	assert.Equal(t, 6, code, "empty URI should exit 6")
}

// ---------------------------------------------------------------------------
// Paymail / DNSLink resolution errors
// ---------------------------------------------------------------------------

func TestPaymailResolveFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://alice@example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "paymail resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "bcat:")
	assert.Empty(t, stdout.String())
}

func TestDNSLinkResolveFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "dnslink resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "bcat:")
}

func TestPubKeyNoHost_RequiresHostFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{makeURI("/docs")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "--host")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Flag edge cases
// ---------------------------------------------------------------------------

func TestUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--unknown", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
}

func TestInvalidTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", "http://localhost:8080", "--timeout", "notaduration", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "invalid timeout")
}

// ---------------------------------------------------------------------------
// Large content streaming
// ---------------------------------------------------------------------------

func TestFreeContent_LargeFile(t *testing.T) {
	// Verify that large content is decrypted and output correctly.
	size := 1024 * 64 // 64 KB
	plaintext := make([]byte, size)
	for i := range plaintext {
		plaintext[i] = byte(i % 251) // Use prime to vary bytes
	}

	// Encrypt with Method 42 AccessFree.
	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/large.bin",
				KeyHash: keyHashHex,
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/large.bin")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	require.Equal(t, size, stdout.Len(), "output size should match input")
	assert.Equal(t, plaintext, stdout.Bytes())
}

// ---------------------------------------------------------------------------
// Verify data endpoint is called
// ---------------------------------------------------------------------------

func TestFreeContent_DataEndpointCalled(t *testing.T) {
	plaintext := []byte("tracked content")
	var dataRequested bool

	// Encrypt with Method 42 AccessFree.
	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/tracked.txt",
				KeyHash: keyHashHex,
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			dataRequested = true
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/tracked.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	assert.True(t, dataRequested, "data endpoint should have been called")
	assert.Equal(t, plaintext, stdout.Bytes())
}

// ---------------------------------------------------------------------------
// Write error simulation
// ---------------------------------------------------------------------------

func TestFreeContent_WriteError(t *testing.T) {
	plaintext := []byte("some content")

	// Encrypt with Method 42 AccessFree.
	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/werror.txt",
				KeyHash: keyHashHex,
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	// Use a writer that always fails.
	var stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/werror.txt")}, &failWriter{}, &stderr)

	assert.NotEqual(t, 0, code, "write error should produce non-zero exit")
	assert.Contains(t, stderr.String(), "write error")
}

// failWriter always returns an error on Write.
type failWriter struct{}

func (f *failWriter) Write(p []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

// ---------------------------------------------------------------------------
// JSON output mode (--json flag)
// ---------------------------------------------------------------------------

func TestJSON_FlagParsing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", "http://localhost:1"}, &stdout, &stderr)
	assert.Equal(t, 6, code) // Missing URI
}

func TestJSON_MissingURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json"}, &stdout, &stderr)
	assert.Equal(t, 6, code)
}

func TestJSON_FreeContent_TextPlain(t *testing.T) {
	plaintext := []byte("Hello, JSON world!\n")

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/hello.txt",
				MimeType: "text/plain",
				FileSize: uint64(len(plaintext)),
				KeyHash:  keyHashHex,
				Access:   "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	var resp buyer.CatResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	require.NotNil(t, resp.Content, "text content should use content field")
	assert.Equal(t, string(plaintext), *resp.Content)
	assert.Nil(t, resp.ContentBase64, "text content should not use content_base64")
	assert.Equal(t, "text/plain", resp.Meta.MimeType)
}

func TestJSON_FreeContent_Binary(t *testing.T) {
	plaintext := make([]byte, 256)
	for i := range plaintext {
		plaintext[i] = byte(i)
	}

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/data.bin",
				MimeType: "application/octet-stream",
				FileSize: uint64(len(plaintext)),
				KeyHash:  keyHashHex,
				Access:   "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/data.bin")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())

	var resp buyer.CatResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	assert.Nil(t, resp.Content, "binary content should not use content field")
	require.NotNil(t, resp.ContentBase64, "binary content should use content_base64")
}

func TestJSON_FreeContent_ApplicationJSON(t *testing.T) {
	plaintext := []byte(`{"key":"value"}`)

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/data.json",
				MimeType: "application/json",
				FileSize: uint64(len(plaintext)),
				KeyHash:  keyHashHex,
				Access:   "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/data.json")}, &stdout, &stderr)

	assert.Equal(t, 0, code)

	var resp buyer.CatResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	require.NotNil(t, resp.Content, "application/json should use content field")
	assert.Equal(t, string(plaintext), *resp.Content)
	assert.Nil(t, resp.ContentBase64)
}

func TestJSON_PaidContent_PaymentRequired(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      testPubKey,
				Type:       "file",
				Path:       "/premium.pdf",
				FileSize:   5242880,
				Access:     "paid",
				PricePerKB: 100,
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "JSON payment-required should exit 0")
	assert.Empty(t, stderr.String())

	var resp buyer.CatResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	assert.True(t, resp.PaymentRequired)
	require.NotNil(t, resp.PaymentInfo)
	assert.Equal(t, uint64(100), resp.PaymentInfo.PricePerKB)
	assert.True(t, resp.PaymentInfo.Price > 0, "computed price should be positive")
}

func TestJSON_PrivateContent_ErrorJSON(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:  testPubKey,
				Type:   "file",
				Path:   "/secret.key",
				Access: "private",
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/secret.key")}, &stdout, &stderr)

	assert.Equal(t, 1, code)

	var errResp buyer.ErrorResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "private content")
	assert.Equal(t, 1, errResp.Code)
}

func TestJSON_NotFoundError(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no such path", http.StatusNotFound)
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	// Note: --json flag alone does not affect pre-access-mode errors (GetMeta failure).
	// The error is still printed to stderr for now. JSON error output only applies
	// within the access mode switch branches.
	code := run([]string{"--json", "--host", srv.URL, makeURI("/nonexistent")}, &stdout, &stderr)

	assert.Equal(t, 2, code, "not found should exit 2")
}

// ---------------------------------------------------------------------------
// --buy flag parsing / buyer.LoadConfig integration
// ---------------------------------------------------------------------------

func TestRun_BuyFlagParsing(t *testing.T) {
	// Verify that --buy without a wallet key triggers a config error
	// mentioning wallet configuration.
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      "02" + strings.Repeat("ab", 32),
				Type:       "file",
				Path:       "/file",
				Access:     "paid",
				PricePerKB: 100,
				FileSize:   1024,
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--host", srv.URL, "bitfs://02" + strings.Repeat("ab", 32) + "/file"}, &stdout, &stderr)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr.String(), "wallet")
}

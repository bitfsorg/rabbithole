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
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/buy"
	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/libbitfs-go/method42"
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
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()
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
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()
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
// outputContent error branches (free mode decryption errors)
// ---------------------------------------------------------------------------

func TestFreeContent_InvalidPNodeHex(t *testing.T) {
	// Meta returns an invalid PNode hex string — triggers hex decode error in outputContent.
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   "ZZZZ_not_hex",
				Type:    "file",
				Path:    "/bad-pnode.txt",
				KeyHash: testKeyHash("aa"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0x01, 0x02}) // some ciphertext
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, "bitfs://" + testPubKey + "/bad-pnode.txt"}, &stdout, &stderr)

	assert.NotEqual(t, 0, code, "invalid pnode hex should error")
	assert.Contains(t, stderr.String(), "invalid pnode")
}

func TestFreeContent_InvalidKeyHashHex(t *testing.T) {
	// Meta returns an invalid KeyHash hex string — triggers client-side validation error
	// which surfaces through the data fetch path (GetData rejects bad hash).
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/bad-keyhash.txt",
				KeyHash: "ZZZZ_not_valid_hex",
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0x01, 0x02})
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/bad-keyhash.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr.String(), "hash")
}

func TestFreeContent_DecryptionError(t *testing.T) {
	// Valid pnode and key_hash but ciphertext is garbage — triggers Method 42 decrypt error.
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/corrupt.txt",
				KeyHash: testKeyHash("dd"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			// Garbage ciphertext that is long enough to not be empty
			// but will fail GCM authentication.
			_, _ = w.Write(bytes.Repeat([]byte{0xDE, 0xAD}, 50))
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/corrupt.txt")}, &stdout, &stderr)

	assert.Equal(t, 5, code, "decryption failure should exit 5")
	assert.Contains(t, stderr.String(), "decrypt")
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

	var resp buy.CatResponse
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

	var resp buy.CatResponse
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

	var resp buy.CatResponse
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

	var resp buy.CatResponse
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

	var errResp buy.ErrorResponse
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
// --buy flag parsing / buy.LoadConfig integration
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Paid content — full purchase flow: outputPaidContent + outputPaidContentJSON
// ---------------------------------------------------------------------------

// paidTestSetup holds crypto material for a paid purchase test scenario.
type paidTestSetup struct {
	NodePriv        *ec.PrivateKey
	BuyerPriv       *ec.PrivateKey
	BuyerKeyHex     string
	Plaintext       []byte
	EncResult       *method42.EncryptResult
	Capsule         []byte
	CapsuleHex      string
	CapsuleHashHex  string
	KeyHashHex      string
	NodePubHex      string
	SellerAddr      string
	SellerPubKeyHex string
	UTXOFlag        string
}

// newPaidTestSetup creates key pairs, encrypts plaintext, computes capsule and
// all hex strings needed for a paid-content test.
func newPaidTestSetup(t *testing.T, plaintext []byte) *paidTestSetup {
	t.Helper()

	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	utxoTxID := strings.Repeat("ff", 32)
	return &paidTestSetup{
		NodePriv:        nodePriv,
		BuyerPriv:       buyerPriv,
		BuyerKeyHex:     hex.EncodeToString(buyerPriv.Serialize()),
		Plaintext:       plaintext,
		EncResult:       encResult,
		Capsule:         capsule,
		CapsuleHex:      hex.EncodeToString(capsule),
		CapsuleHashHex:  hex.EncodeToString(capsuleHash),
		KeyHashHex:      hex.EncodeToString(encResult.KeyHash),
		NodePubHex:      hex.EncodeToString(nodePriv.PubKey().Compressed()),
		SellerAddr:      func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }(),
		SellerPubKeyHex: hex.EncodeToString(nodePriv.PubKey().Compressed()),
		UTXOFlag:        utxoTxID + ":0:100000",
	}
}

// paidMockDaemon creates a full mock daemon for a paid purchase test, returning
// the capsule on POST /_bitfs/buy/.
func paidMockDaemon(t *testing.T, s *paidTestSetup, mimeType string) *httptest.Server {
	t.Helper()
	return newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      s.NodePubHex,
				Type:       "file",
				Path:       "/paid-file",
				MimeType:   mimeType,
				FileSize:   uint64(len(s.Plaintext)),
				Access:     "paid",
				PricePerKB: 50,
				TxID:       "invoice-test",
				KeyHash:    s.KeyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(s.EncResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash:  s.CapsuleHashHex,
					Price:        1000,
					PaymentAddr:  s.SellerAddr,
					SellerPubKey: s.SellerPubKeyHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{
				Capsule: s.CapsuleHex,
			})
		},
	)
}

func TestPaid_WithBuy_Success_TextOutput(t *testing.T) {
	// Full purchase flow in non-JSON mode — drives outputPaidContent to stdout.
	s := newPaidTestSetup(t, []byte("paid text content via outputPaidContent"))
	srv := paidMockDaemon(t, s, "text/plain")
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--buy", "--wallet-key", s.BuyerKeyHex,
		"--utxo", s.UTXOFlag,
		"--host", srv.URL,
		"bitfs://" + s.NodePubHex + "/paid-file",
	}, &stdout, &stderr)

	assert.Equal(t, 0, code, "successful paid purchase should exit 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	assert.Equal(t, s.Plaintext, stdout.Bytes(), "decrypted content should match original plaintext")
}

func TestJSON_PaidContent_WithBuy_TextMime(t *testing.T) {
	// JSON + --buy with text MIME: drives outputPaidContentJSON, Content field set.
	s := newPaidTestSetup(t, []byte("json paid text content"))
	srv := paidMockDaemon(t, s, "text/plain")
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--json", "--buy", "--wallet-key", s.BuyerKeyHex,
		"--utxo", s.UTXOFlag,
		"--host", srv.URL,
		"bitfs://" + s.NodePubHex + "/paid-file",
	}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit 0 expected; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	var resp buy.CatResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	require.NotNil(t, resp.Content, "text/plain should populate content field")
	assert.Equal(t, string(s.Plaintext), *resp.Content)
	assert.Nil(t, resp.ContentBase64, "text content should not set content_base64")
	require.NotNil(t, resp.Payment, "payment result should be present")
	assert.True(t, resp.Payment.CostSatoshis > 0, "cost should be positive")
	assert.NotEmpty(t, resp.Payment.HTLCTxID, "HTLC txid should be present")
}

func TestJSON_PaidContent_WithBuy_BinaryMime(t *testing.T) {
	// JSON + --buy with binary MIME: drives outputPaidContentJSON, ContentBase64 field set.
	binData := make([]byte, 128)
	for i := range binData {
		binData[i] = byte(i)
	}
	s := newPaidTestSetup(t, binData)
	srv := paidMockDaemon(t, s, "application/octet-stream")
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--json", "--buy", "--wallet-key", s.BuyerKeyHex,
		"--utxo", s.UTXOFlag,
		"--host", srv.URL,
		"bitfs://" + s.NodePubHex + "/paid-file",
	}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit 0 expected; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	var resp buy.CatResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	assert.Nil(t, resp.Content, "binary content should not set content field")
	require.NotNil(t, resp.ContentBase64, "binary MIME should populate content_base64")
	require.NotNil(t, resp.Payment)
}

func TestJSON_PaidContent_WithBuy_DataFetchError(t *testing.T) {
	// JSON + --buy: purchase succeeds but data endpoint returns 500.
	// This drives the data-fetch error path inside outputPaidContent with jsonOut=true.
	s := newPaidTestSetup(t, []byte("unreachable data"))
	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      s.NodePubHex,
				Type:       "file",
				Path:       "/paid-broken",
				MimeType:   "text/plain",
				FileSize:   uint64(len(s.Plaintext)),
				Access:     "paid",
				PricePerKB: 50,
				TxID:       "invoice-broken",
				KeyHash:    s.KeyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			// Data endpoint fails with 500.
			http.Error(w, "storage failure", http.StatusInternalServerError)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash:  s.CapsuleHashHex,
					Price:        1000,
					PaymentAddr:  s.SellerAddr,
					SellerPubKey: s.SellerPubKeyHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{
				Capsule: s.CapsuleHex,
			})
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--json", "--buy", "--wallet-key", s.BuyerKeyHex,
		"--utxo", s.UTXOFlag,
		"--host", srv.URL,
		"bitfs://" + s.NodePubHex + "/paid-broken",
	}, &stdout, &stderr)

	// Should output a JSON error (not crash).
	assert.NotEqual(t, 0, code, "data fetch error should produce non-zero exit")

	var errResp buy.ErrorResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &errResp), "output should be valid JSON error")
	assert.NotEmpty(t, errResp.Error, "error message should be non-empty")
	assert.True(t, errResp.Code > 0, "error code should be positive")
}

// ---------------------------------------------------------------------------
// SPV verification
// ---------------------------------------------------------------------------

func newSPVMockDaemon(t *testing.T, metaHandler func(w http.ResponseWriter, r *http.Request),
	dataHandler func(w http.ResponseWriter, r *http.Request),
	spvHandler func(w http.ResponseWriter, r *http.Request),
) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", metaHandler)
	if dataHandler != nil {
		mux.HandleFunc("/_bitfs/data/", dataHandler)
	}
	if spvHandler != nil {
		mux.HandleFunc("/_bitfs/spv/", spvHandler)
	}
	return httptest.NewServer(mux)
}

func TestVerify_Confirmed(t *testing.T) {
	plaintext := []byte("verified content")
	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newSPVMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/verified.txt",
				MimeType: "text/plain",
				FileSize: uint64(len(plaintext)),
				KeyHash:  keyHashHex,
				Access:   "free",
				TxID:     "abc123",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.SPVProofResponse{
				TxID:        "abc123",
				Confirmed:   true,
				BlockHash:   "000000000000000000",
				BlockHeight: 850000,
			})
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--verify", "--host", srv.URL, makeURI("/verified.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "verified content should exit 0; stderr: %s", stderr.String())
	assert.Contains(t, stderr.String(), "verified tx")
	assert.Contains(t, stderr.String(), "850000")
	assert.Equal(t, plaintext, stdout.Bytes())
}

func TestVerify_Unconfirmed(t *testing.T) {
	plaintext := []byte("unconfirmed content")
	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	srv := newSPVMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/unconfirmed.txt",
				MimeType: "text/plain",
				FileSize: uint64(len(plaintext)),
				KeyHash:  keyHashHex,
				Access:   "free",
				TxID:     "def456",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.SPVProofResponse{
				TxID:      "def456",
				Confirmed: false,
			})
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--verify", "--host", srv.URL, makeURI("/unconfirmed.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "unconfirmed should still exit 0")
	assert.Contains(t, stderr.String(), "unconfirmed")
	assert.Equal(t, plaintext, stdout.Bytes())
}

func TestVerify_SPVFails(t *testing.T) {
	srv := newSPVMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/spvfail.txt",
				MimeType: "text/plain",
				FileSize: 10,
				KeyHash:  testKeyHash("ff"),
				Access:   "free",
				TxID:     "bad-tx",
			})
		},
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "proof not found", http.StatusNotFound)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--verify", "--host", srv.URL, makeURI("/spvfail.txt")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "SPV failure should exit 4")
	assert.Contains(t, stderr.String(), "SPV verification failed")
}

// ---------------------------------------------------------------------------
// Unknown access mode
// ---------------------------------------------------------------------------

func TestUnknownAccessMode(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:  testPubKey,
				Type:   "file",
				Path:   "/strange.txt",
				Access: "unknown-mode",
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/strange.txt")}, &stdout, &stderr)

	assert.Equal(t, 1, code, "unknown access mode should exit 1")
	assert.Contains(t, stderr.String(), "unknown access mode")
}

func TestJSON_UnknownAccessMode(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:  testPubKey,
				Type:   "file",
				Path:   "/strange.txt",
				Access: "unknown-mode",
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/strange.txt")}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	var errResp buy.ErrorResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "unknown access mode")
}

// ---------------------------------------------------------------------------
// outputPaidContent non-JSON error branches
// ---------------------------------------------------------------------------

func TestPaid_WithBuy_MissingKeyHash(t *testing.T) {
	// Purchase succeeds but meta has empty KeyHash — hits outputPaidContent
	// empty key_hash branch (non-JSON).
	s := newPaidTestSetup(t, []byte("content"))
	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      s.NodePubHex,
				Type:       "file",
				Path:       "/paid-no-hash",
				MimeType:   "text/plain",
				FileSize:   7,
				Access:     "paid",
				PricePerKB: 50,
				TxID:       "invoice-nohash",
				KeyHash:    "", // Empty key hash
			})
		},
		nil, // no data handler needed
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash:  s.CapsuleHashHex,
					Price:        1000,
					PaymentAddr:  s.SellerAddr,
					SellerPubKey: s.SellerPubKeyHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: s.CapsuleHex})
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--buy", "--wallet-key", s.BuyerKeyHex,
		"--utxo", s.UTXOFlag,
		"--host", srv.URL,
		"bitfs://" + s.NodePubHex + "/paid-no-hash",
	}, &stdout, &stderr)

	assert.Equal(t, 1, code, "missing key hash should exit 1")
	assert.Contains(t, stderr.String(), "no content hash")
}

func TestPaid_WithBuy_DataFetchError_NonJSON(t *testing.T) {
	// Purchase succeeds but data endpoint returns 500 (non-JSON mode).
	s := newPaidTestSetup(t, []byte("data"))
	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      s.NodePubHex,
				Type:       "file",
				Path:       "/paid-datafail",
				MimeType:   "text/plain",
				FileSize:   4,
				Access:     "paid",
				PricePerKB: 50,
				TxID:       "invoice-datafail",
				KeyHash:    s.KeyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "storage failure", http.StatusInternalServerError)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash:  s.CapsuleHashHex,
					Price:        1000,
					PaymentAddr:  s.SellerAddr,
					SellerPubKey: s.SellerPubKeyHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: s.CapsuleHex})
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--buy", "--wallet-key", s.BuyerKeyHex,
		"--utxo", s.UTXOFlag,
		"--host", srv.URL,
		"bitfs://" + s.NodePubHex + "/paid-datafail",
	}, &stdout, &stderr)

	assert.Equal(t, 4, code, "data server error should exit 4")
	assert.Contains(t, stderr.String(), "server error")
}

func TestPaid_WithBuy_WriteError(t *testing.T) {
	// Full paid purchase succeeds, but stdout.Write fails (non-JSON mode).
	s := newPaidTestSetup(t, []byte("paid write-fail content"))
	srv := paidMockDaemon(t, s, "text/plain")
	defer srv.Close()

	var stderr bytes.Buffer
	code := run([]string{
		"--buy", "--wallet-key", s.BuyerKeyHex,
		"--utxo", s.UTXOFlag,
		"--host", srv.URL,
		"bitfs://" + s.NodePubHex + "/paid-file",
	}, &failWriter{}, &stderr)

	assert.NotEqual(t, 0, code, "write error should produce non-zero exit")
	assert.Contains(t, stderr.String(), "write error")
}

// ---------------------------------------------------------------------------
// --buy flag parsing / buy.LoadConfig integration
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

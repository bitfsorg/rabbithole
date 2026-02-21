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
	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/libbitfs/method42"
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
	content := []byte("Hello, BitFS world!\nSecond line.\n")

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/hello.txt",
				MimeType: "text/plain",
				FileSize: uint64(len(content)),
				KeyHash:  testKeyHash("aa"),
				Access:   "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			// Verify the data endpoint was called with the correct hash.
			assert.True(t, strings.HasSuffix(r.URL.Path, "/"+testKeyHash("aa")),
				"data request should include key_hash; got %s", r.URL.Path)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	assert.Equal(t, content, stdout.Bytes(), "stdout should contain exact file content")
}

func TestFreeContent_BinaryData(t *testing.T) {
	// Binary content with null bytes, high bytes, etc.
	content := make([]byte, 256)
	for i := range content {
		content[i] = byte(i)
	}

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "file",
				Path:     "/binary.dat",
				MimeType: "application/octet-stream",
				FileSize: uint64(len(content)),
				KeyHash:  testKeyHash("bb"),
				Access:   "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/binary.dat")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Equal(t, content, stdout.Bytes(), "binary content should be preserved exactly")
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
	assert.Contains(t, stderr.String(), "--wallet-key is required")
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
		{"not hex", "zzzz", "invalid wallet key hex"},
		{"wrong length", "aabbcc", "wallet key must be 32 or 33 bytes"},
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
	assert.Contains(t, stderr.String(), "no invoice txid")
	assert.Empty(t, stdout.String())
}

func TestPaid_WithBuy_SubmitHTLCFails(t *testing.T) {
	// Generate key pairs.
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	// Encrypt test content using buyer's pubkey so capsule-based decryption works.
	// For paid content, the daemon re-encrypts with ECDH(D_node, P_buyer).
	plaintext := []byte("paid premium content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, buyerPriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	// Compute capsule = ECDH(D_node, P_buyer).x
	capsule, err := method42.ComputeCapsule(nodePriv, buyerPriv.PubKey())
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	// Use a fake 20-byte seller address.
	sellerAddr := hex.EncodeToString(make([]byte, 20))

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      testPubKey,
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
					CapsuleHash: capsuleHashHex,
					Price:       1000,
					PaymentAddr: sellerAddr,
				})
				return
			}
			// POST: Submit HTLC fails with server error.
			http.Error(w, "payment processing failed", http.StatusInternalServerError)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "submit HTLC failure should exit 4 (server error)")
	assert.Contains(t, stderr.String(), "server error")
	assert.Empty(t, stdout.String())
}

func TestPaid_WithBuy_Success(t *testing.T) {
	// Generate key pairs.
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	// Encrypt test content using buyer's pubkey so capsule-based decryption works.
	// For paid content, the daemon re-encrypts with ECDH(D_node, P_buyer).
	plaintext := []byte("Hello, this is paid premium content!")
	encResult, err := method42.Encrypt(plaintext, nodePriv, buyerPriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	// Compute capsule = ECDH(D_node, P_buyer).x
	capsule, err := method42.ComputeCapsule(nodePriv, buyerPriv.PubKey())
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	// Use a fake 20-byte seller address.
	sellerAddr := hex.EncodeToString(make([]byte, 20))

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      testPubKey,
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
					CapsuleHash: capsuleHashHex,
					Price:       1000,
					PaymentAddr: sellerAddr,
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
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--host", srv.URL, makeURI("/premium.txt")}, &stdout, &stderr)

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
// Paymail / DNSLink not supported
// ---------------------------------------------------------------------------

func TestPaymailNotSupported(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://alice@example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "paymail should exit 6")
	assert.Contains(t, stderr.String(), "paymail/dnslink resolution not yet supported")
	assert.Empty(t, stdout.String())
}

func TestDNSLinkNotSupported(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "dnslink should exit 6")
	assert.Contains(t, stderr.String(), "paymail/dnslink resolution not yet supported")
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
	code := run([]string{"--timeout", "notaduration", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "invalid timeout")
}

// ---------------------------------------------------------------------------
// Large content streaming
// ---------------------------------------------------------------------------

func TestFreeContent_LargeFile(t *testing.T) {
	// Verify that large content is streamed correctly via io.Copy.
	size := 1024 * 64 // 64 KB
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 251) // Use prime to vary bytes
	}

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/large.bin",
				KeyHash: testKeyHash("ff"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/large.bin")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	require.Equal(t, size, stdout.Len(), "output size should match input")
	assert.Equal(t, content, stdout.Bytes())
}

// ---------------------------------------------------------------------------
// Verify data endpoint is called
// ---------------------------------------------------------------------------

func TestFreeContent_DataEndpointCalled(t *testing.T) {
	content := []byte("tracked content")
	var dataRequested bool

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/tracked.txt",
				KeyHash: testKeyHash("11"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			dataRequested = true
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		},
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/tracked.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code)
	assert.True(t, dataRequested, "data endpoint should have been called")
	assert.Equal(t, content, stdout.Bytes())
}

// ---------------------------------------------------------------------------
// Write error simulation
// ---------------------------------------------------------------------------

func TestFreeContent_WriteError(t *testing.T) {
	content := []byte("some content")

	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/werror.txt",
				KeyHash: testKeyHash("22"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
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

// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
// Free content download to default filename
// ---------------------------------------------------------------------------

func TestFreeContent_DefaultFilename(t *testing.T) {
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
			assert.True(t, strings.HasSuffix(r.URL.Path, "/"+keyHashHex),
				"data request should include key_hash; got %s", r.URL.Path)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "hello.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	assert.Contains(t, stdout.String(), "Downloaded")
	assert.Contains(t, stdout.String(), "hello.txt")

	// Verify file was created with correct decrypted contents.
	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// Download with -o custom filename
// ---------------------------------------------------------------------------

func TestFreeContent_CustomOutputFilename(t *testing.T) {
	plaintext := []byte("custom output content")

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

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "myfile.dat")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	assert.Contains(t, stdout.String(), "Downloaded")
	assert.Contains(t, stdout.String(), "myfile.dat")

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
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

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{"--buy", "--wallet-key", tt.walletKey, "--utxo", fakeUTXO, "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

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

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

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

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

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

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "premium.txt")

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

	// Provide a fake UTXO with enough funds for the purchase (price=1000 sat).
	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", outFile, "--host", srv.URL, makeURI("/premium.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "successful purchase should exit 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	assert.Contains(t, stdout.String(), "Downloaded")

	// Verify file was created with correct decrypted content.
	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data, "decrypted content should match original plaintext")
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
// --version flag
// ---------------------------------------------------------------------------

func TestVersionFlag_ReturnsExit0(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)

	assert.Equal(t, 0, code, "--version should exit 0")
	assert.Contains(t, stdout.String(), "not yet supported")
	assert.Empty(t, stderr.String())
}

// ---------------------------------------------------------------------------
// Not found
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

// ---------------------------------------------------------------------------
// Network error
// ---------------------------------------------------------------------------

func TestNetworkError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", "http://127.0.0.1:1", "--timeout", "1s", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "network error should exit 4")
	assert.Contains(t, stderr.String(), "network error")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Missing URI argument
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

// ---------------------------------------------------------------------------
// Missing key_hash in metadata
// ---------------------------------------------------------------------------

func TestMissingKeyHash(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/broken.txt",
				Access:  "free",
				KeyHash: "",
			})
		},
		nil,
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "broken.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/broken.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code, "missing key_hash should be an error")
	assert.Contains(t, stderr.String(), "no content hash")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Default filename for root path -> "download.dat"
// ---------------------------------------------------------------------------

func TestDefaultFilename_RootPath(t *testing.T) {
	plaintext := []byte("root content")

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
				Path:    "/",
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

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "download.dat")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "download.dat")

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// Binary content preserved
// ---------------------------------------------------------------------------

func TestFreeContent_BinaryData(t *testing.T) {
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

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "binary.dat")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/binary.dat")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data, "binary content should be preserved exactly")
}

// ---------------------------------------------------------------------------
// Server error on data endpoint
// ---------------------------------------------------------------------------

func TestDataEndpoint_ServerError(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/fail.txt",
				KeyHash: testKeyHash("ee"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "storage failure", http.StatusInternalServerError)
		},
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "fail.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/fail.txt")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "data server error should exit 4")
	assert.Contains(t, stderr.String(), "server error")
}

// ---------------------------------------------------------------------------
// Data endpoint not found
// ---------------------------------------------------------------------------

func TestDataEndpoint_NotFound(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/missing-data.txt",
				KeyHash: testKeyHash("ff"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		},
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "missing.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/missing-data.txt")}, &stdout, &stderr)

	assert.Equal(t, 2, code, "data not found should exit 2")
	assert.Contains(t, stderr.String(), "not found")
}

// ---------------------------------------------------------------------------
// Invalid URI
// ---------------------------------------------------------------------------

func TestInvalidURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"http://not-a-bitfs-uri"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "invalid URI should exit 6")
	assert.Contains(t, stderr.String(), "bget:")
	assert.Empty(t, stdout.String())
}

func TestEmptyURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{""}, &stdout, &stderr)

	assert.Equal(t, 6, code, "empty URI should exit 6")
}

// ---------------------------------------------------------------------------
// --output long form flag
// ---------------------------------------------------------------------------

func TestOutputLongFlag(t *testing.T) {
	plaintext := []byte("long flag content")

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
				Path:    "/doc.txt",
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

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "longflag.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--output", outFile, "--host", srv.URL, makeURI("/doc.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
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
// Invalid timeout
// ---------------------------------------------------------------------------

func TestInvalidTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--timeout", "notaduration", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "invalid timeout")
}

// ---------------------------------------------------------------------------
// Unknown flag
// ---------------------------------------------------------------------------

func TestUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--unknown", makeURI("/")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
}

// ---------------------------------------------------------------------------
// Large file streaming
// ---------------------------------------------------------------------------

func TestFreeContent_LargeFile(t *testing.T) {
	size := 1024 * 64 // 64 KB
	plaintext := make([]byte, size)
	for i := range plaintext {
		plaintext[i] = byte(i % 251)
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

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "large.bin")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/large.bin")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	require.Equal(t, size, len(data), "output size should match input")
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// Byte count in success message
// ---------------------------------------------------------------------------

func TestFreeContent_ByteCountInMessage(t *testing.T) {
	plaintext := []byte("12345678901234567890") // 20 bytes

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
				Path:    "/count.txt",
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

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "count.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/count.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "Downloaded 20 bytes")
}

// ---------------------------------------------------------------------------
// Decrypt failure — invalid ciphertext should not leave partial file on disk
// ---------------------------------------------------------------------------

func TestDecryptFailure_NoPartialFile(t *testing.T) {
	// Serve garbage data that cannot be decrypted — decryption should fail
	// and no file should be left on disk.
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/corrupt.bin",
				KeyHash: testKeyHash("44"),
				Access:  "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			// Write garbage that will fail AES-GCM decryption.
			_, _ = w.Write([]byte("this is not valid ciphertext at all"))
		},
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "corrupt.bin")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", outFile, "--host", srv.URL, makeURI("/corrupt.bin")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code, "decrypt failure should return non-zero exit code")
	assert.Contains(t, stderr.String(), "decrypt",
		"stderr should contain decrypt error message")

	// The critical assertion: no file should be created on decrypt failure.
	_, statErr := os.Stat(outFile)
	assert.True(t, os.IsNotExist(statErr),
		"no file should be left on disk after decrypt failure, but got: %v", statErr)
}

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
		{"not hex", "zzzz", "invalid wallet key"},
		{"wrong length", "aabbcc", "expected 32 bytes"},
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
	// Use the node's pubkey hash as seller address.
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()
	sellerPubKeyHex := nodePubHex

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
	// Use the node's pubkey as PNode and seller address.
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()
	sellerPubKeyHex := nodePubHex

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "premium.txt")

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

func TestVersionFlag_DownloadsSpecificVersion(t *testing.T) {
	plaintext := []byte("Hello, BitFS version 2!")

	// Encrypt with Method 42 AccessFree.
	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:    testPubKey,
			Type:     "file",
			Path:     "/hello.txt",
			MimeType: "text/plain",
			FileSize: uint64(len(plaintext)),
			KeyHash:  keyHashHex,
			Access:   "free",
			TxID:     "latest-txid",
		})
	})
	mux.HandleFunc("/_bitfs/versions/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, []client.VersionEntry{
			{Version: 1, TxID: "latest-txid", FileSize: uint64(len(plaintext)), Access: "free"},
			{Version: 2, TxID: "older-txid", FileSize: 100, Access: "free"},
		})
	})
	mux.HandleFunc("/_bitfs/data/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(encResult.Ciphertext)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "hello.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--version", "2", "-o", outFile, "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())
	assert.Contains(t, stdout.String(), "Downloaded")

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

func TestVersionFlag_OutOfRange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:   testPubKey,
			Type:    "file",
			Path:    "/hello.txt",
			KeyHash: testKeyHash("aa"),
			Access:  "free",
		})
	})
	mux.HandleFunc("/_bitfs/versions/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, []client.VersionEntry{
			{Version: 1, TxID: "only-txid", FileSize: 10, Access: "free"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--version", "5", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 2, code, "out-of-range version should exit 2")
	assert.Contains(t, stderr.String(), "version 5 not found")
	assert.Contains(t, stderr.String(), "only 1 versions")
}

func TestVersionFlag_Zero_NoVersionLookup(t *testing.T) {
	// --version 0 (default) should behave as if --version was not specified.
	plaintext := []byte("no version lookup")

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	versionsCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode:   testPubKey,
			Type:    "file",
			Path:    "/hello.txt",
			KeyHash: keyHashHex,
			Access:  "free",
		})
	})
	mux.HandleFunc("/_bitfs/versions/", func(w http.ResponseWriter, r *http.Request) {
		versionsCalled = true
		serveJSON(w, []client.VersionEntry{})
	})
	mux.HandleFunc("/_bitfs/data/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(encResult.Ciphertext)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "hello.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--version", "0", "-o", outFile, "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.False(t, versionsCalled, "versions endpoint should not be called when --version is 0")
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
// Paymail / DNSLink resolution errors
// ---------------------------------------------------------------------------

func TestPaymailResolveFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://alice@example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "paymail resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "bget:")
	assert.Empty(t, stdout.String())
}

func TestDNSLinkResolveFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bitfs://example.com/docs"}, &stdout, &stderr)

	assert.Equal(t, 6, code, "dnslink resolve failure should exit 6")
	assert.Contains(t, stderr.String(), "bget:")
}

func TestPubKeyNoHost_RequiresHostFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{makeURI("/docs")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "--host")
	assert.Empty(t, stdout.String())
}

// ---------------------------------------------------------------------------
// Invalid timeout
// ---------------------------------------------------------------------------

func TestInvalidTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", "http://localhost:8080", "--timeout", "notaduration", makeURI("/")}, &stdout, &stderr)

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

// ===========================================================================
// --json flag tests
// ===========================================================================

func TestJSON_FlagParsing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", "http://localhost:1"}, &stdout, &stderr)
	assert.Equal(t, 6, code, "missing URI should still exit 6")
}

func TestJSON_MissingURI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json"}, &stdout, &stderr)
	assert.Equal(t, 6, code)
}

func TestJSON_FreeContent_Success(t *testing.T) {
	plaintext := []byte("Hello, JSON output!")

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
	outFile := filepath.Join(tmpDir, "hello.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "-o", outFile, "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Empty(t, stderr.String())

	// Parse JSON output.
	var resp buy.GetResponse
	err = json.Unmarshal(stdout.Bytes(), &resp)
	require.NoError(t, err, "stdout should be valid JSON: %s", stdout.String())

	assert.NotNil(t, resp.Meta)
	assert.Equal(t, "free", resp.Meta.Access)
	assert.Equal(t, outFile, resp.OutputPath)
	assert.Equal(t, int64(len(plaintext)), resp.BytesWritten)
	assert.False(t, resp.PaymentRequired)

	// Verify file was actually written.
	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
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

	assert.Equal(t, 0, code, "JSON payment required should exit 0")

	var resp buy.GetResponse
	err := json.Unmarshal(stdout.Bytes(), &resp)
	require.NoError(t, err, "stdout should be valid JSON: %s", stdout.String())

	assert.True(t, resp.PaymentRequired)
	assert.NotNil(t, resp.PaymentInfo)
	assert.Equal(t, uint64(100), resp.PaymentInfo.PricePerKB)
	assert.True(t, resp.PaymentInfo.Price > 0, "total price should be computed")
}

func TestJSON_PrivateContent_Error(t *testing.T) {
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

	assert.Equal(t, 1, code, "private content JSON should exit 1")

	var resp buy.ErrorResponse
	err := json.Unmarshal(stdout.Bytes(), &resp)
	require.NoError(t, err, "stdout should be valid JSON: %s", stdout.String())

	assert.Contains(t, resp.Error, "private content")
	assert.Equal(t, 1, resp.Code)
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
	code := run([]string{"--json", "--host", srv.URL, makeURI("/nonexistent")}, &stdout, &stderr)

	assert.Equal(t, 2, code, "not found should exit 2")

	var resp buy.ErrorResponse
	err := json.Unmarshal(stdout.Bytes(), &resp)
	require.NoError(t, err, "stdout should be valid JSON: %s", stdout.String())

	assert.Equal(t, "not found", resp.Error)
	assert.Equal(t, 2, resp.Code)
}

func TestJSON_DirectoryError(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:  testPubKey,
				Type:   "dir",
				Path:   "/docs",
				Access: "free",
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/docs")}, &stdout, &stderr)

	assert.Equal(t, 1, code, "directory JSON should exit 1")

	var resp buy.ErrorResponse
	err := json.Unmarshal(stdout.Bytes(), &resp)
	require.NoError(t, err, "stdout should be valid JSON: %s", stdout.String())

	assert.Contains(t, resp.Error, "directory")
}

// ---------------------------------------------------------------------------
// deriveFilename edge cases
// ---------------------------------------------------------------------------

func TestDeriveFilename_DotPath(t *testing.T) {
	assert.Equal(t, "download.dat", deriveFilename("."))
}

func TestDeriveFilename_Empty(t *testing.T) {
	assert.Equal(t, "download.dat", deriveFilename(""))
}

// ---------------------------------------------------------------------------
// JSON error paths for free content
// ---------------------------------------------------------------------------

func TestJSON_FreeContent_MissingKeyHash(t *testing.T) {
	// Meta returns Access="free" but KeyHash=""
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode: testPubKey, Type: "file", Path: "/file.txt",
			Access: "free", KeyHash: "",
		})
	}, nil)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/file.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "no content hash")
}

func TestJSON_FreeContent_DataServerError(t *testing.T) {
	// Meta succeeds, data endpoint returns 500
	keyHash := testKeyHash("ff")
	srv := newMockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode: testPubKey, Type: "file", Path: "/file.txt",
			Access: "free", KeyHash: keyHash,
		})
	}, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/file.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "error")
}

// ---------------------------------------------------------------------------
// JSON paid content with --buy
// ---------------------------------------------------------------------------

func TestJSON_PaidContent_WithBuy_Success(t *testing.T) {
	// Full paid+JSON flow: --json --buy
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	plaintext := []byte("Paid JSON content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "paid.txt")

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode: nodePubHex, Type: "file", Path: "/paid.txt",
				MimeType: "text/plain", FileSize: uint64(len(plaintext)),
				Access: "paid", PricePerKB: 50, TxID: "invoice123", KeyHash: keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash: capsuleHashHex, Price: 1000,
					PaymentAddr: sellerAddr, SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: capsuleHex})
		},
	)
	defer srv.Close()

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", outFile, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "stderr: %s", stderr.String())

	// Parse JSON output.
	var resp buy.GetResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	assert.NotNil(t, resp.Payment)
	assert.Equal(t, outFile, resp.OutputPath)
}

func TestJSON_PaidContent_WithBuy_DataFetchError(t *testing.T) {
	// Purchase succeeds but data endpoint fails
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	plaintext := []byte("content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "paid.txt")

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode: nodePubHex, Type: "file", Path: "/paid.txt",
				Access: "paid", PricePerKB: 50, TxID: "invoice123", KeyHash: keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash: capsuleHashHex, Price: 1000,
					PaymentAddr: sellerAddr, SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: capsuleHex})
		},
	)
	defer srv.Close()

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", outFile, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
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
				Path:   "/weird.txt",
				Access: "restricted",
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/weird.txt")}, &stdout, &stderr)

	assert.Equal(t, 1, code, "unknown access mode should exit 1")
	assert.Contains(t, stderr.String(), "unknown access mode")
	assert.Contains(t, stderr.String(), "restricted")
}

func TestJSON_UnknownAccessMode(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:  testPubKey,
				Type:   "file",
				Path:   "/weird.txt",
				Access: "restricted",
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/weird.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "unknown access mode")
}

// ---------------------------------------------------------------------------
// downloadContent without -o flag (uses deriveFilename)
// ---------------------------------------------------------------------------

func TestFreeContent_NoOutputFlag_DeriveFilename(t *testing.T) {
	plaintext := []byte("auto name content")

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
				Path:    "/subdir/autoname.txt",
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

	// Run in a temp directory to avoid polluting the working directory.
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/subdir/autoname.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "autoname.txt")

	// Verify the file was created with the derived name.
	data, err := os.ReadFile(filepath.Join(tmpDir, "autoname.txt"))
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// JSON downloadContentJSON with auto-derived filename (no -o)
// ---------------------------------------------------------------------------

func TestJSON_FreeContent_NoOutputFlag_DeriveFilename(t *testing.T) {
	plaintext := []byte("json auto name")

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
				Path:    "/jsonauto.dat",
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
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/jsonauto.dat")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "exit code should be 0; stderr: %s", stderr.String())

	var resp buy.GetResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	assert.Contains(t, resp.OutputPath, "jsonauto.dat")
}

// ---------------------------------------------------------------------------
// JSON paid config error paths
// ---------------------------------------------------------------------------

func TestJSON_PaidContent_WithBuy_MissingWalletKey(t *testing.T) {
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
	code := run([]string{"--json", "--buy", "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "error")
}

func TestJSON_PaidContent_WithBuy_PurchaseFails(t *testing.T) {
	// Valid wallet key but missing TxID causes purchase to fail.
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
	code := run([]string{"--json", "--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "--host", srv.URL, makeURI("/premium.pdf")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "error")
}

// ---------------------------------------------------------------------------
// Paid content: non-JSON data fetch error (downloadPaidContent non-JSON branch)
// ---------------------------------------------------------------------------

func TestPaid_WithBuy_DataFetchError(t *testing.T) {
	// Purchase succeeds but data endpoint returns 500 (non-JSON mode).
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	plaintext := []byte("content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode: nodePubHex, Type: "file", Path: "/paid.txt",
				Access: "paid", PricePerKB: 50, TxID: "invoice123", KeyHash: keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash: capsuleHashHex, Price: 1000,
					PaymentAddr: sellerAddr, SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: capsuleHex})
		},
	)
	defer srv.Close()

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr.String(), "server error")
}

// ---------------------------------------------------------------------------
// Paid content: no -o flag derives filename
// ---------------------------------------------------------------------------

func TestPaid_WithBuy_NoOutputFlag(t *testing.T) {
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	plaintext := []byte("paid no -o flag content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode: nodePubHex, Type: "file", Path: "/autoname-paid.txt",
				MimeType: "text/plain", FileSize: uint64(len(plaintext)),
				Access: "paid", PricePerKB: 50, TxID: "invoice123", KeyHash: keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash: capsuleHashHex, Price: 1000,
					PaymentAddr: sellerAddr, SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: capsuleHex})
		},
	)
	defer srv.Close()

	// Run in temp dir to avoid polluting cwd.
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "--host", srv.URL, makeURI("/autoname-paid.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "autoname-paid.txt")

	data, err := os.ReadFile(filepath.Join(tmpDir, "autoname-paid.txt"))
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// Paid content: decrypt failure (garbage ciphertext)
// ---------------------------------------------------------------------------

func TestPaid_WithBuy_DecryptFailure(t *testing.T) {
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	plaintext := []byte("content to encrypt")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "paid.txt")

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode: nodePubHex, Type: "file", Path: "/paid.txt",
				Access: "paid", PricePerKB: 50, TxID: "invoice123", KeyHash: keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			// Return garbage ciphertext that will fail decryption.
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("this is not valid ciphertext"))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash: capsuleHashHex, Price: 1000,
					PaymentAddr: sellerAddr, SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: capsuleHex})
		},
	)
	defer srv.Close()

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	var stdout, stderr bytes.Buffer
	code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", outFile, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)

	assert.Equal(t, 5, code, "decrypt failure should exit 5")
	assert.Contains(t, stderr.String(), "decrypt")
}

func TestJSON_PaidContent_WithBuy_DecryptFailure(t *testing.T) {
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	plaintext := []byte("content to encrypt")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "paid.txt")

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode: nodePubHex, Type: "file", Path: "/paid.txt",
				Access: "paid", PricePerKB: 50, TxID: "invoice123", KeyHash: keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("this is not valid ciphertext"))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash: capsuleHashHex, Price: 1000,
					PaymentAddr: sellerAddr, SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: capsuleHex})
		},
	)
	defer srv.Close()

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", outFile, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "decrypt")
}

// ---------------------------------------------------------------------------
// JSON: decrypt failure for free content
// ---------------------------------------------------------------------------

func TestJSON_FreeContent_DecryptFailure(t *testing.T) {
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
			_, _ = w.Write([]byte("this is not valid ciphertext at all"))
		},
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "corrupt.bin")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "-o", outFile, "--host", srv.URL, makeURI("/corrupt.bin")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "decrypt")
}

// ---------------------------------------------------------------------------
// Paid content: invalid PNode key bytes (valid hex but not a valid EC point)
// ---------------------------------------------------------------------------

func TestPaid_WithBuy_InvalidPNodeKey(t *testing.T) {
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	plaintext := []byte("content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	// Use valid hex that is 33 bytes but not a valid compressed pubkey.
	badPNodeHex := "02" + strings.Repeat("ff", 32) // valid hex, invalid EC point

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "paid.txt")

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode: badPNodeHex, Type: "file", Path: "/paid.txt",
				Access: "paid", PricePerKB: 50, TxID: "invoice123", KeyHash: keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash: capsuleHashHex, Price: 1000,
					PaymentAddr: sellerAddr, SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: capsuleHex})
		},
	)
	defer srv.Close()

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	t.Run("non-JSON", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", outFile, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)
		assert.NotEqual(t, 0, code)
	})

	t.Run("JSON", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--json", "--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", outFile, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)
		assert.NotEqual(t, 0, code)
	})
}

// ---------------------------------------------------------------------------
// SPV verification (--verify flag)
// ---------------------------------------------------------------------------

func TestVerifyFlag_SPVSuccess_Confirmed(t *testing.T) {
	plaintext := []byte("verified content")

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode: testPubKey, Type: "file", Path: "/verified.txt",
			KeyHash: keyHashHex, Access: "free", TxID: "abc123txid",
		})
	})
	mux.HandleFunc("/_bitfs/spv/proof/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.SPVProofResponse{
			TxID: "abc123txid", Confirmed: true, BlockHeight: 12345,
		})
	})
	mux.HandleFunc("/_bitfs/data/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(encResult.Ciphertext)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "verified.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--verify", "-o", outFile, "--host", srv.URL, makeURI("/verified.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Contains(t, stderr.String(), "verified tx")
	assert.Contains(t, stderr.String(), "12345")
}

func TestVerifyFlag_SPVSuccess_Unconfirmed(t *testing.T) {
	plaintext := []byte("unconfirmed content")

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode: testPubKey, Type: "file", Path: "/unconfirmed.txt",
			KeyHash: keyHashHex, Access: "free", TxID: "pending-txid",
		})
	})
	mux.HandleFunc("/_bitfs/spv/proof/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.SPVProofResponse{
			TxID: "pending-txid", Confirmed: false,
		})
	})
	mux.HandleFunc("/_bitfs/data/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(encResult.Ciphertext)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "unconfirmed.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--verify", "-o", outFile, "--host", srv.URL, makeURI("/unconfirmed.txt")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Contains(t, stderr.String(), "unconfirmed")
}

func TestVerifyFlag_SPVFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode: testPubKey, Type: "file", Path: "/bad-spv.txt",
			KeyHash: testKeyHash("aa"), Access: "free", TxID: "bad-txid",
		})
	})
	mux.HandleFunc("/_bitfs/spv/proof/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "verification failed", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--verify", "--host", srv.URL, makeURI("/bad-spv.txt")}, &stdout, &stderr)

	assert.Equal(t, 4, code, "SPV failure should exit 4")
	assert.Contains(t, stderr.String(), "SPV verification failed")
}

// ---------------------------------------------------------------------------
// Version error with JSON output
// ---------------------------------------------------------------------------

func TestJSON_VersionOutOfRange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode: testPubKey, Type: "file", Path: "/hello.txt",
			KeyHash: testKeyHash("aa"), Access: "free",
		})
	})
	mux.HandleFunc("/_bitfs/versions/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, []client.VersionEntry{
			{Version: 1, TxID: "only-txid", FileSize: 10, Access: "free"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--version", "5", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "version 5 not found")
}

func TestJSON_VersionEndpointError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode: testPubKey, Type: "file", Path: "/hello.txt",
			KeyHash: testKeyHash("aa"), Access: "free",
		})
	})
	mux.HandleFunc("/_bitfs/versions/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--version", "1", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "error")
}

// ---------------------------------------------------------------------------
// File creation error (output path in non-existent directory)
// ---------------------------------------------------------------------------

func TestFreeContent_CreateFileError(t *testing.T) {
	plaintext := []byte("content")

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
				PNode: testPubKey, Type: "file", Path: "/file.txt",
				KeyHash: keyHashHex, Access: "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	// Output path points to non-existent directory — os.Create will fail.
	badPath := filepath.Join(t.TempDir(), "nonexistent", "subdir", "file.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-o", badPath, "--host", srv.URL, makeURI("/file.txt")}, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "cannot create file")
}

func TestJSON_FreeContent_CreateFileError(t *testing.T) {
	plaintext := []byte("content")

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
				PNode: testPubKey, Type: "file", Path: "/file.txt",
				KeyHash: keyHashHex, Access: "free",
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	badPath := filepath.Join(t.TempDir(), "nonexistent", "subdir", "file.txt")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "-o", badPath, "--host", srv.URL, makeURI("/file.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stdout.String(), "cannot create file")
}

func TestPaid_WithBuy_CreateFileError(t *testing.T) {
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	plaintext := []byte("paid content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode: nodePubHex, Type: "file", Path: "/paid.txt",
				MimeType: "text/plain", FileSize: uint64(len(plaintext)),
				Access: "paid", PricePerKB: 50, TxID: "invoice123", KeyHash: keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash: capsuleHashHex, Price: 1000,
					PaymentAddr: sellerAddr, SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: capsuleHex})
		},
	)
	defer srv.Close()

	badPath := filepath.Join(t.TempDir(), "nonexistent", "subdir", "paid.txt")
	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	t.Run("non-JSON", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", badPath, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)
		assert.Equal(t, 1, code)
		assert.Contains(t, stderr.String(), "cannot create file")
	})

	t.Run("JSON", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--json", "--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", badPath, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)
		assert.NotEqual(t, 0, code)
		assert.Contains(t, stdout.String(), "error")
	})
}

func TestVersionEndpointError_NonJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/meta/", func(w http.ResponseWriter, r *http.Request) {
		serveJSON(w, client.MetaResponse{
			PNode: testPubKey, Type: "file", Path: "/hello.txt",
			KeyHash: testKeyHash("aa"), Access: "free",
		})
	})
	mux.HandleFunc("/_bitfs/versions/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--version", "1", "--host", srv.URL, makeURI("/hello.txt")}, &stdout, &stderr)

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr.String(), "server error")
}

// ---------------------------------------------------------------------------
// Paid content: invalid PNode hex (non-JSON and JSON)
// ---------------------------------------------------------------------------

func TestPaid_WithBuy_InvalidPNodeHex(t *testing.T) {
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	plaintext := []byte("content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "paid.txt")

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode: "zzzz-bad-hex", Type: "file", Path: "/paid.txt",
				Access: "paid", PricePerKB: 50, TxID: "invoice123", KeyHash: keyHashHex,
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash: capsuleHashHex, Price: 1000,
					PaymentAddr: sellerAddr, SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{Capsule: capsuleHex})
		},
	)
	defer srv.Close()

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	t.Run("non-JSON", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", outFile, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)
		assert.Equal(t, 5, code)
		assert.Contains(t, stderr.String(), "invalid pnode hex")
	})

	t.Run("JSON", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--json", "--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO, "-o", outFile, "--host", srv.URL, makeURI("/paid.txt")}, &stdout, &stderr)
		assert.NotEqual(t, 0, code)
		assert.Contains(t, stdout.String(), "invalid pnode hex")
	})
}

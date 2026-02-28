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
	"sync/atomic"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/buy"
	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/x402"
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

// ---------------------------------------------------------------------------
// Original tests (kept as-is)
// ---------------------------------------------------------------------------

func TestRun_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "Usage")
}

func TestRun_JSONFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json"}, &stdout, &stderr)
	assert.Equal(t, 6, code)
}

// ---------------------------------------------------------------------------
// Test 1: Not a directory
// ---------------------------------------------------------------------------

func TestRun_NotADirectory(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/readme.txt",
				KeyHash: testKeyHash("01"),
				Access:  "free",
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/readme.txt")}, &stdout, &stderr)

	assert.Equal(t, 6, code, "file target should exit 6")
	assert.Contains(t, stderr.String(), "not a directory")
}

// ---------------------------------------------------------------------------
// Test 2: Empty directory (text output)
// ---------------------------------------------------------------------------

func TestRun_EmptyDirectory_Text(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "dir",
				Path:     "/empty",
				Access:   "free",
				Children: []client.ChildEntry{},
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/empty")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "empty dir should exit 0")
	assert.Contains(t, stdout.String(), "no files in directory")
}

// ---------------------------------------------------------------------------
// Test 3: Empty directory (JSON output)
// ---------------------------------------------------------------------------

func TestRun_EmptyDirectory_JSON(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:    testPubKey,
				Type:     "dir",
				Path:     "/empty",
				Access:   "free",
				Children: []client.ChildEntry{},
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/empty")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "empty dir JSON should exit 0")

	var resp buy.BatchGetResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	assert.Equal(t, 0, resp.Total)
	assert.Equal(t, 0, resp.Succeeded)
	assert.Equal(t, 0, resp.Failed)
	assert.Empty(t, resp.Files)
}

// ---------------------------------------------------------------------------
// Test 4: Free single file download
// ---------------------------------------------------------------------------

func TestRun_FreeSingleFile(t *testing.T) {
	plaintext := []byte("Hello, BitFS batch download!")

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	var metaCalls int32
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			call := atomic.AddInt32(&metaCalls, 1)
			if call == 1 {
				// First call: directory listing
				serveJSON(w, client.MetaResponse{
					PNode:  testPubKey,
					Type:   "dir",
					Path:   "/docs",
					Access: "free",
					Children: []client.ChildEntry{
						{Name: "readme.txt", Type: "file"},
						{Name: "subdir", Type: "dir"}, // should be filtered out
					},
				})
			} else {
				// Second call: file metadata
				serveJSON(w, client.MetaResponse{
					PNode:    testPubKey,
					Type:     "file",
					Path:     "/docs/readme.txt",
					MimeType: "text/plain",
					FileSize: uint64(len(plaintext)),
					KeyHash:  keyHashHex,
					Access:   "free",
				})
			}
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	tmpDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/docs"), tmpDir}, &stdout, &stderr)

	assert.Equal(t, 0, code, "single free file should exit 0; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "readme.txt")
	assert.Contains(t, stdout.String(), "1/1 files downloaded")

	// Verify the file was written to disk with correct content.
	data, err := os.ReadFile(filepath.Join(tmpDir, "readme.txt"))
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// Test 5: Invalid timeout flag
// ---------------------------------------------------------------------------

func TestRun_InvalidTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", "http://localhost:8080", "--timeout", "notaduration", makeURI("/docs")}, &stdout, &stderr)

	assert.Equal(t, 6, code)
	assert.Contains(t, stderr.String(), "invalid timeout")
}

// ---------------------------------------------------------------------------
// Test 6: Fail-fast mode
// ---------------------------------------------------------------------------

func TestRun_FailFast(t *testing.T) {
	// Directory with 2 files; first file's data returns 500, triggering fail-fast.
	var metaCalls int32
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			call := atomic.AddInt32(&metaCalls, 1)
			if call == 1 {
				// Directory listing
				serveJSON(w, client.MetaResponse{
					PNode:  testPubKey,
					Type:   "dir",
					Path:   "/data",
					Access: "free",
					Children: []client.ChildEntry{
						{Name: "a.txt", Type: "file"},
						{Name: "b.txt", Type: "file"},
					},
				})
			} else {
				// Individual file metadata
				serveJSON(w, client.MetaResponse{
					PNode:   testPubKey,
					Type:    "file",
					Path:    r.URL.Path,
					KeyHash: testKeyHash("ff"),
					Access:  "free",
				})
			}
		},
		func(w http.ResponseWriter, r *http.Request) {
			// All data requests fail with 500
			http.Error(w, "storage failure", http.StatusInternalServerError)
		},
	)
	defer srv.Close()

	tmpDir := t.TempDir()

	var stdout, stderr bytes.Buffer
	// Use concurrency=1 to ensure deterministic ordering (a.txt fails first, b.txt skipped).
	code := run([]string{"--fail-fast", "--concurrency", "1", "--host", srv.URL, makeURI("/data"), tmpDir}, &stdout, &stderr)

	assert.NotEqual(t, 0, code, "fail-fast with errors should exit non-zero")
	assert.Contains(t, stderr.String(), "FAIL")
}

// ---------------------------------------------------------------------------
// Test 7: downloadFile - free mode (unit test)
// ---------------------------------------------------------------------------

func TestDownloadFile_FreeMode(t *testing.T) {
	plaintext := []byte("free file content for unit test")

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	var metaCalls int32
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&metaCalls, 1)
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/test.txt",
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

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "test.txt")

	entry := downloadFile(c, c, testPubKey, "/test.txt", localPath, false, nil)

	assert.Empty(t, entry.Error, "downloadFile should succeed")
	assert.Equal(t, int64(len(plaintext)), entry.BytesWritten)
	assert.Equal(t, localPath, entry.OutputPath)

	// Verify written file.
	data, err := os.ReadFile(localPath)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// Test 8: downloadFile - paid without buy enabled
// ---------------------------------------------------------------------------

func TestDownloadFile_PaidNoConfig(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      testPubKey,
				Type:       "file",
				Path:       "/premium.pdf",
				Access:     "paid",
				PricePerKB: 200,
			})
		},
		nil,
	)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "premium.pdf")

	entry := downloadFile(c, c, testPubKey, "/premium.pdf", localPath, false, nil)

	assert.Contains(t, entry.Error, "payment required")
	assert.Equal(t, 5, entry.Code)
}

// ---------------------------------------------------------------------------
// Test 9: downloadFile - private content
// ---------------------------------------------------------------------------

func TestDownloadFile_Private(t *testing.T) {
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

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "secret.key")

	entry := downloadFile(c, c, testPubKey, "/secret.key", localPath, false, nil)

	assert.Contains(t, entry.Error, "private content")
	assert.Equal(t, 6, entry.Code)
}

// ---------------------------------------------------------------------------
// Test 10: downloadFreeFile - success with real encryption
// ---------------------------------------------------------------------------

func TestDownloadFreeFile_Success(t *testing.T) {
	plaintext := []byte("Method42 free-mode encrypted content, successfully decrypted!")

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	// downloadFreeFile only uses the data endpoint, so set up a server with just that.
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/data/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(encResult.Ciphertext)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "decrypted.txt")

	meta := &client.MetaResponse{
		PNode:   testPubKey,
		Type:    "file",
		Path:    "/decrypted.txt",
		KeyHash: keyHashHex,
		Access:  "free",
	}

	entry := downloadFreeFile(c, meta, localPath)

	assert.Empty(t, entry.Error, "downloadFreeFile should succeed")
	assert.Equal(t, int64(len(plaintext)), entry.BytesWritten)
	assert.Equal(t, localPath, entry.OutputPath)

	// Verify written file matches plaintext.
	data, err := os.ReadFile(localPath)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// Test 11: downloadFreeFile - no key hash
// ---------------------------------------------------------------------------

func TestDownloadFreeFile_NoKeyHash(t *testing.T) {
	// downloadFreeFile doesn't call any HTTP endpoints when KeyHash is empty,
	// but we still need a valid client.
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "nokey.txt")

	meta := &client.MetaResponse{
		PNode:   testPubKey,
		Type:    "file",
		Path:    "/nokey.txt",
		KeyHash: "", // empty
		Access:  "free",
	}

	entry := downloadFreeFile(c, meta, localPath)

	assert.Contains(t, entry.Error, "no content hash")
	assert.Equal(t, 1, entry.Code)
}

// ---------------------------------------------------------------------------
// Test 12: writeFile - success
// ---------------------------------------------------------------------------

func TestWriteFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "subdir", "output.txt")
	content := []byte("writeFile test content 12345")

	n, err := writeFile(filePath, content)

	require.NoError(t, err)
	assert.Equal(t, len(content), n)

	// Read back and verify.
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

// ---------------------------------------------------------------------------
// Test 13: writeJSON - success
// ---------------------------------------------------------------------------

func TestWriteJSON_Success(t *testing.T) {
	resp := &buy.BatchGetResponse{
		Total:     3,
		Succeeded: 2,
		Failed:    1,
		Files: []buy.BatchFileEntry{
			{Path: "a.txt", OutputPath: "/tmp/a.txt", BytesWritten: 100},
			{Path: "b.txt", OutputPath: "/tmp/b.txt", BytesWritten: 200},
			{Path: "c.txt", Error: "server error", Code: 4},
		},
	}

	var stdout, stderr bytes.Buffer
	code := writeJSON(resp, &stdout, &stderr)

	assert.Equal(t, 0, code)
	assert.Empty(t, stderr.String())

	// Verify JSON roundtrip.
	var decoded buy.BatchGetResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	assert.Equal(t, 3, decoded.Total)
	assert.Equal(t, 2, decoded.Succeeded)
	assert.Equal(t, 1, decoded.Failed)
	assert.Len(t, decoded.Files, 3)
	assert.Equal(t, "a.txt", decoded.Files[0].Path)
	assert.Equal(t, int64(100), decoded.Files[0].BytesWritten)
	assert.Equal(t, "c.txt", decoded.Files[2].Path)
	assert.Contains(t, decoded.Files[2].Error, "server error")
}

// ---------------------------------------------------------------------------
// Test 14: handleErrorJSON
// ---------------------------------------------------------------------------

func TestHandleErrorJSON(t *testing.T) {
	var stdout bytes.Buffer
	err := json.NewEncoder(&stdout).Encode(nil)
	_ = err
	stdout.Reset()

	testErr := client.ErrNotFound
	code := handleErrorJSON(testErr, &stdout)

	assert.Equal(t, 2, code, "not-found should map to exit code 2")

	var errResp buy.ErrorResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "not found")
	assert.Equal(t, 2, errResp.Code)
}

func TestHandleErrorJSON_GenericError(t *testing.T) {
	var stdout bytes.Buffer

	testErr := assert.AnError // generic error
	code := handleErrorJSON(testErr, &stdout)

	assert.Equal(t, 1, code, "generic error should map to exit code 1")

	var errResp buy.ErrorResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &errResp))
	assert.NotEmpty(t, errResp.Error)
	assert.Equal(t, 1, errResp.Code)
}

// ---------------------------------------------------------------------------
// Test: Not-a-directory with JSON output
// ---------------------------------------------------------------------------

func TestRun_NotADirectory_JSON(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/readme.txt",
				KeyHash: testKeyHash("02"),
				Access:  "free",
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "--host", srv.URL, makeURI("/readme.txt")}, &stdout, &stderr)

	// handleErrorJSON maps generic errors to exit code 1 (not 6 like text mode).
	assert.Equal(t, 1, code)

	var errResp buy.ErrorResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &errResp))
	assert.Contains(t, errResp.Error, "not a directory")
}

// ---------------------------------------------------------------------------
// Test: Directory with only subdirectories (no files)
// ---------------------------------------------------------------------------

func TestRun_DirWithOnlySubdirs(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:  testPubKey,
				Type:   "dir",
				Path:   "/onlydirs",
				Access: "free",
				Children: []client.ChildEntry{
					{Name: "sub1", Type: "dir"},
					{Name: "sub2", Type: "dir"},
				},
			})
		},
		nil,
	)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--host", srv.URL, makeURI("/onlydirs")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "dir with only subdirs should exit 0 (no files)")
	assert.Contains(t, stdout.String(), "no files in directory")
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
// Test: writeFile creates parent directories
// ---------------------------------------------------------------------------

func TestWriteFile_CreatesParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	// Path with multiple levels of nonexistent parent dirs.
	filePath := filepath.Join(tmpDir, "a", "b", "c", "output.txt")
	content := []byte("nested directory creation test")

	n, err := writeFile(filePath, content)

	require.NoError(t, err)
	assert.Equal(t, len(content), n)

	// Verify parent dirs exist.
	info, err := os.Stat(filepath.Join(tmpDir, "a", "b", "c"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify file content.
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

// ---------------------------------------------------------------------------
// Test: writeFile with impossible path
// ---------------------------------------------------------------------------

func TestWriteFile_BadPath(t *testing.T) {
	_, err := writeFile("/dev/null/impossible/file.txt", []byte("test"))

	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Test: run with --buy, single paid file, successful purchase flow
// ---------------------------------------------------------------------------

func TestRun_WithBuy_SinglePaidFile(t *testing.T) {
	// Generate key pairs for node (seller) and buyer.
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	// Encrypt test content using node's own pubkey (Method 42 Paid mode).
	plaintext := []byte("Paid content downloaded via bmget batch!")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	// Compute capsule for the buyer.
	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	capsuleHex := hex.EncodeToString(capsule)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	var metaCalls int32
	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			call := atomic.AddInt32(&metaCalls, 1)
			if call == 1 {
				// First call: directory listing with one paid file.
				serveJSON(w, client.MetaResponse{
					PNode:  nodePubHex,
					Type:   "dir",
					Path:   "/paid-docs",
					Access: "free",
					Children: []client.ChildEntry{
						{Name: "report.pdf", Type: "file"},
					},
				})
			} else {
				// Second call: file metadata (paid).
				serveJSON(w, client.MetaResponse{
					PNode:      nodePubHex,
					Type:       "file",
					Path:       "/paid-docs/report.pdf",
					MimeType:   "application/pdf",
					FileSize:   uint64(len(plaintext)),
					Access:     "paid",
					PricePerKB: 50,
					TxID:       "invoice-paid-001",
					KeyHash:    keyHashHex,
				})
			}
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
					SellerPubKey: nodePubHex,
				})
				return
			}
			// POST: return the capsule.
			serveJSON(w, client.CapsuleResponse{
				Capsule: capsuleHex,
			})
		},
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO,
		"--concurrency", "1",
		"--host", srv.URL,
		makeURI("/paid-docs"), tmpDir,
	}, &stdout, &stderr)

	assert.Equal(t, 0, code, "paid file download should exit 0; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "report.pdf")
	assert.Contains(t, stdout.String(), "1/1 files downloaded")
	assert.Contains(t, stdout.String(), "paid")

	// Verify the file was written and decrypted correctly.
	data, err := os.ReadFile(filepath.Join(tmpDir, "report.pdf"))
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// Test: run with --buy but invalid wallet key
// ---------------------------------------------------------------------------

func TestRun_WithBuy_InvalidWalletKey(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:  testPubKey,
				Type:   "dir",
				Path:   "/data",
				Access: "free",
				Children: []client.ChildEntry{
					{Name: "a.txt", Type: "file"},
				},
			})
		},
		nil,
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--buy", "--wallet-key", "zzz", "--utxo", fakeUTXO,
		"--host", srv.URL,
		makeURI("/data"), tmpDir,
	}, &stdout, &stderr)

	assert.NotEqual(t, 0, code, "invalid wallet key should exit non-zero")
	assert.Contains(t, stderr.String(), "invalid wallet key")
}

// ---------------------------------------------------------------------------
// Test: run with --buy, paid file with missing TxID
// ---------------------------------------------------------------------------

func TestRun_WithBuy_MissingTxID(t *testing.T) {
	// Generate a valid buyer key.
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	var metaCalls int32
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			call := atomic.AddInt32(&metaCalls, 1)
			if call == 1 {
				// Directory listing.
				serveJSON(w, client.MetaResponse{
					PNode:  testPubKey,
					Type:   "dir",
					Path:   "/data",
					Access: "free",
					Children: []client.ChildEntry{
						{Name: "no-txid.pdf", Type: "file"},
					},
				})
			} else {
				// File metadata: paid but missing TxID.
				serveJSON(w, client.MetaResponse{
					PNode:      testPubKey,
					Type:       "file",
					Path:       "/data/no-txid.pdf",
					FileSize:   1024,
					Access:     "paid",
					PricePerKB: 100,
					TxID:       "", // No TxID
					KeyHash:    testKeyHash("cc"),
				})
			}
		},
		nil,
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO,
		"--concurrency", "1",
		"--host", srv.URL,
		makeURI("/data"), tmpDir,
	}, &stdout, &stderr)

	assert.NotEqual(t, 0, code, "missing txid should cause failure")
	assert.Contains(t, stderr.String(), "no invoice txid")
}

// ---------------------------------------------------------------------------
// Test: downloadFile - unknown access mode
// ---------------------------------------------------------------------------

func TestDownloadFile_UnknownAccess(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:   testPubKey,
				Type:    "file",
				Path:    "/weird.txt",
				Access:  "custom-mode",
				KeyHash: testKeyHash("dd"),
			})
		},
		nil,
	)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "weird.txt")

	entry := downloadFile(c, c, testPubKey, "/weird.txt", localPath, false, nil)

	assert.Contains(t, entry.Error, "unknown access mode")
	assert.Equal(t, 1, entry.Code)
}

// ---------------------------------------------------------------------------
// Test: downloadFile - meta fetch error
// ---------------------------------------------------------------------------

func TestDownloadFile_MetaError(t *testing.T) {
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		},
		nil,
	)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "missing.txt")

	entry := downloadFile(c, c, testPubKey, "/missing.txt", localPath, false, nil)

	assert.NotEmpty(t, entry.Error)
	assert.Contains(t, entry.Error, "get meta")
}

// ---------------------------------------------------------------------------
// Test: downloadFreeFile - data fetch error (HTTP 500)
// ---------------------------------------------------------------------------

func TestDownloadFreeFile_DataFetchError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/data/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "fail.txt")

	meta := &client.MetaResponse{
		PNode:   testPubKey,
		Type:    "file",
		Path:    "/fail.txt",
		KeyHash: testKeyHash("ee"),
		Access:  "free",
	}

	entry := downloadFreeFile(c, meta, localPath)

	assert.Contains(t, entry.Error, "get data")
	assert.NotEqual(t, 0, entry.Code)
}

// ---------------------------------------------------------------------------
// Test: downloadPaidFile - missing KeyHash
// ---------------------------------------------------------------------------

func TestDownloadPaidFile_NoKeyHash(t *testing.T) {
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	cfg := &buy.BuyerConfig{PrivKey: buyerPriv}

	c := client.New("http://localhost:1") // won't be called

	meta := &client.MetaResponse{
		PNode:   testPubKey,
		Type:    "file",
		Path:    "/nokey.pdf",
		Access:  "paid",
		TxID:    "some-txid",
		KeyHash: "", // missing
	}

	entry := downloadPaidFile(c, meta, "/tmp/nokey.pdf", cfg)

	assert.Equal(t, "no content hash available", entry.Error)
	assert.Equal(t, 1, entry.Code)
}

// ---------------------------------------------------------------------------
// Test: downloadPaidFile - no TxID
// ---------------------------------------------------------------------------

func TestDownloadPaidFile_NoTxID(t *testing.T) {
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	cfg := &buy.BuyerConfig{PrivKey: buyerPriv}

	c := client.New("http://localhost:1")

	meta := &client.MetaResponse{
		PNode:   testPubKey,
		Type:    "file",
		Path:    "/notxid.pdf",
		Access:  "paid",
		TxID:    "", // missing
		KeyHash: testKeyHash("ff"),
	}

	entry := downloadPaidFile(c, meta, "/tmp/notxid.pdf", cfg)

	assert.Equal(t, "paid content has no invoice txid", entry.Error)
	assert.Equal(t, 5, entry.Code)
}

// ---------------------------------------------------------------------------
// Test: run with --json for paid batch showing payment info
// ---------------------------------------------------------------------------

func TestRun_WithBuy_JSON_SinglePaidFile(t *testing.T) {
	// Generate key pairs for node (seller) and buyer.
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerKeyHex := hex.EncodeToString(buyerPriv.Serialize())

	// Encrypt content.
	plaintext := []byte("Paid JSON output test content")
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

	var metaCalls int32
	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			call := atomic.AddInt32(&metaCalls, 1)
			if call == 1 {
				serveJSON(w, client.MetaResponse{
					PNode:  nodePubHex,
					Type:   "dir",
					Path:   "/json-docs",
					Access: "free",
					Children: []client.ChildEntry{
						{Name: "file.txt", Type: "file"},
					},
				})
			} else {
				serveJSON(w, client.MetaResponse{
					PNode:      nodePubHex,
					Type:       "file",
					Path:       "/json-docs/file.txt",
					FileSize:   uint64(len(plaintext)),
					Access:     "paid",
					PricePerKB: 50,
					TxID:       "invoice-json-001",
					KeyHash:    keyHashHex,
				})
			}
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
					SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{
				Capsule: capsuleHex,
			})
		},
	)
	defer srv.Close()

	tmpDir := t.TempDir()
	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--json", "--buy", "--wallet-key", buyerKeyHex, "--utxo", fakeUTXO,
		"--concurrency", "1",
		"--host", srv.URL,
		makeURI("/json-docs"), tmpDir,
	}, &stdout, &stderr)

	assert.Equal(t, 0, code, "paid JSON download should exit 0; stderr: %s", stderr.String())

	var resp buy.BatchGetResponse
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, 1, resp.Succeeded)
	assert.Equal(t, 0, resp.Failed)
	require.Len(t, resp.Files, 1)
	assert.NotNil(t, resp.Files[0].Payment)
	assert.Greater(t, resp.Files[0].Payment.CostSatoshis, uint64(0))
}

// ---------------------------------------------------------------------------
// Test: downloadFile - paid with buyEnabled=true (unit level)
// ---------------------------------------------------------------------------

func TestDownloadFile_PaidWithBuy_Success(t *testing.T) {
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	plaintext := []byte("unit test paid download via downloadFile")
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
				PNode:      nodePubHex,
				Type:       "file",
				Path:       "/paid.txt",
				FileSize:   uint64(len(plaintext)),
				Access:     "paid",
				PricePerKB: 50,
				TxID:       "tx-unit-001",
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
					Price:        500,
					PaymentAddr:  sellerAddr,
					SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{
				Capsule: capsuleHex,
			})
		},
	)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "paid.txt")

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	utxo, err := buy.ParseUTXOFlag(fakeUTXO)
	require.NoError(t, err)
	utxo.ScriptPubKey = buy.BuildP2PKHScript(buyerPriv.PubKey().Hash())

	cfg := &buy.BuyerConfig{
		PrivKey:     buyerPriv,
		ManualUTXOs: []*x402.HTLCUTXO{utxo},
	}

	entry := downloadFile(c, c, nodePubHex, "/paid.txt", localPath, true, cfg)

	assert.Empty(t, entry.Error, "downloadFile paid should succeed")
	assert.NotNil(t, entry.Payment)
	assert.Greater(t, entry.Payment.CostSatoshis, uint64(0))
	assert.Equal(t, int64(len(plaintext)), entry.BytesWritten)

	data, err := os.ReadFile(localPath)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)
}

// ---------------------------------------------------------------------------
// Test: downloadPaidFile - buy.Buy() fails (HTLC submit failure)
// ---------------------------------------------------------------------------

func TestDownloadPaidFile_BuyFails(t *testing.T) {
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	plaintext := []byte("buy failure test content")
	encResult, err := method42.Encrypt(plaintext, nodePriv, nodePriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(nodePriv, nodePriv.PubKey(), buyerPriv.PubKey(), encResult.KeyHash)
	require.NoError(t, err)
	capsuleHash := method42.ComputeCapsuleHash(make([]byte, 32), capsule)

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	capsuleHashHex := hex.EncodeToString(capsuleHash)
	nodePubHex := hex.EncodeToString(nodePriv.PubKey().Compressed())
	sellerAddr := func() string { a, _ := script.NewAddressFromPublicKey(nodePriv.PubKey(), false); return a.AddressString }()

	srv := newFullMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			serveJSON(w, client.MetaResponse{
				PNode:      nodePubHex,
				Type:       "file",
				Path:       "/fail.txt",
				FileSize:   uint64(len(plaintext)),
				Access:     "paid",
				PricePerKB: 50,
				TxID:       "tx-fail-001",
				KeyHash:    keyHashHex,
			})
		},
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash:  capsuleHashHex,
					Price:        1000,
					PaymentAddr:  sellerAddr,
					SellerPubKey: nodePubHex,
				})
				return
			}
			// POST: server error
			http.Error(w, "payment failed", http.StatusInternalServerError)
		},
	)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "fail.txt")

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	utxo, err := buy.ParseUTXOFlag(fakeUTXO)
	require.NoError(t, err)
	utxo.ScriptPubKey = buy.BuildP2PKHScript(buyerPriv.PubKey().Hash())

	cfg := &buy.BuyerConfig{
		PrivKey:     buyerPriv,
		ManualUTXOs: []*x402.HTLCUTXO{utxo},
	}

	entry := downloadPaidFile(c, &client.MetaResponse{
		PNode:   nodePubHex,
		Type:    "file",
		Path:    "/fail.txt",
		Access:  "paid",
		TxID:    "tx-fail-001",
		KeyHash: keyHashHex,
	}, localPath, cfg)

	assert.Contains(t, entry.Error, "buy")
	assert.Equal(t, 5, entry.Code)
}

// ---------------------------------------------------------------------------
// Test: downloadPaidFile - data fetch fails after purchase
// ---------------------------------------------------------------------------

func TestDownloadPaidFile_DataFetchFails(t *testing.T) {
	nodePriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	plaintext := []byte("data fetch fail after purchase")
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
			// Meta handler not used by downloadPaidFile directly.
			http.Error(w, "not used", http.StatusNotFound)
		},
		func(w http.ResponseWriter, r *http.Request) {
			// Data endpoint returns 500 after purchase
			http.Error(w, "storage offline", http.StatusInternalServerError)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				serveJSON(w, client.BuyInfo{
					CapsuleHash:  capsuleHashHex,
					Price:        1000,
					PaymentAddr:  sellerAddr,
					SellerPubKey: nodePubHex,
				})
				return
			}
			serveJSON(w, client.CapsuleResponse{
				Capsule: capsuleHex,
			})
		},
	)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "fail.txt")

	fakeUTXO := strings.Repeat("00", 32) + ":0:100000"
	utxo, err := buy.ParseUTXOFlag(fakeUTXO)
	require.NoError(t, err)
	utxo.ScriptPubKey = buy.BuildP2PKHScript(buyerPriv.PubKey().Hash())

	cfg := &buy.BuyerConfig{
		PrivKey:     buyerPriv,
		ManualUTXOs: []*x402.HTLCUTXO{utxo},
	}

	entry := downloadPaidFile(c, &client.MetaResponse{
		PNode:   nodePubHex,
		Type:    "file",
		Path:    "/fail.txt",
		Access:  "paid",
		TxID:    "tx-data-fail-001",
		KeyHash: keyHashHex,
	}, localPath, cfg)

	assert.Contains(t, entry.Error, "get data after purchase")
	assert.NotNil(t, entry.Payment, "payment info should still be present even on data fetch failure")
}

// ---------------------------------------------------------------------------
// Test: run with default localDir (no second arg)
// ---------------------------------------------------------------------------

func TestRun_DefaultLocalDir(t *testing.T) {
	plaintext := []byte("default localDir test content")

	pubKeyBytes, err := hex.DecodeString(testPubKey)
	require.NoError(t, err)
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nil, pubKey, method42.AccessFree)
	require.NoError(t, err)
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	var metaCalls int32
	srv := newMockDaemon(t,
		func(w http.ResponseWriter, r *http.Request) {
			call := atomic.AddInt32(&metaCalls, 1)
			if call == 1 {
				serveJSON(w, client.MetaResponse{
					PNode:  testPubKey,
					Type:   "dir",
					Path:   "/mydir",
					Access: "free",
					Children: []client.ChildEntry{
						{Name: "test.txt", Type: "file"},
					},
				})
			} else {
				serveJSON(w, client.MetaResponse{
					PNode:    testPubKey,
					Type:     "file",
					Path:     "/mydir/test.txt",
					MimeType: "text/plain",
					FileSize: uint64(len(plaintext)),
					KeyHash:  keyHashHex,
					Access:   "free",
				})
			}
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(encResult.Ciphertext)
		},
	)
	defer srv.Close()

	// Change to a temp dir so the default "mydir" is created there.
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	var stdout, stderr bytes.Buffer
	// No second positional arg -- default localDir should be "mydir".
	code := run([]string{"--host", srv.URL, makeURI("/mydir")}, &stdout, &stderr)

	assert.Equal(t, 0, code, "default localDir should work; stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "1/1 files downloaded")

	// Verify file was created in "mydir" subdirectory.
	readData, readErr := os.ReadFile(filepath.Join(tmpDir, "mydir", "test.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, plaintext, readData)
}

// ---------------------------------------------------------------------------
// Test: downloadFreeFile - invalid PNode hex
// ---------------------------------------------------------------------------

func TestDownloadFreeFile_InvalidPNodeHex(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/data/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x01, 0x02}) // some ciphertext
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "bad-pnode.txt")

	meta := &client.MetaResponse{
		PNode:   "zzzz-not-hex", // invalid hex
		Type:    "file",
		Path:    "/bad-pnode.txt",
		KeyHash: testKeyHash("11"),
		Access:  "free",
	}

	entry := downloadFreeFile(c, meta, localPath)

	assert.Contains(t, entry.Error, "invalid pnode hex")
	assert.Equal(t, 1, entry.Code)
}

// ---------------------------------------------------------------------------
// Test: downloadFreeFile - invalid KeyHash hex
// ---------------------------------------------------------------------------

func TestDownloadFreeFile_InvalidKeyHashHex(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/data/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x01, 0x02})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "bad-keyhash.txt")

	meta := &client.MetaResponse{
		PNode:   testPubKey,
		Type:    "file",
		Path:    "/bad-keyhash.txt",
		KeyHash: "zzzz-not-valid-hex-at-all-zzzz", // invalid hex (wrong length)
		Access:  "free",
	}

	entry := downloadFreeFile(c, meta, localPath)

	// Client validates hash length before sending request, so error comes from "get data".
	assert.NotEmpty(t, entry.Error)
	assert.NotEqual(t, 0, entry.Code)
}

// ---------------------------------------------------------------------------
// Test: downloadFreeFile - decryption failure (corrupted ciphertext)
// ---------------------------------------------------------------------------

func TestDownloadFreeFile_DecryptionFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_bitfs/data/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		// Return garbage ciphertext that will fail AES-GCM decryption.
		_, _ = w.Write([]byte("this is not valid AES-GCM ciphertext at all!"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := client.New(srv.URL)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "corrupt.txt")

	meta := &client.MetaResponse{
		PNode:   testPubKey,
		Type:    "file",
		Path:    "/corrupt.txt",
		KeyHash: testKeyHash("99"),
		Access:  "free",
	}

	entry := downloadFreeFile(c, meta, localPath)

	assert.Contains(t, entry.Error, "decrypt")
	assert.Equal(t, 5, entry.Code)
}

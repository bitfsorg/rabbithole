//go:build e2e

package e2e

import (
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
	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// --- Mock implementations for daemon integration ---

// testWalletService implements daemon.WalletService using a real HD wallet.
type testWalletService struct {
	w *wallet.Wallet
}

func (s *testWalletService) DeriveNodePubKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*ec.PublicKey, error) {
	kp, err := s.w.DeriveNodeKey(vaultIndex, filePath, hardened)
	if err != nil {
		return nil, err
	}
	return kp.PublicKey, nil
}

func (s *testWalletService) GetSellerKeyPair() (*ec.PrivateKey, *ec.PublicKey, error) {
	kp, err := s.w.DeriveNodeKey(0, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	return kp.PrivateKey, kp.PublicKey, nil
}

func (s *testWalletService) DeriveNodeKeyPair(pnode []byte) (*ec.PrivateKey, *ec.PublicKey, error) {
	// In e2e tests, return the vault root key pair.
	kp, err := s.w.DeriveNodeKey(0, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	return kp.PrivateKey, kp.PublicKey, nil
}

func (s *testWalletService) GetVaultPubKey(alias string) (string, error) {
	kp, err := s.w.DeriveVaultRootKey(0)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(kp.PublicKey.Compressed()), nil
}

// testMetanetService implements daemon.MetanetService with a path->NodeInfo map.
type testMetanetService struct {
	nodes map[string]*daemon.NodeInfo
}

func (m *testMetanetService) GetNodeByPath(path string) (*daemon.NodeInfo, error) {
	node, ok := m.nodes[path]
	if !ok {
		return nil, daemon.ErrContentNotFound
	}
	return node, nil
}

// TestFreeContentUploadAndRetrieve exercises the full path:
//
//  1. Create HD wallet and derive keys
//  2. Encrypt content with Method 42 AccessFree (scalar-1 private key trick)
//  3. Store ciphertext in a real FileStore (content-addressed, on temp dir)
//  4. Start daemon httptest.Server with real WalletService + FileStore + mock MetanetService
//  5. HTTP GET /_bitfs/data/{keyHash} -> 200 with raw ciphertext
//  6. Decrypt the retrieved ciphertext client-side and verify plaintext
//  7. HTTP GET /docs/readme.txt with content negotiation (JSON, HTML, Markdown)
func TestFreeContentUploadAndRetrieve(t *testing.T) {
	// ===================================================================
	// Step 1: Create HD wallet and derive file node key.
	// ===================================================================
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	// Derive file node key: m/44'/236'/1'/0/0/0'
	fileKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive file node key")
	t.Logf("file node pub key: %x", fileKey.PublicKey.Compressed())

	// ===================================================================
	// Step 2: Encrypt content with Method 42 AccessFree.
	// ===================================================================
	plaintext := []byte("Hello BitFS! This is free content accessible to anyone.")

	encResult, err := method42.Encrypt(
		plaintext,
		fileKey.PrivateKey,
		fileKey.PublicKey,
		method42.AccessFree,
	)
	require.NoError(t, err, "method42 encrypt (free mode)")
	require.NotEmpty(t, encResult.Ciphertext, "ciphertext should not be empty")
	require.Len(t, encResult.KeyHash, 32, "key hash should be 32 bytes")
	t.Logf("encrypted %d bytes -> %d bytes ciphertext", len(plaintext), len(encResult.Ciphertext))
	t.Logf("key hash: %x", encResult.KeyHash)

	// ===================================================================
	// Step 3: Verify Free mode decryption works (anyone can decrypt).
	//
	// Free mode uses scalar 1 as private key:
	//   ECDH(1, P_node) = P_node, so shared_x = P_node.X
	// Since P_node is public, anyone can compute the AES key.
	// ===================================================================
	freePrivKey := method42.FreePrivateKey()
	decResult, err := method42.Decrypt(
		encResult.Ciphertext,
		freePrivKey,
		fileKey.PublicKey,
		encResult.KeyHash,
		method42.AccessFree,
	)
	require.NoError(t, err, "decrypt with free private key (scalar 1)")
	require.Equal(t, plaintext, decResult.Plaintext,
		"decrypted content should match original plaintext")
	t.Logf("free-mode decrypt verified: %d bytes recovered", len(decResult.Plaintext))

	// ===================================================================
	// Step 4: Store ciphertext in real FileStore.
	// ===================================================================
	tmpDir := t.TempDir()
	fileStore, err := storage.NewFileStore(tmpDir)
	require.NoError(t, err, "create file store")

	err = fileStore.Put(encResult.KeyHash, encResult.Ciphertext)
	require.NoError(t, err, "store ciphertext in file store")

	// Verify it was stored correctly.
	exists, err := fileStore.Has(encResult.KeyHash)
	require.NoError(t, err)
	require.True(t, exists, "content should exist in file store")

	storedSize, err := fileStore.Size(encResult.KeyHash)
	require.NoError(t, err)
	require.Equal(t, int64(len(encResult.Ciphertext)), storedSize,
		"stored size should match ciphertext length")

	// ===================================================================
	// Step 5: Set up the daemon with real FileStore + mock MetanetService.
	// ===================================================================
	walletSvc := &testWalletService{w: w}

	keyHashHex := hex.EncodeToString(encResult.KeyHash)
	metanetSvc := &testMetanetService{
		nodes: map[string]*daemon.NodeInfo{
			"/docs/readme.txt": {
				PNode:    fileKey.PublicKey.Compressed(),
				Type:     "file",
				MimeType: "text/plain",
				FileSize: uint64(len(plaintext)),
				KeyHash:  encResult.KeyHash,
				Access:   "free",
			},
			"/docs": {
				Type: "dir",
				Children: []daemon.ChildInfo{
					{Name: "readme.txt", Type: "file"},
				},
				Access: "free",
			},
		},
	}

	config := daemon.DefaultConfig()
	config.Security.RateLimit.RPM = 0 // disable rate limiting for tests

	d, err := daemon.New(config, walletSvc, fileStore, metanetSvc)
	require.NoError(t, err, "create daemon")

	server := httptest.NewServer(d.Handler())
	defer server.Close()
	t.Logf("daemon test server: %s", server.URL)

	// ===================================================================
	// Step 6: HTTP GET /_bitfs/data/{keyHash} -> raw ciphertext.
	// ===================================================================
	t.Run("data_endpoint_returns_ciphertext", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/_bitfs/data/" + keyHashHex)
		require.NoError(t, err, "GET data endpoint")
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "should return 200")
		assert.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"))
		assert.Equal(t, keyHashHex, resp.Header.Get("X-Key-Hash"))

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, encResult.Ciphertext, body,
			"retrieved ciphertext should match stored ciphertext")

		// Decrypt the retrieved ciphertext client-side.
		decResult2, err := method42.Decrypt(
			body,
			freePrivKey,
			fileKey.PublicKey,
			encResult.KeyHash,
			method42.AccessFree,
		)
		require.NoError(t, err, "client-side decrypt of retrieved ciphertext")
		assert.Equal(t, plaintext, decResult2.Plaintext,
			"client-side decrypted content should match original")
		t.Logf("client-side decrypt of HTTP-retrieved ciphertext: OK")
	})

	// ===================================================================
	// Step 7: HTTP GET /_bitfs/data/{keyHash} for non-existent hash -> 404.
	// ===================================================================
	t.Run("data_endpoint_not_found", func(t *testing.T) {
		fakeHash := strings.Repeat("00", 32)
		resp, err := http.Get(server.URL + "/_bitfs/data/" + fakeHash)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"non-existent hash should return 404")
	})

	// ===================================================================
	// Step 8: Content negotiation - JSON (default).
	// ===================================================================
	t.Run("content_negotiation_json", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/docs/readme.txt", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

		var nodeInfo daemon.NodeInfo
		err = json.NewDecoder(resp.Body).Decode(&nodeInfo)
		require.NoError(t, err, "decode JSON response")

		assert.Equal(t, "file", nodeInfo.Type)
		assert.Equal(t, "text/plain", nodeInfo.MimeType)
		assert.Equal(t, uint64(len(plaintext)), nodeInfo.FileSize)
		assert.Equal(t, "free", nodeInfo.Access)
		t.Logf("JSON response: type=%s, mime=%s, size=%d, access=%s",
			nodeInfo.Type, nodeInfo.MimeType, nodeInfo.FileSize, nodeInfo.Access)
	})

	// ===================================================================
	// Step 9: Content negotiation - HTML.
	// ===================================================================
	t.Run("content_negotiation_html", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/docs/readme.txt", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/html")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		htmlBody := string(body)

		assert.Contains(t, htmlBody, "<!DOCTYPE html>")
		assert.Contains(t, htmlBody, "text/plain") // MimeType in the HTML
		t.Logf("HTML response length: %d bytes", len(body))
	})

	// ===================================================================
	// Step 10: Content negotiation - Markdown.
	// ===================================================================
	t.Run("content_negotiation_markdown", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/docs/readme.txt", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/markdown")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/markdown")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		mdBody := string(body)

		assert.Contains(t, mdBody, "# File")
		assert.Contains(t, mdBody, "text/plain")
		t.Logf("Markdown response: %s", strings.TrimSpace(mdBody))
	})

	// ===================================================================
	// Step 11: Content negotiation - directory listing (HTML).
	// ===================================================================
	t.Run("directory_listing_html", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/docs", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", "text/html")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "readme.txt")
		t.Logf("directory listing HTML contains child entry 'readme.txt'")
	})

	// ===================================================================
	// Step 12: Content negotiation - no Accept header defaults to JSON.
	// ===================================================================
	t.Run("content_negotiation_default_json", func(t *testing.T) {
		req, err := http.NewRequest("GET", server.URL+"/docs/readme.txt", nil)
		require.NoError(t, err)
		// No Accept header set

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	})

	// ===================================================================
	// Step 13: Non-existent path returns 404.
	// ===================================================================
	t.Run("nonexistent_path_returns_404", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/nonexistent/file.txt")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	// ===================================================================
	// Step 14: Health endpoint.
	// ===================================================================
	t.Run("health_endpoint", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/_bitfs/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), `"status":"ok"`)
	})

	// ===================================================================
	// Step 15: Verify Method 42 Free-mode round-trip through FileStore.
	//
	// This sub-test independently verifies the full encrypt -> store ->
	// retrieve -> decrypt path without the HTTP layer.
	// ===================================================================
	t.Run("method42_free_roundtrip_via_filestore", func(t *testing.T) {
		content := []byte("Another free file: the quick brown fox jumps over the lazy dog.")

		// Encrypt with AccessFree.
		enc, err := method42.Encrypt(content, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
		require.NoError(t, err)

		// Store.
		err = fileStore.Put(enc.KeyHash, enc.Ciphertext)
		require.NoError(t, err)

		// Retrieve.
		retrieved, err := fileStore.Get(enc.KeyHash)
		require.NoError(t, err)
		assert.Equal(t, enc.Ciphertext, retrieved)

		// Decrypt using the free private key (scalar 1).
		dec, err := method42.Decrypt(retrieved, freePrivKey, fileKey.PublicKey, enc.KeyHash, method42.AccessFree)
		require.NoError(t, err)
		assert.Equal(t, content, dec.Plaintext)

		t.Logf("FileStore round-trip: %d bytes plaintext -> %d bytes ciphertext -> decrypt OK",
			len(content), len(enc.Ciphertext))
	})

	// ===================================================================
	// Summary.
	// ===================================================================
	t.Logf("--- Free Content E2E Summary ---")
	t.Logf("Wallet:   mnemonic generated, file key derived")
	t.Logf("Encrypt:  %d bytes plaintext -> %d bytes ciphertext (AccessFree)", len(plaintext), len(encResult.Ciphertext))
	t.Logf("Store:    FileStore at %s", tmpDir)
	t.Logf("Daemon:   httptest.Server at %s", server.URL)
	t.Logf("Retrieve: /_bitfs/data/%s -> 200", keyHashHex[:16]+"...")
	t.Logf("Decrypt:  client-side decrypt of HTTP-retrieved ciphertext -> OK")
	t.Logf("Content negotiation: JSON, HTML, Markdown all verified")
}

// TestFreeContentEncryptDecryptSymmetry verifies that Method 42 Free mode
// encryption and decryption are symmetric: anyone with the public key and
// key_hash can decrypt, since the private key is the well-known scalar 1.
func TestFreeContentEncryptDecryptSymmetry(t *testing.T) {
	// Generate a random key pair (simulating a file node).
	privKey, err := ec.NewPrivateKey()
	require.NoError(t, err)
	pubKey := privKey.PubKey()

	testCases := []struct {
		name    string
		content []byte
	}{
		{"short text", []byte("hello")},
		{"medium text", []byte(strings.Repeat("BitFS is great! ", 100))},
		{"empty-like", []byte(" ")},
		{"binary data", func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}()},
		{"unicode", []byte("Hello world in Chinese")},
	}

	freeKey := method42.FreePrivateKey()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Encrypt with Free mode.
			enc, err := method42.Encrypt(tc.content, privKey, pubKey, method42.AccessFree)
			require.NoError(t, err)

			// Ciphertext should be longer than plaintext (nonce + tag overhead).
			assert.Greater(t, len(enc.Ciphertext), len(tc.content),
				"ciphertext should be longer due to GCM overhead")

			// Decrypt using the free private key.
			dec, err := method42.Decrypt(enc.Ciphertext, freeKey, pubKey, enc.KeyHash, method42.AccessFree)
			require.NoError(t, err)
			assert.Equal(t, tc.content, dec.Plaintext)

			// Verify key_hash matches.
			expectedHash := method42.ComputeKeyHash(tc.content)
			assert.Equal(t, expectedHash, enc.KeyHash)
			assert.Equal(t, expectedHash, dec.KeyHash)

			t.Logf("%s: %d bytes -> %d bytes ciphertext, decrypt OK",
				tc.name, len(tc.content), len(enc.Ciphertext))
		})
	}
}

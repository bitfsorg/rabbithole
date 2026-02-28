//go:build e2e

package e2e

import (
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestLargeFileEncryptDecrypt generates 1MB of deterministic data, encrypts it
// with Method 42 AccessFree, decrypts, and verifies the plaintext matches.
//
// This validates that the AES-256-GCM encryption handles large payloads
// correctly without truncation, corruption, or memory issues.
func TestLargeFileEncryptDecrypt(t *testing.T) {
	privKey, err := ec.NewPrivateKey()
	require.NoError(t, err, "generate private key")
	pubKey := privKey.PubKey()

	// Generate 1MB of deterministic data.
	const size = 1 << 20 // 1 MB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Encrypt with AccessFree.
	enc, err := method42.Encrypt(data, privKey, pubKey, method42.AccessFree)
	require.NoError(t, err, "encrypt 1MB data")
	t.Logf("1MB encrypted: %d bytes plaintext -> %d bytes ciphertext (overhead: %d bytes)",
		len(data), len(enc.Ciphertext), len(enc.Ciphertext)-len(data))

	// Ciphertext should be larger than plaintext by GCM overhead (12-byte nonce + 16-byte tag).
	assert.Greater(t, len(enc.Ciphertext), len(data),
		"ciphertext should be larger than plaintext due to GCM overhead")
	expectedOverhead := method42.NonceLen + method42.GCMTagLen // 28 bytes
	assert.Equal(t, len(data)+expectedOverhead, len(enc.Ciphertext),
		"ciphertext should be plaintext + nonce + tag")

	// KeyHash should be 32 bytes.
	require.Len(t, enc.KeyHash, 32, "key hash should be 32 bytes")

	// Verify key hash matches independent computation.
	expectedHash := method42.ComputeKeyHash(data)
	assert.Equal(t, expectedHash, enc.KeyHash,
		"key hash should equal SHA256(SHA256(plaintext))")

	// Decrypt with the free private key (scalar 1).
	freeKey := method42.FreePrivateKey()
	dec, err := method42.Decrypt(enc.Ciphertext, freeKey, pubKey, enc.KeyHash, method42.AccessFree)
	require.NoError(t, err, "decrypt 1MB data")

	// Verify full plaintext match.
	require.Equal(t, len(data), len(dec.Plaintext),
		"decrypted length should match original")
	assert.Equal(t, data, dec.Plaintext,
		"decrypted content should match original plaintext byte-for-byte")

	t.Logf("1MB decrypted successfully: %d bytes, key_hash=%s",
		len(dec.Plaintext), hex.EncodeToString(dec.KeyHash)[:16]+"...")
}

// TestLargeFileStoreDaemonRoundtrip exercises the full large-file path:
//
//  1. Encrypt 1MB of deterministic data with AccessFree
//  2. Store ciphertext in a real FileStore
//  3. Start daemon httptest.Server with mock MetanetService
//  4. HTTP GET /_bitfs/data/{keyHash} -> retrieve ciphertext
//  5. Decrypt the retrieved ciphertext client-side
//  6. Verify: decrypted plaintext matches original 1MB data
//
// This validates that the daemon correctly serves large binary payloads
// without truncation or corruption in the HTTP transport layer.
func TestLargeFileStoreDaemonRoundtrip(t *testing.T) {
	// ===================================================================
	// Step 1: Create wallet and derive keys.
	// ===================================================================
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	fileKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive file node key")

	// ===================================================================
	// Step 2: Generate and encrypt 1MB of deterministic data.
	// ===================================================================
	const size = 1 << 20 // 1 MB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	enc, err := method42.Encrypt(data, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "encrypt 1MB data")
	t.Logf("encrypted 1MB: %d bytes ciphertext, key_hash=%s",
		len(enc.Ciphertext), hex.EncodeToString(enc.KeyHash)[:16]+"...")

	// ===================================================================
	// Step 3: Store ciphertext in FileStore.
	// ===================================================================
	fileStore, err := storage.NewFileStore(t.TempDir())
	require.NoError(t, err, "create file store")

	err = fileStore.Put(enc.KeyHash, enc.Ciphertext)
	require.NoError(t, err, "store 1MB ciphertext in file store")

	// Verify storage.
	storedSize, err := fileStore.Size(enc.KeyHash)
	require.NoError(t, err, "get stored size")
	assert.Equal(t, int64(len(enc.Ciphertext)), storedSize,
		"stored size should match ciphertext length")

	// ===================================================================
	// Step 4: Set up daemon with mock MetanetService.
	// ===================================================================
	walletSvc := &testWalletService{w: w}
	metanetSvc := &testMetanetService{nodes: map[string]*daemon.NodeInfo{
		"/big.bin": {
			PNode:    fileKey.PublicKey.Compressed(),
			Type:     "file",
			Access:   "free",
			KeyHash:  enc.KeyHash,
			FileSize: uint64(len(data)),
			MimeType: "application/octet-stream",
		},
	}}

	config := daemon.DefaultConfig()
	config.Security.RateLimit.RPM = 0 // disable rate limiting for tests

	d, err := daemon.New(config, walletSvc, fileStore, metanetSvc)
	require.NoError(t, err, "create daemon")

	server := httptest.NewServer(d.Handler())
	defer server.Close()
	t.Logf("daemon test server: %s", server.URL)

	// ===================================================================
	// Step 5: Retrieve ciphertext via HTTP GET.
	// ===================================================================
	keyHashHex := hex.EncodeToString(enc.KeyHash)
	resp, err := http.Get(server.URL + "/_bitfs/data/" + keyHashHex)
	require.NoError(t, err, "GET data endpoint for 1MB file")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "should return 200 for 1MB file")
	assert.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"),
		"content-type should be application/octet-stream")
	assert.Equal(t, keyHashHex, resp.Header.Get("X-Key-Hash"),
		"X-Key-Hash header should match")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read 1MB response body")
	require.Equal(t, len(enc.Ciphertext), len(body),
		"response body length should match ciphertext length")
	assert.Equal(t, enc.Ciphertext, body,
		"retrieved ciphertext should match stored ciphertext exactly")
	t.Logf("HTTP GET returned %d bytes ciphertext", len(body))

	// ===================================================================
	// Step 6: Decrypt retrieved ciphertext and verify.
	// ===================================================================
	freeKey := method42.FreePrivateKey()
	dec, err := method42.Decrypt(body, freeKey, fileKey.PublicKey, enc.KeyHash, method42.AccessFree)
	require.NoError(t, err, "decrypt 1MB ciphertext retrieved from daemon")

	require.Equal(t, len(data), len(dec.Plaintext),
		"decrypted length should match original")
	assert.Equal(t, data, dec.Plaintext,
		"decrypted content should match original 1MB data byte-for-byte")

	t.Logf("--- Large File Daemon Roundtrip Summary ---")
	t.Logf("Plaintext:  %d bytes (1MB deterministic data)", len(data))
	t.Logf("Ciphertext: %d bytes (GCM overhead: %d bytes)", len(enc.Ciphertext), len(enc.Ciphertext)-len(data))
	t.Logf("Stored:     FileStore, key_hash=%s", keyHashHex[:16]+"...")
	t.Logf("Retrieved:  HTTP GET /_bitfs/data/%s -> %d bytes", keyHashHex[:16]+"...", len(body))
	t.Logf("Decrypted:  %d bytes, matches original: OK", len(dec.Plaintext))
}

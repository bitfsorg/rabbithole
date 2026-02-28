//go:build e2e

package e2e

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// --- Mock SPV service for client roundtrip testing ---

// clientTestSPVService implements daemon.SPVService with pre-configured results.
type clientTestSPVService struct {
	results map[string]*daemon.SPVResult
}

func (s *clientTestSPVService) VerifyTx(_ context.Context, txid string) (*daemon.SPVResult, error) {
	r, ok := s.results[txid]
	if !ok {
		return nil, fmt.Errorf("tx not found: %s", txid)
	}
	return r, nil
}

// setupClientTestDaemon creates a daemon httptest.Server with a real wallet,
// file store, and a mock MetanetService containing the given nodes.
// Returns the httptest.Server and the file store for data insertion.
func setupClientTestDaemon(
	t *testing.T,
	nodes map[string]*daemon.NodeInfo,
	spvSvc daemon.SPVService,
) (*httptest.Server, *storage.FileStore) {
	t.Helper()

	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	walletSvc := &testWalletService{w: w}
	metanetSvc := &testMetanetService{nodes: nodes}

	fileStore, err := storage.NewFileStore(t.TempDir())
	require.NoError(t, err, "create file store")

	config := daemon.DefaultConfig()
	config.Security.RateLimit.RPM = 0 // disable rate limiting for tests

	d, err := daemon.New(config, walletSvc, fileStore, metanetSvc)
	require.NoError(t, err, "create daemon")

	if spvSvc != nil {
		d.SetSPV(spvSvc)
	}

	server := httptest.NewServer(d.Handler())
	t.Cleanup(server.Close)

	return server, fileStore
}

// TestClientGetMeta verifies the client.GetMeta method against a real daemon:
//
//   - Set up daemon with mock MetanetService containing a file node
//   - Use client.New(server.URL) to call GetMeta
//   - Verify: MetaResponse fields (PNode, Type, Access, MimeType, FileSize,
//     KeyHash, Path) all match the mock data
func TestClientGetMeta(t *testing.T) {
	// Derive a real key to use as the pnode.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)
	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)
	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err)

	fileKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err)
	pnodeBytes := fileKey.PublicKey.Compressed()
	pnodeHex := hex.EncodeToString(pnodeBytes)

	// Encrypt some content to get a real key hash.
	plaintext := []byte("Hello, BitFS client roundtrip!")
	encResult, err := method42.Encrypt(plaintext, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "encrypt content")
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	// Register the node in the mock MetanetService.
	// handleMeta prepends "/" to the path from the URL, so register as "/docs/readme.txt".
	nodes := map[string]*daemon.NodeInfo{
		"/docs/readme.txt": {
			PNode:    pnodeBytes,
			Type:     "file",
			Access:   "free",
			MimeType: "text/plain",
			FileSize: uint64(len(plaintext)),
			KeyHash:  encResult.KeyHash,
		},
	}

	server, _ := setupClientTestDaemon(t, nodes, nil)

	// Create the client and call GetMeta.
	c := client.New(server.URL)
	meta, err := c.GetMeta(pnodeHex, "docs/readme.txt")
	require.NoError(t, err, "GetMeta should succeed")

	// Verify all response fields.
	assert.Equal(t, pnodeHex, meta.PNode, "pnode should match")
	assert.Equal(t, "file", meta.Type, "type should be file")
	assert.Equal(t, "free", meta.Access, "access should be free")
	assert.Equal(t, "text/plain", meta.MimeType, "mime_type should match")
	assert.Equal(t, uint64(len(plaintext)), meta.FileSize, "file_size should match")
	assert.Equal(t, keyHashHex, meta.KeyHash, "key_hash should match")
	assert.Equal(t, "/docs/readme.txt", meta.Path, "path should match")

	t.Logf("GetMeta roundtrip: pnode=%s, type=%s, access=%s, mime=%s, size=%d, key_hash=%s",
		meta.PNode[:16]+"...", meta.Type, meta.Access, meta.MimeType, meta.FileSize, meta.KeyHash[:16]+"...")
}

// TestClientGetData verifies the client.GetData method against a real daemon:
//
//   - Store ciphertext in the daemon's FileStore
//   - Use client.GetData(keyHashHex) to retrieve it
//   - Verify: the body matches the stored ciphertext exactly
func TestClientGetData(t *testing.T) {
	// Derive a key and encrypt content.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)
	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)
	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err)

	fileKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err)

	plaintext := []byte("Encrypted file content for client roundtrip test.")
	encResult, err := method42.Encrypt(plaintext, fileKey.PrivateKey, fileKey.PublicKey, method42.AccessFree)
	require.NoError(t, err, "encrypt content")
	keyHashHex := hex.EncodeToString(encResult.KeyHash)

	// Set up daemon and store ciphertext in the file store.
	nodes := map[string]*daemon.NodeInfo{}
	server, fileStore := setupClientTestDaemon(t, nodes, nil)

	err = fileStore.Put(encResult.KeyHash, encResult.Ciphertext)
	require.NoError(t, err, "store ciphertext in file store")

	// Create the client and call GetData.
	c := client.New(server.URL)
	rc, err := c.GetData(keyHashHex)
	require.NoError(t, err, "GetData should succeed")
	defer rc.Close()

	body, err := io.ReadAll(rc)
	require.NoError(t, err, "read GetData body")

	// Verify the body matches the stored ciphertext.
	assert.Equal(t, encResult.Ciphertext, body,
		"retrieved data should match stored ciphertext")

	t.Logf("GetData roundtrip: keyHash=%s, ciphertext=%d bytes",
		keyHashHex[:16]+"...", len(body))
}

// TestClientGetMetaNotFound verifies that calling GetMeta for a non-existent
// path returns client.ErrNotFound:
//
//   - Set up daemon with an empty MetanetService
//   - Call GetMeta for a path that does not exist
//   - Verify: error wraps client.ErrNotFound (errors.Is check)
func TestClientGetMetaNotFound(t *testing.T) {
	// We need a valid 33-byte compressed pubkey for the pnode parameter.
	pnodeHex := "02" + strings.Repeat("ab", 32)

	// Empty MetanetService -- no paths registered.
	nodes := map[string]*daemon.NodeInfo{}
	server, _ := setupClientTestDaemon(t, nodes, nil)

	c := client.New(server.URL)
	_, err := c.GetMeta(pnodeHex, "nonexistent/file.txt")

	require.Error(t, err, "GetMeta should return error for non-existent path")
	assert.True(t, errors.Is(err, client.ErrNotFound),
		"error should wrap client.ErrNotFound, got: %v", err)

	t.Logf("GetMeta not found: err=%v", err)
}

// TestClientVerifySPV verifies the client.VerifySPV method against a real daemon:
//
//   - Set up daemon with a mock SPVService containing a confirmed proof
//   - Use client.VerifySPV(txid) to retrieve the proof
//   - Verify: SPVProofResponse fields (TxID, Confirmed, BlockHash, BlockHeight)
//     match the mock data
func TestClientVerifySPV(t *testing.T) {
	const testTxID = "aabbccdd11223344556677889900aabbccdd11223344556677889900aabbccdd"
	const testBlockHash = "000000000000000003fa5ec0e8f3e6e7d7f1c8b4a2e0d1c3b5a7f9e1d3c5b7a9"
	const testBlockHeight = uint64(800123)

	spvSvc := &clientTestSPVService{
		results: map[string]*daemon.SPVResult{
			testTxID: {
				Confirmed:   true,
				BlockHash:   testBlockHash,
				BlockHeight: testBlockHeight,
			},
		},
	}

	nodes := map[string]*daemon.NodeInfo{}
	server, _ := setupClientTestDaemon(t, nodes, spvSvc)

	c := client.New(server.URL)
	proof, err := c.VerifySPV(testTxID)
	require.NoError(t, err, "VerifySPV should succeed")

	// Verify all response fields.
	assert.Equal(t, testTxID, proof.TxID, "txid should be echoed back")
	assert.True(t, proof.Confirmed, "confirmed should be true")
	assert.Equal(t, testBlockHash, proof.BlockHash, "block_hash should match")
	assert.Equal(t, testBlockHeight, proof.BlockHeight, "block_height should match")

	t.Logf("VerifySPV roundtrip: txid=%s, confirmed=%t, block_hash=%s, block_height=%d",
		proof.TxID[:16]+"...", proof.Confirmed, proof.BlockHash[:16]+"...", proof.BlockHeight)
}

// TestClientGetDataNotFound verifies that calling GetData for a non-existent
// hash returns client.ErrNotFound.
func TestClientGetDataNotFound(t *testing.T) {
	// A valid 64-hex-char hash that is not stored.
	missingHash := strings.Repeat("de", 32)

	nodes := map[string]*daemon.NodeInfo{}
	server, _ := setupClientTestDaemon(t, nodes, nil)

	c := client.New(server.URL)
	_, err := c.GetData(missingHash)

	require.Error(t, err, "GetData should return error for non-existent hash")
	assert.True(t, errors.Is(err, client.ErrNotFound),
		"error should wrap client.ErrNotFound, got: %v", err)

	t.Logf("GetData not found: err=%v", err)
}

// TestClientVerifySPVUnconfirmed verifies that the client correctly receives
// an unconfirmed SPV proof (confirmed=false, no block info).
func TestClientVerifySPVUnconfirmed(t *testing.T) {
	const testTxID = "1111111122222222333333334444444455555555666666667777777788888888"

	spvSvc := &clientTestSPVService{
		results: map[string]*daemon.SPVResult{
			testTxID: {
				Confirmed: false,
			},
		},
	}

	nodes := map[string]*daemon.NodeInfo{}
	server, _ := setupClientTestDaemon(t, nodes, spvSvc)

	c := client.New(server.URL)
	proof, err := c.VerifySPV(testTxID)
	require.NoError(t, err, "VerifySPV should succeed for unconfirmed tx")

	assert.Equal(t, testTxID, proof.TxID, "txid should be echoed back")
	assert.False(t, proof.Confirmed, "confirmed should be false")
	assert.Empty(t, proof.BlockHash, "block_hash should be empty for unconfirmed tx")
	assert.Equal(t, uint64(0), proof.BlockHeight, "block_height should be 0 for unconfirmed tx")

	t.Logf("VerifySPV unconfirmed: txid=%s, confirmed=%t", testTxID[:16]+"...", proof.Confirmed)
}

// TestClientGetMetaWithChildren verifies that GetMeta correctly returns
// directory metadata including children entries.
func TestClientGetMetaWithChildren(t *testing.T) {
	pnodeHex := "03" + strings.Repeat("cd", 32)
	pnodeBytes, err := hex.DecodeString(pnodeHex)
	require.NoError(t, err)

	nodes := map[string]*daemon.NodeInfo{
		"/": {
			PNode:  pnodeBytes,
			Type:   "dir",
			Access: "free",
			Children: []daemon.ChildInfo{
				{Name: "docs", Type: "dir"},
				{Name: "main.go", Type: "file"},
				{Name: "README.md", Type: "file"},
			},
		},
	}

	server, _ := setupClientTestDaemon(t, nodes, nil)

	c := client.New(server.URL)
	// GetMeta with empty path -- daemon prepends "/" to get "/"
	meta, err := c.GetMeta(pnodeHex, "")
	require.NoError(t, err, "GetMeta for root dir should succeed")

	assert.Equal(t, "dir", meta.Type, "type should be dir")
	assert.Equal(t, "free", meta.Access, "access should be free")
	require.Len(t, meta.Children, 3, "should have 3 children")

	assert.Equal(t, "docs", meta.Children[0].Name)
	assert.Equal(t, "dir", meta.Children[0].Type)
	assert.Equal(t, "main.go", meta.Children[1].Name)
	assert.Equal(t, "file", meta.Children[1].Type)
	assert.Equal(t, "README.md", meta.Children[2].Name)
	assert.Equal(t, "file", meta.Children[2].Type)

	t.Logf("GetMeta dir with %d children OK", len(meta.Children))
}

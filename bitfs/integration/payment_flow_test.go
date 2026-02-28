//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/wallet"
	"github.com/bitfsorg/libbitfs-go/x402"
)

// --- Mock wallet service for daemon ---

type mockWalletService struct {
	privKey *ec.PrivateKey
	pubKey  *ec.PublicKey
}

func (m *mockWalletService) DeriveNodePubKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*ec.PublicKey, error) {
	return m.pubKey, nil
}

func (m *mockWalletService) GetSellerKeyPair() (*ec.PrivateKey, *ec.PublicKey, error) {
	return m.privKey, m.pubKey, nil
}

func (m *mockWalletService) DeriveNodeKeyPair(pnode []byte) (*ec.PrivateKey, *ec.PublicKey, error) {
	return m.privKey, m.pubKey, nil
}

func (m *mockWalletService) GetVaultPubKey(alias string) (string, error) {
	return "", fmt.Errorf("not implemented in mock")
}

// --- Mock content store for daemon ---

type mockContentStore struct {
	data map[string][]byte
}

func newMockContentStore() *mockContentStore {
	return &mockContentStore{data: make(map[string][]byte)}
}

func (m *mockContentStore) Put(keyHash []byte, ciphertext []byte) {
	m.data[string(keyHash)] = ciphertext
}

func (m *mockContentStore) Get(keyHash []byte) ([]byte, error) {
	d, ok := m.data[string(keyHash)]
	if !ok {
		return nil, nil
	}
	return d, nil
}

func (m *mockContentStore) Has(keyHash []byte) (bool, error) {
	_, ok := m.data[string(keyHash)]
	return ok, nil
}

func (m *mockContentStore) Size(keyHash []byte) (int64, error) {
	d, ok := m.data[string(keyHash)]
	if !ok {
		return 0, nil
	}
	return int64(len(d)), nil
}

// --- Mock metanet service for daemon ---

type mockMetanetService struct {
	nodes map[string]*daemon.NodeInfo
}

func newMockMetanetService() *mockMetanetService {
	return &mockMetanetService{nodes: make(map[string]*daemon.NodeInfo)}
}

func (m *mockMetanetService) GetNodeByPath(path string) (*daemon.NodeInfo, error) {
	node, ok := m.nodes[path]
	if !ok {
		return nil, daemon.ErrContentNotFound
	}
	return node, nil
}

// createTestDaemon creates a daemon with mock services for testing.
func createTestDaemon(t *testing.T) (*daemon.Daemon, *mockContentStore) {
	t.Helper()

	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	walletSvc := &mockWalletService{
		privKey: nodeKey.PrivateKey,
		pubKey:  nodeKey.PublicKey,
	}
	contentStore := newMockContentStore()
	metanetSvc := newMockMetanetService()

	config := daemon.DefaultConfig()
	config.ListenAddr = ":0" // auto port

	d, err := daemon.New(config, walletSvc, contentStore, metanetSvc)
	require.NoError(t, err)

	return d, contentStore
}

// --- TestX402InvoiceHeaderRoundTrip ---

func TestX402InvoiceHeaderRoundTrip(t *testing.T) {
	// 1. Create invoice with price, file size, capsule hash
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)
	invoice := x402.NewInvoice(100, 10240, "1BitFSAddress...", capsuleHash, 3600)
	require.NotNil(t, invoice)
	assert.Greater(t, invoice.Price, uint64(0))
	assert.Equal(t, uint64(100), invoice.PricePerKB)
	assert.Equal(t, uint64(10240), invoice.FileSize)
	assert.False(t, invoice.IsExpired())

	// Verify price calculation: ceil(100 * 10240 / 1024) = 1000
	expectedPrice := x402.CalculatePrice(100, 10240)
	assert.Equal(t, uint64(1000), expectedPrice)
	assert.Equal(t, expectedPrice, invoice.Price)

	// 2. Set payment headers on HTTP response
	recorder := httptest.NewRecorder()
	headers := x402.PaymentHeadersFromInvoice(invoice)
	x402.SetPaymentHeaders(recorder, headers)

	// 3. Parse headers from HTTP response
	resp := recorder.Result()
	assert.Equal(t, http.StatusPaymentRequired, resp.StatusCode)

	parsedHeaders, err := x402.ParsePaymentHeaders(resp)
	require.NoError(t, err)

	// 4. Verify all fields match
	assert.Equal(t, invoice.Price, parsedHeaders.Price)
	assert.Equal(t, invoice.PricePerKB, parsedHeaders.PricePerKB)
	assert.Equal(t, invoice.FileSize, parsedHeaders.FileSize)
	assert.Equal(t, invoice.ID, parsedHeaders.InvoiceID)
	assert.Equal(t, invoice.Expiry, parsedHeaders.Expiry)
}

// --- TestHTLCScriptConstruction ---

func TestHTLCScriptConstruction(t *testing.T) {
	// 1. Generate buyer and seller keys
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	buyerKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	sellerKey, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	// 2. Compute capsule and capsule_hash
	// Encrypt a dummy content to get a keyHash for ComputeCapsule
	dummyPlaintext := []byte("dummy content for capsule test")
	dummyEnc, err := method42.Encrypt(dummyPlaintext, sellerKey.PrivateKey, sellerKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	capsule, err := method42.ComputeCapsule(sellerKey.PrivateKey, sellerKey.PublicKey, buyerKey.PublicKey, dummyEnc.KeyHash)
	require.NoError(t, err)
	assert.Len(t, capsule, 32)

	fileTxID := bytes.Repeat([]byte{0xf0}, 32) // mock file txid
	capsuleHash := method42.ComputeCapsuleHash(fileTxID, capsule)
	assert.Len(t, capsuleHash, 32)

	// Build a mock seller address (20-byte hash)
	sellerAddr := bytes.Repeat([]byte{0x11}, 20)

	// 3. Build HTLC script
	sellerPubKey := sellerKey.PublicKey.Compressed()
	htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerKey.PublicKey.Compressed(),
		SellerPubKey: sellerPubKey,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       1000,
		Timeout:      144,
	})
	require.NoError(t, err)
	require.NotEmpty(t, htlcScript)

	// 4. Verify script contains correct opcodes
	// OP_IF = 0x63, OP_SHA256 = 0xa8, OP_EQUALVERIFY = 0x88
	// OP_ELSE = 0x67, OP_CHECKMULTISIG = 0xae, OP_CHECKSIG = 0xac
	// OP_ENDIF = 0x68
	assert.True(t, bytes.Contains(htlcScript, []byte{0x63}), "script should contain OP_IF")
	assert.True(t, bytes.Contains(htlcScript, []byte{0xa8}), "script should contain OP_SHA256")
	assert.True(t, bytes.Contains(htlcScript, []byte{0x88}), "script should contain OP_EQUALVERIFY")
	assert.True(t, bytes.Contains(htlcScript, []byte{0x67}), "script should contain OP_ELSE")
	assert.True(t, bytes.Contains(htlcScript, []byte{0xae}), "script should contain OP_CHECKMULTISIG")
	assert.True(t, bytes.Contains(htlcScript, []byte{0xac}), "script should contain OP_CHECKSIG")
	assert.True(t, bytes.Contains(htlcScript, []byte{0x68}), "script should contain OP_ENDIF")

	// 5. Verify capsule_hash is in the script
	assert.True(t, bytes.Contains(htlcScript, capsuleHash), "script should contain capsule_hash")

	// 6. Verify buyer pubkey is in the script
	assert.True(t, bytes.Contains(htlcScript, buyerKey.PublicKey.Compressed()), "script should contain buyer pubkey")

	// 7. Verify seller address is in the script
	assert.True(t, bytes.Contains(htlcScript, sellerAddr), "script should contain seller address")
}

// --- TestHTLCScriptValidation ---

func TestHTLCScriptValidation(t *testing.T) {
	buyerPub := bytes.Repeat([]byte{0x02}, 33)
	sellerAddr := bytes.Repeat([]byte{0x11}, 20)
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)

	sellerPub := bytes.Repeat([]byte{0x03}, 33)

	// Missing buyer pubkey
	_, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  []byte{0x02, 0x03}, // too short
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       1000,
		Timeout:      144,
	})
	assert.ErrorIs(t, err, x402.ErrHTLCBuildFailed)

	// Missing seller address
	_, err = x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPub,
		SellerPubKey: sellerPub,
		SellerAddr:   []byte{0x11}, // too short
		CapsuleHash:  capsuleHash,
		Amount:       1000,
		Timeout:      144,
	})
	assert.ErrorIs(t, err, x402.ErrHTLCBuildFailed)

	// Zero amount
	_, err = x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPub,
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       0,
		Timeout:      144,
	})
	assert.ErrorIs(t, err, x402.ErrHTLCBuildFailed)

	// Nil params
	_, err = x402.BuildHTLC(nil)
	assert.ErrorIs(t, err, x402.ErrHTLCBuildFailed)
}

// --- TestDaemonContentNegotiation ---

func TestDaemonContentNegotiation(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()

	// 1. Start daemon with httptest.NewServer
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 2. Request with Accept: text/html -> get HTML
	req, err := http.NewRequest("GET", ts.URL+"/", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/html")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "<!DOCTYPE html>")
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	// 3. Request with Accept: application/json -> get JSON
	req2, err := http.NewRequest("GET", ts.URL+"/", nil)
	require.NoError(t, err)
	req2.Header.Set("Accept", "application/json")

	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	assert.Contains(t, resp2.Header.Get("Content-Type"), "application/json")
	var jsonData map[string]interface{}
	err = json.Unmarshal(body2, &jsonData)
	require.NoError(t, err)

	// 4. Request with Accept: text/markdown -> get Markdown
	req3, err := http.NewRequest("GET", ts.URL+"/", nil)
	require.NoError(t, err)
	req3.Header.Set("Accept", "text/markdown")

	resp3, err := http.DefaultClient.Do(req3)
	require.NoError(t, err)
	defer resp3.Body.Close()

	assert.Equal(t, http.StatusOK, resp3.StatusCode)
	body3, err := io.ReadAll(resp3.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body3), "# BitFS")
	assert.Contains(t, resp3.Header.Get("Content-Type"), "text/markdown")

	// 5. Default (no Accept header) -> JSON
	req4, err := http.NewRequest("GET", ts.URL+"/", nil)
	require.NoError(t, err)

	resp4, err := http.DefaultClient.Do(req4)
	require.NoError(t, err)
	defer resp4.Body.Close()

	assert.Equal(t, http.StatusOK, resp4.StatusCode)
	assert.Contains(t, resp4.Header.Get("Content-Type"), "application/json")
}

// --- TestDaemonHealthEndpoint ---

func TestDaemonHealthEndpoint(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()

	// 1. Start daemon
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 2. GET /_bitfs/health -> 200
	resp, err := http.Get(ts.URL + "/_bitfs/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"status":"ok"`)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// --- TestDaemonBSVAliasEndpoint ---

func TestDaemonBSVAliasEndpoint(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()

	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/bsvalias")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var caps map[string]interface{}
	err = json.Unmarshal(body, &caps)
	require.NoError(t, err)
	assert.Equal(t, "1.0", caps["bsvalias"])
	assert.NotNil(t, caps["capabilities"])
}

// --- TestDaemonCORSHeaders ---

func TestDaemonCORSHeaders(t *testing.T) {
	d, _ := createTestDaemon(t)
	handler := d.Handler()

	ts := httptest.NewServer(handler)
	defer ts.Close()

	req, err := http.NewRequest("OPTIONS", ts.URL+"/_bitfs/health", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, resp.Header.Get("Access-Control-Allow-Methods"))
}

// --- TestInvoicePriceCalculation ---

func TestInvoicePriceCalculation(t *testing.T) {
	tests := []struct {
		name       string
		pricePerKB uint64
		fileSize   uint64
		expected   uint64
	}{
		{"zero price", 0, 1024, 0},
		{"zero size", 100, 0, 0},
		{"exact 1KB", 100, 1024, 100},
		{"10KB", 100, 10240, 1000},
		{"partial KB rounds up", 100, 1025, 101},
		{"1 byte", 100, 1, 1},
		{"large file", 10, 1048576, 10240}, // 1MB
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			price := x402.CalculatePrice(tc.pricePerKB, tc.fileSize)
			assert.Equal(t, tc.expected, price)
		})
	}
}

// --- TestEndToEndPaymentFlow ---

func TestEndToEndPaymentFlow(t *testing.T) {
	// Simulates the full x402 payment flow without a real BSV node
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	// Seller setup
	sellerKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	// Encrypt content
	plaintext := []byte("Premium article that requires payment")
	encResult, err := method42.Encrypt(plaintext, sellerKey.PrivateKey, sellerKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// Buyer keypair
	buyerKey, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	// Compute capsule for HTLC (seller side)
	capsule, err := method42.ComputeCapsule(sellerKey.PrivateKey, sellerKey.PublicKey, buyerKey.PublicKey, encResult.KeyHash)
	require.NoError(t, err)
	fileTxID := bytes.Repeat([]byte{0xf0}, 32) // mock file txid
	capsuleHash := method42.ComputeCapsuleHash(fileTxID, capsule)

	// Create invoice
	invoice := x402.NewInvoice(100, uint64(len(plaintext)), "1SellerAddr...", capsuleHash, 3600)
	assert.Greater(t, invoice.Price, uint64(0))
	assert.False(t, invoice.IsExpired())

	// Build HTLC (buyer creates this)
	sellerPub := bytes.Repeat([]byte{0x03}, 33)
	htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerKey.PublicKey.Compressed(),
		SellerPubKey: sellerPub,
		SellerAddr:   bytes.Repeat([]byte{0x11}, 20),
		CapsuleHash:  capsuleHash,
		Amount:       invoice.Price,
		Timeout:      x402.DefaultHTLCTimeout,
	})
	require.NoError(t, err)
	require.NotEmpty(t, htlcScript)

	// After HTLC resolves, buyer gets the capsule and decrypts
	decResult, err := method42.DecryptWithCapsule(encResult.Ciphertext, capsule, encResult.KeyHash, buyerKey.PrivateKey, sellerKey.PublicKey)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)
}

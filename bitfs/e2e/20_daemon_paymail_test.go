//go:build e2e

package e2e

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// setupPaymailServer creates a daemon httptest.Server configured for
// BSV Alias / Paymail testing. Returns the server and the underlying wallet.
func setupPaymailServer(t *testing.T) (*httptest.Server, *wallet.Wallet) {
	t.Helper()

	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	walletSvc := &testWalletService{w: w}
	metanetSvc := &testMetanetService{nodes: map[string]*daemon.NodeInfo{}}

	fileStore, err := storage.NewFileStore(t.TempDir())
	require.NoError(t, err, "create file store")

	config := daemon.DefaultConfig()
	config.Security.RateLimit.RPM = 0 // disable rate limiting for tests

	d, err := daemon.New(config, walletSvc, fileStore, metanetSvc)
	require.NoError(t, err, "create daemon")

	server := httptest.NewServer(d.Handler())
	t.Cleanup(server.Close)

	return server, w
}

// TestBSVAliasCapabilities verifies the BSV Alias well-known endpoint:
//
//   - GET /.well-known/bsvalias returns 200
//   - Response is valid JSON with "bsvalias" version "1.0"
//   - Response contains a "capabilities" map with at least "pki" entry
//   - PKI capability URL contains the expected path template
func TestBSVAliasCapabilities(t *testing.T) {
	server, _ := setupPaymailServer(t)

	resp, err := http.Get(server.URL + "/.well-known/bsvalias")
	require.NoError(t, err, "GET /.well-known/bsvalias")
	defer resp.Body.Close()

	// Verify 200 OK.
	require.Equal(t, http.StatusOK, resp.StatusCode, "should return 200")
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json",
		"Content-Type should be JSON")

	// Parse the JSON response.
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read response body")

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "unmarshal JSON response")

	// Verify "bsvalias" version field.
	bsvalias, ok := result["bsvalias"].(string)
	require.True(t, ok, "bsvalias field should be a string")
	assert.Equal(t, "1.0", bsvalias, "bsvalias version should be 1.0")

	// Verify "capabilities" map exists and has expected entries.
	capsRaw, ok := result["capabilities"]
	require.True(t, ok, "capabilities field should exist")

	caps, ok := capsRaw.(map[string]interface{})
	require.True(t, ok, "capabilities should be a map")

	// Verify PKI capability exists and contains the expected URL template.
	pkiURL, ok := caps["pki"].(string)
	require.True(t, ok, "pki capability should be a string URL")
	assert.Contains(t, pkiURL, "/api/v1/pki/", "PKI URL should contain the API path")
	assert.Contains(t, pkiURL, "{alias}", "PKI URL should contain {alias} template")
	assert.Contains(t, pkiURL, "{domain.tld}", "PKI URL should contain {domain.tld} template")

	// Verify public-profile capability exists (f12f968c92d6 is the BSV Alias
	// capability ID for public profiles).
	profileURL, ok := caps["f12f968c92d6"].(string)
	require.True(t, ok, "public-profile capability should be present")
	assert.Contains(t, profileURL, "/api/v1/public-profile/",
		"public-profile URL should contain expected path")

	t.Logf("BSV Alias capabilities: bsvalias=%s, pki=%s", bsvalias, pkiURL)
}

// TestPKILookup verifies the PKI endpoint resolves a valid handle to a
// compressed public key:
//
//   - GET /api/v1/pki/{alias}@{domain} returns 200
//   - Response contains "bsvalias", "handle", and "pubkey" fields
//   - Pubkey is a valid 33-byte compressed hex string (02 or 03 prefix)
func TestPKILookup(t *testing.T) {
	server, w := setupPaymailServer(t)

	// Derive the expected public key from the wallet (vault 0).
	vaultKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err, "derive vault root key")
	expectedPubKey := hex.EncodeToString(vaultKey.PublicKey.Compressed())

	// The testWalletService.GetVaultPubKey always returns vault 0 root key
	// regardless of alias, so any alias@domain will work.
	handle := "alice@bitfs.org"

	resp, err := http.Get(server.URL + "/api/v1/pki/" + handle)
	require.NoError(t, err, "GET /api/v1/pki/"+handle)
	defer resp.Body.Close()

	// Verify 200 OK.
	require.Equal(t, http.StatusOK, resp.StatusCode, "should return 200")
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json",
		"Content-Type should be JSON")

	// Parse the response.
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read response body")

	var pkiResp struct {
		BSVAlias string `json:"bsvalias"`
		Handle   string `json:"handle"`
		PubKey   string `json:"pubkey"`
	}
	err = json.Unmarshal(body, &pkiResp)
	require.NoError(t, err, "unmarshal PKI response")

	// Verify fields.
	assert.Equal(t, "1.0", pkiResp.BSVAlias, "bsvalias should be 1.0")
	assert.Equal(t, handle, pkiResp.Handle, "handle should echo back the input")
	assert.Equal(t, expectedPubKey, pkiResp.PubKey,
		"pubkey should match the wallet's vault 0 root key")

	// Verify pubkey is a valid 33-byte compressed public key.
	pubKeyBytes, err := hex.DecodeString(pkiResp.PubKey)
	require.NoError(t, err, "pubkey should be valid hex")
	assert.Len(t, pubKeyBytes, 33, "compressed public key should be 33 bytes")
	assert.True(t, pubKeyBytes[0] == 0x02 || pubKeyBytes[0] == 0x03,
		"compressed public key prefix should be 0x02 or 0x03, got 0x%02x", pubKeyBytes[0])

	t.Logf("PKI lookup: handle=%s, pubkey=%s...%s",
		pkiResp.Handle, pkiResp.PubKey[:8], pkiResp.PubKey[len(pkiResp.PubKey)-8:])
}

// TestPKINotFound verifies the PKI endpoint returns an error for a
// nonexistent alias. Since the testWalletService always returns the vault 0
// key for any alias, we test with a malformed handle (no @ sign) which
// triggers INVALID_HANDLE, and also test that the endpoint correctly rejects
// handles with empty parts.
func TestPKINotFound(t *testing.T) {
	server, _ := setupPaymailServer(t)

	tests := []struct {
		name       string
		handle     string
		wantCode   int
		wantErrStr string
	}{
		{
			name:       "malformed_no_at_sign",
			handle:     "nonexistent",
			wantCode:   http.StatusBadRequest,
			wantErrStr: "INVALID_HANDLE",
		},
		{
			name:       "empty_alias",
			handle:     "@bitfs.org",
			wantCode:   http.StatusBadRequest,
			wantErrStr: "INVALID_HANDLE",
		},
		{
			name:       "empty_domain",
			handle:     "alice@",
			wantCode:   http.StatusBadRequest,
			wantErrStr: "INVALID_HANDLE",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(server.URL + "/api/v1/pki/" + tc.handle)
			require.NoError(t, err, "GET /api/v1/pki/"+tc.handle)
			defer resp.Body.Close()

			assert.Equal(t, tc.wantCode, resp.StatusCode,
				"expected HTTP %d for handle %q", tc.wantCode, tc.handle)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(body), tc.wantErrStr,
				"error response should contain %q", tc.wantErrStr)

			t.Logf("%s: HTTP %d, body contains %q", tc.name, resp.StatusCode, tc.wantErrStr)
		})
	}
}

// TestPKIDifferentAliases verifies that the PKI endpoint correctly echoes
// back the full handle including different domains, and that the pubkey
// returned is consistent for the same alias regardless of domain.
func TestPKIDifferentAliases(t *testing.T) {
	server, _ := setupPaymailServer(t)

	// Test multiple handles with different domains.
	handles := []string{
		"alice@bitfs.org",
		"alice@example.com",
		"bob@metanet.org",
	}

	var firstPubKey string
	for _, handle := range handles {
		t.Run(handle, func(t *testing.T) {
			resp, err := http.Get(server.URL + "/api/v1/pki/" + handle)
			require.NoError(t, err, "GET /api/v1/pki/"+handle)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode, "should return 200 for %s", handle)

			var pkiResp struct {
				BSVAlias string `json:"bsvalias"`
				Handle   string `json:"handle"`
				PubKey   string `json:"pubkey"`
			}
			err = json.NewDecoder(resp.Body).Decode(&pkiResp)
			require.NoError(t, err, "decode PKI response for %s", handle)

			// Handle should be echoed back exactly.
			assert.Equal(t, handle, pkiResp.Handle,
				"handle should be echoed back exactly")

			// All aliases resolve to the same vault 0 key via testWalletService.
			if firstPubKey == "" {
				firstPubKey = pkiResp.PubKey
			} else {
				assert.Equal(t, firstPubKey, pkiResp.PubKey,
					"all aliases should resolve to the same vault key in test")
			}

			t.Logf("handle=%s -> pubkey=%s...", handle, pkiResp.PubKey[:16])
		})
	}
}

// TestBSVAliasCapabilitiesURLBase verifies that the capabilities document
// uses the correct base URL derived from the Host header. Since the test
// server uses http (not TLS), URLs should start with http://.
func TestBSVAliasCapabilitiesURLBase(t *testing.T) {
	server, _ := setupPaymailServer(t)

	resp, err := http.Get(server.URL + "/.well-known/bsvalias")
	require.NoError(t, err, "GET /.well-known/bsvalias")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err, "decode response")

	caps, ok := result["capabilities"].(map[string]interface{})
	require.True(t, ok, "capabilities should be a map")

	pkiURL, ok := caps["pki"].(string)
	require.True(t, ok, "pki capability should be a string")

	// The daemon uses http:// when TLS is disabled (default for tests).
	assert.Contains(t, pkiURL, "http://",
		"capability URLs should use http:// when TLS is disabled")

	t.Logf("PKI URL template: %s", pkiURL)
}

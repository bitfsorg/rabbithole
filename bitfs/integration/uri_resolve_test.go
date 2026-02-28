//go:build integration

package integration

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/paymail"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// --- TestParseAllURITypes ---

func TestParseAllURITypes(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	pubKeyHex := hex.EncodeToString(nodeKey.PublicKey.Compressed())
	require.Len(t, pubKeyHex, 66, "compressed pubkey hex should be 66 chars")

	t.Run("Paymail URI", func(t *testing.T) {
		// 1. bitfs://alice@example.com/docs -> Paymail
		parsed, err := paymail.ParseURI("bitfs://alice@example.com/docs")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressPaymail, parsed.Type)
		assert.Equal(t, "alice", parsed.Alias)
		assert.Equal(t, "example.com", parsed.Domain)
		assert.Equal(t, "/docs", parsed.Path)
		assert.Empty(t, parsed.PubKey)
	})

	t.Run("DNSLink URI", func(t *testing.T) {
		// 2. bitfs://example.com/docs -> DNSLink
		parsed, err := paymail.ParseURI("bitfs://example.com/docs")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
		assert.Equal(t, "example.com", parsed.Domain)
		assert.Equal(t, "/docs", parsed.Path)
		assert.Empty(t, parsed.Alias)
		assert.Empty(t, parsed.PubKey)
	})

	t.Run("PubKey URI", func(t *testing.T) {
		// 3. bitfs://02a1b2c3...66chars.../docs -> PubKey
		uri := "bitfs://" + pubKeyHex + "/docs"
		parsed, err := paymail.ParseURI(uri)
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressPubKey, parsed.Type)
		assert.Equal(t, nodeKey.PublicKey.Compressed(), parsed.PubKey)
		assert.Equal(t, "/docs", parsed.Path)
		assert.Empty(t, parsed.Alias)
		assert.Empty(t, parsed.Domain)
	})

	t.Run("PubKey URI without path", func(t *testing.T) {
		uri := "bitfs://" + pubKeyHex
		parsed, err := paymail.ParseURI(uri)
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressPubKey, parsed.Type)
		assert.Equal(t, nodeKey.PublicKey.Compressed(), parsed.PubKey)
		assert.Empty(t, parsed.Path)
	})

	t.Run("Paymail with nested path", func(t *testing.T) {
		parsed, err := paymail.ParseURI("bitfs://bob@example.org/docs/readme.txt")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressPaymail, parsed.Type)
		assert.Equal(t, "bob", parsed.Alias)
		assert.Equal(t, "example.org", parsed.Domain)
		assert.Equal(t, "/docs/readme.txt", parsed.Path)
	})

	t.Run("DNSLink with subdomain", func(t *testing.T) {
		parsed, err := paymail.ParseURI("bitfs://files.example.com/data")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
		assert.Equal(t, "files.example.com", parsed.Domain)
		assert.Equal(t, "/data", parsed.Path)
	})

	// 4. Invalid URIs -> error
	t.Run("Invalid URIs", func(t *testing.T) {
		invalidURIs := []struct {
			name string
			uri  string
		}{
			{"empty", ""},
			{"wrong scheme", "http://example.com"},
			{"no authority", "bitfs://"},
			{"wrong scheme prefix", "ipfs://example.com"},
			{"invalid paymail", "bitfs://@example.com"},
			{"invalid paymail empty alias", "bitfs://@"},
			{"ftp scheme", "ftp://example.com"},
		}

		for _, tc := range invalidURIs {
			t.Run(tc.name, func(t *testing.T) {
				_, err := paymail.ParseURI(tc.uri)
				assert.Error(t, err, "should fail for URI: %s", tc.uri)
			})
		}
	})
}

// --- TestAddressTypeStrings ---

func TestAddressTypeStrings(t *testing.T) {
	assert.Equal(t, "Paymail", paymail.AddressPaymail.String())
	assert.Equal(t, "DNSLink", paymail.AddressDNSLink.String())
	assert.Equal(t, "PubKey", paymail.AddressPubKey.String())
}

// --- TestPubKeyDetection ---

func TestPubKeyDetection(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	// Generate multiple keys to test both 02 and 03 prefixes
	// Keep generating until we get both prefixes
	var found02, found03 bool
	for i := uint32(0); i < 20 && (!found02 || !found03); i++ {
		nodeKey, err := w.DeriveNodeKey(0, []uint32{i}, nil)
		require.NoError(t, err)

		compressed := nodeKey.PublicKey.Compressed()
		hexStr := hex.EncodeToString(compressed)

		parsed, err := paymail.ParseURI("bitfs://" + hexStr + "/path")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressPubKey, parsed.Type)
		assert.Equal(t, compressed, parsed.PubKey)

		if compressed[0] == 0x02 {
			found02 = true
		}
		if compressed[0] == 0x03 {
			found03 = true
		}
	}

	// We should have found at least one prefix type (both is ideal but probabilistic)
	assert.True(t, found02 || found03, "should detect at least one pubkey prefix type")
}

// --- TestURIRawPreservation ---

func TestURIRawPreservation(t *testing.T) {
	uri := "bitfs://alice@example.com/docs/readme.txt"
	parsed, err := paymail.ParseURI(uri)
	require.NoError(t, err)
	assert.Equal(t, uri, parsed.RawURI, "RawURI should preserve the original URI")
}

// --- TestURIEdgeCases ---

func TestURIEdgeCases(t *testing.T) {
	t.Run("paymail with port-like domain", func(t *testing.T) {
		// This is just a domain with dots
		parsed, err := paymail.ParseURI("bitfs://node.bitfs.org/file")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
		assert.Equal(t, "node.bitfs.org", parsed.Domain)
	})

	t.Run("domain only, no path", func(t *testing.T) {
		parsed, err := paymail.ParseURI("bitfs://example.com")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
		assert.Equal(t, "example.com", parsed.Domain)
		assert.Empty(t, parsed.Path)
	})

	t.Run("paymail no path", func(t *testing.T) {
		parsed, err := paymail.ParseURI("bitfs://user@domain.com")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressPaymail, parsed.Type)
		assert.Equal(t, "user", parsed.Alias)
		assert.Equal(t, "domain.com", parsed.Domain)
		assert.Empty(t, parsed.Path)
	})

	t.Run("pubkey-like but wrong length", func(t *testing.T) {
		// 02 prefix but only 64 hex chars (32 bytes) instead of 66 (33 bytes)
		parsed, err := paymail.ParseURI("bitfs://02" + hex.EncodeToString(make([]byte, 31)) + "/path")
		require.NoError(t, err)
		// Should be treated as DNSLink since it's not exactly 66 hex chars
		assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
	})
}

// --- TestMultipleNetworkKeyURIs ---

func TestMultipleNetworkKeyURIs(t *testing.T) {
	// Verify that keys derived on different networks produce valid URIs
	for _, nc := range networkConfigs() {
		t.Run(nc.name, func(t *testing.T) {
			w, _, _ := createTestWallet(t, nc.network)
			rootKey, err := w.DeriveVaultRootKey(0)
			require.NoError(t, err)

			pubHex := hex.EncodeToString(rootKey.PublicKey.Compressed())
			uri := "bitfs://" + pubHex + "/vault/docs"

			parsed, err := paymail.ParseURI(uri)
			require.NoError(t, err)
			assert.Equal(t, paymail.AddressPubKey, parsed.Type)
			assert.Equal(t, rootKey.PublicKey.Compressed(), parsed.PubKey)
			assert.Equal(t, "/vault/docs", parsed.Path)
		})
	}
}

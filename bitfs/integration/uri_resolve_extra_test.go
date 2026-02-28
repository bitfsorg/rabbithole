//go:build integration

package integration

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/paymail"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// --- TestParseURIWithTrailingSlash ---

func TestParseURIWithTrailingSlash(t *testing.T) {
	parsed, err := paymail.ParseURI("bitfs://example.com/docs/")
	require.NoError(t, err)
	assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
	assert.Equal(t, "example.com", parsed.Domain)
	assert.Equal(t, "/docs/", parsed.Path, "trailing slash should be preserved in path")
}

// --- TestParseURIDeepNestedPath ---

func TestParseURIDeepNestedPath(t *testing.T) {
	parsed, err := paymail.ParseURI("bitfs://alice@example.com/a/b/c/d/e/f/g.txt")
	require.NoError(t, err)
	assert.Equal(t, paymail.AddressPaymail, parsed.Type)
	assert.Equal(t, "alice", parsed.Alias)
	assert.Equal(t, "example.com", parsed.Domain)
	assert.Equal(t, "/a/b/c/d/e/f/g.txt", parsed.Path)
}

// --- TestParseURICaseSensitiveDomain ---

func TestParseURICaseSensitiveDomain(t *testing.T) {
	// The parser preserves domain as-is (does not lowercase).
	// Test that the result is at least consistent.
	parsed, err := paymail.ParseURI("bitfs://Example.COM/path")
	require.NoError(t, err)
	assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
	// Domain should be preserved or lowercased consistently
	assert.Equal(t, "Example.COM", parsed.Domain,
		"domain should be preserved as given by the parser")
	assert.Equal(t, "/path", parsed.Path)
}

// --- TestParseURIPubKeyWith02Prefix ---

func TestParseURIPubKeyWith02Prefix(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	// Keep generating keys until we find one with 0x02 prefix
	for i := uint32(0); i < 100; i++ {
		nodeKey, err := w.DeriveNodeKey(0, []uint32{i}, nil)
		require.NoError(t, err)

		compressed := nodeKey.PublicKey.Compressed()
		if compressed[0] != 0x02 {
			continue
		}

		hexStr := hex.EncodeToString(compressed)
		require.True(t, strings.HasPrefix(hexStr, "02"))

		parsed, err := paymail.ParseURI("bitfs://" + hexStr + "/file.txt")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressPubKey, parsed.Type)
		assert.Equal(t, compressed, parsed.PubKey)
		assert.Equal(t, "/file.txt", parsed.Path)
		return
	}

	t.Skip("could not generate a 0x02-prefix key in 100 attempts")
}

// --- TestParseURIPubKeyWith03Prefix ---

func TestParseURIPubKeyWith03Prefix(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	// Keep generating keys until we find one with 0x03 prefix
	for i := uint32(0); i < 100; i++ {
		nodeKey, err := w.DeriveNodeKey(0, []uint32{i}, nil)
		require.NoError(t, err)

		compressed := nodeKey.PublicKey.Compressed()
		if compressed[0] != 0x03 {
			continue
		}

		hexStr := hex.EncodeToString(compressed)
		require.True(t, strings.HasPrefix(hexStr, "03"))

		parsed, err := paymail.ParseURI("bitfs://" + hexStr + "/data")
		require.NoError(t, err)
		assert.Equal(t, paymail.AddressPubKey, parsed.Type)
		assert.Equal(t, compressed, parsed.PubKey)
		assert.Equal(t, "/data", parsed.Path)
		return
	}

	t.Skip("could not generate a 0x03-prefix key in 100 attempts")
}

// --- TestParseURIPubKeyWithInvalidPrefix ---

func TestParseURIPubKeyWithInvalidPrefix(t *testing.T) {
	// 04 prefix (uncompressed) with 64 hex chars = 66 total
	// Should NOT be detected as a pubkey since we only accept 02/03 prefix
	fakeUncompressed := "04" + strings.Repeat("ab", 32) // 04 + 64 hex chars = 66 chars
	require.Len(t, fakeUncompressed, 66)

	parsed, err := paymail.ParseURI("bitfs://" + fakeUncompressed + "/path")
	require.NoError(t, err)
	assert.NotEqual(t, paymail.AddressPubKey, parsed.Type,
		"04-prefix should NOT be detected as PubKey")
	assert.Equal(t, paymail.AddressDNSLink, parsed.Type,
		"04-prefix authority should fall through to DNSLink")
}

// --- TestParseURIPaymailAliasParsing ---

func TestParseURIPaymailAliasParsing(t *testing.T) {
	parsed, err := paymail.ParseURI("bitfs://alice.bob@sub.example.com/file")
	require.NoError(t, err)
	assert.Equal(t, paymail.AddressPaymail, parsed.Type)
	assert.Equal(t, "alice.bob", parsed.Alias,
		"alias should include dots before the @ sign")
	assert.Equal(t, "sub.example.com", parsed.Domain)
	assert.Equal(t, "/file", parsed.Path)
}

// --- TestParseURISchemeVariations ---

func TestParseURISchemeVariations(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{"uppercase BITFS", "BITFS://example.com/path", true},
		{"mixed case Bitfs", "Bitfs://example.com/path", true},
		{"lowercase bitfs", "bitfs://example.com/path", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := paymail.ParseURI(tc.uri)
			if tc.wantErr {
				assert.Error(t, err, "non-lowercase scheme %q should be rejected", tc.uri)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- TestParseURIEmptyPath ---

func TestParseURIEmptyPath(t *testing.T) {
	parsed, err := paymail.ParseURI("bitfs://example.com")
	require.NoError(t, err)
	assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
	assert.Equal(t, "example.com", parsed.Domain)
	assert.Empty(t, parsed.Path, "no path component should produce empty path")
}

// --- TestParseURIRootPath ---

func TestParseURIRootPath(t *testing.T) {
	parsed, err := paymail.ParseURI("bitfs://example.com/")
	require.NoError(t, err)
	assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
	assert.Equal(t, "example.com", parsed.Domain)
	assert.Equal(t, "/", parsed.Path, "trailing slash only should produce path '/'")
}

// --- TestParsedURIRawPreservation ---

func TestParsedURIRawPreservation(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	pubHex := hex.EncodeToString(rootKey.PublicKey.Compressed())

	testCases := []struct {
		name string
		uri  string
	}{
		{"paymail", "bitfs://alice@example.com/docs/readme.txt"},
		{"dnslink", "bitfs://files.example.org/data/archive"},
		{"pubkey", "bitfs://" + pubHex + "/vault/docs"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := paymail.ParseURI(tc.uri)
			require.NoError(t, err)
			assert.Equal(t, tc.uri, parsed.RawURI,
				"RawURI must match original input for %s", tc.name)
		})
	}
}

// --- TestAddressTypeStringValues ---

func TestAddressTypeStringValues(t *testing.T) {
	assert.Equal(t, "Paymail", paymail.AddressPaymail.String())
	assert.Equal(t, "DNSLink", paymail.AddressDNSLink.String())
	assert.Equal(t, "PubKey", paymail.AddressPubKey.String())
}

// --- TestParseURIMultipleAtSigns ---

func TestParseURIMultipleAtSigns(t *testing.T) {
	// "bitfs://a@b@c.com/path" - SplitN with N=2 should treat "a" as alias, "b@c.com" as domain
	parsed, err := paymail.ParseURI("bitfs://a@b@c.com/path")
	if err != nil {
		// If the parser rejects it, that is also acceptable
		assert.Error(t, err)
		return
	}
	// If it succeeds, the behavior should be consistent
	assert.Equal(t, paymail.AddressPaymail, parsed.Type)
	// SplitN("a@b@c.com", "@", 2) => ["a", "b@c.com"]
	assert.Equal(t, "a", parsed.Alias)
	assert.Equal(t, "b@c.com", parsed.Domain)
	assert.Equal(t, "/path", parsed.Path)
}

// --- TestParseURIUnicodeInDomain ---

func TestParseURIUnicodeInDomain(t *testing.T) {
	// Non-ASCII domain - parser should handle it (either accept or error consistently)
	parsed, err := paymail.ParseURI("bitfs://\u4f8b\u3048.jp/docs")
	if err != nil {
		// Rejecting non-ASCII domains is acceptable
		assert.Error(t, err)
		return
	}
	// If accepted, should be DNSLink with the domain preserved
	assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
	assert.Equal(t, "\u4f8b\u3048.jp", parsed.Domain)
	assert.Equal(t, "/docs", parsed.Path)
}

// --- TestParseURIVeryLongPath ---

func TestParseURIVeryLongPath(t *testing.T) {
	// 200-char path
	longPath := "/" + strings.Repeat("a", 200)
	uri := "bitfs://example.com" + longPath

	parsed, err := paymail.ParseURI(uri)
	require.NoError(t, err, "long path should not cause an error")
	assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
	assert.Equal(t, longPath, parsed.Path)
	assert.Len(t, parsed.Path, 201) // leading slash + 200 chars
}

// --- TestParseURIAllInvalidCases ---

func TestParseURIAllInvalidCases(t *testing.T) {
	invalidCases := []struct {
		name string
		uri  string
	}{
		{"empty string", ""},
		{"http scheme", "http://example.com"},
		{"no authority", "bitfs://"},
		{"ftp scheme", "ftp://x"},
		{"at-only authority", "bitfs://@"},
		{"at-domain no alias", "bitfs://@domain.com"},
		{"ipfs scheme", "ipfs://example.com"},
		{"no scheme", "example.com/path"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := paymail.ParseURI(tc.uri)
			assert.Error(t, err, "should fail for URI: %q", tc.uri)
		})
	}
}

// --- TestParseURIDNSLinkSingleLabel ---

func TestParseURIDNSLinkSingleLabel(t *testing.T) {
	parsed, err := paymail.ParseURI("bitfs://localhost/path")
	require.NoError(t, err)
	assert.Equal(t, paymail.AddressDNSLink, parsed.Type)
	assert.Equal(t, "localhost", parsed.Domain)
	assert.Equal(t, "/path", parsed.Path)
}

// --- TestParseURIPubKeyExactLength ---

func TestParseURIPubKeyExactLength(t *testing.T) {
	// Exactly 66 hex chars with 02 prefix -> should be detected as PubKey
	valid66 := "02" + strings.Repeat("ab", 32) // 2 + 64 = 66
	require.Len(t, valid66, 66)

	parsed, err := paymail.ParseURI("bitfs://" + valid66 + "/path")
	require.NoError(t, err)
	assert.Equal(t, paymail.AddressPubKey, parsed.Type,
		"exactly 66 hex chars with 02 prefix should be PubKey")

	// 64 hex chars with 02 prefix -> NOT PubKey (too short)
	short64 := "02" + strings.Repeat("ab", 31) // 2 + 62 = 64
	require.Len(t, short64, 64)

	parsed2, err := paymail.ParseURI("bitfs://" + short64 + "/path")
	require.NoError(t, err)
	assert.NotEqual(t, paymail.AddressPubKey, parsed2.Type,
		"64 hex chars should NOT be detected as PubKey")

	// 68 hex chars with 02 prefix -> NOT PubKey (too long)
	long68 := "02" + strings.Repeat("ab", 33) // 2 + 66 = 68
	require.Len(t, long68, 68)

	parsed3, err := paymail.ParseURI("bitfs://" + long68 + "/path")
	require.NoError(t, err)
	assert.NotEqual(t, paymail.AddressPubKey, parsed3.Type,
		"68 hex chars should NOT be detected as PubKey")
}

// --- TestPaymailCapabilitiesFields ---

func TestPaymailCapabilitiesFields(t *testing.T) {
	caps := &paymail.PaymailCapabilities{
		PKI:           "https://example.com/api/v1/bsvalias/id/{alias}@{domain.tld}",
		PublicProfile: "https://example.com/api/v1/bsvalias/public-profile/{alias}@{domain.tld}",
		VerifyPubKey:  "https://example.com/api/v1/bsvalias/verify-pubkey/{alias}@{domain.tld}/{pubkey}",
	}

	assert.NotEmpty(t, caps.PKI)
	assert.Contains(t, caps.PKI, "{alias}")
	assert.NotEmpty(t, caps.PublicProfile)
	assert.Contains(t, caps.PublicProfile, "public-profile")
	assert.NotEmpty(t, caps.VerifyPubKey)
	assert.Contains(t, caps.VerifyPubKey, "verify-pubkey")
}

// --- TestParseURIConsistencyAcrossNetworks ---

func TestParseURIConsistencyAcrossNetworks(t *testing.T) {
	for _, nc := range networkConfigs() {
		t.Run(nc.name, func(t *testing.T) {
			w, _, _ := createTestWallet(t, nc.network)
			rootKey, err := w.DeriveVaultRootKey(0)
			require.NoError(t, err)

			pubHex := hex.EncodeToString(rootKey.PublicKey.Compressed())
			uri := "bitfs://" + pubHex + "/vault/docs"

			parsed, err := paymail.ParseURI(uri)
			require.NoError(t, err,
				"URI parsing should work for keys derived on %s", nc.name)
			assert.Equal(t, paymail.AddressPubKey, parsed.Type)
			assert.Equal(t, rootKey.PublicKey.Compressed(), parsed.PubKey)
			assert.Equal(t, "/vault/docs", parsed.Path)
			assert.Equal(t, uri, parsed.RawURI)
		})
	}
}

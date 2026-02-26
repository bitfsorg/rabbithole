package client

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPubKeyHex is a well-known compressed public key (33 bytes, prefix 02).
const testPubKeyHex = "02b4632d08485ff1df2db55b9dafd23347d1c47a457072a1e87be26896549a8737"

// mockDNS implements paymail.DNSResolver for testing.
type mockDNS struct {
	srvRecords map[string][]*net.SRV // key: "service.domain"
	txtRecords map[string][]string   // key: domain
}

func (m *mockDNS) LookupSRV(service, proto, name string) (string, []*net.SRV, error) {
	key := service + "." + name
	if recs, ok := m.srvRecords[key]; ok {
		return "", recs, nil
	}
	return "", nil, fmt.Errorf("no SRV records for %s", key)
}

func (m *mockDNS) LookupTXT(name string) ([]string, error) {
	if recs, ok := m.txtRecords[name]; ok {
		return recs, nil
	}
	return nil, fmt.Errorf("no TXT records for %s", name)
}

func TestResolveURI_PubKey_WithHostOverride(t *testing.T) {
	uri := "bitfs://" + testPubKeyHex + "/docs/readme.txt"
	result, err := ResolveURI(uri, "http://example.com:8080", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, testPubKeyHex, result.PNode)
	assert.Equal(t, "/docs/readme.txt", result.Path)
	assert.Equal(t, "http://example.com:8080", result.Client.BaseURL)
}

func TestResolveURI_PubKey_NoHost_Error(t *testing.T) {
	uri := "bitfs://" + testPubKeyHex + "/docs"
	_, err := ResolveURI(uri, "", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--host")
}

func TestResolveURI_Paymail(t *testing.T) {
	// Paymail resolution requires HTTPS mocking — covered by integration tests.
	t.Skip("paymail resolution requires HTTPS mocking — covered by integration tests")
}

func TestResolveURI_DNSLink(t *testing.T) {
	dns := &mockDNS{
		txtRecords: map[string][]string{
			"_bitfs.example.com": {"bitfs=" + testPubKeyHex},
		},
		srvRecords: map[string][]*net.SRV{
			"bitfs.example.com": {{Target: "cdn.example.com.", Port: 443, Priority: 1, Weight: 100}},
		},
	}

	uri := "bitfs://example.com/docs/readme.txt"
	result, err := ResolveURI(uri, "", nil, dns)
	require.NoError(t, err)
	assert.Equal(t, testPubKeyHex, result.PNode)
	assert.Equal(t, "/docs/readme.txt", result.Path)
	assert.Equal(t, "https://cdn.example.com:443", result.Client.BaseURL)
}

func TestResolveURI_DNSLink_WithHostOverride(t *testing.T) {
	dns := &mockDNS{
		txtRecords: map[string][]string{
			"_bitfs.example.com": {"bitfs=" + testPubKeyHex},
		},
	}

	uri := "bitfs://example.com/docs"
	result, err := ResolveURI(uri, "http://localhost:9090", nil, dns)
	require.NoError(t, err)
	assert.Equal(t, testPubKeyHex, result.PNode)
	assert.Equal(t, "/docs", result.Path)
	assert.Equal(t, "http://localhost:9090", result.Client.BaseURL)
}

func TestResolveURI_InvalidURI(t *testing.T) {
	_, err := ResolveURI("not-a-uri", "", nil, nil)
	require.Error(t, err)
}

func TestResolveURI_EmptyPath_DefaultsToRoot(t *testing.T) {
	uri := "bitfs://" + testPubKeyHex
	result, err := ResolveURI(uri, "http://localhost:8080", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "/", result.Path)
}

func TestEndpointToBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		expected string
	}{
		{"bare host:port", "example.com:8080", "https://example.com:8080"},
		{"has https", "https://example.com", "https://example.com"},
		{"has http", "http://example.com", "http://example.com"},
		{"bare host", "example.com", "https://example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, endpointToBaseURL(tt.endpoint))
		})
	}
}

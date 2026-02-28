package client

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/bitfsorg/libbitfs-go/paymail"
)

// ResolveResult holds the resolved connection parameters from a bitfs:// URI.
type ResolveResult struct {
	Client *Client // HTTP client connected to the resolved endpoint.
	PNode  string  // Hex-encoded 33-byte compressed pubkey (66 chars).
	Path   string  // Path component from URI (with leading /).
}

// ResolveURI resolves a bitfs:// URI into a connected Client, pnode, and path.
//
// If hostOverride is non-empty, it is used as the daemon base URL
// (skipping SRV endpoint resolution, but pubkey is still resolved for
// paymail/dnslink URIs).
//
// For bare pubkey URIs (bitfs://02abc.../path) without hostOverride,
// an error is returned since there is no domain to resolve endpoints from.
//
// httpClient and dnsResolver may be nil to use defaults.
func ResolveURI(uri, hostOverride string, httpClient paymail.HTTPClient, dnsResolver paymail.DNSResolver) (*ResolveResult, error) {
	if httpClient == nil {
		httpClient = paymail.DefaultHTTPClient
	}
	if dnsResolver == nil {
		dnsResolver = paymail.DefaultDNSResolver
	}

	pubKey, endpoints, err := paymail.ResolveURIWith(uri, httpClient, dnsResolver)
	if err != nil {
		return nil, fmt.Errorf("resolve URI: %w", err)
	}

	pnode := hex.EncodeToString(pubKey)

	// Determine daemon base URL.
	var baseURL string
	switch {
	case hostOverride != "":
		baseURL = strings.TrimRight(hostOverride, "/")
	case len(endpoints) > 0:
		baseURL = endpointToBaseURL(endpoints[0])
	default:
		return nil, fmt.Errorf("bare pubkey URI requires --host flag to specify the daemon address")
	}

	// Extract path from the parsed URI.
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		return nil, fmt.Errorf("resolve URI: %w", err)
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}

	return &ResolveResult{
		Client: New(baseURL),
		PNode:  pnode,
		Path:   path,
	}, nil
}

// endpointToBaseURL prepends "https://" to an endpoint only if it does not
// already have a scheme (L-NEW-21).
func endpointToBaseURL(endpoint string) string {
	if strings.HasPrefix(endpoint, "https://") || strings.HasPrefix(endpoint, "http://") {
		return endpoint
	}
	return "https://" + endpoint
}

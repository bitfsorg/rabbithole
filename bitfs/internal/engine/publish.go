package engine

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
)

// DNSResolver is an interface for DNS TXT record lookups.
// It is injectable for testability.
type DNSResolver interface {
	LookupTXT(name string) ([]string, error)
}

// netDNSResolver is the default DNS resolver using net.LookupTXT.
type netDNSResolver struct{}

func (r *netDNSResolver) LookupTXT(name string) ([]string, error) {
	return net.LookupTXT(name)
}

// DefaultDNSResolver returns the default DNS resolver backed by net.LookupTXT.
func DefaultDNSResolver() DNSResolver {
	return &netDNSResolver{}
}

// PublishOpts holds options for the Publish operation.
type PublishOpts struct {
	VaultIndex uint32
	Domain     string
}

// Publish binds a domain to a vault via DNSLink. When a domain is provided,
// it derives the vault root pubkey, outputs DNS TXT record instructions,
// attempts DNS verification, and stores the binding in local state.
// When no domain is provided (empty string), it lists all existing bindings
// and re-verifies each against DNS.
func (e *Engine) Publish(opts *PublishOpts) (*Result, error) {
	if opts.Domain == "" {
		return e.listPublishBindings()
	}
	return e.publishDomain(opts)
}

// publishDomain handles the case where a specific domain is provided.
func (e *Engine) publishDomain(opts *PublishOpts) (*Result, error) {
	if strings.ContainsAny(opts.Domain, " \t\n\r/\\") || !strings.Contains(opts.Domain, ".") {
		return nil, fmt.Errorf("engine: invalid domain: %q", opts.Domain)
	}

	rootKP, err := e.Wallet.DeriveVaultRootKey(opts.VaultIndex)
	if err != nil {
		return nil, fmt.Errorf("engine: derive vault root key: %w", err)
	}

	rootPubHex := hex.EncodeToString(rootKP.PublicKey.Compressed())

	// Build the DNS instruction message.
	var msg strings.Builder
	fmt.Fprintf(&msg, "To publish vault to %s, add the following DNS TXT record:\n\n", opts.Domain)
	fmt.Fprintf(&msg, "  _bitfs.%s  TXT  \"bitfs=%s\"\n\n", opts.Domain, rootPubHex)
	fmt.Fprintf(&msg, "Then visitors can access your content at:\n")
	fmt.Fprintf(&msg, "  bitfs://%s/\n", opts.Domain)
	fmt.Fprintf(&msg, "  https://%s/ (via daemon)\n", opts.Domain)

	// Attempt DNS verification.
	verified := false
	resolver := e.dnsResolver()
	dnsPubHex, err := lookupBitfsPubkey(resolver, opts.Domain)
	if err != nil {
		fmt.Fprintf(&msg, "\nDNS verification: not yet configured (%v)", err)
	} else if dnsPubHex == rootPubHex {
		verified = true
		fmt.Fprintf(&msg, "\nDNS verification: VERIFIED (bidirectional match)")
	} else {
		fmt.Fprintf(&msg, "\nDNS verification: MISMATCH (DNS pubkey %s != vault pubkey %s)", dnsPubHex, rootPubHex)
	}

	// Store binding in local state.
	e.State.SetPublishBinding(&PublishBinding{
		Domain:     opts.Domain,
		VaultIndex: opts.VaultIndex,
		PubKeyHex:  rootPubHex,
		Verified:   verified,
	})

	return &Result{
		Message: msg.String(),
		NodePub: rootPubHex,
	}, nil
}

// listPublishBindings lists all stored publish bindings, re-verifying each.
func (e *Engine) listPublishBindings() (*Result, error) {
	bindings := e.State.ListPublishBindings()

	if len(bindings) == 0 {
		return &Result{
			Message: "No publish bindings configured.\n\nUsage: bitfs publish <domain> [--vault NAME]",
		}, nil
	}

	resolver := e.dnsResolver()
	var msg strings.Builder
	fmt.Fprintf(&msg, "Publish bindings:\n\n")

	for _, b := range bindings {
		// Re-verify against DNS.
		status := "unverified"
		verified := false
		dnsPubHex, err := lookupBitfsPubkey(resolver, b.Domain)
		if err == nil && dnsPubHex == b.PubKeyHex {
			status = "VERIFIED"
			verified = true
		} else if err == nil {
			status = "MISMATCH"
		}

		// Update binding in state with new verification status.
		e.State.SetPublishBinding(&PublishBinding{
			Domain:     b.Domain,
			VaultIndex: b.VaultIndex,
			PubKeyHex:  b.PubKeyHex,
			Verified:   verified,
		})

		fmt.Fprintf(&msg, "  %s -> vault[%d] pubkey=%s...%s [%s]\n",
			b.Domain, b.VaultIndex,
			b.PubKeyHex[:8], b.PubKeyHex[len(b.PubKeyHex)-8:],
			status)
	}

	return &Result{
		Message: msg.String(),
	}, nil
}

// dnsResolver returns the engine's DNS resolver, falling back to the default.
func (e *Engine) dnsResolver() DNSResolver {
	if e.DNS != nil {
		return e.DNS
	}
	return DefaultDNSResolver()
}

// lookupBitfsPubkey queries DNS for _bitfs.<domain> TXT records and extracts
// the pubkey from a record matching the format "bitfs=<hex_pubkey>".
func lookupBitfsPubkey(resolver DNSResolver, domain string) (string, error) {
	name := "_bitfs." + domain
	records, err := resolver.LookupTXT(name)
	if err != nil {
		return "", fmt.Errorf("DNS lookup %s: %w", name, err)
	}

	for _, record := range records {
		record = strings.TrimSpace(record)
		if strings.HasPrefix(record, "bitfs=") {
			pubHex := strings.TrimPrefix(record, "bitfs=")
			pubHex = strings.TrimSpace(pubHex)
			if len(pubHex) == 66 { // compressed pubkey = 33 bytes = 66 hex chars
				pubBytes, hexErr := hex.DecodeString(pubHex)
				if hexErr != nil {
					continue
				}
				// Validate that the bytes represent a valid secp256k1 point.
				if _, ecErr := ec.PublicKeyFromBytes(pubBytes); ecErr != nil {
					continue
				}
				return pubHex, nil
			}
		}
	}

	return "", fmt.Errorf("no valid bitfs= TXT record found at %s", name)
}

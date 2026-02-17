# Module Specification: internal/paymail

## PURPOSE

Paymail identity resolution for BitFS. Parses `bitfs://alias@domain/path` URIs, discovers Paymail capabilities via `.well-known/bsvalias`, and resolves aliases to Metanet root public keys. Implements the three addressing modes: Paymail (`@`), DNSLink (domain), and bare public key (hex).

Design references: ConceptDesign #13, #69, #70, #71; SystemDesign section 6; DetailedDesign section 16-B.

## PUBLIC API

### Types

```go
// AddressType represents the three BitFS addressing modes.
type AddressType int

const (
    AddressPaymail  AddressType = iota // bitfs://alias@domain/path
    AddressDNSLink                      // bitfs://domain/path
    AddressPubKey                       // bitfs://02abcdef.../path
)

// ParsedURI holds a parsed bitfs:// URI.
type ParsedURI struct {
    Type      AddressType
    Alias     string   // Paymail alias (empty for non-Paymail)
    Domain    string   // Domain name
    PubKey    []byte   // Raw public key bytes (for AddressPubKey)
    Path      string   // Path component after authority
    RawURI    string   // Original URI string
}

// PaymailCapabilities holds discovered Paymail server capabilities.
type PaymailCapabilities struct {
    PKI           string // URL template for public key infrastructure
    PublicProfile string // URL template for profile info
    VerifyPubKey  string // URL template for key verification
}

// PKIResponse holds the response from a Paymail PKI endpoint.
type PKIResponse struct {
    BSVAlias string `json:"bsvalias"`
    Handle   string `json:"handle"`
    PubKey   string `json:"pubkey"` // Hex-encoded compressed public key
}
```

### Functions

```go
// ParseURI parses a bitfs:// URI into its components.
// Detects address type based on:
//   - Contains '@' -> Paymail
//   - Starts with hex pubkey prefix (02/03) -> PubKey
//   - Otherwise -> DNSLink
func ParseURI(uri string) (*ParsedURI, error)

// DiscoverCapabilities fetches .well-known/bsvalias from a domain
// and returns the Paymail server capabilities.
func DiscoverCapabilities(domain string) (*PaymailCapabilities, error)

// ResolvePKI resolves a Paymail alias to its public key using the PKI capability.
// Returns the P_root public key for the alias's vault.
func ResolvePKI(alias, domain string) ([]byte, error)

// ResolveEndpoints resolves SRV records for a domain.
// For Paymail: _bsvalias._tcp.{domain}
// For DNSLink: _bitfs._tcp.{domain}
// Returns endpoints sorted by priority/weight.
func ResolveEndpoints(domain string, recordType string) ([]string, error)

// ResolveDNSLinkPubKey resolves _bitfs_pubkey.{domain} TXT record.
// Returns the P_node compressed public key bytes.
func ResolveDNSLinkPubKey(domain string) ([]byte, error)

// ResolveURI performs full URI resolution:
//   1. Parse URI
//   2. Resolve public key (via Paymail PKI or DNSLink TXT)
//   3. Resolve endpoints (via SRV records)
// Returns (pubkey, endpoints, error)
func ResolveURI(uri string) (pubKey []byte, endpoints []string, err error)
```

## DEPENDENCIES

- `net` -- DNS SRV/TXT lookups
- `net/http` -- HTTP client for .well-known discovery
- `encoding/json` -- JSON parsing
- `net/url` -- URI parsing

## DATA STRUCTURES

### URI Format
```
bitfs://[alias@]<authority>/<path>

authority:
  - alias@domain  -> Paymail resolution
  - domain        -> DNSLink resolution
  - 02abcdef...   -> Direct public key

Examples:
  bitfs://alice@example.com/docs/paper.pdf
  bitfs://example.com/docs/paper.pdf
  bitfs://02a1b2c3d4e5f6.../docs/paper.pdf
```

### DNS Records
```
;; DNSLink identity
_bitfs_pubkey.example.com   TXT  "02a1b2c3d4e5f6..."

;; Service endpoints
_bitfs._tcp.example.com     SRV  10 60 443 cdn1.example.com

;; Paymail endpoints
_bsvalias._tcp.example.com  SRV  10 60 443 cdn1.example.com
```

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrInvalidURI` | URI does not match bitfs:// scheme or is malformed |
| `ErrDNSLookupFailed` | DNS SRV/TXT lookup failed |
| `ErrPaymailDiscovery` | .well-known/bsvalias fetch failed |
| `ErrPKIResolution` | Paymail PKI endpoint returned error |
| `ErrNoEndpoints` | No SRV records found for domain |
| `ErrInvalidPubKey` | Public key from DNS/Paymail is not valid compressed key |

## SECURITY CONSIDERATIONS

1. **DNS verification**: For DNSLink, both directions must match -- DNS TXT points to P_node AND Metanet node's domain field matches the domain. Prevents DNS hijacking.
2. **HTTPS for Paymail**: All Paymail capability discovery and PKI resolution must use HTTPS.
3. **Public key validation**: All public keys received from DNS or Paymail must be validated as valid secp256k1 compressed points before use.

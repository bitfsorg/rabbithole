# Module Specification: internal/method42

## PURPOSE

Method 42 ECDH encryption engine for BitFS. Provides deterministic per-file encryption using secp256k1 elliptic curve Diffie-Hellman key exchange combined with AES-256-GCM symmetric encryption. All data stored in BitFS is encrypted by default; this module is the core cryptographic primitive.

Key derivation formula: `aes_key = HKDF-SHA256(ECDH(D_node, P_node).x, key_hash, "bitfs-file-encryption")`
where `key_hash = SHA256(SHA256(plaintext))` serves dual purpose as KDF salt and content commitment.

Design references: ConceptDesign #11, #12, #53, #54, #66; SystemDesign section 5; DetailedDesign sections 2-B.D, 5-B.

## PUBLIC API

### Types

```go
// Access represents the three access control modes for encrypted content.
type Access int

const (
    AccessPrivate Access = 0 // Only owner can decrypt (ECDH with BIP32 D_node)
    AccessFree    Access = 1 // Anyone can decrypt (D_node = scalar 1, trivial ECDH)
    AccessPaid    Access = 2 // Buyer decrypts via HTLC-obtained capsule
)

// EncryptResult holds the output of an encryption operation.
type EncryptResult struct {
    Ciphertext []byte // nonce(12B) || AES-256-GCM(plaintext, aes_key) || tag(16B)
    KeyHash    []byte // SHA256(SHA256(plaintext)), 32 bytes
    AESKey     []byte // Derived AES-256 key, 32 bytes (caller may discard)
}

// DecryptResult holds the output of a decryption operation.
type DecryptResult struct {
    Plaintext []byte // Decrypted content
    KeyHash   []byte // Recomputed SHA256(SHA256(plaintext)) for verification
}
```

### Functions

```go
// ComputeKeyHash computes the double-SHA256 content commitment.
// Returns SHA256(SHA256(plaintext)), 32 bytes.
func ComputeKeyHash(plaintext []byte) []byte

// ECDH computes the shared secret point between a private key scalar
// and a public key point on secp256k1.
// Returns the x-coordinate of the shared point (32 bytes).
// For AccessFree mode, pass privateKey as the scalar 1 (big.Int).
func ECDH(privateKey *ec.PrivateKey, publicKey *ec.PublicKey) ([]byte, error)

// DeriveAESKey derives a 32-byte AES-256 key using HKDF-SHA256.
//   ikm  = ECDH shared secret x-coordinate (32 bytes)
//   salt = key_hash (32 bytes)
//   info = "bitfs-file-encryption"
func DeriveAESKey(sharedSecretX []byte, keyHash []byte) ([]byte, error)

// Encrypt encrypts plaintext using Method 42.
//   - Computes key_hash = SHA256(SHA256(plaintext))
//   - Performs ECDH(D_node, P_node) to get shared secret
//   - Derives AES key via HKDF-SHA256
//   - Encrypts with AES-256-GCM (random 12-byte nonce)
//
// For AccessFree: D_node is scalar 1 (anyone can reproduce).
// For AccessPrivate/AccessPaid: D_node is the BIP32-derived private key.
func Encrypt(plaintext []byte, privateKey *ec.PrivateKey, publicKey *ec.PublicKey, access Access) (*EncryptResult, error)

// Decrypt decrypts ciphertext using Method 42.
//   - Performs ECDH to recover shared secret
//   - Derives AES key using provided key_hash
//   - Decrypts with AES-256-GCM
//   - Verifies SHA256(SHA256(plaintext)) == key_hash
func Decrypt(ciphertext []byte, privateKey *ec.PrivateKey, publicKey *ec.PublicKey, keyHash []byte, access Access) (*DecryptResult, error)

// DecryptWithCapsule decrypts using a pre-computed ECDH shared secret (capsule).
// Used by buyers who obtained the capsule via HTLC atomic swap.
func DecryptWithCapsule(ciphertext []byte, capsule []byte, keyHash []byte) (*DecryptResult, error)

// ReEncrypt re-encrypts content from one access mode to another.
// Decrypts with fromAccess parameters, then encrypts with toAccess parameters.
// Returns new ciphertext and new key_hash.
func ReEncrypt(ciphertext []byte, privateKey *ec.PrivateKey, publicKey *ec.PublicKey, keyHash []byte, fromAccess, toAccess Access) (*EncryptResult, error)

// ComputeCapsule computes the ECDH capsule for a buyer.
// capsule = ECDH(D_node, P_buyer).x
// Used by seller during HTLC flow.
func ComputeCapsule(nodePrivateKey *ec.PrivateKey, buyerPublicKey *ec.PublicKey) ([]byte, error)

// ComputeCapsuleHash computes SHA256(capsule) for HTLC hash lock.
func ComputeCapsuleHash(capsule []byte) []byte

// FreePrivateKey returns a private key with scalar value 1.
// Used for AccessFree mode where ECDH(1, P_node) = P_node.
func FreePrivateKey() *ec.PrivateKey
```

### Internal (unexported) Functions

```go
// aesGCMEncrypt encrypts plaintext with AES-256-GCM.
// Returns nonce(12B) || ciphertext || tag(16B).
func aesGCMEncrypt(plaintext, key []byte) ([]byte, error)

// aesGCMDecrypt decrypts AES-256-GCM ciphertext.
// Input format: nonce(12B) || ciphertext || tag(16B).
func aesGCMDecrypt(ciphertext, key []byte) ([]byte, error)
```

## DEPENDENCIES

- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- secp256k1 elliptic curve operations
- `crypto/aes` -- AES block cipher
- `crypto/cipher` -- GCM mode
- `crypto/rand` -- Cryptographic random number generation
- `crypto/sha256` -- SHA-256 hashing
- `golang.org/x/crypto/hkdf` -- HKDF key derivation

## DATA STRUCTURES

### AES-256-GCM Ciphertext Format
```
[nonce: 12 bytes] [ciphertext: variable] [GCM tag: 16 bytes]
```

### Key Hash
```
key_hash = SHA256(SHA256(plaintext))  // 32 bytes, double-hash
```

### HKDF Parameters
```
IKM  = ECDH(D_node, P_node).x   // 32 bytes (x-coordinate of shared point)
Salt = key_hash                   // 32 bytes
Info = "bitfs-file-encryption"    // constant string
Len  = 32                         // AES-256 key length
```

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrNilPrivateKey` | Private key is nil |
| `ErrNilPublicKey` | Public key is nil |
| `ErrInvalidCiphertext` | Ciphertext too short (< 28 bytes: 12 nonce + 16 tag) |
| `ErrDecryptionFailed` | AES-GCM authentication failed |
| `ErrKeyHashMismatch` | SHA256(SHA256(decrypted)) != expected key_hash |
| `ErrInvalidAccess` | Unknown access mode value |
| `ErrHKDFFailure` | HKDF key derivation failed |

## SECURITY CONSIDERATIONS

1. **Nonce uniqueness**: 12-byte random nonce per encryption. Different files use different AES keys (derived via ECDH), so cross-file nonce collision is not a risk. Same-file re-encryption count is far below 2^48.

2. **Double hash**: `key_hash = SHA256(SHA256(plaintext))` prevents direct exposure of content hash. Still vulnerable to dictionary attack on known content -- this is an accepted trade-off documented in design decision #54.

3. **Free mode trivial key**: `AccessFree` uses D_node=1, making `aes_key = KDF(P_node, key_hash)`. P_node is public, so anyone can compute the key. This is by design -- "encrypted at rest" even for free content.

4. **BIP32 algebraic preservation**: ECDH uses D_node directly (not a derived hash), preserving BIP32 non-hardened derivation transitivity. This enables directory-tree-level capsule derivation for bulk purchases.

5. **Key hash as content commitment**: After decryption, callers MUST verify `SHA256(SHA256(plaintext)) == key_hash` to confirm content integrity.

6. **No key storage**: AES keys are deterministically derived from (D_node, P_node, key_hash) and never stored. Only the HD seed needs backup.

# Module Specification: internal/wallet

## PURPOSE

HD wallet implementing BIP32/BIP39 key derivation for BitFS. Manages the deterministic key hierarchy that mirrors the filesystem structure: `m/44'/236'/{account}'/{chain}/{index}`. Provides seed generation, Argon2id encryption, vault management, and UTXO tracking.

Design references: ConceptDesign #5, #7, #30, #64, #65, #81; SystemDesign section 2; DetailedDesign section 2-B.

## PUBLIC API

### Constants

```go
const (
    PurposeBIP44        = 44
    CoinTypeBitFS       = 236    // BitFS registered BIP44 coin type
    FeeAccount          = 0      // Account index for fee key chain
    DefaultVaultAccount = 1      // First vault starts at account index 1
    ExternalChain       = 0      // Receive addresses
    InternalChain       = 1      // Change addresses
    MaxFileIndex        = 1<<31 - 1  // BIP32 non-hardened max (2^31 - 1)
    MaxPathDepth        = 64     // Maximum filesystem nesting depth

    Mnemonic12Words = 128  // 12-word mnemonic entropy bits
    Mnemonic24Words = 256  // 24-word mnemonic entropy bits

    // Argon2id parameters for seed encryption
    Argon2Time        = 3
    Argon2Memory      = 64 * 1024 // 64 MB
    Argon2Parallelism = 4
    Argon2KeyLen      = 32
    SaltLen           = 16
    NonceLen          = 12
    ChecksumLen       = 4
)
```

### Types

```go
// Wallet represents an HD wallet instance.
type Wallet struct {
    masterKey *bip32.ExtendedKey  // BIP32 master key (from seed)
    network   *NetworkConfig
}

// Vault represents an independent Metanet directory tree.
type Vault struct {
    Name         string `json:"name"`
    AccountIndex uint32 `json:"account_index"` // BIP44 account number (1-based for vaults)
    RootTxID     []byte `json:"root_txid"`     // Root node transaction ID (nil if not published)
}

// KeyPair holds a derived public/private key pair.
type KeyPair struct {
    PrivateKey *ec.PrivateKey
    PublicKey  *ec.PublicKey
    Path       string // Human-readable derivation path, e.g. "m/44'/236'/1'/0/0/3"
}

// WalletState holds persisted wallet metadata.
type WalletState struct {
    NextReceiveIndex uint32  `json:"next_receive_index"`
    NextChangeIndex  uint32  `json:"next_change_index"`
    Vaults           []Vault `json:"vaults"`
}

// UTXOEntry represents a tracked unspent output.
type UTXOEntry struct {
    TxID         []byte `json:"txid"`
    Vout         uint32 `json:"vout"`
    Amount       uint64 `json:"amount"`
    Spent        bool   `json:"spent"`
    Chain        uint32 `json:"chain"`        // 0=external, 1=internal
    AddressIndex uint32 `json:"address_index"`
}

// NetworkConfig defines network parameters.
type NetworkConfig struct {
    Name           string   `json:"name"`
    AddressVersion byte     `json:"address_version"`
    P2SHVersion    byte     `json:"p2sh_version"`
    DefaultPort    uint16   `json:"default_port"`
    RPCPort        uint16   `json:"rpc_port"`
    DNSSeeds       []string `json:"seeds"`
    GenesisHash    string   `json:"genesis_hash"`
}
```

### Functions -- Seed Management

```go
// GenerateMnemonic creates a new BIP39 mnemonic with the specified entropy bits.
// Use Mnemonic12Words (128) or Mnemonic24Words (256).
func GenerateMnemonic(entropyBits int) (string, error)

// ValidateMnemonic checks if a mnemonic string is valid BIP39.
func ValidateMnemonic(mnemonic string) bool

// SeedFromMnemonic derives a 64-byte BIP39 seed from mnemonic + optional passphrase.
// seed = PBKDF2(mnemonic, "mnemonic"+passphrase, 2048, 64, SHA512)
func SeedFromMnemonic(mnemonic, passphrase string) ([]byte, error)

// NewWallet creates a new Wallet from a BIP39 seed.
func NewWallet(seed []byte, network *NetworkConfig) (*Wallet, error)

// EncryptSeed encrypts the seed with Argon2id + AES-256-GCM.
// Returns: salt(16B) || nonce(12B) || AES-GCM(Argon2id(password,salt), nonce, seed||checksum)
func EncryptSeed(seed []byte, password string) ([]byte, error)

// DecryptSeed decrypts the seed from wallet.enc format.
// Parses salt(16B) || nonce(12B) || ciphertext, derives key with Argon2id.
// Verifies SHA256(seed)[:4] checksum after decryption.
func DecryptSeed(encrypted []byte, password string) ([]byte, error)
```

### Functions -- Key Derivation

```go
// DeriveNodeKey derives a key pair for a filesystem node.
//   vaultIndex: 0-based vault number
//   filePath: sequence of child indices from root, e.g. [3, 1, 7]
//   Path: m/44'/236'/(vaultIndex+1)'/0/0[/filePath...]
//
// Each index in filePath uses hardened or non-hardened derivation
// as specified by the hardened parameter array. If hardened is nil,
// all indices default to hardened (design decision #82).
func (w *Wallet) DeriveNodeKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*KeyPair, error)

// DeriveNodePubKey derives only the public key for a filesystem node.
// More efficient than DeriveNodeKey when private key is not needed.
func (w *Wallet) DeriveNodePubKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*ec.PublicKey, error)

// DeriveFeeKey derives a key pair from the fee key chain.
//   chain: ExternalChain (0) or InternalChain (1)
//   index: address index
//   Path: m/44'/236'/0'/chain/index
func (w *Wallet) DeriveFeeKey(chain, index uint32) (*KeyPair, error)

// DeriveVaultRootKey derives the root key pair for a vault.
//   Path: m/44'/236'/(vaultIndex+1)'/0/0
func (w *Wallet) DeriveVaultRootKey(vaultIndex uint32) (*KeyPair, error)

// DeriveKeyCacheKey derives the encryption key for key cache files.
// Used to encrypt cached AES keys in ~/.bitfs/cache/keys/.
func (w *Wallet) DeriveKeyCacheKey() (*KeyPair, error)
```

### Functions -- Vault Management

```go
// CreateVault creates a new vault with the given name.
// Allocates the next available account index.
func (w *Wallet) CreateVault(state *WalletState, name string) (*Vault, error)

// GetVault retrieves a vault by name.
func (w *Wallet) GetVault(state *WalletState, name string) (*Vault, error)

// ListVaults returns all vaults.
func (w *Wallet) ListVaults(state *WalletState) []Vault

// RenameVault renames an existing vault.
func (w *Wallet) RenameVault(state *WalletState, oldName, newName string) error

// DeleteVault marks a vault as deleted (soft delete).
func (w *Wallet) DeleteVault(state *WalletState, name string) error
```

### Functions -- Network

```go
// Predefined network configurations.
var (
    MainNet      NetworkConfig
    TestNet      NetworkConfig
    TeraTestNet  NetworkConfig
    RegTest      NetworkConfig
)

// GetNetwork returns a predefined network by name, or loads custom config.
func GetNetwork(name string) (*NetworkConfig, error)

// LoadCustomNetwork loads a NetworkConfig from a JSON file.
func LoadCustomNetwork(path string) (*NetworkConfig, error)
```

## DEPENDENCIES

- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- secp256k1 keys
- `github.com/bsv-blockchain/go-sdk/compat/bip32` -- HD key derivation
- `github.com/bsv-blockchain/go-sdk/compat/bip39` -- Mnemonic generation
- `golang.org/x/crypto/argon2` -- Argon2id password hashing
- `crypto/aes`, `crypto/cipher` -- AES-256-GCM for seed encryption
- `crypto/rand` -- Cryptographic random
- `crypto/sha256` -- Checksum

## DATA STRUCTURES

### HD Key Tree Layout
```
m/44'/236'/0'          Fee key chain (shared across all vaults)
  /0/M                   Receive addresses
  /1/M                   Change addresses

m/44'/236'/1'          Vault #0
  /0/0                   Root directory
  /0/0/K1                First child of root
  /0/0/K1/K2             Nested child

m/44'/236'/N'          Vault #(N-1)
  /0/0                   Root directory
```

### wallet.enc Binary Format
```
[salt: 16 bytes] [nonce: 12 bytes] [AES-GCM ciphertext of (seed || checksum)]
```
Where:
- salt: random, for Argon2id
- nonce: random, for AES-GCM
- checksum: SHA256(seed)[:4]

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrInvalidMnemonic` | Mnemonic fails BIP39 validation |
| `ErrInvalidEntropy` | Entropy bits not 128 or 256 |
| `ErrFileIndexOutOfRange` | Index exceeds MaxFileIndex (2^31-1) |
| `ErrPathTooDeep` | Path exceeds MaxPathDepth (64) |
| `ErrVaultNotFound` | Named vault does not exist |
| `ErrVaultExists` | Vault name already taken |
| `ErrDecryptionFailed` | Wrong password or corrupted wallet.enc |
| `ErrChecksumMismatch` | Seed checksum verification failed after decryption |
| `ErrInvalidNetwork` | Unknown network name and no custom config |

## SECURITY CONSIDERATIONS

1. **Argon2id**: Seed encryption uses Argon2id (m=64MB, t=3, p=4) to resist GPU/ASIC brute force. Single SHA256 can be attacked at billions/sec; Argon2id raises cost by orders of magnitude.

2. **Seed in memory**: After wallet creation, the mnemonic should be cleared from memory. Only the encrypted seed is persisted.

3. **Hardened default**: Child derivation defaults to hardened mode (design decision #82). This prevents child capsule from revealing parent capsule. Non-hardened is only used for explicit directory purchase scenarios.

4. **Key cache encryption**: Cached AES keys in `~/.bitfs/cache/keys/` are encrypted with a wallet-derived key, never stored as plaintext JSON.

5. **Deterministic recovery**: Given (mnemonic, passphrase), all keys can be recomputed. Transaction data requires separate backup.

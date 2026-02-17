# 模块规范：internal/wallet

## 目的

为 BitFS 实现 BIP32/BIP39 密钥派生的 HD 钱包。管理镜像文件系统结构的确定性密钥层次：`m/44'/236'/{account}'/{chain}/{index}`。提供种子生成、Argon2id 加密、保险库（Vault）管理和 UTXO 追踪。

设计参考：ConceptDesign #5, #7, #30, #64, #65, #81; SystemDesign 第 2 节; DetailedDesign 第 2-B 节。

## 公共 API

### 常量

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

### 类型

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

### 函数 -- 种子管理

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

### 函数 -- 密钥派生

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

### 函数 -- 保险库管理

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

### 函数 -- 网络

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

## 依赖

- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- secp256k1 密钥
- `github.com/bsv-blockchain/go-sdk/compat/bip32` -- HD 密钥派生
- `github.com/bsv-blockchain/go-sdk/compat/bip39` -- 助记词生成
- `golang.org/x/crypto/argon2` -- Argon2id 密码哈希
- `crypto/aes`, `crypto/cipher` -- AES-256-GCM 种子加密
- `crypto/rand` -- 密码学安全随机数
- `crypto/sha256` -- 校验和

## 数据结构

### HD 密钥树布局
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

### wallet.enc 二进制格式
```
[salt: 16 bytes] [nonce: 12 bytes] [AES-GCM ciphertext of (seed || checksum)]
```
其中：
- salt：随机值，用于 Argon2id
- nonce：随机值，用于 AES-GCM
- checksum：SHA256(seed)[:4]

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrInvalidMnemonic` | 助记词未通过 BIP39 验证 |
| `ErrInvalidEntropy` | 熵值位数不是 128 或 256 |
| `ErrFileIndexOutOfRange` | 索引超过 MaxFileIndex (2^31-1) |
| `ErrPathTooDeep` | 路径超过 MaxPathDepth (64) |
| `ErrVaultNotFound` | 指定名称的保险库不存在 |
| `ErrVaultExists` | 保险库名称已被占用 |
| `ErrDecryptionFailed` | 密码错误或 wallet.enc 已损坏 |
| `ErrChecksumMismatch` | 解密后种子校验和验证失败 |
| `ErrInvalidNetwork` | 未知网络名称且无自定义配置 |

## 安全考量

1. **Argon2id**：种子加密使用 Argon2id（m=64MB, t=3, p=4）以抵抗 GPU/ASIC 暴力破解。单次 SHA256 可以以数十亿次/秒的速度被攻击；Argon2id 将成本提高了数个数量级。

2. **内存中的种子**：钱包创建后，助记词应从内存中清除。仅持久化加密后的种子。

3. **默认硬化派生**：子密钥派生默认使用硬化模式（设计决策 #82）。这防止子胶囊（Capsule）泄露父胶囊。非硬化派生仅用于显式的目录购买场景。

4. **密钥缓存加密**：`~/.bitfs/cache/keys/` 中缓存的 AES 密钥使用钱包派生的密钥加密，永远不会以明文 JSON 存储。

5. **确定性恢复**：给定（助记词，口令），所有密钥均可重新计算。交易数据需要单独备份。

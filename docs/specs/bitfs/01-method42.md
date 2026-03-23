# 模块规范：libbitfs-go/method42

## 目的

BitFS 的 Method 42 ECDH 加密引擎。基于 secp256k1 椭圆曲线 Diffie-Hellman 密钥交换结合 AES-256-GCM 对称加密，提供确定性的逐文件加密。BitFS 中存储的所有数据默认加密；本模块是核心密码学原语。

密钥推导公式：`aes_key = HKDF-SHA256(ECDH(D_node, P_node).x, key_hash, "bitfs-file-encryption")`
其中 `key_hash = SHA256(SHA256(plaintext))` 同时用作 KDF 盐值和内容承诺（Content Commitment）。

设计参考：ConceptDesign #11, #12, #53, #54, #66; SystemDesign 第 5 节; DetailedDesign 第 2-B.D, 5-B 节。

## 公共 API

### 类型

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
}

// DecryptResult holds the output of a decryption operation.
type DecryptResult struct {
    Plaintext []byte // Decrypted content
    KeyHash   []byte // Recomputed SHA256(SHA256(plaintext)) for verification
}

// RabinKeyPair holds the private and public keys for Rabin signatures.
type RabinKeyPair struct {
    P *big.Int // Private Blum prime p ≡ 3 (mod 4)
    Q *big.Int // Private Blum prime q ≡ 3 (mod 4)
    N *big.Int // Public modulus n = p * q
}
```

### 常量

```go
const (
    HKDFInfo         = "bitfs-file-encryption"   // HKDF info for AES key derivation
    HKDFBuyerMaskInfo = "bitfs-buyer-mask"        // HKDF info for buyer mask derivation
    HKDFMetadataInfo = "bitfs-metadata-encryption" // HKDF info for PRIVATE metadata key
    AESKeyLen        = 32                         // AES-256 key length in bytes
    MetadataSaltLen  = 16                         // Random salt length for metadata key derivation
    NonceLen         = 12                         // AES-GCM nonce length in bytes
    GCMTagLen        = 16                         // GCM authentication tag length in bytes
    MinCiphertextLen = NonceLen + GCMTagLen        // 28 — minimum valid ciphertext length
    MinEncPayloadLen = MetadataSaltLen + NonceLen + GCMTagLen // 44 — minimum valid EncPayload length
)
```

### 函数

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

// DecryptWithCapsule decrypts using an XOR-masked capsule obtained via HTLC
// (legacy deterministic version without nonce).
// The buyer recovers the AES key as:
//   buyer_mask = HKDF(ECDH(D_buyer, P_node).x, key_hash, "bitfs-buyer-mask")
//   aes_key    = capsule XOR buyer_mask
// For capsules generated with ComputeCapsuleWithNonce, use DecryptWithCapsuleNonce.
func DecryptWithCapsule(ciphertext []byte, capsule []byte, keyHash []byte, buyerPrivateKey *ec.PrivateKey, nodePublicKey *ec.PublicKey) (*DecryptResult, error)

// DecryptWithCapsuleNonce decrypts using an XOR-masked capsule obtained via HTLC,
// with an optional per-invoice nonce for capsule unlinkability.
// The nonce must match the one used by the seller in ComputeCapsuleWithNonce.
// When nonce is nil, equivalent to DecryptWithCapsule.
func DecryptWithCapsuleNonce(ciphertext []byte, capsule []byte, keyHash []byte, buyerPrivateKey *ec.PrivateKey, nodePublicKey *ec.PublicKey, nonce []byte) (*DecryptResult, error)

// ComputeCapsuleWithNonce computes the XOR-masked capsule for a buyer with an
// optional per-invoice nonce for capsule unlinkability.
//   capsule = aes_key XOR buyer_mask
// When nonce is non-nil, it is included in the buyer mask derivation salt
// (keyHash || nonce), making each capsule unique per purchase even for the
// same (buyer, file) pair. When nonce is nil, equivalent to ComputeCapsule.
func ComputeCapsuleWithNonce(nodePrivateKey *ec.PrivateKey, nodePublicKey *ec.PublicKey, buyerPublicKey *ec.PublicKey, keyHash []byte, nonce []byte) ([]byte, error)

// DeriveBuyerMask derives a 32-byte buyer mask using HKDF-SHA256.
// Used in the paid content flow: capsule = aes_key XOR buyer_mask.
// Legacy deterministic version; for per-purchase unlinkability use
// DeriveBuyerMaskWithNonce.
func DeriveBuyerMask(sharedSecretX, keyHash []byte) ([]byte, error)

// DeriveBuyerMaskWithNonce derives a 32-byte buyer mask using HKDF-SHA256,
// with an optional per-invoice nonce for capsule unlinkability.
// When nonce is non-nil, HKDF salt = keyHash || nonce.
// When nonce is nil, equivalent to DeriveBuyerMask.
func DeriveBuyerMaskWithNonce(sharedSecretX, keyHash, nonce []byte) ([]byte, error)

// DeriveMetadataKey derives a 32-byte AES-256 key for PRIVATE mode metadata
// encryption, using a random 16-byte salt (P0 §3.2 fix).
// Returns (key, salt, error). The salt MUST be stored as a prefix of EncPayload.
func DeriveMetadataKey(sharedSecretX []byte) (key []byte, salt []byte, err error)

// DeriveMetadataKeyWithSalt derives a 32-byte AES-256 key for PRIVATE mode
// metadata encryption using a provided salt. Used during decryption when the
// salt is read from the EncPayload prefix.
func DeriveMetadataKeyWithSalt(sharedSecretX, salt []byte) ([]byte, error)

// EncryptMetadata encrypts a TLV metadata payload for PRIVATE mode.
// Uses a random 16-byte salt for HKDF key derivation.
// Output format: salt(16B) || nonce(12B) || AES-GCM(tlvPayload) || tag(16B).
func EncryptMetadata(tlvPayload []byte, privateKey *ec.PrivateKey, publicKey *ec.PublicKey) ([]byte, error)

// DecryptMetadata decrypts a PRIVATE mode EncPayload back to TLV bytes.
// Input format: salt(16B) || nonce(12B) || AES-GCM(tlvPayload) || tag(16B).
func DecryptMetadata(encPayload []byte, privateKey *ec.PrivateKey, publicKey *ec.PublicKey) ([]byte, error)

// ReEncrypt re-encrypts content from one access mode to another.
// Decrypts with fromAccess parameters, then encrypts with toAccess parameters.
// Returns new ciphertext and new key_hash.
func ReEncrypt(ciphertext []byte, privateKey *ec.PrivateKey, publicKey *ec.PublicKey, keyHash []byte, fromAccess, toAccess Access) (*EncryptResult, error)

// ComputeCapsule computes the XOR-masked capsule for a buyer (legacy deterministic version).
//   capsule = aes_key XOR buyer_mask
// where:
//   aes_key    = HKDF(ECDH(D_node, P_node).x, key_hash, "bitfs-file-encryption")
//   buyer_mask = HKDF(ECDH(D_node, P_buyer).x, key_hash, "bitfs-buyer-mask")
// The buyer recovers aes_key by computing buyer_mask from ECDH(D_buyer, P_node)
// and XORing with the capsule. Deterministic: same (D_node, P_buyer, key_hash)
// always produces the same capsule. For per-purchase unlinkability, use
// ComputeCapsuleWithNonce instead.
func ComputeCapsule(nodePrivateKey *ec.PrivateKey, nodePublicKey *ec.PublicKey, buyerPublicKey *ec.PublicKey, keyHash []byte) ([]byte, error)

// ComputeCapsuleHash computes SHA256(fileTxID ‖ capsule) for the HTLC hash lock.
// Binding the capsule hash to the file's transaction ID prevents a malicious
// seller from reusing a valid capsule across different files.
func ComputeCapsuleHash(fileTxID, capsule []byte) ([]byte, error)

// FreePrivateKey returns a private key with scalar value 1.
// Used for AccessFree mode where ECDH(1, P_node) = P_node.
func FreePrivateKey() *ec.PrivateKey

// --- Rabin Signature Scheme ---
// Rabin signatures provide content authenticity using the computational
// difficulty of finding modular square roots without factoring n.

// GenerateRabinKey generates a new Rabin key pair.
// bitSize is the bit length of each prime (e.g., 1024 for 2048-bit modulus).
// Both primes are Blum primes: p ≡ 3 (mod 4), q ≡ 3 (mod 4).
func GenerateRabinKey(bitSize int) (*RabinKeyPair, error)

// RabinSign signs a message using the Rabin signature scheme.
// Finds padding U such that H(message || U) is a quadratic residue mod N.
// H = SHA256 mapped to [0, N).
// Returns (S, U) where S² ≡ H(message || U) (mod N).
// Uses CRT for efficient square root computation.
func RabinSign(key *RabinKeyPair, message []byte) (sig *big.Int, pad []byte, err error)

// RabinVerify verifies a Rabin signature.
// Checks: sig² ≡ H(message || pad) (mod n).
// Requires only the public modulus n.
func RabinVerify(n *big.Int, message []byte, sig *big.Int, pad []byte) bool

// SerializeRabinSignature encodes (sig, pad) to binary.
// Format: sigLen(4 BE) || sig(big-endian) || padLen(4 BE) || pad
func SerializeRabinSignature(sig *big.Int, pad []byte) []byte

// DeserializeRabinSignature decodes binary to (sig, pad).
func DeserializeRabinSignature(data []byte) (*big.Int, []byte, error)

// SerializeRabinPubKey encodes the public modulus n.
// Returns n.Bytes() (big-endian).
func SerializeRabinPubKey(n *big.Int) []byte

// DeserializeRabinPubKey decodes the public modulus.
func DeserializeRabinPubKey(data []byte) (*big.Int, error)
```

### 内部（未导出）函数

```go
// aesGCMEncrypt encrypts plaintext with AES-256-GCM.
// Returns nonce(12B) || ciphertext || tag(16B).
func aesGCMEncrypt(plaintext, key []byte) ([]byte, error)

// aesGCMDecrypt decrypts AES-256-GCM ciphertext.
// Input format: nonce(12B) || ciphertext || tag(16B).
func aesGCMDecrypt(ciphertext, key []byte) ([]byte, error)

// xorBytes XORs two byte slices of equal length.
func xorBytes(a, b []byte) []byte

// effectivePrivateKey returns the private key to use for ECDH based on access mode.
// For AccessFree, returns FreePrivateKey() (scalar 1).
// For AccessPrivate and AccessPaid, returns the provided nodePrivateKey.
func effectivePrivateKey(access Access, nodePrivateKey *ec.PrivateKey) (*ec.PrivateKey, error)
```

## 依赖

- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- secp256k1 椭圆曲线操作
- `crypto/aes` -- AES 分组密码
- `crypto/cipher` -- GCM 模式
- `crypto/rand` -- 密码学安全随机数生成
- `crypto/sha256` -- SHA-256 哈希
- `golang.org/x/crypto/hkdf` -- HKDF 密钥推导

## 数据结构

### AES-256-GCM 密文格式
```
[nonce: 12 bytes] [ciphertext: variable] [GCM tag: 16 bytes]
```

### 密钥哈希（Key Hash）
```
key_hash = SHA256(SHA256(plaintext))  // 32 bytes, double-hash
```

### HKDF 参数
```
IKM  = ECDH(D_node, P_node).x   // 32 bytes (x-coordinate of shared point)
Salt = key_hash                   // 32 bytes
Info = "bitfs-file-encryption"    // constant string
Len  = 32                         // AES-256 key length
```

### PRIVATE 模式 EncPayload 格式
```
[salt: 16 bytes] [nonce: 12 bytes] [AES-GCM(TLV payload): variable] [GCM tag: 16 bytes]
```

### Rabin 签名方案

Rabin 签名基于二次剩余的计算困难性。无需椭圆曲线——安全性归约到整数分解。

#### 密钥生成
- 生成两个 Blum 素数 p, q：p ≡ 3 (mod 4), q ≡ 3 (mod 4)
- 公钥: n = p × q
- 私钥: (p, q)

#### 签名
1. 遍历计数器 U = 0, 1, 2, ... (4 字节 big-endian)
2. 计算 h = SHA256(message || U) mod n
3. 检查 h 是否为模 p 和模 q 的二次剩余 (Legendre 符号)
4. 若是，通过 CRT 计算平方根: S = sqrt(h) mod n
5. 返回 (S, U)

#### 验证
- 验证: S² mod n == SHA256(message || pad) mod n
- 仅需公钥 n

#### 序列化格式

**签名**: `sigLen(4 BE) || sig(big-endian bytes) || padLen(4 BE) || pad`
**公钥**: `n.Bytes()` (大端序)

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrNilPrivateKey` | 私钥为 nil |
| `ErrNilPublicKey` | 公钥为 nil |
| `ErrInvalidCiphertext` | 密文过短（< 28 字节：12 nonce + 16 tag） |
| `ErrDecryptionFailed` | AES-GCM 认证失败 |
| `ErrKeyHashMismatch` | SHA256(SHA256(decrypted)) != 期望的 key_hash |
| `ErrInvalidAccess` | 未知的访问模式值 |
| `ErrHKDFFailure` | HKDF 密钥推导失败 |

## 安全考量

1. **Nonce 唯一性**：每次加密使用 12 字节随机 nonce。不同文件使用不同的 AES 密钥（通过 ECDH 推导），因此跨文件的 nonce 碰撞不构成风险。同一文件的重加密次数远低于 2^48。

2. **双重哈希**：`key_hash = SHA256(SHA256(plaintext))` 防止直接暴露内容哈希。仍然容易受到已知内容的字典攻击——这是设计决策 #54 中记录的已接受折衷。

3. **免费模式平凡密钥**：`AccessFree` 使用 D_node=1，使得 `aes_key = KDF(P_node, key_hash)`。P_node 是公开的，因此任何人都可以计算密钥。这是设计意图——即使是免费内容也"静态加密"。

4. **BIP32 代数保持性**：ECDH 直接使用 D_node（而非派生哈希），保持 BIP32 非硬化派生的传递性。这使得目录树级别的胶囊（Capsule）派生成为可能，支持批量购买。

5. **密钥哈希作为内容承诺**：解密后，调用者必须验证 `SHA256(SHA256(plaintext)) == key_hash` 以确认内容完整性。

6. **无密钥存储**：AES 密钥从 (D_node, P_node, key_hash) 确定性推导，永远不会存储。只需备份 HD 种子。

7. **Rabin 安全性**：2048-bit 模数 (1024-bit 素数) 提供 ~112-bit 安全强度。签名伪造等价于分解 n。
8. **填充重试**：签名需要找到使哈希为二次剩余的填充。期望值约 4 次尝试（每次 ~1/4 概率成功）。计数器为 uint32，理论上无上限。

# 模块规范：libbitfs-go/method42

## 目的

BitFS 的 Method 42 ECDH 加密引擎。基于 secp256k1 椭圆曲线 Diffie-Hellman 密钥交换结合 AES-256-GCM 对称加密，提供确定性的逐文件加密。BitFS 中存储的所有数据默认加密；本模块是核心密码学原语。

密钥推导公式：`aes_key = HKDF-SHA256(ECDH(D_node, P_node).x, key_hash, "bitfs-file-encryption")`
其中 `key_hash = SHA256(SHA256(plaintext))` 同时用作 KDF 盐值和内容承诺（Content Commitment）。

设计参考：ConceptDesign #11, #12, #53, #54, #66; SystemDesign 第 5 节; DetailedDesign 第 2-B.D, 5-B 节。

## 公共 API

### 类型

```go
// AccessLevel represents the three access control modes for encrypted content.
type AccessLevel int32

const (
    AccessPrivate AccessLevel = 0 // Only owner can decrypt (ECDH with BIP32 D_node)
    AccessFree    AccessLevel = 1 // Anyone can decrypt (D_node = scalar 1, trivial ECDH)
    AccessPaid    AccessLevel = 2 // Buyer decrypts via HTLC-obtained capsule
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
func Encrypt(plaintext []byte, privateKey *ec.PrivateKey, publicKey *ec.PublicKey, access AccessLevel) (*EncryptResult, error)

// Decrypt decrypts ciphertext using Method 42.
//   - Performs ECDH to recover shared secret
//   - Derives AES key using provided key_hash
//   - Decrypts with AES-256-GCM
//   - Verifies SHA256(SHA256(plaintext)) == key_hash
func Decrypt(ciphertext []byte, privateKey *ec.PrivateKey, publicKey *ec.PublicKey, keyHash []byte, access AccessLevel) (*DecryptResult, error)

// DecryptWithCapsule decrypts using a pre-computed ECDH shared secret (capsule).
// Used by buyers who obtained the capsule via HTLC atomic swap.
func DecryptWithCapsule(ciphertext []byte, capsule []byte, keyHash []byte) (*DecryptResult, error)

// ReEncrypt re-encrypts content from one access mode to another.
// Decrypts with fromAccess parameters, then encrypts with toAccess parameters.
// Returns new ciphertext and new key_hash.
func ReEncrypt(ciphertext []byte, privateKey *ec.PrivateKey, publicKey *ec.PublicKey, keyHash []byte, fromAccess, toAccess AccessLevel) (*EncryptResult, error)

// ComputeCapsule computes the ECDH capsule for a buyer.
// capsule = ECDH(D_node, P_buyer).x
// Used by seller during HTLC flow.
func ComputeCapsule(nodePrivateKey *ec.PrivateKey, nodePublicKey *ec.PublicKey, buyerPublicKey *ec.PublicKey, keyHash []byte) ([]byte, error)

// ComputeCapsuleHash computes SHA256(capsule) for HTLC hash lock.
func ComputeCapsuleHash(capsule []byte) []byte

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

### Rabin 签名方案

Rabin 签名基于二次剩余的计算困难性。无需椭圆曲线——安全性归约到整数分解。

#### 密钥生成
- 生成两个 Blum 素数 p, q：p ≡ 3 (mod 4), q ≡ 3 (mod 4)
- 公钥: n = p × q
- 私钥: (p, q)

#### 签名
1. 尝试随机填充 U (最多 256 次)
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
| `ErrRabinKeyGeneration` | 素数生成失败 |
| `ErrRabinNoQuadraticResidue` | 256 次填充尝试均未找到二次剩余 |
| `ErrInvalidRabinSignature` | 签名反序列化失败 |

## 安全考量

1. **Nonce 唯一性**：每次加密使用 12 字节随机 nonce。不同文件使用不同的 AES 密钥（通过 ECDH 推导），因此跨文件的 nonce 碰撞不构成风险。同一文件的重加密次数远低于 2^48。

2. **双重哈希**：`key_hash = SHA256(SHA256(plaintext))` 防止直接暴露内容哈希。仍然容易受到已知内容的字典攻击——这是设计决策 #54 中记录的已接受折衷。

3. **免费模式平凡密钥**：`AccessFree` 使用 D_node=1，使得 `aes_key = KDF(P_node, key_hash)`。P_node 是公开的，因此任何人都可以计算密钥。这是设计意图——即使是免费内容也"静态加密"。

4. **BIP32 代数保持性**：ECDH 直接使用 D_node（而非派生哈希），保持 BIP32 非硬化派生的传递性。这使得目录树级别的胶囊（Capsule）派生成为可能，支持批量购买。

5. **密钥哈希作为内容承诺**：解密后，调用者必须验证 `SHA256(SHA256(plaintext)) == key_hash` 以确认内容完整性。

6. **无密钥存储**：AES 密钥从 (D_node, P_node, key_hash) 确定性推导，永远不会存储。只需备份 HD 种子。

7. **Rabin 安全性**：2048-bit 模数 (1024-bit 素数) 提供 ~112-bit 安全强度。签名伪造等价于分解 n。
8. **填充重试**：签名需要找到使哈希为二次剩余的填充。期望值约 4 次尝试，最坏 256 次。

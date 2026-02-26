# 模块规范：libbitfs/method42

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
func ComputeCapsule(nodePrivateKey *ec.PrivateKey, nodePublicKey *ec.PublicKey, buyerPublicKey *ec.PublicKey, keyHash []byte) ([]byte, error)

// ComputeCapsuleHash computes SHA256(capsule) for HTLC hash lock.
func ComputeCapsuleHash(capsule []byte) []byte

// FreePrivateKey returns a private key with scalar value 1.
// Used for AccessFree mode where ECDH(1, P_node) = P_node.
func FreePrivateKey() *ec.PrivateKey
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

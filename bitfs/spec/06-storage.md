# 模块规范：internal/storage

## 目的

BitFS 的内容存储抽象层。提供扁平键值存储，其中 `key_hash`（SHA256(SHA256(plaintext))）映射到加密密文。支持链下存储（默认，在 `~/.bitfs/store/` 中）和链上引用追踪。

设计参考：ConceptDesign #14, #25, #60; SystemDesign 第 4 节（内容存储）; DetailedDesign 第 8-B 节。

## 公共 API

### 接口

```go
// Store provides content-addressed storage for encrypted file data.
type Store interface {
    // Put stores encrypted content indexed by key_hash.
    // key_hash = SHA256(SHA256(plaintext)), 32 bytes.
    Put(keyHash []byte, ciphertext []byte) error

    // Get retrieves encrypted content by key_hash.
    Get(keyHash []byte) ([]byte, error)

    // Has checks if content exists for the given key_hash.
    Has(keyHash []byte) (bool, error)

    // Delete removes content by key_hash.
    Delete(keyHash []byte) error

    // Size returns the size in bytes of stored content for key_hash.
    Size(keyHash []byte) (int64, error)

    // List returns all stored key hashes (for backup/export).
    List() ([][]byte, error)
}
```

### 类型

```go
// FileStore implements Store using the local filesystem.
// Files stored at: {baseDir}/{hex(keyHash[:2])}/{hex(keyHash)}
type FileStore struct {
    baseDir string
}

// OnChainRef tracks content stored on-chain in data transactions.
type OnChainRef struct {
    KeyHash      []byte   // Content key hash
    ContentTxIDs [][]byte // Data transaction TxIDs (ordered chunks)
    TotalChunks  uint32   // Number of chunks (0 = single tx)
}
```

### 函数

```go
// NewFileStore creates a new file-based content store.
// baseDir is typically "~/.bitfs/store".
func NewFileStore(baseDir string) (*FileStore, error)

// KeyHashToPath converts a key_hash to its filesystem path.
// Uses first 2 bytes as subdirectory for sharding: {base}/{ab}/{abcdef...}
func KeyHashToPath(baseDir string, keyHash []byte) string
```

## 依赖

- `os` -- 文件 I/O
- `encoding/hex` -- 密钥哈希到文件名的转换
- `path/filepath` -- 路径构建

## 数据结构

### 文件布局
```
~/.bitfs/store/
  ab/
    abcdef0123456789...  (hex-encoded key_hash, content = ciphertext)
  cd/
    cdef...
```
使用 key_hash 的第一个字节作为子目录前缀，避免单个目录中文件过多。

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrNotFound` | 给定 key_hash 无对应内容 |
| `ErrInvalidKeyHash` | 密钥哈希不是 32 字节 |
| `ErrStoreFull` | 磁盘空间耗尽 |
| `ErrIOFailure` | 文件读写错误 |

## 安全考量

1. **内容始终加密**：存储中仅保存密文。即使文件系统被入侵，内容仍受 Method 42 加密保护。
2. **密钥哈希作为索引**：文件名（key_hash）是明文的双重哈希。对于文件系统级别的观察者，它不会泄露任何内容信息（但存在对已知内容的字典攻击风险，已在文档中说明）。
3. **存储中无元数据**：存储是纯粹的内容寻址。所有元数据存在于 Metanet 交易中。

# 模块规范：libbitfs-go/storage

## 目的

BitFS 的内容存储抽象层。提供扁平键值存储，其中 `key_hash`（SHA256(SHA256(plaintext))）映射到加密密文。支持链下存储（默认，在 `~/.bitfs/store/` 中）和链上引用追踪。

设计参考：ConceptDesign #14, #25, #60; SystemDesign 第 4 节（内容存储）; DetailedDesign 第 8-B 节。

## 公共 API

### 类型（压缩）

```go
// CompressionScheme identifies the compression algorithm.
type CompressionScheme int32

const (
    CompressNone CompressionScheme = 0 // No compression
    CompressLZW  CompressionScheme = 1 // compress/lzw (LSB byte order)
    CompressGZIP CompressionScheme = 2 // compress/gzip
    CompressZSTD CompressionScheme = 3 // [PLANNED] Zstandard
)
```

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

// --- Content Processing ---

// Compress compresses data using the specified scheme.
// CompressNone returns data unchanged.
// CompressZSTD is defined but not yet implemented (returns ErrUnsupportedCompression).
func Compress(data []byte, scheme CompressionScheme) ([]byte, error)

// Decompress decompresses data using the specified scheme.
// Mirrors Compress: CompressNone returns data unchanged.
func Decompress(data []byte, scheme CompressionScheme) ([]byte, error)

// --- Content Chunking ---

const DefaultChunkSize = 1 << 20 // 1 MB

// SplitIntoChunks splits data into fixed-size chunks.
// Last chunk may be smaller. Returns nil if data is empty.
func SplitIntoChunks(data []byte, chunkSize int) [][]byte

// ComputeRecombinationHash computes SHA256(chunk₀ ‖ chunk₁ ‖ ...).
// This hash verifies that all chunks recombine to the original content.
func ComputeRecombinationHash(chunks [][]byte) []byte

// RecombineChunks concatenates chunks and verifies the recombination hash.
// Returns ErrRecombinationHashMismatch if hash doesn't match.
func RecombineChunks(chunks [][]byte, expectedHash []byte) ([]byte, error)
```

## 依赖

- `os` -- 文件 I/O
- `encoding/hex` -- 密钥哈希到文件名的转换
- `path/filepath` -- 路径构建
- `compress/lzw` -- LZW 压缩
- `compress/gzip` -- GZIP 压缩
- `crypto/sha256` -- 重组哈希计算

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

### 内容分片模型

大文件 (>1MB) 拆分为固定大小分片，每个分片独立存储和传输。
Node 的 `ContentTxIDs` 字段 (TLV tag 0x15, 可重复) 记录每个分片的链上 TxID。

```
原始文件 (3.5 MB)
  ├── chunk_0 (1 MB)  → ContentTxID[0]
  ├── chunk_1 (1 MB)  → ContentTxID[1]
  ├── chunk_2 (1 MB)  → ContentTxID[2]
  └── chunk_3 (0.5 MB) → ContentTxID[3]

Node fields:
  TotalChunks = 4
  ChunkIndex  = (per-chunk node only, 0-based)
  RecombinationHash = SHA256(chunk_0 || chunk_1 || chunk_2 || chunk_3)
  Compression = scheme applied BEFORE chunking
```

处理流程：plaintext -> Compress -> SplitIntoChunks -> Encrypt each -> Store
还原流程：Retrieve -> Decrypt each -> RecombineChunks (verify hash) -> Decompress

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrNotFound` | 给定 key_hash 无对应内容 |
| `ErrInvalidKeyHash` | 密钥哈希不是 32 字节 |
| `ErrStoreFull` | 磁盘空间耗尽 |
| `ErrIOFailure` | 文件读写错误 |
| `ErrUnsupportedCompression` | 不支持的压缩方案 (ZSTD) |
| `ErrDecompressionFailed` | 解压缩失败 (损坏数据) |
| `ErrRecombinationHashMismatch` | 分片重组后哈希不匹配 |

## 安全考量

1. **内容始终加密**：存储中仅保存密文。即使文件系统被入侵，内容仍受 Method 42 加密保护。
2. **密钥哈希作为索引**：文件名（key_hash）是明文的双重哈希。对于文件系统级别的观察者，它不会泄露任何内容信息（但存在对已知内容的字典攻击风险，已在文档中说明）。
3. **存储中无元数据**：存储是纯粹的内容寻址。所有元数据存在于 Metanet 交易中。

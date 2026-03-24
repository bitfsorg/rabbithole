# 模块规范：libbitfs-go/storage

## 目的

BitFS 的内容存储抽象层。提供扁平键值存储，其中 `key_hash`（SHA256(SHA256(plaintext))）映射到加密密文。支持链下存储（默认，在 `~/.bitfs/store/` 中）和链上引用追踪。

设计参考：ConceptDesign #14, #25, #60; SystemDesign 第 4 节（内容存储）; DetailedDesign 第 8-B 节。

## 公共 API

### 常量

```go
// KeyHashSize is the required length of a key hash (SHA256 output = 32 bytes).
const KeyHashSize = 32
```

压缩方案常量定义在 `metanet` 包中（`metanet.CompressNone` = 0, `CompressLZW` = 1, `CompressGZIP` = 2, `CompressZSTD` = 3），类型为 `int32`。`Compress`/`Decompress` 直接接受 `int32` 参数。

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
// Files stored at: {baseDir}/{hex(keyHash[:1])}/{hex(keyHash)}
// The first byte (2 hex chars) is used as a subdirectory for sharding.
type FileStore struct {
    baseDir string
    mu      sync.RWMutex
}
```

### 函数

```go
// NewFileStore creates a new file-based content store.
// baseDir is typically "~/.bitfs/store".
func NewFileStore(baseDir string) (*FileStore, error)

// KeyHashToPath converts a key_hash to its filesystem path.
// Uses first byte (2 hex chars) as subdirectory for sharding: {base}/{ab}/{abcdef...}
func KeyHashToPath(baseDir string, keyHash []byte) string

// --- Content Processing ---

// Compress compresses data using the specified scheme (metanet.Compress* int32 constants).
// CompressNone returns data unchanged.
// CompressZSTD is defined but not yet implemented (returns ErrUnsupportedCompression).
func Compress(data []byte, scheme int32) ([]byte, error)

// Decompress decompresses data using the specified scheme.
// Mirrors Compress: CompressNone returns data unchanged.
func Decompress(data []byte, scheme int32) ([]byte, error)

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

### ContentResolver

```go
// MaxContentResponseSize is the maximum allowed response body size for content
// fetches (1 GB). Prevents memory exhaustion from malicious endpoints.
const MaxContentResponseSize = 1 << 30

// ContentResolver fetches encrypted content by key_hash from multiple sources
// in priority order: local FileStore -> daemon HTTP endpoints.
// Returns ciphertext only; the caller is responsible for decryption.
type ContentResolver struct {
    Store     *FileStore   // local content-addressed storage
    Endpoints []string     // daemon/CDN base URLs (e.g. "http://localhost:8080")
    Client    *http.Client // HTTP client for remote fetches; nil uses default
}

// NewContentResolver creates a ContentResolver with the given local store.
// Endpoints and Client can be set after creation.
// Default HTTP client timeout: 30s.
func NewContentResolver(store *FileStore) *ContentResolver

// Fetch retrieves ciphertext for the given key_hash, trying sources in order:
//  1. Local FileStore
//  2. Daemon HTTP endpoints (GET {baseURL}/_bitfs/data/{hex(keyHash)})
// Returns the first successful result. Caches remote content locally on success.
// Returns ErrNotFound if all sources fail.
func (r *ContentResolver) Fetch(keyHash []byte) ([]byte, error)
```

## 依赖

- `os` -- 文件 I/O
- `sync` -- FileStore 读写锁
- `encoding/hex` -- 密钥哈希到文件名的转换
- `path/filepath` -- 路径构建
- `compress/lzw` -- LZW 压缩
- `compress/gzip` -- GZIP 压缩
- `crypto/sha256` -- 重组哈希计算
- `net/http` -- ContentResolver 远程获取
- `io` -- 响应体限制读取
- `time` -- HTTP 客户端超时
- `github.com/bitfsorg/libbitfs-go/metanet` -- 压缩方案常量

## 数据结构

### 文件布局
```
~/.bitfs/store/
  ab/
    abcdef0123456789...  (hex-encoded key_hash, content = ciphertext)
  cd/
    cdef...
```
使用 key_hash 的第一个字节（2 个十六进制字符）作为子目录前缀，避免单个目录中文件过多。共 256 个分片桶。

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
| `ErrEmptyContent` | 尝试存储空内容 |
| `ErrInvalidBaseDir` | 基础目录路径无效（空字符串） |
| `ErrStoreFull` | 磁盘空间耗尽（已定义，当前未使用） |
| `ErrIOFailure` | 文件读写错误 |
| `ErrUnsupportedCompression` | 不支持的压缩方案 (如 ZSTD) |
| `ErrRecombinationHashMismatch` | 分片重组后哈希不匹配 |

## 安全考量

1. **内容始终加密**：存储中仅保存密文。即使文件系统被入侵，内容仍受 Method 42 加密保护。
2. **密钥哈希作为索引**：文件名（key_hash）是明文的双重哈希。对于文件系统级别的观察者，它不会泄露任何内容信息（但存在对已知内容的字典攻击风险，已在文档中说明）。
3. **存储中无元数据**：存储是纯粹的内容寻址。所有元数据存在于 Metanet 交易中。

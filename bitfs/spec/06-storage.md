# Module Specification: internal/storage

## PURPOSE

Content storage abstraction for BitFS. Provides a flat key-value store where `key_hash` (SHA256(SHA256(plaintext))) maps to encrypted ciphertext. Supports off-chain storage (default, in `~/.bitfs/store/`) and on-chain reference tracking.

Design references: ConceptDesign #14, #25, #60; SystemDesign section 4 (content storage); DetailedDesign section 8-B.

## PUBLIC API

### Interfaces

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

### Types

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

### Functions

```go
// NewFileStore creates a new file-based content store.
// baseDir is typically "~/.bitfs/store".
func NewFileStore(baseDir string) (*FileStore, error)

// KeyHashToPath converts a key_hash to its filesystem path.
// Uses first 2 bytes as subdirectory for sharding: {base}/{ab}/{abcdef...}
func KeyHashToPath(baseDir string, keyHash []byte) string
```

## DEPENDENCIES

- `os` -- File I/O
- `encoding/hex` -- Key hash to filename conversion
- `path/filepath` -- Path construction

## DATA STRUCTURES

### File Layout
```
~/.bitfs/store/
  ab/
    abcdef0123456789...  (hex-encoded key_hash, content = ciphertext)
  cd/
    cdef...
```
First byte of key_hash used as subdirectory prefix to avoid too many files in one directory.

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrNotFound` | No content for given key_hash |
| `ErrInvalidKeyHash` | Key hash is not 32 bytes |
| `ErrStoreFull` | Disk space exhausted |
| `ErrIOFailure` | File read/write error |

## SECURITY CONSIDERATIONS

1. **Content is always encrypted**: The store only holds ciphertext. Even if the filesystem is compromised, content remains protected by Method 42 encryption.
2. **Key hash as index**: The filename (key_hash) is a double-hash of plaintext. It leaks no information about content to filesystem-level observers (subject to dictionary attack on known content, as documented).
3. **No metadata in store**: The store is pure content-addressed. All metadata lives in Metanet transactions.

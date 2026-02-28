package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// cacheEntry wraps a MetaResponse with a timestamp for TTL.
type cacheEntry struct {
	CachedAt time.Time     `json:"cached_at"`
	Response *MetaResponse `json:"response"`
}

// MetaCache provides file-based caching of MetaResponse objects.
// Cache files are stored in a hash-sharded directory structure using
// 2-char hex prefix subdirectories (same pattern as content-addressed storage).
type MetaCache struct {
	dir string
	ttl time.Duration
}

// NewMetaCache creates a MetaCache backed by the given directory.
func NewMetaCache(dir string, ttl time.Duration) *MetaCache {
	return &MetaCache{dir: dir, ttl: ttl}
}

// cacheKey returns a hex-encoded SHA256 of "pnode/path".
func cacheKey(pnode, path string) string {
	h := sha256.Sum256([]byte(pnode + "/" + path))
	return hex.EncodeToString(h[:])
}

// cachePath returns the file path for a cache key: dir/ab/abcdef...json
func (c *MetaCache) cachePath(key string) string {
	return filepath.Join(c.dir, key[:2], key+".json")
}

// Get retrieves a cached MetaResponse. Returns nil if not found or expired.
func (c *MetaCache) Get(pnode, path string) (*MetaResponse, error) {
	key := cacheKey(pnode, path)
	data, err := os.ReadFile(c.cachePath(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		_ = os.Remove(c.cachePath(key)) // remove corrupt entry
		return nil, nil                 //nolint:nilerr // corrupt cache file, treat as miss
	}

	if time.Since(entry.CachedAt) > c.ttl {
		return nil, nil // expired
	}

	return entry.Response, nil
}

// Put stores a MetaResponse in the cache.
func (c *MetaCache) Put(pnode, path string, resp *MetaResponse) {
	key := cacheKey(pnode, path)
	fp := c.cachePath(key)

	_ = os.MkdirAll(filepath.Dir(fp), 0700)

	entry := cacheEntry{CachedAt: time.Now(), Response: resp}
	data, err := json.Marshal(entry)
	if err != nil {
		return // best-effort
	}
	_ = os.WriteFile(fp, data, 0600)
}

// Invalidate removes a cached entry.
func (c *MetaCache) Invalidate(pnode, path string) {
	key := cacheKey(pnode, path)
	_ = os.Remove(c.cachePath(key))
}

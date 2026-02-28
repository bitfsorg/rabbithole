package client

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetaCache_PutGet(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	resp := &MetaResponse{
		PNode:  "02abc123",
		Type:   "file",
		Path:   "/hello.txt",
		Access: "free",
	}

	cache.Put("02abc123", "/hello.txt", resp)

	got, err := cache.Get("02abc123", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "02abc123", got.PNode)
	assert.Equal(t, "/hello.txt", got.Path)
}

func TestMetaCache_Miss(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	got, err := cache.Get("nonexistent", "/path")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestMetaCache_Expired(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 1*time.Millisecond)

	resp := &MetaResponse{PNode: "02abc", Type: "file", Path: "/x"}
	cache.Put("02abc", "/x", resp)

	time.Sleep(5 * time.Millisecond)

	got, err := cache.Get("02abc", "/x")
	assert.NoError(t, err)
	assert.Nil(t, got) // expired
}

func TestMetaCache_Invalidate(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	resp := &MetaResponse{PNode: "02abc", Type: "file", Path: "/x"}
	cache.Put("02abc", "/x", resp)

	cache.Invalidate("02abc", "/x")

	got, err := cache.Get("02abc", "/x")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestMetaCache_CorruptEntry(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	// Put a valid entry first to create the directory structure.
	resp := &MetaResponse{PNode: "02abc", Type: "file", Path: "/x"}
	cache.Put("02abc", "/x", resp)

	// Corrupt the cache file.
	key := cacheKey("02abc", "/x")
	fp := cache.cachePath(key)
	os.WriteFile(fp, []byte(`{corrupt`), 0600)

	// Get should treat corrupt as cache miss (return nil, nil).
	got, err := cache.Get("02abc", "/x")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestMetaCache_ReadError(t *testing.T) {
	// Use a directory that's not readable.
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	// Put a valid entry, then make the file unreadable.
	resp := &MetaResponse{PNode: "02abc", Type: "file", Path: "/y"}
	cache.Put("02abc", "/y", resp)

	key := cacheKey("02abc", "/y")
	fp := cache.cachePath(key)
	os.Chmod(fp, 0000)
	defer os.Chmod(fp, 0600) // restore for cleanup

	got, err := cache.Get("02abc", "/y")
	// Non-NotExist read error should be returned.
	assert.Error(t, err)
	assert.Nil(t, got)
}

func TestMetaCache_CreatesSubdirs(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)

	resp := &MetaResponse{PNode: "02abc", Type: "file", Path: "/x"}
	cache.Put("02abc", "/x", resp)

	// Verify subdirectory was created (2-char hex prefix).
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
	assert.Len(t, entries[0].Name(), 2) // hex prefix
}

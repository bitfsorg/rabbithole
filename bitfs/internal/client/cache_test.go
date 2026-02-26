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

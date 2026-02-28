package client

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCachedClient_PopulatesCache(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	inner := &mockClient{
		meta: &MetaResponse{PNode: "02abc", Type: "file", Path: "/hello.txt"},
	}
	cc := NewCachedClient(inner, cache)

	got, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "/hello.txt", got.Path)
	assert.Equal(t, 1, inner.calls) // called inner

	// Second call should hit cache.
	got2, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "/hello.txt", got2.Path)
	assert.Equal(t, 1, inner.calls) // NOT called again
}

func TestCachedClient_NoCache(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	inner := &mockClient{
		meta: &MetaResponse{PNode: "02abc", Type: "file", Path: "/hello.txt"},
	}
	cc := NewCachedClient(inner, cache)
	cc.NoCache = true

	_, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, 1, inner.calls)

	// With NoCache, should call inner again.
	_, err = cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, 2, inner.calls)
}

func TestCachedClient_Offline_Hit(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	cache.Put("02abc", "/hello.txt", &MetaResponse{PNode: "02abc", Type: "file", Path: "/hello.txt"})

	inner := &mockClient{}
	cc := NewCachedClient(inner, cache)
	cc.Offline = true

	got, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "/hello.txt", got.Path)
	assert.Equal(t, 0, inner.calls) // never called inner
}

func TestCachedClient_Offline_Miss(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	inner := &mockClient{}
	cc := NewCachedClient(inner, cache)
	cc.Offline = true

	_, err := cc.GetMeta("02abc", "/missing")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrOfflineCacheMiss))
}

func TestCachedClient_WithPrefix(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	inner := &mockClient{
		meta: &MetaResponse{PNode: "02abc", Type: "file", Path: "/hello.txt"},
	}
	cc := NewCachedClient(inner, cache)
	cc.Prefix = "http://localhost:8080"

	got, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "/hello.txt", got.Path)

	// Cache key includes prefix — second call should hit cache.
	got2, err := cc.GetMeta("02abc", "/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "/hello.txt", got2.Path)
	assert.Equal(t, 1, inner.calls)
}

func TestCachedClient_InnerError(t *testing.T) {
	dir := t.TempDir()
	cache := NewMetaCache(dir, 5*time.Minute)
	inner := &mockClient{
		err: errors.New("connection refused"),
	}
	cc := NewCachedClient(inner, cache)

	_, err := cc.GetMeta("02abc", "/hello.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// mockClient is a test double for MetaGetter.
type mockClient struct {
	meta  *MetaResponse
	err   error
	calls int
}

func (m *mockClient) GetMeta(pnode, path string) (*MetaResponse, error) {
	m.calls++
	return m.meta, m.err
}

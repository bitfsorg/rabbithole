package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper functions ---

// makeKeyHash creates a deterministic 32-byte key hash from a seed.
func makeKeyHash(seed byte) []byte {
	data := []byte{seed}
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return second[:]
}

// newTestStore creates a FileStore in a temporary directory.
func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	return store
}

// --- NewFileStore tests ---

func TestNewFileStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	assert.NotNil(t, store)
}

func TestNewFileStore_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "store")
	store, err := NewFileStore(dir)
	require.NoError(t, err)
	assert.NotNil(t, store)

	// Verify directory was created
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewFileStore_EmptyDir(t *testing.T) {
	_, err := NewFileStore("")
	assert.ErrorIs(t, err, ErrInvalidBaseDir)
}

// --- KeyHashToPath tests ---

func TestKeyHashToPath(t *testing.T) {
	keyHash := makeKeyHash(0x42)
	hexHash := hex.EncodeToString(keyHash)
	shard := hexHash[:2]

	path := KeyHashToPath("/base", keyHash)
	expected := filepath.Join("/base", shard, hexHash)
	assert.Equal(t, expected, path)
}

func TestKeyHashToPath_DifferentShards(t *testing.T) {
	// Different key hashes should generally go to different shard dirs
	k1 := makeKeyHash(0x01)
	k2 := makeKeyHash(0x02)

	p1 := KeyHashToPath("/base", k1)
	p2 := KeyHashToPath("/base", k2)
	assert.NotEqual(t, p1, p2)
}

// --- Put tests ---

func TestPut(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)
	data := []byte("encrypted content")

	err := store.Put(keyHash, data)
	assert.NoError(t, err)
}

func TestPut_InvalidKeyHash(t *testing.T) {
	store := newTestStore(t)

	tests := []struct {
		name    string
		keyHash []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"too short", make([]byte, 16)},
		{"too long", make([]byte, 64)},
		{"one byte", []byte{0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Put(tt.keyHash, []byte("data"))
			assert.ErrorIs(t, err, ErrInvalidKeyHash)
		})
	}
}

func TestPut_EmptyContent(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)

	err := store.Put(keyHash, []byte{})
	assert.ErrorIs(t, err, ErrEmptyContent)
}

func TestPut_NilContent(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)

	err := store.Put(keyHash, nil)
	assert.ErrorIs(t, err, ErrEmptyContent)
}

func TestPut_Overwrite(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)

	err := store.Put(keyHash, []byte("original"))
	require.NoError(t, err)

	err = store.Put(keyHash, []byte("overwritten"))
	require.NoError(t, err)

	data, err := store.Get(keyHash)
	require.NoError(t, err)
	assert.Equal(t, []byte("overwritten"), data)
}

func TestPut_LargeContent(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x42)
	data := bytes.Repeat([]byte{0xFF}, 1024*1024) // 1 MB

	err := store.Put(keyHash, data)
	require.NoError(t, err)

	got, err := store.Get(keyHash)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

// --- Get tests ---

func TestGet(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)
	expected := []byte("encrypted file content")

	require.NoError(t, store.Put(keyHash, expected))

	got, err := store.Get(keyHash)
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestGet_NotFound(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0xFF)

	_, err := store.Get(keyHash)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGet_InvalidKeyHash(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Get([]byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidKeyHash)
}

// --- Has tests ---

func TestHas_Exists(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)

	require.NoError(t, store.Put(keyHash, []byte("data")))

	exists, err := store.Has(keyHash)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestHas_NotExists(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0xFF)

	exists, err := store.Has(keyHash)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestHas_InvalidKeyHash(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Has([]byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidKeyHash)
}

// --- Delete tests ---

func TestDelete(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)

	require.NoError(t, store.Put(keyHash, []byte("data")))
	err := store.Delete(keyHash)
	require.NoError(t, err)

	exists, err := store.Has(keyHash)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestDelete_NotFound(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0xFF)

	err := store.Delete(keyHash)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDelete_InvalidKeyHash(t *testing.T) {
	store := newTestStore(t)
	err := store.Delete([]byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidKeyHash)
}

func TestDelete_ThenGet(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)

	require.NoError(t, store.Put(keyHash, []byte("data")))
	require.NoError(t, store.Delete(keyHash))

	_, err := store.Get(keyHash)
	assert.ErrorIs(t, err, ErrNotFound)
}

// --- Size tests ---

func TestSize(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)
	data := []byte("some encrypted data here")

	require.NoError(t, store.Put(keyHash, data))

	size, err := store.Size(keyHash)
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)), size)
}

func TestSize_NotFound(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0xFF)

	_, err := store.Size(keyHash)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSize_InvalidKeyHash(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Size([]byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidKeyHash)
}

// --- List tests ---

func TestList_Empty(t *testing.T) {
	store := newTestStore(t)
	keys, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestList_MultipleItems(t *testing.T) {
	store := newTestStore(t)

	k1 := makeKeyHash(0x01)
	k2 := makeKeyHash(0x02)
	k3 := makeKeyHash(0x03)

	require.NoError(t, store.Put(k1, []byte("data1")))
	require.NoError(t, store.Put(k2, []byte("data2")))
	require.NoError(t, store.Put(k3, []byte("data3")))

	keys, err := store.List()
	require.NoError(t, err)
	assert.Len(t, keys, 3)

	// Check all keys are present (order not guaranteed)
	found := make(map[string]bool)
	for _, k := range keys {
		found[hex.EncodeToString(k)] = true
	}
	assert.True(t, found[hex.EncodeToString(k1)])
	assert.True(t, found[hex.EncodeToString(k2)])
	assert.True(t, found[hex.EncodeToString(k3)])
}

func TestList_AfterDelete(t *testing.T) {
	store := newTestStore(t)

	k1 := makeKeyHash(0x01)
	k2 := makeKeyHash(0x02)

	require.NoError(t, store.Put(k1, []byte("data1")))
	require.NoError(t, store.Put(k2, []byte("data2")))
	require.NoError(t, store.Delete(k1))

	keys, err := store.List()
	require.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Equal(t, k2, keys[0])
}

// --- Concurrent access tests ---

func TestConcurrentPutGet(t *testing.T) {
	store := newTestStore(t)
	const goroutines = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			keyHash := makeKeyHash(byte(idx))
			data := bytes.Repeat([]byte{byte(idx)}, 100)

			err := store.Put(keyHash, data)
			assert.NoError(t, err)

			got, err := store.Get(keyHash)
			assert.NoError(t, err)
			assert.Equal(t, data, got)
		}(i)
	}

	wg.Wait()

	// Verify all items stored
	keys, err := store.List()
	require.NoError(t, err)
	assert.Len(t, keys, goroutines)
}

func TestConcurrentHas(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x42)
	require.NoError(t, store.Put(keyHash, []byte("data")))

	var wg sync.WaitGroup
	const goroutines = 20
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			exists, err := store.Has(keyHash)
			assert.NoError(t, err)
			assert.True(t, exists)
		}()
	}

	wg.Wait()
}

// --- Store interface compliance ---

func TestFileStoreImplementsStore(t *testing.T) {
	store := newTestStore(t)
	var _ Store = store // compile-time check
}

// --- Shard directory structure ---

func TestShardDirectoryCreated(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	require.NoError(t, err)

	keyHash := makeKeyHash(0x01)
	require.NoError(t, store.Put(keyHash, []byte("data")))

	// Verify shard directory exists
	hexHash := hex.EncodeToString(keyHash)
	shardDir := filepath.Join(dir, hexHash[:2])
	info, err := os.Stat(shardDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify file exists within shard
	filePath := filepath.Join(shardDir, hexHash)
	_, err = os.Stat(filePath)
	assert.NoError(t, err)
}

func TestMultipleShardDirectories(t *testing.T) {
	store := newTestStore(t)

	// Store items that will land in different shards
	for i := 0; i < 20; i++ {
		keyHash := makeKeyHash(byte(i))
		require.NoError(t, store.Put(keyHash, []byte("data")))
	}

	keys, err := store.List()
	require.NoError(t, err)
	assert.Len(t, keys, 20)
}

// --- Edge cases ---

func TestPutGetBinaryContent(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)

	// Binary content with all byte values 0-255
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}

	require.NoError(t, store.Put(keyHash, data))

	got, err := store.Get(keyHash)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestSizeAfterOverwrite(t *testing.T) {
	store := newTestStore(t)
	keyHash := makeKeyHash(0x01)

	require.NoError(t, store.Put(keyHash, []byte("short")))
	size1, err := store.Size(keyHash)
	require.NoError(t, err)
	assert.Equal(t, int64(5), size1)

	require.NoError(t, store.Put(keyHash, []byte("much longer content here")))
	size2, err := store.Size(keyHash)
	require.NoError(t, err)
	assert.Equal(t, int64(24), size2)
}

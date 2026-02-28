//go:build integration

package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/bitfsorg/libbitfs-go/metanet"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// --- Test 1: TestHardLinkBehavior ---

func TestHardLinkBehavior(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	store := newMockNodeStore()

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	// Derive a single key to be the hard link target (a file node)
	targetKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	targetNode := &metanet.Node{
		TxID:     bytes.Repeat([]byte{0x02}, 32),
		PNode:    targetKey.PublicKey.Compressed(),
		Type:     metanet.NodeTypeFile,
		MimeType: "text/plain",
		FileSize: 42,
	}
	store.AddNode(targetNode)

	// Add first entry pointing to target
	_, err = metanet.AddChild(dirNode, "link-a", metanet.NodeTypeFile, targetKey.PublicKey.Compressed(), true)
	require.NoError(t, err)

	// Add second entry (hard link) pointing to the same PubKey
	_, err = metanet.AddChild(dirNode, "link-b", metanet.NodeTypeFile, targetKey.PublicKey.Compressed(), true)
	require.NoError(t, err)

	// Verify both entries exist
	assert.Len(t, dirNode.Children, 2)

	entryA, foundA := metanet.FindChild(dirNode, "link-a")
	require.True(t, foundA)
	entryB, foundB := metanet.FindChild(dirNode, "link-b")
	require.True(t, foundB)

	// Both should point to the same PubKey
	assert.Equal(t, entryA.PubKey, entryB.PubKey)

	// Both should resolve to the same node from store
	store.AddNode(dirNode)
	nodeA, err := store.GetNodeByPubKey(entryA.PubKey)
	require.NoError(t, err)
	nodeB, err := store.GetNodeByPubKey(entryB.PubKey)
	require.NoError(t, err)
	assert.Equal(t, nodeA.PNode, nodeB.PNode)
	assert.Equal(t, nodeA.TxID, nodeB.TxID)
}

// --- Test 2: TestHardLinkToDirectoryFails ---

func TestHardLinkToDirectoryFails(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	// Add a directory child first
	childDirKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	_, err = metanet.AddChild(dirNode, "subdir", metanet.NodeTypeDir, childDirKey.PublicKey.Compressed(), true)
	require.NoError(t, err)

	// Attempt to add another directory entry pointing to the same PubKey (hard link to dir)
	_, err = metanet.AddChild(dirNode, "subdir-link", metanet.NodeTypeDir, childDirKey.PublicKey.Compressed(), true)
	assert.ErrorIs(t, err, metanet.ErrHardLinkToDirectory,
		"hard linking to a directory should fail with ErrHardLinkToDirectory")
}

// --- Test 3: TestRemoveThenReaddSequentialIndex ---

func TestRemoveThenReaddSequentialIndex(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	// Add 10 children
	for i := uint32(0); i < 10; i++ {
		childKey, err := w.DeriveNodeKey(0, []uint32{i + 1}, nil)
		require.NoError(t, err)
		_, err = metanet.AddChild(dirNode, fmt.Sprintf("child-%d", i), metanet.NodeTypeFile, childKey.PublicKey.Compressed(), true)
		require.NoError(t, err)
	}

	assert.Equal(t, uint32(10), dirNode.NextChildIndex)

	// Remove child-5
	err = metanet.RemoveChild(dirNode, "child-5")
	require.NoError(t, err)
	assert.Len(t, dirNode.Children, 9)
	assert.Equal(t, uint32(10), dirNode.NextChildIndex, "NextChildIndex should not decrease after remove")

	// Add a new child - it should get index 10, not 5
	newKey, err := w.DeriveNodeKey(0, []uint32{11}, nil)
	require.NoError(t, err)
	newEntry, err := metanet.AddChild(dirNode, "child-new", metanet.NodeTypeFile, newKey.PublicKey.Compressed(), true)
	require.NoError(t, err)
	assert.Equal(t, uint32(10), newEntry.Index, "new child should get index 10 (monotonic), not 5 (reused)")
	assert.Equal(t, uint32(11), dirNode.NextChildIndex)
}

// --- Test 4: TestLargeDirectoryOperations ---

func TestLargeDirectoryOperations(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	// Add 100 children
	for i := uint32(0); i < 100; i++ {
		childKey, err := w.DeriveNodeKey(0, []uint32{i + 1}, nil)
		require.NoError(t, err)
		_, err = metanet.AddChild(dirNode, fmt.Sprintf("file-%03d", i), metanet.NodeTypeFile, childKey.PublicKey.Compressed(), true)
		require.NoError(t, err)
	}

	assert.Len(t, dirNode.Children, 100)
	assert.Equal(t, uint32(100), dirNode.NextChildIndex)

	// List and find some random children
	entries, err := metanet.ListDirectory(dirNode)
	require.NoError(t, err)
	assert.Len(t, entries, 100)

	for _, idx := range []int{0, 42, 77, 99} {
		name := fmt.Sprintf("file-%03d", idx)
		_, found := metanet.FindChild(dirNode, name)
		assert.True(t, found, "should find %s", name)
	}

	// Remove 10 children (indices 10-19)
	for i := 10; i < 20; i++ {
		err = metanet.RemoveChild(dirNode, fmt.Sprintf("file-%03d", i))
		require.NoError(t, err)
	}

	assert.Len(t, dirNode.Children, 90)
	assert.Equal(t, uint32(100), dirNode.NextChildIndex, "NextChildIndex should still be 100 after removals")

	// Verify removed children are gone
	for i := 10; i < 20; i++ {
		_, found := metanet.FindChild(dirNode, fmt.Sprintf("file-%03d", i))
		assert.False(t, found, "file-%03d should be removed", i)
	}
}

// --- Test 5: TestMoveChildBetweenDirectories ---

func TestMoveChildBetweenDirectories(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	// Create dirA and dirB
	dirA := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x0A}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	dirBKey, err := w.DeriveNodeKey(0, []uint32{99}, nil)
	require.NoError(t, err)
	dirB := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x0B}, 32),
		PNode:          dirBKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	// Add a child to dirA
	childKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	_, err = metanet.AddChild(dirA, "moveable.txt", metanet.NodeTypeFile, childKey.PublicKey.Compressed(), true)
	require.NoError(t, err)
	assert.Len(t, dirA.Children, 1)

	// Find the child entry to get its PubKey
	entry, found := metanet.FindChild(dirA, "moveable.txt")
	require.True(t, found)
	movePubKey := make([]byte, len(entry.PubKey))
	copy(movePubKey, entry.PubKey)

	// Remove from dirA
	err = metanet.RemoveChild(dirA, "moveable.txt")
	require.NoError(t, err)
	assert.Len(t, dirA.Children, 0)

	// Add to dirB with same PubKey
	_, err = metanet.AddChild(dirB, "moveable.txt", metanet.NodeTypeFile, movePubKey, true)
	require.NoError(t, err)
	assert.Len(t, dirB.Children, 1)

	// Verify child is gone from dirA
	_, found = metanet.FindChild(dirA, "moveable.txt")
	assert.False(t, found)

	// Verify child is present in dirB
	_, found = metanet.FindChild(dirB, "moveable.txt")
	assert.True(t, found)
}

// --- Test 6: TestSymlinkToDirectory ---

func TestSymlinkToDirectory(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	store := newMockNodeStore()

	// Create a directory node as the target
	dirKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	dirNode := &metanet.Node{
		TxID:  bytes.Repeat([]byte{0x01}, 32),
		PNode: dirKey.PublicKey.Compressed(),
		Type:  metanet.NodeTypeDir,
	}
	store.AddNode(dirNode)

	// Create a link node pointing to the directory
	linkKey, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)
	linkNode := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x02}, 32),
		PNode:      linkKey.PublicKey.Compressed(),
		Type:       metanet.NodeTypeLink,
		LinkType:   metanet.LinkTypeSoft,
		LinkTarget: dirKey.PublicKey.Compressed(),
	}
	store.AddNode(linkNode)

	// FollowLink should resolve to the directory
	resolved, err := metanet.FollowLink(store, linkNode, 0)
	require.NoError(t, err)
	assert.Equal(t, dirNode.PNode, resolved.PNode)
	assert.Equal(t, metanet.NodeTypeDir, resolved.Type)
}

// --- Test 7: TestNodeMetadataRoundTrip ---

func TestNodeMetadataRoundTrip(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	parentKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	childKey, err := w.DeriveNodeKey(0, []uint32{1, 1}, nil)
	require.NoError(t, err)

	// Create a node with ALL fields set
	original := &metanet.Node{
		PNode:      nodeKey.PublicKey.Compressed(),
		ParentTxID: bytes.Repeat([]byte{0xAA}, 32),
		Version:    3,
		Type:       metanet.NodeTypeDir,
		Op:         metanet.OpUpdate,
		MimeType:   "application/json",
		FileSize:   123456,
		KeyHash:    bytes.Repeat([]byte{0xBB}, 32),
		Access:     metanet.AccessPaid,
		PricePerKB: 500,
		Timestamp:  1700000000,
		Parent:     parentKey.PublicKey.Compressed(),
		Index:      7,
		Children: []metanet.ChildEntry{
			{
				Index:    0,
				Name:     "subfile.txt",
				Type:     metanet.NodeTypeFile,
				PubKey:   childKey.PublicKey.Compressed(),
				Hardened: true,
			},
		},
		NextChildIndex: 1,
		Domain:         "example.bitfs.org",
		Keywords:       "test,integration,metadata",
		Description:    "A test node with all fields populated",
		Encrypted:      true,
		OnChain:        true,
		ContentTxIDs:   [][]byte{bytes.Repeat([]byte{0xCC}, 32)},
		Compression:    1,
		CltvHeight:     100000,
		RevenueShare:   50,
		NetworkName:    "mainnet",
	}

	// Serialize
	payload, err := metanet.SerializePayload(original)
	require.NoError(t, err)
	require.NotEmpty(t, payload)

	// Build OP_RETURN pushes
	pushes := [][]byte{
		{0x6d, 0x65, 0x74, 0x61},   // MetaFlag
		original.PNode,               // P_node
		original.ParentTxID,          // TxID_parent
		payload,                      // Payload
	}

	// Parse back
	parsed, err := metanet.ParseNode(pushes)
	require.NoError(t, err)

	// Verify all fields match
	assert.Equal(t, original.Version, parsed.Version)
	assert.Equal(t, original.Type, parsed.Type)
	assert.Equal(t, original.Op, parsed.Op)
	assert.Equal(t, original.MimeType, parsed.MimeType)
	assert.Equal(t, original.FileSize, parsed.FileSize)
	assert.Equal(t, original.KeyHash, parsed.KeyHash)
	assert.Equal(t, original.Access, parsed.Access)
	assert.Equal(t, original.PricePerKB, parsed.PricePerKB)
	assert.Equal(t, original.Timestamp, parsed.Timestamp)
	assert.Equal(t, original.Parent, parsed.Parent)
	assert.Equal(t, original.Index, parsed.Index)
	assert.Equal(t, original.NextChildIndex, parsed.NextChildIndex)
	assert.Equal(t, original.Domain, parsed.Domain)
	assert.Equal(t, original.Keywords, parsed.Keywords)
	assert.Equal(t, original.Description, parsed.Description)
	assert.Equal(t, original.Encrypted, parsed.Encrypted)
	assert.Equal(t, original.OnChain, parsed.OnChain)
	assert.Equal(t, original.Compression, parsed.Compression)
	assert.Equal(t, original.CltvHeight, parsed.CltvHeight)
	assert.Equal(t, original.RevenueShare, parsed.RevenueShare)
	assert.Equal(t, original.NetworkName, parsed.NetworkName)

	// Verify children
	require.Len(t, parsed.Children, 1)
	assert.Equal(t, original.Children[0].Name, parsed.Children[0].Name)
	assert.Equal(t, original.Children[0].Index, parsed.Children[0].Index)
	assert.Equal(t, original.Children[0].Type, parsed.Children[0].Type)
	assert.Equal(t, original.Children[0].PubKey, parsed.Children[0].PubKey)
	assert.Equal(t, original.Children[0].Hardened, parsed.Children[0].Hardened)

	// Verify ContentTxIDs
	require.Len(t, parsed.ContentTxIDs, 1)
	assert.Equal(t, original.ContentTxIDs[0], parsed.ContentTxIDs[0])
}

// --- Test 8: TestAccessLevelValues ---

func TestAccessLevelValues(t *testing.T) {
	assert.Equal(t, metanet.AccessLevel(0), metanet.AccessPrivate)
	assert.Equal(t, metanet.AccessLevel(1), metanet.AccessFree)
	assert.Equal(t, metanet.AccessLevel(2), metanet.AccessPaid)
}

// --- Test 9: TestDirectoryAddChildEmptyName ---

func TestDirectoryAddChildEmptyName(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	childKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	_, err = metanet.AddChild(dirNode, "", metanet.NodeTypeFile, childKey.PublicKey.Compressed(), true)
	assert.ErrorIs(t, err, metanet.ErrInvalidName, "empty name should return ErrInvalidName")
}

// --- Test 10: TestSplitPathSpecialCharacters ---

func TestSplitPathSpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []string
	}{
		{"spaces in name", "/my files/doc 1.txt", []string{"my files", "doc 1.txt"}},
		{"unicode chinese", "/文档/readme.md", []string{"文档", "readme.md"}},
		{"dashes and underscores", "/my-dir/my_file.txt", []string{"my-dir", "my_file.txt"}},
		{"mixed unicode", "/日本語/한국어/file.txt", []string{"日本語", "한국어", "file.txt"}},
		{"parentheses", "/archive (2024)/report.pdf", []string{"archive (2024)", "report.pdf"}},
		{"dots in name", "/my.dir/file.name.ext", []string{"my.dir", "file.name.ext"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := metanet.SplitPath(tc.path)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// --- Test 11: TestResolvePathNonexistentChild ---

func TestResolvePathNonexistentChild(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	store := newMockNodeStore()

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	rootNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}
	store.AddNode(rootNode)

	components, err := metanet.SplitPath("/nonexistent/file.txt")
	require.NoError(t, err)

	_, err = metanet.ResolvePath(store, rootNode, components)
	assert.ErrorIs(t, err, metanet.ErrChildNotFound,
		"resolving a nonexistent child should return ErrChildNotFound")
}

// --- Test 12: TestResolvePathIntoFile ---

func TestResolvePathIntoFile(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	store := newMockNodeStore()

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	fileKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	rootNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	// Add file.txt as a child of root
	_, err = metanet.AddChild(rootNode, "file.txt", metanet.NodeTypeFile, fileKey.PublicKey.Compressed(), true)
	require.NoError(t, err)

	fileNode := &metanet.Node{
		TxID:     bytes.Repeat([]byte{0x02}, 32),
		PNode:    fileKey.PublicKey.Compressed(),
		Type:     metanet.NodeTypeFile,
		MimeType: "text/plain",
	}
	store.AddNode(rootNode)
	store.AddNode(fileNode)

	// Try to resolve "/file.txt/deeper" - should fail because file.txt is not a directory
	components, err := metanet.SplitPath("/file.txt/deeper")
	require.NoError(t, err)

	_, err = metanet.ResolvePath(store, rootNode, components)
	assert.ErrorIs(t, err, metanet.ErrNotDirectory,
		"traversing into a file should return ErrNotDirectory")
}

// --- Test 13: TestMultipleVersionsSameNodeOrdering ---

func TestMultipleVersionsSameNodeOrdering(t *testing.T) {
	pNode := bytes.Repeat([]byte{0xAA}, 33)

	t.Run("highest block height wins", func(t *testing.T) {
		versions := []*metanet.Node{
			{TxID: bytes.Repeat([]byte{0x01}, 32), PNode: pNode, BlockHeight: 100, Timestamp: 5000},
			{TxID: bytes.Repeat([]byte{0x02}, 32), PNode: pNode, BlockHeight: 300, Timestamp: 1000},
			{TxID: bytes.Repeat([]byte{0x03}, 32), PNode: pNode, BlockHeight: 200, Timestamp: 9000},
			{TxID: bytes.Repeat([]byte{0x04}, 32), PNode: pNode, BlockHeight: 150, Timestamp: 8000},
			{TxID: bytes.Repeat([]byte{0x05}, 32), PNode: pNode, BlockHeight: 250, Timestamp: 3000},
		}

		latest := metanet.LatestVersion(versions)
		require.NotNil(t, latest)
		assert.Equal(t, uint32(300), latest.BlockHeight)
		assert.Equal(t, versions[1].TxID, latest.TxID)
	})

	t.Run("same block height, highest timestamp wins", func(t *testing.T) {
		versions := []*metanet.Node{
			{TxID: bytes.Repeat([]byte{0x01}, 32), PNode: pNode, BlockHeight: 200, Timestamp: 1000},
			{TxID: bytes.Repeat([]byte{0x02}, 32), PNode: pNode, BlockHeight: 200, Timestamp: 3000},
			{TxID: bytes.Repeat([]byte{0x03}, 32), PNode: pNode, BlockHeight: 200, Timestamp: 2000},
			{TxID: bytes.Repeat([]byte{0x04}, 32), PNode: pNode, BlockHeight: 200, Timestamp: 5000},
			{TxID: bytes.Repeat([]byte{0x05}, 32), PNode: pNode, BlockHeight: 200, Timestamp: 4000},
		}

		latest := metanet.LatestVersion(versions)
		require.NotNil(t, latest)
		assert.Equal(t, uint64(5000), latest.Timestamp)
		assert.Equal(t, versions[3].TxID, latest.TxID)
	})

	t.Run("same block height and timestamp, higher TxID wins", func(t *testing.T) {
		versions := []*metanet.Node{
			{TxID: bytes.Repeat([]byte{0x01}, 32), PNode: pNode, BlockHeight: 200, Timestamp: 1000},
			{TxID: bytes.Repeat([]byte{0xFF}, 32), PNode: pNode, BlockHeight: 200, Timestamp: 1000},
			{TxID: bytes.Repeat([]byte{0x80}, 32), PNode: pNode, BlockHeight: 200, Timestamp: 1000},
		}

		latest := metanet.LatestVersion(versions)
		require.NotNil(t, latest)
		assert.Equal(t, versions[1].TxID, latest.TxID, "0xFF TxID should win")
	})
}

// --- Test 14: TestStorageNonexistentKey ---

func TestStorageNonexistentKey(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "store"))
	require.NoError(t, err)

	// A key that was never stored
	fakeKey := bytes.Repeat([]byte{0xDE}, 32)
	_, err = fs.Get(fakeKey)
	assert.ErrorIs(t, err, storage.ErrNotFound)
}

// --- Test 15: TestStorageDeleteThenGet ---

func TestStorageDeleteThenGet(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "store"))
	require.NoError(t, err)

	key := bytes.Repeat([]byte{0xAB}, 32)
	content := []byte("data to be deleted")

	// Put
	err = fs.Put(key, content)
	require.NoError(t, err)

	// Verify it exists
	exists, err := fs.Has(key)
	require.NoError(t, err)
	assert.True(t, exists)

	// Delete
	err = fs.Delete(key)
	require.NoError(t, err)

	// Get should return ErrNotFound
	_, err = fs.Get(key)
	assert.ErrorIs(t, err, storage.ErrNotFound)

	// Has should return false
	exists, err = fs.Has(key)
	require.NoError(t, err)
	assert.False(t, exists)
}

// --- Test 16: TestStorageEmptyContent ---

func TestStorageEmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "store"))
	require.NoError(t, err)

	key := bytes.Repeat([]byte{0xCC}, 32)

	// Put with empty ciphertext should return ErrEmptyContent per the implementation
	err = fs.Put(key, []byte{})
	assert.ErrorIs(t, err, storage.ErrEmptyContent,
		"storing empty content should return ErrEmptyContent")
}

// --- Test 17: TestStorageLargeContent ---

func TestStorageLargeContent(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "store"))
	require.NoError(t, err)

	key := bytes.Repeat([]byte{0xDD}, 32)

	// 1MB of data
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	err = fs.Put(key, largeData)
	require.NoError(t, err)

	retrieved, err := fs.Get(key)
	require.NoError(t, err)
	assert.Equal(t, largeData, retrieved)

	size, err := fs.Size(key)
	require.NoError(t, err)
	assert.Equal(t, int64(1024*1024), size)
}

// --- Test 18: TestStorageKeyHashSharding ---

func TestStorageKeyHashSharding(t *testing.T) {
	baseDir := "/some/base/dir"

	// Key hash starting with 0xAB -> hex "ab" -> shard directory "ab"
	key := make([]byte, 32)
	key[0] = 0xAB
	for i := 1; i < 32; i++ {
		key[i] = byte(i)
	}

	path := storage.KeyHashToPath(baseDir, key)
	hexHash := hex.EncodeToString(key)

	expectedShard := hexHash[:2] // "ab"
	expectedPath := filepath.Join(baseDir, expectedShard, hexHash)
	assert.Equal(t, expectedPath, path)

	// Verify the shard directory is the first byte of the key in hex
	assert.Equal(t, "ab", expectedShard)
}

// --- Test 19: TestResolvePathDotSelf ---

func TestResolvePathDotSelf(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	store := newMockNodeStore()

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	docsKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	fileKey, err := w.DeriveNodeKey(0, []uint32{1, 1}, nil)
	require.NoError(t, err)

	rootNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}
	_, err = metanet.AddChild(rootNode, "docs", metanet.NodeTypeDir, docsKey.PublicKey.Compressed(), true)
	require.NoError(t, err)

	docsNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x02}, 32),
		PNode:          docsKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		Parent:         rootKey.PublicKey.Compressed(),
		NextChildIndex: 0,
	}
	_, err = metanet.AddChild(docsNode, "readme.txt", metanet.NodeTypeFile, fileKey.PublicKey.Compressed(), true)
	require.NoError(t, err)

	fileNode := &metanet.Node{
		TxID:     bytes.Repeat([]byte{0x03}, 32),
		PNode:    fileKey.PublicKey.Compressed(),
		Type:     metanet.NodeTypeFile,
		MimeType: "text/plain",
	}

	store.AddNode(rootNode)
	store.AddNode(docsNode)
	store.AddNode(fileNode)

	// SplitPath preserves "."
	components, err := metanet.SplitPath("/docs/./readme.txt")
	require.NoError(t, err)
	assert.Equal(t, []string{"docs", ".", "readme.txt"}, components)

	// ResolvePath should resolve "." as current directory and still find readme.txt
	result, err := metanet.ResolvePath(store, rootNode, components)
	require.NoError(t, err)
	assert.Equal(t, fileNode.PNode, result.Node.PNode)
	assert.Equal(t, metanet.NodeTypeFile, result.Node.Type)
}

// --- Test 20: TestLinkFollowNonLink ---

func TestLinkFollowNonLink(t *testing.T) {
	store := newMockNodeStore()

	fileNode := &metanet.Node{
		TxID:     bytes.Repeat([]byte{0x01}, 32),
		PNode:    bytes.Repeat([]byte{0xAA}, 33),
		Type:     metanet.NodeTypeFile,
		MimeType: "text/plain",
	}
	store.AddNode(fileNode)

	// FollowLink on a non-link node should return ErrNotLink
	_, err := metanet.FollowLink(store, fileNode, 0)
	assert.ErrorIs(t, err, metanet.ErrNotLink,
		"FollowLink on a non-link node should return ErrNotLink")
}

// --- Test 21: TestInheritPricePerKBNoAncestors ---

func TestInheritPricePerKBNoAncestors(t *testing.T) {
	store := newMockNodeStore()

	orphanNode := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x01}, 32),
		PNode:      bytes.Repeat([]byte{0xAA}, 33),
		Type:       metanet.NodeTypeFile,
		PricePerKB: 0,
		Parent:     nil,
	}
	store.AddNode(orphanNode)

	price, err := metanet.InheritPricePerKB(store, orphanNode)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), price)
}

// --- Test 22: TestInheritPricePerKBSelfHasPrice ---

func TestInheritPricePerKBSelfHasPrice(t *testing.T) {
	store := newMockNodeStore()

	node := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x01}, 32),
		PNode:      bytes.Repeat([]byte{0xAA}, 33),
		Type:       metanet.NodeTypeFile,
		PricePerKB: 500,
		Parent:     nil,
	}
	store.AddNode(node)

	price, err := metanet.InheritPricePerKB(store, node)
	require.NoError(t, err)
	assert.Equal(t, uint64(500), price, "node with its own price should return that price")
}

// --- Test 23: TestDirectoryListEmptyDir ---

func TestDirectoryListEmptyDir(t *testing.T) {
	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          bytes.Repeat([]byte{0xAA}, 33),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	entries, err := metanet.ListDirectory(dirNode)
	require.NoError(t, err)
	assert.Empty(t, entries, "empty directory should return empty slice")
}

// --- Test 24: TestFindChildCaseSensitive ---

func TestFindChildCaseSensitive(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	childKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	_, err = metanet.AddChild(dirNode, "README.txt", metanet.NodeTypeFile, childKey.PublicKey.Compressed(), true)
	require.NoError(t, err)

	// Exact match should work
	_, found := metanet.FindChild(dirNode, "README.txt")
	assert.True(t, found)

	// Different case should not match
	_, found = metanet.FindChild(dirNode, "readme.txt")
	assert.False(t, found, "FindChild should be case-sensitive")

	_, found = metanet.FindChild(dirNode, "Readme.txt")
	assert.False(t, found, "FindChild should be case-sensitive")
}

// --- Test 25: TestRenameChildToExistingName ---

func TestRenameChildToExistingName(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	keyA, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	keyB, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	_, err = metanet.AddChild(dirNode, "child-A", metanet.NodeTypeFile, keyA.PublicKey.Compressed(), true)
	require.NoError(t, err)
	_, err = metanet.AddChild(dirNode, "child-B", metanet.NodeTypeFile, keyB.PublicKey.Compressed(), true)
	require.NoError(t, err)

	err = metanet.RenameChild(dirNode, "child-A", "child-B")
	assert.ErrorIs(t, err, metanet.ErrChildExists,
		"renaming to an existing name should return ErrChildExists")
}

// --- Test 26: TestRenameChildNonexistent ---

func TestRenameChildNonexistent(t *testing.T) {
	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          bytes.Repeat([]byte{0xAA}, 33),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	err := metanet.RenameChild(dirNode, "nonexistent", "new-name")
	assert.ErrorIs(t, err, metanet.ErrChildNotFound,
		"renaming a nonexistent child should return ErrChildNotFound")
}

// --- Test 27: TestRemoveChildNonexistent ---

func TestRemoveChildNonexistent(t *testing.T) {
	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          bytes.Repeat([]byte{0xAA}, 33),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	err := metanet.RemoveChild(dirNode, "nonexistent")
	assert.ErrorIs(t, err, metanet.ErrChildNotFound,
		"removing a nonexistent child should return ErrChildNotFound")
}

// --- Test 28: TestNodeTypeStrings ---

func TestNodeTypeStrings(t *testing.T) {
	tests := []struct {
		nt       metanet.NodeType
		expected string
	}{
		{metanet.NodeTypeFile, "FILE"},
		{metanet.NodeTypeDir, "DIR"},
		{metanet.NodeTypeLink, "LINK"},
		{metanet.NodeType(99), "UNKNOWN"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.nt.String())
		})
	}
}

// --- Test 29: TestOpTypeStrings ---

func TestOpTypeStrings(t *testing.T) {
	tests := []struct {
		op       metanet.OpType
		expected string
	}{
		{metanet.OpCreate, "CREATE"},
		{metanet.OpUpdate, "UPDATE"},
		{metanet.OpDelete, "DELETE"},
		{metanet.OpType(99), "UNKNOWN"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.op.String())
		})
	}
}

// --- Test 30: TestNodeIsRootIsFileIsDir ---

func TestNodeIsRootIsFileIsDir(t *testing.T) {
	tests := []struct {
		name   string
		node   *metanet.Node
		isRoot bool
		isFile bool
		isDir  bool
		isLink bool
	}{
		{
			name:   "root directory",
			node:   &metanet.Node{Type: metanet.NodeTypeDir, ParentTxID: nil},
			isRoot: true, isFile: false, isDir: true, isLink: false,
		},
		{
			name:   "non-root directory",
			node:   &metanet.Node{Type: metanet.NodeTypeDir, ParentTxID: bytes.Repeat([]byte{0x01}, 32)},
			isRoot: false, isFile: false, isDir: true, isLink: false,
		},
		{
			name:   "file",
			node:   &metanet.Node{Type: metanet.NodeTypeFile, ParentTxID: bytes.Repeat([]byte{0x02}, 32)},
			isRoot: false, isFile: true, isDir: false, isLink: false,
		},
		{
			name:   "link",
			node:   &metanet.Node{Type: metanet.NodeTypeLink, ParentTxID: bytes.Repeat([]byte{0x03}, 32)},
			isRoot: false, isFile: false, isDir: false, isLink: true,
		},
		{
			name:   "root file (edge case)",
			node:   &metanet.Node{Type: metanet.NodeTypeFile, ParentTxID: nil},
			isRoot: true, isFile: true, isDir: false, isLink: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.isRoot, tc.node.IsRoot())
			assert.Equal(t, tc.isFile, tc.node.IsFile())
			assert.Equal(t, tc.isDir, tc.node.IsDir())
			assert.Equal(t, tc.isLink, tc.node.IsLink())
		})
	}
}

// --- Test 31: TestStorageConcurrentAccess ---

func TestStorageConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "concurrent-store"))
	require.NoError(t, err)

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	keys := make([][]byte, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		key := make([]byte, 32)
		key[0] = byte(i)
		for j := 1; j < 32; j++ {
			key[j] = byte(i + j)
		}
		keys[i] = key
	}

	// Spawn goroutines to store unique keys concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			content := []byte(fmt.Sprintf("content-from-goroutine-%d", idx))
			putErr := fs.Put(keys[idx], content)
			assert.NoError(t, putErr)
		}(i)
	}

	wg.Wait()

	// Verify all keys exist
	for i := 0; i < numGoroutines; i++ {
		exists, err := fs.Has(keys[i])
		require.NoError(t, err)
		assert.True(t, exists, "key %d should exist after concurrent Put", i)

		retrieved, err := fs.Get(keys[i])
		require.NoError(t, err)
		expected := []byte(fmt.Sprintf("content-from-goroutine-%d", i))
		assert.Equal(t, expected, retrieved)
	}
}

// --- Test 32: TestResolvePathRootOnly ---

func TestResolvePathRootOnly(t *testing.T) {
	store := newMockNodeStore()

	rootNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          bytes.Repeat([]byte{0xAA}, 33),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}
	store.AddNode(rootNode)

	// Empty path components should return root
	result, err := metanet.ResolvePath(store, rootNode, []string{})
	require.NoError(t, err)
	assert.Equal(t, rootNode.PNode, result.Node.PNode)
	assert.Empty(t, result.Path)

	// SplitPath("/") also returns empty components
	components, err := metanet.SplitPath("/")
	require.NoError(t, err)
	assert.Empty(t, components)

	result2, err := metanet.ResolvePath(store, rootNode, components)
	require.NoError(t, err)
	assert.Equal(t, rootNode.PNode, result2.Node.PNode)
}

// --- Test 33: TestContentStorageIntegrity ---

func TestContentStorageIntegrity(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "integrity-store"))
	require.NoError(t, err)

	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := []byte("Content integrity verification test data")

	// Encrypt
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// Store
	err = fs.Put(encResult.KeyHash, encResult.Ciphertext)
	require.NoError(t, err)

	// Compute SHA256 of stored ciphertext
	storedHash := sha256.Sum256(encResult.Ciphertext)

	// Retrieve
	retrieved, err := fs.Get(encResult.KeyHash)
	require.NoError(t, err)

	// Compute SHA256 of retrieved bytes
	retrievedHash := sha256.Sum256(retrieved)

	// Verify SHA256 matches
	assert.Equal(t, storedHash, retrievedHash, "SHA256 of stored and retrieved data should match")
	assert.Equal(t, encResult.Ciphertext, retrieved, "byte-for-byte match")
}

// --- Test 34: TestEncryptStoreRetrieveDecrypt ---

func TestEncryptStoreRetrieveDecrypt(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "e2e-store"))
	require.NoError(t, err)

	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	testCases := []struct {
		name   string
		access method42.Access
	}{
		{"Private", method42.AccessPrivate},
		{"Free", method42.AccessFree},
		{"Paid", method42.AccessPaid},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plaintext := []byte(fmt.Sprintf("Full cycle test content for %s access mode - random: %d", tc.name, rand.Int()))

			// Step 1: Derive keys and encrypt
			var encResult *method42.EncryptResult
			switch tc.access {
			case method42.AccessFree:
				encResult, err = method42.Encrypt(plaintext, nil, nodeKey.PublicKey, method42.AccessFree)
			default:
				encResult, err = method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
			}
			require.NoError(t, err)
			require.NotEmpty(t, encResult.Ciphertext)
			require.Len(t, encResult.KeyHash, 32)

			// Step 2: Store in FileStore
			err = fs.Put(encResult.KeyHash, encResult.Ciphertext)
			require.NoError(t, err)

			// Step 3: Retrieve from FileStore
			retrieved, err := fs.Get(encResult.KeyHash)
			require.NoError(t, err)
			assert.Equal(t, encResult.Ciphertext, retrieved)

			// Step 4: Decrypt
			var decResult *method42.DecryptResult
			switch tc.access {
			case method42.AccessFree:
				decResult, err = method42.Decrypt(retrieved, nil, nodeKey.PublicKey, encResult.KeyHash, method42.AccessFree)
			case method42.AccessPaid:
				// Simulate capsule-based decryption with buyer keypair
				buyerPriv, buyerErr := ec.NewPrivateKey()
				require.NoError(t, buyerErr)
				capsule, capsuleErr := method42.ComputeCapsule(nodeKey.PrivateKey, nodeKey.PublicKey, buyerPriv.PubKey(), encResult.KeyHash)
				require.NoError(t, capsuleErr)
				decResult, err = method42.DecryptWithCapsule(retrieved, capsule, encResult.KeyHash, buyerPriv, nodeKey.PublicKey)
			default:
				decResult, err = method42.Decrypt(retrieved, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
			}
			require.NoError(t, err)

			// Step 5: Verify plaintext matches
			assert.Equal(t, plaintext, decResult.Plaintext,
				"decrypted content should match original for %s mode", tc.name)

			// Step 6: Verify key_hash integrity
			assert.Equal(t, encResult.KeyHash, decResult.KeyHash)

			// Cleanup
			err = fs.Delete(encResult.KeyHash)
			require.NoError(t, err)
		})
	}
}

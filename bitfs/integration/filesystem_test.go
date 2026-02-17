//go:build integration

package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/metanet"
	"github.com/tongxiaofeng/bitfs/internal/method42"
	"github.com/tongxiaofeng/bitfs/internal/storage"
	"github.com/tongxiaofeng/bitfs/internal/wallet"
)

// --- Mock NodeStore ---

// mockNodeStore implements metanet.NodeStore for testing.
type mockNodeStore struct {
	nodes map[string]*metanet.Node // key = hex(P_node)
}

func newMockNodeStore() *mockNodeStore {
	return &mockNodeStore{
		nodes: make(map[string]*metanet.Node),
	}
}

// pubKeyHex converts a compressed pubkey to a hex string for use as map key.
func pubKeyHex(pk []byte) string {
	return string(pk) // Use raw bytes as key for simplicity in tests
}

func (m *mockNodeStore) GetNodeByPubKey(pNode []byte) (*metanet.Node, error) {
	node, ok := m.nodes[pubKeyHex(pNode)]
	if !ok {
		return nil, metanet.ErrNodeNotFound
	}
	return node, nil
}

func (m *mockNodeStore) GetNodeByTxID(txID []byte) (*metanet.Node, error) {
	for _, node := range m.nodes {
		if bytes.Equal(node.TxID, txID) {
			return node, nil
		}
	}
	return nil, metanet.ErrNodeNotFound
}

func (m *mockNodeStore) GetNodeVersions(pNode []byte) ([]*metanet.Node, error) {
	// Return all nodes with matching PNode
	var versions []*metanet.Node
	for _, node := range m.nodes {
		if bytes.Equal(node.PNode, pNode) {
			versions = append(versions, node)
		}
	}
	if len(versions) == 0 {
		return nil, metanet.ErrNodeNotFound
	}
	return versions, nil
}

func (m *mockNodeStore) GetChildNodes(dirNode *metanet.Node) ([]*metanet.Node, error) {
	var children []*metanet.Node
	for _, child := range dirNode.Children {
		node, ok := m.nodes[pubKeyHex(child.PubKey)]
		if ok {
			children = append(children, node)
		}
	}
	return children, nil
}

func (m *mockNodeStore) AddNode(node *metanet.Node) {
	m.nodes[pubKeyHex(node.PNode)] = node
}

// --- TestCreateAndResolveDirectory ---

func TestCreateAndResolveDirectory(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	store := newMockNodeStore()

	// Derive keys for the tree
	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	docsKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	readmeKey, err := w.DeriveNodeKey(0, []uint32{1, 1}, nil)
	require.NoError(t, err)

	// 1. Create root DIR node
	rootNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		Op:             metanet.OpCreate,
		NextChildIndex: 0,
	}

	// 2. Add child DIR "docs"
	_, err = metanet.AddChild(rootNode, "docs", metanet.NodeTypeDir, docsKey.PublicKey.Compressed(), true)
	require.NoError(t, err)
	assert.Len(t, rootNode.Children, 1)
	assert.Equal(t, "docs", rootNode.Children[0].Name)
	assert.Equal(t, uint32(1), rootNode.NextChildIndex)

	docsNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x02}, 32),
		PNode:          docsKey.PublicKey.Compressed(),
		ParentTxID:     rootNode.TxID,
		Type:           metanet.NodeTypeDir,
		Op:             metanet.OpCreate,
		Parent:         rootKey.PublicKey.Compressed(),
		NextChildIndex: 0,
	}

	// 3. Add child FILE "readme.txt" to docs
	_, err = metanet.AddChild(docsNode, "readme.txt", metanet.NodeTypeFile, readmeKey.PublicKey.Compressed(), true)
	require.NoError(t, err)
	assert.Len(t, docsNode.Children, 1)

	readmeNode := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x03}, 32),
		PNode:      readmeKey.PublicKey.Compressed(),
		ParentTxID: docsNode.TxID,
		Type:       metanet.NodeTypeFile,
		Op:         metanet.OpCreate,
		MimeType:   "text/plain",
		FileSize:   42,
		Parent:     docsKey.PublicKey.Compressed(),
	}

	// Register nodes in store
	store.AddNode(rootNode)
	store.AddNode(docsNode)
	store.AddNode(readmeNode)

	// 4. Resolve path "/docs/readme.txt" -> correct node
	components, err := metanet.SplitPath("/docs/readme.txt")
	require.NoError(t, err)
	assert.Equal(t, []string{"docs", "readme.txt"}, components)

	result, err := metanet.ResolvePath(store, rootNode, components)
	require.NoError(t, err)
	assert.Equal(t, readmeNode.PNode, result.Node.PNode)
	assert.Equal(t, metanet.NodeTypeFile, result.Node.Type)
	assert.Equal(t, "text/plain", result.Node.MimeType)
	assert.Equal(t, []string{"docs", "readme.txt"}, result.Path)

	// 5. Resolve ".." from readme -> docs
	components2, err := metanet.SplitPath("/docs/readme.txt/..")
	require.NoError(t, err)
	result2, err := metanet.ResolvePath(store, rootNode, components2)
	require.NoError(t, err)
	assert.Equal(t, docsNode.PNode, result2.Node.PNode)

	// 6. Resolve "../.." -> root
	components3, err := metanet.SplitPath("/docs/readme.txt/../..")
	require.NoError(t, err)
	result3, err := metanet.ResolvePath(store, rootNode, components3)
	require.NoError(t, err)
	assert.Equal(t, rootNode.PNode, result3.Node.PNode)

	// Can't go above root
	components4, err := metanet.SplitPath("/../../..")
	require.NoError(t, err)
	result4, err := metanet.ResolvePath(store, rootNode, components4)
	require.NoError(t, err)
	assert.Equal(t, rootNode.PNode, result4.Node.PNode, ".. from root should stay at root")
}

// --- TestContentStorageRoundTrip ---

func TestContentStorageRoundTrip(t *testing.T) {
	// Create temp dir for storage
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "bitfs-store")

	fs, err := storage.NewFileStore(storeDir)
	require.NoError(t, err)

	// Create wallet and derive keys
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	// 1. Encrypt test content with Method42
	plaintext := []byte("This is test content stored in FileStore")
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// 2. Store encrypted bytes in storage (key_hash as key)
	err = fs.Put(encResult.KeyHash, encResult.Ciphertext)
	require.NoError(t, err)

	// Verify it exists
	exists, err := fs.Has(encResult.KeyHash)
	require.NoError(t, err)
	assert.True(t, exists)

	// Verify size
	size, err := fs.Size(encResult.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, int64(len(encResult.Ciphertext)), size)

	// 3. Retrieve from storage
	retrieved, err := fs.Get(encResult.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, encResult.Ciphertext, retrieved)

	// 4. Decrypt
	decResult, err := method42.Decrypt(retrieved, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
	require.NoError(t, err)

	// 5. Verify matches original
	assert.Equal(t, plaintext, decResult.Plaintext)

	// Verify the file is on disk in the correct sharded path
	expectedPath := storage.KeyHashToPath(storeDir, encResult.KeyHash)
	_, err = os.Stat(expectedPath)
	assert.NoError(t, err, "file should exist at sharded path")

	// Cleanup: delete from store
	err = fs.Delete(encResult.KeyHash)
	require.NoError(t, err)
	exists, err = fs.Has(encResult.KeyHash)
	require.NoError(t, err)
	assert.False(t, exists)
}

// --- TestVersionResolution ---

func TestVersionResolution(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	pNode := nodeKey.PublicKey.Compressed()

	// 1. Create file node v1 (block height 100)
	v1 := &metanet.Node{
		TxID:        bytes.Repeat([]byte{0x01}, 32),
		PNode:       pNode,
		BlockHeight: 100,
		Type:        metanet.NodeTypeFile,
		Version:     1,
		Timestamp:   1000,
	}

	// 2. Create file node v2 (same P_node, block height 200)
	v2 := &metanet.Node{
		TxID:        bytes.Repeat([]byte{0x02}, 32),
		PNode:       pNode,
		BlockHeight: 200,
		Type:        metanet.NodeTypeFile,
		Version:     2,
		Timestamp:   2000,
	}

	// 3. LatestVersion should return v2
	latest := metanet.LatestVersion([]*metanet.Node{v1, v2})
	assert.Equal(t, v2.TxID, latest.TxID, "latest should be v2 with higher block height")
	assert.Equal(t, uint32(200), latest.BlockHeight)

	// Also test reverse order
	latest2 := metanet.LatestVersion([]*metanet.Node{v2, v1})
	assert.Equal(t, v2.TxID, latest2.TxID)

	// Test same block height, different timestamp (TTOR)
	v3 := &metanet.Node{
		TxID:        bytes.Repeat([]byte{0x03}, 32),
		PNode:       pNode,
		BlockHeight: 200,
		Type:        metanet.NodeTypeFile,
		Version:     3,
		Timestamp:   3000,
	}
	latest3 := metanet.LatestVersion([]*metanet.Node{v1, v2, v3})
	assert.Equal(t, v3.TxID, latest3.TxID, "latest should be v3 with same height but later timestamp")

	// Test nil handling
	latest4 := metanet.LatestVersion([]*metanet.Node{nil, v1, nil})
	assert.Equal(t, v1.TxID, latest4.TxID)

	// Empty list
	assert.Nil(t, metanet.LatestVersion(nil))
	assert.Nil(t, metanet.LatestVersion([]*metanet.Node{}))
}

// --- TestLinkFollowing ---

func TestLinkFollowing(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	store := newMockNodeStore()

	// Create target file
	targetKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	targetNode := &metanet.Node{
		TxID:     bytes.Repeat([]byte{0x01}, 32),
		PNode:    targetKey.PublicKey.Compressed(),
		Type:     metanet.NodeTypeFile,
		MimeType: "text/plain",
		FileSize: 100,
	}
	store.AddNode(targetNode)

	// Create soft link pointing to target
	linkKey, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)
	linkNode := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x02}, 32),
		PNode:      linkKey.PublicKey.Compressed(),
		Type:       metanet.NodeTypeLink,
		LinkType:   metanet.LinkTypeSoft,
		LinkTarget: targetKey.PublicKey.Compressed(),
	}
	store.AddNode(linkNode)

	// Follow link -> resolve to target
	resolved, err := metanet.FollowLink(store, linkNode, 0) // 0 = default MaxLinkDepth
	require.NoError(t, err)
	assert.Equal(t, targetNode.PNode, resolved.PNode)
	assert.Equal(t, metanet.NodeTypeFile, resolved.Type)
	assert.Equal(t, "text/plain", resolved.MimeType)

	// Test link chain (link -> link -> file)
	link2Key, err := w.DeriveNodeKey(0, []uint32{3}, nil)
	require.NoError(t, err)
	link2Node := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x03}, 32),
		PNode:      link2Key.PublicKey.Compressed(),
		Type:       metanet.NodeTypeLink,
		LinkType:   metanet.LinkTypeSoft,
		LinkTarget: linkKey.PublicKey.Compressed(),
	}
	store.AddNode(link2Node)

	resolved2, err := metanet.FollowLink(store, link2Node, 0)
	require.NoError(t, err)
	assert.Equal(t, targetNode.PNode, resolved2.PNode, "should follow chain: link2 -> link -> target")

	// Test max depth prevents infinite loops
	// Create a circular link chain
	loopKey1, err := w.DeriveNodeKey(0, []uint32{10}, nil)
	require.NoError(t, err)
	loopKey2, err := w.DeriveNodeKey(0, []uint32{11}, nil)
	require.NoError(t, err)

	loopNode1 := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x10}, 32),
		PNode:      loopKey1.PublicKey.Compressed(),
		Type:       metanet.NodeTypeLink,
		LinkType:   metanet.LinkTypeSoft,
		LinkTarget: loopKey2.PublicKey.Compressed(),
	}
	loopNode2 := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x11}, 32),
		PNode:      loopKey2.PublicKey.Compressed(),
		Type:       metanet.NodeTypeLink,
		LinkType:   metanet.LinkTypeSoft,
		LinkTarget: loopKey1.PublicKey.Compressed(),
	}
	store.AddNode(loopNode1)
	store.AddNode(loopNode2)

	_, err = metanet.FollowLink(store, loopNode1, metanet.MaxLinkDepth)
	assert.ErrorIs(t, err, metanet.ErrLinkDepthExceeded, "circular links should hit depth limit")
}

// --- TestPriceInheritance ---

func TestPriceInheritance(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	store := newMockNodeStore()

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	childKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	grandchildKey, err := w.DeriveNodeKey(0, []uint32{1, 1}, nil)
	require.NoError(t, err)

	// 1. Set price on root directory (100 sat/KB)
	rootNode := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x01}, 32),
		PNode:      rootKey.PublicKey.Compressed(),
		Type:       metanet.NodeTypeDir,
		PricePerKB: 100,
	}
	store.AddNode(rootNode)

	// 2. Create child file with no price
	childNode := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x02}, 32),
		PNode:      childKey.PublicKey.Compressed(),
		Type:       metanet.NodeTypeFile,
		PricePerKB: 0, // no price set
		Parent:     rootKey.PublicKey.Compressed(),
	}
	store.AddNode(childNode)

	// 3. InheritPricePerKB should return 100 (from root)
	price, err := metanet.InheritPricePerKB(store, childNode)
	require.NoError(t, err)
	assert.Equal(t, uint64(100), price, "child should inherit price from root")

	// Test grandchild also inherits
	grandchildNode := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x03}, 32),
		PNode:      grandchildKey.PublicKey.Compressed(),
		Type:       metanet.NodeTypeFile,
		PricePerKB: 0,
		Parent:     childKey.PublicKey.Compressed(),
	}
	store.AddNode(grandchildNode)

	price2, err := metanet.InheritPricePerKB(store, grandchildNode)
	require.NoError(t, err)
	assert.Equal(t, uint64(100), price2, "grandchild should inherit from root")

	// Test node with own price overrides
	childNode.PricePerKB = 200
	store.AddNode(childNode) // update
	price3, err := metanet.InheritPricePerKB(store, grandchildNode)
	require.NoError(t, err)
	assert.Equal(t, uint64(200), price3, "grandchild should inherit from nearest ancestor with price")

	// Root returns its own price
	price4, err := metanet.InheritPricePerKB(store, rootNode)
	require.NoError(t, err)
	assert.Equal(t, uint64(100), price4)

	// Node with no price and no parent returns 0
	orphanNode := &metanet.Node{
		TxID:       bytes.Repeat([]byte{0x04}, 32),
		PNode:      bytes.Repeat([]byte{0x02}, 33),
		Type:       metanet.NodeTypeFile,
		PricePerKB: 0,
		Parent:     nil,
	}
	store.AddNode(orphanNode)
	price5, err := metanet.InheritPricePerKB(store, orphanNode)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), price5)
}

// --- TestDirectoryOperations ---

func TestDirectoryOperations(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	// Add multiple children
	for i := uint32(1); i <= 3; i++ {
		childKey, err := w.DeriveNodeKey(0, []uint32{i}, nil)
		require.NoError(t, err)
		_, err = metanet.AddChild(dirNode, childName(i), metanet.NodeTypeFile, childKey.PublicKey.Compressed(), true)
		require.NoError(t, err)
	}

	assert.Len(t, dirNode.Children, 3)
	assert.Equal(t, uint32(3), dirNode.NextChildIndex)

	// List directory
	entries, err := metanet.ListDirectory(dirNode)
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	// Find child
	entry, found := metanet.FindChild(dirNode, "file-1")
	assert.True(t, found)
	assert.Equal(t, "file-1", entry.Name)

	_, found = metanet.FindChild(dirNode, "nonexistent")
	assert.False(t, found)

	// Rename child
	err = metanet.RenameChild(dirNode, "file-1", "renamed.txt")
	require.NoError(t, err)
	_, found = metanet.FindChild(dirNode, "renamed.txt")
	assert.True(t, found)
	_, found = metanet.FindChild(dirNode, "file-1")
	assert.False(t, found)

	// Remove child
	err = metanet.RemoveChild(dirNode, "file-2")
	require.NoError(t, err)
	assert.Len(t, dirNode.Children, 2)
	assert.Equal(t, uint32(3), dirNode.NextChildIndex, "NextChildIndex should not decrease after remove")

	// Duplicate name
	childKey, err := w.DeriveNodeKey(0, []uint32{4}, nil)
	require.NoError(t, err)
	_, err = metanet.AddChild(dirNode, "file-3", metanet.NodeTypeFile, childKey.PublicKey.Compressed(), true)
	assert.ErrorIs(t, err, metanet.ErrChildExists)
}

func childName(i uint32) string {
	names := map[uint32]string{1: "file-1", 2: "file-2", 3: "file-3"}
	return names[i]
}

// --- TestStorageListAndMultipleFiles ---

func TestStorageListAndMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "store"))
	require.NoError(t, err)

	w, _, _ := createTestWallet(t, &wallet.MainNet)

	// Store 3 encrypted files
	var keyHashes [][]byte
	for i := uint32(1); i <= 3; i++ {
		nodeKey, err := w.DeriveNodeKey(0, []uint32{i}, nil)
		require.NoError(t, err)

		plaintext := []byte("File content #" + string(rune('0'+i)))
		encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
		require.NoError(t, err)

		err = fs.Put(encResult.KeyHash, encResult.Ciphertext)
		require.NoError(t, err)
		keyHashes = append(keyHashes, encResult.KeyHash)
	}

	// List should return all 3
	list, err := fs.List()
	require.NoError(t, err)
	assert.Len(t, list, 3)

	// All key hashes should exist
	for _, kh := range keyHashes {
		exists, err := fs.Has(kh)
		require.NoError(t, err)
		assert.True(t, exists)
	}
}

// --- TestSplitPathEdgeCases ---

func TestSplitPathEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []string
		wantErr  bool
	}{
		{"root", "/", []string{}, false},
		{"simple", "/docs/file.txt", []string{"docs", "file.txt"}, false},
		{"trailing slash", "/docs/", []string{"docs"}, false},
		{"double slash", "/docs//file.txt", []string{"docs", "file.txt"}, false},
		{"relative", "docs/file.txt", []string{"docs", "file.txt"}, false},
		{"dots", "/docs/./file.txt", []string{"docs", ".", "file.txt"}, false},
		{"dotdot", "/docs/../file.txt", []string{"docs", "..", "file.txt"}, false},
		{"empty", "", nil, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := metanet.SplitPath(tc.path)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

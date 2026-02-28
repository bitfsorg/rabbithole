//go:build integration

package integration

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/bitfsorg/libbitfs-go/metanet"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/wallet"
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

// --- TestDeepNestedPath (T034) ---

func TestDeepNestedPath(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	store := newMockNodeStore()

	// Build 12-level deep directory tree: /a/b/c/d/e/f/g/h/i/j/k/file.txt
	levels := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "file.txt"}
	depth := len(levels) // 12

	// Derive keys for every level
	// Root: vault root key
	// Level 1..11 (dirs a..k): DeriveNodeKey(0, [1]..[1,1,...,1])
	// Level 12 (file.txt): DeriveNodeKey(0, [1,1,...,1,1]) with one more level

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	// Pre-derive all keys for the path [1,1,1,...,1] at each depth
	type levelInfo struct {
		key  *wallet.KeyPair
		node *metanet.Node
	}
	levelInfos := make([]levelInfo, depth+1) // index 0 = root

	// Root node
	rootNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x00}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		Op:             metanet.OpCreate,
		NextChildIndex: 0,
	}
	levelInfos[0] = levelInfo{key: rootKey, node: rootNode}
	store.AddNode(rootNode)

	// Build each level
	for i := 0; i < depth; i++ {
		parentInfo := levelInfos[i]

		// Build the file path indices (all 1's)
		filePath := make([]uint32, i+1)
		for j := range filePath {
			filePath[j] = 1
		}

		childKey, err := w.DeriveNodeKey(0, filePath, nil)
		require.NoError(t, err, "failed to derive key at depth %d", i+1)

		name := levels[i]

		// Determine node type
		nodeType := metanet.NodeTypeDir
		if i == depth-1 {
			nodeType = metanet.NodeTypeFile
		}

		// Add child to parent
		_, err = metanet.AddChild(parentInfo.node, name, nodeType, childKey.PublicKey.Compressed(), true)
		require.NoError(t, err, "failed to add child %q at depth %d", name, i+1)

		// Create the child node
		childNode := &metanet.Node{
			TxID:       bytes.Repeat([]byte{byte(i + 1)}, 32),
			PNode:      childKey.PublicKey.Compressed(),
			ParentTxID: parentInfo.node.TxID,
			Type:       nodeType,
			Op:         metanet.OpCreate,
			Parent:     parentInfo.key.PublicKey.Compressed(),
		}
		if nodeType == metanet.NodeTypeDir {
			childNode.NextChildIndex = 0
		}
		if nodeType == metanet.NodeTypeFile {
			childNode.MimeType = "text/plain"
			childNode.FileSize = 42
		}

		store.AddNode(childNode)
		levelInfos[i+1] = levelInfo{key: childKey, node: childNode}
	}

	// 1. ResolvePath from root with full path
	fullPath := "/" + joinPath(levels)
	components, err := metanet.SplitPath(fullPath)
	require.NoError(t, err)
	assert.Len(t, components, depth)

	result, err := metanet.ResolvePath(store, rootNode, components)
	require.NoError(t, err, "should resolve 12-level deep path")
	assert.Equal(t, levelInfos[depth].node.PNode, result.Node.PNode)
	assert.Equal(t, metanet.NodeTypeFile, result.Node.Type)
	assert.Equal(t, "text/plain", result.Node.MimeType)
	assert.Equal(t, levels, result.Path)

	// 2. ResolvePath with ".." at various depths
	// /a/b/c/d/.. should resolve to /a/b/c
	partialPath := "/a/b/c/d/.."
	components2, err := metanet.SplitPath(partialPath)
	require.NoError(t, err)
	result2, err := metanet.ResolvePath(store, rootNode, components2)
	require.NoError(t, err)
	assert.Equal(t, levelInfos[3].node.PNode, result2.Node.PNode, ".. from d should resolve to c")

	// 3. Going up all the way to root using multiple ..
	upAll := "/a/b/c/../../.."
	components3, err := metanet.SplitPath(upAll)
	require.NoError(t, err)
	result3, err := metanet.ResolvePath(store, rootNode, components3)
	require.NoError(t, err)
	assert.Equal(t, rootNode.PNode, result3.Node.PNode, "going up to root from c should reach root")

	// 4. Resolving intermediate directory works
	midPath := "/a/b/c/d/e/f"
	components4, err := metanet.SplitPath(midPath)
	require.NoError(t, err)
	result4, err := metanet.ResolvePath(store, rootNode, components4)
	require.NoError(t, err)
	assert.Equal(t, levelInfos[6].node.PNode, result4.Node.PNode, "should resolve to level 6 (f)")
	assert.Equal(t, metanet.NodeTypeDir, result4.Node.Type)
}

// joinPath concatenates path components with "/".
func joinPath(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "/"
		}
		result += p
	}
	return result
}

// --- TestConcurrentDirectoryIndex (T036) ---

func TestConcurrentDirectoryIndex(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)

	dirNode := &metanet.Node{
		TxID:           bytes.Repeat([]byte{0x01}, 32),
		PNode:          rootKey.PublicKey.Compressed(),
		Type:           metanet.NodeTypeDir,
		NextChildIndex: 0,
	}

	// 1. Add 20 children sequentially
	for i := uint32(0); i < 20; i++ {
		childKey, err := w.DeriveNodeKey(0, []uint32{i + 1}, nil)
		require.NoError(t, err)

		name := fmt.Sprintf("child-%d", i)
		entry, err := metanet.AddChild(dirNode, name, metanet.NodeTypeFile, childKey.PublicKey.Compressed(), true)
		require.NoError(t, err)

		// Verify each child gets a monotonically increasing index
		assert.Equal(t, i, entry.Index, "child %d should get index %d", i, i)
	}

	// 2. Verify NextChildIndex is 20
	assert.Equal(t, uint32(20), dirNode.NextChildIndex,
		"after adding 20 children, NextChildIndex should be 20")
	assert.Len(t, dirNode.Children, 20)

	// 3. Verify indices are monotonically increasing (0,1,2,...,19)
	for i, child := range dirNode.Children {
		assert.Equal(t, uint32(i), child.Index,
			"child at position %d should have index %d", i, i)
	}

	// 4. Remove child at index 5 (name "child-5")
	err = metanet.RemoveChild(dirNode, "child-5")
	require.NoError(t, err)
	assert.Len(t, dirNode.Children, 19)

	// Verify NextChildIndex stays at 20 (indices are never reused)
	assert.Equal(t, uint32(20), dirNode.NextChildIndex,
		"NextChildIndex should NOT decrease after remove")

	// 5. Add another child, verify it gets index 20 (not 5)
	newChildKey, err := w.DeriveNodeKey(0, []uint32{21}, nil)
	require.NoError(t, err)

	newEntry, err := metanet.AddChild(dirNode, "child-new", metanet.NodeTypeFile, newChildKey.PublicKey.Compressed(), true)
	require.NoError(t, err)
	assert.Equal(t, uint32(20), newEntry.Index,
		"new child should get index 20, not reuse deleted index 5")
	assert.Equal(t, uint32(21), dirNode.NextChildIndex)
	assert.Len(t, dirNode.Children, 20)

	// 6. Verify the removed child-5 is actually gone
	_, found := metanet.FindChild(dirNode, "child-5")
	assert.False(t, found, "removed child should not be findable")

	// Verify remaining children are intact
	_, found = metanet.FindChild(dirNode, "child-0")
	assert.True(t, found)
	_, found = metanet.FindChild(dirNode, "child-19")
	assert.True(t, found)
	_, found = metanet.FindChild(dirNode, "child-new")
	assert.True(t, found)
}

// --- TestStorageWithAllAccessModes (T037) ---

func TestStorageWithAllAccessModes(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "access-mode-store"))
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
		{"Paid (via capsule)", method42.AccessPaid},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plaintext := []byte(fmt.Sprintf("Content with %s access mode", tc.name))

			// 1. Encrypt content
			var encResult *method42.EncryptResult
			var encErr error

			switch tc.access {
			case method42.AccessFree:
				// Free mode: use nil private key
				encResult, encErr = method42.Encrypt(plaintext, nil, nodeKey.PublicKey, method42.AccessFree)
			default:
				// Private and Paid use the same encryption (private key)
				encResult, encErr = method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
			}
			require.NoError(t, encErr)
			require.NotEmpty(t, encResult.Ciphertext)
			require.Len(t, encResult.KeyHash, 32)

			// 2. Store in FileStore
			err := fs.Put(encResult.KeyHash, encResult.Ciphertext)
			require.NoError(t, err)

			// 3. Retrieve from FileStore
			retrieved, err := fs.Get(encResult.KeyHash)
			require.NoError(t, err)
			assert.Equal(t, encResult.Ciphertext, retrieved)

			// Verify storage integrity
			exists, err := fs.Has(encResult.KeyHash)
			require.NoError(t, err)
			assert.True(t, exists)

			// 4. Decrypt and verify
			var decResult *method42.DecryptResult
			var decErr error

			switch tc.access {
			case method42.AccessFree:
				decResult, decErr = method42.Decrypt(retrieved, nil, nodeKey.PublicKey, encResult.KeyHash, method42.AccessFree)
			case method42.AccessPaid:
				// For Paid mode, generate a buyer keypair and use XOR capsule flow
				buyerPriv, err := ec.NewPrivateKey()
				require.NoError(t, err)
				capsule, err := method42.ComputeCapsule(nodeKey.PrivateKey, nodeKey.PublicKey, buyerPriv.PubKey(), encResult.KeyHash)
				require.NoError(t, err)
				decResult, decErr = method42.DecryptWithCapsule(retrieved, capsule, encResult.KeyHash, buyerPriv, nodeKey.PublicKey)
			default:
				decResult, decErr = method42.Decrypt(retrieved, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
			}

			require.NoError(t, decErr)
			assert.Equal(t, plaintext, decResult.Plaintext,
				"decrypted content should match original for %s mode", tc.name)

			// Verify key_hash integrity
			assert.Equal(t, encResult.KeyHash, decResult.KeyHash)

			// Cleanup for next iteration
			err = fs.Delete(encResult.KeyHash)
			require.NoError(t, err)
		})
	}
}

// --- TestContentAddressedDedup (T038) ---

func TestContentAddressedDedup(t *testing.T) {
	tmpDir := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(tmpDir, "dedup-store"))
	require.NoError(t, err)

	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := []byte("Identical content for deduplication test")

	// 1. Encrypt the same plaintext twice with the same keys
	enc1, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	enc2, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// 2. Both should produce the same key_hash (SHA256(SHA256(plaintext)))
	assert.Equal(t, enc1.KeyHash, enc2.KeyHash,
		"same plaintext should produce identical key_hash")

	// Verify key_hash matches manual computation
	first := sha256.Sum256(plaintext)
	second := sha256.Sum256(first[:])
	assert.Equal(t, second[:], enc1.KeyHash,
		"key_hash should be double-SHA256 of plaintext")

	// 3. Ciphertext is different (random IV per encryption)
	assert.NotEqual(t, enc1.Ciphertext, enc2.Ciphertext,
		"different encryptions should produce different ciphertext (random IV)")

	// 4. Store first
	err = fs.Put(enc1.KeyHash, enc1.Ciphertext)
	require.NoError(t, err)

	// Store second (overwrites because same key_hash)
	err = fs.Put(enc2.KeyHash, enc2.Ciphertext)
	require.NoError(t, err)

	// 5. FileStore should have exactly 1 entry (same key)
	list, err := fs.List()
	require.NoError(t, err)
	assert.Len(t, list, 1, "content-addressed store should have exactly 1 entry for duplicate content")

	// 6. The stored ciphertext should be the second one (overwrite)
	retrieved, err := fs.Get(enc1.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, enc2.Ciphertext, retrieved,
		"second Put should overwrite first")

	// 7. Both ciphertexts should still decrypt correctly (same AES key)
	dec1, err := method42.Decrypt(enc1.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, enc1.KeyHash, method42.AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, plaintext, dec1.Plaintext)

	dec2, err := method42.Decrypt(retrieved, nodeKey.PrivateKey, nodeKey.PublicKey, enc2.KeyHash, method42.AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, plaintext, dec2.Plaintext)

	// 8. Different plaintext -> different key_hash -> separate entry
	differentPlaintext := []byte("Completely different content")
	enc3, err := method42.Encrypt(differentPlaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	assert.NotEqual(t, enc1.KeyHash, enc3.KeyHash,
		"different plaintext should produce different key_hash")

	err = fs.Put(enc3.KeyHash, enc3.Ciphertext)
	require.NoError(t, err)

	list2, err := fs.List()
	require.NoError(t, err)
	assert.Len(t, list2, 2, "two different contents should result in 2 entries")
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

package metanet

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/tx"
)

// --- Helper types and functions ---

// mockNodeStore is a simple in-memory NodeStore for testing.
type mockNodeStore struct {
	byPubKey map[string]*Node
	byTxID   map[string]*Node
	versions map[string][]*Node
}

func newMockStore() *mockNodeStore {
	return &mockNodeStore{
		byPubKey: make(map[string]*Node),
		byTxID:   make(map[string]*Node),
		versions: make(map[string][]*Node),
	}
}

func (m *mockNodeStore) addNode(node *Node) {
	if len(node.PNode) > 0 {
		key := string(node.PNode)
		m.byPubKey[key] = node
		m.versions[key] = append(m.versions[key], node)
	}
	if len(node.TxID) > 0 {
		m.byTxID[string(node.TxID)] = node
	}
}

func (m *mockNodeStore) GetNodeByPubKey(pNode []byte) (*Node, error) {
	n, ok := m.byPubKey[string(pNode)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return n, nil
}

func (m *mockNodeStore) GetNodeByTxID(txID []byte) (*Node, error) {
	n, ok := m.byTxID[string(txID)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return n, nil
}

func (m *mockNodeStore) GetNodeVersions(pNode []byte) ([]*Node, error) {
	v := m.versions[string(pNode)]
	if len(v) == 0 {
		return nil, fmt.Errorf("not found")
	}
	return v, nil
}

func (m *mockNodeStore) GetChildNodes(dirNode *Node) ([]*Node, error) {
	var result []*Node
	for _, child := range dirNode.Children {
		n, ok := m.byPubKey[string(child.PubKey)]
		if ok {
			result = append(result, n)
		}
	}
	return result, nil
}

func makePubKey(seed byte) []byte {
	pk := make([]byte, CompressedPubKeyLen)
	pk[0] = 0x02 // valid compressed key prefix
	for i := 1; i < CompressedPubKeyLen; i++ {
		pk[i] = seed
	}
	return pk
}

func makeTxID(seed byte) []byte {
	id := make([]byte, TxIDLen)
	for i := range id {
		id[i] = seed
	}
	return id
}

func makeRootDir(pNode []byte) *Node {
	return &Node{
		TxID:           makeTxID(0x01),
		PNode:          pNode,
		Type:           NodeTypeDir,
		Children:       []ChildEntry{},
		NextChildIndex: 0,
		Metadata:       make(map[string]string),
	}
}

func makeFileNode(pNode, parentPNode []byte, txID []byte) *Node {
	return &Node{
		TxID:     txID,
		PNode:    pNode,
		Type:     NodeTypeFile,
		Parent:   parentPNode,
		MimeType: "text/plain",
		FileSize: 1024,
		KeyHash:  bytes.Repeat([]byte{0xAA}, 32),
		Metadata: make(map[string]string),
	}
}

func makeDirNode(pNode, parentPNode []byte, txID []byte) *Node {
	return &Node{
		TxID:           txID,
		PNode:          pNode,
		Type:           NodeTypeDir,
		Parent:         parentPNode,
		Children:       []ChildEntry{},
		NextChildIndex: 0,
		Metadata:       make(map[string]string),
	}
}

func makeLinkNode(pNode, target []byte, linkType LinkType) *Node {
	return &Node{
		TxID:       makeTxID(0xCC),
		PNode:      pNode,
		Type:       NodeTypeLink,
		LinkTarget: target,
		LinkType:   linkType,
		Metadata:   make(map[string]string),
	}
}

// --- NodeType tests ---

func TestNodeType_String(t *testing.T) {
	tests := []struct {
		nt       NodeType
		expected string
	}{
		{NodeTypeFile, "FILE"},
		{NodeTypeDir, "DIR"},
		{NodeTypeLink, "LINK"},
		{NodeType(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.nt.String())
		})
	}
}

func TestOpType_String(t *testing.T) {
	tests := []struct {
		op       OpType
		expected string
	}{
		{OpCreate, "CREATE"},
		{OpUpdate, "UPDATE"},
		{OpDelete, "DELETE"},
		{OpType(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.op.String())
		})
	}
}

// --- Node method tests ---

func TestNode_IsRoot(t *testing.T) {
	root := &Node{ParentTxID: nil}
	assert.True(t, root.IsRoot())

	child := &Node{ParentTxID: makeTxID(0x01)}
	assert.False(t, child.IsRoot())
}

func TestNode_IsDir(t *testing.T) {
	dir := &Node{Type: NodeTypeDir}
	assert.True(t, dir.IsDir())
	assert.False(t, dir.IsFile())
	assert.False(t, dir.IsLink())
}

func TestNode_IsFile(t *testing.T) {
	file := &Node{Type: NodeTypeFile}
	assert.True(t, file.IsFile())
	assert.False(t, file.IsDir())
	assert.False(t, file.IsLink())
}

func TestNode_IsLink(t *testing.T) {
	link := &Node{Type: NodeTypeLink}
	assert.True(t, link.IsLink())
	assert.False(t, link.IsDir())
	assert.False(t, link.IsFile())
}

// --- Serialization round-trip tests ---

func TestSerializePayload_RoundTrip_File(t *testing.T) {
	node := &Node{
		Version:    1,
		Type:       NodeTypeFile,
		Op:         OpCreate,
		MimeType:   "application/pdf",
		FileSize:   4096,
		KeyHash:    bytes.Repeat([]byte{0xBB}, 32),
		Access:     AccessPaid,
		PricePerKB: 100,
		Timestamp:  1700000000,
		Parent:     makePubKey(0x01),
		Index:      5,
		Encrypted:  true,
		Metadata:   make(map[string]string),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)
	assert.NotEmpty(t, payload)

	// Deserialize back
	decoded := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, decoded)
	require.NoError(t, err)

	assert.Equal(t, node.Version, decoded.Version)
	assert.Equal(t, node.Type, decoded.Type)
	assert.Equal(t, node.Op, decoded.Op)
	assert.Equal(t, node.MimeType, decoded.MimeType)
	assert.Equal(t, node.FileSize, decoded.FileSize)
	assert.Equal(t, node.KeyHash, decoded.KeyHash)
	assert.Equal(t, node.Access, decoded.Access)
	assert.Equal(t, node.PricePerKB, decoded.PricePerKB)
	assert.Equal(t, node.Timestamp, decoded.Timestamp)
	assert.Equal(t, node.Parent, decoded.Parent)
	assert.Equal(t, node.Index, decoded.Index)
	assert.Equal(t, node.Encrypted, decoded.Encrypted)
}

func TestSerializePayload_RoundTrip_Dir(t *testing.T) {
	node := &Node{
		Version: 1,
		Type:    NodeTypeDir,
		Op:      OpCreate,
		Children: []ChildEntry{
			{Index: 0, Name: "readme.txt", Type: NodeTypeFile, PubKey: makePubKey(0x10), Hardened: false},
			{Index: 1, Name: "docs", Type: NodeTypeDir, PubKey: makePubKey(0x20), Hardened: true},
		},
		NextChildIndex: 2,
		Domain:         "example.com",
		Description:    "Test directory",
		Metadata:       make(map[string]string),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	decoded := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, decoded)
	require.NoError(t, err)

	assert.Equal(t, node.Type, decoded.Type)
	assert.Len(t, decoded.Children, 2)
	assert.Equal(t, "readme.txt", decoded.Children[0].Name)
	assert.Equal(t, NodeTypeFile, decoded.Children[0].Type)
	assert.Equal(t, "docs", decoded.Children[1].Name)
	assert.Equal(t, NodeTypeDir, decoded.Children[1].Type)
	assert.True(t, decoded.Children[1].Hardened)
	assert.Equal(t, uint32(2), decoded.NextChildIndex)
	assert.Equal(t, "example.com", decoded.Domain)
	assert.Equal(t, "Test directory", decoded.Description)
}

func TestSerializePayload_RoundTrip_Link(t *testing.T) {
	node := &Node{
		Version:    1,
		Type:       NodeTypeLink,
		Op:         OpCreate,
		LinkTarget: makePubKey(0x42),
		LinkType:   LinkTypeSoft,
		Metadata:   make(map[string]string),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	decoded := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, decoded)
	require.NoError(t, err)

	assert.Equal(t, NodeTypeLink, decoded.Type)
	assert.Equal(t, node.LinkTarget, decoded.LinkTarget)
	assert.Equal(t, LinkTypeSoft, decoded.LinkType)
}

func TestSerializePayload_Nil(t *testing.T) {
	_, err := SerializePayload(nil)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestSerializePayload_OnChainContent(t *testing.T) {
	node := &Node{
		Version:  1,
		Type:     NodeTypeFile,
		OnChain:  true,
		ContentTxIDs: [][]byte{
			makeTxID(0x01),
			makeTxID(0x02),
		},
		Compression: 1,
		Metadata:    make(map[string]string),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	decoded := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, decoded)
	require.NoError(t, err)

	assert.True(t, decoded.OnChain)
	assert.Len(t, decoded.ContentTxIDs, 2)
	assert.Equal(t, makeTxID(0x01), decoded.ContentTxIDs[0])
	assert.Equal(t, int32(1), decoded.Compression)
}

func TestSerializePayload_WithAllOptionalFields(t *testing.T) {
	node := &Node{
		Version:      1,
		Type:         NodeTypeFile,
		Op:           OpUpdate,
		MimeType:     "image/png",
		FileSize:     1048576,
		KeyHash:      bytes.Repeat([]byte{0xCC}, 32),
		Access:       AccessFree,
		PricePerKB:   50,
		Timestamp:    1700000000,
		Parent:       makePubKey(0x99),
		Index:        3,
		Keywords:     "test,image,png",
		Description:  "A test image",
		Encrypted:    true,
		OnChain:      true,
		ContentTxIDs: [][]byte{makeTxID(0xAA)},
		Compression:  2,
		CltvHeight:   100000,
		RevenueShare: 25,
		NetworkName:  "mainnet",
		Metadata:     make(map[string]string),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	decoded := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, decoded)
	require.NoError(t, err)

	assert.Equal(t, "image/png", decoded.MimeType)
	assert.Equal(t, uint64(1048576), decoded.FileSize)
	assert.Equal(t, AccessFree, decoded.Access)
	assert.Equal(t, "test,image,png", decoded.Keywords)
	assert.Equal(t, uint32(100000), decoded.CltvHeight)
	assert.Equal(t, uint32(25), decoded.RevenueShare)
	assert.Equal(t, "mainnet", decoded.NetworkName)
}

// --- ParseNode tests (using tx.BuildOPReturnData + ParseNode) ---

func TestParseNode_ValidFile(t *testing.T) {
	pNode := makePubKey(0x01)
	parentTxID := makeTxID(0x02)

	node := &Node{
		Version:  1,
		Type:     NodeTypeFile,
		Op:       OpCreate,
		MimeType: "text/plain",
		FileSize: 256,
		KeyHash:  bytes.Repeat([]byte{0xDD}, 32),
		Metadata: make(map[string]string),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	// Use tx package to build OP_RETURN data
	// We need an ec.PublicKey - since we can't easily construct one from raw bytes
	// in tests, we'll construct the pushes manually matching the format
	pushes := [][]byte{
		tx.MetaFlagBytes,
		pNode,
		parentTxID,
		payload,
	}

	parsed, err := ParseNode(pushes)
	require.NoError(t, err)
	assert.NotNil(t, parsed)
	assert.Equal(t, pNode, parsed.PNode)
	assert.Equal(t, parentTxID, parsed.ParentTxID)
	assert.Equal(t, NodeTypeFile, parsed.Type)
	assert.Equal(t, "text/plain", parsed.MimeType)
	assert.Equal(t, uint64(256), parsed.FileSize)
}

func TestParseNode_ValidDir(t *testing.T) {
	pNode := makePubKey(0x01)

	node := &Node{
		Version: 1,
		Type:    NodeTypeDir,
		Op:      OpCreate,
		Children: []ChildEntry{
			{Index: 0, Name: "file.txt", Type: NodeTypeFile, PubKey: makePubKey(0x10)},
		},
		NextChildIndex: 1,
		Metadata:       make(map[string]string),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	pushes := [][]byte{tx.MetaFlagBytes, pNode, nil, payload}

	parsed, err := ParseNode(pushes)
	require.NoError(t, err)
	assert.Equal(t, NodeTypeDir, parsed.Type)
	assert.Len(t, parsed.Children, 1)
	assert.Equal(t, "file.txt", parsed.Children[0].Name)
}

func TestParseNode_ValidLink(t *testing.T) {
	pNode := makePubKey(0x01)
	target := makePubKey(0x42)

	node := &Node{
		Version:    1,
		Type:       NodeTypeLink,
		LinkTarget: target,
		LinkType:   LinkTypeSoft,
		Metadata:   make(map[string]string),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	pushes := [][]byte{tx.MetaFlagBytes, pNode, nil, payload}
	parsed, err := ParseNode(pushes)
	require.NoError(t, err)
	assert.Equal(t, NodeTypeLink, parsed.Type)
	assert.Equal(t, target, parsed.LinkTarget)
}

func TestParseNode_InvalidPushes(t *testing.T) {
	_, err := ParseNode([][]byte{{0x01}})
	assert.Error(t, err)
}

func TestParseNode_WrongMetaFlag(t *testing.T) {
	pushes := [][]byte{
		{0xFF, 0xFF, 0xFF, 0xFF},
		makePubKey(0x01),
		makeTxID(0x02),
		[]byte("payload"),
	}
	_, err := ParseNode(pushes)
	assert.Error(t, err)
}

func TestParseNodeFromPushesWithTxID(t *testing.T) {
	pNode := makePubKey(0x01)
	txID := makeTxID(0xAB)

	node := &Node{
		Version:  1,
		Type:     NodeTypeFile,
		Metadata: make(map[string]string),
	}
	payload, err := SerializePayload(node)
	require.NoError(t, err)

	pushes := [][]byte{tx.MetaFlagBytes, pNode, nil, payload}
	parsed, err := ParseNodeFromPushesWithTxID(pushes, txID)
	require.NoError(t, err)
	assert.Equal(t, txID, parsed.TxID)
}

// --- Directory operations tests ---

func TestListDirectory(t *testing.T) {
	root := makeRootDir(makePubKey(0x01))
	root.Children = []ChildEntry{
		{Index: 0, Name: "a.txt", Type: NodeTypeFile, PubKey: makePubKey(0x10)},
		{Index: 1, Name: "b.txt", Type: NodeTypeFile, PubKey: makePubKey(0x11)},
	}

	entries, err := ListDirectory(root)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
	assert.Equal(t, "a.txt", entries[0].Name)
	assert.Equal(t, "b.txt", entries[1].Name)
}

func TestListDirectory_NotDir(t *testing.T) {
	file := &Node{Type: NodeTypeFile}
	_, err := ListDirectory(file)
	assert.ErrorIs(t, err, ErrNotDirectory)
}

func TestListDirectory_Nil(t *testing.T) {
	_, err := ListDirectory(nil)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestListDirectory_Empty(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	entries, err := ListDirectory(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestListDirectory_ReturnsCopy(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	dir.Children = []ChildEntry{
		{Index: 0, Name: "file.txt", Type: NodeTypeFile, PubKey: makePubKey(0x10)},
	}

	entries, err := ListDirectory(dir)
	require.NoError(t, err)
	entries[0].Name = "modified"
	assert.Equal(t, "file.txt", dir.Children[0].Name, "original should not be modified")
}

func TestFindChild(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	dir.Children = []ChildEntry{
		{Index: 0, Name: "readme.txt", Type: NodeTypeFile, PubKey: makePubKey(0x10)},
		{Index: 1, Name: "docs", Type: NodeTypeDir, PubKey: makePubKey(0x20)},
	}

	entry, found := FindChild(dir, "readme.txt")
	assert.True(t, found)
	assert.Equal(t, "readme.txt", entry.Name)
	assert.Equal(t, NodeTypeFile, entry.Type)

	entry, found = FindChild(dir, "docs")
	assert.True(t, found)
	assert.Equal(t, NodeTypeDir, entry.Type)
}

func TestFindChild_NotFound(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	_, found := FindChild(dir, "nonexistent")
	assert.False(t, found)
}

func TestFindChild_NilNode(t *testing.T) {
	_, found := FindChild(nil, "test")
	assert.False(t, found)
}

func TestFindChild_NotDir(t *testing.T) {
	file := &Node{Type: NodeTypeFile}
	_, found := FindChild(file, "test")
	assert.False(t, found)
}

func TestAddChild(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))

	entry, err := AddChild(dir, "file.txt", NodeTypeFile, makePubKey(0x10), false)
	require.NoError(t, err)
	assert.Equal(t, "file.txt", entry.Name)
	assert.Equal(t, uint32(0), entry.Index)
	assert.Equal(t, NodeTypeFile, entry.Type)
	assert.False(t, entry.Hardened)
	assert.Len(t, dir.Children, 1)
	assert.Equal(t, uint32(1), dir.NextChildIndex)
}

func TestAddChild_MultipleChildren(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))

	_, err := AddChild(dir, "a.txt", NodeTypeFile, makePubKey(0x10), false)
	require.NoError(t, err)
	_, err = AddChild(dir, "b.txt", NodeTypeFile, makePubKey(0x11), false)
	require.NoError(t, err)
	_, err = AddChild(dir, "subdir", NodeTypeDir, makePubKey(0x20), true)
	require.NoError(t, err)

	assert.Len(t, dir.Children, 3)
	assert.Equal(t, uint32(0), dir.Children[0].Index)
	assert.Equal(t, uint32(1), dir.Children[1].Index)
	assert.Equal(t, uint32(2), dir.Children[2].Index)
	assert.Equal(t, uint32(3), dir.NextChildIndex)
}

func TestAddChild_DuplicateName(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))

	_, err := AddChild(dir, "file.txt", NodeTypeFile, makePubKey(0x10), false)
	require.NoError(t, err)

	_, err = AddChild(dir, "file.txt", NodeTypeFile, makePubKey(0x11), false)
	assert.ErrorIs(t, err, ErrChildExists)
}

func TestAddChild_InvalidName(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))

	tests := []struct {
		name string
		err  error
	}{
		{"", ErrInvalidName},
		{"/", ErrInvalidName},
		{".", ErrInvalidName},
		{"..", ErrInvalidName},
		{"path/to/file", ErrInvalidName},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("name=%q", tt.name), func(t *testing.T) {
			_, err := AddChild(dir, tt.name, NodeTypeFile, makePubKey(0x10), false)
			assert.ErrorIs(t, err, tt.err)
		})
	}
}

func TestAddChild_InvalidPubKey(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	_, err := AddChild(dir, "file.txt", NodeTypeFile, []byte{0x01}, false)
	assert.ErrorIs(t, err, ErrInvalidPubKey)
}

func TestAddChild_NotDir(t *testing.T) {
	file := &Node{Type: NodeTypeFile}
	_, err := AddChild(file, "child", NodeTypeFile, makePubKey(0x10), false)
	assert.ErrorIs(t, err, ErrNotDirectory)
}

func TestAddChild_NilDir(t *testing.T) {
	_, err := AddChild(nil, "child", NodeTypeFile, makePubKey(0x10), false)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestAddChild_Hardened(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	entry, err := AddChild(dir, "secret.txt", NodeTypeFile, makePubKey(0x10), true)
	require.NoError(t, err)
	assert.True(t, entry.Hardened)
}

func TestRemoveChild(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	_, err := AddChild(dir, "file.txt", NodeTypeFile, makePubKey(0x10), false)
	require.NoError(t, err)

	err = RemoveChild(dir, "file.txt")
	require.NoError(t, err)
	assert.Empty(t, dir.Children)
	// NextChildIndex should NOT decrease
	assert.Equal(t, uint32(1), dir.NextChildIndex)
}

func TestRemoveChild_NotFound(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	err := RemoveChild(dir, "nonexistent")
	assert.ErrorIs(t, err, ErrChildNotFound)
}

func TestRemoveChild_NotDir(t *testing.T) {
	file := &Node{Type: NodeTypeFile}
	err := RemoveChild(file, "child")
	assert.ErrorIs(t, err, ErrNotDirectory)
}

func TestRemoveChild_NilDir(t *testing.T) {
	err := RemoveChild(nil, "child")
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestRemoveChild_IndexNotReused(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	_, err := AddChild(dir, "a.txt", NodeTypeFile, makePubKey(0x10), false)
	require.NoError(t, err)
	_, err = AddChild(dir, "b.txt", NodeTypeFile, makePubKey(0x11), false)
	require.NoError(t, err)

	err = RemoveChild(dir, "a.txt")
	require.NoError(t, err)
	assert.Equal(t, uint32(2), dir.NextChildIndex, "index should not be reused")

	entry, err := AddChild(dir, "c.txt", NodeTypeFile, makePubKey(0x12), false)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), entry.Index, "new child gets next available index")
}

func TestRenameChild(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	_, err := AddChild(dir, "old.txt", NodeTypeFile, makePubKey(0x10), false)
	require.NoError(t, err)

	err = RenameChild(dir, "old.txt", "new.txt")
	require.NoError(t, err)

	_, found := FindChild(dir, "old.txt")
	assert.False(t, found)

	entry, found := FindChild(dir, "new.txt")
	assert.True(t, found)
	assert.Equal(t, "new.txt", entry.Name)
}

func TestRenameChild_TargetExists(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	_, err := AddChild(dir, "a.txt", NodeTypeFile, makePubKey(0x10), false)
	require.NoError(t, err)
	_, err = AddChild(dir, "b.txt", NodeTypeFile, makePubKey(0x11), false)
	require.NoError(t, err)

	err = RenameChild(dir, "a.txt", "b.txt")
	assert.ErrorIs(t, err, ErrChildExists)
}

func TestRenameChild_SourceNotFound(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	err := RenameChild(dir, "nonexistent", "new.txt")
	assert.ErrorIs(t, err, ErrChildNotFound)
}

func TestRenameChild_InvalidNewName(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	_, err := AddChild(dir, "file.txt", NodeTypeFile, makePubKey(0x10), false)
	require.NoError(t, err)

	err = RenameChild(dir, "file.txt", "")
	assert.ErrorIs(t, err, ErrInvalidName)

	err = RenameChild(dir, "file.txt", "path/name")
	assert.ErrorIs(t, err, ErrInvalidName)
}

func TestRenameChild_NotDir(t *testing.T) {
	file := &Node{Type: NodeTypeFile}
	err := RenameChild(file, "a", "b")
	assert.ErrorIs(t, err, ErrNotDirectory)
}

func TestNextChildIndex_Normal(t *testing.T) {
	dir := makeRootDir(makePubKey(0x01))
	dir.NextChildIndex = 5

	idx, err := NextChildIndex(dir)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), idx)
}

func TestNextChildIndex_NotDir(t *testing.T) {
	file := &Node{Type: NodeTypeFile}
	_, err := NextChildIndex(file)
	assert.ErrorIs(t, err, ErrNotDirectory)
}

// --- FollowLink tests ---

func TestFollowLink_SoftLink(t *testing.T) {
	store := newMockStore()

	targetPK := makePubKey(0x42)
	target := makeFileNode(targetPK, makePubKey(0x01), makeTxID(0x55))
	store.addNode(target)

	link := makeLinkNode(makePubKey(0x30), targetPK, LinkTypeSoft)

	resolved, err := FollowLink(store, link, MaxLinkDepth)
	require.NoError(t, err)
	assert.Equal(t, NodeTypeFile, resolved.Type)
	assert.Equal(t, targetPK, resolved.PNode)
}

func TestFollowLink_ChainedLinks(t *testing.T) {
	store := newMockStore()

	// link1 -> link2 -> file
	filePK := makePubKey(0x42)
	file := makeFileNode(filePK, makePubKey(0x01), makeTxID(0x55))
	store.addNode(file)

	link2PK := makePubKey(0x30)
	link2 := makeLinkNode(link2PK, filePK, LinkTypeSoft)
	store.addNode(link2)

	link1 := makeLinkNode(makePubKey(0x20), link2PK, LinkTypeSoft)

	resolved, err := FollowLink(store, link1, MaxLinkDepth)
	require.NoError(t, err)
	assert.Equal(t, NodeTypeFile, resolved.Type)
}

func TestFollowLink_DepthExceeded(t *testing.T) {
	store := newMockStore()

	// Create a chain of 12 links, each pointing to the next
	var links []*Node
	for i := 0; i < 12; i++ {
		pk := makePubKey(byte(i + 1))
		var target []byte
		if i < 11 {
			target = makePubKey(byte(i + 2))
		} else {
			target = makePubKey(0xFF)
		}
		link := makeLinkNode(pk, target, LinkTypeSoft)
		links = append(links, link)
		store.addNode(link)
	}

	_, err := FollowLink(store, links[0], MaxLinkDepth)
	assert.ErrorIs(t, err, ErrLinkDepthExceeded)
}

func TestFollowLink_RemoteLink(t *testing.T) {
	store := newMockStore()
	link := &Node{
		Type:     NodeTypeLink,
		LinkType: LinkTypeSoftRemote,
		Domain:   "example.com/path",
		Metadata: make(map[string]string),
	}

	_, err := FollowLink(store, link, MaxLinkDepth)
	assert.ErrorIs(t, err, ErrRemoteLinkNotSupported)
}

func TestFollowLink_NotLink(t *testing.T) {
	store := newMockStore()
	file := &Node{Type: NodeTypeFile, Metadata: make(map[string]string)}

	_, err := FollowLink(store, file, MaxLinkDepth)
	assert.ErrorIs(t, err, ErrNotLink)
}

func TestFollowLink_NilStore(t *testing.T) {
	link := makeLinkNode(makePubKey(0x01), makePubKey(0x02), LinkTypeSoft)
	_, err := FollowLink(nil, link, MaxLinkDepth)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestFollowLink_NilNode(t *testing.T) {
	store := newMockStore()
	_, err := FollowLink(store, nil, MaxLinkDepth)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestFollowLink_TargetNotFound(t *testing.T) {
	store := newMockStore()
	link := makeLinkNode(makePubKey(0x01), makePubKey(0xFF), LinkTypeSoft)

	_, err := FollowLink(store, link, MaxLinkDepth)
	assert.ErrorIs(t, err, ErrNodeNotFound)
}

func TestFollowLink_EmptyTarget(t *testing.T) {
	store := newMockStore()
	link := &Node{
		Type:       NodeTypeLink,
		LinkTarget: nil,
		Metadata:   make(map[string]string),
	}

	_, err := FollowLink(store, link, MaxLinkDepth)
	assert.ErrorIs(t, err, ErrNodeNotFound)
}

// --- LatestVersion tests ---

func TestLatestVersion_SingleNode(t *testing.T) {
	node := &Node{BlockHeight: 100, TxID: makeTxID(0x01)}
	result := LatestVersion([]*Node{node})
	assert.Equal(t, node, result)
}

func TestLatestVersion_DifferentHeights(t *testing.T) {
	n1 := &Node{BlockHeight: 100, TxID: makeTxID(0x01)}
	n2 := &Node{BlockHeight: 200, TxID: makeTxID(0x02)}
	n3 := &Node{BlockHeight: 150, TxID: makeTxID(0x03)}

	result := LatestVersion([]*Node{n1, n2, n3})
	assert.Equal(t, n2, result, "highest block height should win")
}

func TestLatestVersion_SameHeight_DifferentTimestamp(t *testing.T) {
	n1 := &Node{BlockHeight: 100, Timestamp: 1000, TxID: makeTxID(0x01)}
	n2 := &Node{BlockHeight: 100, Timestamp: 2000, TxID: makeTxID(0x02)}

	result := LatestVersion([]*Node{n1, n2})
	assert.Equal(t, n2, result, "higher timestamp should win in same block")
}

func TestLatestVersion_SameHeightSameTimestamp_TxIDTiebreak(t *testing.T) {
	n1 := &Node{BlockHeight: 100, Timestamp: 1000, TxID: makeTxID(0x01)}
	n2 := &Node{BlockHeight: 100, Timestamp: 1000, TxID: makeTxID(0xFF)}

	result := LatestVersion([]*Node{n1, n2})
	assert.Equal(t, n2, result, "higher TxID should win as tiebreaker")
}

func TestLatestVersion_Empty(t *testing.T) {
	result := LatestVersion(nil)
	assert.Nil(t, result)
}

func TestLatestVersion_WithNils(t *testing.T) {
	n1 := &Node{BlockHeight: 100, TxID: makeTxID(0x01)}
	result := LatestVersion([]*Node{nil, n1, nil})
	assert.Equal(t, n1, result)
}

// --- InheritPricePerKB tests ---

func TestInheritPricePerKB_NodeHasPrice(t *testing.T) {
	store := newMockStore()
	node := &Node{PricePerKB: 100, Metadata: make(map[string]string)}

	price, err := InheritPricePerKB(store, node)
	require.NoError(t, err)
	assert.Equal(t, uint64(100), price)
}

func TestInheritPricePerKB_InheritFromParent(t *testing.T) {
	store := newMockStore()

	parentPK := makePubKey(0x01)
	parent := &Node{
		PNode:      parentPK,
		PricePerKB: 50,
		Metadata:   make(map[string]string),
	}
	store.addNode(parent)

	child := &Node{
		Parent:     parentPK,
		PricePerKB: 0,
		Metadata:   make(map[string]string),
	}

	price, err := InheritPricePerKB(store, child)
	require.NoError(t, err)
	assert.Equal(t, uint64(50), price)
}

func TestInheritPricePerKB_InheritFromGrandparent(t *testing.T) {
	store := newMockStore()

	grandparentPK := makePubKey(0x01)
	grandparent := &Node{
		PNode:      grandparentPK,
		PricePerKB: 200,
		Metadata:   make(map[string]string),
	}
	store.addNode(grandparent)

	parentPK := makePubKey(0x02)
	parent := &Node{
		PNode:      parentPK,
		Parent:     grandparentPK,
		PricePerKB: 0,
		Metadata:   make(map[string]string),
	}
	store.addNode(parent)

	child := &Node{
		Parent:     parentPK,
		PricePerKB: 0,
		Metadata:   make(map[string]string),
	}

	price, err := InheritPricePerKB(store, child)
	require.NoError(t, err)
	assert.Equal(t, uint64(200), price)
}

func TestInheritPricePerKB_NoPriceAnywhere(t *testing.T) {
	store := newMockStore()

	rootPK := makePubKey(0x01)
	root := &Node{
		PNode:      rootPK,
		PricePerKB: 0,
		Metadata:   make(map[string]string),
	}
	store.addNode(root)

	child := &Node{
		Parent:     rootPK,
		PricePerKB: 0,
		Metadata:   make(map[string]string),
	}

	price, err := InheritPricePerKB(store, child)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), price)
}

func TestInheritPricePerKB_NilNode(t *testing.T) {
	store := newMockStore()
	_, err := InheritPricePerKB(store, nil)
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestInheritPricePerKB_NilStore(t *testing.T) {
	node := &Node{Metadata: make(map[string]string)}
	_, err := InheritPricePerKB(nil, node)
	assert.ErrorIs(t, err, ErrNilParam)
}

// --- ResolvePath tests ---

func TestResolvePath_EmptyPath(t *testing.T) {
	store := newMockStore()
	root := makeRootDir(makePubKey(0x01))

	result, err := ResolvePath(store, root, []string{})
	require.NoError(t, err)
	assert.Equal(t, root, result.Node)
	assert.Empty(t, result.Path)
}

func TestResolvePath_SingleComponent(t *testing.T) {
	store := newMockStore()

	childPK := makePubKey(0x10)
	child := makeFileNode(childPK, makePubKey(0x01), makeTxID(0x55))
	store.addNode(child)

	root := makeRootDir(makePubKey(0x01))
	root.Children = []ChildEntry{
		{Index: 0, Name: "readme.txt", Type: NodeTypeFile, PubKey: childPK},
	}

	result, err := ResolvePath(store, root, []string{"readme.txt"})
	require.NoError(t, err)
	assert.Equal(t, child, result.Node)
	assert.Equal(t, "readme.txt", result.Entry.Name)
	assert.Equal(t, root, result.Parent)
	assert.Equal(t, []string{"readme.txt"}, result.Path)
}

func TestResolvePath_NestedPath(t *testing.T) {
	store := newMockStore()

	// root/docs/report.pdf
	rootPK := makePubKey(0x01)
	docsPK := makePubKey(0x20)
	filePK := makePubKey(0x30)

	docsNode := makeDirNode(docsPK, rootPK, makeTxID(0x22))
	docsNode.Children = []ChildEntry{
		{Index: 0, Name: "report.pdf", Type: NodeTypeFile, PubKey: filePK},
	}
	store.addNode(docsNode)

	fileNode := makeFileNode(filePK, docsPK, makeTxID(0x33))
	store.addNode(fileNode)

	root := makeRootDir(rootPK)
	root.Children = []ChildEntry{
		{Index: 0, Name: "docs", Type: NodeTypeDir, PubKey: docsPK},
	}

	result, err := ResolvePath(store, root, []string{"docs", "report.pdf"})
	require.NoError(t, err)
	assert.Equal(t, fileNode, result.Node)
	assert.Equal(t, []string{"docs", "report.pdf"}, result.Path)
}

func TestResolvePath_DotNavigation(t *testing.T) {
	store := newMockStore()

	childPK := makePubKey(0x10)
	child := makeFileNode(childPK, makePubKey(0x01), makeTxID(0x55))
	store.addNode(child)

	root := makeRootDir(makePubKey(0x01))
	root.Children = []ChildEntry{
		{Index: 0, Name: "file.txt", Type: NodeTypeFile, PubKey: childPK},
	}

	// "." should be a no-op
	result, err := ResolvePath(store, root, []string{".", "file.txt"})
	require.NoError(t, err)
	assert.Equal(t, child, result.Node)
}

func TestResolvePath_DotDotNavigation(t *testing.T) {
	store := newMockStore()

	rootPK := makePubKey(0x01)
	subPK := makePubKey(0x10)
	filePK := makePubKey(0x20)

	subDir := makeDirNode(subPK, rootPK, makeTxID(0x11))
	store.addNode(subDir)

	file := makeFileNode(filePK, rootPK, makeTxID(0x22))
	store.addNode(file)

	root := makeRootDir(rootPK)
	root.Children = []ChildEntry{
		{Index: 0, Name: "sub", Type: NodeTypeDir, PubKey: subPK},
		{Index: 1, Name: "file.txt", Type: NodeTypeFile, PubKey: filePK},
	}

	// sub/../file.txt should resolve to file.txt
	result, err := ResolvePath(store, root, []string{"sub", "..", "file.txt"})
	require.NoError(t, err)
	assert.Equal(t, file, result.Node)
}

func TestResolvePath_DotDotAtRoot(t *testing.T) {
	store := newMockStore()
	root := makeRootDir(makePubKey(0x01))

	// ".." at root should stay at root
	result, err := ResolvePath(store, root, []string{"..", ".."})
	require.NoError(t, err)
	assert.Equal(t, root, result.Node)
}

func TestResolvePath_FollowsSoftLink(t *testing.T) {
	store := newMockStore()

	rootPK := makePubKey(0x01)
	targetPK := makePubKey(0x42)
	linkPK := makePubKey(0x30)

	target := makeFileNode(targetPK, rootPK, makeTxID(0x55))
	store.addNode(target)

	link := makeLinkNode(linkPK, targetPK, LinkTypeSoft)
	store.addNode(link)

	root := makeRootDir(rootPK)
	root.Children = []ChildEntry{
		{Index: 0, Name: "shortcut", Type: NodeTypeLink, PubKey: linkPK},
	}

	result, err := ResolvePath(store, root, []string{"shortcut"})
	require.NoError(t, err)
	assert.Equal(t, NodeTypeFile, result.Node.Type)
	assert.Equal(t, targetPK, result.Node.PNode)
}

func TestResolvePath_ChildNotFound(t *testing.T) {
	store := newMockStore()
	root := makeRootDir(makePubKey(0x01))

	_, err := ResolvePath(store, root, []string{"nonexistent"})
	assert.ErrorIs(t, err, ErrChildNotFound)
}

func TestResolvePath_NotDirectory(t *testing.T) {
	store := newMockStore()

	filePK := makePubKey(0x10)
	file := makeFileNode(filePK, makePubKey(0x01), makeTxID(0x55))
	store.addNode(file)

	root := makeRootDir(makePubKey(0x01))
	root.Children = []ChildEntry{
		{Index: 0, Name: "file.txt", Type: NodeTypeFile, PubKey: filePK},
	}

	// Trying to traverse into a file
	_, err := ResolvePath(store, root, []string{"file.txt", "child"})
	assert.ErrorIs(t, err, ErrNotDirectory)
}

func TestResolvePath_NilStore(t *testing.T) {
	root := makeRootDir(makePubKey(0x01))
	_, err := ResolvePath(nil, root, []string{"test"})
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestResolvePath_NilRoot(t *testing.T) {
	store := newMockStore()
	_, err := ResolvePath(store, nil, []string{"test"})
	assert.ErrorIs(t, err, ErrNilParam)
}

func TestResolvePath_EmptyComponent(t *testing.T) {
	store := newMockStore()
	root := makeRootDir(makePubKey(0x01))

	_, err := ResolvePath(store, root, []string{""})
	assert.ErrorIs(t, err, ErrInvalidPath)
}

// --- SplitPath tests ---

func TestSplitPath(t *testing.T) {
	tests := []struct {
		path     string
		expected []string
		err      error
	}{
		{"/", []string{}, nil},
		{"/docs", []string{"docs"}, nil},
		{"/docs/report.pdf", []string{"docs", "report.pdf"}, nil},
		{"docs/report.pdf", []string{"docs", "report.pdf"}, nil},
		{"/a/b/c/d", []string{"a", "b", "c", "d"}, nil},
		{"/a//b/c", []string{"a", "b", "c"}, nil}, // consecutive slashes
		{"/trailing/", []string{"trailing"}, nil},
		{"", nil, ErrInvalidPath},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			parts, err := SplitPath(tt.path)
			if tt.err != nil {
				assert.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, parts)
			}
		})
	}
}

// --- validateChildName tests ---

func TestValidateChildName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"file.txt", false},
		{"My Document.pdf", false},
		{"a", false},
		{"file-name_v2.tar.gz", false},
		{"", true},
		{"/", true},
		{".", true},
		{"..", true},
		{"path/name", true},
		{string([]byte{0x00}), true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("name=%q", tt.name), func(t *testing.T) {
			err := validateChildName(tt.name)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- compareTxIDs tests ---

func TestCompareTxIDs(t *testing.T) {
	tests := []struct {
		a, b     []byte
		expected int
	}{
		{makeTxID(0x01), makeTxID(0x02), -1},
		{makeTxID(0x02), makeTxID(0x01), 1},
		{makeTxID(0x01), makeTxID(0x01), 0},
		{nil, nil, 0},
		{[]byte{0x01}, []byte{0x01, 0x02}, -1},
	}

	for _, tt := range tests {
		result := compareTxIDs(tt.a, tt.b)
		assert.Equal(t, tt.expected, result)
	}
}

// --- DeserializePayload edge cases ---

func TestDeserializePayload_Empty(t *testing.T) {
	node := &Node{Metadata: make(map[string]string)}
	err := deserializePayload([]byte{}, node)
	assert.NoError(t, err)
}

func TestDeserializePayload_UnknownTag(t *testing.T) {
	// Build a payload with unknown tag 0xFF
	var buf []byte
	buf = append(buf, 0xFF, 2, 0, 0x01, 0x02) // tag=0xFF, len=2, data=0x01,0x02
	node := &Node{Metadata: make(map[string]string)}
	err := deserializePayload(buf, node)
	assert.NoError(t, err, "unknown tags should be skipped")
}

func TestDeserializePayload_Truncated(t *testing.T) {
	// Tag present but value truncated
	buf := []byte{tagVersion, 4, 0, 0x01} // says length 4 but only 1 byte of data
	node := &Node{Metadata: make(map[string]string)}
	err := deserializePayload(buf, node)
	assert.Error(t, err)
}

// --- Integration-style test: full workflow ---

func TestFullWorkflow_CreateAndResolve(t *testing.T) {
	store := newMockStore()

	// Create root directory
	rootPK := makePubKey(0x01)
	root := makeRootDir(rootPK)
	root.TxID = makeTxID(0x01)
	store.addNode(root)

	// Add "docs" subdirectory
	docsPK := makePubKey(0x10)
	docsDir := makeDirNode(docsPK, rootPK, makeTxID(0x10))
	store.addNode(docsDir)
	_, err := AddChild(root, "docs", NodeTypeDir, docsPK, false)
	require.NoError(t, err)

	// Add "readme.txt" to docs
	readmePK := makePubKey(0x20)
	readme := makeFileNode(readmePK, docsPK, makeTxID(0x20))
	readme.MimeType = "text/plain"
	readme.FileSize = 512
	store.addNode(readme)
	_, err = AddChild(docsDir, "readme.txt", NodeTypeFile, readmePK, false)
	require.NoError(t, err)

	// Add "images" subdirectory to docs
	imagesPK := makePubKey(0x30)
	imagesDir := makeDirNode(imagesPK, docsPK, makeTxID(0x30))
	store.addNode(imagesDir)
	_, err = AddChild(docsDir, "images", NodeTypeDir, imagesPK, false)
	require.NoError(t, err)

	// Add "logo.png" to images
	logoPK := makePubKey(0x40)
	logo := makeFileNode(logoPK, imagesPK, makeTxID(0x40))
	logo.MimeType = "image/png"
	store.addNode(logo)
	_, err = AddChild(imagesDir, "logo.png", NodeTypeFile, logoPK, false)
	require.NoError(t, err)

	// Create a soft link in root -> docs/readme.txt target
	linkPK := makePubKey(0x50)
	link := makeLinkNode(linkPK, readmePK, LinkTypeSoft)
	store.addNode(link)
	_, err = AddChild(root, "quick-readme", NodeTypeLink, linkPK, false)
	require.NoError(t, err)

	// Test: resolve docs/readme.txt
	result, err := ResolvePath(store, root, []string{"docs", "readme.txt"})
	require.NoError(t, err)
	assert.Equal(t, "text/plain", result.Node.MimeType)

	// Test: resolve docs/images/logo.png
	result, err = ResolvePath(store, root, []string{"docs", "images", "logo.png"})
	require.NoError(t, err)
	assert.Equal(t, "image/png", result.Node.MimeType)

	// Test: resolve via link
	result, err = ResolvePath(store, root, []string{"quick-readme"})
	require.NoError(t, err)
	assert.Equal(t, NodeTypeFile, result.Node.Type)
	assert.Equal(t, "text/plain", result.Node.MimeType)

	// Test: navigate up with ..
	result, err = ResolvePath(store, root, []string{"docs", "images", "..", "readme.txt"})
	require.NoError(t, err)
	assert.Equal(t, "text/plain", result.Node.MimeType)

	// Test: list docs directory
	entries, err := ListDirectory(docsDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	// Test: remove a child
	err = RemoveChild(docsDir, "readme.txt")
	require.NoError(t, err)
	entries, err = ListDirectory(docsDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "images", entries[0].Name)

	// Test: NextChildIndex not reused
	assert.Equal(t, uint32(2), docsDir.NextChildIndex)
}

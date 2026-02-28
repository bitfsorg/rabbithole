//go:build integration

package integration

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// --- Test 1: TestClientGetMeta ---

// TestClientGetMeta creates a file via engine, uses Client.GetMeta to retrieve
// metadata, and verifies type/access/filesize are correct.
func TestClientGetMeta(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	// Create a file via engine.
	plaintext := []byte("client roundtrip metadata test content")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/meta-test.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile /meta-test.txt")

	// Get the file node's pnode (its own PubKeyHex).
	nodeState := eng.State.FindNodeByPath("/meta-test.txt")
	require.NotNil(t, nodeState, "node should exist in local state")
	pnode := nodeState.PubKeyHex

	// Create HTTP client and query metadata.
	c := client.New(ts.URL)
	meta, err := c.GetMeta(pnode, "meta-test.txt")
	require.NoError(t, err, "GetMeta should succeed")

	// Verify metadata fields.
	assert.Equal(t, "file", meta.Type, "Type should be 'file'")
	assert.Equal(t, "free", meta.Access, "Access should be 'free'")
	assert.Equal(t, uint64(len(plaintext)), meta.FileSize, "FileSize should match plaintext length")
	assert.Equal(t, pnode, meta.PNode, "PNode should match the node's pubkey")
	assert.NotEmpty(t, meta.KeyHash, "KeyHash should be present for files")
}

// --- Test 2: TestClientGetMetaNotFound ---

// TestClientGetMetaNotFound queries a nonexistent path and verifies the Client
// returns ErrNotFound.
func TestClientGetMetaNotFound(t *testing.T) {
	_, ts := createDaemonWithEngine(t)

	// Get the root node's pnode so we have a valid pnode format.
	// Use a valid 66-char hex pnode that doesn't match any node.
	// We'll use the root's pnode but query a path that doesn't exist.
	c := client.New(ts.URL)

	// Use a syntactically valid pnode (66 hex chars = 33 bytes compressed pubkey).
	fakePnode := "02" + "0000000000000000000000000000000000000000000000000000000000000001"

	_, err := c.GetMeta(fakePnode, "nonexistent/path/file.txt")
	require.Error(t, err, "GetMeta should fail for nonexistent path")
	assert.True(t, errors.Is(err, client.ErrNotFound),
		"error should wrap ErrNotFound, got: %v", err)
}

// --- Test 3: TestClientGetDirectoryListing ---

// TestClientGetDirectoryListing creates a directory with a file inside, then
// uses Client.GetMeta on "/" to verify children are returned.
func TestClientGetDirectoryListing(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	// Create a file in the root directory.
	plaintext := []byte("file inside root for directory listing test")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/listing.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile /listing.txt")

	// Get root node's pnode.
	rootState := eng.State.FindNodeByPath("/")
	require.NotNil(t, rootState, "root node should exist")
	rootPnode := rootState.PubKeyHex

	// Query root directory via Client.
	c := client.New(ts.URL)
	meta, err := c.GetMeta(rootPnode, "/")
	require.NoError(t, err, "GetMeta / should succeed")

	// Verify directory metadata.
	assert.Equal(t, "dir", meta.Type, "Type should be 'dir'")
	assert.Equal(t, rootPnode, meta.PNode, "PNode should match root's pubkey")

	// Verify children include our file.
	require.NotEmpty(t, meta.Children, "root directory should have children")
	found := false
	for _, child := range meta.Children {
		if child.Name == "listing.txt" {
			found = true
			assert.Equal(t, "file", child.Type, "child type should be 'file'")
		}
	}
	assert.True(t, found, "listing.txt should be in root children")
}

// --- Test 4: TestClientGetData ---

// TestClientGetData creates a file, uses Client.GetData to retrieve the
// encrypted content by hash, and verifies the response is non-empty.
func TestClientGetData(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	// Create a file.
	plaintext := []byte("encrypted data retrieval test via client HTTP roundtrip")
	localFile := createTempFile(t, plaintext)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/data-test.txt",
		Access:     "free",
	})
	require.NoError(t, err, "PutFile /data-test.txt")

	// Get the KeyHash from the node state.
	nodeState := eng.State.FindNodeByPath("/data-test.txt")
	require.NotNil(t, nodeState, "node should exist in local state")
	require.NotEmpty(t, nodeState.KeyHash, "KeyHash should be set")

	// Retrieve encrypted data via Client.
	c := client.New(ts.URL)
	rc, err := c.GetData(nodeState.KeyHash)
	require.NoError(t, err, "GetData should succeed")
	defer rc.Close()

	// Read the response body.
	data, err := io.ReadAll(rc)
	require.NoError(t, err, "reading GetData response")

	// Verify non-empty (encrypted content should be larger than plaintext
	// due to AES-GCM overhead: nonce + tag).
	assert.NotEmpty(t, data, "encrypted data should not be empty")
	assert.Greater(t, len(data), len(plaintext),
		"encrypted data should be larger than plaintext (AES-GCM overhead)")
}

// --- Test 5: TestClientMultipleRequests ---

// TestClientMultipleRequests creates 3 files, queries each via Client.GetMeta,
// then queries the root directory to verify all children are listed.
func TestClientMultipleRequests(t *testing.T) {
	eng, ts := createDaemonWithEngine(t)

	files := []struct {
		name    string
		content string
	}{
		{name: "/alpha.txt", content: "alpha content for multi-request test"},
		{name: "/beta.txt", content: "beta content for multi-request test"},
		{name: "/gamma.txt", content: "gamma content for multi-request test"},
	}

	// Create all 3 files.
	for _, f := range files {
		localFile := createTempFile(t, []byte(f.content))
		_, err := eng.PutFile(&vault.PutOpts{
			VaultIndex: 0,
			LocalFile:  localFile,
			RemotePath: f.name,
			Access:     "free",
		})
		require.NoError(t, err, "PutFile %s", f.name)
	}

	c := client.New(ts.URL)

	// Query each file individually via GetMeta.
	for _, f := range files {
		nodeState := eng.State.FindNodeByPath(f.name)
		require.NotNil(t, nodeState, "node %s should exist", f.name)

		meta, err := c.GetMeta(nodeState.PubKeyHex, f.name[1:]) // strip leading "/"
		require.NoError(t, err, "GetMeta %s should succeed", f.name)

		assert.Equal(t, "file", meta.Type, "%s type should be 'file'", f.name)
		assert.Equal(t, "free", meta.Access, "%s access should be 'free'", f.name)
		assert.Equal(t, uint64(len(f.content)), meta.FileSize,
			"%s filesize should match content length", f.name)
	}

	// Query root directory to verify all 3 files are listed as children.
	rootState := eng.State.FindNodeByPath("/")
	require.NotNil(t, rootState, "root node should exist")

	rootMeta, err := c.GetMeta(rootState.PubKeyHex, "/")
	require.NoError(t, err, "GetMeta / should succeed")

	assert.Equal(t, "dir", rootMeta.Type, "root type should be 'dir'")

	// Collect child names.
	childNames := make(map[string]bool)
	for _, child := range rootMeta.Children {
		childNames[child.Name] = true
	}

	for _, f := range files {
		name := f.name[1:] // strip leading "/"
		assert.True(t, childNames[name],
			"root children should include %s, got: %v", name, rootMeta.Children)
	}
}

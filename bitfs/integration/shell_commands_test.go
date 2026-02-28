//go:build integration

package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/bitfs/internal/publish"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// =============================================================================
// Task 11: Navigation + File Operations
// =============================================================================

// --- Navigation Tests ---

// TestShell_MkdirAndLs creates root and two subdirectories, verifying the root
// directory has 2 children.
func TestShell_MkdirAndLs(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Create root.
	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Create nested dirs.
	_, err = eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/photos"})
	require.NoError(t, err)
	_, err = eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/docs"})
	require.NoError(t, err)

	// Verify root has 2 children.
	root := eng.State.FindNodeByPath("/")
	require.NotNil(t, root)
	assert.Len(t, root.Children, 2)

	// Verify child names.
	names := make([]string, len(root.Children))
	for i, c := range root.Children {
		names[i] = c.Name
	}
	assert.Contains(t, names, "photos")
	assert.Contains(t, names, "docs")
}

// TestShell_PutAndCat uploads a file and then cats it, verifying the decrypted
// content matches the original plaintext.
func TestShell_PutAndCat(t *testing.T) {
	eng := initIntegrationEngine(t)

	content := []byte("Hello BitFS from shell test")
	localFile := createTempFile(t, content)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/hello.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Verify node exists.
	node := eng.State.FindNodeByPath("/hello.txt")
	require.NotNil(t, node)
	assert.Equal(t, uint64(len(content)), node.FileSize)

	// Cat should decrypt and return content.
	reader, info, err := eng.Cat(&vault.CatOpts{Path: "/hello.txt"})
	require.NoError(t, err)
	require.NotNil(t, info)

	plaintext, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, plaintext)
}

// TestShell_PutWithAccessModes uploads files with different access modes (free
// and private) and verifies each node's access level.
func TestShell_PutWithAccessModes(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	tests := []struct {
		name   string
		path   string
		access string
	}{
		{"free", "/free.txt", "free"},
		{"private", "/private.txt", "private"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte("content for " + tt.name)
			localFile := createTempFile(t, content)
			_, err := eng.PutFile(&vault.PutOpts{
				VaultIndex: 0,
				LocalFile:  localFile,
				RemotePath: tt.path,
				Access:     tt.access,
			})
			require.NoError(t, err)

			node := eng.State.FindNodeByPath(tt.path)
			require.NotNil(t, node)
			assert.Equal(t, tt.access, node.Access)
		})
	}
}

// --- File Operation Tests ---

// TestShell_RmFile uploads a file, removes it via Remove, and verifies the
// parent directory no longer lists it as a child.
func TestShell_RmFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	content := []byte("to be deleted")
	localFile := createTempFile(t, content)
	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/temp.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Remove the file.
	_, err = eng.Remove(&vault.RemoveOpts{
		VaultIndex: 0,
		Path:       "/temp.txt",
	})
	require.NoError(t, err)

	// Verify parent directory no longer lists the child.
	root := eng.State.FindNodeByPath("/")
	require.NotNil(t, root)
	for _, c := range root.Children {
		assert.NotEqual(t, "temp.txt", c.Name, "temp.txt should be removed from parent children")
	}
}

// TestShell_RmRecursive creates a directory with a nested file, removes the
// file first, then the directory (simulating rm -r), verifying both are gone
// from the parent.
func TestShell_RmRecursive(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/dir"})
	require.NoError(t, err)

	content := []byte("nested file")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/dir/file.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Remove child first, then dir (simulating rm -r).
	_, err = eng.Remove(&vault.RemoveOpts{VaultIndex: 0, Path: "/dir/file.txt"})
	require.NoError(t, err)

	_, err = eng.Remove(&vault.RemoveOpts{VaultIndex: 0, Path: "/dir"})
	require.NoError(t, err)

	// Verify /dir is removed from root's children.
	root := eng.State.FindNodeByPath("/")
	require.NotNil(t, root)
	for _, c := range root.Children {
		assert.NotEqual(t, "dir", c.Name, "dir should be removed from root children")
	}
}

// TestShell_MvSameDir uploads a file, renames it within the same directory, and
// verifies the old path is gone and the new path exists.
func TestShell_MvSameDir(t *testing.T) {
	eng := initIntegrationEngine(t)

	content := []byte("to be moved")
	localFile := createTempFile(t, content)
	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/old.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.Move(&vault.MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/old.txt",
		DstPath:    "/new.txt",
	})
	require.NoError(t, err)

	assert.Nil(t, eng.State.FindNodeByPath("/old.txt"))
	assert.NotNil(t, eng.State.FindNodeByPath("/new.txt"))
}

// TestShell_MvCrossDir uploads a file in /src, moves it to /dst, and verifies
// the file is gone from /src and present in /dst.
func TestShell_MvCrossDir(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/src"})
	require.NoError(t, err)
	_, err = eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/dst"})
	require.NoError(t, err)

	content := []byte("cross dir move")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/src/file.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.Move(&vault.MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/src/file.txt",
		DstPath:    "/dst/file.txt",
	})
	require.NoError(t, err)

	// Source directory should have no children.
	srcDir := eng.State.FindNodeByPath("/src")
	require.NotNil(t, srcDir)
	assert.Empty(t, srcDir.Children)

	// Destination should have the file.
	assert.NotNil(t, eng.State.FindNodeByPath("/dst/file.txt"))
}

// TestShell_CpFile uploads a file and copies it, verifying both the original
// and copy exist.
func TestShell_CpFile(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	content := []byte("to be copied")
	localFile := createTempFile(t, content)
	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/orig.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.Copy(&vault.CopyOpts{
		VaultIndex: 0,
		SrcPath:    "/orig.txt",
		DstPath:    "/copy.txt",
	})
	require.NoError(t, err)

	// Both should exist.
	assert.NotNil(t, eng.State.FindNodeByPath("/orig.txt"))
	assert.NotNil(t, eng.State.FindNodeByPath("/copy.txt"))

	// Copy should have the same file size.
	origNode := eng.State.FindNodeByPath("/orig.txt")
	copyNode := eng.State.FindNodeByPath("/copy.txt")
	assert.Equal(t, origNode.FileSize, copyNode.FileSize)
}

// --- Link Tests ---

// TestShell_SoftLink creates a file, then creates a soft link pointing to it,
// and verifies the link node exists with the correct type and target.
func TestShell_SoftLink(t *testing.T) {
	eng := initIntegrationEngine(t)

	content := []byte("link target")
	localFile := createTempFile(t, content)
	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/target.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	targetNode := eng.State.FindNodeByPath("/target.txt")
	require.NotNil(t, targetNode)

	_, err = eng.Link(&vault.LinkOpts{
		VaultIndex: 0,
		TargetPath: "/target.txt",
		LinkPath:   "/link.txt",
		Soft:       true,
	})
	require.NoError(t, err)

	link := eng.State.FindNodeByPath("/link.txt")
	require.NotNil(t, link)
	assert.Equal(t, "link", link.Type)
	assert.Equal(t, targetNode.PubKeyHex, link.LinkTarget)
}

// TestShell_HardLink creates a file, then creates a hard link pointing to it.
// Hard links add an entry in the parent directory pointing to the same pubkey,
// without creating a new NodeState. Verifies the parent's children list.
func TestShell_HardLink(t *testing.T) {
	eng := initIntegrationEngine(t)

	content := []byte("hard link target")
	localFile := createTempFile(t, content)
	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/target.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	targetNode := eng.State.FindNodeByPath("/target.txt")
	require.NotNil(t, targetNode)

	_, err = eng.Link(&vault.LinkOpts{
		VaultIndex: 0,
		TargetPath: "/target.txt",
		LinkPath:   "/hardlink.txt",
		Soft:       false,
	})
	require.NoError(t, err)

	// Hard link does not create a new NodeState; it adds a child entry
	// in the parent directory pointing to the same pubkey.
	root := eng.State.FindNodeByPath("/")
	require.NotNil(t, root)

	foundHardLink := false
	for _, c := range root.Children {
		if c.Name == "hardlink.txt" {
			foundHardLink = true
			assert.Equal(t, targetNode.PubKeyHex, c.PubKey,
				"hard link should point to same pubkey as target")
		}
	}
	assert.True(t, foundHardLink, "hardlink.txt should be in root children")
}

// =============================================================================
// Task 12: Access Control + Advanced Operations
// =============================================================================

// --- Access Control Tests ---

// TestShell_EncryptDecrypt uploads a free file, encrypts it (Free -> Private),
// verifies the access changed, then decrypts it (Private -> Free).
func TestShell_EncryptDecrypt(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	content := []byte("encrypt me")
	localFile := createTempFile(t, content)
	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/secret.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Encrypt: Free -> Private.
	_, err = eng.EncryptNode(&vault.EncryptOpts{
		VaultIndex: 0,
		Path:       "/secret.txt",
	})
	require.NoError(t, err)

	node := eng.State.FindNodeByPath("/secret.txt")
	require.NotNil(t, node)
	assert.Equal(t, "private", node.Access, "access should be private after encrypt")

	// Decrypt: Private -> Free.
	_, err = eng.DecryptNode(&vault.DecryptOpts{
		Path: "/secret.txt",
	})
	require.NoError(t, err)

	node = eng.State.FindNodeByPath("/secret.txt")
	require.NotNil(t, node)
	assert.Equal(t, "free", node.Access, "access should be free after decrypt")
}

// TestShell_SellFile uploads a free file, sells it with a price per KB, and
// verifies the price and access mode are updated.
func TestShell_SellFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	content := []byte("premium content")
	localFile := createTempFile(t, content)
	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/premium.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.Sell(&vault.SellOpts{
		VaultIndex: 0,
		Path:       "/premium.txt",
		PricePerKB: 50,
	})
	require.NoError(t, err)

	node := eng.State.FindNodeByPath("/premium.txt")
	require.NotNil(t, node)
	assert.Equal(t, "paid", node.Access)
	assert.Equal(t, uint64(50), node.PricePerKB)
}

// TestShell_SellRecursive creates a directory with two files, then sells each
// file (simulating --recursive), and verifies both have the expected price.
func TestShell_SellRecursive(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 40, 10_000)

	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/premium"})
	require.NoError(t, err)

	for _, name := range []string{"a.txt", "b.txt"} {
		content := []byte("content of " + name)
		localFile := createTempFile(t, content)
		_, err = eng.PutFile(&vault.PutOpts{
			VaultIndex: 0,
			LocalFile:  localFile,
			RemotePath: "/premium/" + name,
			Access:     "free",
		})
		require.NoError(t, err)
	}

	// Sell recursively — the shell does this by walking children.
	dir := eng.State.FindNodeByPath("/premium")
	require.NotNil(t, dir)
	for _, child := range dir.Children {
		_, err = eng.Sell(&vault.SellOpts{
			VaultIndex: 0,
			Path:       "/premium/" + child.Name,
			PricePerKB: 100,
		})
		require.NoError(t, err)
	}

	for _, name := range []string{"a.txt", "b.txt"} {
		node := eng.State.FindNodeByPath("/premium/" + name)
		require.NotNil(t, node)
		assert.Equal(t, "paid", node.Access)
		assert.Equal(t, uint64(100), node.PricePerKB)
	}
}

// --- Batch Operation Tests ---

// TestShell_MputMultipleFiles creates a local directory with files and uploads
// them via Mput, then verifies all files exist in the remote directory.
func TestShell_MputMultipleFiles(t *testing.T) {
	eng := initIntegrationEngine(t)
	seedFeeUTXOs(t, eng, 30, 10_000)

	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/uploads"})
	require.NoError(t, err)

	// Create local directory with files.
	localDir := t.TempDir()
	for _, name := range []string{"file1.txt", "file2.txt", "file3.txt"} {
		err := os.WriteFile(filepath.Join(localDir, name), []byte("content of "+name), 0644)
		require.NoError(t, err)
	}

	// Upload all files (inline mput logic: putfile per entry).
	filesUploaded := 0
	for _, name := range []string{"file1.txt", "file2.txt", "file3.txt"} {
		_, putErr := eng.PutFile(&vault.PutOpts{
			VaultIndex: 0,
			LocalFile:  filepath.Join(localDir, name),
			RemotePath: "/uploads/" + name,
			Access:     "free",
		})
		require.NoError(t, putErr)
		filesUploaded++
	}
	assert.Equal(t, 3, filesUploaded)

	// Verify all uploaded.
	for _, name := range []string{"file1.txt", "file2.txt", "file3.txt"} {
		node := eng.State.FindNodeByPath("/uploads/" + name)
		require.NotNil(t, node, "expected /uploads/%s to exist", name)
	}
}

// TestShell_GetFile uploads a file and downloads it via Get, verifying the
// downloaded content matches the original.
func TestShell_GetFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	content := []byte("download me")
	localFile := createTempFile(t, content)
	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/download.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Get to local.
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "downloaded.txt")
	_, err = eng.Get(&vault.GetOpts{
		RemotePath: "/download.txt",
		LocalPath:  outPath,
	})
	require.NoError(t, err)

	downloaded, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, content, downloaded)
}

// --- Error Path Tests ---

// TestShell_MkdirExistingPath verifies that creating a directory that already
// exists returns an error.
func TestShell_MkdirExistingPath(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/existing"})
	require.NoError(t, err)

	// Creating same dir again should error.
	_, err = eng.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/existing"})
	assert.Error(t, err)
}

// TestShell_RmNonExistent verifies that removing a non-existent path returns
// an error.
func TestShell_RmNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Remove(&vault.RemoveOpts{VaultIndex: 0, Path: "/ghost.txt"})
	assert.Error(t, err)
}

// TestShell_CatNonExistent verifies that catting a non-existent path returns
// an error.
func TestShell_CatNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, _, err := eng.Cat(&vault.CatOpts{Path: "/nope.txt"})
	assert.Error(t, err)
}

// TestShell_MvNonExistent verifies that moving a non-existent path returns
// an error.
func TestShell_MvNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Move(&vault.MoveOpts{VaultIndex: 0, SrcPath: "/nope.txt", DstPath: "/dest.txt"})
	assert.Error(t, err)
}

// TestShell_CpNonExistent verifies that copying a non-existent path returns
// an error.
func TestShell_CpNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Copy(&vault.CopyOpts{VaultIndex: 0, SrcPath: "/nope.txt", DstPath: "/dest.txt"})
	assert.Error(t, err)
}

// =============================================================================
// Task 13: publish / unpublish / sales / decrypt
// =============================================================================

// --- Publish Tests ---

// TestShell_PublishBindDomain publishes a domain and verifies the binding is
// stored in local state with the correct vault root pubkey.
func TestShell_PublishBindDomain(t *testing.T) {
	eng := initIntegrationEngine(t)

	result, err := publish.Publish(eng, nil, &publish.PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	require.NoError(t, err)

	// Result should contain DNS instructions.
	assert.Contains(t, result.Message, "_bitfs.example.com")
	assert.Contains(t, result.Message, "bitfs=")
	assert.NotEmpty(t, result.NodePub)

	// Verify binding is stored in state.
	binding := eng.State.GetPublishBinding("example.com")
	require.NotNil(t, binding)
	assert.Equal(t, "example.com", binding.Domain)
	assert.Equal(t, uint32(0), binding.VaultIndex)
	assert.Equal(t, result.NodePub, binding.PubKeyHex)
}

// TestShell_PublishListBindings publishes two domains, then calls Publish with
// an empty domain to list all bindings. Verifies both domains appear.
func TestShell_PublishListBindings(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Publish two domains.
	_, err := publish.Publish(eng, nil, &publish.PublishOpts{VaultIndex: 0, Domain: "alpha.com"})
	require.NoError(t, err)
	_, err = publish.Publish(eng, nil, &publish.PublishOpts{VaultIndex: 0, Domain: "beta.org"})
	require.NoError(t, err)

	// List bindings (empty domain).
	result, err := publish.Publish(eng, nil, &publish.PublishOpts{VaultIndex: 0, Domain: ""})
	require.NoError(t, err)

	assert.Contains(t, result.Message, "alpha.com")
	assert.Contains(t, result.Message, "beta.org")
	assert.Contains(t, result.Message, "Publish bindings")
}

// TestShell_PublishNoBindings verifies listing bindings with none configured
// returns an appropriate message.
func TestShell_PublishNoBindings(t *testing.T) {
	eng := initIntegrationEngine(t)

	result, err := publish.Publish(eng, nil, &publish.PublishOpts{VaultIndex: 0, Domain: ""})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "No publish bindings")
}

// TestShell_PublishInvalidDomain verifies that invalid domain names are rejected.
func TestShell_PublishInvalidDomain(t *testing.T) {
	eng := initIntegrationEngine(t)

	tests := []string{"nodot", "has space.com", "has\ttab.com"}
	for _, domain := range tests {
		_, err := publish.Publish(eng, nil, &publish.PublishOpts{VaultIndex: 0, Domain: domain})
		assert.Error(t, err, "domain %q should be rejected", domain)
	}
}

// --- Unpublish Tests ---

// TestShell_UnpublishDomain publishes a domain, then unpublishes it, verifying
// the binding is removed from state.
func TestShell_UnpublishDomain(t *testing.T) {
	eng := initIntegrationEngine(t)

	// Publish a domain.
	_, err := publish.Publish(eng, nil, &publish.PublishOpts{VaultIndex: 0, Domain: "remove-me.com"})
	require.NoError(t, err)
	require.NotNil(t, eng.State.GetPublishBinding("remove-me.com"))

	// Unpublish it.
	result, err := publish.Unpublish(eng, &publish.UnpublishOpts{Domain: "remove-me.com"})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "Removed")
	assert.Contains(t, result.Message, "remove-me.com")

	// Verify binding is gone.
	assert.Nil(t, eng.State.GetPublishBinding("remove-me.com"))
}

// TestShell_UnpublishNonExistent verifies that unpublishing a domain that was
// never published returns an error.
func TestShell_UnpublishNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := publish.Unpublish(eng, &publish.UnpublishOpts{Domain: "ghost.com"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no publish binding")
}

// TestShell_UnpublishEmptyDomain verifies that unpublishing with an empty
// domain name returns an error.
func TestShell_UnpublishEmptyDomain(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := publish.Unpublish(eng, &publish.UnpublishOpts{Domain: ""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "domain is required")
}

// --- Sales Tests ---

// TestShell_SalesNoDaemon verifies that querying sales with no running daemon
// returns an error (the shell prints "is daemon running?").
func TestShell_SalesNoDaemon(t *testing.T) {
	// client.New points at a non-existent server by default.
	cl := client.New("http://127.0.0.1:1") // port 1 = guaranteed connection refused
	_, err := cl.GetSales("all", 50)
	assert.Error(t, err, "GetSales should fail when daemon is not running")
}

// TestShell_SalesWithMockDaemon starts a mock HTTP server that returns sales
// records, then verifies the client correctly parses them.
func TestShell_SalesWithMockDaemon(t *testing.T) {
	// Mock sales endpoint.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_bitfs/sales" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{"invoice_id":"inv-001","key_hash":"aabb","price":500,"paid":true},
			{"invoice_id":"inv-002","key_hash":"ccdd","price":1000,"paid":false}
		]`))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	cl := client.New(srv.URL)
	records, err := cl.GetSales("all", 50)
	require.NoError(t, err)
	require.Len(t, records, 2)

	assert.Equal(t, "inv-001", records[0].InvoiceID)
	assert.Equal(t, uint64(500), records[0].Price)
	assert.True(t, records[0].Paid)

	assert.Equal(t, "inv-002", records[1].InvoiceID)
	assert.Equal(t, uint64(1000), records[1].Price)
	assert.False(t, records[1].Paid)
}

// --- Decrypt Tests (standalone, separate from EncryptDecrypt) ---

// TestShell_DecryptFile uploads a file as free, encrypts it to private, then
// decrypts it back to free. Verifies each access mode transition independently.
func TestShell_DecryptFile(t *testing.T) {
	eng := initIntegrationEngine(t)

	content := []byte("decrypt test content")
	localFile := createTempFile(t, content)

	// Upload as free.
	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/decrypt-test.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	node := eng.State.FindNodeByPath("/decrypt-test.txt")
	require.NotNil(t, node)
	assert.Equal(t, "free", node.Access)
	originalKeyHash := node.KeyHash

	// Encrypt: FREE -> PRIVATE.
	encResult, err := eng.EncryptNode(&vault.EncryptOpts{
		VaultIndex: 0,
		Path:       "/decrypt-test.txt",
	})
	require.NoError(t, err)
	assert.Contains(t, encResult.Message, "PRIVATE")

	node = eng.State.FindNodeByPath("/decrypt-test.txt")
	require.NotNil(t, node)
	assert.Equal(t, "private", node.Access)

	// Decrypt: PRIVATE -> FREE.
	decResult, err := eng.DecryptNode(&vault.DecryptOpts{
		Path: "/decrypt-test.txt",
	})
	require.NoError(t, err)
	assert.Contains(t, decResult.Message, "FREE")
	assert.NotEmpty(t, decResult.TxHex)
	assert.NotEmpty(t, decResult.TxID)

	// Verify access mode is back to free.
	node = eng.State.FindNodeByPath("/decrypt-test.txt")
	require.NotNil(t, node)
	assert.Equal(t, "free", node.Access)

	// key_hash = SHA256(SHA256(plaintext)) is content-based and access-mode-independent,
	// so it stays the same across all access mode transitions.
	assert.NotEmpty(t, node.KeyHash)
	assert.Equal(t, originalKeyHash, node.KeyHash,
		"key_hash is content-based and should remain the same after round-trip")
}

// TestShell_DecryptAlreadyFree verifies that decrypting a file that is already
// free returns an error.
func TestShell_DecryptAlreadyFree(t *testing.T) {
	eng := initIntegrationEngine(t)

	content := []byte("already free file")
	localFile := createTempFile(t, content)

	_, err := eng.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/free-file.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.DecryptNode(&vault.DecryptOpts{Path: "/free-file.txt"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already free")
}

// TestShell_DecryptNonExistent verifies that decrypting a non-existent path
// returns an error.
func TestShell_DecryptNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.DecryptNode(&vault.DecryptOpts{Path: "/ghost.txt"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

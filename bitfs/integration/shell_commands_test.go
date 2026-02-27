//go:build integration

package integration

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/engine"
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
	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Create nested dirs.
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/photos"})
	require.NoError(t, err)
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/docs"})
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

	_, err := eng.PutFile(&engine.PutOpts{
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
	reader, info, err := eng.Cat(&engine.CatOpts{Path: "/hello.txt"})
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
			_, err := eng.PutFile(&engine.PutOpts{
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
	_, err := eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/temp.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Remove the file.
	_, err = eng.Remove(&engine.RemoveOpts{
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

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/dir"})
	require.NoError(t, err)

	content := []byte("nested file")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/dir/file.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Remove child first, then dir (simulating rm -r).
	_, err = eng.Remove(&engine.RemoveOpts{VaultIndex: 0, Path: "/dir/file.txt"})
	require.NoError(t, err)

	_, err = eng.Remove(&engine.RemoveOpts{VaultIndex: 0, Path: "/dir"})
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
	_, err := eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/old.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.Move(&engine.MoveOpts{
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

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/src"})
	require.NoError(t, err)
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/dst"})
	require.NoError(t, err)

	content := []byte("cross dir move")
	localFile := createTempFile(t, content)
	_, err = eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/src/file.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.Move(&engine.MoveOpts{
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
	_, err := eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/orig.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.Copy(&engine.CopyOpts{
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
	_, err := eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/target.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	targetNode := eng.State.FindNodeByPath("/target.txt")
	require.NotNil(t, targetNode)

	_, err = eng.Link(&engine.LinkOpts{
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
	_, err := eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/target.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	targetNode := eng.State.FindNodeByPath("/target.txt")
	require.NotNil(t, targetNode)

	_, err = eng.Link(&engine.LinkOpts{
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
	_, err := eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/secret.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Encrypt: Free -> Private.
	_, err = eng.EncryptNode(&engine.EncryptOpts{
		VaultIndex: 0,
		Path:       "/secret.txt",
	})
	require.NoError(t, err)

	node := eng.State.FindNodeByPath("/secret.txt")
	require.NotNil(t, node)
	assert.Equal(t, "private", node.Access, "access should be private after encrypt")

	// Decrypt: Private -> Free.
	_, err = eng.DecryptNode(&engine.DecryptOpts{
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
	_, err := eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/premium.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	_, err = eng.Sell(&engine.SellOpts{
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

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/premium"})
	require.NoError(t, err)

	for _, name := range []string{"a.txt", "b.txt"} {
		content := []byte("content of " + name)
		localFile := createTempFile(t, content)
		_, err = eng.PutFile(&engine.PutOpts{
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
		_, err = eng.Sell(&engine.SellOpts{
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

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/uploads"})
	require.NoError(t, err)

	// Create local directory with files.
	localDir := t.TempDir()
	for _, name := range []string{"file1.txt", "file2.txt", "file3.txt"} {
		err := os.WriteFile(filepath.Join(localDir, name), []byte("content of "+name), 0644)
		require.NoError(t, err)
	}

	// Mput all files.
	mputResult, err := eng.Mput(&engine.MputOpts{
		VaultIndex: 0,
		LocalDir:   localDir,
		RemoteDir:  "/uploads",
		Access:     "free",
	})
	require.NoError(t, err)
	assert.Equal(t, 3, mputResult.FilesUploaded)
	assert.Empty(t, mputResult.Errors)

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
	_, err := eng.PutFile(&engine.PutOpts{
		VaultIndex: 0,
		LocalFile:  localFile,
		RemotePath: "/download.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	// Get to local.
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "downloaded.txt")
	_, err = eng.Get(&engine.GetOpts{
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

	_, err := eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/existing"})
	require.NoError(t, err)

	// Creating same dir again should error.
	_, err = eng.Mkdir(&engine.MkdirOpts{VaultIndex: 0, Path: "/existing"})
	assert.Error(t, err)
}

// TestShell_RmNonExistent verifies that removing a non-existent path returns
// an error.
func TestShell_RmNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Remove(&engine.RemoveOpts{VaultIndex: 0, Path: "/ghost.txt"})
	assert.Error(t, err)
}

// TestShell_CatNonExistent verifies that catting a non-existent path returns
// an error.
func TestShell_CatNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, _, err := eng.Cat(&engine.CatOpts{Path: "/nope.txt"})
	assert.Error(t, err)
}

// TestShell_MvNonExistent verifies that moving a non-existent path returns
// an error.
func TestShell_MvNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Move(&engine.MoveOpts{VaultIndex: 0, SrcPath: "/nope.txt", DstPath: "/dest.txt"})
	assert.Error(t, err)
}

// TestShell_CpNonExistent verifies that copying a non-existent path returns
// an error.
func TestShell_CpNonExistent(t *testing.T) {
	eng := initIntegrationEngine(t)

	_, err := eng.Copy(&engine.CopyOpts{VaultIndex: 0, SrcPath: "/nope.txt", DstPath: "/dest.txt"})
	assert.Error(t, err)
}

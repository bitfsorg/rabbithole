package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMput_Success(t *testing.T) {
	eng := initTestEngine(t)

	// Create root.
	addFeeUTXO(t, eng, 100000)
	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Create local directory tree: localdir/a.txt, localdir/sub/b.txt
	localDir := t.TempDir()
	subDir := filepath.Join(localDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "a.txt"), []byte("file a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("file b"), 0644))

	// Add fee UTXOs for mkdir + 2 puts.
	addFeeUTXO(t, eng, 100000)
	addFeeUTXO(t, eng, 100000)
	addFeeUTXO(t, eng, 100000)

	result, err := eng.Mput(&MputOpts{
		VaultIndex: 0,
		LocalDir:   localDir,
		RemoteDir:  "/",
		Access:     "free",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.DirsCreated)   // /sub
	assert.Equal(t, 2, result.FilesUploaded) // a.txt, sub/b.txt

	// Verify nodes exist.
	assert.NotNil(t, eng.State.FindNodeByPath("/a.txt"))
	assert.NotNil(t, eng.State.FindNodeByPath("/sub"))
	assert.NotNil(t, eng.State.FindNodeByPath("/sub/b.txt"))
}

func TestMput_NotDirectory(t *testing.T) {
	eng := initTestEngine(t)

	// Point to a file, not a directory.
	testFile := writeTestFile(t, t.TempDir(), "file.txt", "content")

	_, err := eng.Mput(&MputOpts{VaultIndex: 0, LocalDir: testFile, RemoteDir: "/"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestMput_NonexistentLocal(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Mput(&MputOpts{VaultIndex: 0, LocalDir: "/nonexistent/dir", RemoteDir: "/"})
	require.Error(t, err)
}

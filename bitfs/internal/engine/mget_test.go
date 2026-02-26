package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMget_Success(t *testing.T) {
	eng := setupDirTreeEngine(t)
	outDir := t.TempDir()

	result, err := eng.Mget(&MgetOpts{
		VaultIndex: 0,
		RemotePath: "/docs",
		LocalDir:   outDir,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.DirsCreated)
	assert.Equal(t, 1, result.FilesDownloaded)
	assert.Empty(t, result.Errors)

	// Verify downloaded file.
	data, err := os.ReadFile(filepath.Join(outDir, "readme.txt"))
	require.NoError(t, err)
	assert.Equal(t, "readme content", string(data))
}

func TestMget_NotFound(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Mget(&MgetOpts{VaultIndex: 0, RemotePath: "/nope", LocalDir: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMget_NotDirectory(t *testing.T) {
	eng, _ := setupCopyTestEngine(t)

	_, err := eng.Mget(&MgetOpts{VaultIndex: 0, RemotePath: "/test.txt", LocalDir: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

// setupDirTreeEngine creates an engine with /docs/readme.txt.
func setupDirTreeEngine(t *testing.T) *Engine {
	t.Helper()
	eng := initTestEngine(t)

	addFeeUTXO(t, eng, 100000)
	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	addFeeUTXO(t, eng, 100000)
	_, err = eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/docs"})
	require.NoError(t, err)

	testFile := writeTestFile(t, eng.DataDir, "readme.txt", "readme content")
	addFeeUTXO(t, eng, 100000)
	_, err = eng.PutFile(&PutOpts{
		VaultIndex: 0,
		LocalFile:  testFile,
		RemotePath: "/docs/readme.txt",
		Access:     "free",
	})
	require.NoError(t, err)

	return eng
}

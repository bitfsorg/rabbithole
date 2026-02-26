package engine

import (
	"os"
	"path/filepath"
	"strings"
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

func TestMget_PathTraversalRejected(t *testing.T) {
	eng := initTestEngine(t)

	// Create a directory node with malicious child names that could escape the target path.
	dir := &NodeState{
		PubKeyHex: "deadbeef01",
		Path:      "/testdir",
		Type:      "dir",
		Children: []*ChildState{
			{Name: "../../etc/passwd", Type: "file", PubKey: "malicious01", Index: 0},
			{Name: "normal.txt", Type: "file", PubKey: "goodfile01", Index: 1},
			{Name: "sub/../escape", Type: "file", PubKey: "malicious02", Index: 2},
			{Name: "back\\slash", Type: "file", PubKey: "malicious03", Index: 3},
			{Name: "", Type: "file", PubKey: "malicious04", Index: 4},
		},
	}
	eng.State.SetNode(dir.PubKeyHex, dir)

	result := &MgetResult{}
	eng.mgetRecurse(0, dir, t.TempDir(), result)

	// All 4 malicious names (traversal, backslash, empty) should be rejected, recorded as errors.
	assert.GreaterOrEqual(t, len(result.Errors), 4, "should reject traversal/backslash/empty names")

	// The normal.txt child will also produce an error (node not found), but that's a different error.
	// Check that traversal-specific errors are present.
	traversalCount := 0
	for _, e := range result.Errors {
		if strings.Contains(e, "unsafe child name") {
			traversalCount++
		}
	}
	assert.Equal(t, 4, traversalCount, "should have 4 unsafe-child-name errors")
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

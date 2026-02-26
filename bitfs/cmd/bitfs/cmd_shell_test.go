package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAccessMode(t *testing.T) {
	assert.NoError(t, validateAccessMode("free"))
	assert.NoError(t, validateAccessMode("private"))
	assert.NoError(t, validateAccessMode("paid"))
	assert.Error(t, validateAccessMode("prviate"))
	assert.Error(t, validateAccessMode(""))
	assert.Error(t, validateAccessMode("PUBLIC"))
}

func TestShellHistoryFile_RestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	historyFile := filepath.Join(dir, "shell_history")

	// Create the file to simulate what readline does.
	require.NoError(t, os.WriteFile(historyFile, []byte("test\n"), 0644))

	// Our function should fix permissions.
	ensureHistoryFilePermissions(historyFile)

	info, err := os.Stat(historyFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

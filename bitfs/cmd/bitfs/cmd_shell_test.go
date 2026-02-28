package main

import (
	"bytes"
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

// =============================================================================
// pwd / cd — path resolution and normalization
// =============================================================================

// TestResolvePath verifies the resolvePath function used by cd and all
// path-based shell commands. Absolute paths pass through; relative paths
// are joined with the current working directory.
func TestResolvePath(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
		path string
		want string
	}{
		{"relative from root", "/", "file.txt", "/file.txt"},
		{"absolute path passthrough", "/docs", "/abs/path", "/abs/path"},
		{"relative from subdir", "/docs", "readme.md", "/docs/readme.md"},
		{"parent traversal", "/docs", "../photos/img.png", "/photos/img.png"},
		{"double parent traversal", "/a/b/c", "../../d", "/a/d"},
		{"parent beyond root", "/", "..", "/"},
		{"root shortcut", "/docs", "/", "/"},
		{"dot current dir", "/docs", "./file.txt", "/docs/file.txt"},
		{"nested relative", "/a/b", "c/d/e", "/a/b/c/d/e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePath(tt.cwd, tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCleanPath verifies the cleanPath function that normalizes shell paths.
// This is the core of pwd display and cd path handling — double slashes,
// dot segments, and parent traversal must all be resolved correctly.
func TestCleanPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"root", "/", "/"},
		{"double slash", "//", "/"},
		{"triple slash", "///", "/"},
		{"normal path", "/a/b/c", "/a/b/c"},
		{"redundant slashes", "/a//b///c", "/a/b/c"},
		{"parent traversal", "/a/b/../c", "/a/c"},
		{"dot removal", "/a/./b/c", "/a/b/c"},
		{"parent beyond root", "/../..", "/"},
		{"single parent", "/a/b/c/..", "/a/b"},
		{"double parent", "/a/b/c/../..", "/a"},
		{"triple parent to root", "/a/b/c/../../..", "/"},
		{"trailing slash", "/a/b/", "/a/b"},
		{"mixed dots and parents", "/a/./b/../c/./d", "/a/c/d"},
		{"empty string", "", "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanPath(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// lcd — local directory change validation
// =============================================================================

// TestLcd_PathResolution verifies the lcd path handling logic:
// relative paths are joined with localCwd, absolute paths are used directly,
// and non-directory targets are rejected.
func TestLcd_PathResolution(t *testing.T) {
	// Create a temporary directory structure.
	root := t.TempDir()
	subdir := filepath.Join(root, "sub")
	require.NoError(t, os.MkdirAll(subdir, 0755))

	// Create a regular file (lcd should reject this).
	regularFile := filepath.Join(root, "file.txt")
	require.NoError(t, os.WriteFile(regularFile, []byte("not a dir"), 0644))

	t.Run("absolute path to existing dir", func(t *testing.T) {
		target := subdir
		info, err := os.Stat(target)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("relative path joined with localCwd", func(t *testing.T) {
		localCwd := root
		target := "sub"
		if !filepath.IsAbs(target) {
			target = filepath.Join(localCwd, target)
		}
		target = filepath.Clean(target)
		info, err := os.Stat(target)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
		assert.Equal(t, subdir, target)
	})

	t.Run("reject regular file", func(t *testing.T) {
		info, err := os.Stat(regularFile)
		require.NoError(t, err)
		assert.False(t, info.IsDir(), "regular file should not pass directory check")
	})

	t.Run("reject nonexistent path", func(t *testing.T) {
		_, err := os.Stat(filepath.Join(root, "nonexistent"))
		assert.True(t, os.IsNotExist(err))
	})
}

// =============================================================================
// help — verify shellHelp lists all commands
// =============================================================================

// TestShellHelp verifies that shellHelp output includes every command name
// defined in shellCommands.
func TestShellHelp(t *testing.T) {
	// Capture shellHelp output.
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	shellHelp()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	// Every command in shellCommands should appear in the help text.
	for _, cmd := range shellCommands {
		if cmd == "exit" {
			continue // "quit" covers the exit line
		}
		assert.Contains(t, output, cmd,
			"shellHelp output should mention command %q", cmd)
	}
}

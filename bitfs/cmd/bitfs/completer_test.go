package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/vault"
)

// shellCommandsList is the full list of shell command names.
var shellCommandsList = []string{
	"ls", "cd", "lcd", "pwd", "cat", "get", "mget", "mput", "mkdir", "put", "rm", "mv", "cp",
	"link", "sell", "encrypt", "publish", "unpublish", "help", "quit", "exit",
}

func TestCompleteCommandNames_EmptyInput(t *testing.T) {
	sc := &shellCompleter{commands: shellCommandsList}
	candidates := sc.completeCommandName("")
	assert.Equal(t, shellCommandsList, candidates)
}

func TestCompleteCommandNames_Prefix(t *testing.T) {
	sc := &shellCompleter{commands: shellCommandsList}
	candidates := sc.completeCommandName("l")
	assert.Equal(t, []string{"ls", "lcd", "link"}, candidates)
}

func TestCompleteCommandNames_ExactMatch(t *testing.T) {
	sc := &shellCompleter{commands: shellCommandsList}
	candidates := sc.completeCommandName("pwd")
	assert.Equal(t, []string{"pwd"}, candidates)
}

func TestCompleteCommandNames_NoMatch(t *testing.T) {
	sc := &shellCompleter{commands: shellCommandsList}
	candidates := sc.completeCommandName("zzz")
	assert.Empty(t, candidates)
}

func TestCompleteRemotePath_RootChildren(t *testing.T) {
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "docs", Type: "dir"},
			{Name: "hello.txt", Type: "file"},
			{Name: "data", Type: "dir"},
		},
	})

	sc := &shellCompleter{state: state, cwd: "/"}
	candidates := sc.completeRemotePath("")
	assert.Equal(t, []string{"docs/", "hello.txt", "data/"}, candidates)
}

func TestCompleteRemotePath_Prefix(t *testing.T) {
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "docs", Type: "dir"},
			{Name: "hello.txt", Type: "file"},
			{Name: "data", Type: "dir"},
		},
	})

	sc := &shellCompleter{state: state, cwd: "/"}
	candidates := sc.completeRemotePath("d")
	assert.Equal(t, []string{"docs/", "data/"}, candidates)
}

func TestCompleteRemotePath_NestedDir(t *testing.T) {
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "docs", Type: "dir"},
		},
	})
	state.SetNode("bbb", &vault.NodeState{
		Path: "/docs", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "readme.md", Type: "file"},
			{Name: "api.md", Type: "file"},
		},
	})

	sc := &shellCompleter{state: state, cwd: "/"}
	candidates := sc.completeRemotePath("docs/")
	assert.Equal(t, []string{"docs/readme.md", "docs/api.md"}, candidates)
}

func TestCompleteRemotePath_AbsolutePath(t *testing.T) {
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "docs", Type: "dir"},
		},
	})
	state.SetNode("bbb", &vault.NodeState{
		Path: "/docs", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "readme.md", Type: "file"},
		},
	})

	sc := &shellCompleter{state: state, cwd: "/other"}
	candidates := sc.completeRemotePath("/docs/")
	assert.Equal(t, []string{"/docs/readme.md"}, candidates)
}

func TestCompleteLocalPath(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "subdir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("x"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "file2.go"), []byte("x"), 0644))

	sc := &shellCompleter{localCwd: tmp}
	candidates := sc.completeLocalPath("file")
	assert.ElementsMatch(t, []string{"file.txt", "file2.go"}, candidates)
}

func TestCompleteLocalPath_Subdir(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "subdir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "subdir", "a.txt"), []byte("x"), 0644))

	sc := &shellCompleter{localCwd: tmp}
	candidates := sc.completeLocalPath("subdir/")
	assert.Equal(t, []string{"subdir/a.txt"}, candidates)
}

func TestShellCompleterDo_FirstToken(t *testing.T) {
	sc := &shellCompleter{commands: shellCommandsList}
	line := []rune("l")
	newLine, length := sc.Do(line, 1)
	assert.Equal(t, 1, length)
	assert.Len(t, newLine, 3)
}

func TestShellCompleterDo_CdArgument(t *testing.T) {
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "docs", Type: "dir"},
			{Name: "music", Type: "dir"},
		},
	})

	sc := &shellCompleter{
		commands: shellCommandsList,
		state:    state,
		cwd:      "/",
	}
	line := []rune("cd d")
	newLine, length := sc.Do(line, 4)
	assert.Equal(t, 1, length)
	require.Len(t, newLine, 1)
	assert.Equal(t, "ocs/", string(newLine[0]))
}

func TestShellCompleterDo_LcdArgument(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "mydir"), 0755))

	sc := &shellCompleter{
		commands: shellCommandsList,
		localCwd: tmp,
	}
	line := []rune("lcd m")
	newLine, length := sc.Do(line, 5)
	assert.Equal(t, 1, length)
	require.Len(t, newLine, 1)
	assert.Equal(t, "ydir/", string(newLine[0]))
}

func TestShellCompleterDo_PutFirstArg_Local(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "upload.bin"), []byte("x"), 0644))

	sc := &shellCompleter{
		commands: shellCommandsList,
		localCwd: tmp,
	}
	line := []rune("put u")
	newLine, length := sc.Do(line, 5)
	assert.Equal(t, 1, length)
	require.Len(t, newLine, 1)
	assert.Equal(t, "pload.bin ", string(newLine[0]))
}

func TestShellCompleterDo_PutSecondArg_Remote(t *testing.T) {
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "uploads", Type: "dir"},
		},
	})

	sc := &shellCompleter{
		commands: shellCommandsList,
		state:    state,
		cwd:      "/",
	}
	line := []rune("put file.txt u")
	newLine, length := sc.Do(line, 14)
	assert.Equal(t, 1, length)
	require.Len(t, newLine, 1)
	assert.Equal(t, "ploads/", string(newLine[0]))
}

func TestParseLineForCompletion_Empty(t *testing.T) {
	tokens, current := parseLineForCompletion("")
	assert.Nil(t, tokens)
	assert.Equal(t, "", current)
}

func TestParseLineForCompletion_SinglePartialToken(t *testing.T) {
	tokens, current := parseLineForCompletion("ls")
	assert.Empty(t, tokens)
	assert.Equal(t, "ls", current)
}

func TestParseLineForCompletion_CommandAndPartialArg(t *testing.T) {
	tokens, current := parseLineForCompletion("cd do")
	assert.Equal(t, []string{"cd"}, tokens)
	assert.Equal(t, "do", current)
}

func TestParseLineForCompletion_CommandAndTrailingSpace(t *testing.T) {
	tokens, current := parseLineForCompletion("cd ")
	assert.Equal(t, []string{"cd"}, tokens)
	assert.Equal(t, "", current)
}

func TestParseLineForCompletion_TwoArgsAndPartial(t *testing.T) {
	tokens, current := parseLineForCompletion("put file.txt /docs/r")
	assert.Equal(t, []string{"put", "file.txt"}, tokens)
	assert.Equal(t, "/docs/r", current)
}

func TestFormatCandidates_Empty(t *testing.T) {
	result, length := formatCandidates(nil, "x")
	assert.Nil(t, result)
	assert.Equal(t, 0, length)
}

func TestFormatCandidates_DirSuffix(t *testing.T) {
	result, length := formatCandidates([]string{"docs/"}, "d")
	require.Len(t, result, 1)
	assert.Equal(t, "ocs/", string(result[0]))
	assert.Equal(t, 1, length)
}

func TestFormatCandidates_FileSuffix(t *testing.T) {
	result, length := formatCandidates([]string{"readme.md"}, "r")
	require.Len(t, result, 1)
	// File completions get a trailing space.
	assert.Equal(t, "eadme.md ", string(result[0]))
	assert.Equal(t, 1, length)
}

func TestCompleteRemotePath_CacheHit(t *testing.T) {
	// Set up initial state with one child.
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "alpha", Type: "file"},
		},
	})

	sc := &shellCompleter{state: state, cwd: "/"}

	// First call populates the cache.
	c1 := sc.completeRemotePath("")
	assert.Equal(t, []string{"alpha"}, c1)

	// Mutate the underlying state — add a second child.
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "alpha", Type: "file"},
			{Name: "beta", Type: "file"},
		},
	})

	// Second call within TTL should still return cached (old) result.
	c2 := sc.completeRemotePath("")
	assert.Equal(t, []string{"alpha"}, c2, "expected cached result within TTL")
}

func TestCompleteRemotePath_CacheExpiry(t *testing.T) {
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "alpha", Type: "file"},
		},
	})

	sc := &shellCompleter{state: state, cwd: "/"}

	// First call populates the cache.
	c1 := sc.completeRemotePath("")
	assert.Equal(t, []string{"alpha"}, c1)

	// Mutate state.
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "alpha", Type: "file"},
			{Name: "beta", Type: "file"},
		},
	})

	// Expire the cache manually.
	sc.cacheExpiry = time.Now().Add(-1 * time.Second)

	// Third call should see the fresh data.
	c3 := sc.completeRemotePath("")
	assert.Equal(t, []string{"alpha", "beta"}, c3, "expected fresh result after cache expiry")
}

// ---------------------------------------------------------------------------
// Do function — additional switch branch coverage
// ---------------------------------------------------------------------------

func newCompleterWithState(t *testing.T) *shellCompleter {
	t.Helper()
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "docs", Type: "dir"},
			{Name: "file.txt", Type: "file"},
		},
	})
	return &shellCompleter{
		commands: shellCommands, // use real shellCommands
		state:    state,
		cwd:      "/",
		localCwd: t.TempDir(),
	}
}

func TestCompleterDo_MvArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("mv "), 3)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_CpArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("cp "), 3)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_LinkArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("link "), 5)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_CatArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("cat "), 4)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_GetArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("get "), 4)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_MgetArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("mget "), 5)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_MputFirstArg(t *testing.T) {
	sc := newCompleterWithState(t)
	os.MkdirAll(filepath.Join(sc.localCwd, "up"), 0755)
	candidates, _ := sc.Do([]rune("mput "), 5)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_MputSecondArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("mput localdir "), 14)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_RmArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("rm "), 3)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_MkdirArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("mkdir "), 6)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_EncryptArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("encrypt "), 8)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_SellArg(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("sell "), 5)
	assert.NotNil(t, candidates)
}

func TestCompleterDo_UnknownCmd(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune("unknown "), 8)
	assert.Nil(t, candidates)
}

func TestCompleterDo_EmptyInput(t *testing.T) {
	sc := newCompleterWithState(t)
	candidates, _ := sc.Do([]rune(""), 0)
	assert.NotNil(t, candidates)
}

func TestCompleteRemotePath_CacheDifferentDir(t *testing.T) {
	state := vault.NewLocalState("")
	state.SetNode("aaa", &vault.NodeState{
		Path: "/", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "docs", Type: "dir"},
		},
	})
	state.SetNode("bbb", &vault.NodeState{
		Path: "/docs", Type: "dir",
		Children: []*vault.ChildState{
			{Name: "readme.md", Type: "file"},
		},
	})

	sc := &shellCompleter{state: state, cwd: "/"}

	// Populate cache for "/".
	c1 := sc.completeRemotePath("")
	assert.Equal(t, []string{"docs/"}, c1)

	// Query a different directory — cache should miss.
	c2 := sc.completeRemotePath("docs/")
	assert.Equal(t, []string{"docs/readme.md"}, c2)
}

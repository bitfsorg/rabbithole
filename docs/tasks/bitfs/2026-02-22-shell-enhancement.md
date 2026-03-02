# Shell Enhancement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the `bufio.Scanner`-based REPL in `bitfs shell` with `ergochat/readline` to add line editing, persistent history, tab completion (commands + remote/local paths), and the missing `cp` command.

**Architecture:** Add `ergochat/readline` dependency. Refactor `cmd_shell.go` to use `readline.NewFromConfig` with a dynamic `AutoCompleter` implementation. The completer parses the current line to determine context (command vs argument position) and dispatches to command-name, remote-path, or local-path completion accordingly. History is persisted to `~/.bitfs/shell_history`.

**Tech Stack:** Go 1.25.6, `github.com/ergochat/readline` (chzyer/readline fork), `github.com/tongxiaofeng/bitfs/internal/engine`

**TDD:** Each task writes tests first, then implementation.

---

### Task 1: Add `ergochat/readline` dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add the dependency**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
go get github.com/ergochat/readline@latest
```

**Step 2: Verify it resolves**

```bash
go mod tidy
```

Expected: No errors. `go.mod` now contains `github.com/ergochat/readline`.

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add ergochat/readline dependency"
```

---

### Task 2: Extract completion logic into testable `cmd/bitfs/completer.go`

**Files:**
- Create: `cmd/bitfs/completer.go`
- Create: `cmd/bitfs/completer_test.go`

**Step 1: Write the failing tests**

Create `cmd/bitfs/completer_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/bitfs/internal/engine"
)

// shellCommands is the full list of shell command names.
var shellCommandsList = []string{
	"ls", "cd", "lcd", "pwd", "mkdir", "put", "rm", "mv", "cp",
	"link", "sell", "encrypt", "help", "quit", "exit",
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
	state := engine.NewLocalState("")
	state.SetNode("aaa", &engine.NodeState{
		Path: "/", Type: "dir",
		Children: []*engine.ChildState{
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
	state := engine.NewLocalState("")
	state.SetNode("aaa", &engine.NodeState{
		Path: "/", Type: "dir",
		Children: []*engine.ChildState{
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
	state := engine.NewLocalState("")
	state.SetNode("aaa", &engine.NodeState{
		Path: "/", Type: "dir",
		Children: []*engine.ChildState{
			{Name: "docs", Type: "dir"},
		},
	})
	state.SetNode("bbb", &engine.NodeState{
		Path: "/docs", Type: "dir",
		Children: []*engine.ChildState{
			{Name: "readme.md", Type: "file"},
			{Name: "api.md", Type: "file"},
		},
	})

	sc := &shellCompleter{state: state, cwd: "/"}
	candidates := sc.completeRemotePath("docs/")
	assert.Equal(t, []string{"docs/readme.md", "docs/api.md"}, candidates)
}

func TestCompleteRemotePath_AbsolutePath(t *testing.T) {
	state := engine.NewLocalState("")
	state.SetNode("aaa", &engine.NodeState{
		Path: "/", Type: "dir",
		Children: []*engine.ChildState{
			{Name: "docs", Type: "dir"},
		},
	})
	state.SetNode("bbb", &engine.NodeState{
		Path: "/docs", Type: "dir",
		Children: []*engine.ChildState{
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
	// Should offer completions for "ls", "lcd", "link"
	assert.Len(t, newLine, 3)
}

func TestShellCompleterDo_CdArgument(t *testing.T) {
	state := engine.NewLocalState("")
	state.SetNode("aaa", &engine.NodeState{
		Path: "/", Type: "dir",
		Children: []*engine.ChildState{
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
	assert.Equal(t, "pload.bin", string(newLine[0]))
}

func TestShellCompleterDo_PutSecondArg_Remote(t *testing.T) {
	state := engine.NewLocalState("")
	state.SetNode("aaa", &engine.NodeState{
		Path: "/", Type: "dir",
		Children: []*engine.ChildState{
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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/bitfs/ -run TestComplete -v -count=1`
Expected: FAIL — `shellCompleter` type doesn't exist.

**Step 3: Write the completer implementation**

Create `cmd/bitfs/completer.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/tongxiaofeng/bitfs/internal/engine"
)

// shellCompleter implements readline.AutoCompleter for the BitFS shell.
// It provides context-aware completion: command names for the first token,
// remote paths for filesystem commands, local paths for lcd/put.
type shellCompleter struct {
	commands []string
	state    *engine.LocalState
	cwd      string  // remote working directory (mutable pointer updated by shell loop)
	localCwd string  // local working directory (mutable pointer updated by shell loop)
}

// Do implements the readline.AutoCompleter interface.
// It parses the line up to pos to determine context, then dispatches
// to the appropriate completion function.
func (sc *shellCompleter) Do(line []rune, pos int) ([][]rune, int) {
	// Only complete up to cursor position.
	lineStr := string(line[:pos])

	// Split into tokens. We need to know which token the cursor is in.
	tokens, currentPrefix := parseLineForCompletion(lineStr)

	if len(tokens) == 0 {
		// First token: command name completion.
		candidates := sc.completeCommandName(currentPrefix)
		return formatCandidates(candidates, currentPrefix)
	}

	cmd := tokens[0]
	argIndex := len(tokens) // 0-based index of the argument being completed
	// If currentPrefix is empty and line ends with space, we're starting a new arg.
	// If currentPrefix is non-empty, we're in the middle of an arg.

	switch cmd {
	case "cd", "ls", "rm", "mkdir", "encrypt", "sell":
		// All args are remote paths.
		candidates := sc.completeRemotePath(currentPrefix)
		return formatCandidates(candidates, currentPrefix)

	case "lcd":
		// Local directory path.
		candidates := sc.completeLocalPath(currentPrefix)
		return formatCandidates(candidates, currentPrefix)

	case "put":
		if argIndex <= 1 {
			// First arg: local file.
			candidates := sc.completeLocalPath(currentPrefix)
			return formatCandidates(candidates, currentPrefix)
		}
		// Second arg: remote path.
		candidates := sc.completeRemotePath(currentPrefix)
		return formatCandidates(candidates, currentPrefix)

	case "mv", "cp", "link":
		// Both args are remote paths.
		candidates := sc.completeRemotePath(currentPrefix)
		return formatCandidates(candidates, currentPrefix)

	default:
		return nil, 0
	}
}

// completeCommandName returns command names matching the given prefix.
func (sc *shellCompleter) completeCommandName(prefix string) []string {
	if prefix == "" {
		return sc.commands
	}
	var matches []string
	for _, cmd := range sc.commands {
		if strings.HasPrefix(cmd, prefix) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

// completeRemotePath returns remote path completions from the Metanet DAG.
// The partial argument may be relative to cwd or absolute.
func (sc *shellCompleter) completeRemotePath(partial string) []string {
	if sc.state == nil {
		return nil
	}

	// Split partial into directory part and name prefix.
	// e.g. "docs/re" → dir="docs", namePrefix="re"
	// e.g. "docs/"   → dir="docs", namePrefix=""
	// e.g. "d"       → dir="",     namePrefix="d"
	var dirPart, namePrefix string
	if idx := strings.LastIndex(partial, "/"); idx >= 0 {
		dirPart = partial[:idx+1] // include trailing slash
		namePrefix = partial[idx+1:]
	} else {
		dirPart = ""
		namePrefix = partial
	}

	// Resolve the directory to look up children from.
	var lookupDir string
	if strings.HasPrefix(partial, "/") {
		// Absolute path.
		if dirPart == "" {
			lookupDir = "/"
		} else {
			lookupDir = cleanPath(dirPart)
		}
	} else {
		// Relative to cwd.
		if dirPart == "" {
			lookupDir = sc.cwd
		} else {
			lookupDir = cleanPath(sc.cwd + "/" + dirPart)
		}
	}

	node := sc.state.FindNodeByPath(lookupDir)
	if node == nil || node.Type != "dir" {
		return nil
	}

	var matches []string
	for _, c := range node.Children {
		if strings.HasPrefix(c.Name, namePrefix) {
			candidate := dirPart + c.Name
			if c.Type == "dir" {
				candidate += "/"
			}
			matches = append(matches, candidate)
		}
	}
	return matches
}

// completeLocalPath returns local filesystem path completions.
func (sc *shellCompleter) completeLocalPath(partial string) []string {
	// Split into directory and name prefix.
	var dir, namePrefix string
	if idx := strings.LastIndex(partial, string(filepath.Separator)); idx >= 0 {
		dir = partial[:idx+1]
		namePrefix = partial[idx+1:]
	} else if idx := strings.LastIndex(partial, "/"); idx >= 0 {
		dir = partial[:idx+1]
		namePrefix = partial[idx+1:]
	} else {
		dir = ""
		namePrefix = partial
	}

	// Resolve the directory to list.
	var lookupDir string
	if filepath.IsAbs(dir) {
		lookupDir = filepath.Clean(dir)
	} else if dir == "" {
		lookupDir = sc.localCwd
	} else {
		lookupDir = filepath.Clean(filepath.Join(sc.localCwd, dir))
	}

	entries, err := os.ReadDir(lookupDir)
	if err != nil {
		return nil
	}

	var matches []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), namePrefix) {
			candidate := dir + e.Name()
			if e.IsDir() {
				candidate += "/"
			}
			matches = append(matches, candidate)
		}
	}
	return matches
}

// parseLineForCompletion splits a line into completed tokens and the current
// partial token being typed. Tokens are whitespace-separated.
func parseLineForCompletion(line string) (tokens []string, current string) {
	// If line ends with whitespace, cursor is at start of a new token.
	if len(line) == 0 {
		return nil, ""
	}

	if line[len(line)-1] == ' ' || line[len(line)-1] == '\t' {
		fields := strings.Fields(line)
		return fields, ""
	}

	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil, ""
	}
	return fields[:len(fields)-1], fields[len(fields)-1]
}

// formatCandidates converts full candidate strings into readline-compatible
// suffix completions. The readline library expects the suffix to append after
// the shared prefix of length `length`.
func formatCandidates(candidates []string, prefix string) ([][]rune, int) {
	if len(candidates) == 0 {
		return nil, 0
	}

	// Find the last word boundary in prefix — readline expects offset from
	// the start of the word being completed, not the whole line.
	// We strip the prefix from each candidate to return only the suffix.
	var result [][]rune
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			suffix := c[len(prefix):]
			// Add a trailing space for non-directory completions.
			if !strings.HasSuffix(suffix, "/") {
				suffix += " "
			}
			result = append(result, []rune(suffix))
		}
	}

	// length = number of shared characters to go back.
	// We've already stripped the prefix, so length is the prefix length itself.
	// But readline's "length" means how many chars to go back from pos,
	// i.e., the length of the partial word.
	lastSpace := strings.LastIndexFunc(prefix, unicode.IsSpace)
	prefixLen := len(prefix)
	if lastSpace >= 0 {
		prefixLen = len(prefix) - lastSpace - 1
	}

	return result, prefixLen
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./cmd/bitfs/ -run TestComplete -v -count=1`
Expected: All 13 tests pass.

**Step 5: Commit**

```bash
git add cmd/bitfs/completer.go cmd/bitfs/completer_test.go
git commit -m "feat(shell): add context-aware tab completer with tests"
```

---

### Task 3: Replace `bufio.Scanner` with `ergochat/readline` in `cmd_shell.go`

**Files:**
- Modify: `cmd/bitfs/cmd_shell.go`

**Step 1: Write the updated `runShell` function**

Replace the entire content of `cmd/bitfs/cmd_shell.go` with:

```go
// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ergochat/readline"

	"github.com/tongxiaofeng/bitfs/internal/engine"
	"github.com/tongxiaofeng/libbitfs/config"
)

// shellCommands is the list of all shell command names for tab completion.
var shellCommands = []string{
	"ls", "cd", "lcd", "pwd", "mkdir", "put", "rm", "mv", "cp",
	"link", "sell", "encrypt", "help", "quit", "exit",
}

// runShell handles the "bitfs shell" command.
// Provides an FTP-style interactive REPL with line editing, history,
// and tab completion (commands + remote/local paths).
func runShell(args []string) int {
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	vault := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	eng, err := engine.New(*dataDir, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer eng.Close()

	vaultIdx, err := eng.ResolveVaultIndex(*vault)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNotFound
	}

	cwd := "/"
	localCwd, _ := os.Getwd()

	completer := &shellCompleter{
		commands: shellCommands,
		state:    eng.State,
		cwd:      cwd,
		localCwd: localCwd,
	}

	historyFile := filepath.Join(*dataDir, "shell_history")
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:          fmt.Sprintf("bitfs:%s> ", cwd),
		HistoryFile:     historyFile,
		HistoryLimit:    500,
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing shell: %v\n", err)
		return exitError
	}
	defer rl.Close()

	fmt.Fprintf(rl.Stdout(), "BitFS Shell (vault %d). Type 'help' for commands, 'quit' to exit.\n", vaultIdx)

	for {
		line, err := rl.ReadLine()
		if err == readline.ErrInterrupt {
			continue // Ctrl-C: cancel current line.
		}
		if err == io.EOF {
			fmt.Fprintln(rl.Stdout(), "Bye.")
			return exitSuccess
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitError
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := parts[0]
		cmdArgs := parts[1:]

		switch cmd {
		case "help":
			shellHelp()
		case "quit", "exit":
			fmt.Println("Bye.")
			return exitSuccess
		case "pwd":
			fmt.Println(cwd)
		case "cd":
			if len(cmdArgs) == 0 {
				cwd = "/"
			} else {
				target := cmdArgs[0]
				if !strings.HasPrefix(target, "/") {
					target = cwd + "/" + target
				}
				target = cleanPath(target)
				cwd = target
			}
			completer.cwd = cwd
			rl.SetPrompt(fmt.Sprintf("bitfs:%s> ", cwd))
		case "lcd":
			if len(cmdArgs) == 0 {
				fmt.Println(localCwd)
			} else {
				target := cmdArgs[0]
				if !filepath.IsAbs(target) {
					target = filepath.Join(localCwd, target)
				}
				target = filepath.Clean(target)
				info, statErr := os.Stat(target)
				if statErr != nil || !info.IsDir() {
					fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", target)
					continue
				}
				localCwd = target
				completer.localCwd = localCwd
				fmt.Printf("Local directory: %s\n", localCwd)
			}
		case "ls":
			dir := cwd
			if len(cmdArgs) > 0 {
				dir = resolvePath(cwd, cmdArgs[0])
			}
			shellLs(eng, dir)
		case "mkdir":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: mkdir <path>")
				continue
			}
			path := resolvePath(cwd, cmdArgs[0])
			result, mkErr := eng.Mkdir(&engine.MkdirOpts{VaultIndex: vaultIdx, Path: path})
			if mkErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", mkErr)
			} else {
				fmt.Println(result.Message)
			}
		case "put":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: put <local-file> <remote-path>")
				continue
			}
			localFile := cmdArgs[0]
			if !filepath.IsAbs(localFile) {
				localFile = filepath.Join(localCwd, localFile)
			}
			remotePath := resolvePath(cwd, cmdArgs[1])
			result, putErr := eng.PutFile(&engine.PutOpts{
				VaultIndex: vaultIdx,
				LocalFile:  localFile,
				RemotePath: remotePath,
				Access:     "free",
			})
			if putErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", putErr)
			} else {
				fmt.Println(result.Message)
			}
		case "rm":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: rm <path>")
				continue
			}
			path := resolvePath(cwd, cmdArgs[0])
			result, rmErr := eng.Remove(&engine.RemoveOpts{VaultIndex: vaultIdx, Path: path})
			if rmErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", rmErr)
			} else {
				fmt.Println(result.Message)
			}
		case "mv":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: mv <src> <dst>")
				continue
			}
			result, mvErr := eng.Move(&engine.MoveOpts{
				VaultIndex: vaultIdx,
				SrcPath:    resolvePath(cwd, cmdArgs[0]),
				DstPath:    resolvePath(cwd, cmdArgs[1]),
			})
			if mvErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", mvErr)
			} else {
				fmt.Println(result.Message)
			}
		case "cp":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: cp <src> <dst>")
				continue
			}
			result, cpErr := eng.Copy(&engine.CopyOpts{
				VaultIndex: vaultIdx,
				SrcPath:    resolvePath(cwd, cmdArgs[0]),
				DstPath:    resolvePath(cwd, cmdArgs[1]),
			})
			if cpErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", cpErr)
			} else {
				fmt.Println(result.Message)
			}
		case "link":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: link <target> <link-path> [--soft]")
				continue
			}
			soft := len(cmdArgs) > 2 && cmdArgs[2] == "--soft"
			result, lnErr := eng.Link(&engine.LinkOpts{
				VaultIndex: vaultIdx,
				TargetPath: resolvePath(cwd, cmdArgs[0]),
				LinkPath:   resolvePath(cwd, cmdArgs[1]),
				Soft:       soft,
			})
			if lnErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", lnErr)
			} else {
				fmt.Println(result.Message)
			}
		case "sell":
			if len(cmdArgs) < 2 {
				fmt.Println("Usage: sell <path> <price-sats-per-kb>")
				continue
			}
			var price uint64
			_, _ = fmt.Sscanf(cmdArgs[1], "%d", &price)
			if price == 0 {
				fmt.Println("Error: price must be positive")
				continue
			}
			result, sellErr := eng.Sell(&engine.SellOpts{
				VaultIndex: vaultIdx,
				Path:       resolvePath(cwd, cmdArgs[0]),
				PricePerKB: price,
			})
			if sellErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", sellErr)
			} else {
				fmt.Println(result.Message)
			}
		case "encrypt":
			if len(cmdArgs) < 1 {
				fmt.Println("Usage: encrypt <path>")
				continue
			}
			result, encErr := eng.EncryptNode(&engine.EncryptOpts{
				VaultIndex: vaultIdx,
				Path:       resolvePath(cwd, cmdArgs[0]),
			})
			if encErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", encErr)
			} else {
				fmt.Println(result.Message)
			}
		default:
			fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", cmd)
		}
	}
}

func shellHelp() {
	fmt.Println(`Available commands:
  ls [path]                List directory contents
  cd [path]                Change remote directory
  lcd [path]               Change local directory (or print current)
  pwd                      Print remote working directory
  mkdir <path>             Create directory
  put <local> <remote>     Upload file
  rm <path>                Remove file/directory
  mv <src> <dst>           Move/rename
  cp <src> <dst>           Copy file
  link <target> <path>     Create hard link (--soft for symlink)
  sell <path> <price>      Set price (sats/KB)
  encrypt <path>           Encrypt (FREE -> PRIVATE)
  help                     Show this help
  quit                     Exit shell`)
}

func shellLs(eng *engine.Engine, dir string) {
	node := eng.State.FindNodeByPath(dir)
	if node == nil {
		fmt.Printf("Not found: %s\n", dir)
		return
	}
	if node.Type != "dir" {
		fmt.Printf("%s  %s\n", node.Type, dir)
		return
	}
	if len(node.Children) == 0 {
		fmt.Println("(empty)")
		return
	}
	for _, c := range node.Children {
		fmt.Printf("  %s  %s\n", c.Type, c.Name)
	}
}

// resolvePath resolves a path relative to cwd.
func resolvePath(cwd, p string) string {
	if strings.HasPrefix(p, "/") {
		return cleanPath(p)
	}
	return cleanPath(cwd + "/" + p)
}

// cleanPath normalizes a path, removing double slashes and trailing slashes.
func cleanPath(p string) string {
	parts := strings.Split(p, "/")
	var clean []string
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(clean) > 0 {
				clean = clean[:len(clean)-1]
			}
			continue
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "/"
	}
	return "/" + strings.Join(clean, "/")
}
```

**Key changes from original:**
1. `bufio.Scanner` → `readline.NewFromConfig` with history and completer
2. `fmt.Printf("bitfs:%s> ")` prompt → `rl.SetPrompt()` (dynamic on cd)
3. `scanner.Scan()` → `rl.ReadLine()` with `ErrInterrupt`/`io.EOF` handling
4. Added `cp` command case
5. `put` now resolves local file relative to `localCwd`
6. `completer.cwd` and `completer.localCwd` updated on cd/lcd
7. `shellHelp` updated with `cp` entry

**Step 2: Verify build**

Run: `go build ./cmd/bitfs/`
Expected: Clean build.

**Step 3: Run all tests**

Run: `go test ./cmd/bitfs/ -v -count=1`
Expected: All tests pass (existing + new completer tests).

**Step 4: Commit**

```bash
git add cmd/bitfs/cmd_shell.go
git commit -m "feat(shell): replace bufio.Scanner with ergochat/readline

Adds line editing, persistent history (~/.bitfs/shell_history),
context-aware tab completion (commands + remote/local paths),
cp command, Ctrl-C/Ctrl-D handling."
```

---

### Task 4: Add unit tests for `parseLineForCompletion` and `formatCandidates`

**Files:**
- Modify: `cmd/bitfs/completer_test.go`

**Step 1: Append these tests to `completer_test.go`**

```go
func TestParseLineForCompletion_Empty(t *testing.T) {
	tokens, current := parseLineForCompletion("")
	assert.Nil(t, tokens)
	assert.Equal(t, "", current)
}

func TestParseLineForCompletion_SinglePartialToken(t *testing.T) {
	tokens, current := parseLineForCompletion("ls")
	assert.Nil(t, tokens)
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
```

**Step 2: Run tests**

Run: `go test ./cmd/bitfs/ -run "TestParseLine|TestFormatCandidate" -v -count=1`
Expected: All 8 tests pass.

**Step 3: Commit**

```bash
git add cmd/bitfs/completer_test.go
git commit -m "test(shell): add parseLineForCompletion and formatCandidates tests"
```

---

### Task 5: Run full test suite and verify build

**Step 1: Run all tests**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs
go test ./... -count=1
```

Expected: All packages pass, including `cmd/bitfs` with the new completer tests.

**Step 2: Build all binaries**

```bash
go build ./cmd/...
```

Expected: All 6 binaries build cleanly (bitfs, bls, bcat, bget, bstat, btree).

**Step 3: Verify no lint issues (if golangci-lint available)**

```bash
golangci-lint run ./cmd/bitfs/
```

Expected: No new issues.

**Step 4: Final commit if any cleanup was needed**

Only if steps above revealed issues to fix.

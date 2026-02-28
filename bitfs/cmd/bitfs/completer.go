package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/bitfsorg/libbitfs-go/vault"
)

// shellCompleter implements readline.AutoCompleter for the BitFS shell.
// It provides context-aware completion: command names for the first token,
// remote paths for filesystem commands, local paths for lcd/put.
// cacheTTL is how long a directory lookup is cached for tab completion.
const cacheTTL = 500 * time.Millisecond

type shellCompleter struct {
	commands []string
	state    *vault.LocalState
	cwd      string // remote working directory (mutable, updated by shell loop)
	localCwd string // local working directory (mutable, updated by shell loop)

	// Per-directory cache to avoid O(n) FindNodeByPath on every keypress.
	cacheDir    string           // cached directory path
	cacheNode   *vault.NodeState // cached node
	cacheExpiry time.Time        // TTL expiry
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

	case "cat", "get":
		// Remote path completion.
		candidates := sc.completeRemotePath(currentPrefix)
		return formatCandidates(candidates, currentPrefix)

	case "mget":
		// Remote directory path completion.
		candidates := sc.completeRemotePath(currentPrefix)
		return formatCandidates(candidates, currentPrefix)

	case "mput":
		if argIndex <= 1 {
			// First arg: local directory.
			candidates := sc.completeLocalPath(currentPrefix)
			return formatCandidates(candidates, currentPrefix)
		}
		// Second arg: remote path.
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

	// Use cached node if the directory matches and TTL hasn't expired.
	var node *vault.NodeState
	now := time.Now()
	if lookupDir == sc.cacheDir && now.Before(sc.cacheExpiry) {
		node = sc.cacheNode
	} else {
		node = sc.state.FindNodeByPath(lookupDir)
		sc.cacheDir = lookupDir
		sc.cacheNode = node
		sc.cacheExpiry = now.Add(cacheTTL)
	}
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
	switch {
	case filepath.IsAbs(dir):
		lookupDir = filepath.Clean(dir)
	case dir == "":
		lookupDir = sc.localCwd
	default:
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
// suffix completions.
func formatCandidates(candidates []string, prefix string) ([][]rune, int) {
	if len(candidates) == 0 {
		return nil, 0
	}

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

	lastSpace := strings.LastIndexFunc(prefix, unicode.IsSpace)
	prefixLen := len(prefix)
	if lastSpace >= 0 {
		prefixLen = len(prefix) - lastSpace - 1
	}

	return result, prefixLen
}

# Shell Enhancement Design

**Date**: 2026-02-22
**Status**: Approved

## Goal

Replace the `bufio.Scanner`-based REPL in `bitfs shell` with `ergochat/readline` to add line editing, persistent history, and tab completion (commands + paths).

## Decisions

| Decision | Choice |
|----------|--------|
| Readline library | `github.com/ergochat/readline` (active fork of chzyer/readline) |
| History storage | `~/.bitfs/shell_history` (persistent across sessions) |
| Tab completion scope | Command names + remote paths (DAG) + local paths (lcd/put) |
| Missing commands | Add `cp` to shell (already exists in engine) |
| Keymap | Default emacs mode (no vi toggle) |

## Architecture

```
cmd_shell.go (refactor)
  └── readline.Instance
       ├── History      → ~/.bitfs/shell_history
       ├── Completer    → context-aware PrefixCompleter
       │     ├── first token → command name completion
       │     ├── cd/ls/rm/... args → remote path completion (DAG children)
       │     └── lcd/put local arg → local filesystem completion (os.ReadDir)
       └── Prompt       → "bitfs:/path> " (dynamic cwd)
```

## Features

| Feature | Implementation |
|---------|---------------|
| Line editing (← → Home End Del) | readline built-in |
| Command history (↑ ↓) | readline + `~/.bitfs/shell_history` |
| Ctrl-R reverse search | readline built-in |
| Command name completion | Static list: ls/cd/lcd/pwd/mkdir/put/rm/mv/cp/link/sell/encrypt/help/quit |
| Remote path completion | `eng.State.FindNodeByPath(dir)` → filter children by prefix |
| Local path completion | `os.ReadDir` for lcd and put's local-file argument |
| cp command in shell | Call `eng.Copy()` |
| Ctrl-C | Cancel current input line (don't exit) |
| Ctrl-D on empty line | Exit shell |

## Completion Rules

- **First token**: match command names
- **cd/ls/rm/encrypt/sell** args: remote path completion (directories append `/`)
- **put first arg**: local path completion; **put second arg**: remote path completion
- **lcd**: local directory path completion
- **mv/cp/link**: both args → remote path completion

## Out of Scope

- vi/emacs mode switching
- Syntax highlighting
- Multi-line input
- Aliases or variables

## Dependencies

- Add `github.com/ergochat/readline` to `bitfs/go.mod`
- No changes to libbitfs

## Files Changed

- Modify: `cmd/bitfs/cmd_shell.go` — replace Scanner with readline, add completer
- Modify: `go.mod` / `go.sum` — add ergochat/readline

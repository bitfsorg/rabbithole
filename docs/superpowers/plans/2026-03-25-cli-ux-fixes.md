# BitFS CLI UX Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all CLI UX issues found during Docker smoke test from a third-party user perspective.

**Architecture:** All changes are in `bitfs/` (Go CLI). Fixes are independent per-issue. No library changes needed — all issues are in the `cmd/` layer and `internal/banner/`.

**Tech Stack:** Go 1.25.6, standard `flag` package, `golang.org/x/term`

**Source:** Docker smoke test on Debian bookworm-slim (linux/arm64) against v0.0.1.

---

### Task 1: Add `--network` flag to all wallet/vault/file commands

**Problem:** `wallet init` accepts `--network` but all other commands only accept `--datadir`. Users must remember to pass `--datadir /root/.bitfs-testnet` for every command after init.

**Solution:** Add `--network` flag to every command that takes `--datadir`. When `--network` is set but `--datadir` is not explicitly set, call `applyNetworkDefaultDataDir()` to resolve the correct datadir. This is purely additive — `--datadir` still works as before.

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_wallet.go` (runWalletShow, runWalletBalance, runWalletFund)
- Modify: `bitfs/cmd/bitfs/cmd_vault.go` (all 5 functions)
- Modify: `bitfs/cmd/bitfs/cmd_put.go`
- Modify: `bitfs/cmd/bitfs/cmd_get.go`
- Modify: `bitfs/cmd/bitfs/cmd_cat.go`
- Modify: `bitfs/cmd/bitfs/cmd_mkdir.go`
- Modify: `bitfs/cmd/bitfs/cmd_rm.go`
- Modify: `bitfs/cmd/bitfs/cmd_mv.go`
- Modify: `bitfs/cmd/bitfs/cmd_cp.go`
- Modify: `bitfs/cmd/bitfs/cmd_link.go`
- Modify: `bitfs/cmd/bitfs/cmd_sell.go`
- Modify: `bitfs/cmd/bitfs/cmd_encrypt.go`
- Modify: `bitfs/cmd/bitfs/cmd_mget.go`
- Modify: `bitfs/cmd/bitfs/cmd_mput.go`
- Modify: `bitfs/cmd/bitfs/cmd_publish.go`
- Modify: `bitfs/cmd/bitfs/cmd_unpublish.go`
- Modify: `bitfs/cmd/bitfs/cmd_verify.go`
- Modify: `bitfs/cmd/bitfs/cmd_shell.go`
- Modify: `bitfs/cmd/bitfs/cmd_fund.go`
- Modify: `bitfs/cmd/bitfs/vault_export.go`
- Modify: `bitfs/cmd/bitfs/paymail.go`
- Modify: `bitfs/cmd/bitfs/cmd_daemon.go`
- Create: `bitfs/cmd/bitfs/network.go` — helper to add --network flag + resolve datadir
- Test: `bitfs/cmd/bitfs/network_test.go`

**Pattern — extract a reusable helper:**

```go
// network.go
package main

import "flag"

// addNetworkFlag adds --network to a FlagSet and returns the pointer.
// After fs.Parse(), call resolveNetworkDataDir(fs, network, dataDir).
func addNetworkFlag(fs *flag.FlagSet) *string {
    return fs.String("network", "", "BSV network: mainnet, testnet, or regtest (auto-detected from datadir)")
}

// resolveNetworkDataDir applies --network to --datadir when datadir was not
// explicitly set. Call after fs.Parse().
func resolveNetworkDataDir(fs *flag.FlagSet, network *string, dataDir *string) {
    if *network != "" {
        applyNetworkDefaultDataDir(fs, dataDir, *network)
    }
}
```

Then each command adds two lines:

```go
network := addNetworkFlag(fs)
// ... after fs.Parse() ...
resolveNetworkDataDir(fs, network, dataDir)
```

- [ ] **Step 1: Create `network.go` with the helper**
- [ ] **Step 2: Write test `network_test.go`** — verify that `--network testnet` resolves to `~/.bitfs-testnet`, and that explicit `--datadir` takes precedence
- [ ] **Step 3: Run test to verify it fails** — `cd bitfs && go test ./cmd/bitfs/ -run TestResolveNetworkDataDir -v`
- [ ] **Step 4: Add `--network` to all wallet commands** (wallet show, balance, fund)
- [ ] **Step 5: Add `--network` to all vault commands** (create, list, rename, delete, export)
- [ ] **Step 6: Add `--network` to all file commands** (put, get, cat, mkdir, rm, mv, cp, link, mget, mput)
- [ ] **Step 7: Add `--network` to remaining commands** (sell, encrypt, publish, unpublish, verify, shell, paymail *)
- [ ] **Step 8: Run full test suite** — `cd bitfs && go test ./...`
- [ ] **Step 9: Commit** — `feat(cli): add --network flag to all commands`

---

### Task 2: Add confirmation prompt to `vault delete`

**Problem:** `vault delete` immediately deletes without confirmation. The `vault export` command has a confirmation, creating dangerous inconsistency.

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_vault.go:178-213`

- [ ] **Step 1: Add confirmation prompt before deletion**

```go
func runVaultDelete(args []string) int {
    fs := flag.NewFlagSet("vault delete", flag.ContinueOnError)
    dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
    password := fs.String("password", "", "wallet password (for testing)")
    network := addNetworkFlag(fs)
    force := fs.Bool("force", false, "skip confirmation prompt")

    if err := fs.Parse(args); err != nil {
        return exitUsageError
    }
    resolveNetworkDataDir(fs, network, dataDir)

    if fs.NArg() < 1 {
        fmt.Fprintf(os.Stderr, "Usage: bitfs vault delete [--force] <name>\n")
        return exitUsageError
    }

    name := fs.Arg(0)

    if !*force {
        if !promptYesNo(fmt.Sprintf("Delete vault %q? This cannot be undone", name)) {
            fmt.Println("Aborted.")
            return exitSuccess
        }
    }

    // ... rest unchanged
}
```

- [ ] **Step 2: Test manually** — `bitfs vault delete default` should now prompt
- [ ] **Step 3: Commit** — `fix(cli): add confirmation prompt to vault delete`

---

### Task 3: TTY detection for banner/ANSI output

**Problem:** Banner outputs ANSI escape codes unconditionally. When piped or in CI, this produces garbage.

**Files:**
- Modify: `bitfs/internal/banner/banner.go`
- Test: `bitfs/internal/banner/banner_test.go`

- [ ] **Step 1: Write test** — verify `Print()` produces no ANSI when stderr is not a terminal

```go
func TestPrint_NoANSI_WhenNotTerminal(t *testing.T) {
    // Capture output by redirecting stderr to a pipe
    // Verify no \033 escape sequences in output
}
```

- [ ] **Step 2: Add TTY detection to `Print()`**

```go
import "golang.org/x/term"

func Print(version string) {
    if !term.IsTerminal(int(os.Stderr.Fd())) {
        // Plain text fallback — no colors
        fmt.Fprint(os.Stderr, "\n")
        fmt.Fprintln(os.Stderr, bitfsArt)
        fmt.Fprintf(os.Stderr, "  Decentralized Encrypted File System  v%s\n", version)
        fmt.Fprintf(os.Stderr, "  https://bitfs.org\n\n")
        return
    }
    // existing colored output
    fmt.Fprint(os.Stderr, "\n")
    fmt.Fprintln(os.Stderr, applyGradient(bitfsArt))
    fmt.Fprintf(os.Stderr, "%s  Decentralized Encrypted File System%s  %sv%s%s\n", textColor, reset, dimColor, version, reset)
    fmt.Fprintf(os.Stderr, "%s  https://bitfs.org%s\n\n", dimColor, reset)
}
```

Also respect `NO_COLOR` env var (https://no-color.org/):

```go
func useColor() bool {
    if os.Getenv("NO_COLOR") != "" {
        return false
    }
    return term.IsTerminal(int(os.Stderr.Fd()))
}
```

- [ ] **Step 3: Run tests** — `cd bitfs && go test ./internal/banner/ -v`
- [ ] **Step 4: Commit** — `fix(banner): respect TTY detection and NO_COLOR`

---

### Task 4: Clean up error messages for end users

**Problem:** Errors expose internal Go error chains like `vault: ensure root: vault: no fee UTXO with >= 2000 sats; run 'bitfs fund' first: insufficient funds`.

**Solution:** Add a `userError()` helper that extracts the innermost actionable message. Apply to all `fmt.Fprintf(os.Stderr, "Error: %v\n", err)` call sites in cmd/.

**Files:**
- Create: `bitfs/cmd/bitfs/usererror.go`
- Test: `bitfs/cmd/bitfs/usererror_test.go`
- Modify: all `cmd_*.go` files that print errors

- [ ] **Step 1: Write the helper and test**

```go
// usererror.go
package main

import "strings"

// userMessage returns a clean, user-facing error message.
// It unwraps known sentinel errors to their actionable message,
// and falls back to the innermost error in the chain.
func userMessage(err error) string {
    if err == nil {
        return ""
    }

    // Use the full message but trim redundant prefixes from nested wrapping.
    msg := err.Error()

    // Remove repeated "context: context: " prefixes from nested Go error wrapping.
    for _, prefix := range []string{"vault: vault: ", "engine: engine: "} {
        for strings.HasPrefix(msg, prefix) {
            msg = msg[len(prefix)-len(prefix[strings.Index(prefix, ": ")+2:]):]
        }
    }

    // Collapse "vault: ensure root: vault: " → "vault: "
    msg = strings.Replace(msg, "vault: ensure root: vault: ", "vault: ", 1)

    return msg
}
```

- [ ] **Step 2: Write tests for known error patterns**
- [ ] **Step 3: Apply `userMessage()` to error output in cmd files** — grep for `"Error: %v\n", err` and replace with `"Error: %s\n", userMessage(err)` where the error comes from vault/engine operations
- [ ] **Step 4: Run full test suite** — `cd bitfs && go test ./...`
- [ ] **Step 5: Commit** — `fix(cli): clean up user-facing error messages`

---

### Task 5: Improve b-tools help output

**Problem:** b-tools show bare `Usage of bls:` from Go flag package with no description, exit code 2 on --help, and output to stderr.

**Solution:** Add `--help`/`-h` handling before `fs.Parse()`, output descriptive help to stdout with exit 0.

**Files:**
- Modify: `bitfs/cmd/bls/main.go`
- Modify: `bitfs/cmd/bcat/main.go`
- Modify: `bitfs/cmd/bget/main.go`
- Modify: `bitfs/cmd/bmget/main.go`
- Modify: `bitfs/cmd/bstat/main.go`
- Modify: `bitfs/cmd/btree/main.go`

**Pattern for each b-tool:**

```go
func run(args []string, stdout, stderr io.Writer) int {
    // Handle --help before flag parsing (so -h doesn't trigger flag error).
    for _, a := range args {
        if a == "--help" || a == "-h" || a == "help" {
            printUsage(stdout)
            return 0
        }
        if a == "--" {
            break
        }
    }

    fs := flag.NewFlagSet("bls", flag.ContinueOnError)
    fs.SetOutput(stderr)
    // ... existing flags ...
```

And add a `printUsage` function with proper description:

```go
func printUsage(w io.Writer) {
    banner.PrintTo(w, "0.1.0")  // new function that writes to arbitrary writer
    fmt.Fprintf(w, `bls - List directory contents in a BitFS filesystem

Usage:
  bls [options] <bitfs-uri>

Options:
  --json        JSON output
  --long, -l    Detailed listing (type, access, size, name)
  --host URL    Daemon URL override
  --timeout D   Request timeout (e.g. 10s, 1m)
  --keyword S   Filter children by name substring
  --no-cache    Skip metadata cache
  --offline     Cache-only mode

Examples:
  bls bitfs://example.com/docs/
  bls bitfs://alice@example.com/docs/
  bls --long bitfs://02abc.../docs/ --host http://localhost:8080
`)
}
```

- [ ] **Step 1: Add `PrintTo(w io.Writer, version string)` to banner package** — renders to any writer, respects TTY check on stderr only
- [ ] **Step 2: Add help handler + descriptive usage to `bls`**
- [ ] **Step 3: Add help handler + descriptive usage to `bcat`**
- [ ] **Step 4: Add help handler + descriptive usage to `bget`**
- [ ] **Step 5: Add help handler + descriptive usage to `bmget`**
- [ ] **Step 6: Add help handler + descriptive usage to `bstat`**
- [ ] **Step 7: Add help handler + descriptive usage to `btree`**
- [ ] **Step 8: Run all tests** — `cd bitfs && go test ./...`
- [ ] **Step 9: Commit** — `fix(b-tools): add descriptive help output with exit 0`

---

### Task 6: Fix `bitfs` no-args exit code and wallet-init hint

**Problem:** `bitfs` with no args returns exit code 2 (usage error), but showing help is not an error. Also, `wallet already exists` suggests `Remove <dir>` which is dangerously destructive.

**Files:**
- Modify: `bitfs/cmd/bitfs/main.go:38-42`
- Modify: `bitfs/cmd/bitfs/cmd_wallet.go:107-111`

- [ ] **Step 1: Change no-args exit code to 0**

```go
func run(args []string) int {
    if len(args) == 0 {
        printUsage()
        return exitSuccess  // was exitUsageError
    }
```

- [ ] **Step 2: Improve wallet-already-exists message**

```go
// Replace:
fmt.Fprintf(os.Stderr, "Remove %s to reinitialize.\n", *dataDir)
// With:
fmt.Fprintf(os.Stderr, "Back up your mnemonic, then remove %s to reinitialize.\n", *dataDir)
```

- [ ] **Step 3: Improve testnet fund hint** — replace `make testnet` with useful instructions

In `cmd_fund.go`, find the testnet/regtest-specific text and replace both `make testnet` (line ~176) and `make regtest` (line ~167) with user-friendly instructions:

```
Before:
  make testnet / make regtest

After:
  Get testnet BSV from a faucet or your own testnet node
  (For regtest: use bitcoin-cli -regtest sendtoaddress)
```

- [ ] **Step 4: Run tests** — `cd bitfs && go test ./...`
- [ ] **Step 5: Commit** — `fix(cli): improve exit codes and user-facing messages`

---

## Summary

| Task | Issue | Priority |
|------|-------|----------|
| 1 | `--network` flag on all commands | P1 (most impactful) |
| 2 | `vault delete` confirmation | P1 (safety) |
| 3 | TTY detection for banner | P2 |
| 4 | Clean error messages | P2 |
| 5 | b-tools help output | P2 |
| 6 | Exit codes + message fixes | P3 |

Total: 6 tasks, ~40 steps. All changes are in `bitfs/` directory.

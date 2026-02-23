# Spec Completion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bring code into full spec compliance — 14 missing features, 6 interface mismatches, 19 missing flags, 1 empty module, plus lock/unlock session management from design doc T23.

**Architecture:** 6 dependency-ordered tiers. Library foundations first, then session management, CLI commands, global flags, b-tools flags, and daemon endpoints. Each task is self-contained with clear files, steps, and test expectations.

**Tech Stack:** Go 1.25.6, `libbitfs/*`, `bitfs/internal/*`, `github.com/stretchr/testify`

**Branch:** Create new branch `feat/spec-completion` from current `main`.

---

## Tier 0: Library Foundations

### Task 1: Align tx function signatures to spec

The spec defines `BuildOPReturn`, `ParseOPReturn`, `P2PKHScript`. The code has `BuildOPReturnData`, `ParseOPReturnData`, `BuildP2PKHScript` with different parameter/return types.

**Files:**
- Modify: `libbitfs/tx/opreturn.go`
- Modify: `libbitfs/tx/sign.go`
- Modify: all callers (grep for old names)

**Step 1: Rename and update `BuildOPReturnData` → `BuildOPReturn`**

In `libbitfs/tx/opreturn.go`, rename the function. The spec signature is `BuildOPReturn(pNode *ec.PublicKey, parentTxID []byte, payload []byte) (*script.Script, error)`. Currently returns `([][]byte, error)`.

Change the function to build and return a full `*script.Script` instead of raw push data:

```go
import "github.com/bsv-blockchain/go-sdk/script"

// BuildOPReturn builds a Metanet OP_RETURN script.
func BuildOPReturn(pNode *ec.PublicKey, parentTxID []byte, payload []byte) (*script.Script, error) {
    // validation (same as current)
    if pNode == nil { return nil, ErrNilPublicKey }
    pubKeyBytes := pNode.Compressed()
    if len(pubKeyBytes) != CompressedPubKeyLen { return nil, fmt.Errorf(...) }
    if len(parentTxID) != 0 && len(parentTxID) != TxIDLen { return nil, fmt.Errorf(...) }

    // Build script: OP_FALSE OP_RETURN <meta_flag> <pubkey> <parent_txid> <payload>
    s := &script.Script{}
    _ = s.AppendOpcodes(script.OpFALSE, script.OpRETURN)
    _ = s.AppendPushData(MetaFlagBytes)
    _ = s.AppendPushData(pubKeyBytes)
    if len(parentTxID) > 0 {
        _ = s.AppendPushData(parentTxID)
    } else {
        _ = s.AppendPushData([]byte{})
    }
    if len(payload) > 0 {
        _ = s.AppendPushData(payload)
    }
    return s, nil
}
```

Keep the old function as a deprecated alias temporarily if needed, or just rename all callers.

**Step 2: Rename `ParseOPReturnData` → `ParseOPReturn`**

Change signature to accept `*script.Script` and return `*ec.PublicKey`:

```go
// ParseOPReturn parses a Metanet OP_RETURN script.
func ParseOPReturn(s *script.Script) (*ec.PublicKey, []byte, []byte, error) {
    // Decode script to get push data chunks
    pushes, err := DecodePushData(s)
    if err != nil { return nil, nil, nil, err }
    // existing validation logic...
    // parse pNode as *ec.PublicKey instead of raw bytes
    pubKey, err := ec.PublicKeyFromBytes(pushes[1])
    if err != nil { return nil, nil, nil, fmt.Errorf("invalid pNode pubkey: %w", err) }
    return pubKey, pushes[2], payload, nil
}

// DecodePushData extracts push data elements from a script (helper).
func DecodePushData(s *script.Script) ([][]byte, error) {
    // Use script.DecodeScript() or iterate chunks
    // ...
}
```

**Step 3: Rename `BuildP2PKHScript` → `P2PKHScript`**

In `libbitfs/tx/sign.go`, rename and change return type to `*script.Script`:

```go
func P2PKHScript(pubKey *ec.PublicKey) (*script.Script, error) {
    addr, err := script.NewAddressFromPublicKey(pubKey, true)
    if err != nil { return nil, err }
    return script.NewP2PKHFromAddress(addr)
}
```

**Step 4: Update all callers**

Search with `grep -r "BuildOPReturnData\|ParseOPReturnData\|BuildP2PKHScript"` across both `libbitfs/` and `bitfs/`. Update each call site to use new names and handle the new return types.

Key callers:
- `libbitfs/tx/sign.go` — `BuildUnsignedCreateRootTx`, `BuildUnsignedCreateChildTx`, `BuildUnsignedSelfUpdateTx` all call `BuildOPReturnData` → now call `BuildOPReturn`
- `libbitfs/tx/sign.go` — `buildOPReturnScript` helper may become redundant since `BuildOPReturn` now returns `*script.Script` directly
- `bitfs/internal/engine/txbuild.go` — may call old names

**Step 5: Update tests**

Fix all tests in `libbitfs/tx/opreturn_test.go`, `sign_test.go` etc.

**Step 6: Run tests**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./tx/ -v -count=1
cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1
```

**Step 7: Commit**

```bash
git add -A && git commit -m "refactor: align tx function signatures to spec (BuildOPReturn, ParseOPReturn, P2PKHScript)"
```

---

### Task 2: Add OnChainRef type to storage

**Files:**
- Create: `libbitfs/storage/onchain.go`
- Create: `libbitfs/storage/onchain_test.go`

**Step 1: Create the type and methods**

```go
// libbitfs/storage/onchain.go
package storage

// OnChainRef tracks content stored on-chain in data transactions.
type OnChainRef struct {
    KeyHash      []byte   `json:"key_hash"`
    ContentTxIDs [][]byte `json:"content_txids"`
    TotalChunks  uint32   `json:"total_chunks"`
}

// IsComplete returns true if all chunks have been stored.
func (r *OnChainRef) IsComplete() bool {
    return uint32(len(r.ContentTxIDs)) == r.TotalChunks
}

// AddChunk records a data transaction for a content chunk.
func (r *OnChainRef) AddChunk(txID []byte) {
    r.ContentTxIDs = append(r.ContentTxIDs, txID)
}
```

**Step 2: Write tests**

```go
func TestOnChainRef_IsComplete(t *testing.T) { ... }
func TestOnChainRef_AddChunk(t *testing.T) { ... }
```

**Step 3: Run tests and commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./storage/ -v -count=1
git add storage/onchain.go storage/onchain_test.go && git commit -m "feat: add OnChainRef type for on-chain content tracking"
```

---

### Task 3: Add DeriveKeyCacheKey to wallet

**Files:**
- Modify: `libbitfs/wallet/hd.go`
- Modify: `libbitfs/wallet/hd_test.go`

**Step 1: Add the function**

In `hd.go`, add after `DeriveNodePubKey`:

```go
// DeriveKeyCacheKey derives the encryption key for cached AES keys.
// Uses path m/44'/236'/0'/2/0 (fee account, chain=2 for cache).
func (w *Wallet) DeriveKeyCacheKey() (*KeyPair, error) {
    const cacheChain = 2
    const cacheIndex = 0
    acct, err := w.deriveAccount(FeeAccount)
    if err != nil {
        return nil, fmt.Errorf("derive fee account: %w", err)
    }
    chainKey, err := acct.Child(cacheChain)
    if err != nil {
        return nil, fmt.Errorf("derive cache chain: %w", err)
    }
    idxKey, err := chainKey.Child(cacheIndex)
    if err != nil {
        return nil, fmt.Errorf("derive cache index: %w", err)
    }
    path := fmt.Sprintf("m/%d'/%d'/%d'/%d/%d", PurposeBIP44, CoinTypeBitFS, FeeAccount, cacheChain, cacheIndex)
    return extKeyToKeyPair(idxKey, path)
}
```

**Step 2: Write tests**

Test deterministic derivation, non-nil keys, path string correctness.

**Step 3: Run tests and commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./wallet/ -v -run TestDeriveKeyCacheKey -count=1
git add wallet/hd.go wallet/hd_test.go && git commit -m "feat: add DeriveKeyCacheKey for AES key cache encryption"
```

---

### Task 4: Create revshare module skeleton

**Files:**
- Create: `libbitfs/revshare/revshare.go`
- Create: `libbitfs/revshare/revshare_test.go`

**Step 1: Create types and validation**

```go
// libbitfs/revshare/revshare.go
package revshare

import "fmt"

// Split defines a single revenue share recipient.
type Split struct {
    Address string `json:"address"`
    Share   uint32 `json:"share"` // basis points (1/10000)
}

// Plan defines how revenue is distributed.
type Plan struct {
    Splits []Split `json:"splits"`
}

// Output represents a calculated payment output.
type Output struct {
    Address string
    Amount  uint64
}

const MaxBasisPoints = 10000

// Validate checks that a revenue sharing plan is valid.
func Validate(p *Plan) error {
    if p == nil || len(p.Splits) == 0 {
        return fmt.Errorf("revshare: plan has no splits")
    }
    var total uint32
    for _, s := range p.Splits {
        if s.Address == "" {
            return fmt.Errorf("revshare: split has empty address")
        }
        if s.Share == 0 {
            return fmt.Errorf("revshare: split for %s has zero share", s.Address)
        }
        total += s.Share
    }
    if total != MaxBasisPoints {
        return fmt.Errorf("revshare: shares sum to %d, expected %d", total, MaxBasisPoints)
    }
    return nil
}

// CalculateOutputs calculates payment outputs for a given total.
func CalculateOutputs(p *Plan, totalSats uint64) []Output {
    outputs := make([]Output, len(p.Splits))
    var distributed uint64
    for i, s := range p.Splits {
        if i == len(p.Splits)-1 {
            // Last split gets remainder to avoid rounding errors.
            outputs[i] = Output{Address: s.Address, Amount: totalSats - distributed}
        } else {
            amount := totalSats * uint64(s.Share) / MaxBasisPoints
            outputs[i] = Output{Address: s.Address, Amount: amount}
            distributed += amount
        }
    }
    return outputs
}
```

**Step 2: Write tests — validate edge cases, rounding**

**Step 3: Run tests and commit**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./revshare/ -v -count=1
git add revshare/ && git commit -m "feat: add revshare module skeleton with validation and output calculation"
```

---

## Tier 1: Lock/Unlock Session Management

### Task 5: Session file types and secure delete

**Files:**
- Create: `bitfs/internal/engine/session.go`
- Create: `bitfs/internal/engine/session_test.go`

**Step 1: Define session types**

```go
package engine

import (
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"
    "time"
)

// SessionFile represents a persistent unlock session.
type SessionFile struct {
    CreatedAt     int64  `json:"created_at"`
    ExpiresAt     int64  `json:"expires_at"` // 0 = never expires
    EncryptionKey string `json:"encryption_key"` // hex-encoded derived key
    Vault         string `json:"vault"`
}

// IsExpired checks if the session has expired.
func (s *SessionFile) IsExpired() bool {
    if s.ExpiresAt == 0 {
        return false
    }
    return time.Now().Unix() > s.ExpiresAt
}

// SessionPath returns the path to the session file.
func SessionPath(dataDir string) string {
    return filepath.Join(dataDir, "session.json")
}

// WriteSession writes a session file with 0600 permissions.
func WriteSession(dataDir string, session *SessionFile) error {
    data, err := json.MarshalIndent(session, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal session: %w", err)
    }
    path := SessionPath(dataDir)
    return os.WriteFile(path, data, 0600)
}

// ReadSession reads and validates a session file. Returns nil if missing or expired.
func ReadSession(dataDir string) (*SessionFile, error) {
    path := SessionPath(dataDir)
    data, err := os.ReadFile(path)
    if os.IsNotExist(err) {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("read session: %w", err)
    }
    var session SessionFile
    if err := json.Unmarshal(data, &session); err != nil {
        // Corrupted file — delete it
        _ = SecureDelete(path)
        return nil, nil
    }
    if session.IsExpired() {
        _ = SecureDelete(path)
        return nil, nil
    }
    return &session, nil
}

// SecureDelete overwrites a file with zeros 3 times, fsyncs, then deletes.
func SecureDelete(path string) error {
    info, err := os.Stat(path)
    if os.IsNotExist(err) {
        return nil // idempotent
    }
    if err != nil {
        return err
    }
    size := info.Size()
    zeros := make([]byte, size)
    for i := 0; i < 3; i++ {
        f, err := os.OpenFile(path, os.O_WRONLY, 0)
        if err != nil {
            return err
        }
        if _, err := f.Write(zeros); err != nil {
            _ = f.Close()
            return err
        }
        if err := f.Sync(); err != nil {
            _ = f.Close()
            return err
        }
        _ = f.Close()
    }
    return os.Remove(path)
}
```

**Step 2: Write tests** — session file round-trip, expiry detection, corrupted file cleanup, secure delete (verify file removed), idempotent delete of non-existent file, file permissions check (0600).

**Step 3: Run tests and commit**

---

### Task 6: Implement `bitfs unlock` command

**Files:**
- Create: `bitfs/cmd/bitfs/cmd_unlock.go`
- Modify: `bitfs/cmd/bitfs/main.go` (add case "unlock")

**Step 1: Implement unlock**

```go
func runUnlock(args []string) int {
    fs := flag.NewFlagSet("unlock", flag.ExitOnError)
    duration := fs.String("duration", "", "session duration (e.g. 30m, 2h). Empty = never expires")
    dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
    password := fs.String("password", "", "wallet password")
    _ = fs.Parse(args)

    // Load wallet to verify password is correct.
    w, _, err := loadWalletFromDataDir(*dataDir, *password)
    if err != nil {
        fmt.Fprintf(os.Stderr, "unlock: %v\n", err)
        return exitWalletError
    }

    // Derive the cache key to use as session encryption key.
    cacheKP, err := w.DeriveKeyCacheKey()
    if err != nil {
        fmt.Fprintf(os.Stderr, "unlock: derive cache key: %v\n", err)
        return exitError
    }
    encKeyHex := hex.EncodeToString(cacheKP.PrivateKey.Serialise())

    now := time.Now().Unix()
    var expiresAt int64
    if *duration != "" {
        dur, err := time.ParseDuration(*duration)
        if err != nil {
            fmt.Fprintf(os.Stderr, "unlock: invalid duration %q: %v\n", *duration, err)
            return exitUsageError
        }
        expiresAt = now + int64(dur.Seconds())
    }

    session := &engine.SessionFile{
        CreatedAt:     now,
        ExpiresAt:     expiresAt,
        EncryptionKey: encKeyHex,
        Vault:         "", // uses active vault
    }

    if err := engine.WriteSession(*dataDir, session); err != nil {
        fmt.Fprintf(os.Stderr, "unlock: %v\n", err)
        return exitError
    }

    if expiresAt > 0 {
        fmt.Printf("Wallet unlocked for %s\n", *duration)
    } else {
        fmt.Println("Wallet unlocked (no expiry)")
    }
    return exitSuccess
}
```

**Step 2: Add to main.go switch**: `case "unlock": return runUnlock(cmdArgs)`

**Step 3: Run tests and commit**

---

### Task 7: Implement `bitfs lock` command

**Files:**
- Create: `bitfs/cmd/bitfs/cmd_lock.go`
- Modify: `bitfs/cmd/bitfs/main.go` (add case "lock")

**Step 1: Implement lock**

```go
func runLock(args []string) int {
    fs := flag.NewFlagSet("lock", flag.ExitOnError)
    dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
    _ = fs.Parse(args)

    path := engine.SessionPath(*dataDir)
    if err := engine.SecureDelete(path); err != nil {
        fmt.Fprintf(os.Stderr, "lock: %v\n", err)
        return exitError
    }
    fmt.Println("Wallet locked")
    return exitSuccess
}
```

**Step 2: Add to main.go switch**: `case "lock": return runLock(cmdArgs)`

**Step 3: Run tests and commit**

---

### Task 8: Integrate key resolution chain into engine

**Files:**
- Modify: `bitfs/internal/engine/engine.go`
- Modify: `bitfs/cmd/bitfs/cmd_wallet.go` (shared helper)

**Step 1: Add session-aware wallet loading**

Add to `cmd_wallet.go` a helper that checks session before prompting password:

```go
// loadWalletWithSession tries: 1) session file 2) password prompt/flag.
func loadWalletWithSession(dataDir, passwordFlag string) (*wallet.Wallet, *wallet.WalletState, error) {
    // Try session file first.
    session, err := engine.ReadSession(dataDir)
    if err == nil && session != nil {
        // Session exists and is valid — use its encryption key to load wallet.
        return loadWalletFromSessionKey(dataDir, session.EncryptionKey)
    }
    // Fall back to password.
    return loadWalletFromDataDir(dataDir, passwordFlag)
}
```

Update all commands that call `loadWalletFromDataDir` to use `loadWalletWithSession` instead (put, mkdir, rm, mv, cp, link, sell, encrypt, publish, unpublish).

**Step 2: Run all tests and commit**

---

## Tier 2: Missing CLI Commands

### Task 9: Implement `bitfs init`

**Files:**
- Create: `bitfs/cmd/bitfs/cmd_init.go`
- Modify: `bitfs/cmd/bitfs/main.go`

**Step 1: Implement init as wrapper**

```go
func runInit(args []string) int {
    // Parse flags same as wallet init.
    fs := flag.NewFlagSet("init", flag.ExitOnError)
    network := fs.String("network", "mainnet", "network (mainnet/testnet/regtest)")
    dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
    password := fs.String("password", "", "wallet password")
    words := fs.Int("words", 12, "mnemonic word count (12 or 24)")
    _ = fs.Parse(args)

    // Delegate to wallet init + vault create.
    walletArgs := []string{"init", "--network", *network, "--datadir", *dataDir, "--words", fmt.Sprint(*words)}
    if *password != "" {
        walletArgs = append(walletArgs, "--password", *password)
    }
    code := runWallet(walletArgs)
    if code != exitSuccess {
        return code
    }
    fmt.Println("BitFS initialized successfully.")
    return exitSuccess
}
```

Note: `wallet init` already creates a default vault, so `init` is essentially an alias.

**Step 2: Add to main.go switch**: `case "init": return runInit(cmdArgs)`

**Step 3: Commit**

---

### Task 10: Implement `bitfs vault use` and `bitfs vault info`

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_vault.go`

**Step 1: Add `vault use`**

Write active vault name to `~/.bitfs/active_vault`:

```go
func runVaultUse(args []string) int {
    fs := flag.NewFlagSet("vault use", flag.ExitOnError)
    dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
    password := fs.String("password", "", "wallet password")
    _ = fs.Parse(args)
    if fs.NArg() < 1 { usage error }

    name := fs.Arg(0)
    // Verify vault exists.
    _, state, err := loadWalletFromDataDir(*dataDir, *password)
    // check vault exists in state...

    // Write active vault file.
    path := filepath.Join(*dataDir, "active_vault")
    os.WriteFile(path, []byte(name), 0600)
    fmt.Printf("Active vault set to %q\n", name)
    return exitSuccess
}
```

Add `GetActiveVault(dataDir string) string` helper that reads `active_vault` file (returns "default" if absent).

**Step 2: Add `vault info`**

```go
func runVaultInfo(args []string) int {
    // Parse flags, load wallet, get vault by name (or active vault if no arg).
    // Print: name, account index, BIP44 path, root txid (or "unpublished"),
    //   published status, children count.
}
```

**Step 3: Update switch in `runVault`**: add `case "use":` and `case "info":`

**Step 4: Update all commands to use `GetActiveVault` when no `--vault` flag is specified.**

**Step 5: Run tests and commit**

---

### Task 11: Implement `bitfs decrypt`

**Files:**
- Create: `bitfs/cmd/bitfs/cmd_decrypt.go`
- Modify: `bitfs/cmd/bitfs/main.go`
- Modify: `bitfs/internal/engine/encrypt.go` (add DecryptNode method)

**Step 1: Add `DecryptNode` to engine**

Mirror of `EncryptNode` but sets AccessFree instead of AccessPrivate:

```go
type DecryptOpts struct {
    VaultIndex uint32
    Path       string
}

func (e *Engine) DecryptNode(opts *DecryptOpts) (*Result, error) {
    // Find node, verify it's currently Private.
    // Re-encrypt content with AccessFree.
    // Build SelfUpdate tx with new access mode.
    // Return result with tx hex.
}
```

**Step 2: Create CLI command**

Same pattern as `cmd_encrypt.go` but calls `eng.DecryptNode(...)`.

**Step 3: Add to main.go switch**: `case "decrypt": return runDecrypt(cmdArgs)`

**Step 4: Write tests and commit**

---

### Task 12: Implement `bitfs rmdir`

**Files:**
- Create: `bitfs/cmd/bitfs/cmd_rmdir.go`
- Modify: `bitfs/cmd/bitfs/main.go`

**Step 1: Implement rmdir**

```go
func runRmdir(args []string) int {
    // Parse flags, load engine.
    // Find node, verify it's a directory.
    // Error if children non-empty.
    // Call eng.Remove (same as rm but with pre-check).
}
```

**Step 2: Add to main.go switch**: `case "rmdir": return runRmdir(cmdArgs)`

**Step 3: Write tests and commit**

---

### Task 13: Implement `bitfs sales`

**Files:**
- Create: `bitfs/cmd/bitfs/cmd_sales.go`
- Modify: `bitfs/cmd/bitfs/main.go`
- Modify: `bitfs/internal/engine/state.go` (add SaleRecord type if not present)

**Step 1: Add SaleRecord to state**

```go
type SaleRecord struct {
    Path      string    `json:"path"`
    Buyer     string    `json:"buyer"`     // buyer pubkey hex
    Amount    uint64    `json:"amount"`    // satoshis
    Timestamp int64     `json:"timestamp"`
    TxID      string    `json:"txid"`
}
```

Add `Sales []SaleRecord` to `LocalState`.

**Step 2: Implement CLI**

```go
func runSales(args []string) int {
    // Parse flags, load engine.
    // Filter by path if provided.
    // Print sales records in table format.
}
```

**Step 3: Add to main.go switch**: `case "sales": return runSales(cmdArgs)`

**Step 4: Write tests and commit**

---

### Task 14: Implement `bitfs wallet restore`

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_wallet.go`

**Step 1: Add restore subcommand**

```go
func runWalletRestore(args []string) int {
    fs := flag.NewFlagSet("wallet restore", flag.ExitOnError)
    network := fs.String("network", "mainnet", "network")
    dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
    password := fs.String("password", "", "new wallet password")
    _ = fs.Parse(args)

    // Read mnemonic from stdin (secure — not command line).
    fmt.Print("Enter mnemonic phrase: ")
    // Use terminal.ReadPassword or bufio.Scanner
    // Validate mnemonic, derive seed, encrypt with new password.
    // Write wallet.enc and state.json.
    // Create default vault.
}
```

**Step 2: Add to wallet subcommand switch**: `case "restore": return runWalletRestore(subArgs)`

**Step 3: Write tests and commit**

---

### Task 15: Rename `wallet show` → `wallet info`

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_wallet.go`

**Step 1:** Rename `runWalletShow` → `runWalletInfo`. Update the switch:

```go
case "info", "show": return runWalletInfo(subArgs) // "show" kept as alias
```

**Step 2: Commit**

---

### Task 16: Implement `bitfs daemon status` and `daemon config`

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_daemon.go`

**Step 1: Add `daemon status`**

```go
func runDaemonStatus(args []string) int {
    // Read PID file from datadir.
    // Check if process is alive (os.FindProcess + signal 0).
    // Try GET /_bitfs/health on configured listen address.
    // Print: running/stopped, PID, uptime, listen address.
}
```

**Step 2: Add `daemon config`**

```go
func runDaemonConfig(args []string) int {
    // Read and print ~/.bitfs/config.toml.
    // If file doesn't exist, print defaults.
}
```

**Step 3: Add to daemon switch**: `case "status":` and `case "config":`

**Step 4: Commit**

---

### Task 17: Implement `bitfs sell --recursive`

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_sell.go`
- Modify: `bitfs/internal/engine/sell.go` (add recursive support)

**Step 1: Add `--recursive` flag to cmd_sell.go**

```go
recursive := fs.Bool("recursive", false, "apply pricing to all files in directory")
```

**Step 2: Add `SellRecursive` to engine**

```go
func (e *Engine) SellRecursive(opts *SellOpts) ([]*Result, error) {
    // Find node, verify it's a directory.
    // Walk children, call Sell on each file.
    // Skip sub-directories (or recurse if they contain files).
}
```

**Step 3: Run tests and commit**

---

## Tier 3: Global CLI Flags

### Task 18: Implement `--json` global output mode

**Files:**
- Modify: `bitfs/cmd/bitfs/main.go`
- Create: `bitfs/cmd/bitfs/output.go` (shared output helpers)
- Modify: all command files to use output helpers

**Step 1: Parse global flags before subcommand dispatch**

In `main.go`, before the switch, extract global flags:

```go
var globalJSON bool
var globalVault, globalHome string
var globalTimeout int
var globalNoCache, globalOffline bool

func parseGlobalFlags(args []string) (remaining []string) {
    // Extract --json, --vault, --home, --no-cache, --offline, --timeout
    // Return remaining args for subcommand parsing.
}
```

**Step 2: Create output helper**

```go
// output.go
package main

import (
    "encoding/json"
    "fmt"
    "os"
)

func printResult(result interface{}, message string) {
    if globalJSON {
        data, _ := json.MarshalIndent(result, "", "  ")
        fmt.Println(string(data))
    } else {
        fmt.Println(message)
    }
}
```

**Step 3: Update all commands to use `printResult`**

**Step 4: Run tests and commit**

---

### Task 19: Implement `--vault`, `--home`, `--no-cache`, `--offline`, `--timeout`

**Files:**
- Modify: `bitfs/cmd/bitfs/main.go`
- Modify: command files to respect global flags

**Step 1:** Integrate into `parseGlobalFlags` from Task 18.

- `--vault`: override active vault, pass to `resolveVaultIndex`
- `--home`: override `config.DefaultDataDir()`, replace per-command `--datadir`
- `--no-cache`: set in engine/client context
- `--offline`: error on any network call in engine
- `--timeout`: set on HTTP client

**Step 2:** Each command uses `getDataDir()` helper that checks `globalHome` first, then `--datadir` flag, then default.

**Step 3: Run tests and commit**

---

## Tier 4: B-tools Flags + Paymail Resolution

### Task 20: Add missing flags to bls

**Files:**
- Modify: `bitfs/cmd/bls/main.go`

**Step 1: Add `--keyword`, `--no-cache`, `--offline` flags**

```go
keyword := fs.String("keyword", "", "filter entries by keyword")
noCache := fs.Bool("no-cache", false, "skip local cache")
offline := fs.Bool("offline", false, "cache-only mode")
```

Filter logic: if keyword is set, only show entries where name contains keyword.

**Step 2: Run tests and commit**

---

### Task 21: Add missing flags to bcat

**Files:**
- Modify: `bitfs/cmd/bcat/main.go`

**Step 1: Add `--json`, `--no-cache` flags**

`--json`: wrap output in JSON with metadata (path, mime type, size, content base64).

**Step 2: Run tests and commit**

---

### Task 22: Implement real `--version N` for bget and `--versions` for bstat

**Files:**
- Modify: `bitfs/cmd/bget/main.go`
- Modify: `bitfs/cmd/bstat/main.go`

**Step 1: bget `--version N`**

Replace stub with actual version query. The client needs to support querying version history from the daemon's meta endpoint (add version parameter to meta request).

**Step 2: bstat `--versions`**

Replace stub with listing all versions. Query daemon for version history.

**Step 3: Add `--json`, `--no-cache` to both tools**

**Step 4: Run tests and commit**

---

### Task 23: Add `--no-cache` to btree and remaining tools

**Files:**
- Modify: `bitfs/cmd/btree/main.go`
- Modify: all b-tools that still lack `--no-cache`

**Step 1: Add flag, pass to client config**

**Step 2: Run tests and commit**

---

### Task 24: Wire paymail/DNSLink resolution into all b-tools

**Files:**
- Modify: `bitfs/cmd/bls/main.go`, `bcat/main.go`, `bget/main.go`, `bstat/main.go`, `btree/main.go`

**Step 1: Replace "not yet supported" stubs**

Currently all b-tools parse URIs with a simple check and print "paymail/dnslink resolution not yet supported" for non-PubKey URIs. Replace with:

```go
import "github.com/tongxiaofeng/libbitfs/paymail"

func resolveURI(rawURI string) (host string, pnodeHex string, path string, err error) {
    parsed, err := paymail.ParseURI(rawURI)
    if err != nil { return "", "", "", err }
    if parsed.Type == paymail.AddressPubKey {
        return parsed.Host, parsed.PubKeyHex, parsed.Path, nil
    }
    // Resolve via paymail/DNSLink.
    resolved, err := paymail.ResolveURI(parsed)
    if err != nil { return "", "", "", err }
    return resolved.Host, resolved.PubKeyHex, resolved.Path, nil
}
```

**Step 2: Update each tool's URI handling to use `resolveURI`**

**Step 3: Run tests and commit**

---

## Tier 5: Daemon Endpoints

### Task 25: Implement `POST /_bitfs/pay/{invoice_id}`

**Files:**
- Modify: `bitfs/internal/daemon/routes.go`
- Create: `bitfs/internal/daemon/handler_pay.go`

**Step 1: Register route**

In `RegisterRoutes`, add:
```go
mux.HandleFunc("POST /_bitfs/pay/{invoice_id}", d.handlePay)
```

**Step 2: Implement handler**

```go
func (d *Daemon) handlePay(w http.ResponseWriter, r *http.Request) {
    invoiceID := r.PathValue("invoice_id")
    // Look up invoice.
    // Parse and verify BSV payment tx from request body.
    // If valid, release content capsule (encrypted AES key).
    // Record sale.
}
```

**Step 3: Write tests using httptest**

**Step 4: Commit**

---

### Task 26: Implement `POST /_bitfs/git/push`

**Files:**
- Create: `bitfs/internal/daemon/handler_git.go`
- Modify: `bitfs/internal/daemon/routes.go`

**Step 1: Register route**

```go
mux.HandleFunc("POST /_bitfs/git/push", d.handleGitPush)
```

**Step 2: Implement handler**

```go
func (d *Daemon) handleGitPush(w http.ResponseWriter, r *http.Request) {
    // Parse git packfile from request body.
    // Extract git objects (blobs, trees, commits).
    // Map to Metanet transactions:
    //   - blob → file node (put)
    //   - tree → directory node (mkdir)
    //   - commit → root update (self-update)
    // Build and sign transactions.
    // Return tx IDs.
}
```

This is the most complex endpoint. It requires understanding git pack format. Consider using `go-git` or a minimal pack parser.

**Step 3: Write tests**

**Step 4: Commit**

---

### Task 27: Implement `GET /_bitfs/git/refs/{path}`

**Files:**
- Modify: `bitfs/internal/daemon/handler_git.go`
- Modify: `bitfs/internal/daemon/routes.go`

**Step 1: Register route**

```go
mux.HandleFunc("GET /_bitfs/git/refs/{path...}", d.handleGitRefs)
```

**Step 2: Implement handler**

```go
func (d *Daemon) handleGitRefs(w http.ResponseWriter, r *http.Request) {
    refPath := r.PathValue("path")
    // Map git refs to Metanet node paths.
    // HEAD → root node of vault
    // refs/heads/main → specific branch node
    // Return ref list in git smart HTTP protocol format.
}
```

**Step 3: Write tests and commit**

---

## Final Task

### Task 28: Full test suite verification and lint

**Step 1: Run all tests**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./... -count=1
cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1
```

**Step 2: Run linter**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs && golangci-lint run ./...
cd /Users/alex/Codes/RabbitHole/bitfs && golangci-lint run ./...
```

**Step 3: Fix any issues**

**Step 4: Commit and summary**

---

## Implementation Order Summary

| Task | Tier | Item | Depends On |
|------|------|------|------------|
| 1 | 0 | tx interface alignment | — |
| 2 | 0 | OnChainRef type | — |
| 3 | 0 | DeriveKeyCacheKey | — |
| 4 | 0 | revshare skeleton | — |
| 5 | 1 | Session file + secure delete | — |
| 6 | 1 | `bitfs unlock` | T3, T5 |
| 7 | 1 | `bitfs lock` | T5 |
| 8 | 1 | Key resolution chain | T5, T6 |
| 9 | 2 | `bitfs init` | — |
| 10 | 2 | `vault use` + `vault info` | — |
| 11 | 2 | `bitfs decrypt` | — |
| 12 | 2 | `bitfs rmdir` | — |
| 13 | 2 | `bitfs sales` | — |
| 14 | 2 | `wallet restore` | — |
| 15 | 2 | `wallet info` rename | — |
| 16 | 2 | `daemon status` + `config` | — |
| 17 | 2 | `sell --recursive` | — |
| 18 | 3 | `--json` global flag | — |
| 19 | 3 | Other global flags | T18 |
| 20 | 4 | bls flags | — |
| 21 | 4 | bcat flags | — |
| 22 | 4 | bget/bstat version flags | — |
| 23 | 4 | btree + remaining flags | — |
| 24 | 4 | Paymail resolution wiring | — |
| 25 | 5 | `POST /_bitfs/pay` | — |
| 26 | 5 | `POST /_bitfs/git/push` | — |
| 27 | 5 | `GET /_bitfs/git/refs` | — |
| 28 | — | Final verification | All |

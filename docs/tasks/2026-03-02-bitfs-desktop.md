# BitFS Desktop Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a cross-platform desktop client for BitFS using Wails v2 (Go + React), connecting directly to libbitfs-go for all operations.

**Architecture:** Wails v2 app with Go backend directly importing libbitfs-go for wallet/file operations. React 19 frontend with BitFS Dark Botanical theme. Single binary output. Daemon mode optional (Phase 2).

**Tech Stack:** Go 1.25, Wails v2, libbitfs-go, React 19, TypeScript 5, Vite 6, Tailwind CSS 4, React Router v7, Lucide React

**Design Doc:** `docs/tasks/2026-03-02-bitfs-desktop-design.md`

---

## Task 1: Project Scaffolding

**Files:**
- Create: `bitfs-desktop/` (entire Wails project)
- Create: `bitfs-desktop/go.mod`
- Create: `bitfs-desktop/main.go`
- Create: `bitfs-desktop/app.go`
- Create: `bitfs-desktop/wails.json`

**Step 1: Install Wails CLI (if not installed)**

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails doctor  # verify installation
```

**Step 2: Create Wails project**

```bash
cd /Users/alex/Codes/RabbitHole
wails init -n bitfs-desktop -t react-ts
cd bitfs-desktop
```

**Step 3: Configure go.mod with libbitfs-go**

Edit `go.mod` to add libbitfs-go dependency with local replace:

```go
module github.com/bitfsorg/bitfs-desktop

go 1.25

require (
    github.com/bitfsorg/libbitfs-go v0.1.0
    github.com/wailsapp/wails/v2 v2.9.2
)

replace github.com/bitfsorg/libbitfs-go => ../libbitfs-go
```

Run: `go mod tidy`

**Step 4: Install frontend dependencies**

```bash
cd frontend
npm install tailwindcss@4 @tailwindcss/vite@4
npm install react-router-dom@7 lucide-react @tanstack/react-query@5
npm install clsx tailwind-merge class-variance-authority
```

**Step 5: Configure Vite for Tailwind CSS 4**

Edit `frontend/vite.config.ts`:

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { resolve } from 'path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
})
```

**Step 6: Set up BitFS Dark Botanical theme**

Create `frontend/src/index.css`:

```css
@import "tailwindcss";

@theme {
  --color-bg-primary: #1a1a1a;
  --color-bg-card: #242424;
  --color-bg-card-hover: #2a2a2a;
  --color-bg-sidebar: #1e1e1e;
  --color-bg-input: #2a2a2a;
  --color-border: #333333;
  --color-border-focus: #c9956b;
  --color-accent: #c9956b;
  --color-accent-hover: #d4a57a;
  --color-text-primary: #e5e5e5;
  --color-text-secondary: #999999;
  --color-text-muted: #666666;
  --color-success: #4ade80;
  --color-warning: #fbbf24;
  --color-error: #f87171;
  --font-sans: "Inter", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", monospace;
}

@layer base {
  body {
    @apply bg-bg-primary text-text-primary font-sans antialiased;
    font-feature-settings: "liga" 1, "calt" 1;
  }
}
```

Add font imports in `frontend/index.html` `<head>`:

```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
```

**Step 7: Update wails.json**

```json
{
  "$schema": "https://wails.io/schemas/config.v2.json",
  "name": "bitfs-desktop",
  "outputfilename": "BitFS",
  "frontend:install": "npm install",
  "frontend:build": "npm run build",
  "frontend:dev:serverUrl": "auto",
  "author": {
    "name": "BitFS",
    "email": "hello@bitfs.org"
  }
}
```

**Step 8: Verify project builds and runs**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs-desktop
wails dev
```

Expected: Window opens with default Wails React template, no build errors.

**Step 9: Commit**

```bash
git init
git add -A
git commit -m "feat: scaffold Wails v2 project with React + Tailwind"
```

---

## Task 2: Go Backend — Wallet Service

**Files:**
- Create: `bitfs-desktop/wallet_service.go`
- Create: `bitfs-desktop/wallet_service_test.go`
- Modify: `bitfs-desktop/app.go`
- Modify: `bitfs-desktop/main.go`

**Prereqs:** Read `libbitfs-go/wallet/` package for exact API signatures. Read `libbitfs-go/vault/vault.go` for `vault.New()` constructor.

**Step 1: Write wallet service tests**

Create `wallet_service_test.go`:

```go
package main

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func tmpDataDir(t *testing.T) string {
    t.Helper()
    dir := t.TempDir()
    return filepath.Join(dir, ".bitfs")
}

func TestHasWallet_NoWallet(t *testing.T) {
    ws := NewWalletService(tmpDataDir(t))
    assert.False(t, ws.HasWallet())
}

func TestCreateAndUnlockWallet(t *testing.T) {
    dir := tmpDataDir(t)
    ws := NewWalletService(dir)

    // Create wallet
    mnemonic, err := ws.CreateWallet("test-password")
    require.NoError(t, err)
    assert.NotEmpty(t, mnemonic)

    // wallet.enc should exist
    _, err = os.Stat(filepath.Join(dir, "wallet.enc"))
    assert.NoError(t, err)

    // HasWallet should be true
    assert.True(t, ws.HasWallet())

    // Not unlocked yet
    assert.False(t, ws.IsUnlocked())

    // Unlock
    err = ws.UnlockWallet("test-password")
    require.NoError(t, err)
    assert.True(t, ws.IsUnlocked())

    // Lock
    ws.LockWallet()
    assert.False(t, ws.IsUnlocked())
}

func TestUnlockWallet_WrongPassword(t *testing.T) {
    dir := tmpDataDir(t)
    ws := NewWalletService(dir)

    _, err := ws.CreateWallet("correct-password")
    require.NoError(t, err)

    err = ws.UnlockWallet("wrong-password")
    assert.Error(t, err)
}

func TestRestoreWallet(t *testing.T) {
    dir1 := tmpDataDir(t)
    ws1 := NewWalletService(dir1)

    mnemonic, err := ws1.CreateWallet("pass1")
    require.NoError(t, err)

    // Restore in new location
    dir2 := tmpDataDir(t)
    ws2 := NewWalletService(dir2)

    err = ws2.RestoreWallet(mnemonic, "pass2")
    require.NoError(t, err)
    assert.True(t, ws2.HasWallet())

    // Should unlock with new password
    err = ws2.UnlockWallet("pass2")
    require.NoError(t, err)
    assert.True(t, ws2.IsUnlocked())
}

func TestGetWalletInfo_Locked(t *testing.T) {
    dir := tmpDataDir(t)
    ws := NewWalletService(dir)

    _, err := ws.GetWalletInfo()
    assert.Error(t, err) // not unlocked
}

func TestGetWalletInfo_Unlocked(t *testing.T) {
    dir := tmpDataDir(t)
    ws := NewWalletService(dir)

    _, err := ws.CreateWallet("pass")
    require.NoError(t, err)

    err = ws.UnlockWallet("pass")
    require.NoError(t, err)

    info, err := ws.GetWalletInfo()
    require.NoError(t, err)
    assert.NotEmpty(t, info.Address)
    assert.NotEmpty(t, info.PublicKey)
}
```

**Step 2: Run tests to verify they fail**

```bash
cd bitfs-desktop
go test -v -run TestHasWallet
```

Expected: FAIL — `NewWalletService` not defined.

**Step 3: Implement WalletService**

Create `wallet_service.go`:

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"
    "sync"

    "github.com/bitfsorg/libbitfs-go/wallet"
    "github.com/bitfsorg/libbitfs-go/vault"
)

// WalletInfo is the frontend-visible wallet information.
type WalletInfo struct {
    Address   string `json:"address"`
    PublicKey string `json:"publicKey"`
    Network   string `json:"network"`
    Path      string `json:"path"`
}

// WalletService manages wallet lifecycle. All methods are goroutine-safe.
type WalletService struct {
    mu      sync.RWMutex
    dataDir string
    vault   *vault.Vault
}

func NewWalletService(dataDir string) *WalletService {
    return &WalletService{dataDir: dataDir}
}

func (ws *WalletService) walletPath() string {
    return filepath.Join(ws.dataDir, "wallet.enc")
}

// HasWallet returns true if an encrypted wallet file exists.
func (ws *WalletService) HasWallet() bool {
    _, err := os.Stat(ws.walletPath())
    return err == nil
}

// CreateWallet generates a new BIP39 mnemonic, derives seed, encrypts with
// password, and saves to dataDir. Returns the mnemonic for user backup.
func (ws *WalletService) CreateWallet(password string) (string, error) {
    ws.mu.Lock()
    defer ws.mu.Unlock()

    if ws.HasWallet() {
        return "", fmt.Errorf("wallet already exists")
    }

    // Ensure data directory exists
    if err := os.MkdirAll(ws.dataDir, 0700); err != nil {
        return "", fmt.Errorf("create data dir: %w", err)
    }

    // Generate 12-word mnemonic
    mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
    if err != nil {
        return "", fmt.Errorf("generate mnemonic: %w", err)
    }

    // Derive seed (no BIP39 passphrase)
    seed, err := wallet.SeedFromMnemonic(mnemonic, "")
    if err != nil {
        return "", fmt.Errorf("derive seed: %w", err)
    }

    // Encrypt seed with user password
    encrypted, err := wallet.EncryptSeed(seed, password)
    if err != nil {
        return "", fmt.Errorf("encrypt seed: %w", err)
    }

    // Save encrypted seed
    if err := os.WriteFile(ws.walletPath(), encrypted, 0600); err != nil {
        return "", fmt.Errorf("save wallet: %w", err)
    }

    // Initialize wallet state
    state := wallet.NewWalletState()
    w, err := wallet.NewWallet(seed, &wallet.MainNet)
    if err != nil {
        return "", fmt.Errorf("create wallet: %w", err)
    }

    // Create default vault
    if _, err := w.CreateVault(state, "default"); err != nil {
        return "", fmt.Errorf("create vault: %w", err)
    }

    // Save state
    statePath := filepath.Join(ws.dataDir, "state.json")
    if err := vault.SaveWalletState(statePath, state); err != nil {
        return "", fmt.Errorf("save state: %w", err)
    }

    return mnemonic, nil
}

// RestoreWallet creates a wallet from an existing BIP39 mnemonic.
func (ws *WalletService) RestoreWallet(mnemonic, password string) error {
    ws.mu.Lock()
    defer ws.mu.Unlock()

    if ws.HasWallet() {
        return fmt.Errorf("wallet already exists")
    }

    if !wallet.ValidateMnemonic(mnemonic) {
        return fmt.Errorf("invalid mnemonic")
    }

    if err := os.MkdirAll(ws.dataDir, 0700); err != nil {
        return fmt.Errorf("create data dir: %w", err)
    }

    seed, err := wallet.SeedFromMnemonic(mnemonic, "")
    if err != nil {
        return fmt.Errorf("derive seed: %w", err)
    }

    encrypted, err := wallet.EncryptSeed(seed, password)
    if err != nil {
        return fmt.Errorf("encrypt seed: %w", err)
    }

    if err := os.WriteFile(ws.walletPath(), encrypted, 0600); err != nil {
        return fmt.Errorf("save wallet: %w", err)
    }

    state := wallet.NewWalletState()
    w, err := wallet.NewWallet(seed, &wallet.MainNet)
    if err != nil {
        return fmt.Errorf("create wallet: %w", err)
    }

    if _, err := w.CreateVault(state, "default"); err != nil {
        return fmt.Errorf("create vault: %w", err)
    }

    statePath := filepath.Join(ws.dataDir, "state.json")
    if err := vault.SaveWalletState(statePath, state); err != nil {
        return fmt.Errorf("save state: %w", err)
    }

    return nil
}

// UnlockWallet decrypts the wallet and opens the vault.
func (ws *WalletService) UnlockWallet(password string) error {
    ws.mu.Lock()
    defer ws.mu.Unlock()

    if ws.vault != nil {
        return nil // already unlocked
    }

    v, err := vault.New(ws.dataDir, password)
    if err != nil {
        return fmt.Errorf("unlock failed: %w", err)
    }

    ws.vault = v
    return nil
}

// LockWallet closes the vault and clears sensitive data.
func (ws *WalletService) LockWallet() {
    ws.mu.Lock()
    defer ws.mu.Unlock()

    if ws.vault != nil {
        _ = ws.vault.Close()
        ws.vault = nil
    }
}

// IsUnlocked returns true if the wallet is currently unlocked.
func (ws *WalletService) IsUnlocked() bool {
    ws.mu.RLock()
    defer ws.mu.RUnlock()
    return ws.vault != nil
}

// GetWalletInfo returns wallet address and public key.
func (ws *WalletService) GetWalletInfo() (*WalletInfo, error) {
    ws.mu.RLock()
    defer ws.mu.RUnlock()

    if ws.vault == nil {
        return nil, fmt.Errorf("wallet is locked")
    }

    // Derive the first receive address key
    kp, err := ws.vault.Wallet.DeriveFeeKey(wallet.ExternalChain, 0)
    if err != nil {
        return nil, fmt.Errorf("derive key: %w", err)
    }

    addr, err := kp.PublicKey.Address()
    if err != nil {
        return nil, fmt.Errorf("derive address: %w", err)
    }

    return &WalletInfo{
        Address:   addr.AddressString,
        PublicKey: kp.PublicKey.ToDERHex(),
        Network:   "mainnet",
        Path:      kp.Path,
    }, nil
}

// GetVault returns the unlocked vault (for use by FileService).
func (ws *WalletService) GetVault() (*vault.Vault, error) {
    ws.mu.RLock()
    defer ws.mu.RUnlock()

    if ws.vault == nil {
        return nil, fmt.Errorf("wallet is locked")
    }
    return ws.vault, nil
}
```

**Note:** The `vault.SaveWalletState` function may not exist as a standalone function — check the vault package. If not, implement it as JSON marshal + os.WriteFile. The vault.New() constructor likely handles state loading internally.

**Step 4: Run tests**

```bash
go test -v -run TestHasWallet -count=1
go test -v -run TestCreate -count=1
go test -v -count=1
```

Expected: All tests pass. Note: `TestCreateAndUnlockWallet` may be slow (~3s) due to Argon2id.

**Step 5: Wire WalletService into App struct**

Update `app.go`:

```go
package main

import (
    "context"
    "os"
    "path/filepath"
)

type App struct {
    ctx           context.Context
    walletService *WalletService
}

func NewApp() *App {
    homeDir, _ := os.UserHomeDir()
    dataDir := filepath.Join(homeDir, ".bitfs")

    return &App{
        walletService: NewWalletService(dataDir),
    }
}

func (a *App) startup(ctx context.Context) {
    a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
    a.walletService.LockWallet()
}

// ---- Wallet methods exposed to frontend ----

func (a *App) HasWallet() bool {
    return a.walletService.HasWallet()
}

func (a *App) CreateWallet(password string) (string, error) {
    return a.walletService.CreateWallet(password)
}

func (a *App) RestoreWallet(mnemonic, password string) error {
    return a.walletService.RestoreWallet(mnemonic, password)
}

func (a *App) UnlockWallet(password string) error {
    return a.walletService.UnlockWallet(password)
}

func (a *App) LockWallet() {
    a.walletService.LockWallet()
}

func (a *App) IsUnlocked() bool {
    return a.walletService.IsUnlocked()
}

func (a *App) GetWalletInfo() (*WalletInfo, error) {
    return a.walletService.GetWalletInfo()
}
```

Update `main.go`:

```go
package main

import (
    "embed"

    "github.com/wailsapp/wails/v2"
    "github.com/wailsapp/wails/v2/pkg/options"
    "github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
    app := NewApp()

    err := wails.Run(&options.App{
        Title:     "BitFS",
        Width:     1200,
        Height:    800,
        MinWidth:  800,
        MinHeight: 600,
        AssetServer: &assetserver.Options{
            Assets: assets,
        },
        BackgroundColour: &options.RGBA{R: 26, G: 26, B: 26, A: 1},
        OnStartup:        app.startup,
        OnShutdown:       app.shutdown,
        Bind: []interface{}{
            app,
        },
    })
    if err != nil {
        println("Error:", err.Error())
    }
}
```

**Step 6: Verify build**

```bash
wails build
```

Expected: Compiles successfully. Binary at `build/bin/BitFS`.

**Step 7: Commit**

```bash
git add -A
git commit -m "feat: wallet service with create/restore/unlock/lock"
```

---

## Task 3: Go Backend — File Service

**Files:**
- Create: `bitfs-desktop/file_service.go`
- Create: `bitfs-desktop/file_service_test.go`
- Modify: `bitfs-desktop/app.go`

**Prereqs:** Read `libbitfs-go/vault/` for exact method signatures of Mkdir, PutFile, Get, Cat, Remove, Move, Copy. Read `vault/state.go` for NodeState and ChildState types.

**Step 1: Write file service tests**

Create `file_service_test.go`:

```go
package main

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func setupUnlockedService(t *testing.T) (*WalletService, *FileService) {
    t.Helper()
    dir := tmpDataDir(t)
    ws := NewWalletService(dir)
    _, err := ws.CreateWallet("test")
    require.NoError(t, err)
    err = ws.UnlockWallet("test")
    require.NoError(t, err)
    fs := NewFileService(ws)
    return ws, fs
}

func TestListFiles_Root(t *testing.T) {
    _, fs := setupUnlockedService(t)
    entries, err := fs.ListFiles("/")
    require.NoError(t, err)
    assert.NotNil(t, entries)
    // Root should exist but be empty initially
    assert.Empty(t, entries)
}

func TestListFiles_Locked(t *testing.T) {
    dir := tmpDataDir(t)
    ws := NewWalletService(dir)
    _, err := ws.CreateWallet("test")
    require.NoError(t, err)
    // Don't unlock
    fs := NewFileService(ws)

    _, err = fs.ListFiles("/")
    assert.Error(t, err) // wallet is locked
}
```

**Step 2: Run tests to verify they fail**

```bash
go test -v -run TestListFiles -count=1
```

Expected: FAIL — `NewFileService` not defined.

**Step 3: Implement FileService**

Create `file_service.go`:

```go
package main

// FileEntry represents a file or directory for the frontend.
type FileEntry struct {
    Name     string `json:"name"`
    Type     string `json:"type"`     // "file", "dir", "link"
    Size     uint64 `json:"size"`
    MimeType string `json:"mimeType"`
    Access   string `json:"access"`   // "free", "private", "paid"
    Path     string `json:"path"`
}

// FileService handles file operations via the vault.
type FileService struct {
    walletSvc *WalletService
}

func NewFileService(ws *WalletService) *FileService {
    return &FileService{walletSvc: ws}
}

// ListFiles returns directory entries at the given path.
func (fs *FileService) ListFiles(path string) ([]FileEntry, error) {
    v, err := fs.walletSvc.GetVault()
    if err != nil {
        return nil, err
    }

    // Get the root node state and find the target directory
    // The vault's State.Nodes map contains all tracked nodes
    entries := []FileEntry{}

    // Find the node at the given path
    for _, node := range v.State.Nodes {
        if node.Type == "dir" && node.Path == path {
            for _, child := range node.Children {
                childNode := v.State.Nodes[child.PubKey]
                entry := FileEntry{
                    Name: child.Name,
                    Type: child.Type,
                    Path: path + "/" + child.Name,
                }
                if childNode != nil {
                    entry.Size = childNode.FileSize
                    entry.MimeType = childNode.MimeType
                    entry.Access = childNode.Access
                }
                entries = append(entries, entry)
            }
            return entries, nil
        }
    }

    // Root path with no nodes tracked yet
    if path == "/" {
        return entries, nil
    }

    return nil, fmt.Errorf("directory not found: %s", path)
}

// MakeDir creates a new directory.
func (fs *FileService) MakeDir(path string) (*Result, error) {
    v, err := fs.walletSvc.GetVault()
    if err != nil {
        return nil, err
    }

    vaultIdx, err := v.ResolveVaultIndex("")
    if err != nil {
        return nil, err
    }

    return v.Mkdir(&vault.MkdirOpts{
        VaultIndex: vaultIdx,
        Path:       path,
    })
}

// Remove deletes a file or directory.
func (fs *FileService) Remove(path string) (*Result, error) {
    v, err := fs.walletSvc.GetVault()
    if err != nil {
        return nil, err
    }

    vaultIdx, err := v.ResolveVaultIndex("")
    if err != nil {
        return nil, err
    }

    return v.Remove(&vault.RemoveOpts{
        VaultIndex: vaultIdx,
        Path:       path,
    })
}

// Move renames or moves a file/directory.
func (fs *FileService) Move(src, dst string) (*Result, error) {
    v, err := fs.walletSvc.GetVault()
    if err != nil {
        return nil, err
    }

    vaultIdx, err := v.ResolveVaultIndex("")
    if err != nil {
        return nil, err
    }

    return v.Move(&vault.MoveOpts{
        VaultIndex: vaultIdx,
        SrcPath:    src,
        DstPath:    dst,
        Force:      true,
    })
}

// Copy duplicates a file.
func (fs *FileService) Copy(src, dst string) (*Result, error) {
    v, err := fs.walletSvc.GetVault()
    if err != nil {
        return nil, err
    }

    vaultIdx, err := v.ResolveVaultIndex("")
    if err != nil {
        return nil, err
    }

    return v.Copy(&vault.CopyOpts{
        VaultIndex: vaultIdx,
        SrcPath:    src,
        DstPath:    dst,
    })
}
```

**Note:** Add missing imports (`fmt`, `github.com/bitfsorg/libbitfs-go/vault`). Adapt the Result type — if vault.Result is not directly JSON-serializable for the frontend, create a frontend-facing ResultInfo struct.

**Step 4: Run tests**

```bash
go test -v -run TestListFiles -count=1
```

Expected: Tests pass.

**Step 5: Wire FileService into App**

Add to `app.go`:

```go
// In NewApp():
func NewApp() *App {
    homeDir, _ := os.UserHomeDir()
    dataDir := filepath.Join(homeDir, ".bitfs")
    ws := NewWalletService(dataDir)

    return &App{
        walletService: ws,
        fileService:   NewFileService(ws),
    }
}

// Add field to App struct:
type App struct {
    ctx           context.Context
    walletService *WalletService
    fileService   *FileService
}

// File methods exposed to frontend:
func (a *App) ListFiles(path string) ([]FileEntry, error) {
    return a.fileService.ListFiles(path)
}

func (a *App) MakeDir(path string) error {
    _, err := a.fileService.MakeDir(path)
    return err
}

func (a *App) RemoveFile(path string) error {
    _, err := a.fileService.Remove(path)
    return err
}

func (a *App) MoveFile(src, dst string) error {
    _, err := a.fileService.Move(src, dst)
    return err
}

func (a *App) CopyFile(src, dst string) error {
    _, err := a.fileService.Copy(src, dst)
    return err
}
```

**Step 6: Add file upload via native dialog**

Add to `app.go`:

```go
import wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

// SelectFile opens a native file dialog and returns the selected path.
func (a *App) SelectFile() (string, error) {
    return wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
        Title: "Select File to Upload",
    })
}

// UploadFile uploads a local file to the given remote path.
func (a *App) UploadFile(localPath, remotePath string) error {
    v, err := a.walletService.GetVault()
    if err != nil {
        return err
    }

    vaultIdx, err := v.ResolveVaultIndex("")
    if err != nil {
        return err
    }

    _, err = v.PutFile(&vault.PutOpts{
        VaultIndex: vaultIdx,
        LocalFile:  localPath,
        RemotePath: remotePath,
        Access:     "private",
    })
    return err
}

// DownloadFile saves a remote file to a local path chosen by user.
func (a *App) DownloadFile(remotePath string) error {
    localPath, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
        Title: "Save File As",
    })
    if err != nil || localPath == "" {
        return err
    }

    v, err := a.walletService.GetVault()
    if err != nil {
        return err
    }

    vaultIdx, err := v.ResolveVaultIndex("")
    if err != nil {
        return err
    }

    _, err = v.Get(&vault.GetOpts{
        VaultIndex: vaultIdx,
        RemotePath: remotePath,
        LocalPath:  localPath,
    })
    return err
}
```

**Step 7: Commit**

```bash
git add -A
git commit -m "feat: file service with list/mkdir/remove/move/copy/upload/download"
```

---

## Task 4: Frontend — Layout, Routing & Sidebar

**Files:**
- Create: `frontend/src/App.tsx`
- Create: `frontend/src/layouts/MainLayout.tsx`
- Create: `frontend/src/components/Sidebar.tsx`
- Create: `frontend/src/lib/cn.ts`

**Step 1: Create utility function**

Create `frontend/src/lib/cn.ts`:

```typescript
import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
```

**Step 2: Create Sidebar component**

Create `frontend/src/components/Sidebar.tsx`:

```tsx
import { NavLink } from 'react-router-dom'
import { Files, Wallet, Settings, FolderTree } from 'lucide-react'
import { cn } from '@/lib/cn'

const navItems = [
  { to: '/files', icon: FolderTree, label: 'Files' },
  { to: '/wallet', icon: Wallet, label: 'Wallet' },
  { to: '/settings', icon: Settings, label: 'Settings' },
]

export function Sidebar() {
  return (
    <aside className="w-56 bg-bg-sidebar border-r border-border flex flex-col h-screen">
      <div className="p-4 border-b border-border">
        <h1 className="text-lg font-semibold text-accent tracking-wide">BitFS</h1>
        <p className="text-xs text-text-muted mt-0.5">Desktop</p>
      </div>
      <nav className="flex-1 p-2 space-y-1">
        {navItems.map(({ to, icon: Icon, label }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              cn(
                'flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors',
                isActive
                  ? 'bg-accent/10 text-accent'
                  : 'text-text-secondary hover:text-text-primary hover:bg-bg-card-hover'
              )
            }
          >
            <Icon className="h-4 w-4" />
            {label}
          </NavLink>
        ))}
      </nav>
    </aside>
  )
}
```

**Step 3: Create MainLayout**

Create `frontend/src/layouts/MainLayout.tsx`:

```tsx
import { Outlet } from 'react-router-dom'
import { Sidebar } from '@/components/Sidebar'

export function MainLayout() {
  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  )
}
```

**Step 4: Set up routing in App.tsx**

Replace `frontend/src/App.tsx`:

```tsx
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MainLayout } from '@/layouts/MainLayout'
import { FilesPage } from '@/pages/Files'
import { WalletPage } from '@/pages/Wallet'
import { SettingsPage } from '@/pages/Settings'
import { OnboardingPage } from '@/pages/Onboarding'
import { UnlockPage } from '@/pages/Unlock'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/onboarding" element={<OnboardingPage />} />
          <Route path="/unlock" element={<UnlockPage />} />
          <Route element={<MainLayout />}>
            <Route path="/files" element={<FilesPage />} />
            <Route path="/wallet" element={<WalletPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="/" element={<Navigate to="/files" replace />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
```

**Step 5: Create placeholder pages**

Create `frontend/src/pages/Files.tsx`:

```tsx
export function FilesPage() {
  return <div className="p-6"><h2 className="text-xl font-semibold">Files</h2></div>
}
```

Create similar placeholders for `Wallet.tsx`, `Settings.tsx`, `Onboarding.tsx`, `Unlock.tsx`.

**Step 6: Update main.tsx entry point**

```tsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)
```

**Step 7: Verify with wails dev**

```bash
wails dev
```

Expected: Window shows sidebar with navigation. Clicking items switches pages.

**Step 8: Commit**

```bash
cd frontend && git add -A && cd ..
git add -A
git commit -m "feat: frontend layout with sidebar, routing, BitFS theme"
```

---

## Task 5: Frontend — Onboarding Page

**Files:**
- Modify: `frontend/src/pages/Onboarding.tsx`
- Create: `frontend/src/components/MnemonicGrid.tsx`

**Step 1: Create MnemonicGrid component**

Create `frontend/src/components/MnemonicGrid.tsx`:

```tsx
import { cn } from '@/lib/cn'

interface Props {
  words: string[]
  editable?: boolean
  onChange?: (index: number, value: string) => void
}

export function MnemonicGrid({ words, editable = false, onChange }: Props) {
  return (
    <div className="grid grid-cols-3 gap-2">
      {words.map((word, i) => (
        <div
          key={i}
          className="flex items-center gap-2 bg-bg-card border border-border rounded-md px-3 py-2"
        >
          <span className="text-text-muted text-xs w-5">{i + 1}.</span>
          {editable ? (
            <input
              type="text"
              value={word}
              onChange={(e) => onChange?.(i, e.target.value)}
              className="bg-transparent text-sm text-text-primary outline-none w-full font-mono"
              placeholder="word"
            />
          ) : (
            <span className="text-sm font-mono text-text-primary">{word}</span>
          )}
        </div>
      ))}
    </div>
  )
}
```

**Step 2: Implement Onboarding page**

Replace `frontend/src/pages/Onboarding.tsx`:

```tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { CreateWallet, RestoreWallet } from '../../wailsjs/go/main/App'
import { MnemonicGrid } from '@/components/MnemonicGrid'
import { KeyRound, RotateCcw } from 'lucide-react'

type Mode = 'choose' | 'create-show' | 'create-password' | 'restore-input' | 'restore-password'

export function OnboardingPage() {
  const navigate = useNavigate()
  const [mode, setMode] = useState<Mode>('choose')
  const [mnemonic, setMnemonic] = useState<string[]>(Array(12).fill(''))
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleCreate = async () => {
    setLoading(true)
    setError('')
    try {
      const result = await CreateWallet(password)
      setMnemonic(result.split(' '))
      setMode('create-show')
    } catch (e: any) {
      setError(e.message || String(e))
    } finally {
      setLoading(false)
    }
  }

  const handleSetPassword = () => {
    if (password.length < 8) {
      setError('Password must be at least 8 characters')
      return
    }
    if (password !== confirmPassword) {
      setError('Passwords do not match')
      return
    }
    setError('')

    if (mode === 'choose') {
      // Creating new wallet
      setMode('create-password')
    }
  }

  const handleCreateWithPassword = async () => {
    if (password.length < 8 || password !== confirmPassword) return
    setLoading(true)
    setError('')
    try {
      const result = await CreateWallet(password)
      setMnemonic(result.split(' '))
      setMode('create-show')
    } catch (e: any) {
      setError(e.message || String(e))
    } finally {
      setLoading(false)
    }
  }

  const handleRestore = async () => {
    if (password.length < 8) {
      setError('Password must be at least 8 characters')
      return
    }
    if (password !== confirmPassword) {
      setError('Passwords do not match')
      return
    }
    setLoading(true)
    setError('')
    try {
      await RestoreWallet(mnemonic.join(' '), password)
      navigate('/unlock')
    } catch (e: any) {
      setError(e.message || String(e))
    } finally {
      setLoading(false)
    }
  }

  const handleDone = () => {
    navigate('/unlock')
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg-primary p-8">
      <div className="w-full max-w-lg space-y-6">
        <div className="text-center">
          <h1 className="text-2xl font-semibold text-accent">BitFS</h1>
          <p className="text-text-secondary mt-1">Decentralized Encrypted File System</p>
        </div>

        {mode === 'choose' && (
          <div className="space-y-3">
            <button
              onClick={() => { setMode('create-password') }}
              className="w-full flex items-center gap-3 p-4 bg-bg-card border border-border rounded-lg hover:border-accent transition-colors"
            >
              <KeyRound className="h-5 w-5 text-accent" />
              <div className="text-left">
                <div className="text-sm font-medium text-text-primary">Create New Wallet</div>
                <div className="text-xs text-text-muted">Generate a new BIP39 seed phrase</div>
              </div>
            </button>
            <button
              onClick={() => setMode('restore-input')}
              className="w-full flex items-center gap-3 p-4 bg-bg-card border border-border rounded-lg hover:border-accent transition-colors"
            >
              <RotateCcw className="h-5 w-5 text-accent" />
              <div className="text-left">
                <div className="text-sm font-medium text-text-primary">Restore Wallet</div>
                <div className="text-xs text-text-muted">Enter existing 12-word mnemonic</div>
              </div>
            </button>
          </div>
        )}

        {mode === 'create-password' && (
          <div className="space-y-4 bg-bg-card border border-border rounded-lg p-6">
            <h2 className="text-lg font-medium">Set Password</h2>
            <p className="text-sm text-text-secondary">This password encrypts your wallet seed locally.</p>
            <input
              type="password"
              placeholder="Password (min 8 chars)"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full bg-bg-input border border-border rounded-md px-3 py-2 text-sm text-text-primary outline-none focus:border-border-focus"
            />
            <input
              type="password"
              placeholder="Confirm password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              className="w-full bg-bg-input border border-border rounded-md px-3 py-2 text-sm text-text-primary outline-none focus:border-border-focus"
            />
            <button
              onClick={handleCreateWithPassword}
              disabled={loading || password.length < 8 || password !== confirmPassword}
              className="w-full bg-accent text-bg-primary font-medium py-2 rounded-md hover:bg-accent-hover disabled:opacity-50 transition-colors"
            >
              {loading ? 'Creating...' : 'Create Wallet'}
            </button>
          </div>
        )}

        {mode === 'create-show' && (
          <div className="space-y-4 bg-bg-card border border-border rounded-lg p-6">
            <h2 className="text-lg font-medium">Backup Your Seed Phrase</h2>
            <p className="text-sm text-text-secondary">
              Write down these 12 words in order. This is the only way to recover your wallet.
            </p>
            <MnemonicGrid words={mnemonic} />
            <div className="bg-warning/10 border border-warning/20 rounded-md p-3">
              <p className="text-xs text-warning">
                Never share your seed phrase. Anyone with these words can access your wallet.
              </p>
            </div>
            <button
              onClick={handleDone}
              className="w-full bg-accent text-bg-primary font-medium py-2 rounded-md hover:bg-accent-hover transition-colors"
            >
              I've Saved My Seed Phrase
            </button>
          </div>
        )}

        {mode === 'restore-input' && (
          <div className="space-y-4 bg-bg-card border border-border rounded-lg p-6">
            <h2 className="text-lg font-medium">Enter Seed Phrase</h2>
            <MnemonicGrid
              words={mnemonic}
              editable
              onChange={(i, v) => {
                const next = [...mnemonic]
                next[i] = v.toLowerCase().trim()
                setMnemonic(next)
              }}
            />
            <button
              onClick={() => setMode('restore-password')}
              disabled={mnemonic.some((w) => w === '')}
              className="w-full bg-accent text-bg-primary font-medium py-2 rounded-md hover:bg-accent-hover disabled:opacity-50 transition-colors"
            >
              Next
            </button>
          </div>
        )}

        {mode === 'restore-password' && (
          <div className="space-y-4 bg-bg-card border border-border rounded-lg p-6">
            <h2 className="text-lg font-medium">Set Password</h2>
            <input
              type="password"
              placeholder="Password (min 8 chars)"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full bg-bg-input border border-border rounded-md px-3 py-2 text-sm text-text-primary outline-none focus:border-border-focus"
            />
            <input
              type="password"
              placeholder="Confirm password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              className="w-full bg-bg-input border border-border rounded-md px-3 py-2 text-sm text-text-primary outline-none focus:border-border-focus"
            />
            <button
              onClick={handleRestore}
              disabled={loading || password.length < 8 || password !== confirmPassword}
              className="w-full bg-accent text-bg-primary font-medium py-2 rounded-md hover:bg-accent-hover disabled:opacity-50 transition-colors"
            >
              {loading ? 'Restoring...' : 'Restore Wallet'}
            </button>
          </div>
        )}

        {error && (
          <div className="bg-error/10 border border-error/20 rounded-md p-3">
            <p className="text-sm text-error">{error}</p>
          </div>
        )}
      </div>
    </div>
  )
}
```

**Step 3: Verify with wails dev**

```bash
wails dev
```

Navigate to `/onboarding`. Expected: Create/Restore wallet UI renders correctly.

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: onboarding page with create/restore wallet flow"
```

---

## Task 6: Frontend — Unlock Page + Auth Guard

**Files:**
- Modify: `frontend/src/pages/Unlock.tsx`
- Create: `frontend/src/hooks/useAuth.ts`
- Modify: `frontend/src/App.tsx`

**Step 1: Create auth hook**

Create `frontend/src/hooks/useAuth.ts`:

```typescript
import { useQuery } from '@tanstack/react-query'
import { HasWallet, IsUnlocked } from '../../wailsjs/go/main/App'

export function useAuth() {
  const { data: hasWallet, isLoading: walletLoading } = useQuery({
    queryKey: ['hasWallet'],
    queryFn: HasWallet,
    staleTime: 5000,
  })

  const { data: isUnlocked, isLoading: unlockLoading } = useQuery({
    queryKey: ['isUnlocked'],
    queryFn: IsUnlocked,
    staleTime: 2000,
    refetchInterval: 5000,
  })

  return {
    hasWallet: hasWallet ?? false,
    isUnlocked: isUnlocked ?? false,
    isLoading: walletLoading || unlockLoading,
  }
}
```

**Step 2: Implement Unlock page**

Replace `frontend/src/pages/Unlock.tsx`:

```tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { UnlockWallet } from '../../wailsjs/go/main/App'
import { Lock } from 'lucide-react'

export function UnlockPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleUnlock = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      await UnlockWallet(password)
      queryClient.invalidateQueries({ queryKey: ['isUnlocked'] })
      navigate('/files')
    } catch (e: any) {
      setError('Incorrect password')
      setPassword('')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg-primary p-8">
      <div className="w-full max-w-sm space-y-6">
        <div className="text-center">
          <Lock className="h-10 w-10 text-accent mx-auto" />
          <h1 className="text-xl font-semibold mt-4">Unlock BitFS</h1>
          <p className="text-sm text-text-secondary mt-1">Enter your password to continue</p>
        </div>
        <form onSubmit={handleUnlock} className="space-y-4">
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Password"
            autoFocus
            className="w-full bg-bg-input border border-border rounded-md px-3 py-2 text-sm text-text-primary outline-none focus:border-border-focus"
          />
          {error && <p className="text-sm text-error">{error}</p>}
          <button
            type="submit"
            disabled={loading || !password}
            className="w-full bg-accent text-bg-primary font-medium py-2 rounded-md hover:bg-accent-hover disabled:opacity-50 transition-colors"
          >
            {loading ? 'Unlocking...' : 'Unlock'}
          </button>
        </form>
      </div>
    </div>
  )
}
```

**Step 3: Add auth guard to App.tsx routing**

Update `App.tsx` to redirect based on auth state:

```tsx
import { useAuth } from '@/hooks/useAuth'
import { Navigate, useLocation } from 'react-router-dom'

function AuthGuard({ children }: { children: React.ReactNode }) {
  const { hasWallet, isUnlocked, isLoading } = useAuth()
  const location = useLocation()

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-bg-primary">
        <div className="text-text-muted">Loading...</div>
      </div>
    )
  }

  if (!hasWallet && location.pathname !== '/onboarding') {
    return <Navigate to="/onboarding" replace />
  }

  if (hasWallet && !isUnlocked && location.pathname !== '/unlock') {
    return <Navigate to="/unlock" replace />
  }

  if (hasWallet && isUnlocked && (location.pathname === '/onboarding' || location.pathname === '/unlock')) {
    return <Navigate to="/files" replace />
  }

  return <>{children}</>
}
```

Wrap all routes inside `<AuthGuard>` in `App.tsx`.

**Step 4: Verify with wails dev**

Expected: App redirects to `/onboarding` on first run, then to `/unlock` after wallet creation, then to `/files` after unlock.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: unlock page and auth guard with redirect logic"
```

---

## Task 7: Frontend — Files Page

**Files:**
- Modify: `frontend/src/pages/Files.tsx`
- Create: `frontend/src/components/FileTable.tsx`
- Create: `frontend/src/components/Breadcrumb.tsx`

**Step 1: Create Breadcrumb component**

Create `frontend/src/components/Breadcrumb.tsx`:

```tsx
import { ChevronRight, Home } from 'lucide-react'

interface Props {
  path: string
  onNavigate: (path: string) => void
}

export function Breadcrumb({ path, onNavigate }: Props) {
  const parts = path.split('/').filter(Boolean)

  return (
    <div className="flex items-center gap-1 text-sm text-text-secondary">
      <button onClick={() => onNavigate('/')} className="hover:text-accent p-1">
        <Home className="h-4 w-4" />
      </button>
      {parts.map((part, i) => {
        const fullPath = '/' + parts.slice(0, i + 1).join('/')
        return (
          <span key={fullPath} className="flex items-center gap-1">
            <ChevronRight className="h-3 w-3 text-text-muted" />
            <button onClick={() => onNavigate(fullPath)} className="hover:text-accent">
              {part}
            </button>
          </span>
        )
      })}
    </div>
  )
}
```

**Step 2: Create FileTable component**

Create `frontend/src/components/FileTable.tsx`:

```tsx
import { Folder, File, Lock, Globe, DollarSign } from 'lucide-react'
import { cn } from '@/lib/cn'

interface FileEntry {
  name: string
  type: string
  size: number
  mimeType: string
  access: string
  path: string
}

interface Props {
  entries: FileEntry[]
  onOpen: (entry: FileEntry) => void
  onContextMenu?: (entry: FileEntry, e: React.MouseEvent) => void
}

function formatSize(bytes: number): string {
  if (bytes === 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i]
}

function AccessIcon({ access }: { access: string }) {
  switch (access) {
    case 'private': return <Lock className="h-3 w-3 text-warning" />
    case 'paid': return <DollarSign className="h-3 w-3 text-success" />
    default: return <Globe className="h-3 w-3 text-text-muted" />
  }
}

export function FileTable({ entries, onOpen, onContextMenu }: Props) {
  if (entries.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-text-muted">
        <Folder className="h-10 w-10 mb-2" />
        <p className="text-sm">Empty directory</p>
      </div>
    )
  }

  // Sort: directories first, then files
  const sorted = [...entries].sort((a, b) => {
    if (a.type === 'dir' && b.type !== 'dir') return -1
    if (a.type !== 'dir' && b.type === 'dir') return 1
    return a.name.localeCompare(b.name)
  })

  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b border-border text-text-muted">
          <th className="text-left py-2 px-3 font-medium">Name</th>
          <th className="text-left py-2 px-3 font-medium w-20">Access</th>
          <th className="text-right py-2 px-3 font-medium w-24">Size</th>
        </tr>
      </thead>
      <tbody>
        {sorted.map((entry) => (
          <tr
            key={entry.path}
            onDoubleClick={() => onOpen(entry)}
            onContextMenu={(e) => onContextMenu?.(entry, e)}
            className="border-b border-border/50 hover:bg-bg-card-hover cursor-pointer transition-colors"
          >
            <td className="py-2 px-3 flex items-center gap-2">
              {entry.type === 'dir' ? (
                <Folder className="h-4 w-4 text-accent" />
              ) : (
                <File className="h-4 w-4 text-text-muted" />
              )}
              <span className="text-text-primary">{entry.name}</span>
            </td>
            <td className="py-2 px-3">
              <AccessIcon access={entry.access} />
            </td>
            <td className="py-2 px-3 text-right text-text-muted font-mono text-xs">
              {entry.type === 'dir' ? '—' : formatSize(entry.size)}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
```

**Step 3: Implement Files page**

Replace `frontend/src/pages/Files.tsx`:

```tsx
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ListFiles, MakeDir, SelectFile, UploadFile } from '../../wailsjs/go/main/App'
import { Breadcrumb } from '@/components/Breadcrumb'
import { FileTable } from '@/components/FileTable'
import { FolderPlus, Upload, RefreshCw } from 'lucide-react'

export function FilesPage() {
  const [currentPath, setCurrentPath] = useState('/')
  const queryClient = useQueryClient()

  const { data: files = [], isLoading, error } = useQuery({
    queryKey: ['files', currentPath],
    queryFn: () => ListFiles(currentPath),
  })

  const handleOpen = (entry: any) => {
    if (entry.type === 'dir') {
      setCurrentPath(entry.path)
    }
  }

  const handleNewFolder = async () => {
    const name = prompt('Folder name:')
    if (!name) return
    const path = currentPath === '/' ? `/${name}` : `${currentPath}/${name}`
    try {
      await MakeDir(path)
      queryClient.invalidateQueries({ queryKey: ['files', currentPath] })
    } catch (e: any) {
      alert(e.message || String(e))
    }
  }

  const handleUpload = async () => {
    try {
      const localPath = await SelectFile()
      if (!localPath) return
      const filename = localPath.split('/').pop() || 'file'
      const remotePath = currentPath === '/' ? `/${filename}` : `${currentPath}/${filename}`
      await UploadFile(localPath, remotePath)
      queryClient.invalidateQueries({ queryKey: ['files', currentPath] })
    } catch (e: any) {
      alert(e.message || String(e))
    }
  }

  const handleRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ['files', currentPath] })
  }

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <Breadcrumb path={currentPath} onNavigate={setCurrentPath} />
        <div className="flex items-center gap-2">
          <button onClick={handleRefresh} className="p-2 hover:bg-bg-card-hover rounded-md transition-colors" title="Refresh">
            <RefreshCw className="h-4 w-4 text-text-secondary" />
          </button>
          <button onClick={handleNewFolder} className="p-2 hover:bg-bg-card-hover rounded-md transition-colors" title="New Folder">
            <FolderPlus className="h-4 w-4 text-text-secondary" />
          </button>
          <button onClick={handleUpload} className="flex items-center gap-2 px-3 py-1.5 bg-accent text-bg-primary text-sm font-medium rounded-md hover:bg-accent-hover transition-colors">
            <Upload className="h-4 w-4" />
            Upload
          </button>
        </div>
      </div>

      {isLoading ? (
        <div className="py-20 text-center text-text-muted">Loading...</div>
      ) : error ? (
        <div className="py-20 text-center text-error">Error: {String(error)}</div>
      ) : (
        <FileTable entries={files} onOpen={handleOpen} />
      )}
    </div>
  )
}
```

**Step 4: Verify with wails dev**

Expected: Files page shows breadcrumb, toolbar, and file table (empty initially). New Folder button works.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: files page with directory browsing, upload, new folder"
```

---

## Task 8: Frontend — Wallet Page

**Files:**
- Modify: `frontend/src/pages/Wallet.tsx`

**Step 1: Implement Wallet page**

Replace `frontend/src/pages/Wallet.tsx`:

```tsx
import { useQuery } from '@tanstack/react-query'
import { GetWalletInfo, LockWallet } from '../../wailsjs/go/main/App'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Copy, Lock, Wallet as WalletIcon } from 'lucide-react'
import { useState } from 'react'

export function WalletPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [copied, setCopied] = useState<string | null>(null)

  const { data: info, isLoading, error } = useQuery({
    queryKey: ['walletInfo'],
    queryFn: GetWalletInfo,
  })

  const handleCopy = (text: string, field: string) => {
    navigator.clipboard.writeText(text)
    setCopied(field)
    setTimeout(() => setCopied(null), 2000)
  }

  const handleLock = () => {
    LockWallet()
    queryClient.invalidateQueries({ queryKey: ['isUnlocked'] })
    navigate('/unlock')
  }

  if (isLoading) return <div className="p-6 text-text-muted">Loading...</div>
  if (error || !info) return <div className="p-6 text-error">Failed to load wallet info</div>

  return (
    <div className="p-6 space-y-6 max-w-2xl">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold flex items-center gap-2">
          <WalletIcon className="h-5 w-5 text-accent" />
          Wallet
        </h2>
        <button
          onClick={handleLock}
          className="flex items-center gap-2 px-3 py-1.5 text-sm text-text-secondary hover:text-error border border-border rounded-md hover:border-error/50 transition-colors"
        >
          <Lock className="h-4 w-4" />
          Lock
        </button>
      </div>

      <div className="space-y-4">
        <InfoRow
          label="Address"
          value={info.address}
          mono
          onCopy={() => handleCopy(info.address, 'address')}
          copied={copied === 'address'}
        />
        <InfoRow
          label="Public Key"
          value={info.publicKey}
          mono
          truncate
          onCopy={() => handleCopy(info.publicKey, 'pubkey')}
          copied={copied === 'pubkey'}
        />
        <InfoRow label="Network" value={info.network} />
        <InfoRow label="Derivation Path" value={info.path} mono />
      </div>
    </div>
  )
}

function InfoRow({
  label, value, mono, truncate, onCopy, copied,
}: {
  label: string
  value: string
  mono?: boolean
  truncate?: boolean
  onCopy?: () => void
  copied?: boolean
}) {
  return (
    <div className="bg-bg-card border border-border rounded-lg p-4">
      <div className="text-xs text-text-muted mb-1">{label}</div>
      <div className="flex items-center gap-2">
        <span className={`text-sm text-text-primary ${mono ? 'font-mono' : ''} ${truncate ? 'truncate' : ''} flex-1`}>
          {value}
        </span>
        {onCopy && (
          <button onClick={onCopy} className="p-1 hover:bg-bg-card-hover rounded transition-colors" title="Copy">
            {copied ? (
              <span className="text-xs text-success">Copied</span>
            ) : (
              <Copy className="h-3.5 w-3.5 text-text-muted" />
            )}
          </button>
        )}
      </div>
    </div>
  )
}
```

**Step 2: Verify with wails dev**

Expected: Wallet page shows address, public key, network, and derivation path. Copy and Lock buttons work.

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: wallet page with address, pubkey, copy, lock"
```

---

## Task 9: Frontend — Settings Page

**Files:**
- Modify: `frontend/src/pages/Settings.tsx`

**Step 1: Implement Settings page**

Replace `frontend/src/pages/Settings.tsx`:

```tsx
import { Settings as SettingsIcon, HardDrive, Globe, Info } from 'lucide-react'

export function SettingsPage() {
  return (
    <div className="p-6 space-y-6 max-w-2xl">
      <h2 className="text-xl font-semibold flex items-center gap-2">
        <SettingsIcon className="h-5 w-5 text-accent" />
        Settings
      </h2>

      <Section icon={Globe} title="Network">
        <SelectRow
          label="Network"
          value="mainnet"
          options={[
            { value: 'mainnet', label: 'Mainnet' },
            { value: 'testnet', label: 'Testnet' },
            { value: 'regtest', label: 'Regtest' },
          ]}
          onChange={() => {}}
        />
      </Section>

      <Section icon={HardDrive} title="Storage">
        <InfoRow label="Data Directory" value="~/.bitfs" />
      </Section>

      <Section icon={Info} title="About">
        <InfoRow label="Version" value="0.1.0" />
        <InfoRow label="Framework" value="Wails v2 + React 19" />
        <InfoRow label="License" value="OpenBSV License" />
      </Section>
    </div>
  )
}

function Section({ icon: Icon, title, children }: {
  icon: any; title: string; children: React.ReactNode
}) {
  return (
    <div className="bg-bg-card border border-border rounded-lg">
      <div className="flex items-center gap-2 px-4 py-3 border-b border-border">
        <Icon className="h-4 w-4 text-accent" />
        <h3 className="text-sm font-medium">{title}</h3>
      </div>
      <div className="divide-y divide-border/50">{children}</div>
    </div>
  )
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between px-4 py-3">
      <span className="text-sm text-text-secondary">{label}</span>
      <span className="text-sm text-text-primary font-mono">{value}</span>
    </div>
  )
}

function SelectRow({ label, value, options, onChange }: {
  label: string; value: string; options: { value: string; label: string }[]; onChange: (v: string) => void
}) {
  return (
    <div className="flex items-center justify-between px-4 py-3">
      <span className="text-sm text-text-secondary">{label}</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="bg-bg-input border border-border rounded px-2 py-1 text-sm text-text-primary outline-none"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    </div>
  )
}
```

**Step 2: Commit**

```bash
git add -A
git commit -m "feat: settings page with network, storage, about sections"
```

---

## Task 10: Integration Test + Production Build

**Files:**
- No new files

**Step 1: Run all Go tests**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs-desktop
go test -v -count=1 ./...
```

Expected: All wallet and file service tests pass.

**Step 2: Run frontend type check**

```bash
cd frontend
npx tsc --noEmit
```

Expected: No TypeScript errors.

**Step 3: Production build**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs-desktop
wails build
```

Expected: Binary at `build/bin/BitFS` (or `build/bin/BitFS.app` on macOS).

**Step 4: Manual test**

Launch the built binary and verify:
1. First launch → Onboarding page → Create wallet → Seed phrase shown
2. Restart → Unlock page → Enter password → Files page
3. Files page → New Folder → Folder appears
4. Wallet page → Shows address and pubkey → Copy works → Lock returns to unlock
5. Settings page → Network selector, version info

**Step 5: Final commit**

```bash
git add -A
git commit -m "chore: verify build and integration"
```

---

## Dependency Notes

- **vault.SaveWalletState**: May not exist as a standalone function. Check the vault package — the state may be saved internally by vault operations. If needed, implement as `json.Marshal(state)` → `os.WriteFile()`.
- **Address derivation**: The exact method to get a BSV address from a public key depends on go-sdk. Check `ec.PublicKey` methods — likely `.Address()` returns a `*script.Address` with `.AddressString`.
- **Wails bindings**: After modifying Go methods, run `wails generate module` or restart `wails dev` to regenerate `frontend/wailsjs/`.
- **Argon2id**: Wallet creation/unlock will be slow (~3s) due to hardened Argon2id. Consider showing a loading spinner during these operations.

## Testing Notes

- Go tests use `t.TempDir()` for isolation — no cleanup needed
- Frontend components can be tested with React Testing Library (not included in MVP scope)
- Integration between Go and React is tested via `wails dev` manual testing

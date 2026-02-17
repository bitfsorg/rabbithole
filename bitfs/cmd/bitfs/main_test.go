// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// CLI dispatch tests
// ---------------------------------------------------------------------------

func TestRunVersion(t *testing.T) {
	code := run([]string{"--version"})
	if code != exitSuccess {
		t.Errorf("run --version returned %d, want %d", code, exitSuccess)
	}
}

func TestRunHelp(t *testing.T) {
	code := run([]string{"--help"})
	if code != exitSuccess {
		t.Errorf("run --help returned %d, want %d", code, exitSuccess)
	}
}

func TestRunHelpAlias(t *testing.T) {
	code := run([]string{"help"})
	if code != exitSuccess {
		t.Errorf("run help returned %d, want %d", code, exitSuccess)
	}
}

func TestRunNoArgs(t *testing.T) {
	code := run(nil)
	if code != exitUsageError {
		t.Errorf("run (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	code := run([]string{"foobar"})
	if code != exitUsageError {
		t.Errorf("run unknown command returned %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// Wallet init/show tests
// ---------------------------------------------------------------------------

func initTestWallet(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testbitfs")

	code := runWalletInit([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Fatalf("runWalletInit returned %d, want %d", code, exitSuccess)
	}

	return dataDir
}

func TestWalletInit(t *testing.T) {
	dataDir := initTestWallet(t)

	// Verify wallet file exists.
	walletPath := filepath.Join(dataDir, "wallet.enc")
	if _, err := os.Stat(walletPath); err != nil {
		t.Errorf("wallet.enc not created: %v", err)
	}

	// Verify state file exists.
	statePath := filepath.Join(dataDir, "state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("state.json not created: %v", err)
	}

	// Verify config file exists.
	configPath := filepath.Join(dataDir, "config")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config not created: %v", err)
	}
}

func TestWalletInit24Words(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testbitfs")

	code := runWalletInit([]string{"--datadir", dataDir, "--password", "testpass", "--words", "24"})
	if code != exitSuccess {
		t.Fatalf("runWalletInit --words 24 returned %d, want %d", code, exitSuccess)
	}
}

func TestWalletInitInvalidWords(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testbitfs")

	code := runWalletInit([]string{"--datadir", dataDir, "--password", "testpass", "--words", "15"})
	if code != exitUsageError {
		t.Errorf("runWalletInit --words 15 returned %d, want %d", code, exitUsageError)
	}
}

func TestWalletInitAlreadyExists(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runWalletInit([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitWalletError {
		t.Errorf("second init returned %d, want %d", code, exitWalletError)
	}
}

func TestWalletShow(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runWalletShow([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("runWalletShow returned %d, want %d", code, exitSuccess)
	}
}

func TestWalletShowNoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runWalletShow([]string{"--datadir", dir, "--password", "testpass"})
	if code != exitWalletError {
		t.Errorf("runWalletShow (no wallet) returned %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// Vault CRUD tests
// ---------------------------------------------------------------------------

func TestVaultCreate(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runVaultCreate([]string{"--datadir", dataDir, "--password", "testpass", "myfiles"})
	if code != exitSuccess {
		t.Errorf("runVaultCreate returned %d, want %d", code, exitSuccess)
	}
}

func TestVaultCreateDuplicate(t *testing.T) {
	dataDir := initTestWallet(t)

	// "default" vault already exists from init.
	code := runVaultCreate([]string{"--datadir", dataDir, "--password", "testpass", "default"})
	if code != exitConflict {
		t.Errorf("duplicate vault create returned %d, want %d", code, exitConflict)
	}
}

func TestVaultCreateNoName(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runVaultCreate([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitUsageError {
		t.Errorf("vault create (no name) returned %d, want %d", code, exitUsageError)
	}
}

func TestVaultList(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runVaultList([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("runVaultList returned %d, want %d", code, exitSuccess)
	}
}

func TestVaultRename(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runVaultRename([]string{"--datadir", dataDir, "--password", "testpass", "default", "primary"})
	if code != exitSuccess {
		t.Errorf("runVaultRename returned %d, want %d", code, exitSuccess)
	}

	// Verify old name is gone.
	code = runVaultRename([]string{"--datadir", dataDir, "--password", "testpass", "default", "other"})
	if code != exitNotFound {
		t.Errorf("rename nonexistent vault returned %d, want %d", code, exitNotFound)
	}
}

func TestVaultRenameNoArgs(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runVaultRename([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitUsageError {
		t.Errorf("vault rename (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestVaultDelete(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runVaultDelete([]string{"--datadir", dataDir, "--password", "testpass", "default"})
	if code != exitSuccess {
		t.Errorf("runVaultDelete returned %d, want %d", code, exitSuccess)
	}
}

func TestVaultDeleteNotFound(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runVaultDelete([]string{"--datadir", dataDir, "--password", "testpass", "nonexistent"})
	if code != exitNotFound {
		t.Errorf("delete nonexistent vault returned %d, want %d", code, exitNotFound)
	}
}

func TestVaultDeleteNoArgs(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runVaultDelete([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitUsageError {
		t.Errorf("vault delete (no args) returned %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// Stub command tests (filesystem operations)
// ---------------------------------------------------------------------------

func TestPutStub(t *testing.T) {
	dataDir := initTestWallet(t)

	// Create a temp file to "upload".
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}

	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", tmpFile, "/docs/test.txt"})
	if code != exitSuccess {
		t.Errorf("runPut returned %d, want %d", code, exitSuccess)
	}
}

func TestPutNoArgs(t *testing.T) {
	code := runPut(nil)
	if code != exitUsageError {
		t.Errorf("runPut (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestPutBadAccess(t *testing.T) {
	dataDir := initTestWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	os.WriteFile(tmpFile, []byte("hello"), 0600)

	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", "--access", "invalid", tmpFile, "/test.txt"})
	if code != exitUsageError {
		t.Errorf("runPut bad access returned %d, want %d", code, exitUsageError)
	}
}

func TestMkdirStub(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runMkdir([]string{"--datadir", dataDir, "--password", "testpass", "/docs"})
	if code != exitSuccess {
		t.Errorf("runMkdir returned %d, want %d", code, exitSuccess)
	}
}

func TestMkdirNoArgs(t *testing.T) {
	code := runMkdir(nil)
	if code != exitUsageError {
		t.Errorf("runMkdir (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestRmStub(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runRm([]string{"--datadir", dataDir, "--password", "testpass", "/docs/test.txt"})
	if code != exitSuccess {
		t.Errorf("runRm returned %d, want %d", code, exitSuccess)
	}
}

func TestRmNoArgs(t *testing.T) {
	code := runRm(nil)
	if code != exitUsageError {
		t.Errorf("runRm (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestMvStub(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runMv([]string{"--datadir", dataDir, "--password", "testpass", "/a.txt", "/b.txt"})
	if code != exitSuccess {
		t.Errorf("runMv returned %d, want %d", code, exitSuccess)
	}
}

func TestMvNoArgs(t *testing.T) {
	code := runMv(nil)
	if code != exitUsageError {
		t.Errorf("runMv (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestLinkStub(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runLink([]string{"--datadir", dataDir, "--password", "testpass", "/target.txt", "/link.txt"})
	if code != exitSuccess {
		t.Errorf("runLink returned %d, want %d", code, exitSuccess)
	}
}

func TestLinkSoftStub(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runLink([]string{"--datadir", dataDir, "--password", "testpass", "--soft", "/target.txt", "/link.txt"})
	if code != exitSuccess {
		t.Errorf("runLink --soft returned %d, want %d", code, exitSuccess)
	}
}

func TestLinkNoArgs(t *testing.T) {
	code := runLink(nil)
	if code != exitUsageError {
		t.Errorf("runLink (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestSellStub(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runSell([]string{"--datadir", dataDir, "--password", "testpass", "--price", "50", "/premium/data.csv"})
	if code != exitSuccess {
		t.Errorf("runSell returned %d, want %d", code, exitSuccess)
	}
}

func TestSellNoPrice(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runSell([]string{"--datadir", dataDir, "--password", "testpass", "/premium/data.csv"})
	if code != exitUsageError {
		t.Errorf("runSell (no price) returned %d, want %d", code, exitUsageError)
	}
}

func TestSellNoArgs(t *testing.T) {
	code := runSell(nil)
	if code != exitUsageError {
		t.Errorf("runSell (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestEncryptStub(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runEncrypt([]string{"--datadir", dataDir, "--password", "testpass", "/docs/secret.txt"})
	if code != exitSuccess {
		t.Errorf("runEncrypt returned %d, want %d", code, exitSuccess)
	}
}

func TestEncryptNoArgs(t *testing.T) {
	code := runEncrypt(nil)
	if code != exitUsageError {
		t.Errorf("runEncrypt (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestPublishStub(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runPublish([]string{"--datadir", dataDir, "--password", "testpass", "example.com"})
	if code != exitSuccess {
		t.Errorf("runPublish returned %d, want %d", code, exitSuccess)
	}
}

func TestPublishNoArgs(t *testing.T) {
	code := runPublish(nil)
	if code != exitUsageError {
		t.Errorf("runPublish (no args) returned %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// Daemon stub tests
// ---------------------------------------------------------------------------

func TestDaemonStart(t *testing.T) {
	code := runDaemonStart(nil)
	if code != exitSuccess {
		t.Errorf("runDaemonStart returned %d, want %d", code, exitSuccess)
	}
}

func TestDaemonStop(t *testing.T) {
	code := runDaemonStop(nil)
	if code != exitSuccess {
		t.Errorf("runDaemonStop returned %d, want %d", code, exitSuccess)
	}
}

func TestDaemonNoSubcommand(t *testing.T) {
	code := runDaemon(nil)
	if code != exitUsageError {
		t.Errorf("runDaemon (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestDaemonHelp(t *testing.T) {
	code := runDaemon([]string{"--help"})
	if code != exitSuccess {
		t.Errorf("runDaemon --help returned %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// Shell stub test
// ---------------------------------------------------------------------------

func TestShellStub(t *testing.T) {
	code := runShell(nil)
	if code != exitSuccess {
		t.Errorf("runShell returned %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// Wallet dispatch tests
// ---------------------------------------------------------------------------

func TestWalletHelp(t *testing.T) {
	code := runWallet([]string{"--help"})
	if code != exitSuccess {
		t.Errorf("runWallet --help returned %d, want %d", code, exitSuccess)
	}
}

func TestWalletNoSubcommand(t *testing.T) {
	code := runWallet(nil)
	if code != exitUsageError {
		t.Errorf("runWallet (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestWalletUnknownSubcommand(t *testing.T) {
	code := runWallet([]string{"foobar"})
	if code != exitUsageError {
		t.Errorf("runWallet unknown returned %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// Vault dispatch tests
// ---------------------------------------------------------------------------

func TestVaultHelp(t *testing.T) {
	code := runVault([]string{"--help"})
	if code != exitSuccess {
		t.Errorf("runVault --help returned %d, want %d", code, exitSuccess)
	}
}

func TestVaultNoSubcommand(t *testing.T) {
	code := runVault(nil)
	if code != exitUsageError {
		t.Errorf("runVault (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestVaultUnknownSubcommand(t *testing.T) {
	code := runVault([]string{"foobar"})
	if code != exitUsageError {
		t.Errorf("runVault unknown returned %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// pathToIndices tests
// ---------------------------------------------------------------------------

func TestPathToIndices(t *testing.T) {
	tests := []struct {
		path string
		want int // expected number of indices
	}{
		{"/docs", 1},
		{"/docs/readme.txt", 2},
		{"/a/b/c/d", 4},
		{"/", 0},
		{"", 0},
	}

	for _, tc := range tests {
		indices := pathToIndices(tc.path)
		if len(indices) != tc.want {
			t.Errorf("pathToIndices(%q) = %d indices, want %d", tc.path, len(indices), tc.want)
		}
	}
}

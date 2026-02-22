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

	code := runWalletInit([]string{"--datadir", dataDir, "--password", "testpass", "--network", "regtest"})
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

	code := runWalletInit([]string{"--datadir", dataDir, "--password", "testpass", "--words", "24", "--network", "regtest"})
	if code != exitSuccess {
		t.Fatalf("runWalletInit --words 24 returned %d, want %d", code, exitSuccess)
	}
}

func TestWalletInitInvalidWords(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testbitfs")

	code := runWalletInit([]string{"--datadir", dataDir, "--password", "testpass", "--words", "15", "--network", "regtest"})
	if code != exitUsageError {
		t.Errorf("runWalletInit --words 15 returned %d, want %d", code, exitUsageError)
	}
}

func TestWalletInitAlreadyExists(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runWalletInit([]string{"--datadir", dataDir, "--password", "testpass", "--network", "regtest"})
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
// Filesystem command tests (require engine + UTXOs)
// ---------------------------------------------------------------------------

func TestPutNoUTXO(t *testing.T) {
	dataDir := initTestWallet(t)

	// Create a temp file to "upload".
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}

	// Without funded UTXOs, put should fail with exitError.
	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", tmpFile, "/docs/test.txt"})
	if code == exitSuccess {
		t.Errorf("runPut without UTXOs should not succeed (got %d)", code)
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

func TestMkdirNoUTXO(t *testing.T) {
	dataDir := initTestWallet(t)

	// Without funded UTXOs, mkdir should fail.
	code := runMkdir([]string{"--datadir", dataDir, "--password", "testpass", "/docs"})
	if code == exitSuccess {
		t.Errorf("runMkdir without UTXOs should not succeed (got %d)", code)
	}
}

func TestMkdirNoArgs(t *testing.T) {
	code := runMkdir(nil)
	if code != exitUsageError {
		t.Errorf("runMkdir (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestRmNoNode(t *testing.T) {
	dataDir := initTestWallet(t)

	// No node exists at this path, so rm should fail.
	code := runRm([]string{"--datadir", dataDir, "--password", "testpass", "/docs/test.txt"})
	if code == exitSuccess {
		t.Errorf("runRm on nonexistent node should not succeed (got %d)", code)
	}
}

func TestRmNoArgs(t *testing.T) {
	code := runRm(nil)
	if code != exitUsageError {
		t.Errorf("runRm (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestMvNoNode(t *testing.T) {
	dataDir := initTestWallet(t)

	// No node exists, so mv should fail.
	code := runMv([]string{"--datadir", dataDir, "--password", "testpass", "/a.txt", "/b.txt"})
	if code == exitSuccess {
		t.Errorf("runMv on nonexistent node should not succeed (got %d)", code)
	}
}

func TestMvNoArgs(t *testing.T) {
	code := runMv(nil)
	if code != exitUsageError {
		t.Errorf("runMv (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestLinkNoNode(t *testing.T) {
	dataDir := initTestWallet(t)

	// No target exists, so link should fail.
	code := runLink([]string{"--datadir", dataDir, "--password", "testpass", "/target.txt", "/link.txt"})
	if code == exitSuccess {
		t.Errorf("runLink on nonexistent target should not succeed (got %d)", code)
	}
}

func TestLinkSoftNoNode(t *testing.T) {
	dataDir := initTestWallet(t)

	// No target exists, so soft link should fail.
	code := runLink([]string{"--datadir", dataDir, "--password", "testpass", "--soft", "/target.txt", "/link.txt"})
	if code == exitSuccess {
		t.Errorf("runLink --soft on nonexistent target should not succeed (got %d)", code)
	}
}

func TestLinkNoArgs(t *testing.T) {
	code := runLink(nil)
	if code != exitUsageError {
		t.Errorf("runLink (no args) returned %d, want %d", code, exitUsageError)
	}
}

func TestSellNoNode(t *testing.T) {
	dataDir := initTestWallet(t)

	// No node exists at this path, so sell should fail.
	code := runSell([]string{"--datadir", dataDir, "--password", "testpass", "--price", "50", "/premium/data.csv"})
	if code == exitSuccess {
		t.Errorf("runSell on nonexistent node should not succeed (got %d)", code)
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

func TestEncryptNoNode(t *testing.T) {
	dataDir := initTestWallet(t)

	// No node exists at this path, so encrypt should fail.
	code := runEncrypt([]string{"--datadir", dataDir, "--password", "testpass", "/docs/secret.txt"})
	if code == exitSuccess {
		t.Errorf("runEncrypt on nonexistent node should not succeed (got %d)", code)
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

func TestPublishNoArgs_NoWallet(t *testing.T) {
	// With no domain but no valid wallet, should fail with wallet error.
	dir := t.TempDir()
	code := runPublish([]string{"--datadir", dir})
	if code != exitWalletError {
		t.Errorf("runPublish (no domain, no wallet) returned %d, want %d", code, exitWalletError)
	}
}

func TestPublishNoArgs_ListsBindings(t *testing.T) {
	// With a valid wallet but no domain, publish lists bindings.
	dataDir := initTestWallet(t)
	code := runPublish([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("runPublish (no domain) returned %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// Daemon tests
// ---------------------------------------------------------------------------

func TestDaemonStartNoWallet(t *testing.T) {
	// Without a wallet at the default datadir, daemon start should fail.
	dir := t.TempDir()
	code := runDaemonStart([]string{"--datadir", dir})
	if code != exitWalletError {
		t.Errorf("runDaemonStart (no wallet) returned %d, want %d", code, exitWalletError)
	}
}

func TestDaemonStopNoPID(t *testing.T) {
	// Without a PID file, daemon stop should fail.
	dir := t.TempDir()
	code := runDaemonStop([]string{"--datadir", dir})
	if code != exitNotFound {
		t.Errorf("runDaemonStop (no PID) returned %d, want %d", code, exitNotFound)
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
// Shell test
// ---------------------------------------------------------------------------

func TestShellNoWallet(t *testing.T) {
	// Without a wallet, shell should fail with wallet error.
	dir := t.TempDir()
	code := runShell([]string{"--datadir", dir})
	if code != exitWalletError {
		t.Errorf("runShell (no wallet) returned %d, want %d", code, exitWalletError)
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

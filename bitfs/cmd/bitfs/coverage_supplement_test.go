package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// run() — uncovered switch branches (52% → higher)
// ---------------------------------------------------------------------------

func TestRun_AllCommands_NoWallet(t *testing.T) {
	// These commands require a wallet but none exists — exercises the
	// loadWalletFromDataDir error branch in each command.
	bogusDir := filepath.Join(t.TempDir(), "nonexistent")
	cmds := []struct {
		name string
		args []string
	}{
		{"put", []string{"put", "--datadir", bogusDir, "file.txt", "/path"}},
		{"mkdir", []string{"mkdir", "--datadir", bogusDir, "/dir"}},
		{"rm", []string{"rm", "--datadir", bogusDir, "/file"}},
		{"mv", []string{"mv", "--datadir", bogusDir, "/a", "/b"}},
		{"link", []string{"link", "--datadir", bogusDir, "/target", "/link"}},
		{"sell", []string{"sell", "--datadir", bogusDir, "--price", "100", "/file"}},
		{"encrypt", []string{"encrypt", "--datadir", bogusDir, "/file"}},
		{"publish", []string{"publish", "--datadir", bogusDir, "example.com"}},
	}
	for _, tc := range cmds {
		code := run(tc.args)
		if code == exitSuccess {
			t.Errorf("run %s with missing wallet should not succeed", tc.name)
		}
	}
}

func TestRun_DaemonSubcommands(t *testing.T) {
	tmpDir := t.TempDir()
	tests := []struct {
		args []string
		want int
	}{
		// start needs a wallet → fails with wallet error
		{[]string{"daemon", "start", "--datadir", tmpDir}, exitWalletError},
		// stop needs a PID file → fails with not found
		{[]string{"daemon", "stop", "--datadir", tmpDir}, exitNotFound},
		{[]string{"daemon", "--help"}, exitSuccess},
		{[]string{"daemon", "-h"}, exitSuccess},
		{[]string{"daemon", "restart"}, exitUsageError},
	}
	for _, tc := range tests {
		code := run(tc.args)
		if code != tc.want {
			t.Errorf("run(%v) = %d, want %d", tc.args, code, tc.want)
		}
	}
}

func TestRun_ShellCommand_NoWallet(t *testing.T) {
	tmpDir := t.TempDir()
	code := run([]string{"shell", "--datadir", tmpDir})
	if code != exitWalletError {
		t.Errorf("run shell (no wallet) = %d, want %d", code, exitWalletError)
	}
}

func TestRun_HelpAlias(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		code := run([]string{arg})
		if code != exitSuccess {
			t.Errorf("run(%s) = %d, want %d", arg, code, exitSuccess)
		}
	}
}

// ---------------------------------------------------------------------------
// resolveVaultIndex — (44.4% → higher)
// ---------------------------------------------------------------------------

func TestResolveVaultIndex_DefaultVault(t *testing.T) {
	dataDir := initTestWallet(t)
	w, state, err := loadWalletFromDataDir(dataDir, "testpass")
	if err != nil {
		t.Fatalf("loadWalletFromDataDir: %v", err)
	}
	// Empty name should return first active vault.
	idx, err := resolveVaultIndex(w, state, "")
	if err != nil {
		t.Fatalf("resolveVaultIndex empty name: %v", err)
	}
	if idx != 0 {
		t.Errorf("resolveVaultIndex default = %d, want 0", idx)
	}
}

func TestResolveVaultIndex_ByName(t *testing.T) {
	dataDir := initTestWallet(t)
	w, state, err := loadWalletFromDataDir(dataDir, "testpass")
	if err != nil {
		t.Fatalf("loadWalletFromDataDir: %v", err)
	}
	// "default" vault should be findable.
	idx, err := resolveVaultIndex(w, state, "default")
	if err != nil {
		t.Fatalf("resolveVaultIndex by name: %v", err)
	}
	if idx != 0 {
		t.Errorf("resolveVaultIndex 'default' = %d, want 0", idx)
	}
}

func TestResolveVaultIndex_NonexistentName(t *testing.T) {
	dataDir := initTestWallet(t)
	w, state, err := loadWalletFromDataDir(dataDir, "testpass")
	if err != nil {
		t.Fatalf("loadWalletFromDataDir: %v", err)
	}
	_, err = resolveVaultIndex(w, state, "nonexistent")
	if err == nil {
		t.Error("resolveVaultIndex nonexistent should fail")
	}
}

func TestResolveVaultIndex_NamedVaultAfterCreate(t *testing.T) {
	dataDir := initTestWallet(t)
	// Create a second vault.
	code := runVaultCreate([]string{"--datadir", dataDir, "--password", "testpass", "photos"})
	if code != exitSuccess {
		t.Fatalf("vault create returned %d", code)
	}

	w, state, err := loadWalletFromDataDir(dataDir, "testpass")
	if err != nil {
		t.Fatalf("loadWalletFromDataDir: %v", err)
	}
	idx, err := resolveVaultIndex(w, state, "photos")
	if err != nil {
		t.Fatalf("resolveVaultIndex 'photos': %v", err)
	}
	// photos should be vault index 1 (default is 0).
	if idx != 1 {
		t.Errorf("resolveVaultIndex 'photos' = %d, want 1", idx)
	}
}

// ---------------------------------------------------------------------------
// runVaultList — empty vault state (68.2% → higher)
// ---------------------------------------------------------------------------

func TestVaultList_AfterDeleteAll(t *testing.T) {
	dataDir := initTestWallet(t) // creates "default"
	// Delete the default vault.
	code := runVaultDelete([]string{"--datadir", dataDir, "--password", "testpass", "default"})
	if code != exitSuccess {
		t.Fatalf("vault delete returned %d", code)
	}
	// List should show "No vaults found".
	out := captureStdout(t, func() {
		code := runVaultList([]string{"--datadir", dataDir, "--password", "testpass"})
		if code != exitSuccess {
			t.Fatalf("vault list returned %d", code)
		}
	})
	if !strings.Contains(out, "No vaults found") {
		t.Errorf("vault list after delete all: %q missing 'No vaults found'", out)
	}
}

func TestVaultList_NoWallet(t *testing.T) {
	bogusDir := filepath.Join(t.TempDir(), "nowallethere")
	code := runVaultList([]string{"--datadir", bogusDir})
	if code != exitWalletError {
		t.Errorf("vault list with no wallet = %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// runWalletInit — error paths (69.9% → higher)
// ---------------------------------------------------------------------------

func TestWalletInit_24Words(t *testing.T) {
	dir := t.TempDir()
	out := captureStdout(t, func() {
		code := runWalletInit([]string{"--datadir", dir, "--password", "p", "--words", "24", "--network", "regtest"})
		if code != exitSuccess {
			t.Fatalf("wallet init 24 words = %d", code)
		}
	})
	if !strings.Contains(out, "Words:          24") {
		t.Errorf("wallet init 24 words output missing '24': %q", out)
	}
}

func TestWalletInit_InvalidWordCount(t *testing.T) {
	for _, w := range []string{"6", "18", "0", "48"} {
		code := runWalletInit([]string{"--datadir", t.TempDir(), "--password", "p", "--words", w, "--network", "regtest"})
		if code != exitUsageError {
			t.Errorf("wallet init --words %s = %d, want %d", w, code, exitUsageError)
		}
	}
}

// ---------------------------------------------------------------------------
// loadWalletFromDataDir — error paths (72.2% → higher)
// ---------------------------------------------------------------------------

func TestLoadWalletFromDataDir_NoWallet(t *testing.T) {
	_, _, err := loadWalletFromDataDir(t.TempDir(), "pass")
	if err == nil {
		t.Error("loadWalletFromDataDir with no wallet should fail")
	}
}

func TestLoadWalletFromDataDir_CorruptedWallet(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "wallet.enc"), []byte("corrupted"), 0600)
	os.WriteFile(filepath.Join(dir, "state.json"), []byte("{}"), 0600)
	_, _, err := loadWalletFromDataDir(dir, "pass")
	if err == nil {
		t.Error("loadWalletFromDataDir with corrupted wallet should fail")
	}
}

func TestLoadWalletFromDataDir_MissingState(t *testing.T) {
	dataDir := initTestWallet(t)
	// Remove state.json.
	os.Remove(filepath.Join(dataDir, "state.json"))
	_, _, err := loadWalletFromDataDir(dataDir, "testpass")
	if err == nil {
		t.Error("loadWalletFromDataDir with missing state should fail")
	}
}

func TestLoadWalletFromDataDir_CorruptedState(t *testing.T) {
	dataDir := initTestWallet(t)
	os.WriteFile(filepath.Join(dataDir, "state.json"), []byte("not json"), 0600)
	_, _, err := loadWalletFromDataDir(dataDir, "testpass")
	if err == nil {
		t.Error("loadWalletFromDataDir with corrupted state should fail")
	}
}

// ---------------------------------------------------------------------------
// saveWalletState / loadWalletState — error paths (75% / 71.4% → higher)
// ---------------------------------------------------------------------------

func TestSaveWalletState_InvalidPath(t *testing.T) {
	err := saveWalletState("/nonexistent/dir/state.json", nil)
	// nil state should cause marshal error or path error.
	if err == nil {
		t.Error("saveWalletState to invalid path should fail")
	}
}

func TestLoadWalletState_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte("{invalid json"), 0600)
	_, err := loadWalletState(path)
	if err == nil {
		t.Error("loadWalletState with invalid JSON should fail")
	}
}

func TestLoadWalletState_NonexistentFile(t *testing.T) {
	_, err := loadWalletState(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Error("loadWalletState with missing file should fail")
	}
}

// ---------------------------------------------------------------------------
// cmd with --vault flag — exercises resolveVaultIndex through commands
// ---------------------------------------------------------------------------

func TestPut_WithVaultName(t *testing.T) {
	dataDir := initTestWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "data.bin")
	os.WriteFile(tmpFile, []byte("content"), 0600)

	// With valid vault but no UTXOs, put should fail (not succeed).
	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "default", tmpFile, "/data.bin"})
	if code == exitSuccess {
		t.Errorf("put with --vault default but no UTXOs should not succeed")
	}
}

func TestPut_WithNonexistentVault(t *testing.T) {
	dataDir := initTestWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "data.bin")
	os.WriteFile(tmpFile, []byte("content"), 0600)

	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nope", tmpFile, "/data.bin"})
	if code != exitNotFound {
		t.Errorf("put with --vault nope = %d, want %d", code, exitNotFound)
	}
}

func TestPut_FileNotFound(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", "/nonexistent/file.txt", "/dest"})
	if code != exitNotFound {
		t.Errorf("put with missing file = %d, want %d", code, exitNotFound)
	}
}

func TestMkdir_WithVaultName(t *testing.T) {
	dataDir := initTestWallet(t)
	// Valid vault, but no UTXOs funded — should fail.
	code := runMkdir([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "default", "/mydir"})
	if code == exitSuccess {
		t.Error("mkdir with --vault but no UTXOs should not succeed")
	}
}

func TestRm_WithVaultName(t *testing.T) {
	dataDir := initTestWallet(t)
	// No node exists at this path — should fail.
	code := runRm([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "default", "/file"})
	if code == exitSuccess {
		t.Error("rm on nonexistent node should not succeed")
	}
}

func TestMv_WithVaultName(t *testing.T) {
	dataDir := initTestWallet(t)
	// No node exists at src — should fail.
	code := runMv([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "default", "/src", "/dst"})
	if code == exitSuccess {
		t.Error("mv on nonexistent node should not succeed")
	}
}

func TestLink_WithVaultName(t *testing.T) {
	dataDir := initTestWallet(t)
	// No target exists — should fail.
	code := runLink([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "default", "/target", "/link"})
	if code == exitSuccess {
		t.Error("link on nonexistent target should not succeed")
	}
}

func TestSell_WithVaultName(t *testing.T) {
	dataDir := initTestWallet(t)
	// No node exists at path — should fail.
	code := runSell([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "default", "--price", "500", "/file"})
	if code == exitSuccess {
		t.Error("sell on nonexistent node should not succeed")
	}
}

func TestEncrypt_WithVaultName(t *testing.T) {
	dataDir := initTestWallet(t)
	// No node exists at path — should fail.
	code := runEncrypt([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "default", "/file"})
	if code == exitSuccess {
		t.Error("encrypt on nonexistent node should not succeed")
	}
}

func TestPublish_WithVaultName(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runPublish([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "default", "example.com"})
	if code != exitSuccess {
		t.Errorf("publish with --vault = %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// Wallet & vault command help variants
// ---------------------------------------------------------------------------

func TestWalletShow_InvalidFlags(t *testing.T) {
	code := runWalletShow([]string{"--badarg"})
	if code != exitUsageError {
		t.Errorf("wallet show --badarg = %d, want %d", code, exitUsageError)
	}
}

func TestVaultCreate_InvalidFlags(t *testing.T) {
	code := runVaultCreate([]string{"--badarg"})
	if code != exitUsageError {
		t.Errorf("vault create --badarg = %d, want %d", code, exitUsageError)
	}
}

func TestVaultRename_InvalidFlags(t *testing.T) {
	code := runVaultRename([]string{"--badarg"})
	if code != exitUsageError {
		t.Errorf("vault rename --badarg = %d, want %d", code, exitUsageError)
	}
}

func TestVaultDelete_InvalidFlags(t *testing.T) {
	code := runVaultDelete([]string{"--badarg"})
	if code != exitUsageError {
		t.Errorf("vault delete --badarg = %d, want %d", code, exitUsageError)
	}
}

func TestDaemonStart_NoWallet(t *testing.T) {
	tmpDir := t.TempDir()
	code := runDaemonStart([]string{"--listen", ":9090", "--datadir", tmpDir})
	if code != exitWalletError {
		t.Errorf("daemon start (no wallet) = %d, want %d", code, exitWalletError)
	}
}

func TestDaemonStop_NoPIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	code := runDaemonStop([]string{"--datadir", tmpDir})
	if code != exitNotFound {
		t.Errorf("daemon stop (no PID) = %d, want %d", code, exitNotFound)
	}
}

func TestShell_NoWallet(t *testing.T) {
	tmpDir := t.TempDir()
	code := runShell([]string{"--vault", "test", "--datadir", tmpDir})
	if code != exitWalletError {
		t.Errorf("shell (no wallet) = %d, want %d", code, exitWalletError)
	}
}

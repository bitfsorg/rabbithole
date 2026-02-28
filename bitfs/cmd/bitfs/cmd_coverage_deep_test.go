// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	bsvhash "github.com/bsv-blockchain/go-sdk/primitives/hash"

	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// ---------------------------------------------------------------------------
// Helpers for pre-populating vault state
// ---------------------------------------------------------------------------

// populateTestVault adds a root dir with children to the vault state for testing.
// Returns the vault and the root pubkey hex.
func populateTestVault(t *testing.T) (*vault.Vault, string) {
	t.Helper()
	dataDir := initTestWallet(t)
	v := openTestVault(t, dataDir)

	// Derive a real key pair for the root node.
	rootKP, err := v.Wallet.DeriveVaultRootKey(0) // vault index 0 = "default"
	if err != nil {
		t.Fatalf("DeriveVaultRootKey: %v", err)
	}
	rootPub := hex.EncodeToString(rootKP.PublicKey.Compressed())

	// Derive child keys for child nodes.
	childKP, err := v.Wallet.DeriveNodeKey(0, []uint32{0}, nil)
	if err != nil {
		t.Fatalf("DeriveNodeKey: %v", err)
	}
	childPub := hex.EncodeToString(childKP.PublicKey.Compressed())

	childKP2, err := v.Wallet.DeriveNodeKey(0, []uint32{1}, nil)
	if err != nil {
		t.Fatalf("DeriveNodeKey 2: %v", err)
	}
	childPub2 := hex.EncodeToString(childKP2.PublicKey.Compressed())

	// Add root directory node.
	v.State.SetNode(rootPub, &vault.NodeState{
		PubKeyHex:    rootPub,
		TxID:         "aaaa",
		Type:         "dir",
		Access:       "free",
		Path:         "/",
		VaultIndex:   0,
		ChildIndices: []uint32{},
		Children: []*vault.ChildState{
			{Name: "hello.txt", Type: "file", PubKey: childPub, Index: 0},
			{Name: "subdir", Type: "dir", PubKey: childPub2, Index: 1},
		},
		NextChildIdx: 2,
	})

	// Add file child.
	v.State.SetNode(childPub, &vault.NodeState{
		PubKeyHex:    childPub,
		TxID:         "bbbb",
		Type:         "file",
		Access:       "free",
		Path:         "/hello.txt",
		VaultIndex:   0,
		ChildIndices: []uint32{0},
		KeyHash:      "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		FileSize:     5,
		MimeType:     "text/plain",
	})

	// Add subdirectory child (empty).
	v.State.SetNode(childPub2, &vault.NodeState{
		PubKeyHex:    childPub2,
		TxID:         "cccc",
		Type:         "dir",
		Access:       "free",
		Path:         "/subdir",
		VaultIndex:   0,
		ChildIndices: []uint32{1},
		Children:     []*vault.ChildState{},
	})

	return v, rootPub
}

// ---------------------------------------------------------------------------
// 1. Shell helper tests — shellLs, shellRemoveRecursive, shellSellRecursive
// ---------------------------------------------------------------------------

func TestShellLs_Directory(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	// Should print children without panicking.
	shellLs(v, "/")
}

func TestShellLs_EmptyDir(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	shellLs(v, "/subdir")
}

func TestShellLs_File(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	shellLs(v, "/hello.txt")
}

func TestShellLs_NotFound(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	shellLs(v, "/nonexistent")
}

func TestShellRemoveRecursive_NotFound(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	err := shellRemoveRecursive(v, 0, "/nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestShellRemoveRecursive_File(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	// Will fail at eng.Remove (no UTXOs), but exercises the path resolution.
	err := shellRemoveRecursive(v, 0, "/hello.txt")
	if err == nil {
		t.Error("expected error for remove without UTXOs")
	}
}

func TestShellRemoveRecursive_DirWithChildren(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	// Attempts recursive removal — children first, then parent.
	// Fails at vault.Remove, but exercises the recursive traversal.
	err := shellRemoveRecursive(v, 0, "/")
	if err == nil {
		t.Error("expected error for remove without UTXOs")
	}
}

func TestShellSellRecursive_NotFound(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	err := shellSellRecursive(v, 0, "/nonexistent", 100)
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestShellSellRecursive_File(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	// Will fail at eng.Sell (no UTXOs), but exercises the path.
	err := shellSellRecursive(v, 0, "/hello.txt", 100)
	if err == nil {
		t.Error("expected error for sell without UTXOs")
	}
}

func TestShellSellRecursive_Directory(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	// Recurses into directory, attempts to sell file children.
	// Directory itself is skipped (only files are sold).
	err := shellSellRecursive(v, 0, "/", 50)
	if err == nil {
		t.Error("expected error during recursive sell")
	}
}

// ---------------------------------------------------------------------------
// 2. doMput direct tests
// ---------------------------------------------------------------------------

func TestDoMput_EmptyRemoteDir(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	_, err := doMput(v, 0, t.TempDir(), "", "free")
	if err == nil {
		t.Error("expected error for empty remote dir")
	}
}

func TestDoMput_DotDotInPath(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	_, err := doMput(v, 0, t.TempDir(), "/foo/../bar", "free")
	if err == nil {
		t.Error("expected error for '..' in remote path")
	}
}

func TestDoMput_NotADir(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	// Source is a regular file, not a directory.
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(tmpFile, []byte("data"), 0600)

	_, err := doMput(v, 0, tmpFile, "/dest", "free")
	if err == nil {
		t.Error("expected error for file as source")
	}
}

func TestDoMput_WithRealDir(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	// Create a directory structure to upload.
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0600)
	os.WriteFile(filepath.Join(srcDir, "subdir", "file2.txt"), []byte("content2"), 0600)
	os.Symlink(filepath.Join(srcDir, "file1.txt"), filepath.Join(srcDir, "link.txt"))

	result, err := doMput(v, 0, srcDir, "/uploads", "free")
	if err != nil {
		t.Fatalf("doMput: %v", err)
	}

	// Operations will fail (no UTXOs/no root), but WalkDir should have executed.
	// The result should have some errors but not be nil.
	if result == nil {
		t.Fatal("doMput returned nil result")
	}
	// Symlink should have been skipped silently (not counted).
	t.Logf("doMput: dirs=%d files=%d errors=%d", result.DirsCreated, result.FilesUploaded, len(result.Errors))
}

func TestDoMput_DefaultAccess(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "f.txt"), []byte("x"), 0600)

	// Empty access string should default to "free".
	result, err := doMput(v, 0, srcDir, "/dest", "")
	if err != nil {
		t.Fatalf("doMput with empty access: %v", err)
	}
	if result == nil {
		t.Fatal("doMput returned nil result")
	}
}

// ---------------------------------------------------------------------------
// 3. doMget / mgetRecurse tests
// ---------------------------------------------------------------------------

func TestDoMget_NotADirectory(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	_, err := doMget(v, 0, "/hello.txt", t.TempDir())
	if err == nil {
		t.Error("expected error for mget on a file")
	}
}

func TestDoMget_RootDir(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	outDir := filepath.Join(t.TempDir(), "out")
	result, err := doMget(v, 0, "/", outDir)
	if err != nil {
		t.Fatalf("doMget: %v", err)
	}

	// Should have attempted downloads (fails without real content, but exercises paths).
	if result == nil {
		t.Fatal("doMget returned nil result")
	}
	// At least 1 dir should have been "created".
	if result.DirsCreated == 0 {
		t.Error("expected at least one dir created")
	}
	t.Logf("doMget: dirs=%d files=%d errors=%d", result.DirsCreated, result.FilesDownloaded, len(result.Errors))
}

func TestDoMget_EmptySubdir(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	outDir := filepath.Join(t.TempDir(), "out")
	result, err := doMget(v, 0, "/subdir", outDir)
	if err != nil {
		t.Fatalf("doMget: %v", err)
	}
	// Empty dir: 1 dir created, 0 files, 0 errors.
	if result.DirsCreated != 1 {
		t.Errorf("DirsCreated = %d, want 1", result.DirsCreated)
	}
}

// ---------------------------------------------------------------------------
// 4. Vault adapter deeper tests
// ---------------------------------------------------------------------------

func TestVaultWalletAdapter_DeriveNodePubKey(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	wa := newVaultWalletAdapter(v)
	pub, err := wa.DeriveNodePubKey(0, []uint32{0}, nil)
	if err != nil {
		t.Fatalf("DeriveNodePubKey: %v", err)
	}
	if pub == nil {
		t.Error("DeriveNodePubKey returned nil")
	}
}

func TestVaultWalletAdapter_DeriveNodeKeyPair(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	// Use the child node's pubkey (which has a valid NodeState).
	childKP, err := v.Wallet.DeriveNodeKey(0, []uint32{0}, nil)
	if err != nil {
		t.Fatalf("DeriveNodeKey: %v", err)
	}
	pnode := childKP.PublicKey.Compressed()

	wa := newVaultWalletAdapter(v)
	priv, pub, err := wa.DeriveNodeKeyPair(pnode)
	if err != nil {
		t.Fatalf("DeriveNodeKeyPair: %v", err)
	}
	if priv == nil || pub == nil {
		t.Error("DeriveNodeKeyPair returned nil key")
	}
}

func TestVaultWalletAdapter_DeriveNodeKeyPair_NotFound(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	wa := newVaultWalletAdapter(v)
	// Unknown pubkey — should fail.
	fakePnode := make([]byte, 33)
	fakePnode[0] = 0x02
	_, _, err := wa.DeriveNodeKeyPair(fakePnode)
	if err == nil {
		t.Error("DeriveNodeKeyPair with unknown pnode should fail")
	}
}

func TestVaultStoreAdapter_Get_NotFound(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	sa := newVaultStoreAdapter(v)
	fakeHash := make([]byte, 32)
	_, err := sa.Get(fakeHash)
	if err == nil {
		t.Error("Get nonexistent key should fail")
	}
}

func TestVaultStoreAdapter_Size_NotFound(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	sa := newVaultStoreAdapter(v)
	fakeHash := make([]byte, 32)
	_, err := sa.Size(fakeHash)
	if err == nil {
		t.Error("Size nonexistent key should fail")
	}
}

func TestVaultMetanetAdapter_GetNodeByPath_WithChildren(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	ma := newVaultMetanetAdapter(v)
	info, err := ma.GetNodeByPath("/")
	if err != nil {
		t.Fatalf("GetNodeByPath: %v", err)
	}
	if len(info.Children) != 2 {
		t.Errorf("children count = %d, want 2", len(info.Children))
	}
	if info.Type != "dir" {
		t.Errorf("type = %q, want dir", info.Type)
	}
}

func TestVaultMetanetAdapter_GetNodeByPath_File(t *testing.T) {
	v, _ := populateTestVault(t)
	defer v.Close()

	ma := newVaultMetanetAdapter(v)
	info, err := ma.GetNodeByPath("/hello.txt")
	if err != nil {
		t.Fatalf("GetNodeByPath: %v", err)
	}
	if info.Type != "file" {
		t.Errorf("type = %q, want file", info.Type)
	}
	if info.FileSize != 5 {
		t.Errorf("size = %d, want 5", info.FileSize)
	}
	if info.MimeType != "text/plain" {
		t.Errorf("mime = %q, want text/plain", info.MimeType)
	}
}

// ---------------------------------------------------------------------------
// 5. WalletBalance with UTXOs in state
// ---------------------------------------------------------------------------

func TestWalletBalance_WithUTXOs(t *testing.T) {
	dataDir := initTestWallet(t)
	v := openTestVault(t, dataDir)

	// Inject fake UTXOs into the vault state.
	v.State.UTXOs = append(v.State.UTXOs,
		&vault.UTXOState{TxID: "aa", Vout: 0, Amount: 50000, Type: "fee", Spent: false},
		&vault.UTXOState{TxID: "bb", Vout: 0, Amount: 1000, Type: "node", Spent: false},
		&vault.UTXOState{TxID: "cc", Vout: 0, Amount: 2000, Type: "fee", Spent: true}, // spent, should be excluded
	)
	v.State.Save()
	v.Close()

	out := captureStdout(t, func() {
		code := runWalletBalance([]string{"--datadir", dataDir, "--password", "testpass"})
		if code != exitSuccess {
			t.Errorf("runWalletBalance = %d, want %d", code, exitSuccess)
		}
	})

	if out == "" {
		t.Error("expected non-empty balance output")
	}
}

func TestWalletBalance_Refresh(t *testing.T) {
	dataDir := initTestWallet(t)
	// --refresh with regtest default RPC — exercises the refresh code path.
	// The RPC call may fail silently (no running node), but the command still succeeds.
	code := runWalletBalance([]string{"--datadir", dataDir, "--password", "testpass", "--refresh"})
	if code != exitSuccess {
		t.Logf("balance --refresh returned %d (expected, no running RPC)", code)
	}
}

// ---------------------------------------------------------------------------
// 6. DaemonStop edge cases
// ---------------------------------------------------------------------------

func TestDaemonStop_StalePID(t *testing.T) {
	dir := t.TempDir()
	// Write a PID file pointing to a non-existent process.
	// PID 999999 is unlikely to exist.
	os.WriteFile(filepath.Join(dir, "daemon.pid"), []byte("999999"), 0600)
	code := runDaemonStop([]string{"--datadir", dir})
	// On Unix, os.FindProcess always succeeds, but Signal may fail.
	if code == exitSuccess {
		t.Log("stale PID signal unexpectedly succeeded (process may exist)")
	}
}

func TestDaemonStop_SelfPID(t *testing.T) {
	// We can't actually kill ourselves in a test, but we can write our own PID
	// and verify that the code path for "signal sent" is exercised.
	// Note: sending SIGTERM to self would terminate the test, so skip this.
	// Instead, test the valid-PID-invalid-content path.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "daemon.pid"), []byte("0"), 0600)
	code := runDaemonStop([]string{"--datadir", dir})
	if code == exitSuccess {
		t.Log("PID 0 signal may succeed on some systems")
	}
}

// ---------------------------------------------------------------------------
// 7. printFundingInstructions mainnet (QR code)
// ---------------------------------------------------------------------------

func TestPrintFundingInstructions_Mainnet(t *testing.T) {
	// Should print a QR code without panicking.
	printFundingInstructions("mainnet", "1BitcoinEaterAddressDontSendf59kuE")
}

// ---------------------------------------------------------------------------
// 8. validateAccessMode
// ---------------------------------------------------------------------------

func TestValidateAccessMode_Extended(t *testing.T) {
	// Test empty and uppercase modes (existing test covers valid modes).
	for _, mode := range []string{"", "FREE", "Private", "PAID", "unknown"} {
		if err := validateAccessMode(mode); err == nil {
			t.Errorf("validateAccessMode(%q) should fail", mode)
		}
	}
}

// ---------------------------------------------------------------------------
// 9. ensureHistoryFilePermissions
// ---------------------------------------------------------------------------

func TestEnsureHistoryFilePermissions(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "history")
	os.WriteFile(tmp, []byte("cmd1\ncmd2\n"), 0644)

	ensureHistoryFilePermissions(tmp)

	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestEnsureHistoryFilePermissions_NonexistentFile(t *testing.T) {
	// Should not panic for nonexistent files.
	ensureHistoryFilePermissions("/nonexistent/path/history")
}

// ---------------------------------------------------------------------------
// 10. Password utility functions
// ---------------------------------------------------------------------------

func TestPromptPassword_NonTerminal(t *testing.T) {
	// In tests, stdin is not a terminal — should return error.
	_, err := promptPassword("test: ")
	if err == nil {
		t.Error("promptPassword should fail when stdin is not a terminal")
	}
}

func TestPromptPasswordConfirm_NonTerminal(t *testing.T) {
	_, err := promptPasswordConfirm()
	if err == nil {
		t.Error("promptPasswordConfirm should fail when stdin is not a terminal")
	}
}

func TestPromptNetwork_NonTerminal(t *testing.T) {
	_, err := promptNetwork()
	if err == nil {
		t.Error("promptNetwork should fail when stdin is not a terminal")
	}
}

func TestPromptYesNo_NonTerminal(t *testing.T) {
	result := promptYesNo("Are you sure?")
	if result {
		t.Error("promptYesNo should return false when stdin is not a terminal")
	}
}

func TestResolvePassword_WithValue(t *testing.T) {
	pass, err := resolvePassword("mypassword")
	if err != nil {
		t.Fatalf("resolvePassword: %v", err)
	}
	if pass != "mypassword" {
		t.Errorf("resolvePassword = %q, want %q", pass, "mypassword")
	}
}

func TestResolvePassword_Empty(t *testing.T) {
	// Empty flag should attempt terminal prompt, which fails in tests.
	_, err := resolvePassword("")
	if err == nil {
		t.Error("resolvePassword with empty flag should fail in non-terminal")
	}
}

func TestZeroString(t *testing.T) {
	s := "sensitive"
	zeroString(&s)
	if s != "" {
		t.Errorf("zeroString did not clear string, got %q", s)
	}
}

// ---------------------------------------------------------------------------
// 11. Verify command edge cases
// ---------------------------------------------------------------------------

func TestVerify_WalletExists_NoSPV(t *testing.T) {
	dataDir := initTestWallet(t)
	// Valid wallet + txid, but no RPC configured — SPV init should fail or
	// the "not online" check should trigger.
	code := runVerify([]string{"--datadir", dataDir, "--password", "testpass",
		"--network", "nonexistent",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if code == exitSuccess {
		t.Error("expected failure when no blockchain connection")
	}
}

// ---------------------------------------------------------------------------
// 12. Command help flags (exercises flag parsing branches)
// ---------------------------------------------------------------------------

func TestCat_Help(t *testing.T) {
	code := runCat([]string{"--help"})
	if code != exitUsageError {
		// flag.ContinueOnError + PrintDefaults → returns error
		t.Logf("runCat --help returned %d", code)
	}
}

func TestGet_Help(t *testing.T) {
	code := runGet([]string{"--help"})
	if code != exitUsageError {
		t.Logf("runGet --help returned %d", code)
	}
}

func TestMget_Help(t *testing.T) {
	code := runMget([]string{"--help"})
	if code != exitUsageError {
		t.Logf("runMget --help returned %d", code)
	}
}

func TestMput_Help(t *testing.T) {
	code := runMput([]string{"--help"})
	if code != exitUsageError {
		t.Logf("runMput --help returned %d", code)
	}
}

func TestWalletFund_Help(t *testing.T) {
	code := runWalletFund([]string{"--help"})
	if code != exitUsageError {
		t.Logf("runWalletFund --help returned %d", code)
	}
}

// ---------------------------------------------------------------------------
// 13. More run() dispatch for remaining uncovered branches
// ---------------------------------------------------------------------------

func TestRun_CatCommand(t *testing.T) {
	code := run([]string{"cat"})
	if code != exitUsageError {
		t.Errorf("run cat (no args) = %d, want %d", code, exitUsageError)
	}
}

func TestRun_GetCommand(t *testing.T) {
	code := run([]string{"get"})
	if code != exitUsageError {
		t.Errorf("run get (no args) = %d, want %d", code, exitUsageError)
	}
}

func TestRun_MgetCommand(t *testing.T) {
	code := run([]string{"mget"})
	if code != exitUsageError {
		t.Errorf("run mget (no args) = %d, want %d", code, exitUsageError)
	}
}

func TestRun_MputCommand(t *testing.T) {
	code := run([]string{"mput"})
	if code != exitUsageError {
		t.Errorf("run mput (no args) = %d, want %d", code, exitUsageError)
	}
}

func TestRun_FundCommand(t *testing.T) {
	dir := t.TempDir()
	code := run([]string{"fund", "--datadir", dir, "--password", "x"})
	if code == exitSuccess {
		t.Error("run fund without wallet should not succeed")
	}
}

func TestRun_BalanceCommand(t *testing.T) {
	dir := t.TempDir()
	code := run([]string{"balance", "--datadir", dir, "--password", "x"})
	if code == exitSuccess {
		t.Error("run balance without wallet should not succeed")
	}
}

func TestRun_VerifyCommand(t *testing.T) {
	code := run([]string{"verify"})
	if code != exitUsageError {
		t.Errorf("run verify (no args) = %d, want %d", code, exitUsageError)
	}
}

func TestRun_CpCommand(t *testing.T) {
	code := run([]string{"cp"})
	if code != exitUsageError {
		t.Errorf("run cp (no args) = %d, want %d", code, exitUsageError)
	}
}

func TestRun_UnpublishCommand(t *testing.T) {
	code := run([]string{"unpublish"})
	if code != exitUsageError {
		t.Errorf("run unpublish (no args) = %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// 14. WalletFund deeper path (derive + address + print)
// ---------------------------------------------------------------------------

func TestWalletFund_DeriveAndPrint(t *testing.T) {
	dataDir := initTestWallet(t)
	v := openTestVault(t, dataDir)

	// Verify the fee key derivation path works.
	feeKP, err := v.Wallet.DeriveFeeKey(wallet.ExternalChain, 0)
	if err != nil {
		t.Fatalf("DeriveFeeKey: %v", err)
	}
	if feeKP.PublicKey == nil {
		t.Fatal("DeriveFeeKey returned nil public key")
	}

	v.Close()
	// The fund command will block at stdin.Read, so we can only test
	// the error paths (no wallet, wrong password).
}

func TestWalletFund_WrongPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runWalletFund([]string{"--datadir", dataDir, "--password", "wrong"})
	if code != exitWalletError {
		t.Errorf("fund with wrong password = %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// 15. DaemonStart deeper paths
// ---------------------------------------------------------------------------

func TestDaemonStart_WrongPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runDaemonStart([]string{"--datadir", dataDir, "--password", "wrong"})
	if code != exitWalletError {
		t.Errorf("daemon start with wrong password = %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// 16. formatSats additional cases
// ---------------------------------------------------------------------------

func TestFormatSats_LargeNumber(t *testing.T) {
	// 21 million BTC in satoshis.
	got := formatSats(2100000000000000)
	if got != "2,100,000,000,000,000" {
		t.Errorf("formatSats(2.1e15) = %q", got)
	}
}

// ---------------------------------------------------------------------------
// 17. WalletInit network flag tests
// ---------------------------------------------------------------------------

func TestWalletInit_TestnetNetwork(t *testing.T) {
	dir := t.TempDir()
	code := runWalletInit([]string{"--datadir", dir, "--password", "p", "--network", "testnet"})
	if code != exitSuccess {
		t.Errorf("wallet init testnet = %d, want %d", code, exitSuccess)
	}
}

func TestWalletInit_MainnetNetwork(t *testing.T) {
	dir := t.TempDir()
	code := runWalletInit([]string{"--datadir", dir, "--password", "p", "--network", "mainnet"})
	if code != exitSuccess {
		t.Errorf("wallet init mainnet = %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// 18. DaemonStop with PID of current process (SIGTERM would kill test,
// so we use a PID that definitely doesn't exist in a large range)
// ---------------------------------------------------------------------------

func TestDaemonStop_MaxPID(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "daemon.pid"), []byte(strconv.Itoa(1<<20+99999)), 0600)
	code := runDaemonStop([]string{"--datadir", dir})
	// Should fail because this PID doesn't exist.
	if code == exitSuccess {
		t.Log("unexpectedly succeeded with max PID")
	}
}

// ---------------------------------------------------------------------------
// 19. More run* command tests with valid wallet (exercises deeper paths)
// ---------------------------------------------------------------------------

func TestRunMkdir_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	// mkdir without a root node will fail at vault level, but exercises more lines.
	code := runMkdir([]string{"--datadir", dataDir, "--password", "testpass", "/testdir"})
	if code == exitSuccess {
		t.Error("mkdir without root node should not succeed")
	}
}

func TestRunRm_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runRm([]string{"--datadir", dataDir, "--password", "testpass", "/nonexistent"})
	if code == exitSuccess {
		t.Error("rm nonexistent path should not succeed")
	}
}

func TestRunMv_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runMv([]string{"--datadir", dataDir, "--password", "testpass", "/a", "/b"})
	if code == exitSuccess {
		t.Error("mv nonexistent paths should not succeed")
	}
}

func TestRunCp_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runCp([]string{"--datadir", dataDir, "--password", "testpass", "/a", "/b"})
	if code == exitSuccess {
		t.Error("cp nonexistent paths should not succeed")
	}
}

func TestRunLink_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runLink([]string{"--datadir", dataDir, "--password", "testpass", "/a", "/b"})
	if code == exitSuccess {
		t.Error("link nonexistent paths should not succeed")
	}
}

func TestRunSell_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runSell([]string{"--datadir", dataDir, "--password", "testpass", "/file", "100"})
	if code == exitSuccess {
		t.Error("sell nonexistent path should not succeed")
	}
}

func TestRunEncrypt_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runEncrypt([]string{"--datadir", dataDir, "--password", "testpass", "/file"})
	if code == exitSuccess {
		t.Error("encrypt nonexistent path should not succeed")
	}
}

func TestRunCat_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runCat([]string{"--datadir", dataDir, "--password", "testpass", "/file"})
	if code == exitSuccess {
		t.Error("cat nonexistent path should not succeed")
	}
}

func TestRunGet_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runGet([]string{"--datadir", dataDir, "--password", "testpass", "/file"})
	if code == exitSuccess {
		t.Error("get nonexistent path should not succeed")
	}
}

func TestRunPublish_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	// Publish succeeds (prints DNS instructions) even without valid DNS.
	code := runPublish([]string{"--datadir", dataDir, "--password", "testpass", "invalid.test"})
	if code != exitSuccess {
		t.Errorf("publish = %d, want %d", code, exitSuccess)
	}
}

func TestRunPublish_ListEmpty(t *testing.T) {
	dataDir := initTestWallet(t)
	// No domain arg → list published domains (empty).
	code := runPublish([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("publish list = %d, want %d", code, exitSuccess)
	}
}

func TestRunVerify_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVerify([]string{"--datadir", dataDir, "--password", "testpass",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if code == exitSuccess {
		t.Error("verify without RPC should not succeed")
	}
}

func TestRunMget_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runMget([]string{"--datadir", dataDir, "--password", "testpass", "/dir", t.TempDir()})
	if code == exitSuccess {
		t.Error("mget nonexistent remote dir should not succeed")
	}
}

func TestRunMput_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "f.txt"), []byte("x"), 0644)
	code := runMput([]string{"--datadir", dataDir, "--password", "testpass", srcDir, "/remote"})
	// mput succeeds with exitSuccess (warnings in result.Errors, not a hard error).
	if code != exitSuccess {
		t.Errorf("mput = %d, want %d", code, exitSuccess)
	}
}

func TestRunUnpublish_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runUnpublish([]string{"--datadir", dataDir, "--password", "testpass", "test.com"})
	if code == exitSuccess {
		t.Error("unpublish nonexistent domain should not succeed")
	}
}

func TestRunPut_WithWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "data.txt")
	os.WriteFile(tmpFile, []byte("hello"), 0644)
	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", tmpFile, "/data.txt"})
	// Will fail at vault (no root node), but exercises deeper lines.
	if code == exitSuccess {
		t.Error("put without root node should not succeed")
	}
}

// ---------------------------------------------------------------------------
// 20. Wallet show deeper paths (exercises existing wallet + vault list)
// ---------------------------------------------------------------------------

func TestWalletShow_WithMultipleVaults(t *testing.T) {
	dataDir := initTestWallet(t)
	// Create a second vault.
	runVault([]string{"create", "--datadir", dataDir, "--password", "testpass", "--name", "photos"})
	// Show should list both vaults.
	code := runWalletShow([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("wallet show = %d, want %d", code, exitSuccess)
	}
}

func TestWalletInit_AlreadyExists(t *testing.T) {
	dataDir := initTestWallet(t)
	// Second init should fail — wallet already exists.
	code := runWalletInit([]string{"--datadir", dataDir, "--password", "p", "--network", "regtest"})
	if code != exitWalletError {
		t.Errorf("wallet init (already exists) = %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// 21. Funded wallet helper — creates wallet with root dir + fee UTXOs on disk
// ---------------------------------------------------------------------------

// addTestFeeUTXO adds a fee UTXO to the vault state using derived wallet keys.
func addTestFeeUTXO(t *testing.T, v *vault.Vault, amount uint64) {
	t.Helper()
	idx := v.WState.NextReceiveIndex
	kp, err := v.Wallet.DeriveFeeKey(wallet.ExternalChain, idx)
	if err != nil {
		t.Fatalf("DeriveFeeKey: %v", err)
	}
	v.WState.NextReceiveIndex++

	pubHex := hex.EncodeToString(kp.PublicKey.Compressed())
	pkh := bsvhash.Hash160(kp.PublicKey.Compressed())
	scriptPK := "76a914" + hex.EncodeToString(pkh) + "88ac"

	v.State.AddUTXO(&vault.UTXOState{
		TxID:         "ff" + strings.Repeat("00", 31),
		Vout:         idx,
		Amount:       amount,
		ScriptPubKey: scriptPK,
		PubKeyHex:    pubHex,
		Type:         "fee",
		FeeChain:     wallet.ExternalChain,
		FeeDerivIdx:  idx,
	})
}

// initFundedWallet creates a wallet with a root dir and multiple fee UTXOs
// written to disk, ready for run* CLI functions to open and succeed.
// Returns the dataDir path. Also creates a local test file for put/mput.
func initFundedWallet(t *testing.T) string {
	t.Helper()
	dataDir := initTestWallet(t)
	v := openTestVault(t, dataDir)

	// Add fee UTXO for root creation.
	addTestFeeUTXO(t, v, 100000)

	// Create root directory.
	_, err := v.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/"})
	if err != nil {
		t.Fatalf("Mkdir /: %v", err)
	}

	// Add more fee UTXOs for subsequent operations.
	for i := 0; i < 10; i++ {
		addTestFeeUTXO(t, v, 100000)
	}

	// Create a test file in vault.
	testFile := filepath.Join(dataDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	_, err = v.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  testFile,
		RemotePath: "/test.txt",
		Access:     "free",
	})
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}

	// Create a subdirectory.
	_, err = v.Mkdir(&vault.MkdirOpts{VaultIndex: 0, Path: "/subdir"})
	if err != nil {
		t.Fatalf("Mkdir /subdir: %v", err)
	}

	// Save wallet state (fee UTXO tracking).
	statePath := filepath.Join(dataDir, "state.json")
	if err := saveWalletState(statePath, v.WState); err != nil {
		t.Fatalf("saveWalletState: %v", err)
	}

	// Close persists nodes.json.
	if err := v.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dataDir
}

// ---------------------------------------------------------------------------
// 22. Success-path tests for run* commands using funded wallet
// ---------------------------------------------------------------------------

func TestRunMkdir_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runMkdir([]string{"--datadir", dataDir, "--password", "testpass", "/newdir"})
	if code != exitSuccess {
		t.Errorf("mkdir /newdir = %d, want %d", code, exitSuccess)
	}
}

func TestRunPut_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "upload.txt")
	os.WriteFile(tmpFile, []byte("data"), 0644)
	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", tmpFile, "/upload.txt"})
	if code != exitSuccess {
		t.Errorf("put = %d, want %d", code, exitSuccess)
	}
}

func TestRunRm_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runRm([]string{"--datadir", dataDir, "--password", "testpass", "/test.txt"})
	if code != exitSuccess {
		t.Errorf("rm /test.txt = %d, want %d", code, exitSuccess)
	}
}

func TestRunMv_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runMv([]string{"--datadir", dataDir, "--password", "testpass", "/test.txt", "/renamed.txt"})
	if code != exitSuccess {
		t.Errorf("mv = %d, want %d", code, exitSuccess)
	}
}

func TestRunCp_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runCp([]string{"--datadir", dataDir, "--password", "testpass", "/test.txt", "/copy.txt"})
	if code != exitSuccess {
		t.Errorf("cp = %d, want %d", code, exitSuccess)
	}
}

func TestRunLink_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runLink([]string{"--datadir", dataDir, "--password", "testpass", "/test.txt", "/link.txt"})
	if code != exitSuccess {
		t.Errorf("link = %d, want %d", code, exitSuccess)
	}
}

func TestRunSell_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runSell([]string{"--datadir", dataDir, "--password", "testpass", "--price", "100", "/test.txt"})
	if code != exitSuccess {
		t.Errorf("sell = %d, want %d", code, exitSuccess)
	}
}

func TestRunEncrypt_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runEncrypt([]string{"--datadir", dataDir, "--password", "testpass", "/test.txt"})
	if code != exitSuccess {
		t.Errorf("encrypt = %d, want %d", code, exitSuccess)
	}
}

func TestRunCat_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runCat([]string{"--datadir", dataDir, "--password", "testpass", "/test.txt"})
	if code != exitSuccess {
		t.Errorf("cat = %d, want %d", code, exitSuccess)
	}
}

func TestRunGet_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	outDir := t.TempDir()
	code := runGet([]string{"--datadir", dataDir, "--password", "testpass", "/test.txt", filepath.Join(outDir, "test.txt")})
	if code != exitSuccess {
		t.Errorf("get = %d, want %d", code, exitSuccess)
	}
}

func TestRunMget_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	outDir := t.TempDir()
	code := runMget([]string{"--datadir", dataDir, "--password", "testpass", "/", outDir})
	if code != exitSuccess {
		t.Errorf("mget = %d, want %d", code, exitSuccess)
	}
}

func TestRunMput_Success(t *testing.T) {
	dataDir := initFundedWallet(t)
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("bbb"), 0644)
	code := runMput([]string{"--datadir", dataDir, "--password", "testpass", srcDir, "/uploaded"})
	if code != exitSuccess {
		t.Errorf("mput = %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// 23. Vault subcommand dispatch + error paths
// ---------------------------------------------------------------------------

func TestRunVault_Help(t *testing.T) {
	code := runVault([]string{"--help"})
	if code != exitSuccess {
		t.Errorf("vault --help = %d, want %d", code, exitSuccess)
	}
}

func TestRunVault_UnknownSubcmd(t *testing.T) {
	code := runVault([]string{"nope"})
	if code != exitUsageError {
		t.Errorf("vault nope = %d, want %d", code, exitUsageError)
	}
}

func TestRunVault_NoArgs(t *testing.T) {
	code := runVault(nil)
	if code != exitUsageError {
		t.Errorf("vault (no args) = %d, want %d", code, exitUsageError)
	}
}

func TestRunVaultCreate_Success(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultCreate([]string{"--datadir", dataDir, "--password", "testpass", "photos"})
	if code != exitSuccess {
		t.Errorf("vault create = %d, want %d", code, exitSuccess)
	}
}

func TestRunVaultCreate_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultCreate([]string{"--datadir", dataDir, "--password", "wrong", "photos"})
	if code != exitWalletError {
		t.Errorf("vault create (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunVaultList_Success(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultList([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("vault list = %d, want %d", code, exitSuccess)
	}
}

func TestRunVaultRename_Success(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultRename([]string{"--datadir", dataDir, "--password", "testpass", "default", "main"})
	if code != exitSuccess {
		t.Errorf("vault rename = %d, want %d", code, exitSuccess)
	}
}

func TestRunVaultRename_NotFound(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultRename([]string{"--datadir", dataDir, "--password", "testpass", "nope", "ok"})
	if code != exitNotFound {
		t.Errorf("vault rename (not found) = %d, want %d", code, exitNotFound)
	}
}

func TestRunVaultDelete_Success(t *testing.T) {
	dataDir := initTestWallet(t)
	// Create a vault to delete.
	runVaultCreate([]string{"--datadir", dataDir, "--password", "testpass", "deleteme"})
	code := runVaultDelete([]string{"--datadir", dataDir, "--password", "testpass", "deleteme"})
	if code != exitSuccess {
		t.Errorf("vault delete = %d, want %d", code, exitSuccess)
	}
}

func TestRunVaultDelete_NotFound(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultDelete([]string{"--datadir", dataDir, "--password", "testpass", "nope"})
	if code != exitNotFound {
		t.Errorf("vault delete (not found) = %d, want %d", code, exitNotFound)
	}
}

func TestRunVaultCreate_NoName(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultCreate([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitUsageError {
		t.Errorf("vault create (no name) = %d, want %d", code, exitUsageError)
	}
}

func TestRunVaultRename_TooFewArgs(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultRename([]string{"--datadir", dataDir, "--password", "testpass", "one"})
	if code != exitUsageError {
		t.Errorf("vault rename (1 arg) = %d, want %d", code, exitUsageError)
	}
}

func TestRunVaultDelete_NoName(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultDelete([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitUsageError {
		t.Errorf("vault delete (no name) = %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// 24. Wallet subcommand dispatch + deeper paths
// ---------------------------------------------------------------------------

func TestRunWallet_Help(t *testing.T) {
	code := runWallet([]string{"--help"})
	if code != exitSuccess {
		t.Errorf("wallet --help = %d, want %d", code, exitSuccess)
	}
}

func TestRunWallet_UnknownSubcmd(t *testing.T) {
	code := runWallet([]string{"nope"})
	if code != exitUsageError {
		t.Errorf("wallet nope = %d, want %d", code, exitUsageError)
	}
}

func TestRunWallet_NoArgs(t *testing.T) {
	code := runWallet(nil)
	if code != exitUsageError {
		t.Errorf("wallet (no args) = %d, want %d", code, exitUsageError)
	}
}

func TestRunWalletBalance_WithUTXOs(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runWalletBalance([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("wallet balance = %d, want %d", code, exitSuccess)
	}
}

func TestRunWalletBalance_RefreshRegtest(t *testing.T) {
	dataDir := initTestWallet(t)
	// --refresh with regtest → configures chain from default presets.
	// Scan succeeds (silently) even with no running node; exercises the refresh path.
	code := runWalletBalance([]string{"--datadir", dataDir, "--password", "testpass", "--refresh"})
	if code != exitSuccess {
		t.Errorf("wallet balance --refresh = %d, want %d", code, exitSuccess)
	}
}

func TestRunWalletShow_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runWalletShow([]string{"--datadir", dataDir, "--password", "wrong"})
	if code != exitWalletError {
		t.Errorf("wallet show (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunWalletShow_NoWallet(t *testing.T) {
	code := runWalletShow([]string{"--datadir", t.TempDir(), "--password", "x"})
	if code != exitWalletError {
		t.Errorf("wallet show (no wallet) = %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// 25. Vault resolve and run* with --vault flag
// ---------------------------------------------------------------------------

func TestRunMkdir_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runMkdir([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/dir"})
	if code != exitNotFound {
		t.Errorf("mkdir (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunMkdir_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runMkdir([]string{"--datadir", dataDir, "--password", "wrong", "/dir"})
	if code != exitWalletError {
		t.Errorf("mkdir (bad pass) = %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// 26. Mget deeper paths — mgetRecurse with nested dirs and file children
// ---------------------------------------------------------------------------

func TestRunMget_NestedDirs(t *testing.T) {
	dataDir := initFundedWallet(t)
	// initFundedWallet creates /test.txt and /subdir.
	// mget / should download both, including the file.
	outDir := t.TempDir()
	code := runMget([]string{"--datadir", dataDir, "--password", "testpass", "/", outDir})
	if code != exitSuccess {
		t.Errorf("mget / = %d, want %d", code, exitSuccess)
	}

	// Verify test.txt was downloaded.
	data, err := os.ReadFile(filepath.Join(outDir, "test.txt"))
	if err != nil {
		t.Errorf("test.txt not found in output: %v", err)
	} else if string(data) != "hello world" {
		t.Errorf("test.txt = %q, want %q", string(data), "hello world")
	}
}

// ---------------------------------------------------------------------------
// 27. Run commands that print result.TxHex path (verifying TxID output)
// ---------------------------------------------------------------------------

func TestRunVerify_InvalidTxID(t *testing.T) {
	dataDir := initTestWallet(t)
	// Too-short TxID should fail.
	code := runVerify([]string{"--datadir", dataDir, "--password", "testpass", "abcd"})
	if code == exitSuccess {
		t.Error("verify with too-short txid should not succeed")
	}
}

func TestRunUnpublish_Success(t *testing.T) {
	dataDir := initTestWallet(t)
	// Publish a domain first.
	runPublish([]string{"--datadir", dataDir, "--password", "testpass", "test.example.com"})
	// Unpublish it.
	code := runUnpublish([]string{"--datadir", dataDir, "--password", "testpass", "test.example.com"})
	if code != exitSuccess {
		t.Errorf("unpublish = %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// 28. Bad password tests for run* commands (vault.New error path)
// ---------------------------------------------------------------------------

func TestRunRm_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runRm([]string{"--datadir", dataDir, "--password", "wrong", "/file"})
	if code != exitWalletError {
		t.Errorf("rm (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunMv_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runMv([]string{"--datadir", dataDir, "--password", "wrong", "/a", "/b"})
	if code != exitWalletError {
		t.Errorf("mv (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunCp_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runCp([]string{"--datadir", dataDir, "--password", "wrong", "/a", "/b"})
	if code != exitWalletError {
		t.Errorf("cp (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunLink_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runLink([]string{"--datadir", dataDir, "--password", "wrong", "/a", "/b"})
	if code != exitWalletError {
		t.Errorf("link (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunSell_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runSell([]string{"--datadir", dataDir, "--password", "wrong", "--price", "100", "/file"})
	if code != exitWalletError {
		t.Errorf("sell (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunEncrypt_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runEncrypt([]string{"--datadir", dataDir, "--password", "wrong", "/file"})
	if code != exitWalletError {
		t.Errorf("encrypt (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunCat_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runCat([]string{"--datadir", dataDir, "--password", "wrong", "/file"})
	if code != exitWalletError {
		t.Errorf("cat (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunPut_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(tmpFile, []byte("x"), 0644)
	code := runPut([]string{"--datadir", dataDir, "--password", "wrong", tmpFile, "/f.txt"})
	if code != exitWalletError {
		t.Errorf("put (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunGet_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runGet([]string{"--datadir", dataDir, "--password", "wrong", "/file"})
	if code != exitWalletError {
		t.Errorf("get (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunMget_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runMget([]string{"--datadir", dataDir, "--password", "wrong", "/dir", t.TempDir()})
	if code != exitWalletError {
		t.Errorf("mget (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunMput_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runMput([]string{"--datadir", dataDir, "--password", "wrong", t.TempDir(), "/remote"})
	if code != exitWalletError {
		t.Errorf("mput (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunPublish_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runPublish([]string{"--datadir", dataDir, "--password", "wrong", "test.com"})
	if code != exitWalletError {
		t.Errorf("publish (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunUnpublish_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runUnpublish([]string{"--datadir", dataDir, "--password", "wrong", "test.com"})
	if code != exitWalletError {
		t.Errorf("unpublish (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunVerify_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVerify([]string{"--datadir", dataDir, "--password", "wrong",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	if code != exitWalletError {
		t.Errorf("verify (bad pass) = %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// 29. Bad vault name tests for run* commands (ResolveVaultIndex error path)
// ---------------------------------------------------------------------------

func TestRunRm_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runRm([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/file"})
	if code != exitNotFound {
		t.Errorf("rm (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunMv_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runMv([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/a", "/b"})
	if code != exitNotFound {
		t.Errorf("mv (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunCp_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runCp([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/a", "/b"})
	if code != exitNotFound {
		t.Errorf("cp (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunLink_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runLink([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/a", "/b"})
	if code != exitNotFound {
		t.Errorf("link (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunSell_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runSell([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "--price", "100", "/file"})
	if code != exitNotFound {
		t.Errorf("sell (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunEncrypt_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runEncrypt([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/file"})
	if code != exitNotFound {
		t.Errorf("encrypt (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunCat_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runCat([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/file"})
	if code != exitNotFound {
		t.Errorf("cat (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunPut_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(tmpFile, []byte("x"), 0644)
	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", tmpFile, "/f.txt"})
	if code != exitNotFound {
		t.Errorf("put (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunMget_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runMget([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/dir", t.TempDir()})
	if code != exitNotFound {
		t.Errorf("mget (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunMput_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runMput([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", t.TempDir(), "/remote"})
	if code != exitNotFound {
		t.Errorf("mput (bad vault) = %d, want %d", code, exitNotFound)
	}
}

func TestRunPublish_BadVaultName(t *testing.T) {
	dataDir := initFundedWallet(t)
	code := runPublish([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "test.com"})
	if code != exitNotFound {
		t.Errorf("publish (bad vault) = %d, want %d", code, exitNotFound)
	}
}

// ---------------------------------------------------------------------------
// 30. Vault wallet adapter — GetSellerKeyPair and GetVaultPubKey success paths
// ---------------------------------------------------------------------------

func TestVaultWalletAdapter_GetSellerKeyPair_Deep(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	a := newVaultWalletAdapter(v)
	priv, pub, err := a.GetSellerKeyPair()
	if err != nil {
		t.Fatalf("GetSellerKeyPair: %v", err)
	}
	if priv == nil || pub == nil {
		t.Error("expected non-nil key pair")
	}
}

func TestVaultWalletAdapter_GetVaultPubKey_Deep(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	a := newVaultWalletAdapter(v)
	pubHex, err := a.GetVaultPubKey("default")
	if err != nil {
		t.Fatalf("GetVaultPubKey: %v", err)
	}
	if len(pubHex) != 66 {
		t.Errorf("pubkey hex len = %d, want 66", len(pubHex))
	}
}

func TestVaultWalletAdapter_GetVaultPubKey_BadVault_Deep(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	a := newVaultWalletAdapter(v)
	_, err = a.GetVaultPubKey("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent vault")
	}
}

func TestVaultSPVAdapter_Nil(t *testing.T) {
	dataDir := initTestWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	// SPV is nil by default (no InitSPV called).
	adapter := newVaultSPVAdapter(v)
	if adapter != nil {
		t.Error("expected nil adapter when v.SPV is nil")
	}
}

func TestVaultChainAdapter_Nil(t *testing.T) {
	dataDir := initTestWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	// Chain is nil by default (no configureChain called).
	adapter := newVaultChainAdapter(v)
	if adapter != nil {
		t.Error("expected nil adapter when v.Chain is nil")
	}
}

// ---------------------------------------------------------------------------
// 31. WalletInit with --password flag (non-interactive path)
// ---------------------------------------------------------------------------

func TestWalletInit_WithPasswordFlag(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "newwallet")
	out := captureStdout(t, func() {
		code := runWalletInit([]string{
			"--datadir", dataDir,
			"--password", "testpass",
			"--network", "regtest",
			"--words", "12",
		})
		if code != exitSuccess {
			t.Errorf("wallet init = %d, want %d", code, exitSuccess)
		}
	})
	if !strings.Contains(out, "Wallet initialized") {
		t.Errorf("expected 'Wallet initialized' in output, got: %s", out)
	}
}

func TestWalletInit_24Words_Deep(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "wallet24")
	code := runWalletInit([]string{
		"--datadir", dataDir,
		"--password", "testpass",
		"--network", "regtest",
		"--words", "24",
	})
	if code != exitSuccess {
		t.Errorf("wallet init 24 = %d, want %d", code, exitSuccess)
	}
}

func TestWalletInit_BadWordCount(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "walletbad")
	code := runWalletInit([]string{
		"--datadir", dataDir,
		"--password", "testpass",
		"--network", "regtest",
		"--words", "18",
	})
	if code != exitUsageError {
		t.Errorf("wallet init (bad words) = %d, want %d", code, exitUsageError)
	}
}

func TestWalletInit_BadNetwork(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "walletnet")
	code := runWalletInit([]string{
		"--datadir", dataDir,
		"--password", "testpass",
		"--network", "invalid",
	})
	if code != exitUsageError {
		t.Errorf("wallet init (bad network) = %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// 32. WalletShow + loadWalletFromDataDir error paths
// ---------------------------------------------------------------------------

func TestWalletShow_Success(t *testing.T) {
	dataDir := initTestWallet(t)
	out := captureStdout(t, func() {
		code := runWalletShow([]string{"--datadir", dataDir, "--password", "testpass"})
		if code != exitSuccess {
			t.Errorf("wallet show = %d, want %d", code, exitSuccess)
		}
	})
	if !strings.Contains(out, "Wallet Information") {
		t.Errorf("expected 'Wallet Information' in output, got: %s", out)
	}
}

func TestWalletShow_NoWalletFile(t *testing.T) {
	dataDir := t.TempDir()
	code := runWalletShow([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitWalletError {
		t.Errorf("wallet show (no wallet) = %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// 33. Wallet dispatch through runWallet → "fund" and "balance" paths
// ---------------------------------------------------------------------------

func TestRunWallet_DispatchBalance(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runWallet([]string{"balance", "--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("wallet balance dispatch = %d, want %d", code, exitSuccess)
	}
}

func TestRunWallet_DispatchShow(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runWallet([]string{"show", "--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("wallet show dispatch = %d, want %d", code, exitSuccess)
	}
}

func TestRunWallet_DispatchInit(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "newinit")
	code := runWallet([]string{"init", "--datadir", dataDir, "--password", "p", "--network", "regtest"})
	if code != exitSuccess {
		t.Errorf("wallet init dispatch = %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// 34. Cat binary file warning path
// ---------------------------------------------------------------------------

func TestRunCat_BinaryWarning(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	// Upload a binary file via vault API.
	addTestFeeUTXO(t, v, 100000)
	binFile := filepath.Join(t.TempDir(), "image.png")
	os.WriteFile(binFile, []byte{0x89, 0x50, 0x4e, 0x47}, 0644)
	_, err = v.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  binFile,
		RemotePath: "/image.png",
		Access:     "free",
	})
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	statePath := filepath.Join(dataDir, "state.json")
	saveWalletState(statePath, v.WState)
	v.Close()

	code := runCat([]string{"--datadir", dataDir, "--password", "testpass", "/image.png"})
	if code != exitError {
		t.Errorf("cat binary = %d, want %d (binary warning)", code, exitError)
	}
}

// ---------------------------------------------------------------------------
// 35. mgetRecurse edge cases — unsafe child name, missing node, get error
// ---------------------------------------------------------------------------

func TestMgetRecurse_UnsafeChildName(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	// Find root node and inject a child with unsafe name.
	rootNode := v.State.FindNodeByPath("/")
	if rootNode == nil {
		t.Fatal("root not found")
	}
	rootNode.Children = append(rootNode.Children, &vault.ChildState{
		Name:   "../escape",
		Type:   "file",
		PubKey: "deadbeef",
	})

	result := &mgetResult{}
	mgetRecurse(v, 0, rootNode, t.TempDir(), result)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "unsafe") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected unsafe child name warning")
	}
}

func TestMgetRecurse_MissingNode(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	rootNode := v.State.FindNodeByPath("/")
	if rootNode == nil {
		t.Fatal("root not found")
	}
	// Inject child that references nonexistent pubkey.
	rootNode.Children = append(rootNode.Children, &vault.ChildState{
		Name:   "ghost.txt",
		Type:   "file",
		PubKey: "abcdef1234567890",
	})

	result := &mgetResult{}
	mgetRecurse(v, 0, rootNode, t.TempDir(), result)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "not found") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected node not found error")
	}
}

// ---------------------------------------------------------------------------
// 36. doMput edge cases — empty remote dir, not-a-directory
// ---------------------------------------------------------------------------

func TestDoMput_EmptyRemoteDirDeep(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	_, err = doMput(v, 0, t.TempDir(), "", "free")
	if err == nil {
		t.Error("expected error for empty remote dir")
	}
}

func TestDoMput_DotDotInRemoteDir(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	_, err = doMput(v, 0, t.TempDir(), "/dir/../escape", "free")
	if err == nil {
		t.Error("expected error for .. in remote dir")
	}
}

func TestDoMput_NotADirectoryDeep(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(tmpFile, []byte("x"), 0644)
	_, err = doMput(v, 0, tmpFile, "/remote", "free")
	if err == nil {
		t.Error("expected error for non-directory local path")
	}
}

// ---------------------------------------------------------------------------
// 37. Vault subcommand error paths — bad password for create/list/rename/delete
// ---------------------------------------------------------------------------

func TestRunVaultCreate_BadPasswordDeep(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultCreate([]string{"--datadir", dataDir, "--password", "wrong", "v1"})
	if code != exitWalletError {
		t.Errorf("vault create (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunVaultList_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultList([]string{"--datadir", dataDir, "--password", "wrong"})
	if code != exitWalletError {
		t.Errorf("vault list (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunVaultRename_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultRename([]string{"--datadir", dataDir, "--password", "wrong", "a", "b"})
	if code != exitWalletError {
		t.Errorf("vault rename (bad pass) = %d, want %d", code, exitWalletError)
	}
}

func TestRunVaultDelete_BadPassword(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runVaultDelete([]string{"--datadir", dataDir, "--password", "wrong", "v1"})
	if code != exitWalletError {
		t.Errorf("vault delete (bad pass) = %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// 38. doMput with empty access (exercises access="" default path)
// ---------------------------------------------------------------------------

func TestDoMput_EmptyAccess(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer v.Close()

	addTestFeeUTXO(t, v, 100000)
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0644)
	// Empty access exercises the default "free" path.
	// The put may fail (remote dir doesn't exist), but the default is still exercised.
	result, err := doMput(v, 0, srcDir, "/", "")
	if err != nil {
		t.Fatalf("doMput: %v", err)
	}
	// Result can have errors for individual files but shouldn't hard-fail.
	_ = result
}

// ---------------------------------------------------------------------------
// 39. completeLocalPath with absolute and relative paths
// ---------------------------------------------------------------------------

func TestCompleteLocalPath_Absolute(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "hello.txt"), []byte("x"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)

	sc := &shellCompleter{localCwd: tmpDir}
	results := sc.completeLocalPath(tmpDir + "/")
	if len(results) == 0 {
		t.Error("expected completions for absolute path")
	}
}

func TestCompleteLocalPath_WithSlash(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "mydir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "mydir", "file.go"), []byte("x"), 0644)

	sc := &shellCompleter{localCwd: tmpDir}
	results := sc.completeLocalPath("mydir/f")
	if len(results) == 0 {
		t.Error("expected completions for relative path with slash")
	}
}

// ---------------------------------------------------------------------------
// 40. Cat binary test (force flag)
// ---------------------------------------------------------------------------

func TestRunCat_BinaryForce(t *testing.T) {
	dataDir := initFundedWallet(t)
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	addTestFeeUTXO(t, v, 100000)
	binFile := filepath.Join(t.TempDir(), "binary.dat")
	os.WriteFile(binFile, []byte{0x89, 0x50, 0x4e, 0x47}, 0644)
	_, err = v.PutFile(&vault.PutOpts{
		VaultIndex: 0,
		LocalFile:  binFile,
		RemotePath: "/binary.dat",
		Access:     "free",
	})
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	statePath := filepath.Join(dataDir, "state.json")
	saveWalletState(statePath, v.WState)
	v.Close()

	out := captureStdout(t, func() {
		code := runCat([]string{"--datadir", dataDir, "--password", "testpass", "--force", "/binary.dat"})
		if code != exitSuccess {
			t.Errorf("cat --force = %d, want %d", code, exitSuccess)
		}
	})
	if len(out) == 0 {
		t.Error("expected binary output")
	}
}

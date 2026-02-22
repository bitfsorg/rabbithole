// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout runs fn and returns everything it wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// ---------------------------------------------------------------------------
// 1. Wallet error paths
// ---------------------------------------------------------------------------

func TestWalletShowWrongPassword(t *testing.T) {
	dataDir := initTestWallet(t) // created with password "testpass"

	code := runWalletShow([]string{"--datadir", dataDir, "--password", "wrongpass"})
	if code != exitWalletError {
		t.Errorf("runWalletShow (wrong password) returned %d, want %d", code, exitWalletError)
	}
}

func TestWalletInitCustomDataDir(t *testing.T) {
	dir := t.TempDir()
	customDir := filepath.Join(dir, "custom", "nested", "bitfs-data")

	code := runWalletInit([]string{"--datadir", customDir, "--password", "mypass", "--network", "regtest"})
	if code != exitSuccess {
		t.Fatalf("runWalletInit custom datadir returned %d, want %d", code, exitSuccess)
	}

	// Verify wallet file exists in the custom location.
	walletPath := filepath.Join(customDir, "wallet.enc")
	if _, err := os.Stat(walletPath); err != nil {
		t.Errorf("wallet.enc not created in custom dir: %v", err)
	}

	// Verify we can read it back with the same password.
	code = runWalletShow([]string{"--datadir", customDir, "--password", "mypass"})
	if code != exitSuccess {
		t.Errorf("runWalletShow on custom datadir returned %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// 2. Command dispatch
// ---------------------------------------------------------------------------

func TestDaemonUnknownSubcommand(t *testing.T) {
	code := runDaemon([]string{"foobar"})
	if code != exitUsageError {
		t.Errorf("runDaemon unknown subcommand returned %d, want %d", code, exitUsageError)
	}
}

func TestVersionOutput(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--version"})
		if code != exitSuccess {
			t.Errorf("run --version returned %d, want %d", code, exitSuccess)
		}
	})

	if !strings.Contains(out, "bitfs") {
		t.Errorf("version output %q does not contain 'bitfs'", out)
	}
	if !strings.Contains(out, Version) {
		t.Errorf("version output %q does not contain version %q", out, Version)
	}
}

// ---------------------------------------------------------------------------
// 3. Put command -- access modes
// ---------------------------------------------------------------------------

func TestPutPrivateAccess_NoUTXO(t *testing.T) {
	dataDir := initTestWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(tmpFile, []byte("classified"), 0600)

	// Valid access mode but no UTXOs funded — should fail.
	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", "--access", "private", tmpFile, "/vault/secret.txt"})
	if code == exitSuccess {
		t.Error("runPut --access private without UTXOs should not succeed")
	}
}

func TestPutFreeAccess_NoUTXO(t *testing.T) {
	dataDir := initTestWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "public.txt")
	os.WriteFile(tmpFile, []byte("public data"), 0600)

	// Valid access mode but no UTXOs funded — should fail.
	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", "--access", "free", tmpFile, "/public/readme.txt"})
	if code == exitSuccess {
		t.Error("runPut --access free without UTXOs should not succeed")
	}
}

func TestPutPaidAccess(t *testing.T) {
	dataDir := initTestWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "premium.txt")
	os.WriteFile(tmpFile, []byte("premium data"), 0600)

	// "paid" is not a valid access mode (only "free" and "private").
	code := runPut([]string{"--datadir", dataDir, "--password", "testpass", "--access", "paid", tmpFile, "/premium/data.txt"})
	if code != exitUsageError {
		t.Errorf("runPut --access paid returned %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// 4. Sell command
// ---------------------------------------------------------------------------

func TestSellZeroPrice(t *testing.T) {
	dataDir := initTestWallet(t)

	code := runSell([]string{"--datadir", dataDir, "--password", "testpass", "--price", "0", "/data.csv"})
	if code != exitUsageError {
		t.Errorf("runSell --price 0 returned %d, want %d", code, exitUsageError)
	}
}

// ---------------------------------------------------------------------------
// 5. Vault operations
// ---------------------------------------------------------------------------

func TestVaultCreateMultiple(t *testing.T) {
	dataDir := initTestWallet(t) // creates "default" vault

	// Create 3 additional vaults.
	names := []string{"photos", "documents", "music"}
	for _, name := range names {
		code := runVaultCreate([]string{"--datadir", dataDir, "--password", "testpass", name})
		if code != exitSuccess {
			t.Fatalf("runVaultCreate %q returned %d, want %d", name, code, exitSuccess)
		}
	}

	// Verify vault list shows all 4 (1 default + 3 new).
	out := captureStdout(t, func() {
		code := runVaultList([]string{"--datadir", dataDir, "--password", "testpass"})
		if code != exitSuccess {
			t.Fatalf("runVaultList returned %d, want %d", code, exitSuccess)
		}
	})

	// The output should mention all vault names.
	for _, name := range append([]string{"default"}, names...) {
		if !strings.Contains(out, name) {
			t.Errorf("vault list output missing vault %q", name)
		}
	}
}

func TestVaultRenameToExisting(t *testing.T) {
	dataDir := initTestWallet(t) // creates "default" vault

	// Create a second vault.
	code := runVaultCreate([]string{"--datadir", dataDir, "--password", "testpass", "secondary"})
	if code != exitSuccess {
		t.Fatalf("runVaultCreate returned %d, want %d", code, exitSuccess)
	}

	// Try to rename "secondary" to "default" -- should fail because "default" exists.
	code = runVaultRename([]string{"--datadir", dataDir, "--password", "testpass", "secondary", "default"})
	if code != exitNotFound {
		t.Errorf("vault rename to existing returned %d, want %d", code, exitNotFound)
	}
}

// ---------------------------------------------------------------------------
// 7. Global flags
// ---------------------------------------------------------------------------

func TestGlobalVersionFlag(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"version"})
		if code != exitSuccess {
			t.Errorf("run version returned %d, want %d", code, exitSuccess)
		}
	})

	if !strings.Contains(out, "bitfs") {
		t.Errorf("version output %q does not contain 'bitfs'", out)
	}
}

func TestRunWithInvalidFlag(t *testing.T) {
	// An unknown flag that does not match any command should be treated
	// as an unknown command and return exitUsageError.
	code := run([]string{"--invalid-flag"})
	if code != exitUsageError {
		t.Errorf("run --invalid-flag returned %d, want %d", code, exitUsageError)
	}
}

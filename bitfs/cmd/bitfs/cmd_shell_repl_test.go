// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitfsorg/libbitfs-go/vault"
)

// ---------------------------------------------------------------------------
// Shell entry-point tests
// ---------------------------------------------------------------------------

func TestShell_BadVault(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runShell([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent"})
	if code != exitNotFound {
		t.Errorf("got %d, want %d", code, exitNotFound)
	}
}

// ---------------------------------------------------------------------------
// Wallet balance tests
// ---------------------------------------------------------------------------

func TestWalletBalance_NoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runWalletBalance([]string{"--datadir", dir, "--password", "x"})
	if code != exitWalletError {
		t.Errorf("got %d, want %d", code, exitWalletError)
	}
}

func TestWalletBalance_EmptyWallet(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runWalletBalance([]string{"--datadir", dataDir, "--password", "testpass"})
	if code != exitSuccess {
		t.Errorf("got %d, want %d", code, exitSuccess)
	}
}

// ---------------------------------------------------------------------------
// Daemon stop edge case
// ---------------------------------------------------------------------------

func TestDaemonStop_InvalidPidFile(t *testing.T) {
	dataDir := initTestWallet(t)
	os.WriteFile(filepath.Join(dataDir, "daemon.pid"), []byte("notanumber"), 0600)
	code := runDaemonStop([]string{"--datadir", dataDir})
	if code == exitSuccess {
		t.Error("expected failure for invalid PID file")
	}
}

// ---------------------------------------------------------------------------
// configureChain / envToMap tests (rpc.go)
// ---------------------------------------------------------------------------

func TestConfigureChain_NoRPC(t *testing.T) {
	dataDir := initTestWallet(t)
	eng := openTestVault(t, dataDir)
	defer eng.Close()

	// No RPC configured — configureChain should not panic.
	// With regtest, default RPC may resolve, so just verify no crash.
	configureChain(eng, "", "", "", "regtest")
}

func TestConfigureChain_InvalidNetwork(t *testing.T) {
	dataDir := initTestWallet(t)
	eng := openTestVault(t, dataDir)
	defer eng.Close()

	// Invalid network name — should stay offline.
	configureChain(eng, "", "", "", "nonexistent-network")
	if eng.IsOnline() {
		t.Error("expected offline mode with invalid network")
	}
}

func TestEnvToMap_NoVars(t *testing.T) {
	// Clear any env vars that might interfere.
	os.Unsetenv("BITFS_RPC_URL")
	os.Unsetenv("BITFS_RPC_USER")
	os.Unsetenv("BITFS_RPC_PASS")

	m := envToMap()
	if len(m) != 0 {
		t.Errorf("envToMap with no vars: got %d entries, want 0", len(m))
	}
}

func TestEnvToMap_WithVars(t *testing.T) {
	t.Setenv("BITFS_RPC_URL", "http://localhost:18332")
	t.Setenv("BITFS_RPC_USER", "bitfs")
	t.Setenv("BITFS_RPC_PASS", "bitfs")

	m := envToMap()
	if m["BITFS_RPC_URL"] != "http://localhost:18332" {
		t.Errorf("BITFS_RPC_URL = %q", m["BITFS_RPC_URL"])
	}
	if len(m) != 3 {
		t.Errorf("envToMap: got %d entries, want 3", len(m))
	}
}

// ---------------------------------------------------------------------------
// vault adapter tests (vault_adapters.go)
// ---------------------------------------------------------------------------

func TestHexToBytes(t *testing.T) {
	tests := []struct {
		input string
		len   int
	}{
		{"deadbeef", 4},
		{"", 0},
		{"invalid-hex", 0},
		{"0102030405", 5},
	}
	for _, tt := range tests {
		got := hexToBytes(tt.input)
		if len(got) != tt.len {
			t.Errorf("hexToBytes(%q) len = %d, want %d", tt.input, len(got), tt.len)
		}
	}
}

func TestNewVaultAdapters(t *testing.T) {
	dataDir := initTestWallet(t)
	v := openTestVault(t, dataDir)
	defer v.Close()

	// Constructors should not panic.
	wa := newVaultWalletAdapter(v)
	if wa == nil {
		t.Error("newVaultWalletAdapter returned nil")
	}

	sa := newVaultStoreAdapter(v)
	if sa == nil {
		t.Error("newVaultStoreAdapter returned nil")
	}

	ma := newVaultMetanetAdapter(v)
	if ma == nil {
		t.Error("newVaultMetanetAdapter returned nil")
	}

	// SPV not initialized — should return nil.
	spvSvc := newVaultSPVAdapter(v)
	if spvSvc != nil {
		t.Error("newVaultSPVAdapter should return nil when SPV not initialized")
	}

	// Chain not configured — should return nil.
	chainSvc := newVaultChainAdapter(v)
	if chainSvc != nil {
		t.Error("newVaultChainAdapter should return nil when chain not configured")
	}
}

func TestVaultWalletAdapter_GetVaultPubKey(t *testing.T) {
	dataDir := initTestWallet(t)
	v := openTestVault(t, dataDir)
	defer v.Close()

	wa := newVaultWalletAdapter(v)

	// "default" vault should return a public key.
	pubKey, err := wa.GetVaultPubKey("default")
	if err != nil {
		t.Fatalf("GetVaultPubKey: %v", err)
	}
	if len(pubKey) != 66 { // 33 bytes compressed = 66 hex chars
		t.Errorf("GetVaultPubKey length = %d, want 66", len(pubKey))
	}

	// Nonexistent vault should fail.
	_, err = wa.GetVaultPubKey("nonexistent")
	if err == nil {
		t.Error("GetVaultPubKey nonexistent should fail")
	}
}

func TestVaultWalletAdapter_GetSellerKeyPair(t *testing.T) {
	dataDir := initTestWallet(t)
	v := openTestVault(t, dataDir)
	defer v.Close()

	wa := newVaultWalletAdapter(v)
	priv, pub, err := wa.GetSellerKeyPair()
	if err != nil {
		t.Fatalf("GetSellerKeyPair: %v", err)
	}
	if priv == nil || pub == nil {
		t.Error("GetSellerKeyPair returned nil key")
	}
}

func TestVaultMetanetAdapter_GetNodeByPath(t *testing.T) {
	dataDir := initTestWallet(t)
	v := openTestVault(t, dataDir)
	defer v.Close()

	ma := newVaultMetanetAdapter(v)

	// Empty vault: all paths should return not found.
	_, err := ma.GetNodeByPath("/nonexistent")
	if err == nil {
		t.Error("GetNodeByPath nonexistent should fail")
	}
}

func TestVaultStoreAdapter_HasNotFound(t *testing.T) {
	dataDir := initTestWallet(t)
	v := openTestVault(t, dataDir)
	defer v.Close()

	sa := newVaultStoreAdapter(v)
	// Key hash must be exactly 32 bytes (SHA-256).
	fakeHash := make([]byte, 32)
	has, err := sa.Has(fakeHash)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if has {
		t.Error("Has should return false for nonexistent key")
	}
}

// openTestVault is a helper that opens a vault from a test data directory.
func openTestVault(t *testing.T, dataDir string) *vault.Vault {
	t.Helper()
	v, err := vault.New(dataDir, "testpass")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	return v
}

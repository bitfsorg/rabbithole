// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitfsorg/metanet/internal/config"
)

// ---------------------------------------------------------------------------
// Init workflow tests
// ---------------------------------------------------------------------------

func TestInitCreatesDataDir(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testnode")

	code := cmdInit([]string{"--datadir", dataDir})
	if code != exitSuccess {
		t.Fatalf("cmdInit returned %d, want %d", code, exitSuccess)
	}

	// Verify data directory structure.
	expectedDirs := []string{
		"",
		"chain/blocks",
		"chain/state",
		"cache",
		"channels/bsv",
		"channels/mnt",
		"peers",
		"logs",
	}
	for _, sub := range expectedDirs {
		path := filepath.Join(dataDir, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %s: %v", path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", path)
		}
	}
}

func TestInitCreatesNodeKey(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testnode")

	code := cmdInit([]string{"--datadir", dataDir})
	if code != exitSuccess {
		t.Fatalf("cmdInit returned %d, want %d", code, exitSuccess)
	}

	// Read and validate the node key file.
	keyPath := filepath.Join(dataDir, "node.key")
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("cannot read node.key: %v", err)
	}

	seed, err := hex.DecodeString(string(data))
	if err != nil {
		t.Fatalf("node.key is not valid hex: %v", err)
	}

	if len(seed) != ed25519.SeedSize {
		t.Fatalf("node.key seed length = %d, want %d", len(seed), ed25519.SeedSize)
	}

	// Verify we can derive a valid keypair from the seed.
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("derived public key length = %d, want %d", len(pub), ed25519.PublicKeySize)
	}

	// Verify key file permissions (owner-only read/write).
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("cannot stat node.key: %v", err)
	}
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		t.Errorf("node.key permissions = %o, want no group/other access", perm)
	}
}

func TestInitCreatesConfig(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testnode")

	code := cmdInit([]string{"--datadir", dataDir})
	if code != exitSuccess {
		t.Fatalf("cmdInit returned %d, want %d", code, exitSuccess)
	}

	// Verify config file exists and is loadable.
	cfgPath := config.ConfigPath(dataDir)
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("cannot load config: %v", err)
	}

	if cfg.DataDir != dataDir {
		t.Errorf("Config.DataDir = %q, want %q", cfg.DataDir, dataDir)
	}
	if cfg.Network != "mainnet" {
		t.Errorf("Config.Network = %q, want %q", cfg.Network, "mainnet")
	}
}

func TestInitTestnet(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testnode")

	code := cmdInit([]string{"--datadir", dataDir, "--testnet"})
	if code != exitSuccess {
		t.Fatalf("cmdInit returned %d, want %d", code, exitSuccess)
	}

	cfgPath := config.ConfigPath(dataDir)
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("cannot load config: %v", err)
	}

	if cfg.Network != "testnet" {
		t.Errorf("Config.Network = %q, want %q", cfg.Network, "testnet")
	}
}

func TestInitAlreadyInitialized(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testnode")

	// First init should succeed.
	code := cmdInit([]string{"--datadir", dataDir})
	if code != exitSuccess {
		t.Fatalf("first cmdInit returned %d, want %d", code, exitSuccess)
	}

	// Second init should fail.
	code = cmdInit([]string{"--datadir", dataDir})
	if code != exitError {
		t.Errorf("second cmdInit returned %d, want %d (already initialized)", code, exitError)
	}
}

// ---------------------------------------------------------------------------
// loadNodeKey round-trip tests
// ---------------------------------------------------------------------------

func TestLoadNodeKeyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "testnode")

	code := cmdInit([]string{"--datadir", dataDir})
	if code != exitSuccess {
		t.Fatalf("cmdInit returned %d, want %d", code, exitSuccess)
	}

	pub, priv, err := loadNodeKey(dataDir)
	if err != nil {
		t.Fatalf("loadNodeKey: %v", err)
	}

	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("public key length = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Errorf("private key length = %d, want %d", len(priv), ed25519.PrivateKeySize)
	}

	// Verify the key can sign and verify.
	msg := []byte("test message")
	sig := ed25519.Sign(priv, msg)
	if !ed25519.Verify(pub, msg, sig) {
		t.Error("key pair cannot sign/verify correctly")
	}
}

func TestLoadNodeKeyMissing(t *testing.T) {
	dir := t.TempDir()
	_, _, err := loadNodeKey(dir)
	if err == nil {
		t.Error("loadNodeKey should fail with missing key file")
	}
}

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
// Stub command tests
// ---------------------------------------------------------------------------

func TestContractsJSON(t *testing.T) {
	code := cmdContracts([]string{"--json"})
	if code != exitSuccess {
		t.Errorf("cmdContracts --json returned %d, want %d", code, exitSuccess)
	}
}

func TestPeersJSON(t *testing.T) {
	code := cmdPeers([]string{"--json"})
	if code != exitSuccess {
		t.Errorf("cmdPeers --json returned %d, want %d", code, exitSuccess)
	}
}

func TestMine(t *testing.T) {
	code := cmdMine(nil)
	if code != exitSuccess {
		t.Errorf("cmdMine returned %d, want %d", code, exitSuccess)
	}
}

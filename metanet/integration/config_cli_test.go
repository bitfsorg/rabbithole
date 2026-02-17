// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

//go:build integration

package integration

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/tongxiaofeng/metanet/internal/config"
)

// ---------------------------------------------------------------------------
// TestInitAndLoadConfig — create temp dir, init config, reload, modify,
// and verify persistence.
// ---------------------------------------------------------------------------

func TestInitAndLoadConfig(t *testing.T) {
	dataDir := t.TempDir()

	t.Run("init_and_save_default_config", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.DataDir = dataDir

		cfgPath := config.ConfigPath(dataDir)
		err := config.SaveConfig(cfgPath, cfg)
		if err != nil {
			t.Fatalf("SaveConfig: %v", err)
		}

		// Verify file exists.
		if _, err := os.Stat(cfgPath); err != nil {
			t.Fatalf("config file not created: %v", err)
		}
	})

	t.Run("load_config_from_file", func(t *testing.T) {
		cfgPath := config.ConfigPath(dataDir)
		cfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}

		// Verify all defaults are correct.
		if cfg.DataDir != dataDir {
			t.Errorf("DataDir = %q, want %q", cfg.DataDir, dataDir)
		}
		if cfg.ListenAddr != ":8334" {
			t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8334")
		}
		if cfg.RPCAddr != ":8335" {
			t.Errorf("RPCAddr = %q, want %q", cfg.RPCAddr, ":8335")
		}
		if cfg.Network != "mainnet" {
			t.Errorf("Network = %q, want %q", cfg.Network, "mainnet")
		}
		if cfg.MaxPeers != 50 {
			t.Errorf("MaxPeers = %d, want 50", cfg.MaxPeers)
		}
		if cfg.MineEnabled != false {
			t.Errorf("MineEnabled = %v, want false", cfg.MineEnabled)
		}
		if cfg.LogLevel != "info" {
			t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
		}
	})

	t.Run("modify_save_reload", func(t *testing.T) {
		cfgPath := config.ConfigPath(dataDir)
		cfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}

		// Modify config.
		cfg.Network = "testnet"
		cfg.MaxPeers = 25
		cfg.MineEnabled = true
		cfg.LogLevel = "debug"
		cfg.LogFile = filepath.Join(dataDir, "metanet.log")

		// Save modified config.
		err = config.SaveConfig(cfgPath, cfg)
		if err != nil {
			t.Fatalf("SaveConfig modified: %v", err)
		}

		// Reload and verify changes persisted.
		reloaded, err := config.LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig reloaded: %v", err)
		}

		if reloaded.Network != "testnet" {
			t.Errorf("reloaded Network = %q, want %q", reloaded.Network, "testnet")
		}
		if reloaded.MaxPeers != 25 {
			t.Errorf("reloaded MaxPeers = %d, want 25", reloaded.MaxPeers)
		}
		if reloaded.MineEnabled != true {
			t.Errorf("reloaded MineEnabled = %v, want true", reloaded.MineEnabled)
		}
		if reloaded.LogLevel != "debug" {
			t.Errorf("reloaded LogLevel = %q, want %q", reloaded.LogLevel, "debug")
		}
		if reloaded.LogFile != filepath.Join(dataDir, "metanet.log") {
			t.Errorf("reloaded LogFile = %q, want %q",
				reloaded.LogFile, filepath.Join(dataDir, "metanet.log"))
		}
	})

	t.Run("validate_default_config", func(t *testing.T) {
		cfg := config.DefaultConfig()
		err := config.ValidateConfig(cfg)
		if err != nil {
			t.Errorf("DefaultConfig should validate: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestInitTestnetVsMainnet — verify different network configurations.
// ---------------------------------------------------------------------------

func TestInitTestnetVsMainnet(t *testing.T) {
	t.Run("mainnet_config", func(t *testing.T) {
		dir := t.TempDir()
		cfg := config.DefaultConfig()
		cfg.DataDir = dir
		cfg.Network = "mainnet"

		err := config.ValidateConfig(cfg)
		if err != nil {
			t.Errorf("mainnet config should validate: %v", err)
		}

		cfgPath := config.ConfigPath(dir)
		err = config.SaveConfig(cfgPath, cfg)
		if err != nil {
			t.Fatalf("SaveConfig mainnet: %v", err)
		}

		loaded, err := config.LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig mainnet: %v", err)
		}
		if loaded.Network != "mainnet" {
			t.Errorf("Network = %q, want %q", loaded.Network, "mainnet")
		}
	})

	t.Run("testnet_config", func(t *testing.T) {
		dir := t.TempDir()
		cfg := config.DefaultConfig()
		cfg.DataDir = dir
		cfg.Network = "testnet"

		err := config.ValidateConfig(cfg)
		if err != nil {
			t.Errorf("testnet config should validate: %v", err)
		}

		cfgPath := config.ConfigPath(dir)
		err = config.SaveConfig(cfgPath, cfg)
		if err != nil {
			t.Fatalf("SaveConfig testnet: %v", err)
		}

		loaded, err := config.LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig testnet: %v", err)
		}
		if loaded.Network != "testnet" {
			t.Errorf("Network = %q, want %q", loaded.Network, "testnet")
		}
	})

	t.Run("invalid_network_rejected", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Network = "devnet"
		err := config.ValidateConfig(cfg)
		if err == nil {
			t.Error("invalid network should be rejected")
		}
	})

	t.Run("both_networks_have_same_structure", func(t *testing.T) {
		mainnetDir := t.TempDir()
		testnetDir := t.TempDir()

		mainnetCfg := config.DefaultConfig()
		mainnetCfg.DataDir = mainnetDir
		mainnetCfg.Network = "mainnet"

		testnetCfg := config.DefaultConfig()
		testnetCfg.DataDir = testnetDir
		testnetCfg.Network = "testnet"

		// Both should have the same default ports and settings
		// (except network name and datadir).
		if mainnetCfg.ListenAddr != testnetCfg.ListenAddr {
			t.Errorf("ListenAddr differs: mainnet=%q, testnet=%q",
				mainnetCfg.ListenAddr, testnetCfg.ListenAddr)
		}
		if mainnetCfg.RPCAddr != testnetCfg.RPCAddr {
			t.Errorf("RPCAddr differs: mainnet=%q, testnet=%q",
				mainnetCfg.RPCAddr, testnetCfg.RPCAddr)
		}
		if mainnetCfg.MaxPeers != testnetCfg.MaxPeers {
			t.Errorf("MaxPeers differs: mainnet=%d, testnet=%d",
				mainnetCfg.MaxPeers, testnetCfg.MaxPeers)
		}
	})
}

// ---------------------------------------------------------------------------
// TestNodeIdentityPersistence — generate ed25519 keypair, save, reload,
// verify signing works.
// ---------------------------------------------------------------------------

func TestNodeIdentityPersistence(t *testing.T) {
	dataDir := t.TempDir()
	keyPath := filepath.Join(dataDir, "node.key")

	// Step 1: Generate ed25519 keypair.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// Step 2: Save private key seed as hex.
	seed := priv.Seed()
	err = os.WriteFile(keyPath, []byte(hex.EncodeToString(seed)), 0600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("key_file_created", func(t *testing.T) {
		info, err := os.Stat(keyPath)
		if err != nil {
			t.Fatalf("key file not created: %v", err)
		}
		// Verify permissions (owner only).
		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Errorf("key file permissions = %o, want 0600", perm)
		}
	})

	t.Run("reload_key_same_identity", func(t *testing.T) {
		// Read key back.
		data, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}

		loadedSeed, err := hex.DecodeString(string(data))
		if err != nil {
			t.Fatalf("DecodeString: %v", err)
		}

		if len(loadedSeed) != ed25519.SeedSize {
			t.Fatalf("seed length = %d, want %d", len(loadedSeed), ed25519.SeedSize)
		}

		// Reconstruct key pair.
		loadedPriv := ed25519.NewKeyFromSeed(loadedSeed)
		loadedPub := loadedPriv.Public().(ed25519.PublicKey)

		// Verify same identity.
		if !pub.Equal(loadedPub) {
			t.Error("reloaded public key does not match original")
		}
	})

	t.Run("sign_and_verify", func(t *testing.T) {
		message := []byte("test message for signing")
		sig := ed25519.Sign(priv, message)

		if !ed25519.Verify(pub, message, sig) {
			t.Error("signature verification failed")
		}
	})

	t.Run("reload_and_sign", func(t *testing.T) {
		// Load key again and sign.
		data, _ := os.ReadFile(keyPath)
		loadedSeed, _ := hex.DecodeString(string(data))
		loadedPriv := ed25519.NewKeyFromSeed(loadedSeed)
		loadedPub := loadedPriv.Public().(ed25519.PublicKey)

		message := []byte("another message")
		sig := ed25519.Sign(loadedPriv, message)

		if !ed25519.Verify(loadedPub, message, sig) {
			t.Error("reloaded key: signature verification failed")
		}

		// Signature from reloaded key should verify with original public key.
		if !ed25519.Verify(pub, message, sig) {
			t.Error("signature from reloaded key should verify with original public key")
		}
	})

	t.Run("different_keys_produce_different_identities", func(t *testing.T) {
		pub2, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey 2: %v", err)
		}
		if pub.Equal(pub2) {
			t.Error("different key pairs should produce different public keys")
		}
	})

	t.Run("config_and_identity_coexist", func(t *testing.T) {
		// Save config alongside key file.
		cfg := config.DefaultConfig()
		cfg.DataDir = dataDir
		cfgPath := config.ConfigPath(dataDir)

		err := config.SaveConfig(cfgPath, cfg)
		if err != nil {
			t.Fatalf("SaveConfig: %v", err)
		}

		// Both files should exist.
		if _, err := os.Stat(keyPath); err != nil {
			t.Error("key file should exist")
		}
		if _, err := os.Stat(cfgPath); err != nil {
			t.Error("config file should exist")
		}

		// Load config and verify.
		loaded, err := config.LoadConfig(cfgPath)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if loaded.DataDir != dataDir {
			t.Errorf("DataDir = %q, want %q", loaded.DataDir, dataDir)
		}
	})
}

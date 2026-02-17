// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// DefaultConfig tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"ListenAddr", cfg.ListenAddr, ":8334"},
		{"RPCAddr", cfg.RPCAddr, ":8335"},
		{"Network", cfg.Network, "mainnet"},
		{"MaxPeers", cfg.MaxPeers, 50},
		{"MineEnabled", cfg.MineEnabled, false},
		{"LogLevel", cfg.LogLevel, "info"},
		{"LogFile", cfg.LogFile, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}

	// DataDir should end with .metanet (we don't assert the full path
	// since it depends on the home directory).
	if cfg.DataDir == "" {
		t.Error("DataDir should not be empty")
	}
}

// ---------------------------------------------------------------------------
// SaveConfig / LoadConfig round-trip tests
// ---------------------------------------------------------------------------

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	original := Config{
		DataDir:     "/tmp/test-metanet",
		ListenAddr:  ":9000",
		RPCAddr:     ":9001",
		Network:     "testnet",
		MaxPeers:    25,
		MineEnabled: true,
		LogLevel:    "debug",
		LogFile:     "/tmp/metanet.log",
	}

	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"DataDir", loaded.DataDir, original.DataDir},
		{"ListenAddr", loaded.ListenAddr, original.ListenAddr},
		{"RPCAddr", loaded.RPCAddr, original.RPCAddr},
		{"Network", loaded.Network, original.Network},
		{"MaxPeers", loaded.MaxPeers, original.MaxPeers},
		{"MineEnabled", loaded.MineEnabled, original.MineEnabled},
		{"LogLevel", loaded.LogLevel, original.LogLevel},
		{"LogFile", loaded.LogFile, original.LogFile},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestSaveConfigCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "config")

	cfg := DefaultConfig()
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig should create parent dirs: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("Config file not created: %v", err)
	}
}

// ---------------------------------------------------------------------------
// LoadConfig error tests
// ---------------------------------------------------------------------------

func TestLoadConfigNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config")
	if !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("LoadConfig nonexistent: got %v, want ErrConfigNotFound", err)
	}
}

func TestLoadConfigInvalidLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	content := "this-is-not-key-value\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if !errors.Is(err, ErrInvalidConfigLine) {
		t.Errorf("LoadConfig bad line: got %v, want ErrInvalidConfigLine", err)
	}
}

func TestLoadConfigCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	content := `# This is a comment
network = testnet

# Another comment
maxpeers = 10
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Network != "testnet" {
		t.Errorf("Network = %q, want %q", cfg.Network, "testnet")
	}
	if cfg.MaxPeers != 10 {
		t.Errorf("MaxPeers = %d, want %d", cfg.MaxPeers, 10)
	}
	// Unset fields should retain defaults.
	if cfg.ListenAddr != ":8334" {
		t.Errorf("ListenAddr = %q, want default %q", cfg.ListenAddr, ":8334")
	}
}

func TestLoadConfigUnknownKeysIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	content := "futurekey = futurevalue\nnetwork = testnet\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig with unknown key: %v", err)
	}
	if cfg.Network != "testnet" {
		t.Errorf("Network = %q, want %q", cfg.Network, "testnet")
	}
}

// ---------------------------------------------------------------------------
// ValidateConfig tests
// ---------------------------------------------------------------------------

func TestValidateConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("ValidateConfig(DefaultConfig()) = %v, want nil", err)
	}
}

func TestValidateConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr error
	}{
		{
			name:    "empty_datadir",
			modify:  func(c *Config) { c.DataDir = "" },
			wantErr: ErrEmptyDataDir,
		},
		{
			name:    "bad_network",
			modify:  func(c *Config) { c.Network = "devnet" },
			wantErr: ErrInvalidNetwork,
		},
		{
			name:    "bad_listen_addr",
			modify:  func(c *Config) { c.ListenAddr = "not-a-valid-addr" },
			wantErr: ErrInvalidListenAddr,
		},
		{
			name:    "bad_rpc_addr",
			modify:  func(c *Config) { c.RPCAddr = "bad" },
			wantErr: ErrInvalidRPCAddr,
		},
		{
			name:    "negative_maxpeers",
			modify:  func(c *Config) { c.MaxPeers = -1 },
			wantErr: ErrInvalidMaxPeers,
		},
		{
			name:    "too_many_maxpeers",
			modify:  func(c *Config) { c.MaxPeers = 1001 },
			wantErr: ErrInvalidMaxPeers,
		},
		{
			name:    "bad_loglevel",
			modify:  func(c *Config) { c.LogLevel = "verbose" },
			wantErr: ErrInvalidLogLevel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.modify(&cfg)
			err := ValidateConfig(cfg)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("ValidateConfig: got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateConfigValidNetworks(t *testing.T) {
	for _, network := range []string{"mainnet", "testnet"} {
		cfg := DefaultConfig()
		cfg.Network = network
		if err := ValidateConfig(cfg); err != nil {
			t.Errorf("ValidateConfig with network %q: %v", network, err)
		}
	}
}

func TestValidateConfigValidLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		cfg := DefaultConfig()
		cfg.LogLevel = level
		if err := ValidateConfig(cfg); err != nil {
			t.Errorf("ValidateConfig with loglevel %q: %v", level, err)
		}
	}
}

func TestValidateConfigEdgeMaxPeers(t *testing.T) {
	// Zero and 1000 are valid boundary values.
	for _, n := range []int{0, 1, 999, 1000} {
		cfg := DefaultConfig()
		cfg.MaxPeers = n
		if err := ValidateConfig(cfg); err != nil {
			t.Errorf("ValidateConfig with maxpeers %d: %v", n, err)
		}
	}
}

// ---------------------------------------------------------------------------
// ConfigPath tests
// ---------------------------------------------------------------------------

func TestConfigPath(t *testing.T) {
	got := ConfigPath("/home/user/.metanet")
	want := filepath.Join("/home/user/.metanet", "config")
	if got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
}

// Copyright (c) 2024 The BitFS developers
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
		{"ListenAddr", cfg.ListenAddr, ":8080"},
		{"Network", cfg.Network, "mainnet"},
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

	// DataDir should end with .bitfs (we don't assert the full path
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
		DataDir:    "/tmp/test-bitfs",
		ListenAddr: ":9000",
		Network:    "testnet",
		LogLevel:   "debug",
		LogFile:    "/tmp/bitfs.log",
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
		{"Network", loaded.Network, original.Network},
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
loglevel = debug
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
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	// Unset fields should retain defaults.
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want default %q", cfg.ListenAddr, ":8080")
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

// ---------------------------------------------------------------------------
// ConfigPath tests
// ---------------------------------------------------------------------------

func TestConfigPath(t *testing.T) {
	got := ConfigPath("/home/user/.bitfs")
	want := filepath.Join("/home/user/.bitfs", "config")
	if got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
}

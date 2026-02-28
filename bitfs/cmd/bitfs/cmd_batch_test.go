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
// runMget tests
// ---------------------------------------------------------------------------

func TestMget_NoArgs(t *testing.T) {
	if code := runMget(nil); code != exitUsageError {
		t.Errorf("got %d, want %d", code, exitUsageError)
	}
}

func TestMget_NoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runMget([]string{"--datadir", dir, "--password", "x", "/dir", filepath.Join(dir, "out")})
	if code != exitWalletError {
		t.Errorf("got %d, want %d", code, exitWalletError)
	}
}

func TestMget_SourceNotFound(t *testing.T) {
	dataDir := initTestWallet(t)
	outDir := t.TempDir()
	code := runMget([]string{"--datadir", dataDir, "--password", "testpass", "/nonexistent", filepath.Join(outDir, "out")})
	if code == exitSuccess {
		t.Error("expected failure for nonexistent source")
	}
}

func TestMget_BadVault(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runMget([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/dir"})
	if code != exitNotFound {
		t.Errorf("got %d, want %d", code, exitNotFound)
	}
}

// ---------------------------------------------------------------------------
// runMput tests
// ---------------------------------------------------------------------------

func TestMput_NoArgs(t *testing.T) {
	if code := runMput(nil); code != exitUsageError {
		t.Errorf("got %d, want %d", code, exitUsageError)
	}
}

func TestMput_OneArg(t *testing.T) {
	code := runMput([]string{"/only-one-arg"})
	if code != exitUsageError {
		t.Errorf("got %d, want %d", code, exitUsageError)
	}
}

func TestMput_NoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runMput([]string{"--datadir", dir, "--password", "x", dir, "/dest"})
	if code != exitWalletError {
		t.Errorf("got %d, want %d", code, exitWalletError)
	}
}

func TestMput_SourceDirNotFound(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runMput([]string{"--datadir", dataDir, "--password", "testpass", "/nonexistent/local/dir", "/dest"})
	if code == exitSuccess {
		t.Error("expected failure for nonexistent local dir")
	}
}

func TestMput_SourceIsFile(t *testing.T) {
	dataDir := initTestWallet(t)
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(tmpFile, []byte("hello"), 0600)
	code := runMput([]string{"--datadir", dataDir, "--password", "testpass", tmpFile, "/dest"})
	// mput expects a directory, not a file
	if code == exitSuccess {
		t.Error("expected failure for file as source")
	}
}

func TestMput_BadVault(t *testing.T) {
	dataDir := initTestWallet(t)
	dir := t.TempDir()
	code := runMput([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", dir, "/dest"})
	if code != exitNotFound {
		t.Errorf("got %d, want %d", code, exitNotFound)
	}
}

// ---------------------------------------------------------------------------
// runWalletFund tests
// ---------------------------------------------------------------------------

func TestWalletFund_NoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runWalletFund([]string{"--datadir", dir, "--password", "x"})
	if code != exitWalletError {
		t.Errorf("got %d, want %d", code, exitWalletError)
	}
}

// ---------------------------------------------------------------------------
// formatSats tests
// ---------------------------------------------------------------------------

func TestFormatSats(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{1000000, "1,000,000"},
		{5000000000, "5,000,000,000"},
	}
	for _, tt := range tests {
		if got := formatSats(tt.input); got != tt.want {
			t.Errorf("formatSats(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// printFundingInstructions smoke test
// ---------------------------------------------------------------------------

func TestPrintFundingInstructions(t *testing.T) {
	// Just verify no panic for each network type.
	for _, net := range []string{"regtest", "testnet", "teratestnet"} {
		t.Run(net, func(t *testing.T) {
			printFundingInstructions(net, "1BitcoinEaterAddressDontSendf59kuE")
		})
	}
}

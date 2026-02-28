// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// runCat tests
// ---------------------------------------------------------------------------

func TestCat_NoArgs(t *testing.T) {
	if code := runCat(nil); code != exitUsageError {
		t.Errorf("got %d, want %d", code, exitUsageError)
	}
}

func TestCat_NoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runCat([]string{"--datadir", dir, "--password", "x", "/file.txt"})
	if code != exitWalletError {
		t.Errorf("got %d, want %d", code, exitWalletError)
	}
}

func TestCat_FileNotFound(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runCat([]string{"--datadir", dataDir, "--password", "testpass", "/nonexistent.txt"})
	if code == exitSuccess {
		t.Error("expected failure for nonexistent file")
	}
}

func TestCat_BadVaultFlag(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runCat([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/file.txt"})
	if code != exitNotFound {
		t.Errorf("got %d, want %d", code, exitNotFound)
	}
}

// ---------------------------------------------------------------------------
// isTextMime tests
// ---------------------------------------------------------------------------

func TestIsTextMime(t *testing.T) {
	tests := []struct {
		mime string
		want bool
	}{
		{"text/plain", true},
		{"text/html", true},
		{"text/css", true},
		{"application/json", true},
		{"application/xml", true},
		{"application/javascript", true},
		{"application/x-yaml", true},
		{"application/octet-stream", false},
		{"image/png", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isTextMime(tt.mime); got != tt.want {
			t.Errorf("isTextMime(%q) = %v, want %v", tt.mime, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// runGet tests
// ---------------------------------------------------------------------------

func TestGet_NoArgs(t *testing.T) {
	if code := runGet(nil); code != exitUsageError {
		t.Errorf("got %d, want %d", code, exitUsageError)
	}
}

func TestGet_NoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runGet([]string{"--datadir", dir, "--password", "x", "/file.txt", filepath.Join(dir, "out.txt")})
	if code != exitWalletError {
		t.Errorf("got %d, want %d", code, exitWalletError)
	}
}

func TestGet_FileNotFound(t *testing.T) {
	dataDir := initTestWallet(t)
	outDir := t.TempDir()
	code := runGet([]string{"--datadir", dataDir, "--password", "testpass", "/nonexistent.txt", filepath.Join(outDir, "out.txt")})
	if code == exitSuccess {
		t.Error("expected failure for nonexistent file")
	}
}

func TestGet_BadVault(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runGet([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/file.txt"})
	if code != exitNotFound {
		t.Errorf("got %d, want %d", code, exitNotFound)
	}
}

// ---------------------------------------------------------------------------
// runCp tests
// ---------------------------------------------------------------------------

func TestCp_NoArgs(t *testing.T) {
	if code := runCp(nil); code != exitUsageError {
		t.Errorf("got %d, want %d", code, exitUsageError)
	}
}

func TestCp_OneArg(t *testing.T) {
	code := runCp([]string{"/src.txt"})
	if code != exitUsageError {
		t.Errorf("got %d, want %d", code, exitUsageError)
	}
}

func TestCp_NoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runCp([]string{"--datadir", dir, "--password", "x", "/src.txt", "/dst.txt"})
	if code != exitWalletError {
		t.Errorf("got %d, want %d", code, exitWalletError)
	}
}

func TestCp_SourceNotFound(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runCp([]string{"--datadir", dataDir, "--password", "testpass", "/nonexistent.txt", "/copy.txt"})
	if code == exitSuccess {
		t.Error("expected failure for nonexistent source")
	}
}

func TestCp_BadVault(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runCp([]string{"--datadir", dataDir, "--password", "testpass", "--vault", "nonexistent", "/src.txt", "/dst.txt"})
	if code != exitNotFound {
		t.Errorf("got %d, want %d", code, exitNotFound)
	}
}

// ---------------------------------------------------------------------------
// runUnpublish tests
// ---------------------------------------------------------------------------

func TestUnpublish_NoArgs(t *testing.T) {
	if code := runUnpublish(nil); code != exitUsageError {
		t.Errorf("got %d, want %d", code, exitUsageError)
	}
}

func TestUnpublish_NoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runUnpublish([]string{"--datadir", dir, "--password", "x", "example.com"})
	if code != exitWalletError {
		t.Errorf("got %d, want %d", code, exitWalletError)
	}
}

func TestUnpublish_NoDomain(t *testing.T) {
	dataDir := initTestWallet(t)
	code := runUnpublish([]string{"--datadir", dataDir, "--password", "testpass", "not-bound.com"})
	if code == exitSuccess {
		t.Error("expected failure for unbound domain")
	}
}

// ---------------------------------------------------------------------------
// runVerify tests
// ---------------------------------------------------------------------------

func TestVerify_NoArgs(t *testing.T) {
	if code := runVerify(nil); code != exitUsageError {
		t.Errorf("got %d, want %d", code, exitUsageError)
	}
}

func TestVerify_NoWallet(t *testing.T) {
	dir := t.TempDir()
	code := runVerify([]string{"--datadir", dir, "--password", "x", "deadbeef"})
	if code != exitWalletError {
		t.Errorf("got %d, want %d", code, exitWalletError)
	}
}

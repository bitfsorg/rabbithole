// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bitfsorg/libbitfs-go/config"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// runCat handles the "bitfs cat" command.
func runCat(args []string) int {
	fs := flag.NewFlagSet("cat", flag.ContinueOnError)
	vaultName := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")
	force := fs.Bool("force", false, "output binary files without warning")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs cat <path> [--vault N] [--force]\n")
		return exitUsageError
	}

	remotePath := fs.Arg(0)

	pass, err := resolvePassword(*password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	eng, err := vault.New(*dataDir, pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer func() { _ = eng.Close() }()

	// Vault resolution kept for CLI flag compatibility; Cat resolves by path.
	if _, err := eng.ResolveVaultIndex(*vaultName); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNotFound
	}

	reader, info, err := eng.Cat(&vault.CatOpts{
		Path: remotePath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	// Warn for binary files unless --force.
	if !*force && !isTextMime(info.MimeType) {
		fmt.Fprintf(os.Stderr, "Binary file (%s, %d bytes). Use --force to output or 'bitfs get' to download.\n", info.MimeType, info.FileSize)
		return exitError
	}

	if _, err := io.Copy(os.Stdout, reader); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		return exitError
	}

	return exitSuccess
}

// isTextMime returns true if the MIME type is a text-based format.
func isTextMime(mime string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	textTypes := []string{
		"application/json",
		"application/xml",
		"application/javascript",
		"application/x-yaml",
	}
	for _, t := range textTypes {
		if mime == t {
			return true
		}
	}
	return false
}

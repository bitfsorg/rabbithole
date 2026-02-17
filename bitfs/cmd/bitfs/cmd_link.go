// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tongxiaofeng/bitfs/internal/config"
)

// runLink handles the "bitfs link" command.
// Stub: parses args, loads wallet, derives key, prints intended action.
func runLink(args []string) int {
	fs := flag.NewFlagSet("link", flag.ContinueOnError)
	soft := fs.Bool("soft", false, "create soft link instead of hard link")
	vault := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs link <target> <link-path> [--soft] [--vault N]\n")
		return exitUsageError
	}

	target := fs.Arg(0)
	linkPath := fs.Arg(1)

	w, state, err := loadWalletFromDataDir(*dataDir, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	vaultIdx, err := resolveVaultIndex(w, state, *vault)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNotFound
	}

	indices := pathToIndices(linkPath)
	key, err := w.DeriveNodeKey(vaultIdx, indices, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	linkType := "hard"
	if *soft {
		linkType = "soft"
	}

	fmt.Printf("Would create %s link: %s -> %s\n", linkType, linkPath, target)
	fmt.Printf("  Vault:    %d\n", vaultIdx)
	fmt.Printf("  Key path: %s\n", key.Path)

	return exitSuccess
}

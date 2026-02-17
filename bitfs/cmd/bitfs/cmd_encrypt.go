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

// runEncrypt handles the "bitfs encrypt" command.
// Stub: parses args, loads wallet, prints intended action.
// Converts content from FREE access to PRIVATE (encrypted) access.
func runEncrypt(args []string) int {
	fs := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	vault := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs encrypt <remote-path> [--vault N]\n")
		return exitUsageError
	}

	remotePath := fs.Arg(0)

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

	indices := pathToIndices(remotePath)
	key, err := w.DeriveNodeKey(vaultIdx, indices, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	fmt.Printf("Would encrypt %s (FREE -> PRIVATE)\n", remotePath)
	fmt.Printf("  Vault:    %d\n", vaultIdx)
	fmt.Printf("  Key path: %s\n", key.Path)

	return exitSuccess
}

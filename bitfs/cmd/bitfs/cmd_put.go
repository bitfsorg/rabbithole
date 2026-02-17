// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tongxiaofeng/libbitfs/config"
)

// runPut handles the "bitfs put" command.
// Stub: parses args, loads wallet, derives key, prints intended action.
func runPut(args []string) int {
	fs := flag.NewFlagSet("put", flag.ContinueOnError)
	vault := fs.String("vault", "", "vault name")
	access := fs.String("access", "free", "access mode: free or private")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs put <local-file> <remote-path> [--vault N] [--access free|private]\n")
		return exitUsageError
	}

	localFile := fs.Arg(0)
	remotePath := fs.Arg(1)

	if *access != "free" && *access != "private" {
		fmt.Fprintf(os.Stderr, "Error: --access must be 'free' or 'private'\n")
		return exitUsageError
	}

	// Verify local file exists.
	if _, err := os.Stat(localFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: local file %q not found\n", localFile)
		return exitNotFound
	}

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

	fmt.Printf("Would upload %q to %s\n", localFile, remotePath)
	fmt.Printf("  Vault:    %d\n", vaultIdx)
	fmt.Printf("  Access:   %s\n", *access)
	fmt.Printf("  Key path: %s\n", key.Path)

	return exitSuccess
}

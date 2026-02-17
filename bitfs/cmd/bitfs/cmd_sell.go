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

// runSell handles the "bitfs sell" command.
// Stub: parses args, loads wallet, prints intended action.
func runSell(args []string) int {
	fs := flag.NewFlagSet("sell", flag.ContinueOnError)
	price := fs.Int("price", 0, "price in sats/KB")
	vault := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs sell <remote-path> --price <sats/KB> [--vault N]\n")
		return exitUsageError
	}

	if *price <= 0 {
		fmt.Fprintf(os.Stderr, "Error: --price must be a positive integer (sats/KB)\n")
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

	fmt.Printf("Would set price for %s to %d sats/KB\n", remotePath, *price)
	fmt.Printf("  Vault:    %d\n", vaultIdx)
	fmt.Printf("  Key path: %s\n", key.Path)

	return exitSuccess
}

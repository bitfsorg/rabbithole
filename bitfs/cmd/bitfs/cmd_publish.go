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

// runPublish handles the "bitfs publish" command.
// Stub: parses args, loads wallet, prints intended action.
// Binds a domain to a vault's root via DNSLink.
func runPublish(args []string) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	vault := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs publish <domain> [--vault N]\n")
		return exitUsageError
	}

	domain := fs.Arg(0)

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

	rootKey, err := w.DeriveVaultRootKey(vaultIdx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	fmt.Printf("Would publish vault to domain %s\n", domain)
	fmt.Printf("  Vault:         %d\n", vaultIdx)
	fmt.Printf("  Root key path: %s\n", rootKey.Path)

	return exitSuccess
}

// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tongxiaofeng/bitfs/internal/engine"
	"github.com/tongxiaofeng/libbitfs/config"
)

// runPublish handles the "bitfs publish" command.
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

	eng, err := engine.New(*dataDir, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer eng.Close()

	vaultIdx, err := eng.ResolveVaultIndex(*vault)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNotFound
	}

	result, err := eng.Publish(&engine.PublishOpts{
		VaultIndex: vaultIdx,
		Domain:     domain,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	fmt.Println(result.Message)

	return exitSuccess
}

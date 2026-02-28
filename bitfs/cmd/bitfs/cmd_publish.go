// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bitfsorg/bitfs/internal/publish"
	"github.com/bitfsorg/libbitfs-go/config"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// runPublish handles the "bitfs publish" command.
// With a domain argument, it binds that domain to a vault via DNSLink.
// Without arguments, it lists all existing publish bindings with verification status.
func runPublish(args []string) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	vaultName := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	pass, err := resolvePassword(*password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	v, err := vault.New(*dataDir, pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer func() { _ = v.Close() }()

	dns := publish.DefaultDNSResolver()

	// No domain argument: list all bindings.
	if fs.NArg() < 1 {
		result, err := publish.Publish(v, dns, &publish.PublishOpts{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitError
		}
		fmt.Println(result.Message)
		return exitSuccess
	}

	domain := fs.Arg(0)

	vaultIdx, err := v.ResolveVaultIndex(*vaultName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNotFound
	}

	result, err := publish.Publish(v, dns, &publish.PublishOpts{
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

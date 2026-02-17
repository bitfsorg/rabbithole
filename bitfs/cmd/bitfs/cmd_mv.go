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

// runMv handles the "bitfs mv" command.
// Stub: parses args, loads wallet, derives keys, prints intended action.
func runMv(args []string) int {
	fs := flag.NewFlagSet("mv", flag.ContinueOnError)
	vault := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs mv <src> <dst> [--vault N]\n")
		return exitUsageError
	}

	src := fs.Arg(0)
	dst := fs.Arg(1)

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

	srcIndices := pathToIndices(src)
	srcKey, err := w.DeriveNodeKey(vaultIdx, srcIndices, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	dstIndices := pathToIndices(dst)
	dstKey, err := w.DeriveNodeKey(vaultIdx, dstIndices, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	fmt.Printf("Would move %s -> %s\n", src, dst)
	fmt.Printf("  Vault:        %d\n", vaultIdx)
	fmt.Printf("  Src key path: %s\n", srcKey.Path)
	fmt.Printf("  Dst key path: %s\n", dstKey.Path)

	return exitSuccess
}

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

// runMv handles the "bitfs mv" command.
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

	result, err := eng.Move(&engine.MoveOpts{
		VaultIndex: vaultIdx,
		SrcPath:    src,
		DstPath:    dst,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	fmt.Println(result.Message)
	if result.TxHex != "" {
		fmt.Printf("  TxID: %s\n", result.TxID)
		fmt.Printf("  Raw:  %s\n", result.TxHex)
	}

	return exitSuccess
}

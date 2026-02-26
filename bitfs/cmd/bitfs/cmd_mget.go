// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tongxiaofeng/bitfs/internal/engine"
	"github.com/tongxiaofeng/libbitfs-go/config"
)

// runMget handles the "bitfs mget" command.
func runMget(args []string) int {
	fs := flag.NewFlagSet("mget", flag.ContinueOnError)
	vault := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs mget <remote-dir> [local-dir] [--vault N]\n")
		return exitUsageError
	}

	remotePath := fs.Arg(0)
	localDir, _ := os.Getwd()
	if fs.NArg() > 1 {
		localDir = fs.Arg(1)
	}

	pass, err := resolvePassword(*password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	eng, err := engine.New(*dataDir, pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer func() { _ = eng.Close() }()

	vaultIdx, err := eng.ResolveVaultIndex(*vault)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNotFound
	}

	result, err := eng.Mget(&engine.MgetOpts{
		VaultIndex: vaultIdx,
		RemotePath: remotePath,
		LocalDir:   localDir,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	fmt.Printf("Downloaded %d files, created %d directories\n",
		result.FilesDownloaded, result.DirsCreated)
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", e)
	}

	return exitSuccess
}

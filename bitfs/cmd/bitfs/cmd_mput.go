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

// runMput handles the "bitfs mput" command.
func runMput(args []string) int {
	fs := flag.NewFlagSet("mput", flag.ContinueOnError)
	vault := fs.String("vault", "", "vault name")
	access := fs.String("access", "free", "access mode: free or private")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs mput <local-dir> [remote-dir] [--vault N] [--access free|private]\n")
		return exitUsageError
	}

	localDir := fs.Arg(0)
	remoteDir := "/"
	if fs.NArg() > 1 {
		remoteDir = fs.Arg(1)
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

	result, err := eng.Mput(&engine.MputOpts{
		VaultIndex: vaultIdx,
		LocalDir:   localDir,
		RemoteDir:  remoteDir,
		Access:     *access,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	fmt.Printf("Uploaded %d files, created %d directories\n",
		result.FilesUploaded, result.DirsCreated)
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", e)
	}

	return exitSuccess
}

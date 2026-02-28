// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/bitfsorg/libbitfs-go/config"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// runVerify handles the "bitfs verify" command.
// Performs on-demand SPV verification of a transaction against the blockchain.
func runVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")
	rpcURL := fs.String("rpc-url", "", "BSV node JSON-RPC URL")
	rpcUser := fs.String("rpc-user", "", "RPC username")
	rpcPass := fs.String("rpc-pass", "", "RPC password")
	netName := fs.String("network", "regtest", "network name (regtest, testnet, mainnet)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs verify [options] <txid>\n\n")
		fmt.Fprintf(os.Stderr, "Verify a transaction's confirmation status via SPV.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		return exitUsageError
	}

	txid := fs.Arg(0)

	pass, err := resolvePassword(*password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	eng, err := vault.New(*dataDir, pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer func() { _ = eng.Close() }()

	configureChain(eng, *rpcURL, *rpcUser, *rpcPass, *netName)
	if err := eng.InitSPV(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNetError
	}

	if !eng.IsOnline() {
		fmt.Fprintf(os.Stderr, "Error: verify requires a blockchain connection (configure RPC)\n")
		return exitNetError
	}

	result, err := eng.VerifyTx(context.Background(), txid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNetError
	}

	if result.Confirmed {
		fmt.Printf("Verified: tx %s confirmed at block %d (%s)\n", txid, result.BlockHeight, result.BlockHash)
	} else {
		fmt.Printf("Unconfirmed: tx %s has not been confirmed yet\n", txid)
	}

	return exitSuccess
}

// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bsv-blockchain/go-sdk/script"
	qrterminal "github.com/mdp/qrterminal/v3"

	"github.com/bitfsorg/libbitfs-go/config"
	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// runWalletFund handles the "bitfs wallet fund" command.
// Displays the next deposit address with network-specific funding instructions,
// waits for user confirmation, then queries the network to discover new UTXOs.
func runWalletFund(args []string) int {
	fs := flag.NewFlagSet("wallet fund", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")
	rpcURL := fs.String("rpc-url", "", "BSV node JSON-RPC URL (override)")
	rpcUser := fs.String("rpc-user", "", "RPC username (override)")
	rpcPass := fs.String("rpc-pass", "", "RPC password (override)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

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

	// Derive the current fee receive key.
	feeIdx := eng.WState.NextReceiveIndex
	feeKP, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, feeIdx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: derive fee key: %v\n", err)
		return exitWalletError
	}

	// Convert to BSV address (mainnet = "1...", testnet/regtest = "m/n...").
	isMainnet := eng.Wallet.Network().Name == "mainnet"
	addr, err := script.NewAddressFromPublicKey(feeKP.PublicKey, isMainnet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: derive address: %v\n", err)
		return exitWalletError
	}

	netName := eng.Wallet.Network().Name

	fmt.Printf("Deposit Address\n")
	fmt.Printf("  Address:   %s\n", addr.AddressString)
	fmt.Printf("  Network:   %s\n", netName)
	fmt.Printf("  Key path:  %s\n", feeKP.Path)
	fmt.Printf("  Key index: %d\n\n", feeIdx)

	// Show network-specific funding instructions.
	printFundingInstructions(netName, addr.AddressString)

	// Wait for user confirmation.
	fmt.Printf("\nPress Enter after sending payment...")
	var buf [64]byte
	_, _ = os.Stdin.Read(buf[:])
	fmt.Println()

	// Configure blockchain connection.
	cfg, cfgErr := config.LoadConfig(config.ConfigPath(*dataDir))
	if cfgErr != nil {
		cfg = config.DefaultConfig()
	}
	configureChain(eng, *rpcURL, *rpcUser, *rpcPass, cfg.Network)
	if !eng.IsOnline() {
		fmt.Fprintf(os.Stderr, "Error: cannot query network — no RPC configured\n")
		fmt.Fprintf(os.Stderr, "Set BITFS_RPC_URL or use --rpc-url flag.\n")
		return exitNetError
	}

	// Query network for UTXOs at this address.
	fmt.Printf("Checking address %s for UTXOs...\n", addr.AddressString)
	ctx := context.Background()
	pubHex := hex.EncodeToString(feeKP.PublicKey.Compressed())
	if err := eng.RefreshFeeUTXOs(ctx, addr.AddressString, pubHex); err != nil {
		fmt.Fprintf(os.Stderr, "Error: query UTXOs: %v\n", err)
		return exitNetError
	}

	// Bump receive index for next deposit.
	eng.WState.NextReceiveIndex++
	statePath := filepath.Join(*dataDir, "state.json")
	if err := saveWalletState(statePath, eng.WState); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save wallet state: %v\n", err)
	}

	// Tally and display balance.
	var feeBalance uint64
	var feeCount int
	for _, u := range eng.State.UTXOs {
		if u.Spent {
			continue
		}
		if u.Type == "fee" {
			feeBalance += u.Amount
			feeCount++
		}
	}

	if feeCount == 0 {
		fmt.Printf("\nNo UTXOs found yet. The transaction may still be unconfirmed.\n")
		fmt.Printf("Try again in a few minutes with:\n")
		fmt.Printf("  bitfs wallet balance --refresh\n")
	} else {
		fmt.Printf("\nWallet Balance\n")
		fmt.Printf("  Fee (spendable): %s sats (%d UTXOs)\n", formatSats(feeBalance), feeCount)
		fmt.Printf("\nWallet funded successfully!\n")
	}

	return exitSuccess
}

// formatSats formats a satoshi amount with comma-separated thousands (e.g. 5,000,000,000).
func formatSats(n uint64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// printFundingInstructions displays network-specific instructions for funding.
func printFundingInstructions(netName, address string) {
	switch netName {
	case "regtest":
		fmt.Println("Regtest — mine blocks to fund this address:")
		fmt.Println()
		fmt.Println("  1. Start regtest node (if not running):")
		fmt.Println("     make regtest")
		fmt.Println()
		fmt.Println("  2. Mine 101 blocks (first 100 must mature):")
		fmt.Printf("     docker exec bitfs-regtest bitcoin-cli -regtest -rpcuser=bitfs -rpcpassword=bitfs generatetoaddress 101 %s\n", address)

	case "testnet":
		fmt.Println("Testnet — start a local node and use a faucet:")
		fmt.Println()
		fmt.Println("  1. Start testnet node (if not running):")
		fmt.Println("     make testnet")
		fmt.Println()
		fmt.Println("  2. Get tBSV from faucet:")
		fmt.Println("     https://faucet.bitcoincloud.net")
		fmt.Printf("     Send to: %s\n", address)

	case "teratestnet":
		fmt.Println("Teranode STN — start a local node and use a faucet:")
		fmt.Println()
		fmt.Println("  1. Start STN node (if not running):")
		fmt.Println("     make stn")
		fmt.Println()
		fmt.Println("  2. Get tBSV from faucet:")
		fmt.Println("     https://faucet.teranode.bsvblockchain.org")
		fmt.Printf("     Send to: %s\n", address)

	default: // mainnet
		uri := "bitcoin:" + address
		fmt.Println("Scan QR code with a BSV wallet (HandCash, Sensilet, etc.):")
		fmt.Println()
		qrterminal.GenerateWithConfig(uri, qrterminal.Config{
			Level:      qrterminal.M,
			Writer:     os.Stdout,
			HalfBlocks: true,
			BlackChar:  qrterminal.BLACK_BLACK,
			WhiteChar:  qrterminal.WHITE_WHITE,
		})
	}
}

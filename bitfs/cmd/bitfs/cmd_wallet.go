// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bsv-blockchain/go-sdk/script"

	"github.com/bitfsorg/libbitfs-go/config"
	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// runWallet dispatches wallet subcommands.
func runWallet(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs wallet <init|show|balance|fund> [options]\n")
		return exitUsageError
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "init":
		return runWalletInit(subArgs)
	case "show":
		return runWalletShow(subArgs)
	case "balance":
		return runWalletBalance(subArgs)
	case "fund":
		return runWalletFund(subArgs)
	case "--help", "-h":
		fmt.Fprintf(os.Stderr, "Usage: bitfs wallet <init|show|balance|fund> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Subcommands:\n")
		fmt.Fprintf(os.Stderr, "  init      Initialize a new HD wallet\n")
		fmt.Fprintf(os.Stderr, "  show      Show wallet information\n")
		fmt.Fprintf(os.Stderr, "  balance   Show UTXO balance (--refresh to sync from network)\n")
		fmt.Fprintf(os.Stderr, "  fund      Show deposit address with QR code\n")
		return exitSuccess
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown wallet subcommand %q\n", sub)
		return exitUsageError
	}
}

// runWalletInit creates a new HD wallet with encrypted seed storage.
func runWalletInit(args []string) int {
	fs := flag.NewFlagSet("wallet init", flag.ContinueOnError)
	words := fs.Int("words", 12, "mnemonic word count (12 or 24)")
	netName := fs.String("network", "mainnet", "BSV network: mainnet, testnet, teratestnet, or regtest")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing; normally prompted)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	// Validate word count.
	var entropyBits int
	switch *words {
	case 12:
		entropyBits = wallet.Mnemonic12Words
	case 24:
		entropyBits = wallet.Mnemonic24Words
	default:
		fmt.Fprintf(os.Stderr, "Error: --words must be 12 or 24\n")
		return exitUsageError
	}

	// If --network was not explicitly passed, prompt interactively.
	networkSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "network" {
			networkSet = true
		}
	})
	if !networkSet {
		chosen, promptErr := promptNetwork()
		if promptErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", promptErr)
			return exitUsageError
		}
		*netName = chosen
	}

	// Resolve network config.
	netCfg, err := wallet.GetNetwork(*netName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: --network must be mainnet, testnet, teratestnet, or regtest\n")
		return exitUsageError
	}

	// Check if wallet already exists.
	walletPath := filepath.Join(*dataDir, "wallet.enc")
	if _, err := os.Stat(walletPath); err == nil {
		fmt.Fprintf(os.Stderr, "Error: wallet already exists at %s\n", walletPath)
		fmt.Fprintf(os.Stderr, "Remove %s to reinitialize.\n", *dataDir)
		return exitWalletError
	}

	// Create data directory.
	dirs := []string{
		*dataDir,
		filepath.Join(*dataDir, "cache"),
		filepath.Join(*dataDir, "logs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot create directory %s: %v\n", d, err)
			return exitError
		}
	}

	// Generate mnemonic.
	mnemonic, err := wallet.GenerateMnemonic(entropyBits)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to generate mnemonic: %v\n", err)
		return exitWalletError
	}

	// Derive seed.
	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to derive seed: %v\n", err)
		return exitWalletError
	}

	// Get password.
	pass := *password
	if pass == "" {
		var promptErr error
		pass, promptErr = promptPasswordConfirm()
		if promptErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", promptErr)
			return exitWalletError
		}
	}

	// Encrypt and store seed.
	encrypted, err := wallet.EncryptSeed(seed, pass)
	if err != nil {
		zeroString(&pass)
		fmt.Fprintf(os.Stderr, "Error: failed to encrypt seed: %v\n", err)
		return exitWalletError
	}

	if err := os.WriteFile(walletPath, encrypted, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to write wallet file: %v\n", err)
		return exitWalletError
	}

	// Create initial wallet state with a default vault.
	w, err := wallet.NewWallet(seed, netCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create wallet: %v\n", err)
		return exitWalletError
	}

	state := wallet.NewWalletState()
	_, err = w.CreateVault(state, "default")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create default vault: %v\n", err)
		return exitWalletError
	}

	statePath := filepath.Join(*dataDir, "state.json")
	if err := saveWalletState(statePath, state); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to save wallet state: %v\n", err)
		return exitWalletError
	}

	// Write config with selected network.
	cfg := config.DefaultConfig()
	cfg.DataDir = *dataDir
	cfg.Network = *netName
	cfgPath := config.ConfigPath(*dataDir)
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to write config: %v\n", err)
		return exitError
	}

	// Derive the deposit address for display.
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to derive fee key: %v\n", err)
		return exitWalletError
	}

	fmt.Printf("Wallet initialized successfully.\n")
	fmt.Printf("  Data directory: %s\n", *dataDir)
	fmt.Printf("  Words:          %d\n", *words)
	fmt.Printf("  Network:        %s\n", *netName)
	fmt.Printf("  Fee address:    %s\n", hex.EncodeToString(feeKey.PublicKey.Compressed()))
	fmt.Printf("\n")
	fmt.Printf("IMPORTANT: Write down your mnemonic phrase and store it safely.\n")
	fmt.Printf("This is the ONLY time it will be shown.\n\n")
	fmt.Printf("  %s\n\n", mnemonic)

	// Ask if the user wants to fund the wallet now.
	if promptYesNo("Fund wallet now?") {
		code := runWalletFund([]string{"--datadir", *dataDir, "--password", pass})
		zeroString(&pass)
		return code
	}
	zeroString(&pass)
	return exitSuccess
}

// runWalletShow displays wallet information.
func runWalletShow(args []string) int {
	fs := flag.NewFlagSet("wallet show", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing; normally prompted)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	// Load wallet.
	walletPath := filepath.Join(*dataDir, "wallet.enc")
	encrypted, err := os.ReadFile(walletPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot read wallet at %s\n", walletPath)
		fmt.Fprintf(os.Stderr, "Run 'bitfs wallet init' first.\n")
		return exitWalletError
	}

	pass := *password
	if pass == "" {
		var promptErr error
		pass, promptErr = promptPassword("Enter wallet password: ")
		if promptErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", promptErr)
			return exitWalletError
		}
	}

	seed, err := wallet.DecryptSeed(encrypted, pass)
	zeroString(&pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to decrypt wallet: %v\n", err)
		return exitWalletError
	}

	w, err := wallet.NewWallet(seed, &wallet.MainNet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load wallet: %v\n", err)
		return exitWalletError
	}

	// Load state.
	statePath := filepath.Join(*dataDir, "state.json")
	state, err := loadWalletState(statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load wallet state: %v\n", err)
		return exitWalletError
	}

	// Derive fee key.
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to derive fee key: %v\n", err)
		return exitWalletError
	}

	vaults := w.ListVaults(state)

	fmt.Printf("Wallet Information\n")
	fmt.Printf("  Data directory: %s\n", *dataDir)
	fmt.Printf("  Network:        %s\n", w.Network().Name)
	fmt.Printf("  Fee address:    %s\n", hex.EncodeToString(feeKey.PublicKey.Compressed()))
	fmt.Printf("  Fee key path:   %s\n", feeKey.Path)
	fmt.Printf("  Vaults:         %d\n", len(vaults))

	for _, v := range vaults {
		rootKey, err := w.DeriveVaultRootKey(v.AccountIndex)
		if err != nil {
			continue
		}
		fmt.Printf("    - %s (account %d, root %s)\n",
			v.Name, v.AccountIndex,
			hex.EncodeToString(rootKey.PublicKey.Compressed())[:16]+"...")
	}

	return exitSuccess
}

// runWalletBalance displays the UTXO balance.
// With --refresh, it queries the network for new UTXOs before reporting.
func runWalletBalance(args []string) int {
	fs := flag.NewFlagSet("wallet balance", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")
	refresh := fs.Bool("refresh", false, "query network for new UTXOs before showing balance")
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

	if *refresh {
		// Read network from config file.
		cfg, cfgErr := config.LoadConfig(config.ConfigPath(*dataDir))
		if cfgErr != nil {
			cfg = config.DefaultConfig()
		}
		configureChain(eng, *rpcURL, *rpcUser, *rpcPass, cfg.Network)
		if !eng.IsOnline() {
			fmt.Fprintf(os.Stderr, "Error: --refresh requires a blockchain connection (configure RPC)\n")
			return exitNetError
		}

		// Scan all derived external fee addresses for new UTXOs.
		ctx := context.Background()
		isMainnet := eng.Wallet.Network().Name == "mainnet"
		scanned := 0
		for i := uint32(0); i <= eng.WState.NextReceiveIndex; i++ {
			kp, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, i)
			if err != nil {
				continue
			}
			addr, addrErr := script.NewAddressFromPublicKey(kp.PublicKey, isMainnet)
			if addrErr != nil {
				continue
			}
			pubHex := hex.EncodeToString(kp.PublicKey.Compressed())
			if err := eng.RefreshFeeUTXOs(ctx, addr.AddressString, pubHex); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: refresh index %d: %v\n", i, err)
				continue
			}
			scanned++
		}
		fmt.Printf("Scanned %d fee addresses for UTXOs.\n\n", scanned)
	}

	// Tally balances from local state.
	var feeBalance, nodeBalance uint64
	var feeCount, nodeCount int
	for _, u := range eng.State.UTXOs {
		if u.Spent {
			continue
		}
		switch u.Type {
		case "fee":
			feeBalance += u.Amount
			feeCount++
		case "node":
			nodeBalance += u.Amount
			nodeCount++
		}
	}

	total := feeBalance + nodeBalance
	fmt.Printf("Wallet Balance\n")
	fmt.Printf("  Fee (spendable):  %s sats  (%d UTXOs)\n", formatSats(feeBalance), feeCount)
	fmt.Printf("  Node (locked):    %s sats  (%d UTXOs)\n", formatSats(nodeBalance), nodeCount)
	fmt.Printf("  Total:            %s sats\n", formatSats(total))

	return exitSuccess
}

// saveWalletState writes wallet state as JSON.
func saveWalletState(path string, state *wallet.WalletState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// loadWalletState reads wallet state from JSON.
func loadWalletState(path string) (*wallet.WalletState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}
	var state wallet.WalletState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}
	return &state, nil
}

// loadWalletFromDataDir loads the wallet and state from the data directory.
// This helper is shared by filesystem command stubs.
func loadWalletFromDataDir(dataDir, password string) (*wallet.Wallet, *wallet.WalletState, error) {
	walletPath := filepath.Join(dataDir, "wallet.enc")
	encrypted, err := os.ReadFile(walletPath)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read wallet: %w", err)
	}

	pass := password
	if pass == "" {
		var promptErr error
		pass, promptErr = promptPassword("Enter wallet password: ")
		if promptErr != nil {
			return nil, nil, fmt.Errorf("password prompt: %w", promptErr)
		}
	}

	seed, err := wallet.DecryptSeed(encrypted, pass)
	zeroString(&pass)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt wallet: %w", err)
	}

	w, err := wallet.NewWallet(seed, &wallet.MainNet)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	statePath := filepath.Join(dataDir, "state.json")
	state, err := loadWalletState(statePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load state: %w", err)
	}

	return w, state, nil
}

// resolveVaultIndex resolves a vault name or index from the flags.
// If vaultName is empty, it uses the first active vault.
func resolveVaultIndex(w *wallet.Wallet, state *wallet.WalletState, vaultName string) (uint32, error) {
	if vaultName == "" {
		vaults := w.ListVaults(state)
		if len(vaults) == 0 {
			return 0, fmt.Errorf("no vaults found; run 'bitfs vault create <name>'")
		}
		return vaults[0].AccountIndex, nil
	}

	vault, err := w.GetVault(state, vaultName)
	if err != nil {
		return 0, err
	}
	return vault.AccountIndex, nil
}

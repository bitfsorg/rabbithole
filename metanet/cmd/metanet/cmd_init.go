// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tongxiaofeng/metanet/internal/config"
)

// cmdInit handles the "metanet init" command.
// It creates the data directory, generates a node identity keypair,
// and writes the default configuration file.
func cmdInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	testnet := fs.Bool("testnet", false, "use testnet network")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	// Check if already initialized.
	keyPath := filepath.Join(*dataDir, "node.key")
	if _, err := os.Stat(keyPath); err == nil {
		fmt.Fprintf(os.Stderr, "Error: node already initialized at %s\n", *dataDir)
		fmt.Fprintf(os.Stderr, "Remove %s to reinitialize.\n", *dataDir)
		return exitError
	}

	// Create data directory structure.
	dirs := []string{
		*dataDir,
		filepath.Join(*dataDir, "chain", "blocks"),
		filepath.Join(*dataDir, "chain", "state"),
		filepath.Join(*dataDir, "cache"),
		filepath.Join(*dataDir, "channels", "bsv"),
		filepath.Join(*dataDir, "channels", "mnt"),
		filepath.Join(*dataDir, "peers"),
		filepath.Join(*dataDir, "logs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot create directory %s: %v\n", d, err)
			return exitError
		}
	}

	// Generate ed25519 keypair for node identity.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot generate keypair: %v\n", err)
		return exitError
	}

	// Store the private key seed (first 32 bytes of ed25519 private key) as hex.
	seed := priv.Seed()
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(seed)), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot write key file: %v\n", err)
		return exitError
	}

	// Write default config.
	cfg := config.DefaultConfig()
	cfg.DataDir = *dataDir
	if *testnet {
		cfg.Network = "testnet"
	}

	cfgPath := config.ConfigPath(*dataDir)
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot write config: %v\n", err)
		return exitError
	}

	nodeID := hex.EncodeToString(pub)
	fmt.Printf("Metanet Node initialized successfully.\n")
	fmt.Printf("  Data directory: %s\n", *dataDir)
	fmt.Printf("  Network:        %s\n", cfg.Network)
	fmt.Printf("  Node ID:        %s\n", nodeID)
	fmt.Printf("\nRun 'metanet start' to start the node.\n")

	return exitSuccess
}

// loadNodeKey reads the node private key seed from the data directory
// and returns the ed25519 public and private keys.
func loadNodeKey(dataDir string) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	keyPath := filepath.Join(dataDir, "node.key")
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read key file %s: %w", keyPath, err)
	}

	seed, err := hex.DecodeString(string(data))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid key file format: %w", err)
	}

	if len(seed) != ed25519.SeedSize {
		return nil, nil, fmt.Errorf("invalid key seed length: got %d, want %d", len(seed), ed25519.SeedSize)
	}

	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return pub, priv, nil
}

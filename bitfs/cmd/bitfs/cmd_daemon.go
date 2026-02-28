// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/config"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// runDaemon dispatches daemon subcommands.
func runDaemon(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs daemon <start|stop> [options]\n")
		return exitUsageError
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "start":
		return runDaemonStart(subArgs)
	case "stop":
		return runDaemonStop(subArgs)
	case "--help", "-h":
		fmt.Fprintf(os.Stderr, "Usage: bitfs daemon <start|stop> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Subcommands:\n")
		fmt.Fprintf(os.Stderr, "  start    Start the daemon\n")
		fmt.Fprintf(os.Stderr, "  stop     Stop the daemon\n")
		return exitSuccess
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown daemon subcommand %q\n", sub)
		return exitUsageError
	}
}

// runDaemonStart handles "bitfs daemon start".
func runDaemonStart(args []string) int {
	fs := flag.NewFlagSet("daemon start", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "listen address")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")
	rpcURL := fs.String("rpc-url", "", "BSV node JSON-RPC URL")
	rpcUser := fs.String("rpc-user", "", "RPC username")
	rpcPass := fs.String("rpc-pass", "", "RPC password")
	netName := fs.String("network", "regtest", "network name (regtest, testnet, mainnet)")

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

	// Wire up blockchain service if RPC is configured.
	configureChain(eng, *rpcURL, *rpcUser, *rpcPass, *netName)

	// Initialize SPV client and persistent store.
	if err := eng.InitSPV(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: SPV initialization failed: %v\n", err)
	}

	// Create daemon with vault adapters.
	cfg := daemon.DefaultConfig()
	cfg.ListenAddr = *listen
	cfg.Mainnet = eng.Wallet.Network().Name == "mainnet"

	d, err := daemon.New(cfg, newVaultWalletAdapter(eng), newVaultStoreAdapter(eng), newVaultMetanetAdapter(eng))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	// Attach SPV service if available.
	if spvAdapter := newVaultSPVAdapter(eng); spvAdapter != nil {
		d.SetSPV(spvAdapter)
	}

	// Attach chain service for payment broadcast verification.
	if chainAdapter := newVaultChainAdapter(eng); chainAdapter != nil {
		d.SetChain(chainAdapter)
	}

	// Write PID file.
	pidPath := filepath.Join(*dataDir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write PID file: %v\n", err)
	}

	if err := d.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	fmt.Printf("BitFS daemon started on %s\n", *listen)
	fmt.Printf("  Data directory: %s\n", *dataDir)
	fmt.Printf("  PID: %d\n", os.Getpid())
	if eng.IsOnline() {
		fmt.Printf("  Network: online (RPC)\n")
	} else {
		fmt.Printf("  Network: offline\n")
	}

	// Wait for interrupt signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Printf("\nShutting down...\n")
	if err := d.Stop(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
	}
	_ = os.Remove(pidPath)
	fmt.Printf("Daemon stopped.\n")

	return exitSuccess
}

// runDaemonStop handles "bitfs daemon stop".
func runDaemonStop(args []string) int {
	fs := flag.NewFlagSet("daemon stop", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	pidPath := filepath.Join(*dataDir, "daemon.pid")
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no running daemon found (no PID file at %s)\n", pidPath)
		return exitNotFound
	}

	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid PID file: %v\n", err)
		return exitError
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot find process %d: %v\n", pid, err)
		return exitError
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot signal process %d: %v\n", pid, err)
		return exitError
	}

	_ = os.Remove(pidPath)
	fmt.Printf("Sent SIGTERM to daemon (PID %d).\n", pid)

	return exitSuccess
}

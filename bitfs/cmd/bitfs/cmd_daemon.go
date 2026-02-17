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

	"github.com/tongxiaofeng/bitfs/internal/daemon"
	"github.com/tongxiaofeng/bitfs/internal/engine"
	"github.com/tongxiaofeng/libbitfs/config"
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

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	eng, err := engine.New(*dataDir, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer eng.Close()

	// Create daemon with adapter types.
	cfg := daemon.DefaultConfig()
	cfg.ListenAddr = *listen

	walletAdapter := engine.NewWalletAdapter(eng)
	storeAdapter := engine.NewStoreAdapter(eng)
	metanetAdapter := engine.NewMetanetAdapter(eng)

	d, err := daemon.New(cfg, walletAdapter, storeAdapter, metanetAdapter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
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

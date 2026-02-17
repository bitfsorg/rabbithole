// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/tongxiaofeng/metanet/internal/config"
)

// cmdStart handles the "metanet start" command.
// It loads the configuration, verifies the node is initialized,
// and starts the node daemon (stub for now).
func cmdStart(args []string) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	listen := fs.String("listen", "", "override listen address")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	// Verify node is initialized.
	keyPath := filepath.Join(*dataDir, "node.key")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: node not initialized. Run 'metanet init' first.\n")
		return exitError
	}

	// Load config.
	cfgPath := config.ConfigPath(*dataDir)
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot load config: %v\n", err)
		return exitError
	}

	// Apply overrides.
	if *listen != "" {
		cfg.ListenAddr = *listen
	}

	// Validate config.
	if err := config.ValidateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid configuration: %v\n", err)
		return exitError
	}

	// Check if already running.
	pidPath := filepath.Join(*dataDir, "metanet.pid")
	if pid, err := readPIDFile(pidPath); err == nil {
		fmt.Fprintf(os.Stderr, "Error: daemon already running (PID %d).\n", pid)
		return exitError
	}

	// Write PID file.
	pid := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot write PID file: %v\n", err)
		return exitError
	}

	fmt.Printf("Starting Metanet node...\n")
	fmt.Printf("  Data directory: %s\n", cfg.DataDir)
	fmt.Printf("  Network:        %s\n", cfg.Network)
	fmt.Printf("  Listen:         %s\n", cfg.ListenAddr)
	fmt.Printf("  RPC:            %s\n", cfg.RPCAddr)
	fmt.Printf("  PID:            %d\n", pid)
	fmt.Printf("\nNode started (stub — full server not yet implemented).\n")

	// Clean up PID file on exit.
	os.Remove(pidPath)

	return exitSuccess
}

// readPIDFile reads a PID from the given file and checks if the process
// is still running. Returns the PID if the process exists.
func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file: %w", err)
	}

	// Check if the process is still running by sending signal 0.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, err
	}

	// On Unix, FindProcess always succeeds. We need to check if the
	// process actually exists by sending signal 0.
	if err := proc.Signal(os.Signal(nil)); err != nil {
		// Process not running; clean up stale PID file.
		os.Remove(path)
		return 0, fmt.Errorf("process %d not running", pid)
	}

	return pid, nil
}

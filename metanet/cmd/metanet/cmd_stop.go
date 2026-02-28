// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"github.com/bitfsorg/metanet/internal/config"
)

// cmdStop handles the "metanet stop" command.
// It reads the PID file and sends SIGTERM to the running daemon.
func cmdStop(args []string) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	pidPath := filepath.Join(*dataDir, "metanet.pid")
	pid, err := readPIDFile(pidPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: daemon not running.\n")
		return exitError
	}

	// Send SIGTERM to the daemon process.
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot find process %d: %v\n", pid, err)
		return exitError
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot stop daemon (PID %d): %v\n", pid, err)
		// Remove stale PID file if the process no longer exists (best-effort).
		if removeErr := os.Remove(pidPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("warning: failed to remove stale PID file %s: %v", pidPath, removeErr)
		}
		return exitError
	}

	// Remove PID file after sending signal (best-effort).
	if err := os.Remove(pidPath); err != nil && !os.IsNotExist(err) {
		log.Printf("warning: failed to remove PID file %s: %v", pidPath, err)
	}

	fmt.Printf("Sent stop signal to Metanet node (PID %d).\n", pid)
	return exitSuccess
}

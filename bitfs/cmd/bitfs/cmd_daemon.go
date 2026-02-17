// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"

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
// Stub: prints intended action.
func runDaemonStart(args []string) int {
	fs := flag.NewFlagSet("daemon start", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "listen address")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	fmt.Printf("Starting BitFS daemon...\n")
	fmt.Printf("  Data directory: %s\n", *dataDir)
	fmt.Printf("  Listen:         %s\n", *listen)
	fmt.Printf("\nDaemon started (stub -- full server not yet implemented).\n")

	return exitSuccess
}

// runDaemonStop handles "bitfs daemon stop".
// Stub: prints intended action.
func runDaemonStop(args []string) int {
	fs := flag.NewFlagSet("daemon stop", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	fmt.Printf("Stopping BitFS daemon...\n")
	fmt.Printf("  Data directory: %s\n", *dataDir)
	fmt.Printf("\nDaemon stopped (stub -- full server not yet implemented).\n")

	return exitSuccess
}

// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command metanet is the Metanet Node operator's CLI tool. It manages
// the lifecycle of a Metanet Node: initialization, daemon control,
// status monitoring, contract management, peer listing, and mining.
package main

import (
	"fmt"
	"os"
)

// Version is the current build version of the metanet CLI.
const Version = "0.1.0-dev"

// Exit codes.
const (
	exitSuccess    = 0
	exitError      = 1
	exitUsageError = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return exitUsageError
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "init":
		return cmdInit(cmdArgs)
	case "start":
		return cmdStart(cmdArgs)
	case "stop":
		return cmdStop(cmdArgs)
	case "status":
		return cmdStatus(cmdArgs)
	case "contracts":
		return cmdContracts(cmdArgs)
	case "peers":
		return cmdPeers(cmdArgs)
	case "mine":
		return cmdMine(cmdArgs)
	case "--help", "-h", "help":
		printUsage()
		return exitSuccess
	case "--version", "-v", "version":
		fmt.Printf("metanet version %s\n", Version)
		return exitSuccess
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n\n", cmd)
		printUsage()
		return exitUsageError
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `metanet - Metanet Node CLI (version %s)

Usage:
  metanet <command> [options]

Commands:
  init       Initialize a new Metanet Node
  start      Start the Metanet Node daemon
  stop       Stop the Metanet Node daemon
  status     Show node status
  contracts  List storage contracts
  peers      List connected peers
  mine       Configure merged mining

Options:
  --help     Show this help message
  --version  Show version

Run 'metanet <command> --help' for command-specific options.
`, Version)
}

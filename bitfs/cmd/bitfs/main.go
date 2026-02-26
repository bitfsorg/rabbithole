// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bitfs is the main CLI for the BitFS decentralized encrypted
// file system. It provides file management, encryption, trading, wallet,
// vault, publishing, and daemon commands.
package main

import (
	"fmt"
	"os"
)

// Version is the current build version of the bitfs CLI.
// Overridden at build time via -ldflags "-X main.Version=...".
var Version = "0.1.0-dev"

// Exit codes.
const (
	exitSuccess     = 0
	exitError       = 1
	exitUsageError  = 2
	exitWalletError = 3
	exitNetError    = 4
	exitPermError   = 5
	exitNotFound    = 6
	exitConflict    = 7
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
	case "wallet":
		return runWallet(cmdArgs)
	case "vault":
		return runVault(cmdArgs)
	case "cat":
		return runCat(cmdArgs)
	case "get":
		return runGet(cmdArgs)
	case "mget":
		return runMget(cmdArgs)
	case "mput":
		return runMput(cmdArgs)
	case "put":
		return runPut(cmdArgs)
	case "mkdir":
		return runMkdir(cmdArgs)
	case "rm":
		return runRm(cmdArgs)
	case "mv":
		return runMv(cmdArgs)
	case "cp":
		return runCp(cmdArgs)
	case "link":
		return runLink(cmdArgs)
	case "sell":
		return runSell(cmdArgs)
	case "encrypt":
		return runEncrypt(cmdArgs)
	case "publish":
		return runPublish(cmdArgs)
	case "unpublish":
		return runUnpublish(cmdArgs)
	case "daemon":
		return runDaemon(cmdArgs)
	case "shell":
		return runShell(cmdArgs)
	case "verify":
		return runVerify(cmdArgs)
	case "--help", "-h", "help":
		printUsage()
		return exitSuccess
	case "--version", "-v", "version":
		fmt.Printf("bitfs version %s\n", Version)
		return exitSuccess
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n\n", cmd)
		printUsage()
		return exitUsageError
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `bitfs - BitFS Decentralized Encrypted File System (version %s)

Usage:
  bitfs <command> [options]

Wallet Commands:
  wallet init      Initialize HD wallet
  wallet show      Show wallet information
  wallet balance   Show UTXO balance
  wallet fund      Show deposit address with QR code

Vault Commands:
  vault create   Create a new vault
  vault list     List all vaults
  vault rename   Rename a vault
  vault delete   Delete a vault

File Commands:
  cat            View file contents
  get            Download a file
  mget           Download a directory recursively
  mput           Upload a directory recursively
  put            Upload a file
  mkdir          Create a directory
  rm             Remove a file or directory
  mv             Move or rename a file
  cp             Copy a file
  link           Create a hard or soft link

Trading Commands:
  sell           Set price for content
  encrypt        Encrypt content (free -> private)

Publishing Commands:
  publish        Bind a domain via DNSLink
  unpublish      Remove a domain binding

Daemon Commands:
  daemon start   Start the daemon
  daemon stop    Stop the daemon

Verification:
  verify         SPV-verify a transaction

Interactive:
  shell          FTP-style interactive REPL

Options:
  --help         Show this help message
  --version      Show version

Run 'bitfs <command> --help' for command-specific options.
`, Version)
}

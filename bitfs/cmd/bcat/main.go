// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bcat outputs file content from a BitFS filesystem, like Unix cat.
package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/libbitfs/paymail"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bcat", flag.ContinueOnError)
	fs.SetOutput(stderr)

	buy := fs.Bool("buy", false, "attempt to purchase paid content")
	host := fs.String("host", "http://localhost:8080", "daemon URL")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "Usage: bcat [--buy] [--host URL] [--timeout DURATION] <bitfs-uri>\n")
		return 6
	}

	uri := fs.Arg(0)
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: %v\n", err)
		return 6
	}

	// Resolve pnode from parsed URI.
	var pnode string
	switch parsed.Type {
	case paymail.AddressPubKey:
		pnode = hex.EncodeToString(parsed.PubKey)
	case paymail.AddressPaymail, paymail.AddressDNSLink:
		fmt.Fprintf(stderr, "bcat: paymail/dnslink resolution not yet supported\n")
		return 6
	default:
		fmt.Fprintf(stderr, "bcat: unknown address type\n")
		return 6
	}

	// Build client.
	c := client.New(*host)
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bcat: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	// Determine the path to query. Default to root "/" if none specified.
	path := parsed.Path
	if path == "" {
		path = "/"
	}

	meta, err := c.GetMeta(pnode, path)
	if err != nil {
		return handleError(err, stderr)
	}

	// Directories cannot be cat'd.
	if meta.Type == "dir" {
		fmt.Fprintf(stderr, "bcat: %s: is a directory\n", path)
		return 6
	}

	// Handle access modes.
	switch meta.Access {
	case "free":
		return outputContent(c, meta, stdout, stderr)
	case "paid":
		return handlePaid(meta, *buy, stderr)
	case "private":
		fmt.Fprintf(stderr, "bcat: private content cannot be accessed remotely\n")
		return 6
	default:
		fmt.Fprintf(stderr, "bcat: unknown access mode %q\n", meta.Access)
		return 1
	}
}

// outputContent fetches the raw data by key_hash and writes it to stdout.
func outputContent(c *client.Client, meta *client.MetaResponse, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		fmt.Fprintf(stderr, "bcat: no content hash available\n")
		return 1
	}

	// TODO: Decrypt content using Method 42 (D_node=1 for free, session key for paid)
	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleError(err, stderr)
	}
	defer reader.Close()

	if _, err := io.Copy(stdout, reader); err != nil {
		fmt.Fprintf(stderr, "bcat: write error: %v\n", err)
		return 1
	}
	return 0
}

// handlePaid handles paid content access (with or without --buy).
func handlePaid(meta *client.MetaResponse, buy bool, stderr io.Writer) int {
	if buy {
		// TODO: Implement HTLC purchase flow (Phase 3).
		fmt.Fprintf(stderr, "bcat: purchase flow not yet implemented\n")
		return 5
	}
	fmt.Fprintf(stderr, "bcat: content requires payment: %d sat/KB (%d bytes)\nUse --buy to purchase\n",
		meta.PricePerKB, meta.FileSize)
	return 5
}

// handleError maps client errors to exit codes and prints a message.
func handleError(err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "bcat: not found\n")
		return 2
	case errors.Is(err, client.ErrTimeout):
		fmt.Fprintf(stderr, "bcat: request timeout\n")
		return 4
	case errors.Is(err, client.ErrNetwork):
		fmt.Fprintf(stderr, "bcat: network error: %v\n", err)
		return 4
	case errors.Is(err, client.ErrServer):
		fmt.Fprintf(stderr, "bcat: server error: %v\n", err)
		return 4
	default:
		fmt.Fprintf(stderr, "bcat: %v\n", err)
		return 1
	}
}

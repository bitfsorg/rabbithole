// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bget downloads a file from a BitFS filesystem, like Unix wget.
package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/libbitfs/paymail"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bget", flag.ContinueOnError)
	fs.SetOutput(stderr)

	output := fs.String("o", "", "output filename")
	fs.StringVar(output, "output", "", "output filename")
	buy := fs.Bool("buy", false, "attempt to purchase paid content")
	version := fs.Bool("version", false, "show version-specific content")
	host := fs.String("host", "http://localhost:8080", "daemon URL")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if *version {
		fmt.Fprintf(stdout, "bget: --version not yet supported\n")
		return 0
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "Usage: bget [-o FILE] [--buy] [--host URL] [--timeout DURATION] <bitfs-uri>\n")
		return 6
	}

	uri := fs.Arg(0)
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		fmt.Fprintf(stderr, "bget: %v\n", err)
		return 6
	}

	// Resolve pnode from parsed URI.
	var pnode string
	switch parsed.Type {
	case paymail.AddressPubKey:
		pnode = hex.EncodeToString(parsed.PubKey)
	case paymail.AddressPaymail, paymail.AddressDNSLink:
		fmt.Fprintf(stderr, "bget: paymail/dnslink resolution not yet supported\n")
		return 6
	default:
		fmt.Fprintf(stderr, "bget: unknown address type\n")
		return 6
	}

	// Build client.
	c := client.New(*host)
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	// Determine the path to query. Default to root "/" if none specified.
	uriPath := parsed.Path
	if uriPath == "" {
		uriPath = "/"
	}

	meta, err := c.GetMeta(pnode, uriPath)
	if err != nil {
		return handleError(err, stderr)
	}

	// Directories cannot be downloaded.
	if meta.Type == "dir" {
		fmt.Fprintf(stderr, "bget: %s: is a directory\n", uriPath)
		return 6
	}

	// Handle access modes.
	switch meta.Access {
	case "free":
		return downloadContent(c, meta, *output, stdout, stderr)
	case "paid":
		return handlePaid(meta, *buy, stderr)
	case "private":
		fmt.Fprintf(stderr, "bget: private content cannot be accessed remotely\n")
		return 6
	default:
		fmt.Fprintf(stderr, "bget: unknown access mode %q\n", meta.Access)
		return 1
	}
}

// downloadContent fetches the raw data by key_hash and writes it to a file.
func downloadContent(c *client.Client, meta *client.MetaResponse, outputName string, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		fmt.Fprintf(stderr, "bget: no content hash available\n")
		return 1
	}

	// Determine output filename.
	filename := outputName
	if filename == "" {
		filename = deriveFilename(meta.Path)
	}

	// TODO: Decrypt content using Method 42 (D_node=1 for free, session key for paid)
	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleError(err, stderr)
	}
	defer reader.Close()

	file, err := os.Create(filename)
	if err != nil {
		fmt.Fprintf(stderr, "bget: cannot create file %q: %v\n", filename, err)
		return 1
	}

	n, err := io.Copy(file, reader)
	if err != nil {
		file.Close()
		os.Remove(filename)
		fmt.Fprintf(stderr, "bget: write error: %v\n", err)
		return 1
	}

	if err := file.Close(); err != nil {
		os.Remove(filename)
		fmt.Fprintf(stderr, "bget: close error: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Downloaded %d bytes to %s\n", n, filename)
	return 0
}

// deriveFilename extracts a reasonable filename from the URI path.
// If the path is "/" or empty, falls back to "download.dat".
func deriveFilename(uriPath string) string {
	base := path.Base(uriPath)
	if base == "" || base == "/" || base == "." {
		return "download.dat"
	}
	return base
}

// handlePaid handles paid content access (with or without --buy).
func handlePaid(meta *client.MetaResponse, buy bool, stderr io.Writer) int {
	if buy {
		// TODO: Implement HTLC purchase flow (Phase 3).
		fmt.Fprintf(stderr, "bget: purchase flow not yet implemented\n")
		return 5
	}
	fmt.Fprintf(stderr, "bget: content requires payment: %d sat/KB (%d bytes)\nUse --buy to purchase\n",
		meta.PricePerKB, meta.FileSize)
	return 5
}

// handleError maps client errors to exit codes and prints a message.
func handleError(err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "bget: not found\n")
		return 2
	case errors.Is(err, client.ErrTimeout):
		fmt.Fprintf(stderr, "bget: request timeout\n")
		return 4
	case errors.Is(err, client.ErrNetwork):
		fmt.Fprintf(stderr, "bget: network error: %v\n", err)
		return 4
	case errors.Is(err, client.ErrServer):
		fmt.Fprintf(stderr, "bget: server error: %v\n", err)
		return 4
	default:
		fmt.Fprintf(stderr, "bget: %v\n", err)
		return 1
	}
}

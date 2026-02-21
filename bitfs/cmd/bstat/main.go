// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bstat shows file metadata from a BitFS filesystem, like Unix stat.
package main

import (
	"encoding/hex"
	"encoding/json"
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
	fs := flag.NewFlagSet("bstat", flag.ContinueOnError)
	fs.SetOutput(stderr)

	jsonOut := fs.Bool("json", false, "JSON output")
	versions := fs.Bool("versions", false, "show version history")
	host := fs.String("host", "http://localhost:8080", "daemon URL")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	// --versions is a placeholder for future functionality.
	if *versions {
		fmt.Fprintf(stderr, "version listing not yet supported\n")
		return 0
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "Usage: bstat [--json] [--versions] [--host URL] [--timeout DURATION] <bitfs-uri>\n")
		return 6
	}

	uri := fs.Arg(0)
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		fmt.Fprintf(stderr, "bstat: %v\n", err)
		return 6
	}

	// Resolve pnode from parsed URI.
	var pnode string
	switch parsed.Type {
	case paymail.AddressPubKey:
		pnode = hex.EncodeToString(parsed.PubKey)
	case paymail.AddressPaymail, paymail.AddressDNSLink:
		fmt.Fprintf(stderr, "bstat: paymail/dnslink resolution not yet supported\n")
		return 6
	default:
		fmt.Fprintf(stderr, "bstat: unknown address type\n")
		return 6
	}

	// Build client.
	c := client.New(*host)
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bstat: invalid timeout %q: %v\n", *timeout, err)
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

	// Format output.
	if *jsonOut {
		return outputJSON(meta, stdout, stderr)
	}
	return outputHuman(meta, stdout)
}

// outputHuman prints right-aligned label: value pairs, omitting empty/zero fields.
func outputHuman(meta *client.MetaResponse, w io.Writer) int {
	// Labels are right-aligned to 8 characters (longest label is "PriceKB:" = 8).
	// Only show fields that have non-empty/non-zero values.

	fmt.Fprintf(w, "    Path: %s\n", meta.Path)
	fmt.Fprintf(w, "    Type: %s\n", meta.Type)
	fmt.Fprintf(w, "   Owner: %s\n", meta.PNode)
	fmt.Fprintf(w, "  Access: %s\n", meta.Access)

	if meta.MimeType != "" {
		fmt.Fprintf(w, "    MIME: %s\n", meta.MimeType)
	}

	if meta.Type == "dir" {
		fmt.Fprintf(w, "Children: %d\n", len(meta.Children))
	} else {
		if meta.FileSize > 0 {
			fmt.Fprintf(w, "    Size: %s\n", formatSize(meta.FileSize))
		}
		if meta.KeyHash != "" {
			fmt.Fprintf(w, "    Hash: %s\n", meta.KeyHash)
		}
	}

	if meta.PricePerKB > 0 {
		fmt.Fprintf(w, " PriceKB: %d sat\n", meta.PricePerKB)
	}

	if meta.TxID != "" {
		fmt.Fprintf(w, "    TxID: %s\n", meta.TxID)
	}

	return 0
}

// outputJSON marshals the full MetaResponse as indented JSON.
func outputJSON(meta *client.MetaResponse, stdout, stderr io.Writer) int {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "bstat: json marshal: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

// handleError maps client errors to exit codes and prints a message.
func handleError(err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "bstat: not found\n")
		return 2
	case errors.Is(err, client.ErrTimeout):
		fmt.Fprintf(stderr, "bstat: request timeout\n")
		return 4
	case errors.Is(err, client.ErrNetwork):
		fmt.Fprintf(stderr, "bstat: network error: %v\n", err)
		return 4
	case errors.Is(err, client.ErrServer):
		fmt.Fprintf(stderr, "bstat: server error: %v\n", err)
		return 4
	default:
		fmt.Fprintf(stderr, "bstat: %v\n", err)
		return 1
	}
}

// formatSize returns a human-readable file size string.
func formatSize(bytes uint64) string {
	if bytes == 0 {
		return "0"
	}

	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

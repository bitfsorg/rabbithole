// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bls lists directory contents in a BitFS filesystem, like Unix ls.
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
	fs := flag.NewFlagSet("bls", flag.ContinueOnError)
	fs.SetOutput(stderr)

	jsonOut := fs.Bool("json", false, "JSON output")
	long := fs.Bool("long", false, "detailed listing")
	longAlias := fs.Bool("l", false, "detailed listing (alias)")
	host := fs.String("host", "http://localhost:8080", "daemon URL")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	// -l is an alias for --long
	if *longAlias {
		*long = true
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "Usage: bls [--json] [--long|-l] [--host URL] [--timeout DURATION] <bitfs-uri>\n")
		return 6
	}

	uri := fs.Arg(0)
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		fmt.Fprintf(stderr, "bls: %v\n", err)
		return 6
	}

	// Resolve pnode from parsed URI.
	var pnode string
	switch parsed.Type {
	case paymail.AddressPubKey:
		pnode = hex.EncodeToString(parsed.PubKey)
	case paymail.AddressPaymail, paymail.AddressDNSLink:
		fmt.Fprintf(stderr, "bls: paymail/dnslink resolution not yet supported\n")
		return 6
	default:
		fmt.Fprintf(stderr, "bls: unknown address type\n")
		return 6
	}

	// Build client.
	c := client.New(*host)
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bls: invalid timeout %q: %v\n", *timeout, err)
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
	if *long {
		return outputLong(meta, stdout)
	}
	return outputDefault(meta, stdout)
}

// outputDefault prints one name per line, with trailing "/" for directories.
func outputDefault(meta *client.MetaResponse, w io.Writer) int {
	// If this is a file node (not a directory), just print its path/name.
	if meta.Type != "dir" {
		name := meta.Path
		if name == "" {
			name = meta.PNode
		}
		fmt.Fprintln(w, name)
		return 0
	}

	for _, child := range meta.Children {
		name := child.Name
		if child.Type == "dir" {
			name += "/"
		}
		fmt.Fprintln(w, name)
	}
	return 0
}

// outputLong prints type, access, size, and name (tab-separated).
func outputLong(meta *client.MetaResponse, w io.Writer) int {
	// If this is a file node, show its own info.
	if meta.Type != "dir" {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			meta.Type, meta.Access, formatSize(meta.FileSize), meta.Path)
		return 0
	}

	for _, child := range meta.Children {
		size := "-"
		access := "-"
		if meta.Type == "dir" {
			// Children in a directory listing only have name and type.
			// Size and access are not available in ChildEntry.
			access = "-"
			size = "-"
		}
		name := child.Name
		if child.Type == "dir" {
			name += "/"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", child.Type, access, size, name)
	}
	return 0
}

// outputJSON marshals the full MetaResponse as indented JSON.
func outputJSON(meta *client.MetaResponse, stdout, stderr io.Writer) int {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "bls: json marshal: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

// handleError maps client errors to exit codes and prints a message.
func handleError(err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "bls: not found\n")
		return 2
	case errors.Is(err, client.ErrTimeout):
		fmt.Fprintf(stderr, "bls: request timeout\n")
		return 4
	case errors.Is(err, client.ErrNetwork):
		fmt.Fprintf(stderr, "bls: network error: %v\n", err)
		return 4
	case errors.Is(err, client.ErrServer):
		fmt.Fprintf(stderr, "bls: server error: %v\n", err)
		return 4
	default:
		fmt.Fprintf(stderr, "bls: %v\n", err)
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
		return fmt.Sprintf("%.1fT", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.1fG", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1fM", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1fK", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d", bytes)
	}
}

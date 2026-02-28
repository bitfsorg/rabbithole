// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bls lists directory contents in a BitFS filesystem, like Unix ls.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitfsorg/bitfs/internal/buy"
	"github.com/bitfsorg/bitfs/internal/client"
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
	host := fs.String("host", "", "daemon URL override")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")
	keyword := fs.String("keyword", "", "filter children by name substring")
	noCache := fs.Bool("no-cache", false, "skip metadata cache")
	offline := fs.Bool("offline", false, "cache-only mode")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	// -l is an alias for --long
	if *longAlias {
		*long = true
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, `Usage: bls [--json] [--long|-l] [--host URL] [--timeout DURATION] <bitfs-uri>

Examples:
  bls bitfs://example.com/docs/                (domain)
  bls bitfs://alice@example.com/docs/          (paymail)
  bls bitfs://02abc...66chars.../docs/         (pubkey, requires --host)
`)
		return 6
	}

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "bls: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bls: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "bls: cannot determine home directory: %v\n", err)
		return 1
	}
	cacheDir := filepath.Join(homeDir, ".bitfs", "cache", "meta")
	cache := client.NewMetaCache(cacheDir, 5*time.Minute)
	cc := client.NewCachedClient(c, cache)
	cc.NoCache = *noCache
	cc.Offline = *offline
	cc.Prefix = c.BaseURL

	meta, err := cc.GetMeta(resolved.PNode, resolved.Path)
	if err != nil {
		return buy.HandleError(err, "bls", stderr)
	}

	// Apply keyword filter if set.
	if *keyword != "" {
		meta.Children = filterChildren(meta.Children, *keyword)
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
		_, _ = fmt.Fprintln(w, name)
		return 0
	}

	for _, child := range meta.Children {
		name := child.Name
		if child.Type == "dir" {
			name += "/"
		}
		_, _ = fmt.Fprintln(w, name)
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
		// ChildEntry only has name and type; size/access unavailable.
		name := child.Name
		if child.Type == "dir" {
			name += "/"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", child.Type, "-", "-", name)
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
	_, _ = fmt.Fprintln(stdout, string(data))
	return 0
}

// filterChildren returns only children whose name contains the keyword (case-insensitive).
func filterChildren(children []client.ChildEntry, keyword string) []client.ChildEntry {
	if keyword == "" {
		return children
	}
	kw := strings.ToLower(keyword)
	filtered := make([]client.ChildEntry, 0, len(children))
	for _, c := range children {
		if strings.Contains(strings.ToLower(c.Name), kw) {
			filtered = append(filtered, c)
		}
	}
	return filtered
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

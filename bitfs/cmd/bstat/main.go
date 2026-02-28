// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bstat shows file metadata from a BitFS filesystem, like Unix stat.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bitfsorg/bitfs/internal/buy"
	"github.com/bitfsorg/bitfs/internal/client"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bstat", flag.ContinueOnError)
	fs.SetOutput(stderr)

	jsonOut := fs.Bool("json", false, "JSON output")
	versions := fs.Bool("versions", false, "show version history")
	host := fs.String("host", "", "daemon URL override")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")
	noCache := fs.Bool("no-cache", false, "skip metadata cache")
	offline := fs.Bool("offline", false, "cache-only mode")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, `Usage: bstat [--json] [--versions] [--host URL] [--timeout DURATION] <bitfs-uri>

Examples:
  bstat bitfs://example.com/docs/readme.txt          (domain)
  bstat bitfs://alice@example.com/docs/readme.txt    (paymail)
  bstat bitfs://02abc...66chars.../docs/readme.txt   (pubkey, requires --host)
`)
		return 6
	}

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "bstat: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bstat: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	// --versions: fetch version history using the raw (non-cached) client.
	if *versions {
		vers, versErr := c.GetVersions(resolved.PNode, resolved.Path)
		if versErr != nil {
			return buy.HandleError(versErr, "bstat", stderr)
		}
		if *jsonOut {
			data, _ := json.Marshal(vers)
			fmt.Fprintln(stdout, string(data))
			return 0
		}
		fmt.Fprintf(stdout, "Versions for %s (%d total):\n\n", resolved.Path, len(vers))
		for _, v := range vers {
			txID := v.TxID
			if len(txID) > 16 {
				txID = txID[:16] + "..."
			}
			t := time.Unix(v.Timestamp, 0).UTC().Format("2006-01-02 15:04:05")
			fmt.Fprintf(stdout, "  v%-4d  %s  height=%-8d  %s  [%s]\n",
				v.Version, txID, v.BlockHeight, t, v.Access)
		}
		return 0
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "bstat: cannot determine home directory: %v\n", err)
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
		return buy.HandleError(err, "bstat", stderr)
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

	if meta.Timestamp > 0 {
		t := time.Unix(meta.Timestamp, 0).UTC()
		fmt.Fprintf(w, "    Time: %s\n", t.Format("2006-01-02 15:04:05 UTC"))
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
	_, _ = fmt.Fprintln(stdout, string(data))
	return 0
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

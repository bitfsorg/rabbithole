// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command btree shows a directory tree from a BitFS filesystem, like Unix tree.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
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
	fs := flag.NewFlagSet("btree", flag.ContinueOnError)
	fs.SetOutput(stderr)

	jsonOut := fs.Bool("json", false, "JSON output")
	depth := fs.Int("d", 0, "max depth (0 = unlimited)")
	fs.IntVar(depth, "depth", 0, "max depth (0 = unlimited)")
	host := fs.String("host", "", "daemon URL override")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")
	noCache := fs.Bool("no-cache", false, "skip metadata cache")
	offline := fs.Bool("offline", false, "cache-only mode")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, `Usage: btree [--json] [-d|--depth N] [--host URL] [--timeout DURATION] <bitfs-uri>

Examples:
  btree bitfs://example.com/                  (domain)
  btree bitfs://alice@example.com/            (paymail)
  btree bitfs://02abc...66chars.../           (pubkey, requires --host)
`)
		return 6
	}

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "btree: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "btree: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "btree: cannot determine home directory: %v\n", err)
		return 1
	}
	cacheDir := filepath.Join(homeDir, ".bitfs", "cache", "meta")
	cache := client.NewMetaCache(cacheDir, 5*time.Minute)
	cc := client.NewCachedClient(c, cache)
	cc.NoCache = *noCache
	cc.Offline = *offline
	cc.Prefix = c.BaseURL

	pnode := resolved.PNode
	uriPath := resolved.Path

	meta, err := cc.GetMeta(pnode, uriPath)
	if err != nil {
		return buy.HandleError(err, "btree", stderr)
	}

	// If root target is a file (not a directory), print single file info.
	if meta.Type != "dir" {
		if *jsonOut {
			node := treeNode{
				Name:   path.Base(meta.Path),
				Type:   meta.Type,
				Access: meta.Access,
				Size:   meta.FileSize,
			}
			if meta.PricePerKB > 0 {
				node.PricePerKB = meta.PricePerKB
			}
			return outputJSON(node, stdout, stderr)
		}
		name := meta.Path
		if name == "" || name == "/" {
			name = meta.PNode
		}
		fmt.Fprintf(stdout, "%s (%s)\n", name, formatFileAnnotation(meta.Access, meta.FileSize, meta.PricePerKB))
		fmt.Fprintf(stdout, "\n0 directories, 1 file\n")
		return 0
	}

	// Build full tree recursively.
	var dirs, files int
	root := buildTree(cc, pnode, meta, *depth, 1, &dirs, &files)

	if *jsonOut {
		return outputJSON(root, stdout, stderr)
	}

	// Print tree-style output.
	_, _ = fmt.Fprintln(stdout, root.Name)
	printTree(stdout, root.Children, "")
	dirWord := "directories"
	if dirs == 1 {
		dirWord = "directory"
	}
	fileWord := "files"
	if files == 1 {
		fileWord = "file"
	}
	fmt.Fprintf(stdout, "\n%d %s, %d %s\n", dirs, dirWord, files, fileWord)
	return 0
}

// treeNode represents a node in the tree structure used for both
// text and JSON output.
type treeNode struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Access     string     `json:"access,omitempty"`
	Size       uint64     `json:"size,omitempty"`
	PricePerKB uint64     `json:"price_per_kb,omitempty"`
	Children   []treeNode `json:"children,omitempty"`
}

// buildTree recursively builds a treeNode from a MetaResponse that is a directory.
func buildTree(c client.MetaGetter, pnode string, meta *client.MetaResponse, maxDepth, currentDepth int, dirs, files *int) treeNode {
	name := path.Base(meta.Path)
	if meta.Path == "/" || meta.Path == "" {
		name = "/"
	}

	root := treeNode{
		Name: name,
		Type: "dir",
	}

	for _, child := range meta.Children {
		if child.Type == "dir" {
			*dirs++

			// If depth-limited, show directory without recursing.
			if maxDepth > 0 && currentDepth >= maxDepth {
				root.Children = append(root.Children, treeNode{
					Name: child.Name,
					Type: "dir",
				})
				continue
			}

			// Recurse into child directory.
			childPath := path.Join(meta.Path, child.Name)
			if !strings.HasPrefix(childPath, "/") {
				childPath = "/" + childPath
			}
			childMeta, err := c.GetMeta(pnode, childPath)
			if err != nil {
				// On error, show directory without children.
				root.Children = append(root.Children, treeNode{
					Name: child.Name,
					Type: "dir",
				})
				continue
			}
			subtree := buildTree(c, pnode, childMeta, maxDepth, currentDepth+1, dirs, files)
			subtree.Name = child.Name
			root.Children = append(root.Children, subtree)
		} else {
			*files++
			// For files in a directory listing, we only have name and type
			// from ChildEntry. To get full metadata, we'd need another GetMeta call.
			// For efficiency, we show just the name for files discovered via Children.
			root.Children = append(root.Children, treeNode{
				Name: child.Name,
				Type: child.Type,
			})
		}
	}

	return root
}

// printTree prints the tree lines with box-drawing characters.
func printTree(w io.Writer, children []treeNode, prefix string) {
	for i, child := range children {
		isLast := i == len(children)-1

		connector := "\u251c\u2500\u2500 " // "├── "
		if isLast {
			connector = "\u2514\u2500\u2500 " // "└── "
		}

		if child.Type == "dir" {
			fmt.Fprintf(w, "%s%s%s/\n", prefix, connector, child.Name)
		} else {
			annotation := ""
			if child.Access != "" {
				annotation = " (" + formatFileAnnotation(child.Access, child.Size, child.PricePerKB) + ")"
			}
			fmt.Fprintf(w, "%s%s%s%s\n", prefix, connector, child.Name, annotation)
		}

		// Recurse into directory children.
		if child.Type == "dir" && len(child.Children) > 0 {
			childPrefix := prefix + "\u2502   " // "│   "
			if isLast {
				childPrefix = prefix + "    "
			}
			printTree(w, child.Children, childPrefix)
		}
	}
}

// formatFileAnnotation returns a parenthetical annotation for a file,
// e.g. "free, 1.2K" or "paid, 100 sat/KB".
func formatFileAnnotation(access string, size uint64, pricePerKB uint64) string {
	if access == "paid" && pricePerKB > 0 {
		return fmt.Sprintf("%s, %d sat/KB", access, pricePerKB)
	}
	if size > 0 {
		return fmt.Sprintf("%s, %s", access, formatSize(size))
	}
	return access
}

// outputJSON marshals the tree node as indented JSON.
func outputJSON(node treeNode, stdout, stderr io.Writer) int {
	data, err := json.MarshalIndent(node, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "btree: json marshal: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, string(data))
	return 0
}

// formatSize returns a human-readable file size string (compact style).
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

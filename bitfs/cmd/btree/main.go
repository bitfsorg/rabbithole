// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command btree shows a directory tree from a BitFS filesystem, like Unix tree.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tongxiaofeng/bitfs/internal/paymail"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("btree", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
	depth := fs.Int("depth", 0, "max depth (0 = unlimited)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: btree [--json] [--depth N] <bitfs-uri>\n")
		return 2
	}

	uri := fs.Arg(0)
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	if *jsonOut {
		fmt.Printf(`{"command":"btree","uri":%q,"type":%q,"path":%q,"depth":%d,"status":"stub"}`, uri, parsed.Type.String(), parsed.Path, *depth)
		fmt.Println()
	} else {
		fmt.Printf("Would show tree for %s\n", uri)
		fmt.Printf("  Address type: %s\n", parsed.Type.String())
		fmt.Printf("  Path:         %s\n", parsed.Path)
		if *depth > 0 {
			fmt.Printf("  Max depth:    %d\n", *depth)
		}
	}

	return 0
}

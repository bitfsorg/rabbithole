// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bget downloads a file from a BitFS filesystem, like Unix wget.
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
	fs := flag.NewFlagSet("bget", flag.ContinueOnError)
	output := fs.String("output", "", "output filename")
	buy := fs.Bool("buy", false, "auto-purchase paid content")
	jsonOut := fs.Bool("json", false, "JSON progress output")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bget [--output FILE] [--buy] [--json] <bitfs-uri>\n")
		return 2
	}

	uri := fs.Arg(0)
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	if *jsonOut {
		fmt.Printf(`{"command":"bget","uri":%q,"type":%q,"path":%q,"buy":%t,"status":"stub"}`, uri, parsed.Type.String(), parsed.Path, *buy)
		fmt.Println()
	} else {
		fmt.Printf("Would download %s\n", uri)
		fmt.Printf("  Address type: %s\n", parsed.Type.String())
		fmt.Printf("  Path:         %s\n", parsed.Path)
		if *output != "" {
			fmt.Printf("  Output:       %s\n", *output)
		}
		if *buy {
			fmt.Printf("  Auto-buy:     yes\n")
		}
	}

	return 0
}

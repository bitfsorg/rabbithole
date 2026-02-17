// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bls lists directory contents in a BitFS filesystem, like Unix ls.
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
	fs := flag.NewFlagSet("bls", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
	long := fs.Bool("long", false, "detailed listing")
	_ = fs.Bool("l", false, "detailed listing (alias)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bls [--json] [--long] <bitfs-uri>\n")
		return 2
	}

	uri := fs.Arg(0)
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	if *jsonOut {
		fmt.Printf(`{"command":"bls","uri":%q,"type":%q,"path":%q,"status":"stub"}`, uri, parsed.Type.String(), parsed.Path)
		fmt.Println()
	} else {
		fmt.Printf("Would list directory at %s\n", uri)
		if *long {
			fmt.Printf("  Mode: detailed listing\n")
		}
		fmt.Printf("  Address type: %s\n", parsed.Type.String())
		fmt.Printf("  Path:         %s\n", parsed.Path)
	}

	return 0
}

// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/bitfsorg/metanet/internal/config"
)

// cmdPeers handles the "metanet peers" command.
// Stub: outputs an empty peer list.
func cmdPeers(args []string) int {
	fs := flag.NewFlagSet("peers", flag.ContinueOnError)
	_ = fs.String("datadir", config.DefaultDataDir(), "data directory")
	jsonOut := fs.Bool("json", false, "output JSON format")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode([]interface{}{}); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitError
		}
	} else {
		fmt.Printf("No connected peers.\n")
	}

	return exitSuccess
}

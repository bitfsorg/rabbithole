// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"

	"github.com/bitfsorg/metanet/internal/config"
)

// cmdMine handles the "metanet mine" command.
// Stub: prints a not-yet-implemented message.
func cmdMine(args []string) int {
	fs := flag.NewFlagSet("mine", flag.ContinueOnError)
	_ = fs.String("datadir", config.DefaultDataDir(), "data directory")
	_ = fs.Int("threads", 1, "number of mining threads")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	fmt.Printf("Mining not yet implemented.\n")
	return exitSuccess
}

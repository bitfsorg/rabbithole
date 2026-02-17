// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
)

// runShell handles the "bitfs shell" command.
// Stub: prints a message indicating shell mode is not yet implemented.
func runShell(args []string) int {
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	_ = fs.String("vault", "", "vault name")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	fmt.Printf("Shell mode not yet implemented.\n")
	fmt.Printf("This will provide an FTP-style interactive REPL for BitFS.\n")

	return exitSuccess
}

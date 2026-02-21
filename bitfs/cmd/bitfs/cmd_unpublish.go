// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
)

// runUnpublish handles the "bitfs unpublish" command.
// Prints instructions to remove the DNS TXT record for a domain.
func runUnpublish(args []string) int {
	fs := flag.NewFlagSet("unpublish", flag.ContinueOnError)

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs unpublish <domain>\n")
		return exitUsageError
	}

	domain := fs.Arg(0)

	fmt.Printf(`To unpublish from %s, remove the following DNS TXT record:

  _bitfs.%s  TXT  (delete this record)

After DNS propagation, the domain will no longer resolve to your BitFS vault.
`, domain, domain)

	return exitSuccess
}

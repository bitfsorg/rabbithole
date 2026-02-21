// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// promptPassword reads a password from the terminal without echo.
// Returns the password string. The caller is responsible for zeroing
// the password after use via zeroString.
func promptPassword(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("password prompt requires interactive terminal; use --password flag")
	}
	fmt.Fprint(os.Stderr, prompt)
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr) // newline after hidden input
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	if len(pass) == 0 {
		return "", fmt.Errorf("password cannot be empty")
	}
	return string(pass), nil
}

// promptPasswordConfirm reads a password twice and confirms they match.
// Used for wallet init where the user must confirm their password.
func promptPasswordConfirm() (string, error) {
	pass1, err := promptPassword("Enter wallet password: ")
	if err != nil {
		return "", err
	}

	pass2, err := promptPassword("Confirm wallet password: ")
	if err != nil {
		zeroString(&pass1)
		return "", err
	}

	if pass1 != pass2 {
		zeroString(&pass1)
		zeroString(&pass2)
		return "", fmt.Errorf("passwords do not match")
	}

	zeroString(&pass2)
	return pass1, nil
}

// zeroString attempts to overwrite the string's bytes with zeros.
// Go strings are immutable, so []byte(*s) creates a copy; this provides
// defense in depth but cannot guarantee the original data is cleared.
// For stronger guarantees, avoid string conversion and use []byte directly.
func zeroString(s *string) {
	b := []byte(*s)
	for i := range b {
		b[i] = 0
	}
	*s = ""
}

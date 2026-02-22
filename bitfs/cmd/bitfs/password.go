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

// resolvePassword returns the password flag value if non-empty,
// otherwise prompts the user interactively.
func resolvePassword(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	return promptPassword("Enter wallet password: ")
}

// promptNetwork displays a numbered menu and reads the user's choice.
func promptNetwork() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("network prompt requires interactive terminal; use --network flag")
	}
	networks := []struct {
		name string
		desc string
	}{
		{"mainnet", "BSV mainnet (production)"},
		{"testnet", "BSV testnet (testing)"},
		{"teratestnet", "BSV Teranode testnet (scaling)"},
		{"regtest", "Local regtest (development)"},
	}

	fmt.Fprintln(os.Stderr, "Select network:")
	for i, n := range networks {
		fmt.Fprintf(os.Stderr, "  %d) %s  — %s\n", i+1, n.name, n.desc)
	}
	fmt.Fprint(os.Stderr, "Enter choice [1]: ")

	var buf [8]byte
	n, err := os.Stdin.Read(buf[:])
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	input := string(buf[:n])
	// Trim newline/spaces.
	for len(input) > 0 && (input[len(input)-1] == '\n' || input[len(input)-1] == '\r' || input[len(input)-1] == ' ') {
		input = input[:len(input)-1]
	}
	if input == "" {
		return networks[0].name, nil // default
	}

	idx := 0
	switch input {
	case "1":
		idx = 0
	case "2":
		idx = 1
	case "3":
		idx = 2
	case "4":
		idx = 3
	default:
		return "", fmt.Errorf("invalid choice %q; enter 1–4", input)
	}
	return networks[idx].name, nil
}

// promptYesNo prints a y/N prompt and returns true if the user enters "y" or "Y".
// Non-interactive terminals default to "no".
func promptYesNo(question string) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", question)
	var buf [8]byte
	n, err := os.Stdin.Read(buf[:])
	if err != nil || n == 0 {
		return false
	}
	input := buf[0]
	return input == 'y' || input == 'Y'
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

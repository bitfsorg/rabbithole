// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bcat outputs file content from a BitFS filesystem, like Unix cat.
package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/libbitfs/method42"
	"github.com/tongxiaofeng/libbitfs/paymail"
	"github.com/tongxiaofeng/libbitfs/x402"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bcat", flag.ContinueOnError)
	fs.SetOutput(stderr)

	buy := fs.Bool("buy", false, "attempt to purchase paid content")
	walletKey := fs.String("wallet-key", "", "hex-encoded buyer private key (32 or 33 bytes)")
	host := fs.String("host", "http://localhost:8080", "daemon URL")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "Usage: bcat [--buy] [--host URL] [--timeout DURATION] <bitfs-uri>\n")
		return 6
	}

	uri := fs.Arg(0)
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: %v\n", err)
		return 6
	}

	// Resolve pnode from parsed URI.
	var pnode string
	switch parsed.Type {
	case paymail.AddressPubKey:
		pnode = hex.EncodeToString(parsed.PubKey)
	case paymail.AddressPaymail, paymail.AddressDNSLink:
		fmt.Fprintf(stderr, "bcat: paymail/dnslink resolution not yet supported\n")
		return 6
	default:
		fmt.Fprintf(stderr, "bcat: unknown address type\n")
		return 6
	}

	// Build client.
	c := client.New(*host)
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bcat: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	// Determine the path to query. Default to root "/" if none specified.
	path := parsed.Path
	if path == "" {
		path = "/"
	}

	meta, err := c.GetMeta(pnode, path)
	if err != nil {
		return handleError(err, stderr)
	}

	// Directories cannot be cat'd.
	if meta.Type == "dir" {
		fmt.Fprintf(stderr, "bcat: %s: is a directory\n", path)
		return 6
	}

	// Handle access modes.
	switch meta.Access {
	case "free":
		return outputContent(c, meta, stdout, stderr)
	case "paid":
		return handlePaid(c, meta, *buy, *walletKey, stdout, stderr)
	case "private":
		fmt.Fprintf(stderr, "bcat: private content cannot be accessed remotely\n")
		return 6
	default:
		fmt.Fprintf(stderr, "bcat: unknown access mode %q\n", meta.Access)
		return 1
	}
}

// outputContent fetches the raw data by key_hash and writes it to stdout.
func outputContent(c *client.Client, meta *client.MetaResponse, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		fmt.Fprintf(stderr, "bcat: no content hash available\n")
		return 1
	}

	// TODO: Decrypt content using Method 42 (D_node=1 for free, session key for paid)
	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleError(err, stderr)
	}
	defer reader.Close()

	if _, err := io.Copy(stdout, reader); err != nil {
		fmt.Fprintf(stderr, "bcat: write error: %v\n", err)
		return 1
	}
	return 0
}

// handlePaid handles paid content access (with or without --buy).
func handlePaid(c *client.Client, meta *client.MetaResponse, buy bool, walletKey string, stdout, stderr io.Writer) int {
	if !buy {
		fmt.Fprintf(stderr, "bcat: content requires payment: %d sat/KB (%d bytes)\nUse --buy to purchase\n",
			meta.PricePerKB, meta.FileSize)
		return 5
	}

	// Validate wallet key is provided.
	if walletKey == "" {
		fmt.Fprintf(stderr, "bcat: --wallet-key is required for purchases\n")
		return 6
	}

	// Parse the hex-encoded private key.
	keyBytes, err := hex.DecodeString(walletKey)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid wallet key hex: %v\n", err)
		return 6
	}

	// Accept 32-byte raw scalar or 33-byte compressed key (strip prefix).
	switch len(keyBytes) {
	case 32:
		// raw scalar, use as-is
	case 33:
		// compressed pubkey format: strip the 02/03 prefix
		keyBytes = keyBytes[1:]
	default:
		fmt.Fprintf(stderr, "bcat: wallet key must be 32 or 33 bytes, got %d\n", len(keyBytes))
		return 6
	}

	privKey, _ := ec.PrivateKeyFromBytes(keyBytes)
	if privKey == nil {
		fmt.Fprintf(stderr, "bcat: failed to parse wallet key\n")
		return 6
	}

	// Validate that meta has a TxID for the purchase invoice.
	if meta.TxID == "" {
		fmt.Fprintf(stderr, "bcat: paid content has no invoice txid\n")
		return 5
	}

	// Step 1: Get buy info (capsule_hash, price, payment_addr).
	buyInfo, err := c.GetBuyInfo(meta.TxID)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: get buy info: %v\n", err)
		return handleError(err, stderr)
	}

	// Decode capsule hash from hex.
	capsuleHash, err := hex.DecodeString(buyInfo.CapsuleHash)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid capsule hash hex: %v\n", err)
		return 5
	}

	// Decode payment address (hex-encoded 20-byte pubkey hash).
	sellerAddr, err := hex.DecodeString(buyInfo.PaymentAddr)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid payment address hex: %v\n", err)
		return 5
	}

	// Step 2: Build HTLC transaction.
	htlcRaw, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey: privKey.PubKey().Compressed(),
		SellerAddr:  sellerAddr,
		CapsuleHash: capsuleHash,
		Amount:      buyInfo.Price,
		Timeout:     x402.DefaultHTLCTimeout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "bcat: build HTLC: %v\n", err)
		return 5
	}

	// Step 3: Submit HTLC to get the capsule.
	capsuleResp, err := c.SubmitHTLC(meta.TxID, htlcRaw)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: submit HTLC: %v\n", err)
		return handleError(err, stderr)
	}

	// Decode capsule from hex.
	capsule, err := hex.DecodeString(capsuleResp.Capsule)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid capsule hex: %v\n", err)
		return 5
	}

	// Step 4: Fetch encrypted content.
	if meta.KeyHash == "" {
		fmt.Fprintf(stderr, "bcat: no content hash available\n")
		return 1
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleError(err, stderr)
	}
	defer reader.Close()

	ciphertext, err := io.ReadAll(reader)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: read content: %v\n", err)
		return 4
	}

	// Decode keyHash from hex.
	keyHashBytes, err := hex.DecodeString(meta.KeyHash)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid key hash hex: %v\n", err)
		return 5
	}

	// Step 5: Decrypt with capsule.
	result, err := method42.DecryptWithCapsule(ciphertext, capsule, keyHashBytes)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: decrypt: %v\n", err)
		return 5
	}

	// Step 6: Output decrypted content to stdout.
	if _, err := stdout.Write(result.Plaintext); err != nil {
		fmt.Fprintf(stderr, "bcat: write error: %v\n", err)
		return 1
	}

	return 0
}

// handleError maps client errors to exit codes and prints a message.
func handleError(err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "bcat: not found\n")
		return 2
	case errors.Is(err, client.ErrTimeout):
		fmt.Fprintf(stderr, "bcat: request timeout\n")
		return 4
	case errors.Is(err, client.ErrNetwork):
		fmt.Fprintf(stderr, "bcat: network error: %v\n", err)
		return 4
	case errors.Is(err, client.ErrServer):
		fmt.Fprintf(stderr, "bcat: server error: %v\n", err)
		return 4
	default:
		fmt.Fprintf(stderr, "bcat: %v\n", err)
		return 1
	}
}

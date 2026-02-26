// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bcat outputs file content from a BitFS filesystem, like Unix cat.
package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/tongxiaofeng/bitfs/internal/buyer"
	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/libbitfs-go/method42"
	"github.com/tongxiaofeng/libbitfs-go/x402"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bcat", flag.ContinueOnError)
	fs.SetOutput(stderr)

	buy := fs.Bool("buy", false, "attempt to purchase paid content")
	verify := fs.Bool("verify", false, "SPV-verify the Metanet tx before outputting")
	walletKey := fs.String("wallet-key", "", "hex-encoded buyer private key (32 or 33 bytes)")
	jsonOut := fs.Bool("json", false, "JSON output")
	host := fs.String("host", "", "daemon URL override")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, `Usage: bcat [--buy] [--host URL] [--timeout DURATION] <bitfs-uri>

Examples:
  bcat bitfs://example.com/docs/readme.txt          (domain)
  bcat bitfs://alice@example.com/docs/readme.txt    (paymail)
  bcat bitfs://02abc...66chars.../docs/readme.txt   (pubkey, requires --host)
`)
		return 6
	}

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bcat: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	meta, err := c.GetMeta(resolved.PNode, resolved.Path)
	if err != nil {
		return handleError(err, stderr)
	}

	// Directories cannot be cat'd.
	if meta.Type == "dir" {
		fmt.Fprintf(stderr, "bcat: %s: is a directory\n", resolved.Path)
		return 6
	}

	// SPV verification if requested.
	if *verify && meta.TxID != "" {
		proof, err := c.VerifySPV(meta.TxID)
		if err != nil {
			fmt.Fprintf(stderr, "bcat: SPV verification failed: %v\n", err)
			return 4
		}
		if !proof.Confirmed {
			fmt.Fprintf(stderr, "bcat: warning: tx %s is unconfirmed\n", meta.TxID)
		} else {
			fmt.Fprintf(stderr, "bcat: verified tx %s at block %d\n", meta.TxID, proof.BlockHeight)
		}
	}

	// Handle access modes.
	switch meta.Access {
	case "free":
		if *jsonOut {
			return outputContentJSON(c, meta, stdout, stderr)
		}
		return outputContent(c, meta, stdout, stderr)
	case "paid":
		return handlePaid(c, meta, *buy, *walletKey, *jsonOut, stdout, stderr)
	case "private":
		if *jsonOut {
			return handleErrorJSON(fmt.Errorf("private content"), stdout)
		}
		fmt.Fprintf(stderr, "bcat: private content cannot be accessed remotely\n")
		return 6
	default:
		if *jsonOut {
			return handleErrorJSON(fmt.Errorf("unknown access mode %q", meta.Access), stdout)
		}
		fmt.Fprintf(stderr, "bcat: unknown access mode %q\n", meta.Access)
		return 1
	}
}

// outputContent fetches encrypted data by key_hash, decrypts it using Method 42
// (free mode: D_node = scalar 1), and writes plaintext to stdout.
func outputContent(c *client.Client, meta *client.MetaResponse, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		fmt.Fprintf(stderr, "bcat: no content hash available\n")
		return 1
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleError(err, stderr)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(reader)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: read error: %v\n", err)
		return 4
	}

	// Empty content — nothing to decrypt.
	if len(ciphertext) == 0 {
		return 0
	}

	// Decode the node's public key for Method 42 free-mode decryption.
	pubKeyBytes, err := hex.DecodeString(meta.PNode)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid pnode hex: %v\n", err)
		return 1
	}
	pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid pnode key: %v\n", err)
		return 1
	}

	keyHashBytes, err := hex.DecodeString(meta.KeyHash)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid key hash hex: %v\n", err)
		return 1
	}

	// Decrypt: nil private key triggers FreePrivateKey() (scalar 1).
	result, err := method42.Decrypt(ciphertext, nil, pubKey, keyHashBytes, method42.AccessFree)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: decrypt: %v\n", err)
		return 5
	}

	if _, err := stdout.Write(result.Plaintext); err != nil {
		fmt.Fprintf(stderr, "bcat: write error: %v\n", err)
		return 1
	}
	return 0
}

// handlePaid handles paid content access (with or without --buy).
func handlePaid(c *client.Client, meta *client.MetaResponse, buy bool, walletKey string, jsonOut bool, stdout, stderr io.Writer) int {
	if !buy {
		if jsonOut {
			return outputPaymentRequiredJSON(meta, stdout, stderr)
		}
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
	// Pass buyer's pubkey so the server computes the buyer-specific capsule.
	buyerPubHex := hex.EncodeToString(privKey.PubKey().Compressed())
	buyInfo, err := c.GetBuyInfo(meta.TxID, buyerPubHex)
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

	// Decode seller pubkey (hex-encoded 33-byte compressed public key).
	sellerPubKey, err := hex.DecodeString(buyInfo.SellerPubKey)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid seller pubkey hex: %v\n", err)
		return 5
	}

	// Step 2: Build HTLC transaction.
	htlcRaw, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  privKey.PubKey().Compressed(),
		SellerPubKey: sellerPubKey,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       buyInfo.Price,
		Timeout:      x402.DefaultHTLCTimeout,
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
	defer func() { _ = reader.Close() }()

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

	// Decode the node's public key for capsule decryption.
	nodePubBytes, err := hex.DecodeString(meta.PNode)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid pnode hex: %v\n", err)
		return 5
	}
	nodePub, err := ec.PublicKeyFromBytes(nodePubBytes)
	if err != nil {
		fmt.Fprintf(stderr, "bcat: invalid pnode key: %v\n", err)
		return 5
	}

	// Step 5: Decrypt with capsule.
	result, err := method42.DecryptWithCapsule(ciphertext, capsule, keyHashBytes, privKey, nodePub)
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

// outputContentJSON fetches, decrypts, and outputs content as JSON.
func outputContentJSON(c *client.Client, meta *client.MetaResponse, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		return handleErrorJSON(fmt.Errorf("no content hash available"), stdout)
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleErrorJSON(err, stdout)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(reader)
	if err != nil {
		return handleErrorJSON(fmt.Errorf("read error: %w", err), stdout)
	}

	var plaintext []byte
	if len(ciphertext) > 0 {
		pubKeyBytes, err := hex.DecodeString(meta.PNode)
		if err != nil {
			return handleErrorJSON(fmt.Errorf("invalid pnode hex: %w", err), stdout)
		}
		pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
		if err != nil {
			return handleErrorJSON(fmt.Errorf("invalid pnode key: %w", err), stdout)
		}

		keyHashBytes, err := hex.DecodeString(meta.KeyHash)
		if err != nil {
			return handleErrorJSON(fmt.Errorf("invalid key hash hex: %w", err), stdout)
		}

		result, err := method42.Decrypt(ciphertext, nil, pubKey, keyHashBytes, method42.AccessFree)
		if err != nil {
			return handleErrorJSON(fmt.Errorf("decrypt: %w", err), stdout)
		}
		plaintext = result.Plaintext
	}

	resp := &buyer.CatResponse{Meta: meta}
	if strings.HasPrefix(meta.MimeType, "text/") || meta.MimeType == "application/json" {
		s := string(plaintext)
		resp.Content = &s
	} else {
		s := base64.StdEncoding.EncodeToString(plaintext)
		resp.ContentBase64 = &s
	}
	return writeJSON(resp, stdout, stderr)
}

// outputPaymentRequiredJSON outputs payment-required info as JSON.
func outputPaymentRequiredJSON(meta *client.MetaResponse, stdout, stderr io.Writer) int {
	resp := &buyer.CatResponse{
		Meta:            meta,
		PaymentRequired: true,
		PaymentInfo: &buyer.PaymentInfo{
			Price:      meta.PricePerKB * (meta.FileSize/1024 + 1),
			PricePerKB: meta.PricePerKB,
		},
	}
	return writeJSON(resp, stdout, stderr)
}

// handleErrorJSON outputs an error as JSON and returns the exit code.
func handleErrorJSON(err error, stdout io.Writer) int {
	code := errorToCode(err)
	resp := &buyer.ErrorResponse{Error: errorMessage(err), Code: code}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(stdout, string(data))
	return code
}

func writeJSON(v interface{}, stdout, stderr io.Writer) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "bcat: json marshal: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

func errorToCode(err error) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		return 2
	case errors.Is(err, client.ErrTimeout), errors.Is(err, client.ErrNetwork):
		return 4
	case errors.Is(err, client.ErrPaymentRequired):
		return 5
	default:
		return 1
	}
}

func errorMessage(err error) string {
	switch {
	case errors.Is(err, client.ErrNotFound):
		return "not found"
	case errors.Is(err, client.ErrTimeout):
		return "request timeout"
	case errors.Is(err, client.ErrNetwork):
		return "network error"
	default:
		return err.Error()
	}
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

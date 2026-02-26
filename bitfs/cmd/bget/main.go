// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bget downloads a file from a BitFS filesystem, like Unix wget.
package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/tongxiaofeng/bitfs/internal/buyer"
	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/libbitfs-go/method42"
)

// maxContentSize is the maximum encrypted content size bget will read (1 GB).
const maxContentSize = 1 << 30

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bget", flag.ContinueOnError)
	fs.SetOutput(stderr)

	output := fs.String("o", "", "output filename")
	fs.StringVar(output, "output", "", "output filename")
	buy := fs.Bool("buy", false, "attempt to purchase paid content")
	verify := fs.Bool("verify", false, "SPV-verify the Metanet tx before downloading")
	walletKey := fs.String("wallet-key", "", "hex-encoded buyer private key (32 or 33 bytes)")
	utxoStr := fs.String("utxo", "", "buyer UTXO for purchase (txid:vout:amount)")
	version := fs.Bool("version", false, "show version-specific content")
	jsonOut := fs.Bool("json", false, "JSON output")
	host := fs.String("host", "", "daemon URL override")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if *version {
		fmt.Fprintf(stdout, "bget: --version not yet supported\n")
		return 0
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, `Usage: bget [-o FILE] [--buy] [--host URL] [--timeout DURATION] <bitfs-uri>

Examples:
  bget bitfs://example.com/docs/report.pdf            (domain)
  bget bitfs://alice@example.com/docs/report.pdf      (paymail)
  bget bitfs://02abc...66chars.../docs/report.pdf     (pubkey, requires --host)
`)
		return 6
	}

	uri := fs.Arg(0)
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "bget: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	uriPath := resolved.Path

	meta, err := c.GetMeta(resolved.PNode, uriPath)
	if err != nil {
		if *jsonOut {
			return handleErrorJSON(err, stdout)
		}
		return handleError(err, stderr)
	}

	// Directories cannot be downloaded.
	if meta.Type == "dir" {
		if *jsonOut {
			return handleErrorJSON(fmt.Errorf("is a directory"), stdout)
		}
		fmt.Fprintf(stderr, "bget: %s: is a directory\n", uriPath)
		return 6
	}

	// SPV verification if requested.
	if *verify && meta.TxID != "" {
		proof, err := c.VerifySPV(meta.TxID)
		if err != nil {
			fmt.Fprintf(stderr, "bget: SPV verification failed: %v\n", err)
			return 4
		}
		if !proof.Confirmed {
			fmt.Fprintf(stderr, "bget: warning: tx %s is unconfirmed\n", meta.TxID)
		} else {
			fmt.Fprintf(stderr, "bget: verified tx %s at block %d\n", meta.TxID, proof.BlockHeight)
		}
	}

	// Handle access modes.
	switch meta.Access {
	case "free":
		if *jsonOut {
			return downloadContentJSON(c, meta, *output, stdout, stderr)
		}
		return downloadContent(c, meta, *output, stdout, stderr)
	case "paid":
		return handlePaid(c, meta, *buy, *walletKey, *utxoStr, *output, *jsonOut, stdout, stderr)
	case "private":
		if *jsonOut {
			return handleErrorJSON(fmt.Errorf("private content"), stdout)
		}
		fmt.Fprintf(stderr, "bget: private content cannot be accessed remotely\n")
		return 6
	default:
		if *jsonOut {
			return handleErrorJSON(fmt.Errorf("unknown access mode %q", meta.Access), stdout)
		}
		fmt.Fprintf(stderr, "bget: unknown access mode %q\n", meta.Access)
		return 1
	}
}

// downloadContent fetches encrypted data by key_hash, decrypts it using Method 42
// (free mode: D_node = scalar 1), and writes plaintext to a file.
func downloadContent(c *client.Client, meta *client.MetaResponse, outputName string, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		fmt.Fprintf(stderr, "bget: no content hash available\n")
		return 1
	}

	filename := outputName
	if filename == "" {
		filename = deriveFilename(meta.Path)
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleError(err, stderr)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
	if err != nil {
		fmt.Fprintf(stderr, "bget: read error: %v\n", err)
		return 4
	}

	// Decrypt using Method 42 free mode.
	var plaintext []byte
	if len(ciphertext) > 0 {
		pubKeyBytes, err := hex.DecodeString(meta.PNode)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid pnode hex: %v\n", err)
			return 1
		}
		pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid pnode key: %v\n", err)
			return 1
		}

		keyHashBytes, err := hex.DecodeString(meta.KeyHash)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid key hash hex: %v\n", err)
			return 1
		}

		result, err := method42.Decrypt(ciphertext, nil, pubKey, keyHashBytes, method42.AccessFree)
		if err != nil {
			fmt.Fprintf(stderr, "bget: decrypt: %v\n", err)
			return 5
		}
		plaintext = result.Plaintext
	}

	file, err := os.Create(filename)
	if err != nil {
		fmt.Fprintf(stderr, "bget: cannot create file %q: %v\n", filename, err)
		return 1
	}

	n, err := file.Write(plaintext)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(filename)
		fmt.Fprintf(stderr, "bget: write error: %v\n", err)
		return 1
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(filename)
		fmt.Fprintf(stderr, "bget: close error: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Downloaded %d bytes to %s\n", n, filename)
	return 0
}

// deriveFilename extracts a reasonable filename from the URI path.
// If the path is "/" or empty, falls back to "download.dat".
func deriveFilename(uriPath string) string {
	base := path.Base(uriPath)
	if base == "" || base == "/" || base == "." {
		return "download.dat"
	}
	return base
}

// handlePaid handles paid content access (with or without --buy).
func handlePaid(c *client.Client, meta *client.MetaResponse, buy bool, walletKey, utxoFlag, outputName string, jsonOut bool, stdout, stderr io.Writer) int {
	if !buy {
		if jsonOut {
			return outputPaymentRequiredJSON(meta, stdout, stderr)
		}
		fmt.Fprintf(stderr, "bget: content requires payment: %d sat/KB (%d bytes)\nUse --buy to purchase\n",
			meta.PricePerKB, meta.FileSize)
		return 5
	}

	cfg, err := buyer.LoadConfig(buyer.LoadConfigOpts{WalletKeyFlag: walletKey, UTXOFlag: utxoFlag})
	if err != nil {
		if jsonOut {
			return handleErrorJSON(err, stdout)
		}
		fmt.Fprintf(stderr, "bget: %v\n", err)
		return 6
	}

	result, err := buyer.Buy(&buyer.BuyParams{
		Client: c,
		TxID:   meta.TxID,
		Config: cfg,
	})
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("purchase failed: %w", err), stdout)
		}
		// Map client errors (e.g. server error) to appropriate exit codes.
		if errors.Is(err, client.ErrServer) || errors.Is(err, client.ErrNetwork) || errors.Is(err, client.ErrTimeout) {
			return handleError(err, stderr)
		}
		fmt.Fprintf(stderr, "bget: purchase failed: %v\n", err)
		return 5
	}

	return downloadPaidContent(c, meta, result, cfg.PrivKey, outputName, jsonOut, stdout, stderr)
}

// downloadPaidContent fetches encrypted data, decrypts it using the capsule
// obtained from the purchase, and writes the plaintext to a file.
func downloadPaidContent(c *client.Client, meta *client.MetaResponse, buyResult *buyer.BuyResult, privKey *ec.PrivateKey, outputName string, jsonOut bool, stdout, stderr io.Writer) int {
	// Fetch encrypted content.
	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(err, stdout)
		}
		return handleError(err, stderr)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("read error: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bget: read: %v\n", err)
		return 4
	}

	// Decrypt with capsule.
	keyHashBytes, err := hex.DecodeString(meta.KeyHash)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("invalid key hash hex: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bget: invalid key hash hex: %v\n", err)
		return 5
	}

	nodePubBytes, err := hex.DecodeString(meta.PNode)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("invalid pnode hex: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bget: invalid pnode hex: %v\n", err)
		return 5
	}
	nodePub, err := ec.PublicKeyFromBytes(nodePubBytes)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("invalid pnode key: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bget: invalid pnode key: %v\n", err)
		return 5
	}

	decResult, err := method42.DecryptWithCapsule(ciphertext, buyResult.Capsule, keyHashBytes, privKey, nodePub)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("decrypt: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bget: decrypt: %v\n", err)
		return 5
	}

	// Write to file.
	filename := outputName
	if filename == "" {
		filename = deriveFilename(meta.Path)
	}

	file, err := os.Create(filename)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(err, stdout)
		}
		fmt.Fprintf(stderr, "bget: cannot create file %q: %v\n", filename, err)
		return 1
	}

	n, err := file.Write(decResult.Plaintext)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(filename)
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("write error: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bget: write error: %v\n", err)
		return 1
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(filename)
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("close error: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bget: close error: %v\n", err)
		return 1
	}

	if jsonOut {
		resp := &buyer.GetResponse{
			Meta:         meta,
			OutputPath:   filename,
			BytesWritten: int64(n),
			Payment:      &buyer.PaymentResult{CostSatoshis: buyResult.CostSatoshis, HTLCTxID: buyResult.HTLCTxID},
		}
		return writeJSON(resp, stdout, stderr)
	}

	fmt.Fprintf(stdout, "Downloaded %d bytes to %s\n", n, filename)
	return 0
}

// handleError maps client errors to exit codes and prints a message.
func handleError(err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "bget: not found\n")
		return 2
	case errors.Is(err, client.ErrTimeout):
		fmt.Fprintf(stderr, "bget: request timeout\n")
		return 4
	case errors.Is(err, client.ErrNetwork):
		fmt.Fprintf(stderr, "bget: network error: %v\n", err)
		return 4
	case errors.Is(err, client.ErrServer):
		fmt.Fprintf(stderr, "bget: server error: %v\n", err)
		return 4
	default:
		fmt.Fprintf(stderr, "bget: %v\n", err)
		return 1
	}
}

// ---------------------------------------------------------------------------
// JSON output helpers
// ---------------------------------------------------------------------------

// downloadContentJSON fetches and decrypts free content, then outputs a JSON
// result instead of the human-readable "Downloaded N bytes" message.
func downloadContentJSON(c *client.Client, meta *client.MetaResponse, outputName string, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		return handleErrorJSON(fmt.Errorf("no content hash available"), stdout)
	}

	filename := outputName
	if filename == "" {
		filename = deriveFilename(meta.Path)
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleErrorJSON(err, stdout)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
	if err != nil {
		return handleErrorJSON(fmt.Errorf("read error: %w", err), stdout)
	}

	// Decrypt using Method 42 free mode.
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

	file, err := os.Create(filename)
	if err != nil {
		return handleErrorJSON(fmt.Errorf("cannot create file %q: %w", filename, err), stdout)
	}

	n, err := file.Write(plaintext)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(filename)
		return handleErrorJSON(fmt.Errorf("write error: %w", err), stdout)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(filename)
		return handleErrorJSON(fmt.Errorf("close error: %w", err), stdout)
	}

	resp := &buyer.GetResponse{
		Meta:         meta,
		OutputPath:   filename,
		BytesWritten: int64(n),
	}
	return writeJSON(resp, stdout, stderr)
}

// outputPaymentRequiredJSON outputs a JSON response indicating payment is required.
func outputPaymentRequiredJSON(meta *client.MetaResponse, stdout, stderr io.Writer) int {
	resp := &buyer.GetResponse{
		Meta:            meta,
		PaymentRequired: true,
		PaymentInfo: &buyer.PaymentInfo{
			Price:      meta.PricePerKB * (meta.FileSize/1024 + 1),
			PricePerKB: meta.PricePerKB,
		},
	}
	return writeJSON(resp, stdout, stderr)
}

// handleErrorJSON outputs a JSON error response to stdout and returns the
// appropriate exit code.
func handleErrorJSON(err error, stdout io.Writer) int {
	code := errorToCode(err)
	resp := &buyer.ErrorResponse{Error: errorMessage(err), Code: code}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(stdout, string(data))
	return code
}

// writeJSON marshals v as indented JSON to stdout.
func writeJSON(v interface{}, stdout, stderr io.Writer) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "bget: json marshal: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

// errorToCode maps an error to an exit code for JSON output.
func errorToCode(err error) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		return 2
	case errors.Is(err, client.ErrTimeout), errors.Is(err, client.ErrNetwork):
		return 4
	case errors.Is(err, client.ErrServer):
		return 4
	case errors.Is(err, client.ErrPaymentRequired):
		return 5
	default:
		return 1
	}
}

// errorMessage returns a human-readable error string for JSON output.
func errorMessage(err error) string {
	switch {
	case errors.Is(err, client.ErrNotFound):
		return "not found"
	case errors.Is(err, client.ErrTimeout):
		return "request timeout"
	case errors.Is(err, client.ErrNetwork):
		return "network error"
	case errors.Is(err, client.ErrServer):
		return "server error"
	default:
		return err.Error()
	}
}

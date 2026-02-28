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
	"path/filepath"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bitfsorg/bitfs/internal/buy"
	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/libbitfs-go/method42"
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
	buyFlag := fs.Bool("buy", false, "attempt to purchase paid content")
	verify := fs.Bool("verify", false, "SPV-verify the Metanet tx before downloading")
	walletKey := fs.String("wallet-key", "", "hex-encoded buyer private key (32 bytes)")
	utxoStr := fs.String("utxo", "", "buyer UTXO for purchase (txid:vout:amount)")
	version := fs.Int("version", 0, "download a specific version (1=latest, 2=previous, ...)")
	jsonOut := fs.Bool("json", false, "JSON output")
	host := fs.String("host", "", "daemon URL override")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")
	noCache := fs.Bool("no-cache", false, "skip metadata cache")
	offline := fs.Bool("offline", false, "cache-only mode")

	if err := fs.Parse(args); err != nil {
		return 6
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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "bget: cannot determine home directory: %v\n", err)
		return 1
	}
	cacheDir := filepath.Join(homeDir, ".bitfs", "cache", "meta")
	cache := client.NewMetaCache(cacheDir, 5*time.Minute)
	cc := client.NewCachedClient(c, cache)
	cc.NoCache = *noCache
	cc.Offline = *offline
	cc.Prefix = c.BaseURL

	uriPath := resolved.Path

	meta, err := cc.GetMeta(resolved.PNode, uriPath)
	if err != nil {
		if *jsonOut {
			return handleErrorJSON(err, stdout)
		}
		return buy.HandleError(err, "bget", stderr)
	}

	// Version override: fetch version history and apply the selected version's metadata.
	if *version > 0 {
		vers, versErr := c.GetVersions(resolved.PNode, uriPath)
		if versErr != nil {
			if *jsonOut {
				return handleErrorJSON(versErr, stdout)
			}
			return buy.HandleError(versErr, "bget", stderr)
		}
		if *version > len(vers) {
			msg := fmt.Errorf("version %d not found (only %d versions)", *version, len(vers))
			if *jsonOut {
				return handleErrorJSON(msg, stdout)
			}
			fmt.Fprintf(stderr, "bget: %v\n", msg)
			return 2
		}
		v := vers[*version-1]
		meta.TxID = v.TxID
		meta.FileSize = v.FileSize
		meta.Access = v.Access
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
		return handlePaid(c, meta, *buyFlag, *walletKey, *utxoStr, *output, *jsonOut, stdout, stderr)
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
		return buy.HandleError(err, "bget", stderr)
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
func handlePaid(c *client.Client, meta *client.MetaResponse, buyEnabled bool, walletKey, utxoFlag, outputName string, jsonOut bool, stdout, stderr io.Writer) int {
	if !buyEnabled {
		if jsonOut {
			return outputPaymentRequiredJSON(meta, stdout, stderr)
		}
		fmt.Fprintf(stderr, "bget: content requires payment: %d sat/KB (%d bytes)\nUse --buy to purchase\n",
			meta.PricePerKB, meta.FileSize)
		return 5
	}

	cfg, err := buy.LoadConfig(buy.LoadConfigOpts{WalletKeyFlag: walletKey, UTXOFlag: utxoFlag})
	if err != nil {
		if jsonOut {
			return handleErrorJSON(err, stdout)
		}
		fmt.Fprintf(stderr, "bget: %v\n", err)
		return 6
	}

	result, err := buy.Buy(&buy.BuyParams{
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
			return buy.HandleError(err, "bget", stderr)
		}
		fmt.Fprintf(stderr, "bget: purchase failed: %v\n", err)
		return 5
	}

	return downloadPaidContent(c, meta, result, cfg.PrivKey, outputName, jsonOut, stdout, stderr)
}

// downloadPaidContent fetches encrypted data, decrypts it using the capsule
// obtained from the purchase, and writes the plaintext to a file.
func downloadPaidContent(c *client.Client, meta *client.MetaResponse, buyResult *buy.BuyResult, privKey *ec.PrivateKey, outputName string, jsonOut bool, stdout, stderr io.Writer) int {
	// Fetch encrypted content.
	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(err, stdout)
		}
		return buy.HandleError(err, "bget", stderr)
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
		resp := &buy.GetResponse{
			Meta:         meta,
			OutputPath:   filename,
			BytesWritten: int64(n),
			Payment:      &buy.PaymentResult{CostSatoshis: buyResult.CostSatoshis, HTLCTxID: buyResult.HTLCTxID},
		}
		return writeJSON(resp, stdout, stderr)
	}

	fmt.Fprintf(stdout, "Downloaded %d bytes to %s\n", n, filename)
	return 0
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

	resp := &buy.GetResponse{
		Meta:         meta,
		OutputPath:   filename,
		BytesWritten: int64(n),
	}
	return writeJSON(resp, stdout, stderr)
}

// outputPaymentRequiredJSON outputs a JSON response indicating payment is required.
func outputPaymentRequiredJSON(meta *client.MetaResponse, stdout, stderr io.Writer) int {
	resp := &buy.GetResponse{
		Meta:            meta,
		PaymentRequired: true,
		PaymentInfo: &buy.PaymentInfo{
			Price:      meta.PricePerKB * (meta.FileSize/1024 + 1),
			PricePerKB: meta.PricePerKB,
		},
	}
	return writeJSON(resp, stdout, stderr)
}

// handleErrorJSON outputs a JSON error response to stdout and returns the
// appropriate exit code.
func handleErrorJSON(err error, stdout io.Writer) int {
	code := buy.ExitCodeFromError(err)
	resp := &buy.ErrorResponse{Error: buy.ErrorMessage(err), Code: code}
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

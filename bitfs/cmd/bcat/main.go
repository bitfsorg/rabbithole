// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bcat outputs file content from a BitFS filesystem, like Unix cat.
package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bitfsorg/bitfs/internal/buy"
	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/libbitfs-go/method42"
)

// maxContentSize is the maximum encrypted content size bcat will read (1 GB).
const maxContentSize = 1 << 30

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bcat", flag.ContinueOnError)
	fs.SetOutput(stderr)

	buyFlag := fs.Bool("buy", false, "attempt to purchase paid content")
	verify := fs.Bool("verify", false, "SPV-verify the Metanet tx before outputting")
	walletKey := fs.String("wallet-key", "", "hex-encoded buyer private key (32 bytes)")
	utxoFlag := fs.String("utxo", "", "manual UTXO for purchase (txid:vout:amount)")
	jsonOut := fs.Bool("json", false, "JSON output")
	host := fs.String("host", "", "daemon URL override")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")
	noCache := fs.Bool("no-cache", false, "skip metadata cache")
	offline := fs.Bool("offline", false, "cache-only mode")

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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "bcat: cannot determine home directory: %v\n", err)
		return 1
	}
	cacheDir := filepath.Join(homeDir, ".bitfs", "cache", "meta")
	cache := client.NewMetaCache(cacheDir, 5*time.Minute)
	cc := client.NewCachedClient(c, cache)
	cc.NoCache = *noCache
	cc.Offline = *offline
	cc.Prefix = c.BaseURL

	meta, err := cc.GetMeta(resolved.PNode, resolved.Path)
	if err != nil {
		return buy.HandleError(err, "bcat", stderr)
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
		return handlePaid(c, meta, *buyFlag, *walletKey, *utxoFlag, *jsonOut, stdout, stderr)
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
		return buy.HandleError(err, "bcat", stderr)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
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
func handlePaid(c *client.Client, meta *client.MetaResponse, buyEnabled bool, walletKey, utxoFlag string, jsonOut bool, stdout, stderr io.Writer) int {
	if !buyEnabled {
		if jsonOut {
			return outputPaymentRequiredJSON(meta, stdout, stderr)
		}
		fmt.Fprintf(stderr, "bcat: content requires payment: %d sat/KB (%d bytes)\nUse --buy to purchase\n",
			meta.PricePerKB, meta.FileSize)
		return 5
	}

	cfg, err := buy.LoadConfig(buy.LoadConfigOpts{
		WalletKeyFlag: walletKey,
		UTXOFlag:      utxoFlag,
	})
	if err != nil {
		if jsonOut {
			return handleErrorJSON(err, stdout)
		}
		fmt.Fprintf(stderr, "bcat: %v\n", err)
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
		fmt.Fprintf(stderr, "bcat: purchase failed: %v\n", err)
		return 5
	}

	// Decrypt with capsule and output.
	return outputPaidContent(c, meta, result, cfg.PrivKey, jsonOut, stdout, stderr)
}

// outputPaidContent fetches encrypted data, decrypts with the purchase capsule,
// and writes plaintext to stdout.
func outputPaidContent(c *client.Client, meta *client.MetaResponse, buyResult *buy.BuyResult, privKey *ec.PrivateKey, jsonOut bool, stdout, stderr io.Writer) int {
	if meta.KeyHash == "" {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("no content hash available"), stdout)
		}
		fmt.Fprintf(stderr, "bcat: no content hash available\n")
		return 1
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(err, stdout)
		}
		return buy.HandleError(err, "bcat", stderr)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("read error: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bcat: read: %v\n", err)
		return 4
	}

	keyHashBytes, err := hex.DecodeString(meta.KeyHash)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("invalid key hash: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bcat: invalid key hash hex: %v\n", err)
		return 5
	}

	nodePubBytes, err := hex.DecodeString(meta.PNode)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("invalid pnode: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bcat: invalid pnode hex: %v\n", err)
		return 5
	}
	nodePub, err := ec.PublicKeyFromBytes(nodePubBytes)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("invalid pnode key: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bcat: invalid pnode key: %v\n", err)
		return 5
	}

	decResult, err := method42.DecryptWithCapsule(ciphertext, buyResult.Capsule, keyHashBytes, privKey, nodePub)
	if err != nil {
		if jsonOut {
			return handleErrorJSON(fmt.Errorf("decrypt: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bcat: decrypt: %v\n", err)
		return 5
	}

	if jsonOut {
		return outputPaidContentJSON(meta, decResult.Plaintext, buyResult, stdout, stderr)
	}

	if _, err := stdout.Write(decResult.Plaintext); err != nil {
		fmt.Fprintf(stderr, "bcat: write error: %v\n", err)
		return 1
	}
	return 0
}

// outputPaidContentJSON outputs decrypted paid content as a JSON response.
func outputPaidContentJSON(meta *client.MetaResponse, plaintext []byte, buyResult *buy.BuyResult, stdout, stderr io.Writer) int {
	resp := &buy.CatResponse{Meta: meta}
	if strings.HasPrefix(meta.MimeType, "text/") || meta.MimeType == "application/json" {
		s := string(plaintext)
		resp.Content = &s
	} else {
		s := base64.StdEncoding.EncodeToString(plaintext)
		resp.ContentBase64 = &s
	}
	resp.Payment = &buy.PaymentResult{
		CostSatoshis: buyResult.CostSatoshis,
		HTLCTxID:     buyResult.HTLCTxID,
	}
	return writeJSON(resp, stdout, stderr)
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

	ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
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

	resp := &buy.CatResponse{Meta: meta}
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
	resp := &buy.CatResponse{
		Meta:            meta,
		PaymentRequired: true,
		PaymentInfo: &buy.PaymentInfo{
			Price:      meta.PricePerKB * (meta.FileSize/1024 + 1),
			PricePerKB: meta.PricePerKB,
		},
	}
	return writeJSON(resp, stdout, stderr)
}

// handleErrorJSON outputs an error as JSON and returns the exit code.
func handleErrorJSON(err error, stdout io.Writer) int {
	code := buy.ExitCodeFromError(err)
	resp := &buy.ErrorResponse{Error: buy.ErrorMessage(err), Code: code}
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

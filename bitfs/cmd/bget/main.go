// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bget downloads a file from a BitFS filesystem, like Unix wget.
package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/tongxiaofeng/bitfs/internal/client"
	"github.com/tongxiaofeng/libbitfs-go/method42"
	"github.com/tongxiaofeng/libbitfs-go/paymail"
	"github.com/tongxiaofeng/libbitfs-go/x402"
)

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
	host := fs.String("host", "http://localhost:8080", "daemon URL")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if *version {
		fmt.Fprintf(stdout, "bget: --version not yet supported\n")
		return 0
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "Usage: bget [-o FILE] [--buy] [--host URL] [--timeout DURATION] <bitfs-uri>\n")
		return 6
	}

	uri := fs.Arg(0)
	parsed, err := paymail.ParseURI(uri)
	if err != nil {
		fmt.Fprintf(stderr, "bget: %v\n", err)
		return 6
	}

	// Resolve pnode from parsed URI.
	var pnode string
	switch parsed.Type {
	case paymail.AddressPubKey:
		pnode = hex.EncodeToString(parsed.PubKey)
	case paymail.AddressPaymail, paymail.AddressDNSLink:
		fmt.Fprintf(stderr, "bget: paymail/dnslink resolution not yet supported\n")
		return 6
	default:
		fmt.Fprintf(stderr, "bget: unknown address type\n")
		return 6
	}

	// Build client.
	c := client.New(*host)
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			fmt.Fprintf(stderr, "bget: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	// Determine the path to query. Default to root "/" if none specified.
	uriPath := parsed.Path
	if uriPath == "" {
		uriPath = "/"
	}

	meta, err := c.GetMeta(pnode, uriPath)
	if err != nil {
		return handleError(err, stderr)
	}

	// Directories cannot be downloaded.
	if meta.Type == "dir" {
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
		return downloadContent(c, meta, *output, stdout, stderr)
	case "paid":
		return handlePaid(c, meta, *buy, *walletKey, *utxoStr, *output, stdout, stderr)
	case "private":
		fmt.Fprintf(stderr, "bget: private content cannot be accessed remotely\n")
		return 6
	default:
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

	ciphertext, err := io.ReadAll(reader)
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
func handlePaid(c *client.Client, meta *client.MetaResponse, buy bool, walletKey, utxoFlag, outputName string, stdout, stderr io.Writer) int {
	if !buy {
		fmt.Fprintf(stderr, "bget: content requires payment: %d sat/KB (%d bytes)\nUse --buy to purchase\n",
			meta.PricePerKB, meta.FileSize)
		return 5
	}

	// Validate wallet key is provided.
	if walletKey == "" {
		fmt.Fprintf(stderr, "bget: --wallet-key is required for purchases\n")
		return 6
	}

	// Validate UTXO is provided.
	if utxoFlag == "" {
		fmt.Fprintf(stderr, "bget: --utxo is required for purchases (format: txid:vout:amount)\n")
		return 6
	}

	// Parse the hex-encoded private key.
	keyBytes, err := hex.DecodeString(walletKey)
	if err != nil {
		fmt.Fprintf(stderr, "bget: invalid wallet key hex: %v\n", err)
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
		fmt.Fprintf(stderr, "bget: wallet key must be 32 or 33 bytes, got %d\n", len(keyBytes))
		return 6
	}

	privKey, _ := ec.PrivateKeyFromBytes(keyBytes)
	if privKey == nil {
		fmt.Fprintf(stderr, "bget: failed to parse wallet key\n")
		return 6
	}

	// Parse UTXO from flag.
	utxo, err := parseUTXOFlag(utxoFlag)
	if err != nil {
		fmt.Fprintf(stderr, "bget: invalid --utxo: %v\n", err)
		return 6
	}

	// Derive buyer's P2PKH script for the UTXO.
	buyerPKH := privKey.PubKey().Hash()
	utxo.ScriptPubKey = buildBuyerP2PKHScript(buyerPKH)

	// Validate that meta has a TxID for the purchase invoice.
	if meta.TxID == "" {
		fmt.Fprintf(stderr, "bget: paid content has no invoice txid\n")
		return 5
	}

	// Step 1: Get buy info (capsule_hash, price, payment_addr).
	// Pass buyer's pubkey so the server computes the buyer-specific capsule.
	buyerPubHex := hex.EncodeToString(privKey.PubKey().Compressed())
	buyInfo, err := c.GetBuyInfo(meta.TxID, buyerPubHex)
	if err != nil {
		fmt.Fprintf(stderr, "bget: get buy info: %v\n", err)
		return handleError(err, stderr)
	}

	// Decode capsule hash from hex.
	capsuleHash, err := hex.DecodeString(buyInfo.CapsuleHash)
	if err != nil {
		fmt.Fprintf(stderr, "bget: invalid capsule hash hex: %v\n", err)
		return 5
	}

	// Decode payment address (hex-encoded 20-byte pubkey hash).
	sellerAddr, err := hex.DecodeString(buyInfo.PaymentAddr)
	if err != nil {
		fmt.Fprintf(stderr, "bget: invalid payment address hex: %v\n", err)
		return 5
	}

	// Decode seller pubkey (hex-encoded 33-byte compressed public key).
	sellerPubKey, err := hex.DecodeString(buyInfo.SellerPubKey)
	if err != nil {
		fmt.Fprintf(stderr, "bget: invalid seller pubkey hex: %v\n", err)
		return 5
	}

	// Step 2: Build HTLC funding transaction.
	fundingResult, err := x402.BuildHTLCFundingTx(&x402.HTLCFundingParams{
		BuyerPrivKey: privKey,
		SellerAddr:   sellerAddr,
		SellerPubKey: sellerPubKey,
		CapsuleHash:  capsuleHash,
		Amount:       buyInfo.Price,
		Timeout:      x402.DefaultHTLCTimeout,
		UTXOs:        []*x402.HTLCUTXO{utxo},
		ChangeAddr:   buyerPKH,
		FeeRate:      1,
	})
	if err != nil {
		fmt.Fprintf(stderr, "bget: build HTLC funding tx: %v\n", err)
		return 5
	}

	// Step 3: Submit the signed funding transaction to get the capsule.
	capsuleResp, err := c.SubmitHTLC(meta.TxID, fundingResult.RawTx)
	if err != nil {
		fmt.Fprintf(stderr, "bget: submit HTLC: %v\n", err)
		return handleError(err, stderr)
	}

	// Decode capsule from hex.
	capsule, err := hex.DecodeString(capsuleResp.Capsule)
	if err != nil {
		fmt.Fprintf(stderr, "bget: invalid capsule hex: %v\n", err)
		return 5
	}

	// Step 4: Fetch encrypted content.
	if meta.KeyHash == "" {
		fmt.Fprintf(stderr, "bget: no content hash available\n")
		return 1
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return handleError(err, stderr)
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(reader)
	if err != nil {
		fmt.Fprintf(stderr, "bget: read content: %v\n", err)
		return 4
	}

	// Decode keyHash from hex.
	keyHashBytes, err := hex.DecodeString(meta.KeyHash)
	if err != nil {
		fmt.Fprintf(stderr, "bget: invalid key hash hex: %v\n", err)
		return 5
	}

	// Decode the node's public key for capsule decryption.
	nodePubBytes, err := hex.DecodeString(meta.PNode)
	if err != nil {
		fmt.Fprintf(stderr, "bget: invalid pnode hex: %v\n", err)
		return 5
	}
	nodePub, err := ec.PublicKeyFromBytes(nodePubBytes)
	if err != nil {
		fmt.Fprintf(stderr, "bget: invalid pnode key: %v\n", err)
		return 5
	}

	// Step 5: Decrypt with capsule.
	result, err := method42.DecryptWithCapsule(ciphertext, capsule, keyHashBytes, privKey, nodePub)
	if err != nil {
		fmt.Fprintf(stderr, "bget: decrypt: %v\n", err)
		return 5
	}

	// Step 6: Write decrypted content to file.
	filename := outputName
	if filename == "" {
		filename = deriveFilename(meta.Path)
	}

	file, err := os.Create(filename)
	if err != nil {
		fmt.Fprintf(stderr, "bget: cannot create file %q: %v\n", filename, err)
		return 1
	}

	n, err := file.Write(result.Plaintext)
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

// parseUTXOFlag parses a UTXO from the --utxo flag (format: txid:vout:amount).
func parseUTXOFlag(s string) (*x402.HTLCUTXO, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("expected txid:vout:amount")
	}
	txid, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid txid hex: %w", err)
	}
	if len(txid) != 32 {
		return nil, fmt.Errorf("txid must be 32 bytes")
	}
	vout, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid vout: %w", err)
	}
	amount, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}
	return &x402.HTLCUTXO{
		TxID:   txid,
		Vout:   uint32(vout),
		Amount: amount,
	}, nil
}

// buildBuyerP2PKHScript builds a standard P2PKH locking script from a pubkey hash.
func buildBuyerP2PKHScript(pkh []byte) []byte {
	// OP_DUP OP_HASH160 <20 bytes> OP_EQUALVERIFY OP_CHECKSIG
	s := make([]byte, 0, 25)
	s = append(s, 0x76, 0xa9, 0x14) // OP_DUP OP_HASH160 PUSH20
	s = append(s, pkh...)
	s = append(s, 0x88, 0xac) // OP_EQUALVERIFY OP_CHECKSIG
	return s
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

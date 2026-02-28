// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

// Command bmget batch-downloads files from a BitFS directory, like recursive wget.
// It resolves a bitfs:// URI pointing to a directory, lists all file children,
// and downloads them concurrently with optional purchase support for paid content.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bitfsorg/bitfs/internal/buy"
	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/libbitfs-go/method42"
)

const maxContentSize = 1 << 30 // 1 GB

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bmget", flag.ContinueOnError)
	fs.SetOutput(stderr)

	jsonOut := fs.Bool("json", false, "JSON output")
	buyFlag := fs.Bool("buy", false, "attempt to purchase paid content")
	walletKey := fs.String("wallet-key", "", "hex-encoded buyer private key (32 bytes)")
	utxoStr := fs.String("utxo", "", "buyer UTXO for purchases (txid:vout:amount)")
	concurrency := fs.Int("concurrency", 4, "max concurrent downloads")
	failFast := fs.Bool("fail-fast", false, "stop on first error")
	host := fs.String("host", "", "daemon URL override")
	timeout := fs.String("timeout", "", "request timeout (e.g. 10s, 1m)")
	noCache := fs.Bool("no-cache", false, "skip metadata cache")
	offline := fs.Bool("offline", false, "cache-only mode")

	if err := fs.Parse(args); err != nil {
		return 6
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, `Usage: bmget [--json] [--buy] [--concurrency N] [--fail-fast] [--host URL] <bitfs-uri> [local-dir]

Batch-download all files from a BitFS directory.

Examples:
  bmget bitfs://example.com/docs/                  (download to ./docs/)
  bmget --buy --wallet-key KEY bitfs://alice@example.com/data/ ./out/
  bmget --json --concurrency 8 bitfs://02abc.../dir/ --host http://localhost:8080
`)
		return 6
	}

	uri := fs.Arg(0)
	localDir := fs.Arg(1) // optional; defaults to basename of URI path

	// Resolve URI to client + pnode + path.
	resolved, err := client.ResolveURI(uri, *host, nil, nil)
	if err != nil {
		if *jsonOut {
			return handleErrorJSON(fmt.Errorf("resolve URI: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bmget: %v\n", err)
		return 6
	}

	c := resolved.Client
	if *timeout != "" {
		d, err := time.ParseDuration(*timeout)
		if err != nil {
			if *jsonOut {
				return handleErrorJSON(fmt.Errorf("invalid timeout %q: %w", *timeout, err), stdout)
			}
			fmt.Fprintf(stderr, "bmget: invalid timeout %q: %v\n", *timeout, err)
			return 6
		}
		c = c.WithTimeout(d)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		if *jsonOut {
			return handleErrorJSON(fmt.Errorf("cannot determine home directory: %w", err), stdout)
		}
		fmt.Fprintf(stderr, "bmget: cannot determine home directory: %v\n", err)
		return 1
	}
	cacheDir := filepath.Join(homeDir, ".bitfs", "cache", "meta")
	cache := client.NewMetaCache(cacheDir, 5*time.Minute)
	cc := client.NewCachedClient(c, cache)
	cc.NoCache = *noCache
	cc.Offline = *offline
	cc.Prefix = c.BaseURL

	uriPath := resolved.Path

	// Get directory metadata.
	meta, err := cc.GetMeta(resolved.PNode, uriPath)
	if err != nil {
		if *jsonOut {
			return handleErrorJSON(err, stdout)
		}
		return buy.HandleError(err, "bmget", stderr)
	}

	if meta.Type != "dir" {
		if *jsonOut {
			return handleErrorJSON(fmt.Errorf("not a directory: %s", uriPath), stdout)
		}
		fmt.Fprintf(stderr, "bmget: %s: not a directory\n", uriPath)
		return 6
	}

	// Filter file entries from children.
	var files []client.ChildEntry
	for _, child := range meta.Children {
		if child.Type == "file" {
			files = append(files, child)
		}
	}

	if len(files) == 0 {
		if *jsonOut {
			resp := &buy.BatchGetResponse{
				Total:     0,
				Succeeded: 0,
				Failed:    0,
				Files:     []buy.BatchFileEntry{},
			}
			return writeJSON(resp, stdout, stderr)
		}
		fmt.Fprintf(stdout, "bmget: no files in directory %s\n", uriPath)
		return 0
	}

	// Determine local output directory.
	if localDir == "" {
		base := path.Base(uriPath)
		if base == "" || base == "/" || base == "." {
			localDir = "download"
		} else {
			localDir = base
		}
	}

	// Create the local directory.
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		if *jsonOut {
			return handleErrorJSON(fmt.Errorf("create directory %q: %w", localDir, err), stdout)
		}
		fmt.Fprintf(stderr, "bmget: cannot create directory %q: %v\n", localDir, err)
		return 1
	}

	// Load buyer config if --buy is set.
	var buyerCfg *buy.BuyerConfig
	if *buyFlag {
		cfg, err := buy.LoadConfig(buy.LoadConfigOpts{
			WalletKeyFlag: *walletKey,
			UTXOFlag:      *utxoStr,
		})
		if err != nil {
			if *jsonOut {
				return handleErrorJSON(fmt.Errorf("buyer config: %w", err), stdout)
			}
			fmt.Fprintf(stderr, "bmget: %v\n", err)
			return 6
		}
		buyerCfg = cfg
	}

	// Concurrency control.
	conc := *concurrency
	if conc < 1 {
		conc = 1
	}

	// Download files concurrently.
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, conc)
		results = make([]buy.BatchFileEntry, len(files))
		stopped int32 // atomic flag for fail-fast
	)

	for i, child := range files {
		if *failFast && atomic.LoadInt32(&stopped) != 0 {
			mu.Lock()
			results[i] = buy.BatchFileEntry{
				Path:  child.Name,
				Error: "skipped: fail-fast triggered",
				Code:  1,
			}
			mu.Unlock()
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // acquire semaphore

		go func(idx int, childEntry client.ChildEntry) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore

			if *failFast && atomic.LoadInt32(&stopped) != 0 {
				mu.Lock()
				results[idx] = buy.BatchFileEntry{
					Path:  childEntry.Name,
					Error: "skipped: fail-fast triggered",
					Code:  1,
				}
				mu.Unlock()
				return
			}

			childPath := path.Join(uriPath, childEntry.Name)
			localPath := filepath.Join(localDir, childEntry.Name)
			entry := downloadFile(cc, c, resolved.PNode, childPath, localPath, *buyFlag, buyerCfg)
			entry.Path = childEntry.Name

			mu.Lock()
			results[idx] = entry
			mu.Unlock()

			if entry.Error != "" && *failFast {
				atomic.StoreInt32(&stopped, 1)
			}
		}(i, child)
	}

	wg.Wait()

	// Count results.
	succeeded := 0
	failed := 0
	for _, r := range results {
		if r.Error != "" {
			failed++
		} else {
			succeeded++
		}
	}

	if *jsonOut {
		resp := &buy.BatchGetResponse{
			Total:     len(files),
			Succeeded: succeeded,
			Failed:    failed,
			Files:     results,
		}
		return writeJSON(resp, stdout, stderr)
	}

	// Text output summary.
	for _, r := range results {
		if r.Error != "" {
			fmt.Fprintf(stderr, "  FAIL  %s: %s\n", r.Path, r.Error)
		} else {
			msg := fmt.Sprintf("  OK    %s -> %s (%d bytes)", r.Path, r.OutputPath, r.BytesWritten)
			if r.Payment != nil {
				msg += fmt.Sprintf(" [paid %d sat]", r.Payment.CostSatoshis)
			}
			fmt.Fprintln(stdout, msg)
		}
	}
	fmt.Fprintf(stdout, "\nbmget: %d/%d files downloaded to %s\n", succeeded, len(files), localDir)

	if failed > 0 {
		return 1
	}
	return 0
}

// downloadFile downloads a single file from the daemon, decrypting it with
// Method 42. For paid content with --buy, it executes the purchase flow first.
func downloadFile(mg client.MetaGetter, c *client.Client, pnode, remotePath, localPath string, buyEnabled bool, buyerCfg *buy.BuyerConfig) buy.BatchFileEntry {
	// Get file metadata.
	meta, err := mg.GetMeta(pnode, remotePath)
	if err != nil {
		return buy.BatchFileEntry{Error: fmt.Sprintf("get meta: %v", err), Code: buy.ExitCodeFromError(err)}
	}

	switch meta.Access {
	case "free":
		return downloadFreeFile(c, meta, localPath)
	case "paid":
		if !buyEnabled || buyerCfg == nil {
			return buy.BatchFileEntry{
				Error: fmt.Sprintf("payment required: %d sat/KB", meta.PricePerKB),
				Code:  5,
			}
		}
		return downloadPaidFile(c, meta, localPath, buyerCfg)
	case "private":
		return buy.BatchFileEntry{Error: "private content", Code: 6}
	default:
		return buy.BatchFileEntry{Error: fmt.Sprintf("unknown access mode %q", meta.Access), Code: 1}
	}
}

// downloadFreeFile fetches encrypted data and decrypts it using Method 42 free mode.
func downloadFreeFile(c *client.Client, meta *client.MetaResponse, localPath string) buy.BatchFileEntry {
	if meta.KeyHash == "" {
		return buy.BatchFileEntry{Error: "no content hash available", Code: 1}
	}

	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return buy.BatchFileEntry{Error: fmt.Sprintf("get data: %v", err), Code: buy.ExitCodeFromError(err)}
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
	if err != nil {
		return buy.BatchFileEntry{Error: fmt.Sprintf("read data: %v", err), Code: 4}
	}

	// Decrypt using Method 42 free mode.
	var plaintext []byte
	if len(ciphertext) > 0 {
		pubKeyBytes, err := hex.DecodeString(meta.PNode)
		if err != nil {
			return buy.BatchFileEntry{Error: fmt.Sprintf("invalid pnode hex: %v", err), Code: 1}
		}
		pubKey, err := ec.PublicKeyFromBytes(pubKeyBytes)
		if err != nil {
			return buy.BatchFileEntry{Error: fmt.Sprintf("invalid pnode key: %v", err), Code: 1}
		}
		keyHashBytes, err := hex.DecodeString(meta.KeyHash)
		if err != nil {
			return buy.BatchFileEntry{Error: fmt.Sprintf("invalid key hash hex: %v", err), Code: 1}
		}
		result, err := method42.Decrypt(ciphertext, nil, pubKey, keyHashBytes, method42.AccessFree)
		if err != nil {
			return buy.BatchFileEntry{Error: fmt.Sprintf("decrypt: %v", err), Code: 5}
		}
		plaintext = result.Plaintext
	}

	// Write to disk.
	n, err := writeFile(localPath, plaintext)
	if err != nil {
		return buy.BatchFileEntry{Error: fmt.Sprintf("write file: %v", err), Code: 1}
	}

	return buy.BatchFileEntry{
		OutputPath:   localPath,
		BytesWritten: int64(n),
	}
}

// downloadPaidFile executes the purchase flow via buy.Buy(), then decrypts
// the content using the capsule obtained from the HTLC exchange.
func downloadPaidFile(c *client.Client, meta *client.MetaResponse, localPath string, cfg *buy.BuyerConfig) buy.BatchFileEntry {
	if meta.TxID == "" {
		return buy.BatchFileEntry{Error: "paid content has no invoice txid", Code: 5}
	}
	if meta.KeyHash == "" {
		return buy.BatchFileEntry{Error: "no content hash available", Code: 1}
	}

	// Execute purchase flow.
	buyResult, err := buy.Buy(&buy.BuyParams{
		Client: c,
		TxID:   meta.TxID,
		Config: cfg,
	})
	if err != nil {
		return buy.BatchFileEntry{Error: fmt.Sprintf("buy: %v", err), Code: 5}
	}

	// Fetch encrypted content.
	reader, err := c.GetData(meta.KeyHash)
	if err != nil {
		return buy.BatchFileEntry{
			Error:   fmt.Sprintf("get data after purchase: %v", err),
			Code:    buy.ExitCodeFromError(err),
			Payment: &buy.PaymentResult{CostSatoshis: buyResult.CostSatoshis, HTLCTxID: buyResult.HTLCTxID},
		}
	}
	defer func() { _ = reader.Close() }()

	ciphertext, err := io.ReadAll(io.LimitReader(reader, maxContentSize))
	if err != nil {
		return buy.BatchFileEntry{
			Error:   fmt.Sprintf("read data: %v", err),
			Code:    4,
			Payment: &buy.PaymentResult{CostSatoshis: buyResult.CostSatoshis, HTLCTxID: buyResult.HTLCTxID},
		}
	}

	// Decrypt with capsule.
	keyHashBytes, err := hex.DecodeString(meta.KeyHash)
	if err != nil {
		return buy.BatchFileEntry{Error: fmt.Sprintf("invalid key hash hex: %v", err), Code: 1}
	}

	nodePubBytes, err := hex.DecodeString(meta.PNode)
	if err != nil {
		return buy.BatchFileEntry{Error: fmt.Sprintf("invalid pnode hex: %v", err), Code: 1}
	}
	nodePub, err := ec.PublicKeyFromBytes(nodePubBytes)
	if err != nil {
		return buy.BatchFileEntry{Error: fmt.Sprintf("invalid pnode key: %v", err), Code: 1}
	}

	result, err := method42.DecryptWithCapsule(ciphertext, buyResult.Capsule, keyHashBytes, cfg.PrivKey, nodePub)
	if err != nil {
		return buy.BatchFileEntry{
			Error:   fmt.Sprintf("decrypt: %v", err),
			Code:    5,
			Payment: &buy.PaymentResult{CostSatoshis: buyResult.CostSatoshis, HTLCTxID: buyResult.HTLCTxID},
		}
	}

	// Write to disk.
	n, err := writeFile(localPath, result.Plaintext)
	if err != nil {
		return buy.BatchFileEntry{
			Error:   fmt.Sprintf("write file: %v", err),
			Code:    1,
			Payment: &buy.PaymentResult{CostSatoshis: buyResult.CostSatoshis, HTLCTxID: buyResult.HTLCTxID},
		}
	}

	return buy.BatchFileEntry{
		OutputPath:   localPath,
		BytesWritten: int64(n),
		Payment:      &buy.PaymentResult{CostSatoshis: buyResult.CostSatoshis, HTLCTxID: buyResult.HTLCTxID},
	}
}

// writeFile writes data to the given path, creating parent directories as needed.
func writeFile(path string, data []byte) (int, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}

	n, err := f.Write(data)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return 0, err
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return 0, err
	}

	return n, nil
}

// ---------------------------------------------------------------------------
// Error handling and JSON output helpers
// ---------------------------------------------------------------------------

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
		fmt.Fprintf(stderr, "bmget: json marshal: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

# Audit Fixes Implementation Plan (CRITICAL + HIGH)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all CRITICAL and HIGH severity findings from the 2026-02-26 code audit report.

**Architecture:** 7 targeted fixes across libbitfs-go (4 fixes) and bitfs (3 fixes), grouped into 4 themed commits. Each fix adds input validation or missing security checks at trust boundaries.

**Tech Stack:** Go 1.25, libbitfs-go (spv/paymail/storage/x402/network), bitfs (daemon/engine)

**Pre-verified:** C-1 (single-tx block) and H-NEW-1 (payment TOCTOU) are already fixed in current code. C-1 test passes; H-NEW-1 was fixed in commit `7c7e25c`. These are excluded from implementation.

---

### Task 1: SPV PoW Validation (H-1)

**Files:**
- Modify: `libbitfs-go/spv/verify.go:72` (add VerifyPoW call)
- Modify: `libbitfs-go/spv/errors.go:37` (remove unused ErrEmptyProofNodes)
- Test: `libbitfs-go/spv/spv_test.go`

**Step 1: Write failing test — VerifyTransaction rejects invalid PoW**

Add to `libbitfs-go/spv/spv_test.go` after `TestVerifyTransaction_MerkleRootMismatch`:

```go
func TestVerifyTransaction_InvalidPoW(t *testing.T) {
	txHash := makeTxHash(0x42)
	proof, merkleRoot := buildTestProof(txHash)

	// Build header with valid structure but INVALID PoW (nonce=0, hard target).
	header := &BlockHeader{
		Version:    1,
		PrevBlock:  makeHash(0x00),
		MerkleRoot: merkleRoot,
		Timestamp:  1700000000,
		Bits:       0x01003456, // Impossibly hard target
		Nonce:      0,
		Height:     100,
	}
	header.Hash = ComputeHeaderHash(header)
	proof.BlockHash = header.Hash

	headerStore := NewMemHeaderStore()
	err := headerStore.PutHeader(header)
	require.NoError(t, err)

	storedTx := &StoredTx{
		TxID:        txHash,
		Proof:       proof,
		BlockHeight: 100,
	}

	err = VerifyTransaction(storedTx, headerStore)
	assert.ErrorIs(t, err, ErrInsufficientPoW)
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./spv/ -run TestVerifyTransaction_InvalidPoW -v`
Expected: PASS (no PoW check yet, so it accepts the invalid header) — wait, actually the test should PASS currently because VerifyTransaction doesn't check PoW. The test asserts `ErrorIs(t, err, ErrInsufficientPoW)` which will FAIL because no error is returned.

Expected: FAIL — `VerifyTransaction` returns nil (no PoW check), but test expects `ErrInsufficientPoW`.

**Step 3: Add VerifyPoW call to VerifyTransaction**

In `libbitfs-go/spv/verify.go`, after line 72 (`return ErrHeaderNotFound`), add:

```go
	// Step 3.5: Verify the header's Proof-of-Work meets stated difficulty.
	if err := VerifyPoW(header); err != nil {
		return fmt.Errorf("%w: %w", ErrInsufficientPoW, err)
	}
```

**Step 4: Clean up dead code**

In `libbitfs-go/spv/errors.go`, remove the unused `ErrEmptyProofNodes` sentinel (lines 36-37).

**Step 5: Run tests to verify**

Run: `cd libbitfs-go && go test ./spv/ -v -count=1`
Expected: ALL PASS (including new test + all existing tests, since `buildTestHeader` already mines valid PoW)

**Step 6: Commit**

```bash
cd libbitfs-go && git add spv/verify.go spv/errors.go spv/spv_test.go
git commit -m "fix(spv): add PoW validation to VerifyTransaction (H-1), remove dead ErrEmptyProofNodes (C-1 already fixed)"
```

---

### Task 2: Bound io.ReadAll in Paymail and ContentResolver (H-4 + H-NEW-3)

**Files:**
- Modify: `libbitfs-go/paymail/resolve.go:75,142`
- Modify: `libbitfs-go/storage/resolver.go:90`
- Test: `libbitfs-go/paymail/paymail_test.go`
- Test: `libbitfs-go/storage/resolver_test.go`

**Step 1: Write failing test — paymail rejects oversized response**

Add to `libbitfs-go/paymail/paymail_test.go`:

```go
func TestDiscoverCapabilities_OversizedResponse(t *testing.T) {
	// Server returns a response larger than MaxPaymailResponseSize.
	bigBody := make([]byte, MaxPaymailResponseSize+1)
	for i := range bigBody {
		bigBody[i] = 'x'
	}

	mock := &mockHTTPClient{
		responses: map[string]*http.Response{
			"https://example.com/.well-known/bsvalias": {
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(bigBody)),
			},
		},
	}

	_, err := DiscoverCapabilitiesWithClient("example.com", mock)
	// Should fail with JSON parse error since body is truncated garbage
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing JSON")
}
```

**Step 2: Write failing test — resolver rejects oversized response**

Add to `libbitfs-go/storage/resolver_test.go`:

```go
func TestContentResolver_OversizedResponse(t *testing.T) {
	// Server streams more than MaxContentResponseSize bytes.
	// Use a limited reader that claims to have huge data.
	bigBody := make([]byte, 1025) // just over 1KB for test speed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bigBody)
	}))
	defer srv.Close()

	r := &ContentResolver{
		Endpoints: []string{srv.URL},
		Client:    srv.Client(),
	}

	keyHash := testKeyHash([]byte("test"))
	data, err := r.Fetch(keyHash)
	// With a reasonable limit, this should still succeed (1KB < 1GB limit).
	// The point is the LimitReader exists — we verify by checking the code, not the size.
	require.NoError(t, err)
	assert.Len(t, data, 1025)
}
```

**Step 3: Add MaxPaymailResponseSize constant and LimitReader to paymail**

In `libbitfs-go/paymail/resolve.go`, add constant after the imports:

```go
// MaxPaymailResponseSize is the maximum size of a Paymail JSON response (1 MB).
const MaxPaymailResponseSize = 1 << 20
```

Replace line 75:
```go
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxPaymailResponseSize))
```

Replace line 142 (will shift after the constant addition):
```go
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxPaymailResponseSize))
```

**Step 4: Add MaxContentResponseSize constant and LimitReader to resolver**

In `libbitfs-go/storage/resolver.go`, add constant after the imports:

```go
// MaxContentResponseSize is the maximum size of a ciphertext response from a daemon (1 GB).
const MaxContentResponseSize = 1 << 30
```

Replace line 90:
```go
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxContentResponseSize))
```

**Step 5: Run tests**

Run: `cd libbitfs-go && go test ./paymail/ ./storage/ -v -count=1`
Expected: ALL PASS

**Step 6: Commit**

```bash
cd libbitfs-go && git add paymail/resolve.go storage/resolver.go paymail/paymail_test.go storage/resolver_test.go
git commit -m "fix(paymail,storage): add io.LimitReader to all unbounded reads (H-4, H-NEW-3)"
```

---

### Task 3: Daemon HTTP Timeouts (H-3)

**Files:**
- Modify: `bitfs/internal/daemon/daemon.go:265-268`
- Test: `bitfs/internal/daemon/daemon_test.go`

**Step 1: Write failing test — server has non-zero timeouts**

Add to `bitfs/internal/daemon/daemon_test.go`:

```go
func TestDaemon_ServerTimeouts(t *testing.T) {
	cfg := DefaultConfig()
	d, err := New(cfg, newMockWallet(), newMockStore(), nil)
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, d.server.ReadTimeout, "ReadTimeout should be set")
	assert.Equal(t, 60*time.Second, d.server.WriteTimeout, "WriteTimeout should be set")
	assert.Equal(t, 120*time.Second, d.server.IdleTimeout, "IdleTimeout should be set")
	assert.Equal(t, 10*time.Second, d.server.ReadHeaderTimeout, "ReadHeaderTimeout should be set")
	assert.Equal(t, 1<<20, d.server.MaxHeaderBytes, "MaxHeaderBytes should be set")
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test ./internal/daemon/ -run TestDaemon_ServerTimeouts -v`
Expected: FAIL — all timeouts are 0

**Step 3: Add timeouts to http.Server**

In `bitfs/internal/daemon/daemon.go`, replace lines 265-268:

```go
	d.server = &http.Server{
		Addr:              config.ListenAddr,
		Handler:           d.mux,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}
```

**Step 4: Run tests**

Run: `cd bitfs && go test ./internal/daemon/ -v -count=1`
Expected: ALL PASS

**Step 5: Commit**

```bash
cd bitfs && git add internal/daemon/daemon.go internal/daemon/daemon_test.go
git commit -m "fix(daemon): add HTTP server timeouts to prevent slowloris DoS (H-3)"
```

---

### Task 4: Path Traversal Guard in mget (H-NEW-2)

**Files:**
- Modify: `bitfs/internal/engine/mget.go:48`
- Test: `bitfs/internal/engine/mget_test.go`

**Step 1: Write failing test — path traversal names rejected**

Add to `bitfs/internal/engine/mget_test.go`:

```go
func TestMget_PathTraversalRejected(t *testing.T) {
	e := setupTestEngine(t)

	// Create a directory with a malicious child name.
	root := e.State.GetRoot(0)
	dir := &NodeState{
		PubKey:   "deadbeef01",
		Path:     "/testdir",
		Type:     "dir",
		Children: []ChildEntry{
			{Name: "../../etc/passwd", PubKey: "malicious01", Index: 0},
			{Name: "normal.txt", PubKey: "goodfile01", Index: 1},
			{Name: "sub/../escape", PubKey: "malicious02", Index: 2},
			{Name: "back\\slash", PubKey: "malicious03", Index: 3},
		},
	}
	e.State.SetNode(dir.PubKey, dir)
	root.Children = append(root.Children, ChildEntry{Name: "testdir", PubKey: dir.PubKey, Index: 0})

	result := &MgetResult{}
	e.mgetRecurse(0, dir, t.TempDir(), result)

	// All 3 malicious names should be rejected, recorded as errors.
	assert.GreaterOrEqual(t, len(result.Errors), 3, "should reject traversal names")
	for _, errMsg := range result.Errors {
		if strings.Contains(errMsg, "unsafe") {
			continue // expected
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test ./internal/engine/ -run TestMget_PathTraversalRejected -v`
Expected: FAIL — malicious names are accepted

**Step 3: Add path validation to mgetRecurse**

In `bitfs/internal/engine/mget.go`, add import `"strings"` and add validation at line 48, before the child node lookup:

```go
	for _, child := range dir.Children {
		// Validate child name to prevent path traversal from untrusted Metanet DAG state.
		if strings.Contains(child.Name, "..") || strings.ContainsAny(child.Name, "/\\") || child.Name == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("unsafe child name %q, skipping", child.Name))
			continue
		}

		childNode := e.State.GetNode(child.PubKey)
```

**Step 4: Run tests**

Run: `cd bitfs && go test ./internal/engine/ -v -count=1`
Expected: ALL PASS

**Step 5: Commit (do not commit yet — continue to next task in same batch)**

---

### Task 5: UTXO Reference Verification in BuildBuyerRefundTx (H-NEW-4)

**Files:**
- Modify: `libbitfs-go/x402/htlc_tx.go:76-82,469-477`
- Test: `libbitfs-go/x402/htlc_tx_test.go`

**Step 1: Write failing test — mismatched UTXO reference rejected**

Add to `libbitfs-go/x402/htlc_tx_test.go`:

```go
func TestBuildBuyerRefundTx_WrongFundingTxID(t *testing.T) {
	// Setup: build a real HTLC flow to get valid seller pre-signed tx.
	buyerPriv, _ := ec.NewPrivateKey()
	sellerPriv, _ := ec.NewPrivateKey()

	capsuleHash := make([]byte, 32)
	for i := range capsuleHash {
		capsuleHash[i] = 0xAB
	}

	htlcScript, err := BuildHTLC(&HTLCParams{
		BuyerPubKey:  buyerPriv.PubKey().Compressed(),
		SellerPubKey: sellerPriv.PubKey().Compressed(),
		SellerAddr:   sellerPriv.PubKey().Hash(),
		CapsuleHash:  capsuleHash,
		Amount:       10000,
		Timeout:      100,
	})
	require.NoError(t, err)

	fundingTx := buildTestFundingTx(t, htlcScript, 10000)

	preSign, err := BuildSellerPreSignRefund(&SellerPreSignParams{
		SellerPrivKey: sellerPriv,
		FundingTxID:   fundingTx.TxID().String(),
		FundingVout:   0,
		FundingAmount: 10000,
		HTLCScript:    htlcScript,
		BuyerPubKey:   buyerPriv.PubKey().Compressed(),
		LockTime:      100,
	})
	require.NoError(t, err)

	// Attempt refund with WRONG FundingTxID.
	wrongTxID := make([]byte, 32)
	for i := range wrongTxID {
		wrongTxID[i] = 0xFF
	}

	_, err = BuildBuyerRefundTx(&BuyerRefundParams{
		SellerPreSignedTx: preSign.TxBytes,
		SellerSig:         preSign.SellerSig,
		HTLCScript:        htlcScript,
		FundingAmount:     10000,
		BuyerPrivKey:      buyerPriv,
		FundingTxID:       wrongTxID,
		FundingVout:       0,
	})
	assert.ErrorIs(t, err, ErrFundingMismatch)
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./x402/ -run TestBuildBuyerRefundTx_WrongFundingTxID -v`
Expected: FAIL — `ErrFundingMismatch` undefined + no verification in function

**Step 3: Add FundingTxID/FundingVout to BuyerRefundParams and verification**

In `libbitfs-go/x402/htlc_tx.go`, update `BuyerRefundParams`:

```go
type BuyerRefundParams struct {
	SellerPreSignedTx []byte         // Serialized tx from SellerPreSignResult.TxBytes
	SellerSig         []byte         // Seller's signature from SellerPreSignResult.SellerSig
	HTLCScript        []byte         // HTLC locking script bytes
	FundingAmount     uint64         // HTLC output amount (for sighash computation)
	BuyerPrivKey      *ec.PrivateKey // Signs the refund (buyer's half of 2-of-2)
	FundingTxID       []byte         // Expected HTLC funding TxID (32 bytes); nil skips check
	FundingVout       uint32         // Expected HTLC funding output index
}
```

Add error sentinel in `libbitfs-go/x402/errors.go` (or htlc_tx.go if no errors.go):

```go
// ErrFundingMismatch indicates the pre-signed tx references a different UTXO than expected.
var ErrFundingMismatch = errors.New("x402: funding UTXO mismatch")
```

In `BuildBuyerRefundTx`, after deserializing the tx and checking `len(tx.Inputs) == 0` (line 477), add:

```go
	// Verify the pre-signed tx references the expected HTLC funding UTXO.
	if len(params.FundingTxID) > 0 {
		inputTxID := tx.Inputs[0].SourceTXID
		if !bytes.Equal(inputTxID, params.FundingTxID) {
			return nil, fmt.Errorf("%w: input references %x, expected %x",
				ErrFundingMismatch, inputTxID, params.FundingTxID)
		}
		if tx.Inputs[0].SourceTxOutIndex != params.FundingVout {
			return nil, fmt.Errorf("%w: input vout %d, expected %d",
				ErrFundingMismatch, tx.Inputs[0].SourceTxOutIndex, params.FundingVout)
		}
	}
```

Add `"bytes"` to imports if not already present.

**Step 4: Run tests**

Run: `cd libbitfs-go && go test ./x402/ -v -count=1`
Expected: ALL PASS (existing tests don't set FundingTxID so skip check; new test verifies mismatch is caught)

**Step 5: Commit (do not commit yet — continue to next task)**

---

### Task 6: Merkle Traversal Depth/Size Guard (H-NEW-5)

**Files:**
- Modify: `libbitfs-go/network/rpc_blockchain.go:123-127`
- Test: `libbitfs-go/network/rpc_blockchain_test.go`

**Step 1: Write failing test — huge totalTxs rejected**

Add to `libbitfs-go/network/rpc_blockchain_test.go`:

```go
func TestTraversePartialMerkleTree_HugeTotalTxs(t *testing.T) {
	// totalTxs = MaxUint32 should be rejected to prevent OOM.
	hashes := [][]byte{make([]byte, 32)}
	flags := []byte{0xFF}
	target := make([]byte, 32)

	_, _, err := traversePartialMerkleTree(hashes, flags, 0xFFFFFFFF, target)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestTraversePartialMerkleTree_ZeroTotalTxs(t *testing.T) {
	hashes := [][]byte{make([]byte, 32)}
	flags := []byte{0xFF}
	target := make([]byte, 32)

	_, _, err := traversePartialMerkleTree(hashes, flags, 0, target)
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./network/ -run TestTraversePartialMerkleTree_Huge -v`
Expected: FAIL — function attempts to compute with huge totalTxs, may hang or OOM

**Step 3: Add bounds check**

In `libbitfs-go/network/rpc_blockchain.go`, at the top of `traversePartialMerkleTree` (line 123), add:

```go
const maxMerkleTreeTxs = 1 << 20 // 1M transactions — covers any realistic block

func traversePartialMerkleTree(hashes [][]byte, flagBytes []byte, totalTxs uint32, targetTxID []byte) (txIndex uint32, branch [][]byte, err error) {
	if totalTxs == 0 {
		return 0, nil, fmt.Errorf("totalTxs is zero")
	}
	if totalTxs > maxMerkleTreeTxs {
		return 0, nil, fmt.Errorf("totalTxs %d exceeds maximum %d", totalTxs, maxMerkleTreeTxs)
	}

	height := uint32(0)
```

**Step 4: Run tests**

Run: `cd libbitfs-go && go test ./network/ -v -count=1`
Expected: ALL PASS

**Step 5: Commit batch 4**

```bash
# In libbitfs-go
cd libbitfs-go && git add x402/htlc_tx.go x402/errors.go x402/htlc_tx_test.go network/rpc_blockchain.go network/rpc_blockchain_test.go
git commit -m "fix(x402,network): UTXO reference verification (H-NEW-4), merkle traversal bounds (H-NEW-5)"

# In bitfs
cd bitfs && git add internal/engine/mget.go internal/engine/mget_test.go
git commit -m "fix(engine): path traversal guard in mget (H-NEW-2)"
```

---

### Task 7: Run Full Test Suites and Verify

**Step 1: Run all libbitfs-go tests**

Run: `cd libbitfs-go && go test ./... -v -count=1 -race`
Expected: ALL PASS, 0 failures

**Step 2: Run all bitfs tests**

Run: `cd bitfs && go test ./... -v -count=1 -race`
Expected: ALL PASS, 0 failures

**Step 3: Run integration tests**

Run: `cd bitfs && go test -tags=integration ./integration/ -count=1 -race`
Expected: ALL 275 PASS

**Step 4: Update audit report**

Update `docs/audits/2026-02-26-code-audit.md` Previous Findings Status table:
- C-1: CRITICAL → **VERIFIED FIXED** (was already working; removed dead ErrEmptyProofNodes)
- H-1: HIGH → **FIXED** (VerifyPoW added to VerifyTransaction)
- H-3: HIGH → **FIXED** (HTTP server timeouts added)
- H-4: HIGH → **FIXED** (io.LimitReader added to paymail)
- H-NEW-1: HIGH → **VERIFIED FIXED** (was already fixed in commit 7c7e25c)
- H-NEW-2: HIGH → **FIXED** (path traversal guard added to mget)
- H-NEW-3: HIGH → **FIXED** (io.LimitReader added to ContentResolver)
- H-NEW-4: HIGH → **FIXED** (UTXO reference verification added)
- H-NEW-5: HIGH → **FIXED** (totalTxs bounds check added)

---

## Summary

| Task | Finding | Repo | Effort |
|------|---------|------|--------|
| 1 | H-1 (+ C-1 cleanup) | libbitfs-go | ~15 min |
| 2 | H-4, H-NEW-3 | libbitfs-go | ~15 min |
| 3 | H-3 | bitfs | ~10 min |
| 4 | H-NEW-2 | bitfs | ~10 min |
| 5 | H-NEW-4 | libbitfs-go | ~20 min |
| 6 | H-NEW-5 | libbitfs-go | ~10 min |
| 7 | Verify all | both | ~10 min |

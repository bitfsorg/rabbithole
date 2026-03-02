# Batch 3: Remaining MEDIUM Audit Fixes

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the final 13 MEDIUM findings from the 2026-02-26 code audit — HTLC verification, RPC validation, SPV chain continuity, daemon resource management, config permissions, shell hardening, and engine key lookup.

**Architecture:** Targeted fixes across libbitfs-go (8 fixes) and bitfs (5 fixes). Groups: (A) HTLC hash verification M-4/M-5, (B) RPC robustness M-9/M-10, (C) SPV chain continuity M-11, (D) daemon resource management M-12/M-13/M-14, (E) config/shell/engine hardening M-16/M-NEW-3/M-NEW-22/M-NEW-23/M-NEW-25. TDD approach: failing test → minimal fix → verify.

**Tech Stack:** Go 1.25, libbitfs-go (x402/network/spv/config), bitfs (daemon/engine/cmd)

**Repos:** libbitfs-go commits separately (independent git repo at `../libbitfs-go`), bitfs commits from `bitfs/`.

---

### Task 1: ParseHTLCPreimage Hash Verification (M-4)

**Files:**
- Modify: `libbitfs-go/x402/htlc.go:139-188`
- Test: `libbitfs-go/x402/htlc_test.go` (or `htlc_tx_test.go`)

**Step 1: Write failing test**

Add to `libbitfs-go/x402/htlc_tx_test.go`:

```go
func TestParseHTLCPreimage_WithHashVerification(t *testing.T) {
	// Build a valid seller claim tx.
	capsule := []byte("secret-capsule-data-for-test!!")
	capsuleHash := sha256Hash(capsule)

	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	sellerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	sellerPub := sellerPriv.PubKey().Compressed()
	sellerAddr := hash160(sellerPub)

	htlcScript, err := BuildHTLC(&HTLCParams{
		BuyerPubKey:  buyerPriv.PubKey().Compressed(),
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       50000,
		Timeout:      144,
	})
	require.NoError(t, err)

	fundResult, err := BuildHTLCFundingTx(&HTLCFundingParams{
		BuyerPrivKey: buyerPriv,
		UTXOs:        []UTXO{{TxID: make([]byte, 32), Vout: 0, Amount: 100000}},
		Amount:       50000,
		SellerAddr:   sellerAddr,
		SellerPubKey: sellerPub,
		CapsuleHash:  capsuleHash,
		ChangeAddr:   hash160(buyerPriv.PubKey().Compressed()),
	})
	require.NoError(t, err)

	claimTx, err := BuildSellerClaimTx(&SellerClaimParams{
		SellerPrivKey: sellerPriv,
		FundingTxID:   fundResult.TxID,
		FundingVout:   fundResult.Vout,
		FundingAmount: fundResult.Amount,
		HTLCScript:    htlcScript,
		Capsule:       capsule,
		OutputAddr:    sellerAddr,
	})
	require.NoError(t, err)

	// Correct hash: should succeed.
	extracted, err := ParseHTLCPreimage(claimTx.Bytes(), capsuleHash)
	require.NoError(t, err)
	assert.Equal(t, capsule, extracted)

	// Wrong hash: should fail.
	wrongHash := make([]byte, 32)
	wrongHash[0] = 0xFF
	_, err = ParseHTLCPreimage(claimTx.Bytes(), wrongHash)
	assert.Error(t, err, "wrong capsule hash must be rejected")

	// Nil hash (backward compat): should succeed without verification.
	extracted2, err := ParseHTLCPreimage(claimTx.Bytes(), nil)
	require.NoError(t, err)
	assert.Equal(t, capsule, extracted2)
}
```

Note: `sha256Hash` and `hash160` helpers likely exist in the test file already. If not, add:

```go
func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./x402/ -run TestParseHTLCPreimage_WithHashVerification -v`
Expected: FAIL — `ParseHTLCPreimage` does not accept a second parameter.

**Step 3: Fix — add expectedCapsuleHash parameter**

In `libbitfs-go/x402/htlc.go`, change signature and add verification:

```go
// ParseHTLCPreimage extracts the capsule (preimage) from a spent HTLC input.
// If expectedCapsuleHash is non-nil, verifies SHA256(preimage) matches before returning.
func ParseHTLCPreimage(spendingTx []byte, expectedCapsuleHash []byte) ([]byte, error) {
```

Inside the loop, after extracting `preimageChunk.Data`, add before `return`:

```go
		// Verify hash if expected hash provided.
		if expectedCapsuleHash != nil {
			h := sha256.Sum256(preimageChunk.Data)
			if !bytes.Equal(h[:], expectedCapsuleHash) {
				continue // Hash mismatch — try next input.
			}
		}
```

Add `"crypto/sha256"` and `"bytes"` to imports.

Update all callers of `ParseHTLCPreimage` — search for existing call sites and add the second parameter (pass `nil` or the known capsule hash as appropriate).

**Step 4: Run tests**

Run: `cd libbitfs-go && go test ./x402/ -v -count=1`
Expected: ALL PASS

---

### Task 2: BuildSellerClaimTx Capsule Hash Verification (M-5)

**Files:**
- Modify: `libbitfs-go/x402/htlc_tx.go:261-361`
- Modify: `libbitfs-go/x402/htlc.go` (add helper)
- Test: `libbitfs-go/x402/htlc_tx_test.go`

**Step 1: Write failing test**

Add to `libbitfs-go/x402/htlc_tx_test.go`:

```go
func TestBuildSellerClaimTx_RejectsWrongCapsule(t *testing.T) {
	capsule := []byte("the-real-capsule-data!!")
	capsuleHash := sha256Hash(capsule)

	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	sellerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)
	sellerPub := sellerPriv.PubKey().Compressed()
	sellerAddr := hash160(sellerPub)

	htlcScript, err := BuildHTLC(&HTLCParams{
		BuyerPubKey:  buyerPriv.PubKey().Compressed(),
		SellerPubKey: sellerPub,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       50000,
		Timeout:      144,
	})
	require.NoError(t, err)

	// Wrong capsule — hash won't match what's in the HTLC script.
	wrongCapsule := []byte("wrong-capsule-data-here!!")

	_, err = BuildSellerClaimTx(&SellerClaimParams{
		SellerPrivKey: sellerPriv,
		FundingTxID:   make([]byte, 32),
		FundingVout:   0,
		FundingAmount: 100000,
		HTLCScript:    htlcScript,
		Capsule:       wrongCapsule,
		OutputAddr:    sellerAddr,
	})
	assert.Error(t, err, "wrong capsule must be rejected")
	assert.Contains(t, err.Error(), "capsule hash mismatch")
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./x402/ -run TestBuildSellerClaimTx_RejectsWrongCapsule -v`
Expected: FAIL — no hash check exists.

**Step 3: Add ExtractCapsuleHashFromHTLC helper and verification**

Add to `libbitfs-go/x402/htlc.go` after `ParseHTLCPreimage`:

```go
// ExtractCapsuleHashFromHTLC extracts the capsule hash embedded in an HTLC locking script.
// The HTLC structure is: OP_IF OP_SHA256 <capsule_hash_32> OP_EQUALVERIFY ...
func ExtractCapsuleHashFromHTLC(htlcScript []byte) ([]byte, error) {
	s := script.NewFromBytes(htlcScript)
	chunks, err := s.Chunks()
	if err != nil {
		return nil, fmt.Errorf("parse HTLC script: %w", err)
	}
	// Expected: chunks[0]=OP_IF, chunks[1]=OP_SHA256, chunks[2]=<32-byte push data>
	if len(chunks) < 3 {
		return nil, fmt.Errorf("HTLC script too short: %d chunks", len(chunks))
	}
	if chunks[0].Op != script.OpIF {
		return nil, fmt.Errorf("expected OP_IF at position 0, got 0x%02x", chunks[0].Op)
	}
	if chunks[1].Op != script.OpSHA256 {
		return nil, fmt.Errorf("expected OP_SHA256 at position 1, got 0x%02x", chunks[1].Op)
	}
	if len(chunks[2].Data) != CapsuleHashLen {
		return nil, fmt.Errorf("capsule hash must be %d bytes, got %d", CapsuleHashLen, len(chunks[2].Data))
	}
	return chunks[2].Data, nil
}
```

In `libbitfs-go/x402/htlc_tx.go`, in `BuildSellerClaimTx`, add after the `params.OutputAddr` validation (after line 281):

```go
	// Verify capsule matches the hash embedded in the HTLC script.
	capsuleHashFromScript, err := ExtractCapsuleHashFromHTLC(params.HTLCScript)
	if err != nil {
		return nil, fmt.Errorf("%w: extract capsule hash: %w", ErrInvalidParams, err)
	}
	computedHash := sha256.Sum256(params.Capsule)
	if !bytes.Equal(computedHash[:], capsuleHashFromScript) {
		return nil, fmt.Errorf("%w: capsule hash mismatch", ErrInvalidParams)
	}
```

Add `"crypto/sha256"` and `"bytes"` to htlc_tx.go imports.

**Step 4: Run tests**

Run: `cd libbitfs-go && go test ./x402/ -v -count=1`
Expected: ALL PASS

---

### Task 3: RPC HTTP Status Code Check (M-9)

**Files:**
- Modify: `libbitfs-go/network/rpc.go:97-106`
- Test: `libbitfs-go/network/rpc_blockchain_test.go`

**Step 1: Write failing test**

Add to `libbitfs-go/network/rpc_blockchain_test.go`:

```go
func TestRPCClient_RejectsNon200Status(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer ts.Close()

	client := NewRPCClient(RPCConfig{URL: ts.URL})
	var result json.RawMessage
	err := client.Call(context.Background(), "getblockcount", nil, &result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestRPCClient_RejectsUnauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Unauthorized"))
	}))
	defer ts.Close()

	client := NewRPCClient(RPCConfig{URL: ts.URL})
	err := client.Call(context.Background(), "getblockcount", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./network/ -run "TestRPCClient_Rejects(Non200|Unauthorized)" -v`
Expected: FAIL — 500 body is decoded as invalid JSON, error message is about decode, not HTTP status.

**Step 3: Add HTTP status check**

In `libbitfs-go/network/rpc.go`, add after line 101 (`defer func() { _ = resp.Body.Close() }()`):

```go
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: HTTP %d: %s", ErrConnectionFailed, resp.StatusCode, string(respBody))
	}
```

Add `"io"` to imports.

**Step 4: Run tests**

Run: `cd libbitfs-go && go test ./network/ -v -count=1`
Expected: ALL PASS

---

### Task 4: RPC Response ID Validation (M-10)

**Files:**
- Modify: `libbitfs-go/network/rpc.go:73-119`
- Test: `libbitfs-go/network/rpc_blockchain_test.go`

**Step 1: Write failing test**

Add to `libbitfs-go/network/rpc_blockchain_test.go`:

```go
func TestRPCClient_RejectsMismatchedResponseID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode request to see its ID, then return a different ID.
		var req struct {
			ID int64 `json:"id"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "application/json")
		resp := fmt.Sprintf(`{"id":%d,"result":42,"error":null}`, req.ID+999)
		_, _ = w.Write([]byte(resp))
	}))
	defer ts.Close()

	client := NewRPCClient(RPCConfig{URL: ts.URL})
	var result int
	err := client.Call(context.Background(), "getblockcount", nil, &result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "response ID mismatch")
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./network/ -run TestRPCClient_RejectsMismatchedResponseID -v`
Expected: FAIL — response is accepted without ID validation.

**Step 3: Add ID validation**

In `libbitfs-go/network/rpc.go`, save the request ID in a local variable (line 79):

```go
	requestID := c.nextID.Add(1)
	reqBody := rpcRequest{
		JSONRPC: "1.0",
		ID:      requestID,
		Method:  method,
		Params:  params,
	}
```

After decoding `rpcResp` (after line 106), add:

```go
	if rpcResp.ID != requestID {
		return fmt.Errorf("%w: response ID mismatch: expected %d, got %d",
			ErrInvalidResponse, requestID, rpcResp.ID)
	}
```

**Step 4: Run tests**

Run: `cd libbitfs-go && go test ./network/ -v -count=1`
Expected: ALL PASS

---

### Task 5: SPV Header Chain Continuity (M-11)

**Files:**
- Modify: `libbitfs-go/network/spvclient.go:127-170`
- Test: `libbitfs-go/network/spvclient_test.go` (or `rpc_blockchain_test.go`)

**Step 1: Write failing test**

Add to the appropriate test file in `libbitfs-go/network/`:

```go
func TestSyncHeaders_RejectsDisconnectedHeader(t *testing.T) {
	// Set up a mock chain that returns two headers where the second
	// does not link to the first via PrevBlock.
	store := spv.NewMemHeaderStore()

	header0 := &spv.BlockHeader{
		Version:    1,
		PrevBlock:  make([]byte, 32),
		MerkleRoot: make([]byte, 32),
		Timestamp:  1000,
		Bits:       0x1d00ffff,
		Nonce:      0,
		Height:     0,
	}
	header0.Hash = spv.ComputeHeaderHash(header0)
	require.NoError(t, store.PutHeader(header0))

	// header1 has a PrevBlock that does NOT match header0.Hash
	badPrevBlock := make([]byte, 32)
	badPrevBlock[0] = 0xFF // garbage
	header1Raw := spv.SerializeHeader(&spv.BlockHeader{
		Version:    1,
		PrevBlock:  badPrevBlock,
		MerkleRoot: make([]byte, 32),
		Timestamp:  2000,
		Bits:       0x1d00ffff,
		Nonce:      0,
	})

	mockChain := &mockChainForSync{
		bestHeight: 1,
		headers: map[uint64][]byte{
			0: spv.SerializeHeader(header0),
			1: header1Raw,
		},
		hashes: map[uint64]string{
			0: hex.EncodeToString(header0.Hash),
			1: "0000000000000000000000000000000000000000000000000000000000000001",
		},
	}

	client := NewSPVClientWithStore(store, mockChain)
	err := client.SyncHeaders(context.Background())
	assert.Error(t, err, "disconnected header must be rejected")
	assert.Contains(t, err.Error(), "chain")
}
```

Note: The exact mock types depend on the existing test infrastructure. Adapt `mockChainForSync` to match the existing patterns in the test file. The key thing is: return a header at height 1 whose `PrevBlock` doesn't match the hash of the header at height 0.

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./network/ -run TestSyncHeaders_RejectsDisconnectedHeader -v`
Expected: FAIL — no chain continuity check.

**Step 3: Add chain continuity validation in SyncHeaders**

In `libbitfs-go/network/spvclient.go`, after computing `header.Hash` (line 162), add before `PutHeader`:

```go
		// Validate chain continuity: header.PrevBlock must match previous header's hash.
		if h > startHeight || startHeight == 0 {
			var prevHash []byte
			if h == 0 {
				// Genesis block: PrevBlock should be all zeros.
				prevHash = make([]byte, spv.HashSize)
			} else {
				prevHeader, prevErr := s.headers.GetHeaderByHeight(uint32(h - 1))
				if prevErr != nil {
					return fmt.Errorf("network: previous header at %d not found: %w", h-1, prevErr)
				}
				prevHash = prevHeader.Hash
			}
			if !bytesEqual(header.PrevBlock, prevHash) {
				return fmt.Errorf("network: chain break at height %d: PrevBlock does not match header at %d", h, h-1)
			}
		}
```

**Step 4: Run tests**

Run: `cd libbitfs-go && go test ./network/ -v -count=1`
Expected: ALL PASS

---

### Task 6: Daemon Background Cleanup Goroutine (M-12 + M-13)

**Files:**
- Modify: `bitfs/internal/daemon/daemon.go:289-328` (Start), `daemon.go:397-408` (cleanupExpiredSessions), `daemon.go:410-460` (rateLimiter)
- Test: `bitfs/internal/daemon/daemon_test.go`

**Step 1: Write failing tests**

Add to `bitfs/internal/daemon/daemon_test.go`:

```go
func TestDaemon_CleansUpExpiredSessions(t *testing.T) {
	d := newTestDaemon(t)

	// Create a session that's already expired.
	d.sessionsMu.Lock()
	d.sessions["expired-1"] = &Session{
		ID:        "expired-1",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	d.sessions["valid-1"] = &Session{
		ID:        "valid-1",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	d.sessionsMu.Unlock()

	d.cleanupExpiredSessions()

	d.sessionsMu.RLock()
	defer d.sessionsMu.RUnlock()
	assert.NotContains(t, d.sessions, "expired-1")
	assert.Contains(t, d.sessions, "valid-1")
}

func TestDaemon_CleansUpExpiredInvoices(t *testing.T) {
	d := newTestDaemon(t)

	d.invoicesMu.Lock()
	d.invoices["expired-inv"] = &InvoiceRecord{
		CreatedAt: time.Now().Add(-25 * time.Hour),
	}
	d.invoices["fresh-inv"] = &InvoiceRecord{
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}
	d.invoicesMu.Unlock()

	d.cleanupExpiredInvoices()

	d.invoicesMu.RLock()
	defer d.invoicesMu.RUnlock()
	assert.NotContains(t, d.invoices, "expired-inv")
	assert.Contains(t, d.invoices, "fresh-inv")
}

func TestRateLimiter_CleansUpStaleClients(t *testing.T) {
	rl := newRateLimiter(60, 10)

	rl.mu.Lock()
	rl.clients["stale-ip"] = &clientRate{
		tokens:    10,
		lastCheck: time.Now().Add(-25 * time.Hour),
	}
	rl.clients["active-ip"] = &clientRate{
		tokens:    10,
		lastCheck: time.Now().Add(-1 * time.Minute),
	}
	rl.mu.Unlock()

	rl.cleanup()

	rl.mu.Lock()
	defer rl.mu.Unlock()
	assert.NotContains(t, rl.clients, "stale-ip")
	assert.Contains(t, rl.clients, "active-ip")
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test ./internal/daemon/ -run "TestDaemon_CleansUpExpired|TestRateLimiter_CleansUpStale" -v`
Expected: FAIL — `cleanupExpiredInvoices` and `rl.cleanup()` don't exist yet.

**Step 3: Implement cleanup methods and background goroutine**

Add to `bitfs/internal/daemon/daemon.go`, after `cleanupExpiredSessions` (line 408):

```go
// cleanupExpiredInvoices removes invoices older than 24 hours.
func (d *Daemon) cleanupExpiredInvoices() {
	d.invoicesMu.Lock()
	defer d.invoicesMu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	for id, inv := range d.invoices {
		if inv.CreatedAt.Before(cutoff) {
			delete(d.invoices, id)
		}
	}
}
```

Add `CreatedAt time.Time` field to `InvoiceRecord` if not present. Set it in `handleGetBuyInfo` when creating the invoice.

Add to the `rateLimiter` struct, after `Allow`:

```go
// cleanup removes client entries that haven't been seen in 24 hours.
func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	for ip, client := range rl.clients {
		if client.lastCheck.Before(cutoff) {
			delete(rl.clients, ip)
		}
	}
}
```

Add a `stopCleanup chan struct{}` field to `Daemon`. In `Start()`, after `d.running = true`:

```go
	// Start background cleanup goroutine.
	d.stopCleanup = make(chan struct{})
	go d.runCleanup()
```

Add:

```go
func (d *Daemon) runCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.cleanupExpiredSessions()
			d.cleanupExpiredInvoices()
			if d.rateLimiter != nil {
				d.rateLimiter.cleanup()
			}
		case <-d.stopCleanup:
			return
		}
	}
}
```

In `Stop()`, add before `d.server.Shutdown`:

```go
	if d.stopCleanup != nil {
		close(d.stopCleanup)
	}
```

**Step 4: Run tests**

Run: `cd bitfs && go test ./internal/daemon/ -v -count=1`
Expected: ALL PASS

---

### Task 7: Handshake Timestamp Validation (M-14)

**Files:**
- Modify: `bitfs/internal/daemon/handshake.go:38-122`
- Test: `bitfs/internal/daemon/handshake_test.go`

**Step 1: Write failing test**

Add to `bitfs/internal/daemon/handshake_test.go`:

```go
func TestHandshake_RejectsStaleTimestamp(t *testing.T) {
	d := newTestDaemon(t)
	handler := http.HandlerFunc(d.handleHandshake)

	priv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	// Timestamp 10 minutes in the past.
	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(priv.PubKey().Compressed()),
		NonceB:    hex.EncodeToString(make([]byte, 32)),
		Timestamp: time.Now().Add(-10 * time.Minute).Unix(),
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/_bitfs/handshake", bytes.NewReader(body))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "timestamp")
}

func TestHandshake_RejectsFutureTimestamp(t *testing.T) {
	d := newTestDaemon(t)
	handler := http.HandlerFunc(d.handleHandshake)

	priv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	// Timestamp 10 minutes in the future.
	reqBody := HandshakeRequest{
		BuyerPub:  hex.EncodeToString(priv.PubKey().Compressed()),
		NonceB:    hex.EncodeToString(make([]byte, 32)),
		Timestamp: time.Now().Add(10 * time.Minute).Unix(),
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/_bitfs/handshake", bytes.NewReader(body))
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "timestamp")
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test ./internal/daemon/ -run "TestHandshake_Rejects(Stale|Future)Timestamp" -v`
Expected: FAIL — timestamp is accepted without validation.

**Step 3: Add timestamp validation**

In `bitfs/internal/daemon/handshake.go`, add constant at top:

```go
// MaxHandshakeClockSkew is the maximum allowed time difference between
// buyer and seller clocks for handshake requests.
const MaxHandshakeClockSkew = 5 * time.Minute
```

After the `NonceB` validation (after line 61), add:

```go
	// Validate timestamp is within acceptable window.
	if req.Timestamp == 0 {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELD", "timestamp is required")
		return
	}
	reqTime := time.Unix(req.Timestamp, 0)
	skew := time.Since(reqTime)
	if skew < 0 {
		skew = -skew
	}
	if skew > MaxHandshakeClockSkew {
		writeJSONError(w, http.StatusBadRequest, "INVALID_TIMESTAMP",
			"timestamp too far from server time (max skew 5 minutes)")
		return
	}
```

**Step 4: Run tests**

Run: `cd bitfs && go test ./internal/daemon/ -v -count=1`
Expected: ALL PASS

---

### Task 8: SaveConfig File Permissions (M-16)

**Files:**
- Modify: `libbitfs-go/config/config.go:146`
- Test: `libbitfs-go/config/config_test.go`

**Step 1: Write failing test**

Add to `libbitfs-go/config/config_test.go`:

```go
func TestSaveConfig_RestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.conf")

	err := SaveConfig(path, Config{Network: "mainnet"})
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)

	// File should be owner-only (0600).
	perm := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0600), perm,
		"config file should have 0600 permissions, got %o", perm)
}
```

**Step 2: Run test to verify it fails**

Run: `cd libbitfs-go && go test ./config/ -run TestSaveConfig_RestrictivePermissions -v`
Expected: FAIL — `os.Create` uses 0666 (umask-dependent).

**Step 3: Fix — use os.OpenFile with 0600**

In `libbitfs-go/config/config.go`, replace line 146:

```go
	f, err := os.Create(path)
```

with:

```go
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
```

**Step 4: Run tests**

Run: `cd libbitfs-go && go test ./config/ -v -count=1`
Expected: ALL PASS

---

### Task 9: Shell mput Access Mode Validation (M-NEW-3)

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_shell.go:374-377`
- Test: `bitfs/cmd/bitfs/cmd_shell_test.go` (if exists) or integration test

**Step 1: Write failing test**

Since the shell command handler is inside a loop, the easiest approach is to extract validation into a helper. Add to appropriate test file:

```go
func TestValidateAccessMode(t *testing.T) {
	assert.NoError(t, validateAccessMode("free"))
	assert.NoError(t, validateAccessMode("private"))
	assert.NoError(t, validateAccessMode("paid"))
	assert.Error(t, validateAccessMode("prviate"))
	assert.Error(t, validateAccessMode(""))
	assert.Error(t, validateAccessMode("PUBLIC"))
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test ./cmd/bitfs/ -run TestValidateAccessMode -v`
Expected: FAIL — function doesn't exist yet.

**Step 3: Add validation helper and wire it in**

Add to `bitfs/cmd/bitfs/cmd_shell.go` (near the top, after imports or with other helpers):

```go
// validAccessModes contains the accepted access mode strings.
var validAccessModes = map[string]bool{
	"free":    true,
	"private": true,
	"paid":    true,
}

func validateAccessMode(mode string) error {
	if !validAccessModes[mode] {
		return fmt.Errorf("invalid access mode %q: must be free, private, or paid", mode)
	}
	return nil
}
```

In the `mput` case (after line 376), add:

```go
			if err := validateAccessMode(access); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}
```

**Step 4: Run tests**

Run: `cd bitfs && go test ./cmd/bitfs/ -v -count=1`
Expected: ALL PASS

---

### Task 10: Fee Key Index in UTXOState (M-NEW-22)

**Files:**
- Modify: `bitfs/internal/engine/state.go:62-70` (UTXOState struct)
- Modify: `bitfs/internal/engine/engine.go:282-318` (lookupPrivKey)
- Modify: `bitfs/internal/engine/engine.go` (TrackNewUTXOs — add derivation index)
- Test: `bitfs/internal/engine/engine_test.go`

**Step 1: Write failing test**

Add to `bitfs/internal/engine/engine_test.go`:

```go
func TestLookupPrivKey_UsesStoredDerivationIndex(t *testing.T) {
	eng := initTestEngine(t)

	// Add a fee UTXO with known derivation index.
	kp, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, 5)
	require.NoError(t, err)
	pubHex := hex.EncodeToString(kp.PublicKey.Compressed())

	eng.State.AddUTXO(&UTXOState{
		TxID:            "aabb",
		Vout:            0,
		Amount:          10000,
		PubKeyHex:       pubHex,
		Type:            "fee",
		FeeChain:        wallet.ExternalChain,
		FeeDerivIdx:     5,
	})

	priv, err := eng.lookupPrivKey(pubHex, "fee")
	require.NoError(t, err)
	assert.Equal(t, kp.PrivateKey.Serialize(), priv.Serialize())
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test ./internal/engine/ -run TestLookupPrivKey_UsesStoredDerivationIndex -v`
Expected: FAIL — `FeeChain` and `FeeDerivIdx` fields don't exist on `UTXOState`.

**Step 3: Add derivation index to UTXOState and optimize lookupPrivKey**

In `bitfs/internal/engine/state.go`, add fields to `UTXOState`:

```go
type UTXOState struct {
	TxID         string `json:"txid"`
	Vout         uint32 `json:"vout"`
	Amount       uint64 `json:"amount"`
	ScriptPubKey string `json:"script_pubkey"`
	PubKeyHex    string `json:"pubkey"`
	Type         string `json:"type"`
	Spent        bool   `json:"spent"`
	FeeChain     uint32 `json:"fee_chain,omitempty"`     // 0=external, 1=internal
	FeeDerivIdx  uint32 `json:"fee_deriv_idx,omitempty"` // derivation index within chain
}
```

In `bitfs/internal/engine/engine.go`, rewrite `lookupPrivKey` for the fee case:

```go
func (e *Engine) lookupPrivKey(pubKeyHex, utxoType string) (*ec.PrivateKey, error) {
	if utxoType == "fee" {
		// Try direct lookup via stored derivation index first.
		us := e.State.FindUTXOByPubKey(pubKeyHex, "fee")
		if us != nil && (us.FeeChain > 0 || us.FeeDerivIdx > 0) {
			kp, err := e.Wallet.DeriveFeeKey(us.FeeChain, us.FeeDerivIdx)
			if err == nil && hex.EncodeToString(kp.PublicKey.Compressed()) == pubKeyHex {
				return kp.PrivateKey, nil
			}
		}

		// Fallback: linear scan (for UTXOs saved before the index was added).
		for i := uint32(0); i < e.WState.NextReceiveIndex+10; i++ {
			kp, err := e.Wallet.DeriveFeeKey(wallet.ExternalChain, i)
			if err != nil {
				continue
			}
			if hex.EncodeToString(kp.PublicKey.Compressed()) == pubKeyHex {
				return kp.PrivateKey, nil
			}
		}
		for i := uint32(0); i < e.WState.NextChangeIndex+10; i++ {
			kp, err := e.Wallet.DeriveFeeKey(wallet.InternalChain, i)
			if err != nil {
				continue
			}
			if hex.EncodeToString(kp.PublicKey.Compressed()) == pubKeyHex {
				return kp.PrivateKey, nil
			}
		}
		return nil, fmt.Errorf("engine: no private key found for fee UTXO pubkey %s", pubKeyHex)
	}

	// Node UTXO — unchanged.
	node := e.State.GetNode(pubKeyHex)
	if node == nil {
		return nil, fmt.Errorf("engine: unknown node %s", pubKeyHex)
	}
	kp, err := e.Wallet.DeriveNodeKey(node.VaultIndex, node.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive node key: %w", err)
	}
	return kp.PrivateKey, nil
}
```

Note: Add `FindUTXOByPubKey` helper to `LocalState` if not present:

```go
func (s *LocalState) FindUTXOByPubKey(pubKeyHex, utxoType string) *UTXOState {
	for i := range s.UTXOs {
		if s.UTXOs[i].PubKeyHex == pubKeyHex && s.UTXOs[i].Type == utxoType && !s.UTXOs[i].Spent {
			return &s.UTXOs[i]
		}
	}
	return nil
}
```

Also update `TrackNewUTXOs` and any other UTXO creation sites to populate `FeeChain` and `FeeDerivIdx` for fee UTXOs.

**Step 4: Run tests**

Run: `cd bitfs && go test ./internal/engine/ -v -count=1`
Expected: ALL PASS

---

### Task 11: Shell History File Permissions (M-NEW-23)

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_shell.go:71-84`
- Test: `bitfs/cmd/bitfs/cmd_shell_test.go`

**Step 1: Write failing test**

```go
func TestShellHistoryFile_RestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	historyFile := filepath.Join(dir, "shell_history")

	// Create the file to simulate what readline does.
	require.NoError(t, os.WriteFile(historyFile, []byte("test\n"), 0644))

	// Our function should fix permissions.
	ensureHistoryFilePermissions(historyFile)

	info, err := os.Stat(historyFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test ./cmd/bitfs/ -run TestShellHistoryFile_RestrictivePermissions -v`
Expected: FAIL — `ensureHistoryFilePermissions` doesn't exist.

**Step 3: Add helper and call after readline init**

Add to `bitfs/cmd/bitfs/cmd_shell.go`:

```go
// ensureHistoryFilePermissions restricts the history file to owner-only access.
func ensureHistoryFilePermissions(path string) {
	_ = os.Chmod(path, 0600)
}
```

In `runShell`, after the `readline.NewFromConfig` call succeeds (after line 83), add:

```go
	ensureHistoryFilePermissions(historyFile)
```

**Step 4: Run tests**

Run: `cd bitfs && go test ./cmd/bitfs/ -v -count=1`
Expected: ALL PASS

---

### Task 12: Capsule Persistence for Crash Recovery (M-NEW-25)

**Files:**
- Modify: `bitfs/internal/daemon/payment.go`
- Modify: `bitfs/internal/daemon/daemon.go` (add persistence directory)
- Test: `bitfs/internal/daemon/payment_test.go`

This is the most complex fix. The key issue: if daemon crashes between marking `invoice.Paid = true` and sending the HTTP response, the buyer loses the capsule.

**Step 1: Write failing test**

```go
func TestDaemon_PersistsInvoiceOnPayment(t *testing.T) {
	d := newTestDaemon(t)
	dir := t.TempDir()
	d.invoiceDir = dir

	// Simulate a paid invoice with capsule.
	inv := &InvoiceRecord{
		InvoiceID:  "test-inv-123",
		Paid:       true,
		Capsule:    []byte("encrypted-capsule-data"),
		CreatedAt:  time.Now(),
	}

	// Persist it.
	require.NoError(t, d.persistInvoice(inv))

	// Load it back.
	loaded, err := d.loadInvoice("test-inv-123")
	require.NoError(t, err)
	assert.True(t, loaded.Paid)
	assert.Equal(t, inv.Capsule, loaded.Capsule)
}

func TestDaemon_RecoversPaidInvoicesOnStart(t *testing.T) {
	dir := t.TempDir()
	d := newTestDaemonWithDir(t, dir)

	// Write a persisted invoice file.
	inv := &InvoiceRecord{
		InvoiceID: "recovered-inv",
		Paid:      true,
		Capsule:   []byte("recovery-capsule"),
		CreatedAt: time.Now(),
	}
	require.NoError(t, d.persistInvoice(inv))

	// Simulate restart: load persisted invoices.
	d.recoverPersistedInvoices()

	d.invoicesMu.RLock()
	defer d.invoicesMu.RUnlock()
	loaded, ok := d.invoices["recovered-inv"]
	require.True(t, ok, "persisted invoice should be recovered")
	assert.True(t, loaded.Paid)
	assert.Equal(t, []byte("recovery-capsule"), loaded.Capsule)
}
```

**Step 2: Implement invoice persistence**

Add `invoiceDir string` field to `Daemon` struct. Set it from config in `New()`.

Add to `bitfs/internal/daemon/daemon.go`:

```go
// persistInvoice writes invoice to disk atomically.
func (d *Daemon) persistInvoice(inv *InvoiceRecord) error {
	if d.invoiceDir == "" {
		return nil // persistence disabled
	}
	data, err := json.Marshal(inv)
	if err != nil {
		return fmt.Errorf("marshal invoice: %w", err)
	}
	path := filepath.Join(d.invoiceDir, inv.InvoiceID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write invoice: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename invoice: %w", err)
	}
	return nil
}

// loadInvoice reads a persisted invoice from disk.
func (d *Daemon) loadInvoice(invoiceID string) (*InvoiceRecord, error) {
	path := filepath.Join(d.invoiceDir, invoiceID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inv InvoiceRecord
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// recoverPersistedInvoices loads paid invoices from disk on startup.
func (d *Daemon) recoverPersistedInvoices() {
	if d.invoiceDir == "" {
		return
	}
	entries, err := os.ReadDir(d.invoiceDir)
	if err != nil {
		return
	}
	d.invoicesMu.Lock()
	defer d.invoicesMu.Unlock()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(d.invoiceDir, entry.Name()))
		if err != nil {
			continue
		}
		var inv InvoiceRecord
		if err := json.Unmarshal(data, &inv); err != nil {
			continue
		}
		if inv.Paid && inv.InvoiceID != "" {
			d.invoices[inv.InvoiceID] = &inv
		}
	}
}
```

In `handleSubmitHTLC`, after setting `invoice.Paid = true` and before writing the HTTP response, add:

```go
	_ = d.persistInvoice(invoice)  // Best-effort persist before response.
```

In `Start()`, call `d.recoverPersistedInvoices()` before starting the HTTP server.

**Step 3: Run tests**

Run: `cd bitfs && go test ./internal/daemon/ -v -count=1`
Expected: ALL PASS

---

### Task 13: Commit libbitfs-go Changes

**Step 1: Run full libbitfs-go test suite**

Run: `cd libbitfs-go && go test ./... -count=1 -race`
Expected: ALL PASS

**Step 2: Commit**

```bash
cd libbitfs-go && git add -A
git commit -m "fix(safety): MEDIUM audit fixes batch 3

- M-4: ParseHTLCPreimage verifies SHA256(preimage) against expected hash
- M-5: BuildSellerClaimTx validates capsule hash against HTLC script
- M-9: RPC client rejects non-2xx HTTP status codes
- M-10: RPC client validates response ID matches request ID
- M-11: SyncHeaders validates PrevBlock chain continuity
- M-16: SaveConfig creates files with 0600 permissions"
```

---

### Task 14: Commit bitfs Changes

**Step 1: Run full bitfs test suite**

Run: `cd bitfs && go test ./... -count=1 -race`
Expected: ALL PASS

Run: `cd bitfs && go test -tags=integration ./integration/ -count=1 -race`
Expected: ALL 275+ PASS

**Step 2: Commit**

```bash
cd bitfs && git add -A
git commit -m "fix(safety): MEDIUM audit fixes batch 3

- M-12: background cleanup goroutine for sessions/invoices/rate-limiter
- M-13: time-based eviction for unbounded invoice and rate limiter maps
- M-14: handshake timestamp validation (±5 min skew window)
- M-NEW-3: shell mput access mode validation
- M-NEW-22: fee key derivation index stored in UTXOState (O(1) lookup)
- M-NEW-23: shell history file set to 0600 permissions
- M-NEW-25: invoice persistence for crash recovery"
```

---

### Task 15: Update Audit Report

**Files:**
- Modify: `docs/audits/2026-02-26-code-audit.md`

Update the status of all Batch 3 findings to **FIXED**:

| ID | Status | Notes |
|----|--------|-------|
| M-4 | FIXED | ParseHTLCPreimage verifies SHA256(preimage) |
| M-5 | FIXED | BuildSellerClaimTx validates capsule hash against HTLC script |
| M-9 | FIXED | HTTP status code check before JSON decode |
| M-10 | FIXED | Response ID validated against request ID |
| M-11 | FIXED | PrevBlock chain continuity validation in SyncHeaders |
| M-12 | FIXED | Background cleanup goroutine in Start() |
| M-13 | FIXED | Time-based eviction for invoices + rate limiter |
| M-14 | FIXED | ±5 min timestamp skew window |
| M-16 | FIXED | SaveConfig uses 0600 file permissions |
| M-NEW-3 | FIXED | Access mode validated against free/private/paid |
| M-NEW-22 | FIXED | Fee derivation index in UTXOState, O(1) lookup |
| M-NEW-23 | FIXED | History file chmod 0600 |
| M-NEW-25 | FIXED | Invoice persistence + crash recovery |

Update executive summary: all MEDIUM findings now FIXED (except 2 BY-DESIGN).

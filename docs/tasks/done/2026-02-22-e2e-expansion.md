# E2E Test Expansion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Expand regtest E2E test coverage from 7 files (~12 tests) to 25 files (~50 tests), covering all major BitFS features.

**Architecture:** Tests follow existing patterns: `//go:build e2e` tag, `package e2e`, `testutil.RegtestNode` for on-chain tests, `httptest.Server` for daemon tests. Engine-level tests construct `Engine` struct directly with temp dirs. Each test file is numbered sequentially (08-25).

**Tech Stack:** Go 1.25.6, `testify/require` + `testify/assert`, `go-sdk` (ec, script, transaction), `libbitfs/*` (method42, tx, wallet, storage, x402), `internal/daemon`, `internal/client`, `internal/engine`

**Design Doc:** `docs/plans/2026-02-22-e2e-expansion-design.md`

---

## Prerequisites

All tests reuse existing helpers from `02_metanet_root_test.go`:
- `setupFundedWallet(t, ctx, node) *wallet.Wallet`
- `getFundedUTXO(t, ctx, node, addr, kp) *tx.UTXO`
- `extractPushData(t, script) [][]byte`
- `reverseBytes(b []byte)` (in-place byte reversal)

All test files follow the same structure:
```go
//go:build e2e

package e2e

import (...)

func TestXxx(t *testing.T) {
    node := testutil.NewRegtestNode()
    testutil.SkipIfUnavailable(t, node)
    ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
    defer cancel()
    // ...
}
```

---

## Phase 1: Testutil Helpers (Task 1)

### Task 1: Add engine and daemon test helpers

These helpers simplify engine-level and daemon-level E2E tests.

**Files:**
- Create: `e2e/testutil/engine_helpers.go`

**Step 1: Write `engine_helpers.go`**

```go
//go:build e2e

package testutil

import (
    "encoding/hex"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/tongxiaofeng/bitfs/internal/engine"
    "github.com/tongxiaofeng/libbitfs/storage"
    "github.com/tongxiaofeng/libbitfs/wallet"
)

// SetupTestEngine creates a fully initialized Engine in a temp directory
// with a fresh wallet and empty state. Returns the engine and its data dir.
// The engine has no Chain configured (offline mode).
func SetupTestEngine(t *testing.T) (*engine.Engine, string) {
    t.Helper()
    dataDir := t.TempDir()

    // Generate wallet.
    mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
    require.NoError(t, err, "generate mnemonic")
    t.Logf("engine mnemonic: %s", mnemonic)

    seed, err := wallet.SeedFromMnemonic(mnemonic, "")
    require.NoError(t, err, "seed from mnemonic")

    w, err := wallet.NewWallet(seed, &wallet.RegTest)
    require.NoError(t, err, "create wallet")

    // Create wallet state with default vault.
    wState := &wallet.WalletState{
        Vaults: []wallet.Vault{
            {Name: "default", AccountIndex: 0},
        },
        NextVaultIndex: 1,
    }

    // Create file store.
    storeDir := filepath.Join(dataDir, "storage")
    store, err := storage.NewFileStore(storeDir)
    require.NoError(t, err, "create file store")

    // Create local state.
    statePath := filepath.Join(dataDir, "nodes.json")
    state := engine.NewLocalState(statePath)

    eng := &engine.Engine{
        Wallet:  w,
        WState:  wState,
        Store:   store,
        State:   state,
        DataDir: dataDir,
    }

    return eng, dataDir
}

// FundEngineWallet funds the engine's fee wallet via regtest.
// Derives the first fee key, imports its address, mines 101 blocks,
// sends 0.01 BSV, mines 1 confirmation, and adds the UTXO to engine state.
func FundEngineWallet(t *testing.T, eng *engine.Engine, node *RegtestNode) {
    t.Helper()
    ctx := context.Background()

    feeKey, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, eng.WState.NextReceiveIndex)
    require.NoError(t, err, "derive fee key")

    feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
    require.NoError(t, err, "fee address")

    err = node.ImportAddress(ctx, feeAddr.AddressString)
    require.NoError(t, err, "import address")

    mineAddr, err := node.NewAddress(ctx)
    require.NoError(t, err)
    _, err = node.MineBlocks(ctx, 101, mineAddr)
    require.NoError(t, err, "mine 101 blocks")

    _, err = node.SendToAddress(ctx, feeAddr.AddressString, 0.01)
    require.NoError(t, err, "fund fee address")

    _, err = node.MineBlocks(ctx, 1, mineAddr)
    require.NoError(t, err, "mine confirmation")

    utxos, err := node.ListUnspent(ctx, feeAddr.AddressString)
    require.NoError(t, err, "list unspent")
    require.NotEmpty(t, utxos, "should have UTXO")

    // Convert to engine UTXOState.
    u := utxos[0]
    txidBytes, err := hex.DecodeString(u.TxID)
    require.NoError(t, err)
    // Reverse for internal byte order.
    for i, j := 0, len(txidBytes)-1; i < j; i, j = i+1, j-1 {
        txidBytes[i], txidBytes[j] = txidBytes[j], txidBytes[i]
    }

    scriptPubKey, err := tx.BuildP2PKHScript(feeKey.PublicKey)
    require.NoError(t, err)

    eng.State.AddUTXO(&engine.UTXOState{
        TxID:         hex.EncodeToString(txidBytes),
        Vout:         u.Vout,
        Amount:       uint64(u.Amount * 1e8),
        ScriptPubKey: hex.EncodeToString(scriptPubKey),
        PubKeyHex:    hex.EncodeToString(feeKey.PublicKey.Compressed()),
        Type:         "fee",
    })

    eng.WState.NextReceiveIndex++
    t.Logf("funded engine wallet: %s (%d sat)", feeAddr.AddressString, uint64(u.Amount*1e8))
}
```

Note: This file needs proper imports for `context`, `script`, and `tx` packages. The implementer should add the correct import paths:
- `"context"`
- `"github.com/bsv-blockchain/go-sdk/script"`
- `"github.com/tongxiaofeng/libbitfs/tx"`

**Step 2: Verify compilation**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go build -tags e2e ./e2e/...`
Expected: Compiles without errors

**Step 3: Commit**

```bash
git add e2e/testutil/engine_helpers.go
git commit -m "test(e2e): add engine and daemon test helpers for E2E expansion"
```

---

## Phase 2: DAG Mutation Tests (Tasks 2-5)

### Task 2: Move/rename operations (`08_move_rename_test.go`)

**Files:**
- Create: `e2e/08_move_rename_test.go`

**Context:** Tests build a DAG: root → dir_a (with file), dir_b (empty). Then move the file from dir_a to dir_b by:
1. SelfUpdate on dir_a: remove file from children
2. SelfUpdate on dir_b: add file to children
Verify the file's P_node is unchanged, only its parent context changes.

**Step 1: Write test file**

The test should follow the pattern from `07_full_lifecycle_test.go`:
1. Setup wallet, derive keys: fee, root, dir_a, dir_b, file (5 keys)
2. Fund fee key
3. Build root tx, mine
4. Build dir_a child tx, mine
5. Build dir_b child tx (uses dir_a's change as fee), mine
6. Build file child tx under dir_a, mine
7. **Move test**: Build two SelfUpdate txs:
   - SelfUpdate on dir_a with empty payload (file removed)
   - SelfUpdate on dir_b with payload referencing file
8. Mine and verify: dir_a no longer references file, dir_b references file
9. **Rename test** (sub-test): Build SelfUpdate on dir_b with changed payload name

Key derivation paths:
- fee: `w.DeriveFeeKey(wallet.ExternalChain, 0)`
- root: `w.DeriveNodeKey(0, nil, nil)`
- dir_a: `w.DeriveNodeKey(0, []uint32{0}, nil)`
- dir_b: `w.DeriveNodeKey(0, []uint32{1}, nil)`
- file: `w.DeriveNodeKey(0, []uint32{0, 0}, nil)`

UTXO chain: Each tx's `ChangeUTXO` feeds the next tx's fee. Each `NodeUTXO` and `ParentUTXO` must be prepared with ScriptPubKey + PrivateKey before reuse.

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags e2e ./e2e/... -run TestMoveRename -v -timeout 180s`
Expected: PASS (requires regtest node running)

**Step 3: Commit**

```bash
git add e2e/08_move_rename_test.go
git commit -m "test(e2e): add move/rename DAG mutation tests"
```

---

### Task 3: Copy operations (`09_copy_test.go`)

**Files:**
- Create: `e2e/09_copy_test.go`

**Context:** Tests verify that copying creates an independent node with a new P_node but identical content.

Tests:
- `TestCopyFile`: Create root → dir → file (encrypted free). Create a new key for copy, re-encrypt same plaintext with new key, build a new CreateChild tx in same dir. Verify: different P_node, different TxID, same decrypted plaintext.
- `TestCopyIndependence`: Copy file, then SelfUpdate original with new content. Verify copy still has original content.

Pattern: Same wallet/funding setup as Task 2. The "copy" is implemented by:
1. Decrypt original file content
2. Generate new node key for copy
3. Re-encrypt with new key
4. Build CreateChild tx for the copy

**Step 1: Write test file**

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags e2e ./e2e/... -run TestCopy -v -timeout 180s`

**Step 3: Commit**

```bash
git add e2e/09_copy_test.go
git commit -m "test(e2e): add copy operation E2E tests"
```

---

### Task 4: Remove operations (`10_remove_test.go`)

**Files:**
- Create: `e2e/10_remove_test.go`

**Context:** Tests verify removal by publishing a new parent directory version without the child entry (same pattern as step 10 in `07_full_lifecycle_test.go`).

Tests:
- `TestRemoveFile`: Create root → dir → file. SelfUpdate dir with empty payload. Verify: new dir version on-chain, P_node and parentTxID preserved, file entry gone.
- `TestRemoveDirectory`: Create root → dir_parent → dir_child. SelfUpdate dir_parent to remove dir_child. Verify same.
- `TestRemoveAndVerifyDAG`: Full removal + DAG state cross-check (all txids on chain, parent links correct).

**Step 1-3: Write, run, commit** (same pattern)

```bash
git commit -m "test(e2e): add remove operation E2E tests"
```

---

### Task 5: Link operations (`11_link_test.go`)

**Files:**
- Create: `e2e/11_link_test.go`

**Context:** Hard links share the same P_node across multiple parent directories. Soft links create a separate node whose payload contains the target path/pubkey.

Tests:
- `TestHardLink`: Create root → dir_a → file. SelfUpdate dir_b to add a ChildEntry pointing to file's pubkey. Verify: same P_node in both directories' OP_RETURN payloads.
- `TestSoftLink`: Create root → dir → file. Create a new "link" node (CreateChild) whose payload contains `file`'s compressed pubkey as the link target. Verify: link node has different P_node, payload contains target reference.

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add hard/soft link E2E tests"
```

---

## Phase 3: Access Control Tests (Tasks 6-7)

### Task 6: Encryption transitions (`12_encrypt_transition_test.go`)

**Files:**
- Create: `e2e/12_encrypt_transition_test.go`

**Context:** Verify that re-encrypting from Free→Private produces a new ciphertext that the FreePrivateKey can no longer decrypt, but the owner's real key still can.

Tests (no regtest needed — pure crypto):
- `TestFreeToPrivateTransition`: Encrypt content as Free, verify FreePrivateKey decrypts. Re-encrypt as Private (same node key), verify FreePrivateKey fails, owner key succeeds.
- `TestPrivateContentOwnerAccess`: Encrypt directly as Private. Verify only owner key decrypts.
- `TestAccessModePreservedInPayload`: Build OP_RETURN payloads for Free and Private, verify the access mode indicator byte differs (if encoded).

Key crypto:
```go
// Free mode: ECDH(scalar_1, P_node) → anyone can compute
enc := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessFree)

// Private mode: ECDH(D_node, P_node) → only owner knows D_node
encPriv := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPaid)

// Free key should NOT decrypt private content:
freeKey := method42.FreePrivateKey()
_, err := method42.Decrypt(encPriv.Ciphertext, freeKey, nodeKey.PublicKey, encPriv.KeyHash, method42.AccessFree)
// err != nil
```

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add encryption transition E2E tests"
```

---

### Task 7: Sell/pricing (`13_sell_pricing_test.go`)

**Files:**
- Create: `e2e/13_sell_pricing_test.go`

**Context:** Verify that setting a price on a file updates its metadata. This uses the engine layer.

Tests:
- `TestSetPriceOnFile`: Use `SetupTestEngine`, create root and file nodes in state, call `engine.Sell(SellOpts{Path: "/file.txt", PricePerKB: 100})`. Verify: node state updated with PricePerKB=100, Access="paid".
- `TestPriceInNodeState`: After sell, verify `engine.State.FindNodeByPath("/file.txt").PricePerKB == 100`.

Note: These tests may need to create engine state directly (populate nodes map) since engine.Sell operates on local state. If Sell requires on-chain tx building, the test needs regtest funding.

Check the engine.Sell implementation to determine if it builds a tx (needs UTXO) or just updates state. From research: it creates a SelfUpdate tx, so it needs funded UTXOs.

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add sell/pricing E2E tests"
```

---

## Phase 4: Vault & Wallet Tests (Tasks 8-10)

### Task 8: Vault CRUD (`14_vault_crud_test.go`)

**Files:**
- Create: `e2e/14_vault_crud_test.go`

**Context:** These tests verify wallet-level vault operations. No regtest needed — pure wallet state management.

Tests:
- `TestCreateVault`: Create wallet, create vault "photos". Verify: vault exists, has unique account index.
- `TestListVaults`: Create wallet with default + 2 more vaults. List. Verify: 3 vaults, correct names.
- `TestRenameVault`: Create "old", rename to "new". Verify: GetVault("old") fails, GetVault("new") succeeds.
- `TestDeleteVault`: Create vault, delete it. Verify: ListVaults no longer includes it, GetVault fails.

```go
func TestVaultCRUD(t *testing.T) {
    mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
    require.NoError(t, err)
    seed, err := wallet.SeedFromMnemonic(mnemonic, "")
    require.NoError(t, err)
    w, err := wallet.NewWallet(seed, &wallet.RegTest)
    require.NoError(t, err)

    wState := &wallet.WalletState{
        Vaults:         []wallet.Vault{{Name: "default", AccountIndex: 0}},
        NextVaultIndex: 1,
    }

    t.Run("create", func(t *testing.T) {
        v, err := w.CreateVault(wState, "photos")
        require.NoError(t, err)
        assert.Equal(t, "photos", v.Name)
        assert.Equal(t, uint32(1), v.AccountIndex)
    })

    t.Run("list", func(t *testing.T) {
        vaults := w.ListVaults(wState)
        assert.Len(t, vaults, 2)
    })

    t.Run("rename", func(t *testing.T) {
        err := w.RenameVault(wState, "photos", "images")
        require.NoError(t, err)
        _, err = w.GetVault(wState, "photos")
        assert.ErrorIs(t, err, wallet.ErrVaultNotFound)
        v, err := w.GetVault(wState, "images")
        require.NoError(t, err)
        assert.Equal(t, uint32(1), v.AccountIndex)
    })

    t.Run("delete", func(t *testing.T) {
        err := w.DeleteVault(wState, "images")
        require.NoError(t, err)
        vaults := w.ListVaults(wState)
        assert.Len(t, vaults, 1)
        assert.Equal(t, "default", vaults[0].Name)
    })

    t.Run("duplicate_name_error", func(t *testing.T) {
        _, err := w.CreateVault(wState, "default")
        assert.ErrorIs(t, err, wallet.ErrVaultExists)
    })
}
```

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add vault CRUD E2E tests"
```

---

### Task 9: Multi-vault isolation (`15_multi_vault_test.go`)

**Files:**
- Create: `e2e/15_multi_vault_test.go`

**Context:** Verify that different vaults derive completely different key hierarchies and content encrypted in one vault cannot be decrypted with another vault's keys.

Tests:
- `TestVaultKeyIsolation`: Create wallet, derive root keys for vault 0 and vault 1. Verify: different compressed pubkeys.
- `TestVaultEncryptionIsolation`: Encrypt content with vault 0's node key. Attempt decrypt with vault 1's node key. Verify: decryption fails.
- `TestVaultIndependentRoots`: On regtest, create root txs for two vaults with different keys. Verify: different P_nodes on chain.

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add multi-vault isolation E2E tests"
```

---

### Task 10: External UTXO funding (`16_fund_external_test.go`)

**Files:**
- Create: `e2e/16_fund_external_test.go`

**Context:** Verify the `fund` command pattern: register an externally-created UTXO into engine state, then use it for a tx.

Tests:
- `TestFundExternalUTXO`: Send coins to engine's fee address via regtest RPC (not through engine). Manually add UTXO to engine state. Build and broadcast a root tx using that UTXO. Verify: tx broadcasts and confirms.

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add external UTXO funding E2E tests"
```

---

## Phase 5: Daemon HTTP API Tests (Tasks 11-15)

### Task 11: Method 42 handshake (`17_daemon_handshake_test.go`)

**Files:**
- Create: `e2e/17_daemon_handshake_test.go`

**Context:** Test the ECDH handshake endpoint using httptest.Server with mock services (same pattern as `05_free_content_test.go`).

Tests:
- `TestMethod42Handshake`: POST `/_bitfs/handshake` with valid buyer_pub + nonce_b. Verify: 200, response has seller_pub (33-byte hex), nonce_s, session_id, expires_at.
- `TestHandshakeSessionPersists`: Complete handshake, verify session_id can be retrieved (subsequent request or daemon internal state).
- `TestHandshakeInvalidPubkey`: POST with malformed buyer_pub. Verify: 400 error.

Setup pattern:
```go
// Create wallet and daemon.
w := createTestWallet(t)
walletSvc := &testWalletService{w: w}
metanetSvc := &testMetanetService{nodes: map[string]*daemon.NodeInfo{}}
config := daemon.DefaultConfig()
config.Security.RateLimit.RPM = 0
d, err := daemon.New(config, walletSvc, fileStore, metanetSvc)
server := httptest.NewServer(d.Handler())

// POST handshake.
buyerPriv, _ := ec.NewPrivateKey()
nonce := make([]byte, 32)
rand.Read(nonce)
body := fmt.Sprintf(`{"buyer_pub":"%x","nonce_b":"%x","timestamp":%d}`,
    buyerPriv.PubKey().Compressed(), nonce, time.Now().Unix())
resp, err := http.Post(server.URL+"/_bitfs/handshake", "application/json", strings.NewReader(body))
```

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add Method 42 handshake E2E tests"
```

---

### Task 12: Buy API endpoints (`18_daemon_buy_api_test.go`)

**Files:**
- Create: `e2e/18_daemon_buy_api_test.go`

**Context:** Test GET/POST `/_bitfs/buy/{txid}` with a daemon that has a paid file node. The daemon needs a mock MetanetService that returns a paid NodeInfo with a known key hash.

Tests:
- `TestGetBuyInfo`: Register paid file node in mock MetanetService. GET `/buy/{txid}`. Verify: 200 with capsule_hash, price, payment_addr.
- `TestSubmitInvalidHTLC`: POST `/buy/{txid}` with garbage body. Verify: 400 error.

Note: Testing the full HTLC submit+capsule flow requires the daemon to verify the HTLC tx structure. This may be complex to mock correctly. Focus on the error path and basic GET flow.

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add buy API endpoint E2E tests"
```

---

### Task 13: Content negotiation (`19_daemon_content_neg_test.go`)

**Files:**
- Create: `e2e/19_daemon_content_neg_test.go`

**Context:** Extend the content negotiation tests from `05_free_content_test.go` with additional edge cases. This test creates a richer mock MetanetService with nested directories and multiple file types.

Tests:
- `TestContentNegDeepPath`: Directory with 3 levels of nesting, verify HTML shows breadcrumbs.
- `TestContentNegPaidFile402`: Paid file node returns 402 Payment Required with x402 headers.
- `TestContentNegEmptyDirectory`: Directory with no children, HTML shows empty state.
- `TestContentNegMultipleFiles`: Directory with 5+ children, verify all appear in HTML listing.

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add content negotiation edge case E2E tests"
```

---

### Task 14: Paymail/BSV Alias (`20_daemon_paymail_test.go`)

**Files:**
- Create: `e2e/20_daemon_paymail_test.go`

**Context:** Test the BSV Alias endpoints.

Tests:
- `TestBSVAliasCapabilities`: GET `/.well-known/bsvalias`. Verify: 200, JSON with `bsvalias` version field and `capabilities` map.
- `TestPKILookup`: GET `/api/v1/pki/alice`. Verify: 200, JSON with `handle`, `pubkey` fields. Pubkey should be valid 33-byte compressed hex.
- `TestPKINotFound`: GET `/api/v1/pki/nonexistent`. Verify: 404 (or appropriate error depending on implementation).

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add paymail/BSV alias E2E tests"
```

---

### Task 15: SPV proof endpoint (`21_daemon_spv_endpoint_test.go`)

**Files:**
- Create: `e2e/21_daemon_spv_endpoint_test.go`

**Context:** Test the SPV proof endpoint. This needs a mock SPVService since we can't easily run a full SPV client in httptest.

Tests:
- `TestSPVProofEndpointSuccess`: Mock SPVService returns confirmed proof. GET `/spv/proof/{txid}`. Verify: 200, `confirmed: true`, `block_hash` present.
- `TestSPVProofEndpointNotFound`: Mock SPVService returns error. GET `/spv/proof/{bad-txid}`. Verify: 404 or appropriate error.
- `TestSPVProofEndpointNoSPV`: Daemon with nil SPV service. GET `/spv/proof/{txid}`. Verify: 503 or error indicating SPV not available.

```go
// Mock SPV service.
type testSPVService struct {
    results map[string]*daemon.SPVResult
}

func (s *testSPVService) VerifyTx(ctx context.Context, txid string) (*daemon.SPVResult, error) {
    r, ok := s.results[txid]
    if !ok {
        return nil, fmt.Errorf("tx not found")
    }
    return r, nil
}
```

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add SPV proof endpoint E2E tests"
```

---

## Phase 6: Client Library Roundtrip (Task 16)

### Task 16: Client→Daemon roundtrip (`22_client_roundtrip_test.go`)

**Files:**
- Create: `e2e/22_client_roundtrip_test.go`

**Context:** Test the `internal/client` library against a real httptest.Server daemon. This validates the full client→daemon→response chain.

Tests:
- `TestClientGetMeta`: Set up daemon with mock MetanetService. Use `client.New(server.URL)` to call `GetMeta`. Verify: MetaResponse fields match mock data.
- `TestClientGetData`: Store ciphertext in FileStore. Use `client.GetData(keyHash)`. Verify: body matches stored ciphertext.
- `TestClientGetMetaNotFound`: Call `GetMeta` for non-existent path. Verify: `client.ErrNotFound` returned.
- `TestClientVerifySPV`: Mock SPV service, call `client.VerifySPV`. Verify: SPVProofResponse matches mock.

```go
import "github.com/tongxiaofeng/bitfs/internal/client"

func TestClientRoundtrip(t *testing.T) {
    // Set up daemon with real store + mock metanet.
    // ...
    server := httptest.NewServer(d.Handler())
    defer server.Close()

    c := client.New(server.URL)

    t.Run("GetMeta", func(t *testing.T) {
        meta, err := c.GetMeta(pnodeHex, "/docs/readme.txt")
        require.NoError(t, err)
        assert.Equal(t, "file", meta.Type)
        assert.Equal(t, "free", meta.Access)
    })

    t.Run("GetData", func(t *testing.T) {
        rc, err := c.GetData(keyHashHex)
        require.NoError(t, err)
        defer rc.Close()
        body, _ := io.ReadAll(rc)
        assert.Equal(t, storedCiphertext, body)
    })

    t.Run("GetMetaNotFound", func(t *testing.T) {
        _, err := c.GetMeta(pnodeHex, "/nonexistent")
        assert.ErrorIs(t, err, client.ErrNotFound)
    })
}
```

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add client library roundtrip E2E tests"
```

---

## Phase 7: Edge Cases & Stress (Tasks 17-19)

### Task 17: Error paths (`23_error_paths_test.go`)

**Files:**
- Create: `e2e/23_error_paths_test.go`

**Context:** Verify error handling for invalid operations on regtest.

Tests:
- `TestDoubleSpendRejected`: Build and broadcast a tx. Try to build another tx spending the same UTXO (same TxID+Vout). Verify: second broadcast fails with RPC error (txn-mempool-conflict).
- `TestMalformedTxBroadcast`: Send invalid hex to `SendRawTransaction`. Verify: RPC error.
- `TestInsufficientFeeUTXO`: Build a tx with a fee UTXO that has insufficient funds (e.g., dust-only). Verify: build or broadcast error.
- `TestBroadcastEmptyTx`: Send empty string to broadcast. Verify: error.

```go
func TestDoubleSpendRejected(t *testing.T) {
    node := testutil.NewRegtestNode()
    testutil.SkipIfUnavailable(t, node)
    ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
    defer cancel()

    w := setupFundedWallet(t, ctx, node)
    feeKey, _ := w.DeriveFeeKey(wallet.ExternalChain, 0)
    feeAddr, _ := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
    feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)

    rootKey1, _ := w.DeriveNodeKey(0, nil, nil)
    rootKey2, _ := w.DeriveNodeKey(0, []uint32{0}, nil)

    // First tx: should succeed.
    mtx1, _ := tx.BuildUnsignedCreateRootTx(&tx.CreateRootParams{...feeUTXO...})
    hex1, _ := tx.SignMetanetTx(mtx1, []*tx.UTXO{feeUTXO})
    _, err := node.SendRawTransaction(ctx, hex1)
    require.NoError(t, err)

    // Second tx: same feeUTXO → double-spend.
    mtx2, _ := tx.BuildUnsignedCreateRootTx(&tx.CreateRootParams{...feeUTXO...})
    hex2, _ := tx.SignMetanetTx(mtx2, []*tx.UTXO{feeUTXO})
    _, err = node.SendRawTransaction(ctx, hex2)
    require.Error(t, err, "double-spend should be rejected")
    t.Logf("double-spend correctly rejected: %v", err)
}
```

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add error path E2E tests"
```

---

### Task 18: Large file handling (`24_large_file_test.go`)

**Files:**
- Create: `e2e/24_large_file_test.go`

**Context:** Verify that Method 42 encryption/decryption works correctly for larger payloads and the FileStore + daemon can handle them.

Tests:
- `TestLargeFileEncryptDecrypt`: Generate 1MB of random data. Encrypt with AccessFree. Decrypt. Verify: plaintext matches.
- `TestLargeFileStoreDaemonRoundtrip`: Encrypt 1MB, store in FileStore, retrieve via daemon HTTP GET. Decrypt retrieved content. Verify: plaintext matches.

```go
func TestLargeFileEncryptDecrypt(t *testing.T) {
    privKey, _ := ec.NewPrivateKey()
    pubKey := privKey.PubKey()

    // Generate 1MB of data.
    data := make([]byte, 1<<20) // 1 MB
    for i := range data {
        data[i] = byte(i % 256)
    }

    enc, err := method42.Encrypt(data, privKey, pubKey, method42.AccessFree)
    require.NoError(t, err)
    t.Logf("1MB encrypted: %d bytes ciphertext", len(enc.Ciphertext))

    freeKey := method42.FreePrivateKey()
    dec, err := method42.Decrypt(enc.Ciphertext, freeKey, pubKey, enc.KeyHash, method42.AccessFree)
    require.NoError(t, err)
    assert.Equal(t, data, dec.Plaintext)
    t.Logf("1MB decrypted successfully: %d bytes", len(dec.Plaintext))
}
```

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add large file handling E2E tests"
```

---

### Task 19: Self-update version chain (`25_self_update_chain_test.go`)

**Files:**
- Create: `e2e/25_self_update_chain_test.go`

**Context:** Verify that a node can be updated multiple times and each version is independently readable on-chain.

Tests:
- `TestMultipleUpdates`: Create root → dir → file. Perform 3 SelfUpdates with different content ("v1", "v2", "v3"). Retrieve all 4 versions from chain (original + 3 updates). Decrypt each. Verify: correct content for each version.
- `TestUpdatePreservesIdentity`: SelfUpdate a file. Verify: P_node unchanged, parentTxID unchanged. Only the TxID and payload change.

Key pattern: After each SelfUpdate, the node's NodeUTXO is refreshed (output 1 of the update tx). The fee UTXO is the change from the previous tx.

```go
// Version chain:
// file_tx (v0) → update1_tx (v1) → update2_tx (v2) → update3_tx (v3)
// Each update spends the previous NodeUTXO.
```

**Step 1-3: Write, run, commit**

```bash
git commit -m "test(e2e): add self-update version chain E2E tests"
```

---

## Phase 8: Final Verification (Task 20)

### Task 20: Run full E2E suite and verify

**Step 1: Start regtest node**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs/e2e && docker compose up -d`
Expected: BSV regtest node running on port 18332

**Step 2: Run all E2E tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags e2e ./e2e/... -v -timeout 300s`
Expected: All tests PASS

**Step 3: Run with race detector**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags e2e -race ./e2e/... -v -timeout 600s`
Expected: No race conditions detected

**Step 4: Commit all remaining changes**

Verify all files are committed. Run `git status` to check.

**Step 5: Update README**

Add new test descriptions to `e2e/README.md` documenting the expanded test suite.

```bash
git add e2e/README.md
git commit -m "docs: update E2E README with expanded test suite"
```

---

## Summary

| Phase | Tasks | Test Files | Test Functions |
|-------|-------|------------|----------------|
| 1. Testutil helpers | 1 | 1 helper file | N/A |
| 2. DAG mutations | 2-5 | 08-11 | ~13 |
| 3. Access control | 6-7 | 12-13 | ~5 |
| 4. Vault/wallet | 8-10 | 14-16 | ~9 |
| 5. Daemon HTTP | 11-15 | 17-21 | ~14 |
| 6. Client roundtrip | 16 | 22 | ~4 |
| 7. Edge cases | 17-19 | 23-25 | ~8 |
| 8. Final verification | 20 | N/A | Full suite run |
| **Total** | **20** | **18 new files** | **~53 functions** |

Estimated implementation: ~20 tasks, each 5-15 minutes. Tests that need regtest require Docker Desktop running.

# Integration Test Enhancement — Design

**Date:** 2026-02-22
**Status:** Approved
**Scope:** Add ~2,500-3,000 lines of cross-package integration tests to `bitfs/integration/`

## Problem

The existing 5 integration test files cover libbitfs package combinations well (wallet↔method42, metanet↔storage, tx↔spv, x402↔daemon, paymail↔wallet). However, the 3 internal packages (`engine`, `daemon`, `client`) that orchestrate these libraries have almost no cross-layer integration testing.

23 specific gaps identified across 10 categories.

## Approach

- Extend existing `integration/` directory (same `//go:build integration` tag)
- Interface mocks for Network/Store/MetanetService (no Docker required)
- 6 new test files, 3 batches by priority

## Batch 1: Engine Workflows (Highest Priority)

### `engine_workflow_test.go`

Tests the Engine orchestrator with mock Chain and Store:

| Test | Cross-Package Flow |
|------|--------------------|
| TestEnginePutAndRetrieve | wallet→key derivation→method42 encrypt→engine.Put→store.Put→engine.Get→method42 decrypt |
| TestEngineMkdirNested | wallet→engine.Mkdir→metanet.AddChild→state update→path resolution |
| TestEngineMoveFile | engine.Put→engine.Move→state: source gone, target exists, store unchanged |
| TestEngineCopyFile | engine.Put→engine.Copy→both nodes exist, different keys, store has both |
| TestEngineRemoveFile | engine.Put→engine.Remove→state: node deleted, store: ciphertext removed |
| TestEngineLink | engine.Put→engine.Link(soft)→metanet.FollowLink→resolves to target |
| TestEngineSellAndPrice | engine.Put→engine.Sell(pricePerKB)→metanet.InheritPricePerKB propagates |
| TestEngineEncryptTransition | engine.Put(Free)→engine.Encrypt(Private)→old Free fails, new Private decrypts |
| TestEngineOfflineMode | engine with Chain=nil→Broadcast/Verify return error, local ops still work |
| TestEngineFeeAllocation | 5× engine.Put→NextChangeIndex=5, each change key unique |

### `engine_state_test.go`

Tests state consistency under sequential mutations:

| Test | What It Verifies |
|------|-----------------|
| TestStateConsistencyAfterMutations | Put→Mkdir→Move→Remove→Copy sequence→state snapshot matches expectations |
| TestCrossVaultIsolationEngine | Put to vault[0] and vault[1]→different keys, different state entries |
| TestFeeKeyRotation | 10× Put→10 unique change addresses, NextChangeIndex=10 |
| TestUTXOChainRefreshEngine | CreateRoot→CreateChild×2→ParentUTXO refreshed after each child |
| TestResolveVaultIndexDefault | Unspecified vault→selects vault[0]; explicit vault→selects correctly |

## Batch 2: Daemon↔Client Full Chain (Medium Priority)

### `daemon_engine_test.go`

Tests Daemon ← Engine state propagation using httptest.Server:

| Test | What It Verifies |
|------|-----------------|
| TestDaemonSeesEngineMkdir | Engine.Mkdir→Daemon.GetNodeByPath returns NodeInfo |
| TestDaemonReflectsRemove | Engine.Remove→Daemon returns 404 |
| TestDaemonAccessModeChange | Engine.Encrypt(Private)→Daemon adjusts content negotiation |
| TestDaemonPriceInInvoice | Engine.Sell(100 sat/KB)→Daemon invoice headers have correct price |
| TestDaemonConcurrentOps | Parallel Put+Mkdir+Move→no race condition (run with -race) |

### `client_roundtrip_test.go` (rename existing mock if needed)

Tests Client HTTP → Daemon → Engine round trips:

| Test | What It Verifies |
|------|-----------------|
| TestClientGetMeta | Client.GetMeta("/path")→Daemon resolves→returns NodeInfo |
| TestClientAcceptHeaders | Client with Accept:text/html vs application/json→correct content type |
| TestClientPaymentFlow | Client gets 402→reads invoice→builds HTLC→capsule exchange→decrypts |
| TestClientNodeRemoved | Engine.Remove mid-flight→Client gets 404 |
| TestClientSessionLifecycle | Handshake→access→TTL expire→re-handshake |

## Batch 3: Merkle + SPV Verification (Complete Crypto Chain)

### `merkle_mutations_test.go`

Tests directory Merkle root under mutations:

| Test | What It Verifies |
|------|-----------------|
| TestMerkleRootChangesOnAddChild | AddChild→root R1≠R0 |
| TestMerkleRootChangesOnRemove | RemoveChild→root changes, other proofs still valid |
| TestMerkleRootChangesOnRename | RenameChild→root changes (name in ChildEntry hash) |
| TestMerkleProofLargeDirectory | 1000 children→proof size ~10 nodes (log2) |
| TestMerkleProofAfterParentUpdate | Parent SelfUpdate→child membership proofs still valid |

### `spv_verification_test.go`

Tests SPV cache and verification pipeline:

| Test | What It Verifies |
|------|-----------------|
| TestSPVCacheAndReuse | Store header→verify tx→cache proof→second verify uses cache |
| TestSPVBlockHashNotFound | Unknown block hash→ErrHeaderNotFound |
| TestSPVTamperedProof | Modified proof nodes→rejected |
| TestSPVMultipleTxsSameBlock | 2 txs in same block→shared Merkle root→both verify |
| TestSPVUnconfirmedTx | No proof→ErrUnconfirmed |

## Mock Infrastructure

Extend existing helpers in `integration/` package:

```go
// mockChain implements network.BlockchainService
type mockChain struct {
    broadcastedTxs [][]byte
    utxos          map[string][]*tx.UTXO
    broadcastErr   error
}

// mockEngineStore wraps storage.FileStore with tracking
type mockEngineStore struct {
    *storage.FileStore
    putCount    int
    deleteCount int
}

// testEngineWithMocks creates Engine with mock Chain + real State/Store
func testEngineWithMocks(t *testing.T) (*engine.Engine, *mockChain, *mockEngineStore)

// testDaemonWithEngine creates Daemon backed by a real Engine
func testDaemonWithEngine(t *testing.T) (*daemon.Daemon, *engine.Engine, *httptest.Server)
```

## Run Commands

```bash
# All integration tests (existing + new)
go test -tags=integration ./integration/ -v -count=1

# With race detector
go test -tags=integration ./integration/ -v -count=1 -race

# Single batch
go test -tags=integration ./integration/ -v -run "TestEngine"
go test -tags=integration ./integration/ -v -run "TestDaemon|TestClient"
go test -tags=integration ./integration/ -v -run "TestMerkle|TestSPV"
```

## Estimated Scope

| Batch | Files | Tests | Lines |
|-------|-------|-------|-------|
| 1: Engine | 2 | 15 | ~1,000 |
| 2: Daemon↔Client | 2 | 10 | ~800 |
| 3: Merkle+SPV | 2 | 10 | ~700 |
| **Total** | **6** | **35** | **~2,500** |

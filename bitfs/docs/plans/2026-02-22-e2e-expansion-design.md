# E2E Test Expansion Design

## Goal

Expand regtest end-to-end test coverage from 7 test files (~12 test functions) to 25 test files (~50 test functions), achieving full-sweep coverage of all major BitFS features against a live BSV regtest node.

## Current Coverage (tests 01-07)

| File | Coverage |
|------|----------|
| 01_wallet_fund | HD wallet derivation + regtest funding |
| 02_metanet_root | Root directory tx broadcast + parse |
| 03_mkdir_upload | 3-level DAG chain + Method 42 free encryption |
| 04_spv_verify | SPV Merkle proof verification (BIP37 + manual) |
| 05_free_content | Free content upload + daemon HTTP retrieval + content negotiation |
| 06_paid_purchase | x402 HTLC atomic swap (claim + buyer refund) |
| 07_full_lifecycle | Smoke test combining all above |

## New Test Files (08-25)

### Category A: DAG Mutation Operations

**08_move_rename_test.go** (3 tests)
- `TestMoveFileBetweenDirs`: Create root → dir_a → file, dir_b. Move file from dir_a to dir_b. Verify: dir_a ChildEntry removed, dir_b ChildEntry added, file P_node unchanged, file parentTxID updated to dir_b.
- `TestRenameFile`: Create file "old.txt", rename to "new.txt" in same dir. Verify: ChildEntry name changed, node identity unchanged.
- `TestRenameDirectory`: Create subdir "docs", rename to "files". Verify: parent ChildEntry updated, children still accessible.

**09_copy_test.go** (3 tests)
- `TestCopyFile`: Copy file to new location. Verify: new P_node (different key), same plaintext content, independent encryption.
- `TestCopyDirectory`: Copy directory with children. Verify: entire subtree duplicated with new node identities.
- `TestCopyIndependence`: Copy file, modify original via SelfUpdate. Verify: copy retains original content.

**10_remove_test.go** (3 tests)
- `TestRemoveFile`: Create dir with file, remove file. Verify: parent dir SelfUpdate removes ChildEntry.
- `TestRemoveDirectory`: Create nested dirs, remove inner dir. Verify: parent updated, child entries gone.
- `TestRemoveNonExistent`: Attempt to remove path that doesn't exist. Verify: appropriate error returned.

**11_link_test.go** (4 tests)
- `TestHardLink`: Create file, create hard link in another dir. Verify: same P_node in both ChildEntries, same content.
- `TestSoftLink`: Create file, create soft link. Verify: different P_node, link payload contains target path.
- `TestSoftLinkResolution`: Create soft link, resolve it. Verify: traversal reaches target node.
- `TestBrokenSoftLink`: Create soft link to non-existent target. Verify: resolution returns error.

### Category B: Access Control & Encryption Transitions

**12_encrypt_transition_test.go** (3 tests)
- `TestFreeToPrivate`: Upload free file, encrypt to private. Verify: new ciphertext on chain, FreePrivateKey no longer decrypts, owner key still decrypts.
- `TestPrivateContentOwnerAccess`: Upload private file directly. Verify: only owner can decrypt with node private key.
- `TestAccessModeInOPReturn`: Verify access mode byte in OP_RETURN changes from 0x01 (Free) to 0x00 (Private) after encrypt.

**13_sell_pricing_test.go** (2 tests)
- `TestSetPrice`: Upload free file, call Sell with price. Verify: node state updated with price_per_kb.
- `TestPriceInheritance`: Set price on directory. Verify: child files inherit parent price when queried.

### Category C: Vault & Wallet Operations

**14_vault_crud_test.go** (4 tests)
- `TestCreateVault`: Create wallet, create second vault. Verify: different BIP44 account index, different root key.
- `TestListVaults`: Create multiple vaults. Verify: list returns all with correct names and indices.
- `TestRenameVault`: Create vault, rename it. Verify: old name gone, new name resolves to same account.
- `TestDeleteVault`: Create vault, delete it. Verify: no longer appears in list, account index not reused.

**15_multi_vault_test.go** (2 tests)
- `TestVaultKeyIsolation`: Create files in two vaults. Verify: vault A private key cannot decrypt vault B file.
- `TestVaultIndependentRoots`: Create root in each vault. Verify: different P_node for each root, different Metanet DAG trees.

**16_fund_external_test.go** (2 tests)
- `TestFundExternalUTXO`: Send coins to engine's fee address via regtest, register UTXO manually. Verify: engine can use it to build tx.
- `TestFundAndBroadcast`: Fund engine, use funded UTXO to create and broadcast a Metanet root tx.

### Category D: Daemon HTTP API

**17_daemon_handshake_test.go** (3 tests)
- `TestMethod42Handshake`: POST /_bitfs/handshake with buyer_pub + nonce. Verify: response has seller_pub, nonce_s, session_id.
- `TestHandshakeSessionRetrieve`: Complete handshake, use session_id to verify session exists.
- `TestHandshakeInvalidPubkey`: POST with malformed pubkey. Verify: 400 error.

**18_daemon_buy_api_test.go** (3 tests)
- `TestGetBuyInfo`: Upload paid file, GET /buy/{txid}. Verify: response has capsule_hash, price, payment_addr.
- `TestSubmitHTLCGetCapsule`: Build and submit valid HTLC funding tx via POST /buy/{txid}. Verify: response contains correct capsule.
- `TestSubmitInvalidHTLC`: POST /buy/{txid} with garbage data. Verify: error response.

**19_daemon_content_neg_test.go** (4 tests)
- `TestContentNegJSON`: GET path with Accept: application/json. Verify: JSON NodeInfo response.
- `TestContentNegHTML`: GET path with Accept: text/html. Verify: HTML response with file info.
- `TestContentNegMarkdown`: GET path with Accept: text/markdown. Verify: Markdown response.
- `TestDirListingHTML`: GET directory path with Accept: text/html. Verify: HTML with child entries.

**20_daemon_paymail_test.go** (2 tests)
- `TestBSVAliasCapabilities`: GET /.well-known/bsvalias. Verify: JSON capabilities document with correct fields.
- `TestPKILookup`: GET /api/v1/pki/{handle}. Verify: returns pubkey for known handle.

**21_daemon_spv_endpoint_test.go** (2 tests)
- `TestSPVProofEndpoint`: Broadcast tx, mine block, GET /spv/proof/{txid}. Verify: confirmed=true, block_hash present.
- `TestSPVProofNotFound`: GET /spv/proof/{nonexistent}. Verify: error response.

### Category E: Client Library Roundtrip

**22_client_roundtrip_test.go** (4 tests)
- `TestClientGetMeta`: Upload file via engine, query via Client.GetMeta through daemon. Verify: metadata matches.
- `TestClientGetData`: Upload free file, retrieve via Client.GetData. Verify: ciphertext matches stored content.
- `TestClientBuyFlow`: Upload paid file, Client.GetBuyInfo → Client.SubmitHTLC → decrypt with capsule.
- `TestClientVerifySPV`: Broadcast tx, Client.VerifySPV. Verify: confirmed.

### Category F: Edge Cases & Stress

**23_error_paths_test.go** (4 tests)
- `TestDoubleSpend`: Broadcast tx, try to spend same UTXO again. Verify: RPC rejects.
- `TestMalformedTxBroadcast`: Send invalid raw tx hex. Verify: RPC error.
- `TestInsufficientFee`: Try to build tx with empty fee wallet. Verify: engine returns allocation error.
- `TestDuplicateNodeCreation`: Try to create two nodes at same path. Verify: error or overwrite semantics.

**24_large_file_test.go** (2 tests)
- `TestLargeFileUpload`: Upload 1MB file with Method 42 encryption. Verify: encrypted content on chain, decrypt matches original.
- `TestLargeFileRetrievalViaDaemon`: Upload 1MB, retrieve via daemon HTTP. Verify: content integrity.

**25_self_update_chain_test.go** (2 tests)
- `TestMultipleUpdates`: Create file, perform 3 sequential SelfUpdates with different content. Verify: each update readable, latest version has correct content.
- `TestUpdatePreservesIdentity`: SelfUpdate a file. Verify: P_node and parentTxID unchanged, only content and TxID change.

## Testutil Additions

New helpers in `e2e/testutil/`:

```go
// engine_helpers.go — Engine setup for E2E tests
func SetupEngine(t *testing.T, node *RegtestNode) (*engine.Engine, string)
func FundEngineWallet(t *testing.T, eng *engine.Engine, node *RegtestNode)
func EngineUploadFile(t *testing.T, eng *engine.Engine, path, content string, access int) *engine.Result

// daemon_helpers.go — Daemon setup for HTTP API tests
func SetupDaemon(t *testing.T, eng *engine.Engine) (*daemon.Daemon, string)
func DaemonClient(t *testing.T, baseURL string) *client.Client
```

## Excluded from Scope

- **Shell REPL**: Readline/TTY interaction is brittle to E2E test. All shell commands delegate to engine methods already tested.
- **Publishing/DNSLink**: Requires real DNS records, not feasible in regtest environment.
- **Rate limiting**: Timing-dependent, better suited to integration tests with mocks.
- **Concurrent operations**: Complex coordination, defer to separate stress-test suite.

## Test Execution

Same as existing E2E tests:
```bash
cd bitfs/e2e && docker compose up -d
go test -tags e2e ./e2e/... -v -timeout 300s
```

Timeout increased from 120s to 300s to accommodate larger test suite.

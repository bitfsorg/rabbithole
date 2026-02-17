# BitFS Implementation Task Breakdown

Tasks ordered by dependency. Each phase builds on the previous. Estimated ~938 test cases total across all phases (per TestDesign document).

---

## Phase 1: Foundation (Cryptography + Wallet + Transactions)

### Task 1: internal/method42 -- Method 42 ECDH Encryption Engine
- **Package**: `internal/method42/`
- **Files**: `encrypt.go`, `ecdh.go`, `kdf.go`, `access.go`, `method42_test.go`
- **Description**: Implement the core encryption system. ECDH key exchange on secp256k1, HKDF-SHA256 key derivation, AES-256-GCM encryption/decryption, three access modes (private/free/paid), capsule computation for HTLC, re-encryption between modes.
- **Acceptance Criteria**:
  - [x] ComputeKeyHash returns SHA256(SHA256(plaintext))
  - [x] ECDH computes correct shared secret x-coordinate
  - [x] DeriveAESKey produces deterministic 32-byte keys via HKDF
  - [x] Encrypt/Decrypt round-trip succeeds for all three access modes
  - [x] FreePrivateKey (scalar 1) produces reproducible encryption
  - [x] DecryptWithCapsule works for buyer flow
  - [x] ReEncrypt correctly converts between FREE and PRIVATE
  - [x] Key hash verification catches tampered content
  - [x] All error conditions produce correct error types
- **Estimated Tests**: 45
- **Dependencies**: go-sdk (ec primitives), golang.org/x/crypto (hkdf)

### Task 2: internal/wallet -- HD Wallet (BIP32/BIP39)
- **Package**: `internal/wallet/`
- **Files**: `seed.go`, `hd.go`, `vault.go`, `network.go`, `wallet_test.go`
- **Description**: BIP39 mnemonic generation/validation, BIP32 key derivation with BitFS path scheme (m/44'/236'/...), Argon2id seed encryption, vault CRUD, fee key chain derivation, network configuration.
- **Acceptance Criteria**:
  - [x] GenerateMnemonic produces valid 12/24-word mnemonics
  - [x] SeedFromMnemonic is deterministic (same input -> same seed)
  - [x] EncryptSeed/DecryptSeed round-trip with correct password
  - [x] DecryptSeed fails with wrong password (ErrDecryptionFailed)
  - [x] Checksum verification catches corrupted data
  - [x] DeriveNodeKey produces correct path m/44'/236'/(V+1)'/0/0/...
  - [x] DeriveFeeKey produces correct path m/44'/236'/0'/chain/index
  - [x] Hardened vs non-hardened derivation works correctly
  - [x] MaxFileIndex and MaxPathDepth limits enforced
  - [x] Vault create/list/rename/delete operations
  - [x] Network configs (mainnet/testnet/regtest) load correctly
  - [x] Same mnemonic+passphrase always produces same keys
- **Estimated Tests**: 55
- **Dependencies**: go-sdk (bip32, bip39, ec), golang.org/x/crypto (argon2)

### Task 3: internal/tx -- BSV Transaction Construction
- **Package**: `internal/tx/`
- **Files**: `metanet_tx.go`, `utxo.go`, `opreturn.go`, `tx_test.go`
- **Description**: Build the four Metanet transaction templates (CreateRoot, CreateChild, SelfUpdate, DataTransaction). OP_RETURN construction and parsing. UTXO tracking for the self-sustaining chain. Fee estimation.
- **Acceptance Criteria**:
  - [x] BuildCreateRoot produces valid tx with correct OP_RETURN format
  - [x] BuildCreateChild spends P_parent UTXO and refreshes it on Output 2
  - [x] BuildSelfUpdate preserves ParentTxID across updates
  - [x] BuildDataTransaction embeds content with OP_DROP
  - [x] BuildOPReturn/ParseOPReturn round-trip correctly
  - [x] MetaFlag (0x6d657461) is correctly placed
  - [x] Dust limit (546 sat) enforced on all P2PKH outputs
  - [x] Fee estimation produces reasonable values
  - [x] UTXO tracking correctly follows refresh chain
  - [x] Insufficient funds errors are clear
- **Estimated Tests**: 40
- **Dependencies**: go-sdk (transaction, script, ec), internal/wallet

---

## Phase 2: Filesystem (DAG + Verification + Storage)

### Task 4: internal/metanet -- Metanet DAG Parser
- **Package**: `internal/metanet/`
- **Files**: `node.go`, `parser.go`, `resolve.go`, `directory.go`, `link.go`, `metanet_test.go`
- **Description**: Parse Metanet transactions into Node structs, implement Unix filesystem operations (path resolution, directory listing, link following), version resolution (latest block height + TTOR), price inheritance.
- **Acceptance Criteria**:
  - [x] ParseNode extracts P_node, ParentTxID, Protobuf payload
  - [x] ResolvePath traverses directories correctly
  - [x] "." and ".." navigation works (.. cannot escape root)
  - [x] Soft link following with max depth 10
  - [x] Hard link detection (same P_node, multiple ChildEntries)
  - [x] Remote soft links return appropriate error
  - [x] LatestVersion correctly orders by block height then TTOR
  - [x] AddChild/RemoveChild/RenameChild directory operations
  - [x] NextChildIndex monotonically increases (deleted indices never reused)
  - [x] InheritPricePerKB walks up directory tree
  - [x] Three node types (FILE/DIR/LINK) correctly parsed
- **Estimated Tests**: 65
- **Dependencies**: internal/tx, protobuf, go-sdk (ec)

### Task 5: internal/spv -- SPV Light Client
- **Package**: `internal/spv/`
- **Files**: `merkle.go`, `header.go`, `verify.go`, `store.go`, `spv_test.go`
- **Description**: Merkle proof verification, block header chain validation, full SPV verification chain (tx integrity -> Merkle proof -> block header -> longest chain). Header and transaction storage interfaces.
- **Acceptance Criteria**:
  - [x] VerifyMerkleProof computes correct root from branch
  - [x] ComputeMerkleRoot handles odd/even number of leaves
  - [x] VerifyTransaction completes full 4-step verification chain
  - [x] VerifyHeaderChain validates PrevBlock linkage
  - [x] SerializeHeader/DeserializeHeader round-trip (80 bytes)
  - [x] DoubleHash matches known BSV block hashes
  - [x] Unconfirmed transactions correctly flagged
  - [x] Invalid proofs rejected with appropriate errors
- **Estimated Tests**: 35
- **Dependencies**: crypto/sha256

### Task 6: internal/storage -- Content Storage
- **Package**: `internal/storage/`
- **Files**: `store.go`, `filestore.go`, `storage_test.go`
- **Description**: File-based content-addressed storage. Flat KV where key_hash maps to ciphertext files, with directory sharding by first byte of hash.
- **Acceptance Criteria**:
  - [x] Put/Get round-trip for various content sizes
  - [x] Has returns correct existence check
  - [x] Delete removes content
  - [x] Size returns correct byte count
  - [x] List returns all stored hashes
  - [x] Directory sharding creates proper subdirectories
  - [x] Invalid key hash (not 32 bytes) rejected
  - [x] Concurrent access safety
- **Estimated Tests**: 25
- **Dependencies**: os, encoding/hex

---

## Phase 3: Network (Identity + Payment + Daemon)

### Task 7: internal/paymail -- Paymail Identity Resolution
- **Package**: `internal/paymail/`
- **Files**: `uri.go`, `resolve.go`, `dns.go`, `paymail_test.go`
- **Description**: Parse bitfs:// URIs, detect address type (Paymail/@, DNSLink, bare pubkey), DNS SRV/TXT resolution, Paymail capability discovery, PKI resolution.
- **Acceptance Criteria**:
  - [x] ParseURI correctly classifies all three address types
  - [x] Paymail URI extracts alias and domain
  - [x] DNSLink URI extracts domain
  - [x] Bare pubkey URI extracts compressed key bytes
  - [x] Invalid URIs produce clear errors
  - [x] Path component parsing handles edge cases
  - [x] DNS resolution (SRV, TXT) with timeout handling
  - [x] Paymail capability discovery from .well-known
  - [x] PKI resolution returns valid public key
- **Estimated Tests**: 35
- **Dependencies**: net, net/http, net/url

### Task 8: internal/x402 -- x402 Payment Protocol
- **Package**: `internal/x402/`
- **Files**: `invoice.go`, `headers.go`, `htlc.go`, `verify.go`, `x402_test.go`
- **Description**: Invoice creation, HTTP 402 headers, HTLC script construction, payment verification.
- **Acceptance Criteria**:
  - [x] CalculatePrice correctly computes ceil(pricePerKB * size / 1024)
  - [x] NewInvoice generates valid invoices with expiry
  - [x] SetPaymentHeaders/ParsePaymentHeaders round-trip
  - [x] BuildHTLC produces correct IF/ELSE/ENDIF script
  - [x] VerifyPayment validates tx output against invoice
  - [x] Expired invoices rejected
  - [x] ParseHTLCPreimage extracts capsule from spending tx
- **Estimated Tests**: 30
- **Dependencies**: go-sdk (transaction, script), internal/method42

### Task 9: internal/daemon -- BitFS Daemon (LFCP)
- **Package**: `internal/daemon/`
- **Files**: `daemon.go`, `routes.go`, `handshake.go`, `content.go`, `webmcp.go`, `daemon_test.go`
- **Description**: HTTP server with all endpoints, Method 42 handshake, content negotiation, x402 payment flow, WebMCP declarations, Paymail server capabilities.
- **Acceptance Criteria**:
  - [x] Health check endpoint returns 200
  - [x] Content negotiation returns HTML/Markdown/JSON based on Accept header
  - [x] Free content served directly
  - [x] Paid content returns 402 with correct headers
  - [x] Method 42 handshake establishes authenticated session
  - [x] HTLC buy flow: capsule_hash -> submit HTLC -> reveal capsule
  - [x] x402 payment verification accepts valid transactions
  - [x] WebMCP forms included in HTML responses
  - [x] Paymail .well-known endpoint serves capabilities
  - [x] Rate limiting enforced
  - [x] Graceful shutdown
- **Estimated Tests**: 80
- **Dependencies**: all internal packages, net/http

---

## Phase 4: CLI (User Interface)

### Task 10: cmd/bitfs -- Main CLI
- **Package**: `cmd/bitfs/`
- **Files**: `main.go`, `cmd_put.go`, `cmd_mkdir.go`, `cmd_rm.go`, `cmd_mv.go`, `cmd_cp.go`, `cmd_link.go`, `cmd_sell.go`, `cmd_encrypt.go`, `cmd_vault.go`, `cmd_wallet.go`, `cmd_publish.go`, `cmd_daemon.go`, `cmd_shell.go`, integration tests
- **Description**: Full CLI implementation with all subcommands, Cobra command tree, Viper configuration, interactive shell (FTP-style REPL).
- **Acceptance Criteria**:
  - [x] All subcommands parse arguments correctly
  - [x] --json flag produces valid JSON output
  - [x] Exit codes follow specification
  - [x] Error messages are clear and actionable
  - [x] Shell mode supports all documented commands
  - [x] Shell supports local/remote navigation (lcd/cd)
  - [x] Password prompts use terminal (not stdin echo)
  - [x] BITFS_HOME environment variable override works
  - [x] Configuration file is read correctly
- **Estimated Tests**: 120
- **Dependencies**: cobra, viper, all internal packages

### Task 11: cmd/b* -- Read-only Tools
- **Package**: `cmd/bls/`, `cmd/bcat/`, `cmd/bget/`, `cmd/bstat/`, `cmd/btree/`
- **Files**: `main.go` in each, shared `internal/client/` utilities
- **Description**: Five independent binaries for read-only filesystem access. Each wraps shared client code with specific output formatting.
- **Acceptance Criteria**:
  - [x] bls produces ls-style output for directories
  - [x] bcat outputs file content to stdout
  - [x] bget downloads file to local filesystem
  - [x] bstat shows file metadata (size, hash, owner, time, access)
  - [x] btree shows recursive directory tree
  - [x] All tools support --json flag
  - [x] --buy flag triggers purchase flow
  - [x] --offline uses cache only
  - [x] URI parsing handles all three address types
  - [x] Free content auto-decrypted
- **Estimated Tests**: 75
- **Dependencies**: cobra, internal/paymail, internal/method42, internal/x402

---

## Phase Summary

| Phase | Packages | Est. Tests | Cumulative |
|-------|----------|-----------|------------|
| 1 Foundation | method42, wallet, tx | 140 | 140 |
| 2 Filesystem | metanet, spv, storage | 125 | 265 |
| 3 Network | paymail, x402, daemon | 145 | 410 |
| 4 CLI | cmd/bitfs, cmd/b* | 195 | 605 |
| Integration | cross-package | ~333 | ~938 |

**Total estimated**: ~938 test cases (matching TestDesign document).

---

## Implementation Notes

1. **go-sdk is the ONLY BSV dependency**: `github.com/bsv-blockchain/go-sdk`. No other BSV libraries.
2. **Testing**: Table-driven tests using `github.com/stretchr/testify`. Each package has comprehensive unit tests.
3. **Protobuf**: `BitFSPayload` proto file to be generated from the schema in SystemDesign section 4.
4. **Error wrapping**: Use `fmt.Errorf("context: %w", err)` for error chains.
5. **Context propagation**: Long-running operations accept `context.Context` for cancellation.

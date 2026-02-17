# Feature Specification: BitFS Core Filesystem

**Feature Branch**: `001-bitfs-core`
**Created**: 2026-02-17
**Status**: Draft
**Input**: Design documents from `design/0-OverallDesign.zh.md`, `design/bitfs/1-4*.zh.md`

## User Scenarios & Testing

### User Story 1 - Encrypted File Storage (Priority: P1)

A file owner creates an HD wallet, then stores files on BSV blockchain
with automatic Method 42 ECDH encryption. Files are content-addressed
and encrypted by default (Private mode). The owner can also set files
to Free (D_node=1 trivial key trick) or Paid (requires HTLC capsule).

**Why this priority**: Core value proposition. Without encrypted storage
nothing else works.

**Independent Test**: Create wallet, put a file, get it back, verify
round-trip encryption/decryption with all three access modes.

**Acceptance Scenarios**:

1. **Given** a wallet with funds, **When** `bitfs put file.txt /docs/`,
   **Then** file is encrypted with Method 42, Metanet CreateChild tx
   is built, and encrypted content is stored locally.
2. **Given** an encrypted file at `/docs/file.txt`, **When**
   `bitfs get /docs/file.txt`, **Then** file is decrypted and written
   to local filesystem with original content.
3. **Given** a file in Private mode, **When** `bitfs encrypt --mode free /docs/file.txt`,
   **Then** file is re-encrypted with D_node=1 so anyone can derive
   the decryption key.

---

### User Story 2 - Unix Filesystem Navigation (Priority: P2)

Users and AI Agents navigate the Metanet DAG as a Unix filesystem.
Directory operations (mkdir, ls, cd), file operations (cat, stat),
symlinks and hardlinks all work as expected. Five stateless read-only
tools (bls, bcat, bget, bstat, btree) provide pipe-friendly access.

**Why this priority**: Filesystem abstraction is what makes BitFS
usable. Agent integration depends on Unix semantics + JSON output.

**Independent Test**: Build a directory tree with files, directories,
and links, then navigate with b* tools and verify correct output.

**Acceptance Scenarios**:

1. **Given** a root node, **When** `bitfs mkdir /docs/reports`,
   **Then** nested directories are created with correct ChildEntry
   indices, inode=P_node mapping.
2. **Given** files in `/docs/`, **When** `bls --json bitfs://owner/docs/`,
   **Then** JSON array of entries with name, type, size, access mode.
3. **Given** a symlink `/shortcut -> /docs/reports/q4.pdf`, **When**
   `bcat bitfs://owner/shortcut`, **Then** symlink is followed
   (max depth 10) and target content is output.
4. **Given** a deep tree, **When** `btree bitfs://owner/`, **Then**
   recursive tree output with indentation like Unix `tree` command.

---

### User Story 3 - Content Monetization via HTLC (Priority: P3)

Content owners sell access to paid files through trustless HTLC
atomic swaps. Buyers pay BSV, sellers reveal capsule (ECDH preimage),
buyers derive decryption key. x402 HTTP payment protocol enables
web-native purchasing.

**Why this priority**: Monetization is the business model. Requires
P1 (encryption) and P2 (filesystem) to be complete.

**Independent Test**: Owner sets price, buyer initiates HTLC purchase,
seller reveals capsule, buyer decrypts content.

**Acceptance Scenarios**:

1. **Given** a file with `pricePerKB=100`, **When** buyer requests via
   HTTP, **Then** daemon returns 402 with x402 payment headers
   (invoice, capsule_hash, amount, expiry).
2. **Given** a valid HTLC transaction, **When** seller verifies payment,
   **Then** capsule is revealed and buyer can derive AES key.
3. **Given** a directory tree, **When** buyer purchases with
   `--buy-tree`, **Then** single HTLC unlocks all non-hardened child
   nodes via xpub+capsule.

---

### User Story 4 - Self-Hosting Daemon (Priority: P4)

File owners run a BitFS daemon that serves content over HTTP with
LFCP protocol, DNSLink publishing, Method 42 ECDH handshake for
authenticated sessions, and WebMCP forms for AI agent interaction.

**Why this priority**: Daemon enables network access. Depends on all
core libraries being complete.

**Independent Test**: Start daemon, fetch free content via HTTP,
complete ECDH handshake, purchase paid content via x402 flow.

**Acceptance Scenarios**:

1. **Given** a running daemon, **When** GET request with
   `Accept: application/json`, **Then** content negotiation returns
   JSON metadata.
2. **Given** TLS configured, **When** ECDH handshake completes,
   **Then** session is authenticated and private content accessible.
3. **Given** WebMCP enabled, **When** AI agent sends POST to form
   endpoint, **Then** structured response suitable for LLM consumption.

---

### User Story 5 - SPV Verification (Priority: P5)

All file metadata is verified locally via SPV (Simplified Payment
Verification). Owners store their own transactions + Merkle proofs.
Visitors verify metadata received from owner daemons. No blockchain
queries required for normal operation.

**Why this priority**: Security foundation. SPV ensures trustless
verification without depending on indexers.

**Independent Test**: Create transactions, build Merkle proofs,
verify complete chain (tx integrity -> Merkle proof -> block header
-> longest chain).

**Acceptance Scenarios**:

1. **Given** a transaction with Merkle proof, **When**
   `spv.VerifyTransaction()`, **Then** 4-step verification chain passes.
2. **Given** an unconfirmed transaction, **When** verification attempted,
   **Then** tx is accepted but flagged as unconfirmed.
3. **Given** an invalid Merkle proof, **When** verification attempted,
   **Then** appropriate error returned.

---

### Edge Cases

- What happens when HTLC expires before seller reveals capsule?
  Buyer reclaims funds via CLTV timelock.
- How does system handle concurrent writes to same directory?
  Index allocation is monotonically increasing, never reused.
- What happens when symlink chain exceeds max depth (10)?
  Returns ErrSymlinkLoop error.
- How does system handle files larger than BSV tx size limit?
  Content chunking with recombination_hash verification.
- What happens when wallet seed encryption password is wrong?
  ErrDecryptionFailed with no key material leaked.

## Requirements

### Functional Requirements

- **FR-001**: System MUST implement Method 42 ECDH encryption with
  three access modes (Private, Free, Paid)
- **FR-002**: System MUST implement BIP32/BIP39 HD wallet with
  BitFS path scheme m/44'/236'/N'
- **FR-003**: System MUST build four Metanet transaction templates
  (CreateRoot, CreateChild, SelfUpdate, DataTransaction)
- **FR-004**: System MUST parse Metanet DAG as Unix filesystem
  with inode=P_node, dirent=ChildEntry, symlinks, hardlinks
- **FR-005**: System MUST verify transactions via SPV (Merkle proof
  + block header chain) without querying blockchain
- **FR-006**: System MUST implement content-addressed storage with
  hash-sharded directories
- **FR-007**: System MUST resolve bitfs:// URIs via Paymail, DNSLink,
  or bare public key
- **FR-008**: System MUST implement x402 payment protocol with HTLC
  atomic swaps
- **FR-009**: System MUST serve content via HTTP daemon with LFCP
  protocol, content negotiation, and WebMCP
- **FR-010**: System MUST provide six CLI binaries (bitfs + 5 b* tools)
  with JSON output for Agent integration
- **FR-011**: System MUST encrypt wallet seed with Argon2id + AES-256-GCM
- **FR-012**: System MUST enforce P2PKH dust limit (546 satoshis)

### Key Entities

- **Node**: Metanet DAG node (P_node public key, ParentTxID, type,
  children, metadata). Types: FILE, DIR, LINK.
- **ChildEntry**: Directory entry (index, name, type, P_child).
  Index monotonically increasing, never reused on delete.
- **Wallet**: HD wallet with BIP39 seed, Argon2id encryption, multiple
  vaults, fee chain at m/44'/236'/0'.
- **Invoice**: x402 payment request (amount, capsule_hash, expiry,
  payment address).
- **MerkleProof**: SPV verification data (tx hash, branch hashes,
  block header, confirmations).

## Success Criteria

### Measurable Outcomes

- **SC-001**: All 938 planned test cases pass (currently 659/938)
- **SC-002**: All six CLI binaries build successfully
- **SC-003**: Method 42 encryption/decryption round-trips for all
  three access modes with deterministic key derivation
- **SC-004**: SPV verification chain completes in under 10ms for
  cached block headers
- **SC-005**: Daemon handles 100 concurrent HTTP requests
- **SC-006**: All b* tools produce valid JSON with --json flag

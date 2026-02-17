# Research: BitFS Core Filesystem

**Feature**: 001-bitfs-core
**Date**: 2026-02-17

## Technology Decisions

All technology decisions were made during the design phase and are
documented in `design/bitfs/1-ConceptDesign.zh.md` (86 design decisions).
No NEEDS CLARIFICATION items remain.

### 1. BSV Library Selection

**Decision**: `github.com/bsv-blockchain/go-sdk` v1.2.18
**Rationale**: Official BSV SDK with complete support for EC primitives,
BIP32, BIP39, transaction building, and script construction. Only BSV
dependency allowed by design decision #24.
**Alternatives**: go-bt (older, less maintained), libsv (C bindings,
complex build), hand-rolled (too risky for crypto)

### 2. Encryption Scheme

**Decision**: Method 42 — ECDH (secp256k1) + HKDF-SHA256 + AES-256-GCM
**Rationale**: Reuses BIP32 key hierarchy for per-file deterministic
encryption. ECDH preserves algebraic relationships needed for capsule-based
key sharing. AES-256-GCM provides authenticated encryption.
**Formula**: `aes_key = HKDF-SHA256(ikm=ECDH(D_node, P_node).x, salt=key_hash, info="bitfs-file-encryption")`
**Alternatives**: NaCl box (no BIP32 integration), RSA (key size too large
for blockchain), hybrid PGP (too complex)

### 3. Key Derivation Path

**Decision**: BIP32 path `m/44'/236'/N'/chain/index`
**Rationale**: Coin type 236 registered for BitFS. Vault index N starts
at 1 (0 reserved for fee chain). Hardened derivation for vault isolation.
Non-hardened for child nodes to enable capsule-based sharing.
**Alternatives**: Custom derivation (no BIP32 compatibility), flat keys
(no hierarchy)

### 4. Filesystem Model

**Decision**: Unix inode/dirent model mapped to Metanet DAG
**Rationale**: Most widely understood filesystem semantics. inode=P_node
(public key), dirent=ChildEntry (name→pubkey mapping). Supports hard
links (multiple entries → same P_node) and symbolic links (LINK nodes).
**Alternatives**: Plan 9 (too niche), flat namespace (insufficient for
tree structures), custom model (unnecessary complexity)

### 5. Data Encoding

**Decision**: Protocol Buffers (Protobuf)
**Rationale**: Compact binary encoding, forward/backward version
compatibility, language-neutral schema definition. BitFSPayload proto
with 56 fields covers all metadata.
**Alternatives**: JSON (too verbose for blockchain), CBOR (less tooling),
custom binary (no schema evolution)

### 6. Content Storage

**Decision**: Content-addressed file store with hash-sharded directories
**Rationale**: key_hash (SHA256(SHA256(plaintext))) serves as both
content identifier and encryption key derivation input. First byte of
hash determines subdirectory for even distribution.
**Alternatives**: Database (overkill for blob storage), flat directory
(too many files), CAS with dedup (unnecessary, content already encrypted)

### 7. Payment Protocol

**Decision**: x402 (HTTP 402) with HTLC atomic swaps
**Rationale**: Web-native payment using HTTP status codes. HTLC provides
trustless exchange: buyer locks BSV, seller reveals capsule (ECDH
preimage), buyer derives decryption key. No escrow needed.
**Alternatives**: Lightning Network (BSV doesn't support), centralized
payment (defeats decentralization), simple P2PKH (no atomicity)

### 8. SPV Verification

**Decision**: Local tx + Merkle proof, no blockchain queries
**Rationale**: Owner stores own transactions with Merkle proofs. Visitor
verifies against block header chain. Degradation to third-party indexer
only as fallback.
**Alternatives**: Full node (too heavy for CLI), always-online indexer
(centralization risk), trust-the-server (no verification)

### 9. Wallet Encryption

**Decision**: Argon2id + AES-256-GCM for seed encryption at rest
**Rationale**: Argon2id is memory-hard KDF resistant to GPU/ASIC attacks.
AES-256-GCM provides authenticated encryption. Stored at ~/.bitfs/wallet.enc.
**Alternatives**: PBKDF2 (insufficient memory hardness), scrypt (less
tunable), bcrypt (not designed for key derivation)

### 10. CLI Framework

**Decision**: Cobra + Viper (Go standard CLI libraries)
**Rationale**: Industry standard for Go CLI applications. Cobra for
command tree, Viper for configuration management. Supports subcommands,
flags, environment variables, config files.
**Alternatives**: urfave/cli (less feature-rich), custom (unnecessary work)

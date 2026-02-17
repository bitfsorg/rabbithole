# Data Model: BitFS Core Filesystem

**Feature**: 001-bitfs-core
**Date**: 2026-02-17

## Core Entities

### Node (Metanet DAG Node)

Represents a filesystem object in the Metanet DAG. Maps to Unix inode.

| Field | Type | Description |
|-------|------|-------------|
| PNode | *ec.PublicKey | Node public key (= inode number) |
| ParentTxID | []byte | Parent transaction ID (32 bytes) |
| Type | NodeType | FILE, DIR, or LINK |
| Children | []ChildEntry | Directory entries (DIR only) |
| Version | uint32 | Protobuf schema version |
| AccessMode | Access | Private(0), Free(1), Paid(2) |
| PricePerKB | uint64 | Price in satoshis (Paid mode) |
| ContentHash | []byte | SHA256(SHA256(plaintext)) = key_hash |
| EncryptedData | []byte | AES-256-GCM ciphertext |
| CreatedAt | int64 | Unix timestamp |
| BlockHeight | uint32 | Confirmation block height |
| TxIndex | uint32 | TTOR position within block |

**Validation Rules**:
- PNode MUST be a valid compressed secp256k1 public key (33 bytes)
- ParentTxID MUST be 32 bytes (or nil for root nodes)
- Type MUST be one of FILE(0), DIR(1), LINK(2)
- PricePerKB MUST be 0 for Free and Private modes
- ContentHash MUST be 32 bytes (double SHA256)

**State Transitions**:
- Created → Active (on blockchain confirmation)
- Active → Updated (SelfUpdate transaction)
- Active → Deleted (removed from parent directory)

### ChildEntry (Directory Entry)

Maps to Unix dirent. Links a name to a child node.

| Field | Type | Description |
|-------|------|-------------|
| Index | uint32 | Monotonically increasing, never reused |
| Name | string | Entry name (max 255 bytes) |
| Type | NodeType | FILE, DIR, or LINK |
| PChild | *ec.PublicKey | Child node public key |

**Validation Rules**:
- Index MUST be unique within parent, MUST NOT reuse deleted indices
- Name MUST NOT contain '/' or null bytes
- Name MUST NOT be "." or ".."
- PChild MUST be a valid compressed secp256k1 public key

### Wallet

HD wallet with BIP39 seed and BIP32 key derivation.

| Field | Type | Description |
|-------|------|-------------|
| EncryptedSeed | []byte | Argon2id + AES-256-GCM encrypted seed |
| Salt | []byte | Argon2id salt (16 bytes) |
| Nonce | []byte | AES-256-GCM nonce (12 bytes) |
| Checksum | []byte | HMAC-SHA256 of plaintext seed |
| Vaults | []Vault | Named key subtrees |
| Network | NetworkConfig | mainnet/testnet/regtest |

**Key Derivation Tree**:
```
m/44'/236'/0'          ← Fee chain (shared across all vaults)
m/44'/236'/1'/0/0      ← Vault 1, root node
m/44'/236'/1'/0/0/i    ← Vault 1, child index i
m/44'/236'/2'/0/0      ← Vault 2, root node
...
```

### Vault

Named key subtree within a wallet.

| Field | Type | Description |
|-------|------|-------------|
| Name | string | Human-readable vault name |
| Index | uint32 | BIP32 account index (starts at 1) |
| RootPNode | *ec.PublicKey | Root node public key |
| MaxFileIndex | uint32 | Highest allocated child index |
| CreatedAt | int64 | Unix timestamp |

### Invoice (x402)

Payment request for content access.

| Field | Type | Description |
|-------|------|-------------|
| Amount | uint64 | Total price in satoshis |
| CapsuleHash | []byte | SHA256(capsule) for HTLC |
| PaymentAddr | string | P2PKH address for payment |
| Expiry | int64 | Unix timestamp expiration |
| ContentPath | string | Requested file path |
| ContentSize | uint64 | File size in bytes |

**Validation Rules**:
- Amount MUST be >= 546 satoshis (dust limit)
- Expiry MUST be in the future
- CapsuleHash MUST be 32 bytes

### MerkleProof (SPV)

Verification data for SPV validation.

| Field | Type | Description |
|-------|------|-------------|
| TxHash | []byte | Transaction hash (32 bytes) |
| Branches | [][]byte | Merkle tree branch hashes |
| Index | uint32 | Leaf position in Merkle tree |
| BlockHeader | []byte | 80-byte block header |
| Confirmations | uint32 | Number of confirmations |

**Validation Rules**:
- TxHash MUST be 32 bytes
- BlockHeader MUST be exactly 80 bytes
- Computing Merkle root from branches MUST match header's MerkleRoot

## Entity Relationships

```
Wallet 1──* Vault
Vault  1──1 Node (root)
Node   1──* ChildEntry (if DIR)
ChildEntry *──1 Node (via PChild)
Node   1──* MerkleProof
Node   1──0..1 Invoice (if Paid mode)
```

## Transaction Templates

Four Metanet transaction types map entities to BSV blockchain:

| Template | Inputs | Outputs | Purpose |
|----------|--------|---------|---------|
| CreateRoot | Fee UTXO | OP_RETURN(meta), P2PKH(P_root) | Create root node |
| CreateChild | Fee UTXO + P_parent UTXO | OP_RETURN(meta), P2PKH(P_child), P2PKH(P_parent refresh) | Create child node |
| SelfUpdate | Fee UTXO + P_node UTXO | OP_RETURN(meta), P2PKH(P_node refresh) | Update node metadata |
| DataTransaction | Fee UTXO | OP_DROP(content), P2PKH(change) | Store content on-chain |

**Invariants**:
- All OP_RETURN outputs start with MetaFlag `0x6d657461`
- CreateChild Output 2 MUST refresh P_parent UTXO for chain continuity
- All P2PKH outputs MUST be >= 546 satoshis (dust limit)

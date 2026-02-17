# Module Specification: internal/contract

## PURPOSE

The `contract` package implements storage contracts (StorageDeal) on the Metanet Chain using standard Bitcoin Script. A StorageDeal is a 1-to-1 agreement between a content Owner and a Metanet Node: the Owner locks MNT tokens into N UTXO outputs (one per challenge period), each containing a pre-computed expected proof hash. The Metanet Node must submit correct storage proofs to claim each period's payment; otherwise, the Owner can reclaim the tokens after the expiry block.

All contract logic uses standard Bitcoin Script opcodes (OP_IF/ELSE, OP_SHA256, OP_CHECKSIG, OP_CHECKLOCKTIMEVERIFY) -- no custom opcodes or virtual machines are introduced.

## PUBLIC API

### Types

```go
// StorageDeal represents the parameters of a storage contract between
// an Owner and a Metanet Node.
type StorageDeal struct {
    OwnerPubKey    []byte   // Owner's compressed public key (33 bytes)
    NodePubKey     []byte   // Metanet Node's compressed public key (33 bytes)
    NumPeriods     uint32   // Number of challenge periods (N)
    TokenPerPeriod uint64   // MNT satoshis locked per period
    BlocksPerPeriod uint32  // Number of blocks per challenge period
    StartBlock     uint32   // Block height when contract begins
    MerkleRoot     [32]byte // Merkle root of the node-specific encrypted data
    NumChunks      uint32   // Total number of data chunks
    ExpectedHashes [][32]byte // Pre-computed expected_hash_k for each period k
}

// Challenge represents a deterministic challenge for period k.
type Challenge struct {
    Period      uint32   // Challenge period index (0-based)
    Seed        [32]byte // SHA256(contract_txid || uint32_le(k))
    ChunkIndex  uint32   // Which chunk to prove: uint32(seed[0:4]) % num_chunks
}

// StorageDealUTXO represents a single period's UTXO in the contract.
type StorageDealUTXO struct {
    Period       uint32   // Period index
    Value        uint64   // Locked MNT satoshis
    ExpireBlock  uint32   // CLTV expiry block height
    ExpectedHash [32]byte // Expected SHA256(proof_data)
    Script       []byte   // Locking script
}
```

### Functions

```go
// NewStorageDeal creates a new StorageDeal with all challenge parameters
// pre-computed. The contractTxID is the TxID of the funding transaction
// (must be known or predicted).
//
// The function pre-computes all N expected hashes by:
//   For each period k:
//     challenge_k = SHA256(contractTxID || uint32_le(k))
//     chunk_index = uint32(challenge_k[0:4]) % numChunks
//     expected_hash_k = SHA256(merkle_proof_k || chunks[chunk_index])
func NewStorageDeal(
    ownerPubKey, nodePubKey []byte,
    numPeriods uint32,
    tokenPerPeriod uint64,
    blocksPerPeriod uint32,
    startBlock uint32,
    merkleRoot [32]byte,
    numChunks uint32,
    chunks [][]byte,        // All data chunks (for pre-computing proofs)
    merkleTree *MerkleTree, // Pre-built Merkle tree
    contractTxID [32]byte,
) (*StorageDeal, error)

// ComputeChallenge deterministically computes the challenge for period k
// given a contract TxID.
func ComputeChallenge(contractTxID [32]byte, period uint32, numChunks uint32) *Challenge

// ComputeExpectedHash computes the expected proof hash for a given challenge.
// expected_hash = SHA256(merkle_proof || chunk_data)
func ComputeExpectedHash(
    chunkIndex uint32,
    chunkData []byte,
    merkleProof [][]byte,
) [32]byte

// BuildDealScript constructs the Bitcoin Script for a single period's
// StorageDeal UTXO.
//
// Script:
//   OP_IF
//     <node_pubkey> OP_CHECKSIGVERIFY
//     OP_SHA256 <expected_hash_k> OP_EQUALVERIFY
//     OP_TRUE
//   OP_ELSE
//     <expire_block> OP_CHECKLOCKTIMEVERIFY OP_DROP
//     <owner_pubkey> OP_CHECKSIG
//   OP_ENDIF
func BuildDealScript(
    nodePubKey []byte,
    ownerPubKey []byte,
    expectedHash [32]byte,
    expireBlock uint32,
) ([]byte, error)

// BuildClaimInput constructs the unlocking script for a Metanet Node
// to claim a period's payment.
// Input: <node_sig> <proof_data> OP_TRUE
func BuildClaimInput(nodeSig []byte, proofData []byte) []byte

// BuildRefundInput constructs the unlocking script for an Owner to
// reclaim tokens after expiry.
// Input: <owner_sig> OP_FALSE
func BuildRefundInput(ownerSig []byte) []byte

// BuildStorageDealTx constructs the complete StorageDeal transaction
// with N outputs (one per period), each containing the appropriate
// locking script.
func BuildStorageDealTx(deal *StorageDeal) ([]*StorageDealUTXO, error)

// ValidateDealParams checks that deal parameters are within acceptable bounds.
func ValidateDealParams(deal *StorageDeal) error
```

## DEPENDENCIES

- `internal/chain` -- Chain parameters (for block height calculations)
- `github.com/bsv-blockchain/go-sdk/transaction` -- Transaction construction
- `github.com/bsv-blockchain/go-sdk/script` -- Script construction
- `crypto/sha256` -- SHA256 hashing
- `encoding/binary` -- uint32 LE serialization

## DATA STRUCTURES

### StorageDeal Script (per-period UTXO)

```
OP_IF
    // Node claim path: <node_sig> <proof_data> 1
    <33-byte node_pubkey> OP_CHECKSIGVERIFY
    OP_SHA256 <32-byte expected_hash_k> OP_EQUALVERIFY
    OP_TRUE
OP_ELSE
    // Owner refund path: <owner_sig> 0
    <4-byte expire_block_le> OP_CHECKLOCKTIMEVERIFY OP_DROP
    <33-byte owner_pubkey> OP_CHECKSIG
OP_ENDIF
```

### Deterministic Challenge Derivation

```
For period k (0-indexed):
    challenge_seed = SHA256(contract_txid || uint32_le(k))
    chunk_index    = uint32_le(challenge_seed[0:4]) % num_chunks
```

The entropy comes from `contract_txid`, which is unpredictable before the transaction is created (it depends on the hash of the transaction contents). Once the contract is on-chain, all challenges are deterministic and publicly verifiable.

### Expected Proof Hash

```
For period k:
    challenge = ComputeChallenge(contract_txid, k, num_chunks)
    merkle_proof = MerkleProof(tree, challenge.ChunkIndex)
    proof_data = Serialize(merkle_proof) || chunk_data[challenge.ChunkIndex]
    expected_hash_k = SHA256(proof_data)
```

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrInvalidPubKey` | Public key is not a valid 33-byte compressed key |
| `ErrZeroPeriods` | NumPeriods is zero |
| `ErrZeroPayment` | TokenPerPeriod is zero |
| `ErrInvalidChunkCount` | NumChunks is zero or does not match data |
| `ErrPeriodTooShort` | BlocksPerPeriod is less than minimum (e.g., 6 blocks) |
| `ErrChunkIndexOutOfRange` | Computed chunk index exceeds NumChunks |
| `ErrMerkleRootMismatch` | Computed Merkle root does not match deal MerkleRoot |
| `ErrScriptTooLarge` | Generated script exceeds acceptable size |

## SECURITY CONSIDERATIONS

1. **Deterministic challenges**: All challenges derive from `SHA256(contract_txid || period)`. The contract TxID is unpredictable before creation (depends on transaction content hash), so the Metanet Node cannot selectively store only challenged chunks.
2. **No block-hash randomness**: The design deliberately avoids using block hashes as entropy to prevent miner manipulation of challenges.
3. **CLTV expiry**: The Owner refund path uses OP_CHECKLOCKTIMEVERIFY with absolute block heights, ensuring the Metanet Node has sufficient time to submit proofs before the Owner can reclaim.
4. **Signature binding**: Both paths require cryptographic signatures (OP_CHECKSIG/VERIFY), preventing unauthorized third parties from claiming or refunding.
5. **Pre-computation**: All expected hashes are embedded in the contract at creation time. This ensures the contract is fully self-contained and verifiable without external oracles.
6. **One UTXO per period**: Each period's payment is isolated in its own UTXO, so the Metanet Node can claim each period independently. Failure in one period does not affect others.

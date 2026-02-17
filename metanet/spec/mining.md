# Module Specification: internal/mining

## PURPOSE

The `mining` package implements merged mining (AuxPoW) for the Metanet Chain, allowing BTC/BSV SHA256 miners to simultaneously mine Metanet Chain blocks without additional work. It also handles BSV anchoring -- periodic publication of Metanet Chain Merkle roots to BSV for long-range attack prevention.

Merged mining is the primary security mechanism for the Metanet Chain. Miners embed a commitment to the Metanet Chain block hash in their BTC/BSV coinbase transaction. The Metanet Chain then validates the auxiliary proof-of-work by verifying the parent chain header, coinbase transaction, and Merkle branch.

## PUBLIC API

### Types

```go
// AuxPoWHeader extends a standard block header with merged mining proof.
type AuxPoWHeader struct {
    // Standard Metanet Chain block header (80 bytes)
    Header chain.BlockHeader

    // Parent chain (BTC/BSV) block header (80 bytes)
    ParentHeader [80]byte

    // Coinbase transaction from the parent chain containing the MNMP marker
    CoinbaseTx []byte

    // Merkle branch proving the coinbase is in the parent block
    CoinbaseBranch [][32]byte

    // Index of coinbase in the parent Merkle tree
    CoinbaseIndex uint32
}

// AnchorTx represents a BSV anchor transaction for Metanet Chain.
type AnchorTx struct {
    Version       uint8    // Protocol version (0x01)
    Flag          [4]byte  // "MNTA"
    StartHeight   uint32   // First Metanet Chain block in range
    EndHeight     uint32   // Last Metanet Chain block in range
    MerkleRoot    [32]byte // Merkle root of the block range
    BlockCount    uint16   // Number of blocks (typically 100)
    PrevAnchorTx  [32]byte // Previous anchor TxID (chain linkage)
}

// DifficultyAdjustment holds the parameters for a difficulty recalculation.
type DifficultyAdjustment struct {
    OldBits         uint32
    ActualTimespan  int64 // Actual seconds for the last 2016 blocks
    ExpectedTimespan int64 // Expected seconds (2016 * 600)
    NewBits         uint32
}
```

### Functions

```go
// ValidateAuxPoW validates the auxiliary proof-of-work for a Metanet Chain block.
// It verifies:
//   1. ParentHeader hash meets Metanet Chain difficulty target
//   2. CoinbaseTx is in ParentHeader's Merkle tree (via CoinbaseBranch)
//   3. CoinbaseTx contains the MNMP magic followed by the Metanet block hash
//   4. The embedded block hash matches SHA256d(Header)
func ValidateAuxPoW(auxpow *AuxPoWHeader, params *chain.Params) error

// FindAuxPoWCommitment scans a coinbase transaction for the MNMP marker
// and returns the committed Metanet Chain block hash.
// Returns nil if no commitment found.
func FindAuxPoWCommitment(coinbaseTx []byte) ([]byte, error)

// BuildCoinbaseCommitment constructs the OP_RETURN output data for
// embedding in a parent chain coinbase transaction.
// Format: OP_RETURN <MNMP> <metanet_block_hash>
func BuildCoinbaseCommitment(blockHash [32]byte) []byte

// VerifyCoinbaseBranch verifies a Merkle branch proving a coinbase
// transaction is included in a block with the given Merkle root.
func VerifyCoinbaseBranch(coinbaseTxHash [32]byte, branch [][32]byte, index uint32, merkleRoot [32]byte) bool

// CalcNextDifficulty computes the new difficulty target for the next
// 2016-block retarget period.
// Uses the Bitcoin algorithm:
//   - Clamp actual timespan to [expectedTimespan/4, expectedTimespan*4]
//   - new_target = old_target * actual_timespan / expected_timespan
func CalcNextDifficulty(oldBits uint32, actualTimespan int64) uint32

// CompactToBig converts a Bitcoin compact target representation to a big.Int.
func CompactToBig(compact uint32) *big.Int

// BigToCompact converts a big.Int target to Bitcoin compact representation.
func BigToCompact(target *big.Int) uint32

// HashMeetsTarget checks whether a block hash meets the given difficulty target.
func HashMeetsTarget(hash [32]byte, bits uint32) bool

// BuildAnchorTx constructs a BSV anchor transaction for a range of
// Metanet Chain blocks.
func BuildAnchorTx(startHeight, endHeight uint32, blockHeaders []chain.BlockHeader, prevAnchorTxID [32]byte) (*AnchorTx, error)

// SerializeAnchorData serializes an AnchorTx into the OP_RETURN payload.
func SerializeAnchorData(anchor *AnchorTx) []byte

// DeserializeAnchorData deserializes an OP_RETURN payload into an AnchorTx.
func DeserializeAnchorData(data []byte) (*AnchorTx, error)

// ValidateAnchorChain validates that a sequence of anchor transactions
// forms a valid chain (each references the previous).
func ValidateAnchorChain(anchors []*AnchorTx) error

// BuildBlockRangeMerkleRoot computes the Merkle root for a range of
// Metanet Chain block headers.
func BuildBlockRangeMerkleRoot(headers []chain.BlockHeader) [32]byte
```

## DEPENDENCIES

- `internal/chain` -- Block header types, chain parameters
- `github.com/bsv-blockchain/go-sdk/transaction` -- BSV transaction construction (for anchors)
- `crypto/sha256` -- SHA256 hashing
- `encoding/binary` -- Serialization
- `math/big` -- Target arithmetic

## DATA STRUCTURES

### Coinbase Commitment Format

```
OP_RETURN <MNMP> <block_hash>

Where:
  OP_RETURN   = 0x6a
  MNMP        = 4 bytes: 0x4d4e4d50 ("MNMP" = Metanet Merged PoW)
  block_hash  = 32 bytes: SHA256d(metanet_chain_block_header)
```

### BSV Anchor OP_RETURN Format

```
OP_RETURN <MNTA> <version> <start_height> <end_height> <merkle_root> <block_count> <prev_anchor_txid>

Where:
  OP_RETURN       = 0x6a
  MNTA            = 4 bytes: 0x4d4e5441 ("MNTA" = Metanet Anchor)
  version         = 1 byte: 0x01
  start_height    = 4 bytes LE: first Metanet Chain block height
  end_height      = 4 bytes LE: last Metanet Chain block height
  merkle_root     = 32 bytes: Merkle root of block range
  block_count     = 2 bytes LE: number of blocks
  prev_anchor_txid = 32 bytes: previous BSV anchor TxID
```

### Difficulty Adjustment Algorithm

```
Every 2016 blocks:
  actual_timespan = block[N].timestamp - block[N-2016].timestamp
  clamped_timespan = clamp(actual_timespan, expected/4, expected*4)
  new_target = old_target * clamped_timespan / expected_timespan

Where expected_timespan = 2016 * 600 seconds = 1,209,600 seconds
```

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrNoAuxPoWCommitment` | Coinbase does not contain MNMP marker |
| `ErrInvalidCoinbaseBranch` | Merkle branch does not prove coinbase inclusion |
| `ErrAuxPoWHashMismatch` | Committed block hash does not match actual header hash |
| `ErrInsufficientAuxPoW` | Parent header hash does not meet Metanet difficulty target |
| `ErrInvalidParentHeader` | Parent header fails basic validation |
| `ErrAnchorHeightMismatch` | Anchor block range is not contiguous |
| `ErrAnchorChainBroken` | Anchor PrevAnchorTx does not reference previous anchor |
| `ErrDifficultyOverflow` | Computed difficulty exceeds representable range |

## SECURITY CONSIDERATIONS

1. **Parent header validation**: Only the hash of the parent header must meet Metanet difficulty -- we do not validate the parent header against parent chain rules (that would require a BTC/BSV full node). This is standard merged mining behavior.
2. **Coinbase branch verification**: The Merkle branch must cryptographically prove the coinbase is included in the parent block, preventing fabricated AuxPoW proofs.
3. **MNMP uniqueness**: Only one MNMP commitment per coinbase is allowed. Multiple commitments indicate an error.
4. **Difficulty clamping**: The 4x cap on difficulty adjustment per period prevents sudden difficulty swings that could destabilize the chain.
5. **BSV anchor integrity**: Anchor transactions form a chain (each references the previous TxID), making it impossible to insert or remove anchors without detection.
6. **Timejacking mitigation**: Miners cannot manipulate Metanet Chain difficulty via parent chain timestamps because difficulty uses Metanet Chain block timestamps, not parent chain timestamps.

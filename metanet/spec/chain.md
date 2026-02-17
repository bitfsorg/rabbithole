# Module Specification: internal/chain

## PURPOSE

The `chain` package implements the Metanet Chain core -- a BSV-homomorphic sidechain for CDN economics. It uses the identical transaction format, Bitcoin Script engine, and UTXO model as BSV, but with a different genesis block and chain parameters tuned for the Metanet CDN incentive layer.

The Metanet Chain's native token is MNT (21M total supply), following the exact same emission schedule as Bitcoin (50 token initial reward, 210,000 block halving). The chain is secured by SHA256 merged mining (AuxPoW) shared with BTC/BSV miners.

## PUBLIC API

### Types

```go
// Params defines the consensus parameters for the Metanet Chain.
type Params struct {
    // Genesis block definition
    GenesisBlock *Block

    // Block parameters
    MaxBlockSize       uint32 // 32MB (33,554,432 bytes)
    TargetBlockTime    time.Duration // 10 minutes
    DifficultyAdjustmentInterval uint32 // 2016 blocks

    // Token economics
    InitialReward      uint64 // 50 MNT in satoshis (5,000,000,000)
    HalvingInterval    uint32 // 210,000 blocks
    MaxTotalSupply     uint64 // 21,000,000 MNT in satoshis (2,100,000,000,000,000)
    SatoshiPerToken    uint64 // 100,000,000

    // Fee parameters
    MinFeePerByte      uint64 // 1 sat/byte

    // BSV anchoring
    AnchorInterval     uint32 // ~100 blocks
    AnchorMagic        []byte // "MNTA"

    // Merged mining
    AuxPowMagic        []byte // "MNMP"
}

// Block represents a Metanet Chain block.
type Block struct {
    Header     BlockHeader
    AuxPoW     *AuxPoW     // nil for genesis block
    Txs        []*tx.Tx    // BSV-format transactions
}

// BlockHeader is the standard 80-byte block header (BSV-identical).
type BlockHeader struct {
    Version    uint32
    PrevHash   [32]byte
    MerkleRoot [32]byte
    Timestamp  uint32
    Bits       uint32
    Nonce      uint32
}

// AuxPoW contains the auxiliary proof-of-work data for merged mining.
type AuxPoW struct {
    ParentHeader    [80]byte   // BTC/BSV block header
    CoinbaseTx      []byte     // Coinbase transaction containing MNMP marker
    CoinbaseBranch  [][32]byte // Merkle branch proving coinbase is in parent block
}
```

### Functions

```go
// MainNetParams returns the Metanet Chain mainnet consensus parameters.
func MainNetParams() *Params

// TestNetParams returns the Metanet Chain testnet consensus parameters.
func TestNetParams() *Params

// GenesisBlock returns the Metanet Chain genesis block.
func GenesisBlock() *Block

// GenesisBlockHeader returns the genesis block header.
func GenesisBlockHeader() *BlockHeader

// GenesisBlockHash returns the double-SHA256 hash of the genesis block header.
func GenesisBlockHash() [32]byte

// BlockReward computes the block reward for a given block height,
// accounting for halving schedule.
func BlockReward(height uint32) uint64

// TotalSupplyAtHeight computes the cumulative MNT supply mined up to height.
func TotalSupplyAtHeight(height uint32) uint64

// HalvingEra returns the halving era (0-indexed) for a given block height.
func HalvingEra(height uint32) uint32

// HashBlockHeader computes the double-SHA256 hash of a block header.
func HashBlockHeader(h *BlockHeader) [32]byte

// SerializeBlockHeader serializes a block header to its 80-byte form.
func SerializeBlockHeader(h *BlockHeader) [80]byte

// DeserializeBlockHeader deserializes an 80-byte block header.
func DeserializeBlockHeader(data [80]byte) *BlockHeader

// ValidateBlockHeader checks that a block header meets consensus rules:
// - PrevHash references a known block
// - Timestamp is within acceptable range
// - Bits matches expected difficulty
// - PoW hash meets the target
func ValidateBlockHeader(h *BlockHeader, params *Params) error
```

## DEPENDENCIES

- `github.com/bsv-blockchain/go-sdk/transaction` -- BSV transaction types
- `crypto/sha256` -- SHA256 hashing
- `encoding/binary` -- Little-endian serialization
- `math/big` -- Difficulty target arithmetic

## DATA STRUCTURES

### Genesis Block

```
Timestamp:    TBD (mainnet launch)
Message:      "Metanet: Decentralized CDN on Bitcoin"
Reward:       50 MNT to foundation multisig (3-of-5)
PrevHash:     0x0000...0000 (all zeros)
Bits:         Initial difficulty target
Nonce:        TBD (mined at launch)
```

### Halving Schedule

| Era | Block Range | Reward/Block | Era Total |
|-----|-------------|--------------|-----------|
| 0 | 0 - 209,999 | 50.0 MNT | 10,500,000 MNT |
| 1 | 210,000 - 419,999 | 25.0 MNT | 5,250,000 MNT |
| 2 | 420,000 - 629,999 | 12.5 MNT | 2,625,000 MNT |
| 3 | 630,000 - 839,999 | 6.25 MNT | 1,312,500 MNT |
| ... | ... | ... | ... |
| 32+ | 6,720,000+ | 0 MNT | 0 MNT |

### Difficulty Encoding

The `Bits` field uses Bitcoin's compact target representation (same as BSV). Difficulty adjusts every 2016 blocks to maintain ~10 minute average block time.

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrInvalidPrevHash` | PrevHash does not reference a known block |
| `ErrTimestampTooOld` | Block timestamp before median of last 11 blocks |
| `ErrTimestampTooNew` | Block timestamp more than 2 hours in the future |
| `ErrInvalidDifficulty` | Bits does not match expected difficulty |
| `ErrInsufficientPoW` | Block hash does not meet difficulty target |
| `ErrBlockTooLarge` | Serialized block exceeds MaxBlockSize |
| `ErrInvalidMerkleRoot` | MerkleRoot does not match transaction Merkle tree |
| `ErrDuplicateTx` | Block contains duplicate transaction IDs |

## SECURITY CONSIDERATIONS

1. **Double-SHA256**: All block hashing uses SHA256d (SHA256(SHA256(x))) for consistency with BSV.
2. **Timestamp validation**: Blocks must have timestamps after median of last 11 blocks and no more than 2 hours in the future, preventing timestamp manipulation.
3. **Difficulty integrity**: Difficulty adjustment uses exact Bitcoin algorithm (every 2016 blocks, capped at 4x change per period).
4. **Genesis block immutability**: The genesis block is a compile-time constant; its hash anchors the entire chain.
5. **BSV anchoring**: Periodic Merkle root anchoring to BSV prevents long-range attacks on the Metanet Chain.

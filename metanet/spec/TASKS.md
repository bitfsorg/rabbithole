# Implementation Task Breakdown

> Tasks are ordered by dependency. Each phase builds on the previous.
> Acceptance criteria map to test design document (4-TestDesign.zh.md).

---

## Phase 1: Chain Core

Foundation layer. No external dependencies beyond go-sdk. Must be complete before any other phase.

### Task 1: internal/chain — Chain Parameters & Token Economics

**Package**: `internal/chain`
**Description**: Implement Metanet Chain consensus parameters, token economics (MNT), block header types, and genesis block definition. This is the bedrock of the entire system -- all other packages depend on these constants and types.

**Files**:
- `params.go` — Chain parameters struct and mainnet/testnet values
- `block.go` — Block and BlockHeader types, serialization, hashing
- `token.go` — Block reward calculation, halving schedule, supply computation
- `genesis.go` — Genesis block definition and hash
- `errors.go` — Package error types

**Acceptance Criteria**:
- [ ] MainNetParams() returns correct parameters (21M supply, 50 MNT reward, 210K halving, 32MB block, 10min target)
- [ ] TestNetParams() returns separate parameters suitable for testing
- [ ] BlockReward(height) returns correct reward for all halving eras (0 through 33+)
- [ ] BlockReward returns 0 for heights beyond the last halving era
- [ ] TotalSupplyAtHeight correctly sums all rewards up to height
- [ ] TotalSupplyAtHeight at final height equals 2,100,000,000,000,000 satoshis (21M MNT)
- [ ] GenesisBlock() has message "Metanet: Decentralized CDN on Bitcoin" in coinbase
- [ ] GenesisBlockHash() is deterministic and stable across builds
- [ ] SerializeBlockHeader/DeserializeBlockHeader roundtrip correctly
- [ ] HashBlockHeader produces double-SHA256 of the 80-byte serialized header

**Estimated Tests**: 18

---

### Task 2: internal/mining — Merged Mining & BSV Anchoring

**Package**: `internal/mining`
**Description**: Implement AuxPoW (auxiliary proof-of-work) validation for merged mining with BTC/BSV, coinbase commitment construction/parsing, difficulty adjustment algorithm, and BSV anchor transaction format.

**Files**:
- `auxpow.go` — AuxPoW header type, coinbase commitment construction
- `validate.go` — AuxPoW validation logic (parent header, coinbase branch, commitment)
- `difficulty.go` — Difficulty adjustment algorithm (every 2016 blocks, Bitcoin-identical)
- `anchor.go` — BSV anchor transaction construction and validation (MNTA format)
- `errors.go` — Package error types

**Acceptance Criteria**:
- [ ] BuildCoinbaseCommitment produces `OP_RETURN MNMP <block_hash>` format
- [ ] FindAuxPoWCommitment correctly extracts block hash from coinbase
- [ ] FindAuxPoWCommitment returns nil for coinbase without MNMP marker
- [ ] ValidateAuxPoW accepts valid merged mining proofs
- [ ] ValidateAuxPoW rejects proofs with wrong block hash
- [ ] ValidateAuxPoW rejects proofs with invalid coinbase branch
- [ ] ValidateAuxPoW rejects proofs that don't meet difficulty target
- [ ] VerifyCoinbaseBranch correctly validates Merkle inclusion
- [ ] CalcNextDifficulty matches Bitcoin algorithm (clamp to 4x, same formula)
- [ ] CalcNextDifficulty clamps timespan to [expected/4, expected*4]
- [ ] CompactToBig/BigToCompact roundtrip correctly
- [ ] HashMeetsTarget correctly compares hash against compact target
- [ ] SerializeAnchorData/DeserializeAnchorData roundtrip correctly
- [ ] BuildAnchorTx produces correct MNTA format
- [ ] ValidateAnchorChain detects broken chain linkage
- [ ] BuildBlockRangeMerkleRoot computes correct Merkle root

**Estimated Tests**: 22
**Depends on**: Task 1

---

## Phase 2: Economics

Storage contracts and proofs. Depends on Phase 1 for chain types.

### Task 3: internal/contract — Storage Contracts

**Package**: `internal/contract`
**Description**: Implement StorageDeal transaction construction using Bitcoin Script. Each deal creates N UTXOs (one per challenge period) with deterministic challenges derived from the contract TxID. The Metanet Node claims by submitting valid proofs; the Owner can refund after CLTV expiry.

**Files**:
- `deal.go` — StorageDeal type, parameter validation, UTXO generation
- `challenge.go` — Deterministic challenge computation from contract TxID
- `script.go` — Bitcoin Script construction (OP_IF/ELSE branches)
- `merkle.go` — Simple Merkle tree for pre-computing expected hashes
- `errors.go` — Package error types

**Acceptance Criteria**:
- [ ] ComputeChallenge is deterministic: same (txid, period, numChunks) always produces same result
- [ ] ComputeChallenge produces different results for different periods
- [ ] ComputeChallenge chunk_index is always within [0, numChunks)
- [ ] BuildDealScript produces correct OP_IF/ELSE/ENDIF structure
- [ ] BuildDealScript includes correct OP_CHECKSIGVERIFY and OP_SHA256
- [ ] BuildDealScript includes correct OP_CHECKLOCKTIMEVERIFY
- [ ] BuildClaimInput produces `<sig> <proof_data> OP_TRUE`
- [ ] BuildRefundInput produces `<sig> OP_FALSE`
- [ ] ComputeExpectedHash matches SHA256(proof || chunk_data)
- [ ] NewStorageDeal creates correct number of UTXOs
- [ ] ValidateDealParams rejects zero periods, zero payment, invalid keys
- [ ] StorageDeal → T1.1 (N UTXOs with pre-computed expected_proof_hash)
- [ ] Challenge determinism → T1.2 (reproducible, different per k)

**Estimated Tests**: 16
**Depends on**: Task 1

---

### Task 4: internal/proof — Storage Proofs & ECDH Encryption

**Package**: `internal/proof`
**Description**: Implement ECDH double-layer encryption (Method 42 first layer + node-specific second layer), Merkle tree construction over encrypted chunks, and challenge-response proof generation/verification.

**Files**:
- `encrypt.go` — ECDH double-layer encryption (DeriveNodeKey, EncryptForNode)
- `merkle.go` — SHA256 Merkle tree construction and proof generation
- `verify.go` — Storage proof verification (full pipeline)
- `serialize.go` — Proof data serialization/deserialization
- `errors.go` — Package error types

**Acceptance Criteria**:
- [ ] DeriveNodeKey produces different keys for different node public keys
- [ ] EncryptForNode produces different ciphertext for different nodes
- [ ] EncryptForNode ciphertext differs from input (double encryption applied)
- [ ] BuildMerkleTree produces correct root for known test vectors
- [ ] BuildMerkleTree handles odd number of leaves (duplicate last)
- [ ] GenerateMerkleProof generates valid proof for any leaf
- [ ] VerifyMerkleProof accepts valid proofs
- [ ] VerifyMerkleProof rejects proofs with wrong chunk data
- [ ] VerifyMerkleProof rejects proofs with wrong siblings
- [ ] ComputeProofHash matches expected_hash format
- [ ] VerifyStorageProof accepts valid complete proof
- [ ] VerifyStorageProof rejects wrong chunk index
- [ ] VerifyStorageProof rejects tampered chunk data
- [ ] SerializeProofData/DeserializeProofData roundtrip correctly
- [ ] ECDH uniqueness → T3.1 (provider ciphertext != owner ciphertext)
- [ ] Merkle proof → T2.1, T2.2, T2.3 (correct/wrong/mismatched proofs)

**Estimated Tests**: 20
**Depends on**: Task 1, Task 3 (for ComputeChallenge)

---

## Phase 3: Network

Payment channels and overlay network. Depends on Phase 1 for chain types and Phase 2 for contract/proof types.

### Task 5: internal/payment — Payment Channels

**Package**: `internal/payment`
**Description**: Implement dual payment channels (BSV and MNT). Includes 2-of-2 multisig funding, off-chain commitment updates with sequence numbers, revocation-based dispute resolution, and x402 HTTP header protocol extensions.

**Files**:
- `channel.go` — Channel types, state management, parameters
- `funding.go` — Funding transaction construction (2-of-2 multisig)
- `commitment.go` — Commitment transaction construction, state updates
- `dispute.go` — Revocation keys, punishment transactions
- `voucher.go` — x402 payment voucher encoding/decoding
- `errors.go` — Package error types

**Acceptance Criteria**:
- [ ] BuildFundingTx creates correct 2-of-2 multisig output
- [ ] OpenChannel initializes with full capacity on initiator side
- [ ] UpdateChannel transfers correct amount, increments sequence
- [ ] UpdateChannel returns revocation key for previous state
- [ ] CloseChannelCooperative produces valid settlement without dispute window
- [ ] CloseChannelUnilateral broadcasts latest commitment
- [ ] BuildPunishmentTx claims full balance using revocation key
- [ ] VerifyVoucher accepts valid signed commitment updates
- [ ] VerifyVoucher rejects expired or tampered vouchers
- [ ] EncodeVoucher/DecodeVoucher roundtrip correctly
- [ ] FormatChannelID/ParseChannelID roundtrip correctly
- [ ] Payment channel open → T4.2
- [ ] Payment channel update → T4.3
- [ ] Payment channel close → T4.4

**Estimated Tests**: 18
**Depends on**: Task 1

---

### Task 6: internal/overlay — BRC Overlay Network

**Package**: `internal/overlay`
**Description**: Implement BRC-compliant overlay network for node discovery, content routing, and service advertisement. Uses BRC-31, BRC-22, BRC-23, BRC-24, BRC-25, and BRC-87.

**Files**:
- `service.go` — OverlayService, initialization, lifecycle
- `discovery.go` — Peer discovery, content location
- `advertise.go` — Node advertisement, signature
- `topic.go` — Topic management (subscribe, unsubscribe)
- `store.go` — PeerStore and TopicStore interfaces + in-memory implementations
- `errors.go` — Package error types

**Acceptance Criteria**:
- [ ] NewOverlayService initializes correctly with local node
- [ ] Advertise produces signed advertisement
- [ ] VerifyAdvertisement accepts valid signatures
- [ ] VerifyAdvertisement rejects forged signatures
- [ ] Discover returns matching nodes
- [ ] LocateContent finds nodes with specific content
- [ ] RegisterTopic/UnregisterTopic manage subscriptions correctly
- [ ] PruneStalePeers removes old peers
- [ ] In-memory PeerStore add/remove/list operations

**Estimated Tests**: 14
**Depends on**: Task 1

---

## Phase 4: CLI

Command-line interface. Depends on all other phases.

### Task 7: cmd/metanet — Metanet Node CLI

**Package**: `cmd/metanet`
**Description**: Implement the `metanet` CLI binary with subcommands: init, start, stop, status, contracts, peers, mine. Uses cobra or standard flag package.

**Files**:
- `main.go` — Entry point, command registration
- `cmd_init.go` — `metanet init` implementation
- `cmd_start.go` — `metanet start` implementation
- `cmd_stop.go` — `metanet stop` implementation
- `cmd_status.go` — `metanet status` implementation
- `cmd_contracts.go` — `metanet contracts` implementation
- `cmd_peers.go` — `metanet peers` implementation
- `cmd_mine.go` — `metanet mine` implementation

**Acceptance Criteria**:
- [ ] `metanet init` creates data directory, generates keypair, writes config
- [ ] `metanet init` with `--testnet` uses testnet parameters
- [ ] `metanet init` fails gracefully if already initialized
- [ ] `metanet status --json` produces valid JSON output
- [ ] `metanet contracts --json` lists contracts in JSON format
- [ ] `metanet peers --json` lists peers in JSON format
- [ ] All commands use consistent exit codes
- [ ] `--help` works for all subcommands

**Estimated Tests**: 12
**Depends on**: Tasks 1-6, `internal/config`

---

### Task 8: internal/config — Configuration Management

**Package**: `internal/config`
**Description**: Configuration file parsing, validation, and defaults. Supports TOML format.

**Files**:
- `config.go` — Config struct, Load/Save, defaults
- `validate.go` — Parameter validation

**Acceptance Criteria**:
- [ ] Load parses valid TOML config
- [ ] Load returns defaults for missing fields
- [ ] Save writes valid TOML
- [ ] Validate rejects invalid port numbers, paths, etc.

**Estimated Tests**: 8
**Depends on**: Task 1

---

## Summary

| Phase | Task | Package | Est. Tests | Dependencies |
|-------|------|---------|-----------|-------------|
| 1 | 1 | internal/chain | 18 | none |
| 1 | 2 | internal/mining | 22 | Task 1 |
| 2 | 3 | internal/contract | 16 | Task 1 |
| 2 | 4 | internal/proof | 20 | Task 1, 3 |
| 3 | 5 | internal/payment | 18 | Task 1 |
| 3 | 6 | internal/overlay | 14 | Task 1 |
| 4 | 7 | cmd/metanet | 12 | Tasks 1-6, 8 |
| 4 | 8 | internal/config | 8 | Task 1 |
| | | **Total** | **128** | |

---

## Implementation Order

```
Week 1:  Task 1 (chain) → Task 2 (mining)
Week 2:  Task 3 (contract) → Task 4 (proof)
Week 3:  Task 5 (payment) → Task 6 (overlay)
Week 4:  Task 8 (config) → Task 7 (cmd/metanet)
```

All packages target >=80% line coverage. Tests use table-driven patterns with testify/require + testify/assert.

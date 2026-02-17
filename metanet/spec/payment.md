# Module Specification: internal/payment

## PURPOSE

The `payment` package implements dual payment channels for the Metanet CDN: BSV channels for end-user x402 micropayments and MNT channels for inter-node economics. Payment channels enable off-chain microtransactions with on-chain settlement, supporting high-frequency x402 content retrieval without per-request transaction fees.

The channel design uses standard Bitcoin Script (2-of-2 multisig funding, OP_CHECKSEQUENCEVERIFY for dispute windows, revocation keys for punishment). There are three channel types:
- **BSV channels** (User <-> Metanet Node): x402 streaming micropayments for content retrieval
- **MNT channels** (Owner <-> Metanet Node): CDN hosting fee payments
- **MNT channels** (Metanet Node <-> Metanet Node): Wholesale data trading between nodes

The x402 HTTP protocol extensions enable channel-based payments via custom HTTP headers.

## PUBLIC API

### Types

```go
// ChannelType identifies the currency and purpose of a payment channel.
type ChannelType int

const (
    ChannelBSV ChannelType = iota  // BSV: User <-> Node (x402)
    ChannelMNT                      // MNT: Owner <-> Node or Node <-> Node
)

// Channel represents an open payment channel between two parties.
type Channel struct {
    ID             [32]byte    // SHA256d(funding_txid || vout) -- unique channel ID
    Type           ChannelType
    FundingTxID    [32]byte    // On-chain funding transaction
    FundingVout    uint32      // Output index in funding tx
    Capacity       uint64      // Total channel capacity in satoshis
    InitiatorPubKey []byte     // 33-byte compressed public key
    ResponderPubKey []byte     // 33-byte compressed public key
    State          *ChannelState
}

// ChannelState represents the current balance state of a channel.
type ChannelState struct {
    SeqNum            uint64   // Monotonically increasing sequence number
    InitiatorBalance  uint64   // Initiator's current balance (satoshis)
    ResponderBalance  uint64   // Responder's current balance (satoshis)
    CommitmentTx      []byte   // Serialized commitment transaction
    InitiatorSig      []byte   // Initiator's signature on commitment tx
    ResponderSig      []byte   // Responder's signature on commitment tx
    RevocationKey     []byte   // Revocation key for this state (given to counterparty)
}

// ChannelParams defines the negotiated parameters for a channel.
type ChannelParams struct {
    MinDeposit      uint64        // Minimum funding amount (10,000 sat default)
    MaxDuration     uint32        // Maximum channel lifetime in blocks (144 default, ~1 day)
    DisputeWindow   uint32        // CSV dispute window in blocks (6 default)
}

// x402ChannelHeaders contains the HTTP header fields for x402 channel payments.
type x402ChannelHeaders struct {
    // Request headers
    AcceptChannel  bool     // X-Accept-Channel
    ChannelID      string   // X-Channel-ID: <funding_txid>:<vout>
    PaymentVoucher []byte   // X-Payment-Voucher: base64(signed_commitment)

    // Response headers
    ChannelPrice    uint64  // X-Channel-Price: sats_per_kb
    MinDeposit      uint64  // X-Channel-Min-Deposit: sats
    Balance         uint64  // X-Channel-Balance: remaining_sats
    Expiry          uint32  // X-Channel-Expiry: block_height
    TopUpRequired   bool    // X-Channel-TopUp-Required
    Expired         bool    // X-Channel-Expired
}
```

### Functions

```go
// BuildFundingTx constructs a 2-of-2 multisig funding transaction.
//
// Output script: OP_2 <pubkey_a> <pubkey_b> OP_2 OP_CHECKMULTISIG
func BuildFundingTx(
    initiatorPubKey, responderPubKey []byte,
    capacity uint64,
) (fundingScript []byte, err error)

// OpenChannel creates a new channel from a confirmed funding transaction.
func OpenChannel(
    channelType ChannelType,
    fundingTxID [32]byte,
    fundingVout uint32,
    capacity uint64,
    initiatorPubKey, responderPubKey []byte,
    params *ChannelParams,
) (*Channel, error)

// UpdateChannel creates a new commitment transaction reflecting a payment.
// The sequence number is incremented and new revocation keys are generated.
//
// Returns the new state and the revocation key for the OLD state
// (to be given to the counterparty).
func UpdateChannel(
    ch *Channel,
    amount uint64,          // Amount to transfer from initiator to responder
    initiatorPrivKey []byte,
) (*ChannelState, []byte, error)

// SignCommitment signs a commitment transaction as the responder.
func SignCommitment(
    ch *Channel,
    state *ChannelState,
    responderPrivKey []byte,
) ([]byte, error)

// BuildCommitmentTx constructs the commitment transaction for the current state.
//
// Output 0: Responder balance (with CSV time lock for dispute)
// Output 1: Initiator balance
func BuildCommitmentTx(
    fundingTxID [32]byte,
    fundingVout uint32,
    initiatorPubKey, responderPubKey []byte,
    initiatorBalance, responderBalance uint64,
    seqNum uint64,
    disputeWindow uint32,
) ([]byte, error)

// CloseChannelCooperative constructs a final settlement transaction
// signed by both parties. No dispute window needed.
func CloseChannelCooperative(
    ch *Channel,
    initiatorPrivKey, responderPrivKey []byte,
) ([]byte, error)

// CloseChannelUnilateral broadcasts the latest commitment transaction
// for unilateral close. Subject to dispute window.
func CloseChannelUnilateral(ch *Channel) ([]byte, error)

// BuildPunishmentTx constructs a punishment transaction using a
// revocation key to claim the full channel balance when the counterparty
// broadcasts an old state.
func BuildPunishmentTx(
    ch *Channel,
    revokedState *ChannelState,
    revocationKey []byte,
    claimerPubKey []byte,
) ([]byte, error)

// VerifyVoucher verifies an x402 payment voucher (signed commitment update).
func VerifyVoucher(
    ch *Channel,
    voucher []byte,
    expectedAmount uint64,
) error

// EncodeVoucher encodes a channel state update as a base64 payment voucher
// for use in the X-Payment-Voucher HTTP header.
func EncodeVoucher(state *ChannelState) (string, error)

// DecodeVoucher decodes a base64 payment voucher from an HTTP header.
func DecodeVoucher(encoded string) (*ChannelState, error)

// FormatChannelID formats a channel ID for use in HTTP headers.
// Format: "<funding_txid_hex>:<vout>"
func FormatChannelID(fundingTxID [32]byte, vout uint32) string

// ParseChannelID parses a channel ID from HTTP header format.
func ParseChannelID(id string) (fundingTxID [32]byte, vout uint32, err error)

// DefaultBSVParams returns default channel parameters for BSV channels.
func DefaultBSVParams() *ChannelParams

// DefaultMNTParams returns default channel parameters for MNT channels.
func DefaultMNTParams() *ChannelParams
```

## DEPENDENCIES

- `internal/chain` -- Chain parameters
- `github.com/bsv-blockchain/go-sdk/transaction` -- Transaction construction
- `github.com/bsv-blockchain/go-sdk/script` -- Script construction (multisig, CSV)
- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- Key operations, signing
- `crypto/sha256` -- Hashing
- `encoding/base64` -- Voucher encoding

## DATA STRUCTURES

### Funding Transaction Script

```
2-of-2 multisig:
  OP_2 <initiator_pubkey> <responder_pubkey> OP_2 OP_CHECKMULTISIG
```

### Commitment Transaction

```
Input:
    prev_tx: funding_txid
    prev_vout: funding_vout
    script_sig: OP_0 <sig_initiator> <sig_responder>

Output 0 (Responder):
    value: responder_balance
    script: <dispute_window> OP_CHECKSEQUENCEVERIFY OP_DROP
            <responder_pubkey> OP_CHECKSIG

Output 1 (Initiator):
    value: initiator_balance
    script: OP_DUP OP_HASH160 <initiator_pubkey_hash> OP_EQUALVERIFY OP_CHECKSIG
```

### Revocation Mechanism

Each state update produces a revocation key for the previous state. If a party broadcasts an old commitment:
1. The counterparty detects the old sequence number
2. Within the dispute window (CSV blocks), submits a punishment transaction using the revocation key
3. Punishment claims the entire channel balance

### x402 Channel HTTP Headers

```
Request:
    X-Accept-Channel: true
    X-Channel-ID: <funding_txid_hex>:<vout>
    X-Payment-Voucher: <base64(signed_commitment_update)>

Response:
    X-Accept-Channel: true
    X-Channel-Price: <sats_per_kb>
    X-Channel-Min-Deposit: <sats>
    X-Channel-Balance: <remaining_sats>
    X-Channel-Expiry: <block_height>

Error responses:
    402 + X-Channel-Balance: 0 + X-Channel-TopUp-Required: true
    402 + X-Channel-Expired: true
    400 Bad Request (invalid signature)
```

## ERROR HANDLING

| Error | Condition |
|-------|-----------|
| `ErrInsufficientCapacity` | Payment amount exceeds remaining balance |
| `ErrChannelClosed` | Attempt to update a closed channel |
| `ErrInvalidSequence` | Commitment sequence number is not greater than current |
| `ErrInvalidSignature` | Signature verification failed |
| `ErrInvalidVoucher` | Voucher decoding or verification failed |
| `ErrDisputeWindowActive` | Attempting action during active dispute period |
| `ErrInvalidRevocationKey` | Revocation key does not match expected value |
| `ErrBelowMinDeposit` | Funding amount is below MinDeposit |
| `ErrChannelExpired` | Channel has exceeded MaxDuration |
| `ErrInvalidChannelID` | Channel ID format is invalid |

## SECURITY CONSIDERATIONS

1. **2-of-2 multisig**: The funding output requires both signatures to spend, preventing either party from unilaterally stealing funds (except via the commitment mechanism).
2. **Revocation punishment**: Broadcasting an old state results in total loss of channel funds. This provides strong economic incentive to only broadcast the latest state.
3. **CSV dispute window**: The 6-block (~1 hour) dispute window gives the counterparty time to detect and punish old state broadcasts. This must be long enough for monitoring but short enough for practical use.
4. **Sequence number monotonicity**: Strictly increasing sequence numbers enable clear identification of stale commitments.
5. **Voucher replay protection**: Each voucher contains the sequence number and channel ID, preventing replay across channels or reuse of old vouchers.
6. **Channel capacity limits**: MaxDuration prevents indefinite locking of funds. MinDeposit prevents dust channel spam.

# 模块规格说明：internal/payment

## 目的

`payment` 包实现 Metanet CDN 的双通道支付：用于终端用户 x402 微支付的 BSV 通道和用于节点间经济的 MNT 通道。支付通道（Payment Channel）实现链下微交易与链上结算，支持高频率的 x402 内容检索而无需逐笔交易费用。

通道设计使用标准 Bitcoin Script（2-of-2 多重签名资金锁定、OP_CHECKSEQUENCEVERIFY 用于争议窗口、撤销密钥用于惩罚机制）。共有三种通道类型：
- **BSV 通道**（用户 <-> Metanet Node）：用于内容检索的 x402 流式微支付
- **MNT 通道**（所有者 <-> Metanet Node）：CDN 托管费用支付
- **MNT 通道**（Metanet Node <-> Metanet Node）：节点间批量数据交易

x402 HTTP 协议扩展通过自定义 HTTP 头实现基于通道的支付。

## 公开 API

### 类型

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

### 函数

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

## 依赖

- `internal/chain` -- 链参数
- `github.com/bsv-blockchain/go-sdk/transaction` -- 交易构造
- `github.com/bsv-blockchain/go-sdk/script` -- 脚本构造（多重签名、CSV）
- `github.com/bsv-blockchain/go-sdk/primitives/ec` -- 密钥操作、签名
- `crypto/sha256` -- 哈希
- `encoding/base64` -- 凭证编码

## 数据结构

### 资金交易脚本（Funding Transaction Script）

```
2-of-2 multisig:
  OP_2 <initiator_pubkey> <responder_pubkey> OP_2 OP_CHECKMULTISIG
```

### 承诺交易（Commitment Transaction）

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

### 撤销机制（Revocation Mechanism）

每次状态更新都会为前一个状态生成撤销密钥。如果一方广播旧的承诺交易：
1. 对手方检测到旧的序列号
2. 在争议窗口（CSV 区块数）内，使用撤销密钥提交惩罚交易
3. 惩罚交易领取通道内的全部余额

### x402 通道 HTTP 头

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

## 错误处理

| 错误 | 条件 |
|-------|-----------|
| `ErrInsufficientCapacity` | 支付金额超过剩余余额 |
| `ErrChannelClosed` | 试图更新已关闭的通道 |
| `ErrInvalidSequence` | 承诺序列号不大于当前值 |
| `ErrInvalidSignature` | 签名验证失败 |
| `ErrInvalidVoucher` | 凭证解码或验证失败 |
| `ErrDisputeWindowActive` | 在活跃争议期间尝试操作 |
| `ErrInvalidRevocationKey` | 撤销密钥与预期值不匹配 |
| `ErrBelowMinDeposit` | 资金金额低于 MinDeposit |
| `ErrChannelExpired` | 通道已超过 MaxDuration |
| `ErrInvalidChannelID` | 通道 ID 格式无效 |

## 安全考量

1. **2-of-2 多重签名**：资金输出需要双方签名才能花费，防止任何一方单方面窃取资金（通过承诺机制除外）。
2. **撤销惩罚**：广播旧状态将导致通道资金全部损失。这提供了强大的经济激励，促使各方只广播最新状态。
3. **CSV 争议窗口**：6 个区块（约 1 小时）的争议窗口给予对手方时间来检测和惩罚旧状态广播。这个时间必须足够长以便于监控，但又足够短以保证实用性。
4. **序列号单调性**：严格递增的序列号使得旧承诺可以被明确识别。
5. **凭证重放保护**：每个凭证包含序列号和通道 ID，防止跨通道重放或旧凭证重复使用。
6. **通道容量限制**：MaxDuration 防止资金被无限期锁定。MinDeposit 防止微尘通道的垃圾攻击。

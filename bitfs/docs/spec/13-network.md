# 模块规范：libbitfs-go/network

## 目的

BitFS 的区块链网络抽象层。定义统一的 `BlockchainService` 接口，使上层业务逻辑（engine、daemon）与底层区块链交互解耦。提供 JSON-RPC 客户端实现（RPCClient）和 SPV 验证桥接（SPVClient），支持 mainnet/testnet/regtest 多网络预设。

设计参考：SystemDesign 第 6 节（网络层）; DetailedDesign 第 7-B 节。

## 公共 API

### 接口

```go
// BlockchainService is the primary interface for blockchain interaction.
// Both BitFS and Metanet products import and use this interface.
type BlockchainService interface {
    // ListUnspent returns all unspent transaction outputs for the given address.
    ListUnspent(ctx context.Context, address string) ([]*UTXO, error)

    // GetUTXO returns a specific unspent transaction output by txid and output index.
    GetUTXO(ctx context.Context, txid string, vout uint32) (*UTXO, error)

    // BroadcastTx submits a raw transaction hex to the network and returns the txid.
    BroadcastTx(ctx context.Context, rawTxHex string) (string, error)

    // GetRawTx returns the raw transaction bytes for the given txid.
    GetRawTx(ctx context.Context, txid string) ([]byte, error)

    // GetTxStatus returns the confirmation status of a transaction.
    GetTxStatus(ctx context.Context, txid string) (*TxStatus, error)

    // GetBlockHeader returns the raw 80-byte block header for the given block hash.
    GetBlockHeader(ctx context.Context, blockHash string) ([]byte, error)

    // GetMerkleProof returns a Merkle inclusion proof for a confirmed transaction.
    GetMerkleProof(ctx context.Context, txid string) (*MerkleProof, error)

    // GetBestBlockHeight returns the height of the current chain tip.
    GetBestBlockHeight(ctx context.Context) (uint64, error)

    // ImportAddress imports a watch-only address into the node's wallet so that
    // ListUnspent can find its UTXOs. Rescans the chain to discover existing outputs.
    // No-op if the address is already imported. Safe to call multiple times.
    ImportAddress(ctx context.Context, address string) error
}
```

### 类型

```go
// UTXO represents an unspent transaction output.
type UTXO struct {
    TxID          string `json:"txid"`
    Vout          uint32 `json:"vout"`
    Amount        uint64 `json:"amount"`          // satoshis
    ScriptPubKey  string `json:"script_pubkey"`
    Address       string `json:"address"`
    Confirmations int64  `json:"confirmations"`
}

// TxStatus represents the confirmation status of a transaction.
type TxStatus struct {
    Confirmed   bool   `json:"confirmed"`
    BlockHash   string `json:"block_hash"`
    BlockHeight uint64 `json:"block_height"`
    TxIndex     int    `json:"tx_index"`
}

// MerkleProof represents a Merkle inclusion proof for SPV verification.
type MerkleProof struct {
    TxID      string   `json:"txid"`
    BlockHash string   `json:"block_hash"`
    Branches  [][]byte `json:"branches"`
    Index     int      `json:"index"`
}

// RPCConfig holds the connection parameters for a BSV node's JSON-RPC interface.
type RPCConfig struct {
    URL      string `json:"url"`
    User     string `json:"user"`
    Password string `json:"password"`
    Network  string `json:"network"`
}

// VerifyResult holds the result of an SPV verification.
type VerifyResult struct {
    Confirmed   bool
    BlockHash   string
    BlockHeight uint64
}
```

### RPCClient（JSON-RPC 实现）

```go
// RPCClient is a JSON-RPC 1.0 client for communicating with BSV nodes.
// Implements BlockchainService. All high-level methods built on top of Call.
type RPCClient struct { /* unexported fields */ }

// NewRPCClient creates a new JSON-RPC client with the given configuration.
// Uses HTTP Basic Auth when User is non-empty. Maintains a connection pool
// with 30s timeout, 10 max idle connections, 90s idle connection timeout.
func NewRPCClient(cfg RPCConfig) *RPCClient

// Call invokes a JSON-RPC method on the BSV node. Serializes request,
// sends with Basic Auth, deserializes response. Returns ErrConnectionFailed
// on HTTP errors, ErrInvalidResponse on decode errors.
func (c *RPCClient) Call(ctx context.Context, method string, params []interface{}, result interface{}) error
```

RPC 方法映射:

| BlockchainService 方法 | Bitcoin RPC 命令 | 说明 |
|---|---|---|
| ListUnspent | `listunspent 0 9999999 [addr]` | BTC 金额转换为 satoshis |
| GetUTXO | `gettxout txid vout` | JSON null = spent (ErrTxNotFound) |
| BroadcastTx | `sendrawtransaction hex` | 拒绝时返回 ErrBroadcastRejected |
| GetRawTx | `getrawtransaction txid false` | 返回 hex 解码后的原始字节 |
| GetTxStatus | `getrawtransaction txid true` | verbose 模式，提取确认信息 |
| GetBlockHeader | `getblockheader hash false` | 返回 hex 解码后的 80 字节头 |
| GetMerkleProof | `gettxoutproof [txid]` | 解析 BIP37 CMerkleBlock |
| GetBestBlockHeight | `getblockcount` | 返回当前链尖高度 |
| ImportAddress | `importaddress addr "" true` | 导入 watch-only 地址 |

### SPVClient（SPV 验证桥接）

```go
// SPVClient bridges the network layer with libbitfs-go/spv verification.
type SPVClient struct { /* unexported fields */ }

// NewSPVClient creates an SPV client backed by a blockchain service and header store.
func NewSPVClient(chain BlockchainService, headers spv.HeaderStore) *SPVClient

// VerifyTx performs SPV verification of a transaction:
//   1. Check confirmation status via GetTxStatus
//   2. Fetch/store block header if not cached
//   3. Fetch Merkle proof and verify against stored header
func (s *SPVClient) VerifyTx(ctx context.Context, txid string) (*VerifyResult, error)

// SyncHeaders fetches block headers from the network and stores them locally.
// Syncs from current local tip to the latest block. Validates chain continuity
// (each header's PrevBlock must match the previous header's hash).
func (s *SPVClient) SyncHeaders(ctx context.Context) error
```

### MockBlockchainService（测试用）

```go
// MockBlockchainService is a test double for BlockchainService.
// All function fields must be set before the corresponding method is called.
type MockBlockchainService struct {
    ListUnspentFn        func(ctx context.Context, address string) ([]*UTXO, error)
    GetUTXOFn            func(ctx context.Context, txid string, vout uint32) (*UTXO, error)
    BroadcastTxFn        func(ctx context.Context, rawTxHex string) (string, error)
    GetRawTxFn           func(ctx context.Context, txid string) ([]byte, error)
    GetTxStatusFn        func(ctx context.Context, txid string) (*TxStatus, error)
    GetBlockHeaderFn     func(ctx context.Context, blockHash string) ([]byte, error)
    GetMerkleProofFn     func(ctx context.Context, txid string) (*MerkleProof, error)
    GetBestBlockHeightFn func(ctx context.Context) (uint64, error)
    ImportAddressFn      func(ctx context.Context, address string) error
}
```

### 网络配置

```go
// NetworkPresets contains default RPC configurations for known networks.
// Mainnet is intentionally omitted to require explicit configuration.
var NetworkPresets = map[string]RPCConfig{
    "regtest":     {URL: "http://localhost:18332", User: "bitfs", Password: "bitfs"},
    "testnet":     {URL: "http://localhost:18333", User: "bitfs", Password: "bitfs"},
    "teratestnet": {URL: "http://localhost:18334", User: "bitfs", Password: "bitfs"},
}

// ResolveConfig merges RPC configuration from three sources (decreasing priority):
//   1. CLI flags (highest)
//   2. Environment variables (BITFS_RPC_URL, BITFS_RPC_USER, BITFS_RPC_PASS)
//   3. Network presets (lowest, regtest/testnet only)
// For mainnet, explicit configuration is required -- there is no preset.
func ResolveConfig(flags *RPCConfig, env map[string]string, network string) (*RPCConfig, error)
```

## 依赖

- `context` -- 请求超时和取消
- `net/http` -- HTTP 客户端（JSON-RPC 传输）
- `encoding/json` -- JSON-RPC 请求/响应序列化
- `encoding/hex` -- 十六进制编码/解码
- `libbitfs-go/spv` -- SPV 头存储和 Merkle 验证

## 数据结构

### JSON-RPC 请求格式

```json
{
  "jsonrpc": "1.0",
  "id": 1,
  "method": "listunspent",
  "params": [0, 9999999, ["address"]]
}
```

### 配置解析优先级

```
CLI flags (--rpc-url, --rpc-user, --rpc-pass)
  ↓ (覆盖)
Environment variables (BITFS_RPC_URL, BITFS_RPC_USER, BITFS_RPC_PASS)
  ↓ (覆盖)
Network presets (regtest: localhost:18332, testnet: localhost:18333)
```

Mainnet 无预设，必须显式配置。

### BIP37 CMerkleBlock 解析

`GetMerkleProof` 内部解析 `gettxoutproof` 返回的 BIP37 CMerkleBlock 二进制格式：

```
Header     [80]byte     // 区块头
TotalTxs   uint32       // 区块中交易总数
NumHashes  varint       // 部分 Merkle 树哈希数
Hashes     [32]byte × N // 哈希列表
NumFlags   varint       // 标志字节数
FlagBytes  []byte       // 位标志（树遍历方向）
```

解析后提取目标交易的 Merkle 分支路径，用于 SPV 验证。

### SPV 验证流程

```
VerifyTx(txid):
  1. GetTxStatus(txid) → status
  2. if !status.Confirmed → return {Confirmed: false}
  3. headers.GetHeader(blockHash) → header
     if not found: GetBlockHeader → DeserializeHeader → PutHeader
  4. GetMerkleProof(txid) → proof
  5. spv.VerifyMerkleProof(proof, header.MerkleRoot) → ok
  6. return {Confirmed: true, BlockHash, BlockHeight}
```

### 头同步流程

```
SyncHeaders():
  1. GetBestBlockHeight() → bestHeight
  2. headers.GetTip() → localTip
  3. for h = localTip+1 to bestHeight:
     a. getblockhash(h) → hash
     b. GetBlockHeader(hash) → rawHeader
     c. DeserializeHeader(rawHeader) → header
     d. Validate: header.PrevBlock == previous header's hash
     e. PutHeader(header)
```

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrConnectionFailed` | HTTP 请求失败或 HTTP 状态码非 2xx |
| `ErrAuthFailed` | RPC 认证被拒绝 |
| `ErrTxNotFound` | 请求的交易不存在或 UTXO 已花费 |
| `ErrBroadcastRejected` | 节点拒绝广播的交易 |
| `ErrInvalidResponse` | 节点返回格式错误或意外的响应 |

## 安全考量

1. **HTTP Basic Auth**：RPCClient 使用 HTTP Basic Auth 进行 RPC 认证。Mainnet/testnet 应使用 TLS 保护凭据传输。
2. **超时控制**：RPCClient 默认 30s HTTP 超时，10 个最大空闲连接，90s 空闲连接超时。所有方法接受 `context.Context` 支持请求级超时和取消。
3. **响应体限制**：错误响应读取限制为 1024 字节，防止恶意节点发送超大错误响应。
4. **Merkle 树安全**：解析 CMerkleBlock 时验证哈希计数不超过剩余数据量，防止 OOM 攻击。交易总数上限 1M（`maxMerkleTreeTxs = 1 << 20`）。
5. **链连续性验证**：SyncHeaders 验证每个区块头的 PrevBlock 等于前一个头的哈希，防止链分叉或跳跃。
6. **Mainnet 安全**：Mainnet 无默认预设，强制要求显式配置。避免意外连接到生产网络。

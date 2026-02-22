# SPV 按需验证深化 — 设计文档

日期：2026-02-22

## 背景

Engine 层已接入 BlockchainService（broadcast + UTXO refresh），但 SPV proof 存储和验证存在以下 gap：

1. `parseCMerkleBlock()` 是 stub，完整 BIP37 解析仅在 E2E 测试中
2. HeaderStore / TxStore 只有内存实现，无持久化
3. Engine 未调用 SPV 验证（仅用了 BroadcastTx 和 ListUnspent）
4. Daemon 无 SPV proof 端点供 Visitor 验证
5. 广播的 Metanet tx 未存储 proof

## 设计决策

### 按需验证 vs 完整 SPV

选择**按需验证**，理由：

| | 传统 SPV 钱包 | BitFS |
|---|---|---|
| 保护的是 | 资金安全 | 元数据真实性 |
| 被骗代价 | 丢钱 | 白付一笔小额 x402 费 |
| 激励对齐 | 攻击者有直接收益 | Owner 造假 = 失去客户 |
| 需要持续同步 | 是 | 不需要，Metanet tx 确认后不变 |

因此：
- 不做后台 header sync
- 不自动验证每笔 tx
- Visitor 付费前可选验证，CLI 用户可显式 `--verify`
- Header 按需获取并缓存，无 eviction（几十万 header 也才几十 MB）

## 分层设计

### 第一层：BIP37 CMerkleBlock 完整解析

**改动文件**：`libbitfs/network/rpc_blockchain.go`

**现状**：`parseCMerkleBlock()` 只做最小长度检查（≥84 bytes），返回空 branches、index=0 的 stub。完整解析逻辑在 `bitfs/e2e/04_spv_verify_test.go` 的 `parseBIP37MerkleBlock()` 中，已通过 regtest 验证。

**做法**：
- 将 E2E 的 `parseBIP37MerkleBlock()` 提取到 `libbitfs/network/rpc_blockchain.go`
- 解析 BIP37 partial Merkle tree：80-byte header → varint tx count → bit flags → hashes → 提取目标 tx 的 branch path + index
- 返回完整 `MerkleProof{TxID, BlockHash, Branches, Index}`
- E2E 测试改为调用生产代码，消除重复
- 补充单元测试（各种 tx count、edge case）

### 第二层：bbolt 持久化 HeaderStore + TxStore

**新文件**：`libbitfs/spv/boltstore.go`

**依赖**：`go.etcd.io/bbolt`（纯 Go，零 CGO）

**存储结构**：
```
文件：~/.bitfs/spv/spv.db

Buckets:
├── headers         # key: block_hash (32 bytes) → value: BlockHeader (gob)
├── headers_height  # key: height (4 bytes big-endian) → value: block_hash (32 bytes)
└── txs             # key: txid (32 bytes) → value: StoredTx (gob, 含 MerkleProof)
```

**接口**：实现已有的 `spv.HeaderStore` 和 `spv.TxStore` 接口，上层代码零改动。

**编码**：gob — 纯 Go 内部存储，不涉及跨语言，零依赖。

**按需缓存策略**：
- 验证 tx 时，如果 header 不在本地 → 从网络获取 → 存入 bbolt
- 下次同 block 的 tx 直接命中缓存
- 无 eviction（BlockHeader ~120 bytes/条，空间可忽略）

### 第三层：Engine 按需验证 + CLI --verify

**改动文件**：`bitfs/internal/engine/engine.go`、`cmd/bitfs/` 相关命令文件

**Engine 新增**：
```go
// VerifyTx 按需验证一笔 tx 的链上确认状态。
// Chain == nil 时返回 ErrOffline。
func (e *Engine) VerifyTx(ctx context.Context, txid string) (*network.VerifyResult, error)
```

Engine 初始化时，如果 Chain != nil，创建 SPVClient + BoltHeaderStore + BoltTxStore。

**CLI 改动**：
- `bitfs verify <txid>` — 新命令，显式验证单笔 tx
- `bitfs get --verify` — 获取文件前先验证对应 Metanet tx
- `bitfs cat --verify` — 同上

**调用时机**（非自动）：
- CLI 用户显式 `--verify` 或 `verify` 命令
- Daemon 端点被 Visitor 调用时
- 不改动现有 BroadcastTx / RefreshFeeUTXOs 流程

### 第四层：Daemon SPV proof 端点

**改动文件**：`bitfs/internal/daemon/routes.go`（或新增 `spv.go`）

**新增路由**：
```
GET /_bitfs/spv/proof/{txid}
```

**响应**（JSON）：
```json
{
  "txid": "abc123...",
  "confirmed": true,
  "block_hash": "000000...",
  "block_height": 812345,
  "merkle_proof": {
    "index": 3,
    "branches": ["aaa...", "bbb...", "ccc..."]
  }
}
```

**未确认 tx**：`{"txid": "...", "confirmed": false}`，不含 proof。

**流程**：Daemon 内部调用 `Engine.VerifyTx()` → 返回 proof 给 Visitor。Visitor 可以自己用 `libbitfs/spv` 本地验证，也可以直接信任 Daemon。

### 第五层：广播后存 tx，验证后回填 proof

**改动文件**：Engine 的广播流程

**流程**：
1. Engine 广播 Metanet tx 成功后 → 以 `Proof: nil`（unconfirmed）存入 BoltTxStore
2. 用户调用 `verify` 时 → 验证通过 → 把 MerkleProof 回填到 StoredTx
3. 后续查询同一 tx 时，如果已有 proof 直接返回（跳过网络请求）

**存储策略**：
- 统一存入 `~/.bitfs/spv/spv.db`，不按 vault 分库
- TxStore 已支持 `GetTxsByPubKey()`，可按 root P_node 检索 vault 下所有 tx
- 不做自动回填（按需原则）
- 不做导出/备份（后续需求）

## 文件变更汇总

| 文件 | 变更类型 | 内容 |
|------|----------|------|
| `libbitfs/network/rpc_blockchain.go` | 修改 | 完整 BIP37 CMerkleBlock 解析 |
| `libbitfs/network/rpc_blockchain_test.go` | 修改 | BIP37 解析单元测试 |
| `libbitfs/spv/boltstore.go` | 新增 | BoltHeaderStore + BoltTxStore |
| `libbitfs/spv/boltstore_test.go` | 新增 | bbolt 存储测试 |
| `libbitfs/go.mod` | 修改 | 添加 bbolt 依赖 |
| `bitfs/internal/engine/engine.go` | 修改 | VerifyTx 方法 + SPV 初始化 |
| `bitfs/internal/engine/verify_test.go` | 新增 | Engine 验证测试 |
| `bitfs/internal/daemon/spv.go` | 新增 | SPV proof 端点 |
| `bitfs/internal/daemon/spv_test.go` | 新增 | Daemon SPV 端点测试 |
| `bitfs/cmd/bitfs/` | 修改 | verify 命令 + get/cat --verify flag |
| `bitfs/e2e/04_spv_verify_test.go` | 修改 | 改用生产代码的 BIP37 解析 |

## 新增依赖

- `go.etcd.io/bbolt` — 纯 Go 嵌入式 KV 存储，零 CGO

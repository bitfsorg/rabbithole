# Agent Friendly 落地设计 — 2026-02-26

## 背景

BitFS 核心卖点之一是 "Agent Friendly"，但当前实现存在差距：

- b* 工具 `--json` 输出不完整（bcat、bget 缺失）
- x402 购买流程需要 8 步手动交互，Agent 不友好
- bcat 和 bget 的支付逻辑不一致
- 无批量购买支持

本设计聚焦可编码实施的改进，不涉及 libbitfs-ts 和 MCP（后续再做）。

## 范围

| # | 模块 | 改动范围 | 描述 |
|---|------|---------|------|
| 1 | bcat/bget --json | cmd/bcat, cmd/bget | 补全 JSON 输出 |
| 2 | 统一购买流程 | cmd/bcat, cmd/bget, internal/buyer | bcat 对齐 bget 的完整 funding tx 模式 |
| 3 | 买方钱包 + 自动 UTXO | 新增 internal/buyer/ | buyer.conf + BlockchainService UTXO 查询 + 自动选币 |
| 4 | 批量购买 | cmd/bget (mget --buy) | mget 支持 --buy，并行购买多文件 |

## 角色模型

- **Daemon** = 卖方（发布者），对外提供付费文件服务
- **b\* CLI** = 买方（访问者/Agent），购买和下载远程文件
- **bitfs shell** = 文件所有者，管理链上文件结构

x402 支付简化在买方侧（b* CLI），不改 daemon。

---

## 模块 1：bcat/bget JSON 输出补全

### 设计

为 bcat 和 bget 添加 `--json` flag，沿用现有 b* 工具模式。

### bcat --json

输出结构根据内容类型智能切换：

**text/* MIME 类型**（content 字段，原文）：
```json
{
  "pnode": "02a1b2c3...",
  "path": "/docs/readme.txt",
  "mime_type": "text/plain",
  "file_size": 1234,
  "access": "free",
  "content": "Hello world, this is the file content..."
}
```

**非 text/* MIME 类型**（content_base64 字段，base64 编码）：
```json
{
  "pnode": "02a1b2c3...",
  "path": "/photos/cat.jpg",
  "mime_type": "image/jpeg",
  "file_size": 56789,
  "access": "free",
  "content_base64": "iVBORw0KGgo..."
}
```

**付费文件未购买**（payment_required）：
```json
{
  "pnode": "02a1b2c3...",
  "path": "/premium/secret.txt",
  "mime_type": "text/plain",
  "file_size": 4567,
  "access": "paid",
  "payment_required": true,
  "price": 1000,
  "price_per_kb": 100,
  "invoice_id": "abc123",
  "seller_pubkey": "03f2e1d0...",
  "payment_addr": "1A2bC3d4..."
}
```

### bget --json

**成功下载**：
```json
{
  "pnode": "02a1b2c3...",
  "path": "/photos/cat.jpg",
  "mime_type": "image/jpeg",
  "file_size": 56789,
  "access": "free",
  "output_path": "/tmp/cat.jpg",
  "bytes_written": 56789
}
```

**付费购买成功**：
```json
{
  "pnode": "02a1b2c3...",
  "path": "/photos/cat.jpg",
  "file_size": 56789,
  "access": "paid",
  "output_path": "/tmp/cat.jpg",
  "bytes_written": 56789,
  "payment": {
    "cost_satoshis": 1000,
    "htlc_txid": "a1b2c3..."
  }
}
```

**付费未购买**：同 bcat。

### 统一错误输出（所有 b* 工具）

```json
{"error": "not found", "code": 2}
{"error": "connection refused", "code": 4}
{"error": "payment required", "code": 5}
```

Exit code 映射保持不变（2=not found, 4=network, 5=payment required）。

---

## 模块 2：统一购买流程

### 现状问题

| 方面 | bcat | bget |
|------|------|------|
| HTLC 提交 | 只发 locking script | 发完整 signed funding tx |
| UTXO 管理 | 不需要 | 手动 --utxo |
| 手续费计算 | 无 | 1 sat/byte |

bcat 的 "只发脚本" 模式不完整——没有链上资金锁定，卖方无法安全释放 capsule。

### 设计

1. **bcat 对齐 bget**：统一为完整 funding tx 模式
2. **提取共享购买逻辑**到 `internal/buyer/buy.go`
3. bcat 和 bget 的 handlePaid() 都调用 `buyer.Buy()`

### 共享接口

```go
// internal/buyer/buy.go

type BuyResult struct {
    Capsule     []byte // 解密用 capsule
    HTLCTxID    string // HTLC funding tx ID
    CostSatoshi uint64 // 实际花费（含手续费）
}

// Buy 执行完整购买流程：GetBuyInfo → 选 UTXO → BuildHTLC → SubmitHTLC
func Buy(c *client.Client, txID string, conf *BuyerConfig) (*BuyResult, error)
```

---

## 模块 3：买方钱包 + 自动 UTXO

### 买方配置

**配置文件** `~/.bitfs/buyer.conf`：

```ini
wallet_key = <hex private key (32 bytes)>
network = mainnet
```

**优先级**（高 → 低）：
1. CLI flag `--wallet-key` / `--utxo`（显式覆盖）
2. 环境变量 `BITFS_WALLET_KEY`
3. 配置文件 `~/.bitfs/buyer.conf`

### 自动 UTXO 查询

利用已有的 `libbitfs-go/network/BlockchainService` 接口：

```go
type BlockchainService interface {
    GetUTXOs(address string) ([]UTXO, error)
    BroadcastTx(rawTx []byte) (string, error)
    // ...
}
```

**选币算法**（贪心）：
1. 查询 buyer 地址的所有 UTXO
2. 按金额降序排列
3. 累加直到满足 `price + estimated_fee`
4. 手续费估算：`(N_inputs × 148 + 2 × 34 + 10) × fee_rate`
5. 如果余额不足，返回明确错误：`"insufficient balance: need %d sat, have %d sat"`

### 新增包结构

```
bitfs/internal/buyer/
├── config.go     // BuyerConfig, LoadConfig()
├── buy.go        // Buy(), 核心购买流程
├── utxo.go       // SelectUTXOs(), 自动选币
└── buyer_test.go // 单元测试
```

### CLI 使用方式

```bash
# 一次性配置
echo "wallet_key = <hex>" > ~/.bitfs/buyer.conf

# 之后 Agent 单步购买
bcat --buy bitfs://alice@example.com/docs/readme.txt
bget --buy bitfs://alice@example.com/photos/cat.jpg

# 仍支持手动模式（覆盖配置）
bget --buy --wallet-key <hex> --utxo <txid:vout:amount> bitfs://...
```

### 购买后 JSON 输出

bcat --buy --json：
```json
{
  "pnode": "02a1...",
  "path": "/docs/readme.txt",
  "mime_type": "text/plain",
  "access": "paid",
  "content": "Secret content here...",
  "payment": {
    "cost_satoshis": 1000,
    "htlc_txid": "a1b2c3..."
  }
}
```

---

## 模块 4：批量购买 (mget --buy)

### 设计

mget 已有基本批量下载能力，新增 `--buy` flag：

```bash
# 批量购买并下载
mget --buy bitfs://alice@example.com/photos/*.jpg /tmp/photos/

# 批量购买 + JSON 输出
mget --buy --json bitfs://alice@example.com/docs/*.txt /tmp/docs/
```

### 内部实现

1. 先 `bls` 展开 glob 获取文件列表
2. 过滤出 `access=paid` 的文件
3. 并发购买（默认 4 并发，`--concurrency` 可调）
4. 每个文件独立 UTXO 选择和 HTLC 提交
5. 汇总结果

### JSON 输出

```json
{
  "total": 5,
  "succeeded": 4,
  "failed": 1,
  "files": [
    {"path": "/photos/a.jpg", "output_path": "/tmp/photos/a.jpg", "bytes_written": 12345, "payment": {"cost_satoshis": 500, "htlc_txid": "..."}},
    {"path": "/photos/b.jpg", "error": "insufficient balance", "code": 6}
  ]
}
```

### 错误处理

- 单个文件失败不影响其他文件
- `--fail-fast` flag 可选：任一失败立即停止
- 返回最终汇总（成功数/失败数/总花费）

---

## 不在范围内

- libbitfs-ts TypeScript SDK（后续）
- MCP Server（后续）
- OpenAPI/JSON Schema 文档（后续，当前先实现功能）
- Daemon 侧改动（daemon 保持不变）

## 依赖关系

```
模块 1 (JSON 输出)  ← 独立，可先做
模块 2 (统一购买)   ← 独立，可先做
模块 3 (自动 UTXO)  ← 依赖模块 2（共享 Buy 函数）
模块 4 (批量购买)   ← 依赖模块 2 + 3
```

建议实施顺序：1 → 2 → 3 → 4

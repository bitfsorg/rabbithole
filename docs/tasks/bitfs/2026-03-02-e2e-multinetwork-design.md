# E2E 多网络测试设计

**日期**: 2026-03-02
**目标**: 将现有 25 个 regtest e2e 测试适配到 testnet 和 mainnet，验证真实网络广播

## 背景

现有 e2e 测试全部硬编码 regtest 模式：
- `RegtestNode` 直连 localhost:18332
- `FundAddress` 用 `generatetoaddress` 挖矿产币
- 确认通过 `MineBlocks` 立即完成

真实网络无法自主挖矿，需要外部 funding 和轮询等待确认。

## 设计方案：统一 Node 抽象

### 1. TestNode 接口

将 `RegtestNode` 泛化为 `TestNode` 接口，三网实现不同策略：

```go
type TestNode interface {
    Network() string                                                    // "regtest"|"testnet"|"mainnet"
    IsAvailable(ctx context.Context) bool
    Fund(ctx context.Context, addr string, amount float64) (*UTXO, error)
    WaitForConfirmation(ctx context.Context, txid string, minConf int) error
    SendRawTransaction(ctx context.Context, hex string) (string, error)
    GetRawTransaction(ctx context.Context, txid string) ([]byte, error)
    GetTxOutProof(ctx context.Context, txid string) ([]byte, error)
    GetBlockHeader(ctx context.Context, hash string) ([]byte, error)
    GetBlockHeaderVerbose(ctx context.Context, hash string) (map[string]interface{}, error)
    GetBestBlockHash(ctx context.Context) (string, error)
    GetBlockHash(ctx context.Context, height int) (string, error)
    GetBlockCount(ctx context.Context) (int64, error)
    ImportAddress(ctx context.Context, addr string) error
    ListUnspent(ctx context.Context, addr string) ([]UTXO, error)
    SendToAddress(ctx context.Context, addr string, amount float64) (string, error)
}
```

两个实现：
- **`regtestNode`** — 现有逻辑，`Fund()` = 挖 101 块 + send，`WaitForConfirmation()` = 挖 N 块
- **`liveNode`** — testnet/mainnet 共用，`Fund()` = faucet → fallback WIF 钱包，`WaitForConfirmation()` = 轮询

### 2. 配置（环境变量）

```bash
BITFS_E2E_NETWORK=regtest|testnet|mainnet     # 默认 regtest
BITFS_E2E_RPC_URL=http://localhost:18332       # RPC 端点
BITFS_E2E_RPC_USER=bitfs                       # RPC 用户名
BITFS_E2E_RPC_PASS=bitfs                       # RPC 密码
BITFS_E2E_FAUCET_URL=https://...               # Faucet API（testnet 可选）
BITFS_E2E_FUND_WIF=<私钥>                      # 预充值钱包私钥（mainnet 必须）
BITFS_E2E_CONFIRM_TIMEOUT=30m                  # 确认等待超时
```

默认值：

| 网络 | RPC URL | 确认超时 | Funding |
|------|---------|----------|---------|
| regtest | localhost:18332 | 30s | 挖矿 |
| testnet | localhost:18333 | 30m | faucet → WIF |
| mainnet | localhost:8332 | 60m | WIF only |

### 3. Funding 策略

```
regtest:  generatetoaddress 挖矿 → sendtoaddress → 挖确认块
testnet:  faucet API → fallback WIF 钱包转账
mainnet:  WIF 钱包转账（必须配置 BITFS_E2E_FUND_WIF）
```

**Faucet 适配器** (`testutil/faucet.go`)：
- 接口：`Faucet.Fund(ctx, addr, amount) error`
- 通过 `BITFS_E2E_FAUCET_URL` 配置
- 请求失败或未配置 → fallback 到 WIF 钱包

**WIF 钱包** (`testutil/funder.go`)：
- 从 `BITFS_E2E_FUND_WIF` 导入私钥
- 构建 P2PKH 交易发送指定金额到目标地址
- mainnet 未配置 WIF → `t.Skip`

### 4. 确认等待

```go
// regtestNode: 立即挖矿
func (n *regtestNode) WaitForConfirmation(ctx, txid, minConf) error {
    _, err := n.MineBlocks(ctx, minConf, miningAddr)
    return err
}

// liveNode: 轮询等待
func (n *liveNode) WaitForConfirmation(ctx, txid, minConf) error {
    ticker := time.NewTicker(15 * time.Second)
    for {
        select {
        case <-ctx.Done(): return ctx.Err()
        case <-ticker.C:
            conf := n.getConfirmations(txid)
            if conf >= minConf { return nil }
        }
    }
}
```

### 5. 测试文件改动模式

25 个测试文件统一适配：

```go
// 改前
node := testutil.NewRegtestNode()
testutil.SkipIfUnavailable(t, node)

// 改后
node := testutil.NewTestNode(t)  // 自动选择实现，不可用则 t.Skip
```

```go
// 改前
node.MineBlocks(ctx, 1, addr)

// 改后
node.WaitForConfirmation(ctx, txid, 1)
```

`engine_helpers.go` 中 `FundEngineWallet` 同样适配 TestNode 接口。

### 6. 文件结构

```
e2e/
├── testutil/
│   ├── rpc.go              ← 不变
│   ├── config.go           ← 新增：环境变量读取 + Config struct
│   ├── node.go             ← 重构：TestNode 接口 + regtestNode + NewTestNode()
│   ├── live_node.go        ← 新增：liveNode (testnet/mainnet)
│   ├── faucet.go           ← 新增：Faucet 适配器
│   ├── funder.go           ← 新增：WIF 钱包 funding
│   └── engine_helpers.go   ← 适配 TestNode 接口
├── docker-compose.yml          ← regtest（不变）
├── docker-compose.testnet.yml  ← testnet（已有）
├── bitcoin.conf                ← 不变
├── bitcoin-testnet.conf        ← 已有
└── *_test.go                   ← 25 个文件统一适配
```

删除：`docker-compose.stn.yml`、`bitcoin-stn.conf`

### 7. 运行方式

```bash
# regtest（默认，与现有行为一致）
go test -tags e2e ./e2e/... -v -timeout 300s

# testnet
BITFS_E2E_NETWORK=testnet go test -tags e2e ./e2e/... -v -timeout 60m

# mainnet
BITFS_E2E_NETWORK=mainnet BITFS_E2E_FUND_WIF=L... go test -tags e2e ./e2e/... -v -timeout 120m
```

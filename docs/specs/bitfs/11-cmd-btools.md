# 模块规范：cmd/b*（只读工具）

## 目的

六个独立的只读 CLI 工具，用于查询 BitFS 文件系统。这些是无状态的访问者工具，不需要钱包（除 --buy 购买流程外）。它们遵循 Unix 约定，可通过管道组合使用。

设计参考：ConceptDesign #1, #2, #3; SystemDesign 第 8 节。

## 工具

### cmd/bls -- 列出目录（类似 `ls`）

```
bls [OPTIONS] bitfs://<authority>/<path>

Options:
  -l, --long       详细列表
  --json           JSON 输出
  --keyword <kw>   按关键字过滤
  --no-cache       跳过缓存
  --timeout N      超时（秒）
  --offline        仅缓存模式

Output (default):
  readme.txt    4.2 KB    2026-02-14    [free]
  images/       dir       2026-02-13
  report.pdf    1.2 MB    2026-02-10    [paid: 50 sat/KB]

Output (--json):
  [{"name":"readme.txt","type":"file","size":4300,"access":"free",...}]
```

### cmd/bcat -- 输出文件内容（类似 `cat`）

```
bcat [OPTIONS] bitfs://<authority>/<path>

Options:
  --buy            自动购买（付费内容）
  --verify         SPV 验证 Metanet 交易后再输出
  --wallet-key HEX 买方私钥（32 字节 hex）
  --utxo SPEC      手动 UTXO（txid:vout:amount）
  --json           JSON 封装输出
  --no-cache       跳过缓存
  --timeout N      超时
  --offline        仅缓存模式

Behavior:
  - Free content: auto-decrypt using D_node=1 trick
  - Cached paid content: use cached key
  - Uncached paid content: show price, suggest --buy
```

### cmd/bget -- 下载文件（类似 `wget`）

```
bget [OPTIONS] bitfs://<authority>/<path>

Options:
  -o <file>        输出文件名
  --buy            自动购买
  --verify         SPV 验证 Metanet 交易后再下载
  --wallet-key HEX 买方私钥（32 字节 hex）
  --utxo SPEC      买方 UTXO（txid:vout:amount）
  --version N      下载指定版本
  --json           JSON 进度输出
  --no-cache       跳过缓存
  --timeout N      超时
  --offline        仅缓存模式

Behavior:
  - Downloads file to local filesystem
  - Free content: auto-decrypt
  - Paid content without --buy: show price and usage hint
  - Paid with --buy: HTLC purchase + download + cache key
```

### cmd/bstat -- 文件元数据（类似 `stat`）

```
bstat [OPTIONS] bitfs://<authority>/<path>

Options:
  --versions       显示所有版本
  --json           JSON 输出
  --no-cache       跳过缓存
  --timeout N      超时
  --offline        仅缓存模式

Output:
    Path: readme.txt
    Type: file
   Owner: 02a1b2c3...
  Access: free
    MIME: text/plain
    Size: 4.2 KB
    Hash: 3a7bd3e2...
 PriceKB: 50 sat
    TxID: abc123...
    Time: 2026-02-14 10:30:00 UTC
```

### cmd/bmget -- 批量下载（类似 `wget -r`）

```
bmget [OPTIONS] bitfs://<authority>/<path> [local-dir]

Options:
  --buy            自动购买付费内容
  --wallet-key HEX 买方私钥（32 字节 hex）
  --utxo SPEC      买方 UTXO（txid:vout:amount）
  --concurrency N  最大并发下载数（默认 4）
  --fail-fast      遇到第一个错误即停止
  --json           JSON 输出
  --no-cache       跳过缓存
  --timeout N      超时
  --offline        仅缓存模式

Behavior:
  - Downloads all files from a BitFS directory concurrently
  - local-dir defaults to basename of URI path
  - Creates local directory structure automatically
  - Skips subdirectories (only downloads direct file children)
  - Summary: succeeded/failed counts + per-file status
```

### cmd/btree -- 目录树（类似 `tree`）

```
btree [OPTIONS] bitfs://<authority>/<path>

Options:
  -d N             最大深度
  --json           JSON 输出
  --no-cache       跳过缓存
  --timeout N      超时
  --offline        仅缓存模式

Output:
  /
  ├── docs/
  │   ├── readme.txt (free, 4.2K)
  │   └── images/
  │       └── logo.png (free, 12K)
  ├── premium/
  │   └── data.csv (paid, 50 sat/KB)
  └── LICENSE (free, 1.1K)
```

## 共享实现

所有 b* 工具共享：
- 通过 `internal/client.ResolveURI()` 进行 URI 解析和端点发现
- 通过 HTTP 从守护进程获取元数据
- `~/.bitfs/cache/meta/` 中的本地缓存（TTL 5 分钟，`CachedClient` 封装）
- 公共标志：`--json`、`--no-cache`、`--timeout`、`--offline`、`--host`（可选覆盖）
- `--host` 为可选覆盖：未指定时从 URI 域名解析 daemon 端点（Paymail SRV / DNSLink SRV / domain:443 fallback）
- 裸公钥 URI（`bitfs://02abc...`）必须提供 `--host`
- 购买相关标志（bcat/bget/bmget）：`--buy`、`--wallet-key`、`--utxo`，通过 `internal/buyer` 包处理
- SPV 验证标志（bcat/bget）：`--verify`，购买前验证 Metanet 交易
- 退出码：与 cmd/bitfs 相同

## 依赖

- `flag` (stdlib) -- CLI 参数解析
- `internal/client` -- daemon HTTP 客户端、URI 解析、MetaCache
- `internal/buyer` -- 购买状态机、错误处理、配置加载
- `libbitfs-go/method42` -- 解密（用于免费内容 + 付费胶囊解密）
- `net/http` -- 守护进程 API 客户端

## 错误处理

与 cmd/bitfs 相同的退出码（0-7）。所有工具优雅处理：
- 网络超时并重试
- 缺失的缓存条目
- 无效 URI
- 需要支付的响应

## 安全考量

1. **只读**：这些工具永远不会写入区块链或修改本地钱包状态。
2. **密钥缓存**：已购买的密钥以加密形式缓存在本地。工具读取缓存的密钥，但只有带 --buy 标志的 b-tools 才会触发购买。
3. **无需钱包**：默认操作不需要钱包。只有 --buy 标志会触发钱包交互。

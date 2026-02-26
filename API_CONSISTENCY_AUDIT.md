# API 一致性审查报告

**日期**: 2026-02-26
**范围**: HTTP Daemon API、libbitfs-go 公共 API、CLI 命令接口、Spec 与实现对比

---

## 总览

| 维度 | 得分 | 关键问题数 |
|------|------|-----------|
| HTTP Daemon API | 88/100 | 3 |
| libbitfs-go 公共 API | 81/100 | 7 |
| CLI 命令接口 | 72/100 | 10 |
| Spec 与实现一致性 | 95/100 | 0 |

**总体评分: 84/100** — 架构层设计扎实，engine 层高度一致，但 CLI 表面层和跨包 API 约定需要标准化。

---

## 一、HTTP Daemon API (88/100)

### 1.1 端点清单 (14 个独立端点)

| # | 端点 | 方法 | 路由 | 用途 |
|---|------|------|------|------|
| 1 | Health | GET | `/_bitfs/health` | 健康检查 |
| 2 | Handshake | POST | `/_bitfs/handshake` | Method 42 ECDH 握手 |
| 3 | Data | GET | `/_bitfs/data/{hash}` | 加密内容下载 |
| 4 | Meta | GET | `/_bitfs/meta/{pnode}/{path...}` | Metanet 元数据查询 |
| 5 | Buy Info | GET | `/_bitfs/buy/{txid}` | 购买信息 (价格、capsule_hash) |
| 6 | Submit HTLC | POST | `/_bitfs/buy/{txid}` | 提交 HTLC 支付，获取 capsule |
| 7 | SPV Proof | GET | `/_bitfs/spv/proof/{txid}` | SPV Merkle 证明 |
| 8 | BSV Alias | GET | `/.well-known/bsvalias` | Paymail 能力发现 |
| 9 | PKI | GET | `/api/v1/pki/{handle}` | Paymail 公钥解析 |
| 10 | Root/Path | GET | `/{path...}` | 内容协商 (HTML/Markdown/JSON) |

另有 4 个 OPTIONS 端点用于 CORS preflight，共 14 个路由注册。

### 1.2 一致性优势

- **路由命名**: `/_bitfs/*` 系统端点、`/.well-known/*` 标准发现、`/api/v1/*` RESTful — 三层分明
- **错误码命名**: 统一 `SCREAMING_SNAKE_CASE` (如 `MISSING_HASH`, `INVALID_PUBKEY`)
- **JSON 字段命名**: 统一 `snake_case`
- **HTTP 方法使用**: GET 幂等读取、POST 状态变更，语义正确
- **路径参数提取**: 统一使用 Go 1.22 `r.PathValue()`
- **输入校验**: Client 端预校验 + Daemon 端再校验，双重防御

### 1.3 发现的问题

#### H-1: 402 错误响应格式不统一 (HIGH)

**标准错误格式**:
```json
{"error": {"code": "ERROR_CODE", "message": "...", "retry": false}}
```

**402 Payment Required 实际返回**:
```json
{"error": "payment required", "invoice_id": "..."}
```

此处 `"error"` 是字符串而非对象，与所有其他错误端点不一致。

**建议**: 将 402 响应改为标准格式，invoice 信息放入 error 对象的扩展字段:
```json
{"error": {"code": "PAYMENT_REQUIRED", "message": "...", "invoice_id": "...", "total_price": 1024}}
```

#### H-2: X-Session-Id 声明但未使用 (MEDIUM)

CORS 头声明 `Access-Control-Allow-Headers: Content-Type, Authorization, X-Session-Id`，但没有任何 handler 检查 `X-Session-Id`。Session 管理 (`CreateSession`, `GetSession`) 存在但未接入请求处理流程。

**建议**: 要么实现 session 验证中间件，要么从 CORS 头中移除 `X-Session-Id`。

#### H-3: Health 端点始终返回 200 (LOW)

`/_bitfs/health` 无论 daemon 内部状态如何都返回 `{"status":"ok"}`。在存储不可用或钱包未加载时仍报告健康。

**建议**: 增加组件健康检查 (storage、wallet、metanet service)，返回降级状态时使用 503。

---

## 二、libbitfs-go 公共 API (81/100)

### 2.1 10 个包 API 概览

| 包 | 导出函数 | 导出类型 | 接口 | 一致性 |
|---|---------|---------|------|--------|
| method42 | 8 | 2 result + 1 enum | 0 | 95% |
| wallet | 10 | 3 + 1 enum | 0 | 90% |
| tx | 12+ | 8 param/result | 0 | 85% |
| metanet | 20+ | 7 + 5 enum | 1 | 80% |
| spv | 15+ | 6 | 3 | 75% |
| storage | 10+ | 2 | 1 | 85% |
| network | 12+ | 6 | 1 | 90% |
| config | 6 | 1 | 0 | 95% |
| paymail | 10+ | 5 + 1 enum | 2 | 85% |
| x402 | 12+ | 8 | 0 | 90% |

### 2.2 一致性优势

- **构造函数**: 统一 `New*` 命名 (`NewWallet`, `NewInvoice`, `NewFileStore`, `NewContentResolver`)
- **结果类型**: 统一 `*Result` 命名 (`EncryptResult`, `DecryptResult`, `BatchResult`)
- **参数类型**: 统一 `*Params` 命名 (`CreateRootParams`, `HTLCParams`)
- **枚举**: 所有枚举实现 `String()` 方法
- **哨兵错误**: 统一使用 `errors.New()`，无自定义错误类型
- **错误包装**: 统一使用 `fmt.Errorf("%w", err)`

### 2.3 发现的问题

#### L-1: `context.Context` 仅 network 包使用 (CRITICAL)

**现状**: 只有 `network.BlockchainService` 的 9 个方法接受 `context.Context`，其余所有 I/O 操作均不支持 context:
- `storage.Fetch` — 无 context (HTTP 请求无法取消)
- `paymail.DiscoverCapabilities` — 无 context (DNS/HTTP 查询无法超时)
- `config.LoadConfig` — 无 context (文件 I/O 无法中断)
- `spv.VerifyTransaction` — 无 context (验证无法设超时)

**影响**: I/O 操作无法被调用方取消或设置超时，与 Go 标准实践严重不符。

**建议**: 为所有接口方法和公共 I/O 函数添加 `ctx context.Context` 作为第一个参数。这是一个 breaking change，需要分阶段实施。

#### L-2: 检索动词不统一 (HIGH)

| 包 | 动词 | 示例 |
|---|------|------|
| metanet | Get | `GetVault`, `GetNetwork` |
| metanet | Find | `FindChild` |
| spv | Get | `GetHeader`, `GetTx` |
| storage | Get (裸) | `Get(keyHash)` |
| config | Load | `LoadConfig` |
| paymail | Resolve | `ResolvePKIKey` |
| paymail | Discover | `DiscoverCapabilities` |
| network | Get | `GetUTXO`, `GetRawTx` |
| network | List | `ListUnspent` |

**建议**: 制定动词约定:
- `Get*`: 按 ID/键精确查找 (单个结果)
- `List*`: 返回集合
- `Resolve*`: 需要外部查询/解析的间接查找
- `Load*`: 从持久化存储加载 → 改为 `ReadConfig` 或保持 `Load` 但统一为一种
- `Find*`: 搜索匹配项 → 改为 `Get*` 或在找不到时返回 `error` 而非 `bool`

#### L-3: 跨包重复类型定义 (HIGH)

| 类型 | 定义位置 | 字段差异 |
|------|---------|---------|
| `UTXO` | `tx` 包、`network` 包 | 不同字段集 |
| `MerkleProof` | `spv` 包、`network` 包 | 不同字段集 |
| `MetanetTx.UTXO` vs `network.UTXO` vs `x402.HTLCUTXO` | 三处 | 各自独立 |

**建议**: 创建共享 `types` 包消除重复，或确保类型间有明确的语义区分并加注释说明。

#### L-4: `FindChild` 返回 bool 而非 error (MEDIUM)

```go
// 当前:
FindChild(dirNode *Node, name string) (*ChildEntry, bool)

// 应为:
FindChild(dirNode *Node, name string) (*ChildEntry, error)  // ErrChildNotFound
```

其他所有包的查找操作都返回 `(value, error)`。`FindChild` 使用 `bool` 打破了统一模式。

#### L-5: `ParseBIP37MerkleBlock` 返回 5 个裸值 (MEDIUM)

```go
// 当前:
func ParseBIP37MerkleBlock(data, targetTxID []byte) (header []byte, txIndex uint32, branches [][]byte, totalTxs uint32, err error)

// 应为:
type BIP37ParseResult struct {
    Header   []byte
    TxIndex  uint32
    Branches [][]byte
    TotalTxs uint32
}
func ParseBIP37MerkleBlock(data, targetTxID []byte) (*BIP37ParseResult, error)
```

5 个返回值难以阅读和维护，应包装为结构体。

#### L-6: BoltStore 返回具体类型而非接口 (MEDIUM)

```go
// 当前:
func (bs *BoltStore) Headers() *BoltHeaderStore   // 返回具体类型
func (bs *BoltStore) Txs() *BoltTxStore            // 返回具体类型

// 应为:
func (bs *BoltStore) Headers() HeaderStore          // 返回接口
func (bs *BoltStore) Txs() TxStore                  // 返回接口
```

返回具体类型降低了可测试性，迫使消费方依赖实现细节。

#### L-7: nil 参数错误不统一 (LOW)

- `spv`, `tx`, `metanet`: 使用通用 `ErrNilParam`
- `method42`: 使用特定 `ErrNilPrivateKey`, `ErrNilPublicKey`

**建议**: 统一策略 — 要么全部使用通用 `ErrNilParam`（简洁），要么全部使用特定错误（可调试性更好）。推荐后者。

---

## 三、CLI 命令接口 (72/100)

### 3.1 命令清单

**bitfs (所有者命令)**: ~27 个命令
- wallet (init/show/balance/fund), vault (create/list/rename/delete)
- 文件操作: cat, get, mget, mput, put, mkdir, rm, mv, cp, link
- 交易: sell, encrypt, publish, unpublish
- 系统: daemon (start/stop), shell, verify

**b* tools (访问者只读)**: 6 个工具
- bls, bcat, bget, bstat, btree, bmget

### 3.2 一致性优势

- **Engine 方法签名**: 统一 `func (e *Engine) Method(opts *MethodOpts) (*Result, error)` — 设计优秀
- **通用标志**: `--vault`, `--datadir`, `--password` 在 bitfs 命令间一致
- **b* tools 共享标志**: `--host`, `--json`, `--timeout` 一致

### 3.3 发现的问题

#### C-1: 退出码语义不一致 (HIGH)

| 语义 | bitfs 退出码 | b* tools 退出码 |
|------|------------|----------------|
| 成功 | 0 | 0 |
| 通用错误 | 1 | 1 |
| 使用错误 | 2 | 6 |
| 未找到 | 6 | 2 |
| 网络错误 | 4 | 4 |
| 支付/权限 | 5 | 5 |

`未找到` 和 `使用错误` 的退出码完全互换了！这对脚本自动化是严重障碍。

**建议**: 统一退出码表，两套工具使用相同语义。

#### C-2: bitfs 命令无 JSON 输出 (HIGH)

b* tools 全部支持 `--json` 标志输出结构化 JSON，但 bitfs 所有者命令 (put, mkdir, rm, mv, cp 等) 只输出纯文本。

**影响**: 无法在脚本中解析 bitfs 命令的输出 (如获取 TxID)。

**建议**: 为所有 bitfs 命令添加 `--json` 标志，输出统一的 JSON 结构。

#### C-3: 错误消息格式不一致 (MEDIUM)

```
# bitfs 命令:
Error: vault not found

# b* tools:
bget: not found
```

bitfs 使用 `"Error: %v"` 前缀，b* tools 使用 `"<工具名>: %v"` 前缀。

**建议**: 统一为 `"<命令名>: <消息>"` 格式 (Unix 惯例)。

#### C-4: 位置参数顺序不一致 (MEDIUM)

```
bitfs put <本地文件> <远程路径>    # 源 → 目标
bitfs get <远程路径> [本地路径]     # 源 → 目标 (OK，但与 put 的"本地在前"不一致)
```

`put` 是 `<local> <remote>`，`get` 是 `<remote> [local]`。虽然语义上都是 `<源> <目标>`，但本地/远程位置的优先顺序不统一。

**建议**: 可以保持现状 (cp/scp 的惯例也类似)，但应在帮助文本中明确说明。

#### C-5: Shell REPL 的 put 硬编码 access="free" (MEDIUM)

Shell REPL 的 `put` 命令将 access 硬编码为 `"free"`，用户无法在 shell 中创建 paid 或 private 文件。`mput` 支持第三个位置参数设置 access，但 `put` 不支持。

**建议**: Shell 的 `put` 应支持 `put <file> <path> [free|paid|private]` 语法。

#### C-6: 命令级帮助文本不统一 (MEDIUM)

- bitfs 命令: 无详细的 per-command `--help`，仅在参数不足时打印 Usage 行
- b* tools: 嵌入示例的详细帮助文本

**建议**: 为所有命令添加统一的 `--help` 输出，包含用法、选项列表、示例。

#### C-7: 标志定义顺序无规范 (LOW)

各命令的 `flag.NewFlagSet` 中标志定义顺序随机。

**建议**: 制定标志排序规范:
1. 业务逻辑标志 (--vault, --access, --buy)
2. 输出控制标志 (--json, --long, --verify)
3. 基础设施标志 (--host, --timeout, --datadir, --password)

#### C-8: `--timeout` 仅 b* tools 支持 (LOW)

bitfs 所有者命令无超时控制，在网络不佳时可能无限阻塞。

**建议**: 为涉及网络 I/O 的 bitfs 命令添加 `--timeout` 标志。

#### C-9: SPV 验证标志覆盖范围有限 (LOW)

只有 `cat`/`bcat` 支持 `--verify` 标志。`get`/`bget` 下载文件后也应可选验证。

**建议**: 为所有内容获取命令添加 `--verify` 标志。

#### C-10: b* tools 无 `--no-cache` 和 `--offline` 标志 (LOW)

Spec 文档 (`spec/11-cmd-btools.md`) 指定了 `--no-cache` 和 `--offline` 共享选项，但实际实现中未找到。

**建议**: 按 spec 实现这两个标志，或更新 spec 移除。

---

## 四、Spec 与实现一致性 (95/100)

### 4.1 对比矩阵

| 组件 | Spec 文档 | 实现文件 | 状态 |
|------|----------|---------|------|
| LFCP 端点 | spec/09-daemon.md | internal/daemon/routes.go | 完整 |
| x402 协议 | spec/08-x402.md | internal/daemon/payment.go | 完整 |
| HTTP 头 | spec/08-x402.md | internal/daemon/payment.go | 完整 |
| Method 42 握手 | spec/01-method42.md | internal/daemon/handshake.go | 完整 |
| 内容协商 | design 2-SystemDesign | internal/daemon/routes.go | 完整 |
| Paymail/DNSLink | spec/07-paymail.md | internal/daemon/paymail.go | 完整 |
| b-tools CLI | spec/11-cmd-btools.md | cmd/b* | 完整 |
| 交易构建 | spec/02-tx.md | libbitfs-go/tx/ | 完整 |
| 加密/解密 | spec/01-method42.md | libbitfs-go/method42/ | 完整 |
| 错误处理 | design 2-SystemDesign | internal/daemon/errors.go | 完整 |

### 4.2 Spec 中指定但未实现的项

| 项目 | Spec 来源 | 状态 |
|------|----------|------|
| `--no-cache` 标志 | spec/11-cmd-btools.md | 未实现 |
| `--offline` 标志 | spec/11-cmd-btools.md | 未实现 |
| Session 验证中间件 | design 2-SystemDesign | 结构存在，未接入路由 |

---

## 五、优先级排序

### P0 — 必须修复 (影响正确性/可用性)

| ID | 问题 | 影响 |
|----|------|------|
| C-1 | 退出码语义互换 | 脚本自动化得到错误语义 |
| L-1 | context.Context 仅 network 包使用 | I/O 操作无法取消/超时 |

### P1 — 应该修复 (影响一致性/开发体验)

| ID | 问题 | 影响 |
|----|------|------|
| C-2 | bitfs 命令无 JSON 输出 | 无法脚本化所有者操作 |
| H-1 | 402 错误格式不统一 | 客户端需特殊处理 402 |
| L-2 | 检索动词不统一 | API 学习曲线增加 |
| L-3 | 跨包重复类型定义 | 维护负担、类型混淆 |
| C-3 | 错误消息格式不一致 | 用户/脚本体验不一致 |

### P2 — 可以修复 (影响代码质量)

| ID | 问题 | 影响 |
|----|------|------|
| C-5 | Shell put 硬编码 access | Shell 功能受限 |
| L-4 | FindChild 返回 bool | 不符合 Go 惯例 |
| L-5 | ParseBIP37MerkleBlock 返回 5 值 | 可读性差 |
| L-6 | BoltStore 返回具体类型 | 可测试性降低 |
| H-2 | X-Session-Id 未使用 | CORS 声明与实际不符 |
| C-6 | 命令级帮助不统一 | 用户体验不一致 |

### P3 — 建议改进 (低优先级)

| ID | 问题 | 影响 |
|----|------|------|
| L-7 | nil 参数错误不统一 | 调试体验不一致 |
| C-7 | 标志定义顺序无规范 | 代码可维护性 |
| C-8 | bitfs 命令无 --timeout | 网络不佳时阻塞 |
| C-9 | --verify 覆盖范围有限 | 功能完整性 |
| C-10 | --no-cache/--offline 未实现 | Spec 与实现偏差 |
| H-3 | Health 端点始终 200 | 运维可观测性 |

---

## 六、各维度评分细节

### HTTP Daemon API 评分

| 子维度 | 分数 | 说明 |
|--------|------|------|
| 路由命名 | 95 | 三层命名空间清晰 |
| 错误格式 | 80 | 402 格式不统一 |
| HTTP 方法 | 95 | GET/POST 语义正确 |
| 响应结构 | 85 | 内容协商 vs 标准 JSON |
| 安全 | 85 | 双重验证好，但 session 未接入 |
| **平均** | **88** | |

### libbitfs-go 公共 API 评分

| 子维度 | 分数 | 说明 |
|--------|------|------|
| 构造函数 | 95 | `New*` 统一 |
| 枚举实现 | 95 | 全部有 `String()` |
| 错误处理 | 80 | 哨兵错误好，但 wrapping 风格不一 |
| 命名一致性 | 75 | 检索动词混乱 |
| 参数约定 | 70 | context.Context 严重不一致 |
| 返回值模式 | 80 | 部分裸多返回值 |
| 接口设计 | 75 | BlockchainService 动词混乱 |
| **平均** | **81** | |

### CLI 命令接口评分

| 子维度 | 分数 | 说明 |
|--------|------|------|
| Engine 层 | 95 | 统一 Opts/Result 模式 |
| 退出码 | 50 | 两套工具语义互换 |
| 标志命名 | 75 | 同类标志基本一致 |
| 输出格式 | 60 | bitfs 无 JSON |
| 错误消息 | 70 | 两种前缀格式 |
| 帮助文本 | 65 | 详细度不一 |
| **平均** | **72** | |

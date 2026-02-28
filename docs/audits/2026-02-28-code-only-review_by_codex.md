# 2026-02-28 代码级复审（仅代码，不看设计文档）

## 审查范围
- 仓库路径：`/Users/alex/Codes/RabbitHole`
- 仅基于源码与测试/构建结果进行审查
- 明确排除：`docs/`、`design/`、`whitepaper/` 等设计与说明文档内容

## 执行方式
- 枚举 Go 模块并执行：
  - `go test ./...`（`bitfs`）
  - `go test ./...`（`metanet`）
  - `go test ./...`（`den`）
- 使用静态检索定位高风险模式（忽略错误、边界处理、并发锁范围）
- 回到源码逐行确认可触发性与影响

## 发现的问题（按严重级别）

### 1. 高严重：`den/cmd/seed` 无法构建（发布阻断）
- 文件：`den/cmd/seed/main.go`
- 行号：110, 152, 196, 242
- 现象：调用了不存在的 `tx` API：
  - `tx.BuildUnsignedCreateRootTx`
  - `tx.CreateRootParams`
  - `tx.BuildUnsignedCreateChildTx`
  - `tx.CreateChildParams`
- 影响：`go test ./...` 在 `github.com/tongxiaofeng/den/cmd/seed` 直接构建失败，seed 命令不可用。

### 2. 中严重：SPV 验证路径存在 panic 风险（TxID 长度未校验）
- 文件：`den/verify.go`
- 行号：61-64
- 现象：固定分配 `txidInternal := make([]byte, 32)`，随后按 `txidBytes` 全长写入 `txidInternal[31-i]`，未验证 `txidBytes` 是否恰为 32 字节。
- 影响：异常/恶意输入下可能触发越界 panic，导致进程崩溃（DoS）。

### 3. 中严重：支付接口在持锁期间读取请求体，可能放大锁竞争
- 文件：`bitfs/internal/daemon/payment.go`
- 行号：394, 417
- 现象：先 `d.invoicesMu.Lock()`，再 `io.ReadAll(io.LimitReader(r.Body, ...))`。
- 影响：慢请求或大请求在读取阶段长期占锁，阻塞其他发票相关并发路径，降低吞吐并增加 DoS 面。

### 4. 低严重：搜索高度解析可能误判
- 文件：`den/rpc.go`
- 行号：162
- 现象：`fmt.Sscanf(q, "%d", &height)` 对 `"123abc"` 等输入会解析出前缀数字并通过高度分支。
- 影响：搜索行为与用户输入语义不一致，可能错误跳转。

### 5. 低严重：关键 RPC 错误被吞，降低可观测性
- 文件：`den/handlers.go`
- 行号：106, 198
- 现象：
  - `rawBytes, _ := s.explorer.rpc.GetRawTx(...)`
  - `headerBytes, _ := s.explorer.rpc.GetBlockHeader(...)`
- 影响：页面失败时难区分“真实业务失败”和“RPC 后端故障”，不利于排障。

## 复现记录
- `bitfs`: `go test ./...` 通过
- `metanet`: `go test ./...` 通过
- `den`: 主包通过，但 `den/cmd/seed` 构建失败（见问题 1）

## 建议修复顺序
1. 先修复 `den/cmd/seed` 的 API 调用漂移（阻断项）。
2. 为 `den/verify.go` 增加严格 `txid` 长度校验（必须为 32 字节）。
3. 将 `handlePayInvoice` 的请求体读取移到加锁之前，再做状态校验与落库。
4. 将 `SearchQuery` 的高度解析改为“整串数字匹配”而非 `Sscanf` 前缀匹配。
5. 补齐 `handlers` 中被忽略错误的处理与日志。

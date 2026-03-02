# BitFS 发布前审查报告_by_codex

- 日期: 2026-02-28
- 审查范围: `bitfs`、`libbitfs-go`、`metanet` 的设计文档与实现一致性、支付安全路径、测试与并发基线
- 审查方式: 代码审阅 + 规范对照 + 测试/静态检查
- 状态: All P0/P1 findings confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md`. Archived 2026-03-03.

## 执行摘要

当前版本在测试层面整体健康（单测、`go vet`、关键模块 `-race` 均通过），但支付链路存在发布阻断级问题，尤其是 `/_bitfs/pay/{invoice_id}` 可被空请求直接标记支付成功，以及支付状态机在异常分支下的一致性问题。

结论：**不建议直接发布**。需先完成本文 P0/P1 项修复。

## 发现清单（按严重度）

### P0-1 `/_bitfs/pay/{invoice_id}` 存在零成本支付漏洞

问题：当请求 body 为空时，接口会直接将发票标记为 `Paid=true` 并返回成功。

影响：攻击者可在无链上支付、无交易证明情况下伪造“已支付”状态。

证据：
- 路由开放支付入口: `bitfs/internal/daemon/routes.go:39`
- 实现中读取 body 后仅在 `len(body) > 0` 时才做交易校验，随后统一 `invoice.Paid = true`: `bitfs/internal/daemon/payment.go:417`, `bitfs/internal/daemon/payment.go:467`
- 测试明确验证空 body 成功: `bitfs/internal/daemon/daemon_test.go:2016`

修复建议：
- 生产模式下强制要求支付证明（禁用空 body 成功路径）。
- 如需保留开发捷径，必须受显式 `dev/test` 配置开关保护，默认关闭。

### P0-2 `/_bitfs/pay` 的“带交易验证”路径会被过期校验误杀

问题：`handlePayInvoice` 构造 `x402.Invoice` 时未填充 `Expiry`，`x402.VerifyPayment` 会先检查过期，导致非空支付证明路径几乎必失败。

证据：
- 未传 `Expiry`: `bitfs/internal/daemon/payment.go:440`
- `VerifyPayment` 首先检查 `invoice.Expiry`: `libbitfs-go/x402/verify.go:24`

影响：实际支付验证流程不可用，迫使系统依赖不安全的空 body 分支。

修复建议：
- 构造 `x402.Invoice` 时传入 `Expiry: invoice.Expiry.Unix()`。
- 增加“有效 raw_tx -> 200”正向测试与回归测试。

### P1-1 `/_bitfs/buy/{txid}` 在 `chain == nil` 时可不广播直接放行

问题：HTLC 提交后，仅在 `d.chain != nil` 时才广播验证上链；若未配置链服务，仍会继续流程并可能返回 capsule。

证据：
- 条件广播逻辑: `bitfs/internal/daemon/payment.go:345`
- `chain` 字段为可选: `bitfs/internal/daemon/daemon.go:221`
- 设计要求“揭示 capsule 前验证上链”: `bitfs/docs/spec/08-x402.md:132`

影响：违背支付原子性与协议安全边界，增加欺诈面。

修复建议：
- 对买入主路径强制依赖链广播成功（`chain == nil` 直接拒绝）。
- 将“离线/测试模式”显式隔离，不与生产接口共享默认行为。

### P1-2 `/_bitfs/buy` 的 `NO_CAPSULE` 分支存在状态不一致

问题：流程先乐观标记 `invoice.Paid = true`，若后续发现 `Capsule` 为空则返回 500，但该分支未回滚支付状态与交易占用记录。

证据：
- 先置 `Paid=true`: `bitfs/internal/daemon/payment.go:286`
- `NO_CAPSULE` 直接返回: `bitfs/internal/daemon/payment.go:362`
- 存在对应失败场景测试: `bitfs/internal/daemon/payment_test.go:423`

影响：支付状态、资金状态、对账状态可能分裂，造成人工补偿与审计成本上升。

修复建议：
- 在 `NO_CAPSULE` 及所有失败出口执行统一 rollback（`Paid`、`usedTxIDs`）。
- 优化状态机：将 `Paid=true` 放到所有关键校验（含 capsule 存在）之后。

### P2-1 `/_bitfs/pay` 在写锁内做 I/O 与验签，存在放大阻塞风险

问题：`handlePayInvoice` 在持有 `invoicesMu.Lock()` 期间读取 body、JSON 解析、交易验证。

证据：
- 加锁后读取 body: `bitfs/internal/daemon/payment.go:394`, `bitfs/internal/daemon/payment.go:417`

影响：在高并发或慢请求下，可能导致发票域锁竞争恶化，影响支付与查询延迟。

修复建议：
- 参照 `handleSubmitHTLC` 的模式，将 I/O 与重计算移到锁外。
- 缩小临界区为状态检查与原子状态迁移。

### P2-2 x402 文档与实现存在一致性偏差

问题：文档声明与实现不一致。

证据：
- 文档写默认 HTLC 超时为 144 块: `bitfs/docs/spec/08-x402.md:48`, `bitfs/docs/spec/08-x402.md:131`
- 实现默认为 72 块: `libbitfs-go/x402/htlc.go:29`
- 文档写 `VerifyPayment` 包含 mempool/Merkle 验证: `bitfs/docs/spec/08-x402.md:71`
- 当前实现未使用 `MerkleProof`: `libbitfs-go/x402/verify.go:15`

影响：对接方、审计方、运维方对协议行为预期不一致。

修复建议：
- 统一代码与规格（择一）：要么按文档补实现，要么修文档并标注版本变更。

## 验证基线

已执行（本地）：
- `cd bitfs && go test ./...`
- `cd libbitfs-go && go test ./...`
- `cd metanet && go test ./...`
- `go vet ./...`（三模块）
- `go test -race`（关键包）：
  - `bitfs/internal/daemon`
  - `bitfs/internal/client`
  - `libbitfs-go/x402`
  - `libbitfs-go/spv`
  - `libbitfs-go/storage`
  - `libbitfs-go/vault`

结果：上述命令均通过。

说明：测试通过不代表支付状态机安全正确；当前阻断项主要来自业务安全语义与异常路径一致性，而非编译/单测层错误。

## 发布门槛建议（Release Gate）

上线前至少完成以下 5 项：

1. 禁止 `/_bitfs/pay` 空 body 直接成功（默认生产行为）。
2. 修复 `handlePayInvoice` 的 `Expiry` 传递，确保有效 raw_tx 可以通过校验。
3. `/_bitfs/buy` 强制“广播成功后再揭示 capsule”；`chain == nil` 不得放行。
4. 为 `NO_CAPSULE` 等失败出口补齐完整回滚（支付状态 + 防重放状态）。
5. 同步 `x402` 规范与实现差异，形成单一可信版本。

## 审查结论

当前版本具备较好的工程测试基础，但支付路径存在可被利用的安全与一致性缺陷。完成本文 P0/P1 修复并补充回归用例后，再进行一次短周期复审可进入发布候选。

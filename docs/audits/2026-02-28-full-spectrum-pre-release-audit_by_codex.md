# BitFS / Metanet 发布前全景深度审查报告

- 日期: 2026-02-28
- 作者: Codex (GPT-5)
- 结论: 当前不建议直接发布，需先完成 P0/P1 修复

## 审查范围

- 宏观: 商业方向、产品定位、发布策略一致性
- 中观: 规格文档与模块实现一致性（BitFS + Metanet）
- 微观: 关键代码路径（支付、握手、并发状态机、CLI 买入流程）
- 验证信号: go test / go vet / race / 覆盖率

## 执行摘要

本次审查确认：

1. `libbitfs-go` 核心库总体工程质量较高，测试通过率和覆盖率表现良好。
2. BitFS 主产品的“付费购买主链路”存在协议字段与编码不一致，属于发布阻断问题。
3. `/_bitfs/pay` 与 `/_bitfs/buy` 的支付状态机仍有高风险分支，可能导致“未支付标记已支付”或“状态不一致”。
4. Metanet 侧与文档宣称的“可运营节点产品”仍存在显著差距（stub 命令、身份算法偏差、退出码偏差）。

---

## 发现清单（按严重度）

### P0-1 购买协议字段不一致：`total_price` vs `price`

问题：daemon 在 `GET /_bitfs/buy/{txid}` 返回 `total_price`，client `BuyInfo` 结构却解析 `price`。`buy.Buy()` 使用 `buyInfo.Price` 构建 HTLC，因此金额会变成 0。

证据：
- `bitfs/internal/daemon/payment.go:231`
- `bitfs/internal/client/client.go:63`
- `bitfs/internal/buy/buy.go:79`
- 设计文档明确返回 `total_price`: `design/bitfs/3-DetailedDesign.zh.md:1844`

影响：paid 主链路在真实服务端返回下不可用或异常。

建议：统一字段名（建议全链路使用 `total_price`，并兼容读取旧 `price` 一段时间）。

### P0-2 支付地址编码不一致：base58 地址被当作 hex 解码

问题：daemon 返回 `payment_addr` 为 P2PKH 地址字符串（base58），而买方代码按 hex 解码为 20-byte hash。

证据：
- daemon 生成地址字符串：`bitfs/internal/daemon/payment.go:75`, `bitfs/internal/daemon/payment.go:80`
- buy 侧 hex 解码：`bitfs/internal/buy/buy.go:67`

影响：HTLC 资金交易构建失败或资金脚本错误。

建议：统一传输语义。可选方案：
- 方案 A: API 始终传 base58 地址，buy 侧转 `script.Address -> pubkey hash`
- 方案 B: API 单独传 `seller_pkh_hex`（20-byte hex）

### P0-3 `/_bitfs/pay/{invoice_id}` 空请求可直接记账为已支付

问题：`len(body)==0` 时不做支付证明校验，最终仍 `invoice.Paid = true`。

证据：
- `bitfs/internal/daemon/payment.go:417`
- `bitfs/internal/daemon/payment.go:467`
- 测试将其视为成功：`bitfs/internal/daemon/daemon_test.go:2016`

影响：零成本伪造支付。

建议：生产模式禁用空 body 成功路径；若保留仅用于测试，需显式开关且默认关闭。

### P0-4 `/_bitfs/pay` 的验证路径会被过期校验误杀

问题：`handlePayInvoice` 调 `x402.VerifyPayment` 时构造的 invoice 未设置 `Expiry`，而 `VerifyPayment` 第一阶段先检查过期。

证据：
- `bitfs/internal/daemon/payment.go:440`
- `libbitfs-go/x402/verify.go:24`

影响：非空支付证明路径不可用，实质推动系统依赖不安全分支（P0-3）。

建议：传递 `Expiry: invoice.Expiry.Unix()` 并补充正向回归用例。

---

### P1-1 `/_bitfs/buy` 在 `chain == nil` 时可绕过上链验证

问题：只在 `d.chain != nil` 时广播支付交易；未配置链服务时仍可能继续并返回 capsule。

证据：
- `bitfs/internal/daemon/payment.go:346`
- 规范要求释放内容前支付证明需验证：`bitfs/docs/spec/09-daemon.md:173`

影响：支付原子性边界变弱。

建议：生产路径强制 `chain != nil` 且广播成功后才能返回 capsule。

### P1-2 `NO_CAPSULE` 分支状态机不完整（已置 Paid 但未回滚）

问题：`handleSubmitHTLC` 先 `invoice.Paid = true`，后续若 `len(invoice.Capsule)==0` 返回 500，但未 rollback。

证据：
- `bitfs/internal/daemon/payment.go:287`
- `bitfs/internal/daemon/payment.go:362`

影响：账务状态与交付状态不一致。

建议：在该分支及所有失败出口统一回滚 `Paid` 与 `usedTxIDs`。

### P1-3 Method42 会话仅“创建”未“消费”

问题：握手会话（`session_id`, `SessionKey`）创建后未用于后续买入/内容请求授权或加密通道。

证据：
- 创建会话：`bitfs/internal/daemon/handshake.go:129`
- 会话查询 API 存在：`bitfs/internal/daemon/daemon.go:417`
- 业务 handler 无对应会话校验链路（仅 CORS 允许头）

影响：设计上的握手安全闭环未真正落地。

建议：为受保护端点引入 session 验证中间件，或在文档中降级声明为“预留能力”。

### P1-4 Metanet CLI 与规格承诺不一致（多命令仍 stub）

问题：`start/contracts/peers/mine` 未实现运营能力，`start` 仅打印 stub 并退出。

证据：
- `metanet/cmd/metanet/cmd_start.go:20`, `metanet/cmd/metanet/cmd_start.go:76`
- `metanet/cmd/metanet/cmd_contracts.go:15`
- `metanet/cmd/metanet/cmd_peers.go:15`
- `metanet/cmd/metanet/cmd_mine.go:15`
- 规格承诺完整生命周期：`metanet/docs/spec/cmd-metanet.md:21`

影响：不适合按“可运营节点产品”口径对外发布。

建议：
- 对外版本明确标注 preview/experimental，或
- 完成最小可运行 daemon/peer/contract 读写能力后再发布。

### P1-5 Metanet 身份算法偏差：实现 `ed25519`，规格要求 secp256k1 体系

问题：`metanet init` 生成 ed25519 key；规格文档在多个模块定义 33-byte compressed pubkey（secp256k1）。

证据：
- 实现：`metanet/cmd/metanet/cmd_init.go:8`
- 规格：`metanet/docs/spec/cmd-metanet.md:16`
- 规格：`metanet/docs/spec/overlay.md:20`
- 规格：`metanet/docs/spec/payment.md:34`

影响：跨模块接口/签名验证/与 BitFS 共享密码体系不一致。

建议：统一到 secp256k1（与 BSV/BitFS/Method42 对齐）。

---

### P2-1 b-tools 的 paid 默认体验与文档不一致

问题：文档示例仅要求 `--wallet-key` 即可购买，但代码若无 `--utxo` 且未注入 `Blockchain`，会直接报 “no UTXOs available”。

证据：
- 调用 buy 不传 Blockchain：
  - `bitfs/cmd/bget/main.go:281`
  - `bitfs/cmd/bcat/main.go:220`
  - `bitfs/cmd/bmget/main.go:377`
- buy 逻辑要求 Blockchain 或手工 UTXO：`bitfs/internal/buy/buy.go:162`
- 文档示例仅 `--wallet-key`：`bitfs/docs/user-guide.md:380`

影响：用户按文档操作会失败。

建议：
- CLI 注入链服务支持自动 UTXO；或
- 更新文档明确 `--utxo`/RPC 先决条件。

### P2-2 自动 UTXO 选择的地址格式疑点

问题：`buy.resolveUTXOs` 将 `PubKeyHash` hex 作为 address 传 `ListUnspent`，而 RPC 测试语义是 base58 address。

证据：
- 传入 hex hash：`bitfs/internal/buy/buy.go:168`
- RPC 测试期望 base58 地址：`libbitfs-go/network/rpc_blockchain_test.go:78`

影响：一旦启用自动选币，可能查不到 UTXO。

建议：将私钥派生出的公钥转标准地址字符串后再调用 RPC。

### P2-3 文档/实现漂移：`--wallet-key` 说明与实现不一致

问题：文档写支持 32 或 33 字节私钥；实现仅接受 32。

证据：
- 文档：`bitfs/docs/user-guide.md:390`
- 实现：`bitfs/internal/buy/config.go:130`

影响：操作预期偏差与支持成本上升。

建议：二选一：
- 扩展实现支持 33-byte 格式；或
- 修正文档为“仅 32-byte scalar”。

---

## 宏观评估（商业与发布策略）

1. BitFS 主产品方向正确（文件系统 + 付费内容 + Agent 友好接口），但付费链路的实现一致性问题会直接伤害首批用户体验。
2. Metanet 当前更像“技术预研原型”而非可运营网络产品。建议与 BitFS 发行节奏解耦。
3. 推荐发布策略：
   - BitFS 修复 P0/P1 后进入 RC
   - Metanet 明确标注预览版，不绑定商业承诺

## 验证信号

已检查并复核：

- `go test ./...`（bitfs/libbitfs-go/metanet）通过
- `go vet ./...`（三模块）通过
- 关键路径 `-race` 抽样通过（daemon/x402/spv/storage/vault）
- 覆盖率观察：
  - libbitfs-go 多数包 >=80%
  - bitfs/cmd/bitfs 约 35%，命令面回归风险仍较高

> 说明：测试通过不等于发布可行；本报告阻断项主要来自协议字段一致性、支付状态机与产品完成度。

## 发布门槛（建议）

上线前至少满足：

1. 修复 paid 购买字段与地址编码不一致（P0-1, P0-2）。
2. 封堵 `/_bitfs/pay` 空请求支付漏洞并修复 Expiry 校验链（P0-3, P0-4）。
3. `/_bitfs/buy` 强制上链验证 + 完整失败回滚（P1-1, P1-2）。
4. 增加“真实 daemon + client + bget --buy”端到端回归用例，覆盖 `total_price/payment_addr` 协议字段。
5. Metanet 若对外发布，先消除 stub 能力与身份算法偏差；否则明确为 preview。

---

作者说明：本报告由 **Codex (GPT-5)** 编写并提交。

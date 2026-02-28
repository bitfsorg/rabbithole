# 2026-02-28 安全审查报告（代码/加密/协议）

## 审查范围
- 仓库：`/Users/alex/Codes/RabbitHole`
- 重点模块：
  - `bitfs/internal/daemon`
  - `libbitfs-go/method42`
  - `libbitfs-go/x402`
  - `metanet/internal/payment`
  - `metanet/internal/overlay`
  - `den`
- 审查维度：
  - 认证/授权与接口滥用
  - 支付与防重放流程
  - 并发与锁使用（可用性/DoS）
  - 加密与密钥派生实现
  - 输入校验与 panic 风险

## 方法
- 静态审查关键安全路径源码并定位到行号
- 结合现有测试验证当前行为是否被“固化”
- 执行模块测试确认基础状态：
  - `bitfs`: `go test ./...` 通过
  - `metanet`: `go test ./...` 通过
  - `den`: 主包通过；`den/cmd/seed` 构建失败（非本报告核心安全项）

## 发现（按严重级别）

### Critical-1：`/_bitfs/pay/{invoice_id}` 可零成本将订单置为 paid
- 证据：
  - 代码注释明确“空 body 标记 paid”：`bitfs/internal/daemon/payment.go:385`
  - 实现上仅在 `len(body) > 0` 才验交易；否则直接 `invoice.Paid = true`：
    - `bitfs/internal/daemon/payment.go:417`
    - `bitfs/internal/daemon/payment.go:467`
  - 测试固化该行为（空 body 返回 paid）：
    - `bitfs/internal/daemon/daemon_test.go:2016`
- 影响：
  - 只要拿到 `invoice_id`，可不支付直接“成功支付”。
- 风险等级：Critical

### Critical-2：`/_bitfs/buy/{txid}` 在未配置链广播时可伪造支付换 capsule
- 证据：
  - `Daemon` 默认可不设置 `chain`：
    - `bitfs/internal/daemon/daemon.go:262`
    - `bitfs/internal/daemon/daemon.go:302`
  - 提交支付仅在 `d.chain != nil` 时广播：
    - `bitfs/internal/daemon/payment.go:346`
- 影响：
  - 在默认/误配置部署下，业务层通过即可泄露 capsule，经济安全失效。
- 风险等级：Critical

### High-1：`x402.VerifyPayment` 未校验交易可上链性，仅做输出匹配
- 证据：
  - `libbitfs-go/x402/verify.go:49-97` 仅遍历输出匹配地址与金额。
- 影响：
  - 本地伪造/不可上链交易可能通过业务校验。
  - 若调用方未做广播与确认约束，可被绕过。
- 风险等级：High

### Medium-1：`handlePayInvoice` 持锁读取请求体，存在 DoS 放大面
- 证据：
  - 先加锁：`bitfs/internal/daemon/payment.go:394`
  - 后读取 body：`bitfs/internal/daemon/payment.go:417`
- 影响：
  - 慢连接可长时间占用 `invoicesMu`，阻塞并发支付/查询路径。
- 风险等级：Medium

### Medium-2：`den` SPV 校验存在越界 panic 风险
- 证据：
  - 固定 32 字节数组按输入全长反向写入，未验证长度：
    - `den/verify.go:61-64`
- 影响：
  - 异常 `proof.TxID`（长度>32）可触发 panic，导致 DoS。
- 风险等级：Medium

### Medium-3：Overlay 广告“验签”非真实密码学验证
- 证据：
  - 明确为模拟签名：
    - `metanet/internal/overlay/advertise.go:21-23`
  - `VerifyAdvertisement` 仅检查格式/长度，不做公钥验签：
    - `metanet/internal/overlay/advertise.go:45-58`
- 影响：
  - 若用于真实网络，节点身份可伪造。
- 风险等级：Medium（在生产使用该模块时可升高）

### Low-1：支付通道代码存在 panic 路径
- 证据：
  - 随机源失败直接 panic：
    - `metanet/internal/payment/channel.go:325`
- 影响：
  - 极端环境下可用性风险（应返回错误而非崩溃）。
- 风险等级：Low

## 额外观察
- `bitfs` 的 `SecurityConfig.MaxRequestSize` 有配置项但未看到统一生效路径（仅定义默认值）：
  - `bitfs/internal/daemon/daemon.go:125`
  - `bitfs/internal/daemon/daemon.go:191`
- 该项不直接构成漏洞，但会降低可配置防护的一致性。

## 修复优先级建议
1. 立即移除/隔离 `/_bitfs/pay` 空 body 自动 paid 逻辑（测试逻辑改为 test-only）。
2. 在 `/_bitfs/buy` 与 `/_bitfs/pay` 强制链广播成功后再标记 paid，并建议引入最小确认策略。
3. 将 `x402.VerifyPayment` 的“输出匹配”定位为“预校验”，并在调用侧强制上链可接受性校验。
4. 重构 `handlePayInvoice` 为“先读请求体，后加锁”。
5. 修复 `den/verify.go`：强制 `txid` 解码后长度必须为 32。
6. 若 `overlay`/`payment` 进入生产路径，替换模拟签名与 panic 实现。

## 复现要点
- 空 body 支付行为可用现有测试直接复现：`TestHandlePayInvoice_EmptyBody`
  - `bitfs/internal/daemon/daemon_test.go:2016`
- `chain == nil` 路径可在未调用 `SetChain(...)` 的 daemon 实例中触发：
  - `bitfs/internal/daemon/daemon.go:302`
  - `bitfs/internal/daemon/payment.go:346`

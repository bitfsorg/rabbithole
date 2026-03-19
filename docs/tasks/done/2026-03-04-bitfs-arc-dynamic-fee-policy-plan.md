# 2026-03-04 BitFS ARC 动态费率策略改造计划

> 状态: **Done（文档完成，2026-03-04）**
>
> 目标仓库: `bitfs` + `libbitfs-go`
>
> 对应用例: `2026-03-04-bitfs-arc-dynamic-fee-policy-test-cases.md`

## 完成情况（文档）

- [x] 冻结费率来源优先级（override > ARC policy > fallback）。
- [x] 冻结单位与换算规则（统一 `sat/KB`，`ceil(satoshis*1000/bytes)`）。
- [x] 冻结缓存/刷新与 `465` 重试策略（TTL=5m，最多一次 refresh+retry）。
- [x] 冻结分层测试范围并与用例文档一一对应（39 条）。
- [x] 输出可直接执行的分阶段落地顺序与回归命令。

> 说明：本文件“完成”指计划与执行标准已冻结并可直接落地实施；代码实现与测试执行结果按本计划推进。

## 0. 执行顺序（已冻结）

1. 先冻结测试用例（已完成，39 条）。
2. 按用例分阶段实现代码（TDD：Red -> Green -> Refactor）。
3. 通过 unit/integration 后，再跑 live smoke。

## 1. 目标

将当前硬编码费率改为 **ARC policy 驱动 + 可回退**：

- live 网络费率优先取 ARC `/v1/policy`；
- 手动覆盖可应急兜底；
- policy 不可用时自动回退 `100 sat/KB`；
- 所有路径统一单位 `sat/KB`。

## 2. 当前基线与缺口

当前仍存在固定费率路径：

- `bitfs/e2e/testutil/funder.go`：构建 funding tx 时写死 `100 sat/KB`。
- `bitfs/internal/buy/buy.go`：`defaultFeeRate = 100` 常量直用。
- `libbitfs-go/payment` 与 `libbitfs-go/tx`：默认费率常量语义未对齐“fallback”。

缺口：

- 无 `GET /v1/policy` 读取与解析；
- 无 endpoint+network 维度 policy 缓存；
- 无 `465` 自动 refresh + retry 机制；
- 无统一费率来源观测字段。

## 3. 冻结决策

### D1. 费率来源优先级

1. `BITFS_E2E_FEE_RATE_SAT_PER_KB[_MAINNET|_TESTNET|_REGTEST]`
2. ARC `GET /v1/policy -> policy.miningFee`
3. fallback `100 sat/KB`

规则：

- 后缀变量优先于无后缀变量。
- 命中手动覆盖后不再请求 policy。
- 内部和日志统一输出 `sat/KB`。

### D2. 单位换算

- ARC 返回: `satoshis / bytes`
- 目标单位: `sat/KB`
- 公式: `ceil(satoshis * 1000 / bytes)`
- 约束: `satoshis > 0 && bytes > 0`
- 防御下限: 最小 `1 sat/KB`

### D3. 缓存与刷新

- 缓存 key: `network + endpoint`
- TTL: `5m`
- `465 Fee Too Low`：仅允许一次 `refresh + retry`
- policy 请求超时: `3s`

### D4. 多 endpoint

- 使用“当前广播 endpoint”的 policy。
- failover 后按新 endpoint 独立缓存读取。
- 不做多 endpoint 聚合（v2 不引入）。

### D5. 可观测字段

- `fee_rate_sat_per_kb`
- `fee_source` (`override|arc_policy|fallback`)
- `arc_endpoint`
- `policy_timestamp`
- `retry_on_465` (`0|1`)

## 4. 实施设计（按仓库）

### 4.1 `bitfs/e2e/testutil`

- `arc_client.go`
  - 新增 `policy(ctx)`（`GET /v1/policy`）与响应结构体。
  - 新增 miningFee -> sat/KB 换算 helper（含边界与溢出保护）。
- 新增 `policy_fee.go`（建议）
  - 封装 `ResolveFeeRate(ctx, endpoint, refresh bool)`。
  - 维护 endpoint+network 缓存（TTL `5m`）。
- `config.go`
  - 增加 `FeeRateSatPerKB`（含网络后缀解析）。
- `funder.go`
  - `buildFundingRawTxFromWIF(...)` 接收动态 `feeRateSatPerKB`。
  - 去除构建流程中的硬编码 `100`。
- `arc_node.go`
  - broadcast/fund 路径接入 dynamic fee provider。
  - 处理 `465`：强制刷新 policy 后重试一次。

### 4.2 `bitfs/internal/buy`

- `config.go`
  - 新增可选 `FeeRateSatPerKB` 配置项（env/flag 接口）。
- `buy.go`
  - 用“解析后费率”替换 `defaultFeeRate` 直接引用。
  - 将 `fee_source` 注入调试日志/输出（至少在调试模式下可见）。
- `utxo.go`
  - 继续复用 `sat/KB` 估算函数，不引入第二套单位。

### 4.3 `libbitfs-go`

- `tx` / `payment` 维持默认 `100 sat/KB`，但语义明确为 fallback。
- 补齐注释与测试，确保 `feeRate=0` 分支一致走 fallback。
- 不在 v2 引入新的外部依赖或 policy client。

## 5. TDD 分阶段（绑定用例 ID）

### Phase 1: 核心单测先行

- 落地 `U-POL-*`、`U-CFG-*`、`U-CACHE-*`、`U-465-*`。
- 目标: `bitfs/e2e/testutil` 新增/改造能力全由单测驱动。

### Phase 2: Buy 与 libbitfs-go 收口

- 落地 `U-BUY-*`、`U-LIB-*`。
- 目标: 消除 buy 路径固定费率常量直连。

### Phase 3: Integration

- 落地 `I-ARC-*`（mock ARC + failover + timeout + retry）。
- 目标: 验证跨组件行为而非仅函数级行为。

### Phase 4: Live Smoke

- 落地 `E-LIVE-*`，先 testnet，后 mainnet（显式启用）。
- 目标: 真实端点最小链路可复现，日志可追踪费率来源。

## 6. 验收标准

1. 新增能力全部被测试用例文档覆盖且可自动执行。
2. live 路径默认不再依赖写死费率常量。
3. `465` 重试最多一次且可观测。
4. policy 不可用时交易流程不中断并回退 fallback。
5. `bitfs` 与 `libbitfs-go` 相关测试集通过。

## 7. 风险与缓解

- 风险: policy 接口短时抖动  
  缓解: 3s 超时 + TTL 缓存 + fallback。

- 风险: endpoint 切换造成费率抖动  
  缓解: endpoint 隔离缓存，切换后重新解析 policy。

- 风险: 单位混用（sat/b vs sat/KB）  
  缓解: 统一 helper + 强制断言 `fee_source` 与单位字段。

## 8. 回归命令草案

```bash
# bitfs
cd bitfs
go test -tags e2e ./e2e/testutil -v -count=1
go test ./internal/buy/... -v -count=1

# libbitfs-go
cd ../libbitfs-go
go test ./tx ./payment -v -count=1

# smoke (live)
cd ../bitfs
go test -tags e2e ./e2e -run "TestWalletFund|TestSPVVerify|TestFundExternalUTXO" -v -count=1 -timeout 60m
```

## 9. 实施追踪矩阵（开发侧）

| 阶段 | 目标文件/模块 | 对应用例组 | 退出条件 |
|---|---|---|---|
| Phase 1 | `bitfs/e2e/testutil/{arc_client.go,policy_fee.go,config.go,funder.go,arc_node.go}` | `U-POL-*`, `U-CFG-*`, `U-CACHE-*`, `U-465-*` | 单测全绿，硬编码 `100` 从 live 路径移除 |
| Phase 2 | `bitfs/internal/buy/{config.go,buy.go,utxo.go}` | `U-BUY-*` | buy 路径不再直连固定费率常量 |
| Phase 3 | `libbitfs-go/{tx,payment}` | `U-LIB-*` | `feeRate=0` 统一 fallback 语义，测试覆盖 |
| Phase 4 | `bitfs/e2e/...`（mock ARC + live smoke） | `I-ARC-*`, `E-LIVE-*` | testnet smoke 可复现，mainnet 仅显式启用 |

## 10. 交付物清单（本次完成）

- [x] 改造计划文档（本文件）完成并冻结。
- [x] 对应用例文档完成并冻结：`2026-03-04-bitfs-arc-dynamic-fee-policy-test-cases.md`。
- [x] 决策参数、验收标准、风险缓解、回归命令齐备。

# 2026-03-04 BitFS ARC 动态费率测试用例

> 状态: **Done（文档完成，2026-03-04）**
>
> 对应计划: `2026-03-04-bitfs-arc-dynamic-fee-policy-plan.md`
>
> 执行原则: **先测后改（Test-first）**

## 完成情况（文档）

- [x] 用例总数冻结为 **39**，覆盖 unit/integration/e2e smoke 三层。
- [x] 每条核心机制均有正常、异常、边界路径（policy、缓存、`465`、failover）。
- [x] 与计划文档的 Phase 分解、命令入口、验收标准保持一致。
- [x] 输出可直接映射到目标文件的建议落点与执行顺序。

> 说明：本文件“完成”指测试设计已冻结并可直接转化为自动化测试代码；测试运行结果按实施节奏产出。

## 1. 冻结参数

- fallback 费率: `100 sat/KB`
- policy TTL: `5m`
- policy 请求超时: `3s`
- `465 Fee Too Low` 自动重试上限: `1`
- 手动覆盖变量:
  - `BITFS_E2E_FEE_RATE_SAT_PER_KB`
  - `BITFS_E2E_FEE_RATE_SAT_PER_KB_MAINNET`
  - `BITFS_E2E_FEE_RATE_SAT_PER_KB_TESTNET`
  - `BITFS_E2E_FEE_RATE_SAT_PER_KB_REGTEST`
- 统一单位: 内部一律 `sat/KB`

## 2. 分层与数量

| 层级 | 目标模块 | 目标 | 用例数 |
|---|---|---|---|
| Unit | `bitfs/e2e/testutil` | policy 拉取/换算、缓存、465 重试 | 17 |
| Unit | `bitfs/internal/buy` | 买方费估算动态化 | 6 |
| Unit | `libbitfs-go` | fee fallback 语义收口 | 4 |
| Integration | `bitfs/e2e/testutil` + mock ARC | endpoint/failover/timeout 联动 | 8 |
| E2E Smoke | `bitfs/e2e` | testnet/mainnet 最小真实链路 | 4 |
| 合计 | - | - | **39** |

## 3. Unit 用例

### 3.1 Policy 解析与单位换算（`bitfs/e2e/testutil`）

| ID | 目标函数（新增） | 场景 | 输入 | 预期 |
|---|---|---|---|---|
| U-POL-001 | `satPerKBFromMiningFee` | 标准换算 | `100/1000` | `100 sat/KB` |
| U-POL-002 | `satPerKBFromMiningFee` | 0.5 sat/b | `1/2` | `500 sat/KB` |
| U-POL-003 | `satPerKBFromMiningFee` | 向上取整 | `1/3` | `334 sat/KB` |
| U-POL-004 | `satPerKBFromMiningFee` | bytes=0 | `100/0` | 返回错误 |
| U-POL-005 | `satPerKBFromMiningFee` | satoshis=0 | `0/1000` | 返回错误 |
| U-POL-006 | `parsePolicyResponse` | 缺失 `policy.miningFee` | 缺字段 JSON | 返回错误 |
| U-POL-007 | `parsePolicyResponse` | 非法类型 | `satoshis="abc"` | 返回错误 |
| U-POL-008 | `satPerKBFromMiningFee` | 极端小值 | `1/10000000` | 下限保护为 `1 sat/KB` |

### 3.2 配置优先级（`bitfs/e2e/testutil/config.go`）

| ID | 场景 | 输入 | 预期 |
|---|---|---|---|
| U-CFG-001 | 后缀优先 mainnet | 同时设置 `*_MAINNET` 与无后缀 | 命中 `*_MAINNET` |
| U-CFG-002 | 后缀优先 testnet | 同时设置 `*_TESTNET` 与无后缀 | 命中 `*_TESTNET` |
| U-CFG-003 | 后缀缺失回退 | 只设无后缀 | 命中无后缀值 |
| U-CFG-004 | 手动覆盖最高优先级 | 设 `BITFS_E2E_FEE_RATE_SAT_PER_KB_MAINNET` | 不请求 `/v1/policy` |
| U-CFG-005 | 后缀覆盖无后缀 | 同时设 `..._SAT_PER_KB` 与 `..._SAT_PER_KB_MAINNET` | 取后缀值 |
| U-CFG-006 | 非法手动值 | `-1`、`abc` | 忽略该值，走 policy/fallback |

### 3.3 缓存与刷新（`bitfs/e2e/testutil` 新增 provider）

| ID | 场景 | 步骤 | 预期 |
|---|---|---|---|
| U-CACHE-001 | TTL 命中 | 连续 2 次读取 | 第 2 次不发 HTTP |
| U-CACHE-002 | TTL 过期 | 超过 `5m` 再读 | 发起新 HTTP 拉取 |
| U-CACHE-003 | endpoint 隔离 | endpoint A/B 分别读取 | A/B 缓存互不污染 |
| U-CACHE-004 | refresh=true | 强制刷新读取 | 忽略缓存，重新拉取 |
| U-CACHE-005 | policy 拉取失败缓存不污染 | 第一次返回 500，第二次返回 200 | 第二次成功后才入缓存 |

### 3.4 `465` 重试语义（`arc_node.go` / `funder.go`）

| ID | 场景 | 步骤 | 预期 |
|---|---|---|---|
| U-465-001 | 首次 465、二次成功 | broadcast#1=465 -> refresh -> broadcast#2=200 | 成功，重试次数=1 |
| U-465-002 | 两次都 465 | 465 -> refresh -> 465 | 失败，重试次数仍=1 |
| U-465-003 | 非 465 错误 | 首次 400/401/403 | 不刷新 policy，不重试 |
| U-465-004 | refresh 失败 | 465 + `/v1/policy` 失败 | 直接失败并返回原上下文错误 |

### 3.5 Buy 费用估算（`bitfs/internal/buy`）

| ID | 场景 | 输入 | 预期 |
|---|---|---|---|
| U-BUY-001 | policy 费率生效 | `feeRate=120` | HTLC 估算按 `120 sat/KB` |
| U-BUY-002 | fallback 生效 | policy 不可用 | 使用 `100 sat/KB` |
| U-BUY-003 | 手动覆盖生效 | env 覆盖 `250` | 使用 `250 sat/KB` |
| U-BUY-004 | 手动覆盖非法 | `feeRate=abc` | 忽略并回退 policy/fallback |
| U-BUY-005 | 低费率边界 | `feeRate=1` | 费用向上取整且 `>=1 sat` |
| U-BUY-006 | 手动 UTXO 校验 | 固定 UTXO 总额=price+fee-1 | 报 `ErrInsufficientBalance` |

### 3.6 `libbitfs-go` 语义收口

| ID | 模块 | 场景 | 预期 |
|---|---|---|---|
| U-LIB-001 | `tx.EstimateFee` | `feeRate=0` | 按 fallback `DefaultFeeRate` |
| U-LIB-002 | `payment` HTLC | `FeeRate=0` | 按默认 `100 sat/KB` |
| U-LIB-003 | `tx.EstimateFee` | 溢出输入 | 饱和到 `math.MaxUint64` |
| U-LIB-004 | 统一单位 | 新增 helper 注释/测试 | 明确单位始终 `sat/KB` |

## 4. Integration 用例（Mock ARC + `httptest`）

| ID | 场景 | 步骤 | 预期 |
|---|---|---|---|
| I-ARC-001 | 初次拉取 policy 后广播 | `/v1/policy=100/1000`，广播一次 | 广播使用 `100 sat/KB` |
| I-ARC-002 | policy 401 | `/v1/policy=401` | 使用 fallback，记录 `fee_source=fallback` |
| I-ARC-003 | policy 非法 JSON | `/v1/policy` 返回 malformed | 使用 fallback，记录 warning |
| I-ARC-004 | policy 超时 | `/v1/policy` 延迟 >3s | 快速回退 fallback，无长阻塞 |
| I-ARC-005 | 465 刷新后成功 | broadcast#1=465，refresh 后 policy 改高，broadcast#2=200 | 成功且仅重试一次 |
| I-ARC-006 | 465 刷新后仍失败 | broadcast 两次都 465 | 返回失败，带 retry 标记 |
| I-ARC-007 | endpoint failover | A 广播失败切 B；A/B policy 不同 | 切到 B 后按 B policy |
| I-ARC-008 | 手动覆盖禁用 policy 拉取 | 设置 `BITFS_E2E_FEE_RATE_SAT_PER_KB` | `/v1/policy` 命中次数为 0 |

## 5. E2E Smoke（最小真实链路）

| ID | 网络 | 场景 | 预期 |
|---|---|---|---|
| E-LIVE-001 | testnet | `Fund + publish` | 使用 policy 或 fallback 成功广播 |
| E-LIVE-002 | testnet | 指定手动覆盖 | 费率来源为 `override` |
| E-LIVE-003 | mainnet | 单笔最小 funding | 成功广播，日志含 `fee_source` |
| E-LIVE-004 | mainnet | policy 暂时不可用 | 自动 fallback 仍可广播 |

## 6. 断言规范（所有层级通用）

- 必须同时断言 `fee_rate_sat_per_kb` 与 `fee_source`（`override|arc_policy|fallback`）。
- 必须断言单位一致：所有公开/内部计算路径都使用 `sat/KB`。
- `465` 分支必须断言“最多一次重试”，防止重试风暴。
- policy 超时场景必须断言“主流程不中断”，回退时间不超过请求超时窗口。
- failover 场景必须断言“按 endpoint 隔离缓存”，不允许跨 endpoint 复用。

## 7. 建议测试文件落点

- `bitfs/e2e/testutil/policy_fee_test.go`（新增）
- `bitfs/e2e/testutil/policy_cache_test.go`（新增）
- `bitfs/e2e/testutil/arc_retry_465_test.go`（新增）
- `bitfs/internal/buy/fee_policy_test.go`（新增）
- `libbitfs-go/tx/fee_policy_test.go`（新增，可并入现有 `*_test.go`）

## 8. 执行命令（测试优先顺序）

```bash
# 1) unit: e2e/testutil
cd bitfs
go test -tags e2e ./e2e/testutil -v -count=1

# 2) unit: buy
go test ./internal/buy/... -v -count=1

# 3) unit: libbitfs-go
cd ../libbitfs-go
go test ./tx ./payment -v -count=1

# 4) integration + e2e smoke（按环境执行）
cd ../bitfs
go test -tags e2e ./e2e -run "TestWalletFund|TestSPVVerify|TestFundExternalUTXO" -v -count=1 -timeout 60m
```

## 9. 通过标准

1. Unit/Integration 覆盖正常、异常、边界三类路径。
2. `465` 重试、policy 缓存、failover 三个核心机制均有自动化断言。
3. testnet smoke 通过；mainnet smoke 仅在显式启用时执行。
4. 日志/调试输出可追踪费率来源，满足定位与审计需求。

## 10. 用例实施顺序（建议）

1. `U-POL-*` + `U-CFG-*`：先固化输入输出与优先级规则。
2. `U-CACHE-*` + `U-465-*`：再固化状态机行为（缓存与重试）。
3. `U-BUY-*` + `U-LIB-*`：收口业务层和底层库语义。
4. `I-ARC-*`：用 `httptest` 组装跨组件联动。
5. `E-LIVE-*`：最后执行 testnet/mainnet 最小真实链路。

## 11. 交付物清单（本次完成）

- [x] 用例分层、数量、ID 命名规范冻结。
- [x] 关键断言规范冻结（`fee_rate_sat_per_kb` + `fee_source`）。
- [x] 执行命令与建议测试文件落点冻结。

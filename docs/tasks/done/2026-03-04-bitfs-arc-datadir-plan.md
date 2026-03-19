# 2026-03-04 BitFS `ARC` 交互与网络数据目录规范改造计划

> 状态: **Done (2026-03-04)**
>  
> 目标仓库: `bitfs`（主改造）+ `libbitfs-go`（配置层支持）

## 完成情况（2026-03-04）

- [x] live 网络默认 provider 切换为 `arc`，支持 `ARC` 主备 endpoint。
- [x] e2e live 主路径不再等待链上确认；改为传播可见 + proof 驱动。
- [x] `testnet` 全量 e2e 已在 `ARC` 路径通过。
- [x] `DefaultDataDirForNetwork` 按网络目录生效：
  - mainnet: `~/.bitfs`
  - testnet: `~/.bitfs-testnet`
  - regtest: `~/.regtest`
- [x] CLI `--datadir` / `BITFS_DATADIR` / network 默认优先级落地。
- [x] 增加旧 `~/.bitfs` 检测下的迁移提示（testnet/regtest 默认目录启用时）。

## 1. 决策摘要

本计划只覆盖两项强制改造：

1. **链上交互改造（Live 网络）**  
   testnet/mainnet 不再以“链上轮询确认”为主流程；交易走 `ARC` 广播，并以 `ARC`/`BHS` 证明链路驱动 SPV 验证。

2. **默认数据目录规范化（按网络）**  
   默认目录调整为：
   - `mainnet` -> `~/.bitfs`
   - `testnet` -> `~/.bitfs-testnet`
   - `regtest` -> `~/.regtest`

---

## 2. 背景与现状问题

### 2.1 交互层现状

当前 e2e live provider 主要是 `RPC` / `WoC`，存在以下问题：

- 测试流程依赖 `WaitForConfirmation` 轮询；
- `WoC` 的 proof/tx 可见性存在索引延迟，容易出现“已广播但 proof 暂空”；
- 资金与确认状态查询是“拉模式”，网络波动时不稳定。

### 2.2 数据目录现状

当前 `libbitfs-go/config.DefaultDataDir()` 固定返回 `~/.bitfs`，导致：

- testnet / regtest 与 mainnet 默认目录混用风险；
- 本地测试和生产使用边界不清晰；
- 团队约定目录（`~/.bitfs-testnet` / `~/.regtest`）无法作为默认行为。

---

## 3. 范围定义

### In Scope

- `bitfs/e2e` 的 live 网络交互切换到 `ARC` 优先；
- 引入 proof-first 的等待与验证流程（替代确认轮询主路径）；
- 按网络解析默认 data dir；
- 补齐测试与文档（含迁移说明）。

### Out of Scope

- 业务协议（HTLC/Metanet）语义重设计；
- 主网矿工路由策略优化（多矿池调度算法）；
- GUI 产品（desktop/app/extension）的完整 UI 引导实现（仅文档与接口预留）。

---

## 4. 工作流 A: `ARC` 广播 + Proof 驱动 SPV

## A1. 目标行为（完成态）

在 `BITFS_E2E_NETWORK in {testnet, mainnet}` 下：

- 交易广播统一走 `ARC`；
- 交易状态不再依赖“链上确认轮询”作为主逻辑；
- proof 获取优先走 `ARC`（回调或状态接口），必要时以 `BHS` 校验区块头/merkle root；
- 测试等待条件由“确认数 >= N”转为“proof 可验证”。

## A2. 设计原则

- **单一广播入口**: live 网络仅 `ARC` 广播；
- **proof-first**: 验证以 merkle proof + header chain 为准；
- **可观测**: 每笔交易保留状态机日志（broadcasted / accepted / proof-ready / verified）；
- **可降级**: callback 不可达时允许短期主动查询 ARC 状态接口，但不回退到 WoC/RPC 确认轮询。

## A3. 代码改造点（文件级）

### `bitfs/e2e/testutil`

- `config.go`
  - 新增 `ARC` / `BHS` 配置字段（示例）：
    - `BITFS_E2E_PROVIDER=arc`
    - `BITFS_E2E_ARC_BASE_URL`
    - `BITFS_E2E_ARC_API_KEY`
    - `BITFS_E2E_ARC_CALLBACK_URL`（可选）
    - `BITFS_E2E_BHS_BASE_URL`
- `node.go`
  - provider 路由新增 `arc`；
  - live 网络默认 provider 从 `rpc` 调整为 `arc`（若需平滑过渡，可先保留开关）。
- 新增 `arc_client.go`
  - 封装 `ARC` 广播、状态查询、proof 获取；
  - 统一重试与速率限制处理。
- 新增 `arc_node.go`
  - 实现 `TestNode` 的 ARC 版本；
  - `SendRawTransaction/GetMerkleProof/GetTxStatus` 改为 ARC/BHS 路径。
- 视需要新增 `proof_waiter.go`
  - 管理 callback 与主动查询的统一等待逻辑。
- `funder.go`
  - 资金交易广播改用 ARC（不再直接 WoC `/tx/raw`）。

## A4. 接口契约调整

当前 `TestNode` 的 `WaitForConfirmation` 语义偏“确认数轮询”。计划调整为：

- 保留 `WaitForConfirmation` 仅供 `regtest`；
- 新增/替换为 `WaitForProof`（或等价命名），live 网络基于 proof-ready 结束等待；
- e2e helper 统一改为“等待可验证证明”而不是“等待区块确认计数”。

## A5. 测试与验收

### 回归范围

- 重点：`04_spv_verify`、`16_fund_external`；
- 扩展：所有依赖 `WaitForConfirmation` 的 e2e live 测试。

### 通过标准

- live 网络下不再出现“仅因确认轮询超时导致失败”；
- `SPVVerify` 能稳定拿到可验证 proof；
- e2e 日志出现完整状态机轨迹（broadcast -> proof-ready -> verified）。

## A6. 风险与缓解

- 风险: callback 不稳定  
  缓解: callback + pull 双通道，pull 仅查 ARC，不查 WoC/RPC 确认。
- 风险: ARC proof 最终一致性延迟  
  缓解: proof 查询指数退避 + 可配置超时。
- 风险: provider 切换引入回归  
  缓解: 保留 `woc/rpc` 临时兼容期，逐步下线。

---

## 5. 工作流 B: 按网络默认 `datadir` 规范化

## B1. 目标行为（完成态）

用户未显式传 `--datadir` 时：

- mainnet 默认 `~/.bitfs`
- testnet 默认 `~/.bitfs-testnet`
- regtest 默认 `~/.regtest`

并且行为在 CLI 与 e2e 文档保持一致。

## B2. 配置层改造点

### `libbitfs-go/config`

- 在 `config/config.go` 增加：
  - `DefaultDataDirForNetwork(network string) string`
  - `mainnet/testnet/regtest` 三分支映射
- 保留现有 `DefaultDataDir()` 兼容（返回 mainnet 路径，或委托到 `DefaultDataDirForNetwork("mainnet")`）。

### `bitfs/cmd/bitfs`

- 新增公共 helper（建议新文件 `cmd/bitfs/datadir.go`）：
  - 解析优先级建议：
    1. `--datadir` 显式传入
    2. 环境变量 `BITFS_DATADIR`
    3. `--network`（若命令支持）或 `BITFS_NETWORK`
    4. 网络默认目录映射
- `wallet init` 需要两阶段解析：
  - 先拿 `--network`；
  - 若用户未传 `--datadir`，再按网络回填默认目录。

## B3. 兼容与迁移策略

- 对已存在旧目录用户：
  - 启动时检测旧路径并给出一次性迁移提示；
  - 提供文档化迁移步骤（复制 + 权限校验 + 回滚）。
- 非交互环境不自动搬迁，只告警。

## B4. 测试与验收

- `libbitfs-go/config` 新增单测：
  - `DefaultDataDirForNetwork(mainnet/testnet/regtest)` 断言。
- `bitfs/cmd` 新增单测：
  - 不同 network 下默认 datadir 解析正确；
  - `--datadir` 优先级高于 network 默认。
- 文档验收：
  - e2e 手册与 CLI 指南使用一致目录约定。

---

## 6. 分阶段实施计划（建议）

### Phase 0 — 设计冻结（0.5 天）

- 冻结 provider 命名与 env 变量；
- 冻结 `WaitForProof` 接口语义；
- 冻结 datadir 解析优先级。

### Phase 1 — 最小可用改造（1.5 天）

- 完成 ARC client + arcNode（e2e 先落地）；
- 完成 `DefaultDataDirForNetwork` 与 CLI 默认目录映射；
- 关键测试 `04/16` 跑通。

### Phase 2 — 全量迁移与清理（1~2 天）

- e2e 其余 live 测试切换；
- 清理 WoC 直连依赖到“兼容模式”；
- 文档与命令示例全量更新。

### Phase 3 — 验收与发布（0.5 天）

- 回归矩阵执行；
- 输出迁移说明；
- 关闭旧流程开关（按发布策略可延后一版）。

---

## 7. DoD（完成定义）

- live 网络 e2e 主路径不再依赖链上确认轮询；
- `ARC` 广播 + proof 验证链路稳定可复现；
- 默认数据目录按网络区分生效；
- 文档、测试、代码三方一致。

---

## 8. 回归命令草案

```bash
# bitfs
cd bitfs
go test ./... -count=1
go test -tags e2e ./e2e -run "TestSPVVerify|TestFundExternalUTXO" -v -count=1

# libbitfs-go
cd ../libbitfs-go
go test ./... -count=1
```

---

## 9. 参考（接口来源）

- BSV 文档: ARC API  
  https://docs.bsvblockchain.org/network-topology/arc-endpoints/arc/api
- BSV 文档: ARC callback  
  https://docs.bsvblockchain.org/network-topology/arc-endpoints/arc/configuration/callback
- BSV 文档: BHS API  
  https://docs.bsvblockchain.org/network-topology/arc-endpoints/bhs/api
- BSV 文档: SPV 组件  
  https://docs.bsvblockchain.org/payments/spv-wallet/components

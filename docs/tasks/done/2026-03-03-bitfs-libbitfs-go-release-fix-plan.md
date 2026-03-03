# 2026-03-03 BitFS + libbitfs-go 发布修复计划

> 状态: **全部完成（2026-03-03）**，已完成回归验证，文档归档至 done。

## 1. 目标

在不引入新功能的前提下，完成发布前阻断项修复，建立可重复、可信的发布门禁基线。

## 2. 计划范围

### In Scope

- 修复 `bitfs` integration 失败（HTLC timeout 语义漂移）
- 修复 `make e2e` 默认超时配置与文档不一致
- 修正文档/注释中已确认的实现漂移
- 补齐最小 lint 清理策略（至少 `gofmt`）

### Out of Scope

- 新协议设计
- 跨仓库大规模重构
- 非本次审计发现的新增需求

## 3. 执行任务（按优先级）

### [x] P0-1: 修复 integration 阻断用例

目标:
- 使 `TestHTLCParamsZeroTimeout` 与当前 `libbitfs-go/payment` 语义对齐。

改动建议:
- 文件: `bitfs/integration/payment_flow_extra_test.go`
- 将“Timeout=0 必须报错”的断言改为“Timeout=0 可构建成功（timeout 在交易层处理）”。
- 同步更新用例注释，移除旧语义描述。

验收:
- `cd bitfs && go test -tags integration ./integration/... -v -count=1` 全通过。

完成记录:
- 已完成。用例改为 `require.NoError` + `assert.NotEmpty(script)`，并同步注释。

---

### [x] P0-2: 修复 e2e 默认命令超时阻断

目标:
- 保证 `make e2e` 默认即可跑完全量回归，不因超时误失败。

改动建议:
- 文件: `bitfs/Makefile`
- 将 `e2e` 目标中的 `-timeout 120s` 提升到与实测相符的值（建议 `600s`，保留余量）。
- 可引入变量: `E2E_TIMEOUT ?= 600s`，便于 CI 覆写。

文档同步:
- `bitfs/README.md`
- `bitfs/e2e/README.md`
- 保持命令示例一致。

验收:
- `cd bitfs && make e2e` 通过。

完成记录:
- 已完成。`Makefile` 新增 `E2E_TIMEOUT ?= 600s` 并接入 `e2e` 目标。
- `README.md` 与 `e2e/README.md` 的 e2e 命令已同步到 `-timeout 600s`。

---

### [x] P1-1: 清理已确认的注释漂移

目标:
- 让注释准确反映当前 HTLC 设计（plain Bitcoin Script）。

改动建议:
- 文件: `bitfs/integration/payment_flow_extra_test.go`（sCrypt 残留注释）
- 仅改注释，不改行为。

验收:
- 关键支付测试可读性与语义一致，无误导描述。

完成记录:
- 已完成。移除 integration 中 sCrypt 残留注释，改为 plain Bitcoin Script 描述。

---

### [x] P1-2: 修正发布文档统计漂移

目标:
- 清理 README 中已过时的测试统计数字，避免误导。

改动建议:
- 文件: `bitfs/README.md`
- 将硬编码测试数量改为“命令驱动描述”或更新为当前真实统计。

验收:
- 文档与当前仓库状态一致。

完成记录:
- 已完成。`bitfs/README.md` 已移除过时测试数量硬编码。

---

### [x] P1-3: 建立最小 lint 可用基线

目标:
- 至少消除 `bitfs`/`libbitfs-go` 中 `gofmt` 级别噪声。

改动建议:
- 先执行 `gofmt -w`（分批提交，避免超大 diff）。
- 第二阶段处理 `libbitfs-go` 的 `errorlint` 与 `revive`（可单独任务）。

验收:
- `gofmt -l` 对目标范围返回空。
- 发布分支不再新增格式告警。

完成记录:
- 已完成并超出最小目标：
  - 两仓库全量 `gofmt -w` 已执行，`gofmt -l` 为 0。
  - `libbitfs-go` 额外修复 `errorlint` 与 `revive`，两个仓库 `golangci-lint run ./...` 均为 `0 issues`。

## 4. 建议执行顺序

1. `P0-1` integration 语义修复  
2. `P0-2` e2e timeout 与文档统一  
3. 回归: unit + integration + e2e（默认命令）  
4. `P1-1/P1-2` 文档注释校准  
5. `P1-3` gofmt 基线清理  

## 5. 回归命令清单

```bash
cd bitfs
go test ./... -count=1
go test -tags integration ./integration/... -v -count=1
make e2e
go vet ./...

cd ../libbitfs-go
go test ./... -count=1
go vet ./...
```

## 6. 完成定义（DoD）

- `bitfs` 与 `libbitfs-go` 的发布门禁命令可一键稳定通过。
- 阻断项（B-01、B-02）关闭并有回归证据。
- 发布文档与当前实现语义一致。

## 7. 本次回归证据

```bash
cd bitfs
go test ./... -count=1
go test -tags integration ./integration/... -v -count=1
make e2e
go vet ./...
golangci-lint run ./...

cd ../libbitfs-go
go test ./... -count=1
go vet ./...
golangci-lint run ./...
```

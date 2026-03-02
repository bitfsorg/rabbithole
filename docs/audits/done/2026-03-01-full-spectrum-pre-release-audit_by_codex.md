# 2026-03-01 全面发布前审查报告（by Codex）

## 1. 审查元数据

- 审查日期：2026-03-01
- 审查类型：全面发布前审查（设计、文档、实现、测试、安全）
- 审查人：Codex
- 仓库路径：`/Users/alex/Codes/RabbitHole`
- 结论：**NO-GO（当前不建议发布）**
- 状态: All blocking findings confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md`. Archived 2026-03-03.

## 2. 审查范围

本次覆盖以下子项目与组件：

- `bitfs`
- `libbitfs-go`
- `metanet`
- `den-explorer`
- `git-remote-bitfs`
- `bitfs/dashboard`
- `bitfs-extension`
- `bitfs-app`
- `libbitfs-ts`（状态核验）

## 3. 执行方式与验证项

已执行（按模块）：

- `go test ./...`
- `go test ./... -cover`
- `go vet ./...`
- `govulncheck ./...`
- `npm run build`（dashboard、extension）
- `npm run test`（extension）

环境限制：

- 当前机器缺少 `flutter`，无法执行 `bitfs-app` 的 `flutter analyze / flutter test`。

## 4. 总体结论

当前存在**3 个阻断项（Blocker）**和多个高风险问题，不满足稳定发布条件。  
若强行发布，最直接后果是：部分组件不可构建、管理面默认暴露、文档与实际行为不一致。

---

## 5. 关键问题清单（按严重级别）

## 5.1 Blocker

### B-01 `git-remote-bitfs` 编译失败（发布阻断）

`git-remote-bitfs` 使用了已不存在的枚举名：

- `tx.BatchOpNodeUpdate`
- `tx.BatchOpChildCreate`

而 `libbitfs-go/tx` 当前定义为：

- `OpCreate`
- `OpUpdate`
- `OpDelete`
- `OpCreateRoot`

直接导致该模块 `go test ./...`、`go vet ./...`、`govulncheck ./...` 均无法完整通过。

证据：

- `git-remote-bitfs/internal/chain/anchor.go:115`
- `git-remote-bitfs/internal/chain/anchor.go:118`
- `git-remote-bitfs/internal/chain/anchor.go:228`
- `git-remote-bitfs/internal/chain/anchor.go:230`
- `git-remote-bitfs/internal/chain/writer.go:88`
- `git-remote-bitfs/internal/chain/writer.go:90`
- `libbitfs-go/tx/batch.go:18`

---

### B-02 Daemon 管理面默认安全姿态过宽（发布阻断）

当前组合行为：

- 默认监听 `:8080`
- 默认 CORS `*`
- `admin_token` 为空时，管理接口直接放行（“兼容旧行为”）
- CLI 启动路径未提供 `admin_token` 等安全配置入口，使用默认 config

这意味着典型部署下，`/_bitfs/sales`、`/_bitfs/dashboard/*` 可能在无认证下被访问。

证据：

- `bitfs/internal/daemon/routes.go:89`（token 为空直接放行）
- `bitfs/internal/daemon/daemon.go:173`（默认 `:8080`）
- `bitfs/internal/daemon/daemon.go:189`（默认 `CORS=*`）
- `bitfs/cmd/bitfs/cmd_daemon.go:52`（仅 `--listen` 等参数）
- `bitfs/cmd/bitfs/cmd_daemon.go:86`（启动时直接用 `DefaultConfig`）

---

### B-03 Dashboard 文档与实际集成不一致（发布阻断）

文档写明 dashboard 由 daemon 服务在 `/_dashboard/*` 提供，但代码层并未完成：

- `go:embed` 指令仍注释
- daemon 路由未注册 `/_dashboard/*` 的静态资源服务

导致“按文档可用”的能力在当前二进制内不可达。

证据：

- `bitfs/dashboard/README.md:34`
- `bitfs/dashboard/embed.go:10`
- `bitfs/internal/daemon/routes.go:20`
- `bitfs/internal/daemon/routes.go:63`

---

## 5.2 High

### H-01 `bitfs-extension` 处于骨架状态，不具备可发布功能完整性

核心模块仍为 TODO：

- Method42 密码学实现缺失
- 钱包/Metanet/x402 逻辑缺失
- service worker 未实现实际消息处理
- content script 未实现链路
- 测试仅 `it.todo`，无有效断言覆盖

构建虽通过，但功能完整度不足，发布风险高。

证据：

- `bitfs-extension/src/bitfs-core/method42.ts:11`
- `bitfs-extension/src/background/service-worker.ts:10`
- `bitfs-extension/src/content-script/index.ts:4`
- `bitfs-extension/test/method42.test.ts:4`

---

### H-02 `bitfs-app` 处于占位实现状态，不具备可发布功能完整性

关键路径未实现：

- FFI bridge `init/getStatus/free` 直接抛 `UnimplementedError`
- 钱包/存储/provider 层均为 TODO 占位
- UI 页面仍是静态提示

证据：

- `bitfs-app/lib/core/ffi/bridge.dart:41`
- `bitfs-app/lib/providers/wallet_provider.dart:6`
- `bitfs-app/lib/screens/wallet_screen.dart:24`

---

### H-03 钱包网络显示与配置读取存在一致性风险

`wallet show` 与共享加载函数中使用 `wallet.MainNet` 初始化钱包，未显式按配置网络初始化，可能造成网络显示或行为与初始化配置不一致。

证据：

- `bitfs/cmd/bitfs/cmd_wallet.go:255`
- `bitfs/cmd/bitfs/cmd_wallet.go:433`

---

## 5.3 Medium

### M-01 文档路径错误（可用性/可信度问题）

`den-explorer` 快速开始中目录示例与仓库真实目录不一致（`cd ../../den`）。

证据：

- `den-explorer/README.md:13`

---

### M-02 测试策略盲区

- `den-explorer` 集成测试强依赖外部 RPC，不可用时跳过，导致常态化环境下测试信号弱。
- `bitfs/dashboard` 无测试脚本（仅 build）。
- `bitfs-app` 在当前环境无法执行 Flutter 检查。

证据：

- `den-explorer/integration_test.go:18`
- `bitfs/dashboard/package.json:6`

---

## 5.4 Low

### L-01 CLI 明文参数存在操作风险

`--password`、`--rpc-pass` 作为命令行参数使用，会出现在 shell 历史与进程列表中。  
该问题非立即阻断，但建议在生产使用中默认改为交互输入或环境变量/安全文件。

证据：

- `bitfs/cmd/bitfs/cmd_wallet.go:223`
- `bitfs/cmd/bitfs/cmd_wallet.go:307`
- `bitfs/cmd/bitfs/cmd_daemon.go:54`
- `bitfs/cmd/bitfs/cmd_daemon.go:57`

---

## 6. 测试与质量基线（本次实测）

## 6.1 Go 测试

- `libbitfs-go`: 通过
- `bitfs`: 通过
- `metanet`: 通过
- `den-explorer`: 通过
- `git-remote-bitfs`: **失败（编译错误，见 B-01）**

## 6.2 覆盖率（`go test -cover` 摘要）

- `libbitfs-go`：多数核心包约 80%~97%
- `bitfs`：核心包约 80%~97%
- `metanet`：核心包约 79%~97%，CLI 包较低
- `den-explorer`：主包约 29.6%，`cmd/seed` 0%
- `git-remote-bitfs`：因编译失败无法形成完整覆盖率

## 6.3 静态与漏洞检查

- `go vet`：除 `git-remote-bitfs` 外通过
- `govulncheck`：除 `git-remote-bitfs`（受编译失败影响）外未发现漏洞

## 6.4 前端与客户端

- `bitfs/dashboard`: `npm run build` 通过
- `bitfs-extension`: `npm run build` 通过；`vitest` 全部为 todo（无有效测试）
- `bitfs-app`: 因环境缺少 Flutter，无法执行 `flutter analyze`

## 7. 发布门禁建议（必须项）

上线前至少满足以下条件：

1. 修复 `git-remote-bitfs` 枚举兼容问题，恢复可构建与可测。
2. 调整 daemon 安全默认值：
   - 默认要求管理端认证（`admin_token`）
   - 默认收紧 CORS（至少不为 `*`）
   - 提供安全配置入口（CLI 参数或配置文件加载路径）
3. 统一 dashboard 实现与文档：
   - 要么完成 embed + 路由集成
   - 要么文档明确“未集成/实验性”
4. 明确本次 release 边界：
   - 若 `bitfs-extension` / `bitfs-app` 非本次 GA，需在发布说明中标注为实验状态
5. 增加 CI 门禁（建议最小集）：
   - `go test ./...`
   - `go vet ./...`
   - `govulncheck ./...`
   - 前端 build
   - 客户端静态检查（含 Flutter 环境）

## 8. 最终发布判断

**当前判断：NO-GO。**  
建议先完成 7.1~7.3 的阻断修复，再进行一次回归审查与发布决策。

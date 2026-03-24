# Lessons Learned

跨会话积累的经验教训。每次犯错或发现有价值的模式后更新此文件。

## Workflow Patterns

- **Design-first**: 每个功能先写 `*-design.md`，再写实施任务。设计文档是最好的上下文传递方式
- **Speckit for new features**: 新功能开发前用 speckit 生成 spec，避免边做边改
- **Subagent parallelism**: 独立任务用 subagent 并行，显著提速（如 8 repo 审计修复）
- **Cross-repo changes**: 跨多 repo 变更 — 先统一设计，再逐 repo 实施+验证（HTLC 重构: 8 repo, 27 commits）
- **Audit after implement**: 每个子项目实施完立即审计，不要积压
- **Multi-round audit**: 第一轮审计修复后立即做第二轮 — 修复过程中会暴露新问题（第一轮 54 issues → 第二轮又发现 75 issues）。两轮审计 + 修复 = ~85 issues, 44 commits, 10 repos
- **Audit agent decomposition**: 按仓库/维度分 7 个并行 agent 审查，每个 agent 聚焦一个区域（Go code / TS code / docs / infra），比单 agent 全扫更彻底
- **Fix then review**: 代码修复走 implementer → spec reviewer → quality reviewer 三阶段。文档修复可以跳过 quality review，只做 spec review
- **Shared utility extraction**: 发现重复代码时（如 hexToBytes 8 处重复），先写 spec 确认 canonical source 和 error handling 行为差异，再统一。不要直接动手
- **`/clear` before implement**: 设计完成后清空上下文再开始实施，避免超长对话导致质量下降

## Common Gotchas

- **TxID byte ordering**: BSV TxID 显示格式 vs wire 格式是反的，跨语言时容易出错
- **go-sdk compat/bip32**: 包名是 `compat` 不是 `bip32`，必须别名导入
- **BSV OP_CLTV**: post-Genesis 把 0xb1 当 OP_NOP2，被 DISCOURAGE_UPGRADABLE_NOPS mempool policy 拒绝 — 用 nLockTime 替代
- **独立 repo 的 go.mod replace**: `bitfs/go.mod` 用 `replace => ../libbitfs-go`，改了 libbitfs-go 的 API 必须同步改 bitfs
- **libbitfs-ts linking**: bitfs-app 用 `"@bitfs/libbitfs": "file:../../../libbitfs-ts"`，路径层级取决于 worktree 位置
- **Change UTXO ScriptPubKey**: 构建交易时 change output 必须带 ScriptPubKey，否则后续交易引用时无法签名
- **`bun test` vs `bun run test`**: `bun test` 用 bun 原生 runner（不认识 vitest API 如 `vi.stubGlobal`），`bun run test` 走 package.json script 调用 vitest。审计时跑错命令会误报测试失败
- **Barrel import 拉入 Node.js 模块**: `import { hexToBytes } from '@bitfs/libbitfs'` 会通过 barrel 拉入 storage/config 等依赖 `node:path` 的包，导致浏览器构建失败。用 subpath import `@bitfs/libbitfs/util` 替代
- **tongxiaofeng → bitfsorg 残留**: 模块改名后要全文搜索所有 `.md` 和 `.go` 文件，不仅仅是 go.mod 和 CLAUDE.md。设计文档、spec、甚至注释中都可能有旧路径

## Critical Bugs Found

- **Vault Close() clobber**: `Close()` called `State.Save()` unconditionally, so daemon shutdown overwrote concurrent CLI state updates. Fix: Close() only saves wallet state; all node mutations persist via `withWriteLock`. Rule: **long-running processes must not blindly save state on exit — use reload-before-save or skip saving stale data**.
- **State mutations outside withWriteLock**: `Publish` mutated `State.PublishBindings` directly without saving, relying on Close(). Rule: **every state mutation must either go through withWriteLock or call Save() explicitly**.
- **Non-atomic state writes**: `os.WriteFile` 直接写状态文件（engine/vault 的 saveWalletState），crash 时会损坏。必须用 tmp+rename 原子写入模式（write to `.tmp`, then `os.Rename`）。审计发现 vault 层有此 pattern 但 engine 层没有
- **VerifyTx dedup race**: `sync.Map` + `sync.Once` 做去重时，`Delete` 不能放在 `once.Do` 内部 — 否则并发调用者在 Delete 之后、Do 返回之前会创建新 entry，破坏去重。`Delete` 必须在 `once.Do` 返回之后
- **ContentResolver hash mismatch**: 远程获取加密内容时，`SHA256(ciphertext) != SHA256(SHA256(plaintext))`，hash 验证永远失败。内容寻址 key 不是 wire data 的校验和 — AES-GCM 已提供完整性保证，transport 层不需要额外 hash 验证

## Anti-Patterns

- 不要在一个超长对话中同时设计+全部实施
- 不要跳过验证就标记完成 — 每个任务都要 `go test` / `bun test` 证明
- 不要为假设的未来需求设计 — 做当前需要的最简单方案（如批量交易替代增量编码）
- 不要在不了解代码的情况下提出修改建议 — 先 Read 再 Edit
- 方向错了不要硬修 — revert + 记 TODO，重新设计后再来
- 对话中的设计决策不要只停留在对话里 — 当场写入设计文档或 memory
- 设计文档改了实现但没同步文档 — HTLC 从 sCrypt multisig 改为 plain script P2PKH，设计文档和 spec 都没更新。**每次协议/架构变更后，必须同步更新: 设计文档、spec、CLAUDE.md、README**
- 文档里写 "合并挖矿" 但设计决策是 "独立挖矿" — 设计决策必须立即传播到所有描述性文档（README、CLAUDE.md、白皮书大纲），不能只改设计文档本身

## Tool Evolution Log

- 2026-02-12~16: 使用 OpenSpec 做规格管理
- 2026-02-17: 清理 OpenSpec，换成 speckit (spec-kit v0.1.0)
- 教训: 不迁就工具，不好用就换。但换之前要确保旧工具的产出已迁移

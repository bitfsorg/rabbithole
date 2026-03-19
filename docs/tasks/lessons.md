# Lessons Learned

跨会话积累的经验教训。每次犯错或发现有价值的模式后更新此文件。

## Workflow Patterns

- **Design-first**: 每个功能先写 `*-design.md`，再写实施任务。设计文档是最好的上下文传递方式
- **Speckit for new features**: 新功能开发前用 speckit 生成 spec，避免边做边改
- **Subagent parallelism**: 独立任务用 subagent 并行，显著提速（如 8 repo 审计修复）
- **Cross-repo changes**: 跨多 repo 变更 — 先统一设计，再逐 repo 实施+验证（HTLC 重构: 8 repo, 27 commits）
- **Audit after implement**: 每个子项目实施完立即审计，不要积压
- **`/clear` before implement**: 设计完成后清空上下文再开始实施，避免超长对话导致质量下降

## Common Gotchas

- **TxID byte ordering**: BSV TxID 显示格式 vs wire 格式是反的，跨语言时容易出错
- **go-sdk compat/bip32**: 包名是 `compat` 不是 `bip32`，必须别名导入
- **BSV OP_CLTV**: post-Genesis 把 0xb1 当 OP_NOP2，被 DISCOURAGE_UPGRADABLE_NOPS mempool policy 拒绝 — 用 nLockTime 替代
- **独立 repo 的 go.mod replace**: `bitfs/go.mod` 用 `replace => ../libbitfs-go`，改了 libbitfs-go 的 API 必须同步改 bitfs
- **libbitfs-ts linking**: bitfs-app 用 `"@bitfs/libbitfs": "file:../../../libbitfs-ts"`，路径层级取决于 worktree 位置
- **Change UTXO ScriptPubKey**: 构建交易时 change output 必须带 ScriptPubKey，否则后续交易引用时无法签名

## Anti-Patterns

- 不要在一个超长对话中同时设计+全部实施
- 不要跳过验证就标记完成 — 每个任务都要 `go test` / `bun test` 证明
- 不要为假设的未来需求设计 — 做当前需要的最简单方案（如批量交易替代增量编码）
- 不要在不了解代码的情况下提出修改建议 — 先 Read 再 Edit
- 方向错了不要硬修 — revert + 记 TODO，重新设计后再来
- 对话中的设计决策不要只停留在对话里 — 当场写入设计文档或 memory

## Tool Evolution Log

- 2026-02-12~16: 使用 OpenSpec 做规格管理
- 2026-02-17: 清理 OpenSpec，换成 speckit (spec-kit v0.1.0)
- 教训: 不迁就工具，不好用就换。但换之前要确保旧工具的产出已迁移

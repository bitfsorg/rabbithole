# 待办任务

## 遗留问题

### 1. ~~bitfs engine 集成 PRIVATE 元数据加密 API~~ (DONE)
- **来源**: P0 §3.2 修复后遗留
- **现状**: 已完成。添加 `serializePayloadForChain()` helper，PRIVATE 节点的 TLV 自动加密为 EncPayload
- **修改文件**: `bitfs/internal/engine/helpers.go`（新 helper）, `put.go`, `encrypt.go`, `copy.go`, `move.go`（4 处调用点）

### 2. ~~设计文档同步 — PRIVATE 元数据 salt 公式~~ (DONE)
- **来源**: P0 §3.2 修复后遗留
- **现状**: 已完成。所有设计文档和代码注释已更新为 `salt = random(16B)`
- **已更新文件**: `design/bitfs/5-TransactionSpec.zh.md`, `design/bitfs/3-DetailedDesign.zh.md`, `design/bitfs/2-SystemDesign.zh.md`, `libbitfs-go/metanet/node.go`, `bitfs/docs/spec/03-metanet.md`, `docs/audits/2026-02-27-architecture-review.md`

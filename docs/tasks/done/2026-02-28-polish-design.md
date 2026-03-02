# 底层打磨：测试覆盖率 + 协议定型 — 设计文档

**日期**: 2026-02-28
**目标**: 两个 phase — (1) 所有 Go 包测试覆盖率 >80%，(2) 审查并修补 13 个 spec 文件确保与代码一致。

## 现状

- **总测试数**: 2,028（1,106 libbitfs-go + 615 bitfs unit + 307 bitfs integration）
- **libbitfs-go 平均覆盖率**: ~88%，最低 75%（vault, network）
- **bitfs 覆盖率分布**: daemon 87%，publish 92%，但 cmd/bitfs 仅 35%
- **技术债**: 仅 1 个 TODO（metanet hard link validation）
- **TASKS.md**: 所有 60 个验收标准已通过

## Phase 1: 测试覆盖率 >80%

### 目标包及策略

| 包 | 当前 | 目标 | 估算新增测试 | 策略 |
|---|---|---|---|---|
| `cmd/bitfs` | 35% | >80% | ~50-80 | 各 cmd_*.go 子命令 table-driven 测试，mock vault/publish/DNS |
| `cmd/bmget` | 63% | >80% | ~10-15 | 批量下载边界场景、错误路径 |
| `internal/buy` | 64% | >80% | ~10-15 | 购买状态机错误路径、超时 |
| `cmd/bget` | 65% | >80% | ~10-15 | --version 参数、缓存命中/miss、大文件 |
| `cmd/bcat` | 67% | >80% | ~5-10 | 流式输出、编码边界、空文件 |
| `vault` (libbitfs) | 75% | >80% | ~10-15 | flock 竞争、Reload 边界、withWriteLock 错误路径 |
| `network` (libbitfs) | 75% | >80% | ~5-10 | RPC/SPV 客户端错误处理、超时 |

### 测试策略

- **cmd/bitfs**: 最大工作量。每个 cmd_*.go 文件对应一个 cmd_*_test.go。通过接口 mock vault 操作，不需要真实钱包/区块链。关注：参数验证、错误消息、输出格式。
- **b-tools (bget/bcat/bmget)**: 已有测试框架（mock HTTP client），补充边界场景即可。
- **vault/network**: 补充 error path 测试，flock 并发测试用 goroutine + channel 模式。

### 不做

- `dashboard/`: 嵌入式 SPA，无需 Go 测试
- 不追求 >90%，避免为了覆盖率写无意义测试
- 不改变现有测试架构

## Phase 2: Spec 审查 + 修补

### 审查范围

13 个 spec 文件（`bitfs/docs/spec/01-method42.md` 到 `13-network.md`），逐一对照代码：

1. **一致性**: spec 描述的 API、参数、返回值是否与代码一致
2. **完整性**: 是否有代码中存在但 spec 未记录的功能
3. **精确性**: 字节序、编码格式、边界值是否明确无歧义
4. **遗留项**: 修复 metanet hard link validation TODO

### 产出

- 审查报告（每个 spec 的 diff 摘要）
- 修补后的 spec 文件（就地更新）
- 代码修复（如果发现 spec 描述正确但代码实现有误）

### 不做

- 不写新的 Protocol Spec v1.0 文档
- 不改变现有 spec 格式或组织结构
- 不做设计文档的同步（那是独立任务）

## 执行顺序

1. Phase 1 先执行（稳固基础，让后续改动有测试保护）
2. Phase 2 后执行（有了充分测试后，修改更安全）
3. 两个 phase 各自独立可交付、可 commit

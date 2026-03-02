# git-remote-bitfs v0.1 完成设计

## 现状

代码基本完整 (~3,860 LOC + ~4,930 LOC 测试)，6 个内部包全部实现，但存在编译阻塞和功能缺口。

### 已完成

- Git remote helper 协议 (import/export/list)
- Fast-export/fast-import 流解析和生成
- Method 42 加密/解密 (Free + Private)
- Metanet DAG 读写 (anchor/reader/writer)
- Git SHA ↔ Metanet 映射 (git notes + path index)
- UTXO 原子状态管理
- URL 解析 + .bitfsattributes
- ~130+ 单元测试

### 阻塞问题

1. **B-01 编译失败** — `chain/anchor.go` 和 `chain/writer.go` 使用已不存在的 tx API 枚举名 (`BatchOpNodeUpdate` → `OpUpdate` 等)
2. **go.sum 缺失** — `golang.org/x/text/unicode/norm` 未记录（libbitfs-go/metanet 依赖）
3. **CLAUDE.md dust limit** — 第 64 行仍写 546 satoshis（应为 1 sat）

## v0.1 范围

### 在范围内

1. 修复编译阻塞 (B-01 + go.sum)
2. 全面代码审计 (安全性/正确性/设计一致性)
3. 审计修复
4. 增量 fetch 完善
5. 错误处理强化
6. Docker regtest e2e 测试
7. 文档更新 (设计文档恢复 Anchor 描述、CLAUDE.md 修正)

### 不在范围 (deferred to v0.2)

- Paid access 模式
- Symlinks / Submodules
- Repack 优化
- 重构为使用 libbitfs-go/vault

## 设计决策

### Anchor 节点保留

NodeType=ANCHOR (3) 在 libbitfs-go 中已完全实现且测试充分。设计文档需更新以反映这一决策。Anchor 节点的 P_node 作为分支身份，P2PKH 链提供 CAS 一致性保证。

### 增量 Fetch

- Export 时写入 git notes (`refs/notes/bitfs`) 记录每个 ref 的最新 anchor TxID
- Import 时用 notes 作为 stop point，避免重复下载
- 边界：首次 clone 无 notes → 全量；force push → notes 失效需全量重建

### E2E 测试方案

参考 bitfs/ 的 Docker regtest 模式：
- `e2e/` 目录，`-tags e2e` 构建标签
- docker-compose 启动 BSV regtest 节点
- 测试场景：
  - 初始 push (空远端)
  - Clone (完整下载 + 解密)
  - 增量 push + fetch (新 commit)
  - 分支操作 (创建/删除分支)
  - Merge commit (多 parent anchor)
  - Private 加密仓库 (Method 42)
  - 错误场景 (网络断开、余额不足)

### 错误处理强化

- Export 向 git 报告结构化错误 (不只是字符串)
- Import 处理链上数据损坏 (TLV 解析失败时优雅降级)
- UTXO 耗尽时的明确提示

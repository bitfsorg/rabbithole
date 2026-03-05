# Metanet 测试用例设计

> 本文档为 Metanet 设计文档体系的第四层：测试用例设计。
>
> **文档体系**:
> - [整体设计](../OverallDesign.zh.md) — 两产品生态、三层架构、界面划分
> - [概念设计](1-ConceptDesign.zh.md) — 产品定位、核心理念、设计原则
> - [系统设计](2-SystemDesign.zh.md) — 节点架构、合约、支付通道
> - [详细设计](3-DetailedDesign.zh.md) — 共识、挖矿、结算协议细节
> - **测试设计** (本文档) — 测试用例设计
>
> 设计章节交叉引用: 系统设计章节（如"第五节"）见 [2-SystemDesign](2-SystemDesign.zh.md)，详细设计章节（如"第一节"）见 [3-DetailedDesign](3-DetailedDesign.zh.md)。

---

## 设计原则

1. **三段式格式**: 每个测试用例采用"前置条件 → 操作 → 期望结果"
2. **标签分类**: `[unit]` 单元测试 | `[edge]` 边界条件 | `[integration]` 集成测试 | `[property]` 属性不变量 | `[security]` 安全性
3. **覆盖率目标**: 每个 package ≥80% 行覆盖率
4. **命名映射**: 规范 ID `T{N}.{M}.{K}` → 代码函数名 `Test{Category}_{Scenario}`
5. **独立性**: 每个测试可独立运行，无测试间依赖
6. **确定性**: 所有测试使用固定种子/mock，禁止随机行为
7. **表驱动**: Go 惯例，使用 `[]struct{ name string; ... }` + `t.Run()`
8. **断言框架**: `testify/require` (致命错误) 与 `testify/assert` (非致命错误) 区分使用

---

## 总览

| # | 类别 | 设计参照 | 代码文件 | 目标 |
|---|------|---------|---------|------|
| T1 | 存储合约 | 系统设计五, 详细设计一 | `metanet_chain/storage_deal_test.go` | 2 |
| T2 | 存储证明 | 系统设计五, 详细设计二 | `metanet_chain/storage_proof_test.go` | 5 |
| T3 | ECDH 双层加密 | 系统设计五, 详细设计三 | `metanet_chain/ecdh_test.go` | 4 |
| T4 | 支付与检索 | 系统设计七, 详细设计四-五 | `metanet_chain/payment_test.go` | 7 |
| T5 | ON 层 PoW 与热数据 | 系统设计二, 详细设计六 | `metanet_chain/mining_test.go` | 2 |
| | **合计** | | | **20** |

---

## T1. 存储合约

**设计参照**: 系统设计第五节, 详细设计第一节
**代码文件**: `metanet_chain/storage_deal_test.go`

| ID | 用例名称 | 前置条件 | 操作 | 期望结果 | 标签 |
|----|---------|---------|------|---------|------|
| T1.1 | 存储合约创建: N 个 UTXO 对应 N 期 | Owner 与 Metanet Node 协商完成 | 创建 StorageDeal 交易 | 合约交易包含 N 个输出, 各含 expected_proof_hash (预计算), 锁定 MNT Token | [unit] |
| T1.2 | 确定性挑战计算 | 已知 contract_txid | challenge_k = SHA256(contract_txid ‖ k) (k=0..N-1) | 挑战值确定性可复现, 不同 k 产生不同 challenge, 合约创建时即可预计算所有期 | [property] |

---

## T2. 存储证明

**设计参照**: 系统设计第五节, 详细设计第二节
**代码文件**: `metanet_chain/storage_proof_test.go`

| ID | 用例名称 | 前置条件 | 操作 | 期望结果 | 标签 |
|----|---------|---------|------|---------|------|
| T2.1 | Metanet Node 提交正确 Merkle proof | Metanet Node 持有正确数据 | 构建 StorageProof 交易 → Script 执行 | Script 验证通过 (chunk_data + merkle_siblings → merkle_root 匹配), Metanet Node 领取该期奖励 | [unit] |
| T2.2 | Metanet Node 提交错误 proof | Metanet Node 伪造数据 | 构建错误 StorageProof → Script 执行 | Script 验证失败, merkle_root 不匹配, UTXO 不可花费 | [security] |
| T2.3 | Metanet Node 提交其他 chunk 的 proof | Metanet Node 持有数据但提交错误 chunk | chunk_index 与挑战不匹配 | 验证失败, chunk_index 对应的 expected_proof_hash 不匹配 | [security] |
| T2.4 | 过期证明期提交 | 合约第 k 期已过期 | Metanet Node 在第 k+2 期提交第 k 期的 proof | UTXO 已过时间锁, 该期奖励归 Owner 回收 | [edge] |
| T2.5 | 边界 chunk index | 文件仅 1 个 chunk | challenge 指向 chunk_index=0 | Merkle proof 为空 (root = chunk hash), 验证通过 | [edge] |

---

## T3. ECDH 双层加密

**设计参照**: 系统设计第五节, 详细设计第三节
**代码文件**: `metanet_chain/ecdh_test.go`

| ID | 用例名称 | 前置条件 | 操作 | 期望结果 | 标签 |
|----|---------|---------|------|---------|------|
| T3.1 | ECDH 双层加密 | Owner 有加密文件, Metanet Node 已注册 | Owner ECDH 重加密 → Metanet Node 接收 | Provider 密文 != Owner 密文, Metanet Node 无法解密原始内容 (仅持有外层密钥), Owner 可通过两层密钥解密 | [unit] |
| T3.2 | 外层密钥独立性 | 同一文件重加密给不同 Metanet Node | Owner ECDH 重加密 → Node_A, 同一文件 → Node_B | Node_A 和 Node_B 的外层密文不同, 互相无法解密对方的副本 | [property] |
| T3.3 | Metanet Node 无法解密内层 | Metanet Node 持有外层密钥 | Metanet Node 尝试用外层密钥解密原始内容 | 解密失败, 仅能解密外层得到 Owner 密文, 无法进一步解密 | [security] |
| T3.4 | 错误 Node 密钥拒绝 | Node_A 的密钥 | 尝试解密发给 Node_B 的重加密数据 | 外层解密失败, AES-GCM tag 校验不通过 | [security] |

---

## T4. 支付与检索

**设计参照**: 系统设计第七节, 详细设计第四-五节
**代码文件**: `metanet_chain/payment_test.go`

| ID | 用例名称 | 前置条件 | 操作 | 期望结果 | 标签 |
|----|---------|---------|------|---------|------|
| T4.1 | x402 检索支付 | Metanet Node 持有数据, User 请求下载 | User 支付 BSV → Metanet Node 返回数据 | Metanet Node 正确返回解密后的数据, BSV 支付交易有效 | [integration] |
| T4.2 | Token 支付通道: 开启 | Owner 与 Metanet Node 协商 | 创建 2-of-2 多签 funding 交易 | funding 交易正确创建, 双方各持一份签名 | [unit] |
| T4.3 | Token 支付通道: 更新 | 通道已开启 | 双方签署新状态 | sequence_number 递增, 新余额分配正确, 旧状态作废 | [unit] |
| T4.4 | Token 支付通道: 正常关闭 | 通道有多次状态更新 | 双方协商关闭 | 最终余额按最新状态正确分配, 无需等待时间锁 | [unit] |
| T4.5 | 旧状态惩罚 | 通道有多次更新, 一方持有旧 Commitment TX | 恶意方广播旧版本 (sequence_number < latest) | 另一方在 dispute_window 内用 revocation_key 提交惩罚交易, 恶意方损失全部通道余额 | [security] |
| T4.6 | 通道容量耗尽 | 通道余额接近 0 | 继续请求数据 | 返回 402 + X-Channel-Balance: 0 + X-Channel-TopUp-Required: true, 提示开新通道 | [edge] |
| T4.7 | Node↔Node MNT 批发通道 | 两个 Metanet Node | Node_A 向 Node_B 开 MNT 通道, 批量购买热门数据 | 通道在 Metanet Overlay 上创建, chunk 级别更新余额, 结算后双方 MNT 余额正确 | [integration] |

---

## T5. ON 层 PoW 与热数据

**设计参照**: 系统设计第二节, 详细设计第六节
**代码文件**: `metanet_chain/mining_test.go`

| ID | 用例名称 | 前置条件 | 操作 | 期望结果 | 标签 |
|----|---------|---------|------|---------|------|
| T5.1 | ON 层 PoW 出块（非合并） | Metanet Miner 参与 | 矿工打包 ML Block 并封装为合法 BSV 交易 | ON 节点验证 SHA256 PoW 与区块头，BSV 确认后区块生效 | [integration] |
| T5.2 | 热数据 CDN: Metanet Node 缓存热门内容后服务 x402 请求 | Metanet Node 自愿缓存热门文件 | User 通过 x402 请求数据 | Metanet Node 正确返回数据并收取 BSV 费用, 不需要存储合约 | [integration] |

---

## 交叉引用索引

| 测试类别 | 系统设计章节 | 详细设计章节 | 代码路径 |
|----------|------------|------------|---------|
| T1 存储合约 | 五 (冷数据: Archive 合约模式) | 一 (存储合约 Bitcoin Script) | `metanet_chain/storage_deal_test.go` |
| T2 存储证明 | 五 (冷数据: Archive 合约模式) | 二 (存储证明 Bitcoin Script) | `metanet_chain/storage_proof_test.go` |
| T3 ECDH 双层加密 | 五 (冷数据: Archive 合约模式) | 三 (ECDH 双层加密流程) | `metanet_chain/ecdh_test.go` |
| T4 支付与检索 | 七 (x402 支付通道) | 四-五 (支付通道协议, HTTP 扩展) | `metanet_chain/payment_test.go` |
| T5 ON 层 PoW 与热数据 | 二 (Metanet Overlay 基本设计) | 六 (ML Block 结构与挖矿) | `metanet_chain/mining_test.go` |

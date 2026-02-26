# Atomic Transaction Builder Design

Date: 2026-02-27

## Problem

当前 BitFS 的文件系统操作（mkdir, mv, cp, rm）需要多笔独立交易。例如跨目录 mv 需要 4 笔交易，存在中间状态不一致的风险。UTXO 模型天然支持在一笔交易中包含多个 input/output，应利用这一特性实现操作原子性。

## Design Decisions

| 决策 | 结论 |
|------|------|
| 改造层级 | tx 层重构 + Engine 适配 |
| 旧单模板 API | 废弃，MutationBatch 成为唯一构建路径 |
| DataTx | 保持独立，不合并到 Batch |
| DELETE 标记 | 保留在 Batch 中，写 moved_to 指针 |

## Architecture

### 1. tx 层重构

#### 废弃的 API

- `BuildCreateRoot` / `BuildUnsignedCreateRootTx` / `CreateRootParams`
- `BuildCreateChild` / `BuildUnsignedCreateChildTx` / `CreateChildParams`
- `BuildSelfUpdate` / `BuildUnsignedSelfUpdateTx` / `SelfUpdateParams`

#### 保留的 API

- `BuildDataTransaction` / `DataTxParams` — 数据上链独立交易
- `SignMetanetTx` — 签名（适配 BatchResult）
- `EstimateTxSize` / `EstimateFee` — 费用估算
- `ParseOPReturnData` / `ParseTxNodeOps` — 解析（已支持多 OP_RETURN）

#### 扩展 MutationBatch

```go
type BatchOpType int
const (
    OpCreate     BatchOpType = iota // 新节点：OP_RETURN + P2PKH refresh
    OpUpdate                        // 更新节点：OP_RETURN + P2PKH refresh
    OpDelete                        // 删除节点：OP_RETURN only，无 refresh（UTXO 回收）
    OpCreateRoot                    // 根节点创建：无 parent input，OP_RETURN + P2PKH refresh
)

type BatchNodeOp struct {
    Type       BatchOpType
    PubKey     *ec.PublicKey     // 节点公钥
    PrivateKey *ec.PrivateKey    // 签名用（OpCreate 不需要自身 key，parent 签 input）
    ParentTxID []byte            // OP_RETURN field 2（root 为空）
    Payload    []byte            // TLV payload
    InputUTXO  *UTXO             // 要花费的 UTXO（OpCreateRoot 为 nil）
}
```

#### Output 布局规则

每个 op 按序产出：
1. OP_RETURN（0 sats）：`OP_FALSE OP_RETURN <MetaFlag> <P_node> <ParentTxID> <Payload>`
2. P2PKH dust（1 sat）：仅非 DELETE op，锁定到 P_node

末尾：P2PKH change output（如有剩余）

DELETE op 不产出 P2PKH，其 input UTXO 金额回收为手续费。

#### Input 排列

```
[op0.InputUTXO, op1.InputUTXO, ..., opN.InputUTXO, fee0, fee1, ...]
```

只有 InputUTXO 非 nil 的 op 贡献 input。OpCreateRoot 无 input。

#### Parent 去重

同一 parent 被多个 OpCreate 引用时：
- Parent UTXO 只作为一个 input 花费一次
- Parent 的 refresh output 由对应的 OpUpdate op 产出
- Build 时自动检测并合并同 parent 的 input

#### 签名

`SignBatchTx(result *BatchResult, ops []BatchNodeOp, feeInputs []*UTXO) (string, error)`

从 ops（按 input 顺序）+ feeInputs 构建签名 UTXO 数组，调用 go-sdk 签名。

### 2. Engine 层适配

所有文件系统操作统一为：

```
① 收集 []BatchNodeOp（从文件系统语义映射）
② 收集 fee inputs
③ batch.Build() → BatchResult（unsigned）
④ SignBatchTx() → signed hex
⑤ 原子更新 local state
```

#### 操作映射表

| 操作 | Batch Ops | Inputs |
|------|-----------|--------|
| init (create root) | OpCreateRoot | fee |
| mkdir | OpCreate(child) + OpUpdate(parent) | parent + fee |
| put | OpCreate(child) + OpUpdate(parent) | parent + fee |
| rm file | OpDelete(child) + OpUpdate(parent) | child + parent + fee |
| rm -r dir | OpDelete(每个后代) + OpUpdate(parent) | 所有后代 + parent + fee |
| mv 同目录 | OpUpdate(parent) | parent + fee |
| mv 跨目录 | OpCreate(dst) + OpDelete(src) + OpUpdate(src_parent) + OpUpdate(dst_parent) | src + src_parent + dst_parent + fee |
| cp | OpCreate(child) + OpUpdate(dst_parent) | dst_parent + fee |
| link | OpCreate(link) + OpUpdate(parent) | parent + fee |

#### 错误处理

保持 defer-based rollback：

```go
success := false
defer func() {
    if !success {
        for _, us := range spentUTXOs {
            us.Spent = false
        }
    }
}()

ops := collectOps(...)
result, err := batch.Build()
if err != nil { return err }

hex, err := SignBatchTx(result, ops, feeInputs)
if err != nil { return err }

success = true
updateLocalState(result)
```

### 3. 链上兼容性

- 链上格式不变：每个 op 仍然是标准 Metanet OP_RETURN
- 现有交易可被新解析器读取（`ParseTxNodeOps` 已支持多 OP_RETURN）
- 单操作等价于单 op Batch，输出结构与旧模板一致

### 4. 跨目录 mv 交易结构示例

```
Inputs:
  [0] src_node UTXO        (花费，不刷新 → 节点死亡)
  [1] src_parent UTXO       (花费，刷新)
  [2] dst_parent UTXO       (花费，刷新)
  [3..N] fee UTXO(s)

Outputs:
  [0] OP_RETURN: CREATE dst_child
  [1] P2PKH → P_dst_child (1 sat)
  [2] OP_RETURN: DELETE src_node (payload 含 moved_to 指针)
  [3] OP_RETURN: UPDATE src_parent (children 已移除 src_child)
  [4] P2PKH → P_src_parent (1 sat)
  [5] OP_RETURN: UPDATE dst_parent (children 已添加 dst_child)
  [6] P2PKH → P_dst_parent (1 sat)
  [7] P2PKH → change
```

4 个 op，1 笔交易，完全原子。

## Migration Strategy

1. **Phase 1: 扩展 tx/batch.go** — OpCreateRoot、DELETE 无 refresh、parent 去重、SignBatchTx
2. **Phase 2: Engine 逐操作迁移** — 每个命令独立切换到 Batch
3. **Phase 3: 清理** — 删除旧 Build*/Params API，更新测试

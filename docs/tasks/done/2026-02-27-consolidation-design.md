# 协议定型 + 库成熟度 + 测试覆盖 — 设计文档

**日期**: 2026-02-27
**目标**: 三线并行推进，使 BitFS 底层达到可供第三方实现的定型状态

## 背景

经过全面评估，当前状态：
- **协议 spec**: ~65-70% 完成，5 个 CRITICAL 缺口
- **libbitfs-go**: 10/11 包生产就绪，revshare 测试不足（12 个）
- **b*/shell 测试**: 1,817 个测试总量良好，但 shell 22 个命令仅 2 个测试

## 三条工作流

### A. 协议定型（Spec 补全）

完整定型所有 spec，包括已实现和未来功能。

#### A1. 新建 spec 12-revshare.md [CRITICAL]
- RevShareEntry/RegistryState/ShareData/ISOPoolState 类型定义
- 二进制序列化格式（固定大小、大端序）
- DistributeRevenue 算法（余数归末位）
- ISO（Initial Share Offering）完整流程设计
- 份额守恒验证规则

#### A2. 新建 spec 13-network.md [MEDIUM]
- BlockchainService 接口（Broadcast, GetUTXOs, GetTx, etc.）
- RPCClient 实现（JSON-RPC over HTTP）
- SPVClient 实现（桥接 spv 包）
- 网络预设配置（mainnet, testnet, regtest）

#### A3. 大幅更新 03-metanet.md [CRITICAL]
- 补齐 11 个扩展 Node 字段（VersionLog, ShareList, ChunkIndex, TotalChunks, RecombinationHash, RabinSignature, RabinPubKey, RegistryTxID, RegistryVout, ISO, ACLRef）
- NodeTypeAnchor (type=3) 及其 8 个专属字段（TreeRootPNode, TreeRootTxID, ParentAnchorTxID, Author, CommitMessage, GitCommitSHA, FileMode）
- 完整 TLV Tag 参考表（所有 tag ID + 类型 + 描述）
- ISOConfig 结构设计（TotalShares, PricePerShare, CreatorAddr, Status）
- ACL 设计（组公钥哈希 or ACL TxID 引用）

#### A4. 更新 01-method42.md [CRITICAL]
- Rabin 签名方案：Blum 素数生成、CRT 签名、SHA256 填充、验证算法
- 密钥序列化格式
- 与 Node.RabinSignature/RabinPubKey 的关联

#### A5. 更新 06-storage.md [HIGH]
- Compress/Decompress API（LZW=1, GZIP=2, ZSTD=3, None=0）
- SplitIntoChunks/RecombineChunks API（1MB 默认）
- ComputeRecombinationHash（SHA256(chunk₀ ‖ chunk₁ ‖ ...)）
- ZSTD 标注为 [PLANNED]

#### A6. 更新 10-cmd-bitfs.md [MEDIUM]
- `sales [path]` 命令
- Shell 命令完整列表

#### A7. 更新 11-cmd-btools.md [MEDIUM]
- `bstat --versions` 版本历史
- `bget --version N` 指定版本下载
- `--no-cache`/`--offline` flags
- `bls --keyword` 搜索

#### A8. 统一 Access 类型 [LOW]
- 所有 spec 使用 `AccessLevel` 枚举（AccessPrivate=0, AccessFree=1, AccessPaid=2）

### B. libbitfs-go 成熟度

#### B1. revshare 测试扩充（12 → 60+ 个）
- 序列化往返：所有类型空值/最大值/截断数据
- 收入分配边界：零总额、单股东、1000+ 股东、余数、溢出
- 份额验证：总和守恒、非法比例、零份额
- ISO 池状态：状态转换、购买/退款、池满/池空
- 二进制格式：字段边界、版本兼容

#### B2. 裸 return err 修复
- 14 处 bare `return err` 补充 context wrapping（主要在 spv/boltstore.go）

### C. Shell 集成测试

在 `bitfs/integration/shell_commands_test.go` 新建，约 25-30 个测试函数。

#### C1. 导航命令测试
- cd（正常/不存在路径/根目录）
- pwd
- lcd（本地目录切换）
- ls（空目录/有内容/递归）

#### C2. 文件操作测试
- mkdir（正常/已存在/嵌套）
- put（Free/Private/Paid 三种模式）
- get（正常下载/指定版本）
- cat（stdout 输出/二进制文件）
- rm（文件/目录/-r 递归）

#### C3. 高级文件操作测试
- cp（同目录/跨目录）
- mv（同目录/跨目录）
- link（软链接/硬链接/flags）

#### C4. 访问控制测试
- encrypt（PRIVATE→FREE 模式转换）
- decrypt（反向）
- sell（单文件/--recursive）
- publish/unpublish

#### C5. 批量与查询测试
- mget/mput
- sales 查询
- help 输出

#### C6. 错误路径测试
- 不存在的路径
- 无效参数（缺少必需参数、多余参数）
- 权限相关错误

## 并行策略

三条工作流完全独立，可用 subagent 并行：
- **Agent A**: Spec 文件更新（只读代码 + 写 spec）
- **Agent B**: revshare 测试 + error wrapping（libbitfs-go 目录）
- **Agent C**: Shell 集成测试（bitfs/integration/ 目录）

## 完成标准

- [x] 所有 13 个 spec 文件无 TODO/TBD，与代码一致
- [x] revshare 包 60+ 测试，全部通过
- [x] Shell 22 个命令全部有集成测试覆盖
- [x] `go test ./...` 和 `go test -tags=integration ./integration/` 全部通过
- [x] golangci-lint 零新增 warning

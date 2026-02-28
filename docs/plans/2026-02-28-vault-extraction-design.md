# Vault 包提取设计

将 `bitfs/internal/engine/` 提取为 `libbitfs-go/vault/`，统一状态管理，消除重复实现，为 git-remote-bitfs 等外部消费者铺路。

## 背景

当前 `bitfs/internal/engine/` 是 internal 包，只有 bitfs 自己能用。`git-remote-bitfs` 被迫独立实现了一套 chain/utxo/config，与 engine 操作同一个 wallet，存在双花风险和状态不一致。

提取到 libbitfs-go 后，所有 Go 消费者（bitfs, git-remote-bitfs, den, bitfs-app FFI）共享同一个 vault 实现。

## 设计决策

| 决策 | 结论 | 理由 |
|------|------|------|
| 包名 | `vault` | BitFS 核心领域概念，`vault.Put()` 语义自然 |
| 位置 | `libbitfs-go/vault/` | 与 method42, wallet, tx 等包同级 |
| 并发控制 | 文件锁 (flock) | 简单可靠，无需 daemon 运行 |
| 锁粒度 | 整个 vault | 写操作都涉及 UTXO 分配，必须串行 |
| 读操作 | 不加锁 | Cat/Get 只读 storage + 解密，无状态修改 |
| Publish/Unpublish | 移到 `bitfs/internal/publish/` | DNS 是应用层关注点，vault 不应知道 DNS |
| Daemon adapter | 删除 | daemon 直接 import vault，无需 adapter 间接层 |
| mget/mput | 移到 CLI 命令层 | 批量操作是应用层便利函数，循环调用 vault 方法 |
| buyer/ | 改名为 `buy/` | 更简洁 |
| git-remote-bitfs | 本次不改 | 后续单独重构，改用 vault 包 |

## Vault 公共 API

```go
package vault

type Vault struct {
    Wallet   *wallet.Wallet
    WState   *wallet.WalletState
    Store    *storage.FileStore
    Resolver *storage.ContentResolver
    State    *LocalState
    DataDir  string
    DNS      DNSResolver              // 链上操作（paymail 发现、节点寻址）需要
    Chain    network.BlockchainService // nil = offline
    SPV      *network.SPVClient
    SPVStore *spv.BoltStore
}

// 生命周期
func New(dataDir, password string) (*Vault, error)
func (v *Vault) Close() error
func (v *Vault) InitSPV() error

// 文件系统写操作（每个返回 Result，写操作自动加文件锁）
func (v *Vault) PutFile(opts PutOpts) (*Result, error)
func (v *Vault) Mkdir(opts MkdirOpts) (*Result, error)
func (v *Vault) Copy(opts CopyOpts) (*Result, error)
func (v *Vault) Move(opts MoveOpts) (*Result, error)
func (v *Vault) Remove(opts RemoveOpts) (*Result, error)
func (v *Vault) Link(opts LinkOpts) (*Result, error)
func (v *Vault) Sell(opts SellOpts) (*Result, error)
func (v *Vault) EncryptNode(opts EncryptOpts) (*Result, error)
func (v *Vault) DecryptNode(opts DecryptOpts) (*Result, error)

// 文件系统读操作（不加锁）
func (v *Vault) Cat(opts CatOpts) (io.Reader, *FileInfo, error)
func (v *Vault) Get(opts GetOpts) error

// 状态查询
func (v *Vault) IsOnline() bool
func (v *Vault) EnsureRootExists(vaultIndex int) (*Result, error)
func (v *Vault) ResolveParentNode(path string, vaultIndex int) (*NodeState, error)
func (v *Vault) ResolveVaultIndex(path string) (int, error)

// UTXO 管理
func (v *Vault) AllocateFeeUTXO() (*tx.UTXO, error)
func (v *Vault) RefreshFeeUTXOs() error
func (v *Vault) BroadcastTx(rawTx string) (string, error)
func (v *Vault) VerifyTx(txID string) (*network.VerifyResult, error)
func (v *Vault) DeriveChangeAddr() (string, error)
```

## 文件锁机制

```
锁文件: {DataDir}/vault.lock

写操作流程:
1. flock(vault.lock, LOCK_EX)    ← 排他锁
2. Load latest state             ← 防止读过期数据
3. 构建交易 + 广播
4. Save updated state
5. funlock(vault.lock)           ← 释放
```

所有写操作方法内部自动加锁，调用者无需关心。

## 重构后的 bitfs 结构

```
bitfs/
├── cmd/bitfs/              ← CLI 命令，直接 import vault
│   ├── cmd_put.go          ← 调用 vault.PutFile()
│   ├── cmd_mput.go         ← 循环调用 vault.PutFile()（从 engine 移来）
│   ├── cmd_mget.go         ← 循环调用 vault.Get()（从 engine 移来）
│   └── ...
├── cmd/b*/                 ← 只读工具，不变
├── internal/
│   ├── daemon/             ← 直接 import vault（删除 5 个 adapter）
│   ├── publish/            ← Publish/Unpublish + DNS 验证
│   ├── client/             ← b* tools HTTP 客户端，不变
│   └── buy/                ← b* tools 支付流程 + 错误处理（原 buyer/）
├── integration/            ← 改 import 路径
└── e2e/                    ← 不变

bitfs/internal/engine/      ← 整个删除
```

## libbitfs-go 新增包

```
libbitfs-go/
├── vault/                  ← 新包（26 个导出方法，~30 个文件）
│   ├── vault.go            ← Vault 结构体、New、Close、InitSPV
│   ├── state.go            ← LocalState、NodeState、UTXOState + 文件锁
│   ├── result.go           ← Result + 各 Opts 结构体
│   ├── put.go              ← PutFile
│   ├── mkdir.go            ← Mkdir
│   ├── copy.go             ← Copy
│   ├── move.go             ← Move
│   ├── remove.go           ← Remove
│   ├── link.go             ← Link
│   ├── sell.go             ← Sell
│   ├── encrypt.go          ← EncryptNode
│   ├── decrypt.go          ← DecryptNode
│   ├── cat.go              ← Cat
│   ├── get.go              ← Get
│   ├── helpers.go          ← 内部工具函数
│   ├── dir_update.go       ← 父目录更新逻辑
│   └── txbuild.go          ← MutationBatch 包装
├── method42, wallet, tx, metanet ...  ← 现有包不变
```

## 迁移策略

1. 在 libbitfs-go 创建 vault/ 包，从 engine 复制代码，改包名和 import
2. 添加文件锁机制到 state.go
3. 修改 bitfs — CLI/daemon/shell 全部改用 vault 包
4. 创建 `bitfs/internal/publish/` 包，从 engine 移入 Publish/Unpublish
5. 将 buyer/ 改名为 buy/
6. 将 mget/mput 移到 cmd/bitfs/
7. 删除 bitfs/internal/engine/
8. 所有测试通过（unit + integration + e2e）

## 后续工作（不在本次范围）

- git-remote-bitfs 改用 libbitfs-go/vault，删除其 chain/utxo/config 重复代码
- Paymail `a9f510c16bde` Verify Public Key 端点实现
- 设计文档 L2/L3 更新 git 端点说明（改为"git-remote-bitfs 通过 vault 包操作"）

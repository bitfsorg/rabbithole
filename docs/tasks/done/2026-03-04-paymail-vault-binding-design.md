# Paymail-Vault Binding 详细设计

> 日期: 2026-03-04
> 状态: 已完成（实现与测试已落地）
> 对应设计文档: 详细设计 §十六-B 的扩展（新增 §G. Multi-tenant Paymail-Vault 绑定）

## 一、背景与动机

BitFS daemon 已实现 Paymail 协议的标准端点（PKI、Public Profile、Verify PubKey），
以及 BitFS 自定义 BRFC 扩展（Browse/Buy/Sell）。当前 daemon 通过 `WalletService.GetVaultPubKey(alias)` 接口查找 alias 对应的公钥，实现上是 alias = vault 名称的直接映射。

**问题**: 当前模型隐式暴露所有 vault，无法控制哪些身份对外可见，也无法为同一 vault 设置不同的 paymail alias。

**目标**: 引入显式 paymail binding 机制，使 daemon 成为多身份（multi-tenant）Paymail 服务器。一个 daemon 实例可以通过 Paymail 协议暴露多个独立身份，每个身份显式绑定到一个 vault。

### 不涉及应用层语义

本设计是协议/平台层功能。绑定的身份可能是人、AI Agent、组织、服务——
这些是应用层关心的事，协议层只提供 vault ↔ alias 映射机制。

## 二、数据模型

### A. PaymailBinding 结构体

在 `libbitfs-go/wallet/` 包中新增：

```go
// PaymailBinding maps a Paymail alias to a vault.
// Bindings are explicit — only bound vaults are visible via Paymail.
type PaymailBinding struct {
    Alias string `json:"alias"` // Paymail local-part (e.g., "researcher")
    Vault string `json:"vault"` // Target vault name
}
```

### B. WalletState 扩展

在现有 `WalletState` 中新增 `PaymailBindings` 字段：

```go
type WalletState struct {
    NextReceiveIndex uint32           `json:"next_receive_index"`
    NextChangeIndex  uint32           `json:"next_change_index"`
    Vaults           []Vault          `json:"vaults"`
    NextVaultIndex   uint32           `json:"next_vault_index"`
    PaymailBindings  []PaymailBinding `json:"paymail_bindings,omitempty"` // 新增
}
```

序列化后的 JSON 示例：

```json
{
  "next_receive_index": 0,
  "next_change_index": 42,
  "vaults": [
    {"name": "default",    "account_index": 0, "root_txid": null, "deleted": false},
    {"name": "researcher", "account_index": 1, "root_txid": null, "deleted": false},
    {"name": "photos",     "account_index": 2, "root_txid": null, "deleted": false}
  ],
  "next_vault_index": 3,
  "paymail_bindings": [
    {"alias": "alex",       "vault": "default"},
    {"alias": "researcher", "vault": "researcher"}
  ]
}
```

### C. 约束与不变量

| 约束 | 说明 |
|------|------|
| alias 唯一 | 同一 WalletState 内不可有重复 alias |
| vault 存在 | binding 引用的 vault 必须存在且未被删除 |
| alias 格式 | `^[a-z0-9][a-z0-9._-]{0,63}$`（首字符字母或数字，最长 64） |
| 一对一 | 一个 vault 最多绑定一个 alias；一个 alias 绑定恰好一个 vault |
| 不上链 | 绑定存储在本地 WalletState JSON 文件中，不写入区块链 |

不上链的理由：
- 绑定是运营层配置，不是文件系统数据
- 避免每次添加/删除用户都广播交易
- 私钥可从 seed 恢复，alias 绑定丢失后重新配置即可

### D. Validate 扩展

`WalletState.Validate()` 增加 paymail binding 校验：

```go
func (ws *WalletState) Validate() error {
    // ... 现有 vault 校验 ...

    // Paymail binding 校验
    seenAlias := make(map[string]bool)
    seenVault := make(map[string]bool)
    activeVaults := make(map[string]bool)
    for _, v := range ws.Vaults {
        if !v.Deleted {
            activeVaults[v.Name] = true
        }
    }

    for _, b := range ws.PaymailBindings {
        // 1. alias 格式校验
        if !isValidAlias(b.Alias) {
            return fmt.Errorf("paymail binding: invalid alias %q", b.Alias)
        }
        // 2. alias 唯一性
        if seenAlias[b.Alias] {
            return fmt.Errorf("paymail binding: duplicate alias %q", b.Alias)
        }
        seenAlias[b.Alias] = true
        // 3. vault 唯一性（一个 vault 最多一个 alias）
        if seenVault[b.Vault] {
            return fmt.Errorf("paymail binding: vault %q bound to multiple aliases", b.Vault)
        }
        seenVault[b.Vault] = true
        // 4. vault 存在性
        if !activeVaults[b.Vault] {
            return fmt.Errorf("paymail binding: vault %q not found or deleted", b.Vault)
        }
    }
    return nil
}
```

### E. Alias 格式校验算法

```go
import "regexp"

var aliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func isValidAlias(alias string) bool {
    return aliasPattern.MatchString(alias)
}
```

规则说明：
- 首字符：小写字母或数字（防止 `-` 或 `.` 开头的歧义）
- 后续字符：小写字母、数字、`.`、`_`、`-`
- 长度：1-64 字符
- 不允许大写字母（Paymail 规范中 alias 不区分大小写，统一小写存储）
- 不允许 `@`、`/`、空格等特殊字符（防止注入和路径穿越）

### F. 错误定义

在 `libbitfs-go/wallet/errors.go` 新增：

```go
var (
    // ErrAliasExists indicates the paymail alias is already bound.
    ErrAliasExists = errors.New("wallet: paymail alias already exists")

    // ErrAliasNotFound indicates the paymail alias is not bound.
    ErrAliasNotFound = errors.New("wallet: paymail alias not found")

    // ErrInvalidAlias indicates the alias format is invalid.
    ErrInvalidAlias = errors.New("wallet: invalid paymail alias format")

    // ErrVaultAlreadyBound indicates the vault already has a paymail binding.
    ErrVaultAlreadyBound = errors.New("wallet: vault already has a paymail binding")
)
```

## 三、Wallet API

### A. Binding 管理方法

在 `libbitfs-go/wallet/vault.go` 中新增：

```go
// BindPaymail creates a paymail alias binding for a vault.
// Returns ErrAliasExists if alias is taken, ErrVaultNotFound if vault doesn't exist,
// ErrVaultAlreadyBound if vault already has a binding, ErrInvalidAlias if format invalid.
func (w *Wallet) BindPaymail(state *WalletState, alias, vaultName string) error {
    // 1. 校验 alias 格式
    if !isValidAlias(alias) {
        return fmt.Errorf("%w: %q", ErrInvalidAlias, alias)
    }

    // 2. 校验 vault 存在
    vault, err := w.GetVault(state, vaultName)
    if err != nil {
        return err
    }
    _ = vault // vault 存在即可

    // 3. 校验 alias 唯一
    for _, b := range state.PaymailBindings {
        if b.Alias == alias {
            return fmt.Errorf("%w: %q", ErrAliasExists, alias)
        }
        if b.Vault == vaultName {
            return fmt.Errorf("%w: %q already bound as %q", ErrVaultAlreadyBound, vaultName, b.Alias)
        }
    }

    // 4. 追加绑定
    state.PaymailBindings = append(state.PaymailBindings, PaymailBinding{
        Alias: alias,
        Vault: vaultName,
    })
    return nil
}

// UnbindPaymail removes a paymail alias binding.
// Returns ErrAliasNotFound if alias is not bound.
func (w *Wallet) UnbindPaymail(state *WalletState, alias string) error {
    for i, b := range state.PaymailBindings {
        if b.Alias == alias {
            state.PaymailBindings = append(
                state.PaymailBindings[:i],
                state.PaymailBindings[i+1:]...,
            )
            return nil
        }
    }
    return fmt.Errorf("%w: %q", ErrAliasNotFound, alias)
}

// ListPaymailBindings returns all active paymail bindings.
func (w *Wallet) ListPaymailBindings(state *WalletState) []PaymailBinding {
    if len(state.PaymailBindings) == 0 {
        return nil
    }
    result := make([]PaymailBinding, len(state.PaymailBindings))
    copy(result, state.PaymailBindings)
    return result
}

// ResolvePaymailAlias looks up a vault name by paymail alias.
// Returns the vault name, or ErrAliasNotFound.
func (w *Wallet) ResolvePaymailAlias(state *WalletState, alias string) (string, error) {
    for _, b := range state.PaymailBindings {
        if b.Alias == alias {
            return b.Vault, nil
        }
    }
    return "", fmt.Errorf("%w: %q", ErrAliasNotFound, alias)
}
```

### B. GetVaultPubKey 修改

当前 `GetVaultPubKey` 由 daemon 的 `WalletService` 接口调用。修改其实现以查询 paymail binding：

```go
// GetVaultPubKey resolves a paymail alias to its vault's compressed hex public key.
// 查找路径: alias → PaymailBindings → vault name → account_index → DeriveVaultRootKey
func (adapter *WalletAdapter) GetVaultPubKey(alias string) (string, error) {
    // 1. 通过 binding 查找 vault 名称
    vaultName, err := adapter.wallet.ResolvePaymailAlias(adapter.state, alias)
    if err != nil {
        return "", err
    }

    // 2. 查找 vault → account_index
    vault, err := adapter.wallet.GetVault(adapter.state, vaultName)
    if err != nil {
        return "", err
    }

    // 3. 派生公钥
    kp, err := adapter.wallet.DeriveVaultRootKey(vault.AccountIndex)
    if err != nil {
        return "", err
    }

    return hex.EncodeToString(kp.PublicKey.SerialiseCompressed()), nil
}
```

**关键变更**: 原来 alias 直接等于 vault name，现在经过 PaymailBindings 间接层。

### C. Vault 删除时的联动

`DeleteVault` 应同时清理关联的 paymail binding：

```go
func (w *Wallet) DeleteVault(state *WalletState, name string) error {
    for i := range state.Vaults {
        if state.Vaults[i].Name == name && !state.Vaults[i].Deleted {
            state.Vaults[i].Deleted = true
            // 清理关联的 paymail binding
            for j, b := range state.PaymailBindings {
                if b.Vault == name {
                    state.PaymailBindings = append(
                        state.PaymailBindings[:j],
                        state.PaymailBindings[j+1:]...,
                    )
                    break
                }
            }
            return nil
        }
    }
    return fmt.Errorf("%w: %q", ErrVaultNotFound, name)
}
```

### D. Vault 重命名时的联动

`RenameVault` 应同步更新 paymail binding 中的 vault 引用：

```go
func (w *Wallet) RenameVault(state *WalletState, oldName, newName string) error {
    // ... 现有检查 ...
    for i := range state.Vaults {
        if state.Vaults[i].Name == oldName && !state.Vaults[i].Deleted {
            state.Vaults[i].Name = newName
            // 同步更新 paymail binding
            for j := range state.PaymailBindings {
                if state.PaymailBindings[j].Vault == oldName {
                    state.PaymailBindings[j].Vault = newName
                }
            }
            return nil
        }
    }
    return fmt.Errorf("%w: %q", ErrVaultNotFound, oldName)
}
```

## 四、HD 密钥体系映射

### A. 密钥派生路径

```
Master Seed
├── m/44'/236'/0'/...              ← Fee account（交易手续费）
├── m/44'/236'/1'/0/0              ← vault "default"   (account_index=0)
│   │                                 ↳ paymail: alex@domain.com
│   └── m/44'/236'/1'/0/0/N'...   ← vault "default" 的文件节点
├── m/44'/236'/2'/0/0              ← vault "researcher" (account_index=1)
│   │                                 ↳ paymail: researcher@domain.com
│   └── m/44'/236'/2'/0/0/N'...   ← vault "researcher" 的文件节点
├── m/44'/236'/3'/0/0              ← vault "photos"    (account_index=2)
│                                     ↳ (未绑定 paymail)
└── m/44'/236'/N'/0/0              ← 更多 vault...
```

注意 BIP44 account = `vault.AccountIndex + DefaultVaultAccount`（其中 `DefaultVaultAccount=1`），
所以 `account_index=0` 对应 BIP44 路径 `m/44'/236'/1'/...`。

### B. 密钥导出

`bitfs vault export` 命令支持将 vault 私钥导出供外部使用者使用：

```go
// ExportVaultKey exports the root private key of a vault in the requested format.
func (w *Wallet) ExportVaultKey(state *WalletState, vaultName, format string) (string, error) {
    vault, err := w.GetVault(state, vaultName)
    if err != nil {
        return "", err
    }

    kp, err := w.DeriveVaultRootKey(vault.AccountIndex)
    if err != nil {
        return "", err
    }

    switch format {
    case "wif":
        return kp.PrivateKey.Wif(), nil
    case "hex":
        return hex.EncodeToString(kp.PrivateKey.Serialise()), nil
    case "seed-path":
        return kp.Path, nil
    default:
        return "", fmt.Errorf("unknown export format: %q (supported: wif, hex, seed-path)", format)
    }
}
```

导出后的私钥可交给外部使用者（人或程序）。**导出操作是安全敏感的**——
CLI 应在导出前要求确认或密码验证，stdout 输出后立即清屏提示。

## 五、CLI 命令详细设计

### A. `bitfs paymail bind <alias> <vault>`

```
用法: bitfs paymail bind <alias> <vault-name>

描述: 将 vault 绑定为 paymail alias。绑定后该 vault 的公钥可通过
      Paymail PKI 协议被外部发现。

参数:
  alias       Paymail 本地部分 (如 "researcher")
  vault-name  目标 vault 名称

示例:
  bitfs paymail bind alex default
  bitfs paymail bind researcher researcher
  bitfs paymail bind r researcher

输出 (成功):
  Bound paymail alias "researcher" → vault "researcher" (03c4d5e6...)

错误:
  Error: vault "nonexistent" not found
  Error: paymail alias "alex" already exists
  Error: vault "default" already has a paymail binding ("alex")
  Error: invalid paymail alias "UPPER": must match [a-z0-9][a-z0-9._-]{0,63}
```

Engine 层伪代码：

```go
func (e *Engine) PaymailBind(alias, vaultName string) error {
    return e.withWriteLock(func() error {
        state, err := e.loadWalletState()
        if err != nil {
            return err
        }
        if err := e.wallet.BindPaymail(state, alias, vaultName); err != nil {
            return err
        }
        return e.saveWalletState(state)
    })
}
```

### B. `bitfs paymail unbind <alias>`

```
用法: bitfs paymail unbind <alias>

描述: 移除 paymail alias 绑定。解绑后该 alias 不再响应 Paymail 查询。
      不影响 vault 本身及其文件。

参数:
  alias  要解绑的 paymail alias

示例:
  bitfs paymail unbind researcher

输出 (成功):
  Unbound paymail alias "researcher" (was → vault "researcher")

错误:
  Error: paymail alias "unknown" not found
```

### C. `bitfs paymail list`

```
用法: bitfs paymail list [--json]

描述: 列出所有 paymail alias 绑定。

输出 (表格, 默认):
  ALIAS        VAULT        PUBKEY
  alex         default      02a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4
  researcher   researcher   03c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7

输出 (JSON, --json):
  [
    {"alias":"alex","vault":"default","pubkey":"02a1b2c3..."},
    {"alias":"researcher","vault":"researcher","pubkey":"03c4d5e6..."}
  ]

输出 (无绑定):
  No paymail bindings configured.
```

列表展示时需要派生每个 vault 的公钥。由于派生是确定性的且只涉及根公钥，
开销可忽略。

### D. `bitfs vault export <vault> [--format wif|hex|seed-path]`

```
用法: bitfs vault export <vault-name> [--format FORMAT]

描述: 导出 vault 根私钥。导出的私钥可用于外部程序独立操作该 vault。

参数:
  vault-name  要导出的 vault 名称

选项:
  --format    输出格式 (默认: wif)
              wif       — Base58Check 编码的 WIF 私钥
              hex       — 32 字节原始私钥的十六进制
              seed-path — BIP44 派生路径 (如 m/44'/236'/2'/0/0)

示例:
  $ bitfs vault export researcher
  ⚠ WARNING: This will display the private key. Anyone with this key has
    full control over the vault "researcher" and all its files.
  Continue? [y/N] y
  L3p8oAcQTtuBkRqfmFcMj2SZvNxUqYGaDBLTkFasaP9eNsgmkenp

  $ bitfs vault export researcher --format seed-path
  m/44'/236'/2'/0/0
```

seed-path 格式不泄露密钥本身，仅告知派生路径，可安全记录。
使用者需要 seed 才能从路径恢复私钥。

## 六、Daemon 实现变更

### A. handlePKI 修改

当前实现（`bitfs/internal/daemon/paymail.go:43-68`）已通过 `d.wallet.GetVaultPubKey(alias)` 查找。**handler 代码无需修改**——变更封装在 `WalletService` 的实现层（`WalletAdapter.GetVaultPubKey`），handler 是透明的。

```
请求: GET /api/v1/pki/researcher@alex.bitfs.org

当前调用链:
  handlePKI → parseHandle("researcher@alex.bitfs.org") → alias="researcher"
           → d.wallet.GetVaultPubKey("researcher")
           → 直接按 vault name 查找 (隐式 alias=vault name)

修改后调用链:
  handlePKI → parseHandle("researcher@alex.bitfs.org") → alias="researcher"
           → d.wallet.GetVaultPubKey("researcher")
           → ResolvePaymailAlias("researcher") → vault name
           → GetVault(vaultName) → account_index
           → DeriveVaultRootKey(account_index) → 公钥
```

### B. handlePublicProfile 修改

同理，`handlePublicProfile`（`paymail.go:70-94`）已通过 `GetVaultPubKey(alias)` 校验 alias 存在性。代码无需修改。

Profile 的 `name` 字段当前返回 alias 本身。未来可扩展为从 binding 中读取可选的显示名称。

### C. handleVerifyPubKey

同理无需修改。已通过 `GetVaultPubKey(alias)` 做比对。

### D. handleBSVAlias (capabilities)

无需修改。URL 模板中的 `{alias}@{domain.tld}` 占位符由客户端填充。

### E. 内容路由

Paymail 解析到 P_node 后，daemon 的 `/_bitfs/meta/{pnode}/{path}` 端点已按 P_node 路由。
只要 daemon 能服务该 vault 的 Metanet 树，无需额外路由逻辑。

### F. 热加载机制

CLI 修改 WalletState 后，daemon 需要感知变更。两种方案：

**方案 1: 文件监听（推荐）**

daemon 启动时对 WalletState 文件设置 `fsnotify` 监听。
文件写入事件 → 重新加载 WalletState → 更新内存中的绑定表。

```go
// 在 daemon Start() 中
go func() {
    watcher, _ := fsnotify.NewWatcher()
    watcher.Add(walletStatePath)
    for event := range watcher.Events {
        if event.Op&fsnotify.Write != 0 {
            adapter.ReloadState()
        }
    }
}()
```

**方案 2: Reload API**

提供 admin 端点 `POST /_bitfs/admin/reload`（需 admin token），
CLI 修改完后主动调用。

```
POST /_bitfs/admin/reload
Authorization: Bearer <admin_token>
→ 200 OK {"reloaded": true}
```

推荐方案 1，daemon 自动感知，用户体验更好。方案 2 作为备选（无 fsnotify 环境）。

## 七、DNS 配置

### A. 所需 DNS 记录

域名所有者只需配置两条记录：

```
; Paymail SRV — 将 *@alex.bitfs.org 的 Paymail 查询路由到 daemon
_bsvalias._tcp.alex.bitfs.org.  IN  SRV  10 1 443 alex.bitfs.org.

; DNSLink TXT — 平台入口，指向 daemon vault 的根公钥
_bitfs.alex.bitfs.org.          IN  TXT  "bitfs=02a1b2c3d4e5..."
```

### B. DNS 记录与身份的关系

```
DNS 层:
  alex.bitfs.org → 一条 SRV 记录 → 一个 daemon 实例

Paymail 层 (daemon 内部):
  alex@alex.bitfs.org       → vault "default"    → m/44'/236'/1'/0/0
  researcher@alex.bitfs.org → vault "researcher" → m/44'/236'/2'/0/0
  ...任意数量的 alias

DNSLink 层:
  bitfs://alex.bitfs.org/... → daemon vault 的公共文件（平台主页）
```

**关键**: 多用户不需要多条 DNS 记录。一条 SRV 指向 daemon，所有 alias 在 daemon 内部解析。

### C. 多域名支持（未来扩展）

若同一 daemon 服务多个域名（如 `alex.bitfs.org` + `company.bitfs.org`），
需要在 binding 中增加 domain 字段。**当前设计不包含此功能**——
daemon 不校验请求中的 domain 部分，只看 alias。

## 八、完整解析流程

### A. Paymail 路径解析

```
bitfs://researcher@alex.bitfs.org/docs/paper.md

Client                          DNS              Daemon
  |                              |                  |
  |-- SRV lookup ──────────────→|                  |
  |   _bsvalias._tcp.alex...    |                  |
  |←── SRV 10 1 443 alex... ───|                  |
  |                              |                  |
  |-- GET /.well-known/bsvalias ─────────────────→|
  |←── capabilities JSON ────────────────────────|
  |                              |                  |
  |-- GET /api/v1/pki/researcher@alex... ────────→|
  |                              |  parseHandle()   |
  |                              |  → alias="researcher"
  |                              |  GetVaultPubKey("researcher")
  |                              |  → PaymailBindings lookup
  |                              |  → vault "researcher"
  |                              |  → DeriveVaultRootKey(1)
  |                              |  → P_node = 03c4d5...
  |←── {"pubkey":"03c4d5..."} ──────────────────|
  |                              |                  |
  |-- Method 42 Handshake ──────────────────────→|
  |   (使用 P_node 进行 ECDH)    |                  |
  |                              |                  |
  |-- GET /_bitfs/meta/{P_node}/docs/paper.md ──→|
  |←── 文件元数据 ────────────────────────────────|
```

### B. DNSLink 平台入口解析

```
bitfs://alex.bitfs.org/public/readme.md

1. ParseURI → AddressDNSLink (authority 无 @，非公钥 hex)
2. ResolveDNSLinkPubKey("alex.bitfs.org")
   → 查 _bitfs.alex.bitfs.org TXT → "bitfs=02a1b2c3..."
   → 返回 daemon vault 的 P_node
3. 连接 daemon，以 daemon vault 的 P_node 浏览 /public/readme.md
```

DNSLink 访问的是 **daemon 自身 vault 的文件**，不经过 paymail binding。
两种寻址互补：DNSLink = 平台入口，Paymail = 具体用户。

## 九、安全性分析

### A. 私钥隔离

```
安全边界:
  daemon 进程内:
    - master seed 在内存中 (已有, 用于 Method 42 签名)
    - 按需派生公钥 (DeriveVaultRootKey, 只需公钥不需私钥)
    - 不缓存任何 vault 私钥到磁盘

  vault export 后:
    - 私钥移交给外部使用者
    - daemon 不再需要该 vault 的私钥
    - 外部使用者自行保管
```

注意: daemon 目前已需要 seed 在内存中以进行 Method 42 ECDH 签名。
paymail binding 不引入额外的密钥暴露面——所有 vault 的私钥理论上已可从 seed 派生。
binding 只控制哪些 vault **对外可见**。

### B. Alias 枚举防护

- **不提供列表端点**: Paymail 协议不要求也不应提供 "列出所有 alias" 的 HTTP 端点
- **不存在时返回 404**: `handlePKI` 对未知 alias 返回 `404 NOT_FOUND`
- **VerifyPubKey 不泄露存在性**: 未知 alias 返回 `match:false`，不返回 404
- **Rate limiting**: 现有 per-IP token bucket 防止暴力枚举

### C. 路径穿越防护

alias 经过 `isValidAlias()` 校验：
- 不允许 `/`、`..`、空格、null 字节
- 不允许 `@`（防止 handle 解析混淆）
- 限长 64（防止缓冲区溢出和 DoS）

### D. Vault 删除安全

删除 vault 时自动清理 paymail binding（见 §三.C），确保不会出现悬空绑定指向已删除 vault。

## 十、向后兼容性

### A. WalletState 格式

`paymail_bindings` 字段使用 `omitempty`。旧版本的 WalletState JSON 没有该字段，
反序列化时 `PaymailBindings` 为 `nil`（零值），等同于无绑定。

### B. 迁移路径

现有用户升级后，所有 vault 默认**不绑定**任何 paymail alias。
用户需要显式运行 `bitfs paymail bind` 来创建绑定。

这是一个**有意的行为变更**: 升级后 Paymail PKI 将对所有 alias 返回 404，
直到用户重新配置绑定。这比自动暴露所有 vault 更安全。

### C. WalletService 接口

`GetVaultPubKey(alias string) (string, error)` 接口签名不变。
行为变更封装在实现层（`WalletAdapter`），daemon handler 无需修改。

## 十一、边界情况

| 场景 | 行为 |
|------|------|
| alias 与 vault 同名 | 允许，但不自动关联。必须显式 bind |
| 同一 vault bind 后再 bind 不同 alias | 报错 `ErrVaultAlreadyBound`，需先 unbind |
| bind 到已删除的 vault | 报错 `ErrVaultNotFound` |
| 删除已绑定的 vault | 自动清理 binding |
| 重命名已绑定的 vault | 自动更新 binding 中的 vault 名称 |
| WalletState 文件损坏 | `Validate()` 检测不一致，daemon 拒绝启动 |
| 无绑定时 paymail 请求 | 所有 alias 返回 404 |
| alias 包含 Unicode | 拒绝，只允许 ASCII 子集 |
| 并发 bind/unbind | `withWriteLock()` 文件锁序列化 |

## 十二、测试用例

| # | 类别 | 测试 | 预期 |
|---|------|------|------|
| 1 | bind | 绑定 valid alias 到存在的 vault | 成功，state 包含 binding |
| 2 | bind | 绑定 alias 到不存在的 vault | ErrVaultNotFound |
| 3 | bind | 绑定已占用的 alias | ErrAliasExists |
| 4 | bind | 绑定已有 binding 的 vault | ErrVaultAlreadyBound |
| 5 | bind | alias 格式无效（大写、特殊字符、空、超长） | ErrInvalidAlias |
| 6 | unbind | 解绑存在的 alias | 成功，binding 被移除 |
| 7 | unbind | 解绑不存在的 alias | ErrAliasNotFound |
| 8 | resolve | 解析已绑定的 alias | 返回正确的 vault 名和公钥 |
| 9 | resolve | 解析未绑定的 alias | ErrAliasNotFound |
| 10 | delete | 删除已绑定的 vault | vault 标记删除 + binding 被清理 |
| 11 | rename | 重命名已绑定的 vault | binding.vault 同步更新 |
| 12 | validate | PaymailBindings 引用已删除 vault | Validate() 报错 |
| 13 | validate | 重复 alias | Validate() 报错 |
| 14 | validate | 同一 vault 多个 alias | Validate() 报错 |
| 15 | compat | 旧 JSON 无 paymail_bindings | 反序列化成功，bindings 为空 |
| 16 | daemon | PKI 请求已绑定 alias | 返回正确公钥 |
| 17 | daemon | PKI 请求未绑定 alias | 返回 404 |
| 18 | export | 导出 vault 私钥 (WIF/hex/seed-path) | 格式正确，可反向验证 |

## 十三、文件变更清单

| 文件 | 变更类型 | 内容 |
|------|----------|------|
| `libbitfs-go/wallet/vault.go` | 修改 | +PaymailBinding 结构体, +BindPaymail/UnbindPaymail/ListPaymailBindings/ResolvePaymailAlias, 修改 DeleteVault/RenameVault 联动 |
| `libbitfs-go/wallet/vault.go` | 修改 | WalletState 新增 PaymailBindings 字段, Validate() 扩展 |
| `libbitfs-go/wallet/errors.go` | 修改 | +ErrAliasExists, +ErrAliasNotFound, +ErrInvalidAlias, +ErrVaultAlreadyBound |
| `libbitfs-go/wallet/vault_test.go` | 修改 | 18 个新测试用例 |
| `bitfs/internal/engine/paymail.go` | 新建 | PaymailBind/Unbind/List engine 方法 |
| `bitfs/internal/engine/export.go` | 新建 | VaultExport engine 方法 |
| `bitfs/cmd/bitfs/paymail.go` | 新建 | paymail bind/unbind/list 子命令 |
| `bitfs/cmd/bitfs/vault_export.go` | 新建 | vault export 子命令 |
| `bitfs/internal/daemon/wallet_adapter.go` | 修改 | GetVaultPubKey 查询 PaymailBindings |

## 十四、未来扩展（不在本次范围内）

- **bput 工具**: visitor 向 daemon 上传文件的写入工具（bcat 的写入对应物）
- **链上发布**: `--publish` 选项将绑定信息写入 daemon vault 的 Metanet 树
- **外部公钥注册**: 允许非 HD 派生的外部公钥注册为 paymail alias
- **多域名**: 一个 daemon 服务多个域名的 paymail，binding 增加 domain 字段
- **权限/配额**: 按 alias 设置存储/带宽配额
- **Profile 元数据**: binding 中增加 display_name/avatar 可选字段

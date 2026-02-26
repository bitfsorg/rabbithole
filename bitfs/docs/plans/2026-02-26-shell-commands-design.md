# Shell 命令补全设计 — 2026-02-26

对应设计审查 2.2：Shell 补全 cat/publish 命令。

## 背景

bitfs Shell 当前 16 个命令（ls/cd/lcd/pwd/mkdir/put/rm/mv/cp/link/sell/encrypt/help/quit/exit），缺少文件查看、发布管理和批量操作能力。

### 架构澄清

| 组件 | 面向 | 用途 |
|------|------|------|
| `bitfs` (Shell + CLI) | Owner | 管理自己的 vault + 链上数据 |
| `b*` tools | Visitor | 访问/购买别人的文件 |
| daemon | 外界 | 让别人来读取/购买我们的文件 |

两层都有 wallet：owner 持有 D_node 可解密全部文件，visitor 用于 ECDH 握手和 BSV 支付。

### 内容来源是多元的

无论 owner 还是 visitor，内容获取来源链相同：

- **元数据** ← 区块链（永远可用）
- **内容数据** ← 本地 storage / 区块链 DataTx / daemon HTTP / CDN

典型场景：owner 在新设备上恢复种子后，本地没有内容数据，需要从区块链或其他设备的 daemon 拉取。

---

## 一、Content Resolver

cat/get/mget 的共享基础设施。

**位置**: `libbitfs-go/storage/resolver.go`

**接口**:

```go
type ContentResolver struct {
    Store     *FileStore          // 本地 storage
    Chain     BlockchainService   // 区块链查询（DataTx）
    Endpoints []string            // daemon/CDN 端点列表
}

// Fetch 按优先级尝试获取密文
func (r *ContentResolver) Fetch(keyHash string) ([]byte, error)
```

**解析顺序**:

1. 本地 `~/.bitfs/storage/{hash-sharded}/` — 命中直接返回
2. 区块链 DataTx — 通过 BlockchainService 查询 key_hash 对应的交易
3. daemon HTTP — `GET /_bitfs/data/{hash}` 遍历 endpoints 列表
4. （未来）CDN — Metanet 节点

**设计要点**:
- Resolver 只返回密文，不负责解密。调用方用自己的密钥解密。
- Owner 用 D_node 解密，Visitor 用购买获得的 capsule 解密。
- Endpoints 列表可从配置文件或命令行参数获取。

---

## 二、新增命令

### 2.1 cat

查看自己 vault 中的文件内容，输出到 stdout。

**Shell**: `cat <path>`
**CLI**: `bitfs cat <path>`

**Engine 方法**:

```go
type CatOpts struct {
    VaultIndex uint32
    Path       string
}

func (e *Engine) Cat(opts *CatOpts) (io.Reader, *FileInfo, error)

type FileInfo struct {
    MimeType string
    FileSize uint64
    Access   string
}
```

**流程**:

1. `ResolveNode(path)` → 找到节点，获取 key_hash
2. `ContentResolver.Fetch(keyHash)` → 获取密文
3. `wallet.DeriveNodeKey()` → 派生 owner 的 D_node
4. `method42.Decrypt(D_node, ciphertext)` → 明文
5. 输出到 stdout

**Shell 端处理**:
- 文本文件：直接打印
- 二进制文件：提示 "binary file, use `get` to download"，除非 `cat --force`

### 2.2 get

下载单个文件到本地磁盘。

**Shell**: `get <remote> [local]`
**CLI**: `bitfs get <remote> [local]`

**Engine 方法**:

```go
type GetOpts struct {
    VaultIndex uint32
    RemotePath string
    LocalPath  string   // 默认: 当前 lcd + 文件名
}

func (e *Engine) Get(opts *GetOpts) (*Result, error)
```

**流程**: 与 cat 相同的 1-4 步，第 5 步写入本地文件而非 stdout。

### 2.3 mget

递归下载 vault 目录到本地磁盘。

**Shell**: `mget <remotedir> [localdir]`
**CLI**: `bitfs mget <remotedir> [localdir]`

**Engine 方法**:

```go
type MgetOpts struct {
    VaultIndex uint32
    RemotePath string
    LocalDir   string   // 默认: 当前 lcd
}

type MgetResult struct {
    DirsCreated     int
    FilesDownloaded int
    Errors          []string
}

func (e *Engine) Mget(opts *MgetOpts) (*MgetResult, error)
```

**流程**:

1. `ResolveNode(remotePath)` → 获取目录节点
2. 递归遍历 `node.Children`
3. 遇到目录 → `os.MkdirAll()` 创建本地目录
4. 遇到文件 → 调用 `Get()` 下载
5. 保持目录结构：`/vault/a/b.txt` → `localdir/a/b.txt`
6. 单文件失败记录错误但继续，最后汇总

### 2.4 mput

递归上传本地目录到 vault。

**Shell**: `mput <localdir> [remotedir]`
**CLI**: `bitfs mput <localdir> [remotedir]`

**Engine 方法**:

```go
type MputOpts struct {
    VaultIndex uint32
    LocalDir   string
    RemoteDir  string   // 默认: cwd
    Access     string   // free/private，应用到所有文件
}

type MputResult struct {
    DirsCreated   int
    FilesUploaded int
    Errors        []string
}

func (e *Engine) Mput(opts *MputOpts) (*MputResult, error)
```

**流程**:

1. `filepath.WalkDir(localDir)` 遍历本地目录
2. 遇到目录 → `eng.Mkdir()` 创建远程目录
3. 遇到文件 → `eng.PutFile()` 上传
4. 保持目录结构：`local/a/b.txt` → `remote/a/b.txt`
5. 单文件失败记录错误但继续，最后汇总

### 2.5 publish / unpublish

Shell 接入已有功能。

**Shell**:
- `publish` — 列出当前所有域名绑定
- `publish <domain>` — 绑定域名到当前 vault
- `unpublish <domain>` — 解除域名绑定

**Engine 层**:
- `Publish()` 已存在，直接接入 shell
- `Unpublish()` 新增薄方法，调用 `State.RemovePublishBinding(domain)`

---

## 三、实现顺序

1. **ContentResolver** — 基础设施
2. **cat** — 最小验证 resolver 可用
3. **get** — cat 基础上加写磁盘
4. **mget** — 递归调 get
5. **mput** — 递归调 PutFile + Mkdir
6. **publish/unpublish** — 接入已有逻辑

---

## 四、不在本次范围（后续任务）

### 4.1 lock/unlock session 管理

类似 ssh-agent。`bitfs unlock [duration]` 缓存认证（daemon 内存 or session 文件），`bitfs lock` 安全清除。解决 CLI 子命令重复输密码问题。设计已完成（见 claude-mem #2619），待实现。

### 4.2 b* tools 批量命令

- `bmget` — visitor 递归下载别人的目录
- b* tools 共用 ContentResolver

### 4.3 CDN 端点解析

ContentResolver 第 4 级：从 Metanet 节点获取内容。依赖 Metanet CDN 网络实现。

### 4.4 bitfs CLI 简化评估

评估是否将文件操作 CLI 子命令（put/mkdir/rm/mv/cp 等）整合到 Shell 唯一入口，保留 `bitfs -c "cmd"` 非交互模式。非紧急。

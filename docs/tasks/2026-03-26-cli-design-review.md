# BitFS CLI 设计评审 — 资深 Unix 用户视角

> 评审时间: 2026-03-26
> 基于版本: v0.0.1 (5 轮 Docker 审查后)
> 视角: 日常使用 ssh/rsync/git/docker/kubectl 的系统工程师首次深入使用 BitFS

---

## 一、整体印象

BitFS 的命令设计思路是对的——模仿 Unix 文件系统命令 (ls/cat/get/put/rm/mv/cp) + FTP-style shell (类似 sftp)。这是一个好的起点。但作为一个 v0.0.1，有一些设计决策值得在用户群扩大之前重新审视。

以下按**影响面从大到小**排列。

---

## 二、命令结构问题

### 2.1 两层命令体系造成认知分裂

当前有两套工具做同一件事：

| 操作 | Owner (bitfs) | Visitor (b-tools) |
|------|---------------|-------------------|
| 列目录 | shell → ls | `bls` |
| 看文件 | `bitfs cat` | `bcat` |
| 下载 | `bitfs get` | `bget` |
| 批量下载 | `bitfs mget` | `bmget` |
| 元数据 | shell → stat? | `bstat` |
| 目录树 | shell → ? | `btree` |

**问题**: 用户必须学两套工具。`bitfs cat` 需要密码和本地 vault，`bcat` 通过 daemon HTTP 接口。但从用户视角，"我只是想看个文件"。

**建议**:
- 短期：`bitfs ls` 应该存在（当前只能通过 shell 或 bls 访问）
- 长期：考虑统一为 `bitfs ls`（本地时直接读 vault，远程时自动走 daemon）

### 2.2 缺少 `bitfs ls` 和 `bitfs stat` 顶级命令

`ls` 是文件系统最基本的操作。当前 `bitfs` 的文件命令有 cat/get/put/mkdir/rm/mv/cp/link，唯独**没有 ls**。用户想列个目录必须进 shell 或用 bls。

**对比**:
- `ipfs ls <hash>` ✅ 一级命令
- `git ls-files` ✅ 一级命令
- `bitfs ls /` ❌ 不存在

**建议**: 添加 `bitfs ls` 和 `bitfs stat` 作为顶级命令。

### 2.3 命令分组过细

当前帮助把命令分成 7 组：Wallet / Vault / Paymail / File / Trading / Publishing / Daemon / Verification / Interactive。对只有 ~20 个命令的 CLI 来说分组过细，增加认知负担。

**对比**: `git` 有 ~150 个命令才分成 5 组。`docker` 有 ~60 个命令分 3 组。

**建议**: 合并为 3-4 组：
```
Setup:     wallet, vault, paymail, daemon
Files:     ls, cat, get, put, mget, mput, mkdir, rm, mv, cp, link, stat
Access:    sell, encrypt, publish, unpublish
Tools:     shell, verify
```

---

## 三、Unix 惯例违反

### 3.1 不支持 stdin 管道

```bash
# 这些应该能工作但目前不行:
echo "hello" | bitfs put --network testnet - /greeting.txt
tar czf - mydir/ | bitfs put --network testnet - /backup.tar.gz
bitfs cat --network testnet /greeting.txt | grep hello
```

`bitfs put` 只接受本地文件路径，不接受 `-`（stdin）。`bitfs cat` 输出到 stdout（这个对了），但不支持从 stdin 读取。

**对比**:
- `ipfs add -` ← 从 stdin 读取
- `aws s3 cp - s3://bucket/key` ← 从 stdin 读取
- `docker cp - container:/path` ← 从 stdin 读取

**建议**: `bitfs put - /remote/path` 应该从 stdin 读取。这是 Unix 管道哲学的核心。

### 3.2 `--password` 明文参数

每个命令都需要 `--password pw123`，这会出现在 `ps aux` 和 shell history 中。

**对比**:
- `ssh` — 不接受密码命令行参数
- `gpg` — 有 `--passphrase-fd 0` 从 fd 读取
- `mysql` — 有 `-p` 交互提示 + `MYSQL_PWD` 环境变量（已弃用但存在）
- `age` — 用 `AGE_PASSPHRASE` 环境变量

**建议**:
1. 添加 `BITFS_PASSWORD` 环境变量支持（CI/CD 友好）
2. 添加 `--password-fd N` 从文件描述符读取
3. 长期：实现 `bitfs unlock` 会话模型（类似 ssh-agent），unlock 一次后续命令免密

### 3.3 缺少 `--quiet` / `--verbose` 全局选项

Unix 工具通常支持 `-q` (quiet) 和 `-v` (verbose)。当前 bitfs 没有。

- `bitfs put` 成功后打印消息 — 在脚本中这些是噪音
- 错误时没有 debug 信息 — 排错困难

**建议**: 全局 `--quiet` 抑制成功输出，`--verbose` 显示 debug 信息。

### 3.4 `-v` 被 `--version` 占用

`-v` 在 Unix 惯例中是 `--verbose`。`bitfs -v` 输出版本号，这与几乎所有 Unix 工具的惯例冲突。

**对比**:
- `ls -v` = natural sort
- `tar -v` = verbose
- `curl -v` = verbose
- `git -v` = version (但 git 也不太标准)

**建议**: `--version` 保留长形式，`-V`（大写）用于 version，`-v` 用于 verbose。

---

## 四、flag 设计问题

### 4.1 `--datadir` 和 `--network` 在每个命令重复

用户在 testnet 上工作时，每条命令都要写 `--network testnet --password xxx`：

```bash
bitfs vault list --password pw --network testnet
bitfs put --password pw --network testnet /tmp/file /remote
bitfs cat --password pw --network testnet /remote
bitfs rm --password pw --network testnet /remote
```

这非常冗长。

**对比**:
- `kubectl` — 用 `--context` 或 `KUBECONFIG` 环境变量，设置一次
- `aws` — 用 `AWS_PROFILE` 或 `--profile`
- `git` — 从 `.git/config` 自动推导

**建议**:
1. `BITFS_NETWORK` 环境变量（当前虽然有 `BITFS_DATADIR`，但没有 `BITFS_NETWORK`）
2. `BITFS_PASSWORD` 环境变量
3. 或者：`bitfs context use testnet` 持久切换（写入 `~/.bitfs/context`）

### 4.2 `--vault` 参数使用不一致

```bash
bitfs put --vault myvault ...    # flag 形式
bitfs vault delete myvault       # positional 形式
bitfs vault export myvault       # positional 形式
```

对 vault 管理命令，vault 名是 positional；对文件操作命令，vault 名是 `--vault` flag。这可以理解，但 `--vault` 的默认值是隐式的（第一个 vault），没有文档说明。

**建议**: 在 `--vault` 的帮助文字中说明默认行为: `"vault name (default: first vault)"`

### 4.3 长标志用单横线

Go 的 `flag` 包不区分 `-network` 和 `--network`。这意味着 `-network testnet` 能工作。大多数现代 CLI 约定单横线用于短标志，双横线用于长标志。当前没有任何短标志（除了 `-h`, `-v`）。

**建议**: 为高频操作添加短标志:
- `-n` = `--network`
- `-p` = `--password` (或通过环境变量消除)
- `-d` = `--datadir`
- `-V` = `--vault`

---

## 五、工作流痛点

### 5.1 初始化流程太长

首次用户需要:
1. `bitfs wallet init --password ... --network testnet`
2. `bitfs wallet fund --password ... --network testnet`
3. 充值 BSV（需要去第三方购买）
4. `bitfs wallet balance --password ... --network testnet --refresh`
5. 确认余额
6. `bitfs put --password ... --network testnet file /path`

6 步才能上传第一个文件。

**对比**: `ipfs add file` — 1 步（本地操作，不需要钱）。

**建议**:
- 考虑 `bitfs init` 作为一键初始化（wallet init + 配置）的快捷方式
- regtest 模式下自动资金注入（开发者体验）

### 5.2 没有 `bitfs status` 命令

用户经常需要快速了解: 我在哪个网络？钱包余额多少？有多少 vault？daemon 在跑吗？

**对比**:
- `git status` — 一条命令看全局
- `docker info` — 一条命令看系统状态
- `kubectl cluster-info` — 一条命令看连接状态

**建议**: `bitfs status` 输出:
```
Network:  testnet
Wallet:   funded (1,234 sats)
Vaults:   3 (default, photos, docs)
Daemon:   stopped
Storage:  42 files, 1.2 MB
```

### 5.3 shell vs 命令行重复

shell 里有 22 个命令，CLI 有 ~20 个命令。大部分重复。维护两套实现是负担。

**对比**: `sftp` 只提供 shell 模式，不提供每个操作的独立命令。`git` 只提供命令行，不提供 shell。两者选其一更好。

**建议**: shell 应该复用 CLI 命令的实现（内部调用同一个函数），而不是维护独立逻辑。如果已经是这样，应该在代码结构上更明显。

---

## 六、输出设计问题

### 6.1 没有统一的 `--json` 支持

| 命令 | --json |
|------|--------|
| put, rm, mv, cp, mkdir | ✅ |
| cat, get, mget, mput | ❌/部分 |
| wallet show/balance | ❌ |
| vault list | ❌ |
| sell, encrypt | ❌ |

对于 Agent/自动化场景，每个命令都应该支持 `--json`。

**建议**: 所有命令统一支持 `--json`。

### 6.2 成功输出到 stderr

`bitfs put` 的成功消息输出到 stderr（通过 `printError` 系列函数）。但成功不是错误，不应该到 stderr。

**Unix 惯例**:
- stdout: 数据 / 正常输出
- stderr: 错误 / 诊断

`bitfs cat /file` 正确地把内容输出到 stdout。但 `bitfs put` 的 "Uploaded successfully" 去了哪里需要确认。

---

## 七、缺失功能

### 7.1 没有 `bitfs ls`（最严重的缺失）

如上所述。

### 7.2 没有 `bitfs tree`

`btree` 存在但 `bitfs tree` 不存在。

### 7.3 没有 `bitfs diff` 或 `bitfs log`

文件有版本历史（bstat --versions），但没有方便的方式查看变更或历史。

### 7.4 没有 `bitfs whoami`

查看当前钱包/vault/网络信息。`bitfs wallet show` 太长了。

### 7.5 没有 shell completion

没有 `bitfs completion bash/zsh/fish` 命令生成补全脚本。对重度 CLI 用户来说这是必需品。

---

## 八、命名建议

### 8.1 `encrypt` 命名不精确

`bitfs encrypt` 实际含义是"将文件从 free access 改为 private access"。这不是加密（Method 42 已经默认加密所有数据），而是**访问控制变更**。

**建议**: 考虑 `bitfs private <path>` / `bitfs public <path>`（或 `bitfs access private <path>`）。

### 8.2 `sell` 暗示完整的市场功能

`bitfs sell` 设置价格，但买方需要用 `bcat --buy` 或 `bget --buy` 来购买。命名上 sell/buy 是对称的，但工具层面 sell 在 bitfs，buy 在 bcat/bget，这很容易混淆。

### 8.3 `mget`/`mput` 前缀不直觉

`mget`/`mput` 来自 FTP 传统（multiple get/put），但现代用户可能不知道 `m` 前缀的含义。

**替代方案**: `bitfs pull`/`bitfs push`（类似 git 语义）或 `bitfs sync`（类似 rsync）。

---

## 九、总结：建议优先级

### P0（核心体验，v0.1 前应修复）
1. 添加 `bitfs ls` 顶级命令
2. `BITFS_PASSWORD` / `BITFS_NETWORK` 环境变量
3. `bitfs put -`（stdin 支持）

### P1（用户体验，v0.2 前应修复）
4. `bitfs status` 总览命令
5. 简化命令分组（7组→4组）
6. 全局 `--quiet` 选项
7. 所有命令统一 `--json`

### P2（打磨，v1.0 前应修复）
8. `bitfs completion bash/zsh/fish`
9. 短标志 `-n`/`-d` 等
10. `bitfs init` 一键初始化
11. `encrypt` → `access` 命名重构

### P3（远期方向）
12. `bitfs unlock` 会话模型
13. `bitfs context` 持久网络切换
14. 统一 owner/visitor 工具体系

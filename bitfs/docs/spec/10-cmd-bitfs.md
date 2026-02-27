# 模块规范：cmd/bitfs

## 目的

BitFS 的主 CLI 二进制文件——去中心化加密文件系统的所有者/写入者接口。实现所有文件管理、加密、交易、钱包、发布和守护进程命令。使用 Cobra 进行子命令管理，使用 Viper 进行配置管理。

设计参考：ConceptDesign #1, #2, #19; SystemDesign 第 9, 10 节; DetailedDesign 第 9-B 节。

## 公共 API

### 子命令

```
bitfs init [--network <net>]           初始化钱包 + 第一个保险库
bitfs put <local> <remote>             上传文件（创建或更新）
bitfs put --encrypt <local> <remote>   上传加密文件（私有模式）
bitfs mkdir <path>                     创建目录
bitfs rm <path>                        删除文件（从父目录移除 ChildEntry）
bitfs rm -r <path>                     递归删除
bitfs rmdir <path>                     删除空目录
bitfs mv <src> <dst>                   移动/重命名
  同目录: 仅修改父目录 ChildEntry.Name (1 笔交易)
  跨目录: DELETE 旧节点 + CreateChild 新节点, 新密钥, 重新加密 (4 笔交易)
  注意: 跨目录 mv 付费文件会使已购买 capsule 失效
bitfs cp <src> <dst>                   复制（创建独立新节点）
bitfs link <target> <name>             硬链接
bitfs link -s <target> <name>          软链接（本地）
bitfs link -s <domain/path> <name>     软链接（远程）
bitfs encrypt <path>                   免费 -> 私有
bitfs decrypt <path>                   私有 -> 免费
bitfs sell <path> --price <sat/KB>     设置价格（付费模式）
bitfs sell <path> --recursive          递归定价
bitfs sales [path]                     查看销售记录
bitfs publish <domain> [path]          通过 DNSLink 绑定域名
bitfs unpublish <domain>               解绑域名
bitfs publish                          列出绑定
bitfs vault create <name>              创建新保险库
bitfs vault list                       列出保险库
bitfs vault use <name>                 切换活跃保险库
bitfs vault info [name]                显示保险库详情
bitfs vault rename <old> <new>         重命名保险库
bitfs vault delete <name>              删除保险库（软删除）
bitfs wallet init                      创建 HD 钱包
bitfs wallet restore                   从助记词恢复
bitfs wallet info                      余额、地址、网络
bitfs wallet fund                      显示充值地址
bitfs daemon start [-d]                启动守护进程（可选后台运行）
bitfs daemon stop                      停止守护进程
bitfs daemon status                    显示守护进程状态
bitfs daemon config                    显示守护进程配置
bitfs shell                            FTP 风格交互式 REPL
```

### Shell 命令

`bitfs shell` 进入 FTP 风格交互式 REPL，支持以下命令：

| 命令 | 语法 | 说明 |
|------|------|------|
| help | `help` | 显示命令帮助 |
| quit/exit | `quit` / `exit` | 退出 shell |
| pwd | `pwd` | 显示当前远程目录 |
| cd | `cd <path>` | 切换远程目录 |
| lcd | `lcd <path>` | 切换本地目录 |
| ls | `ls [path]` | 列出目录内容 |
| mkdir | `mkdir <path>` | 创建目录 |
| put | `put <local> <remote> [access]` | 上传文件 (access: free/private/paid) |
| rm | `rm [-r] <path>` | 删除文件或目录 (-r 递归) |
| mv | `mv <src> <dst>` | 移动/重命名 |
| cp | `cp <src> <dst>` | 复制文件 |
| link | `link [-s] <target> <name>` | 创建链接 (-s 软链接, 默认硬链接) |
| sell | `sell <path> <price> [--recursive]` | 设置价格 |
| cat | `cat <path> [--force]` | 显示文件内容 |
| get | `get <remote> [local]` | 下载文件 |
| mget | `mget <dir> [local-dir]` | 批量下载 |
| mput | `mput <dir> [remote-dir]` | 批量上传 |
| publish | `publish [domain]` | 发布/列出 DNSLink 绑定 |
| unpublish | `unpublish <domain>` | 解绑域名 |
| encrypt | `encrypt <path>` | Free -> Private |
| decrypt | `decrypt <path>` | Private -> Free |
| sales | `sales` | 查看销售记录 |

Shell 特性：
- Tab 补全（命令名 + 路径）
- 命令历史 (`~/.bitfs/shell_history`, 0600 权限, 500 行上限)
- 路径解析：支持 `.` / `..` / 绝对路径 / 相对路径

### 全局标志

```
--json              JSON 输出（代理友好）
--no-cache          禁用本地缓存
--timeout N         请求超时（秒）
--offline           强制仅缓存模式
--home <path>       覆盖 BITFS_HOME（默认 ~/.bitfs）
--vault <name>      为此命令覆盖活跃保险库
```

### 退出码

```
0 = 成功
1 = 一般错误
2 = 参数错误
3 = 钱包错误
4 = 网络错误
5 = 权限错误
6 = 未找到
7 = 冲突
```

## 依赖

- `github.com/spf13/cobra` -- CLI 框架
- `github.com/spf13/viper` -- 配置管理
- `libbitfs/wallet` -- HD 钱包操作
- `libbitfs/method42` -- 加密
- `libbitfs/tx` -- 交易构建
- `libbitfs/metanet` -- 文件系统操作
- `libbitfs/storage` -- 内容存储
- `libbitfs/spv` -- SPV 验证
- `internal/daemon` -- 守护进程管理
- `libbitfs/paymail` -- URI 解析
- `libbitfs/x402` -- 支付协议

## 数据结构

### 配置文件（~/.bitfs/config.toml）
```toml
network = "mainnet"
output = "plain"

[cache]
enabled = true
max_size = "5GB"
meta_ttl = 3600
data_ttl = 86400
eviction = "lru"

[daemon]
listen = "0.0.0.0:80"
```

## 错误处理

所有命令遵循相同模式：
1. 解析参数，验证输入
2. 加载钱包（如需要），用密码解锁
3. 执行操作
4. 输出结果（根据 --json 标志选择纯文本或 JSON）
5. 返回适当的退出码

网络错误：使用指数退避重试 3 次（1秒/2秒/4秒）。
UTXO 冲突：自动重建并重试（最多 3 次）。
余额不足：显示差额金额 + `bitfs wallet fund` 地址。

## 安全考量

1. **密码提示**：钱包密码从终端读取（不是命令行），避免 shell 历史记录暴露。
2. **会话管理**：当守护进程运行时，CLI 使用 Unix 套接字（内存中）。否则，使用受限权限的会话文件。
3. **助记词显示**：助记词仅在 `wallet init` 期间显示一次，永不以明文存储。

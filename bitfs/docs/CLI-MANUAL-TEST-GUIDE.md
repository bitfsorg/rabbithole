# BitFS CLI 手动测试指南

本文档是 BitFS CLI 的完整手动测试流程。按顺序执行每个阶段，每步都标注了**预期结果**供你验证。

> **约定**：用 `$DATADIR` 表示测试数据目录（默认 `~/.bitfs`）。为避免污染正式数据，建议用 `--datadir /tmp/bitfs-test`。

---

## 阶段 0：构建

```bash
cd ~/Codes/RabbitHole/bitfs

# 构建所有二进制
go build -o ./bin/ ./cmd/...
```

**预期**：`bin/` 下生成 6 个可执行文件：`bitfs`, `bls`, `bcat`, `bget`, `bstat`, `btree`。

```bash
ls -la bin/
```

为方便后续使用，设置 PATH：

```bash
export PATH="$PWD/bin:$PATH"
export TESTDIR="/tmp/bitfs-test"
```

**快速验证**：

```bash
bitfs --version
# 预期输出: bitfs version 0.1.0-dev

bitfs --help
# 预期输出: 完整帮助信息，列出所有命令分类
```

---

## 阶段 1：钱包与保险库（纯本地，无需网络）

### 1.1 初始化钱包

```bash
# 清理之前的测试数据
rm -rf $TESTDIR

# 初始化 12 词助记词钱包
bitfs wallet init --datadir $TESTDIR --password test123
```

**预期**：
- 显示 12 个英文单词的助记词（**记下来**，后面可能用到）
- 显示 fee address（hex 压缩公钥）
- 创建 `$TESTDIR/wallet.enc`、`$TESTDIR/state.json`、`$TESTDIR/config.json`

**验证文件**：

```bash
ls -la $TESTDIR/
# 预期: wallet.enc, state.json, config.json 三个文件
cat $TESTDIR/config.json
# 预期: JSON 格式，含 DataDir, ListenAddr, Network, LogLevel 字段
```

### 1.2 查看钱包信息

```bash
bitfs wallet show --datadir $TESTDIR --password test123
```

**预期**：显示网络类型、fee address、vault 列表（应有一个 default vault）。

### 1.3 查看余额（本地）

```bash
bitfs wallet balance --datadir $TESTDIR --password test123
```

**预期**：显示余额为 0（尚未注资）。

### 1.4 重复初始化（应报错）

```bash
bitfs wallet init --datadir $TESTDIR --password test123
```

**预期**：报错，提示钱包已存在。

### 1.5 用 24 词初始化另一个钱包

```bash
bitfs wallet init --datadir /tmp/bitfs-test-24 --password test456 --words 24
```

**预期**：显示 24 个单词的助记词。

```bash
rm -rf /tmp/bitfs-test-24  # 清理
```

---

## 阶段 2：保险库管理

### 2.1 列出保险库

```bash
bitfs vault list --datadir $TESTDIR --password test123
```

**预期**：显示 default vault（初始化时自动创建）。

### 2.2 创建新保险库

```bash
bitfs vault create work --datadir $TESTDIR --password test123
bitfs vault create personal --datadir $TESTDIR --password test123
```

**预期**：每次成功创建，显示确认信息。

### 2.3 验证保险库列表

```bash
bitfs vault list --datadir $TESTDIR --password test123
```

**预期**：列出 3 个 vault：default, work, personal。

### 2.4 重命名保险库

```bash
bitfs vault rename work projects --datadir $TESTDIR --password test123
bitfs vault list --datadir $TESTDIR --password test123
```

**预期**：`work` 变为 `projects`，列表显示 default, projects, personal。

### 2.5 删除保险库

```bash
bitfs vault delete personal --datadir $TESTDIR --password test123
bitfs vault list --datadir $TESTDIR --password test123
```

**预期**：`personal` 被软删除，列表不再显示（或标记为已删除）。

### 2.6 边界测试

```bash
# 删除不存在的 vault
bitfs vault delete nonexistent --datadir $TESTDIR --password test123
# 预期: 报错

# 创建重名 vault
bitfs vault create default --datadir $TESTDIR --password test123
# 预期: 报错，名称冲突

# 空名称
bitfs vault create "" --datadir $TESTDIR --password test123
# 预期: 报错
```

---

## 阶段 3：启动 Regtest 节点

文件操作需要构建链上交易，后续需要真实 UTXO。启动 Docker 中的 BSV regtest 节点：

```bash
cd ~/Codes/RabbitHole/bitfs/e2e
docker compose up -d
```

**预期**：容器 `bitfs-regtest` 启动。

**验证连通性**：

```bash
# 等待节点就绪
sleep 5

# 测试 RPC
curl -s -u bitfs:bitfs --data-binary \
  '{"jsonrpc":"1.0","method":"getblockchaininfo","params":[]}' \
  http://localhost:18332/ | python3 -m json.tool
```

**预期**：返回 JSON，含 `"chain": "regtest"`、`"blocks": 0`（或少量区块）。

### 3.1 生成初始区块

Regtest 需要手动挖矿。先生成 101 个区块使 coinbase 成熟：

```bash
# 生成一个地址用于接收挖矿奖励
MINING_ADDR=$(curl -s -u bitfs:bitfs --data-binary \
  '{"jsonrpc":"1.0","method":"getnewaddress","params":[]}' \
  http://localhost:18332/ | python3 -c "import sys,json; print(json.load(sys.stdin)['result'])")

echo "Mining address: $MINING_ADDR"

# 挖 101 个区块（第 1 个 coinbase 需 100 个确认才能花费）
curl -s -u bitfs:bitfs --data-binary \
  "{\"jsonrpc\":\"1.0\",\"method\":\"generate\",\"params\":[101]}" \
  http://localhost:18332/ | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(f'Generated {len(r)} blocks')"
```

**预期**：`Generated 101 blocks`。

---

## 阶段 4：注资钱包

### 4.1 获取 BitFS 钱包的 fee address

```bash
bitfs wallet show --datadir $TESTDIR --password test123
```

记下输出中的 **fee address**（hex 压缩公钥）。接下来要将 BSV 发送到这个地址对应的 P2PKH 地址。

> **注意**：fee address 是公钥的 hex，不是 Base58 地址。你需要将其转换或直接使用 `fund` 命令手动注册 UTXO。

### 4.2 方式 A：通过 RPC 发送资金

```bash
# 从 regtest 节点发送 0.01 BSV (1,000,000 sats) 到新地址
# 先获取一个 regtest 地址
RECV_ADDR=$(curl -s -u bitfs:bitfs --data-binary \
  '{"jsonrpc":"1.0","method":"getnewaddress","params":[]}' \
  http://localhost:18332/ | python3 -c "import sys,json; print(json.load(sys.stdin)['result'])")

# 发送资金
FUND_TXID=$(curl -s -u bitfs:bitfs --data-binary \
  "{\"jsonrpc\":\"1.0\",\"method\":\"sendtoaddress\",\"params\":[\"$RECV_ADDR\",0.01]}" \
  http://localhost:18332/ | python3 -c "import sys,json; print(json.load(sys.stdin)['result'])")

echo "Fund TxID: $FUND_TXID"

# 挖一个区块确认
curl -s -u bitfs:bitfs --data-binary \
  "{\"jsonrpc\":\"1.0\",\"method\":\"generate\",\"params\":[1]}" \
  http://localhost:18332/ > /dev/null
```

### 4.3 方式 B：使用 `fund` 命令手动注册

如果你已经知道某个 UTXO 对应 wallet fee key 的输出，可以直接注册：

```bash
bitfs fund \
  --txid <64字符hex-txid> \
  --vout 0 \
  --amount 1000000 \
  --datadir $TESTDIR --password test123
```

**预期**：显示成功注册 UTXO，输出公钥信息。

### 4.4 验证余额

```bash
bitfs wallet balance --datadir $TESTDIR --password test123
```

**预期**：显示注册的余额（如 1,000,000 sats）。

---

## 阶段 5：文件操作（构建交易）

> **重要**：以下命令会构建并输出原始交易 hex。在没有连接 daemon 的情况下，交易不会自动广播。

### 5.1 创建目录

```bash
bitfs mkdir /docs --datadir $TESTDIR --password test123
```

**预期**：输出 TxID 和 raw transaction hex。

### 5.2 上传文件

先创建一个测试文件：

```bash
echo "Hello, BitFS!" > /tmp/test-hello.txt
echo '{"name":"test","version":1}' > /tmp/test-data.json
dd if=/dev/urandom of=/tmp/test-binary.bin bs=1024 count=10 2>/dev/null
```

```bash
# 上传文本文件（free 模式）
bitfs put /tmp/test-hello.txt /hello.txt --datadir $TESTDIR --password test123

# 上传到子目录
bitfs put /tmp/test-data.json /docs/data.json --datadir $TESTDIR --password test123

# 上传私有文件
bitfs put /tmp/test-binary.bin /secret.bin --access private --datadir $TESTDIR --password test123
```

**预期**：每次操作成功输出 TxID + raw hex。

### 5.3 指定 vault 上传

```bash
bitfs put /tmp/test-hello.txt /projects/readme.txt \
  --vault projects --datadir $TESTDIR --password test123
```

**预期**：成功构建交易，使用 projects vault 的密钥。

### 5.4 复制文件

```bash
bitfs cp /hello.txt /hello-copy.txt --datadir $TESTDIR --password test123
```

**预期**：输出 TxID + raw hex。

### 5.5 移动/重命名文件

```bash
bitfs mv /hello-copy.txt /docs/hello-moved.txt --datadir $TESTDIR --password test123
```

**预期**：输出 TxID + raw hex。

### 5.6 创建链接

```bash
# 硬链接
bitfs link /hello.txt /hello-link --datadir $TESTDIR --password test123

# 软链接
bitfs link --soft /hello.txt /hello-symlink --datadir $TESTDIR --password test123
```

**预期**：各输出 TxID + raw hex。

### 5.7 删除文件

```bash
bitfs rm /docs/hello-moved.txt --datadir $TESTDIR --password test123
```

**预期**：输出 TxID + raw hex。

### 5.8 边界测试

```bash
# 上传不存在的文件
bitfs put /tmp/nonexistent.file /foo --datadir $TESTDIR --password test123
# 预期: 报错 "local file not found"

# 缺少参数
bitfs put /tmp/test-hello.txt --datadir $TESTDIR --password test123
# 预期: 用法提示

# 无效的 access 模式
bitfs put /tmp/test-hello.txt /foo --access paid --datadir $TESTDIR --password test123
# 预期: 报错 "access must be 'free' or 'private'"
```

---

## 阶段 6：交易命令

### 6.1 设置售价

```bash
bitfs sell /hello.txt --price 100 --datadir $TESTDIR --password test123
```

**预期**：输出 TxID + raw hex（设置 100 sats/KB 的价格）。

### 6.2 加密内容

```bash
# 将 free 内容转为 private
bitfs encrypt /docs/data.json --datadir $TESTDIR --password test123
```

**预期**：输出 TxID + raw hex。

### 6.3 边界测试

```bash
# sell 缺少 --price
bitfs sell /hello.txt --datadir $TESTDIR --password test123
# 预期: 报错，price 是必需的

# sell 价格为 0 或负数
bitfs sell /hello.txt --price 0 --datadir $TESTDIR --password test123
# 预期: 报错
```

---

## 阶段 7：发布命令

### 7.1 查看域名绑定

```bash
bitfs publish --datadir $TESTDIR --password test123
```

**预期**：无参数时列出当前域名绑定（应为空）。

### 7.2 绑定域名

```bash
bitfs publish example.bitfs.org --datadir $TESTDIR --password test123
```

**预期**：显示 DNSLink 配置指引，告诉你需要设置的 DNS TXT 记录。

### 7.3 解除绑定

```bash
bitfs unpublish example.bitfs.org --datadir $TESTDIR --password test123
```

**预期**：显示移除 DNS TXT 记录的指引。

---

## 阶段 8：Daemon 模式

### 8.1 启动 daemon

```bash
bitfs daemon start \
  --listen :8080 \
  --datadir $TESTDIR \
  --password test123 \
  --rpc-url http://localhost:18332 \
  --rpc-user bitfs \
  --rpc-pass bitfs \
  --network regtest &

# 等待启动
sleep 2
```

**预期**：daemon 启动，监听 :8080。PID 文件写入 `$TESTDIR/daemon.pid`。

**验证**：

```bash
cat $TESTDIR/daemon.pid
# 预期: 进程 PID 数字

# 检查端口
lsof -i :8080
# 预期: 显示 bitfs 进程监听
```

### 8.2 停止 daemon

```bash
bitfs daemon stop --datadir $TESTDIR
```

**预期**：daemon 进程被终止。

```bash
lsof -i :8080
# 预期: 无输出（端口已释放）
```

---

## 阶段 9：只读工具（需要 daemon 运行）

> 以下命令需要 daemon 在 :8080 运行。先确保 daemon 已启动（见阶段 8.1）。

### 9.1 bls — 列出目录

```bash
# 列出根目录
bls --host http://localhost:8080 <pubkey>:/

# 详细列表
bls -l --host http://localhost:8080 <pubkey>:/

# JSON 输出
bls --json --host http://localhost:8080 <pubkey>:/docs
```

> 其中 `<pubkey>` 是你 default vault 的公钥 hex。

**预期**：列出目录内容（如果 daemon 中有数据）或返回空。

### 9.2 bstat — 文件元数据

```bash
bstat --host http://localhost:8080 <pubkey>:/hello.txt

# JSON 格式
bstat --json --host http://localhost:8080 <pubkey>:/hello.txt
```

**预期**：显示文件类型、大小、hash、access mode 等信息。

### 9.3 bcat — 输出文件内容

```bash
bcat --host http://localhost:8080 <pubkey>:/hello.txt
```

**预期**：输出 `Hello, BitFS!`

### 9.4 bget — 下载文件

```bash
bget -o /tmp/downloaded.txt --host http://localhost:8080 <pubkey>:/hello.txt
cat /tmp/downloaded.txt
```

**预期**：下载文件到本地，内容为 `Hello, BitFS!`

### 9.5 btree — 目录树

```bash
btree --host http://localhost:8080 <pubkey>:/

# 限制深度
btree -d 1 --host http://localhost:8080 <pubkey>:/

# JSON 格式
btree --json --host http://localhost:8080 <pubkey>:/
```

**预期**：显示 ASCII 树形目录结构。

### 9.6 超时测试

```bash
# 错误的 host
bls --host http://localhost:9999 --timeout 3s <pubkey>:/
# 预期: 3秒后超时，退出码 4
echo $?
```

---

## 阶段 10：SPV 验证

```bash
# 验证一个已上链的交易
bitfs verify <txid> \
  --datadir $TESTDIR --password test123 \
  --rpc-url http://localhost:18332 \
  --rpc-user bitfs --rpc-pass bitfs \
  --network regtest
```

**预期**：显示交易确认状态和区块高度。

---

## 阶段 11：交互式 Shell

```bash
bitfs shell --datadir $TESTDIR --password test123
```

进入 FTP 风格的 REPL。测试以下命令：

```
bitfs> help
# 预期: 列出所有可用命令

bitfs> pwd
# 预期: /

bitfs> ls
# 预期: 列出根目录内容

bitfs> mkdir /shell-test
# 预期: 成功创建目录

bitfs> cd /shell-test
bitfs> pwd
# 预期: /shell-test

bitfs> lcd /tmp
# 预期: 切换本地目录到 /tmp

bitfs> put test-hello.txt /shell-test/from-shell.txt
# 预期: 构建交易

bitfs> ls
# 预期: 显示 from-shell.txt

bitfs> cd /
bitfs> ls

bitfs> quit
# 预期: 退出 shell
```

### 11.1 Shell 边界测试

```
bitfs> cd /nonexistent
# 预期: 报错或提示目录不存在

bitfs> unknowncmd
# 预期: 提示未知命令

bitfs> exit
# 预期: 正常退出（exit 和 quit 都应工作）
```

---

## 阶段 12：错误密码测试

```bash
bitfs wallet show --datadir $TESTDIR --password wrongpassword
# 预期: 报错，解密失败

bitfs put /tmp/test-hello.txt /foo --datadir $TESTDIR --password wrongpassword
# 预期: 报错
```

---

## 阶段 13：帮助信息完整性

逐一检查所有命令的 `--help`：

```bash
bitfs wallet --help
bitfs wallet init --help
bitfs vault --help
bitfs put --help
bitfs mkdir --help
bitfs rm --help
bitfs mv --help
bitfs cp --help
bitfs link --help
bitfs sell --help
bitfs encrypt --help
bitfs publish --help
bitfs unpublish --help
bitfs daemon --help
bitfs fund --help
bitfs verify --help
bitfs shell --help
```

**预期**：每个命令都输出用法说明，无 panic。

---

## 清理

```bash
# 停止 daemon（如果还在运行）
bitfs daemon stop --datadir $TESTDIR 2>/dev/null

# 停止 regtest 节点
cd ~/Codes/RabbitHole/bitfs/e2e && docker compose down

# 删除测试数据
rm -rf $TESTDIR
rm -f /tmp/test-hello.txt /tmp/test-data.json /tmp/test-binary.bin /tmp/downloaded.txt
```

---

## 退出码速查

| 码 | 含义 | 触发场景 |
|----|------|----------|
| 0 | 成功 | 命令正常完成 |
| 1 | 一般错误 | 内部错误 |
| 2 | 用法错误 | 参数缺失/无效 |
| 3 | 钱包错误 | 密码错误、钱包未初始化 |
| 4 | 网络错误 | 连接超时、RPC 失败 |
| 5 | 权限错误 | 无权访问 |
| 6 | 未找到 | 文件/目录不存在 |
| 7 | 冲突 | 名称重复等 |

每步执行后可用 `echo $?` 检查退出码。

---

## 测试检查清单

- [ ] 阶段 0：构建成功，6 个二进制文件
- [ ] 阶段 1：钱包初始化、查看、余额
- [ ] 阶段 2：保险库 CRUD
- [ ] 阶段 3：Regtest 节点启动
- [ ] 阶段 4：钱包注资
- [ ] 阶段 5：文件操作（put/mkdir/rm/mv/cp/link）
- [ ] 阶段 6：交易命令（sell/encrypt）
- [ ] 阶段 7：发布命令
- [ ] 阶段 8：Daemon 启停
- [ ] 阶段 9：只读工具（bls/bstat/bcat/bget/btree）
- [ ] 阶段 10：SPV 验证
- [ ] 阶段 11：交互式 Shell
- [ ] 阶段 12：错误密码测试
- [ ] 阶段 13：帮助信息完整性
- [ ] 清理完成

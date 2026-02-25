# BitFS 实现任务分解

任务按依赖关系排序。每个阶段建立在前一阶段之上。估计总共约 938 个测试用例（依据测试设计文档）。

---

## 第一阶段：基础（密码学 + 钱包 + 交易）

### 任务 1：libbitfs/method42 -- Method 42 ECDH 加密引擎
- **包**：`libbitfs/method42/`
- **文件**：`encrypt.go`、`ecdh.go`、`kdf.go`、`access.go`、`method42_test.go`
- **描述**：实现核心加密系统。secp256k1 上的 ECDH 密钥交换、HKDF-SHA256 密钥推导、AES-256-GCM 加密/解密、三种访问模式（私有/免费/付费）、用于 HTLC 的胶囊（Capsule）计算、模式间重加密。
- **验收标准**：
  - [x] ComputeKeyHash 返回 SHA256(SHA256(plaintext))
  - [x] ECDH 计算正确的共享密钥 x 坐标
  - [x] DeriveAESKey 通过 HKDF 生成确定性的 32 字节密钥
  - [x] Encrypt/Decrypt 对所有三种访问模式往返成功
  - [x] FreePrivateKey（标量 1）产生可复现的加密结果
  - [x] DecryptWithCapsule 对买方流程正常工作
  - [x] ReEncrypt 正确转换 FREE 和 PRIVATE 之间的模式
  - [x] 密钥哈希验证能捕获被篡改的内容
  - [x] 所有错误条件产生正确的错误类型
- **估计测试数**：45
- **依赖**：go-sdk（ec 原语）、golang.org/x/crypto（hkdf）

### 任务 2：libbitfs/wallet -- HD 钱包（BIP32/BIP39）
- **包**：`libbitfs/wallet/`
- **文件**：`seed.go`、`hd.go`、`vault.go`、`network.go`、`wallet_test.go`
- **描述**：BIP39 助记词生成/验证、使用 BitFS 路径方案（m/44'/236'/...）的 BIP32 密钥派生、Argon2id 种子加密、保险库 CRUD、手续费密钥链派生、网络配置。
- **验收标准**：
  - [x] GenerateMnemonic 产生有效的 12/24 词助记词
  - [x] SeedFromMnemonic 具有确定性（相同输入 -> 相同种子）
  - [x] EncryptSeed/DecryptSeed 使用正确密码往返成功
  - [x] DecryptSeed 使用错误密码失败（ErrDecryptionFailed）
  - [x] 校验和验证能捕获损坏的数据
  - [x] DeriveNodeKey 产生正确路径 m/44'/236'/(V+1)'/0/0/...
  - [x] DeriveFeeKey 产生正确路径 m/44'/236'/0'/chain/index
  - [x] 硬化与非硬化派生正确工作
  - [x] MaxFileIndex 和 MaxPathDepth 限制被执行
  - [x] 保险库创建/列出/重命名/删除操作
  - [x] 网络配置（mainnet/testnet/regtest）正确加载
  - [x] 相同助记词+口令始终产生相同密钥
- **估计测试数**：55
- **依赖**：go-sdk（bip32、bip39、ec）、golang.org/x/crypto（argon2）

### 任务 3：libbitfs/tx -- BSV 交易构建
- **包**：`libbitfs/tx/`
- **文件**：`metanet_tx.go`、`utxo.go`、`opreturn.go`、`tx_test.go`
- **描述**：构建四种 Metanet 交易模板（CreateRoot、CreateChild、SelfUpdate、DataTransaction）。OP_RETURN 构建与解析。自维持链的 UTXO 追踪。手续费估算。
- **验收标准**：
  - [x] BuildCreateRoot 产生具有正确 OP_RETURN 格式的有效交易
  - [x] BuildCreateChild 花费 P_parent UTXO 并在 Output 2 刷新它
  - [x] BuildSelfUpdate 在更新间保持 ParentTxID 不变
  - [x] BuildDataTransaction 使用 OP_DROP 嵌入内容
  - [x] BuildOPReturn/ParseOPReturn 往返正确
  - [x] MetaFlag (0x6d657461) 被正确放置
  - [x] 所有 P2PKH 输出执行粉尘限额（546 聪）
  - [x] 手续费估算产生合理的值
  - [x] UTXO 追踪正确跟随刷新链
  - [x] 资金不足错误信息清晰
- **估计测试数**：40
- **依赖**：go-sdk（transaction、script、ec）、libbitfs/wallet

---

## 第二阶段：文件系统（DAG + 验证 + 存储）

### 任务 4：libbitfs/metanet -- Metanet DAG 解析器
- **包**：`libbitfs/metanet/`
- **文件**：`node.go`、`parser.go`、`resolve.go`、`directory.go`、`link.go`、`metanet_test.go`
- **描述**：将 Metanet 交易解析为 Node 结构体，实现 Unix 文件系统操作（路径解析、目录列表、链接跟踪），版本解析（最高区块高度 + TTOR），价格继承。
- **验收标准**：
  - [x] ParseNode 提取 P_node、ParentTxID、TLV 载荷
  - [x] ResolvePath 正确遍历目录
  - [x] "." 和 ".." 导航正常工作（.. 不能跳出根目录）
  - [x] 软链接跟踪最大深度 10
  - [x] 硬链接检测（同一 P_node，多个 ChildEntry）
  - [x] 远程软链接返回适当的错误
  - [x] LatestVersion 正确按区块高度然后 TTOR 排序
  - [x] AddChild/RemoveChild/RenameChild 目录操作
  - [x] NextChildIndex 单调递增（已删除索引永不复用）
  - [x] InheritPricePerKB 沿目录树向上查找
  - [x] 三种节点类型（FILE/DIR/LINK）正确解析
- **估计测试数**：65
- **依赖**：libbitfs/tx、go-sdk（ec）

### 任务 5：libbitfs/spv -- SPV 轻客户端
- **包**：`libbitfs/spv/`
- **文件**：`merkle.go`、`header.go`、`verify.go`、`store.go`、`spv_test.go`
- **描述**：Merkle 证明验证、区块头链验证、完整 SPV 验证链（交易完整性 -> Merkle 证明 -> 区块头 -> 最长链）。区块头和交易存储接口。
- **验收标准**：
  - [x] VerifyMerkleProof 从分支正确计算根
  - [x] ComputeMerkleRoot 处理奇数/偶数叶节点数
  - [x] VerifyTransaction 完成完整的 4 步验证链
  - [x] VerifyHeaderChain 验证 PrevBlock 链接
  - [x] SerializeHeader/DeserializeHeader 往返正确（80 字节）
  - [x] DoubleHash 匹配已知的 BSV 区块哈希
  - [x] 未确认交易被正确标记
  - [x] 无效证明被适当的错误拒绝
- **估计测试数**：35
- **依赖**：crypto/sha256

### 任务 6：libbitfs/storage -- 内容存储
- **包**：`libbitfs/storage/`
- **文件**：`store.go`、`filestore.go`、`storage_test.go`
- **描述**：基于文件的内容寻址存储。扁平键值存储，key_hash 映射到密文文件，按哈希第一个字节进行目录分片。
- **验收标准**：
  - [x] 各种内容大小的 Put/Get 往返
  - [x] Has 返回正确的存在性检查
  - [x] Delete 移除内容
  - [x] Size 返回正确的字节数
  - [x] List 返回所有已存储的哈希
  - [x] 目录分片创建正确的子目录
  - [x] 无效 key hash（非 32 字节）被拒绝
  - [x] 并发访问安全
- **估计测试数**：25
- **依赖**：os、encoding/hex

---

## 第三阶段：网络（身份 + 支付 + 守护进程）

### 任务 7：libbitfs/paymail -- Paymail 身份解析
- **包**：`libbitfs/paymail/`
- **文件**：`uri.go`、`resolve.go`、`dns.go`、`paymail_test.go`
- **描述**：解析 bitfs:// URI，检测地址类型（Paymail/@、DNSLink、裸公钥），DNS SRV/TXT 解析，Paymail 能力发现，PKI 解析。
- **验收标准**：
  - [x] ParseURI 正确分类所有三种地址类型
  - [x] Paymail URI 提取别名和域名
  - [x] DNSLink URI 提取域名
  - [x] 裸公钥 URI 提取压缩密钥字节
  - [x] 无效 URI 产生清晰的错误
  - [x] 路径组件解析处理边缘情况
  - [x] DNS 解析（SRV、TXT）带超时处理
  - [x] Paymail 能力从 .well-known 发现
  - [x] PKI 解析返回有效公钥
- **估计测试数**：35
- **依赖**：net、net/http、net/url

### 任务 8：libbitfs/x402 -- x402 支付协议
- **包**：`libbitfs/x402/`
- **文件**：`invoice.go`、`headers.go`、`htlc.go`、`verify.go`、`x402_test.go`
- **描述**：发票创建、HTTP 402 头部、HTLC 脚本构建、支付验证。
- **验收标准**：
  - [x] CalculatePrice 正确计算 ceil(pricePerKB * size / 1024)
  - [x] NewInvoice 生成带过期时间的有效发票
  - [x] SetPaymentHeaders/ParsePaymentHeaders 往返正确
  - [x] BuildHTLC 产生正确的 IF/ELSE/ENDIF 脚本
  - [x] VerifyPayment 验证交易输出与发票匹配
  - [x] 过期发票被拒绝
  - [x] ParseHTLCPreimage 从花费交易中提取胶囊
- **估计测试数**：30
- **依赖**：go-sdk（transaction、script）、libbitfs/method42

### 任务 9：internal/daemon -- BitFS 守护进程（LFCP）
- **包**：`internal/daemon/`
- **文件**：`daemon.go`、`routes.go`、`handshake.go`、`content.go`、`webmcp.go`、`daemon_test.go`
- **描述**：HTTP 服务器包含所有端点、Method 42 握手、内容协商、x402 支付流程、WebMCP 声明、Paymail 服务器能力。
- **验收标准**：
  - [x] 健康检查端点返回 200
  - [x] 内容协商根据 Accept 头部返回 HTML/Markdown/JSON
  - [x] 免费内容直接提供
  - [x] 付费内容返回 402 并带正确头部
  - [x] Method 42 握手建立认证会话
  - [x] HTLC 购买流程：capsule_hash -> 提交 HTLC -> 揭示 capsule
  - [x] x402 支付验证接受有效交易
  - [x] HTML 响应中包含 WebMCP 表单
  - [x] Paymail .well-known 端点提供能力声明
  - [x] 速率限制被执行
  - [x] 优雅关闭
- **估计测试数**：80
- **依赖**：所有 internal 包、net/http

---

## 第四阶段：CLI（用户界面）

### 任务 10：cmd/bitfs -- 主 CLI
- **包**：`cmd/bitfs/`
- **文件**：`main.go`、`cmd_put.go`、`cmd_mkdir.go`、`cmd_rm.go`、`cmd_mv.go`、`cmd_cp.go`、`cmd_link.go`、`cmd_sell.go`、`cmd_encrypt.go`、`cmd_vault.go`、`cmd_wallet.go`、`cmd_publish.go`、`cmd_daemon.go`、`cmd_shell.go`、集成测试
- **描述**：完整 CLI 实现，包含所有子命令、Cobra 命令树、Viper 配置、交互式 shell（FTP 风格 REPL）。
- **验收标准**：
  - [x] 所有子命令正确解析参数
  - [x] --json 标志产生有效 JSON 输出
  - [x] 退出码遵循规范
  - [x] 错误消息清晰且可操作
  - [x] Shell 模式支持所有文档化的命令
  - [x] Shell 支持本地/远程导航（lcd/cd）
  - [x] 密码提示使用终端（非 stdin 回显）
  - [x] BITFS_HOME 环境变量覆盖正常工作
  - [x] 配置文件被正确读取
- **估计测试数**：120
- **依赖**：cobra、viper、所有 internal 包

### 任务 11：cmd/b* -- 只读工具
- **包**：`cmd/bls/`、`cmd/bcat/`、`cmd/bget/`、`cmd/bstat/`、`cmd/btree/`
- **文件**：每个包中的 `main.go`，共享的 `internal/client/` 工具库
- **描述**：五个独立的只读文件系统访问二进制文件。每个封装共享客户端代码并提供特定的输出格式化。
- **验收标准**：
  - [x] bls 对目录产生 ls 风格输出
  - [x] bcat 将文件内容输出到 stdout
  - [x] bget 将文件下载到本地文件系统
  - [x] bstat 显示文件元数据（大小、哈希、所有者、时间、访问权限）
  - [x] btree 显示递归目录树
  - [x] 所有工具支持 --json 标志
  - [x] --buy 标志触发购买流程
  - [x] --offline 仅使用缓存
  - [x] URI 解析处理所有三种地址类型
  - [x] 免费内容自动解密
- **估计测试数**：75
- **依赖**：cobra、libbitfs/paymail、libbitfs/method42、libbitfs/x402

---

## 阶段汇总

| 阶段 | 包 | 估计测试数 | 累计 |
|------|------|-----------|------|
| 1 基础 | method42, wallet, tx | 140 | 140 |
| 2 文件系统 | metanet, spv, storage | 125 | 265 |
| 3 网络 | paymail, x402, daemon | 145 | 410 |
| 4 CLI | cmd/bitfs, cmd/b* | 195 | 605 |
| 集成测试 | 跨包 | ~333 | ~938 |

**总计估计**：约 938 个测试用例（与测试设计文档一致）。

---

## 实现注意事项

1. **go-sdk 是唯一的 BSV 依赖**：`github.com/bsv-blockchain/go-sdk`。不使用其他 BSV 库。
2. **测试**：使用 `github.com/stretchr/testify` 的表驱动测试。每个包都有全面的单元测试。
3. **TLV**：`BitFSPayload` TLV 编码格式根据 SystemDesign 第 4 节的模式定义。
4. **错误包装**：使用 `fmt.Errorf("context: %w", err)` 构建错误链。
5. **上下文传播**：长时间运行的操作接受 `context.Context` 以支持取消。

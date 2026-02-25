# BitFS 白皮书大纲

> 本文件是 BitFS 白皮书的内容大纲（中文），作为生成中英文白皮书的唯一来源。
> 工作流：修改本大纲 → 生成 BitFS-Whitepaper.zh.md + BitFS-Whitepaper.en.md

---

## 元信息

- **标题**: BitFS: 基于区块链的点对点加密文件系统
- **英文标题**: BitFS: A Peer-to-Peer Encrypted File System on Blockchain
- **作者**: Alex Tong — alex@bitfs.org

---

## 摘要

核心主张（一段话概括全文）：

- 去中心化文件系统，Unix 语义映射到区块链 DAG
- 所有文件默认 Method 42 ECDH 加密
- 访问控制不取决于"是否加密"，而取决于"谁能派生解密密钥"
- HD 钱包镜像文件系统树，提供稳定节点身份 + 确定性密钥派生
- key_hash = SHA256(SHA256(plaintext)) → 内容寻址存储 + 密钥派生；元数据 → Metanet 交易
- HTLC 原子交换实现无信任数据交易（卖方揭示密钥胶囊 = 哈希原像）
- SPV 模式运行，从不查询区块链
- Agent 优先 HTTP 接口 + x402 支付协议 → AI Agent 自主发现/浏览/购买

---

## 1. 引言

**问题陈述**：现有去中心化存储的根本矛盾
- IPFS：内容寻址但无原生加密或支付
- Filecoin：有经济激励但存储验证需要计算密集 zk 证明
- 中心化云存储：方便但需信任单一实体

**三个关键观察**：
1. 元数据与内容需要不同保障 → 分离存储（元数据上链，内容本地）
2. 加密应默认而非可选 → 公开/私有仅是密钥分发差异
3. AI Agent 是一等公民 → 机器可读接口，无需人类中介

**BitFS 一句话定义**：Unix 文件系统 + 区块链公钥 inode + 确定性 ECDH 加密，单一密码学框架（Method 42）统一提供加密、访问控制、认证、交易和存储证明。

---

## 2. Metanet 即文件系统

**Metanet 协议本质**：BSV 区块链上的 DAG，每个节点 = 一笔 OP_RETURN 交易，携带 P_node + 父交易引用。边通过密码学建立（子交易包含父节点私钥签名的输入）。

**Unix → BitFS 映射表**：

| Unix | BitFS |
|------|-------|
| inode | P_node（压缩公钥，33 字节）|
| 目录项 | ChildEntry(index, name, type, pubkey) |
| inode 编号 | index（每目录单调递增）|
| `..`（父目录）| 载荷中的 parent 字段 |

**关键设计**：
- 文件名仅存在于父目录子节点列表 → 与 Unix inode 不含文件名一致
- 三种节点类型：FILE / DIR / LINK
  - FILE：key_hash, MIME, size
  - DIR：children 列表 + next_child_index
  - LINK：link_target (HARD=TxID, SOFT=P_node, SOFT_REMOTE=domain/path)
- 硬链接 → 固定版本；软链接 → 最新版本；远程软链接 → DNS 跨越信任边界
- 版本控制内在：同一 P_node 多笔交易，区块高度最大者为当前版本

---

## 3. HD 密钥派生

**BIP32 HD 钱包结构**：
```
m/44'/236'/0'         费用密钥链（所有 Vault 共享）
m/44'/236'/1'/0/0     Vault #0 根目录
m/44'/236'/1'/0/0/1   根目录第一个子节点
m/44'/236'/1'/0/0/1/3 第一个子节点的第三个子节点
m/44'/236'/2'/0/0     Vault #1 根目录（独立树）
```

**Vault 概念**：
- 一棵以 BIP32 账户为根的独立 Metanet 树
- 多 Vault 共享种子，维护独立目录层次
- 每个 Vault 可绑定独立域名

**两个核心属性**：
1. 稳定节点身份 → P_node 不随内容更新变化 → 支持持久引用
2. 确定性恢复 → 从 BIP39 助记词重建密钥层次（交易数据需从备份恢复）

---

## 4. 统一加密

**Method 42 ECDH 密钥派生步骤**：
```
1. key_hash = SHA256(SHA256(plaintext))                  双重哈希（密钥派生 + 内容承诺）
2. point = ECDH(D_node, P_node) = D_node × P_node       椭圆曲线 Diffie-Hellman
3. aes_key = HKDF-SHA256(point.x, key_hash)              KDF 派生 AES-256-GCM 密钥
4. ciphertext = AES-GCM(plaintext, aes_key)
```

**变量说明**：D_node = BIP32 派生的节点私钥，P_node = D_node × G = 节点公钥（即 Metanet inode），key_hash = SHA256(SHA256(plaintext)) 兼做密钥派生盐值和内容承诺标识

**关键设计**：
- D_node 直接使用 BIP32 密钥，保留代数关系 → 支持目录树级 capsule 派生（设计决策 #66）
- 仅需记录一个哈希 key_hash：双重哈希不暴露原始数据哈希（设计决策 #54），兼做密钥派生和内容寻址（设计决策 #12）

**三种访问级别**（同一机制）：

| 访问类型 | 密钥派生 | 谁能解密 |
|---------|---------|---------|
| 私有 | aes_key = KDF(ECDH(D_node, D_node×G), key_hash) | 仅所有者（自加密） |
| 免费 | aes_key = KDF(P_node, key_hash)（平凡密钥） | 知道 P_node 的任何人 |
| 付费 | 标准 Method 42 ECDH capsule 交换 | 买方（HTLC 交换后）|

**平凡密钥技巧**：免费数据使用 P_node 作为 KDF 输入 → aes_key = KDF(P_node, key_hash)。P_node 通过 DNS 公开 → 任何人可派生解密密钥，但磁盘上仍加密 → 统一存储模型。

**私有数据**：整个 Metanet 载荷用所有者对称密钥加密 → 文件名/大小/时间戳/目录结构在链上不可见。

---

## 5. 内容寻址存储

- 扁平键值映射：`~/.bitfs/data/{key_hash} → 密文字节`
- Daemon 通过 HTTP `GET /data/{hash}` 提供内容
- 无需外部依赖（如 IPFS）
- 所有内容已加密 → 存储层仅处理不透明字节序列
- 去重自然发生：密文相同 → 共享 key_hash 和单一存储副本

---

## 6. 交易结构

**每个文件系统操作 = 一笔 Metanet 交易**：
```
输入:
  [0] 花费锁定到 P_parent 的 UTXO → Sig(D_parent) 创建 Metanet 边
  [1] 花费费用密钥链 UTXO → 支付矿工费

输出:
  [0] OP_RETURN: <MetaFlag> <P_node> <TxID_parent> <TLV 载荷>
  [1] P2PKH → P_node     (546 sat，节点可花费输出)
  [2] P2PKH → P_parent   (546 sat，刷新父节点 UTXO)
  [3] P2PKH → 找零地址
```

**关键设计**：
- 输出[2] 刷新父节点 UTXO → 自持续 UTXO 链，无需预先充值
- TLV 载荷编码：节点类型、操作(CREATE/UPDATE/DELETE)、内容元数据、访问控制、目录子节点、可选字段(关键词/描述/域名绑定)
- 定价：price_per_kb (satoshis/KB)，支持目录继承

---

## 7. 无信任数据交易

**HTLC 原子交换流程**：
```
1. 买方连接卖方 Daemon
2. Method 42 ECDH 握手（双向身份验证）
3. 买方请求目标文件的 capsule_hash
4. 卖方计算 capsule + capsule_hash
5. 卖方返回 capsule_hash + payment_address
6. 买方创建 HTLC 交易（SHA256 原像验证 + 超时退款）
7. 买方广播 HTLC
8. 卖方揭示 capsule（原像）→ 领取付款
9. 买方从链上读取 capsule → 派生解密密钥 → 解密文件
```

**原子性保证**：卖方收到付款 ↔ 买方收到密钥胶囊。无需可信中介。

**Method 42 握手**：
- 交换 P_buyer/P_seller + nonce + timestamp
- 双方计算 session_key = SHA256(ECDH.x || nonce_b || nonce_s)
- HMAC 验证。卖方 P_seller 必须与 DNS 公布的 P_node 一致 → 防 MITM

**卖方完全无状态**：不维护买家数据库、不保持会话、不记录历史。

---

## 8. SPV 模式

**完全以 SPV 模式运行**：
- 所有者存储自建交易 + Merkle 证明
- 访问者从 Daemon 获取元数据，从不查询区块链
- 通过 Merkle 证明 + 区块头链验证

**本地存储结构**：
```
~/.bitfs/
├── wallet.db            加密的 HD 密钥 + UTXO 集合
├── txstore/{txid}.tx    完整交易数据
├── txstore/{txid}.proof Merkle 证明
├── headers/             区块头链
└── cache/               访问者元数据 + 解密内容 + 密钥胶囊缓存
```

- 第三方索引仅在 Daemon 不可达时作降级备用
- 种子恢复 → 还原 HD 密钥，交易数据需从备份恢复

---

## 9. DNS 解析与发布

**两种 DNS 记录**：
- `_bitfs_pubkey.example.com TXT "02a1b2c3..."` — P_node 身份
- `_bitfs._tcp.example.com SRV 10 60 443 cdn1.example.com` — 服务端点

**双向验证**：DNS TXT → P_node，Metanet 载荷 domain 字段 → 域名。两者必须一致。

**SRV 负载均衡**：priority + weight → 类 CDN 分发，多端点声明。

**URI 解析流程**：
```
bitfs://example.com/docs/readme.txt
  → DNS TXT → P_node
  → DNS SRV → 端点列表
  → 连接最优端点，Method 42 握手
  → 请求元数据(SPV) → 验证域名绑定
  → 遍历目录树 → 返回内容
```

- 公钥直接寻址 `bitfs://<pubkey>/path` 可绕过 DNS

---

## 10. Agent 优先接口

**内容协商**（同一端点服务人类和 AI Agent）：

| Accept 头 | 响应 |
|-----------|------|
| text/html | HTML + WebMCP 工具声明 |
| text/markdown | CLI 使用指南 |
| application/json | 结构化元数据 |

**付费内容 HTTP 402**：
- 响应头：X-Price, X-Price-Per-KB, X-File-Size, X-Invoice-Id
- 人类看到付费墙
- 浏览器 AI Agent 发现 WebMCP → 自主支付
- CLI Agent 收到命令模板 `bget --buy bitfs://...` → 执行

**核心洞察**：有钱包的 Agent 可透明完成微支付。402 从访问壁垒变为可编程支付 API。

---

## 11. 双模式命令行

**只读工具（b*）**：无状态、无需钱包、服务访问者
- bls / bcat / bget / bstat / btree — 类似 ls/cat/wget/stat/tree

**读写命令（bitfs）**：需钱包和私钥、服务所有者
- bitfs put/mkdir/rm/mv/cp/link — 文件系统操作
- bitfs sell/encrypt/decrypt — 交易与访问控制
- bitfs vault/wallet/publish/daemon — 基础设施管理
- bitfs shell — FTP 风格 REPL

- 所有工具支持 --json 和 --offline

---

## 12. 激励层

**Method 42 ECDH 同样实现存储证明**：
- 向不同 Metanet Node 分发时，每份副本使用节点特定 ECDH 密钥重新加密
- 每个节点存储的数据密码学唯一
- 验证 = 简单 Merkle 挑战-响应 → 替代 Filecoin 的 zk 证明
- 从数小时 GPU 降低到毫秒级 ECDH + AES

**Metanet Chain**（BSV 同构链）：
- 自有 PoW 共识 + 代币经济（2100 万固定供应、减半计划）
- OP_RETURN Merkle 根定期锚定到 BSV 主链
- 为 Metanet Node 激励提供经济基础
- 详见 Metanet 白皮书

---

## 13. 结论

总结要点：
- Unix 语义 → 区块链元数据
- 默认加密所有内容
- 原子交换无信任数据交易
- 单一密码学原语（ECDH 密钥派生）统一：加密 + 认证 + 支付验证 + 存储证明
- AI Agent 一等用户 + SPV 模式 → 为自主 Agent 以机器速度与去中心化服务交互的未来而设计

---

## 参考文献

[1] C. S. Wright, "An Immutable File and Data Store," nChain, 2025. (Method 42)
[2] nChain, "The Metanet Technical Summary v1.0," 2020. (Metanet DAG)
[3] P. Wuille, "BIP32: Hierarchical Deterministic Wallets," 2012.
[4] S. Nakamoto, "Bitcoin: A Peer-to-Peer Electronic Cash System," 2008. (SPV, Section 8)
[5] GB2608179A, "Multi-level Blockchain," UKIPO, 2025. (多层区块链)

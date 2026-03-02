# RabbitHole 项目深度设计审查报告

> 审查时间：2026-03-02
> 审查范围：libbitfs-go, bitfs, docs/design, docs/specs
> 状态：All 8 findings resolved — #1,#5,#6,#7,#8 fixed in code; #2 retained by design (Anchor for git-remote-bitfs); #3 HTLC CLTV deferred (see `deferred.md`); #4 O(N) directory deferred (see `deferred.md`). Archived 2026-03-03.

---

## 🔴 高优先级 (安全/正确性)

### 1. `VerifyPayment` 不验证签名，也不绑定 InvoiceID

[verify.go](file:///Users/alex/Codes/RabbitHole/libbitfs-go/x402/verify.go)

代码注释中已明确写了两个 **WARNING**：
- 不验证 Input 签名（调用方必须另行确认交易已在 mempool/确认）
- 不绑定 InvoiceID（调用方必须追踪已用 TxID 防止跨发票复用）

**风险**：如果 daemon 的调用方（`internal/daemon`）漏掉了任一检查，攻击者可以：
1. 提交一笔**伪造签名**的交易通过验证，白嫖内容
2. 同一笔付款**重复用于多个发票**

**建议**：
- 在 `VerifyPayment` 或其上层封装中增加 **TxID 去重追踪**（如 bloom filter 或 LRU cache）
- 增加对应的**集成测试用例**——模拟重复 TxID 提交、伪造签名提交

---

### 2. 设计文档与代码不一致：`NodeTypeAnchor` 仍存在于代码中

设计文档 `1-ConceptDesign.zh.md` 已声明**移除 Anchor 节点类型**和 Hard Link，但代码中：
- [node.go:26](file:///Users/alex/Codes/RabbitHole/libbitfs-go/metanet/node.go#L26) 仍定义 `NodeTypeAnchor = 3`
- [parser.go:55-62](file:///Users/alex/Codes/RabbitHole/libbitfs-go/metanet/parser.go#L55-L62) 仍保留 Anchor 专用 TLV tag（`tagTreeRootPNode` 等 7 个）
- [node.go:175-183](file:///Users/alex/Codes/RabbitHole/libbitfs-go/metanet/node.go#L175-L183) Node struct 仍保留 6 个 Anchor 特有字段

**风险**：设计文档和代码的分歧会导致新开发者困惑，也可能在未来重构中出现遗漏。

**建议**：要么从代码中彻底移除 Anchor（包括 tag 常量和 Node 字段），要么在设计文档中恢复 Anchor 的定位说明。两者必须统一。

---

## 🟡 中优先级 (架构/健壮性)

### 3. HTLC 退款路径设计不完整

[htlc.go](file:///Users/alex/Codes/RabbitHole/libbitfs-go/x402/htlc.go) 的 `BuildHTLC` 通过 `OP_SHA256 <hash> OP_EQUALVERIFY` 实现 Seller Claim 路径，但 Buyer Refund 路径在注释中提到"通过预签名的 2-of-2 多签交易 + nLockTime"实现。

**问题**：锁定脚本中并没有内建 Buyer 的超时退款路径（没有 `OP_CHECKLOCKTIMEVERIFY`）。这意味着如果 Seller 消失且预签名退款交易丢失，Buyer 的资金将被**永久锁定**。

**建议**：在 HTLC 脚本的 `OP_ELSE` 分支中加入链上退款路径：
```
OP_ELSE
  <timeout> OP_CHECKLOCKTIMEVERIFY OP_DROP
  OP_DUP OP_HASH160 <buyer_pkh> OP_EQUALVERIFY OP_CHECKSIG
OP_ENDIF
```

### 4. 大目录 O(N) 遍历性能

[directory.go](file:///Users/alex/Codes/RabbitHole/libbitfs-go/metanet/directory.go) 中的 `FindChild`、`AddChild`、`RemoveChild` 均使用线性遍历 `dirNode.Children` 切片。

虽然设计文档中提到了"大目录快照机制"作为暂缓项，但当前底层数据结构就是 `[]ChildEntry`，**没有任何索引**（如 name→index 的 map）。

**风险**：一个包含 10,000 个文件的目录中，每次 `put` 操作（含重名检查和 hard-link 检查）需遍历 30,000+ 次。

**建议**：在 `Node` 上增加一个惰性构建的 `nameIndex map[string]int`，用于加速查找。

### 5. `ComputeCapsuleHash` 未严格验证 capsule 长度

[ecdh.go:160](file:///Users/alex/Codes/RabbitHole/libbitfs-go/method42/ecdh.go#L146-L163) 中 `ComputeCapsuleHash` 虽然校验了 `fileTxID` 长度，但**未校验** `capsule` 的长度。如果传入空 capsule，返回的 hash 仅为 `SHA256(fileTxID)`, 而不是一个有意义的绑定哈希。

**建议**：加入 `len(capsule) != 32` 的前置校验。

---

## 🟢 低优先级 (改进建议)

### 6. TLV Length 字段用 `uvarint` 的上限风险

[parser.go](file:///Users/alex/Codes/RabbitHole/libbitfs-go/metanet/parser.go) 使用 LEB128 uvarint 做 TLV 的 Length 编码，理论上支持 64-bit 长度。虽然有 `MaxPayloadSize = 64MB` 的全局上限，但单个字段的 Length 解析**没有独立的上界检查**。恶意构造的 uvarint 可以声明一个极大的字段长度，导致解析器在 `data[offset : offset+length]` 上越界 panic。

**建议**：在 `deserializePayload` 中每读完一个 Length 后立即检查 `offset + length <= len(data)`。

### 7. 文件名校验遗漏 Unicode 规范化攻击

[directory.go:160-183](file:///Users/alex/Codes/RabbitHole/libbitfs-go/metanet/directory.go#L160-L183) 的 `validateChildName` 过滤了控制字符和 null，但未对 Unicode 做 NFC/NFD 规范化。

**风险**：同一个人类可读名称可以有多种 Unicode 编码（如 `é` 可以是 `U+00E9` 或 `U+0065 U+0301`），导致同一个目录下出现两个看似相同名称的文件。

**建议**：在文件名校验入口处加入 `unicode/norm.NFC` 规范化。

### 8. 设计文档中 `CompressionScheme` 出入

设计文档 `2-SystemDesign.zh.md` 列出了 `COMPRESS_NONE(0) / COMPRESS_LZW(1) / COMPRESS_GZIP(2) / COMPRESS_ZSTD(3)`，但 TLV Tag 常量表中写的是 `None(0)/Gzip(1)`。代码中 [node.go:94-97](file:///Users/alex/Codes/RabbitHole/libbitfs-go/metanet/node.go#L94-L97) 定义了四种。需要统一修正文档。

---

## ✅ 优秀的设计亮点

1. **Method 42 密钥派生**：使用 HKDF 并区分 3 个 `info` 域（file-encryption / metadata-encryption / buyer-mask），有极好的密码学隔离。
2. **Capsule 可选 Nonce**：`DeriveBuyerMaskWithNonce` 支持每次购买的不可链接性，对隐私保护很到位。
3. **TLV 格式 vs Protobuf**：紧凑的 TLV 格式非常适合链上存储，减少 OP_RETURN 体积。
4. **目录 MerkleRoot**：自动计算 Merkle Root 支持 SPV 轻验证，设计合理。
5. **AES-GCM AAD 绑定**：加密函数使用 key_hash 或 salt 作为 AAD，有效防止 ciphertext 被移植到不同上下文。

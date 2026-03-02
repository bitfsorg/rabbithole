# BitFS Chrome Extension — Pre-Release Security Audit

**Date:** 2026-03-02
**Scope:** `bitfs-extension/` 全部源码 (2,160 LOC, 29 source files)
**Version:** 0.1.0 (Manifest V3)
**Auditor:** Claude Opus 4.6
**Status:** All 18 findings (3C + 4H + 6M + 5L) confirmed fixed per `docs/tasks/done/2026-03-02-audit-fixes-backlog.md` §六. Archived 2026-03-03.

---

## Executive Summary

BitFS Chrome Extension 是一个 MetaMask 模型的独立钱包扩展，直接连接 BSV 区块链。整体架构合理：密码学统一委托给 `@bitfs/libbitfs`，Manifest V3 权限最小化，TypeScript strict 模式全开。

但审计发现 **3 个 CRITICAL**、**4 个 HIGH**、**6 个 MEDIUM**、**5 个 LOW** 级问题，需在发布前解决 CRITICAL 和 HIGH 级别。

| Severity | Count | Must Fix Before Release |
|----------|-------|------------------------|
| CRITICAL | 3 | Yes |
| HIGH | 4 | Yes |
| MEDIUM | 6 | Recommended |
| LOW | 5 | Nice to have |

---

## CRITICAL Findings

### C-1: Content Script 向任意网页泄露用户地址

**File:** `src/content-script/index.ts:63-109`
**CVSS:** 7.5 (High)

Content script 在所有网页注入 `window.bitfs` API，任何网页脚本可无条件调用 `requestAccounts()` 获取用户的 BSV 地址：

```typescript
// 注入到页面的全局 API — 无需用户授权
window.bitfs = {
  isInstalled: true,
  version: "0.1.0",
  requestAccounts: function() {  // 任何页面 JS 都能调用
    return new Promise(function(resolve, reject) {
      window.postMessage({ type: "BITFS_REQUEST_ACCOUNTS" }, "*");
      // ...
    });
  }
};
```

消息中继层同样缺乏保护：

```typescript
window.addEventListener("message", (event) => {
  if (event.source !== window) return;  // 仅检查来源窗口，不验证 origin
  if (event.data?.type === "BITFS_REQUEST_ACCOUNTS") {
    chrome.runtime.sendMessage({ type: "GET_ADDRESS" }, (response) => {
      window.postMessage({ ... }, "*");  // 广播给所有 frame
    });
  }
});
```

**三重问题：**
1. **无用户确认**：`GET_ADDRESS` 不检查钱包是否已解锁即返回地址（从 storage 读取）
2. **无来源限制**：`postMessage("*")` 使响应对同页面所有 iframe 可见
3. **地址即身份**：BSV 地址可关联链上所有交易，泄露即隐私全失

**对比 MetaMask：** MetaMask 的 `eth_requestAccounts` 会弹出授权确认窗口，用户可逐域名批准/拒绝。BitFS 直接返回。

**修复方案：**
1. `requestAccounts()` 必须弹出授权确认 popup
2. 维护已授权域名白名单（存储在 chrome.storage）
3. `postMessage` 指定精确 origin 而非 `"*"`
4. 检查钱包锁定状态，锁定时拒绝请求

---

### C-2: PRIVATE 文件解密使用零值 keyHash 占位

**File:** `src/background/file-reader.ts:32-38`

```typescript
const result = await decrypt(
  content,
  nodeKey.privateKey,
  nodeKey.publicKey,
  new Uint8Array(32), // placeholder keyHash — MVP  ← 零填充
  0 // Access.Private
);
```

Method 42 的安全模型要求 `keyHash = SHA256(content_address_key)`，它绑定了解密密钥与具体文件的关系。使用全零 keyHash 意味着：

1. **密钥计算错误**：`aes_key = HKDF-SHA256(ECDH(priv, pub).x, keyHash)` — keyHash 为零导致所有 PRIVATE 文件使用相同 AES 密钥
2. **与 Go 实现不兼容**：libbitfs-go 的 PRIVATE 加密使用正确的 keyHash，此扩展无法解密那些文件
3. **如果侥幸成功**：说明 PRIVATE 访问控制实质退化为"有私钥就能解"，丧失了文件级隔离

**修复方案：**
从 Metanet 交易的 TLV payload 中提取 `key_hash` 字段（TLV tag 0x0B），传入 `decrypt()`。

---

### C-3: OP_RETURN 解析存在越界读取

**File:** `src/background/file-reader.ts:59-92`

```typescript
} else if (pushLen === 0x4d) {
  // OP_PUSHDATA2
  dataLen = rawTx[i + 2] | (rawTx[i + 3] << 8);  // ← i+3 可能越界
  dataStart = i + 4;
}
```

当 `i >= rawTx.length - 3` 时，`rawTx[i + 3]` 返回 `undefined`，位运算将其转换为 0（`undefined << 8 === 0`），不会抛错但产生错误的 `dataLen`。类似地，OP_PUSHDATA1 路径 (`rawTx[i + 2]`) 在 `i >= rawTx.length - 2` 时也越界。

更严重的是，即使 `dataLen` 被错误计算，只要 `dataStart + dataLen <= rawTx.length` 这个检查意外通过（NaN 比较返回 false，但 0 可以通过），就会返回错误的数据切片。

**修复方案：**
```typescript
if (pushLen === 0x4c) {
  if (i + 3 > rawTx.length) continue;
  dataLen = rawTx[i + 2];
  dataStart = i + 3;
} else if (pushLen === 0x4d) {
  if (i + 4 > rawTx.length) continue;
  dataLen = rawTx[i + 2] | (rawTx[i + 3] << 8);
  dataStart = i + 4;
}
```

---

## HIGH Findings

### H-1: TLV 解析器缺乏边界校验，可被恶意交易 DoS

**File:** `src/background/dag-manager.ts:99-110`

```typescript
private findTLVByte(data: Uint8Array, start: number, tag: number): number | null {
  let i = start;
  while (i < data.length - 2) {
    const t = data[i];
    const len = data[i + 1];
    if (t === tag && len >= 1 && i + 2 < data.length) {
      return data[i + 2];
    }
    i += 2 + len;  // ← len=0 不会死循环（i+=2），但 len=255 跳过大量数据
  }
  return null;
}
```

**问题：**
1. 若恶意交易在 OP_RETURN 后放置 `len=0` 的 TLV 项，`findTLVByte` 会以 O(n) 遍历整个交易
2. 没有验证 `i + 2 + len <= data.length`，大 `len` 值跳过时不检查是否仍在 payload 范围内
3. `parseMetanetTx` 外层循环对原始交易扫描 `0x6a`，可能误匹配数据中的 0x6a 字节（非 OP_RETURN）

**修复方案：**
- 在 OP_RETURN 发现后，应正确跳过 pushdata 前缀以定位到 META_FLAG，而不是暴力扫描
- `findTLVByte` 添加 `if (i + 2 + len > data.length) return null` 保护
- 限制 TLV 遍历的最大次数（如 50）防止畸形数据导致长循环

---

### H-2: Auto-Lock 静默失败，钱包可能永不锁定

**File:** `src/background/wallet-manager.ts:89-96`

```typescript
private scheduleAutoLock(): void {
  if (typeof chrome !== "undefined" && chrome.alarms) {
    chrome.alarms.clear("autoLock");
    chrome.alarms.create("autoLock", {
      delayInMinutes: this.autoLockMinutes,
    });
  }
  // ← 如果 chrome.alarms 不可用（测试环境、Service Worker 重启失败），不报错
}
```

**附加问题：**
- `lock()` 仅 `this.wallet = null`，不清除 `chrome.alarms`，定时器可能仍会触发
- Service Worker 可能因 Chrome 闲置策略被终止后重启，此时 `wallet` 已为 null（安全），但 `autoLockMinutes` 重置为默认 15 而非用户设定值（因未从 storage 加载）
- `chrome.alarms?.onAlarm?.addListener` (service-worker.ts:138) 使用可选链——如果 `onAlarm` 为 undefined，不注册监听器，也无报错

**修复方案：**
1. Auto-lock 失败时抛出错误或回退到 `setTimeout`
2. `lock()` 中清除 alarm
3. Service Worker 启动时从 storage 加载 autoLockMinutes

---

### H-3: 网络请求无超时设置

**File:** `src/lib/provider.ts:21-31`

```typescript
private async fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${this.baseUrl}${path}`);  // ← 无 timeout、无 AbortController
  ...
}
```

Chrome 的 `fetch` 默认无超时。如果 WhatsOnChain API 响应缓慢或连接挂起，整个 Service Worker 被阻塞。由于 Service Worker 是扩展的唯一后端，这会导致：
- 钱包无法解锁/锁定
- 所有 popup 操作挂起
- Auto-lock alarm 可能无法处理

**修复方案：**
```typescript
private async fetchJSON<T>(path: string, timeoutMs = 10000): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(`${this.baseUrl}${path}`, { signal: controller.signal });
    if (!res.ok) throw new Error(`WoC API error: ${res.status}`);
    return res.json() as Promise<T>;
  } finally {
    clearTimeout(timer);
  }
}
```

---

### H-4: DAG 扫描吞掉所有网络错误

**File:** `src/background/dag-manager.ts:27-56`

```typescript
private async scanAddress(...): Promise<void> {
  try {
    const utxos = await this.chain.listUnspent(address);
    for (const utxo of utxos) {
      try {
        const rawTx = await this.chain.getRawTx(utxo.txid);
        ...
      } catch {
        // Skip unparseable transactions  ← 网络超时也被跳过
      }
    }
  } catch {
    // Address may have no transactions  ← 429 限频、网络断开也被忽略
  }
}
```

用户点击 "Scan" 后，如果网络断开或 API 限频，`scan()` 会静默完成并更新 `scanHeight`，UI 显示 "Scan complete" 但 DAG 为空。用户无法区分"无文件"与"网络错误"。

**修复方案：**
- 将 "address not found"（API 返回 404/空数组）与网络错误区分开
- `scan()` 返回 `{ success: boolean; scanned: number; errors: number }` 统计
- Service Worker 将错误传递给 popup 显示

---

## MEDIUM Findings

### M-1: Mnemonic 在 React State 中未及时清除

**File:** `src/popup/pages/CreateWallet.tsx:14,33,42-47`

```typescript
const [mnemonic, setMnemonic] = useState("");
// ... 用户确认后：
const handleConfirm = () => {
  if (confirmWords.trim().toLowerCase() !== mnemonic.trim().toLowerCase()) {
    setError("Mnemonic does not match");
    return;
  }
  navigate("/");  // ← 导航离开，但 mnemonic 仍存在于 React fiber tree 中
};
```

BIP39 助记词是恢复钱包的唯一凭据。确认完成后应立即清除 state：

```typescript
setMnemonic("");
setConfirmWords("");
navigate("/");
```

此外，`select-all` CSS 类（第 56 行）虽方便用户复制，但也使恶意扩展/辅助功能更容易读取。

---

### M-2: 密码强度验证不足

**File:** `src/popup/pages/CreateWallet.tsx:20-21` + `src/background/wallet-manager.ts:48-51`

Popup 仅要求 8 字符最短密码：`password.length < 8`。WalletManager 完全不验证密码强度。

Argon2id 能抵抗暴力攻击，但弱密码（如 "12345678"）仍在攻击者字典范围内。建议：
- 最低 12 字符
- 或实现 zxcvbn 风格的熵评估
- 禁止常见弱密码

---

### M-3: IndexedDB 存储未加密的文件系统元数据

**File:** `src/lib/dag-cache.ts:62-154`

IndexedDB `bitfs-dag` 数据库以明文存储：
- 文件名和目录结构 (`name`, `type`)
- 访问级别 (`access`: PRIVATE/FREE/PAID)
- 关联交易 ID (`txid`, `dataTxid`, `parentTxid`)
- 文件大小和 MIME 类型

有文件系统访问权限的攻击者（或其他扩展通过 `indexedDB.open("bitfs-dag")`）可获取用户的完整文件目录，而 PRIVATE 文件名本身可能就是敏感信息。

**修复方案：** 使用 `chrome.storage.session`（仅存活于浏览器会话）替代 IndexedDB 存储敏感元数据，或在应用层加密。

---

### M-4: Provider 不验证 API 响应结构

**File:** `src/lib/provider.ts:33-51`

```typescript
const data = await this.fetchJSON<WocUTXO[]>(`/address/${address}/unspent`);
return data.map((u) => ({
  txid: u.tx_hash,       // ← 如果 API 返回非预期格式，这里静默产生 undefined
  vout: u.tx_pos,
  amount: BigInt(u.value), // ← BigInt(undefined) 抛出 TypeError
  ...
}));
```

TypeScript 类型断言 (`as WocUTXO[]`) 不提供运行时保护。如果 WoC API 改变响应格式或返回错误消息，扩展会崩溃。

**修复方案：** 添加运行时类型检查，至少验证 `Array.isArray(data)` 和关键字段存在。

---

### M-5: Service Worker 不验证消息来源

**File:** `src/background/service-worker.ts:132-134`

```typescript
chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  handleMessage(message as Message).then(sendResponse);
  return true;
});
```

`_sender` 被忽略。虽然 Manifest V3 的 `chrome.runtime.onMessage` 默认只接收同扩展内的消息，但 content script 运行在所有页面上下文中。恶意页面可能通过 content script 的消息中继触发敏感操作（如 `BROADCAST_TX`）。

**修复方案：** 敏感操作（`WALLET_CREATE`, `WALLET_IMPORT`, `BROADCAST_TX`, `SIGN_TX`）应校验 `sender.url` 来源是 popup 而非 content script。

---

### M-6: `listFiles()` 始终返回空数组

**File:** `src/background/dag-manager.ts:112-120`

```typescript
async listFiles(path: string): Promise<FileEntry[]> {
  if (path === "/" || path === "") {
    // For now, return empty — user needs to scan first
  }
  return [];
}
```

这是一个功能性缺陷而非安全问题，但 Files 页面会显示空列表，用户可能误以为没有文件。发布前应实现基本的缓存查询，或在 UI 中明确标注功能未就绪。

---

## LOW Findings

### L-1: `confirmations` 信息丢失精度

**File:** `src/lib/provider.ts:49`

```typescript
confirmations: u.height > 0 ? 1 : 0,
```

将所有已确认交易的 confirmations 统一为 1，丢失了实际确认深度。虽然当前代码未依赖确认数做安全判断，但未来 SPV 验证或支付逻辑可能需要。

---

### L-2: `getUTXO()` 始终返回 null

**File:** `src/lib/provider.ts:53-59`

```typescript
async getUTXO(txid: string, vout: number): Promise<UTXO | null> {
  void txid; void vout;
  return null;
}
```

`BlockchainService` 接口要求实现此方法，但 WoC API 确实不支持单 UTXO 查询。当前无调用者，但接口合约未满足。

---

### L-3: README 提及不存在的 `bitfs-core/` 目录

**File:** `README.md`

README 的目录结构中列出了 `src/bitfs-core/`，但实际密码学代码全部位于 `@bitfs/libbitfs`（file: 链接到 `../libbitfs-ts`）。应更新文档。

---

### L-4: MutationObserver 可能导致重复链接处理

**File:** `src/content-script/index.ts:124-127`

```typescript
const observer = new MutationObserver(() => {
  scanForBitFSLinks();  // ← 每次 DOM 变更都全量扫描
});
observer.observe(document.body, { childList: true, subtree: true });
```

在 DOM 频繁变更的页面（SPA、实时聊天），会反复全量扫描。虽然 `closest("a, [data-bitfs-link]")` 检查防止了重复包装，但仍浪费 CPU。建议使用 debounce + 增量扫描。

---

### L-5: 测试覆盖率不足（约 20-30%）

**14 个测试仅覆盖 3 个模块**（provider, dag-cache, storage）。关键路径缺少测试：

| 模块 | 测试覆盖 |
|------|---------|
| WalletManager (创建/导入/解锁/锁定) | 无 |
| FileReader (OP_RETURN 解析/解密) | 无 |
| DAGManager (TLV 解析/扫描) | 无 |
| Content Script (链接检测/API 注入) | 无 |
| UI 组件 (创建流程/解锁) | 无 |

至少应为 `extractOPReturnData` 和 `parseMetanetTx` 添加边界测试用例。

---

## Architecture Assessment

### Strengths

| Aspect | Assessment |
|--------|-----------|
| **依赖最小化** | 仅 libbitfs + React。密码学零重复实现 |
| **Manifest V3** | 权限精简：storage + activeTab + alarms，无 host_permissions |
| **TypeScript strict** | 全开。strict null checks 捕获大量潜在 bug |
| **Service Worker 隔离** | 密钥材料仅存于 SW 内存，popup 通过 IPC 访问 |
| **Auto-lock 机制** | 有定时自动锁定（虽有上述可靠性问题） |
| **存储命名空间** | `bitfs:*` 前缀避免与其他扩展冲突 |

### Risks

| Aspect | Assessment |
|--------|-----------|
| **Content Script `<all_urls>`** | 在所有页面注入，攻击面最大 |
| **单一 API 提供者** | 完全依赖 WhatsOnChain，无 fallback |
| **libbitfs 本地链接** | `file:../libbitfs-ts` 打包依赖构建环境。发布需解决 |
| **功能完成度** | listFiles 返回空，SIGN_TX/PAY_INVOICE 是 stub |
| **错误处理** | 过多 silent catch，用户无法排错 |

---

## Fix Priority Matrix

### Must Fix Before Any Release

| # | Issue | Effort | File |
|---|-------|--------|------|
| C-1 | Address 泄露：需用户授权 | 2-3d | content-script/index.ts |
| C-2 | keyHash 占位符 | 0.5d | file-reader.ts |
| C-3 | OP_RETURN 越界读取 | 0.5d | file-reader.ts |
| H-1 | TLV 解析边界校验 | 0.5d | dag-manager.ts |
| H-2 | Auto-lock 可靠性 | 1d | wallet-manager.ts, service-worker.ts |
| H-3 | Fetch 超时 | 0.5d | provider.ts |
| H-4 | 错误区分 | 1d | dag-manager.ts, service-worker.ts |

### Should Fix Before Beta

| # | Issue | Effort | File |
|---|-------|--------|------|
| M-1 | Mnemonic state 清除 | 0.5h | CreateWallet.tsx |
| M-2 | 密码强度 | 0.5d | CreateWallet.tsx, wallet-manager.ts |
| M-3 | IndexedDB 加密 | 1d | dag-cache.ts |
| M-5 | 消息来源校验 | 0.5d | service-worker.ts |
| M-6 | listFiles 实现 | 1d | dag-manager.ts |
| L-5 | 核心路径测试 | 2d | test/ |

---

## Conclusion

BitFS Extension 的架构设计是健全的：密码学委托给经过审计的 libbitfs-ts，Manifest V3 权限控制得当，TypeScript strict 模式提供了强类型保证。核心问题集中在两个方面：

1. **安全边界缺失**：content script 的 DApp API 缺少用户授权流程（C-1），二进制解析缺少边界检查（C-3, H-1）
2. **MVP 占位符残留**：keyHash 全零（C-2）、listFiles 空返回（M-6）、SIGN_TX/PAY_INVOICE stub

建议修复全部 CRITICAL + HIGH 后，在测试网发布 beta 版本，收集反馈后再处理 MEDIUM/LOW。

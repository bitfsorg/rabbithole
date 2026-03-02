# Design Consistency Fixes Implementation Plan (v2)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix design document inconsistencies — both from the original 2026-02-26 audit AND new inconsistencies introduced by TransactionSpec v1.1 becoming the authoritative SSOT.

**Architecture:** Pure documentation fixes — update design docs to match TransactionSpec v1.1 as the authority. No code changes.

**Tech Stack:** Markdown text edits only.

**Scope:** BitFS-focused only. Metanet items (C5, C6, C7, H1, M8) deferred per user decision.

**Authority:** `design/bitfs/5-TransactionSpec.zh.md` v1.1 is the Single Source of Truth. All other design docs must conform to it.

---

## Phase 1: Already Fixed ✅

### Batch 1: Audit fixes (earlier commits)

| ID | Issue | Status |
|----|-------|--------|
| C1 | HKDF info string | FIXED |
| C2 | TLV tag numbering | FIXED |
| C4 | Dust limit 546→1 | FIXED |
| C8 | HTLC initiator | FIXED |
| C9 | wallet.db→wallet.enc | FIXED |
| H4 | mv cross-dir internal consistency | FIXED (but see Task 1 below — now needs TransactionSpec alignment) |
| H7 | Exit codes | FIXED |
| H10 | BIP32 paths | FIXED |
| H11 | FREE mode KDF P_node.x | FIXED |
| H12 | HKDF info parameter | FIXED |
| M5 | DustLimit 546 residual | FIXED |

### Batch 2: Commit 8dd93ae (H2, H3, H9, M1-M3, M7)

| Original Task | Issue | Status |
|---------------|-------|--------|
| Task 1-2 | DNS record format H9 (all docs) | FIXED |
| Task 3 | rm operation count H3 | FIXED |
| Task 4 | libbitfs package list H2 | FIXED |
| Task 5 | Project structure M1, M2, M3, M7 | FIXED |
| Task 6 | Rabin reference H2 | FIXED |
| Task 7 | bitfs/CLAUDE.md | FIXED |

---

## Phase 2: New Fixes Required (TransactionSpec v1.1 alignment)

TransactionSpec v1.1 introduced design decisions that contradict older docs:

- **Decision #8**: 删除 hard link（Metanet DAG 是严格树，不支持多父节点）
- **Decision #9**: 移除 Anchor 节点类型
- **§5.1**: mv 跨目录 = 2 笔交易（SelfUpdate × 2），不再创建新节点/新密钥
- **Appendix A**: TLV tag `cltv_height` = 0x17 (decimal 23)

### Task 1: Update `mv` cross-directory in SystemDesign (CRITICAL)

**Files:**
- Modify: `design/bitfs/2-SystemDesign.zh.md`

TransactionSpec §5.1 defines mv (跨目录) = **2 笔交易** (SelfUpdate src_parent + SelfUpdate dst_parent)。
P_node 不变，不创建新节点，不重新加密。只是移动 ChildEntry。

**Edits:**

**Line 216** — 操作映射表:
```
Old: | `mv` (跨目录) | DELETE 旧节点 + CreateChild 新节点 (4 笔交易) | 新 HD 路径, 新 P_node, 重新加密, 旧节点记录 moved_to 指针 |
New: | `mv` (跨目录) | SelfUpdate(源父目录) + SelfUpdate(目标父目录) (2 笔交易) | P_node 不变, 仅移动 ChildEntry (目标目录分配新 index) |
```

**Commit:** `docs(design): align mv cross-dir to TransactionSpec — 2 txs not 4`

---

### Task 2: Update `mv` cross-directory in DetailedDesign (CRITICAL)

**Files:**
- Modify: `design/bitfs/3-DetailedDesign.zh.md`

**Lines 810-842** — 跨目录 mv 的详细交易流程:
Replace the 4-transaction flow (Tx1-Tx4) with a 2-transaction flow matching TransactionSpec §5.1:

```
交易组合 (2 笔):

  Tx 1: BuildSelfUpdate (源父目录)
    Input 0:  P_srcParent UTXO (Sig D_srcParent)
    Input 1:  fee UTXO[0]
    Output 0: OP_RETURN { type=DIR, op=UPDATE,
              children=[...移除 srcName ChildEntry...] }
    Output 1: P2PKH → P_srcParent (1 sat, refresh)
    Output 2: Change

  Tx 2: BuildSelfUpdate (目标父目录)
    Input 0:  P_dstParent UTXO (Sig D_dstParent)
    Input 1:  fee UTXO[1]
    Output 0: OP_RETURN { type=DIR, op=UPDATE,
              children=[...,原 ChildEntry(pubkey=P_src, index=dst_next_child_index, name=dstName)],
              next_child_index=old+1 }
    Output 1: P2PKH → P_dstParent (1 sat, refresh)
    Output 2: Change

注: P_node 不变, 无需重新加密。目标目录分配新 index, 但 BIP32 密钥不变 (ChildEntry 存储实际 PubKey)。
```

Also remove lines 839-841 about "源节点标记 DELETE" and "moved_to 指针".

**Lines 1522-1526** — CLI 摘要中的跨目录 mv:
```
Old:
  跨目录: 4 笔:
    Tx1: CreateChild (目标新节点, 新 HD 路径, 新密钥)
    Tx2: SelfUpdate (目标父目录, 添加 ChildEntry)
    Tx3: SelfUpdate (源节点 → op=DELETE, link_target=新 P_node 作为 moved_to 指针)
    Tx4: SelfUpdate (源父目录, 移除 ChildEntry)

New:
  跨目录: 2 笔:
    Tx1: SelfUpdate (源父目录, 移除 ChildEntry)
    Tx2: SelfUpdate (目标父目录, 添加 ChildEntry, 分配新 index)
  注: P_node 不变, 不创建新节点, 不重新加密
```

**Commit:** `docs(design): align DetailedDesign mv cross-dir to TransactionSpec — 2 txs`

---

### Task 3: Remove hard link references from SystemDesign (CRITICAL)

**Files:**
- Modify: `design/bitfs/2-SystemDesign.zh.md`

TransactionSpec Decision #8: "删除 hard link。Metanet DAG 是严格树，不支持多父节点。跨目录引用统一使用 Soft Link。"

**Locations to fix:**

| Line | Content | Action |
|------|---------|--------|
| 176 | `硬链接 \| 多个 ChildEntry → 同一 P_node` | 移除硬链接行 |
| 179-180 | `parent 字段...硬链接不改变`, `inode 编号...硬链接共享同一 P_node` | 移除硬链接提及 |
| 191-205 | `### 硬链接与软链接` section | 重写为仅软链接说明，注明硬链接已移除 (Decision #8) |
| 213 | rm 说明 "因硬链接可能引用同一 P_node" | 移除硬链接理由，改为 "不花费目标节点 UTXO" |
| 218 | `link` 硬链接行 | 移除整行 |
| 332 | `parent` field "硬链接不改变" | 移除硬链接提及 |
| 840 | rm CLI "因硬链接可能引用同一 P_node" | 移除硬链接理由 |
| 843 | `bitfs link <target> <name>  # 硬链接` | 移除硬链接命令 |
| 924 | shell 命令 `link <target> <name>  硬链接` | 移除硬链接，保留 `link -s` 软链接 |

**注意:** `bitfs link` (无 `-s`) 命令应移除或改为 `bitfs link -s` 的别名。

**Commit:** `docs(design): remove hard link references per TransactionSpec Decision #8`

---

### Task 4: Remove hard link references from DetailedDesign

**Files:**
- Modify: `design/bitfs/3-DetailedDesign.zh.md`

**Lines 1533-1538** — 硬链接详细描述:
```
Old:
bitfs link <target> <name>
  硬链接 (默认)
  限制: 仅文件, 禁止目录硬链接 (防止环路)
  限制: 仅本 Vault 内
  交易: 1 笔 (SelfUpdate parent, 添加 ChildEntry 复用目标 P_node)
  注: 消耗 next_child_index 但不创建新 HD 密钥

New:
bitfs link -s <target> <name>
  软链接 (唯一支持的链接类型, 详见 TransactionSpec 设计决策 #8)
  ...
```

Also search for other hard link references in DetailedDesign and update accordingly.

**Commit:** `docs(design): remove hard link from DetailedDesign per Decision #8`

---

### Task 5: Fix cltv_height TLV field number (MEDIUM)

**Files:**
- Modify: `design/bitfs/2-SystemDesign.zh.md:1241`
- Modify: `design/bitfs/3-DetailedDesign.zh.md:1196`

TransactionSpec Appendix A: `cltv_height` = tag `0x17` (decimal 23), 不是 "field 35"。

**Edits:**

SystemDesign L1241:
```
Old: `cltv_height` 字段 (field 35, uint32): 信息性标记, 实际约束在链上脚本中执行。
New: `cltv_height` 字段 (tag 0x17, uint32): 信息性标记, 实际约束在链上脚本中执行。
```

DetailedDesign L1196:
```
Old:   - cltv_height 字段 (TLV field 35)
New:   - cltv_height 字段 (TLV tag 0x17)
```

**Commit:** `docs(design): fix cltv_height TLV tag number — 0x17 not field 35`

---

### Task 6: Fix libbitfs path in ConceptDesign (LOW)

**Files:**
- Modify: `design/bitfs/1-ConceptDesign.zh.md:22`

**Edit:**
```
Old: 共享核心库 (libbitfs)
New: 共享核心库 (libbitfs-go)
```

**Commit:** `docs(design): fix libbitfs → libbitfs-go in ConceptDesign`

---

### Task 7: Add TransactionSpec to document navigation links (LOW)

**Files:**
- Modify: `design/bitfs/1-ConceptDesign.zh.md` (top blockquote)
- Modify: `design/bitfs/2-SystemDesign.zh.md` (top blockquote)
- Modify: `design/bitfs/3-DetailedDesign.zh.md` (top blockquote)
- Modify: `design/bitfs/4-TestDesign.zh.md` (top blockquote)

Add navigation bar matching TransactionSpec's format. Each doc should include:

```markdown
> **文档体系导航**: [总体设计](../0-OverallDesign.zh.md) · [概念设计](1-ConceptDesign.zh.md) · [系统设计](2-SystemDesign.zh.md) · [详细设计](3-DetailedDesign.zh.md) · [测试设计](4-TestDesign.zh.md) · [交易规范](5-TransactionSpec.zh.md)
```

Bold the current document name (e.g., in 1-ConceptDesign: `**概念设计** (本文档)`)。

If docs already have a different format navigation, replace it. If no navigation exists, add it after the title.

**Commit:** `docs(design): add cross-document navigation with TransactionSpec link`

---

## Post-Fix Verification

1. `grep -rn '硬链接' design/bitfs/` — confirm only historical mentions or "已移除" notes remain
2. `grep -rn 'field 35' design/` — confirm zero matches
3. `grep -rn '4 笔' design/bitfs/` — confirm mv cross-dir no longer says 4 txs
4. `grep -rn 'libbitfs[^-]' design/` — confirm no old path references
5. Verify each doc has navigation bar with TransactionSpec link
6. Manually review changed sections for coherence

---

## Summary

| Task | Severity | Scope | Files |
|------|----------|-------|-------|
| 1 | CRITICAL | mv 跨目录 2-SystemDesign | 1 file, 1 table cell |
| 2 | CRITICAL | mv 跨目录 3-DetailedDesign | 1 file, 2 sections |
| 3 | CRITICAL | Remove hard links 2-SystemDesign | 1 file, ~10 locations |
| 4 | CRITICAL | Remove hard links 3-DetailedDesign | 1 file, ~3 locations |
| 5 | MEDIUM | cltv_height tag number | 2 files, 2 lines |
| 6 | LOW | libbitfs → libbitfs-go | 1 file, 1 line |
| 7 | LOW | Navigation links | 4 files |

**Total: 7 tasks, 4 CRITICAL + 1 MEDIUM + 2 LOW**

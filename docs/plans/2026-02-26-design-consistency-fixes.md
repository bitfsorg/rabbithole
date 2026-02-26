# Design Consistency Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix remaining design document inconsistencies identified by the 2026-02-26 design consistency audit.

**Architecture:** Pure documentation fixes — update design docs to match code as the authority. No code changes.

**Tech Stack:** Markdown text edits only.

**Scope:** BitFS-focused only. Metanet items (C5, C6, C7, H1, M8) deferred per user decision.

---

## Already Fixed (confirmed by file verification)

These items were resolved by earlier audit fix commits:

| ID | Issue | Status |
|----|-------|--------|
| C1 | HKDF info string | FIXED (2-SystemDesign:472 now says "bitfs-file-encryption") |
| C2 | TLV tag numbering | FIXED (reserved field removed, tags renumbered) |
| C4 | Dust limit 546→1 | FIXED (whitepaper, TASKS.md all show 1 sat) |
| C8 | HTLC initiator | FIXED (website says Buyer) |
| C9 | wallet.db→wallet.enc | FIXED (whitepaper + DetailedDesign) |
| H4 | mv cross-dir inconsistency | FIXED (sections 4-B and 9-B consistent: DELETE + moved_to, 4 txs) |
| H7 | Exit codes | FIXED (spec matches code: 3=wallet, 4=network, 5=permission, 7=conflict) |
| H10 | BIP32 paths | FIXED (website shows full BIP44 paths) |
| H11 | FREE mode KDF P_node.x | FIXED (whitepaper corrected) |
| H12 | HKDF info parameter | FIXED (whitepaper includes info) |
| M5 | DustLimit 546 residual | FIXED (TASKS.md shows 1 聪) |

---

## Remaining Fixes

### Task 1: Update DNS record format in 2-SystemDesign.zh.md (H9)

**Files:**
- Modify: `design/bitfs/2-SystemDesign.zh.md`

Code unified to `_bitfs.{domain}` with `bitfs=<pubkey>` format. Design doc still uses old `_bitfs_pubkey.{domain}` with raw hex.

**Edits (all `replace_all` safe since format is distinctive):**

| Line | Old | New |
|------|-----|-----|
| 538 | `_bitfs_pubkey.example.com   TXT  "02a1b2c3d4e5f6..."` | `_bitfs.example.com   TXT  "bitfs=02a1b2c3d4e5f6..."` |
| 546 | `**\`_bitfs_pubkey\`** (TXT): P_node 公钥 (33 bytes 压缩公钥的 hex 编码)。只能有一个。...` | `**\`_bitfs\`** (TXT): P_node 公钥，格式 \`bitfs=<hex_pubkey>\` (33 bytes 压缩公钥的 hex 编码)。只能有一个。...` |
| 558 | `_bitfs_pubkey` TXT 记录指向 P_node | `_bitfs` TXT 记录指向 P_node |
| 585 | `DNS TXT lookup _bitfs_pubkey.example.com` | `DNS TXT lookup _bitfs.example.com` |
| 616 | `_bitfs_pubkey` TXT | `_bitfs` TXT |
| 629 | `TXT _bitfs_pubkey.{domain}` | `TXT _bitfs.{domain}` |
| 668 | `_bitfs_pubkey.example.com   TXT  "02a1b2c3d4e5f6..."` | `_bitfs.example.com   TXT  "bitfs=02a1b2c3d4e5f6..."` |
| 1570 | `_bitfs_pubkey` TXT + `_bitfs._tcp` SRV | `_bitfs` TXT + `_bitfs._tcp` SRV |

**Commit:** `docs(design): unify DNS TXT format to _bitfs.{domain} + bitfs= prefix (H9)`

---

### Task 2: Update DNS record format in other design docs (H9)

**Files:**
- Modify: `design/bitfs/1-ConceptDesign.zh.md:87`
- Modify: `design/bitfs/3-DetailedDesign.zh.md:1585,1986`
- Modify: `design/bitfs/4-TestDesign.zh.md:389`

**Edits:**
- 1-ConceptDesign line 87: `_bitfs_pubkey` → `_bitfs`
- 3-DetailedDesign line 1585: `DNS TXT (_bitfs_pubkey)` → `DNS TXT (_bitfs.{domain})`
- 3-DetailedDesign line 1986: `_bitfs_pubkey` → `_bitfs`
- 4-TestDesign line 389: `_bitfs_pubkey` TXT → `_bitfs` TXT

**Commit:** `docs(design): unify DNS TXT references across L1/L3/L4 docs (H9)`

---

### Task 3: Fix rm operation count in 2-SystemDesign.zh.md (H3)

**Files:**
- Modify: `design/bitfs/2-SystemDesign.zh.md:840`

Line 213 correctly says "1 笔交易", but line 840 still says "(1) SelfUpdate..., (2) 花费目标节点 UTXO":
```
bitfs rm <path>                # 删除: (1) SelfUpdate 父目录移除 ChildEntry, (2) 花费目标节点 UTXO 到 fee 地址
```

Fix to match line 213 and L3:
```
bitfs rm <path>                # 删除: SelfUpdate 父目录移除 ChildEntry (不花费目标节点 UTXO, 因硬链接可能引用同一 P_node)
```

**Commit:** `docs(design): fix rm to 1 tx in CLI section (H3)`

---

### Task 4: Update libbitfs package list in OverallDesign (H2)

**Files:**
- Modify: `design/0-OverallDesign.zh.md:60-69`

Current list has 8 packages. Actual libbitfs-go has 11: missing `wallet/`, `config/`, `network/`.
Also path should be `libbitfs-go/` not `libbitfs/`.

**Commit:** `docs(design): update libbitfs package list to 11 packages (H2)`

---

### Task 5: Update project structure and tech stack in 2-SystemDesign.zh.md (M1, M7)

**Files:**
- Modify: `design/bitfs/2-SystemDesign.zh.md:1910-1949`

**M7:** Line 1912 "Go 1.21+" → "Go 1.25.6"
**M1:** Lines 1924-1949 project structure: still shows packages in `internal/` that moved to `libbitfs-go/`. Update to reflect current layout where `internal/` only has `buyer/`, `client/`, `daemon/`, `engine/`.

Also fix M3 (line 64: `config.toml` → `config`) and M2 (line 75: `store/` → `storage/`).

**Commit:** `docs(design): update project structure, tech stack, storage/config paths (M1, M2, M3, M7)`

---

### Task 6: Fix Rabin reference in ConceptDesign (H2)

**Files:**
- Modify: `design/bitfs/1-ConceptDesign.zh.md:32`

Rabin package mentioned in architecture diagram but doesn't exist yet. Add note "(计划中)".

**Commit:** `docs(design): mark Rabin package as planned (H2)`

---

### Task 7: Update bitfs/CLAUDE.md project structure

**Files:**
- Modify: `bitfs/CLAUDE.md`

The CLAUDE.md still shows packages in `internal/` that moved to `libbitfs-go/`. Update to reflect actual layout.

**Commit:** `docs(bitfs): update CLAUDE.md project structure to reflect libbitfs extraction`

---

### Task 8: Fix remaining LOW items (L1-L8)

**Files:**
- Various design docs

L1: Add ANCHOR to NodeType enum in design
L2: Document anchor TLV tags (0x20-0x26)
L3: Fix L0 CLI command list
L5: Fix paymail profile URL path
L6: Fix section numbering gap
L7: Fix spec package paths from `libbitfs/` to `libbitfs-go/`
L8: Add cross-references

**Commit:** `docs(design): fix LOW-priority consistency items (L1-L8)`

---

## Post-Fix Verification

Run: `grep -rn '_bitfs_pubkey' design/` to confirm no old DNS format remains.
Manually review each changed section for coherence.

# Whitepaper Technical Accuracy Audit — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Systematically verify every technical claim in both whitepapers against code, and assess academic rigor.

**Architecture:** 5 parallel audit agents each handle a slice of the two whitepapers. Each agent reads the relevant whitepaper sections (from `.en.tex` files), extracts technical claims, and verifies each against code in `libbitfs-go/`, `bitfs/`, and `metanet/`. Agent 5 additionally handles cross-paper consistency and academic rigor. A final merge task assembles the single report.

**Tech Stack:** Read-only exploration of Go source code, LaTeX whitepaper files, and existing audit reports.

---

## Source Files

| File | Role |
|------|------|
| `whitepaper/BitFS-Whitepaper.en.tex` | BitFS whitepaper (published English version) |
| `whitepaper/Metanet-Whitepaper.en.tex` | Metanet whitepaper (published English version) |
| `whitepaper/BitFS-Whitepaper-Outline.md` | BitFS source outline (Chinese) |
| `whitepaper/Metanet-Whitepaper-Outline.md` | Metanet source outline (Chinese) |
| `docs/audits/2026-02-26-design-consistency.md` | Existing findings to cross-reference |

## Code Verification Targets

| Directory | Contents |
|-----------|----------|
| `libbitfs-go/method42/` | ECDH encryption, HKDF, AES-256-GCM |
| `libbitfs-go/wallet/` | HD wallet, BIP32/BIP44, Argon2id |
| `libbitfs-go/metanet/` | DAG, node types, TLV parser, Merkle |
| `libbitfs-go/tx/` | Transaction builder, OP_RETURN |
| `libbitfs-go/spv/` | SPV client, Merkle proof, block headers |
| `libbitfs-go/storage/` | Content-addressed store |
| `libbitfs-go/x402/` | HTTP 402 payment protocol, HTLC |
| `libbitfs-go/paymail/` | Paymail/DNS resolution |
| `libbitfs-go/network/` | Blockchain service interface |
| `libbitfs-go/config/` | Configuration parsing |
| `bitfs/internal/engine/` | Business logic (all commands) |
| `bitfs/internal/daemon/` | HTTP server, LFCP, routes |
| `bitfs/internal/client/` | b-tools HTTP client |
| `bitfs/cmd/` | CLI entry points |
| `metanet/` | CDN node (chain, mining, contract, proof, payment) |

## Severity Labels

| Label | Meaning |
|-------|---------|
| **INACCURATE** | Claim contradicts code implementation |
| **OUTDATED** | Claim was once correct but code has evolved |
| **UNIMPLEMENTED** | Claimed feature does not exist in code |
| **OVERSTATED** | Claim exaggerates actual capability |
| **ACCURATE** | Claim matches code (only note if noteworthy) |

Academic rigor labels:
| Label | Meaning |
|-------|---------|
| **LOGIC-FLAW** | Argument is internally inconsistent |
| **STRAW-MAN** | Comparison unfairly weakens competitor |
| **MISSING-CAVEAT** | Important limitation not mentioned |
| **REF-ERROR** | Reference is wrong, missing, or misattributed |

---

## Task 1: BitFS Whitepaper §1–5 (Core Protocol)

**Sections:** Introduction, System Architecture, Metanet DAG as Filesystem, Hierarchical Key Derivation, Universal Encryption

**Agent type:** `Explore` (read-only, thorough)

**Step 1: Extract claims from BitFS .en.tex §1–5**

Read `whitepaper/BitFS-Whitepaper.en.tex` lines 86–204. For each section, list every verifiable technical claim. Focus on:

- §1 Introduction: Claims about IPFS/Filecoin limitations, BitFS positioning
- §2 System Architecture: Layer diagram (b* tools, libbitfs modules, Rabin Sigs?), self-sustaining UTXO claim
- §3 Metanet DAG: Unix mapping table (inode→P_node, etc.), three node types (FILE/DIR/LINK), three link types, version control model, git remote helper claim
- §4 HD Key Derivation: BIP32 path `m/44'/236'/...`, vault concept, stable identity, deterministic recovery
- §5 Universal Encryption: Method 42 steps (ECDH→key_hash→HKDF→AES-GCM), three access levels (Private/Free/Paid), "trivial key trick" for FREE mode

**Step 2: Verify each claim against code**

For each claim, find the corresponding code and check:

| Whitepaper Claim | Code Location to Check |
|------------------|----------------------|
| inode = P_node (33 bytes) | `libbitfs-go/metanet/node.go` — NodeMeta.PubKey field |
| ChildEntry(index, name, type, pubkey) | `libbitfs-go/metanet/node.go` — ChildEntry struct |
| Three node types: FILE, DIR, LINK | `libbitfs-go/metanet/node.go` — NodeType enum |
| Three link types: HARD, SOFT, SOFT_REMOTE | `libbitfs-go/metanet/node.go` — LinkType enum |
| BIP44 path m/44'/236'/... | `libbitfs-go/wallet/` — derivation constants |
| ECDH(D_node, P_recipient) | `libbitfs-go/method42/kdf.go` — DeriveKey function |
| key_hash = SHA256(SHA256(plaintext)) | `libbitfs-go/method42/` or `libbitfs-go/storage/` |
| HKDF-SHA256(ikm=shared_point.x, salt=key_hash) | `libbitfs-go/method42/kdf.go` — check if info param is missing from whitepaper |
| AES-256-GCM encryption | `libbitfs-go/method42/encrypt.go` |
| FREE mode: "D_node = 1" (trivial key trick) | `libbitfs-go/method42/` — check actual FREE implementation |
| PRIVATE mode: ECDH(D_node, P_node) | `libbitfs-go/method42/` — check actual PRIVATE implementation |
| System architecture diagram: "Rabin Sigs" module | Does `libbitfs-go/` have a rabin package? |
| Self-sustaining UTXO: output[2] refreshes parent | `libbitfs-go/tx/opreturn.go` — transaction builder |

**Step 3: Cross-reference with existing design-consistency audit**

Check if findings overlap with C1 (HKDF info), C2 (TLV tags), H11 (FREE mode KDF), H12 (HKDF info param). Note overlaps and any NEW findings.

**Step 4: Produce findings in standard format**

For each finding:
```
### [B-§N-M] Short title

**Whitepaper:** Quote the exact claim (with .tex line number)
**Code:** What the code actually does (with file:line)
**Severity:** INACCURATE / OUTDATED / UNIMPLEMENTED / OVERSTATED
**Overlap:** C1 from design-consistency audit (if applicable)
**Fix:** Which file to update (whitepaper or code)
```

---

## Task 2: BitFS Whitepaper §6–10 (Application Layer)

**Sections:** Content-Addressed Storage, Trustless Data Commerce, SPV Operation, Agent-First Interface, Identity and Discovery

**Agent type:** `Explore` (read-only, thorough)

**Step 1: Extract claims from BitFS .en.tex §6–10**

Read `whitepaper/BitFS-Whitepaper.en.tex` lines 206–296. Focus on:

- §6 Content-Addressed Storage: key_hash index, daemon HTTP GET /data/{hash}, on-chain OP_DROP storage mode, deduplication claim
- §7 Trustless Data Commerce: Method 42 handshake formula, HTLC script, capsule_hash flow, directory-level xpub purchase, hash chain token system, "seller completely stateless"
- §8 SPV Operation: Full SPV verification chain, "never queries blockchain", recovery from BIP39, "self-certifying data"
- §9 Agent-First Interface: Content negotiation (HTML/Markdown/JSON), WebMCP, HTTP 402 headers (X-Price, X-File-Size, X-Invoice-Id), b* tools list (bls/bcat/bget/bstat/btree), bitfs commands list, shell REPL commands, --json flag, --offline flag, price_per_kb inheritance
- §10 Identity and Discovery: DNS TXT `_bitfs_pubkey`, SRV `_bitfs._tcp`, Paymail integration, direct pubkey addressing, URI resolution precedence

**Step 2: Verify each claim against code**

| Whitepaper Claim | Code Location to Check |
|------------------|----------------------|
| Storage path ~/.bitfs/data/{key_hash} | `libbitfs-go/storage/` — actual path |
| Daemon GET /data/{hash} | `bitfs/internal/daemon/routes.go` — content endpoint |
| OP_DROP on-chain storage | `libbitfs-go/tx/` — does DataTx exist? |
| HTLC script structure | `libbitfs-go/x402/` — HTLC builder |
| Session key = SHA256(ECDH.x \|\| nonce_b \|\| nonce_s) | `bitfs/internal/daemon/` — handshake implementation |
| Directory xpub purchase S_child = S_parent + offset × P_buyer | `libbitfs-go/method42/` or `libbitfs-go/wallet/` |
| Hash chain token system | Search codebase for hash chain / token |
| SPV verification chain | `libbitfs-go/spv/` — VerifyTx, VerifyMerkleProof |
| Content negotiation Accept headers | `bitfs/internal/daemon/` — content handler |
| WebMCP tool declarations | `bitfs/internal/daemon/` — HTML response |
| HTTP 402 headers | `bitfs/internal/daemon/` or `libbitfs-go/x402/` |
| b* tools: bls, bcat, bget, bstat, btree | `bitfs/cmd/b*/` — check all exist |
| --json and --offline flags | `bitfs/cmd/` — flag definitions |
| DNS _bitfs_pubkey TXT record | `libbitfs-go/paymail/dns.go` — check record name |
| Paymail resolution | `libbitfs-go/paymail/` — resolver |
| URI precedence: @ → Paymail, hex → direct, else → DNS | `bitfs/internal/engine/` or `libbitfs-go/` — URI parser |

**Step 3: Cross-reference with existing findings**

Check overlap with: C8 (HTLC initiator), C9 (wallet filename), H9 (DNS record format), H11/H12 (KDF params), M2 (storage directory name).

**Step 4: Produce findings in standard format**

Same format as Task 1.

---

## Task 3: BitFS Whitepaper §11–13 + References

**Sections:** Revenue Rights (ISO), The Metanet Network (summary), Conclusion, Bibliography

**Agent type:** `Explore` (read-only, thorough)

**Step 1: Extract claims from BitFS .en.tex §11–13**

Read `whitepaper/BitFS-Whitepaper.en.tex` lines 299–369. Focus on:

- §11 Revenue Rights: Share UTXOs as bearer instruments, Registry UTXO covenant, ISO Pool automated market maker, automatic payment distribution to shareholders, secondary market atomic swaps
- §12 The Metanet Network: BSV-homomorphic sidechain, MNT token, merged mining, `bitfs put --store metanet`, Method 42 storage proofs, dual currency model
- §13 Conclusion: Future extensions (BBS+ group sigs, sCrypt on-chain verification, CLTV time-locked access)
- References: Check all 6 citations for accuracy

**Step 2: Verify each claim against code**

| Whitepaper Claim | Code Location to Check |
|------------------|----------------------|
| Share UTXOs, Registry UTXO | `libbitfs-go/` — search for revshare, share, registry |
| ISO Pool covenant | Search for ISO, offering |
| `bitfs put --store metanet` flag | `bitfs/cmd/bitfs/` — put command flags |
| Metanet Chain sidechain | `metanet/chain/` — genesis, block structure |
| MNT token | `metanet/chain/` — token definition |
| Merged mining | `metanet/mining/` — AuxPoW |
| ECDH storage proofs | `metanet/proof/` — proof mechanism |
| BBS+ group signatures | Search codebase — likely unimplemented |
| sCrypt verification | Search codebase — likely unimplemented |
| CLTV time-locked access | Search codebase — likely unimplemented |

**Step 3: Verify references**

| Ref | Citation | Check |
|-----|----------|-------|
| [1] method42 | Wright, "An Immutable File and Data Store," nChain, 2025 | Is this the correct title/author/year? |
| [2] metanet | nChain, "The Metanet Technical Summary v1.0," 2020 | Correct? |
| [3] bip32 | Wuille, "BIP32," 2012 | Correct? |
| [4] spv | Nakamoto, "Bitcoin," 2008, Section 8 | Correct section for SPV? |
| [5] metanet_multi | GB2608179A, "Multi-level Blockchain," UKIPO, 2025 | Check patent exists, correct citation |
| [6] paymail | BSV Blockchain, "Paymail (bsvalias) Protocol" | Correct attribution? |

**Step 4: Produce findings**

Same format. Flag UNIMPLEMENTED for features claimed in conclusion that don't exist.

---

## Task 4: Metanet Whitepaper §1–6 (Architecture & Economics)

**Sections:** Introduction, Relationship to BitFS, Three-Layer Architecture, Metanet Chain Design, MNT Token Economics, Hot Data CDN

**Agent type:** `Explore` (read-only, thorough)

**Step 1: Extract claims from Metanet .en.tex §1–6**

Read `whitepaper/Metanet-Whitepaper.en.tex` lines 77–199. Focus on:

- §1 Introduction: Claims about IPFS/Filecoin/Arweave/CDN limitations, "invert Filecoin model"
- §2 Relationship to BitFS: Independence claim, shared Go libraries, independent binaries `bitfs` + `metanet`
- §3 Three-Layer Architecture: L1 BSV / L2 Daemon / L3 Metanet Chain, responsibilities division
- §4 Chain Design: BSV-homomorphic (same tx format, Script engine, UTXO model), merge-mining mechanism, anchor every ~100 blocks
- §5 Token Economics: 21M supply, 50 MNT initial reward, 210K block halving, ~10min block time, dual currency model
- §6 Hot Data: Self-organizing CDN, positive feedback loop, four properties (no contracts/proofs/replica-mgmt/coordinator needed), three data acquisition channels, node decision logic

**Step 2: Verify each claim against code**

| Whitepaper Claim | Code Location to Check |
|------------------|----------------------|
| Independent binaries: bitfs + metanet | `bitfs/cmd/` and `metanet/cmd/` — do both exist? |
| Shared Go libraries | `bitfs/go.mod` and `metanet/go.mod` — both import libbitfs-go? |
| BSV-homomorphic: same tx format | `metanet/chain/` — transaction structure |
| Same Script engine | `metanet/` — does it use BSV script? |
| Merge-mining AuxPoW | `metanet/mining/` — AuxPoW implementation |
| Anchor every ~100 blocks | `metanet/chain/` or `metanet/mining/` — anchor logic |
| 21M supply, 50 MNT reward, 210K halving | `metanet/chain/` — genesis config, subsidy calculation |
| ~10min block time | `metanet/chain/` — target block interval |
| x402 retrieval fees | `metanet/payment/` or cross-reference with bitfs daemon |
| Three data acquisition channels | `metanet/` — push/pull/wholesale logic |

**Step 3: Cross-reference with existing findings**

Check overlap with: C5 (staking), C6 (retrieval fee currency), C7 (three-layer architecture).

**Step 4: Produce findings**

Same format as previous tasks.

---

## Task 5: Metanet Whitepaper §7–12 + Academic Rigor

**Sections:** Cold Data Archive Contracts, Content Revenue Sharing, Payment Channels, Comparison Table, metanet CLI, Conclusion

**Plus:** Cross-paper consistency, all comparison tables fairness, all references accuracy

**Agent type:** `Explore` (read-only, very thorough)

**Step 1: Extract claims from Metanet .en.tex §7–12**

Read `whitepaper/Metanet-Whitepaper.en.tex` lines 201–361. Focus on:

- §7 Archive Contracts: Bitcoin Script on Metanet Chain, MNT locked into UTXOs per proof period, ECDH re-encryption storage proofs, Merkle challenge = SHA256(contract_txid || period_number), unpredictable challenge
- §8 Revenue Sharing: revenue_share parameter in metadata, 70/30 typical split, two cooperation modes
- §9 Payment Channels: 2-of-2 multisig, BSV channels (user↔node) + MNT channels (owner↔node, node↔node), x402 channel extension, dispute resolution with OP_CHECKSEQUENCEVERIFY
- §10 Comparison Table: Filecoin/IPFS/Arweave/Traditional CDN claims
- §11 metanet CLI: init/start/stop/status/contracts/peers/mine commands, "no minimum stake, no slashing"
- §12 Conclusion: Summary claims

**Step 2: Verify each claim against code**

| Whitepaper Claim | Code Location to Check |
|------------------|----------------------|
| Archive contracts in Bitcoin Script | `metanet/contract/` — script construction |
| ECDH re-encryption for storage proofs | `metanet/proof/` — ECDH usage |
| Merkle challenge-response | `metanet/proof/` — challenge derivation |
| revenue_share in file metadata | `libbitfs-go/metanet/node.go` — RevenueShare field, TLV tag |
| Payment channels 2-of-2 multisig | `metanet/payment/` — channel construction |
| OP_CHECKSEQUENCEVERIFY | `metanet/payment/` or `metanet/contract/` — script opcodes |
| metanet CLI commands | `metanet/cmd/` — registered commands |
| No minimum stake | `metanet/` — search for stake/staking |

**Step 3: Academic rigor audit**

**3a. Comparison tables (both whitepapers)**

For each competitor claim in comparison tables, check accuracy:

| Competitor | Claim to Verify | How to Check |
|------------|----------------|--------------|
| Filecoin: "zk-SNARK (hours of GPU)" | Is sealing really hours? | Check Filecoin docs |
| Filecoin: "Weak retrieval incentive" | Is retrieval market really weak? | Check Filecoin retrieval market status |
| IPFS: "None" for incentive | Is this accurate? | IPFS has no native incentive |
| Arweave: "SPoRA (moderate)" | Is SPoRA computation moderate? | Check Arweave docs |
| Arweave: "One-time storage fee" + "Free after upload" for users | Accurate? | Check Arweave model |
| Traditional CDN: "None (trusted)" for proofs | Fair characterization? | CDNs do have SLAs |

Flag any STRAW-MAN or MISSING-CAVEAT findings.

**3b. Argument logic**

Check for logical gaps:
- Does the "positive feedback loop" argument account for bootstrap problem (no content → no nodes → no content)?
- Does the dual-currency model introduction create complexity that contradicts "low barrier" claim?
- Does "millisecond ECDH" vs "hours GPU" comparison account for different security guarantees?
- Does "no minimum stake, no slashing" create Sybil attack risk?

**3c. Cross-paper consistency**

Compare technical claims that appear in both whitepapers:
- Method 42 description consistency
- Three-layer architecture consistency
- Storage proof description consistency
- HTLC flow consistency
- Dual currency model consistency

**3d. References (Metanet whitepaper)**

| Ref | Citation | Check |
|-----|----------|-------|
| [1] | BitFS Project, "BitFS: A Peer-to-Peer Encrypted File System," 2025 | Self-reference, OK |
| [2] | nChain, "Metanet Technical Summary v1.0," 2020 | Same as BitFS ref [2] |
| [3] | Wright, "An Immutable File and Data Store," nChain, 2025 | Same as BitFS ref [1] |
| [4] | Nakamoto, "Bitcoin," 2008 | Same as BitFS ref [4] |
| [5] | GB2608179A, "Multi-level Blockchain," UKIPO, 2025 | Same as BitFS ref [5] |
| [6] | Wuille, "BIP32," 2012 | Same as BitFS ref [3] |
| [7] | Protocol Labs, "Filecoin," 2017 | Correct year/attribution? |

**Step 4: Produce findings**

Same format, plus additional academic rigor findings:
```
### [ACAD-N] Short title

**Claim:** The whitepaper states...
**Issue:** STRAW-MAN / LOGIC-FLAW / MISSING-CAVEAT / REF-ERROR
**Analysis:** Why this is problematic
**Suggestion:** How to fix
```

---

## Task 6: Merge & Assemble Final Report

**Agent type:** `general-purpose`

**Step 1: Collect all findings from Tasks 1–5**

Read all 5 agent outputs.

**Step 2: Deduplicate and cross-reference**

- Merge duplicate findings (same issue found by multiple agents)
- Cross-reference with `docs/audits/2026-02-26-design-consistency.md` findings
- Mark which findings are NEW vs already known

**Step 3: Write the report**

Create `docs/audits/2026-02-26-whitepaper-accuracy.md` with this structure:

```markdown
# 白皮书技术准确性审查报告

**日期**: 2026-02-26
**范围**: BitFS 白皮书 (13 节) + Metanet 白皮书 (12 节)
**标准**: 以代码实现为权威基准
**审查员**: Claude Opus 4.6 (5 个并行审查 Agent)

---

## Executive Summary

[统计: N claims checked, X INACCURATE, Y OUTDATED, Z UNIMPLEMENTED, W OVERSTATED]
[Top 3 most critical findings]
[Academic rigor score]

---

## Part 1: BitFS 白皮书逐章审查

### §1 Introduction
[Findings or "All claims verified accurate"]

### §2 System Architecture
...

[Repeat for all 13 sections]

---

## Part 2: Metanet 白皮书逐章审查

### §1 Introduction
...

[Repeat for all 12 sections]

---

## Part 3: 两篇白皮书交叉一致性

[Findings where the two whitepapers contradict each other]

---

## Part 4: 学术严谨性评估

### 4.1 论证逻辑
[LOGIC-FLAW findings]

### 4.2 对比表公正性
[STRAW-MAN findings]

### 4.3 参考文献准确性
[REF-ERROR findings]

### 4.4 缺失说明
[MISSING-CAVEAT findings]

---

## Part 5: 修正建议优先级

### P0 — 发布前必须修正
[INACCURATE findings that would embarrass if published]

### P1 — 近期修正
[OUTDATED + OVERSTATED findings]

### P2 — 下一版本修正
[UNIMPLEMENTED features, minor issues]

---

## Appendix: 与设计一致性审查的重叠

[Table mapping this audit's findings to design-consistency audit C1-C9, H1-H12]
```

**Step 4: Verify completeness**

- Every section of both whitepapers has at least one entry (even if "all claims accurate")
- All existing design-consistency findings related to whitepapers are accounted for
- Statistics in Executive Summary match actual finding count

**Step 5: Commit**

```bash
git add docs/audits/2026-02-26-whitepaper-accuracy.md
git add docs/plans/2026-02-26-whitepaper-accuracy-design.md
git add docs/plans/2026-02-26-whitepaper-accuracy.md
git commit -m "docs(audit): whitepaper technical accuracy audit report"
```

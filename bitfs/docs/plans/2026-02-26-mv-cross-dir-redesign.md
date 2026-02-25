# Cross-Directory mv Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rewrite cross-directory mv to use DELETE + CreateChild pattern (new P_node, new encryption key, re-encrypted content).

**Architecture:** Cross-directory mv becomes a composite of copy.go (create new node with new identity) + remove.go (mark old node deleted). Same-directory rename is unchanged. The `moved_to` pointer is stored in the DELETE payload via tagLinkTarget (0x09).

**Tech Stack:** Go, libbitfs-go (metanet, method42, tx packages), testify

**Design doc:** `bitfs/docs/plans/2026-02-26-mv-cross-dir-redesign-design.md`

---

### Task 1: Add Force flag to MoveOpts

**Files:**
- Modify: `bitfs/internal/engine/move.go:8-13` (MoveOpts struct)

**Step 1: Add Force field**

```go
type MoveOpts struct {
	VaultIndex uint32
	SrcPath    string
	DstPath    string
	Force      bool // skip interactive warnings (for non-interactive/agent use)
}
```

**Step 2: Verify compilation**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go build ./...`
Expected: compiles cleanly

**Step 3: Commit**

```bash
git add bitfs/internal/engine/move.go
git commit -m "feat(engine): add Force flag to MoveOpts for non-interactive mv"
```

---

### Task 2: Rewrite crossDirectoryMove — tests first

Rewrite cross-directory tests to assert the new DELETE + CreateChild behavior. The key behavioral changes:
- New node gets a NEW PubKey (not the original)
- Old node is marked deleted (not just removed from parent)
- Content is re-encrypted with new key
- Result message mentions 4 txs (not 2)

**Files:**
- Modify: `bitfs/internal/engine/move_test.go:113-189` (TestMove_CrossDirectory)
- Modify: `bitfs/internal/engine/move_test.go:191-228` (TestMove_CrossDirectory_WithRename)
- Modify: `bitfs/internal/engine/move_test.go:379-457` (TestMove_CrossDirectory_FromRoot)
- Modify: `bitfs/internal/engine/move_test.go:318-377` (TestMove_CrossDirectory_SecondTxFailure_PreservesState)

**Step 1: Rewrite TestMove_CrossDirectory**

Replace the test at line 113. Key assertion changes:
- `movedNode.PubKeyHex != originalPubKey` (new identity, NOT equal)
- Destination child PubKey != originalPubKey
- Old node still exists in state but is deleted (node state removed from path index)
- Result message contains "4 txs"
- New node has re-encrypted content (different KeyHash from original)

```go
func TestMove_CrossDirectory(t *testing.T) {
	eng := setupMoveTestEngine(t)

	srcNode := eng.State.FindNodeByPath("/src/file.txt")
	require.NotNil(t, srcNode)
	originalPubKey := srcNode.PubKeyHex
	originalKeyHash := srcNode.KeyHash

	result, err := eng.Move(&MoveOpts{
		VaultIndex: 0,
		SrcPath:    "/src/file.txt",
		DstPath:    "/dst/file.txt",
		Force:      true,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.TxHex)
	assert.NotEmpty(t, result.TxID)
	assert.Contains(t, result.Message, "4 txs")

	// New node at destination has a DIFFERENT pubkey (new identity).
	movedNode := eng.State.FindNodeByPath("/dst/file.txt")
	require.NotNil(t, movedNode)
	assert.NotEqual(t, originalPubKey, movedNode.PubKeyHex, "cross-dir mv must create new identity")

	// Content re-encrypted with new key (different KeyHash).
	assert.NotEqual(t, originalKeyHash, movedNode.KeyHash, "cross-dir mv must re-encrypt")

	// Old path no longer resolves.
	assert.Nil(t, eng.State.FindNodeByPath("/src/file.txt"))

	// /src no longer lists file.txt.
	srcDir := eng.State.FindNodeByPath("/src")
	require.NotNil(t, srcDir)
	for _, c := range srcDir.Children {
		assert.NotEqual(t, "file.txt", c.Name)
	}

	// /dst lists file.txt with new pubkey.
	dstDir := eng.State.FindNodeByPath("/dst")
	require.NotNil(t, dstDir)
	found := false
	for _, c := range dstDir.Children {
		if c.Name == "file.txt" {
			found = true
			assert.NotEqual(t, originalPubKey, c.PubKey, "child entry must have new pubkey")
		}
	}
	assert.True(t, found, "'file.txt' should be in /dst children")
}
```

**Step 2: Rewrite TestMove_CrossDirectory_WithRename similarly**

Same pattern — assert new identity + re-encryption.

**Step 3: Rewrite TestMove_CrossDirectory_FromRoot similarly**

Same pattern — assert new identity.

**Step 4: Update TestMove_CrossDirectory_SecondTxFailure_PreservesState**

This test already tests the build-then-apply pattern. Keep the structure but update the assertion details since there are now 4 txs (the failure could occur at any build step). The test should still verify state is unchanged on failure.

**Step 5: Run tests to verify they FAIL**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestMove_Cross -v -count=1`
Expected: FAIL (old implementation still preserves identity)

**Step 6: Commit failing tests**

```bash
git add bitfs/internal/engine/move_test.go
git commit -m "test(engine): rewrite cross-dir mv tests for DELETE+CreateChild semantics"
```

---

### Task 3: Rewrite crossDirectoryMove implementation

**Files:**
- Modify: `bitfs/internal/engine/move.go:77-174` (crossDirectoryMove function)

The new implementation follows the pattern from `copy.go` (steps 2-12) for creating the new node, plus `remove.go` pattern for deleting the old node. The full flow:

1. Find source and destination parents (existing)
2. Check destination name collision (existing)
3. Read encrypted content from store via source KeyHash
4. Decrypt with source node's Method 42 key
5. Derive new child key at destination (new HD index)
6. Re-encrypt with new key
7. Store new encrypted content
8. Build Tx1: CreateChild at destination (new node)
9. Build Tx2: SelfUpdate destination parent (add child)
10. Build Tx3: SelfUpdate source node (op=DELETE, linkTarget=new P_node)
11. Build Tx4: SelfUpdate source parent (remove child)
12. All builds succeeded → apply state changes

Add imports needed: `"encoding/hex"`, `"time"`, `"github.com/tongxiaofeng/libbitfs-go/metanet"`, `"github.com/tongxiaofeng/libbitfs-go/method42"`.

For FREE-access files: skip decrypt/re-encrypt, reference same content.

**Step 1: Rewrite the crossDirectoryMove function**

Replace lines 77-174 of move.go. Model after copy.go for the create-new-node path, and remove.go for the delete-old-node path. Key points:

- Use `e.Wallet.DeriveNodeKey` to derive new child key (copy.go:83)
- Use `method42.Decrypt` + `method42.Encrypt` to re-encrypt (copy.go:56,90)
- Use `e.Store.Put` to store new content (copy.go:96)
- Build DELETE payload with `metanet.OpDelete` + `LinkTarget = new_P_node` (remove.go:38-43)
- Use `buildUnsignedCreateChildTx` for Tx1 (copy.go:166)
- Use `e.buildParentSelfUpdate` for Tx2 (dstParent) and Tx4 (srcParent)
- Use `buildUnsignedSelfUpdateTx` for Tx3 (delete old node, like remove.go:86)
- Build-then-apply: build all 4 before mutating any state
- UTXO allocation: need 4 fee UTXOs, 1 node UTXO (src), 1 parent UTXO (dst parent refresh)
- Defer UTXO rollback on failure (copy.go:158-163 pattern)

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -run TestMove -v -count=1`
Expected: all TestMove_* tests PASS

**Step 3: Run full engine test suite**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Expected: all tests pass

**Step 4: Commit**

```bash
git add bitfs/internal/engine/move.go
git commit -m "feat(engine): rewrite cross-dir mv as DELETE+CreateChild with re-encryption"
```

---

### Task 4: Add capsule warning to shell mv command

**Files:**
- Modify: `bitfs/cmd/bitfs/cmd_shell.go:200-214` (mv case)

**Step 1: Add interactive warning for PAID files on cross-directory mv**

Before calling `eng.Move()`, check if:
1. Source and destination are in different directories
2. Source file has `access == "paid"`

If both true, print warning and prompt for confirmation (unless running in non-interactive mode):

```go
case "mv":
    if len(cmdArgs) < 2 {
        fmt.Println("Usage: mv <src> <dst>")
        continue
    }
    srcPath := resolvePath(cwd, cmdArgs[0])
    dstPath := resolvePath(cwd, cmdArgs[1])
    force := false

    // Warn about capsule invalidation on cross-directory mv of paid files.
    if path.Dir(srcPath) != path.Dir(dstPath) {
        srcNode := eng.State.FindNodeByPath(srcPath)
        if srcNode != nil && srcNode.Access == "paid" {
            fmt.Println("WARNING: Moving this paid file will invalidate existing capsules.")
            fmt.Println("Buyers will need to re-purchase access at the new location.")
            fmt.Print("Continue? [y/N] ")
            var confirm string
            fmt.Scanln(&confirm)
            if confirm != "y" && confirm != "Y" {
                fmt.Println("Move cancelled.")
                continue
            }
        }
    }

    result, mvErr := eng.Move(&engine.MoveOpts{
        VaultIndex: vaultIdx,
        SrcPath:    srcPath,
        DstPath:    dstPath,
        Force:      force,
    })
```

**Step 2: Verify compilation**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go build ./cmd/bitfs`
Expected: compiles cleanly

**Step 3: Commit**

```bash
git add bitfs/cmd/bitfs/cmd_shell.go
git commit -m "feat(shell): add capsule invalidation warning for cross-dir mv of paid files"
```

---

### Task 5: Update design documents

**Files:**
- Modify: `design/bitfs/2-SystemDesign.zh.md:215` (mv row in operations table)
- Modify: `design/bitfs/3-DetailedDesign.zh.md:801-840` (cross-dir mv section)

**Step 1: Update SystemDesign mv row**

Replace line 215:
```
| `mv` (跨目录) | DELETE 旧节点 + CreateChild 新节点 (4 笔交易) | 新 HD 路径, 新 P_node, 重新加密, 旧节点记录 moved_to 指针 |
```

**Step 2: Update DetailedDesign cross-dir mv section**

Replace lines 801-840 with the new 4-transaction flow matching the design doc:

```
#### 7. mv (跨目录移动)

前置条件:
  - srcParent.PNode != dstParent.PNode
  - 4 个 fee UTXO

交易组合 (4 笔):

  Tx 1: BuildCreateChild (目标新节点)
    Input 0:  P_dstParent UTXO (Sig D_dstParent)
    Input 1:  fee UTXO[0]
    Output 0: OP_RETURN { 克隆源 payload, op=CREATE,
              index=dst_next_child_index, parent=P_dstParent,
              key_hash=新密钥哈希 }
    Output 1: P2PKH → P_dst_new (1 sat)
    Output 2: P2PKH → P_dstParent (1 sat, refresh)
    Output 3: Change

  Tx 2: BuildSelfUpdate (目标父目录更新)
    Input 0:  P_dstParent UTXO (来自 Tx1 Output 2)
    Input 1:  fee UTXO[1]
    Output 0: OP_RETURN { type=DIR, op=UPDATE,
              children=[...,新 ChildEntry(pubkey=P_dst_new)],
              next_child_index=old+1 }

  Tx 3: BuildSelfUpdate (源节点 DELETE + moved_to)
    Input 0:  P_src UTXO (Sig D_src)
    Input 1:  fee UTXO[2]
    Output 0: OP_RETURN { type=FILE, op=DELETE,
              link_target=P_dst_new(33B) }

  Tx 4: BuildSelfUpdate (源父目录更新)
    Input 0:  P_srcParent UTXO (Sig D_srcParent)
    Input 1:  fee UTXO[3]
    Output 0: OP_RETURN { type=DIR, op=UPDATE,
              children=[...移除 srcName...] }

HD 密钥: 目标节点获得新 HD 路径 (dstParent path + dst_index)
注: 已购买 capsule 失效 — mv 命令需警告用户
```

**Step 3: Commit**

```bash
git add design/bitfs/2-SystemDesign.zh.md design/bitfs/3-DetailedDesign.zh.md
git commit -m "docs: update design docs for cross-dir mv DELETE+CreateChild semantics"
```

---

### Task 6: Update spec and run full test suite

**Files:**
- Modify: `bitfs/spec/10-cmd-bitfs.md:21` (mv description)

**Step 1: Update spec description**

Add a note after the mv line:
```
bitfs mv <src> <dst>           # 移动/重命名
  同目录: 仅修改父目录 ChildEntry.Name (1 笔交易)
  跨目录: DELETE 旧节点 + CreateChild 新节点, 新密钥, 重新加密 (4 笔交易)
  注意: 跨目录 mv 付费文件会使已购买 capsule 失效
```

**Step 2: Run full test suite**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1`
Expected: all tests pass

**Step 3: Run integration tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -count=1 -timeout 120s`
Expected: all 276 integration tests pass

**Step 4: Commit**

```bash
git add bitfs/spec/10-cmd-bitfs.md
git commit -m "spec: update mv command spec for cross-dir DELETE+CreateChild behavior"
```

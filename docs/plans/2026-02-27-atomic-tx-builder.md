# Atomic Transaction Builder Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor the tx package so MutationBatch is the sole transaction build path, then migrate all engine operations to use it for atomic multi-op transactions.

**Architecture:** Extend `tx/batch.go` to handle all four transaction templates (CreateRoot, CreateChild, SelfUpdate, Delete), including DELETE ops that produce no P2PKH refresh output and parent UTXO dedup. Add `SignBatchTx` for signing. Then migrate each engine operation from sequential Build*/Sign* calls to a single Batch build+sign. Finally, delete the deprecated single-template API.

**Tech Stack:** Go 1.25.6, `github.com/bsv-blockchain/go-sdk` (transaction, script, ec, chainhash, p2pkh)

**Design doc:** `docs/plans/2026-02-27-atomic-tx-builder-design.md`

---

## Phase 1: tx Layer Refactoring

### Task 1: Extend BatchOpType and BatchNodeOp

**Files:**
- Modify: `libbitfs-go/tx/batch.go`
- Test: `libbitfs-go/tx/batch_test.go`

**Context:** Current `BatchOpType` has 4 values: `BatchOpParentUpdate`, `BatchOpChildCreate`, `BatchOpChildDelete`, `BatchOpNodeUpdate`. These need to be renamed for clarity and `OpCreateRoot` added.

**Step 1: Write failing tests for new op types**

In `batch_test.go`, add tests for:

```go
func TestMutationBatch_OpCreateRoot(t *testing.T) {
	// OpCreateRoot: no InputUTXO, produces OP_RETURN + P2PKH
	batch := tx.NewMutationBatch()

	pub, _ := generateTestKeyPair()
	batch.AddNodeOp(tx.BatchNodeOp{
		Type:    tx.OpCreateRoot,
		PubKey:  pub,
		Payload: []byte("root-payload"),
		// No InputUTXO, no PrivateKey, no ParentTxID
	})
	batch.AddFeeInput(testFeeUTXO(5000))

	result, err := batch.Build()
	require.NoError(t, err)
	require.Len(t, result.NodeOps, 1)
	assert.Equal(t, uint32(0), result.NodeOps[0].OpReturnVout)
	assert.Equal(t, uint32(1), result.NodeOps[0].NodeVout)
	assert.NotNil(t, result.NodeOps[0].NodeUTXO)
}

func TestMutationBatch_OpDelete_NoRefresh(t *testing.T) {
	// OpDelete: has InputUTXO (spent), produces OP_RETURN but NO P2PKH (node dies)
	batch := tx.NewMutationBatch()

	pub, priv := generateTestKeyPair()
	batch.AddNodeOp(tx.BatchNodeOp{
		Type:       tx.OpDelete,
		PubKey:     pub,
		ParentTxID: bytes.Repeat([]byte{0xaa}, 32),
		Payload:    []byte("delete-payload"),
		InputUTXO:  &tx.UTXO{TxID: bytes.Repeat([]byte{0x01}, 32), Vout: 0, Amount: 1, ScriptPubKey: []byte{0x76}},
		PrivateKey: priv,
	})
	batch.AddFeeInput(testFeeUTXO(5000))

	result, err := batch.Build()
	require.NoError(t, err)
	require.Len(t, result.NodeOps, 1)
	assert.Equal(t, uint32(0), result.NodeOps[0].OpReturnVout)
	// DELETE: NodeVout should be 0 (sentinel) and NodeUTXO should be nil
	assert.Nil(t, result.NodeOps[0].NodeUTXO)
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./tx/ -run "TestMutationBatch_OpCreateRoot|TestMutationBatch_OpDelete_NoRefresh" -v`
Expected: FAIL (OpCreateRoot and OpDelete constants not defined, current Build() always produces P2PKH)

**Step 3: Implement op type changes in batch.go**

Replace the existing BatchOpType constants:

```go
type BatchOpType int

const (
	OpCreate     BatchOpType = iota // Create a new child node (OP_RETURN + P2PKH refresh)
	OpUpdate                        // Update existing node (OP_RETURN + P2PKH refresh)
	OpDelete                        // Delete node (OP_RETURN only, no P2PKH — UTXO dies)
	OpCreateRoot                    // Create root node (no input UTXO, OP_RETURN + P2PKH refresh)
)
```

Update `Build()` to handle OpDelete (no P2PKH output) and OpCreateRoot (no InputUTXO):

- In validation: OpCreateRoot should NOT require InputUTXO or PrivateKey. OpDelete should require InputUTXO.
- In output loop: skip P2PKH dust output for OpDelete ops.
- In dust calculation: only count non-Delete ops for `totalDust`.
- In BatchNodeResult for Delete: set NodeUTXO to nil, NodeVout to 0.

**Step 4: Run tests to verify they pass**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./tx/ -run "TestMutationBatch" -v`
Expected: ALL pass

**Step 5: Update existing tests for renamed constants**

Replace all occurrences in `batch_test.go`:
- `BatchOpParentUpdate` → `OpUpdate`
- `BatchOpChildCreate` → `OpCreate`
- `BatchOpChildDelete` → `OpDelete`
- `BatchOpNodeUpdate` → `OpUpdate`

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./tx/ -v`
Expected: ALL pass

**Step 6: Commit**

```
feat(tx): extend BatchOpType with OpCreateRoot and OpDelete semantics

OpDelete produces OP_RETURN but no P2PKH refresh (node UTXO dies).
OpCreateRoot has no input UTXO (root node creation).
Renamed op type constants for clarity.
```

---

### Task 2: Parent UTXO Deduplication in Build()

**Files:**
- Modify: `libbitfs-go/tx/batch.go`
- Test: `libbitfs-go/tx/batch_test.go`

**Context:** When a batch contains OpCreate(child) + OpUpdate(parent), both reference the same parent UTXO. The parent's UTXO should only appear as one input, not two. The OpCreate spends the parent UTXO (Metanet edge), and OpUpdate refreshes it.

**Step 1: Write failing test for parent dedup**

```go
func TestMutationBatch_ParentDedup(t *testing.T) {
	// mkdir scenario: OpCreate(child) spends parent UTXO + OpUpdate(parent) refreshes it
	// The parent UTXO should appear as ONE input, not two.
	batch := tx.NewMutationBatch()

	parentPub, parentPriv := generateTestKeyPair()
	childPub, _ := generateTestKeyPair()
	parentTxID := bytes.Repeat([]byte{0xbb}, 32)
	parentUTXO := &tx.UTXO{
		TxID: bytes.Repeat([]byte{0x01}, 32), Vout: 1, Amount: 1,
		ScriptPubKey: []byte{0x76}, PrivateKey: parentPriv,
	}

	// OpCreate for child — spends parent's UTXO as Metanet edge
	batch.AddNodeOp(tx.BatchNodeOp{
		Type:       tx.OpCreate,
		PubKey:     childPub,
		ParentTxID: parentTxID,
		Payload:    []byte("child-payload"),
		InputUTXO:  parentUTXO,
		PrivateKey: parentPriv,
	})

	// OpUpdate for parent — refreshes the same parent UTXO
	// InputUTXO is the SAME as OpCreate's InputUTXO
	batch.AddNodeOp(tx.BatchNodeOp{
		Type:       tx.OpUpdate,
		PubKey:     parentPub,
		ParentTxID: bytes.Repeat([]byte{0x00}, 32), // parent's own parent
		Payload:    []byte("parent-updated-children"),
		InputUTXO:  parentUTXO, // same UTXO — should be deduped
		PrivateKey: parentPriv,
	})

	batch.AddFeeInput(testFeeUTXO(5000))
	result, err := batch.Build()
	require.NoError(t, err)

	// Parse the raw tx to count inputs
	sdkTx, err := transaction.NewTransactionFromBytes(result.RawTx)
	require.NoError(t, err)

	// Should have 2 inputs (1 deduped parent + 1 fee), not 3
	assert.Len(t, sdkTx.Inputs, 2, "parent UTXO should be deduped to one input")

	// Both ops should still produce their outputs
	assert.Len(t, result.NodeOps, 2)
	assert.NotNil(t, result.NodeOps[0].NodeUTXO) // child P2PKH
	assert.NotNil(t, result.NodeOps[1].NodeUTXO) // parent refresh P2PKH
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./tx/ -run "TestMutationBatch_ParentDedup" -v`
Expected: FAIL (3 inputs instead of 2)

**Step 3: Implement parent dedup in Build()**

In the input construction loop, track seen UTXOs by `TxID:Vout`:

```go
// Dedup: track unique (TxID, Vout) pairs for node inputs.
type utxoKey struct {
	txid string // hex
	vout uint32
}
seenInputs := make(map[utxoKey]bool)

for _, op := range b.ops {
	if op.InputUTXO == nil {
		continue
	}
	key := utxoKey{hex.EncodeToString(op.InputUTXO.TxID), op.InputUTXO.Vout}
	if seenInputs[key] {
		continue // skip duplicate
	}
	seenInputs[key] = true
	// ... add input as before
}
```

Also update `numNodeInputs` counting to match.

Also update the signing UTXO array builder (in Task 3) to match this dedup.

**Step 4: Run all batch tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./tx/ -run "TestMutationBatch" -v`
Expected: ALL pass

**Step 5: Commit**

```
feat(tx): deduplicate parent UTXO inputs in MutationBatch

When multiple ops reference the same UTXO (e.g., OpCreate spends
parent UTXO as Metanet edge, OpUpdate refreshes same parent),
the UTXO is only added as one input. Prevents double-spending.
```

---

### Task 3: Add SignBatchTx Function

**Files:**
- Modify: `libbitfs-go/tx/batch.go` (add SignBatchTx)
- Test: `libbitfs-go/tx/batch_test.go`

**Context:** `SignMetanetTx` works with `MetanetTx` which has fixed UTXO slots (NodeUTXO, ParentUTXO, ChangeUTXO). For batch transactions we need `SignBatchTx` that builds the signing UTXO array from the ops + feeInputs, respecting the dedup order.

**Step 1: Write failing test**

```go
func TestSignBatchTx_SingleOp(t *testing.T) {
	// Build a single-op batch and sign it
	batch := tx.NewMutationBatch()
	pub, priv := generateTestKeyPair()

	nodeUTXO := &tx.UTXO{
		TxID: bytes.Repeat([]byte{0x01}, 32), Vout: 0, Amount: 1,
		ScriptPubKey: buildTestP2PKH(pub), PrivateKey: priv,
	}
	feeUTXO := &tx.UTXO{
		TxID: bytes.Repeat([]byte{0x02}, 32), Vout: 0, Amount: 5000,
		ScriptPubKey: buildTestP2PKH(pub), PrivateKey: priv,
	}

	batch.AddNodeOp(tx.BatchNodeOp{
		Type:       tx.OpUpdate,
		PubKey:     pub,
		ParentTxID: bytes.Repeat([]byte{0xcc}, 32),
		Payload:    []byte("update-payload"),
		InputUTXO:  nodeUTXO,
		PrivateKey: priv,
	})
	batch.AddFeeInput(feeUTXO)

	result, err := batch.Build()
	require.NoError(t, err)

	hex, err := tx.SignBatchTx(result, batch)
	require.NoError(t, err)
	assert.NotEmpty(t, hex)

	// TxID should be set on all NodeUTXOs
	assert.NotNil(t, result.NodeOps[0].NodeUTXO.TxID)
}
```

Note: `SignBatchTx` needs access to the ops and feeInputs to reconstruct the signing UTXO array. We can either pass the batch itself or extract the needed data. Passing the batch is cleanest.

**Step 2: Run test to verify it fails**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./tx/ -run "TestSignBatchTx" -v`
Expected: FAIL (SignBatchTx not defined)

**Step 3: Implement SignBatchTx**

Add to `batch.go`:

```go
// SignBatchTx signs a BatchResult using the private keys from the batch's ops and fee inputs.
// It reconstructs the signing UTXO array in the same order as Build() added inputs:
// deduped node inputs first, then fee inputs.
func SignBatchTx(result *BatchResult, batch *MutationBatch) (string, error) {
	if result == nil || len(result.RawTx) == 0 {
		return "", fmt.Errorf("%w: BatchResult", ErrNilParam)
	}

	// Reconstruct signing UTXOs in input order (matching Build).
	var signingUTXOs []*UTXO

	// 1. Deduped node inputs (same order as Build).
	type utxoKey struct {
		txid string
		vout uint32
	}
	seen := make(map[utxoKey]bool)
	for _, op := range batch.ops {
		if op.InputUTXO == nil {
			continue
		}
		key := utxoKey{hex.EncodeToString(op.InputUTXO.TxID), op.InputUTXO.Vout}
		if seen[key] {
			continue
		}
		seen[key] = true
		signingUTXOs = append(signingUTXOs, op.InputUTXO)
	}

	// 2. Fee inputs.
	signingUTXOs = append(signingUTXOs, batch.feeInputs...)

	// Wrap in a MetanetTx for SignMetanetTx compatibility.
	mtx := &MetanetTx{RawTx: result.RawTx}
	txHex, err := SignMetanetTx(mtx, signingUTXOs)
	if err != nil {
		return "", err
	}

	// Propagate TxID to BatchResult and all NodeUTXOs.
	result.RawTx = mtx.RawTx
	result.TxID = mtx.TxID
	for i := range result.NodeOps {
		if result.NodeOps[i].NodeUTXO != nil {
			result.NodeOps[i].NodeUTXO.TxID = mtx.TxID
		}
	}
	if result.ChangeUTXO != nil {
		result.ChangeUTXO.TxID = mtx.TxID
	}

	return txHex, nil
}
```

Note: `SignBatchTx` needs access to `batch.ops` and `batch.feeInputs`. Either make them exported or pass the batch. Since `MutationBatch` fields are unexported, either:
- (a) Add a method `(b *MutationBatch) Sign(result *BatchResult) (string, error)` — cleaner API
- (b) Export the fields

**Recommended: Option (a)** — add as method:

```go
func (b *MutationBatch) Sign(result *BatchResult) (string, error)
```

This keeps the API clean: `batch.Build()` then `batch.Sign(result)`.

**Step 4: Run all tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./tx/ -v`
Expected: ALL pass

**Step 5: Write test for multi-op signing (cross-directory mv scenario)**

```go
func TestSignBatchTx_MultiOp_CrossMove(t *testing.T) {
	// Simulates cross-directory move: 4 ops, deduped inputs
	batch := tx.NewMutationBatch()

	srcPub, srcPriv := generateTestKeyPair()
	srcParentPub, srcParentPriv := generateTestKeyPair()
	dstParentPub, dstParentPriv := generateTestKeyPair()
	dstChildPub, _ := generateTestKeyPair()

	// ... build 4 ops, sign, verify txHex is non-empty and all NodeUTXOs have TxID set
}
```

**Step 6: Commit**

```
feat(tx): add MutationBatch.Sign() for batch transaction signing

Reconstructs signing UTXO array matching Build()'s input order
(deduped node inputs + fee inputs), signs via SignMetanetTx,
and propagates TxID to all BatchNodeResult UTXOs.
```

---

### Task 4: Add Convenience Builders for Common Patterns

**Files:**
- Modify: `libbitfs-go/tx/batch.go`
- Test: `libbitfs-go/tx/batch_test.go`

**Context:** To make engine migration easier, add helper methods on MutationBatch for common filesystem operations. These are thin wrappers that add the right ops.

**Step 1: Write tests**

```go
func TestMutationBatch_AddCreateChild(t *testing.T) {
	batch := tx.NewMutationBatch()
	childPub, _ := generateTestKeyPair()
	parentPub, parentPriv := generateTestKeyPair()
	parentTxID := bytes.Repeat([]byte{0xaa}, 32)
	parentUTXO := &tx.UTXO{
		TxID: bytes.Repeat([]byte{0x01}, 32), Vout: 1, Amount: 1,
		ScriptPubKey: buildTestP2PKH(parentPub), PrivateKey: parentPriv,
	}

	batch.AddCreateChild(childPub, parentTxID, []byte("payload"), parentUTXO, parentPriv)

	// Should have added one OpCreate op
	assert.Equal(t, 1, batch.OpCount())
}
```

**Step 2: Implement helpers**

```go
// AddCreateChild adds an OpCreate op for a new child node.
// The parentUTXO is the parent's P_node UTXO to spend (Metanet edge).
func (b *MutationBatch) AddCreateChild(childPub *ec.PublicKey, parentTxID []byte, payload []byte, parentUTXO *UTXO, parentPriv *ec.PrivateKey) {
	b.AddNodeOp(BatchNodeOp{
		Type:       OpCreate,
		PubKey:     childPub,
		ParentTxID: parentTxID,
		Payload:    payload,
		InputUTXO:  parentUTXO,
		PrivateKey: parentPriv,
	})
}

// AddSelfUpdate adds an OpUpdate op for updating an existing node.
func (b *MutationBatch) AddSelfUpdate(nodePub *ec.PublicKey, parentTxID []byte, payload []byte, nodeUTXO *UTXO, nodePriv *ec.PrivateKey) {
	b.AddNodeOp(BatchNodeOp{
		Type:       OpUpdate,
		PubKey:     nodePub,
		ParentTxID: parentTxID,
		Payload:    payload,
		InputUTXO:  nodeUTXO,
		PrivateKey: nodePriv,
	})
}

// AddDelete adds an OpDelete op. The node's UTXO is spent but no refresh is produced.
func (b *MutationBatch) AddDelete(nodePub *ec.PublicKey, parentTxID []byte, payload []byte, nodeUTXO *UTXO, nodePriv *ec.PrivateKey) {
	b.AddNodeOp(BatchNodeOp{
		Type:       OpDelete,
		PubKey:     nodePub,
		ParentTxID: parentTxID,
		Payload:    payload,
		InputUTXO:  nodeUTXO,
		PrivateKey: nodePriv,
	})
}

// AddCreateRoot adds an OpCreateRoot op. No input UTXO needed.
func (b *MutationBatch) AddCreateRoot(rootPub *ec.PublicKey, payload []byte) {
	b.AddNodeOp(BatchNodeOp{
		Type:    OpCreateRoot,
		PubKey:  rootPub,
		Payload: payload,
	})
}

// OpCount returns the number of operations in the batch.
func (b *MutationBatch) OpCount() int {
	return len(b.ops)
}
```

**Step 3: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./tx/ -v`
Expected: ALL pass

**Step 4: Commit**

```
feat(tx): add convenience builders for common batch operations

AddCreateChild, AddSelfUpdate, AddDelete, AddCreateRoot provide
typed helpers that map filesystem semantics to BatchNodeOps.
```

---

## Phase 2: Engine Layer Adaptation

### Task 5: Replace txbuild.go With Batch-Based Helper

**Files:**
- Modify: `bitfs/internal/engine/txbuild.go`
- Test: existing engine tests

**Context:** Replace the 6 wrapper functions (buildUnsigned*, sign*) with a single `buildAndSignBatch` helper that uses MutationBatch.

**Step 1: Add new batch helper to txbuild.go**

```go
// buildAndSignBatch builds and signs a MutationBatch transaction.
// Returns the signed tx hex and the BatchResult (with TxID set on all UTXOs).
func buildAndSignBatch(batch *tx.MutationBatch) (string, *tx.BatchResult, error) {
	result, err := batch.Build()
	if err != nil {
		return "", nil, fmt.Errorf("batch build: %w", err)
	}
	txHex, err := batch.Sign(result)
	if err != nil {
		return "", nil, fmt.Errorf("batch sign: %w", err)
	}
	return txHex, result, nil
}
```

Keep the old wrappers for now — they'll be removed after all operations are migrated.

**Step 2: Run existing tests to verify nothing breaks**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Expected: ALL pass

**Step 3: Commit**

```
feat(engine): add buildAndSignBatch helper for MutationBatch
```

---

### Task 6: Migrate Mkdir to Batch

**Files:**
- Modify: `bitfs/internal/engine/mkdir.go`
- Modify: `bitfs/internal/engine/helpers.go` (EnsureRootExists)
- Test: `bitfs/internal/engine/mkdir_test.go` (if exists), integration tests

**Context:** Current `Mkdir` calls `buildUnsignedCreateChildTx` + `signCreateChildTx`. The CreateChild template produces: Input[0]=parentUTXO, Input[1]=feeUTXO, Output[0]=OP_RETURN, Output[1]=P_child, Output[2]=P_parent_refresh, Output[3]=change.

In the new model, `Mkdir` uses a batch with OpCreate(child) + OpUpdate(parent). But wait — the current CreateChild template includes a parent refresh output (Output 2), which is equivalent to what OpUpdate(parent) would produce. So for mkdir, we can simplify:

**Option A (simpler):** Single OpCreate that produces child OP_RETURN + child P2PKH + parent refresh P2PKH. This requires extending OpCreate to optionally include a parent refresh output.

**Option B (consistent with design):** Two ops: OpCreate(child) + OpUpdate(parent). OpUpdate(parent) produces the parent's updated children list on-chain AND refreshes the parent's UTXO.

**Go with Option B** for consistency with the design doc. The parent directory needs an on-chain update anyway (updated children list + MerkleRoot). Having an explicit OpUpdate(parent) makes this visible.

**Step 1: Rewrite Mkdir to use batch**

Replace the build/sign section of `Mkdir`:

```go
// Build batch: OpCreate(child) + OpUpdate(parent)
batch := tx.NewMutationBatch()

// Op 1: Create child node (spends parent UTXO as Metanet edge)
batch.AddCreateChild(childKP.PublicKey, parentTxID, payload, parentUTXO, parentUTXO.PrivateKey)

// Op 2: Update parent directory (updated children list)
// Build parent update payload with new child added
parentChildren := append(parent.Children, &ChildState{...})
parentPayload := serializeParentPayload(parent, parentChildren, childIdx+1)
batch.AddSelfUpdate(parentKP.PublicKey, parentParentTxID, parentPayload, parentUTXO, parentKP.PrivateKey)

batch.AddFeeInput(feeUTXO)
batch.SetChange(changeAddr)

txHex, result, err := buildAndSignBatch(batch)
```

Wait — there's a subtlety. OpCreate(child) and OpUpdate(parent) both reference parentUTXO as InputUTXO. The parent dedup logic (Task 2) ensures it's only one input. Good.

But we need to think about what gets produced:
- OpCreate: OP_RETURN(child) + P2PKH(child) — the child's metadata + dust
- OpUpdate: OP_RETURN(parent) + P2PKH(parent) — the parent's updated children + refresh

This means the tx has 2 OP_RETURNs (one for child, one for parent). This is correct and matches the design.

**Important consideration:** In the old model, CreateChild only wrote ONE OP_RETURN (the child's). The parent's state was NOT updated on-chain in the same transaction — it was either updated in a separate SelfUpdate or not at all (relying on local state only).

Looking at the current code: `Mkdir` only does CreateChild. It updates `parent.Children` locally but does NOT broadcast a parent SelfUpdate transaction. The parent's on-chain OP_RETURN is NOT updated. The only on-chain parent reference is the CreateChild's parent refresh UTXO (Output 2).

So the question is: **should the new batch always write the parent's updated state on-chain too?**

For this migration, let's stay compatible with the current behavior: the batch does OpCreate(child) ONLY. The parent's UTXO is refreshed via the OpCreate's mechanism (which already exists in the current code as Output 2 of CreateChild).

But the current batch.Build() doesn't have the concept of "parent refresh from OpCreate". Let me reconsider...

Actually, looking at the current batch.go more carefully: it does NOT have a parent refresh mechanism. Every op produces OP_RETURN + P2PKH for the op's own node. The CreateChild template's parent refresh is a special feature of the old BuildUnsignedCreateChildTx.

**Revised approach:** For OpCreate, we need to optionally produce a parent refresh P2PKH output. Add a `ParentPubKey` field to `BatchNodeOp`. When set and Type is OpCreate, Build() produces an additional P2PKH output for the parent.

Actually wait — this re-introduces complexity. Let me re-think the whole approach.

**The key insight:** In the current codebase, mkdir does ONE transaction that:
1. Spends parent UTXO (Input 0) — Metanet edge
2. Produces child OP_RETURN (Output 0)
3. Produces child P2PKH dust (Output 1)
4. Produces parent P2PKH refresh (Output 2) — so parent can be spent again
5. Produces change (Output 3)

In the new batch model, to replicate this exactly, the batch needs OpCreate + a parent refresh. Two options:

**Option A: Add ParentPubKey to OpCreate** — when OpCreate has ParentPubKey set, Build() emits an extra P2PKH output for the parent. This keeps the single-tx behavior identical to current.

**Option B: Use OpCreate + OpUpdate(parent)** — OpUpdate(parent) writes parent's updated children on-chain AND produces the refresh. More data on-chain but more consistent.

**Go with Option A** for minimal change. The parent update on-chain is separate and optional (can add later if desired).

This means:
1. Add `ParentPubKey *ec.PublicKey` to `BatchNodeOp`
2. When OpCreate has ParentPubKey set, Build() produces an extra P2PKH dust output after the child's P2PKH
3. Track this in a new `ParentUTXO *UTXO` field in BatchNodeResult

Let me revise the tasks accordingly...

Actually, I realize I'm overcomplicating this. Let me step back and think about what the design doc says:

> | mkdir | OpCreate(child) + OpUpdate(parent) |

The design explicitly says two ops. And the "parent refresh" in the old CreateChild is essentially a hack — it refreshes the parent's UTXO without updating the parent's on-chain state. In the new model, we can do it properly: OpUpdate(parent) writes the updated children list AND refreshes the UTXO.

**But this changes what goes on-chain.** Currently mkdir writes one OP_RETURN (child). With the new model, mkdir writes TWO OP_RETURNs (child + parent update). This is more data on-chain but also more correct — the parent's children list is now recorded on-chain.

**Decision: Follow the design doc. OpCreate + OpUpdate.** The extra on-chain data (parent OP_RETURN) is a feature, not a bug — it makes the DAG self-describing.

So the implementation for Mkdir is:

```go
batch := tx.NewMutationBatch()

// Op 1: Create child
batch.AddCreateChild(childKP.PublicKey, parentTxID, childPayload, parentUTXO, parentUTXO.PrivateKey)

// Op 2: Update parent (add child to children list)
batch.AddSelfUpdate(parentKP.PublicKey, parentParentTxID, parentPayload, parentUTXO, parentKP.PrivateKey)
// ^^ Same parentUTXO — gets deduped to one input
```

And the tx will have:
- Input[0]: parentUTXO (deduped, spent once)
- Input[1]: feeUTXO
- Output[0]: OP_RETURN child
- Output[1]: P2PKH child (1 sat)
- Output[2]: OP_RETURN parent (updated children)
- Output[3]: P2PKH parent (1 sat, refresh)
- Output[4]: change

This is correct and clean.

**Step 2: Build parent payload helper**

Extract the parent payload building from `buildParentSelfUpdate` into a reusable function:

```go
func (e *Engine) buildParentPayload(parent *NodeState) ([]byte, error) {
	// ... same logic as in buildParentSelfUpdate
}
```

**Step 3: Implement Mkdir rewrite**

The full Mkdir body will:
1. EnsureRootExists
2. ResolveParentNode, derive child key, build child payload (unchanged)
3. Derive parent key, build parent payload
4. Allocate UTXOs (parentUTXO, feeUTXO)
5. Build batch with OpCreate + OpUpdate
6. Sign
7. Update local state

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -count=1 -run Mkdir`
Expected: ALL pass

**Step 5: Commit**

```
feat(engine): migrate Mkdir to atomic batch transaction

Mkdir now uses MutationBatch with OpCreate(child) + OpUpdate(parent)
in a single atomic transaction. Parent's updated children list is
written on-chain alongside child creation.
```

---

### Task 7: Migrate PutFile to Batch

**Files:**
- Modify: `bitfs/internal/engine/put.go`

**Context:** Same pattern as Mkdir — OpCreate(child) + OpUpdate(parent). Plus the existing encryption/storage logic stays unchanged.

**Step 1: Rewrite PutFile build/sign section to use batch**

Same batch pattern as Mkdir. The child payload includes file metadata (mime type, size, KeyHash). The parent payload includes updated children list.

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -count=1 -run "Put|Upload"`
Expected: ALL pass

**Step 3: Commit**

```
feat(engine): migrate PutFile to atomic batch transaction
```

---

### Task 8: Migrate EnsureRootExists to Batch

**Files:**
- Modify: `bitfs/internal/engine/helpers.go`

**Context:** `buildAndSignRootTx` currently uses `buildUnsignedCreateRootTx` + `signCreateRootTx`. Replace with batch using OpCreateRoot.

**Step 1: Rewrite buildAndSignRootTx**

```go
batch := tx.NewMutationBatch()
batch.AddCreateRoot(kp.PublicKey, payload)
batch.AddFeeInput(feeUTXO)
batch.SetChange(changeAddr)
txHex, result, err := buildAndSignBatch(batch)
```

**Step 2: Update UTXO tracking**

Map BatchResult outputs to the expected state tracking:
- `result.NodeOps[0].NodeUTXO` → track as root node UTXO
- `result.ChangeUTXO` → track as change UTXO

**Step 3: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Expected: ALL pass

**Step 4: Commit**

```
feat(engine): migrate EnsureRootExists to batch OpCreateRoot
```

---

### Task 9: Migrate buildParentSelfUpdate to Batch

**Files:**
- Modify: `bitfs/internal/engine/dir_update.go`

**Context:** `buildParentSelfUpdate` is used by copy, move (same-dir), remove, link, sell. It does a standalone SelfUpdate. Replace with batch using single OpUpdate.

**Step 1: Rewrite buildParentSelfUpdate**

```go
func (e *Engine) buildParentSelfUpdate(parent *NodeState) (txHex string, txIDHex string, err error) {
	parentKP, err := e.Wallet.DeriveNodeKey(parent.VaultIndex, parent.ChildIndices, nil)
	if err != nil { return "", "", err }

	parentPayload, err := e.buildParentPayload(parent)
	if err != nil { return "", "", err }

	parentTxIDBytes := ...
	parentUTXO, parentUS, err := e.getNodeUTXOWithState(parent.PubKeyHex)
	if err != nil { return "", "", err }

	// ... change addr, fee UTXO allocation ...

	batch := tx.NewMutationBatch()
	batch.AddSelfUpdate(parentKP.PublicKey, parentTxIDBytes, parentPayload, parentUTXO, parentKP.PrivateKey)
	batch.AddFeeInput(feeUTXO)
	batch.SetChange(changeAddr)

	txHex, result, err := buildAndSignBatch(batch)
	if err != nil { return "", "", err }

	success = true
	txIDHex = hex.EncodeToString(result.TxID)

	// Track UTXOs
	e.trackBatchUTXOs(result, parent.PubKeyHex, changePubHex)
	return txHex, txIDHex, nil
}
```

**Step 2: Run tests**

This indirectly tests copy, link, remove, sell, same-dir rename.

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -count=1`
Expected: ALL pass

**Step 3: Commit**

```
feat(engine): migrate buildParentSelfUpdate to batch OpUpdate
```

---

### Task 10: Migrate Encrypt to Batch

**Files:**
- Modify: `bitfs/internal/engine/encrypt.go`

**Context:** `EncryptNode` does a SelfUpdate (changes access level payload). Replace with batch OpUpdate.

**Step 1: Rewrite EncryptNode**

Same pattern as buildParentSelfUpdate but for the node itself (not its parent).

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Expected: ALL pass

**Step 3: Commit**

```
feat(engine): migrate EncryptNode to batch OpUpdate
```

---

### Task 11: Migrate Cross-Directory Move to Single Atomic Batch

**Files:**
- Modify: `bitfs/internal/engine/move.go`

**Context:** This is the biggest win. Current `crossDirectoryMove` builds 4 separate transactions with complex rollback. Replace with a single batch containing 4 ops.

**Step 1: Rewrite crossDirectoryMove**

```go
func (e *Engine) crossDirectoryMove(opts *MoveOpts, srcNodeState *NodeState) (*Result, error) {
	// ... existing validation, key derivation, encryption logic (unchanged) ...

	// --- Build single atomic batch ---
	batch := tx.NewMutationBatch()

	// Op 1: Create new child at destination
	batch.AddCreateChild(childKP.PublicKey, dstParentTxID, createPayload, dstParentUTXO, dstParentUTXO.PrivateKey)

	// Op 2: Delete source node (no refresh — UTXO dies)
	batch.AddDelete(srcKP.PublicKey, srcParentTxIDBytes, deletePayload, srcNodeUTXO, srcKP.PrivateKey)

	// Op 3: Update source parent (remove child entry)
	batch.AddSelfUpdate(srcParentKP.PublicKey, srcParentParentTxID, srcParentPayload, srcParentUTXO, srcParentKP.PrivateKey)

	// Op 4: Update destination parent (add child entry)
	// dstParentUTXO is same as Op 1's — gets deduped
	batch.AddSelfUpdate(dstParentKP.PublicKey, dstParentParentTxID, dstParentPayload, dstParentUTXO, dstParentKP.PrivateKey)

	batch.AddFeeInput(feeUTXO) // Only ONE fee UTXO needed
	batch.SetChange(changeAddr)

	txHex, result, err := buildAndSignBatch(batch)
	if err != nil { return nil, err }

	success = true
	// ... update local state (same as before, but simpler) ...
}
```

**Key improvements:**
- 4 transactions → 1 transaction
- 4 fee UTXOs → 1 fee UTXO
- Complex UTXO chaining rollback → single defer
- `combinedTxHex` with `\n` → single clean txHex

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -count=1 -run "Move|Rename"`
Expected: ALL pass

**Step 3: Commit**

```
feat(engine): migrate cross-directory move to single atomic batch

Replaces 4 separate transactions with one MutationBatch containing
OpCreate(dst) + OpDelete(src) + OpUpdate(src_parent) + OpUpdate(dst_parent).
Single fee UTXO, single signing pass, fully atomic.
```

---

### Task 12: Add trackBatchUTXOs Helper

**Files:**
- Modify: `bitfs/internal/engine/txbuild.go` or new `bitfs/internal/engine/batch_track.go`

**Context:** Each migrated operation needs to extract UTXOs from BatchResult and register them in local state. This logic is repeated — extract into a helper.

**Step 1: Implement helper**

```go
// trackBatchUTXOs registers all UTXOs produced by a BatchResult into local state.
// opPubKeys maps op index → pubkey hex for the node that op creates/updates.
// changePubHex is the change address owner.
func (e *Engine) trackBatchUTXOs(result *tx.BatchResult, opPubKeys []string, changePubHex string) {
	for i, opResult := range result.NodeOps {
		if opResult.NodeUTXO == nil {
			continue // OpDelete — no UTXO produced
		}
		e.State.AddUTXO(&UTXOState{
			TxID:         hex.EncodeToString(opResult.NodeUTXO.TxID),
			Vout:         opResult.NodeUTXO.Vout,
			Amount:       opResult.NodeUTXO.Amount,
			ScriptPubKey: hex.EncodeToString(opResult.NodeUTXO.ScriptPubKey),
			PubKeyHex:    opPubKeys[i],
			Type:         "node",
		})
	}
	if result.ChangeUTXO != nil {
		e.State.AddUTXO(&UTXOState{
			TxID:         hex.EncodeToString(result.ChangeUTXO.TxID),
			Vout:         result.ChangeUTXO.Vout,
			Amount:       result.ChangeUTXO.Amount,
			ScriptPubKey: hex.EncodeToString(result.ChangeUTXO.ScriptPubKey),
			PubKeyHex:    changePubHex,
			Type:         "fee",
		})
	}
}
```

This should be created early (during Task 5/6) and used by all subsequent migrations.

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/engine/ -v -count=1`
Expected: ALL pass

**Step 3: Commit**

```
feat(engine): add trackBatchUTXOs helper for batch result state tracking
```

---

## Phase 3: Cleanup

### Task 13: Delete Deprecated Single-Template API

**Files:**
- Modify: `libbitfs-go/tx/metanet_tx.go` — delete `CreateRootParams`, `CreateChildParams`, `SelfUpdateParams`, `BuildCreateRoot`, `BuildCreateChild`, `BuildSelfUpdate` (keep `DataTxParams` and `BuildDataTransaction`)
- Modify: `libbitfs-go/tx/sign.go` — delete `BuildUnsignedCreateRootTx`, `BuildUnsignedCreateChildTx`, `BuildUnsignedSelfUpdateTx`
- Modify: `libbitfs-go/tx/sign_test.go` — delete tests for removed functions
- Modify: `libbitfs-go/tx/tx_test.go` — delete tests for removed functions
- Modify: `bitfs/internal/engine/txbuild.go` — delete `buildUnsignedCreateRootTx`, `buildUnsignedCreateChildTx`, `buildUnsignedSelfUpdateTx`, `signCreateRootTx`, `signCreateChildTx`, `signSelfUpdateTx`

**Step 1: Delete the functions and types**

Remove all deprecated API from tx package:
- `CreateRootParams` struct
- `CreateChildParams` struct
- `SelfUpdateParams` struct
- `BuildCreateRoot()` function
- `BuildCreateChild()` function
- `BuildSelfUpdate()` function
- `BuildUnsignedCreateRootTx()` function
- `BuildUnsignedCreateChildTx()` function
- `BuildUnsignedSelfUpdateTx()` function

Remove engine wrappers:
- `buildUnsignedCreateRootTx()`
- `buildUnsignedCreateChildTx()`
- `buildUnsignedSelfUpdateTx()`
- `signCreateRootTx()`
- `signCreateChildTx()`
- `signSelfUpdateTx()`

Keep: `MetanetTx`, `UTXO`, `SignMetanetTx`, `BuildP2PKHScript`, `BuildP2PKHOutput`, `BuildDataTransaction`, `DataTxParams`, `buildOPReturnScript`, `TxHexFromBytes`, `pubKeyFromBytes`, `buildAndSignBatch`

**Step 2: Update tests — remove tests for deleted functions, ensure remaining tests pass**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./tx/ -v`
Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -v -count=1`
Expected: ALL pass, no compilation errors

**Step 3: Verify no other files reference deleted functions**

Run: `grep -r "BuildCreateRoot\|BuildCreateChild\|BuildSelfUpdate\|BuildUnsignedCreate\|BuildUnsignedSelf\|signCreate\|signSelfUpdate\|CreateRootParams\|CreateChildParams\|SelfUpdateParams" --include="*.go" libbitfs-go/ bitfs/`
Expected: No matches (only in test snapshots or comments if any)

**Step 4: Commit**

```
refactor(tx): remove deprecated single-template transaction builders

MutationBatch is now the sole transaction build path. Removed:
- BuildCreateRoot, BuildCreateChild, BuildSelfUpdate and their params
- BuildUnsignedCreateRootTx, BuildUnsignedCreateChildTx, BuildUnsignedSelfUpdateTx
- Engine wrapper functions for the above

Kept: BuildDataTransaction (independent), SignMetanetTx (used by batch),
BuildP2PKHScript, BuildP2PKHOutput.
```

---

### Task 14: Final Verification

**Files:** None (verification only)

**Step 1: Run full test suite**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./... -v -count=1 -race
cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -v -count=1 -race
cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags=integration ./integration/ -count=1 -race
```
Expected: ALL pass

**Step 2: Run linter**

```bash
cd /Users/alex/Codes/RabbitHole/libbitfs-go && golangci-lint run
cd /Users/alex/Codes/RabbitHole/bitfs && golangci-lint run
```
Expected: No new warnings

**Step 3: Verify git log**

```bash
git log --oneline -20
```
Verify clean commit history with descriptive messages.

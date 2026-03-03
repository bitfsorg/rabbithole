# Multi-Output Batch Transaction: Node Identity Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend Metanet node identity from `(P_node, TxID)` to `(P_node, TxID, Vout)` so multi-output batch transactions can uniquely identify each node.

**Architecture:** Add `Vout uint32` field to Node struct. Provide `ParseNodeFromPushesWithOutpoint` parser variant. Add optional `OutpointStore` interface for outpoint-based lookups. MutationBatch already creates 1-TX-N-outputs — only the identity layer needs updating. OP_RETURN format stays unchanged (parent disambiguation uses existing `tagParent` P_node + `parentTxID` fields). Interleaved output layout (OP_RETURN/P2PKH pairs) stays unchanged.

**Tech Stack:** Go 1.25 (libbitfs-go, bitfs, git-remote-bitfs), TypeScript ESM (libbitfs-ts), @bsv/sdk

**Repos affected:** libbitfs-go, libbitfs-ts, bitfs (tests only), git-remote-bitfs, RabbitHole (docs)

---

## Task 1: Add `Vout` to Node struct and parser (libbitfs-go)

**Files:**
- Modify: `libbitfs-go/metanet/node.go:133-137`
- Modify: `libbitfs-go/metanet/parser.go:139-150`
- Test: `libbitfs-go/metanet/parser_test.go`

**Step 1: Write failing tests**

In `libbitfs-go/metanet/parser_test.go`, add:

```go
func TestParseNodeFromPushesWithOutpoint(t *testing.T) {
	// Build a minimal valid node payload.
	node := &Node{
		Version: 1,
		Type:    NodeTypeFile,
		Op:      OpCreate,
	}
	payload, err := SerializePayload(node)
	require.NoError(t, err)

	pNode := make([]byte, CompressedPubKeyLen)
	pNode[0] = 0x02
	for i := 1; i < CompressedPubKeyLen; i++ {
		pNode[i] = byte(i)
	}
	pushes := [][]byte{
		tx.MetaFlagBytes(),
		pNode,
		make([]byte, TxIDLen), // parentTxID
		payload,
	}

	txID := make([]byte, TxIDLen)
	txID[0] = 0xAA

	// Parse with outpoint (TxID + Vout).
	parsed, err := ParseNodeFromPushesWithOutpoint(pushes, txID, 3)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), parsed.Vout)
	assert.Equal(t, txID, parsed.TxID)
}

func TestNode_Vout_ZeroDefault(t *testing.T) {
	// Existing ParseNodeFromPushesWithTxID sets Vout=0 (backward compat).
	node := &Node{Version: 1, Type: NodeTypeFile, Op: OpCreate}
	payload, err := SerializePayload(node)
	require.NoError(t, err)

	pNode := make([]byte, CompressedPubKeyLen)
	pNode[0] = 0x02
	pushes := [][]byte{tx.MetaFlagBytes(), pNode, nil, payload}

	parsed, err := ParseNodeFromPushesWithTxID(pushes, make([]byte, TxIDLen))
	require.NoError(t, err)
	assert.Equal(t, uint32(0), parsed.Vout)
}
```

**Step 2: Run tests — expect FAIL (ParseNodeFromPushesWithOutpoint undefined)**

```bash
cd libbitfs-go && go test ./metanet/ -run TestParseNodeFromPushesWithOutpoint -v
```

**Step 3: Implement**

In `libbitfs-go/metanet/node.go`, add `Vout` field to Node struct after `TxID`:

```go
type Node struct {
	TxID        []byte // Transaction ID (32 bytes)
	Vout        uint32 // Output index of this node's P2PKH in its TX (0 for legacy single-op TXs)
	PNode       []byte // P_node compressed public key (33 bytes)
	ParentTxID  []byte // Parent's TxID (empty for root)
	BlockHeight uint32 // Block height (0 = unconfirmed)
	// ... rest unchanged
```

In `libbitfs-go/metanet/parser.go`, add new function after `ParseNodeFromPushesWithTxID`:

```go
// ParseNodeFromPushesWithOutpoint is like ParseNode but also sets TxID and Vout.
// Use this when parsing nodes from multi-output batch transactions where each
// node's P2PKH lives at a specific output index.
func ParseNodeFromPushesWithOutpoint(pushes [][]byte, txID []byte, vout uint32) (*Node, error) {
	node, err := ParseNode(pushes)
	if err != nil {
		return nil, err
	}
	if len(txID) == TxIDLen {
		node.TxID = make([]byte, TxIDLen)
		copy(node.TxID, txID)
	}
	node.Vout = vout
	return node, nil
}
```

**Step 4: Run tests — expect PASS**

```bash
cd libbitfs-go && go test ./metanet/ -run "TestParseNodeFromPushesWithOutpoint|TestNode_Vout" -v
```

**Step 5: Run full metanet test suite to verify no regressions**

```bash
cd libbitfs-go && go test ./metanet/ -v -count=1
```

**Step 6: Commit**

```bash
cd libbitfs-go && git add metanet/node.go metanet/parser.go metanet/parser_test.go
git commit -m "feat(metanet): add Vout field to Node and ParseNodeFromPushesWithOutpoint"
```

---

## Task 2: Add `OutpointStore` optional interface (libbitfs-go)

**Files:**
- Modify: `libbitfs-go/metanet/node.go:212-227`
- Test: `libbitfs-go/metanet/node_test.go` (new file or existing)

**Step 1: Write failing test**

In `libbitfs-go/metanet/node_test.go` (create if needed):

```go
package metanet

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockOutpointStore implements both NodeStore and OutpointStore.
type mockOutpointStore struct {
	nodes []*Node
}

func (m *mockOutpointStore) GetNodeByPubKey(pNode []byte) (*Node, error) {
	for _, n := range m.nodes {
		if len(n.PNode) > 0 && n.PNode[0] == pNode[0] {
			return n, nil
		}
	}
	return nil, ErrNodeNotFound
}

func (m *mockOutpointStore) GetNodeByTxID(txID []byte) (*Node, error) {
	for _, n := range m.nodes {
		if len(n.TxID) > 0 && n.TxID[0] == txID[0] {
			return n, nil
		}
	}
	return nil, ErrNodeNotFound
}

func (m *mockOutpointStore) GetNodeVersions(_ []byte) ([]*Node, error) { return nil, nil }
func (m *mockOutpointStore) GetChildNodes(_ *Node) ([]*Node, error)    { return nil, nil }

func (m *mockOutpointStore) GetNodeByOutpoint(txID []byte, vout uint32) (*Node, error) {
	for _, n := range m.nodes {
		if len(n.TxID) > 0 && n.TxID[0] == txID[0] && n.Vout == vout {
			return n, nil
		}
	}
	return nil, ErrNodeNotFound
}

func TestOutpointStore_Interface(t *testing.T) {
	txID := make([]byte, 32)
	txID[0] = 0xBB

	store := &mockOutpointStore{
		nodes: []*Node{
			{TxID: txID, Vout: 1, PNode: []byte{0x02}},
			{TxID: txID, Vout: 3, PNode: []byte{0x03}},
		},
	}

	// Verify it satisfies both interfaces.
	var _ NodeStore = store
	var _ OutpointStore = store

	// GetNodeByOutpoint distinguishes by vout.
	n1, err := store.GetNodeByOutpoint(txID, 1)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), n1.Vout)
	assert.Equal(t, byte(0x02), n1.PNode[0])

	n3, err := store.GetNodeByOutpoint(txID, 3)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), n3.Vout)
	assert.Equal(t, byte(0x03), n3.PNode[0])

	// Non-existent vout returns error.
	_, err = store.GetNodeByOutpoint(txID, 99)
	assert.ErrorIs(t, err, ErrNodeNotFound)
}
```

**Step 2: Run tests — expect FAIL (OutpointStore undefined)**

```bash
cd libbitfs-go && go test ./metanet/ -run TestOutpointStore_Interface -v
```

**Step 3: Implement**

In `libbitfs-go/metanet/node.go`, add after the `NodeStore` interface:

```go
// OutpointStore is an optional extension of NodeStore that supports
// looking up nodes by their full outpoint (TxID + Vout).
// This is needed for multi-output batch transactions where multiple
// nodes share the same TxID and are distinguished by their Vout.
type OutpointStore interface {
	NodeStore
	// GetNodeByOutpoint returns a node identified by its outpoint (TxID:Vout).
	GetNodeByOutpoint(txID []byte, vout uint32) (*Node, error)
}
```

**Step 4: Run tests — expect PASS**

```bash
cd libbitfs-go && go test ./metanet/ -run TestOutpointStore -v
```

**Step 5: Full suite**

```bash
cd libbitfs-go && go test ./... -count=1
```

**Step 6: Commit**

```bash
cd libbitfs-go && git add metanet/node.go metanet/node_test.go
git commit -m "feat(metanet): add OutpointStore interface for TxID:Vout lookups"
```

---

## Task 3: Add `ParseTxToNodes` helper (libbitfs-go)

Convenience function: given TX outputs and a TxID, returns all Metanet `*Node`s with `.TxID` and `.Vout` populated.

**Files:**
- Modify: `libbitfs-go/metanet/parser.go`
- Test: `libbitfs-go/metanet/parser_test.go`

**Step 1: Write failing test**

```go
func TestParseTxToNodes_MultiOp(t *testing.T) {
	// Create two node payloads.
	file1 := &Node{Version: 1, Type: NodeTypeFile, Op: OpCreate}
	file2 := &Node{Version: 1, Type: NodeTypeDir, Op: OpCreate}
	p1, _ := SerializePayload(file1)
	p2, _ := SerializePayload(file2)

	pNode1 := make([]byte, CompressedPubKeyLen)
	pNode1[0] = 0x02
	pNode2 := make([]byte, CompressedPubKeyLen)
	pNode2[0] = 0x03

	parentTxID := make([]byte, TxIDLen)
	parentTxID[0] = 0x11

	// Build OP_RETURN scripts for two ops (interleaved layout).
	opReturn1, _ := tx.BuildOPReturnScript(pNode1, parentTxID, p1)
	opReturn2, _ := tx.BuildOPReturnScript(pNode2, parentTxID, p2)

	// Build P2PKH dust scripts.
	dustScript := makeDustP2PKH(pNode1) // helper or inline 25-byte P2PKH

	outputs := []tx.TxOutput{
		{Value: 0, ScriptPubKey: opReturn1},   // vout 0: OP_RETURN (file1)
		{Value: 1, ScriptPubKey: dustScript},   // vout 1: P2PKH (file1)
		{Value: 0, ScriptPubKey: opReturn2},   // vout 2: OP_RETURN (file2)
		{Value: 1, ScriptPubKey: dustScript},   // vout 3: P2PKH (file2)
	}

	txID := make([]byte, TxIDLen)
	txID[0] = 0xFF

	nodes, err := ParseTxToNodes(outputs, txID)
	require.NoError(t, err)
	require.Len(t, nodes, 2)

	// Node 0: file, vout=1 (P2PKH position)
	assert.Equal(t, NodeTypeFile, nodes[0].Type)
	assert.Equal(t, txID, nodes[0].TxID)
	assert.Equal(t, uint32(1), nodes[0].Vout)

	// Node 1: dir, vout=3
	assert.Equal(t, NodeTypeDir, nodes[1].Type)
	assert.Equal(t, txID, nodes[1].TxID)
	assert.Equal(t, uint32(3), nodes[1].Vout)
}

func TestParseTxToNodes_DeleteOp(t *testing.T) {
	del := &Node{Version: 1, Type: NodeTypeFile, Op: OpDelete}
	p, _ := SerializePayload(del)
	pNode := make([]byte, CompressedPubKeyLen)
	pNode[0] = 0x02

	opReturn, _ := tx.BuildOPReturnScript(pNode, nil, p)
	outputs := []tx.TxOutput{
		{Value: 0, ScriptPubKey: opReturn}, // vout 0: delete OP_RETURN, no P2PKH follows
	}
	txID := make([]byte, TxIDLen)

	nodes, err := ParseTxToNodes(outputs, txID)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, uint32(0), nodes[0].Vout) // Deletes have Vout=0 (OP_RETURN pos)
	assert.Equal(t, OpDelete, nodes[0].Op)
}
```

> **Note to implementer**: `BuildOPReturnScript` may not exist — use the existing `BuildOPReturnData` to get pushes, then manually build the script bytes. Alternatively, build a small helper in the test file. Check `tx/sign.go` or `tx/batch.go` for how OP_RETURN scripts are built.

**Step 2: Run tests — expect FAIL**

```bash
cd libbitfs-go && go test ./metanet/ -run "TestParseTxToNodes" -v
```

**Step 3: Implement**

In `libbitfs-go/metanet/parser.go`:

```go
// ParseTxToNodes parses all Metanet node operations from a transaction's
// outputs and returns fully populated Node objects with TxID and Vout set.
//
// For non-delete ops, Vout is the P2PKH output index (the node's spendable UTXO).
// For delete ops, Vout is the OP_RETURN output index (no P2PKH exists).
//
// This is the primary function for extracting nodes from batch transactions.
func ParseTxToNodes(outputs []tx.TxOutput, txID []byte) ([]*Node, error) {
	ops, err := tx.ParseTxNodeOps(outputs)
	if err != nil {
		return nil, err
	}

	nodes := make([]*Node, 0, len(ops))
	for _, op := range ops {
		pushes := [][]byte{
			tx.MetaFlagBytes(),
			op.PNode,
			op.ParentTxID,
			op.Payload,
		}

		vout := op.NodeVout
		if op.IsDelete {
			vout = op.Vout // Deletes use OP_RETURN position
		}

		node, err := ParseNodeFromPushesWithOutpoint(pushes, txID, vout)
		if err != nil {
			return nil, fmt.Errorf("parsing node at vout %d: %w", op.Vout, err)
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}
```

**Step 4: Run tests — expect PASS**

```bash
cd libbitfs-go && go test ./metanet/ -run "TestParseTxToNodes" -v
```

**Step 5: Full suite**

```bash
cd libbitfs-go && go test ./... -count=1
```

**Step 6: Commit**

```bash
cd libbitfs-go && git add metanet/parser.go metanet/parser_test.go
git commit -m "feat(metanet): add ParseTxToNodes for batch TX node extraction"
```

---

## Task 4: Mirror changes in libbitfs-ts

**Files:**
- Modify: `libbitfs-ts/src/metanet/types.ts:112-197` (Node interface)
- Modify: `libbitfs-ts/src/metanet/types.ts:215-224` (NodeStore)
- Modify: `libbitfs-ts/src/metanet/types.ts:226-239` (createNode)
- Modify: `libbitfs-ts/src/metanet/parser.ts` (add parseNodeFromPushesWithOutpoint, parseTxToNodes)
- Test: `libbitfs-ts/src/metanet/__tests__/parser.test.ts`

**Step 1: Write failing tests**

In `libbitfs-ts/src/metanet/__tests__/parser.test.ts`, add:

```typescript
describe('parseNodeFromPushesWithOutpoint', () => {
  it('should set txID and vout', () => {
    const node = createNode();
    node.version = 1;
    node.type = NodeType.File;
    node.op = OpType.Create;
    const payload = serializePayload(node);
    const pNode = new Uint8Array(33);
    pNode[0] = 0x02;
    const pushes = [META_FLAG, pNode, new Uint8Array(0), payload];
    const txID = new Uint8Array(32);
    txID[0] = 0xAA;

    const parsed = parseNodeFromPushesWithOutpoint(pushes, txID, 5);
    expect(parsed.vout).toBe(5);
    expect(parsed.txID).toEqual(txID);
  });
});

describe('Node.vout default', () => {
  it('createNode sets vout to 0', () => {
    const n = createNode();
    expect(n.vout).toBe(0);
  });
});
```

**Step 2: Run tests — expect FAIL**

```bash
cd libbitfs-ts && npm test -- --testPathPattern="metanet.*parser"
```

**Step 3: Implement**

In `libbitfs-ts/src/metanet/types.ts`, add `vout` to Node interface after `txID`:

```typescript
export interface Node {
  /** Transaction ID (32 bytes). */
  txID: Uint8Array
  /** Output index of this node's P2PKH in its TX. */
  vout: number
  /** P_node compressed public key (33 bytes). */
  pNode: Uint8Array
  // ... rest unchanged
```

Add `OutpointStore` after `NodeStore`:

```typescript
/**
 * OutpointStore extends NodeStore with outpoint-based lookups.
 * Needed for multi-output batch TXs where nodes share a TxID.
 */
export interface OutpointStore extends NodeStore {
  getNodeByOutpoint(txID: Uint8Array, vout: number): Promise<Node | null>
}
```

In `createNode()`, add `vout: 0`.

In `libbitfs-ts/src/metanet/parser.ts`, add:

```typescript
export function parseNodeFromPushesWithOutpoint(
  pushes: Uint8Array[],
  txID: Uint8Array,
  vout: number,
): Node {
  const node = parseNodeFromPushesWithTxID(pushes, txID);
  node.vout = vout;
  return node;
}
```

**Step 4: Run tests — expect PASS**

```bash
cd libbitfs-ts && npm test -- --testPathPattern="metanet.*parser"
```

**Step 5: Run full TS test suite**

```bash
cd libbitfs-ts && npm test
```

**Step 6: Commit**

```bash
cd libbitfs-ts && git add src/metanet/types.ts src/metanet/parser.ts src/metanet/__tests__/parser.test.ts
git commit -m "feat(metanet): add vout to Node interface and parseNodeFromPushesWithOutpoint"
```

---

## Task 5: Update git-remote-bitfs ChainReader implementations

The two `GetNodeByTxID` implementations in git-remote-bitfs already parse TX outputs via `ParseTxNodeOps`. Update them to set `Node.Vout` from the parsed `NodeVout`.

**Files:**
- Modify: `git-remote-bitfs/cmd/git-remote-bitfs/chainreader.go:25`
- Modify: `git-remote-bitfs/e2e/testutil/chainreader.go:30`
- Test: `git-remote-bitfs/internal/chain/reader_test.go`

**Step 1: Write failing test**

In `git-remote-bitfs/internal/chain/reader_test.go`, add a test that verifies `Node.Vout` is set when reading from chain:

```go
func TestChainReader_GetNodeByTxID_SetsVout(t *testing.T) {
	// The mock should return a node with Vout set.
	// Verify via the existing mockChainReader.
	txID := make([]byte, 32)
	txID[0] = 0xCC
	node := &metanet.Node{
		TxID:  txID,
		Vout:  3,
		PNode: make([]byte, 33),
		Type:  metanet.NodeTypeFile,
	}
	mock := &mockChainReader{nodes: map[string]*metanet.Node{
		hex.EncodeToString(txID): node,
	}}
	reader := NewReader(mock)

	info, err := reader.ReadNode(context.Background(), txID)
	require.NoError(t, err)
	assert.Equal(t, uint32(3), info.Vout)
}
```

> **Note**: Adapt test to existing mock patterns. The key point is verifying Vout propagation.

**Step 2: Update ChainReader implementations**

In both `chainreader.go` files, where `ParseTxNodeOps` is called and the first op's data is used to create a node, also set `node.Vout = ops[0].NodeVout`.

Look for the pattern where `metanet.ParseNodeFromPushesWithTxID(pushes, txID)` is called and change to `metanet.ParseNodeFromPushesWithOutpoint(pushes, txID, ops[0].NodeVout)` (or set Vout after the fact).

**Step 3: Update `GetNodeFromTx` (NodeByPNodeSelector)**

The selector matches by PNode — when it finds the matching op, use `ParseNodeFromPushesWithOutpoint` with the correct vout.

**Step 4: Run tests**

```bash
cd git-remote-bitfs && go test ./... -count=1
```

**Step 5: Commit**

```bash
cd git-remote-bitfs && git add cmd/git-remote-bitfs/chainreader.go e2e/testutil/chainreader.go internal/chain/reader_test.go
git commit -m "feat: set Node.Vout in ChainReader implementations"
```

---

## Task 6: Update git-remote-bitfs PathEntry to include Vout

**Files:**
- Modify: `git-remote-bitfs/internal/mapper/index.go`
- Modify: `git-remote-bitfs/internal/mapper/` (serialization/usage)
- Test: `git-remote-bitfs/internal/mapper/index_test.go`

**Step 1: Write failing test**

```go
func TestPathEntry_Vout(t *testing.T) {
	entry := PathEntry{
		ChildIndex: 0,
		PNode:      "02aabb",
		TxID:       "aabb",
		Vout:       3,
	}
	assert.Equal(t, uint32(3), entry.Vout)
}
```

**Step 2: Implement**

Add `Vout uint32` to PathEntry:

```go
type PathEntry struct {
	ChildIndex int    // index within parent directory
	PNode      string // hex-encoded P_node public key
	TxID       string // hex-encoded transaction ID
	Vout       uint32 // output index (for multi-output batch TXs)
}
```

Update all places that create PathEntry to include Vout from the node.

**Step 3: Run tests**

```bash
cd git-remote-bitfs && go test ./... -count=1
```

**Step 4: Commit**

```bash
cd git-remote-bitfs && git add internal/mapper/
git commit -m "feat(mapper): add Vout to PathEntry for multi-output batch TX support"
```

---

## Task 7: Update bitfs integration test mock

**Files:**
- Modify: `bitfs/integration/filesystem_test.go:27-84`

**Step 1: Update mockNodeStore**

Add `GetNodeByOutpoint` to the mock so it satisfies `OutpointStore`:

```go
func (m *mockNodeStore) GetNodeByOutpoint(txID []byte, vout uint32) (*metanet.Node, error) {
	for _, n := range m.nodes {
		if bytes.Equal(n.TxID, txID) && n.Vout == vout {
			return n, nil
		}
	}
	return nil, fmt.Errorf("node not found for outpoint %x:%d", txID, vout)
}
```

Also verify the mock satisfies both interfaces:

```go
var _ metanet.NodeStore = (*mockNodeStore)(nil)
var _ metanet.OutpointStore = (*mockNodeStore)(nil)
```

**Step 2: Run integration tests**

```bash
cd bitfs && go test -tags=integration ./integration/ -count=1
```

**Step 3: Commit**

```bash
cd bitfs && git add integration/filesystem_test.go
git commit -m "test: update mockNodeStore to implement OutpointStore"
```

---

## Task 8: Update documentation

**Files:**
- Modify: `docs/tasks/deferred.md` — mark P2 as done
- Modify: `docs/specs/bitfs/02-tx.md` — add note about Node.Vout
- Modify: `docs/design/bitfs/2-SystemDesign.zh.md` — update node identity section

**Step 1: Update deferred.md**

Mark P2 section as complete with `[x]` or move to done.

**Step 2: Update spec 02-tx.md**

Add a section noting that node identity is `(P_node, TxID, Vout)` and describe `ParseTxToNodes`.

**Step 3: Update design doc**

Change "NodeID = H(P_node || TxID_node)" to include Vout in the identity.

**Step 4: Commit in RabbitHole repo**

```bash
git add docs/
git commit -m "docs: update node identity to (P_node, TxID:Vout) for multi-output batch TX"
```

---

## Design Decisions

1. **Interleaved output layout preserved**: Current `[OP_RETURN, P2PKH]` pairs per op. No layout change needed. The deferred.md illustration used grouped layout for conceptual clarity — not a prescription.

2. **OP_RETURN format unchanged**: Parent node in a batch TX is disambiguated by `parentTxID` (OP_RETURN pushdata[2]) + `tagParent` (TLV field 0x0C, parent's P_node). No new push data field needed.

3. **ChildEntry unchanged**: Directories identify children by `PubKey` and resolve via `GetNodeByPubKey`. No TxID:Vout in ChildEntry for now. Same-TX vout references in SelfUpdate payloads are a future optimization.

4. **OutpointStore is optional**: Extends `NodeStore`, not replaces it. Existing consumers that only need `GetNodeByPubKey` (path resolution) or `GetNodeByTxID` (single-op TXs) continue working unchanged.

5. **Backward compatible**: Legacy single-op TXs have `Vout=1` (P2PKH at output 1). New code sets this explicitly; old nodes parsed without vout default to `Vout=0`.

## What's NOT in This Plan (Deferred)

- ChildEntry format change (adding TxID:Vout for version pinning)
- SelfUpdate same-TX compact vout references
- sCrypt MetanetBatch contract
- Output layout change (grouped vs interleaved)

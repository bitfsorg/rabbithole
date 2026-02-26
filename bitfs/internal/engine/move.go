package engine

import (
	"encoding/hex"
	"fmt"
	"path"
	"time"

	"github.com/tongxiaofeng/libbitfs-go/metanet"
	"github.com/tongxiaofeng/libbitfs-go/method42"
)

// MoveOpts holds options for the Move (rename) operation.
type MoveOpts struct {
	VaultIndex uint32
	SrcPath    string
	DstPath    string
	Force      bool // skip interactive warnings (for non-interactive/agent use)
}

// Move renames or moves a node. Same-directory renames update a single parent;
// cross-directory moves update both the source and destination parents.
func (e *Engine) Move(opts *MoveOpts) (*Result, error) {
	srcDir := path.Dir(opts.SrcPath)
	dstDir := path.Dir(opts.DstPath)

	// Find the source node.
	nodeState := e.State.FindNodeByPath(opts.SrcPath)
	if nodeState == nil {
		return nil, fmt.Errorf("engine: source %q not found", opts.SrcPath)
	}

	if srcDir != dstDir {
		return e.crossDirectoryMove(opts, nodeState)
	}

	// Find the parent directory.
	parent, err := e.resolveParentDir(srcDir, opts.VaultIndex)
	if err != nil {
		return nil, fmt.Errorf("engine: parent directory %q not found", srcDir)
	}

	// Check destination name doesn't exist.
	dstName := path.Base(opts.DstPath)
	srcName := path.Base(opts.SrcPath)
	for _, c := range parent.Children {
		if c.Name == dstName {
			return nil, fmt.Errorf("engine: %q already exists in %q", dstName, srcDir)
		}
	}

	// Temporarily rename in parent's children list for the build.
	renamedIdx := -1
	for i, c := range parent.Children {
		if c.Name == srcName {
			renamedIdx = i
			break
		}
	}
	if renamedIdx == -1 {
		return nil, fmt.Errorf("engine: %q not found in parent children", srcName)
	}

	parent.Children[renamedIdx].Name = dstName
	txHex, txIDHex, err := e.buildParentSelfUpdate(parent)
	if err != nil {
		parent.Children[renamedIdx].Name = srcName // restore on failure
		return nil, fmt.Errorf("engine: update parent: %w", err)
	}

	// TX build succeeded — apply remaining state changes.
	parent.TxID = txIDHex
	nodeState.Path = opts.DstPath

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Moved %s -> %s", opts.SrcPath, opts.DstPath),
		NodePub: parent.PubKeyHex,
	}, nil
}

// crossDirectoryMove moves a node between two different directories using the
// DELETE + CreateChild pattern. It produces four transactions:
//
//	Tx1: CreateChild at destination (new node with new key, re-encrypted content)
//	Tx2: SelfUpdate destination parent (add child entry)
//	Tx3: SelfUpdate source node (op=DELETE, LinkTarget=new P_node as moved_to pointer)
//	Tx4: SelfUpdate source parent (remove child entry)
//
// The implementation uses a build-then-apply pattern: all four transactions are
// built first (Phase 1) without permanently mutating state. Only after all builds
// succeed are the state changes applied (Phase 2). This ensures that if any
// build fails, local state remains consistent.
func (e *Engine) crossDirectoryMove(opts *MoveOpts, srcNodeState *NodeState) (*Result, error) {
	srcDir := path.Dir(opts.SrcPath)
	dstDir := path.Dir(opts.DstPath)
	srcName := path.Base(opts.SrcPath)
	dstName := path.Base(opts.DstPath)

	// 1. Find source parent directory.
	srcParent, err := e.resolveParentDir(srcDir, opts.VaultIndex)
	if err != nil {
		return nil, fmt.Errorf("engine: source directory %q: %w", srcDir, err)
	}

	// 2. Find destination parent directory.
	dstParent, err := e.resolveParentDir(dstDir, opts.VaultIndex)
	if err != nil {
		return nil, fmt.Errorf("engine: destination directory %q: %w", dstDir, err)
	}

	// 3. Check destination doesn't already have this name.
	for _, c := range dstParent.Children {
		if c.Name == dstName {
			return nil, fmt.Errorf("engine: %q already exists in %q", dstName, dstDir)
		}
	}

	// 4. Find the child entry in source parent.
	var srcChildIdx = -1
	for i, c := range srcParent.Children {
		if c.Name == srcName {
			srcChildIdx = i
			break
		}
	}
	if srcChildIdx == -1 {
		return nil, fmt.Errorf("engine: %q not found in source directory", srcName)
	}

	// 5. Read encrypted content from store via source KeyHash.
	srcKeyHash, err := hex.DecodeString(srcNodeState.KeyHash)
	if err != nil {
		return nil, fmt.Errorf("engine: invalid source key hash: %w", err)
	}
	ciphertext, err := e.Store.Get(srcKeyHash)
	if err != nil {
		return nil, fmt.Errorf("engine: read source content: %w", err)
	}

	// 6. Decrypt with source node's Method 42 key.
	srcKP, err := e.Wallet.DeriveNodeKey(srcNodeState.VaultIndex, srcNodeState.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive source key: %w", err)
	}

	var srcAccess method42.Access
	switch srcNodeState.Access {
	case "private":
		srcAccess = method42.AccessPrivate
	default:
		srcAccess = method42.AccessFree
	}

	decResult, err := method42.Decrypt(ciphertext, srcKP.PrivateKey, srcKP.PublicKey, srcKeyHash, srcAccess)
	if err != nil {
		return nil, fmt.Errorf("engine: decrypt source: %w", err)
	}

	// 7. Derive new child key at destination (new HD index).
	childIdx := dstParent.NextChildIdx
	childIndices := append(append([]uint32{}, dstParent.ChildIndices...), childIdx)
	childKP, err := e.Wallet.DeriveNodeKey(opts.VaultIndex, childIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive child key: %w", err)
	}
	childPubHex := hex.EncodeToString(childKP.PublicKey.Compressed())

	// 8. Re-encrypt with new key.
	encResult, err := method42.Encrypt(decResult.Plaintext, childKP.PrivateKey, childKP.PublicKey, srcAccess)
	if err != nil {
		return nil, fmt.Errorf("engine: encrypt copy: %w", err)
	}

	// 9. Store new encrypted content.
	if err := e.Store.Put(encResult.KeyHash, encResult.Ciphertext); err != nil {
		return nil, fmt.Errorf("engine: store copy: %w", err)
	}

	// --- Phase 1: Build all 4 TXs without mutating state ---

	// === Tx1: CreateChild at destination ===
	var accessLevel metanet.AccessLevel
	switch srcNodeState.Access {
	case "private":
		accessLevel = metanet.AccessPrivate
	case "paid":
		accessLevel = metanet.AccessPaid
	default:
		accessLevel = metanet.AccessFree
	}

	createNode := &metanet.Node{
		Version:     1,
		Type:        metanet.NodeType(nodeTypeInt(srcNodeState.Type)),
		Op:          metanet.OpCreate,
		MimeType:    srcNodeState.MimeType,
		FileSize:    srcNodeState.FileSize,
		KeyHash:     encResult.KeyHash,
		Access:      accessLevel,
		Timestamp:   uint64(time.Now().Unix()),
		Parent:      mustDecodeHex(dstParent.PubKeyHex),
		Index:       childIdx,
		Keywords:    srcNodeState.Keywords,
		Description: srcNodeState.Description,
		Domain:      srcNodeState.Domain,
		OnChain:     srcNodeState.OnChain,
		Compression: srcNodeState.Compression,
	}

	createPayload, err := metanet.SerializePayload(createNode)
	if err != nil {
		return nil, fmt.Errorf("engine: serialize create payload: %w", err)
	}

	dstParentTxID, err := TxIDBytes(dstParent.TxID)
	if err != nil {
		return nil, fmt.Errorf("engine: dst parent txid: %w", err)
	}
	dstParentUTXO, dstParentUS, err := e.getNodeUTXOWithState(dstParent.PubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("engine: dst parent UTXO: %w", err)
	}

	changeAddr1, changePriv1, err := e.DeriveChangeAddr()
	if err != nil {
		dstParentUS.Spent = false
		return nil, err
	}
	changePubHex1 := hex.EncodeToString(changePriv1.PubKey().Compressed())

	feeUTXO1, feeUS1, err := e.AllocateFeeUTXOWithState(3000)
	if err != nil {
		dstParentUS.Spent = false
		return nil, err
	}

	// Track all allocated UTXOs for rollback on failure.
	allSuccess := false
	defer func() {
		if !allSuccess {
			dstParentUS.Spent = false
			feeUS1.Spent = false
		}
	}()

	dstParentPubBytes := mustDecodeHex(dstParent.PubKeyHex)
	createMtx, err := buildUnsignedCreateChildTx(childKP, dstParentTxID, createPayload, dstParentUTXO, feeUTXO1, dstParentPubBytes, changeAddr1)
	if err != nil {
		return nil, fmt.Errorf("engine: build create child tx: %w", err)
	}

	createTxHex, err := signCreateChildTx(createMtx, dstParentUTXO, feeUTXO1)
	if err != nil {
		return nil, fmt.Errorf("engine: sign create child tx: %w", err)
	}
	createTxID := hex.EncodeToString(createMtx.TxID)

	// Track parent refresh UTXO from Tx1 so Tx2 can find a node UTXO for dstParent.
	// Tx1 (CreateChild) consumed dstParent's UTXO as input and produces a fresh
	// parent UTXO as output — we must register it before buildParentSelfUpdate.
	// We snapshot the UTXO list length so we can roll back if Tx2-Tx4 fail,
	// preventing phantom UTXOs from a never-broadcast Tx1.
	utxoSnapshot := len(e.State.UTXOs)
	e.TrackNewUTXOs(createMtx, childPubHex, changePubHex1)
	e.TrackParentRefreshUTXO(createMtx, dstParent.PubKeyHex)
	defer func() {
		if !allSuccess {
			e.State.UTXOs = e.State.UTXOs[:utxoSnapshot]
		}
	}()

	// === Tx2: SelfUpdate destination parent (add child entry) ===
	newChild := &ChildState{
		Name:     dstName,
		Type:     srcNodeState.Type,
		PubKey:   childPubHex,
		Index:    childIdx,
		Hardened: true,
	}
	dstChildrenAfter := make([]*ChildState, len(dstParent.Children)+1)
	copy(dstChildrenAfter, dstParent.Children)
	dstChildrenAfter[len(dstParent.Children)] = newChild

	origDstChildren := dstParent.Children
	origDstNextIdx := dstParent.NextChildIdx
	dstParent.Children = dstChildrenAfter
	dstParent.NextChildIdx = childIdx + 1
	dstParentTxHex, dstParentTxIDHex, err := e.buildParentSelfUpdate(dstParent)
	dstParent.Children = origDstChildren    // restore
	dstParent.NextChildIdx = origDstNextIdx // restore
	if err != nil {
		return nil, fmt.Errorf("engine: update destination parent: %w", err)
	}

	// === Tx3: SelfUpdate source node (op=DELETE, LinkTarget=new_P_node) ===
	deleteNode := &metanet.Node{
		Version:    1,
		Type:       metanet.NodeType(nodeTypeInt(srcNodeState.Type)),
		Op:         metanet.OpDelete,
		Timestamp:  uint64(time.Now().Unix()),
		LinkTarget: childKP.PublicKey.Compressed(), // "moved_to" pointer
	}

	deletePayload, err := metanet.SerializePayload(deleteNode)
	if err != nil {
		return nil, fmt.Errorf("engine: serialize delete payload: %w", err)
	}

	var srcParentTxIDBytes []byte
	if srcNodeState.ParentTxID != "" {
		srcParentTxIDBytes, err = TxIDBytes(srcNodeState.ParentTxID)
		if err != nil {
			return nil, fmt.Errorf("engine: src parent txid: %w", err)
		}
	}

	srcNodeUTXO, srcNodeUS, err := e.getNodeUTXOWithState(srcNodeState.PubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("engine: src node UTXO: %w", err)
	}
	defer func() {
		if !allSuccess {
			srcNodeUS.Spent = false
		}
	}()

	changeAddr3, changePriv3, err := e.DeriveChangeAddr()
	if err != nil {
		return nil, err
	}
	changePubHex3 := hex.EncodeToString(changePriv3.PubKey().Compressed())

	feeUTXO3, feeUS3, err := e.AllocateFeeUTXOWithState(2000)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !allSuccess {
			feeUS3.Spent = false
		}
	}()

	deleteMtx, err := buildUnsignedSelfUpdateTx(srcKP, srcParentTxIDBytes, deletePayload, srcNodeUTXO, feeUTXO3, changeAddr3)
	if err != nil {
		return nil, fmt.Errorf("engine: build delete tx: %w", err)
	}

	deleteTxHex, err := signSelfUpdateTx(deleteMtx, srcNodeUTXO, feeUTXO3)
	if err != nil {
		return nil, fmt.Errorf("engine: sign delete tx: %w", err)
	}
	deleteTxID := hex.EncodeToString(deleteMtx.TxID)

	// === Tx4: SelfUpdate source parent (remove child entry) ===
	srcChildrenAfter := make([]*ChildState, 0, len(srcParent.Children)-1)
	srcChildrenAfter = append(srcChildrenAfter, srcParent.Children[:srcChildIdx]...)
	srcChildrenAfter = append(srcChildrenAfter, srcParent.Children[srcChildIdx+1:]...)

	origSrcChildren := srcParent.Children
	srcParent.Children = srcChildrenAfter
	srcParentTxHex, srcParentTxIDHex, err := e.buildParentSelfUpdate(srcParent)
	srcParent.Children = origSrcChildren // restore
	if err != nil {
		return nil, fmt.Errorf("engine: update source parent: %w", err)
	}

	// --- Phase 2: All 4 builds succeeded — apply state ---
	allSuccess = true

	// Register new child node in state.
	childState := &NodeState{
		PubKeyHex:    childPubHex,
		TxID:         createTxID,
		ParentTxID:   dstParent.TxID,
		Type:         srcNodeState.Type,
		Access:       srcNodeState.Access,
		Path:         opts.DstPath,
		VaultIndex:   opts.VaultIndex,
		ChildIndices: childIndices,
		KeyHash:      hex.EncodeToString(encResult.KeyHash),
		FileSize:     srcNodeState.FileSize,
		MimeType:     srcNodeState.MimeType,
		Keywords:     srcNodeState.Keywords,
		Description:  srcNodeState.Description,
		Domain:       srcNodeState.Domain,
		OnChain:      srcNodeState.OnChain,
		Compression:  srcNodeState.Compression,
	}
	if srcNodeState.PricePerKB > 0 {
		childState.PricePerKB = srcNodeState.PricePerKB
	}
	e.State.SetNode(childPubHex, childState)

	// Update destination parent.
	dstParent.Children = dstChildrenAfter
	dstParent.NextChildIdx = childIdx + 1
	dstParent.TxID = dstParentTxIDHex

	// Mark source node as deleted (remove its path so it doesn't resolve).
	srcNodeState.TxID = deleteTxID
	srcNodeState.Path = "" // no longer resolvable

	// Update source parent.
	srcParent.Children = srcChildrenAfter
	srcParent.TxID = srcParentTxIDHex

	// Clean up old encrypted content from storage (best-effort).
	// The source content has been re-encrypted under the new key, so the old
	// ciphertext at srcKeyHash is no longer needed.
	_ = e.Store.Delete(srcKeyHash)

	// Tx1 UTXOs already tracked before Tx2 build (needed for UTXO chaining).
	// Track UTXOs from Tx3 (Delete source node).
	e.TrackNewUTXOs(deleteMtx, srcNodeState.PubKeyHex, changePubHex3)

	combinedTxHex := createTxHex + "\n" + dstParentTxHex + "\n" + deleteTxHex + "\n" + srcParentTxHex

	return &Result{
		TxHex:   combinedTxHex,
		TxID:    createTxID,
		Message: fmt.Sprintf("Moved %s -> %s (4 txs: create=%s, dstParent=%s, delete=%s, srcParent=%s)", opts.SrcPath, opts.DstPath, createTxID[:8], dstParentTxIDHex[:8], deleteTxID[:8], srcParentTxIDHex[:8]),
		NodePub: childPubHex,
	}, nil
}

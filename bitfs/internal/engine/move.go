package engine

import (
	"encoding/hex"
	"fmt"
	"path"
	"time"

	"github.com/tongxiaofeng/libbitfs/metanet"
)

// MoveOpts holds options for the Move (rename) operation.
type MoveOpts struct {
	VaultIndex uint32
	SrcPath    string
	DstPath    string
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

	// Rename in parent's children list.
	for i, c := range parent.Children {
		if c.Name == srcName {
			parent.Children[i].Name = dstName
			break
		}
	}

	// Build and sign SelfUpdate tx for parent to commit the rename.
	txHex, txIDHex, err := e.buildParentSelfUpdate(parent)
	if err != nil {
		return nil, fmt.Errorf("engine: update parent: %w", err)
	}

	// Update local state.
	parent.TxID = txIDHex
	nodeState.Path = opts.DstPath

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Moved %s -> %s", opts.SrcPath, opts.DstPath),
		NodePub: parent.PubKeyHex,
	}, nil
}

// crossDirectoryMove moves a node between two different directories. It produces
// two SelfUpdate transactions: one for the source parent (child removed) and one
// for the destination parent (child added). The node itself is not modified —
// only the parent directories' children lists change.
//
// Note: This is application-level atomicity only. If the first transaction
// succeeds but the second fails, the filesystem state may be inconsistent.
func (e *Engine) crossDirectoryMove(opts *MoveOpts, nodeState *NodeState) (*Result, error) {
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

	// 4. Find and remove child entry from source parent.
	var movedChild *ChildState
	for i, c := range srcParent.Children {
		if c.Name == srcName {
			movedChild = c
			srcParent.Children = append(srcParent.Children[:i], srcParent.Children[i+1:]...)
			break
		}
	}
	if movedChild == nil {
		return nil, fmt.Errorf("engine: %q not found in source directory", srcName)
	}

	// 5. Build SelfUpdate tx for source parent (child removed).
	srcTxHex, srcTxID, err := e.buildParentSelfUpdate(srcParent)
	if err != nil {
		return nil, fmt.Errorf("engine: update source parent: %w", err)
	}
	srcParent.TxID = srcTxID

	// 6. Add child entry to destination parent (with possibly new name).
	dstParent.Children = append(dstParent.Children, &ChildState{
		Name:     dstName,
		Type:     movedChild.Type,
		PubKey:   movedChild.PubKey,
		Index:    movedChild.Index,
		Hardened: movedChild.Hardened,
	})

	// 7. Build SelfUpdate tx for destination parent (child added).
	dstTxHex, dstTxID, err := e.buildParentSelfUpdate(dstParent)
	if err != nil {
		return nil, fmt.Errorf("engine: update destination parent: %w", err)
	}
	dstParent.TxID = dstTxID

	// 8. Update node path in local state.
	nodeState.Path = opts.DstPath

	// Return the destination parent tx as the primary result. Both transaction
	// hex values are concatenated with a newline so callers can broadcast both.
	return &Result{
		TxHex:   srcTxHex + "\n" + dstTxHex,
		TxID:    dstTxID,
		Message: fmt.Sprintf("Moved %s -> %s (2 txs: src=%s, dst=%s)", opts.SrcPath, opts.DstPath, srcTxID[:8], dstTxID[:8]),
		NodePub: nodeState.PubKeyHex,
	}, nil
}

// resolveParentDir finds the parent directory node for a given directory path.
// Handles the root directory case (path "/" or ".").
func (e *Engine) resolveParentDir(dirPath string, vaultIdx uint32) (*NodeState, error) {
	parent := e.State.FindNodeByPath(dirPath)
	if parent != nil {
		return parent, nil
	}

	if dirPath == "/" || dirPath == "." {
		rootPubHex, err := e.getRootPubHex(vaultIdx)
		if err != nil {
			return nil, err
		}
		parent = e.State.GetNode(rootPubHex)
		if parent != nil {
			return parent, nil
		}
	}

	return nil, fmt.Errorf("directory %q not found", dirPath)
}

// buildParentSelfUpdate builds and signs a SelfUpdate transaction for a parent
// directory node, reflecting its current children list. It allocates a fee UTXO,
// derives a change address, and tracks the resulting UTXOs.
// Returns the signed tx hex and tx ID hex.
func (e *Engine) buildParentSelfUpdate(parent *NodeState) (txHex string, txIDHex string, err error) {
	// Derive parent key.
	parentKP, err := e.Wallet.DeriveNodeKey(parent.VaultIndex, parent.ChildIndices, nil)
	if err != nil {
		return "", "", fmt.Errorf("derive parent key: %w", err)
	}

	// Build children list for payload.
	var children []metanet.ChildEntry
	for _, c := range parent.Children {
		children = append(children, metanet.ChildEntry{
			Index:    c.Index,
			Name:     c.Name,
			Type:     metanet.NodeType(nodeTypeInt(c.Type)),
			PubKey:   mustDecodeHex(c.PubKey),
			Hardened: c.Hardened,
		})
	}

	parentNode := &metanet.Node{
		Version:        1,
		Type:           metanet.NodeTypeDir,
		Op:             metanet.OpUpdate,
		Access:         metanet.AccessFree,
		Timestamp:      uint64(time.Now().Unix()),
		Children:       children,
		NextChildIndex: parent.NextChildIdx,
	}

	payload, err := metanet.SerializePayload(parentNode)
	if err != nil {
		return "", "", fmt.Errorf("serialize payload: %w", err)
	}

	var parentTxIDBytes []byte
	if parent.ParentTxID != "" {
		parentTxIDBytes, err = TxIDBytes(parent.ParentTxID)
		if err != nil {
			return "", "", err
		}
	}

	parentUTXO, err := e.getNodeUTXO(parent.PubKeyHex)
	if err != nil {
		return "", "", fmt.Errorf("parent UTXO: %w", err)
	}

	changeAddr, changePriv, err := e.DeriveChangeAddr()
	if err != nil {
		return "", "", err
	}
	changePubHex := hex.EncodeToString(changePriv.PubKey().Compressed())

	feeUTXO, err := e.AllocateFeeUTXO(2000)
	if err != nil {
		return "", "", err
	}

	mtx, err := buildUnsignedSelfUpdateTx(parentKP, parentTxIDBytes, payload, parentUTXO, feeUTXO, changeAddr)
	if err != nil {
		return "", "", fmt.Errorf("build self-update tx: %w", err)
	}

	signedHex, err := signSelfUpdateTx(mtx, parentUTXO, feeUTXO)
	if err != nil {
		return "", "", fmt.Errorf("sign self-update tx: %w", err)
	}

	txIDHex = hex.EncodeToString(mtx.TxID)

	// Track new UTXOs from this transaction.
	e.TrackNewUTXOs(mtx, parent.PubKeyHex, changePubHex)

	return signedHex, txIDHex, nil
}

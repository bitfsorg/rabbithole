package engine

import (
	"fmt"
	"path"
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
// The implementation uses a build-then-apply pattern: both transactions are built
// first (Phase 1) without permanently mutating state. Only after both builds
// succeed are the state changes applied (Phase 2). This ensures that if either
// build fails, local state remains consistent.
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

	// 4. Find the child entry in source parent (don't remove yet).
	var movedChild *ChildState
	var srcChildIdx int
	for i, c := range srcParent.Children {
		if c.Name == srcName {
			movedChild = c
			srcChildIdx = i
			break
		}
	}
	if movedChild == nil {
		return nil, fmt.Errorf("engine: %q not found in source directory", srcName)
	}

	// --- Phase 1: Build both TXs without mutating state ---

	// Build source parent children list (without the moved child).
	srcChildrenAfter := make([]*ChildState, 0, len(srcParent.Children)-1)
	srcChildrenAfter = append(srcChildrenAfter, srcParent.Children[:srcChildIdx]...)
	srcChildrenAfter = append(srcChildrenAfter, srcParent.Children[srcChildIdx+1:]...)

	// Temporarily swap children for build.
	origSrcChildren := srcParent.Children
	srcParent.Children = srcChildrenAfter
	srcTxHex, srcTxID, err := e.buildParentSelfUpdate(srcParent)
	srcParent.Children = origSrcChildren // restore
	if err != nil {
		return nil, fmt.Errorf("engine: update source parent: %w", err)
	}

	// Build destination parent children list (with the moved child).
	newChild := &ChildState{
		Name:     dstName,
		Type:     movedChild.Type,
		PubKey:   movedChild.PubKey,
		Index:    movedChild.Index,
		Hardened: movedChild.Hardened,
	}
	dstChildrenAfter := make([]*ChildState, len(dstParent.Children)+1)
	copy(dstChildrenAfter, dstParent.Children)
	dstChildrenAfter[len(dstParent.Children)] = newChild

	origDstChildren := dstParent.Children
	dstParent.Children = dstChildrenAfter
	dstTxHex, dstTxID, err := e.buildParentSelfUpdate(dstParent)
	dstParent.Children = origDstChildren // restore
	if err != nil {
		return nil, fmt.Errorf("engine: update destination parent: %w", err)
	}

	// --- Phase 2: Both builds succeeded — apply state ---
	srcParent.Children = srcChildrenAfter
	srcParent.TxID = srcTxID
	dstParent.Children = dstChildrenAfter
	dstParent.TxID = dstTxID
	nodeState.Path = opts.DstPath

	return &Result{
		TxHex:   srcTxHex + "\n" + dstTxHex,
		TxID:    dstTxID,
		Message: fmt.Sprintf("Moved %s -> %s (2 txs: src=%s, dst=%s)", opts.SrcPath, opts.DstPath, srcTxID[:8], dstTxID[:8]),
		NodePub: nodeState.PubKeyHex,
	}, nil
}

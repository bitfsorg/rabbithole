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

// Move renames a node (same-directory rename only for now).
// Cross-directory moves require more complex transaction logic and are deferred.
func (e *Engine) Move(opts *MoveOpts) (*Result, error) {
	srcDir := path.Dir(opts.SrcPath)
	dstDir := path.Dir(opts.DstPath)

	if srcDir != dstDir {
		return nil, fmt.Errorf("engine: cross-directory move not yet supported (src=%s, dst=%s)", srcDir, dstDir)
	}

	// Find the source node.
	nodeState := e.State.FindNodeByPath(opts.SrcPath)
	if nodeState == nil {
		return nil, fmt.Errorf("engine: source %q not found", opts.SrcPath)
	}

	// Find the parent directory.
	parent := e.State.FindNodeByPath(srcDir)
	if parent == nil {
		if srcDir == "/" || srcDir == "." {
			rootPubHex, err := e.getRootPubHex(opts.VaultIndex)
			if err != nil {
				return nil, err
			}
			parent = e.State.GetNode(rootPubHex)
		}
		if parent == nil {
			return nil, fmt.Errorf("engine: parent directory %q not found", srcDir)
		}
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

	// Build SelfUpdate for parent to commit the rename.
	parentKP, err := e.Wallet.DeriveNodeKey(parent.VaultIndex, parent.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive parent key: %w", err)
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
		return nil, fmt.Errorf("engine: serialize payload: %w", err)
	}

	var parentTxIDBytes []byte
	if parent.ParentTxID != "" {
		parentTxIDBytes, err = TxIDBytes(parent.ParentTxID)
		if err != nil {
			return nil, err
		}
	}

	parentUTXO, err := e.getNodeUTXO(parent.PubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("engine: parent UTXO: %w", err)
	}

	changeAddr, changePriv, err := e.DeriveChangeAddr()
	if err != nil {
		return nil, err
	}
	changePubHex := hex.EncodeToString(changePriv.PubKey().Compressed())

	feeUTXO, err := e.AllocateFeeUTXO(2000)
	if err != nil {
		return nil, err
	}

	mtx, err := buildUnsignedSelfUpdateTx(parentKP, parentTxIDBytes, payload, parentUTXO, feeUTXO, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("engine: build self-update tx: %w", err)
	}

	txHex, err := signSelfUpdateTx(mtx, parentUTXO, feeUTXO)
	if err != nil {
		return nil, fmt.Errorf("engine: sign self-update tx: %w", err)
	}

	txIDHex := hex.EncodeToString(mtx.TxID)

	// Update local state.
	parent.TxID = txIDHex
	nodeState.Path = opts.DstPath
	e.TrackNewUTXOs(mtx, parent.PubKeyHex, changePubHex)

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Moved %s -> %s", opts.SrcPath, opts.DstPath),
		NodePub: parent.PubKeyHex,
	}, nil
}

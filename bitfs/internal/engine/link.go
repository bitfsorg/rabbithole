package engine

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tongxiaofeng/libbitfs/metanet"
)

// LinkOpts holds options for the Link operation.
type LinkOpts struct {
	VaultIndex uint32
	TargetPath string // what the link points to
	LinkPath   string // where the link lives
	Soft       bool   // true = soft link, false = hard link
}

// Link creates a hard or soft link.
func (e *Engine) Link(opts *LinkOpts) (*Result, error) {
	// Find target node.
	targetNode := e.State.FindNodeByPath(opts.TargetPath)
	if targetNode == nil {
		return nil, fmt.Errorf("engine: target %q not found", opts.TargetPath)
	}

	if opts.Soft {
		return e.createSoftLink(opts, targetNode)
	}
	return e.createHardLink(opts, targetNode)
}

// createSoftLink creates a new link node (CreateChild tx).
func (e *Engine) createSoftLink(opts *LinkOpts, targetNode *NodeState) (*Result, error) {
	_, _, err := e.EnsureRootExists(opts.VaultIndex)
	if err != nil {
		return nil, err
	}

	parent, childName, err := e.ResolveParentNode(opts.LinkPath, opts.VaultIndex)
	if err != nil {
		return nil, err
	}

	for _, c := range parent.Children {
		if c.Name == childName {
			return nil, fmt.Errorf("engine: %q already exists in %q", childName, parent.Path)
		}
	}

	childIdx := parent.NextChildIdx
	childIndices := append(append([]uint32{}, parent.ChildIndices...), childIdx)
	childKP, err := e.Wallet.DeriveNodeKey(opts.VaultIndex, childIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive child key: %w", err)
	}
	childPubHex := hex.EncodeToString(childKP.PublicKey.Compressed())

	node := &metanet.Node{
		Version:    1,
		Type:       metanet.NodeTypeLink,
		Op:         metanet.OpCreate,
		LinkTarget: mustDecodeHex(targetNode.PubKeyHex),
		LinkType:   metanet.LinkTypeSoft,
		Access:     metanet.AccessFree,
		Timestamp:  uint64(time.Now().Unix()),
		Parent:     mustDecodeHex(parent.PubKeyHex),
		Index:      childIdx,
	}

	payload, err := metanet.SerializePayload(node)
	if err != nil {
		return nil, fmt.Errorf("engine: serialize payload: %w", err)
	}

	parentTxID, err := TxIDBytes(parent.TxID)
	if err != nil {
		return nil, err
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

	feeUTXO, err := e.AllocateFeeUTXO(3000)
	if err != nil {
		return nil, err
	}

	parentPubBytes := mustDecodeHex(parent.PubKeyHex)
	mtx, err := buildUnsignedCreateChildTx(childKP, parentTxID, payload, parentUTXO, feeUTXO, parentPubBytes, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("engine: build child tx: %w", err)
	}

	txHex, err := signCreateChildTx(mtx, parentUTXO, feeUTXO)
	if err != nil {
		return nil, fmt.Errorf("engine: sign child tx: %w", err)
	}

	txIDHex := hex.EncodeToString(mtx.TxID)

	childState := &NodeState{
		PubKeyHex:    childPubHex,
		TxID:         txIDHex,
		ParentTxID:   parent.TxID,
		Type:         "link",
		Access:       "free",
		Path:         opts.LinkPath,
		VaultIndex:   opts.VaultIndex,
		ChildIndices: childIndices,
		LinkTarget:   targetNode.PubKeyHex,
	}
	e.State.SetNode(childPubHex, childState)

	parent.Children = append(parent.Children, &ChildState{
		Name:     childName,
		Type:     "link",
		PubKey:   childPubHex,
		Index:    childIdx,
		Hardened: true,
	})
	parent.NextChildIdx = childIdx + 1
	parent.TxID = txIDHex

	e.TrackNewUTXOs(mtx, childPubHex, changePubHex)
	e.TrackParentRefreshUTXO(mtx, parent.PubKeyHex)

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Created soft link %s -> %s", opts.LinkPath, opts.TargetPath),
		NodePub: childPubHex,
	}, nil
}

// createHardLink adds a ChildEntry in the parent pointing to the same PubKey.
// This is a SelfUpdate on the parent directory.
func (e *Engine) createHardLink(opts *LinkOpts, targetNode *NodeState) (*Result, error) {
	parent, childName, err := e.ResolveParentNode(opts.LinkPath, opts.VaultIndex)
	if err != nil {
		return nil, err
	}

	for _, c := range parent.Children {
		if c.Name == childName {
			return nil, fmt.Errorf("engine: %q already exists in %q", childName, parent.Path)
		}
	}

	// Add hard link: same pubkey, new name.
	parent.Children = append(parent.Children, &ChildState{
		Name:     childName,
		Type:     targetNode.Type,
		PubKey:   targetNode.PubKeyHex,
		Index:    parent.NextChildIdx, // doesn't actually derive a new key
		Hardened: false,
	})
	parent.NextChildIdx++

	// Build SelfUpdate for parent.
	parentKP, err := e.Wallet.DeriveNodeKey(parent.VaultIndex, parent.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive parent key: %w", err)
	}

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

	parent.TxID = txIDHex
	e.TrackNewUTXOs(mtx, parent.PubKeyHex, changePubHex)

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Created hard link %s -> %s", opts.LinkPath, opts.TargetPath),
		NodePub: parent.PubKeyHex,
	}, nil
}

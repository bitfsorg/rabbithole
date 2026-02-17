package engine

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tongxiaofeng/libbitfs/metanet"
	"github.com/tongxiaofeng/libbitfs/tx"
)

// MkdirOpts holds options for the Mkdir operation.
type MkdirOpts struct {
	VaultIndex uint32
	Path       string // remote path, e.g. "/docs"
}

// Mkdir creates a directory node at the given path.
func (e *Engine) Mkdir(opts *MkdirOpts) (*Result, error) {
	// Ensure root exists.
	rootNode, rootResult, err := e.EnsureRootExists(opts.VaultIndex)
	if err != nil {
		return nil, fmt.Errorf("engine: ensure root: %w", err)
	}

	// If creating root ("/"), return the root creation result.
	if opts.Path == "/" {
		if rootResult != nil {
			rootResult.Message = fmt.Sprintf("Created root directory /")
			return rootResult, nil
		}
		return &Result{
			TxID:    rootNode.TxID,
			Message: "Root directory already exists",
			NodePub: rootNode.PubKeyHex,
		}, nil
	}

	// Resolve parent directory.
	parent, childName, err := e.ResolveParentNode(opts.Path, opts.VaultIndex)
	if err != nil {
		return nil, err
	}

	// Check for duplicate child name.
	for _, c := range parent.Children {
		if c.Name == childName {
			return nil, fmt.Errorf("engine: %q already exists in %q", childName, parent.Path)
		}
	}

	// Derive child key.
	childIdx := parent.NextChildIdx
	childIndices := append(append([]uint32{}, parent.ChildIndices...), childIdx)
	childKP, err := e.Wallet.DeriveNodeKey(opts.VaultIndex, childIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive child key: %w", err)
	}
	childPubHex := hex.EncodeToString(childKP.PublicKey.Compressed())

	// Build payload.
	node := &metanet.Node{
		Version:   1,
		Type:      metanet.NodeTypeDir,
		Op:        metanet.OpCreate,
		Access:    metanet.AccessFree,
		Timestamp: uint64(time.Now().Unix()),
		Parent:    mustDecodeHex(parent.PubKeyHex),
		Index:     childIdx,
	}

	payload, err := metanet.SerializePayload(node)
	if err != nil {
		return nil, fmt.Errorf("engine: serialize payload: %w", err)
	}

	// Get parent TxID and UTXO.
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

	// Update local state.
	childPath := opts.Path
	childState := &NodeState{
		PubKeyHex:    childPubHex,
		TxID:         txIDHex,
		ParentTxID:   parent.TxID,
		Type:         "dir",
		Access:       "free",
		Path:         childPath,
		VaultIndex:   opts.VaultIndex,
		ChildIndices: childIndices,
		Children:     make([]*ChildState, 0),
	}
	e.State.SetNode(childPubHex, childState)

	// Update parent.
	parent.Children = append(parent.Children, &ChildState{
		Name:     childName,
		Type:     "dir",
		PubKey:   childPubHex,
		Index:    childIdx,
		Hardened: true,
	})
	parent.NextChildIdx = childIdx + 1
	parent.TxID = txIDHex // parent TxID updates on child creation

	// Track new UTXOs.
	e.TrackNewUTXOs(mtx, childPubHex, changePubHex)
	e.TrackParentRefreshUTXO(mtx, parent.PubKeyHex)

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Created directory %s", opts.Path),
		NodePub: childPubHex,
	}, nil
}

// getNodeUTXO retrieves a node's UTXO from local state, converting to tx.UTXO.
func (e *Engine) getNodeUTXO(pubKeyHex string) (*txUTXO, error) {
	utxoState := e.State.GetNodeUTXO(pubKeyHex)
	if utxoState == nil {
		return nil, fmt.Errorf("no UTXO for node %s", pubKeyHex[:16])
	}
	utxoState.Spent = true
	return e.utxoStateToTx(utxoState)
}

// mustDecodeHex decodes a hex string, returning nil on error.
func mustDecodeHex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

// txUTXO is an alias for the tx package's UTXO type.
type txUTXO = tx.UTXO

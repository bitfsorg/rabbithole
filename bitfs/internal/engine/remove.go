package engine

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tongxiaofeng/libbitfs/metanet"
)

// RemoveOpts holds options for the Remove operation.
type RemoveOpts struct {
	VaultIndex uint32
	Path       string // remote path to remove
}

// Remove marks a node as deleted via SelfUpdate transaction.
func (e *Engine) Remove(opts *RemoveOpts) (*Result, error) {
	// Find the node.
	nodeState := e.State.FindNodeByPath(opts.Path)
	if nodeState == nil {
		return nil, fmt.Errorf("engine: node %q not found", opts.Path)
	}

	// Derive key pair.
	kp, err := e.Wallet.DeriveNodeKey(nodeState.VaultIndex, nodeState.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive key: %w", err)
	}

	// Build delete payload.
	node := &metanet.Node{
		Version:   1,
		Type:      metanet.NodeType(nodeTypeInt(nodeState.Type)),
		Op:        metanet.OpDelete,
		Timestamp: uint64(time.Now().Unix()),
	}

	payload, err := metanet.SerializePayload(node)
	if err != nil {
		return nil, fmt.Errorf("engine: serialize payload: %w", err)
	}

	// Get parent TxID.
	var parentTxID []byte
	if nodeState.ParentTxID != "" {
		parentTxID, err = TxIDBytes(nodeState.ParentTxID)
		if err != nil {
			return nil, err
		}
	}

	// Get node UTXO.
	nodeUTXO, err := e.getNodeUTXO(nodeState.PubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("engine: node UTXO: %w", err)
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

	mtx, err := buildUnsignedSelfUpdateTx(kp, parentTxID, payload, nodeUTXO, feeUTXO, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("engine: build self-update tx: %w", err)
	}

	txHex, err := signSelfUpdateTx(mtx, nodeUTXO, feeUTXO)
	if err != nil {
		return nil, fmt.Errorf("engine: sign self-update tx: %w", err)
	}

	txIDHex := hex.EncodeToString(mtx.TxID)

	// Update local state.
	nodeState.TxID = txIDHex
	e.TrackNewUTXOs(mtx, nodeState.PubKeyHex, changePubHex)

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Removed %s", opts.Path),
		NodePub: nodeState.PubKeyHex,
	}, nil
}

// nodeTypeInt converts a string node type to int for metanet.NodeType.
func nodeTypeInt(s string) int32 {
	switch s {
	case "dir":
		return int32(metanet.NodeTypeDir)
	case "link":
		return int32(metanet.NodeTypeLink)
	default:
		return int32(metanet.NodeTypeFile)
	}
}

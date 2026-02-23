package engine

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tongxiaofeng/libbitfs/metanet"
)

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

	// Preserve extended metadata in on-chain payload.
	parentNode.Keywords = parent.Keywords
	parentNode.Description = parent.Description
	parentNode.Domain = parent.Domain
	parentNode.OnChain = parent.OnChain
	parentNode.Compression = parent.Compression

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

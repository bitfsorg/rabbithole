package engine

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/tongxiaofeng/libbitfs/metanet"
	"github.com/tongxiaofeng/libbitfs/method42"
)

// EncryptOpts holds options for the Encrypt operation.
type EncryptOpts struct {
	VaultIndex uint32
	Path       string // remote path
}

// EncryptNode re-encrypts content from FREE to PRIVATE access.
func (e *Engine) EncryptNode(opts *EncryptOpts) (*Result, error) {
	nodeState := e.State.FindNodeByPath(opts.Path)
	if nodeState == nil {
		return nil, fmt.Errorf("engine: node %q not found", opts.Path)
	}

	if nodeState.Type != "file" {
		return nil, fmt.Errorf("engine: %q is not a file", opts.Path)
	}
	if nodeState.Access != "free" {
		return nil, fmt.Errorf("engine: %q is already %s", opts.Path, nodeState.Access)
	}

	kp, err := e.Wallet.DeriveNodeKey(nodeState.VaultIndex, nodeState.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive key: %w", err)
	}

	// Read current ciphertext from store.
	keyHash := mustDecodeHex(nodeState.KeyHash)
	ciphertext, err := e.Store.Get(keyHash)
	if err != nil {
		return nil, fmt.Errorf("engine: read content: %w", err)
	}

	// Re-encrypt FREE → PRIVATE.
	reEncResult, err := method42.ReEncrypt(ciphertext, kp.PrivateKey, kp.PublicKey, keyHash, method42.AccessFree, method42.AccessPrivate)
	if err != nil {
		return nil, fmt.Errorf("engine: re-encrypt: %w", err)
	}

	// Store new ciphertext (key_hash changes because derivation differs).
	if err := e.Store.Put(reEncResult.KeyHash, reEncResult.Ciphertext); err != nil {
		return nil, fmt.Errorf("engine: store re-encrypted: %w", err)
	}

	// Delete old ciphertext.
	_ = e.Store.Delete(keyHash)

	// Build SelfUpdate payload.
	node := &metanet.Node{
		Version:   1,
		Type:      metanet.NodeTypeFile,
		Op:        metanet.OpUpdate,
		Access:    metanet.AccessPrivate,
		KeyHash:   reEncResult.KeyHash,
		Timestamp: uint64(time.Now().Unix()),
	}
	if nodeState.MimeType != "" {
		node.MimeType = nodeState.MimeType
	}
	if nodeState.FileSize > 0 {
		node.FileSize = nodeState.FileSize
	}

	payload, err := metanet.SerializePayload(node)
	if err != nil {
		return nil, fmt.Errorf("engine: serialize payload: %w", err)
	}

	var parentTxID []byte
	if nodeState.ParentTxID != "" {
		parentTxID, err = TxIDBytes(nodeState.ParentTxID)
		if err != nil {
			return nil, err
		}
	}

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
	nodeState.Access = "private"
	nodeState.KeyHash = hex.EncodeToString(reEncResult.KeyHash)
	e.TrackNewUTXOs(mtx, nodeState.PubKeyHex, changePubHex)

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Encrypted %s (FREE -> PRIVATE)", opts.Path),
		NodePub: nodeState.PubKeyHex,
	}, nil
}

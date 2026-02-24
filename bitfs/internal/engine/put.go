package engine

import (
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/tongxiaofeng/libbitfs-go/metanet"
	"github.com/tongxiaofeng/libbitfs-go/method42"
)

// PutOpts holds options for the Put (upload file) operation.
type PutOpts struct {
	VaultIndex  uint32
	LocalFile   string // local file path
	RemotePath  string // remote path, e.g. "/docs/readme.txt"
	Access      string // "free" or "private"
	Keywords    string // optional comma-separated keywords
	Description string // optional file description
	Domain      string // optional associated domain
	OnChain     bool   // store content on-chain
	Compression int32  // compression type (0=none)
}

// PutFile uploads a local file to the BitFS filesystem.
func (e *Engine) PutFile(opts *PutOpts) (*Result, error) {
	// Read local file.
	plaintext, err := os.ReadFile(opts.LocalFile)
	if err != nil {
		return nil, fmt.Errorf("engine: read file: %w", err)
	}

	// Ensure root exists.
	_, _, err = e.EnsureRootExists(opts.VaultIndex)
	if err != nil {
		return nil, fmt.Errorf("engine: ensure root: %w", err)
	}

	// Resolve parent directory.
	parent, childName, err := e.ResolveParentNode(opts.RemotePath, opts.VaultIndex)
	if err != nil {
		return nil, err
	}

	// Check for duplicate.
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

	// Determine access mode.
	var accessMode method42.Access
	var accessLevel metanet.AccessLevel
	switch opts.Access {
	case "private":
		accessMode = method42.AccessPrivate
		accessLevel = metanet.AccessPrivate
	default:
		accessMode = method42.AccessFree
		accessLevel = metanet.AccessFree
	}

	// Encrypt content.
	encResult, err := method42.Encrypt(plaintext, childKP.PrivateKey, childKP.PublicKey, accessMode)
	if err != nil {
		return nil, fmt.Errorf("engine: encrypt: %w", err)
	}

	// Store encrypted content.
	if err := e.Store.Put(encResult.KeyHash, encResult.Ciphertext); err != nil {
		return nil, fmt.Errorf("engine: store content: %w", err)
	}

	// Build payload.
	mimeType := DetectMimeType(opts.LocalFile)
	node := &metanet.Node{
		Version:     1,
		Type:        metanet.NodeTypeFile,
		Op:          metanet.OpCreate,
		MimeType:    mimeType,
		FileSize:    uint64(len(plaintext)),
		KeyHash:     encResult.KeyHash,
		Access:      accessLevel,
		Timestamp:   uint64(time.Now().Unix()),
		Parent:      mustDecodeHex(parent.PubKeyHex),
		Index:       childIdx,
		Keywords:    opts.Keywords,
		Description: opts.Description,
		Domain:      opts.Domain,
		OnChain:     opts.OnChain,
		Compression: opts.Compression,
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

	parentUTXO, parentUS, err := e.getNodeUTXOWithState(parent.PubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("engine: parent UTXO: %w", err)
	}

	changeAddr, changePriv, err := e.DeriveChangeAddr()
	if err != nil {
		parentUS.Spent = false
		return nil, err
	}
	changePubHex := hex.EncodeToString(changePriv.PubKey().Compressed())

	feeUTXO, feeUS, err := e.AllocateFeeUTXOWithState(3000)
	if err != nil {
		parentUS.Spent = false
		return nil, err
	}

	success := false
	defer func() {
		if !success {
			parentUS.Spent = false
			feeUS.Spent = false
		}
	}()

	parentPubBytes := mustDecodeHex(parent.PubKeyHex)
	mtx, err := buildUnsignedCreateChildTx(childKP, parentTxID, payload, parentUTXO, feeUTXO, parentPubBytes, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("engine: build child tx: %w", err)
	}

	txHex, err := signCreateChildTx(mtx, parentUTXO, feeUTXO)
	if err != nil {
		return nil, fmt.Errorf("engine: sign child tx: %w", err)
	}

	success = true
	txIDHex := hex.EncodeToString(mtx.TxID)

	// Update local state.
	childState := &NodeState{
		PubKeyHex:    childPubHex,
		TxID:         txIDHex,
		ParentTxID:   parent.TxID,
		Type:         "file",
		Access:       opts.Access,
		Path:         opts.RemotePath,
		VaultIndex:   opts.VaultIndex,
		ChildIndices: childIndices,
		KeyHash:      hex.EncodeToString(encResult.KeyHash),
		FileSize:     uint64(len(plaintext)),
		MimeType:     mimeType,
		Keywords:     opts.Keywords,
		Description:  opts.Description,
		Domain:       opts.Domain,
		OnChain:      opts.OnChain,
		Compression:  opts.Compression,
	}
	e.State.SetNode(childPubHex, childState)

	// Update parent.
	parent.Children = append(parent.Children, &ChildState{
		Name:     childName,
		Type:     "file",
		PubKey:   childPubHex,
		Index:    childIdx,
		Hardened: true,
	})
	parent.NextChildIdx = childIdx + 1
	parent.TxID = txIDHex

	// Track UTXOs.
	e.TrackNewUTXOs(mtx, childPubHex, changePubHex)
	e.TrackParentRefreshUTXO(mtx, parent.PubKeyHex)

	return &Result{
		TxHex:   txHex,
		TxID:    txIDHex,
		Message: fmt.Sprintf("Uploaded %s to %s (%d bytes, %s)", path.Base(opts.LocalFile), opts.RemotePath, len(plaintext), opts.Access),
		NodePub: childPubHex,
	}, nil
}

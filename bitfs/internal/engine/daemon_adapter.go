package engine

import (
	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/tongxiaofeng/libbitfs/metanet"
	"github.com/tongxiaofeng/bitfs/internal/daemon"
)

// WalletAdapter implements daemon.WalletService using the engine's wallet.
type WalletAdapter struct {
	engine *Engine
}

// NewWalletAdapter creates a WalletAdapter wrapping the engine.
func NewWalletAdapter(e *Engine) *WalletAdapter {
	return &WalletAdapter{engine: e}
}

// DeriveNodePubKey implements daemon.WalletService.
func (a *WalletAdapter) DeriveNodePubKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*ec.PublicKey, error) {
	return a.engine.Wallet.DeriveNodePubKey(vaultIndex, filePath, hardened)
}

// GetSellerKeyPair implements daemon.WalletService.
func (a *WalletAdapter) GetSellerKeyPair() (*ec.PrivateKey, *ec.PublicKey, error) {
	vaultIdx, err := a.engine.ResolveVaultIndex("")
	if err != nil {
		return nil, nil, err
	}
	kp, err := a.engine.Wallet.DeriveVaultRootKey(vaultIdx)
	if err != nil {
		return nil, nil, err
	}
	return kp.PrivateKey, kp.PublicKey, nil
}

// StoreAdapter implements daemon.ContentStore using the engine's file store.
type StoreAdapter struct {
	engine *Engine
}

// NewStoreAdapter creates a StoreAdapter wrapping the engine.
func NewStoreAdapter(e *Engine) *StoreAdapter {
	return &StoreAdapter{engine: e}
}

// Get implements daemon.ContentStore.
func (a *StoreAdapter) Get(keyHash []byte) ([]byte, error) {
	return a.engine.Store.Get(keyHash)
}

// Has implements daemon.ContentStore.
func (a *StoreAdapter) Has(keyHash []byte) (bool, error) {
	return a.engine.Store.Has(keyHash)
}

// Size implements daemon.ContentStore.
func (a *StoreAdapter) Size(keyHash []byte) (int64, error) {
	return a.engine.Store.Size(keyHash)
}

// MetanetAdapter implements daemon.MetanetService using local state.
type MetanetAdapter struct {
	engine *Engine
}

// NewMetanetAdapter creates a MetanetAdapter wrapping the engine.
func NewMetanetAdapter(e *Engine) *MetanetAdapter {
	return &MetanetAdapter{engine: e}
}

// GetNodeByPath implements daemon.MetanetService.
func (a *MetanetAdapter) GetNodeByPath(path string) (*daemon.NodeInfo, error) {
	nodeState := a.engine.State.FindNodeByPath(path)
	if nodeState == nil {
		return nil, daemon.ErrContentNotFound
	}

	info := &daemon.NodeInfo{
		PNode:      mustDecodeHex(nodeState.PubKeyHex),
		Type:       nodeState.Type,
		MimeType:   nodeState.MimeType,
		FileSize:   nodeState.FileSize,
		KeyHash:    mustDecodeHex(nodeState.KeyHash),
		Access:     nodeState.Access,
		PricePerKB: nodeState.PricePerKB,
		Domain:     "",
	}

	for _, c := range nodeState.Children {
		info.Children = append(info.Children, daemon.ChildInfo{
			Name: c.Name,
			Type: c.Type,
		})
	}

	return info, nil
}

// nodeTypeFromString converts string to metanet.NodeType (used internally).
func nodeTypeFromString(s string) metanet.NodeType {
	switch s {
	case "dir":
		return metanet.NodeTypeDir
	case "link":
		return metanet.NodeTypeLink
	default:
		return metanet.NodeTypeFile
	}
}

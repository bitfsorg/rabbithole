package engine

import (
	"context"
	"encoding/hex"
	"fmt"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/tongxiaofeng/bitfs/internal/daemon"
	"github.com/tongxiaofeng/libbitfs-go/metanet"
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

// DeriveNodeKeyPair implements daemon.WalletService.
// Looks up the node by its compressed public key, retrieves the BIP32
// derivation path from local state, and derives the full key pair.
func (a *WalletAdapter) DeriveNodeKeyPair(pnode []byte) (*ec.PrivateKey, *ec.PublicKey, error) {
	pubHex := hex.EncodeToString(pnode)
	nodeState := a.engine.State.GetNode(pubHex)
	if nodeState == nil {
		return nil, nil, fmt.Errorf("engine: node not found for pubkey %s", pubHex)
	}
	kp, err := a.engine.Wallet.DeriveNodeKey(nodeState.VaultIndex, nodeState.ChildIndices, nil)
	if err != nil {
		return nil, nil, err
	}
	return kp.PrivateKey, kp.PublicKey, nil
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

// GetVaultPubKey implements daemon.WalletService.
func (a *WalletAdapter) GetVaultPubKey(alias string) (string, error) {
	vaultIdx, err := a.engine.ResolveVaultIndex(alias)
	if err != nil {
		return "", err
	}
	kp, err := a.engine.Wallet.DeriveVaultRootKey(vaultIdx)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(kp.PublicKey.Compressed()), nil
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

// SPVAdapter implements daemon.SPVService using the engine's SPV client.
type SPVAdapter struct {
	engine *Engine
}

// NewSPVAdapter creates an SPVAdapter wrapping the engine.
// Returns nil if the engine has no SPV client configured.
func NewSPVAdapter(e *Engine) *SPVAdapter {
	if e.SPV == nil {
		return nil
	}
	return &SPVAdapter{engine: e}
}

// VerifyTx implements daemon.SPVService.
func (a *SPVAdapter) VerifyTx(ctx context.Context, txid string) (*daemon.SPVResult, error) {
	result, err := a.engine.VerifyTx(ctx, txid)
	if err != nil {
		return nil, err
	}
	return &daemon.SPVResult{
		Confirmed:   result.Confirmed,
		BlockHash:   result.BlockHash,
		BlockHeight: result.BlockHeight,
	}, nil
}

// ChainAdapter implements daemon.ChainService using the engine's blockchain service.
type ChainAdapter struct {
	engine *Engine
}

// NewChainAdapter creates a ChainAdapter wrapping the engine.
// Returns nil if the engine has no blockchain service configured (offline mode).
func NewChainAdapter(e *Engine) *ChainAdapter {
	if e.Chain == nil {
		return nil
	}
	return &ChainAdapter{engine: e}
}

// BroadcastTx implements daemon.ChainService.
func (a *ChainAdapter) BroadcastTx(ctx context.Context, rawTxHex string) (string, error) {
	return a.engine.BroadcastTx(ctx, rawTxHex)
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

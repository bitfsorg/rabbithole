//go:build integration

package integration

import (
	"context"
	"encoding/hex"
	"fmt"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// testWalletAdapter implements daemon.WalletService for integration tests.
type testWalletAdapter struct {
	v *vault.Vault
}

func (a *testWalletAdapter) DeriveNodePubKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*ec.PublicKey, error) {
	return a.v.Wallet.DeriveNodePubKey(vaultIndex, filePath, hardened)
}

func (a *testWalletAdapter) DeriveNodeKeyPair(pnode []byte) (*ec.PrivateKey, *ec.PublicKey, error) {
	pubHex := hex.EncodeToString(pnode)
	nodeState := a.v.State.GetNode(pubHex)
	if nodeState == nil {
		return nil, nil, fmt.Errorf("vault: node not found for pubkey %s", pubHex)
	}
	kp, err := a.v.Wallet.DeriveNodeKey(nodeState.VaultIndex, nodeState.ChildIndices, nil)
	if err != nil {
		return nil, nil, err
	}
	return kp.PrivateKey, kp.PublicKey, nil
}

func (a *testWalletAdapter) GetSellerKeyPair() (*ec.PrivateKey, *ec.PublicKey, error) {
	vaultIdx, err := a.v.ResolveVaultIndex("")
	if err != nil {
		return nil, nil, err
	}
	kp, err := a.v.Wallet.DeriveVaultRootKey(vaultIdx)
	if err != nil {
		return nil, nil, err
	}
	return kp.PrivateKey, kp.PublicKey, nil
}

func (a *testWalletAdapter) GetVaultPubKey(alias string) (string, error) {
	vaultIdx, err := a.v.ResolveVaultIndex(alias)
	if err != nil {
		return "", err
	}
	kp, err := a.v.Wallet.DeriveVaultRootKey(vaultIdx)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(kp.PublicKey.Compressed()), nil
}

// testStoreAdapter implements daemon.ContentStore for integration tests.
type testStoreAdapter struct {
	v *vault.Vault
}

func (a *testStoreAdapter) Get(keyHash []byte) ([]byte, error)  { return a.v.Store.Get(keyHash) }
func (a *testStoreAdapter) Has(keyHash []byte) (bool, error)    { return a.v.Store.Has(keyHash) }
func (a *testStoreAdapter) Size(keyHash []byte) (int64, error)  { return a.v.Store.Size(keyHash) }

// testMetanetAdapter implements daemon.MetanetService for integration tests.
type testMetanetAdapter struct {
	v *vault.Vault
}

func (a *testMetanetAdapter) GetNodeByPath(path string) (*daemon.NodeInfo, error) {
	ns := a.v.State.FindNodeByPath(path)
	if ns == nil {
		return nil, daemon.ErrContentNotFound
	}
	info := &daemon.NodeInfo{
		PNode:      mustHexDecode(ns.PubKeyHex),
		Type:       ns.Type,
		MimeType:   ns.MimeType,
		FileSize:   ns.FileSize,
		KeyHash:    mustHexDecode(ns.KeyHash),
		Access:     ns.Access,
		PricePerKB: ns.PricePerKB,
	}
	for _, c := range ns.Children {
		info.Children = append(info.Children, daemon.ChildInfo{Name: c.Name, Type: c.Type})
	}
	return info, nil
}

// testSPVAdapter implements daemon.SPVService for integration tests.
type testSPVAdapter struct {
	v *vault.Vault
}

func (a *testSPVAdapter) VerifyTx(ctx context.Context, txid string) (*daemon.SPVResult, error) {
	result, err := a.v.VerifyTx(ctx, txid)
	if err != nil {
		return nil, err
	}
	return &daemon.SPVResult{
		Confirmed:   result.Confirmed,
		BlockHash:   result.BlockHash,
		BlockHeight: result.BlockHeight,
	}, nil
}

// testChainAdapter implements daemon.ChainService for integration tests.
type testChainAdapter struct {
	v *vault.Vault
}

func (a *testChainAdapter) BroadcastTx(ctx context.Context, rawTxHex string) (string, error) {
	return a.v.BroadcastTx(ctx, rawTxHex)
}

func mustHexDecode(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

// Suppress unused import warning for wallet package.
var _ = wallet.ExternalChain

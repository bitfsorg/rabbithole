// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/hex"
	"fmt"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/bitfsorg/bitfs/internal/daemon"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// vaultWalletAdapter implements daemon.WalletService using a vault.
type vaultWalletAdapter struct {
	v *vault.Vault
}

func newVaultWalletAdapter(v *vault.Vault) *vaultWalletAdapter {
	return &vaultWalletAdapter{v: v}
}

func (a *vaultWalletAdapter) DeriveNodePubKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*ec.PublicKey, error) {
	return a.v.Wallet.DeriveNodePubKey(vaultIndex, filePath, hardened)
}

func (a *vaultWalletAdapter) DeriveNodeKeyPair(pnode []byte) (*ec.PrivateKey, *ec.PublicKey, error) {
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

func (a *vaultWalletAdapter) GetSellerKeyPair() (*ec.PrivateKey, *ec.PublicKey, error) {
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

func (a *vaultWalletAdapter) GetVaultPubKey(alias string) (string, error) {
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

// vaultStoreAdapter implements daemon.ContentStore using a vault.
type vaultStoreAdapter struct {
	v *vault.Vault
}

func newVaultStoreAdapter(v *vault.Vault) *vaultStoreAdapter {
	return &vaultStoreAdapter{v: v}
}

func (a *vaultStoreAdapter) Get(keyHash []byte) ([]byte, error) {
	return a.v.Store.Get(keyHash)
}

func (a *vaultStoreAdapter) Has(keyHash []byte) (bool, error) {
	return a.v.Store.Has(keyHash)
}

func (a *vaultStoreAdapter) Size(keyHash []byte) (int64, error) {
	return a.v.Store.Size(keyHash)
}

// vaultMetanetAdapter implements daemon.MetanetService using a vault.
type vaultMetanetAdapter struct {
	v *vault.Vault
}

func newVaultMetanetAdapter(v *vault.Vault) *vaultMetanetAdapter {
	return &vaultMetanetAdapter{v: v}
}

func (a *vaultMetanetAdapter) GetNodeByPath(path string) (*daemon.NodeInfo, error) {
	nodeState := a.v.State.FindNodeByPath(path)
	if nodeState == nil {
		return nil, daemon.ErrContentNotFound
	}

	info := &daemon.NodeInfo{
		PNode:      hexToBytes(nodeState.PubKeyHex),
		Type:       nodeState.Type,
		MimeType:   nodeState.MimeType,
		FileSize:   nodeState.FileSize,
		KeyHash:    hexToBytes(nodeState.KeyHash),
		Access:     nodeState.Access,
		PricePerKB: nodeState.PricePerKB,
	}

	for _, c := range nodeState.Children {
		info.Children = append(info.Children, daemon.ChildInfo{
			Name: c.Name,
			Type: c.Type,
		})
	}

	return info, nil
}

// vaultSPVAdapter implements daemon.SPVService using a vault.
type vaultSPVAdapter struct {
	v *vault.Vault
}

func newVaultSPVAdapter(v *vault.Vault) daemon.SPVService {
	if v.SPV == nil {
		return nil
	}
	return &vaultSPVAdapter{v: v}
}

func (a *vaultSPVAdapter) VerifyTx(ctx context.Context, txid string) (*daemon.SPVResult, error) {
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

// vaultChainAdapter implements daemon.ChainService using a vault.
type vaultChainAdapter struct {
	v *vault.Vault
}

func newVaultChainAdapter(v *vault.Vault) daemon.ChainService {
	if v.Chain == nil {
		return nil
	}
	return &vaultChainAdapter{v: v}
}

func (a *vaultChainAdapter) BroadcastTx(ctx context.Context, rawTxHex string) (string, error) {
	return a.v.BroadcastTx(ctx, rawTxHex)
}

// hexToBytes decodes a hex string, returning nil on error.
func hexToBytes(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

package engine

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- WalletAdapter tests ---

func TestWalletAdapter_DeriveNodePubKey(t *testing.T) {
	eng := initTestEngine(t)
	adapter := NewWalletAdapter(eng)

	pub, err := adapter.DeriveNodePubKey(0, []uint32{0}, nil)
	require.NoError(t, err)
	assert.NotNil(t, pub, "derived public key should not be nil")
	assert.Len(t, pub.Compressed(), 33, "compressed pubkey should be 33 bytes")
}

func TestWalletAdapter_DeriveNodeKeyPair(t *testing.T) {
	eng := initTestEngine(t)

	// Set up a root directory and a file so we have a node in state.
	addFeeUTXO(t, eng, 100000)
	_, err := eng.Mkdir(&MkdirOpts{VaultIndex: 0, Path: "/"})
	require.NoError(t, err)

	// Find the root node to get a valid pnode.
	rootPubHex, err := eng.getRootPubHex(0)
	require.NoError(t, err)
	rootNode := eng.State.GetNode(rootPubHex)
	require.NotNil(t, rootNode, "root node should exist in state")

	pnodeBytes, err := hex.DecodeString(rootNode.PubKeyHex)
	require.NoError(t, err)

	adapter := NewWalletAdapter(eng)
	priv, pub, err := adapter.DeriveNodeKeyPair(pnodeBytes)
	require.NoError(t, err)
	assert.NotNil(t, priv, "private key should not be nil")
	assert.NotNil(t, pub, "public key should not be nil")

	// Verify the private key corresponds to the public key.
	derivedPub := priv.PubKey()
	assert.Equal(t, pub.Compressed(), derivedPub.Compressed(),
		"priv.PubKey() should match the returned public key")
}

func TestWalletAdapter_DeriveNodeKeyPair_NotFound(t *testing.T) {
	eng := initTestEngine(t)
	adapter := NewWalletAdapter(eng)

	// Use random bytes that won't match any node in state.
	randomPnode := make([]byte, 33)
	randomPnode[0] = 0x02 // valid prefix for compressed pubkey
	for i := 1; i < 33; i++ {
		randomPnode[i] = byte(i)
	}

	_, _, err := adapter.DeriveNodeKeyPair(randomPnode)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWalletAdapter_GetVaultPubKey(t *testing.T) {
	eng := initTestEngine(t)
	adapter := NewWalletAdapter(eng)

	pubHex, err := adapter.GetVaultPubKey("default")
	require.NoError(t, err)
	assert.NotEmpty(t, pubHex, "vault pubkey hex should not be empty")

	// Verify it's valid hex that decodes to a compressed public key (33 bytes).
	pubBytes, err := hex.DecodeString(pubHex)
	require.NoError(t, err)
	assert.Len(t, pubBytes, 33, "vault pubkey should be 33 bytes (compressed)")
}

func TestWalletAdapter_GetVaultPubKey_NotFound(t *testing.T) {
	eng := initTestEngine(t)
	adapter := NewWalletAdapter(eng)

	_, err := adapter.GetVaultPubKey("nonexistent")
	require.Error(t, err, "GetVaultPubKey with nonexistent alias should fail")
}

// --- SPVAdapter tests ---

func TestSPVAdapter_NilEngine(t *testing.T) {
	eng := initTestEngine(t)
	// Default engine has no SPV configured.
	assert.Nil(t, eng.SPV, "test engine should have nil SPV by default")

	adapter := NewSPVAdapter(eng)
	assert.Nil(t, adapter, "NewSPVAdapter should return nil when engine has no SPV")
}

// --- ChainAdapter tests ---

func TestChainAdapter_NilEngine(t *testing.T) {
	eng := initTestEngine(t)
	// Default engine has no Chain configured.
	assert.Nil(t, eng.Chain, "test engine should have nil Chain by default")

	adapter := NewChainAdapter(eng)
	assert.Nil(t, adapter, "NewChainAdapter should return nil when engine has no Chain")
}

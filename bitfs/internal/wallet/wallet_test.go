package wallet

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mnemonic tests ---

func TestGenerateMnemonic_12Words(t *testing.T) {
	mnemonic, err := GenerateMnemonic(Mnemonic12Words)
	require.NoError(t, err)

	words := strings.Fields(mnemonic)
	assert.Len(t, words, 12, "12-word mnemonic should have 12 words")
	assert.True(t, ValidateMnemonic(mnemonic), "generated mnemonic should be valid")
}

func TestGenerateMnemonic_24Words(t *testing.T) {
	mnemonic, err := GenerateMnemonic(Mnemonic24Words)
	require.NoError(t, err)

	words := strings.Fields(mnemonic)
	assert.Len(t, words, 24, "24-word mnemonic should have 24 words")
	assert.True(t, ValidateMnemonic(mnemonic), "generated mnemonic should be valid")
}

func TestGenerateMnemonic_InvalidEntropy(t *testing.T) {
	_, err := GenerateMnemonic(64) // invalid
	assert.ErrorIs(t, err, ErrInvalidEntropy)

	_, err = GenerateMnemonic(192) // invalid
	assert.ErrorIs(t, err, ErrInvalidEntropy)
}

func TestGenerateMnemonic_Unique(t *testing.T) {
	m1, err := GenerateMnemonic(Mnemonic12Words)
	require.NoError(t, err)

	m2, err := GenerateMnemonic(Mnemonic12Words)
	require.NoError(t, err)

	assert.NotEqual(t, m1, m2, "two generated mnemonics should be different")
}

func TestValidateMnemonic(t *testing.T) {
	tests := []struct {
		name     string
		mnemonic string
		valid    bool
	}{
		{"valid 12-word", "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about", true},
		{"invalid words", "foo bar baz qux quux corge grault garply waldo fred plugh xyzzy", false},
		{"empty", "", false},
		{"partial", "abandon abandon", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, ValidateMnemonic(tt.mnemonic))
		})
	}
}

// --- Seed derivation tests ---

func TestSeedFromMnemonic_Deterministic(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	seed1, err := SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	seed2, err := SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	assert.Equal(t, seed1, seed2, "same mnemonic+passphrase should produce same seed")
	assert.Len(t, seed1, 64, "BIP39 seed should be 64 bytes")
}

func TestSeedFromMnemonic_DifferentPassphrase(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	seed1, err := SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	seed2, err := SeedFromMnemonic(mnemonic, "my secret passphrase")
	require.NoError(t, err)

	assert.NotEqual(t, seed1, seed2, "different passphrases should produce different seeds")
}

func TestSeedFromMnemonic_InvalidMnemonic(t *testing.T) {
	_, err := SeedFromMnemonic("invalid mnemonic words here", "")
	assert.ErrorIs(t, err, ErrInvalidMnemonic)
}

// --- Seed encryption tests ---

func TestEncryptDecryptSeed_RoundTrip(t *testing.T) {
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i)
	}

	password := "test-password-123"

	encrypted, err := EncryptSeed(seed, password)
	require.NoError(t, err)
	assert.Greater(t, len(encrypted), len(seed), "encrypted should be larger than seed")

	decrypted, err := DecryptSeed(encrypted, password)
	require.NoError(t, err)
	assert.Equal(t, seed, decrypted, "decrypted seed should match original")
}

func TestDecryptSeed_WrongPassword(t *testing.T) {
	seed := make([]byte, 64)
	password := "correct-password"

	encrypted, err := EncryptSeed(seed, password)
	require.NoError(t, err)

	_, err = DecryptSeed(encrypted, "wrong-password")
	assert.ErrorIs(t, err, ErrDecryptionFailed, "wrong password should fail")
}

func TestEncryptSeed_EmptySeed(t *testing.T) {
	_, err := EncryptSeed([]byte{}, "password")
	assert.ErrorIs(t, err, ErrInvalidSeed)
}

func TestDecryptSeed_TooShort(t *testing.T) {
	_, err := DecryptSeed([]byte{0x01, 0x02, 0x03}, "password")
	assert.ErrorIs(t, err, ErrDecryptionFailed)
}

func TestEncryptSeed_DifferentCiphertexts(t *testing.T) {
	seed := make([]byte, 64)
	password := "same-password"

	enc1, err := EncryptSeed(seed, password)
	require.NoError(t, err)

	enc2, err := EncryptSeed(seed, password)
	require.NoError(t, err)

	// Should differ due to random salt and nonce
	assert.NotEqual(t, enc1, enc2, "same seed+password should produce different ciphertexts")

	// But both should decrypt correctly
	dec1, err := DecryptSeed(enc1, password)
	require.NoError(t, err)
	assert.Equal(t, seed, dec1)

	dec2, err := DecryptSeed(enc2, password)
	require.NoError(t, err)
	assert.Equal(t, seed, dec2)
}

// --- HD Key Derivation tests ---

func newTestWallet(t *testing.T) *Wallet {
	t.Helper()
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed, err := SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	w, err := NewWallet(seed, &MainNet)
	require.NoError(t, err)
	return w
}

func TestNewWallet(t *testing.T) {
	w := newTestWallet(t)
	assert.NotNil(t, w)
	assert.Equal(t, "mainnet", w.Network().Name)
}

func TestNewWallet_EmptySeed(t *testing.T) {
	_, err := NewWallet([]byte{}, nil)
	assert.ErrorIs(t, err, ErrInvalidSeed)
}

func TestNewWallet_NilNetwork(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed, err := SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	w, err := NewWallet(seed, nil)
	require.NoError(t, err)
	assert.Equal(t, "mainnet", w.Network().Name, "nil network should default to mainnet")
}

func TestDeriveFeeKey(t *testing.T) {
	w := newTestWallet(t)

	// Derive receive key
	kp, err := w.DeriveFeeKey(ExternalChain, 0)
	require.NoError(t, err)
	assert.NotNil(t, kp.PrivateKey)
	assert.NotNil(t, kp.PublicKey)
	assert.Equal(t, "m/44'/236'/0'/0/0", kp.Path)

	// Derive change key
	kp2, err := w.DeriveFeeKey(InternalChain, 0)
	require.NoError(t, err)
	assert.Equal(t, "m/44'/236'/0'/1/0", kp2.Path)

	// Different chains should produce different keys
	assert.NotEqual(t, kp.PublicKey.Compressed(), kp2.PublicKey.Compressed())
}

func TestDeriveFeeKey_Deterministic(t *testing.T) {
	w := newTestWallet(t)

	kp1, err := w.DeriveFeeKey(ExternalChain, 5)
	require.NoError(t, err)

	kp2, err := w.DeriveFeeKey(ExternalChain, 5)
	require.NoError(t, err)

	assert.Equal(t, kp1.PublicKey.Compressed(), kp2.PublicKey.Compressed())
}

func TestDeriveFeeKey_DifferentIndices(t *testing.T) {
	w := newTestWallet(t)

	kp1, err := w.DeriveFeeKey(ExternalChain, 0)
	require.NoError(t, err)

	kp2, err := w.DeriveFeeKey(ExternalChain, 1)
	require.NoError(t, err)

	assert.NotEqual(t, kp1.PublicKey.Compressed(), kp2.PublicKey.Compressed())
}

func TestDeriveVaultRootKey(t *testing.T) {
	w := newTestWallet(t)

	kp, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	assert.Equal(t, "m/44'/236'/1'/0/0", kp.Path)

	kp2, err := w.DeriveVaultRootKey(1)
	require.NoError(t, err)
	assert.Equal(t, "m/44'/236'/2'/0/0", kp2.Path)

	assert.NotEqual(t, kp.PublicKey.Compressed(), kp2.PublicKey.Compressed())
}

func TestDeriveNodeKey_RootDirectory(t *testing.T) {
	w := newTestWallet(t)

	kp, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "m/44'/236'/1'/0/0", kp.Path)
}

func TestDeriveNodeKey_ChildNode(t *testing.T) {
	w := newTestWallet(t)

	// First child of root (hardened by default)
	kp, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	assert.Equal(t, "m/44'/236'/1'/0/0/1'", kp.Path)
}

func TestDeriveNodeKey_NestedPath(t *testing.T) {
	w := newTestWallet(t)

	// Nested path: root -> child 3 -> child 1 -> child 7
	kp, err := w.DeriveNodeKey(0, []uint32{3, 1, 7}, nil)
	require.NoError(t, err)
	assert.Equal(t, "m/44'/236'/1'/0/0/3'/1'/7'", kp.Path)
}

func TestDeriveNodeKey_NonHardened(t *testing.T) {
	w := newTestWallet(t)

	// Explicit non-hardened derivation
	kp, err := w.DeriveNodeKey(0, []uint32{1, 2}, []bool{false, false})
	require.NoError(t, err)
	assert.Equal(t, "m/44'/236'/1'/0/0/1/2", kp.Path)
}

func TestDeriveNodeKey_MixedHardened(t *testing.T) {
	w := newTestWallet(t)

	// Mix: index 1 non-hardened, index 2 hardened
	kp, err := w.DeriveNodeKey(0, []uint32{1, 2}, []bool{false, true})
	require.NoError(t, err)
	assert.Equal(t, "m/44'/236'/1'/0/0/1/2'", kp.Path)
}

func TestDeriveNodeKey_HardenedDefault(t *testing.T) {
	w := newTestWallet(t)

	// Default (nil hardened array) = all hardened (design decision #82)
	kpHardened, err := w.DeriveNodeKey(0, []uint32{5}, nil)
	require.NoError(t, err)

	kpExplicit, err := w.DeriveNodeKey(0, []uint32{5}, []bool{true})
	require.NoError(t, err)

	assert.Equal(t, kpHardened.PublicKey.Compressed(), kpExplicit.PublicKey.Compressed())
}

func TestDeriveNodeKey_Deterministic(t *testing.T) {
	w := newTestWallet(t)

	kp1, err := w.DeriveNodeKey(0, []uint32{1, 2, 3}, nil)
	require.NoError(t, err)

	kp2, err := w.DeriveNodeKey(0, []uint32{1, 2, 3}, nil)
	require.NoError(t, err)

	assert.Equal(t, kp1.PublicKey.Compressed(), kp2.PublicKey.Compressed())
}

func TestDeriveNodeKey_DifferentVaults(t *testing.T) {
	w := newTestWallet(t)

	kp1, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	kp2, err := w.DeriveNodeKey(1, []uint32{1}, nil)
	require.NoError(t, err)

	assert.NotEqual(t, kp1.PublicKey.Compressed(), kp2.PublicKey.Compressed(),
		"same path in different vaults should produce different keys")
}

func TestDeriveNodeKey_PathTooDeep(t *testing.T) {
	w := newTestWallet(t)

	deepPath := make([]uint32, MaxPathDepth+1)
	for i := range deepPath {
		deepPath[i] = 1
	}

	_, err := w.DeriveNodeKey(0, deepPath, nil)
	assert.ErrorIs(t, err, ErrPathTooDeep)
}

func TestDeriveNodeKey_MaxDepthOK(t *testing.T) {
	w := newTestWallet(t)

	path := make([]uint32, MaxPathDepth) // exactly at limit
	for i := range path {
		path[i] = 1
	}

	kp, err := w.DeriveNodeKey(0, path, nil)
	require.NoError(t, err)
	assert.NotNil(t, kp)
}

func TestDeriveNodePubKey(t *testing.T) {
	w := newTestWallet(t)

	pubKey, err := w.DeriveNodePubKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	kp, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	assert.Equal(t, kp.PublicKey.Compressed(), pubKey.Compressed())
}

// --- Vault management tests ---

func TestCreateVault(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	vault, err := w.CreateVault(state, "personal")
	require.NoError(t, err)
	assert.Equal(t, "personal", vault.Name)
	assert.Equal(t, uint32(0), vault.AccountIndex)
	assert.Nil(t, vault.RootTxID)
	assert.False(t, vault.Deleted)
	assert.Len(t, state.Vaults, 1)
	assert.Equal(t, uint32(1), state.NextVaultIndex)
}

func TestCreateVault_Multiple(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	v1, err := w.CreateVault(state, "personal")
	require.NoError(t, err)
	assert.Equal(t, uint32(0), v1.AccountIndex)

	v2, err := w.CreateVault(state, "company")
	require.NoError(t, err)
	assert.Equal(t, uint32(1), v2.AccountIndex)

	assert.Len(t, state.Vaults, 2)
}

func TestCreateVault_DuplicateName(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	_, err := w.CreateVault(state, "personal")
	require.NoError(t, err)

	_, err = w.CreateVault(state, "personal")
	assert.ErrorIs(t, err, ErrVaultExists)
}

func TestGetVault(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	_, err := w.CreateVault(state, "personal")
	require.NoError(t, err)

	vault, err := w.GetVault(state, "personal")
	require.NoError(t, err)
	assert.Equal(t, "personal", vault.Name)
}

func TestGetVault_NotFound(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	_, err := w.GetVault(state, "nonexistent")
	assert.ErrorIs(t, err, ErrVaultNotFound)
}

func TestListVaults(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	w.CreateVault(state, "personal")
	w.CreateVault(state, "company")

	vaults := w.ListVaults(state)
	assert.Len(t, vaults, 2)
}

func TestListVaults_ExcludesDeleted(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	w.CreateVault(state, "personal")
	w.CreateVault(state, "company")
	w.DeleteVault(state, "company")

	vaults := w.ListVaults(state)
	assert.Len(t, vaults, 1)
	assert.Equal(t, "personal", vaults[0].Name)
}

func TestRenameVault(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	w.CreateVault(state, "old-name")

	err := w.RenameVault(state, "old-name", "new-name")
	require.NoError(t, err)

	_, err = w.GetVault(state, "new-name")
	require.NoError(t, err)

	_, err = w.GetVault(state, "old-name")
	assert.ErrorIs(t, err, ErrVaultNotFound)
}

func TestRenameVault_ConflictingName(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	w.CreateVault(state, "a")
	w.CreateVault(state, "b")

	err := w.RenameVault(state, "a", "b")
	assert.ErrorIs(t, err, ErrVaultExists)
}

func TestDeleteVault(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	w.CreateVault(state, "personal")

	err := w.DeleteVault(state, "personal")
	require.NoError(t, err)

	_, err = w.GetVault(state, "personal")
	assert.ErrorIs(t, err, ErrVaultNotFound, "deleted vault should not be found")
}

func TestDeleteVault_NotFound(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	err := w.DeleteVault(state, "nonexistent")
	assert.ErrorIs(t, err, ErrVaultNotFound)
}

func TestCreateVault_CanReuseDeletedName(t *testing.T) {
	w := newTestWallet(t)
	state := NewWalletState()

	w.CreateVault(state, "temp")
	w.DeleteVault(state, "temp")

	// Should be able to create a new vault with the same name
	vault, err := w.CreateVault(state, "temp")
	require.NoError(t, err)
	assert.Equal(t, "temp", vault.Name)
	// But it gets a new account index (indices are never reused)
	assert.Equal(t, uint32(1), vault.AccountIndex)
}

// --- Network tests ---

func TestGetNetwork(t *testing.T) {
	tests := []struct {
		name    string
		netName string
		wantErr bool
	}{
		{"mainnet", "mainnet", false},
		{"testnet", "testnet", false},
		{"regtest", "regtest", false},
		{"teratestnet", "teratestnet", false},
		{"unknown", "foonet", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			net, err := GetNetwork(tt.netName)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalidNetwork)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.netName, net.Name)
			}
		})
	}
}

func TestMainNetConfig(t *testing.T) {
	assert.Equal(t, byte(0x00), MainNet.AddressVersion)
	assert.Equal(t, byte(0x05), MainNet.P2SHVersion)
	assert.Equal(t, uint16(8333), MainNet.DefaultPort)
}

func TestTestNetConfig(t *testing.T) {
	assert.Equal(t, byte(0x6f), TestNet.AddressVersion)
	assert.Equal(t, uint16(18333), TestNet.DefaultPort)
}

// --- Integration: Full wallet workflow ---

func TestFullWalletWorkflow(t *testing.T) {
	// 1. Generate mnemonic
	mnemonic, err := GenerateMnemonic(Mnemonic12Words)
	require.NoError(t, err)

	// 2. Derive seed
	seed, err := SeedFromMnemonic(mnemonic, "my-passphrase")
	require.NoError(t, err)
	assert.Len(t, seed, 64)

	// 3. Encrypt seed
	encrypted, err := EncryptSeed(seed, "wallet-password")
	require.NoError(t, err)

	// 4. Create wallet (simulating app start)
	decryptedSeed, err := DecryptSeed(encrypted, "wallet-password")
	require.NoError(t, err)

	w, err := NewWallet(decryptedSeed, &MainNet)
	require.NoError(t, err)

	// 5. Create vaults
	state := NewWalletState()
	_, err = w.CreateVault(state, "personal")
	require.NoError(t, err)

	// 6. Derive keys for filesystem nodes
	rootKey, err := w.DeriveVaultRootKey(0)
	require.NoError(t, err)
	assert.Contains(t, rootKey.Path, "m/44'/236'/1'/0/0")

	// 7. Derive key for a file in the root directory
	fileKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)
	assert.Contains(t, fileKey.Path, "m/44'/236'/1'/0/0/1'")

	// 8. Derive fee chain key
	feeKey, err := w.DeriveFeeKey(ExternalChain, 0)
	require.NoError(t, err)
	assert.NotNil(t, feeKey.PrivateKey)
}

//go:build integration

package integration

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestMnemonic24Words generates a 24-word mnemonic, derives a seed, creates a wallet,
// derives keys, and verifies encrypt/decrypt round-trip.
func TestMnemonic24Words(t *testing.T) {
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic24Words)
	require.NoError(t, err, "GenerateMnemonic(256) should succeed")

	words := strings.Fields(mnemonic)
	require.Len(t, words, 24, "24-word mnemonic must have exactly 24 words")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "integration-test")
	require.NoError(t, err)
	require.Len(t, seed, 64, "BIP39 seed must be 64 bytes")

	w, err := wallet.NewWallet(seed, &wallet.MainNet)
	require.NoError(t, err)

	nodeKey, err := w.DeriveNodeKey(0, []uint32{1, 2}, nil)
	require.NoError(t, err)
	require.NotNil(t, nodeKey.PrivateKey)
	require.NotNil(t, nodeKey.PublicKey)

	plaintext := []byte("24-word mnemonic encrypt/decrypt test")
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	decResult, err := method42.Decrypt(encResult.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)
}

// TestMnemonicValidation is a table-driven test verifying ValidateMnemonic behaviour.
func TestMnemonicValidation(t *testing.T) {
	// Generate a valid 12-word mnemonic for testing.
	validMnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)

	tests := []struct {
		name     string
		mnemonic string
		want     bool
	}{
		{
			name:     "valid 12-word mnemonic",
			mnemonic: validMnemonic,
			want:     true,
		},
		{
			name:     "invalid words",
			mnemonic: "aaa bbb ccc ddd eee fff ggg hhh iii jjj kkk lll",
			want:     false,
		},
		{
			name:     "wrong word count (5 words)",
			mnemonic: "abandon abandon abandon abandon about",
			want:     false,
		},
		{
			name:     "empty string",
			mnemonic: "",
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := wallet.ValidateMnemonic(tc.mnemonic)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestCrossWalletDecryptionFails verifies that wallet B cannot decrypt content
// encrypted by wallet A, even at the same derivation path.
func TestCrossWalletDecryptionFails(t *testing.T) {
	wA, _, _ := createTestWallet(t, &wallet.MainNet)
	wB, _, _ := createTestWallet(t, &wallet.MainNet)

	nodeKeyA, err := wA.DeriveNodeKey(0, []uint32{1, 2, 3}, nil)
	require.NoError(t, err)

	nodeKeyB, err := wB.DeriveNodeKey(0, []uint32{1, 2, 3}, nil)
	require.NoError(t, err)

	// Different seeds must produce different keys at the same path.
	assert.NotEqual(t, nodeKeyA.PublicKey.Compressed(), nodeKeyB.PublicKey.Compressed(),
		"different wallets must derive different keys at same path")

	plaintext := []byte("wallet A secret")
	encResult, err := method42.Encrypt(plaintext, nodeKeyA.PrivateKey, nodeKeyA.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// Wallet B tries to decrypt with its own keys at the same path.
	_, err = method42.Decrypt(encResult.Ciphertext, nodeKeyB.PrivateKey, nodeKeyB.PublicKey, encResult.KeyHash, method42.AccessPrivate)
	assert.Error(t, err, "wallet B should fail to decrypt wallet A content")
}

// TestECDHSymmetry verifies ECDH(privA, pubB) == ECDH(privB, pubA).
func TestECDHSymmetry(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	keyA, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	keyB, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	sharedAB, err := method42.ECDH(keyA.PrivateKey, keyB.PublicKey)
	require.NoError(t, err)

	sharedBA, err := method42.ECDH(keyB.PrivateKey, keyA.PublicKey)
	require.NoError(t, err)

	assert.Equal(t, sharedAB, sharedBA, "ECDH must be symmetric: ECDH(privA, pubB) == ECDH(privB, pubA)")
}

// TestEncryptionKeyHashDeterminism verifies that encrypting the same plaintext twice
// with the same keys produces identical KeyHash but different ciphertext (random IV).
func TestEncryptionKeyHashDeterminism(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := []byte("deterministic key_hash test")

	enc1, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	enc2, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	assert.Equal(t, enc1.KeyHash, enc2.KeyHash, "KeyHash must be identical for same plaintext")
	assert.NotEqual(t, enc1.Ciphertext, enc2.Ciphertext, "Ciphertext must differ due to random IV")
}

// TestCiphertextTamperingDetected verifies that flipping a bit in ciphertext
// causes decryption to fail (AES-GCM authentication).
func TestCiphertextTamperingDetected(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := []byte("tamper detection test content")
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// Flip a random bit in the ciphertext.
	tampered := make([]byte, len(encResult.Ciphertext))
	copy(tampered, encResult.Ciphertext)
	// Pick a random byte index (after the nonce to ensure we tamper the encrypted data).
	idx := method42.NonceLen + 1
	if idx >= len(tampered) {
		idx = len(tampered) - 1
	}
	tampered[idx] ^= 0x01

	_, err = method42.Decrypt(tampered, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
	assert.Error(t, err, "tampered ciphertext must cause decryption failure")
}

// TestKeyHashIsDoubleSHA256 manually computes SHA256(SHA256(plaintext)) and
// verifies it matches EncryptResult.KeyHash.
func TestKeyHashIsDoubleSHA256(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := []byte("verify double SHA256 key hash")
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	first := sha256.Sum256(plaintext)
	second := sha256.Sum256(first[:])
	assert.Equal(t, second[:], encResult.KeyHash, "KeyHash must equal SHA256(SHA256(plaintext))")
}

// TestFreePrivateKeyIsWellKnown verifies FreePrivateKey returns scalar 1
// consistently and that ECDH with it produces the "trivial" shared secret.
func TestFreePrivateKeyIsWellKnown(t *testing.T) {
	fpk1 := method42.FreePrivateKey()
	fpk2 := method42.FreePrivateKey()
	require.NotNil(t, fpk1)
	require.NotNil(t, fpk2)

	// Both calls should return the same scalar.
	d1 := fpk1.Serialize()
	d2 := fpk2.Serialize()
	assert.Equal(t, d1, d2, "FreePrivateKey must be consistent across calls")

	// Scalar should be 1.
	one := new(big.Int).SetBytes(d1)
	assert.Equal(t, int64(1), one.Int64(), "FreePrivateKey scalar must be 1")

	// ECDH(1, P) should equal P.x for any public key P.
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	sharedFree, err := method42.ECDH(fpk1, nodeKey.PublicKey)
	require.NoError(t, err)

	// P.x from the public key (the x-coordinate of the point).
	pubX := nodeKey.PublicKey.X.Bytes()
	if len(pubX) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(pubX):], pubX)
		pubX = padded
	}
	assert.Equal(t, pubX[:32], sharedFree, "ECDH(1, P) must equal P.x")
}

// TestAccessModeTransitions verifies re-encryption between Free and Private modes.
func TestAccessModeTransitions(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{7}, nil)
	require.NoError(t, err)

	plaintext := []byte("access mode transition test")

	// Encrypt as Free.
	freeEnc, err := method42.Encrypt(plaintext, nil, nodeKey.PublicKey, method42.AccessFree)
	require.NoError(t, err)

	// Free decryption should succeed.
	decFree, err := method42.Decrypt(freeEnc.Ciphertext, nil, nodeKey.PublicKey, freeEnc.KeyHash, method42.AccessFree)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decFree.Plaintext)

	// Re-encrypt Free -> Private.
	privEnc, err := method42.ReEncrypt(freeEnc.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey,
		freeEnc.KeyHash, method42.AccessFree, method42.AccessPrivate)
	require.NoError(t, err)

	// Free decryption of private ciphertext should fail.
	_, err = method42.Decrypt(privEnc.Ciphertext, nil, nodeKey.PublicKey, privEnc.KeyHash, method42.AccessFree)
	assert.Error(t, err, "Free decryption should fail on Private ciphertext")

	// Private decryption should succeed.
	decPriv, err := method42.Decrypt(privEnc.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, privEnc.KeyHash, method42.AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decPriv.Plaintext)

	// Re-encrypt Private -> Free.
	freeEnc2, err := method42.ReEncrypt(privEnc.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey,
		privEnc.KeyHash, method42.AccessPrivate, method42.AccessFree)
	require.NoError(t, err)

	// Anyone should be able to decrypt the re-encrypted Free content.
	decFree2, err := method42.Decrypt(freeEnc2.Ciphertext, nil, nodeKey.PublicKey, freeEnc2.KeyHash, method42.AccessFree)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decFree2.Plaintext)
}

// TestLargeContentEncryption tests encrypt/decrypt with 1MB content and
// verifies the ciphertext overhead (IV + GCM tag).
func TestLargeContentEncryption(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := make([]byte, 1<<20) // 1 MB
	_, err = rand.Read(plaintext)
	require.NoError(t, err)

	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	decResult, err := method42.Decrypt(encResult.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)

	// Ciphertext = nonce(12) + encrypted(len(plaintext)) + GCM tag(16)
	expectedLen := len(plaintext) + method42.NonceLen + method42.GCMTagLen
	assert.Equal(t, expectedLen, len(encResult.Ciphertext),
		"ciphertext length should be plaintext + NonceLen + GCMTagLen")
}

// TestEmptyPassphraseAllowed verifies SeedFromMnemonic with "" passphrase
// succeeds and produces a seed different from passphrase "test".
func TestEmptyPassphraseAllowed(t *testing.T) {
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)

	seedEmpty, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)
	require.Len(t, seedEmpty, 64)

	seedTest, err := wallet.SeedFromMnemonic(mnemonic, "test")
	require.NoError(t, err)
	require.Len(t, seedTest, 64)

	assert.NotEqual(t, seedEmpty, seedTest,
		"empty passphrase and 'test' passphrase must produce different seeds")
}

// TestMaxPathDepth verifies DeriveNodeKey at MaxPathDepth succeeds and
// MaxPathDepth+1 fails with ErrPathTooDeep.
func TestMaxPathDepth(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	// Build a path of exactly MaxPathDepth elements.
	maxPath := make([]uint32, wallet.MaxPathDepth)
	for i := range maxPath {
		maxPath[i] = 0
	}

	_, err := w.DeriveNodeKey(0, maxPath, nil)
	require.NoError(t, err, "path of MaxPathDepth should succeed")

	// MaxPathDepth + 1 should fail.
	tooDeep := make([]uint32, wallet.MaxPathDepth+1)
	for i := range tooDeep {
		tooDeep[i] = 0
	}

	_, err = w.DeriveNodeKey(0, tooDeep, nil)
	assert.ErrorIs(t, err, wallet.ErrPathTooDeep, "path exceeding MaxPathDepth should fail")
}

// TestMaxFileIndex verifies DeriveNodeKey with MaxFileIndex succeeds and
// MaxFileIndex+1 fails with ErrFileIndexOutOfRange.
func TestMaxFileIndex(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	_, err := w.DeriveNodeKey(0, []uint32{uint32(wallet.MaxFileIndex)}, nil)
	require.NoError(t, err, "MaxFileIndex should succeed")

	_, err = w.DeriveNodeKey(0, []uint32{uint32(wallet.MaxFileIndex) + 1}, nil)
	assert.ErrorIs(t, err, wallet.ErrFileIndexOutOfRange, "MaxFileIndex+1 should fail")
}

// TestVaultCRUD exercises Create, List, Rename, Delete lifecycle.
func TestVaultCRUD(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	state := wallet.NewWalletState()

	// Create.
	vault, err := w.CreateVault(state, "my-vault")
	require.NoError(t, err)
	assert.Equal(t, "my-vault", vault.Name)

	// List (should contain 1).
	vaults := w.ListVaults(state)
	require.Len(t, vaults, 1)
	assert.Equal(t, "my-vault", vaults[0].Name)

	// Rename.
	err = w.RenameVault(state, "my-vault", "renamed-vault")
	require.NoError(t, err)

	vaults = w.ListVaults(state)
	require.Len(t, vaults, 1)
	assert.Equal(t, "renamed-vault", vaults[0].Name)

	// Delete.
	err = w.DeleteVault(state, "renamed-vault")
	require.NoError(t, err)

	vaults = w.ListVaults(state)
	assert.Len(t, vaults, 0)
}

// TestVaultDeletedExclusion creates 3 vaults, deletes one, and verifies
// it is excluded from ListVaults and GetVault.
func TestVaultDeletedExclusion(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	state := wallet.NewWalletState()

	_, err := w.CreateVault(state, "vault-1")
	require.NoError(t, err)
	_, err = w.CreateVault(state, "vault-2")
	require.NoError(t, err)
	_, err = w.CreateVault(state, "vault-3")
	require.NoError(t, err)

	err = w.DeleteVault(state, "vault-2")
	require.NoError(t, err)

	vaults := w.ListVaults(state)
	assert.Len(t, vaults, 2, "ListVaults should return 2 after deleting vault-2")

	for _, v := range vaults {
		assert.NotEqual(t, "vault-2", v.Name, "deleted vault must not appear in listing")
	}

	_, err = w.GetVault(state, "vault-2")
	assert.Error(t, err, "GetVault for deleted vault should fail")
}

// TestDuplicateVaultName verifies that creating a vault with a duplicate name fails.
func TestDuplicateVaultName(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	state := wallet.NewWalletState()

	_, err := w.CreateVault(state, "myVault")
	require.NoError(t, err)

	_, err = w.CreateVault(state, "myVault")
	assert.ErrorIs(t, err, wallet.ErrVaultExists, "duplicate vault name should fail")
}

// TestKeyPairPathFormat verifies the human-readable derivation path strings.
func TestKeyPairPathFormat(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	t.Run("VaultRootKey", func(t *testing.T) {
		rootKey, err := w.DeriveVaultRootKey(0)
		require.NoError(t, err)
		assert.Equal(t, "m/44'/236'/1'/0/0", rootKey.Path)
	})

	t.Run("NodeKey", func(t *testing.T) {
		nodeKey, err := w.DeriveNodeKey(0, []uint32{1, 2, 3}, nil)
		require.NoError(t, err)
		assert.Equal(t, "m/44'/236'/1'/0/0/1'/2'/3'", nodeKey.Path)
	})

	t.Run("FeeKey", func(t *testing.T) {
		feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
		require.NoError(t, err)
		assert.Equal(t, "m/44'/236'/0'/0/0", feeKey.Path)
	})
}

// TestNetworkConfigs verifies the three predefined network configurations.
func TestNetworkConfigs(t *testing.T) {
	assert.Equal(t, "mainnet", wallet.MainNet.Name)
	assert.Equal(t, "testnet", wallet.TestNet.Name)
	assert.Equal(t, "regtest", wallet.RegTest.Name)

	// Each must have a different GenesisHash.
	assert.NotEqual(t, wallet.MainNet.GenesisHash, wallet.TestNet.GenesisHash)
	assert.NotEqual(t, wallet.MainNet.GenesisHash, wallet.RegTest.GenesisHash)
	assert.NotEqual(t, wallet.TestNet.GenesisHash, wallet.RegTest.GenesisHash)
}

// TestDeriveNodePubKey verifies that DeriveNodePubKey returns only the public key
// and that it matches DeriveNodeKey's public key.
func TestDeriveNodePubKey(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	nodeKey, err := w.DeriveNodeKey(0, []uint32{5, 6}, nil)
	require.NoError(t, err)

	pubKey, err := w.DeriveNodePubKey(0, []uint32{5, 6}, nil)
	require.NoError(t, err)

	assert.Equal(t, nodeKey.PublicKey.Compressed(), pubKey.Compressed(),
		"DeriveNodePubKey must match DeriveNodeKey public key")
}

// TestSeedEncryptionWrongPassword verifies wrong password fails, correct succeeds.
func TestSeedEncryptionWrongPassword(t *testing.T) {
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)

	seed, err := wallet.SeedFromMnemonic(mnemonic, "pass")
	require.NoError(t, err)

	encrypted, err := wallet.EncryptSeed(seed, "passwordA")
	require.NoError(t, err)

	_, err = wallet.DecryptSeed(encrypted, "passwordB")
	assert.Error(t, err, "wrong password must fail decryption")

	decrypted, err := wallet.DecryptSeed(encrypted, "passwordA")
	require.NoError(t, err)
	assert.Equal(t, seed, decrypted)
}

// TestSeedEncryptionDeterministic verifies two EncryptSeed calls with same
// seed+password produce different ciphertexts (random salt) but both decrypt
// to the same seed.
func TestSeedEncryptionDeterministic(t *testing.T) {
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)

	seed, err := wallet.SeedFromMnemonic(mnemonic, "pass")
	require.NoError(t, err)

	enc1, err := wallet.EncryptSeed(seed, "samePassword")
	require.NoError(t, err)

	enc2, err := wallet.EncryptSeed(seed, "samePassword")
	require.NoError(t, err)

	assert.NotEqual(t, enc1, enc2, "two encryptions must differ due to random salt")

	dec1, err := wallet.DecryptSeed(enc1, "samePassword")
	require.NoError(t, err)

	dec2, err := wallet.DecryptSeed(enc2, "samePassword")
	require.NoError(t, err)

	assert.Equal(t, seed, dec1)
	assert.Equal(t, seed, dec2)
}

// TestCapsuleComputationConsistency verifies ComputeCapsule is deterministic
// (same inputs produce the same output) and that the capsule round-trips
// correctly through DecryptWithCapsule.
func TestCapsuleComputationConsistency(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	buyerKey, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	// Encrypt content to get a keyHash
	plaintext := []byte("capsule consistency test content")
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// ComputeCapsule should be deterministic
	capsule1, err := method42.ComputeCapsule(nodeKey.PrivateKey, nodeKey.PublicKey, buyerKey.PublicKey, encResult.KeyHash)
	require.NoError(t, err)

	capsule2, err := method42.ComputeCapsule(nodeKey.PrivateKey, nodeKey.PublicKey, buyerKey.PublicKey, encResult.KeyHash)
	require.NoError(t, err)

	assert.Equal(t, capsule1, capsule2, "repeated ComputeCapsule must produce same result")

	// Capsule should round-trip: ComputeCapsule -> DecryptWithCapsule -> correct plaintext
	decResult, err := method42.DecryptWithCapsule(encResult.Ciphertext, capsule1, encResult.KeyHash, buyerKey.PrivateKey, nodeKey.PublicKey)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext, "capsule round-trip must recover original plaintext")
}

// TestCapsuleHashBindsFileTxID verifies ComputeCapsuleHash == SHA256(fileTxID ‖ capsule).
func TestCapsuleHashBindsFileTxID(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	buyerKey, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	// Generate a plaintext, compute keyHash, then call ComputeCapsule with all 4 args
	plaintext := []byte("capsule hash SHA256 test")
	keyHash := method42.ComputeKeyHash(plaintext)

	capsule, err := method42.ComputeCapsule(nodeKey.PrivateKey, nodeKey.PublicKey, buyerKey.PublicKey, keyHash)
	require.NoError(t, err)

	fileTxID := bytes.Repeat([]byte{0xf0}, 32) // mock file txid
	capsuleHash := method42.ComputeCapsuleHash(fileTxID, capsule)

	manual := sha256.New()
	manual.Write(fileTxID)
	manual.Write(capsule)
	assert.Equal(t, manual.Sum(nil), capsuleHash, "ComputeCapsuleHash must equal SHA256(fileTxID || capsule)")
}

// TestDecryptWithCapsuleMatchesRegularDecrypt verifies that DecryptWithCapsule
// produces the same plaintext as regular Decrypt. A buyer keypair is generated,
// the XOR capsule is computed, and DecryptWithCapsule recovers the same plaintext.
func TestDecryptWithCapsuleMatchesRegularDecrypt(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	// Generate a buyer keypair
	buyerKey, err := w.DeriveNodeKey(0, []uint32{2}, nil)
	require.NoError(t, err)

	plaintext := []byte("capsule vs regular decrypt test")
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	// Regular decrypt (owner side).
	decRegular, err := method42.Decrypt(encResult.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
	require.NoError(t, err)

	// Capsule decrypt: compute XOR capsule (seller side), then decrypt (buyer side).
	capsule, err := method42.ComputeCapsule(nodeKey.PrivateKey, nodeKey.PublicKey, buyerKey.PublicKey, encResult.KeyHash)
	require.NoError(t, err)

	decCapsule, err := method42.DecryptWithCapsule(encResult.Ciphertext, capsule, encResult.KeyHash, buyerKey.PrivateKey, nodeKey.PublicKey)
	require.NoError(t, err)

	assert.Equal(t, decRegular.Plaintext, decCapsule.Plaintext,
		"DecryptWithCapsule must produce same plaintext as Decrypt")
	assert.Equal(t, plaintext, decCapsule.Plaintext)
}

// TestDeriveAESKeyDeterminism verifies DeriveAESKey returns the same key
// for the same inputs.
func TestDeriveAESKeyDeterminism(t *testing.T) {
	sharedSecret := bytes.Repeat([]byte{0xAB}, 32)
	keyHash := bytes.Repeat([]byte{0xCD}, 32)

	key1, err := method42.DeriveAESKey(sharedSecret, keyHash)
	require.NoError(t, err)
	require.Len(t, key1, 32)

	key2, err := method42.DeriveAESKey(sharedSecret, keyHash)
	require.NoError(t, err)

	assert.Equal(t, key1, key2, "DeriveAESKey must be deterministic")
}

// TestDeriveAESKeyDifferentInputs verifies that different inputs produce
// different AES keys.
func TestDeriveAESKeyDifferentInputs(t *testing.T) {
	secretA := bytes.Repeat([]byte{0x01}, 32)
	secretB := bytes.Repeat([]byte{0x02}, 32)
	hashA := bytes.Repeat([]byte{0x03}, 32)
	hashB := bytes.Repeat([]byte{0x04}, 32)

	keyAA, err := method42.DeriveAESKey(secretA, hashA)
	require.NoError(t, err)

	// Different sharedSecret, same keyHash.
	keyBA, err := method42.DeriveAESKey(secretB, hashA)
	require.NoError(t, err)
	assert.NotEqual(t, keyAA, keyBA, "different sharedSecret must produce different key")

	// Same sharedSecret, different keyHash.
	keyAB, err := method42.DeriveAESKey(secretA, hashB)
	require.NoError(t, err)
	assert.NotEqual(t, keyAA, keyAB, "different keyHash must produce different key")
}

// TestComputeKeyHashDifferentContent verifies that different plaintexts
// produce different KeyHashes.
func TestComputeKeyHashDifferentContent(t *testing.T) {
	hashA := method42.ComputeKeyHash([]byte("content A"))
	hashB := method42.ComputeKeyHash([]byte("content B"))

	assert.NotEqual(t, hashA, hashB, "different plaintexts must produce different KeyHashes")
}

// TestEncryptNilPrivateKeyRequiresFreeMode verifies that nil privKey
// with AccessPrivate fails but AccessFree succeeds.
func TestEncryptNilPrivateKeyRequiresFreeMode(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := []byte("nil private key test")

	// Private mode with nil privKey should fail.
	_, err = method42.Encrypt(plaintext, nil, nodeKey.PublicKey, method42.AccessPrivate)
	assert.Error(t, err, "nil privKey + AccessPrivate must fail")

	// Free mode with nil privKey should succeed.
	encResult, err := method42.Encrypt(plaintext, nil, nodeKey.PublicKey, method42.AccessFree)
	require.NoError(t, err)
	assert.NotEmpty(t, encResult.Ciphertext)
}

// TestDecryptNilKeyHashFails verifies that Decrypt with nil keyHash fails.
func TestDecryptNilKeyHashFails(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := []byte("nil keyhash test")
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	_, err = method42.Decrypt(encResult.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, nil, method42.AccessPrivate)
	assert.Error(t, err, "nil keyHash must cause decryption to fail")
}

// TestMultipleVaultNodeKeyIsolation derives nodeKeys from vault 0 and vault 1
// at the same path and verifies they differ and cannot cross-decrypt.
func TestMultipleVaultNodeKeyIsolation(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	state := wallet.NewWalletState()
	_, err := w.CreateVault(state, "vault-0")
	require.NoError(t, err)
	_, err = w.CreateVault(state, "vault-1")
	require.NoError(t, err)

	key0, err := w.DeriveNodeKey(0, []uint32{1, 2}, nil)
	require.NoError(t, err)

	key1, err := w.DeriveNodeKey(1, []uint32{1, 2}, nil)
	require.NoError(t, err)

	assert.NotEqual(t, key0.PublicKey.Compressed(), key1.PublicKey.Compressed(),
		"keys from different vaults must differ")

	plaintext := []byte("vault isolation test")
	encResult, err := method42.Encrypt(plaintext, key0.PrivateKey, key0.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	_, err = method42.Decrypt(encResult.Ciphertext, key1.PrivateKey, key1.PublicKey, encResult.KeyHash, method42.AccessPrivate)
	assert.Error(t, err, "vault 1 key must not decrypt vault 0 content")
}

// TestInternalChainFeeKey verifies that InternalChain and ExternalChain
// produce different fee keys.
func TestInternalChainFeeKey(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)

	extKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err)

	intKey, err := w.DeriveFeeKey(wallet.InternalChain, 0)
	require.NoError(t, err)

	assert.NotEqual(t, extKey.PublicKey.Compressed(), intKey.PublicKey.Compressed(),
		"InternalChain and ExternalChain must produce different keys")
	assert.Equal(t, "m/44'/236'/0'/0/0", extKey.Path)
	assert.Equal(t, "m/44'/236'/0'/1/0", intKey.Path)
}

// TestWalletNetworkAccessor verifies w.Network() returns the same config
// that was passed to NewWallet.
func TestWalletNetworkAccessor(t *testing.T) {
	for _, nc := range networkConfigs() {
		t.Run(nc.name, func(t *testing.T) {
			w, _, _ := createTestWallet(t, nc.network)
			assert.Equal(t, nc.network.Name, w.Network().Name)
			assert.Equal(t, nc.network.GenesisHash, w.Network().GenesisHash)
		})
	}
}

// TestArgon2Parameters verifies the Argon2id constants have expected values.
func TestArgon2Parameters(t *testing.T) {
	assert.Equal(t, uint32(3), uint32(wallet.Argon2Time))
	assert.Equal(t, uint32(65536), uint32(wallet.Argon2Memory))
	assert.Equal(t, uint8(4), uint8(wallet.Argon2Parallelism))
	assert.Equal(t, uint32(32), uint32(wallet.Argon2KeyLen))
}

// TestEncryptResultFieldSizes verifies the sizes of EncryptResult fields.
func TestEncryptResultFieldSizes(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	plaintext := []byte("field size verification test")
	encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
	require.NoError(t, err)

	assert.Len(t, encResult.KeyHash, 32, "KeyHash must be 32 bytes")
	expectedCiphertextLen := len(plaintext) + method42.NonceLen + method42.GCMTagLen
	assert.Len(t, encResult.Ciphertext, expectedCiphertextLen,
		"Ciphertext must be len(plaintext) + NonceLen + GCMTagLen")
}

// TestReEncryptPreservesKeyHash verifies that re-encrypting from Free to Private
// preserves the same KeyHash since the underlying content is unchanged.
func TestReEncryptPreservesKeyHash(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{3}, nil)
	require.NoError(t, err)

	plaintext := []byte("re-encrypt keyhash preservation test")

	freeEnc, err := method42.Encrypt(plaintext, nil, nodeKey.PublicKey, method42.AccessFree)
	require.NoError(t, err)

	privEnc, err := method42.ReEncrypt(freeEnc.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey,
		freeEnc.KeyHash, method42.AccessFree, method42.AccessPrivate)
	require.NoError(t, err)

	assert.Equal(t, freeEnc.KeyHash, privEnc.KeyHash,
		"ReEncrypt must preserve KeyHash since content is unchanged")
}

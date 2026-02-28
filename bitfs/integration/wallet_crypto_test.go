//go:build integration

// Package integration provides cross-package integration tests for BitFS.
// These tests exercise the full stack: wallet -> key derivation -> Method42 encryption.
// Run with: go test -tags=integration ./integration/ -count=1 -v
package integration

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// networkConfigs returns all 3 network configurations for parametric testing.
func networkConfigs() []struct {
	name    string
	network *wallet.NetworkConfig
} {
	return []struct {
		name    string
		network *wallet.NetworkConfig
	}{
		{"regtest", &wallet.RegTest},
		{"testnet", &wallet.TestNet},
		{"mainnet", &wallet.MainNet},
	}
}

// createTestWallet is a helper that generates a mnemonic, derives seed, and creates a wallet.
func createTestWallet(t *testing.T, network *wallet.NetworkConfig) (*wallet.Wallet, string, []byte) {
	t.Helper()
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "failed to generate mnemonic")

	words := strings.Fields(mnemonic)
	require.Len(t, words, 12, "mnemonic should have 12 words")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "integration-test")
	require.NoError(t, err, "failed to derive seed")
	require.Len(t, seed, 64, "BIP39 seed should be 64 bytes")

	w, err := wallet.NewWallet(seed, network)
	require.NoError(t, err, "failed to create wallet")
	require.NotNil(t, w)

	return w, mnemonic, seed
}

// --- TestWalletEncryptDecryptRoundTrip ---

func TestWalletEncryptDecryptRoundTrip_regtest(t *testing.T) {
	testWalletEncryptDecryptRoundTrip(t, &wallet.RegTest)
}

func TestWalletEncryptDecryptRoundTrip_testnet(t *testing.T) {
	testWalletEncryptDecryptRoundTrip(t, &wallet.TestNet)
}

func TestWalletEncryptDecryptRoundTrip_mainnet(t *testing.T) {
	testWalletEncryptDecryptRoundTrip(t, &wallet.MainNet)
}

func testWalletEncryptDecryptRoundTrip(t *testing.T, network *wallet.NetworkConfig) {
	t.Helper()
	t.Run(network.Name, func(t *testing.T) {
		// 1. Generate mnemonic (12 words)
		mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
		require.NoError(t, err)
		words := strings.Fields(mnemonic)
		assert.Len(t, words, 12)

		// 2. Derive seed from mnemonic
		seed, err := wallet.SeedFromMnemonic(mnemonic, "test-passphrase")
		require.NoError(t, err)
		assert.Len(t, seed, 64)

		// 3. Create wallet with network config
		w, err := wallet.NewWallet(seed, network)
		require.NoError(t, err)
		assert.Equal(t, network.Name, w.Network().Name)

		// 4. Derive vault root key (vault 0)
		rootKey, err := w.DeriveVaultRootKey(0)
		require.NoError(t, err)
		assert.NotNil(t, rootKey.PrivateKey)
		assert.NotNil(t, rootKey.PublicKey)
		assert.Equal(t, "m/44'/236'/1'/0/0", rootKey.Path)

		// 5. Derive child node key (file path [1,2,3])
		nodeKey, err := w.DeriveNodeKey(0, []uint32{1, 2, 3}, nil)
		require.NoError(t, err)
		assert.NotNil(t, nodeKey.PrivateKey)
		assert.NotNil(t, nodeKey.PublicKey)
		assert.Equal(t, "m/44'/236'/1'/0/0/1'/2'/3'", nodeKey.Path)

		// 6. Encrypt test content with Method42 (AccessPrivate)
		plaintext := []byte("Integration test content for " + network.Name)
		encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
		require.NoError(t, err)
		assert.NotEmpty(t, encResult.Ciphertext)
		assert.Len(t, encResult.KeyHash, 32)
		// 7. Decrypt with same keys
		decResult, err := method42.Decrypt(encResult.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
		require.NoError(t, err)

		// 8. Verify plaintext matches
		assert.Equal(t, plaintext, decResult.Plaintext, "decrypted content should match original")

		// 9. Verify key_hash = SHA256(SHA256(plaintext))
		first := sha256.Sum256(plaintext)
		second := sha256.Sum256(first[:])
		assert.Equal(t, second[:], encResult.KeyHash, "key_hash should be double-SHA256 of plaintext")
		assert.Equal(t, encResult.KeyHash, decResult.KeyHash, "decrypt key_hash should match encrypt key_hash")
	})
}

// --- TestFreeAccessCrossNodes ---

func TestFreeAccessCrossNodes_regtest(t *testing.T) {
	testFreeAccessCrossNodes(t, &wallet.RegTest)
}

func TestFreeAccessCrossNodes_testnet(t *testing.T) {
	testFreeAccessCrossNodes(t, &wallet.TestNet)
}

func TestFreeAccessCrossNodes_mainnet(t *testing.T) {
	testFreeAccessCrossNodes(t, &wallet.MainNet)
}

func testFreeAccessCrossNodes(t *testing.T, network *wallet.NetworkConfig) {
	t.Helper()
	t.Run(network.Name, func(t *testing.T) {
		w, _, _ := createTestWallet(t, network)

		// Derive node A's keys
		nodeA, err := w.DeriveNodeKey(0, []uint32{1}, nil)
		require.NoError(t, err)

		// 1. Encrypt with AccessFree using node A's keys
		plaintext := []byte("Free content accessible by anyone on " + network.Name)
		encResult, err := method42.Encrypt(plaintext, nil, nodeA.PublicKey, method42.AccessFree)
		require.NoError(t, err)

		// 2. Decrypt using only the public key (any third party)
		// For AccessFree, private key is scalar 1 (FreePrivateKey), so nil is passed
		decResult, err := method42.Decrypt(encResult.Ciphertext, nil, nodeA.PublicKey, encResult.KeyHash, method42.AccessFree)
		require.NoError(t, err)

		// 3. Verify content matches
		assert.Equal(t, plaintext, decResult.Plaintext)

		// Also verify a completely different wallet can decrypt
		w2, _, _ := createTestWallet(t, network)
		_ = w2 // w2 is not needed for Free decryption, just confirming any party can
		decResult2, err := method42.Decrypt(encResult.Ciphertext, nil, nodeA.PublicKey, encResult.KeyHash, method42.AccessFree)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decResult2.Plaintext)
	})
}

// --- TestPaidAccessCapsuleFlow ---

func TestPaidAccessCapsuleFlow_regtest(t *testing.T) {
	testPaidAccessCapsuleFlow(t, &wallet.RegTest)
}

func TestPaidAccessCapsuleFlow_testnet(t *testing.T) {
	testPaidAccessCapsuleFlow(t, &wallet.TestNet)
}

func TestPaidAccessCapsuleFlow_mainnet(t *testing.T) {
	testPaidAccessCapsuleFlow(t, &wallet.MainNet)
}

func testPaidAccessCapsuleFlow(t *testing.T, network *wallet.NetworkConfig) {
	t.Helper()
	t.Run(network.Name, func(t *testing.T) {
		// Owner wallet
		ownerW, _, _ := createTestWallet(t, network)
		ownerNode, err := ownerW.DeriveNodeKey(0, []uint32{1}, nil)
		require.NoError(t, err)

		// 1. Owner encrypts file with AccessPrivate
		plaintext := []byte("Paid premium content on " + network.Name)
		encResult, err := method42.Encrypt(plaintext, ownerNode.PrivateKey, ownerNode.PublicKey, method42.AccessPrivate)
		require.NoError(t, err)

		// 2. Generate buyer keypair
		buyerW, _, _ := createTestWallet(t, network)
		buyerNode, err := buyerW.DeriveNodeKey(0, []uint32{2}, nil)
		require.NoError(t, err)

		// 3. Compute capsule for buyer (seller side): XOR capsule flow
		capsule, err := method42.ComputeCapsule(ownerNode.PrivateKey, ownerNode.PublicKey, buyerNode.PublicKey, encResult.KeyHash)
		require.NoError(t, err)
		assert.Len(t, capsule, 32)

		// Compute capsule_hash for HTLC verification
		fileTxID := bytes.Repeat([]byte{0xf0}, 32) // mock file txid
		capsuleHash := method42.ComputeCapsuleHash(fileTxID, capsule)
		assert.Len(t, capsuleHash, 32)

		// 4. Buyer decrypts using capsule (obtained via HTLC)
		decResult, err := method42.DecryptWithCapsule(encResult.Ciphertext, capsule, encResult.KeyHash, buyerNode.PrivateKey, ownerNode.PublicKey)
		require.NoError(t, err)

		// 4. Verify content matches
		assert.Equal(t, plaintext, decResult.Plaintext)

		// Verify capsule hash matches
		recomputedHash := method42.ComputeCapsuleHash(fileTxID, capsule)
		assert.Equal(t, capsuleHash, recomputedHash)
	})
}

// --- TestReEncryptFreeToPrivate ---

func TestReEncryptFreeToPrivate_regtest(t *testing.T) {
	testReEncryptFreeToPrivate(t, &wallet.RegTest)
}

func TestReEncryptFreeToPrivate_testnet(t *testing.T) {
	testReEncryptFreeToPrivate(t, &wallet.TestNet)
}

func TestReEncryptFreeToPrivate_mainnet(t *testing.T) {
	testReEncryptFreeToPrivate(t, &wallet.MainNet)
}

func testReEncryptFreeToPrivate(t *testing.T, network *wallet.NetworkConfig) {
	t.Helper()
	t.Run(network.Name, func(t *testing.T) {
		w, _, _ := createTestWallet(t, network)
		nodeKey, err := w.DeriveNodeKey(0, []uint32{5}, nil)
		require.NoError(t, err)

		plaintext := []byte("Originally free, now private on " + network.Name)

		// 1. Encrypt as Free
		freeResult, err := method42.Encrypt(plaintext, nil, nodeKey.PublicKey, method42.AccessFree)
		require.NoError(t, err)

		// Verify Free decryption works
		decFree, err := method42.Decrypt(freeResult.Ciphertext, nil, nodeKey.PublicKey, freeResult.KeyHash, method42.AccessFree)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decFree.Plaintext)

		// 2. ReEncrypt from Free -> Private
		privResult, err := method42.ReEncrypt(
			freeResult.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey,
			freeResult.KeyHash, method42.AccessFree, method42.AccessPrivate,
		)
		require.NoError(t, err)

		// 3. Verify old Free key can't decrypt new ciphertext
		// The new ciphertext uses ECDH(D_node, P_node) not ECDH(1, P_node)
		_, err = method42.Decrypt(privResult.Ciphertext, nil, nodeKey.PublicKey, privResult.KeyHash, method42.AccessFree)
		assert.Error(t, err, "Free mode should not decrypt Private-encrypted content")

		// 4. Verify owner Private key can decrypt
		decPriv, err := method42.Decrypt(privResult.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, privResult.KeyHash, method42.AccessPrivate)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decPriv.Plaintext)
	})
}

// --- TestDeterministicKeyDerivation ---

func TestDeterministicKeyDerivation_regtest(t *testing.T) {
	testDeterministicKeyDerivation(t, &wallet.RegTest)
}

func TestDeterministicKeyDerivation_testnet(t *testing.T) {
	testDeterministicKeyDerivation(t, &wallet.TestNet)
}

func TestDeterministicKeyDerivation_mainnet(t *testing.T) {
	testDeterministicKeyDerivation(t, &wallet.MainNet)
}

func testDeterministicKeyDerivation(t *testing.T, network *wallet.NetworkConfig) {
	t.Helper()
	t.Run(network.Name, func(t *testing.T) {
		mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
		passphrase := "deterministic-test"

		// 1. Same mnemonic + passphrase -> same seed
		seed1, err := wallet.SeedFromMnemonic(mnemonic, passphrase)
		require.NoError(t, err)
		seed2, err := wallet.SeedFromMnemonic(mnemonic, passphrase)
		require.NoError(t, err)
		assert.Equal(t, seed1, seed2, "same mnemonic+passphrase should produce same seed")

		// 2. Same seed -> same wallet keys
		w1, err := wallet.NewWallet(seed1, network)
		require.NoError(t, err)
		w2, err := wallet.NewWallet(seed2, network)
		require.NoError(t, err)

		fee1, err := w1.DeriveFeeKey(wallet.ExternalChain, 0)
		require.NoError(t, err)
		fee2, err := w2.DeriveFeeKey(wallet.ExternalChain, 0)
		require.NoError(t, err)
		assert.Equal(t, fee1.PublicKey.Compressed(), fee2.PublicKey.Compressed(),
			"same seed should produce same fee keys")

		// 3. Same vault/path -> same node keys
		node1, err := w1.DeriveNodeKey(0, []uint32{1, 2, 3}, nil)
		require.NoError(t, err)
		node2, err := w2.DeriveNodeKey(0, []uint32{1, 2, 3}, nil)
		require.NoError(t, err)
		assert.Equal(t, node1.PublicKey.Compressed(), node2.PublicKey.Compressed(),
			"same seed+vault+path should produce same node keys")
		assert.Equal(t, node1.Path, node2.Path)

		// 4. Cross-verify: derive independently twice, compare
		root1, err := w1.DeriveVaultRootKey(0)
		require.NoError(t, err)
		root2, err := w2.DeriveVaultRootKey(0)
		require.NoError(t, err)
		assert.Equal(t, root1.PublicKey.Compressed(), root2.PublicKey.Compressed())

		// 5. Different passphrase -> different seed -> different keys
		seed3, err := wallet.SeedFromMnemonic(mnemonic, "different-passphrase")
		require.NoError(t, err)
		assert.NotEqual(t, seed1, seed3, "different passphrase should produce different seed")

		w3, err := wallet.NewWallet(seed3, network)
		require.NoError(t, err)
		node3, err := w3.DeriveNodeKey(0, []uint32{1, 2, 3}, nil)
		require.NoError(t, err)
		assert.NotEqual(t, node1.PublicKey.Compressed(), node3.PublicKey.Compressed(),
			"different seed should produce different keys")

		// 6. Verify encryption with deterministic keys works consistently
		plaintext := []byte("deterministic encryption test")
		enc1, err := method42.Encrypt(plaintext, node1.PrivateKey, node1.PublicKey, method42.AccessPrivate)
		require.NoError(t, err)
		// node2 has the same keys, so it should be able to decrypt
		dec2, err := method42.Decrypt(enc1.Ciphertext, node2.PrivateKey, node2.PublicKey, enc1.KeyHash, method42.AccessPrivate)
		require.NoError(t, err)
		assert.Equal(t, plaintext, dec2.Plaintext, "deterministically derived keys should cross-decrypt")
	})
}

// --- TestWalletCryptoWithSeedEncryption ---

func TestWalletCryptoWithSeedEncryption(t *testing.T) {
	for _, nc := range networkConfigs() {
		t.Run(nc.name, func(t *testing.T) {
			// Full lifecycle: mnemonic -> seed -> encrypt seed -> decrypt seed -> wallet -> encrypt content -> decrypt content
			mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
			require.NoError(t, err)

			seed, err := wallet.SeedFromMnemonic(mnemonic, "my-passphrase")
			require.NoError(t, err)

			// Encrypt the seed (simulating wallet.enc file)
			walletPassword := "strong-wallet-password-123"
			encryptedSeed, err := wallet.EncryptSeed(seed, walletPassword)
			require.NoError(t, err)

			// Decrypt the seed (simulating wallet open)
			decryptedSeed, err := wallet.DecryptSeed(encryptedSeed, walletPassword)
			require.NoError(t, err)
			assert.Equal(t, seed, decryptedSeed)

			// Wrong password should fail
			_, err = wallet.DecryptSeed(encryptedSeed, "wrong-password")
			assert.Error(t, err)

			// Create wallet from decrypted seed
			w, err := wallet.NewWallet(decryptedSeed, nc.network)
			require.NoError(t, err)

			// Create vault and derive key
			state := wallet.NewWalletState()
			_, err = w.CreateVault(state, "test-vault")
			require.NoError(t, err)

			nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
			require.NoError(t, err)

			// Encrypt and decrypt content
			plaintext := []byte("End-to-end lifecycle test content")
			encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
			require.NoError(t, err)

			decResult, err := method42.Decrypt(encResult.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
			require.NoError(t, err)
			assert.Equal(t, plaintext, decResult.Plaintext)
		})
	}
}

// --- TestMultiVaultKeyIsolation ---

func TestMultiVaultKeyIsolation(t *testing.T) {
	for _, nc := range networkConfigs() {
		t.Run(nc.name, func(t *testing.T) {
			w, _, _ := createTestWallet(t, nc.network)

			state := wallet.NewWalletState()
			_, err := w.CreateVault(state, "vault-a")
			require.NoError(t, err)
			_, err = w.CreateVault(state, "vault-b")
			require.NoError(t, err)

			// Same file path in different vaults should produce different keys
			keyA, err := w.DeriveNodeKey(0, []uint32{1, 2}, nil)
			require.NoError(t, err)
			keyB, err := w.DeriveNodeKey(1, []uint32{1, 2}, nil)
			require.NoError(t, err)

			assert.NotEqual(t, keyA.PublicKey.Compressed(), keyB.PublicKey.Compressed(),
				"same path in different vaults should produce different keys")

			// Encrypt with vault A keys
			plaintext := []byte("vault-A secret")
			encA, err := method42.Encrypt(plaintext, keyA.PrivateKey, keyA.PublicKey, method42.AccessPrivate)
			require.NoError(t, err)

			// Vault B keys should NOT be able to decrypt vault A content
			_, err = method42.Decrypt(encA.Ciphertext, keyB.PrivateKey, keyB.PublicKey, encA.KeyHash, method42.AccessPrivate)
			assert.Error(t, err, "vault B keys should not decrypt vault A content")
		})
	}
}

// --- TestFeeKeyIsolationFromVaultKeys (T029) ---

func TestFeeKeyIsolationFromVaultKeys(t *testing.T) {
	for _, nc := range networkConfigs() {
		t.Run(nc.name, func(t *testing.T) {
			w, _, _ := createTestWallet(t, nc.network)

			// 1. Derive fee key (ExternalChain, index 0)
			feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
			require.NoError(t, err)
			require.NotNil(t, feeKey.PrivateKey)
			require.NotNil(t, feeKey.PublicKey)

			// Fee key path should be m/44'/236'/0'/0/0 (fee account = 0)
			assert.Equal(t, "m/44'/236'/0'/0/0", feeKey.Path,
				"fee key should use account 0 (fee account)")

			// 2. Derive vault root key (vault 0) -> account 1
			vaultRootKey, err := w.DeriveVaultRootKey(0)
			require.NoError(t, err)
			require.NotNil(t, vaultRootKey.PrivateKey)
			require.NotNil(t, vaultRootKey.PublicKey)

			// Vault root path should be m/44'/236'/1'/0/0 (vault account starts at 1)
			assert.Equal(t, "m/44'/236'/1'/0/0", vaultRootKey.Path,
				"vault root key should use account 1 (DefaultVaultAccount)")

			// 3. Derive a node key under vault 0
			nodeKey, err := w.DeriveNodeKey(0, []uint32{1, 2}, nil)
			require.NoError(t, err)
			require.NotNil(t, nodeKey.PrivateKey)
			require.NotNil(t, nodeKey.PublicKey)

			// Node key should be under account 1 as well
			assert.Equal(t, "m/44'/236'/1'/0/0/1'/2'", nodeKey.Path,
				"node key should be under vault account")

			// 4. Verify all three keys are different
			feePub := feeKey.PublicKey.Compressed()
			vaultRootPub := vaultRootKey.PublicKey.Compressed()
			nodePub := nodeKey.PublicKey.Compressed()

			assert.NotEqual(t, feePub, vaultRootPub,
				"fee key must differ from vault root key")
			assert.NotEqual(t, feePub, nodePub,
				"fee key must differ from node key")
			assert.NotEqual(t, vaultRootPub, nodePub,
				"vault root key must differ from node key")

			// 5. Encrypt with node key, verify fee key CANNOT decrypt
			plaintext := []byte("Isolated encryption test on " + nc.name)
			encResult, err := method42.Encrypt(plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
			require.NoError(t, err)

			// Fee key should NOT be able to decrypt content encrypted with node key
			_, err = method42.Decrypt(encResult.Ciphertext, feeKey.PrivateKey, feeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
			assert.Error(t, err, "fee key must not decrypt content encrypted with node key")

			// Vault root key should also NOT be able to decrypt (different derivation path)
			_, err = method42.Decrypt(encResult.Ciphertext, vaultRootKey.PrivateKey, vaultRootKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
			assert.Error(t, err, "vault root key must not decrypt content encrypted with node key")

			// 6. Verify a second fee key (different index) is also isolated
			feeKey2, err := w.DeriveFeeKey(wallet.ExternalChain, 1)
			require.NoError(t, err)
			assert.Equal(t, "m/44'/236'/0'/0/1", feeKey2.Path)
			assert.NotEqual(t, feePub, feeKey2.PublicKey.Compressed(),
				"different fee key indices should produce different keys")
		})
	}
}

// --- TestEmptyAndLargeContentCrypto ---

func TestEmptyAndLargeContentCrypto(t *testing.T) {
	w, _, _ := createTestWallet(t, &wallet.MainNet)
	nodeKey, err := w.DeriveNodeKey(0, []uint32{1}, nil)
	require.NoError(t, err)

	testCases := []struct {
		name      string
		plaintext []byte
	}{
		{"empty content", []byte{}},
		{"single byte", []byte{0x42}},
		{"small content", []byte("hello world")},
		{"1KB content", bytes.Repeat([]byte("x"), 1024)},
		{"100KB content", bytes.Repeat([]byte("large data block "), 6000)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encResult, err := method42.Encrypt(tc.plaintext, nodeKey.PrivateKey, nodeKey.PublicKey, method42.AccessPrivate)
			require.NoError(t, err)

			decResult, err := method42.Decrypt(encResult.Ciphertext, nodeKey.PrivateKey, nodeKey.PublicKey, encResult.KeyHash, method42.AccessPrivate)
			require.NoError(t, err)
			assert.Equal(t, tc.plaintext, decResult.Plaintext)
		})
	}
}

//go:build e2e

package e2e

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestVaultKeyIsolation verifies that different vaults derive completely
// different key hierarchies from the same wallet seed.
//
// Vault 0 maps to BIP44 account 1 (m/44'/236'/1'/0/0) and vault 1 maps to
// account 2 (m/44'/236'/2'/0/0). Their root keys must be distinct.
func TestVaultKeyIsolation(t *testing.T) {
	// Create a fresh wallet.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "derive seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	wState := wallet.NewWalletState()

	// Create two vaults.
	vault0, err := w.CreateVault(wState, "vault0")
	require.NoError(t, err, "create vault0")
	require.Equal(t, uint32(0), vault0.AccountIndex, "vault0 should have account index 0")

	vault1, err := w.CreateVault(wState, "vault1")
	require.NoError(t, err, "create vault1")
	require.Equal(t, uint32(1), vault1.AccountIndex, "vault1 should have account index 1")

	// Derive root keys for both vaults.
	kp0, err := w.DeriveNodeKey(vault0.AccountIndex, nil, nil)
	require.NoError(t, err, "derive vault0 root key")

	kp1, err := w.DeriveNodeKey(vault1.AccountIndex, nil, nil)
	require.NoError(t, err, "derive vault1 root key")

	t.Logf("vault0 path: %s", kp0.Path)
	t.Logf("vault1 path: %s", kp1.Path)

	// ------------------------------------------------------------------
	// Paths must be different.
	// ------------------------------------------------------------------
	assert.NotEqual(t, kp0.Path, kp1.Path,
		"vault root key derivation paths must differ")
	assert.Contains(t, kp0.Path, "1'/0/0",
		"vault0 path should contain account 1")
	assert.Contains(t, kp1.Path, "2'/0/0",
		"vault1 path should contain account 2")

	// ------------------------------------------------------------------
	// Compressed public keys must differ.
	// ------------------------------------------------------------------
	pub0 := kp0.PublicKey.Compressed()
	pub1 := kp1.PublicKey.Compressed()
	assert.False(t, bytes.Equal(pub0, pub1),
		"vault0 and vault1 root compressed pubkeys must be different")
	t.Logf("vault0 pubkey: %x", pub0[:8])
	t.Logf("vault1 pubkey: %x", pub1[:8])

	// ------------------------------------------------------------------
	// Private keys must differ.
	// ------------------------------------------------------------------
	priv0 := kp0.PrivateKey.Serialize()
	priv1 := kp1.PrivateKey.Serialize()
	assert.False(t, bytes.Equal(priv0, priv1),
		"vault0 and vault1 root private keys must be different")

	// ------------------------------------------------------------------
	// Child keys also differ (derive /0' under each vault root).
	// ------------------------------------------------------------------
	child0, err := w.DeriveNodeKey(vault0.AccountIndex, []uint32{0}, nil)
	require.NoError(t, err, "derive vault0 child key")

	child1, err := w.DeriveNodeKey(vault1.AccountIndex, []uint32{0}, nil)
	require.NoError(t, err, "derive vault1 child key")

	childPub0 := child0.PublicKey.Compressed()
	childPub1 := child1.PublicKey.Compressed()
	assert.False(t, bytes.Equal(childPub0, childPub1),
		"vault0 and vault1 child keys must differ")
	t.Logf("vault0 child[0] path: %s  pubkey: %x", child0.Path, childPub0[:8])
	t.Logf("vault1 child[0] path: %s  pubkey: %x", child1.Path, childPub1[:8])
}

// TestVaultEncryptionIsolation verifies that content encrypted with one
// vault's keys cannot be decrypted using another vault's keys.
//
// Method 42 uses ECDH(D_node, P_node) to derive the AES key. Since vault 0
// and vault 1 have different D_node and P_node, the ECDH shared secret is
// completely different, making cross-vault decryption impossible.
func TestVaultEncryptionIsolation(t *testing.T) {
	// Create a fresh wallet with two vaults.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "derive seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	wState := wallet.NewWalletState()

	vault0, err := w.CreateVault(wState, "vault0")
	require.NoError(t, err, "create vault0")

	vault1, err := w.CreateVault(wState, "vault1")
	require.NoError(t, err, "create vault1")

	// Derive root keys for both vaults.
	kp0, err := w.DeriveNodeKey(vault0.AccountIndex, nil, nil)
	require.NoError(t, err, "derive vault0 root key")

	kp1, err := w.DeriveNodeKey(vault1.AccountIndex, nil, nil)
	require.NoError(t, err, "derive vault1 root key")

	plaintext := []byte("Top secret vault0 content — must not be readable by vault1 keys.")

	// ------------------------------------------------------------------
	// Encrypt with vault0's key pair (Private mode = ECDH with real D_node).
	// ------------------------------------------------------------------
	enc0, err := method42.Encrypt(plaintext, kp0.PrivateKey, kp0.PublicKey, method42.AccessPrivate)
	require.NoError(t, err, "encrypt with vault0 key")
	require.NotEmpty(t, enc0.Ciphertext, "ciphertext should not be empty")
	require.Len(t, enc0.KeyHash, 32, "keyHash should be 32 bytes")
	t.Logf("Encrypted %d bytes with vault0 (Private), ciphertext=%d bytes",
		len(plaintext), len(enc0.Ciphertext))

	// ------------------------------------------------------------------
	// Vault0 owner can decrypt.
	// ------------------------------------------------------------------
	dec0, err := method42.Decrypt(
		enc0.Ciphertext,
		kp0.PrivateKey,
		kp0.PublicKey,
		enc0.KeyHash,
		method42.AccessPrivate,
	)
	require.NoError(t, err, "vault0 owner should decrypt own content")
	assert.Equal(t, plaintext, dec0.Plaintext,
		"decrypted content should match original plaintext")
	t.Logf("Vault0 owner decrypted OK: %d bytes", len(dec0.Plaintext))

	// ------------------------------------------------------------------
	// Vault1 key cannot decrypt vault0 content.
	// Cross-vault with vault1's private key + vault0's public key.
	// ECDH(D_vault1, P_vault0) != ECDH(D_vault0, P_vault0), so AES key
	// will be wrong and GCM authentication will fail.
	// ------------------------------------------------------------------
	_, err = method42.Decrypt(
		enc0.Ciphertext,
		kp1.PrivateKey,  // wrong private key (vault1)
		kp0.PublicKey,   // vault0's public key
		enc0.KeyHash,
		method42.AccessPrivate,
	)
	assert.Error(t, err,
		"vault1 private key + vault0 pubkey should fail to decrypt vault0 content")
	t.Logf("Cross-vault attempt 1 (D_vault1, P_vault0) correctly rejected: %v", err)

	// ------------------------------------------------------------------
	// Vault1's own key pair also cannot decrypt vault0 content.
	// ECDH(D_vault1, P_vault1) != ECDH(D_vault0, P_vault0).
	// ------------------------------------------------------------------
	_, err = method42.Decrypt(
		enc0.Ciphertext,
		kp1.PrivateKey,  // vault1 private key
		kp1.PublicKey,   // vault1 public key
		enc0.KeyHash,
		method42.AccessPrivate,
	)
	assert.Error(t, err,
		"vault1 own key pair should fail to decrypt vault0 content")
	t.Logf("Cross-vault attempt 2 (D_vault1, P_vault1) correctly rejected: %v", err)

	// ------------------------------------------------------------------
	// Vault0 private key + vault1 public key also cannot decrypt.
	// ECDH(D_vault0, P_vault1) != ECDH(D_vault0, P_vault0).
	// ------------------------------------------------------------------
	_, err = method42.Decrypt(
		enc0.Ciphertext,
		kp0.PrivateKey,  // vault0 private key
		kp1.PublicKey,   // wrong public key (vault1)
		enc0.KeyHash,
		method42.AccessPrivate,
	)
	assert.Error(t, err,
		"vault0 privkey + vault1 pubkey should fail to decrypt vault0 content")
	t.Logf("Cross-vault attempt 3 (D_vault0, P_vault1) correctly rejected: %v", err)

	// ------------------------------------------------------------------
	// Symmetric test: encrypt with vault1, verify vault0 cannot decrypt.
	// ------------------------------------------------------------------
	plaintext1 := []byte("Vault1 exclusive content — inaccessible to vault0.")
	enc1, err := method42.Encrypt(plaintext1, kp1.PrivateKey, kp1.PublicKey, method42.AccessPrivate)
	require.NoError(t, err, "encrypt with vault1 key")

	// Vault1 owner can decrypt.
	dec1, err := method42.Decrypt(
		enc1.Ciphertext,
		kp1.PrivateKey,
		kp1.PublicKey,
		enc1.KeyHash,
		method42.AccessPrivate,
	)
	require.NoError(t, err, "vault1 owner should decrypt own content")
	assert.Equal(t, plaintext1, dec1.Plaintext)
	t.Logf("Vault1 owner decrypted OK: %d bytes", len(dec1.Plaintext))

	// Vault0 cannot decrypt vault1 content.
	_, err = method42.Decrypt(
		enc1.Ciphertext,
		kp0.PrivateKey,
		kp1.PublicKey,
		enc1.KeyHash,
		method42.AccessPrivate,
	)
	assert.Error(t, err,
		"vault0 should fail to decrypt vault1 content")
	t.Logf("Reverse cross-vault (D_vault0, P_vault1) correctly rejected: %v", err)

}

// TestVaultEncryptionIsolationPaid verifies cross-vault isolation under
// AccessPaid mode. Since Paid uses the same ECDH as Private (D_node rather
// than scalar 1), isolation properties are identical.
func TestVaultEncryptionIsolationPaid(t *testing.T) {
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "derive seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	wState := wallet.NewWalletState()

	vault0, err := w.CreateVault(wState, "vault0")
	require.NoError(t, err, "create vault0")

	vault1, err := w.CreateVault(wState, "vault1")
	require.NoError(t, err, "create vault1")

	kp0, err := w.DeriveNodeKey(vault0.AccountIndex, nil, nil)
	require.NoError(t, err, "derive vault0 root key")

	kp1, err := w.DeriveNodeKey(vault1.AccountIndex, nil, nil)
	require.NoError(t, err, "derive vault1 root key")

	plaintext := []byte("Paid content in vault0 — vault1 has no access.")

	// Encrypt with Paid mode (same ECDH as Private).
	enc, err := method42.Encrypt(plaintext, kp0.PrivateKey, kp0.PublicKey, method42.AccessPaid)
	require.NoError(t, err, "encrypt as Paid with vault0")

	// Vault0 owner decrypts successfully.
	dec, err := method42.Decrypt(
		enc.Ciphertext,
		kp0.PrivateKey,
		kp0.PublicKey,
		enc.KeyHash,
		method42.AccessPaid,
	)
	require.NoError(t, err, "vault0 owner should decrypt Paid content")
	assert.Equal(t, plaintext, dec.Plaintext)

	// Vault1 cannot decrypt.
	_, err = method42.Decrypt(
		enc.Ciphertext,
		kp1.PrivateKey,
		kp0.PublicKey,
		enc.KeyHash,
		method42.AccessPaid,
	)
	assert.Error(t, err, "vault1 should fail to decrypt vault0 Paid content")
	t.Logf("Paid-mode cross-vault isolation confirmed")
}

// TestVaultDeepKeyIsolation verifies that isolation holds at deeper
// filesystem paths (not just root keys). Two vaults deriving keys for
// the same relative path (e.g., /0/1/2) must produce different keys.
func TestVaultDeepKeyIsolation(t *testing.T) {
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "derive seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	wState := wallet.NewWalletState()

	vault0, err := w.CreateVault(wState, "vault0")
	require.NoError(t, err, "create vault0")

	vault1, err := w.CreateVault(wState, "vault1")
	require.NoError(t, err, "create vault1")

	// Use the same relative path in both vaults: /0/1/2
	filePath := []uint32{0, 1, 2}

	deep0, err := w.DeriveNodeKey(vault0.AccountIndex, filePath, nil)
	require.NoError(t, err, "derive deep key for vault0")

	deep1, err := w.DeriveNodeKey(vault1.AccountIndex, filePath, nil)
	require.NoError(t, err, "derive deep key for vault1")

	// Public keys must differ.
	pub0 := deep0.PublicKey.Compressed()
	pub1 := deep1.PublicKey.Compressed()
	assert.False(t, bytes.Equal(pub0, pub1),
		"deep keys at same relative path must differ across vaults")
	t.Logf("vault0 deep path: %s  pubkey: %x", deep0.Path, pub0[:8])
	t.Logf("vault1 deep path: %s  pubkey: %x", deep1.Path, pub1[:8])

	// Paths should differ in account number only.
	assert.NotEqual(t, deep0.Path, deep1.Path,
		"derivation paths must differ")

	// Encrypt with vault0 deep key, verify vault1 deep key cannot decrypt.
	plaintext := []byte("Deep node content: /0/1/2 in vault0.")
	enc, err := method42.Encrypt(plaintext, deep0.PrivateKey, deep0.PublicKey, method42.AccessPrivate)
	require.NoError(t, err, "encrypt with vault0 deep key")

	_, err = method42.Decrypt(
		enc.Ciphertext,
		deep1.PrivateKey,
		deep0.PublicKey,
		enc.KeyHash,
		method42.AccessPrivate,
	)
	assert.Error(t, err,
		"vault1 deep key should fail to decrypt vault0 deep content")
	t.Logf("Deep key cross-vault isolation confirmed for path %v", filePath)
}

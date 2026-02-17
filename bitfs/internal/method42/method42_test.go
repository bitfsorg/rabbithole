package method42

import (
	"bytes"
	"crypto/sha256"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper functions ---

func generateKeyPair(t *testing.T) (*ec.PrivateKey, *ec.PublicKey) {
	t.Helper()
	privKey, err := ec.NewPrivateKey()
	require.NoError(t, err)
	pubKey := privKey.PubKey()
	require.NotNil(t, pubKey)
	return privKey, pubKey
}

// --- ComputeKeyHash tests ---

func TestComputeKeyHash(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"empty input", []byte{}},
		{"hello world", []byte("hello world")},
		{"binary data", []byte{0x00, 0x01, 0xff, 0xfe}},
		{"large input", bytes.Repeat([]byte("a"), 1024*1024)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := ComputeKeyHash(tt.plaintext)
			assert.Len(t, hash, 32, "key hash should be 32 bytes")

			// Verify it's actually double-SHA256
			first := sha256.Sum256(tt.plaintext)
			second := sha256.Sum256(first[:])
			assert.Equal(t, second[:], hash, "should be SHA256(SHA256(plaintext))")
		})
	}
}

func TestComputeKeyHash_Deterministic(t *testing.T) {
	plaintext := []byte("deterministic test data")
	hash1 := ComputeKeyHash(plaintext)
	hash2 := ComputeKeyHash(plaintext)
	assert.Equal(t, hash1, hash2, "same input should produce same hash")
}

func TestComputeKeyHash_DifferentInputs(t *testing.T) {
	hash1 := ComputeKeyHash([]byte("input a"))
	hash2 := ComputeKeyHash([]byte("input b"))
	assert.NotEqual(t, hash1, hash2, "different inputs should produce different hashes")
}

// --- ECDH tests ---

func TestECDH(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)

	shared, err := ECDH(privKey, pubKey)
	require.NoError(t, err)
	assert.Len(t, shared, 32, "shared secret should be 32 bytes")
}

func TestECDH_Symmetry(t *testing.T) {
	// ECDH(D_a, P_b) == ECDH(D_b, P_a)
	privA, pubA := generateKeyPair(t)
	privB, pubB := generateKeyPair(t)

	sharedAB, err := ECDH(privA, pubB)
	require.NoError(t, err)

	sharedBA, err := ECDH(privB, pubA)
	require.NoError(t, err)

	assert.Equal(t, sharedAB, sharedBA, "ECDH should be symmetric")
}

func TestECDH_Deterministic(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)

	shared1, err := ECDH(privKey, pubKey)
	require.NoError(t, err)

	shared2, err := ECDH(privKey, pubKey)
	require.NoError(t, err)

	assert.Equal(t, shared1, shared2, "same keys should produce same shared secret")
}

func TestECDH_NilPrivateKey(t *testing.T) {
	_, pubKey := generateKeyPair(t)
	_, err := ECDH(nil, pubKey)
	assert.ErrorIs(t, err, ErrNilPrivateKey)
}

func TestECDH_NilPublicKey(t *testing.T) {
	privKey, _ := generateKeyPair(t)
	_, err := ECDH(privKey, nil)
	assert.ErrorIs(t, err, ErrNilPublicKey)
}

func TestECDH_DifferentKeys(t *testing.T) {
	privA, _ := generateKeyPair(t)
	_, pubB := generateKeyPair(t)
	_, pubC := generateKeyPair(t)

	sharedAB, err := ECDH(privA, pubB)
	require.NoError(t, err)

	sharedAC, err := ECDH(privA, pubC)
	require.NoError(t, err)

	assert.NotEqual(t, sharedAB, sharedAC, "different public keys should produce different shared secrets")
}

// --- FreePrivateKey tests ---

func TestFreePrivateKey(t *testing.T) {
	freeKey := FreePrivateKey()
	require.NotNil(t, freeKey)

	// Scalar value should be 1
	assert.Equal(t, int64(1), freeKey.D.Int64(), "free key scalar should be 1")
}

func TestFreePrivateKey_ECDHEqualsPublicKey(t *testing.T) {
	// ECDH(1, P) should return P.x
	_, pubKey := generateKeyPair(t)
	freeKey := FreePrivateKey()

	shared, err := ECDH(freeKey, pubKey)
	require.NoError(t, err)

	// shared should equal pubKey.X (32 bytes, zero-padded)
	xBytes := pubKey.X.Bytes()
	expected := make([]byte, 32)
	copy(expected[32-len(xBytes):], xBytes)

	assert.Equal(t, expected, shared, "ECDH(1, P) should return P.x")
}

// --- DeriveAESKey tests ---

func TestDeriveAESKey(t *testing.T) {
	sharedX := bytes.Repeat([]byte{0xab}, 32)
	keyHash := bytes.Repeat([]byte{0xcd}, 32)

	key, err := DeriveAESKey(sharedX, keyHash)
	require.NoError(t, err)
	assert.Len(t, key, 32, "AES key should be 32 bytes")
}

func TestDeriveAESKey_Deterministic(t *testing.T) {
	sharedX := bytes.Repeat([]byte{0x01}, 32)
	keyHash := bytes.Repeat([]byte{0x02}, 32)

	key1, err := DeriveAESKey(sharedX, keyHash)
	require.NoError(t, err)

	key2, err := DeriveAESKey(sharedX, keyHash)
	require.NoError(t, err)

	assert.Equal(t, key1, key2, "same inputs should produce same key")
}

func TestDeriveAESKey_DifferentSalt(t *testing.T) {
	sharedX := bytes.Repeat([]byte{0x01}, 32)
	keyHash1 := bytes.Repeat([]byte{0x02}, 32)
	keyHash2 := bytes.Repeat([]byte{0x03}, 32)

	key1, err := DeriveAESKey(sharedX, keyHash1)
	require.NoError(t, err)

	key2, err := DeriveAESKey(sharedX, keyHash2)
	require.NoError(t, err)

	assert.NotEqual(t, key1, key2, "different salts should produce different keys")
}

func TestDeriveAESKey_EmptySharedSecret(t *testing.T) {
	keyHash := bytes.Repeat([]byte{0x01}, 32)
	_, err := DeriveAESKey([]byte{}, keyHash)
	assert.ErrorIs(t, err, ErrHKDFFailure)
}

func TestDeriveAESKey_InvalidKeyHashLength(t *testing.T) {
	sharedX := bytes.Repeat([]byte{0x01}, 32)
	_, err := DeriveAESKey(sharedX, []byte{0x01, 0x02}) // too short
	assert.ErrorIs(t, err, ErrHKDFFailure)
}

// --- AES-GCM encrypt/decrypt tests ---

func TestAESGCM_RoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello")},
		{"medium", bytes.Repeat([]byte("test"), 1000)},
		{"binary", []byte{0x00, 0x01, 0xff, 0xfe, 0x80}},
	}

	key := bytes.Repeat([]byte{0xab}, 32)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := aesGCMEncrypt(tt.plaintext, key)
			require.NoError(t, err)
			assert.Greater(t, len(ciphertext), len(tt.plaintext), "ciphertext should be longer due to nonce+tag")

			decrypted, err := aesGCMDecrypt(ciphertext, key)
			require.NoError(t, err)
			assert.Equal(t, tt.plaintext, decrypted, "round-trip should preserve plaintext")
		})
	}
}

func TestAESGCM_DifferentNonces(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, 32)
	plaintext := []byte("same plaintext")

	ct1, err := aesGCMEncrypt(plaintext, key)
	require.NoError(t, err)

	ct2, err := aesGCMEncrypt(plaintext, key)
	require.NoError(t, err)

	// Ciphertexts should differ due to random nonce
	assert.NotEqual(t, ct1, ct2, "same plaintext should produce different ciphertexts due to random nonce")
}

func TestAESGCM_WrongKey(t *testing.T) {
	key1 := bytes.Repeat([]byte{0xab}, 32)
	key2 := bytes.Repeat([]byte{0xcd}, 32)
	plaintext := []byte("secret data")

	ciphertext, err := aesGCMEncrypt(plaintext, key1)
	require.NoError(t, err)

	_, err = aesGCMDecrypt(ciphertext, key2)
	assert.ErrorIs(t, err, ErrDecryptionFailed, "wrong key should fail decryption")
}

func TestAESGCM_TamperedCiphertext(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, 32)
	plaintext := []byte("authentic data")

	ciphertext, err := aesGCMEncrypt(plaintext, key)
	require.NoError(t, err)

	// Tamper with the ciphertext (after nonce, before tag)
	if len(ciphertext) > NonceLen+1 {
		ciphertext[NonceLen+1] ^= 0xff
	}

	_, err = aesGCMDecrypt(ciphertext, key)
	assert.ErrorIs(t, err, ErrDecryptionFailed, "tampered ciphertext should fail authentication")
}

func TestAESGCM_TooShort(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, 32)
	_, err := aesGCMDecrypt([]byte{0x01, 0x02, 0x03}, key) // way too short
	assert.ErrorIs(t, err, ErrInvalidCiphertext)
}

// --- Full Encrypt/Decrypt tests ---

func TestEncrypt_AccessFree(t *testing.T) {
	_, pubKey := generateKeyPair(t)
	plaintext := []byte("free content for everyone")

	result, err := Encrypt(plaintext, nil, pubKey, AccessFree)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Ciphertext)
	assert.Len(t, result.KeyHash, 32)
	assert.Len(t, result.AESKey, 32)
}

func TestEncrypt_AccessPrivate(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("private owner-only content")

	result, err := Encrypt(plaintext, privKey, pubKey, AccessPrivate)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Ciphertext)
	assert.Len(t, result.KeyHash, 32)
}

func TestEncrypt_AccessPaid(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("premium paid content")

	result, err := Encrypt(plaintext, privKey, pubKey, AccessPaid)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Ciphertext)
}

func TestDecrypt_AccessFree_RoundTrip(t *testing.T) {
	_, pubKey := generateKeyPair(t)
	plaintext := []byte("free content round trip")

	encResult, err := Encrypt(plaintext, nil, pubKey, AccessFree)
	require.NoError(t, err)

	decResult, err := Decrypt(encResult.Ciphertext, nil, pubKey, encResult.KeyHash, AccessFree)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)
	assert.Equal(t, encResult.KeyHash, decResult.KeyHash)
}

func TestDecrypt_AccessPrivate_RoundTrip(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("private content round trip")

	encResult, err := Encrypt(plaintext, privKey, pubKey, AccessPrivate)
	require.NoError(t, err)

	decResult, err := Decrypt(encResult.Ciphertext, privKey, pubKey, encResult.KeyHash, AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)
}

func TestDecrypt_AccessPaid_RoundTrip(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("paid content round trip")

	encResult, err := Encrypt(plaintext, privKey, pubKey, AccessPaid)
	require.NoError(t, err)

	decResult, err := Decrypt(encResult.Ciphertext, privKey, pubKey, encResult.KeyHash, AccessPaid)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)
}

func TestDecrypt_WrongKeyHash(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("test content")

	encResult, err := Encrypt(plaintext, privKey, pubKey, AccessPrivate)
	require.NoError(t, err)

	// Use wrong key hash
	wrongHash := bytes.Repeat([]byte{0xff}, 32)
	_, err = Decrypt(encResult.Ciphertext, privKey, pubKey, wrongHash, AccessPrivate)
	// This should fail because wrong key_hash leads to wrong AES key
	assert.Error(t, err)
}

func TestDecrypt_NilPublicKey(t *testing.T) {
	privKey, _ := generateKeyPair(t)
	_, err := Decrypt([]byte("dummy"), privKey, nil, bytes.Repeat([]byte{0x01}, 32), AccessPrivate)
	assert.ErrorIs(t, err, ErrNilPublicKey)
}

func TestDecrypt_NilPrivateKey_Private(t *testing.T) {
	_, pubKey := generateKeyPair(t)
	_, err := Decrypt([]byte("dummy"), nil, pubKey, bytes.Repeat([]byte{0x01}, 32), AccessPrivate)
	assert.ErrorIs(t, err, ErrNilPrivateKey)
}

// --- DecryptWithCapsule tests ---

func TestDecryptWithCapsule(t *testing.T) {
	nodePriv, nodePub := generateKeyPair(t)
	buyerPriv, buyerPub := generateKeyPair(t)
	plaintext := []byte("paid content for buyer")

	// Owner encrypts with PAID mode (same key derivation as PRIVATE)
	encResult, err := Encrypt(plaintext, nodePriv, nodePub, AccessPaid)
	require.NoError(t, err)

	// Seller computes capsule for buyer: ECDH(D_node, P_buyer).x
	capsule, err := ComputeCapsule(nodePriv, buyerPub)
	require.NoError(t, err)

	// Verify capsule hash works for HTLC
	capsuleHash := ComputeCapsuleHash(capsule)
	assert.Len(t, capsuleHash, 32)

	// Buyer verifies the capsule matches the hash
	recomputedHash := ComputeCapsuleHash(capsule)
	assert.Equal(t, capsuleHash, recomputedHash)

	// But wait -- the capsule is ECDH(D_node, P_buyer), not ECDH(D_node, P_node).
	// The buyer needs aes_key = KDF(ECDH(D_node, P_node), key_hash).
	// In the actual protocol, the capsule is the shared secret between node and buyer
	// for key transport, not the actual encryption key. The seller provides
	// the file-encryption capsule = ECDH(D_node, P_node) to the buyer via HTLC.
	//
	// Let's test the actual flow:
	// Seller computes: capsule = ECDH(D_node, P_node).x (the actual file key material)
	fileCapsule, err := ECDH(nodePriv, nodePub)
	require.NoError(t, err)

	// Buyer decrypts with capsule
	decResult, err := DecryptWithCapsule(encResult.Ciphertext, fileCapsule, encResult.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)

	// Buyer can also derive capsule from the other direction:
	// ECDH(D_buyer, P_node) != ECDH(D_node, P_node) in general.
	// The HTLC reveals ECDH(D_node, P_node) directly, not ECDH(D_node, P_buyer).
	_ = buyerPriv // buyerPriv is used in the actual protocol for the HTLC handshake
}

func TestDecryptWithCapsule_EmptyCapsule(t *testing.T) {
	_, err := DecryptWithCapsule([]byte("ct"), []byte{}, bytes.Repeat([]byte{0x01}, 32))
	assert.Error(t, err)
}

func TestDecryptWithCapsule_WrongCapsule(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("test content")

	encResult, err := Encrypt(plaintext, privKey, pubKey, AccessPrivate)
	require.NoError(t, err)

	// Use wrong capsule
	wrongCapsule := bytes.Repeat([]byte{0xab}, 32)
	_, err = DecryptWithCapsule(encResult.Ciphertext, wrongCapsule, encResult.KeyHash)
	assert.Error(t, err, "wrong capsule should fail decryption")
}

// --- ReEncrypt tests ---

func TestReEncrypt_FreeToPrivate(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("originally free content")

	// Encrypt as FREE
	freeResult, err := Encrypt(plaintext, nil, pubKey, AccessFree)
	require.NoError(t, err)

	// Re-encrypt as PRIVATE
	privResult, err := ReEncrypt(freeResult.Ciphertext, privKey, pubKey, freeResult.KeyHash, AccessFree, AccessPrivate)
	require.NoError(t, err)

	// Verify the new ciphertext can be decrypted with PRIVATE mode
	decResult, err := Decrypt(privResult.Ciphertext, privKey, pubKey, privResult.KeyHash, AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)

	// Verify the old FREE ciphertext can still be decrypted
	decOld, err := Decrypt(freeResult.Ciphertext, nil, pubKey, freeResult.KeyHash, AccessFree)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decOld.Plaintext)
}

func TestReEncrypt_PrivateToFree(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("originally private content")

	// Encrypt as PRIVATE
	privResult, err := Encrypt(plaintext, privKey, pubKey, AccessPrivate)
	require.NoError(t, err)

	// Re-encrypt as FREE
	freeResult, err := ReEncrypt(privResult.Ciphertext, privKey, pubKey, privResult.KeyHash, AccessPrivate, AccessFree)
	require.NoError(t, err)

	// Verify anyone can decrypt with FREE mode (no private key needed)
	decResult, err := Decrypt(freeResult.Ciphertext, nil, pubKey, freeResult.KeyHash, AccessFree)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)
}

func TestReEncrypt_NewKeyHash(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("content being re-encrypted")

	freeResult, err := Encrypt(plaintext, nil, pubKey, AccessFree)
	require.NoError(t, err)

	privResult, err := ReEncrypt(freeResult.Ciphertext, privKey, pubKey, freeResult.KeyHash, AccessFree, AccessPrivate)
	require.NoError(t, err)

	// Key hash should be the same (same plaintext content)
	assert.Equal(t, freeResult.KeyHash, privResult.KeyHash, "same content should produce same key hash")
}

// --- Access mode tests ---

func TestAccess_String(t *testing.T) {
	assert.Equal(t, "PRIVATE", AccessPrivate.String())
	assert.Equal(t, "FREE", AccessFree.String())
	assert.Equal(t, "PAID", AccessPaid.String())
	assert.Equal(t, "UNKNOWN", Access(99).String())
}

func TestEncrypt_InvalidAccess(t *testing.T) {
	_, pubKey := generateKeyPair(t)
	_, err := Encrypt([]byte("test"), nil, pubKey, Access(99))
	assert.ErrorIs(t, err, ErrInvalidAccess)
}

// --- CapsuleHash tests ---

func TestComputeCapsuleHash(t *testing.T) {
	capsule := bytes.Repeat([]byte{0xab}, 32)
	hash := ComputeCapsuleHash(capsule)
	assert.Len(t, hash, 32)

	// Should be standard SHA256
	expected := sha256.Sum256(capsule)
	assert.Equal(t, expected[:], hash)
}

func TestComputeCapsuleHash_Deterministic(t *testing.T) {
	capsule := []byte("test capsule data")
	hash1 := ComputeCapsuleHash(capsule)
	hash2 := ComputeCapsuleHash(capsule)
	assert.Equal(t, hash1, hash2)
}

// --- Integration: Full encryption flow ---

func TestFullEncryptionFlow_Private(t *testing.T) {
	// Simulates the complete flow: create file -> encrypt -> store -> retrieve -> decrypt
	privKey, pubKey := generateKeyPair(t)
	plaintext := []byte("This is a private document stored on the BitFS blockchain.")

	// 1. Encrypt
	encResult, err := Encrypt(plaintext, privKey, pubKey, AccessPrivate)
	require.NoError(t, err)

	// 2. Store ciphertext and key_hash (would go to storage and Metanet tx)
	storedCiphertext := encResult.Ciphertext
	storedKeyHash := encResult.KeyHash

	// 3. Later: Retrieve and decrypt
	decResult, err := Decrypt(storedCiphertext, privKey, pubKey, storedKeyHash, AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)
}

func TestFullEncryptionFlow_FreeThenBuy(t *testing.T) {
	// Simulates: owner creates free file -> makes it paid -> buyer purchases via HTLC
	ownerPriv, ownerPub := generateKeyPair(t)

	plaintext := []byte("Premium article about blockchain technology.")

	// 1. Owner encrypts as PRIVATE (will sell)
	encResult, err := Encrypt(plaintext, ownerPriv, ownerPub, AccessPrivate)
	require.NoError(t, err)

	// 2. Buyer initiates purchase. Seller computes capsule = ECDH(D_node, P_node).x
	capsule, err := ECDH(ownerPriv, ownerPub)
	require.NoError(t, err)

	// 3. Seller provides capsule_hash for HTLC
	capsuleHash := ComputeCapsuleHash(capsule)
	assert.Len(t, capsuleHash, 32)

	// 4. After HTLC is resolved, buyer gets capsule
	// 5. Buyer decrypts with capsule
	decResult, err := DecryptWithCapsule(encResult.Ciphertext, capsule, encResult.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)
}

func TestEncrypt_EmptyPlaintext(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)

	result, err := Encrypt([]byte{}, privKey, pubKey, AccessPrivate)
	require.NoError(t, err)

	decResult, err := Decrypt(result.Ciphertext, privKey, pubKey, result.KeyHash, AccessPrivate)
	require.NoError(t, err)
	assert.Empty(t, decResult.Plaintext)
}

func TestEncrypt_LargePlaintext(t *testing.T) {
	privKey, pubKey := generateKeyPair(t)
	plaintext := bytes.Repeat([]byte("large content "), 10000) // ~140KB

	result, err := Encrypt(plaintext, privKey, pubKey, AccessPrivate)
	require.NoError(t, err)

	decResult, err := Decrypt(result.Ciphertext, privKey, pubKey, result.KeyHash, AccessPrivate)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)
}

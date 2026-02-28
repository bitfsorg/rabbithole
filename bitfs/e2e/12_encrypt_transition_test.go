//go:build e2e

package e2e

import (
	"bytes"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/tx"
)

// TestFreeToPrivateTransition verifies the full Free -> Private encryption
// transition workflow:
//  1. Encrypt content as Free, verify FreePrivateKey (scalar 1) decrypts it.
//  2. Re-encrypt as Private (same node key), verify FreePrivateKey can no
//     longer decrypt, and only the owner key succeeds.
func TestFreeToPrivateTransition(t *testing.T) {
	// Generate a fresh node key pair.
	privKey, err := ec.NewPrivateKey()
	require.NoError(t, err, "generate node private key")
	pubKey := privKey.PubKey()

	plaintext := []byte("Confidential document: initially Free, then upgraded to Private.")

	// ------------------------------------------------------------------
	// Phase 1: Encrypt as AccessFree.
	// ------------------------------------------------------------------
	freeEnc, err := method42.Encrypt(plaintext, privKey, pubKey, method42.AccessFree)
	require.NoError(t, err, "encrypt as AccessFree")
	require.NotEmpty(t, freeEnc.Ciphertext, "free ciphertext should not be empty")
	require.Len(t, freeEnc.KeyHash, 32, "free keyHash should be 32 bytes")

	// Anyone can decrypt Free content using FreePrivateKey (scalar 1).
	freeKey := method42.FreePrivateKey()
	freeDec, err := method42.Decrypt(
		freeEnc.Ciphertext,
		freeKey,
		pubKey,
		freeEnc.KeyHash,
		method42.AccessFree,
	)
	require.NoError(t, err, "decrypt Free content with FreePrivateKey")
	assert.Equal(t, plaintext, freeDec.Plaintext,
		"decrypted Free content should match original plaintext")
	t.Logf("Phase 1 OK: Free encryption/decryption round-trip verified (%d bytes)", len(plaintext))

	// ------------------------------------------------------------------
	// Phase 2: Re-encrypt as AccessPrivate (same node key).
	// ------------------------------------------------------------------
	privEnc, err := method42.ReEncrypt(
		freeEnc.Ciphertext,
		privKey,
		pubKey,
		freeEnc.KeyHash,
		method42.AccessFree,    // from
		method42.AccessPrivate, // to
	)
	require.NoError(t, err, "re-encrypt from Free to Private")
	require.NotEmpty(t, privEnc.Ciphertext, "private ciphertext should not be empty")
	require.Len(t, privEnc.KeyHash, 32, "private keyHash should be 32 bytes")

	// Ciphertext must differ (different ECDH shared secret + random nonce).
	assert.False(t, bytes.Equal(freeEnc.Ciphertext, privEnc.Ciphertext),
		"Private ciphertext should differ from Free ciphertext")

	// FreePrivateKey must NOT decrypt Private-mode content.
	_, err = method42.Decrypt(
		privEnc.Ciphertext,
		freeKey,
		pubKey,
		privEnc.KeyHash,
		method42.AccessFree,
	)
	assert.Error(t, err, "FreePrivateKey should fail to decrypt Private content")
	t.Logf("Phase 2a: FreePrivateKey correctly rejected for Private content")

	// Owner key (real D_node) must succeed.
	ownerDec, err := method42.Decrypt(
		privEnc.Ciphertext,
		privKey,
		pubKey,
		privEnc.KeyHash,
		method42.AccessPrivate,
	)
	require.NoError(t, err, "owner should decrypt Private content")
	assert.Equal(t, plaintext, ownerDec.Plaintext,
		"owner-decrypted Private content should match original plaintext")
	t.Logf("Phase 2b OK: Owner successfully decrypted Private content")
}

// TestPrivateContentOwnerAccess verifies that content encrypted directly as
// Private can only be decrypted by the owner key (D_node), and that
// FreePrivateKey (scalar 1) and random third-party keys both fail.
func TestPrivateContentOwnerAccess(t *testing.T) {
	// Generate owner key pair.
	ownerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err, "generate owner private key")
	ownerPub := ownerPriv.PubKey()

	plaintext := []byte("Top secret: only the owner can read this content.")

	// Encrypt directly as Private.
	enc, err := method42.Encrypt(plaintext, ownerPriv, ownerPub, method42.AccessPrivate)
	require.NoError(t, err, "encrypt as AccessPrivate")
	require.NotEmpty(t, enc.Ciphertext)
	require.Len(t, enc.KeyHash, 32)
	t.Logf("Encrypted %d bytes as Private, ciphertext=%d bytes", len(plaintext), len(enc.Ciphertext))

	// ------------------------------------------------------------------
	// Owner decrypts successfully.
	// ------------------------------------------------------------------
	ownerDec, err := method42.Decrypt(
		enc.Ciphertext,
		ownerPriv,
		ownerPub,
		enc.KeyHash,
		method42.AccessPrivate,
	)
	require.NoError(t, err, "owner should decrypt Private content")
	assert.Equal(t, plaintext, ownerDec.Plaintext)
	t.Logf("Owner decrypt OK: %d bytes recovered", len(ownerDec.Plaintext))

	// ------------------------------------------------------------------
	// FreePrivateKey (scalar 1) must fail.
	// ------------------------------------------------------------------
	freeKey := method42.FreePrivateKey()
	_, err = method42.Decrypt(
		enc.Ciphertext,
		freeKey,
		ownerPub,
		enc.KeyHash,
		method42.AccessFree,
	)
	assert.Error(t, err, "FreePrivateKey should not decrypt Private content")
	t.Logf("FreePrivateKey correctly rejected")

	// ------------------------------------------------------------------
	// Random third-party key must fail.
	// ------------------------------------------------------------------
	thirdPartyPriv, err := ec.NewPrivateKey()
	require.NoError(t, err, "generate third-party private key")

	_, err = method42.Decrypt(
		enc.Ciphertext,
		thirdPartyPriv,
		ownerPub,
		enc.KeyHash,
		method42.AccessPrivate,
	)
	assert.Error(t, err, "third-party key should not decrypt Private content")
	t.Logf("Third-party key correctly rejected")

	// ------------------------------------------------------------------
	// AccessPaid with owner key should also work (same ECDH as Private).
	// ------------------------------------------------------------------
	paidDec, err := method42.Decrypt(
		enc.Ciphertext,
		ownerPriv,
		ownerPub,
		enc.KeyHash,
		method42.AccessPaid,
	)
	require.NoError(t, err, "AccessPaid with owner key should decrypt (same ECDH as Private)")
	assert.Equal(t, plaintext, paidDec.Plaintext,
		"Paid-mode decrypt with owner key should match plaintext")
	t.Logf("AccessPaid with owner key OK (same ECDH as AccessPrivate)")
}

// TestAccessModePreservedInPayload verifies that OP_RETURN payloads built for
// Free vs Private encryption carry different content, reflecting the different
// ECDH inputs used by each access mode.
//
// Specifically:
//   - The keyHash is the same (it's SHA256(SHA256(plaintext)), independent of access mode).
//   - The ciphertext must differ (different AES key due to different ECDH shared secret,
//     plus each encryption uses a fresh random nonce).
//   - Therefore the overall OP_RETURN payload (keyHash || ciphertext) must differ.
func TestAccessModePreservedInPayload(t *testing.T) {
	// Generate node key pair.
	privKey, err := ec.NewPrivateKey()
	require.NoError(t, err, "generate node private key")
	pubKey := privKey.PubKey()

	plaintext := []byte("Payload comparison test content for Free vs Private.")

	// ------------------------------------------------------------------
	// Encrypt under both modes.
	// ------------------------------------------------------------------
	freeEnc, err := method42.Encrypt(plaintext, privKey, pubKey, method42.AccessFree)
	require.NoError(t, err, "encrypt as Free")

	privEnc, err := method42.Encrypt(plaintext, privKey, pubKey, method42.AccessPrivate)
	require.NoError(t, err, "encrypt as Private")

	// ------------------------------------------------------------------
	// keyHash should be identical (content-derived, mode-independent).
	// ------------------------------------------------------------------
	assert.Equal(t, freeEnc.KeyHash, privEnc.KeyHash,
		"keyHash should be identical for same plaintext regardless of access mode")
	t.Logf("keyHash matches: %x", freeEnc.KeyHash[:8])

	// ------------------------------------------------------------------
	// Ciphertext must differ (different ECDH + random nonce).
	// ------------------------------------------------------------------
	assert.False(t, bytes.Equal(freeEnc.Ciphertext, privEnc.Ciphertext),
		"ciphertext should differ between Free and Private modes")

	// ------------------------------------------------------------------
	// Build OP_RETURN payloads (keyHash(32B) + ciphertext) as would be
	// embedded in a Metanet transaction.
	// ------------------------------------------------------------------
	freePayload := make([]byte, 0, 32+len(freeEnc.Ciphertext))
	freePayload = append(freePayload, freeEnc.KeyHash...)
	freePayload = append(freePayload, freeEnc.Ciphertext...)

	privPayload := make([]byte, 0, 32+len(privEnc.Ciphertext))
	privPayload = append(privPayload, privEnc.KeyHash...)
	privPayload = append(privPayload, privEnc.Ciphertext...)

	// Overall payloads must differ (same keyHash prefix, different ciphertext suffix).
	assert.False(t, bytes.Equal(freePayload, privPayload),
		"OP_RETURN payloads should differ between Free and Private modes")

	// First 32 bytes (keyHash) should match.
	assert.True(t, bytes.Equal(freePayload[:32], privPayload[:32]),
		"first 32 bytes (keyHash) of payloads should be identical")

	// Remaining bytes (ciphertext) should differ.
	assert.False(t, bytes.Equal(freePayload[32:], privPayload[32:]),
		"ciphertext portion of payloads should differ")

	// ------------------------------------------------------------------
	// Build full OP_RETURN push data using tx.BuildOPReturnData.
	// ------------------------------------------------------------------
	parentTxID := make([]byte, 32) // dummy parent (zeros)

	freePushes, err := tx.BuildOPReturnData(pubKey, parentTxID, freePayload)
	require.NoError(t, err, "build OP_RETURN pushes for Free payload")

	privPushes, err := tx.BuildOPReturnData(pubKey, parentTxID, privPayload)
	require.NoError(t, err, "build OP_RETURN pushes for Private payload")

	// MetaFlag and P_node should be identical.
	assert.Equal(t, freePushes[0], privPushes[0], "MetaFlag should match")
	assert.Equal(t, freePushes[1], privPushes[1], "P_node should match")
	assert.Equal(t, freePushes[2], privPushes[2], "parentTxID should match")

	// Payload push (index 3) should differ.
	assert.False(t, bytes.Equal(freePushes[3], privPushes[3]),
		"payload push in OP_RETURN should differ between Free and Private")

	t.Logf("OP_RETURN payload comparison verified: identical metadata, different encrypted content")
	t.Logf("Free payload: %d bytes, Private payload: %d bytes", len(freePayload), len(privPayload))
}

// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package proof

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
)

const (
	// DefaultChunkSize is the standard chunk size for Merkle tree construction.
	// 256 KB = 262,144 bytes.
	DefaultChunkSize = 256 * 1024

	// KDFContext is the context string for ECDH key derivation.
	KDFContext = "storage"
)

// DoubleEncryptedData holds the result of ECDH double-layer encryption
// for a specific Metanet Node.
type DoubleEncryptedData struct {
	Ciphertext []byte   // Double-encrypted data.
	Nonce      [12]byte // AES-256-GCM nonce.
	MerkleRoot [32]byte // Merkle root of the chunked ciphertext.
	NumChunks  uint32   // Number of chunks.
	ChunkSize  uint32   // Size of each chunk in bytes.
}

// DeriveNodeKey performs HMAC-based key derivation for a specific Metanet Node.
//
// Since this project does not use go-sdk (no secp256k1 elliptic curve),
// we simulate ECDH with an HMAC-based shared secret:
//
//	shared_secret = HMAC-SHA256(owner_priv_key, node_pub_key)
//	node_key = HKDF-SHA256(shared_secret, nil, "storage", 32)
//
// This produces a unique 32-byte AES-256-GCM key per (owner, node) pair.
func DeriveNodeKey(ownerPrivKey []byte, nodePubKey []byte) ([]byte, error) {
	if len(ownerPrivKey) != 32 {
		return nil, ErrInvalidPrivateKey
	}
	if err := validatePubKey(nodePubKey); err != nil {
		return nil, err
	}

	// Step 1: Compute shared secret via HMAC (simulating ECDH).
	sharedSecret := hmacSHA256(ownerPrivKey, nodePubKey)

	// Step 2: Derive key via HKDF.
	nodeKey := hkdfSHA256(sharedSecret, nil, []byte(KDFContext), 32)

	return nodeKey, nil
}

// EncryptForNode performs ECDH double-layer encryption of data for a
// specific Metanet Node.
//
// Steps:
//  1. Derive node-specific AES key via DeriveNodeKey.
//  2. Generate random 12-byte nonce.
//  3. Encrypt: ciphertext = AES-256-GCM(nodeKey, nonce, encryptedData).
//  4. Chunk the ciphertext and build a Merkle tree.
//
// The input encryptedData is already Method 42 encrypted (first layer).
// This adds the second layer without decrypting.
func EncryptForNode(
	ownerPrivKey []byte,
	nodePubKey []byte,
	encryptedData []byte,
) (*DoubleEncryptedData, error) {
	if len(encryptedData) == 0 {
		return nil, ErrEmptyData
	}

	// Derive the node-specific key.
	nodeKey, err := DeriveNodeKey(ownerPrivKey, nodePubKey)
	if err != nil {
		return nil, err
	}

	// Create AES-256-GCM cipher.
	block, err := aes.NewCipher(nodeKey)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	// Generate random nonce.
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, ErrEncryptionFailed
	}

	// Encrypt the data (second layer).
	ciphertext := gcm.Seal(nil, nonce[:], encryptedData, nil)

	// Chunk and build Merkle tree.
	chunks := SplitIntoChunks(ciphertext, DefaultChunkSize)
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		return nil, err
	}

	return &DoubleEncryptedData{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		MerkleRoot: tree.Root,
		NumChunks:  tree.NumLeaves,
		ChunkSize:  DefaultChunkSize,
	}, nil
}

// DecryptForNode decrypts the second layer of double-encrypted data using
// the derived node key. This is useful for verification and testing.
func DecryptForNode(
	ownerPrivKey []byte,
	nodePubKey []byte,
	ciphertext []byte,
	nonce [12]byte,
) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, ErrEmptyData
	}

	nodeKey, err := DeriveNodeKey(ownerPrivKey, nodePubKey)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(nodeKey)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	plaintext, err := gcm.Open(nil, nonce[:], ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// validatePubKey checks that a public key is a valid 33-byte compressed key.
func validatePubKey(pubKey []byte) error {
	if len(pubKey) != 33 {
		return ErrInvalidPublicKey
	}
	if pubKey[0] != 0x02 && pubKey[0] != 0x03 {
		return ErrInvalidPublicKey
	}
	return nil
}

// hmacSHA256 computes HMAC-SHA256(key, data).
func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// hkdfSHA256 implements HKDF (RFC 5869) using HMAC-SHA256.
//
// Steps:
//  1. Extract: PRK = HMAC-SHA256(salt, IKM)
//  2. Expand:  OKM = T(1) || T(2) || ... where T(i) = HMAC-SHA256(PRK, T(i-1) || info || i)
func hkdfSHA256(secret, salt, info []byte, length int) []byte {
	// Extract phase.
	if salt == nil {
		salt = make([]byte, 32)
	}
	prk := hmacSHA256(salt, secret)

	// Expand phase.
	var okm []byte
	prev := []byte{}
	for i := byte(1); len(okm) < length; i++ {
		input := make([]byte, 0, len(prev)+len(info)+1)
		input = append(input, prev...)
		input = append(input, info...)
		input = append(input, i)
		prev = hmacSHA256(prk, input)
		okm = append(okm, prev...)
	}
	return okm[:length]
}

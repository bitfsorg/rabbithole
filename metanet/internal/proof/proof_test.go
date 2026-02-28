// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package proof

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/bitfsorg/metanet/internal/contract"
)

// testOwnerPrivKey is a 32-byte private key for testing.
var testOwnerPrivKey = func() []byte {
	h := sha256.Sum256([]byte("owner-private-key-seed"))
	return h[:]
}()

// testNodePubKey1 is a valid 33-byte compressed public key for testing.
var testNodePubKey1 = func() []byte {
	key := make([]byte, 33)
	key[0] = 0x02
	h := sha256.Sum256([]byte("node-public-key-seed"))
	copy(key[1:], h[:])
	return key
}()

// testNodePubKey2 is a second valid 33-byte compressed public key.
var testNodePubKey2 = func() []byte {
	key := make([]byte, 33)
	key[0] = 0x03
	h := sha256.Sum256([]byte("node2-public-key-seed"))
	copy(key[1:], h[:])
	return key
}()

// ---------------------------------------------------------------------------
// DeriveNodeKey tests
// ---------------------------------------------------------------------------

func TestDeriveNodeKeyValid(t *testing.T) {
	key, err := DeriveNodeKey(testOwnerPrivKey, testNodePubKey1)
	if err != nil {
		t.Fatalf("DeriveNodeKey error: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}
}

func TestDeriveNodeKeyDeterministic(t *testing.T) {
	key1, err := DeriveNodeKey(testOwnerPrivKey, testNodePubKey1)
	if err != nil {
		t.Fatalf("first DeriveNodeKey: %v", err)
	}
	key2, err := DeriveNodeKey(testOwnerPrivKey, testNodePubKey1)
	if err != nil {
		t.Fatalf("second DeriveNodeKey: %v", err)
	}
	if !bytes.Equal(key1, key2) {
		t.Error("same inputs should produce same key")
	}
}

func TestDeriveNodeKeyUniquePerNode(t *testing.T) {
	key1, err := DeriveNodeKey(testOwnerPrivKey, testNodePubKey1)
	if err != nil {
		t.Fatalf("DeriveNodeKey node1: %v", err)
	}
	key2, err := DeriveNodeKey(testOwnerPrivKey, testNodePubKey2)
	if err != nil {
		t.Fatalf("DeriveNodeKey node2: %v", err)
	}
	if bytes.Equal(key1, key2) {
		t.Error("different nodes should produce different keys")
	}
}

func TestDeriveNodeKeyInvalidPrivateKey(t *testing.T) {
	_, err := DeriveNodeKey([]byte{0x01, 0x02}, testNodePubKey1)
	if err != ErrInvalidPrivateKey {
		t.Errorf("got %v, want ErrInvalidPrivateKey", err)
	}
}

func TestDeriveNodeKeyInvalidPublicKey(t *testing.T) {
	_, err := DeriveNodeKey(testOwnerPrivKey, []byte{0x04, 0x01})
	if err != ErrInvalidPublicKey {
		t.Errorf("got %v, want ErrInvalidPublicKey", err)
	}
}

// ---------------------------------------------------------------------------
// EncryptForNode / DecryptForNode tests
// ---------------------------------------------------------------------------

func TestEncryptForNodeRoundtrip(t *testing.T) {
	plaintext := []byte("this is the Method 42 first-layer encrypted data for testing")

	enc, err := EncryptForNode(testOwnerPrivKey, testNodePubKey1, plaintext)
	if err != nil {
		t.Fatalf("EncryptForNode: %v", err)
	}
	if len(enc.Ciphertext) == 0 {
		t.Fatal("ciphertext is empty")
	}
	if bytes.Equal(plaintext, enc.Ciphertext) {
		t.Error("ciphertext should differ from plaintext")
	}
	if enc.NumChunks == 0 {
		t.Error("NumChunks should be > 0")
	}

	decrypted, err := DecryptForNode(testOwnerPrivKey, testNodePubKey1, enc.Ciphertext, enc.Nonce)
	if err != nil {
		t.Fatalf("DecryptForNode: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Error("decrypted data does not match original")
	}
}

func TestEncryptForNodeUniquePerNode(t *testing.T) {
	data := []byte("shared data to encrypt for different nodes")

	enc1, err := EncryptForNode(testOwnerPrivKey, testNodePubKey1, data)
	if err != nil {
		t.Fatalf("EncryptForNode node1: %v", err)
	}
	enc2, err := EncryptForNode(testOwnerPrivKey, testNodePubKey2, data)
	if err != nil {
		t.Fatalf("EncryptForNode node2: %v", err)
	}
	if bytes.Equal(enc1.Ciphertext, enc2.Ciphertext) {
		t.Error("different nodes should produce different ciphertexts")
	}
}

func TestEncryptForNodeEmptyData(t *testing.T) {
	_, err := EncryptForNode(testOwnerPrivKey, testNodePubKey1, nil)
	if err != ErrEmptyData {
		t.Errorf("nil data: got %v, want ErrEmptyData", err)
	}
	_, err = EncryptForNode(testOwnerPrivKey, testNodePubKey1, []byte{})
	if err != ErrEmptyData {
		t.Errorf("empty data: got %v, want ErrEmptyData", err)
	}
}

func TestEncryptForNodeInvalidKeys(t *testing.T) {
	data := []byte("test data")

	_, err := EncryptForNode([]byte{0x01}, testNodePubKey1, data)
	if err != ErrInvalidPrivateKey {
		t.Errorf("bad privkey: got %v, want ErrInvalidPrivateKey", err)
	}
	_, err = EncryptForNode(testOwnerPrivKey, []byte{0x01}, data)
	if err != ErrInvalidPublicKey {
		t.Errorf("bad pubkey: got %v, want ErrInvalidPublicKey", err)
	}
}

// ---------------------------------------------------------------------------
// SplitIntoChunks tests
// ---------------------------------------------------------------------------

func TestSplitIntoChunksExact(t *testing.T) {
	data := bytes.Repeat([]byte{0xAA}, 100)
	chunks := SplitIntoChunks(data, 25)
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks, want 4", len(chunks))
	}
	for i, c := range chunks {
		if len(c) != 25 {
			t.Errorf("chunk %d: len=%d, want 25", i, len(c))
		}
	}
}

func TestSplitIntoChunksRemainder(t *testing.T) {
	data := bytes.Repeat([]byte{0xBB}, 110)
	chunks := SplitIntoChunks(data, 50)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if len(chunks[2]) != 10 {
		t.Errorf("last chunk len = %d, want 10", len(chunks[2]))
	}
}

func TestSplitIntoChunksEmpty(t *testing.T) {
	if chunks := SplitIntoChunks(nil, 10); chunks != nil {
		t.Error("nil data should return nil")
	}
	if chunks := SplitIntoChunks([]byte{}, 10); chunks != nil {
		t.Error("empty data should return nil")
	}
}

func TestSplitIntoChunksSingleByte(t *testing.T) {
	chunks := SplitIntoChunks([]byte{0xFF}, 1024)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if !bytes.Equal(chunks[0], []byte{0xFF}) {
		t.Error("single byte chunk mismatch")
	}
}

// ---------------------------------------------------------------------------
// BuildMerkleTree tests
// ---------------------------------------------------------------------------

func TestBuildMerkleTreeSingleChunk(t *testing.T) {
	chunks := [][]byte{[]byte("only chunk")}
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}
	if tree.NumLeaves != 1 {
		t.Errorf("NumLeaves = %d, want 1", tree.NumLeaves)
	}
	expected := sha256.Sum256(chunks[0])
	if tree.Root != expected {
		t.Error("single leaf should be the root")
	}
}

func TestBuildMerkleTreeTwoChunks(t *testing.T) {
	chunks := [][]byte{[]byte("chunk0"), []byte("chunk1")}
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}
	if tree.NumLeaves != 2 {
		t.Errorf("NumLeaves = %d, want 2", tree.NumLeaves)
	}

	h0 := sha256.Sum256(chunks[0])
	h1 := sha256.Sum256(chunks[1])
	var combined [64]byte
	copy(combined[0:32], h0[:])
	copy(combined[32:64], h1[:])
	expectedRoot := sha256.Sum256(combined[:])
	if tree.Root != expectedRoot {
		t.Error("two-chunk root does not match manual computation")
	}
}

func TestBuildMerkleTreeOddChunks(t *testing.T) {
	chunks := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}
	if tree.NumLeaves != 3 {
		t.Errorf("NumLeaves = %d, want 3", tree.NumLeaves)
	}
	if tree.Root == ([32]byte{}) {
		t.Error("root should not be zero")
	}
}

func TestBuildMerkleTreeEmpty(t *testing.T) {
	_, err := BuildMerkleTree(nil)
	if err != ErrEmptyData {
		t.Errorf("got %v, want ErrEmptyData", err)
	}
}

func TestBuildMerkleTreeDeterministic(t *testing.T) {
	chunks := [][]byte{[]byte("x"), []byte("y"), []byte("z"), []byte("w")}
	tree1, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("first BuildMerkleTree: %v", err)
	}
	tree2, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("second BuildMerkleTree: %v", err)
	}
	if tree1.Root != tree2.Root {
		t.Error("same chunks should produce same root")
	}
}

// ---------------------------------------------------------------------------
// GenerateMerkleProof / VerifyMerkleProof tests
// ---------------------------------------------------------------------------

func TestMerkleProofVerifyAllLeaves(t *testing.T) {
	chunks := [][]byte{
		[]byte("chunk-0"),
		[]byte("chunk-1"),
		[]byte("chunk-2"),
		[]byte("chunk-3"),
	}
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}

	for i := uint32(0); i < uint32(len(chunks)); i++ {
		proof, err := GenerateMerkleProof(tree, i)
		if err != nil {
			t.Fatalf("GenerateMerkleProof(%d): %v", i, err)
		}
		if !VerifyMerkleProof(chunks[i], proof) {
			t.Errorf("proof should verify for chunk %d", i)
		}
	}
}

func TestMerkleProofVerifyOddLeaves(t *testing.T) {
	chunks := [][]byte{
		[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"),
	}
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}

	for i := uint32(0); i < uint32(len(chunks)); i++ {
		proof, err := GenerateMerkleProof(tree, i)
		if err != nil {
			t.Fatalf("GenerateMerkleProof(%d): %v", i, err)
		}
		if !VerifyMerkleProof(chunks[i], proof) {
			t.Errorf("proof should verify for chunk %d (odd tree)", i)
		}
	}
}

func TestMerkleProofInvalidChunkData(t *testing.T) {
	chunks := [][]byte{[]byte("real"), []byte("data")}
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}
	proof, err := GenerateMerkleProof(tree, 0)
	if err != nil {
		t.Fatalf("GenerateMerkleProof: %v", err)
	}
	if VerifyMerkleProof([]byte("fake"), proof) {
		t.Error("proof should fail for wrong chunk data")
	}
}

func TestMerkleProofOutOfRange(t *testing.T) {
	chunks := [][]byte{[]byte("only")}
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}
	_, err = GenerateMerkleProof(tree, 1)
	if err != ErrChunkIndexOutOfRange {
		t.Errorf("got %v, want ErrChunkIndexOutOfRange", err)
	}
}

// ---------------------------------------------------------------------------
// Serialization tests
// ---------------------------------------------------------------------------

func TestSerializeDeserializeProofData(t *testing.T) {
	original := &ProofData{
		ChunkIndex: 42,
		ChunkData:  []byte("test chunk data for serialization"),
		MerkleProof: &MerkleProof{
			ChunkIndex: 42,
			Siblings: [][32]byte{
				sha256.Sum256([]byte("sibling0")),
				sha256.Sum256([]byte("sibling1")),
			},
		},
	}

	serialized := SerializeProofData(original)
	if len(serialized) == 0 {
		t.Fatal("serialized data is empty")
	}

	deserialized, err := DeserializeProofData(serialized)
	if err != nil {
		t.Fatalf("DeserializeProofData: %v", err)
	}

	if deserialized.ChunkIndex != original.ChunkIndex {
		t.Errorf("ChunkIndex: got %d, want %d", deserialized.ChunkIndex, original.ChunkIndex)
	}
	if !bytes.Equal(deserialized.ChunkData, original.ChunkData) {
		t.Error("ChunkData mismatch")
	}
	if len(deserialized.MerkleProof.Siblings) != 2 {
		t.Fatalf("Siblings: got %d, want 2", len(deserialized.MerkleProof.Siblings))
	}
	if deserialized.MerkleProof.Siblings[0] != original.MerkleProof.Siblings[0] {
		t.Error("Sibling[0] mismatch")
	}
	if deserialized.MerkleProof.Siblings[1] != original.MerkleProof.Siblings[1] {
		t.Error("Sibling[1] mismatch")
	}
}

func TestDeserializeProofDataTooShort(t *testing.T) {
	_, err := DeserializeProofData([]byte{0x01, 0x02})
	if err != ErrDeserialize {
		t.Errorf("got %v, want ErrDeserialize", err)
	}
}

func TestDeserializeProofDataTruncatedChunk(t *testing.T) {
	buf := make([]byte, 10)
	buf[4] = 100 // chunk_size = 100 but not enough data.
	_, err := DeserializeProofData(buf)
	if err != ErrDeserialize {
		t.Errorf("got %v, want ErrDeserialize", err)
	}
}

// ---------------------------------------------------------------------------
// ComputeProofHash tests
// ---------------------------------------------------------------------------

func TestComputeProofHashDeterministic(t *testing.T) {
	proof := &ProofData{
		ChunkIndex: 0,
		ChunkData:  []byte("proof data"),
		MerkleProof: &MerkleProof{
			ChunkIndex: 0,
			Siblings:   [][32]byte{sha256.Sum256([]byte("sib"))},
		},
	}
	h1 := ComputeProofHash(proof)
	h2 := ComputeProofHash(proof)
	if h1 != h2 {
		t.Error("ComputeProofHash should be deterministic")
	}
	if h1 == ([32]byte{}) {
		t.Error("hash should not be zero")
	}
}

func TestComputeProofHashDifferentData(t *testing.T) {
	mkProof := func(data string) *ProofData {
		return &ProofData{
			ChunkIndex:  0,
			ChunkData:   []byte(data),
			MerkleProof: &MerkleProof{ChunkIndex: 0, Siblings: [][32]byte{}},
		}
	}
	h1 := ComputeProofHash(mkProof("data1"))
	h2 := ComputeProofHash(mkProof("data2"))
	if h1 == h2 {
		t.Error("different data should produce different hashes")
	}
}

// ---------------------------------------------------------------------------
// VerifyStorageProof integration test
// ---------------------------------------------------------------------------

func TestVerifyStorageProofValid(t *testing.T) {
	chunks := [][]byte{
		[]byte("chunk-0-data"),
		[]byte("chunk-1-data"),
		[]byte("chunk-2-data"),
		[]byte("chunk-3-data"),
	}

	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}

	contractTxID := [32]byte{0xab, 0xcd, 0xef}
	period := uint32(0)
	numChunks := uint32(len(chunks))

	challenge := contract.ComputeChallenge(contractTxID, period, numChunks)

	merkleProof, err := GenerateMerkleProof(tree, challenge.ChunkIndex)
	if err != nil {
		t.Fatalf("GenerateMerkleProof: %v", err)
	}

	proof := &ProofData{
		ChunkIndex:  challenge.ChunkIndex,
		ChunkData:   chunks[challenge.ChunkIndex],
		MerkleProof: merkleProof,
	}

	expectedHash := ComputeProofHash(proof)

	err = VerifyStorageProof(contractTxID, period, numChunks, expectedHash, tree.Root, proof)
	if err != nil {
		t.Errorf("VerifyStorageProof should succeed: %v", err)
	}
}

func TestVerifyStorageProofChallengeMismatch(t *testing.T) {
	chunks := [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")}
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}

	contractTxID := [32]byte{0x01}
	period := uint32(0)
	numChunks := uint32(len(chunks))
	challenge := contract.ComputeChallenge(contractTxID, period, numChunks)

	wrongIndex := (challenge.ChunkIndex + 1) % numChunks
	merkleProof, err := GenerateMerkleProof(tree, wrongIndex)
	if err != nil {
		t.Fatalf("GenerateMerkleProof: %v", err)
	}

	proof := &ProofData{
		ChunkIndex:  wrongIndex,
		ChunkData:   chunks[wrongIndex],
		MerkleProof: merkleProof,
	}

	err = VerifyStorageProof(contractTxID, period, numChunks, [32]byte{}, tree.Root, proof)
	if err != ErrChallengeMismatch {
		t.Errorf("got %v, want ErrChallengeMismatch", err)
	}
}

func TestVerifyStorageProofHashMismatch(t *testing.T) {
	chunks := [][]byte{[]byte("data0"), []byte("data1")}
	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}

	contractTxID := [32]byte{0x02}
	period := uint32(0)
	numChunks := uint32(len(chunks))
	challenge := contract.ComputeChallenge(contractTxID, period, numChunks)

	merkleProof, err := GenerateMerkleProof(tree, challenge.ChunkIndex)
	if err != nil {
		t.Fatalf("GenerateMerkleProof: %v", err)
	}

	proof := &ProofData{
		ChunkIndex:  challenge.ChunkIndex,
		ChunkData:   chunks[challenge.ChunkIndex],
		MerkleProof: merkleProof,
	}

	wrongHash := [32]byte{0xff}
	err = VerifyStorageProof(contractTxID, period, numChunks, wrongHash, tree.Root, proof)
	if err != ErrProofHashMismatch {
		t.Errorf("got %v, want ErrProofHashMismatch", err)
	}
}

func TestVerifyStorageProofMultiplePeriods(t *testing.T) {
	chunks := [][]byte{
		[]byte("chunk-A"), []byte("chunk-B"),
		[]byte("chunk-C"), []byte("chunk-D"),
		[]byte("chunk-E"), []byte("chunk-F"),
		[]byte("chunk-G"), []byte("chunk-H"),
	}

	tree, err := BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}

	contractTxID := [32]byte{0xde, 0xad, 0xbe, 0xef}
	numChunks := uint32(len(chunks))

	for period := uint32(0); period < 10; period++ {
		challenge := contract.ComputeChallenge(contractTxID, period, numChunks)
		merkleProof, err := GenerateMerkleProof(tree, challenge.ChunkIndex)
		if err != nil {
			t.Fatalf("period %d: GenerateMerkleProof: %v", period, err)
		}

		proof := &ProofData{
			ChunkIndex:  challenge.ChunkIndex,
			ChunkData:   chunks[challenge.ChunkIndex],
			MerkleProof: merkleProof,
		}

		expectedHash := ComputeProofHash(proof)
		err = VerifyStorageProof(contractTxID, period, numChunks, expectedHash, tree.Root, proof)
		if err != nil {
			t.Errorf("period %d: VerifyStorageProof: %v", period, err)
		}
	}
}

// ---------------------------------------------------------------------------
// HKDF tests
// ---------------------------------------------------------------------------

func TestHKDFDeterministic(t *testing.T) {
	secret := []byte("secret")
	info := []byte("info")
	k1 := hkdfSHA256(secret, nil, info, 32)
	k2 := hkdfSHA256(secret, nil, info, 32)
	if !bytes.Equal(k1, k2) {
		t.Error("HKDF should be deterministic")
	}
}

func TestHKDFDifferentInfos(t *testing.T) {
	secret := []byte("secret")
	k1 := hkdfSHA256(secret, nil, []byte("info1"), 32)
	k2 := hkdfSHA256(secret, nil, []byte("info2"), 32)
	if bytes.Equal(k1, k2) {
		t.Error("different info should produce different keys")
	}
}

func TestHKDFOutputLength(t *testing.T) {
	tests := []int{16, 32, 48, 64}
	for _, length := range tests {
		key := hkdfSHA256([]byte("s"), nil, []byte("i"), length)
		if len(key) != length {
			t.Errorf("HKDF output length = %d, want %d", len(key), length)
		}
	}
}

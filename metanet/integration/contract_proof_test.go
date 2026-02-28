// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

//go:build integration

package integration

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/bitfsorg/metanet/internal/contract"
	"github.com/bitfsorg/metanet/internal/proof"
)

// makeTestPubKey creates a valid 33-byte compressed public key from a seed.
func makeTestPubKey(prefix byte, seed string) []byte {
	key := make([]byte, 33)
	key[0] = prefix
	h := sha256.Sum256([]byte(seed))
	copy(key[1:], h[:])
	return key
}

// makeTestPrivKey creates a 32-byte private key from a seed.
func makeTestPrivKey(seed string) []byte {
	h := sha256.Sum256([]byte(seed))
	return h[:]
}

// ---------------------------------------------------------------------------
// TestStorageDealFullLifecycle — the complete storage deal cycle from data
// preparation through all challenge periods.
// ---------------------------------------------------------------------------

func TestStorageDealFullLifecycle(t *testing.T) {
	// Step 1: Create test data (1KB file, split into 4 chunks).
	testData := make([]byte, 1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	// Step 2: Generate owner and node key pairs.
	ownerPrivKey := makeTestPrivKey("owner-storage-deal-key")
	ownerPubKey := makeTestPubKey(0x02, "owner-storage-deal-pubkey")
	nodePubKey := makeTestPubKey(0x03, "node-storage-deal-pubkey")

	// Step 3: Encrypt data for node.
	enc, err := proof.EncryptForNode(ownerPrivKey, nodePubKey, testData)
	if err != nil {
		t.Fatalf("EncryptForNode: %v", err)
	}
	if len(enc.Ciphertext) == 0 {
		t.Fatal("ciphertext is empty")
	}

	// Step 4: Split encrypted data into chunks.
	chunkSize := uint32(len(enc.Ciphertext) / 4)
	if chunkSize == 0 {
		chunkSize = 1
	}
	chunks := proof.SplitIntoChunks(enc.Ciphertext, chunkSize)
	if len(chunks) < 4 {
		t.Fatalf("expected at least 4 chunks, got %d", len(chunks))
	}
	numChunks := uint32(len(chunks))

	// Step 5: Build Merkle tree over chunks.
	tree, err := proof.BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}

	// Step 6: Simulate a contract TxID (deterministic for testing).
	contractTxID := sha256.Sum256([]byte("storage-deal-contract-txid"))
	numPeriods := uint32(5)

	// Step 7: Pre-compute expected hashes for all challenge periods.
	// We use the proof package's ComputeProofHash (which serializes the full
	// ProofData structure) to compute expected hashes, matching what
	// VerifyStorageProof will check on-chain.
	expectedHashes := make([][32]byte, numPeriods)
	for k := uint32(0); k < numPeriods; k++ {
		challenge := contract.ComputeChallenge(contractTxID, k, numChunks)
		merkleProof, err := proof.GenerateMerkleProof(tree, challenge.ChunkIndex)
		if err != nil {
			t.Fatalf("PrecomputeExpectedHashes period %d: %v", k, err)
		}
		pd := &proof.ProofData{
			ChunkIndex:  challenge.ChunkIndex,
			ChunkData:   chunks[challenge.ChunkIndex],
			MerkleProof: merkleProof,
		}
		expectedHashes[k] = proof.ComputeProofHash(pd)
	}

	// Step 8: Create StorageDeal.
	deal := &contract.StorageDeal{
		OwnerPubKey:     ownerPubKey,
		NodePubKey:      nodePubKey,
		NumPeriods:      numPeriods,
		TokenPerPeriod:  50_000,
		BlocksPerPeriod: 144,
		StartBlock:      1000,
		MerkleRoot:      tree.Root,
		NumChunks:       numChunks,
		ExpectedHashes:  expectedHashes,
	}

	// Step 9: Build deal UTXOs.
	utxos, err := contract.BuildStorageDealUTXOs(deal)
	if err != nil {
		t.Fatalf("BuildStorageDealUTXOs: %v", err)
	}
	if uint32(len(utxos)) != numPeriods {
		t.Fatalf("expected %d UTXOs, got %d", numPeriods, len(utxos))
	}

	// Step 10: For each period, generate and verify storage proof.
	for k := uint32(0); k < numPeriods; k++ {
		t.Run("period_"+string(rune('0'+k)), func(t *testing.T) {
			// Step 10a: Compute challenge.
			challenge := contract.ComputeChallenge(contractTxID, k, numChunks)

			// Step 10b: Get challenged chunk.
			chunkData := chunks[challenge.ChunkIndex]

			// Step 10c: Generate Merkle proof.
			merkleProof, err := proof.GenerateMerkleProof(tree, challenge.ChunkIndex)
			if err != nil {
				t.Fatalf("GenerateMerkleProof: %v", err)
			}

			// Step 10d: Build proof data.
			proofData := &proof.ProofData{
				ChunkIndex:  challenge.ChunkIndex,
				ChunkData:   chunkData,
				MerkleProof: merkleProof,
			}

			// Step 10e: Verify storage proof passes.
			computedHash := proof.ComputeProofHash(proofData)
			err = proof.VerifyStorageProof(
				contractTxID, k, numChunks,
				computedHash, tree.Root, proofData,
			)
			if err != nil {
				t.Errorf("VerifyStorageProof should pass for period %d: %v", k, err)
			}

			// Step 10f: Verify the computed hash matches the pre-computed expected hash.
			if computedHash != expectedHashes[k] {
				t.Errorf("period %d: proof hash does not match pre-computed expected hash", k)
			}

			// Step 10g: Verify wrong chunk data fails.
			wrongProof := &proof.ProofData{
				ChunkIndex:  challenge.ChunkIndex,
				ChunkData:   []byte("wrong data"),
				MerkleProof: merkleProof,
			}
			wrongHash := proof.ComputeProofHash(wrongProof)
			err = proof.VerifyStorageProof(
				contractTxID, k, numChunks,
				wrongHash, tree.Root, wrongProof,
			)
			if err == nil {
				t.Errorf("period %d: wrong chunk data should fail Merkle verification", k)
			}
		})
	}

	// Step 11: Verify all 5 periods can be claimed (UTXOs are valid).
	t.Run("all_utxos_have_valid_scripts", func(t *testing.T) {
		for i, utxo := range utxos {
			if len(utxo.Script) == 0 {
				t.Errorf("UTXO %d has empty script", i)
			}
			if utxo.Value != deal.TokenPerPeriod {
				t.Errorf("UTXO %d value = %d, want %d", i, utxo.Value, deal.TokenPerPeriod)
			}
			expectedExpire := deal.StartBlock + uint32(i+1)*deal.BlocksPerPeriod
			if utxo.ExpireBlock != expectedExpire {
				t.Errorf("UTXO %d expire = %d, want %d", i, utxo.ExpireBlock, expectedExpire)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestDoubleLayerEncryptionUniqueness — same plaintext encrypted for
// different nodes produces different ciphertexts.
// ---------------------------------------------------------------------------

func TestDoubleLayerEncryptionUniqueness(t *testing.T) {
	ownerPrivKey := makeTestPrivKey("owner-enc-unique")
	plaintext := []byte("shared plaintext content for all nodes - at least 16 bytes")

	nodePubKeys := [][]byte{
		makeTestPubKey(0x02, "node-A-enc"),
		makeTestPubKey(0x03, "node-B-enc"),
		makeTestPubKey(0x02, "node-C-enc"),
	}

	type encResult struct {
		ciphertext []byte
		nonce      [12]byte
	}
	results := make([]encResult, 3)

	t.Run("encrypt_for_three_nodes", func(t *testing.T) {
		for i, nodePubKey := range nodePubKeys {
			enc, err := proof.EncryptForNode(ownerPrivKey, nodePubKey, plaintext)
			if err != nil {
				t.Fatalf("EncryptForNode node %d: %v", i, err)
			}
			results[i] = encResult{
				ciphertext: enc.Ciphertext,
				nonce:      enc.Nonce,
			}
		}
	})

	t.Run("all_ciphertexts_different", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			for j := i + 1; j < 3; j++ {
				if bytes.Equal(results[i].ciphertext, results[j].ciphertext) {
					t.Errorf("node %d and node %d produced same ciphertext", i, j)
				}
			}
		}
	})

	t.Run("each_node_can_decrypt_own", func(t *testing.T) {
		for i, nodePubKey := range nodePubKeys {
			decrypted, err := proof.DecryptForNode(
				ownerPrivKey, nodePubKey,
				results[i].ciphertext, results[i].nonce,
			)
			if err != nil {
				t.Fatalf("node %d: DecryptForNode failed: %v", i, err)
			}
			if !bytes.Equal(decrypted, plaintext) {
				t.Errorf("node %d: decrypted data does not match plaintext", i)
			}
		}
	})

	t.Run("node_A_cannot_decrypt_node_B", func(t *testing.T) {
		// Node A's key cannot decrypt Node B's ciphertext.
		_, err := proof.DecryptForNode(
			ownerPrivKey, nodePubKeys[0], // Node A's key
			results[1].ciphertext, results[1].nonce, // Node B's ciphertext
		)
		if err == nil {
			t.Error("node A should not be able to decrypt node B's ciphertext")
		}
	})
}

// ---------------------------------------------------------------------------
// TestChallengeReproducibility — same inputs produce same challenges,
// different inputs produce different challenges.
// ---------------------------------------------------------------------------

func TestChallengeReproducibility(t *testing.T) {
	txid := sha256.Sum256([]byte("test-challenge-txid"))
	numChunks := uint32(100)

	t.Run("same_txid_same_challenges", func(t *testing.T) {
		for k := uint32(0); k < 10; k++ {
			c1 := contract.ComputeChallenge(txid, k, numChunks)
			c2 := contract.ComputeChallenge(txid, k, numChunks)
			if c1.Seed != c2.Seed {
				t.Errorf("period %d: seeds differ for same inputs", k)
			}
			if c1.ChunkIndex != c2.ChunkIndex {
				t.Errorf("period %d: chunk indices differ for same inputs", k)
			}
		}
	})

	t.Run("different_txids_different_challenges", func(t *testing.T) {
		txid2 := sha256.Sum256([]byte("different-txid"))
		sameCount := 0
		for k := uint32(0); k < 20; k++ {
			c1 := contract.ComputeChallenge(txid, k, numChunks)
			c2 := contract.ComputeChallenge(txid2, k, numChunks)
			if c1.Seed == c2.Seed {
				t.Errorf("period %d: different txids produced same seed", k)
			}
			if c1.ChunkIndex == c2.ChunkIndex {
				sameCount++
			}
		}
		// With 100 chunks, the probability of ALL 20 matching is astronomically low.
		if sameCount == 20 {
			t.Error("all 20 periods produced same chunk index for different txids")
		}
	})

	t.Run("challenges_cover_different_chunks", func(t *testing.T) {
		seen := make(map[uint32]bool)
		for k := uint32(0); k < 50; k++ {
			c := contract.ComputeChallenge(txid, k, numChunks)
			seen[c.ChunkIndex] = true
			if c.ChunkIndex >= numChunks {
				t.Errorf("period %d: chunk index %d out of range", k, c.ChunkIndex)
			}
		}
		// With 100 chunks and 50 periods, we expect good coverage.
		if len(seen) < 10 {
			t.Errorf("poor chunk coverage: only %d unique chunks in 50 periods", len(seen))
		}
	})
}

// ---------------------------------------------------------------------------
// TestMerkleProofIntegrity — build tree from 16 chunks, verify all proofs,
// test tamper detection.
// ---------------------------------------------------------------------------

func TestMerkleProofIntegrity(t *testing.T) {
	// Build 16 chunks.
	chunks := make([][]byte, 16)
	for i := range chunks {
		chunks[i] = make([]byte, 64)
		for j := range chunks[i] {
			chunks[i][j] = byte((i*64 + j) % 256)
		}
	}

	tree, err := proof.BuildMerkleTree(chunks)
	if err != nil {
		t.Fatalf("BuildMerkleTree: %v", err)
	}

	if tree.NumLeaves != 16 {
		t.Fatalf("NumLeaves = %d, want 16", tree.NumLeaves)
	}

	t.Run("all_proofs_verify", func(t *testing.T) {
		for i := uint32(0); i < 16; i++ {
			p, err := proof.GenerateMerkleProof(tree, i)
			if err != nil {
				t.Fatalf("chunk %d: GenerateMerkleProof: %v", i, err)
			}
			if !proof.VerifyMerkleProof(chunks[i], p) {
				t.Errorf("chunk %d: proof should verify", i)
			}
		}
	})

	t.Run("tampered_chunk_fails", func(t *testing.T) {
		p, _ := proof.GenerateMerkleProof(tree, 0)
		tamperedChunk := make([]byte, len(chunks[0]))
		copy(tamperedChunk, chunks[0])
		tamperedChunk[0] ^= 0xff // Flip a byte.

		if proof.VerifyMerkleProof(tamperedChunk, p) {
			t.Error("tampered chunk should not verify")
		}
	})

	t.Run("tampered_sibling_fails", func(t *testing.T) {
		p, _ := proof.GenerateMerkleProof(tree, 0)
		if len(p.Siblings) == 0 {
			t.Skip("no siblings to tamper with")
		}

		// Create a copy with tampered sibling.
		tamperedProof := &proof.MerkleProof{
			ChunkIndex: p.ChunkIndex,
			Root:       p.Root,
			Siblings:   make([][32]byte, len(p.Siblings)),
		}
		copy(tamperedProof.Siblings, p.Siblings)
		tamperedProof.Siblings[0][0] ^= 0xff // Flip a byte in sibling.

		if proof.VerifyMerkleProof(chunks[0], tamperedProof) {
			t.Error("tampered sibling should not verify")
		}
	})

	t.Run("proof_serialization_roundtrip", func(t *testing.T) {
		p, _ := proof.GenerateMerkleProof(tree, 7)
		proofData := &proof.ProofData{
			ChunkIndex:  7,
			ChunkData:   chunks[7],
			MerkleProof: p,
		}
		serialized := proof.SerializeProofData(proofData)
		deserialized, err := proof.DeserializeProofData(serialized)
		if err != nil {
			t.Fatalf("DeserializeProofData: %v", err)
		}
		if deserialized.ChunkIndex != proofData.ChunkIndex {
			t.Error("ChunkIndex mismatch after roundtrip")
		}
		if !bytes.Equal(deserialized.ChunkData, proofData.ChunkData) {
			t.Error("ChunkData mismatch after roundtrip")
		}
		if len(deserialized.MerkleProof.Siblings) != len(proofData.MerkleProof.Siblings) {
			t.Error("Siblings count mismatch after roundtrip")
		}
	})

	t.Run("wrong_root_fails", func(t *testing.T) {
		p, _ := proof.GenerateMerkleProof(tree, 5)
		wrongRootProof := &proof.MerkleProof{
			ChunkIndex: p.ChunkIndex,
			Root:       [32]byte{0xff}, // Wrong root.
			Siblings:   p.Siblings,
		}
		if proof.VerifyMerkleProof(chunks[5], wrongRootProof) {
			t.Error("wrong root should not verify")
		}
	})
}

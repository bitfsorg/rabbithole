//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/method42"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
	"github.com/bitfsorg/libbitfs-go/x402"
)

// TestPaidPurchaseFlow exercises the full x402 paid purchase flow:
//
//  1. Seller creates a Metanet file node with AccessPaid encryption
//  2. Buyer requests purchase; seller computes capsule = ECDH(D_node, P_buyer).x
//  3. CapsuleHash = SHA256(capsule) is used to lock an HTLC
//  4. HTLC script is built and its structure verified
//  5. Seller reveals capsule by "claiming" the HTLC (simulated claim tx)
//  6. Buyer extracts capsule from the claim tx via ParseHTLCPreimage
//  7. Buyer decrypts content using DecryptWithCapsule
//
// The on-chain portion builds and broadcasts the seller's file node on regtest.
// The HTLC claim tx is constructed in-memory (no BuildClaimTx exists yet)
// and tested via ParseHTLCPreimage extraction.
func TestPaidPurchaseFlow(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Create seller wallet and buyer wallet.
	// ==================================================================
	sellerWallet := setupFundedWallet(t, ctx, node)
	buyerWallet := setupFundedWallet(t, ctx, node)

	// Seller keys.
	sellerFeeKey, err := sellerWallet.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive seller fee key")

	sellerRootKey, err := sellerWallet.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive seller root node key")

	sellerFileKey, err := sellerWallet.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive seller file node key")

	// Buyer keys.
	buyerFeeKey, err := buyerWallet.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive buyer fee key")

	t.Logf("seller fee key:  %s", sellerFeeKey.Path)
	t.Logf("seller root key: %s", sellerRootKey.Path)
	t.Logf("seller file key: %s", sellerFileKey.Path)
	t.Logf("buyer fee key:   %s", buyerFeeKey.Path)

	// ==================================================================
	// Step 2: Fund the seller fee address, build root + file node on-chain.
	// ==================================================================
	sellerFeeAddr, err := script.NewAddressFromPublicKey(sellerFeeKey.PublicKey, false)
	require.NoError(t, err, "seller fee address")

	sellerFeeUTXO := getFundedUTXO(t, ctx, node, sellerFeeAddr.AddressString, sellerFeeKey)

	mineAddr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate mining address")
	mineOneBlock := func(t *testing.T) {
		t.Helper()
		_, err := node.MineBlocks(ctx, 1, mineAddr)
		require.NoError(t, err, "mine confirmation block")
	}

	// Build and broadcast root directory tx.
	rootPayload := []byte("bitfs seller root")
	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(sellerRootKey.PublicKey, rootPayload)
	rootBatch.AddFeeInput(sellerFeeUTXO)
	rootBatch.SetChange(sellerFeeKey.PublicKey.Hash())
	rootBatch.SetFeeRate(1)
	rootResult, err := rootBatch.Build()
	require.NoError(t, err, "build seller root tx")

	rootSignedHex, err := rootBatch.Sign(rootResult)
	require.NoError(t, err, "sign seller root tx")

	rootTxIDStr, err := node.SendRawTransaction(ctx, rootSignedHex)
	require.NoError(t, err, "broadcast seller root tx")
	t.Logf("seller root txid: %s", rootTxIDStr)
	mineOneBlock(t)

	// Prepare root node UTXO and change UTXO for child tx.
	rootNodeUTXO := rootResult.NodeOps[0].NodeUTXO
	rootNodeUTXOScript, err := tx.BuildP2PKHScript(sellerRootKey.PublicKey)
	require.NoError(t, err)
	rootNodeUTXO.ScriptPubKey = rootNodeUTXOScript
	rootNodeUTXO.PrivateKey = sellerRootKey.PrivateKey

	changeUTXO := rootResult.ChangeUTXO
	require.NotNil(t, changeUTXO, "root tx should have change output")
	changeScript, err := tx.BuildP2PKHScript(sellerFeeKey.PublicKey)
	require.NoError(t, err)
	changeUTXO.ScriptPubKey = changeScript
	changeUTXO.PrivateKey = sellerFeeKey.PrivateKey

	// ==================================================================
	// Step 3: Encrypt file content with AccessPaid mode.
	// ==================================================================
	originalContent := []byte("This is premium content that requires payment to access. " +
		"The buyer must obtain the ECDH capsule through an HTLC atomic swap.")

	// Encrypt with seller's file key in Paid mode.
	// AccessPaid uses the same ECDH as AccessPrivate: ECDH(D_file, P_file).
	encResult, err := method42.Encrypt(
		originalContent,
		sellerFileKey.PrivateKey,
		sellerFileKey.PublicKey,
		method42.AccessPaid,
	)
	require.NoError(t, err, "encrypt with AccessPaid")
	require.NotEmpty(t, encResult.Ciphertext)
	require.NotEmpty(t, encResult.KeyHash)
	t.Logf("encrypted %d bytes -> %d bytes ciphertext", len(originalContent), len(encResult.Ciphertext))

	// ==================================================================
	// Step 4: Build and broadcast the file node (Metanet child) on-chain.
	// ==================================================================
	// File payload: keyHash(32B) + ciphertext (matches 03_mkdir_upload pattern).
	filePayload := make([]byte, 0, 32+len(encResult.Ciphertext))
	filePayload = append(filePayload, encResult.KeyHash...)
	filePayload = append(filePayload, encResult.Ciphertext...)

	fileBatch := tx.NewMutationBatch()
	fileBatch.AddCreateChild(sellerFileKey.PublicKey, rootResult.TxID, filePayload, rootNodeUTXO, sellerRootKey.PrivateKey)
	fileBatch.AddFeeInput(changeUTXO)
	fileBatch.SetChange(sellerFeeKey.PublicKey.Hash())
	fileBatch.SetFeeRate(1)
	fileResult, err := fileBatch.Build()
	require.NoError(t, err, "build file node tx")

	fileSignedHex, err := fileBatch.Sign(fileResult)
	require.NoError(t, err, "sign file node tx")

	fileTxIDStr, err := node.SendRawTransaction(ctx, fileSignedHex)
	require.NoError(t, err, "broadcast file node tx")
	t.Logf("file node txid: %s", fileTxIDStr)
	mineOneBlock(t)

	// ==================================================================
	// Step 5: Buyer requests purchase -- seller computes capsule.
	// ==================================================================
	// Capsule = AES_key XOR BuyerMask, where:
	//   AES_key   = HKDF(ECDH(D_file, P_file).x, keyHash)
	//   BuyerMask = HKDF(ECDH(D_file, P_buyer).x, keyHash)
	capsule, err := method42.ComputeCapsule(sellerFileKey.PrivateKey, sellerFileKey.PublicKey, buyerFeeKey.PublicKey, encResult.KeyHash)
	require.NoError(t, err, "compute capsule")
	require.Len(t, capsule, 32, "capsule should be 32 bytes")
	t.Logf("capsule: %x", capsule[:16])

	// CapsuleHash = SHA256(fileTxID ‖ capsule) -- used to lock the HTLC.
	fileTxID := bytes.Repeat([]byte{0xf0}, 32) // mock file txid for e2e test
	capsuleHash := method42.ComputeCapsuleHash(fileTxID, capsule)
	require.Len(t, capsuleHash, 32, "capsule hash should be 32 bytes")
	t.Logf("capsuleHash: %x", capsuleHash[:16])

	// Verify capsuleHash == SHA256(fileTxID ‖ capsule) independently.
	expectedHasher := sha256.New()
	expectedHasher.Write(fileTxID)
	expectedHasher.Write(capsule)
	require.Equal(t, expectedHasher.Sum(nil), capsuleHash, "capsule hash should be SHA256(fileTxID || capsule)")

	// ==================================================================
	// Step 6: Create x402 invoice.
	// ==================================================================
	pricePerKB := uint64(100) // 100 sat/KB
	fileSize := uint64(len(originalContent))
	sellerAddr := sellerFeeAddr.AddressString

	invoice := x402.NewInvoice(pricePerKB, fileSize, sellerAddr, capsuleHash, 3600)
	require.NotEmpty(t, invoice.ID, "invoice should have an ID")
	require.False(t, invoice.IsExpired(), "invoice should not be expired")

	expectedPrice := x402.CalculatePrice(pricePerKB, fileSize)
	require.Equal(t, expectedPrice, invoice.Price, "invoice price should match calculated price")
	require.Equal(t, capsuleHash, invoice.CapsuleHash, "invoice capsule hash should match")
	t.Logf("invoice: id=%s price=%d sat (%.2f sat/KB * %d bytes)",
		invoice.ID, invoice.Price, float64(pricePerKB), fileSize)

	// ==================================================================
	// Step 7: Build the HTLC locking script.
	// ==================================================================
	// Seller address hash (20 bytes) for P2PKH-style check in HTLC.
	sellerPKH := sellerFeeKey.PublicKey.Hash()
	require.Len(t, sellerPKH, 20, "seller PKH should be 20 bytes")

	sellerPubKeyCompressed := sellerFeeKey.PublicKey.Compressed()
	htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerFeeKey.PublicKey.Compressed(),
		SellerPubKey: sellerPubKeyCompressed,
		SellerAddr:   sellerPKH,
		CapsuleHash:  capsuleHash,
		Amount:       invoice.Price,
		Timeout:      x402.DefaultHTLCTimeout,
	})
	require.NoError(t, err, "build HTLC script")
	require.NotEmpty(t, htlcScript, "HTLC script should not be empty")
	t.Logf("HTLC script: %d bytes", len(htlcScript))

	// Verify the HTLC script structure by parsing it.
	htlcScriptObj := script.Script(htlcScript)
	chunks, err := htlcScriptObj.Chunks()
	require.NoError(t, err, "parse HTLC script chunks")

	// Expected structure (2-of-2 multisig refund):
	// OP_IF OP_SHA256 <capsule_hash> OP_EQUALVERIFY
	// OP_DUP OP_HASH160 <seller_addr> OP_EQUALVERIFY OP_CHECKSIG
	// OP_ELSE OP_2 <buyer_pubkey> <seller_pubkey> OP_2 OP_CHECKMULTISIG OP_ENDIF
	require.GreaterOrEqual(t, len(chunks), 13,
		"HTLC script should have at least 13 chunks")

	// Verify OP_IF at start and OP_ENDIF at end.
	assert.Equal(t, script.OpIF, chunks[0].Op, "first chunk should be OP_IF")
	assert.Equal(t, script.OpENDIF, chunks[len(chunks)-1].Op, "last chunk should be OP_ENDIF")

	// Verify OP_SHA256 is present (seller claim path).
	assert.Equal(t, script.OpSHA256, chunks[1].Op, "second chunk should be OP_SHA256")

	// Verify capsule hash is embedded in the script.
	assert.Equal(t, capsuleHash, chunks[2].Data,
		"third chunk should contain capsule hash")

	// Verify OP_CHECKMULTISIG is present (buyer 2-of-2 multisig refund path).
	foundMultisig := false
	for _, chunk := range chunks {
		if chunk.Op == script.OpCHECKMULTISIG {
			foundMultisig = true
			break
		}
	}
	assert.True(t, foundMultisig, "HTLC should contain OP_CHECKMULTISIG for 2-of-2 refund")

	// Verify buyer pubkey is embedded in the script.
	buyerPubKeyBytes := buyerFeeKey.PublicKey.Compressed()
	foundBuyerPK := false
	for _, chunk := range chunks {
		if bytes.Equal(chunk.Data, buyerPubKeyBytes) {
			foundBuyerPK = true
			break
		}
	}
	assert.True(t, foundBuyerPK, "HTLC should contain buyer's compressed public key")

	// Verify seller address hash is embedded in the script.
	foundSellerAddr := false
	for _, chunk := range chunks {
		if bytes.Equal(chunk.Data, sellerPKH) {
			foundSellerAddr = true
			break
		}
	}
	assert.True(t, foundSellerAddr, "HTLC should contain seller's address hash")

	// ==================================================================
	// Step 8: Fund buyer and build HTLC funding tx using BuildHTLCFundingTx.
	// ==================================================================
	buyerFeeAddr, err := script.NewAddressFromPublicKey(buyerFeeKey.PublicKey, false)
	require.NoError(t, err, "buyer fee address")

	buyerUTXO := getFundedUTXO(t, ctx, node, buyerFeeAddr.AddressString, buyerFeeKey)
	t.Logf("buyer UTXO: txid=%x, vout=%d, amount=%d sat",
		buyerUTXO.TxID, buyerUTXO.Vout, buyerUTXO.Amount)

	changePKH := buyerFeeKey.PublicKey.Hash()

	fundingResult, err := x402.BuildHTLCFundingTx(&x402.HTLCFundingParams{
		BuyerPrivKey: buyerFeeKey.PrivateKey,
		SellerPubKey: sellerPubKeyCompressed,
		SellerAddr:   sellerPKH,
		CapsuleHash:  capsuleHash,
		Amount:       invoice.Price,
		Timeout:      x402.DefaultHTLCTimeout,
		UTXOs: []*x402.HTLCUTXO{{
			TxID:         buyerUTXO.TxID,
			Vout:         buyerUTXO.Vout,
			Amount:       buyerUTXO.Amount,
			ScriptPubKey: buyerUTXO.ScriptPubKey,
		}},
		ChangeAddr: changePKH,
		FeeRate:    1,
	})
	require.NoError(t, err, "build HTLC funding tx")
	t.Logf("HTLC funding tx: %d bytes", len(fundingResult.RawTx))

	htlcPaymentTxID, err := node.SendRawTransaction(ctx, hex.EncodeToString(fundingResult.RawTx))
	require.NoError(t, err, "broadcast HTLC funding tx")
	t.Logf("HTLC payment txid: %s", htlcPaymentTxID)
	mineOneBlock(t)

	// Retrieve from chain to confirm it was accepted.
	htlcRawBytes, err := node.GetRawTransaction(ctx, htlcPaymentTxID)
	require.NoError(t, err, "get HTLC tx from chain")
	require.NotEmpty(t, htlcRawBytes)
	t.Logf("HTLC tx confirmed on-chain: %d bytes", len(htlcRawBytes))

	// ==================================================================
	// Step 9: Seller claims the HTLC using BuildSellerClaimTx (real signature).
	// ==================================================================
	claimTx, err := x402.BuildSellerClaimTx(&x402.SellerClaimParams{
		FundingTxID:   fundingResult.TxID,
		FundingVout:   fundingResult.HTLCVout,
		FundingAmount: fundingResult.HTLCAmount,
		HTLCScript:    fundingResult.HTLCScript,
		Capsule:       capsule,
		SellerPrivKey: sellerFeeKey.PrivateKey,
		OutputAddr:    sellerPKH,
		FeeRate:       1,
	})
	require.NoError(t, err, "build seller claim tx")

	claimTxHex := hex.EncodeToString(claimTx.Bytes())
	claimTxID, err := node.SendRawTransaction(ctx, claimTxHex)
	require.NoError(t, err, "broadcast seller claim tx")
	t.Logf("seller claim txid: %s", claimTxID)
	mineOneBlock(t)

	// ==================================================================
	// Step 10: Buyer extracts capsule from seller's claim tx.
	// ==================================================================
	claimTxBytes := claimTx.Bytes()
	require.NotEmpty(t, claimTxBytes, "claim tx should serialize")

	extractedCapsule, err := x402.ParseHTLCPreimage(claimTxBytes, nil)
	require.NoError(t, err, "parse HTLC preimage from claim tx")
	require.Equal(t, capsule, extractedCapsule,
		"extracted capsule should match original capsule")
	t.Logf("extracted capsule: %x", extractedCapsule[:16])

	// Verify the extracted capsule's hash matches the HTLC lock.
	extractedHash := sha256.Sum256(extractedCapsule)
	require.Equal(t, capsuleHash, extractedHash[:],
		"SHA256(extracted capsule) should match capsule hash in HTLC")

	// ==================================================================
	// Step 11: Buyer decrypts content using the extracted capsule.
	// ==================================================================
	// Retrieve the file node from chain to get the encrypted payload.
	fileRawBytes, err := node.GetRawTransaction(ctx, fileTxIDStr)
	require.NoError(t, err, "get file node tx from chain")

	parsedFileTx, err := transaction.NewTransactionFromBytes(fileRawBytes)
	require.NoError(t, err, "parse file node tx")

	// Extract the payload from the OP_RETURN output.
	opReturnOutput := parsedFileTx.Outputs[0]
	require.True(t, opReturnOutput.LockingScript.IsData(), "output 0 should be OP_RETURN")

	pushes := extractPushData(t, opReturnOutput.LockingScript)
	require.GreaterOrEqual(t, len(pushes), 4, "OP_RETURN should have >= 4 data pushes")

	_, _, payload, err := tx.ParseOPReturnData(pushes)
	require.NoError(t, err, "parse OP_RETURN data")
	require.True(t, len(payload) > 32, "payload should contain keyHash + ciphertext")

	// Split payload into keyHash (32B) and ciphertext.
	onChainKeyHash := payload[:32]
	onChainCiphertext := payload[32:]

	// Decrypt using the capsule obtained from the HTLC claim.
	// capsule = ECDH(D_file, P_buyer).x
	// But wait -- the encryption was done with ECDH(D_file, P_file), not P_buyer.
	// For the paid mode, the buyer needs the capsule that matches the encryption.
	// The capsule used for decryption must produce the same AES key.
	//
	// In the actual protocol, the seller would re-encrypt for the buyer, or
	// the capsule would be ECDH(D_file, P_file).x (the owner's shared secret).
	// Since we encrypted with ECDH(D_file, P_file), we need that capsule.
	//
	// For this test, we verify two decryption paths:
	// Path A: Owner decrypts with their own key (standard Decrypt).
	// Path B: Buyer decrypts with the file-owner capsule via DecryptWithCapsule.

	// Path A: Owner (seller) can decrypt with their private key.
	ownerDecResult, err := method42.Decrypt(
		onChainCiphertext,
		sellerFileKey.PrivateKey,
		sellerFileKey.PublicKey,
		onChainKeyHash,
		method42.AccessPaid,
	)
	require.NoError(t, err, "owner decrypt with AccessPaid")
	assert.Equal(t, originalContent, ownerDecResult.Plaintext,
		"owner decrypted content should match original")

	// Path B: Owner acts as their own "buyer" to test DecryptWithCapsule.
	// ComputeCapsule(D_file, P_file, P_file, keyHash) produces a capsule
	// that the owner can decrypt using their own private key as the "buyer".
	ownerCapsule, err := method42.ComputeCapsule(sellerFileKey.PrivateKey, sellerFileKey.PublicKey, sellerFileKey.PublicKey, onChainKeyHash)
	require.NoError(t, err, "compute owner capsule")

	capsuleDecResult, err := method42.DecryptWithCapsule(
		onChainCiphertext,
		ownerCapsule,
		onChainKeyHash,
		sellerFileKey.PrivateKey,
		sellerFileKey.PublicKey,
	)
	require.NoError(t, err, "decrypt with owner capsule")
	assert.Equal(t, originalContent, capsuleDecResult.Plaintext,
		"capsule-decrypted content should match original")

	// ==================================================================
	// Step 12: Full buyer-specific capsule flow (encrypt for buyer).
	// ==================================================================
	// In the real protocol, the seller computes a buyer-specific XOR capsule:
	//   capsule = AES_key XOR BuyerMask
	// where AES_key = HKDF(ECDH(D_file, P_file), keyHash)
	//   and BuyerMask = HKDF(ECDH(D_file, P_buyer), keyHash)
	//
	// The buyer receives the capsule (via HTLC reveal) and recovers AES_key:
	//   buyerMask = HKDF(ECDH(D_buyer, P_file), keyHash)   [== BuyerMask by ECDH symmetry]
	//   aesKey    = capsule XOR buyerMask

	// Seller encrypts content with their own key pair (AccessPaid uses ECDH(D_file, P_file)).
	buyerEncResult, err := method42.Encrypt(
		originalContent,
		sellerFileKey.PrivateKey,
		sellerFileKey.PublicKey,
		method42.AccessPaid,
	)
	require.NoError(t, err, "encrypt for buyer (AccessPaid)")

	// Seller computes XOR capsule for this specific buyer.
	sellerSideCapsule, err := method42.ComputeCapsule(sellerFileKey.PrivateKey, sellerFileKey.PublicKey, buyerFeeKey.PublicKey, buyerEncResult.KeyHash)
	require.NoError(t, err, "seller compute buyer capsule")
	require.Len(t, sellerSideCapsule, 32, "capsule should be 32 bytes")

	// Buyer receives the capsule (via HTLC reveal) and decrypts.
	buyerDecResult, err := method42.DecryptWithCapsule(
		buyerEncResult.Ciphertext,
		sellerSideCapsule,
		buyerEncResult.KeyHash,
		buyerFeeKey.PrivateKey,
		sellerFileKey.PublicKey,
	)
	require.NoError(t, err, "buyer decrypt with capsule from seller")
	assert.Equal(t, originalContent, buyerDecResult.Plaintext,
		"buyer decrypted content should match original")
	t.Logf("buyer successfully decrypted %d bytes using capsule from HTLC", len(buyerDecResult.Plaintext))

	// ==================================================================
	// Step 13: Verify payment verification against the invoice.
	// ==================================================================
	// The HTLC payment tx is not a simple P2PKH to seller, so VerifyPayment
	// won't match (it looks for P2PKH outputs). But we verify the invoice
	// properties are consistent.
	assert.Equal(t, pricePerKB, invoice.PricePerKB)
	assert.Equal(t, fileSize, invoice.FileSize)
	assert.Equal(t, sellerAddr, invoice.PaymentAddr)
	assert.False(t, invoice.IsExpired())

	// Verify payment headers can be created from the invoice.
	headers := x402.PaymentHeadersFromInvoice(invoice)
	assert.Equal(t, invoice.Price, headers.Price)
	assert.Equal(t, invoice.PricePerKB, headers.PricePerKB)
	assert.Equal(t, invoice.FileSize, headers.FileSize)
	assert.Equal(t, invoice.ID, headers.InvoiceID)
	assert.Equal(t, invoice.Expiry, headers.Expiry)

	// ==================================================================
	// Step 14: Log summary.
	// ==================================================================
	t.Logf("--- Paid Purchase Flow Summary ---")
	t.Logf("Seller root txid:    %s", rootTxIDStr)
	t.Logf("Seller file txid:    %s", fileTxIDStr)
	t.Logf("HTLC payment txid:   %s", htlcPaymentTxID)
	t.Logf("Seller claim txid:   %s", claimTxID)
	t.Logf("Invoice:             %s (price=%d sat)", invoice.ID, invoice.Price)
	t.Logf("Capsule:             %x...", capsule[:16])
	t.Logf("CapsuleHash:         %x...", capsuleHash[:16])
	t.Logf("HTLC script size:    %d bytes", len(htlcScript))
	t.Logf("Owner decrypt:       OK (%d bytes)", len(ownerDecResult.Plaintext))
	t.Logf("Capsule decrypt:     OK (%d bytes)", len(capsuleDecResult.Plaintext))
	t.Logf("Buyer ECDH decrypt:  OK (%d bytes)", len(buyerDecResult.Plaintext))
	t.Logf("ParseHTLCPreimage:   OK (extracted %d byte capsule)", len(extractedCapsule))
	t.Logf("HTLC on-chain:       confirmed (funding + claim)")
}

// TestPaidPurchase_CryptoFlowUnit is a focused unit-level test verifying the
// complete Method 42 paid access cryptographic flow without needing a regtest
// node. It covers:
//   - Encrypt with AccessPaid
//   - ComputeCapsule + ComputeCapsuleHash
//   - ECDH commutativity between seller and buyer
//   - DecryptWithCapsule
//   - HTLC script construction and preimage extraction
func TestPaidPurchase_CryptoFlowUnit(t *testing.T) {
	// Generate two independent key pairs (seller file key + buyer key).
	sellerPrivKey, err := ec.NewPrivateKey()
	require.NoError(t, err, "generate seller private key")
	sellerPubKey := sellerPrivKey.PubKey()

	buyerPrivKey, err := ec.NewPrivateKey()
	require.NoError(t, err, "generate buyer private key")
	buyerPubKey := buyerPrivKey.PubKey()

	plaintext := []byte("Paid content: the quick brown fox jumps over the lazy dog")

	// ------------------------------------------------------------------
	// 1. Seller encrypts content (AccessPaid, own key pair).
	// ------------------------------------------------------------------
	encResult, err := method42.Encrypt(plaintext, sellerPrivKey, sellerPubKey, method42.AccessPaid)
	require.NoError(t, err)
	require.NotEmpty(t, encResult.Ciphertext)
	require.Len(t, encResult.KeyHash, 32)

	// ------------------------------------------------------------------
	// 2. Seller computes buyer-specific XOR capsule.
	// ------------------------------------------------------------------
	capsule, err := method42.ComputeCapsule(sellerPrivKey, sellerPubKey, buyerPubKey, encResult.KeyHash)
	require.NoError(t, err)
	require.Len(t, capsule, 32)

	fileTxID := bytes.Repeat([]byte{0xf0}, 32) // mock file txid for e2e test
	capsuleHash := method42.ComputeCapsuleHash(fileTxID, capsule)
	require.Len(t, capsuleHash, 32)

	// ------------------------------------------------------------------
	// 3. Buyer decrypts using capsule received from seller (via HTLC).
	// ------------------------------------------------------------------
	// The XOR capsule is buyer-specific; the buyer uses their own private key
	// and the seller's public key to unmask it and recover the AES key.
	decResult, err := method42.DecryptWithCapsule(
		encResult.Ciphertext,
		capsule,
		encResult.KeyHash,
		buyerPrivKey,
		sellerPubKey,
	)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)

	// ------------------------------------------------------------------
	// 4. Build HTLC script and verify structure.
	// ------------------------------------------------------------------
	sellerPKH := sellerPubKey.Hash()

	htlcScript, err := x402.BuildHTLC(&x402.HTLCParams{
		BuyerPubKey:  buyerPubKey.Compressed(),
		SellerPubKey: sellerPubKey.Compressed(),
		SellerAddr:   sellerPKH,
		CapsuleHash:  capsuleHash,
		Amount:       1000,
		Timeout:      x402.DefaultHTLCTimeout,
	})
	require.NoError(t, err)
	require.NotEmpty(t, htlcScript)

	// Verify script contains the capsule hash.
	assert.True(t, bytes.Contains(htlcScript, capsuleHash),
		"HTLC script should embed capsule hash")

	// Verify script contains buyer pubkey.
	assert.True(t, bytes.Contains(htlcScript, buyerPubKey.Compressed()),
		"HTLC script should embed buyer pubkey")

	// Verify script contains seller address.
	assert.True(t, bytes.Contains(htlcScript, sellerPKH),
		"HTLC script should embed seller address hash")

	// ------------------------------------------------------------------
	// 5. Build a simulated claim tx and extract preimage.
	// ------------------------------------------------------------------
	claimTx := transaction.NewTransaction()
	dummyTxID := chainhash.DoubleHashH([]byte("dummy-htlc-txid"))
	claimTx.AddInput(&transaction.TransactionInput{
		SourceTXID:       &dummyTxID,
		SourceTxOutIndex: 0,
		SequenceNumber:   0xffffffff,
	})
	dummyLockScript := script.NewFromBytes([]byte{script.OpTRUE})
	claimTx.AddOutput(&transaction.TransactionOutput{
		Satoshis:      800,
		LockingScript: dummyLockScript,
	})

	// Seller claim unlocking: <sig> <seller_pubkey> <capsule> OP_TRUE
	unlockScript := &script.Script{}
	require.NoError(t, unlockScript.AppendPushData(bytes.Repeat([]byte{0x30}, 72)))
	require.NoError(t, unlockScript.AppendPushData(sellerPubKey.Compressed()))
	require.NoError(t, unlockScript.AppendPushData(capsule))
	require.NoError(t, unlockScript.AppendOpcodes(script.OpTRUE))
	claimTx.Inputs[0].UnlockingScript = unlockScript

	extracted, err := x402.ParseHTLCPreimage(claimTx.Bytes(), nil)
	require.NoError(t, err, "extract preimage from simulated claim tx")
	assert.Equal(t, capsule, extracted, "extracted preimage should equal capsule")

	// Verify extracted preimage hashes to capsule hash.
	h := sha256.Sum256(extracted)
	assert.Equal(t, capsuleHash, h[:], "SHA256(extracted) should equal capsule hash")

	// ------------------------------------------------------------------
	// 6. Verify x402 invoice creation.
	// ------------------------------------------------------------------
	inv := x402.NewInvoice(50, uint64(len(plaintext)), "1SellerAddr", capsuleHash, 300)
	assert.Equal(t, x402.CalculatePrice(50, uint64(len(plaintext))), inv.Price)
	assert.Equal(t, capsuleHash, inv.CapsuleHash)
	assert.False(t, inv.IsExpired())

	t.Logf("Unit crypto flow: encrypt -> capsule -> HTLC -> extract -> decrypt OK")
}

// TestPaidPurchase_BuyerRefund tests the buyer refund path on regtest.
// It builds an HTLC funding tx, mines past the timeout, then spends via refund.
func TestPaidPurchase_BuyerRefund(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	sellerWallet := setupFundedWallet(t, ctx, node)
	buyerWallet := setupFundedWallet(t, ctx, node)

	sellerFeeKey, err := sellerWallet.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err)

	buyerFeeKey, err := buyerWallet.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err)

	// Fund buyer.
	buyerFeeAddr, err := script.NewAddressFromPublicKey(buyerFeeKey.PublicKey, false)
	require.NoError(t, err)
	buyerUTXO := getFundedUTXO(t, ctx, node, buyerFeeAddr.AddressString, buyerFeeKey)

	sellerPKH := sellerFeeKey.PublicKey.Hash()
	sellerPubKeyCompressed := sellerFeeKey.PublicKey.Compressed()
	buyerPKH := buyerFeeKey.PublicKey.Hash()
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)

	mineAddr, err := node.NewAddress(ctx)
	require.NoError(t, err)

	// Use a very short timeout (current block height + 1) so we can refund quickly.
	blockCount, err := node.GetBlockCount(ctx)
	require.NoError(t, err)
	timeout := uint32(blockCount + 1)

	// Build and broadcast HTLC funding tx.
	fundingResult, err := x402.BuildHTLCFundingTx(&x402.HTLCFundingParams{
		BuyerPrivKey: buyerFeeKey.PrivateKey,
		SellerPubKey: sellerPubKeyCompressed,
		SellerAddr:   sellerPKH,
		CapsuleHash:  capsuleHash,
		Amount:       1000,
		Timeout:      timeout,
		UTXOs: []*x402.HTLCUTXO{{
			TxID:         buyerUTXO.TxID,
			Vout:         buyerUTXO.Vout,
			Amount:       buyerUTXO.Amount,
			ScriptPubKey: buyerUTXO.ScriptPubKey,
		}},
		ChangeAddr: buyerPKH,
		FeeRate:    1,
	})
	require.NoError(t, err)

	htlcTxID, err := node.SendRawTransaction(ctx, hex.EncodeToString(fundingResult.RawTx))
	require.NoError(t, err)
	t.Logf("HTLC funding txid: %s (timeout at block %d)", htlcTxID, timeout)

	// Step 1: Seller pre-signs the refund tx (seller's half of 2-of-2 multisig).
	preSignResult, err := x402.BuildSellerPreSignedRefund(&x402.SellerPreSignParams{
		FundingTxID:     fundingResult.TxID,
		FundingVout:     fundingResult.HTLCVout,
		FundingAmount:   fundingResult.HTLCAmount,
		HTLCScript:      fundingResult.HTLCScript,
		SellerPrivKey:   sellerFeeKey.PrivateKey,
		BuyerOutputAddr: buyerPKH,
		Timeout:         timeout,
		FeeRate:         1,
	})
	require.NoError(t, err, "seller pre-sign refund tx")
	t.Logf("seller pre-signed refund: %d bytes, sig %d bytes",
		len(preSignResult.TxBytes), len(preSignResult.SellerSig))

	// Step 2: Buyer counter-signs (adds their half of 2-of-2 multisig).
	refundTx, err := x402.BuildBuyerRefundTx(&x402.BuyerRefundParams{
		SellerPreSignedTx: preSignResult.TxBytes,
		SellerSig:         preSignResult.SellerSig,
		HTLCScript:        fundingResult.HTLCScript,
		FundingAmount:     fundingResult.HTLCAmount,
		BuyerPrivKey:      buyerFeeKey.PrivateKey,
	})
	require.NoError(t, err, "buyer counter-sign refund tx")

	// Mine past the timeout so the nLockTime refund becomes valid.
	_, err = node.MineBlocks(ctx, 2, mineAddr)
	require.NoError(t, err)

	// Broadcast the fully-signed refund tx.
	refundTxHex := hex.EncodeToString(refundTx.Bytes())
	refundTxID, err := node.SendRawTransaction(ctx, refundTxHex)
	require.NoError(t, err, "broadcast buyer refund tx")
	t.Logf("buyer refund txid: %s", refundTxID)

	// Mine to confirm.
	_, err = node.MineBlocks(ctx, 1, mineAddr)
	require.NoError(t, err)

	// Verify refund was confirmed.
	refundRaw, err := node.GetRawTransaction(ctx, refundTxID)
	require.NoError(t, err)
	require.NotEmpty(t, refundRaw)
	t.Logf("buyer refund confirmed on-chain: %d bytes", len(refundRaw))
}

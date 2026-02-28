//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// setupFundedWallet creates a fresh HD wallet from a random 12-word mnemonic,
// derives the first fee key (m/44'/236'/0'/0/0), funds its address via the
// regtest node (mine 101 blocks + sendtoaddress + mine 1), and returns the
// wallet ready for use.
//
// This helper is designed to be reused by Task 7+ tests.
func setupFundedWallet(t *testing.T, ctx context.Context, node *testutil.RegtestNode) *wallet.Wallet {
	t.Helper()

	// Generate a fresh mnemonic and wallet.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")
	t.Logf("mnemonic: %s", mnemonic)

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	return w
}

// getFundedUTXO funds the given address via the regtest node and returns
// a tx.UTXO ready for use in transaction building. It mines 101 blocks to
// make coinbase spendable, sends 0.01 BSV to the address, and mines 1 more
// block to confirm.
//
// The returned UTXO has TxID (internal byte order), Vout, Amount (satoshis),
// ScriptPubKey, and PrivateKey all populated.
//
// This helper is designed to be reused by Task 7+ tests.
func getFundedUTXO(t *testing.T, ctx context.Context, node *testutil.RegtestNode, addr string, kp *wallet.KeyPair) *tx.UTXO {
	t.Helper()

	// Import the address so the node can track UTXOs for it.
	err := node.ImportAddress(ctx, addr)
	require.NoError(t, err, "import address")

	// Mine 101 blocks so coinbase rewards become spendable.
	nodeAddr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate node address")
	_, err = node.MineBlocks(ctx, 101, nodeAddr)
	require.NoError(t, err, "mine 101 blocks")

	// Send 0.01 BSV to the derived address.
	fundTxID, err := node.SendToAddress(ctx, addr, 0.01)
	require.NoError(t, err, "send to address")
	t.Logf("funding txid: %s", fundTxID)

	// Mine 1 block to confirm the funding transaction.
	_, err = node.MineBlocks(ctx, 1, nodeAddr)
	require.NoError(t, err, "mine confirmation block")

	// Retrieve the UTXO from the node.
	utxos, err := node.ListUnspent(ctx, addr)
	require.NoError(t, err, "list unspent")
	require.NotEmpty(t, utxos, "should have at least one UTXO")

	regtestUTXO := utxos[0]
	t.Logf("funded UTXO: %s:%d = %.8f BSV", regtestUTXO.TxID, regtestUTXO.Vout, regtestUTXO.Amount)

	// Convert the regtest UTXO to a tx.UTXO.
	// Bitcoin txids are displayed in reverse byte order; decode and reverse.
	txidBytes, err := hex.DecodeString(regtestUTXO.TxID)
	require.NoError(t, err, "decode txid hex")
	// Reverse to internal (little-endian) byte order.
	for i, j := 0, len(txidBytes)-1; i < j; i, j = i+1, j-1 {
		txidBytes[i], txidBytes[j] = txidBytes[j], txidBytes[i]
	}

	// Build the P2PKH locking script from the key pair's public key.
	scriptPubKey, err := tx.BuildP2PKHScript(kp.PublicKey)
	require.NoError(t, err, "build P2PKH script")

	// Convert BTC amount to satoshis.
	amountSat := uint64(regtestUTXO.Amount * 1e8)

	return &tx.UTXO{
		TxID:         txidBytes,
		Vout:         regtestUTXO.Vout,
		Amount:       amountSat,
		ScriptPubKey: scriptPubKey,
		PrivateKey:   kp.PrivateKey,
	}
}

// TestMetanetRootTx builds a Metanet root directory transaction, signs it,
// broadcasts to regtest, mines a confirmation block, retrieves it from
// the chain, and verifies the OP_RETURN output contains the MetaFlag.
func TestMetanetRootTx(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// ------------------------------------------------------------------
	// Step 1: Create HD wallet, derive node key and fee key.
	// ------------------------------------------------------------------
	w := setupFundedWallet(t, ctx, node)

	// Derive fee key (m/44'/236'/0'/0/0).
	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")
	t.Logf("fee key path: %s", feeKey.Path)

	// Derive node key for root directory (m/44'/236'/1'/0/0).
	nodeKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive node key")
	t.Logf("node key path: %s", nodeKey.Path)

	// ------------------------------------------------------------------
	// Step 2: Fund the fee key address.
	// ------------------------------------------------------------------
	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")
	t.Logf("fee address: %s", feeAddr.AddressString)

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)
	t.Logf("fee UTXO: txid=%x, vout=%d, amount=%d sat",
		feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Amount)

	// ------------------------------------------------------------------
	// Step 3: Build a root directory tx using MutationBatch.
	// ------------------------------------------------------------------
	payload := []byte("bitfs root directory")
	batch := tx.NewMutationBatch()
	batch.AddCreateRoot(nodeKey.PublicKey, payload)
	batch.AddFeeInput(feeUTXO)
	batch.SetChange(feeKey.PublicKey.Hash())
	batch.SetFeeRate(1)
	batchResult, err := batch.Build()
	require.NoError(t, err, "build root tx batch")
	require.NotEmpty(t, batchResult.RawTx, "unsigned tx bytes should not be empty")
	t.Logf("unsigned tx size: %d bytes", len(batchResult.RawTx))

	// ------------------------------------------------------------------
	// Step 4: Sign the batch.
	// ------------------------------------------------------------------
	signedHex, err := batch.Sign(batchResult)
	require.NoError(t, err, "sign root tx batch")
	require.NotEmpty(t, signedHex, "signed hex should not be empty")
	t.Logf("signed tx hex: %s", signedHex)

	// ------------------------------------------------------------------
	// Step 5: Broadcast via SendRawTransaction.
	// ------------------------------------------------------------------
	broadcastTxID, err := node.SendRawTransaction(ctx, signedHex)
	require.NoError(t, err, "broadcast raw transaction")
	t.Logf("broadcast txid: %s", broadcastTxID)

	// ------------------------------------------------------------------
	// Step 6: Mine 1 block to confirm.
	// ------------------------------------------------------------------
	nodeAddr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate node address for mining")
	_, err = node.MineBlocks(ctx, 1, nodeAddr)
	require.NoError(t, err, "mine confirmation block")

	// ------------------------------------------------------------------
	// Step 7: Retrieve the tx from chain via GetRawTransaction.
	// ------------------------------------------------------------------
	rawTxBytes, err := node.GetRawTransaction(ctx, broadcastTxID)
	require.NoError(t, err, "get raw transaction from chain")
	require.NotEmpty(t, rawTxBytes)
	t.Logf("retrieved tx size: %d bytes", len(rawTxBytes))

	// ------------------------------------------------------------------
	// Step 8: Parse back with transaction.NewTransactionFromBytes.
	// ------------------------------------------------------------------
	parsedTx, err := transaction.NewTransactionFromBytes(rawTxBytes)
	require.NoError(t, err, "parse transaction from bytes")

	// Verify basic structure: 1 input, at least 2 outputs.
	require.Equal(t, 1, parsedTx.InputCount(), "root tx should have 1 input")
	require.GreaterOrEqual(t, parsedTx.OutputCount(), 2,
		"root tx should have at least 2 outputs (OP_RETURN + P_node)")

	// ------------------------------------------------------------------
	// Step 9: Verify OP_RETURN output contains MetaFlag (0x6d657461).
	// ------------------------------------------------------------------
	opReturnOutput := parsedTx.Outputs[0]
	require.NotNil(t, opReturnOutput.LockingScript, "output 0 should have a locking script")
	require.True(t, opReturnOutput.LockingScript.IsData(),
		"output 0 should be an OP_RETURN (data) script")
	require.Equal(t, uint64(0), opReturnOutput.Satoshis,
		"OP_RETURN output should have 0 satoshis")

	// Search for MetaFlag bytes (0x6d657461 = "meta") in the OP_RETURN script.
	metaFlagBytes := tx.MetaFlagBytes
	scriptBytes := []byte(*opReturnOutput.LockingScript)
	require.True(t, bytes.Contains(scriptBytes, metaFlagBytes),
		"OP_RETURN script should contain MetaFlag bytes (0x6d657461)")
	t.Logf("MetaFlag found in OP_RETURN script")

	// Verify output 1 is the P_node dust output (546 sat).
	require.Equal(t, tx.DustLimit, parsedTx.Outputs[1].Satoshis,
		"output 1 should be P_node dust output (%d sat)", tx.DustLimit)

	// If there is a change output, verify it has reasonable amount.
	if parsedTx.OutputCount() > 2 {
		changeOutput := parsedTx.Outputs[2]
		require.Greater(t, changeOutput.Satoshis, tx.DustLimit,
			"change output should be above dust limit")
		t.Logf("change output: %d sat", changeOutput.Satoshis)
	}

	// ------------------------------------------------------------------
	// Step 10: Log tx details.
	// ------------------------------------------------------------------
	t.Logf("--- Metanet Root Tx Summary ---")
	t.Logf("TxID:       %s", broadcastTxID)
	t.Logf("Inputs:     %d", parsedTx.InputCount())
	t.Logf("Outputs:    %d", parsedTx.OutputCount())
	t.Logf("Output[0]:  OP_RETURN (MetaFlag + P_node + payload)")
	t.Logf("Output[1]:  P_node dust = %d sat", parsedTx.Outputs[1].Satoshis)
	if parsedTx.OutputCount() > 2 {
		t.Logf("Output[2]:  Change = %d sat", parsedTx.Outputs[2].Satoshis)
	}
	t.Logf("Node PubKey: %x", nodeKey.PublicKey.Compressed())
	t.Logf("Fee PubKey:  %x", feeKey.PublicKey.Compressed())
}

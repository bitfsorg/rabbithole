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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestFundExternalUTXO verifies the external funding workflow: a user receives
// BSV from an external source (e.g., an exchange), manually registers the UTXO
// in engine state, then uses it to build and broadcast a Metanet root tx.
//
// This simulates the `bitfs fund` command pattern where the engine does not
// control the funding transaction -- it only discovers and registers the UTXO.
func TestFundExternalUTXO(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// ==================================================================
	// Step 1: Create an engine via SetupTestEngine (offline mode).
	// ==================================================================
	eng, _ := testutil.SetupTestEngine(t)
	t.Logf("engine created in offline mode (no Chain)")

	// ==================================================================
	// Step 2: Derive the engine's fee key (m/44'/236'/0'/0/0).
	// ==================================================================
	feeKey, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")
	t.Logf("fee key path: %s", feeKey.Path)

	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "address from pubkey")
	t.Logf("fee address: %s", feeAddr.AddressString)

	// ==================================================================
	// Step 3: Fund externally via regtest RPC (NOT through the engine).
	// This simulates receiving BSV from an exchange or another wallet.
	// ==================================================================
	err = node.ImportAddress(ctx, feeAddr.AddressString)
	require.NoError(t, err, "import address into regtest node")

	// Mine 101 blocks so coinbase rewards are spendable.
	mineAddr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate mining address")
	_, err = node.MineBlocks(ctx, 101, mineAddr)
	require.NoError(t, err, "mine 101 blocks")

	// Send 0.01 BSV to the engine's fee address from the node's wallet.
	fundTxID, err := node.SendToAddress(ctx, feeAddr.AddressString, 0.01)
	require.NoError(t, err, "send 0.01 BSV to fee address externally")
	t.Logf("external funding txid: %s", fundTxID)

	// Mine 1 block to confirm.
	_, err = node.MineBlocks(ctx, 1, mineAddr)
	require.NoError(t, err, "mine confirmation block")

	// ==================================================================
	// Step 4: Retrieve UTXO details from regtest RPC.
	// ==================================================================
	utxos, err := node.ListUnspent(ctx, feeAddr.AddressString)
	require.NoError(t, err, "list unspent for fee address")
	require.NotEmpty(t, utxos, "should have at least one UTXO after external funding")

	regtestUTXO := utxos[0]
	t.Logf("external UTXO: %s:%d = %.8f BSV", regtestUTXO.TxID, regtestUTXO.Vout, regtestUTXO.Amount)

	// Convert display txid (big-endian) to internal byte order (little-endian).
	txidBytes, err := hex.DecodeString(regtestUTXO.TxID)
	require.NoError(t, err, "decode txid hex")
	for i, j := 0, len(txidBytes)-1; i < j; i, j = i+1, j-1 {
		txidBytes[i], txidBytes[j] = txidBytes[j], txidBytes[i]
	}

	// Build the P2PKH locking script for the fee key.
	scriptPubKey, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err, "build P2PKH script")

	amountSat := uint64(regtestUTXO.Amount * 1e8)

	// ==================================================================
	// Step 5: Manually register the UTXO in engine state.
	// This simulates the `bitfs fund` command that scans for external UTXOs
	// and adds them to the engine's local state.
	// ==================================================================
	eng.State.AddUTXO(&vault.UTXOState{
		TxID:         hex.EncodeToString(txidBytes),
		Vout:         regtestUTXO.Vout,
		Amount:       amountSat,
		ScriptPubKey: hex.EncodeToString(scriptPubKey),
		PubKeyHex:    hex.EncodeToString(feeKey.PublicKey.Compressed()),
		Type:         "fee",
		Spent:        false,
	})
	t.Logf("registered external UTXO in engine state: %d sats", amountSat)

	// Verify the UTXO is now discoverable via AllocateFeeUTXO.
	allocatedUTXO := eng.State.AllocateFeeUTXO(1)
	require.NotNil(t, allocatedUTXO, "engine should find the externally registered UTXO")
	assert.Equal(t, amountSat, allocatedUTXO.Amount, "allocated UTXO amount should match")
	assert.Equal(t, "fee", allocatedUTXO.Type, "allocated UTXO type should be 'fee'")
	assert.True(t, allocatedUTXO.Spent, "AllocateFeeUTXO should mark the UTXO as spent")
	t.Logf("AllocateFeeUTXO returned UTXO: %s:%d = %d sats",
		allocatedUTXO.TxID, allocatedUTXO.Vout, allocatedUTXO.Amount)

	// ==================================================================
	// Step 6: Build a root tx using the registered UTXO.
	// ==================================================================
	rootKey, err := eng.Wallet.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")
	t.Logf("root key path: %s", rootKey.Path)

	// Construct the tx.UTXO for the transaction builder.
	feeUTXO := &tx.UTXO{
		TxID:         txidBytes,
		Vout:         regtestUTXO.Vout,
		Amount:       amountSat,
		ScriptPubKey: scriptPubKey,
		PrivateKey:   feeKey.PrivateKey,
	}

	payload := []byte("external-funded root directory")
	batch := tx.NewMutationBatch()
	batch.AddCreateRoot(rootKey.PublicKey, payload)
	batch.AddFeeInput(feeUTXO)
	batch.SetChange(feeKey.PublicKey.Hash())
	batch.SetFeeRate(1)
	batchResult, err := batch.Build()
	require.NoError(t, err, "build unsigned root tx from external UTXO")
	require.NotEmpty(t, batchResult.RawTx, "unsigned tx bytes should not be empty")
	t.Logf("unsigned tx size: %d bytes", len(batchResult.RawTx))

	// ==================================================================
	// Step 7: Sign and broadcast the transaction.
	// ==================================================================
	signedHex, err := batch.Sign(batchResult)
	require.NoError(t, err, "sign metanet tx")
	require.NotEmpty(t, signedHex, "signed hex should not be empty")
	t.Logf("signed tx hex length: %d chars", len(signedHex))

	broadcastTxID, err := node.SendRawTransaction(ctx, signedHex)
	require.NoError(t, err, "broadcast raw transaction")
	t.Logf("broadcast txid: %s", broadcastTxID)

	// Mine 1 block to confirm.
	_, err = node.MineBlocks(ctx, 1, mineAddr)
	require.NoError(t, err, "mine confirmation block for root tx")

	// ==================================================================
	// Step 8: Verify the tx is on chain and structurally correct.
	// ==================================================================
	rawTxBytes, err := node.GetRawTransaction(ctx, broadcastTxID)
	require.NoError(t, err, "get raw transaction from chain")
	require.NotEmpty(t, rawTxBytes, "raw tx bytes should not be empty")
	t.Logf("retrieved tx size: %d bytes", len(rawTxBytes))

	parsedTx, err := transaction.NewTransactionFromBytes(rawTxBytes)
	require.NoError(t, err, "parse transaction from bytes")

	// Root tx should have: 1 input (fee UTXO), at least 2 outputs (OP_RETURN + P_node).
	assert.Equal(t, 1, parsedTx.InputCount(), "root tx should have 1 input")
	assert.GreaterOrEqual(t, parsedTx.OutputCount(), 2,
		"root tx should have at least 2 outputs (OP_RETURN + P_node)")

	// Output 0: OP_RETURN with MetaFlag.
	opReturnOutput := parsedTx.Outputs[0]
	require.NotNil(t, opReturnOutput.LockingScript, "output 0 should have a locking script")
	assert.True(t, opReturnOutput.LockingScript.IsData(),
		"output 0 should be an OP_RETURN (data) script")
	assert.Equal(t, uint64(0), opReturnOutput.Satoshis,
		"OP_RETURN output should have 0 satoshis")

	metaFlagBytes := tx.MetaFlagBytes
	scriptBytes := []byte(*opReturnOutput.LockingScript)
	assert.True(t, bytes.Contains(scriptBytes, metaFlagBytes),
		"OP_RETURN script should contain MetaFlag bytes (0x6d657461)")

	// Output 1: P_node dust output (546 sat).
	assert.Equal(t, tx.DustLimit, parsedTx.Outputs[1].Satoshis,
		"output 1 should be P_node dust output (%d sat)", tx.DustLimit)

	// Change output (if present) should be above dust.
	if parsedTx.OutputCount() > 2 {
		changeOutput := parsedTx.Outputs[2]
		assert.Greater(t, changeOutput.Satoshis, tx.DustLimit,
			"change output should be above dust limit")
		t.Logf("change output: %d sat", changeOutput.Satoshis)
	}

	// ==================================================================
	// Summary
	// ==================================================================
	t.Logf("--- External UTXO Funding Test Summary ---")
	t.Logf("External funding TxID: %s", fundTxID)
	t.Logf("Root Tx TxID:          %s", broadcastTxID)
	t.Logf("Inputs:                %d", parsedTx.InputCount())
	t.Logf("Outputs:               %d", parsedTx.OutputCount())
	t.Logf("Output[0]:             OP_RETURN (MetaFlag + P_node + payload)")
	t.Logf("Output[1]:             P_node dust = %d sat", parsedTx.Outputs[1].Satoshis)
	t.Logf("Node PubKey:           %x", rootKey.PublicKey.Compressed())
	t.Logf("Fee PubKey:            %x", feeKey.PublicKey.Compressed())
}

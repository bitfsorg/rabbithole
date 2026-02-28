//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestDoubleSpendRejected builds and broadcasts a valid Metanet root tx, then
// attempts to broadcast a second tx that spends the same fee UTXO. The regtest
// node must reject the second broadcast as a double-spend.
func TestDoubleSpendRejected(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// ------------------------------------------------------------------
	// Setup: funded wallet, fee key, fee UTXO.
	// ------------------------------------------------------------------
	w := setupFundedWallet(t, ctx, node)

	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "fee address from pubkey")

	feeUTXO := getFundedUTXO(t, ctx, node, feeAddr.AddressString, feeKey)
	t.Logf("fee UTXO: txid=%x, vout=%d, amount=%d sat",
		feeUTXO.TxID, feeUTXO.Vout, feeUTXO.Amount)

	// ------------------------------------------------------------------
	// First tx: build, sign, broadcast -- should succeed.
	// ------------------------------------------------------------------
	rootKey1, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key 1")

	batch1 := tx.NewMutationBatch()
	batch1.AddCreateRoot(rootKey1.PublicKey, []byte("first root tx"))
	batch1.AddFeeInput(feeUTXO)
	batch1.SetChange(feeKey.PublicKey.Hash())
	batch1.SetFeeRate(1)
	result1, err := batch1.Build()
	require.NoError(t, err, "build first root tx")

	hex1, err := batch1.Sign(result1)
	require.NoError(t, err, "sign first root tx")

	txid1, err := node.SendRawTransaction(ctx, hex1)
	require.NoError(t, err, "broadcast first root tx should succeed")
	t.Logf("first tx broadcast OK: %s", txid1)

	// ------------------------------------------------------------------
	// Second tx: same feeUTXO (already spent) -- must fail.
	// ------------------------------------------------------------------
	rootKey2, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	require.NoError(t, err, "derive root node key 2")

	batch2 := tx.NewMutationBatch()
	batch2.AddCreateRoot(rootKey2.PublicKey, []byte("second root tx - double spend"))
	batch2.AddFeeInput(feeUTXO)
	batch2.SetChange(feeKey.PublicKey.Hash())
	batch2.SetFeeRate(1)
	result2, err := batch2.Build()
	require.NoError(t, err, "build second root tx")

	hex2, err := batch2.Sign(result2)
	require.NoError(t, err, "sign second root tx")

	_, err = node.SendRawTransaction(ctx, hex2)
	require.Error(t, err, "double-spend should be rejected by regtest node")
	t.Logf("double-spend correctly rejected: %v", err)
}

// TestMalformedTxBroadcast sends invalid hex data to SendRawTransaction and
// verifies the regtest node returns an RPC error.
func TestMalformedTxBroadcast(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Send garbled hex that is not a valid serialized transaction.
	_, err := node.SendRawTransaction(ctx, "deadbeefcafebabe")
	require.Error(t, err, "malformed tx hex should be rejected")
	t.Logf("malformed tx correctly rejected: %v", err)
}

// TestInsufficientFeeUTXO attempts to build a Metanet root tx with a fee UTXO
// that only has dust-level funds (546 sat). The tx builder should reject this
// because the UTXO cannot cover the P_node dust output plus the mining fee.
func TestInsufficientFeeUTXO(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// ------------------------------------------------------------------
	// Setup: funded wallet, derive keys.
	// ------------------------------------------------------------------
	w := setupFundedWallet(t, ctx, node)

	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	require.NoError(t, err, "derive root node key")

	// Build the P2PKH script for the fee key.
	scriptPubKey, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err, "build P2PKH script")

	// ------------------------------------------------------------------
	// Create a fake UTXO with only dust-level funds (546 sat).
	// The tx builder needs DustLimit (546) for P_node output PLUS a mining
	// fee, so 546 sat total is not enough.
	// ------------------------------------------------------------------
	dustOnlyUTXO := &tx.UTXO{
		TxID:         make([]byte, 32), // dummy 32-byte txid
		Vout:         0,
		Amount:       tx.DustLimit, // 546 sat -- insufficient
		ScriptPubKey: scriptPubKey,
		PrivateKey:   feeKey.PrivateKey,
	}

	batch := tx.NewMutationBatch()
	batch.AddCreateRoot(rootKey.PublicKey, []byte("insufficient funds test"))
	batch.AddFeeInput(dustOnlyUTXO)
	batch.SetChange(feeKey.PublicKey.Hash())
	batch.SetFeeRate(1)
	_, err = batch.Build()
	require.Error(t, err, "tx build with dust-only UTXO should fail with insufficient funds")
	t.Logf("insufficient fee correctly rejected: %v", err)
}

// TestBroadcastEmptyTx sends an empty string to SendRawTransaction and
// verifies the regtest node returns an error.
func TestBroadcastEmptyTx(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := node.SendRawTransaction(ctx, "")
	require.Error(t, err, "broadcasting empty tx should be rejected")
	t.Logf("empty tx correctly rejected: %v", err)
}

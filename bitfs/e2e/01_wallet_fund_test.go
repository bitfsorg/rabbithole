//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/e2e/testutil"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestWalletFund verifies that HD wallet key derivation produces real addresses
// that can receive regtest coins. It creates a wallet, derives a fee key,
// funds the derived address via the regtest node, and confirms the UTXO exists.
func TestWalletFund(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)
	ctx := context.Background()

	// Step 1: Create a real HD wallet.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)
	t.Logf("Generated mnemonic: %s", mnemonic)

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err)

	// Step 2: Derive a fee key (m/44'/236'/0'/0/0).
	feeKey, err := w.DeriveFeeKey(0, 0)
	require.NoError(t, err)
	t.Logf("Fee key path: %s", feeKey.Path)

	// Step 3: Convert to BSV address (false = non-mainnet / regtest).
	addr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err)
	t.Logf("Fee address: %s", addr.AddressString)

	// Step 4: Import address into the regtest node wallet so ListUnspent can find it.
	err = node.ImportAddress(ctx, addr.AddressString)
	require.NoError(t, err)

	// Step 5: Fund the address.
	// Mine 101 blocks to the node's own address (coinbase needs 100 confirmations).
	nodeAddr, err := node.NewAddress(ctx)
	require.NoError(t, err)
	_, err = node.MineBlocks(ctx, 101, nodeAddr)
	require.NoError(t, err)

	// Send 0.01 BSV to our derived address.
	txid, err := node.SendToAddress(ctx, addr.AddressString, 0.01)
	require.NoError(t, err)
	t.Logf("Funding txid: %s", txid)

	// Mine 1 more block to confirm the funding transaction.
	_, err = node.MineBlocks(ctx, 1, nodeAddr)
	require.NoError(t, err)

	// Step 6: Verify the UTXO exists.
	utxos, err := node.ListUnspent(ctx, addr.AddressString)
	require.NoError(t, err)
	require.NotEmpty(t, utxos, "should have at least one UTXO")
	require.InDelta(t, 0.01, utxos[0].Amount, 0.0001)

	// Step 7: Log the UTXO details.
	t.Logf("UTXO: %s:%d = %.8f BSV", utxos[0].TxID, utxos[0].Vout, utxos[0].Amount)
}

//go:build e2e

package testutil

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/storage"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// SetupTestEngine creates a fully initialized Engine in a temporary directory
// with a fresh HD wallet (random 12-word mnemonic, regtest network) and empty
// state. The engine has no Chain configured (offline mode).
//
// Returns the engine and the temporary data directory path. The caller does
// not need to clean up the temp directory; t.TempDir() handles that.
func SetupTestEngine(t *testing.T) (*vault.Vault, string) {
	t.Helper()

	dataDir := t.TempDir()

	// Generate a fresh mnemonic and create the wallet.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	// Create an empty wallet state.
	wState := wallet.NewWalletState()

	// Initialize content-addressed file store.
	storeDir := filepath.Join(dataDir, "storage")
	store, err := storage.NewFileStore(storeDir)
	require.NoError(t, err, "init file store")

	// Create empty local state (nodes + UTXOs).
	localStatePath := filepath.Join(dataDir, "nodes.json")
	localState := vault.NewLocalState(localStatePath)

	eng := &vault.Vault{
		Wallet:  w,
		WState:  wState,
		Store:   store,
		State:   localState,
		DataDir: dataDir,
		// Chain is nil (offline mode).
	}

	t.Cleanup(func() {
		_ = eng.Close()
	})

	return eng, dataDir
}

// FundEngineWallet funds the engine's fee wallet via the regtest node. It
// derives the first fee key (m/44'/236'/0'/0/0), imports its address into
// the regtest node, mines 101 blocks to make coinbase spendable, sends
// 0.01 BSV to the derived address, mines 1 confirmation block, and adds
// the resulting UTXO to the engine's local state.
func FundEngineWallet(t *testing.T, eng *vault.Vault, node *RegtestNode) {
	t.Helper()

	ctx := context.Background()

	// Derive the first fee key (external chain, index 0).
	feeKey, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err, "derive fee key")

	// Derive the BSV address from the public key.
	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	require.NoError(t, err, "address from pubkey")

	// Import the address so the regtest node can track UTXOs for it.
	err = node.ImportAddress(ctx, feeAddr.AddressString)
	require.NoError(t, err, "import address")

	// Mine 101 blocks to make coinbase rewards spendable.
	nodeAddr, err := node.NewAddress(ctx)
	require.NoError(t, err, "generate node mining address")
	_, err = node.MineBlocks(ctx, 101, nodeAddr)
	require.NoError(t, err, "mine 101 blocks")

	// Send 0.01 BSV to the fee address.
	fundTxID, err := node.SendToAddress(ctx, feeAddr.AddressString, 0.01)
	require.NoError(t, err, "send 0.01 BSV to fee address")
	t.Logf("funding txid: %s", fundTxID)

	// Mine 1 block to confirm the funding transaction.
	_, err = node.MineBlocks(ctx, 1, nodeAddr)
	require.NoError(t, err, "mine confirmation block")

	// Retrieve the confirmed UTXO.
	utxos, err := node.ListUnspent(ctx, feeAddr.AddressString)
	require.NoError(t, err, "list unspent")
	require.NotEmpty(t, utxos, "should have at least one UTXO after funding")

	regtestUTXO := utxos[0]
	t.Logf("funded UTXO: %s:%d = %.8f BSV", regtestUTXO.TxID, regtestUTXO.Vout, regtestUTXO.Amount)

	// Convert display txid (big-endian) to internal byte order (little-endian)
	// for consistency, then store as hex in engine state.
	txidBytes, err := hex.DecodeString(regtestUTXO.TxID)
	require.NoError(t, err, "decode txid hex")
	for i, j := 0, len(txidBytes)-1; i < j; i, j = i+1, j-1 {
		txidBytes[i], txidBytes[j] = txidBytes[j], txidBytes[i]
	}

	// Build the P2PKH locking script for the fee key.
	scriptPubKey, err := tx.BuildP2PKHScript(feeKey.PublicKey)
	require.NoError(t, err, "build P2PKH script")

	// Convert BTC amount to satoshis.
	amountSat := uint64(regtestUTXO.Amount * 1e8)

	// Add the UTXO to the engine's local state.
	eng.State.AddUTXO(&vault.UTXOState{
		TxID:         hex.EncodeToString(txidBytes),
		Vout:         regtestUTXO.Vout,
		Amount:       amountSat,
		ScriptPubKey: hex.EncodeToString(scriptPubKey),
		PubKeyHex:    hex.EncodeToString(feeKey.PublicKey.Compressed()),
		Type:         "fee",
		Spent:        false,
	})

	// Advance the wallet state receive index so the engine knows index 0 is used.
	eng.WState.NextReceiveIndex = 1

	t.Logf("engine funded: %d sats at fee key index 0", amountSat)
}

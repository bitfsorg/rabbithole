//go:build integration

package integration

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/tx"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

const integrationPassword = "integration-test"

// initIntegrationEngine creates a fully initialized Engine with:
//   - HD wallet with "default" vault at index 0
//   - 20 fee UTXOs seeded (10,000 sats each)
//   - Content-addressed file store in temp directory
//   - Empty local state
func initIntegrationEngine(t *testing.T) *vault.Vault {
	t.Helper()
	dataDir := t.TempDir()

	// Generate mnemonic and seed.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "GenerateMnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "SeedFromMnemonic")

	// Encrypt and save wallet.
	encrypted, err := wallet.EncryptSeed(seed, integrationPassword)
	require.NoError(t, err, "EncryptSeed")

	err = os.WriteFile(filepath.Join(dataDir, "wallet.enc"), encrypted, 0600)
	require.NoError(t, err, "write wallet.enc")

	// Create wallet state with "default" vault at index 0.
	w, err := wallet.NewWallet(seed, &wallet.MainNet)
	require.NoError(t, err, "NewWallet")

	wState := wallet.NewWalletState()
	_, err = w.CreateVault(wState, "default")
	require.NoError(t, err, "CreateVault")

	stateData, err := encodeJSON(wState)
	require.NoError(t, err, "marshal wallet state")

	err = os.WriteFile(filepath.Join(dataDir, "state.json"), stateData, 0600)
	require.NoError(t, err, "write state.json")

	// Open engine via the standard constructor (reads wallet.enc + state.json).
	eng, err := vault.New(dataDir, integrationPassword)
	require.NoError(t, err, "engine.New")

	t.Cleanup(func() { eng.Close() })

	// Seed 20 fee UTXOs at 10,000 sats each.
	seedFeeUTXOs(t, eng, 20, 10_000)

	return eng
}

// seedFeeUTXOs creates count fake fee UTXOs with the given amount (in satoshis).
// Each UTXO uses an actual derived fee key so that lookupPrivKey succeeds when
// the engine later tries to spend them.
func seedFeeUTXOs(t *testing.T, eng *vault.Vault, count int, amount uint64) {
	t.Helper()

	for i := 0; i < count; i++ {
		idx := uint32(i)

		// Derive the actual fee key at this index.
		kp, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, idx)
		require.NoError(t, err, "DeriveFeeKey(%d)", idx)

		pubHex := hex.EncodeToString(kp.PublicKey.Compressed())

		// Build a real P2PKH locking script for this key.
		scriptPK, err := tx.BuildP2PKHScript(kp.PublicKey)
		require.NoError(t, err, "BuildP2PKHScript(%d)", idx)

		// Create a synthetic UTXO. The TxID is a deterministic fake (32 bytes).
		fakeTxID := fmt.Sprintf("%064x", idx+1) // e.g. "0000...0001"

		eng.State.AddUTXO(&vault.UTXOState{
			TxID:         fakeTxID,
			Vout:         0,
			Amount:       amount,
			ScriptPubKey: hex.EncodeToString(scriptPK),
			PubKeyHex:    pubHex,
			Type:         "fee",
			Spent:        false,
		})
	}

	// Advance NextReceiveIndex so lookupPrivKey scans far enough.
	if eng.WState.NextReceiveIndex < uint32(count) {
		eng.WState.NextReceiveIndex = uint32(count)
	}
}

// createTempFile writes content to a temporary file and returns its path.
// The file is automatically cleaned up when the test finishes.
func createTempFile(t *testing.T, content []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "bitfs-test-*")
	require.NoError(t, err, "create temp file")

	_, err = f.Write(content)
	require.NoError(t, err, "write temp file")

	err = f.Close()
	require.NoError(t, err, "close temp file")

	return f.Name()
}

// encodeJSON pretty-prints v as indented JSON.
func encodeJSON(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// hexEncode converts a byte slice to a hex string.
func hexEncode(b []byte) string {
	return hex.EncodeToString(b)
}

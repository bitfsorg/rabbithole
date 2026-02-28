//go:build e2e

package e2e

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

// TestVaultCRUD exercises wallet-level vault CRUD operations.
// No regtest node is needed — this is pure wallet state management.
func TestVaultCRUD(t *testing.T) {
	// Set up a fresh wallet for all subtests.
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err, "generate mnemonic")

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err, "derive seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	require.NoError(t, err, "create wallet")

	t.Run("create", func(t *testing.T) {
		wState := wallet.NewWalletState()

		// Initially no vaults exist.
		vaults := w.ListVaults(wState)
		assert.Empty(t, vaults, "new wallet state should have no vaults")

		// Create a vault named "photos".
		v, err := w.CreateVault(wState, "photos")
		require.NoError(t, err, "create vault 'photos'")
		require.NotNil(t, v, "created vault should not be nil")

		assert.Equal(t, "photos", v.Name, "vault name should be 'photos'")
		assert.Equal(t, uint32(0), v.AccountIndex, "first vault should get account index 0")
		assert.Nil(t, v.RootTxID, "unpublished vault should have nil RootTxID")
		assert.False(t, v.Deleted, "new vault should not be deleted")

		// Verify the vault is retrievable.
		got, err := w.GetVault(wState, "photos")
		require.NoError(t, err, "get vault 'photos'")
		assert.Equal(t, "photos", got.Name)
		assert.Equal(t, uint32(0), got.AccountIndex)

		// Verify it appears in the list.
		vaults = w.ListVaults(wState)
		assert.Len(t, vaults, 1, "should have exactly 1 vault")
		assert.Equal(t, "photos", vaults[0].Name)

		t.Logf("vault created: name=%s accountIndex=%d", v.Name, v.AccountIndex)
	})

	t.Run("list", func(t *testing.T) {
		wState := wallet.NewWalletState()

		// Create 3 vaults: "default", "documents", "music".
		names := []string{"default", "documents", "music"}
		for _, name := range names {
			_, err := w.CreateVault(wState, name)
			require.NoError(t, err, "create vault %q", name)
		}

		// List all vaults.
		vaults := w.ListVaults(wState)
		require.Len(t, vaults, 3, "should list 3 vaults")

		// Verify names and unique account indices.
		gotNames := make([]string, len(vaults))
		seenIndices := make(map[uint32]bool)
		for i, v := range vaults {
			gotNames[i] = v.Name
			assert.False(t, seenIndices[v.AccountIndex],
				"account index %d should be unique", v.AccountIndex)
			seenIndices[v.AccountIndex] = true
		}

		for _, name := range names {
			assert.Contains(t, gotNames, name, "vault %q should be in the list", name)
		}

		t.Logf("listed %d vaults: %v", len(vaults), gotNames)
	})

	t.Run("rename", func(t *testing.T) {
		wState := wallet.NewWalletState()

		// Create a vault named "old".
		v, err := w.CreateVault(wState, "old")
		require.NoError(t, err, "create vault 'old'")
		originalIndex := v.AccountIndex

		// Rename "old" to "new".
		err = w.RenameVault(wState, "old", "new")
		require.NoError(t, err, "rename vault 'old' -> 'new'")

		// GetVault("old") should fail.
		_, err = w.GetVault(wState, "old")
		require.Error(t, err, "get vault 'old' should fail after rename")
		assert.True(t, errors.Is(err, wallet.ErrVaultNotFound),
			"error should be ErrVaultNotFound, got: %v", err)

		// GetVault("new") should succeed with the same account index.
		got, err := w.GetVault(wState, "new")
		require.NoError(t, err, "get vault 'new' should succeed")
		assert.Equal(t, "new", got.Name)
		assert.Equal(t, originalIndex, got.AccountIndex,
			"account index should be preserved after rename")

		t.Logf("renamed vault: old -> new (accountIndex=%d preserved)", originalIndex)
	})

	t.Run("delete", func(t *testing.T) {
		wState := wallet.NewWalletState()

		// Create a vault named "temporary".
		_, err := w.CreateVault(wState, "temporary")
		require.NoError(t, err, "create vault 'temporary'")

		// Verify it exists.
		vaults := w.ListVaults(wState)
		require.Len(t, vaults, 1, "should have 1 vault before delete")

		// Delete the vault.
		err = w.DeleteVault(wState, "temporary")
		require.NoError(t, err, "delete vault 'temporary'")

		// ListVaults should no longer include it.
		vaults = w.ListVaults(wState)
		assert.Empty(t, vaults, "deleted vault should not appear in list")

		// GetVault should fail.
		_, err = w.GetVault(wState, "temporary")
		require.Error(t, err, "get vault 'temporary' should fail after delete")
		assert.True(t, errors.Is(err, wallet.ErrVaultNotFound),
			"error should be ErrVaultNotFound, got: %v", err)

		// Verify that the next vault gets a new (non-reused) account index.
		v2, err := w.CreateVault(wState, "replacement")
		require.NoError(t, err, "create replacement vault")
		assert.Equal(t, uint32(1), v2.AccountIndex,
			"deleted account index should not be reused")

		t.Logf("vault deleted: 'temporary' no longer accessible; replacement got index=%d", v2.AccountIndex)
	})

	t.Run("duplicate_name_error", func(t *testing.T) {
		wState := wallet.NewWalletState()

		// Create a vault named "default".
		_, err := w.CreateVault(wState, "default")
		require.NoError(t, err, "create vault 'default'")

		// Try to create another vault with the same name.
		_, err = w.CreateVault(wState, "default")
		require.Error(t, err, "creating duplicate vault should fail")
		assert.True(t, errors.Is(err, wallet.ErrVaultExists),
			"error should be ErrVaultExists, got: %v", err)

		t.Logf("correctly rejected duplicate vault name: %v", err)
	})
}
